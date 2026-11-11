package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The session sweep ends the tmux sessions that hold nothing but a shell nobody uses: the session of a chat whose agent
// has stopped (the agent exited, or was put to sleep, and its pane's shell stayed), and a session left without its tab
// and its chat. The chat loses nothing: its resume, the restart of its tab and a terminal opened on it make the
// session again. A shell is some megabytes; a machine with a hundred stopped chats kept a hundred of them.
//
// It never ends a session that runs anything, has anyone attached or had output within the window, belongs to a tab
// that is not an agent's (a terminal a person opened) or to a chat's own terminal while the chat lives, or has a name
// of a form it does not know.
const (
	// sweepEvery is how often the sessions are looked at, and bareFor how long a session must have been seen unused
	// before it is ended: together a stopped chat's shell is gone within a quarter of an hour.
	sweepEvery = 2 * time.Minute
	bareFor    = 10 * time.Minute
	// sweepMost is how many sessions one sweep ends: the first sweep after an update finds them by the hundred, and
	// ending one costs a few tmux commands.
	sweepMost = 40
)

// sweepTmux is what the sweep asks of tmux.
type sweepTmux interface {
	ReadAllPanes() (map[string]tmux.PaneInfo, bool)
	ParseSessionName(name string) (wsID, paneID, tabID string, ok bool)
	Names() tmux.Names
	Display(name, format string) (string, bool)
	KillSession(name string)
}

// sweepTab is a layout tab as the sweep reads it.
type sweepTab struct {
	agent    bool
	cliState string
}

// sweepChat is the conversation on a session.
type sweepChat struct {
	state  wire.AgentStateKind
	queued int
}

type sessionSweep struct {
	tm sweepTmux
	// tabs are the layouts' tabs by session name, and the workspaces by id: true for one whose layout was read.
	tabs func() (tabs map[string]sweepTab, workspaces map[string]bool)
	// chat is the conversation that holds the session as its agent's, hasChat whether a conversation of that id lives;
	// ready is false while the conversations are not loaded (nothing is judged then).
	chat func(session string) (sweepChat, bool)
	// spare asks the owner of a chat's session whether it may be ended; owned false (or nil) for a session whose
	// life the conversations do not decide, which is judged by its tab and its chat as below.
	spare   func(session string) (spare, owned bool)
	hasChat func(convID string) bool
	ready   func() bool
	// children are the processes under a pane's shell.
	children func(pid int) []int
	// attached: a page's terminal holds the session.
	attached func(session string) bool
	// self are this process and its ancestors: the session the runtime itself was started in is never ended.
	self map[int]bool
	now  func() time.Time
	// since is when each session was first seen unused, without a break.
	since map[string]time.Time
}

func newSessionSweep(c *core) *sessionSweep {
	self := map[int]bool{}
	for pid := os.Getpid(); pid > 1 && !self[pid]; {
		self[pid] = true
		parent, ok := proc.Parent(pid)
		if !ok {
			break
		}
		pid = parent
	}
	return &sessionSweep{
		tm: c.cfg.Tmux,
		tabs: func() (map[string]sweepTab, map[string]bool) {
			tabs, workspaces := map[string]sweepTab{}, map[string]bool{}
			for _, ws := range (coreLayouts{store: c.store}).Workspaces() {
				layout := c.store.Layout(ws.ID)
				workspaces[ws.ID] = layout != nil
				if layout == nil {
					continue
				}
				for _, t := range state.AllTabs(layout) {
					panel := t.String("panelType")
					tabs[t.String("sessionName")] = sweepTab{agent: panel == "claude-code" || panel == "codex-cli", cliState: t.String("cliState")}
				}
			}
			return tabs, workspaces
		},
		chat: func(session string) (sweepChat, bool) {
			v := c.v2.Load()
			if v == nil {
				return sweepChat{}, false
			}
			rec, ok := v.rt.Record(v.rt.ConvOfSession(session))
			if !ok {
				return sweepChat{}, false
			}
			return sweepChat{state: rec.State.K, queued: rec.Queue.Count}, true
		},
		spare: func(session string) (bool, bool) {
			v := c.v2.Load()
			if v == nil {
				return false, false
			}
			return v.rt.SessionSpare(session)
		},
		hasChat: func(id string) bool {
			v := c.v2.Load()
			return v != nil && v.rt.Has(id)
		},
		ready: func() bool {
			v := c.v2.Load()
			return v == nil || v.ready.Load()
		},
		children: proc.Descendants,
		attached: func(session string) bool { return c.cfg.Term != nil && c.cfg.Term.Attached(session) },
		self:     self,
		now:      time.Now,
		since:    map[string]time.Time{},
	}
}

func (s *sessionSweep) run(ctx context.Context) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if ended := s.sweep(); len(ended) > 0 {
			agent.Log("info", "core: unused tmux sessions ended", agent.F{K: "count", V: len(ended)}, agent.F{K: "sessions", V: strings.Join(ended, " ")})
		}
	}
}

// unused is why nobody needs the session ("" when someone does, or may): a stopped chat's, or an orphan's.
func (s *sessionSweep) unused(name string, tabs map[string]sweepTab, workspaces map[string]bool) string {
	if s.spare != nil {
		// A chat's session is its owner's to call spare; the sweep only ends what holds a bare shell nobody uses.
		if spare, owned := s.spare(name); owned {
			if spare {
				return "stopped chat"
			}
			return ""
		}
	}
	stopped := func() (string, bool) {
		chat, ok := s.chat(name)
		if !ok {
			return "", false
		}
		if chat.state != wire.StateStopped || chat.queued > 0 {
			return "", true
		}
		return "stopped chat", true
	}
	if tab, ok := tabs[name]; ok {
		// A tab that is not an agent's is a terminal a person opened; an agent the keeper has not seen exit may be on
		// its way up.
		if !tab.agent || tab.cliState != status.Inactive {
			return ""
		}
		if why, has := stopped(); has {
			return why
		}
		return "stopped chat"
	}
	if ws, _, _, ok := s.tm.ParseSessionName(name); ok {
		if why, has := stopped(); has {
			return why
		}
		// The tab may be in a layout that could not be read just now.
		if read, listed := workspaces[ws]; listed && !read {
			return ""
		}
		return "orphan"
	}
	// A chat's own terminal: `<prefix>v2t-<conversation>-<id>`, the person's while the chat lives.
	if rest, ok := strings.CutPrefix(name, s.tm.Names().Prefix+"v2t-"); ok && s.tm.Names().Prefix != "" {
		if i := strings.LastIndex(rest, "-"); i > 0 && !s.hasChat(rest[:i]) {
			return "orphan"
		}
	}
	return ""
}

// bare: the pane holds its shell and nothing under it.
func (s *sessionSweep) bare(p tmux.PaneInfo) bool {
	return p.PID > 0 && !s.self[p.PID] && tmux.SafeShells[p.Command] && len(s.children(p.PID)) == 0
}

// sweep is one look: it ends the sessions that were unused at every look for bareFor, and returns their names.
func (s *sessionSweep) sweep() []string {
	if !s.ready() {
		return nil
	}
	// A tmux that did not answer says nothing: the clocks stand.
	panes, ok := s.tm.ReadAllPanes()
	if !ok {
		return nil
	}
	now := s.now()
	tabs, workspaces := s.tabs()
	seen := map[string]bool{}
	var due []string
	for name, p := range panes {
		quiet := p.WindowActivity == 0 || now.Sub(time.Unix(p.WindowActivity, 0)) >= bareFor
		if !s.bare(p) || !quiet || s.attached(name) || s.unused(name, tabs, workspaces) == "" {
			continue
		}
		seen[name] = true
		first, known := s.since[name]
		if !known {
			s.since[name] = now
			continue
		}
		if now.Sub(first) >= bareFor {
			due = append(due, name)
		}
	}
	for name := range s.since {
		if !seen[name] {
			delete(s.since, name)
		}
	}
	var ended []string
	for _, name := range due {
		if len(ended) == sweepMost {
			break
		}
		if !s.stillUnused(name) {
			// Someone is at it: it is counted from now again.
			s.since[name] = now
			continue
		}
		s.tm.KillSession(name)
		delete(s.since, name)
		ended = append(ended, name)
	}
	return ended
}

// stillUnused asks the session itself, right before it is ended: no tmux client of a person's attached, one window with
// one pane (a person who split it uses it), the shell alone in it, and the chat still stopped.
func (s *sessionSweep) stillUnused(name string) bool {
	if s.attached(name) {
		return false
	}
	out, ok := s.tm.Display(name, "#{session_attached} #{session_windows} #{window_panes} #{pane_pid} #{pane_current_command}")
	f := strings.Fields(out)
	if !ok || len(f) != 5 || f[0] != "0" || f[1] != "1" || f[2] != "1" {
		return false
	}
	pid, err := strconv.Atoi(f[3])
	if err != nil || !s.bare(tmux.PaneInfo{PID: pid, Command: tmux.ForegroundCommand(f[4], pid)}) {
		return false
	}
	tabs, workspaces := s.tabs()
	return s.unused(name, tabs, workspaces) != ""
}
