package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"harnessly/internal/agent"
	"harnessly/internal/pluginreload"
	"harnessly/internal/prompts"
	"harnessly/internal/screen"
	"harnessly/internal/status"
	"harnessly/internal/tmux"
)

// The reload of plugins and skills in idle Claude tabs after the plugin reconciler changed them (internal/pluginreload).
// In front of the old runtime it is the old runtime's (it keeps the applied marks); alone, the core sweeps.

const reloadSweep = 5 * time.Second

type pluginReloader struct {
	c        *core
	markFile string
	applied  *pluginreload.Applied

	mu      sync.Mutex
	mark    pluginreload.Mark
	markMod time.Time
}

// newPluginReloader watches the reconciler's mark (RUNTIME_PLUGIN_RELOAD_MARK); nil without one.
func newPluginReloader(c *core) *pluginReloader {
	mark := os.Getenv("RUNTIME_PLUGIN_RELOAD_MARK")
	if mark == "" {
		return nil
	}
	return &pluginReloader{c: c, markFile: mark, applied: &pluginreload.Applied{File: filepath.Join(c.cfg.DataDir, "claude-reload-applied.json")}}
}

// readMark is the mark, read again when the file changed; an unreadable one keeps the last (the reconciler replaces the
// file whole).
func (r *pluginReloader) readMark() pluginreload.Mark {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := os.Stat(r.markFile)
	if err != nil {
		return nil
	}
	if !st.ModTime().Equal(r.markMod) {
		if raw, err := os.ReadFile(r.markFile); err == nil {
			if m, ok := pluginreload.ParseMark(raw); ok {
				r.mark, r.markMod = m, st.ModTime()
			}
		}
	}
	return r.mark
}

// launched: a Claude the platform starts loads the plugins as they are now; nothing to reload in it.
func (r *pluginReloader) launched(tabID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	mark := r.mark
	r.mu.Unlock()
	if mark == nil {
		return
	}
	next := pluginreload.Apply(nil, mark, "", "settled")
	r.applied.Set(tabID, next)
	if err := r.applied.Save(); err != nil {
		agent.Log("warn", "core: applied reload marks not saved", agent.F{K: "err", V: err.Error()})
	}
}

func (r *pluginReloader) run(ctx context.Context, m *status.Manager, svc *prompts.Service) {
	t := time.NewTicker(reloadSweep)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.sweep(m, svc)
		}
	}
}

func (r *pluginReloader) sweep(m *status.Manager, svc *prompts.Service) {
	mark := r.readMark()
	if mark == nil {
		return
	}
	sources := m.AgentSources()
	live := map[string]bool{}
	for _, s := range sources {
		live[s.TabID] = true
	}
	changed := false
	for _, id := range r.applied.Tabs() {
		if len(sources) > 0 && !live[id] {
			r.applied.Delete(id)
			changed = true
		}
	}
	// One tab at a time: a reload watches the screen for a moment, and the sweep must not crowd tmux.
	for _, s := range sources {
		applied := r.applied.Get(s.TabID)
		queued := len(svc.Queue().List(s.TabID)) > 0
		d := pluginreload.Decide(pluginreload.Tab{PanelType: s.PanelType, Provider: s.Provider, CliState: s.CliState, Queued: queued}, mark, applied)
		if d.Action != "insert" {
			continue
		}
		command := pluginreload.Commands[d.Kind]
		outcome := r.reloadTab(svc, s.TmuxSession, command)
		if outcome == "applied" {
			agent.Log("info", "core: plugins changed, reload typed into an idle Claude", agent.F{K: "tabId", V: s.TabID}, agent.F{K: "command", V: command})
		}
		if next := pluginreload.Apply(applied, mark, d.Kind, outcome); outcome == "applied" || outcome == "settled" {
			r.applied.Set(s.TabID, next)
			changed = true
		}
	}
	if changed {
		if err := r.applied.Save(); err != nil {
			agent.Log("warn", "core: applied reload marks not saved", agent.F{K: "err", V: err.Error()})
		}
	}
}

func (r *pluginReloader) reloadTab(svc *prompts.Service, session, command string) string {
	tm := r.c.cfg.Tmux
	var view *pluginreload.Screen
	if styled, ok := tm.CaptureStyled(session); ok {
		st := screen.Read(styled)
		view = &pluginreload.Screen{Busy: st.Busy, Menu: st.Menu != nil}
		if st.InputLine {
			view.Input = &st.Input
		}
	}
	switch pluginreload.ScreenDecision(view) {
	case "wait":
		return "wait"
	case "probe":
		if p := tm.PaneCurrentCommand(session); p == "" || tmux.SafeShells[p] {
			return "settled"
		}
		return "wait"
	}
	res := svc.RunCommand(prompts.RunInput{Session: session, Provider: prompts.ProviderClaude, Line: command})
	code := ""
	if e, ok := res.Body.(*prompts.RunError); ok {
		code = e.Error
	}
	return pluginreload.Outcome(res.Status, code)
}
