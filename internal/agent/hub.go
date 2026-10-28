package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agentlogin"
	"github.com/ly-solutions/harnessly/internal/agentnet"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/agents/install"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/preflight"
)

// The agent as a hub's machine: it dials the hub with its Ed25519 key, and the hub asks it protocol 2's operations. The
// closed cluster's operations (transcripts, state, automations…) are not served on such a link and nothing of them runs.

const (
	agentsEvery    = 5 * time.Minute
	preflightEvery = 30 * time.Minute
	switchPoll     = time.Second
	// The hub waits 15 s for most answers; these two say they may take longer.
	preflightTimeout = 20 * time.Second
	checkTimeout     = 25 * time.Second
	statusTimeout    = 40 * time.Second
	// apiKeyLimit bounds a key: real ones are ~100 characters.
	apiKeyLimit = 4096
	// The runtime is told what the policy says of the terminal this often, and this soon again after a try that failed
	// (it has not written its secret yet, or is restarting). A change of the policy tells it at once.
	runtimePolicyEvery = 15 * time.Second
	runtimePolicyRetry = 2 * time.Second
)

// hub is the state of a hub-mode agent.
type hub struct {
	a       *Agent
	machine *MachineFile
	policy  *PolicyStore
	audit   *Audit
	env     *agentnet.EnvDir
	proxies *agentnet.Proxies
	check   *agentnet.Checker
	shell   *preflight.Checker
	filter  *routeFilter
	// policyChanged wakes keepRuntimePolicy when the policy file changes.
	policyChanged chan struct{}

	// Set by start.
	ctx      context.Context
	installs *install.Manager
	logins   *agentlogin.Manager

	mu      sync.Mutex
	checks  map[string]hubproto.ProviderCheck // the last provider check of each agent, for its status
	emitted []hubproto.AgentStatus            // the statuses the hub was last told
	revoked bool

	// state is agent-link.json, for `harnessly-agent status`.
	stateMu sync.Mutex
	state   LinkState

	// bg counts the goroutines the link starts (loops, probes, refreshes): stop waits for them, so nothing writes into
	// the data folder after Run has returned.
	bg sync.WaitGroup
}

// goBG runs f on its own goroutine, counted in bg.
func (h *hub) goBG(f func()) {
	h.bg.Add(1)
	go func() {
		defer h.bg.Done()
		f()
	}()
}

func newHub(a *Agent) *hub {
	cfg := a.cfg
	h := &hub{
		a:             a,
		machine:       cfg.Machine,
		policy:        NewPolicyStore(cfg.PolicyFile, cfg.Home, cfg.DataDir),
		audit:         NewAudit(filepath.Join(cfg.DataDir, "agent-audit.log")),
		env:           agentnet.NewEnvDir(cfg.EnvDir),
		check:         &agentnet.Checker{},
		shell:         &preflight.Checker{Home: cfg.Home, Env: os.Environ()},
		checks:        map[string]hubproto.ProviderCheck{},
		policyChanged: make(chan struct{}, 1),
		state:         LinkState{PID: os.Getpid(), MachineID: cfg.Machine.MachineID, HubURL: cfg.Machine.HubURL, Version: cfg.Version},
	}
	h.proxies = agentnet.NewProxies(h.env)
	h.policy.OnPolicy = h.onPolicy
	h.policy.OnSwitch = h.onSwitch
	return h
}

// start brings up everything a hub link relies on. It returns when that is ready; the dial loop is separate.
func (h *hub) start(ctx context.Context) error {
	h.ctx = ctx
	cfg := h.a.cfg
	h.policy.Reload(true)
	if err := h.proxies.Start(); err != nil {
		Log("warn", "proxy settings not restored", F{"error", err.Error()})
	}
	// Tunnels to the runtime end at the filter, which asks the policy before anything reaches the runtime.
	if cfg.RuntimePort != 0 {
		h.filter = newRouteFilter(cfg.RuntimePort, h.machine.RuntimeBasePath, h.policy.Current, h.audit)
		h.filter.protected = h.policy.ProtectedPaths
		h.filter.protectedTrees = func() []string {
			return append(h.policy.ProtectedPaths(), cfg.MachineFile, cfg.EnvDir,
				filepath.Join(cfg.DataDir, linkStateFile), h.audit.path, h.audit.path+".1")
		}
		h.filter.roots = []string{cfg.Home, cfg.WorkDir}
		port, err := h.filter.Start()
		if err != nil {
			return fmt.Errorf("route filter: %w", err)
		}
		h.a.tunnels.runtimePort, h.a.tunnels.filesPort = port, 0
	}
	h.installs = install.NewManager(ctx, install.Deps{
		Shell: h.shell, EnvDir: cfg.EnvDir, Home: cfg.Home, JobsDir: filepath.Join(cfg.DataDir, "agent-install"),
		Preflight: h.report,
		Check: func(ctx context.Context, provider string) hubproto.ProviderCheck {
			return h.checkProvider(ctx, provider, nil, true)
		},
		Emit: func(j hubproto.Job) { h.emit(hubproto.EventJob, "job", j) },
		Done: func(string, bool) { h.goBG(func() { h.refreshAgents(true) }) },
	})
	h.logins = agentlogin.New(ctx, agentlogin.Config{
		Shell: h.shell, EnvDir: cfg.EnvDir, Dir: filepath.Join(cfg.DataDir, "agent-login"), Version: cfg.Version,
		Status:   h.oneStatus,
		Console:  h.openConsole,
		Emit:     func(l hubproto.Login) { h.emit(hubproto.EventLogin, "login", l) },
		Finished: func(string) { h.goBG(func() { h.refreshAgents(true) }) },
	})
	h.goBG(func() {
		t := time.NewTicker(switchPoll)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.policy.Reload(false)
			}
		}
	})
	h.setLink(false, "dialling the hub")
	if cfg.RuntimePort != 0 {
		h.goBG(func() { h.keepRuntimePolicy(ctx) })
	}
	h.goBG(func() { every(ctx, linkStateEvery, false, h.heartbeat) })
	h.goBG(func() { every(ctx, agentsEvery, false, func() { h.refreshAgents(false) }) })
	h.goBG(func() { every(ctx, preflightEvery, false, h.sendPreflight) })
	return nil
}

// stop runs after the link's context is done: the loops have returned or are about to.
func (h *hub) stop() {
	h.bg.Wait()
	_ = os.Remove(LinkStatePath(h.a.cfg.DataDir))
	if h.filter != nil {
		h.filter.Close()
	}
	if h.proxies != nil {
		h.proxies.Close()
	}
	if h.logins != nil {
		h.logins.Wait()
	}
	if h.installs != nil {
		h.installs.Wait()
	}
}

// hubLinks are the links of hubs.
func (h *hub) hubLinks() []*link {
	h.a.mu.Lock()
	defer h.a.mu.Unlock()
	var out []*link
	for l := range h.a.clients {
		if l.hub {
			out = append(out, l)
		}
	}
	return out
}

// emit sends an event of protocol 2 to the links that asked for it.
func (h *hub) emit(event, key string, value any) {
	frame, err := marshal(map[string]any{"event": event, key: value})
	if err != nil {
		Log("error", "event not encoded", F{"event", event}, F{"error", err.Error()})
		return
	}
	for _, l := range h.hubLinks() {
		if l.wants(event) {
			l.sendText(frame)
		}
	}
}

// setLink records whether the link is up, and why not when it is not.
func (h *hub) setLink(linked bool, note string) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	now := time.Now()
	switch {
	case linked && !h.state.Linked:
		h.state.Since = now.UnixMilli()
	case !linked:
		h.state.Since = 0
	}
	h.state.Linked, h.state.Note = linked, note
	h.writeStateLocked(now)
}

// heartbeat tells `status` the agent is still there.
func (h *hub) heartbeat() {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	h.writeStateLocked(time.Now())
}

func (h *hub) writeStateLocked(now time.Time) {
	h.state.UpdatedAt = now.UnixMilli()
	if err := WriteLinkState(LinkStatePath(h.a.cfg.DataDir), h.state); err != nil {
		Log("warn", "agent-link.json not written; `harnessly-agent status` cannot say if the machine is linked", F{"error", err.Error()})
	}
}

func (h *hub) linkUp(l *link) {
	h.setLink(true, "")
	h.audit.Record("link.up", true, nil)
	Log("info", "linked to the hub", F{"machine", h.machine.MachineID})
	h.goBG(h.sendPreflight)
	h.goBG(func() { h.refreshAgents(true) })
}

func (h *hub) linkDown(l *link) {
	note := "the link to the hub closed"
	if h.policy.Disabled() {
		note = "switched off on this machine"
	}
	h.setLink(false, note)
	h.audit.Record("link.down", true, nil)
}

// onPolicy: the policy file changed. The hub is told, and what was open under a wider policy is closed.
func (h *hub) onPolicy(old, now hubproto.Policy) {
	h.audit.Record("policy.changed", true, map[string]any{"policy": now})
	Log("info", "policy changed", F{"source", now.Source})
	if Narrowed(old, now) {
		if n := h.a.tunnels.DropAll(); n > 0 {
			Log("info", "tunnels closed by the narrower policy", F{"tunnels", n})
		}
	}
	h.emit(hubproto.EventPolicy, "policy", now)
	select {
	case h.policyChanged <- struct{}{}:
	default:
	}
}

// pushRuntimePolicy tells the runtime next door what the policy says of the terminal. The route filter in front of it
// refuses the routes of a terminal, but cannot see what a pane holds: a chat message typed into a pane whose agent has
// gone is read by a shell, and "!" starts the agent's own shell mode. With the terminal off the runtime refuses both
// (internal/chatguard), and it knows only because it is told here.
func (h *hub) pushRuntimePolicy() error {
	_, err := h.a.callRuntime(h.a.cfg.RuntimeAPI+"/policy", map[string]bool{"terminal": h.policy.Current().Terminal})
	return err
}

// keepRuntimePolicy tells the runtime at start, when the policy changes and every runtimePolicyEvery, so that a runtime
// which restarted (and starts with the terminal off) hears it again within that time.
func (h *hub) keepRuntimePolicy(ctx context.Context) {
	failing := false
	for {
		wait := runtimePolicyEvery
		if err := h.pushRuntimePolicy(); err != nil {
			wait = runtimePolicyRetry
			if !failing {
				Log("warn", "the runtime was not told the terminal policy; it keeps the terminal off for chat until it is", F{"error", err.Error()})
			}
			failing = true
		} else if failing {
			Log("info", "the runtime was told the terminal policy")
			failing = false
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-h.policyChanged:
		}
	}
}

// onSwitch: the kill switch moved. Switched off, the hub is told and the link goes; the dial loop waits for it to be on.
func (h *hub) onSwitch(off bool) {
	h.audit.Record("killswitch", true, map[string]any{"off": off})
	if !off {
		Log("info", "the agent is switched on again")
		h.setLink(false, "dialling the hub")
		return
	}
	h.setLink(false, "switched off on this machine")
	Log("warn", "the agent is switched off on this machine, closing the hub link")
	h.a.tunnels.DropAll()
	h.emit(hubproto.EventPolicy, "policy", h.policy.Current())
	for _, l := range h.hubLinks() {
		go l.closeAfterFlush(websocket.StatusNormalClosure, "switched off on the machine")
	}
}

// hello is the answer to `hello`.
func (h *hub) hello() hubproto.Hello {
	a := h.a
	features := []string{}
	if a.cfg.RuntimePort != 0 {
		// Of the closed cluster's features only the tunnel is served to a hub.
		features = append(features, "tunnel")
	}
	features = append(features, hubproto.FeaturesV2...)
	return hubproto.Hello{
		Protocol: hubproto.Version, AgentVersion: a.hello.AgentVersion, User: a.hello.User, Home: a.hello.Home,
		Hostname: a.hello.Hostname, Features: features, MachineID: h.machine.MachineID, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Profile: h.machine.Profile, Policy: h.policy.Current(),
	}
}

func (h *hub) report(ctx context.Context) hubproto.PreflightReport {
	return h.shell.Report(ctx, preflight.ReportOptions{WorkDir: h.a.cfg.WorkDir, HubURL: h.machine.HubURL})
}

func (h *hub) sendPreflight() {
	ctx, cancel := context.WithTimeout(h.ctx, preflightTimeout)
	defer cancel()
	h.emit(hubproto.EventPreflight, "report", h.report(ctx))
}

// checkProvider checks the network to a provider, through override if given, else the proxy the agent has. A check made
// with the machine's own settings is kept for the agent's status.
func (h *hub) checkProvider(ctx context.Context, provider string, override *agentnet.ProxyURL, save bool) hubproto.ProviderCheck {
	proxy := override
	if proxy == nil {
		if eff, ok := h.proxies.Effective(provider); ok {
			proxy = &eff
		}
	}
	res := h.check.Check(ctx, provider, proxy)
	if save {
		h.mu.Lock()
		h.checks[provider] = res
		h.mu.Unlock()
	}
	return res
}

// oneStatus is the status of an agent, with its last provider check.
func (h *hub) oneStatus(ctx context.Context, provider string) hubproto.AgentStatus {
	var st hubproto.AgentStatus
	switch provider {
	case hubproto.ProviderClaude:
		st = claude.Status(ctx, h.shell, h.a.cfg.EnvDir)
	case hubproto.ProviderCodex:
		st = codex.Status(ctx, h.shell, h.a.cfg.EnvDir)
	default:
		return hubproto.AgentStatus{Provider: provider, Login: hubproto.LoginUnknown}
	}
	h.mu.Lock()
	if c, ok := h.checks[provider]; ok {
		cp := c
		st.Access = &cp
	}
	h.mu.Unlock()
	return st
}

// statuses are the agents' statuses, the providers looked at in parallel.
func (h *hub) statuses(ctx context.Context, providers []string) []hubproto.AgentStatus {
	if len(providers) == 0 {
		providers = []string{hubproto.ProviderClaude, hubproto.ProviderCodex}
	}
	out := make([]hubproto.AgentStatus, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = h.oneStatus(ctx, p)
		}()
	}
	wg.Wait()
	return out
}

// refreshAgents tells the hub the agents' statuses when they differ from what it was last told (always, with force).
func (h *hub) refreshAgents(force bool) {
	if len(h.hubLinks()) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(h.ctx, statusTimeout)
	defer cancel()
	now := h.statuses(ctx, nil)
	h.mu.Lock()
	same := reflect.DeepEqual(now, h.emitted)
	h.emitted = now
	h.mu.Unlock()
	if force || !same {
		h.emit(hubproto.EventAgents, "agents", now)
	}
}

// openConsole asks the runtime for a terminal tab running command, and says where the page shows it.
func (h *hub) openConsole(_ context.Context, _, command string) (string, error) {
	raw, err := h.a.callRuntime(h.a.cfg.RuntimeAPI+"/terminal-tab", map[string]string{"command": command})
	if err != nil {
		return "", err
	}
	var tab struct {
		TabID string `json:"tabId"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(raw, &tab); err != nil || tab.TabID == "" {
		return "", errors.New("the runtime did not open a terminal")
	}
	url := tab.URL
	if url == "" {
		url = "/?tab=" + tab.TabID
	}
	return h.machine.RuntimeBasePath + url, nil
}
