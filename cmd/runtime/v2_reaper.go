package main

import (
	"errors"
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Stopper is convo.Terminals over tmux and /proc: it ends a chat's terminal (terminal.stop, the idle stop) and tells
// the sweep what runs under a pane and whether anyone looks at it.
type v2Stopper struct {
	v  *v2Live
	tm *tmux.Tmux
	// self are this process and its ancestors: a pane that holds one of them is the runtime's own (a runtime started
	// from a tmux pane) and is never stopped.
	self map[int]bool
}

var _ convo.Terminals = (*v2Stopper)(nil)

// stopGrace and stopPoll: how long the agent and what it started get to exit on SIGTERM, and how often survivors are
// looked at, before SIGKILL.
const (
	stopGrace = 3 * time.Second
	stopPoll  = 150 * time.Millisecond
)

func newV2Stopper(v *v2Live, tm *tmux.Tmux) *v2Stopper {
	self := map[int]bool{}
	for pid := os.Getpid(); pid > 1 && !self[pid]; {
		self[pid] = true
		parent, ok := proc.Parent(pid)
		if !ok {
			break
		}
		pid = parent
	}
	return &v2Stopper{v: v, tm: tm, self: self}
}

// Owned: a workspace tab's session of this runtime (its name has the runtime's prefix and the tab's form — no pane
// shell, no terminal of a chat's own, no session of another program), which does not hold the runtime itself.
func (s *v2Stopper) Owned(session string) bool {
	if _, _, _, ok := s.tm.ParseSessionName(session); !ok || !s.tm.HasSession(session) {
		return false
	}
	pane := s.tm.PanePID(session)
	return pane > 0 && !s.self[pane]
}

func (s *v2Stopper) Sample(session string) (convo.PaneSample, bool) {
	pane := s.tm.PanePID(session)
	if pane <= 0 {
		return convo.PaneSample{}, false
	}
	out := convo.PaneSample{Root: pane}
	for _, pid := range append([]int{pane}, proc.Descendants(pane)...) {
		u, ok := proc.UsageOf(pid)
		if !ok {
			if pid == pane {
				return convo.PaneSample{}, false
			}
			continue // ended while the tree was read
		}
		argv, _ := proc.Argv(pid)
		out.Procs = append(out.Procs, convo.Proc{PID: pid, PPID: u.PPID, Comm: u.Comm, Argv: argv, CPU: u.CPU, RSS: u.RSS})
	}
	return out, true
}

// Watched: a page holds the conversation's channel, or a terminal (a page's or a person's tmux client) is attached.
func (s *v2Stopper) Watched(convID, session string) bool {
	if s.v.hub.Subscribers(wire.ConvChannel(convID)) > 0 {
		return true
	}
	if term := s.v.c.cfg.Term; term != nil && term.Attached(session) {
		return true
	}
	return s.tm.Attached(session) > 0
}

// Stop ends what runs under the pane (SIGTERM, then SIGKILL to what stays), then the session. The tab stays in the
// layout and the keeper takes its agent for gone: the resume makes the session again in the tab's folder.
func (s *v2Stopper) Stop(session string) (int64, error) {
	if !s.tm.HasSession(session) {
		return 0, errors.New("no such session: " + session)
	}
	var freed int64
	pane := s.tm.PanePID(session)
	if pane > 0 {
		if sample, ok := s.Sample(session); ok {
			for _, p := range sample.Procs {
				freed += p.RSS
			}
		}
		proc.KillTree(pane, stopGrace, stopPoll, nil)
	}
	s.tm.KillSession(session)
	if s.tm.HasSession(session) {
		return freed, errors.New("the session is still there: " + session)
	}
	if _, _, tab, ok := s.tm.ParseSessionName(session); ok {
		if m := s.v.c.manager(); m != nil {
			m.MarkAgentGone(tab, true)
		}
	}
	return freed, nil
}
