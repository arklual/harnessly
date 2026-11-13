package launch

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/typist"
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
	// Owned says the tab's tmux session is a chat's, whose owner resumes it itself (convo): such a tab is no target.
	// nil: no tab is.
	Owned func(tmuxSession string) bool
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

// shellStarting are the programs a new pane runs before its shell (tmux.ShellLaunchCommand: `nice -n N env -i … $SHELL
// -l`): a pane showing one is a shell on its way, whoever made the session (a tmux reset makes them in Store.Init,
// before the resume looks).
var shellStarting = map[string]bool{"nice": true, "env": true}

func (s *Starter) log(rc ResumeConfig, level, msg string, kv ...any) {
	if rc.Log != nil {
		rc.Log(level, msg, kv...)
	}
}

// RememberStartStates keeps each agent tab's state as the last run left it, for the next ResumeTargets. Call it before
// the status keeper starts: a turn that was running is settled to idle as soon as the keeper sees no agent in its pane,
// which after a restart it never does, and the resume would then take the working agent for a quiet one.
func (s *Starter) RememberStartStates() {
	states := map[string]string{}
	for _, v := range s.cfg.Store.Workspaces().Array("workspaces") {
		ws, ok := v.(*jsvalue.Object)
		if !ok {
			continue
		}
		layout := s.cfg.Store.Layout(ws.String("id"))
		if layout == nil {
			continue
		}
		for _, tab := range state.AllTabs(layout) {
			states[tab.String("id")] = tab.String("cliState")
		}
	}
	s.mu.Lock()
	s.startStates = states
	s.mu.Unlock()
}

// ResumeTargets are the agent tabs to resume at startup: only conversations that were working or were touched
// recently. A machine that resumed forty tabs at boot ran out of memory; the others stay chats, resumed by their next
// prompt.
func (s *Starter) ResumeTargets(rc ResumeConfig) []Target {
	var targets []Target
	now := s.cfg.Now()
	s.mu.Lock()
	remembered := s.startStates
	s.startStates = nil
	s.mu.Unlock()
	skipped, owned := 0, 0
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
			if rc.Owned != nil && rc.Owned(tab.String("sessionName")) {
				owned++
				continue
			}
			cliState := tab.String("cliState")
			if st, ok := remembered[tab.String("id")]; ok {
				cliState = st
			}
			// The last activity is read off the transcript: only for a tab it can decide for.
			var last *float64
			if rc.LastActivity != nil && sleep.ResumeNeedsActivity(cliState, rc.Sleep) {
				last = rc.LastActivity(provider, sid)
			}
			if !sleep.ShouldResumeAtStartup(cliState, last, now, rc.Sleep) {
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
	if owned > 0 {
		s.log(rc, "info", "agent tabs of chats left to the chats, which resume their own agents", "count", owned)
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
	if p, ok := o.Get("profile"); ok {
		l.Profile, _ = p.(string)
	}
	l.Apps = AppsOf(o)
	l.Fast, l.Permission = ModesOf(o)
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

// shellSettle is how long a pane the resume made itself, or one whose shell is still starting, is looked at before its
// foreground command is taken for one that holds the terminal: on a busy machine the new shell may still be on its way
// or run its rc files (lesspipe, dircolors, nvm) after ShellReady, and the line would be dropped.
const shellSettle = 5 * time.Second

func (s *Starter) sendResume(rc ResumeConfig, t Target, fresh bool) bool {
	deadline := time.Now().Add(shellSettle)
	for {
		command := rc.Tmux.PaneCurrentCommand(t.TmuxSession)
		if safeShells[command] {
			break
		}
		if command != "" {
			var args []string
			if command == "node" {
				args = s.foregroundArgs(rc, t.TmuxSession)
			}
			// The agent already runs: nothing to do.
			if processProvider(command, args) == t.Provider {
				return true
			}
		}
		if !fresh && !shellStarting[command] || time.Now().After(deadline) {
			if command == "" {
				s.log(rc, "warn", "cannot check the pane's process", "session", t.TmuxSession)
			} else {
				// Anything else holds the terminal.
				s.log(rc, "info", "resume not typed: the pane runs another program", "session", t.TmuxSession, "command", command)
			}
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	line, err := s.cfg.Lines.Resume(t.Provider, t.SessionID, t.WorkspaceID, t.Launch)
	if err == nil {
		// The outbox may be typing a queued message into the same pane: the line waits for the pane like any writer.
		err = typist.Write(context.Background(), rc.Tmux, t.TmuxSession, func(context.Context) error {
			return rc.Tmux.SendKeysSeparated(t.TmuxSession, agents.ForAnyShell(s.envSource(t.Provider)+line))
		})
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
	created := map[string]bool{}
	for _, t := range targets {
		if !rc.Tmux.HasSession(t.TmuxSession) {
			if err := rc.Tmux.CreateSession(t.TmuxSession, 80, 24, ""); err != nil {
				s.log(rc, "error", "auto-resume stopped: session not created", "session", t.TmuxSession, "err", err.Error())
				return
			}
			created[t.TmuxSession] = true
		}
	}
	if len(created) > 0 {
		time.Sleep(s.cfg.ShellReady)
	}
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.sendResume(rc, t, created[t.TmuxSession])
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
