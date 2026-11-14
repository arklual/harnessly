package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// OAuthStub is the Claude Code OAuth token of a profile's chat. Claude Code takes the key of apiKeyHelper before an
// OAuth token, so the stub changes nothing for the chat itself; it is for a `claude` the chat's agent starts from Bash,
// which inherits the environment but not the helper's key: without a token of its own that one would fall back to the
// person's subscription and send it to the vendor.
const OAuthStub = "harnessly-no-oauth"

var (
	// ErrNotClaude: only a profile that runs through Claude Code has a settings file.
	ErrNotClaude = errors.New("providers: the profile does not run through Claude Code")
	// ErrUnknownModel: the model is none of the preset's.
	ErrUnknownModel = errors.New("providers: the preset has no such model")
	// ErrNoSecret: a secret variable of the profile has no value in the safe.
	ErrNoSecret = errors.New("providers: a secret variable of the profile has no value in the safe")
	// ErrSubscriptionToken: the key or a secret variable of a provider is the token of a Claude subscription's sign-in.
	// The machine has one such sign-in, the CLI's own; a chat is not started on a second one.
	ErrSubscriptionToken = errors.New("providers: a provider holds the token of a Claude subscription, not a key of the API")
	// ErrSwitchOff: the profile's switch is off, or its proxy has no address yet.
	ErrSwitchOff = errors.New("providers: the profile is switched off")

	slugRe = regexp.MustCompile(`[^a-z0-9]+`)
)

// ClaudeEnv is the environment block of a profile's Claude Code settings for a chat on model. Every variable that could
// carry an Anthropic credential to the vendor is blanked or stubbed; the vendor's key is not here, it is read from a
// file by apiKeyHelper.
func (p Preset) ClaudeEnv(model Model) map[string]string { return p.claudeEnv(p.BaseURL, model) }

// claudeEnv is ClaudeEnv with Claude Code sent to base: a switched profile's proxy.
func (p Preset) claudeEnv(base string, model Model) map[string]string {
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       strings.TrimRight(base, "/"),
		"ANTHROPIC_MODEL":                          model.ID,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             model.ID,
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           model.ID,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            p.Small,
		"CLAUDE_CODE_SUBAGENT_MODEL":               model.ID,
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           strconv.Itoa(model.Window),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"ANTHROPIC_AUTH_TOKEN":                     "",
		"ANTHROPIC_API_KEY":                        "",
		"CLAUDE_CODE_OAUTH_TOKEN":                  OAuthStub,
	}
	if p.Custom && model.Window == 0 {
		// A provider the person added says nothing of its models' windows: Claude Code keeps its own.
		delete(env, "CLAUDE_CODE_MAX_CONTEXT_TOKENS")
	}
	for k, v := range p.Env {
		env[k] = v
	}
	return env
}

// SettingsFile writes (rewritten only when its content changes) the `--settings` file of a Claude Code chat on a
// profile and a model, and gives its path. It is a copy of the hooks settings file (hooksPath) with the profile's
// environment and an apiKeyHelper that reads the key from its file; a variable the hooks file already had in `env` is
// kept unless the profile sets it. The key itself is never written here. The file is named after the profile and the
// model, because the context window is the model's. A switched profile's chat goes to its proxy, with the proxy's key.
func (s *Store) SettingsFile(id, model, hooksPath string) (string, error) {
	p, ok := Find(id)
	if !ok {
		return "", fmt.Errorf("providers: no profile %q", id)
	}
	if p.Via != "claude" {
		return "", ErrNotClaude
	}
	m, ok := p.ModelOf(model)
	if !ok {
		return "", ErrUnknownModel
	}
	s.refresh(id)
	base, keyFile := p.BaseURL, s.KeyFile(id)
	if p.Switched {
		sw := s.SwitchOf(id)
		var ok bool
		if e, starts := sw.(interface{ Ensure(context.Context) error }); starts {
			// A switch on by default: its proxy is started by the first chat that needs it.
			ctx, cancel := context.WithTimeout(context.Background(), proxyLaunchWait)
			err := e.Ensure(ctx)
			cancel()
			if err != nil {
				return "", err
			}
		}
		if sw != nil {
			base, keyFile, ok = sw.Endpoint()
		}
		if !ok {
			return "", ErrSwitchOff
		}
	}
	if p.Proxied != "" {
		if s.Proxy == nil {
			return "", ErrSwitchOff
		}
		ctx, cancel := context.WithTimeout(context.Background(), proxyLaunchWait)
		defer cancel()
		var err error
		if base, keyFile, err = s.Proxy.ExtEndpoint(ctx); err != nil {
			return "", err
		}
	}
	raw, err := os.ReadFile(hooksPath)
	if err != nil {
		return "", err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", err
	}
	env := map[string]any{}
	if have, ok := settings["env"].(map[string]any); ok {
		for k, v := range have {
			env[k] = v
		}
	}
	for k, v := range p.claudeEnv(base, m) {
		env[k] = v
	}
	// What the person typed for the provider replaces whatever stands there: the values come from the safe into this
	// file (0600, in the profiles' folder of 0700) and nowhere else.
	var taken []string
	for name, ref := range p.SecretEnv {
		if !p.Custom || s.Safe == nil {
			return "", ErrNoSecret
		}
		val, err := s.Safe(ref)
		if val = strings.TrimSpace(val); err != nil || val == "" {
			return "", fmt.Errorf("%w: %s", ErrNoSecret, name)
		}
		if agents.ClaudeSubscriptionToken(val) {
			return "", fmt.Errorf("%w: %s", ErrSubscriptionToken, name)
		}
		env[name] = val
		taken = append(taken, val)
	}
	// An agent that prints its environment prints these: the feed hides them from now on.
	s.hide(taken)
	settings["env"] = env
	if p.Proxied != "" {
		// The proxy's own key: the provider's stays with the runtime's relay.
		settings["apiKeyHelper"] = "cat " + shellQuote(keyFile)
	} else if p.Custom && (p.ownCredential() || !s.Configured(id)) {
		// The provider's own token goes in the environment; a helper would be asked before it.
		delete(settings, "apiKeyHelper")
	} else {
		settings["apiKeyHelper"] = "cat " + shellQuote(keyFile)
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(s.Dir, id+"."+strings.Trim(slugRe.ReplaceAllString(strings.ToLower(model), "-"), "-")+".settings.json")
	if have, err := os.ReadFile(target); err == nil && string(have) == string(body) {
		return target, nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// proxyLaunchWait bounds the start of the proxy a chat on an OpenAI-compatible provider waits for: its first start
// downloads it.
const proxyLaunchWait = 2 * time.Minute

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
