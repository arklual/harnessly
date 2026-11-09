package main

import (
	"os"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentnet"
	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/usagefeed"
)

// The fast mode of a GPT chat through Claude Code: the GPT relay asks the ChatGPT backend for the fast tier
// on the requests of a chat in fast mode (providers.GPTProxy). The two answers it needs come from here.

// codexFastTier says whether a model has the fast tier, by Codex's model cache: the same list a Codex chat's fast
// switch is offered by, read at each call (the cache changes when Codex refreshes it).
func codexFastTier(options catalog.LaunchConfig) func(model string) bool {
	return func(model string) bool {
		for _, m := range options.Launch("codex").Models {
			if m.ID == model {
				return m.Fast
			}
		}
		return false
	}
}

// paneSessionsTTL is how long the session a pane belongs to is kept: a pane id stays with its session for the pane's
// life, and a tmux server started again numbers its panes anew.
const paneSessionsTTL = 30 * time.Second

// paneSessions keeps which runtime sessions a pane id names, so a request does not ask tmux each time.
type paneSessions struct {
	mu   sync.Mutex
	seen map[string]paneSeen
}

type paneSeen struct {
	sessions []string
	at       time.Time
}

func (p *paneSessions) of(pane, socket string, look func(pane, socket string) []string) []string {
	key := socket + "\x00" + pane
	now := time.Now()
	p.mu.Lock()
	if s, ok := p.seen[key]; ok && now.Sub(s.at) < paneSessionsTTL {
		p.mu.Unlock()
		return s.sessions
	}
	p.mu.Unlock()
	sessions := look(pane, socket)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seen == nil || len(p.seen) > 512 {
		p.seen = map[string]paneSeen{}
	}
	p.seen[key] = paneSeen{sessions: sessions, at: now}
	return sessions
}

// gptFastPane says whether the chat in a tmux pane is in fast mode: the tab of the pane's session keeps it in its
// launch (launch.fast, written when the chat starts, resumes or turns the mode).
func (c *core) gptFastPane(panes *paneSessions) func(pane, socket string) bool {
	return func(pane, socket string) bool {
		if c.cfg.Tmux == nil || c.store == nil {
			return false
		}
		for _, s := range panes.of(pane, socket, c.cfg.Tmux.SessionsOfPane) {
			if fast, ok := c.store.TabLaunchFast(s); ok {
				return fast
			}
		}
		return false
	}
}

// claudeAuthOf reads how Claude Code signs in, as a chat would start it: the machine agent's env files over the
// runtime's environment, then Claude's own settings and sign-in (usagefeed.Local, the source the hub's limits name the
// account by). It is read at each call, so a key added or a sign-in made shows at once.
func claudeAuthOf(home, envDir string) func() string {
	var env *agentnet.EnvDir
	if envDir != "" {
		env = agentnet.NewEnvDir(envDir)
	}
	local := usagefeed.Local{Home: home, Getenv: func(provider, name string) string {
		if env != nil {
			if v := env.Merged(provider)[name]; v != "" {
				return v
			}
		}
		return os.Getenv(name)
	}}
	return func() string { return local.Identity("claude").AuthMode }
}

// claudeOnKey: Claude Code runs on an API key (or a token of another gateway), where its /fast is offered; on a
// claude.ai subscription, or signed in nowhere, it is not.
func (a *coreAgents) claudeOnKey() bool {
	return a != nil && a.claudeAuth != nil && a.claudeAuth() == "api_key"
}
