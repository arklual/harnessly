package launch

import (
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/state"
)

// ResumeTmux is what the startup resume asks of tmux.
type ResumeTmux interface {
	HasSession(name string) bool
	CreateSession(name string, cols, rows int, cwd string) error
	PaneCurrentCommand(name string) string
	PanePID(name string) int
	SendKeysSeparated(name, text string) error
}

// ResumeConfig is what the startup resume reads besides the starter's stores.
type ResumeConfig struct {
	Tmux ResumeTmux
	// Installed is the preflight of an agent CLI: whether it runs, and (for the log) whether its binary was found off PATH.
	Installed func(provider string) (installed, binaryFound bool)
	// LastActivity is when a conversation last moved (epoch ms), nil when unknown.
	LastActivity func(provider, sessionID string) *float64
	Sleep        sleep.Config
	// ChildPIDs and Cmdline read the processes under a pane.
	ChildPIDs func(pid int) []int
	Cmdline   func(pid int) (string, bool)
	Log       func(level, msg string, kv ...any)
}

// Target is an agent tab to resume.
type Target struct {
	WorkspaceID, TabID, TmuxSession, SessionID, Provider string
	Launch                                               *agents.Launch
}

var safeShells = map[string]bool{"bash": true, "zsh": true, "fish": true, "sh": true, "dash": true}

func (s *Starter) log(rc ResumeConfig, level, msg string, kv ...any) {
	if rc.Log != nil {
		rc.Log(level, msg, kv...)
	}
}

// ResumeTargets are the agent tabs to resume at startup: only conversations that were working or were touched
// recently. A machine that resumed forty tabs at boot ran out of memory; the others stay chats, resumed by their next
// prompt.
func (s *Starter) ResumeTargets(rc ResumeConfig) []Target {
	var targets []Target
	now := s.cfg.Now()
	skipped := 0
	type pre struct{ installed, found bool }
	preflights := map[string]pre{}
	for _, v := range s.cfg.Store.Workspaces().Array("workspaces") {
		ws, ok := v.(*jsvalue.Object)
		if !ok {
			continue
		}
		wsID := ws.String("id")
		layout := s.cfg.Store.Layout(wsID)
		if layout == nil {
			continue
		}
		for _, tab := range state.AllTabs(layout) {
			pt, _ := tab.Get("panelType")
			p, _ := pt.(string)
			provider := agents.ProviderOfPanel(p)
			if provider == "" {
				continue
			}
			pf, seen := preflights[provider]
			if !seen {
				pf.installed, pf.found = rc.Installed(provider)
				preflights[provider] = pf
			}
			if !pf.installed {
				reason := "not installed"
				if provider == agents.Claude && pf.found {
					reason = "PATH not configured"
				}
				s.log(rc, "info", "skip resume: "+reason, "provider", provider, "tabId", tab.String("id"))
				continue
			}
			sid, _ := state.Provider(provider).SessionID(tab).(string)
			if sid == "" {
				continue
			}
			var last *float64
			if rc.LastActivity != nil {
				last = rc.LastActivity(provider, sid)
			}
			if !sleep.ShouldResumeAtStartup(tab.String("cliState"), last, now, rc.Sleep) {
				skipped++
				continue
			}
			targets = append(targets, Target{WorkspaceID: wsID, TabID: tab.String("id"), TmuxSession: tab.String("sessionName"),
				SessionID: sid, Provider: provider, Launch: tabLaunch(s.cfg.Store.TabLaunch(tab))})
		}
	}
	if skipped > 0 {
		s.log(rc, "info", "quiet agent tabs left asleep; their next prompt resumes them", "count", skipped)
	}
	return targets
}

func tabLaunch(o *jsvalue.Object) *agents.Launch {
	if o == nil {
		return nil
	}
	l := &agents.Launch{}
	if m, ok := o.Get("model"); ok {
		if s, isStr := m.(string); isStr {
			l.Model = &s
		}
	}
	if e, ok := o.Get("effort"); ok {
		if s, isStr := e.(string); isStr {
			l.Effort = &s
		}
	}
	return l
}

// processProvider is the agent CLI a pane's foreground command is, "" for none.
func processProvider(command string, args []string) string {
	switch {
	case claude.MatchesProcess(command, args):
		return agents.Claude
	case codex.MatchesProcess(command, args):
		return agents.Codex
	}
	return ""
}

func (s *Starter) foregroundArgs(rc ResumeConfig, session string) []string {
	pid := rc.Tmux.PanePID(session)
	if pid <= 0 || rc.ChildPIDs == nil || rc.Cmdline == nil {
		return nil
	}
	for _, c := range rc.ChildPIDs(pid) {
		if args, ok := rc.Cmdline(c); ok && args != "" {
			return strings.Fields(args)
		}
	}
	return nil
}

func (s *Starter) sendResume(rc ResumeConfig, t Target) bool {
	command := rc.Tmux.PaneCurrentCommand(t.TmuxSession)
	if command == "" {
		s.log(rc, "warn", "cannot check the pane's process", "session", t.TmuxSession)
		return false
	}
	if !safeShells[command] {
		var args []string
		if command == "node" {
			args = s.foregroundArgs(rc, t.TmuxSession)
		}
		// The agent already runs: nothing to do. Anything else holds the terminal.
		return processProvider(command, args) == t.Provider
	}
	line, err := s.cfg.Lines.Resume(t.Provider, t.SessionID, t.WorkspaceID, t.Launch)
	if err == nil {
		err = rc.Tmux.SendKeysSeparated(t.TmuxSession, line)
	}
	if err != nil {
		s.log(rc, "error", "resume failed", "session", t.TmuxSession, "err", err.Error())
		return false
	}
	if k := s.keeper(); k != nil {
		k.MarkAgentLaunch(t.TabID, false)
	}
	return true
}

// ExecuteResume makes the targets' missing tmux sessions one after another (the first one starts the server), gives
// their shells a moment once, then types the resume lines in parallel.
func (s *Starter) ExecuteResume(rc ResumeConfig, targets []Target) {
	created := false
	for _, t := range targets {
		if !rc.Tmux.HasSession(t.TmuxSession) {
			if err := rc.Tmux.CreateSession(t.TmuxSession, 80, 24, ""); err != nil {
				s.log(rc, "error", "auto-resume stopped: session not created", "session", t.TmuxSession, "err", err.Error())
				return
			}
			created = true
		}
	}
	if created {
		time.Sleep(s.cfg.ShellReady)
	}
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.sendResume(rc, t)
		}()
	}
	wg.Wait()
}

// AutoResume finds the targets and resumes them in the background; it answers how many there are.
func (s *Starter) AutoResume(rc ResumeConfig) int {
	targets := s.ResumeTargets(rc)
	if len(targets) > 0 {
		go s.ExecuteResume(rc, targets)
	}
	return len(targets)
}
