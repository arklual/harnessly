package status

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

// a hung NFS folder held the agents in uninterruptible waits for 104 minutes. Their command lines read empty,
// so the poll's look and the fresh one both said "no agent", and tmux named no program in the foreground (read as a
// shell): ten busy chats were forced idle at once and their queues resumed new sessions. A look that cannot read the
// pane's processes decides nothing; once the folder comes back and the agent reads as running, the tab stays busy.
func TestBusyStuckIsNotForcedIdleWhileTheAgentCannotBeRead(t *testing.T) {
	// tmux could not name the foreground program either: "" (which counts as a shell).
	e := newEnv(t, claudeTab(Busy), &tmux.PaneInfo{Command: "", PID: 10})
	e.m.mu.Lock()
	en := e.m.tabs["t1"]
	en.CliState = Busy
	en.EventSeq++
	en.LastEvent = Some(LastEvent{Name: EventPromptSubmit, At: e.m.now() - float64(2*time.Hour/time.Millisecond), Seq: en.EventSeq})
	e.m.mu.Unlock()
	e.tm.mu.Lock()
	e.tm.silent = false
	e.tm.mu.Unlock()

	// The hang: the agent (pid 11, the pane shell's child) waits on the folder; its command line cannot be read, so
	// the provider does not recognise it.
	e.procs.blocked.Store(11, true)
	e.claude.setRunning(10, false)
	for range 3 {
		e.m.Poll()
		if got := e.entry().CliState; got != Busy {
			t.Fatalf("a busy tab whose agent could not be read was moved to %s", got)
		}
	}

	// The folder comes back: the agent reads as running again, the tab is still busy and nothing was resumed.
	e.procs.blocked.Delete(11)
	e.claude.setRunning(10, true)
	e.tm.set(session, tmux.PaneInfo{Command: "claude", PID: 10})
	e.m.Poll()
	if got := e.entry().CliState; got != Busy {
		t.Fatalf("after the folder came back the tab is %s", got)
	}

	// Control: an agent that is really gone (a readable shell in the foreground, no agent) is still forced idle.
	e.claude.setRunning(10, false)
	e.tm.set(session, tmux.PaneInfo{Command: "zsh", PID: 10})
	e.m.Poll()
	if got := e.entry().CliState; got != Idle && got != Inactive {
		t.Fatalf("a busy tab whose agent is gone stayed %s", got)
	}
}

func TestBusyStuckNeedsAShellInTheForeground(t *testing.T) {
	// The agent's own program in the foreground is a witness that it runs, whatever the process look said.
	e := newEnv(t, claudeTab(Busy), &tmux.PaneInfo{Command: "claude", PID: 10})
	e.m.mu.Lock()
	en := e.m.tabs["t1"]
	en.CliState = Busy
	en.EventSeq++
	en.LastEvent = Some(LastEvent{Name: EventPromptSubmit, At: e.m.now() - float64(2*time.Hour/time.Millisecond), Seq: en.EventSeq})
	e.m.mu.Unlock()
	e.tm.mu.Lock()
	e.tm.silent = false
	e.tm.mu.Unlock()
	e.claude.setRunning(10, false)
	e.m.Poll()
	if got := e.entry().CliState; got != Busy {
		t.Fatalf("forced to %s with the agent in the pane's foreground", got)
	}
}
