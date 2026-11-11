package main

import (
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type sweepFakeTmux struct {
	names  *tmux.Tmux
	panes  map[string]tmux.PaneInfo
	silent bool
	// clients, windows and splits are what a session says of itself right before it is ended.
	clients, windows, splits map[string]int
	killed                   []string
}

func (f *sweepFakeTmux) ReadAllPanes() (map[string]tmux.PaneInfo, bool) {
	if f.silent {
		return nil, false
	}
	out := map[string]tmux.PaneInfo{}
	for k, v := range f.panes {
		out[k] = v
	}
	return out, true
}
func (f *sweepFakeTmux) ParseSessionName(n string) (string, string, string, bool) {
	return f.names.ParseSessionName(n)
}
func (f *sweepFakeTmux) Names() tmux.Names { return f.names.Names() }
func (f *sweepFakeTmux) Display(name, _ string) (string, bool) {
	p, ok := f.panes[name]
	if !ok {
		return "", false
	}
	return strconv.Itoa(f.clients[name]) + " " + strconv.Itoa(1+f.windows[name]) + " " + strconv.Itoa(1+f.splits[name]) + " " +
		strconv.Itoa(p.PID) + " " + p.Command, true
}
func (f *sweepFakeTmux) KillSession(name string) {
	f.killed = append(f.killed, name)
	delete(f.panes, name)
}

type sweepRig struct {
	s        *sessionSweep
	tm       *sweepFakeTmux
	now      time.Time
	tabs     map[string]sweepTab
	spaces   map[string]bool
	chats    map[string]sweepChat
	convs    map[string]bool
	children map[int][]int
	attached map[string]bool
	ready    bool
	nextPID  int
}

func newSweepRig() *sweepRig {
	r := &sweepRig{now: time.Unix(1792400400, 0), tabs: map[string]sweepTab{}, spaces: map[string]bool{"ws-a": true},
		chats: map[string]sweepChat{}, convs: map[string]bool{}, children: map[int][]int{}, attached: map[string]bool{},
		ready: true, nextPID: 4194400} // above any pid of the machine: /proc says nothing of them
	r.tm = &sweepFakeTmux{names: tmux.New(tmux.Names{Socket: "unused", Prefix: "tk-", LegacyPrefix: "pt-"}),
		panes: map[string]tmux.PaneInfo{}, clients: map[string]int{}, windows: map[string]int{}, splits: map[string]int{}}
	r.s = &sessionSweep{
		tm:   r.tm,
		tabs: func() (map[string]sweepTab, map[string]bool) { return r.tabs, r.spaces },
		chat: func(session string) (sweepChat, bool) {
			c, ok := r.chats[session]
			return c, ok
		},
		hasChat:  func(id string) bool { return r.convs[id] },
		ready:    func() bool { return r.ready },
		children: func(pid int) []int { return r.children[pid] },
		attached: func(session string) bool { return r.attached[session] },
		self:     map[int]bool{},
		now:      func() time.Time { return r.now },
		since:    map[string]time.Time{},
	}
	return r
}

// session adds a session whose pane runs command with nothing under it and no output for an hour.
func (r *sweepRig) session(name, command string) int {
	r.nextPID++
	r.tm.panes[name] = tmux.PaneInfo{Command: command, PID: r.nextPID, WindowActivity: r.now.Add(-time.Hour).Unix()}
	return r.nextPID
}

// stoppedChat adds an agent tab whose agent has exited, with its conversation, and returns its session.
func (r *sweepRig) stoppedChat(tab string) string {
	name := "tk-ws-a-pane-1-tab-" + tab
	r.session(name, "bash")
	r.tabs[name] = sweepTab{agent: true, cliState: "inactive"}
	r.chats[name] = sweepChat{state: wire.StateStopped}
	return name
}

// pass sweeps every sweepEvery for d and returns what was ended, sorted.
func (r *sweepRig) pass(d time.Duration) []string {
	r.tm.killed = nil
	for end := r.now.Add(d); r.now.Before(end); {
		r.now = r.now.Add(sweepEvery)
		r.s.sweep()
	}
	out := append([]string(nil), r.tm.killed...)
	sort.Strings(out)
	return out
}

func TestSweepEndsTheShellOfAStoppedChat(t *testing.T) {
	r := newSweepRig()
	name := r.stoppedChat("a")
	if got := r.pass(bareFor - sweepEvery); len(got) != 0 {
		t.Fatalf("ended before the window: %v", got)
	}
	// Within a quarter of an hour of the first look it is gone.
	if got := r.pass(15*time.Minute - bareFor + sweepEvery); !reflect.DeepEqual(got, []string{name}) {
		t.Fatalf("ended %v", got)
	}
	if got := r.pass(time.Hour); len(got) != 0 {
		t.Fatalf("ended again: %v", got)
	}
}

func TestSweepLeavesWhatIsUsed(t *testing.T) {
	r := newSweepRig()
	// An agent at work, and one at its prompt.
	working := "tk-ws-a-pane-1-tab-working"
	r.session(working, "claude")
	r.tabs[working] = sweepTab{agent: true, cliState: "busy"}
	r.chats[working] = sweepChat{state: wire.StateWorking}
	// A shell with a build under it in a stopped chat's pane.
	build := r.stoppedChat("build")
	r.children[r.tm.panes[build].PID] = []int{77}
	// The keeper has not seen the agent exit: it may be on its way up.
	starting := r.stoppedChat("starting")
	r.tabs[starting] = sweepTab{agent: true, cliState: "idle"}
	// The chat is being resumed, or holds a message for its agent.
	resumed := r.stoppedChat("resumed")
	r.chats[resumed] = sweepChat{state: wire.StateStarting}
	queued := r.stoppedChat("queued")
	r.chats[queued] = sweepChat{state: wire.StateStopped, queued: 1}
	// A terminal a person opened: a tab that is no agent's, a chat's own terminal, a pane shell, an ad hoc session.
	manual := "tk-ws-a-pane-1-tab-manual"
	r.session(manual, "bash")
	r.tabs[manual] = sweepTab{}
	r.convs["tab-live"] = true
	r.session("tk-v2t-tab-live-x1", "bash")
	r.session("tk-shell-tab-a", "bash")
	r.session("tk-k3J9", "zsh")
	// A stopped chat's shell someone watches, one a person split, and one that printed something a minute ago.
	watched := r.stoppedChat("watched")
	r.attached[watched] = true
	client := r.stoppedChat("client")
	r.tm.clients[client] = 1
	split := r.stoppedChat("split")
	r.tm.splits[split] = 1
	recent := r.stoppedChat("recent")
	p := r.tm.panes[recent]
	p.WindowActivity = r.now.Add(time.Hour).Unix()
	r.tm.panes[recent] = p
	// A session of a workspace whose layout could not be read.
	r.spaces["ws-b"] = false
	r.session("tk-ws-b-pane-1-tab-x", "bash")
	// The session the runtime itself runs in.
	own := r.stoppedChat("own")
	r.s.self[r.tm.panes[own].PID] = true

	if got := r.pass(time.Hour); len(got) != 0 {
		t.Fatalf("ended %v", got)
	}

	// Once nobody watches it, the window starts: the page's terminal held the clock at every look.
	r.attached[watched] = false
	if got := r.pass(bareFor - sweepEvery); len(got) != 0 {
		t.Fatalf("ended right after the person left: %v", got)
	}
	if got := r.pass(2 * sweepEvery); !reflect.DeepEqual(got, []string{watched}) {
		t.Fatalf("ended %v", got)
	}
}

func TestSweepEndsOrphans(t *testing.T) {
	r := newSweepRig()
	// No tab and no chat: of a workspace that is there, of one that is gone, and a deleted chat's own terminal.
	orphans := []string{"tk-v2t-tab-gone-x1", "tk-ws-a-pane-1-tab-lost", "tk-ws-gone-pane-1-tab-lost"}
	for _, name := range orphans {
		r.session(name, "bash")
	}
	// A chat without a tab keeps its session while its agent is up, and something running is never an orphan to end.
	chat := "tk-ws-a-pane-1-tab-chat"
	r.session(chat, "bash")
	r.chats[chat] = sweepChat{state: wire.StateIdle}
	r.session("tk-ws-a-pane-1-tab-busy", "node")
	if got := r.pass(time.Hour); !reflect.DeepEqual(got, orphans) {
		t.Fatalf("ended %v", got)
	}
}

func TestSweepWaitsForTmuxAndTheChats(t *testing.T) {
	r := newSweepRig()
	name := r.stoppedChat("a")
	// The conversations are not loaded: every session would look like an orphan.
	r.ready = false
	if got := r.pass(time.Hour); len(got) != 0 {
		t.Fatalf("ended before the chats were read: %v", got)
	}
	r.ready = true
	r.pass(bareFor - sweepEvery)
	// A tmux that does not answer neither ends a session nor forgets what was seen.
	r.tm.silent = true
	if got := r.pass(time.Hour); len(got) != 0 {
		t.Fatalf("ended while tmux was silent: %v", got)
	}
	r.tm.silent = false
	if got := r.pass(sweepEvery); !reflect.DeepEqual(got, []string{name}) {
		t.Fatalf("ended %v", got)
	}
}

func TestSweepEndsAFewAtATime(t *testing.T) {
	r := newSweepRig()
	for i := range sweepMost + 5 {
		r.stoppedChat("n" + strconv.Itoa(i))
	}
	r.pass(bareFor)
	if got := r.pass(sweepEvery); len(got) != sweepMost {
		t.Fatalf("one sweep ended %d", len(got))
	}
	if got := r.pass(sweepEvery); len(got) != 5 {
		t.Fatalf("the next ended %d", len(got))
	}
}

func TestPaneOwed(t *testing.T) {
	stopped := wire.Conversation{State: wire.AgentState{K: wire.StateStopped}}
	if !paneOwed(stopped, true) {
		t.Fatal("a stopped chat's pane is not made again")
	}
	onPurpose := stopped
	onPurpose.TerminalStop = &wire.TerminalStop{}
	working := wire.Conversation{State: wire.AgentState{K: wire.StateWorking}}
	if paneOwed(stopped, false) || paneOwed(onPurpose, true) || paneOwed(working, true) {
		t.Fatal("a pane made for a session that is not a stopped chat's tab")
	}
}

// A chat's session is its owner's to call spare: the sweep ends it when the owner says so and leaves it when the
// owner does not, whatever the tab's own state says.
func TestSweepAsksTheOwnerOfAChatsSession(t *testing.T) {
	r := newSweepRig()
	spare := map[string]bool{}
	r.s.spare = func(session string) (bool, bool) {
		s, owned := spare[session]
		return s, owned
	}
	// Cold by its owner while its tab still reads busy: ended.
	cold := r.stoppedChat("cold")
	r.tabs[cold] = sweepTab{agent: true, cliState: "busy"}
	spare[cold] = true
	// Stopped by its tab and its record, but the owner has a use for it (a resume on its way): kept.
	kept := r.stoppedChat("kept")
	spare[kept] = false
	// No chat: judged by its tab as before.
	tab := r.stoppedChat("tab")
	delete(r.chats, tab)
	if got := r.pass(15 * time.Minute); !reflect.DeepEqual(got, []string{cold, tab}) {
		t.Fatalf("ended %v, want %v", got, []string{cold, tab})
	}
}
