package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentnet"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/install"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The operations of protocol 2. Each one is asked of the local policy first, on this machine, at this moment; what it
// refuses answers `policy_denied`. What changes something is written to the audit log, without its secrets.

var (
	jobIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

	// closedClusterOps are protocol 1's operations. A hub does not use them, and a hub link may not.
	closedClusterOps = map[string]bool{
		"transcripts.list": true, "transcripts.read": true, "state.list": true, "state.put": true, "state.mode": true,
		"state.result": true, "conversations.start": true, "docs.deliver": true, "docs.notify": true, "design.access": true,
		"design.notify": true, "eval.start": true, "eval.poll": true, "eval.stop": true, "status.send": true,
	}

	// keyVars are the variables the agents take their API key from.
	keyVars = map[string]string{hubproto.ProviderClaude: "ANTHROPIC_API_KEY", hubproto.ProviderCodex: "OPENAI_API_KEY"}
)

// sessionTTL is how long a page session made for a hub's person lasts.
const sessionTTL = 12 * time.Hour

func empty() map[string]any { return map[string]any{} }

func invalid(format string, args ...any) error {
	return hubproto.Errf(hubproto.CodeInvalid, format, args...)
}

// provider reads and checks the `provider` parameter: claude or codex. Gemini is a card in the UI and has nothing here.
func provider(req map[string]json.RawMessage) (string, error) {
	p, _ := field[string](req, "provider")
	switch p {
	case hubproto.ProviderClaude, hubproto.ProviderCodex:
		return p, nil
	case hubproto.ProviderGemini:
		return "", hubproto.Errf(hubproto.CodeUnsupported, "there is nothing for %s on a machine yet", p)
	}
	return "", invalid("provider must be claude or codex")
}

// allow is the policy's yes or no for an operation; a no is recorded and answered.
func (h *hub) allow(op string, ok bool, what string) error {
	if ok {
		return nil
	}
	h.audit.Record("policy.denied", false, map[string]any{"denied": op})
	return hubproto.Errf(hubproto.CodePolicyDenied, "%s is switched off by this machine's policy (%s)", what, h.policy.Current().Source)
}

func (h *hub) handle(l *link, req map[string]json.RawMessage) (any, func(), error) {
	op, _ := field[string](req, "op")
	if h.policy.Disabled() {
		return nil, nil, hubproto.Errf(hubproto.CodePolicyDenied, "this agent is switched off on the machine")
	}
	pol := h.policy.Current()
	switch op {
	case hubproto.OpHello:
		return h.hello(), nil, nil
	case hubproto.OpLinkSubscribe:
		events, _ := field[[]string](req, "events")
		l.subscribe(events)
		return empty(), nil, nil
	case hubproto.OpPreflightRun:
		ctx, cancel := context.WithTimeout(h.ctx, preflightTimeout)
		defer cancel()
		return h.report(ctx), nil, nil
	case hubproto.OpAgentsStatus:
		var providers []string
		for _, p := range func() []string { v, _ := field[[]string](req, "providers"); return v }() {
			if p == hubproto.ProviderClaude || p == hubproto.ProviderCodex {
				providers = append(providers, p)
			}
		}
		ctx, cancel := context.WithTimeout(h.ctx, statusTimeout)
		defer cancel()
		return map[string]any{"agents": h.statuses(ctx, providers)}, nil, nil
	case hubproto.OpAgentsInstall:
		return h.opInstall(req, pol)
	case hubproto.OpProviderCheck:
		return h.opProviderCheck(req)
	case hubproto.OpProxyGet:
		return h.proxies.Summary(), nil, nil
	case hubproto.OpProxySet, hubproto.OpProxyClear:
		return h.opProxy(op, req, pol)
	case hubproto.OpLoginStart, hubproto.OpLoginCode, hubproto.OpLoginCancel:
		return h.opLogin(op, req, pol)
	case hubproto.OpKeyDeliver:
		return h.opKeyDeliver(req, pol)
	case hubproto.OpKeyVerify:
		return h.opKeyVerify(req, pol)
	case hubproto.OpEnvDeliver, hubproto.OpEnvRemove:
		return h.opEnv(op, req, pol)
	case hubproto.OpGitkeyEnsure:
		if err := h.allow(op, pol.Env, "the git key"); err != nil {
			return nil, nil, err
		}
		pub, err := EnsureGitKeyIn(h.a.cfg.Home, h.a.cfg.DataDir, h.a.hello.Hostname)
		h.audit.Record(op, err == nil, nil)
		if err != nil {
			return nil, nil, err
		}
		return map[string]string{"publicKey": pub}, nil, nil
	case hubproto.OpRuntimeSess:
		return h.opSession(req, pol)
	case hubproto.OpTunnelOpen:
		return h.opTunnel(l, req, pol)
	}
	if closedClusterOps[op] {
		return nil, nil, hubproto.Errf(hubproto.CodePolicyDenied, "%s is not served on a hub link", op)
	}
	return nil, nil, hubproto.Errf(hubproto.CodeUnsupported, "unknown operation %s", op)
}

func (h *hub) opInstall(req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(hubproto.OpAgentsInstall, pol.Install, "installing agents"); err != nil {
		return nil, nil, err
	}
	jobID, _ := field[string](req, "jobId")
	if !jobIDPattern.MatchString(jobID) {
		return nil, nil, invalid("jobId must be 32 lower-case hex characters")
	}
	p, err := provider(req)
	if err != nil {
		return nil, nil, err
	}
	from, _ := field[string](req, "from")
	if from != "" && from != "provider" {
		return nil, nil, invalid("from must be provider or absent")
	}
	if err := h.installs.Start(jobID, p, from == "provider"); err != nil {
		if errors.Is(err, install.ErrBusy) {
			return nil, nil, hubproto.Errf(hubproto.CodeBusy, "%s", err.Error())
		}
		return nil, nil, invalid("%s", err.Error())
	}
	h.audit.Record(hubproto.OpAgentsInstall, true, map[string]any{"provider": p, "jobId": jobID, "from": from})
	return map[string]string{"jobId": jobID}, nil, nil
}

func (h *hub) opProviderCheck(req map[string]json.RawMessage) (any, func(), error) {
	p, err := provider(req)
	if err != nil {
		return nil, nil, err
	}
	var override *agentnet.ProxyURL
	if raw, _ := field[string](req, "proxyUrl"); raw != "" {
		pu, err := agentnet.ParseProxy(raw)
		if err != nil {
			return nil, nil, invalid("%s", err.Error())
		}
		override = &pu
	}
	ctx, cancel := context.WithTimeout(h.ctx, checkTimeout)
	defer cancel()
	// A check through a proxy the person is only trying says nothing about the machine's own settings: it is not kept.
	return h.checkProvider(ctx, p, override, override == nil), nil, nil
}

func (h *hub) opProxy(op string, req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(op, pol.Env, "changing the proxy"); err != nil {
		return nil, nil, err
	}
	scope, _ := field[string](req, "scope")
	if !agentnet.ValidScope(scope) {
		return nil, nil, invalid("scope must be all, claude or codex")
	}
	if op == hubproto.OpProxyClear {
		sum, err := h.proxies.Clear(scope)
		h.audit.Record(op, err == nil, map[string]any{"scope": scope})
		if err != nil {
			return nil, nil, err
		}
		return sum, nil, nil
	}
	raw, _ := field[string](req, "url")
	pu, err := agentnet.ParseProxy(raw)
	if err != nil {
		return nil, nil, invalid("%s", err.Error())
	}
	sum, err := h.proxies.Set(scope, raw)
	// The address without its login is what the log may hold.
	h.audit.Record(op, err == nil, map[string]any{"scope": scope, "proxy": pu.Scheme + "://" + pu.HostPort()})
	if err != nil {
		return nil, nil, err
	}
	return sum, nil, nil
}

func (h *hub) opLogin(op string, req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(op, pol.AgentLogin, "signing agents in"); err != nil {
		return nil, nil, err
	}
	loginID, _ := field[string](req, "loginId")
	switch op {
	case hubproto.OpLoginStart:
		p, err := provider(req)
		if err != nil {
			return nil, nil, err
		}
		method, _ := field[string](req, "method")
		// A console login is a terminal tab; without the terminal there is none.
		if err := h.allow(op, pol.Terminal || method != hubproto.MethodConsole, "a login in the console"); err != nil {
			return nil, nil, err
		}
		login, err := h.logins.Start(loginID, p, method)
		h.audit.Record(op, err == nil, map[string]any{"provider": p, "method": method, "loginId": loginID})
		if err != nil {
			return nil, nil, err
		}
		return login, nil, nil
	case hubproto.OpLoginCode:
		code, _ := field[string](req, "code")
		err := h.logins.Code(loginID, code)
		// What was typed is not recorded, only that a code came.
		h.audit.Record(op, err == nil, map[string]any{"loginId": loginID})
		if err != nil {
			return nil, nil, err
		}
		return empty(), nil, nil
	default:
		err := h.logins.Cancel(loginID)
		h.audit.Record(op, err == nil, map[string]any{"loginId": loginID})
		if err != nil {
			return nil, nil, err
		}
		return empty(), nil, nil
	}
}

// openEnvelope opens what the browser sealed to this machine for purpose.
func (h *hub) openEnvelope(req map[string]json.RawMessage, purpose string) ([]byte, error) {
	env, ok := field[hubproto.Envelope](req, "envelope")
	if !ok {
		return nil, invalid("envelope is missing")
	}
	plain, err := hubproto.Open(h.machine.SealingKey(), h.machine.MachineID, purpose, env)
	if err != nil {
		// Nothing of why: an envelope for another machine or purpose, a damaged one and a forged one look alike.
		return nil, invalid("the envelope does not open on this machine")
	}
	return plain, nil
}

// validAPIKey: a key is one printable word.
func validAPIKey(k string) bool {
	if k == "" || len(k) > apiKeyLimit {
		return false
	}
	for _, r := range k {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

func (h *hub) opKeyDeliver(req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(hubproto.OpKeyDeliver, pol.Keys, "saving keys"); err != nil {
		return nil, nil, err
	}
	p, err := provider(req)
	if err != nil {
		return nil, nil, err
	}
	plain, err := h.openEnvelope(req, "key:"+p)
	if err != nil {
		return nil, nil, err
	}
	key := strings.TrimSpace(string(plain))
	if !validAPIKey(key) {
		return nil, nil, invalid("that is not an API key")
	}
	if err := h.env.Set(p, map[string]string{keyVars[p]: key}); err != nil {
		h.audit.Record(hubproto.OpKeyDeliver, false, map[string]any{"provider": p})
		return nil, nil, err
	}
	if p == hubproto.ProviderCodex {
		h.codexLoginWithKey(key)
	}
	h.audit.Record(hubproto.OpKeyDeliver, true, map[string]any{"provider": p})
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
	defer cancel()
	res := h.keyCheck(ctx, p, key)
	res.Saved = true
	h.goBG(func() { h.refreshAgents(true) })
	return res, nil, nil
}

// codexLoginWithKey hands the key to `codex login --with-api-key`: Codex does not sign in from the variable alone. A
// failure is not the key's fault and the variable is saved anyway, so it is only logged.
func (h *hub) codexLoginWithKey(key string) {
	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancel()
	script := agents.EnvSourcePrefix(h.a.cfg.EnvDir, agents.Codex) + agents.FindBinary("codex") +
		`if [ -z "$bin" ]; then exit 0; fi
printf '%s' "$HL_API_KEY" | "$bin" login --with-api-key >/dev/null 2>&1`
	res, err := h.shell.RunLogin(ctx, script, "HL_API_KEY="+key)
	if err != nil || res.Exit != 0 {
		Log("warn", "codex did not take the key with `login --with-api-key`; the variable is saved")
	}
}

// keyCheck asks the provider about a key through the proxy the agent has.
func (h *hub) keyCheck(ctx context.Context, p, key string) hubproto.KeyCheck {
	var proxy *agentnet.ProxyURL
	if eff, ok := h.proxies.Effective(p); ok {
		proxy = &eff
	}
	return h.check.CheckKey(ctx, p, key, proxy)
}

func (h *hub) opKeyVerify(req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(hubproto.OpKeyVerify, pol.Keys, "checking keys"); err != nil {
		return nil, nil, err
	}
	p, err := provider(req)
	if err != nil {
		return nil, nil, err
	}
	// The key a login shell of the account sees: ours, or one the person exported themselves.
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
	defer cancel()
	name := keyVars[p]
	res, err := h.shell.RunLogin(ctx, agents.EnvSourcePrefix(h.a.cfg.EnvDir, p)+`printf '%s' "$`+name+`"`)
	if err != nil {
		return nil, nil, fmt.Errorf("the login shell did not answer: %w", err)
	}
	return h.keyCheck(ctx, p, strings.TrimSpace(res.Stdout)), nil, nil
}

func (h *hub) opEnv(op string, req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(op, pol.Env, "saving secrets"); err != nil {
		return nil, nil, err
	}
	name, _ := field[string](req, "name")
	check := agentnet.CheckName
	if op == hubproto.OpEnvRemove {
		check = agentnet.CheckRemove
	}
	if err := check(name); err != nil {
		return nil, nil, invalid("%s", err.Error())
	}
	if op == hubproto.OpEnvDeliver {
		plain, err := h.openEnvelope(req, "env:"+name)
		if err != nil {
			return nil, nil, err
		}
		value := string(plain)
		if strings.ContainsRune(value, 0) {
			return nil, nil, invalid("a secret cannot hold a NUL character")
		}
		if err := h.env.Set(agentnet.ScopeAll, map[string]string{name: value}); err != nil {
			h.audit.Record(op, false, map[string]any{"name": name})
			return nil, nil, err
		}
	} else if err := h.env.Unset(agentnet.ScopeAll, name); err != nil {
		h.audit.Record(op, false, map[string]any{"name": name})
		return nil, nil, err
	}
	h.audit.Record(op, true, map[string]any{"name": name})
	names := h.env.Names(agentnet.ScopeAll, agentnet.IsProxyName)
	if names == nil {
		names = []string{}
	}
	return map[string]any{"env": names}, nil, nil
}

func (h *hub) opSession(req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(hubproto.OpRuntimeSess, pol.RuntimeSession, "opening the runtime through the hub"); err != nil {
		return nil, nil, err
	}
	subject, _ := field[string](req, "subject")
	subject = strings.TrimSpace(subject)
	if subject == "" || len(subject) > 254 || strings.ContainsFunc(subject, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return nil, nil, invalid("subject must be the person's email")
	}
	secret := h.a.sessionSecret()
	if secret == nil {
		return nil, nil, errors.New("the runtime has not written its secret yet")
	}
	token := SignHS256(secret, subject, "", sessionTTL)
	h.audit.Record(hubproto.OpRuntimeSess, true, map[string]any{"subject": subject})
	return hubproto.SessionGrant{CookieName: h.a.cfg.RuntimeCookie, Token: token, ExpiresAt: time.Now().Add(sessionTTL).UnixMilli()}, nil, nil
}

func (h *hub) opTunnel(l *link, req map[string]json.RawMessage, pol hubproto.Policy) (any, func(), error) {
	if err := h.allow(hubproto.OpTunnelOpen, pol.RuntimeSession, "opening the runtime through the hub"); err != nil {
		return nil, nil, err
	}
	spec, _ := field[TunnelOpen](req, "spec")
	if spec.Target != "runtime" {
		return nil, nil, invalid("a hub may open tunnels to the runtime only")
	}
	start, err := h.a.tunnels.Open(l, spec)
	if err != nil {
		return nil, nil, err
	}
	return map[string]bool{"opened": true}, start, nil
}
