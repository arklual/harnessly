package tmux

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A watched server is listened to instead of asked: one tmux client in control mode (`tmux -C`) stays connected to it
// and hears when sessions and windows come and go and when the program in a pane's foreground changes. The panes are
// listed over that connection when an event says they changed, and ReadAllPanes answers from the last listing. A
// listing at rest happens once in 30–60 s, only to find what the events did not say.
//
// The client needs a session to be attached to: watchSession, which has no runtime prefix (so no listing of the
// runtime's sessions holds it) and ends with its last client. The client never starts a server (-N).

const (
	// watchSession is the session the control client sits in.
	watchSession = "_events"
	// watchFormat is what the server compares once a second: the program, pid and folder of every session's pane.
	watchFormat = "#{S:#{session_name} #{pane_current_command} #{pane_pid} #{pane_current_path},}"

	controlTimeout = 5 * time.Second
	// settleFor gathers the events of one change (a new session is three of them) into one listing.
	settleFor = 50 * time.Millisecond

	defaultReconcile = 45 * time.Second
	redialMin        = time.Second
	redialMax        = 30 * time.Second
)

// eventWithin is how late the event of a change may come: the server compares watchFormat once a second.
var eventWithin = 3 * time.Second

// WatchOptions tune a Watcher; the zero value is what the runtime uses.
type WatchOptions struct {
	// Reconcile is the middle of the time between two listings at rest; each wait is within a third of it. 0 is 45 s.
	Reconcile time.Duration
	// Log, when set, hears what an operator would want in the journal: a connection made or lost, events that were missed.
	Log func(level, msg string, kv ...any)

	dial func(socket string) (*control, error) // tests connect to a server of their own
}

// WatchStats count what a Watcher did since it started.
type WatchStats struct {
	Connects   int64 // control clients that connected
	Listings   int64 // listings over the control connection, at events and at rest
	Reconciles int64 // of them, the ones at rest
	Missed     int64 // listings at rest that found a change no event told of
	Served     int64 // ReadAllPanes answered without a tmux command
}

// Watcher keeps the control clients of a Tmux's servers.
type Watcher struct {
	t     *Tmux
	opt   WatchOptions
	socks []*sockWatch
	stop  context.CancelFunc
	wg    sync.WaitGroup

	connects, listings, reconciles, missed, served atomic.Int64
}

// watchers are the running Watchers by their Tmux.
var watchers sync.Map // *Tmux → *Watcher

// Watch starts listening to t's servers until ctx ends or Stop. While a server cannot be listened to (no client yet,
// a client that stopped answering) ReadAllPanes asks it with a tmux command of its own, as without a Watcher. It
// returns nil when the session names leave no room for the client's own session.
func Watch(ctx context.Context, t *Tmux, opt WatchOptions) *Watcher {
	if t.cfg.Prefix == "" || t.IsAppSession(watchSession) {
		return nil
	}
	if opt.Reconcile <= 0 {
		opt.Reconcile = defaultReconcile
	}
	if opt.dial == nil {
		opt.dial = dialControl
	}
	ctx, stop := context.WithCancel(ctx)
	w := &Watcher{t: t, opt: opt, stop: stop}
	for i, s := range t.Sockets() {
		w.socks = append(w.socks, &sockWatch{w: w, socket: s, legacy: i > 0, ask: make(chan chan bool), soon: make(chan struct{}, 1)})
	}
	watchers.Store(t, w)
	for _, s := range w.socks {
		w.wg.Add(1)
		go func() { defer w.wg.Done(); s.run(ctx) }()
	}
	return w
}

// Stop ends the control clients and waits for them; ReadAllPanes asks tmux itself again.
func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	watchers.CompareAndDelete(w.t, w)
	w.stop()
	w.wg.Wait()
}

// Stats are the counts so far.
func (w *Watcher) Stats() WatchStats {
	if w == nil {
		return WatchStats{}
	}
	return WatchStats{Connects: w.connects.Load(), Listings: w.listings.Load(), Reconciles: w.reconciles.Load(),
		Missed: w.missed.Load(), Served: w.served.Load()}
}

func (w *Watcher) log(level, msg string, kv ...any) {
	if w.opt.Log != nil {
		w.opt.Log(level, msg, kv...)
	}
}

func (t *Tmux) watcher() *Watcher {
	if w, ok := watchers.Load(t); ok {
		return w.(*Watcher)
	}
	return nil
}

// watchedPanes is ReadAllPanes from what the watched servers last said; false when any of them is not listened to now.
func (t *Tmux) watchedPanes() (map[string]PaneInfo, bool) {
	w := t.watcher()
	if w == nil {
		return nil, false
	}
	all := map[string]PaneInfo{}
	for _, s := range w.socks {
		if !s.copyInto(all) {
			return nil, false
		}
	}
	w.served.Add(1)
	return all, true
}

// ReadAllPanesNow is ReadAllPanes after a listing asked for now, for a caller about to act on what it saw a moment
// ago. ok is false when the servers are not watched or did not answer: the caller knows nothing newer than
// ReadAllPanes told it.
func (t *Tmux) ReadAllPanesNow() (map[string]PaneInfo, bool) {
	w := t.watcher()
	if w == nil {
		return nil, false
	}
	for _, s := range w.socks {
		if !s.listNow() {
			return nil, false
		}
	}
	return t.watchedPanes()
}

// watchSoon tells the watcher a server may have just been started, so it connects without waiting out its pause.
func (t *Tmux) watchSoon(socket string) {
	w := t.watcher()
	if w == nil {
		return
	}
	for _, s := range w.socks {
		if s.socket == socket {
			select {
			case s.soon <- struct{}{}:
			default:
			}
		}
	}
}

const (
	watchDown   = iota // not listened to: the server is asked with commands
	watchLive          // the control client is connected and panes is current
	watchAbsent        // nothing listens on the socket
)

// sockWatch is the watch of one server.
type sockWatch struct {
	w      *Watcher
	socket string
	// legacy: the server older releases started sessions on. Nothing starts a session there any more, so while it
	// does not run it has no panes; the current server may be started at any moment and is asked instead.
	legacy bool
	ask    chan chan bool // a listing asked for now
	soon   chan struct{}  // connect now

	mu    sync.Mutex
	state int
	// blind: tmux named a pane's program differently than /proc does, so a change of it may come with no event.
	blind bool
	panes map[string]PaneInfo

	// seen is what the last listing settled of each pane's program; only the serving goroutine touches it.
	seen map[string]seenForeground
}

// seenForeground is a pane's program as last checked against /proc.
type seenForeground struct {
	pid     int
	said    string // tmux's word
	command string
	agreed  bool // /proc said the same as tmux
}

func (s *sockWatch) copyInto(all map[string]PaneInfo) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.state == watchLive && !s.blind:
		for k, v := range s.panes {
			all[k] = v
		}
		return true
	case s.state == watchAbsent && s.legacy:
		return true
	}
	return false
}

func (s *sockWatch) set(state int) {
	s.mu.Lock()
	s.state = state
	if state != watchLive {
		s.panes, s.blind = nil, false
	}
	s.mu.Unlock()
}

func (s *sockWatch) listNow() bool {
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	if state == watchAbsent && s.legacy {
		return true
	}
	if state != watchLive {
		return false
	}
	done := make(chan bool, 1)
	wait := time.NewTimer(controlTimeout)
	defer wait.Stop()
	select {
	case s.ask <- done:
	case <-wait.C:
		return false
	}
	select {
	case ok := <-done:
		return ok
	case <-wait.C:
		return false
	}
}

// run keeps a control client connected: after a client ends it waits, longer each time and never the same time as
// another runtime's, and connects again.
func (s *sockWatch) run(ctx context.Context) {
	pause, told := redialMin, ""
	for ctx.Err() == nil {
		c, err := s.w.opt.dial(s.socket)
		if err == nil {
			err = s.serve(ctx, c, func() { pause, told = redialMin, "" })
			c.close()
		}
		if ctx.Err() != nil {
			break
		}
		if errors.Is(err, errNoServer) {
			s.set(watchAbsent)
		} else {
			s.set(watchDown)
			// A tmux too old for the client fails the same way at every try: said once.
			if why := errText(err); why != told {
				told = why
				s.w.log("warn", "tmux: the event client ended, panes are polled until it is back", "socket", s.socket, "err", why)
			}
		}
		wait := time.NewTimer(jitter(pause))
		select {
		case <-ctx.Done():
		case <-s.soon:
			pause = redialMin
		case <-wait.C:
			pause = min(pause*2, redialMax)
		}
		wait.Stop()
	}
	s.set(watchDown)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// jitter is d within a third of itself.
func jitter(d time.Duration) time.Duration {
	return d - d/3 + time.Duration(rand.Int64N(int64(2*d/3)+1))
}

// serve works one connection until it ends: sets the client up, lists the panes, then lists them again whenever an
// event says they changed, and once in a while to check that no event was lost.
func (s *sockWatch) serve(ctx context.Context, c *control, connected func()) error {
	if _, err := c.reply(); err != nil { // the answer to new-session itself
		return err
	}
	for _, cmd := range []string{
		"refresh-client -f no-output",
		"set-option destroy-unattached on",
		"refresh-client -B 'panes::" + watchFormat + "'",
	} {
		if _, err := c.do(cmd); err != nil {
			return err
		}
	}
	s.seen = nil
	if _, err := s.list(c); err != nil {
		return err
	}
	connected()
	s.w.connects.Add(1)
	s.w.log("info", "tmux: panes are followed by events", "socket", s.socket)

	rest := time.NewTimer(jitter(s.w.opt.Reconcile))
	defer rest.Stop()
	// settle runs while events gather; doubt runs after a listing at rest found a change, until its event comes.
	var settle, doubt <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.gone:
			return c.err()
		case <-c.changed:
			doubt = nil
			if settle == nil {
				settle = time.After(settleFor)
			}
		case <-settle:
			settle = nil
			if _, err := s.list(c); err != nil {
				return err
			}
		case <-rest.C:
			changed, err := s.list(c)
			if err != nil {
				return err
			}
			s.w.reconciles.Add(1)
			// The server tells of a pane's program within a second of the change: a change found here is a lost event
			// only when none follows.
			if changed && settle == nil {
				doubt = time.After(eventWithin)
			}
			rest.Reset(jitter(s.w.opt.Reconcile))
		case <-doubt:
			doubt = nil
			s.w.missed.Add(1)
			s.w.log("warn", "tmux: a change of the panes came with no event", "socket", s.socket)
		case done := <-s.ask:
			_, err := s.list(c)
			done <- err == nil
			if err != nil {
				return err
			}
		}
	}
}

// list reads the panes over the connection; changed says whether what events tell of (the sessions, their pids,
// programs and folders) differs from the listing before.
func (s *sockWatch) list(c *control) (changed bool, err error) {
	lines, err := c.do("list-panes -a -F '" + paneFormat + "'")
	if err != nil {
		return false, err
	}
	seen, blind, misnamed := map[string]seenForeground{}, false, ""
	panes := s.w.t.parsePanes(s.socket, lines, func(session, said string, pid int) string {
		f, known := s.seen[session]
		if !known || !f.agreed || f.pid != pid || f.said != said {
			// A pane whose program /proc confirmed is not read again until tmux says something else of it.
			own := foregroundOf(pid)
			f = seenForeground{pid: pid, said: said, command: own, agreed: own == said}
			if own == "" {
				f.command = said
			} else if own != said {
				blind, misnamed = true, session+": "+said+" for "+own
			}
		}
		seen[session] = f
		return f.command
	})
	s.seen = seen
	s.mu.Lock()
	changed = s.state == watchLive && !s.blind && !sameSeen(s.panes, panes)
	wasBlind := s.blind
	s.state, s.panes, s.blind = watchLive, panes, blind
	s.mu.Unlock()
	s.w.listings.Add(1)
	if blind && !wasBlind {
		s.w.log("warn", "tmux: the server names a pane's program differently than /proc, panes are polled meanwhile",
			"socket", s.socket, "pane", misnamed)
	}
	return changed, nil
}

// sameSeen compares what events tell of; a window's last activity changes with every line an agent prints.
func sameSeen(a, b map[string]PaneInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for k, x := range a {
		y, ok := b[k]
		if !ok || x.PID != y.PID || x.Command != y.Command || x.Path != y.Path {
			return false
		}
	}
	return true
}

var (
	errNoServer    = errors.New("no tmux server on the socket")
	errControlGone = errors.New("the control client ended")
	errControlSlow = errors.New("tmux did not answer the control client in time")
)

// control is one tmux client in control mode: commands go in as lines, each answered by a %begin … %end block, and
// the server's notifications come between the blocks.
type control struct {
	in      io.Writer
	replies chan controlReply
	changed chan struct{} // a notification that the panes may have changed
	gone    chan struct{} // closed when the client's output ended
	stderr  func() string
	close   func()
	timeout time.Duration
}

type controlReply struct {
	lines []string
	ok    bool // %end, as opposed to %error
}

func newControl(in io.Writer, out io.Reader) *control {
	c := &control{in: in, replies: make(chan controlReply, 8), changed: make(chan struct{}, 1), gone: make(chan struct{}),
		stderr: func() string { return "" }, close: func() {}, timeout: controlTimeout}
	go c.read(out)
	return c
}

// paneEvents are the notifications after which the panes are listed again.
var paneEvents = map[string]bool{
	"%sessions-changed": true, "%session-renamed": true, "%session-window-changed": true,
	"%window-add": true, "%window-close": true, "%unlinked-window-add": true, "%unlinked-window-close": true,
	"%window-pane-changed": true, "%subscription-changed": true,
}

func (c *control) read(out io.Reader) {
	defer close(c.gone)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 16<<20) // one notification carries every session's line
	var block []string
	end := "" // inside a block: what follows %end or %error on its last line
	for sc.Scan() {
		line := sc.Text()
		if end != "" {
			word, tail, _ := strings.Cut(line, " ")
			if (word == "%end" || word == "%error") && tail == end {
				select {
				case c.replies <- controlReply{lines: block, ok: word == "%end"}:
				default: // nobody waits for this many answers: the connection is of no use
					return
				}
				block, end = nil, ""
				continue
			}
			block = append(block, line)
			continue
		}
		word, tail, _ := strings.Cut(line, " ")
		switch {
		case word == "%begin":
			end = tail
		case word == "%exit":
			return
		case paneEvents[word]:
			select {
			case c.changed <- struct{}{}:
			default:
			}
		}
	}
}

// err is why the connection ended.
func (c *control) err() error {
	if noServerRe.MatchString(c.stderr()) {
		return errNoServer
	}
	return errControlGone
}

// reply waits for the next answer.
func (c *control) reply() ([]string, error) {
	wait := time.NewTimer(c.timeout)
	defer wait.Stop()
	select {
	case r := <-c.replies:
		if !r.ok {
			return nil, errors.New("tmux: " + strings.Join(r.lines, " "))
		}
		return r.lines, nil
	case <-c.gone:
		// An answer read just before the end still counts.
		select {
		case r := <-c.replies:
			if r.ok {
				return r.lines, nil
			}
		default:
		}
		return nil, c.err()
	case <-wait.C:
		return nil, errControlSlow
	}
}

// do sends one command and waits for its answer.
func (c *control) do(cmd string) ([]string, error) {
	if _, err := io.WriteString(c.in, cmd+"\n"); err != nil {
		return nil, errControlGone
	}
	return c.reply()
}

// dialControl starts the control client of a server. It attaches to watchSession, making it when it is not there, and
// fails when no server runs.
func dialControl(socket string) (*control, error) {
	cmd := exec.Command(tmuxBinary(), "-u", "-L", socket, "-N", "-C", "new-session", "-A", "-s", watchSession, "exec cat")
	// A runtime started inside a tmux pane must not hand its $TMUX on: tmux refuses to attach from inside a session.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var errOut lockedBuffer
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := newControl(in, out)
	// What the client wrote before it ended is whole only once it was waited for.
	wait := sync.OnceFunc(func() { <-c.gone; _ = cmd.Wait() })
	c.stderr = func() string { wait(); return errOut.String() }
	c.close = sync.OnceFunc(func() {
		_ = in.Close()
		_ = cmd.Process.Kill()
		wait()
	})
	return c, nil
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
