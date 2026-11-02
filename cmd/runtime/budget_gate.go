package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/budget"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// The hub's limits on this machine (docs/hub/limits.md, «On the machine»). The machine agent keeps the list in
// <data folder>/budget.json; the runtime reads it here and applies it at the two places a chat can be stopped:
//
//   - a new chat is refused by the starter (launch.Config.Admit), and v2Launcher.Launch tells the person why;
//   - a running chat is told by the budget hook (hooksetup.BudgetHookScript) on every prompt and tool call, through
//     POST /api/budget/hook, which answers what the CLI reads as the hook's output.
//
// Without the file, or with the mode off, the gate does nothing.

const budgetHookPath = "/api/budget/hook"

// budgetGate is the core's gate over the data folder's list; the person is the machine's owner (RUNTIME_OWNER_EMAIL),
// else the only one the list names.
func (c *core) budgetGate() *budget.Gate {
	c.budgetOnce.Do(func() {
		home := c.cfg.Home
		claudeJSON := filepath.Join(home, ".claude.json")
		if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
			claudeJSON = filepath.Join(dir, ".claude.json")
		}
		codexHome := env("CODEX_HOME", filepath.Join(home, ".codex"))
		accounts := budget.Accounts{ClaudeJSON: claudeJSON, CodexHome: codexHome}
		c.budget = &budget.Gate{
			Reader:  &budget.Reader{Path: budget.Path(c.cfg.DataDir)},
			Owner:   strings.TrimSpace(os.Getenv("RUNTIME_OWNER_EMAIL")),
			Account: accounts.Of,
		}
	})
	return c.budget
}

// budgetAdmit is the reason a new chat may not start ("" when it may).
func (c *core) budgetAdmit(provider string, l *agents.Launch) string {
	model := ""
	if l != nil && l.Model != nil {
		model = *l.Model
	}
	return c.budgetGate().Admit(provider, model)
}

// budgetRoutes is the budget hook's route, open to the CLI token as the status hook is.
func (c *core) budgetRoutes(tokenOK func(*http.Request) bool) []gate.Route {
	return []gate.Route{{
		Match: func(r *http.Request) bool {
			return r.Method == http.MethodPost && apiPath(r.URL.Path) == budgetHookPath
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !tokenOK(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			q := r.URL.Query()
			provider := q.Get("provider")
			if provider != agents.Claude && provider != agents.Codex {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			session := q.Get("session")
			model := ""
			if m := c.tabLaunch(session).Model; m != nil {
				model = *m
			}
			reply := c.budgetGate().Reply(provider, q.Get("event"), session, model)
			if reply == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(reply)
		}),
	}}
}
