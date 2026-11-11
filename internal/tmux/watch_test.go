package tmux

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is a tmux server as a control client sees it: it answers commands with blocks and sends notifications.
type fakeServer struct {
	mu       sync.Mutex
	lines    []string // the list-panes listing
	commands []string
	out      io.Writer // the connected client, nil when none
	down     bool      // nothing listens
	dials    int
}

func paneLine(session, command string, pid int) string {
	return session + "\t" + command + "\t/home\t" + strconv.Itoa(pid) + "\t0"
}

func (f *fakeServer) dial(string) (*control, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dials++
	cmdR, cmdW := io.Pipe()
	outR, outW := io.Pipe()
	c := newControl(cmdW, outR)
	c.timeout = 2 * time.Second
	if f.down {
		c.stderr = func() string { return "error connecting to /tmp/tmux-1000/main (No such file or directory)\n" }
		_ = outW.Close()
		return c, nil
	}
	f.out = outW
	c.close = func() { _ = cmdW.Close(); _ = outW.Close() }
	go func() {
		f.block(outW, 0, nil)
		sc, n := bufio.NewScanner(cmdR), 0
		for sc.Scan() {
			n++
			f.mu.Lock()
			f.commands = append(f.commands, sc.Text())
			var lines []string
			if strings.HasPrefix(sc.Text(), "list-panes ") {
				lines = slices.Clone(f.lines)
			}
			f.mu.Unlock()
			f.block(outW, n, lines)
		}
	}()
	return c, nil
}

func (f *fakeServer) block(out io.Writer, n int, lines []string) {
	tail := fmt.Sprintf(" 1700000000 %d 1\n", n)
	_, _ = io.WriteString(out, "%begin"+tail+strings.Join(append(lines, ""), "\n")+"%end"+tail)
}

// say changes the listing and, with an event, tells the client.
func (f *fakeServer) say(event string, lines ...string) {
	f.mu.Lock()
	f.lines = lines
	out := f.out
	f.mu.Unlock()
	if event != "" && out != nil {
		_, _ = io.WriteString(out, event+"\n")
	}
}

// drop ends the client's connection.
func (f *fakeServer) drop() {
	f.mu.Lock()
	out := f.out
	f.out = nil
	f.mu.Unlock()
	if out != nil {
		_ = out.(io.Closer).Close()
	}
}

func (f *fakeServer) listings() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.commands {
		if strings.HasPrefix(c, "list-panes ") {
			n++
		}
	}
	return n
}

// countedProc answers /proc for panes whose shell leads its session with program in its foreground, and counts the reads.
func countedProc(t *testing.T, program string) *atomic.Int64 {
	t.Helper()
	var reads atomic.Int64
	old := readProc
	readProc = func(path string) ([]byte, error) {
		reads.Add(1)
		f := strings.Split(path, "/")
		pid, kind := f[len(f)-2], f[len(f)-1]
		if kind == "stat" {
			return []byte(pid + " (sh) S 1 " + pid + " " + pid + " 34816 " + pid + " 0"), nil
		}
		return []byte(program + "\x00"), nil
	}
	t.Cleanup(func() { readProc = old })
	return &reads
}

type watchRig struct {
	fake   *fakeTmux
	server *fakeServer
	tm     *Tmux
	w      *Watcher
}

func newWatchRig(t *testing.T, opt WatchOptions, lines ...string) *watchRig {
	t.Helper()
	r := &watchRig{fake: newFake(), server: &fakeServer{lines: lines}}
	r.fake.servers["main"] = nil
	r.tm = r.fake.tmux(Config{Names: Names{Socket: "main", Prefix: "nw-"}})
	opt.dial = r.server.dial
	r.w = Watch(context.Background(), r.tm, opt)
	t.Cleanup(r.w.Stop)
	return r
}

// polls is how many tmux commands were started for a listing.
func (r *watchRig) polls() int {
	r.fake.mu.Lock()
	defer r.fake.mu.Unlock()
	n := 0
	for _, c := range r.fake.calls {
		if slices.Contains(c.args, "list-panes") {
			n++
		}
	}
	return n
}

func (r *watchRig) live(t *testing.T) {
	t.Helper()
	waitFor(t, 5*time.Second, "the event client", func() bool { _, ok := r.tm.watchedPanes(); return ok })
}

// A watched server is not asked: the panes come from the listing its events caused, and a new session is seen as
// soon as the server tells of it.
func TestWatchAnswersFromEvents(t *testing.T) {
	countedProc(t, "claude")
	r := newWatchRig(t, WatchOptions{}, paneLine("nw-a", "claude", 11), paneLine(watchSession, "cat", 5), paneLine("other", "vim", 6))
	r.live(t)
	before := r.polls()
	panes, ok := r.tm.ReadAllPanes()
	if !ok || len(panes) != 1 || panes["nw-a"] != (PaneInfo{Command: "claude", Path: "/home", PID: 11}) {
		t.Fatalf("panes %+v %v", panes, ok)
	}
	r.server.say("%sessions-changed", paneLine("nw-a", "claude", 11), paneLine("nw-b", "claude", 12))
	waitFor(t, 5*time.Second, "the new session", func() bool { p, _ := r.tm.ReadAllPanes(); return p["nw-b"].PID == 12 })
	r.server.say("%subscription-changed panes $0 - - - : nw-a bash 11 /home,", paneLine("nw-a", "claude", 11))
	waitFor(t, 5*time.Second, "the closed session", func() bool { p, _ := r.tm.ReadAllPanes(); return len(p) == 1 })
	if got := r.polls() - before; got != 0 {
		t.Fatalf("%d tmux commands were started for the panes of a watched server", got)
	}
	if s := r.w.Stats(); s.Connects != 1 || s.Served == 0 || s.Missed != 0 {
		t.Fatalf("stats %+v", s)
	}
}

// Without a client the server is asked as before; a client that ends is replaced, and the panes are asked for
// meanwhile.
func TestWatchPollsWhileThereIsNoClient(t *testing.T) {
	countedProc(t, "claude")
	r := newWatchRig(t, WatchOptions{}, paneLine("nw-a", "claude", 11))
	r.live(t)
	r.server.mu.Lock()
	r.server.down = true
	r.server.mu.Unlock()
	r.server.drop()
	waitFor(t, 5*time.Second, "the client's end", func() bool { _, ok := r.tm.watchedPanes(); return !ok })
	r.fake.servers["main"] = [][2]string{{"nw-polled", "%7"}}
	before := r.polls()
	if panes, ok := r.tm.ReadAllPanes(); !ok || panes["nw-polled"].PID != 7 || r.polls() != before+1 {
		t.Fatalf("the server was not asked: %+v %v", panes, ok)
	}
	if _, ok := r.tm.ReadAllPanesNow(); ok {
		t.Fatal("a listing made now without a client")
	}
	r.server.mu.Lock()
	r.server.down = false
	r.server.mu.Unlock()
	r.tm.watchSoon("main")
	r.live(t)
	if panes, _ := r.tm.ReadAllPanes(); panes["nw-a"].PID != 11 {
		t.Fatalf("after the client came back: %+v", panes)
	}
}

// The legacy server no longer gets new sessions: while it does not run it has no panes and is not asked.
func TestWatchAbsentLegacyServerHasNoPanes(t *testing.T) {
	countedProc(t, "claude")
	fake, main, old := newFake(), &fakeServer{lines: []string{paneLine("nw-a", "claude", 11)}}, &fakeServer{down: true}
	tm := fake.tmux(Config{Names: fakeNames})
	w := Watch(context.Background(), tm, WatchOptions{dial: func(socket string) (*control, error) {
		if socket == "old" {
			return old.dial(socket)
		}
		return main.dial(socket)
	}})
	defer w.Stop()
	waitFor(t, 5*time.Second, "both servers", func() bool { _, ok := tm.watchedPanes(); return ok })
	if panes, ok := tm.ReadAllPanes(); !ok || len(panes) != 1 || len(fake.calls) != 0 {
		t.Fatalf("panes %+v %v, tmux commands %v", panes, ok, fake.calls)
	}
}

// A listing at rest that finds a change no event told of counts it and takes the change.
func TestWatchReconcileTakesWhatEventsLost(t *testing.T) {
	countedProc(t, "claude")
	old := eventWithin
	eventWithin = 20 * time.Millisecond
	defer func() { eventWithin = old }()
	r := newWatchRig(t, WatchOptions{Reconcile: 30 * time.Millisecond}, paneLine("nw-a", "claude", 11))
	r.live(t)
	waitFor(t, 5*time.Second, "a listing at rest", func() bool { return r.w.Stats().Reconciles >= 2 })
	if s := r.w.Stats(); s.Missed != 0 {
		t.Fatalf("nothing changed, yet %+v", s)
	}
	r.server.say("", paneLine("nw-a", "claude", 11), paneLine("nw-b", "claude", 12))
	waitFor(t, 5*time.Second, "the lost session", func() bool { p, _ := r.tm.ReadAllPanes(); return p["nw-b"].PID == 12 })
	waitFor(t, 5*time.Second, "the count of it", func() bool { return r.w.Stats().Missed == 1 })
}

// Where tmux cannot name a pane's program (it says what the pane was started with) its events are blind to an agent
// that exits: such a server is asked as before.
func TestWatchDoesNotTrustAServerThatMisnamesPrograms(t *testing.T) {
	countedProc(t, "bash")
	r := newWatchRig(t, WatchOptions{}, paneLine("nw-a", "nice", 11))
	waitFor(t, 5*time.Second, "the first listing", func() bool { return r.w.Stats().Listings > 0 })
	r.fake.servers["main"] = [][2]string{{"nw-a", "%11"}}
	if panes, ok := r.tm.ReadAllPanes(); !ok || panes["nw-a"].Command != "bash" || r.polls() != 1 {
		t.Fatalf("panes %+v %v after %d polls", panes, ok, r.polls())
	}
}

// ReadAllPanesNow lists at once, with no event.
func TestWatchListsNowWhenAsked(t *testing.T) {
	countedProc(t, "claude")
	r := newWatchRig(t, WatchOptions{}, paneLine("nw-a", "claude", 11))
	r.live(t)
	r.server.say("", paneLine("nw-a", "bash", 11))
	old := readProc
	readProc = func(string) ([]byte, error) { return nil, errors.New("gone") }
	defer func() { readProc = old }()
	if panes, ok := r.tm.ReadAllPanesNow(); !ok || panes["nw-a"].Command != "bash" || r.polls() != 0 {
		t.Fatalf("panes %+v %v, %d polls", panes, ok, r.polls())
	}
}

// A name space that would take the client's session for one of the runtime's is not watched.
func TestWatchNeedsANameOfItsOwn(t *testing.T) {
	for _, n := range []Names{{Socket: "main"}, {Socket: "main", Prefix: "_"}, {Socket: "main", Prefix: "nw-", LegacyPrefix: "_ev"}} {
		if w := Watch(context.Background(), newFake().tmux(Config{Names: n}), WatchOptions{}); w != nil {
			w.Stop()
			t.Fatalf("%+v is watched", n)
		}
	}
}

// The cost of a minute at rest with a hundred panes, before and after: tmux commands started and files of /proc read
// while the conversations look once a second.
func TestWatchIdleCostOfAHundredPanes(t *testing.T) {
	const panes, seconds = 100, 60
	var lines []string
	fake := newFake()
	for i := 1; i <= panes; i++ {
		name := fmt.Sprintf("nw-chat-%d", i)
		lines = append(lines, paneLine(name, "claude", i))
		fake.servers["main"] = append(fake.servers["main"], [2]string{name, "%" + strconv.Itoa(i)})
	}
	reads := countedProc(t, "claude")
	tm := fake.tmux(Config{Names: Names{Socket: "main", Prefix: "nw-"}})
	look := func() {
		for range seconds {
			if p, ok := tm.ReadAllPanes(); !ok || len(p) != panes {
				t.Fatalf("%d panes, %v", len(p), ok)
			}
		}
	}

	look()
	polledCommands, polledReads := len(fake.calls), reads.Load()

	server := &fakeServer{lines: lines}
	w := Watch(context.Background(), tm, WatchOptions{dial: server.dial})
	defer w.Stop()
	waitFor(t, 5*time.Second, "the event client", func() bool { _, ok := tm.watchedPanes(); return ok })
	connectReads := reads.Load() - polledReads
	fake.calls = nil
	reads.Store(0)
	listed := server.listings()
	look()
	// A minute holds at most two listings at rest (one in 30–60 s).
	for range 2 {
		if _, ok := tm.ReadAllPanesNow(); !ok {
			t.Fatal("no listing at rest")
		}
	}
	t.Logf("%d s at rest, %d panes: tmux commands started %d → %d, /proc reads %d → %d (listings over the control client: %d; /proc reads once at connect: %d)",
		seconds, panes, polledCommands, len(fake.calls), polledReads, reads.Load(), server.listings()-listed, connectReads)
	if polledCommands != seconds || polledReads != 2*panes*seconds {
		t.Fatalf("the poll: %d commands, %d reads", polledCommands, polledReads)
	}
	if len(fake.calls) != 0 || reads.Load() != 0 || server.listings()-listed != 2 {
		t.Fatalf("watched: %d commands, %d reads, %d listings", len(fake.calls), reads.Load(), server.listings()-listed)
	}
}

// A real server: the client hears a session start, the program in its pane change and the session end, lists nothing
// of its own session, and leaves no session behind.
func TestRealWatchFollowsAServer(t *testing.T) {
	r := newRealRig(t)
	tm := NewConfig(Config{Names: Names{Socket: r.main, Prefix: "hs-"}})
	if err := tm.CreateSession("hs-a", 80, 24, ""); err != nil {
		t.Fatal(err)
	}
	w := Watch(context.Background(), tm, WatchOptions{})
	defer w.Stop()
	waitFor(t, 20*time.Second, "the event client", func() bool { _, ok := tm.watchedPanes(); return ok })
	seen := func(name string) (PaneInfo, bool) {
		panes, ok := tm.watchedPanes()
		p, has := panes[name]
		return p, ok && has
	}
	if p, has := seen("hs-a"); !has || p.PID == 0 {
		t.Fatalf("hs-a %+v %v", p, has)
	}
	if _, has := seen(watchSession); has || slices.Contains(tm.ListSessions(), watchSession) {
		t.Fatal("the client's session is listed among the runtime's")
	}
	if err := tm.CreateSession("hs-b", 80, 24, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "the new session", func() bool { _, has := seen("hs-b"); return has })
	if err := tm.SendLiteralThenEnter("hs-b", "sleep 600"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "the program in the pane", func() bool { p, _ := seen("hs-b"); return p.Command == "sleep" })
	if err := tm.SendKeySequence("hs-b", "C-c"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "the shell back in the pane", func() bool { p, _ := seen("hs-b"); return SafeShells[p.Command] })
	tm.KillSession("hs-b")
	waitFor(t, 20*time.Second, "the session's end", func() bool { _, has := seen("hs-b"); return !has })
	if now, ok := tm.ReadAllPanesNow(); !ok || len(now) != 1 {
		t.Fatalf("a listing made now: %+v %v", now, ok)
	}
	if s := w.Stats(); s.Connects != 1 || s.Missed != 0 {
		t.Fatalf("stats %+v", s)
	}
	w.Stop()
	raw := func() string { out, _ := tm.Command("hs-a", "list-sessions", "-F", "#{session_name}"); return out }
	waitFor(t, 20*time.Second, "the client's session to end with it", func() bool { return !strings.Contains(raw(), watchSession) })
	if !strings.Contains(raw(), "hs-a") {
		t.Fatalf("sessions after the client left: %q", raw())
	}
}

// With no server the client does not start one.
func TestRealWatchStartsNoServer(t *testing.T) {
	r := newRealRig(t)
	tm := NewConfig(Config{Names: Names{Socket: r.main, Prefix: "hs-"}})
	c, err := dialControl(r.main)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if _, err := c.reply(); !errors.Is(err, errNoServer) {
		t.Fatalf("err %v", err)
	}
	if tm.HasSession(watchSession) {
		t.Fatal("the client started a server")
	}
}
