package convo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/terminalfailure"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Runtime is the conversations of one runtime: an actor per conversation and the op answers. It implements Service.
type Runtime struct {
	d     Deps
	epoch string

	// resumeMu keeps two resumes of one session from both opening a conversation.
	resumeMu sync.Mutex

	mu     sync.Mutex
	actors map[string]*actor
	// bySession is the conversation of each tmux session: a hook names its session, not its conversation.
	bySession map[string]string
	// projects are the machine's folders made projects.
	projects []wire.Project

	// favMu keeps the favorites' layout and its event in one order; favs is the layout as last saved.
	favMu sync.Mutex
	favs  wire.Favorites

	// draftMu keeps the composers' drafts and their events in one order; draftsDirty: drafts.json is behind.
	draftMu     sync.Mutex
	drafts      draftBook
	draftsDirty bool

	// reapMu keeps one sweep at a time; idle is what the sweeps saw of the chats idle so far (reap.go).
	reapMu sync.Mutex
	idle   map[string]*idleTrack

	opsMu    sync.Mutex
	ops      map[string]opRecord
	inflight map[string]chan struct{}
	opsDirty bool

	// inline runs every actor on the caller's goroutine, one call at a time, and the work it spawns right after the
	// call: a test drives the runtime on a fake clock without sleeping.
	inline  bool
	pending []job

	// indexed: the scan read every conversation into the search index (search.go).
	indexed atomic.Bool

	stop chan struct{}
	wg   sync.WaitGroup
}

type job struct {
	work func() any
	done func(any)
}

type actor struct {
	c    *conv
	mail chan func()
	stop chan struct{}
	// last is the record as the actor left it after its latest turn: what a list answers with when the actor is too
	// busy to be asked (a read that stalls on a loaded disk), so a sidebar never waits behind one conversation.
	last atomic.Pointer[recView]
}

type recView struct {
	rec     wire.Conversation
	deleted bool
	left    []string
}

func (a *actor) publish() {
	a.last.Store(&recView{rec: a.c.rec, deleted: a.c.deletedAt != 0, left: a.c.left})
}

// listWait is how long a list waits for one actor before it takes that actor's last published record.
const listWait = 250 * time.Millisecond

var _ Service = (*Runtime)(nil)

// New is a runtime on d. Start loads what the store holds and starts the actors.
func New(d Deps) *Runtime {
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Limits == (wire.Limits{}) {
		d.Limits = wire.DefaultLimits
	}
	if d.Log == nil {
		d.Log = nopLog{}
	}
	return &Runtime{d: d, epoch: newID(), actors: map[string]*actor{}, bySession: map[string]string{}, ops: map[string]opRecord{}, drafts: draftBook{},
		inflight: map[string]chan struct{}{}, stop: make(chan struct{})}
}

// NewInline is a runtime whose actors run on the caller's goroutine (tests).
func NewInline(d Deps) *Runtime {
	r := New(d)
	r.inline = true
	return r
}

// Epoch is new on every start of the runtime.
func (r *Runtime) Epoch() string { return r.epoch }

type nopLog struct{}

func (nopLog) Info(string, ...any) {}
func (nopLog) Warn(string, ...any) {}

func newID() string {
	var b [9]byte
	rand.Read(b[:])
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, len(b))
	for i, x := range b {
		out[i] = alphabet[int(x)%len(alphabet)]
	}
	return string(out)
}

// --- start and stop ---

// Start loads the conversations written before and starts their actors. Each comes back unknown and is probed;
// a message a restart caught on its way is settled by the screen and the transcript, never typed again blindly.
func (r *Runtime) Start(ctx context.Context) error {
	var errs []error
	if s := r.d.Store; s != nil {
		files, err := s.loadConvs()
		errs = append(errs, err)
		for _, f := range files {
			r.restore(f)
		}
		ops, err := s.loadOps()
		errs = append(errs, err)
		now := r.d.Clock.Now()
		for k, op := range ops {
			if now.Sub(time.UnixMilli(op.At)) < OpsKept {
				r.ops[k] = op
			}
		}
		ps, err := s.loadProjects()
		errs = append(errs, err)
		r.projects = ps
		fs, err := s.loadFavorites()
		errs = append(errs, err)
		r.favs = fs
		ds, err := s.loadDrafts()
		errs = append(errs, err)
		r.drafts = ds
	}
	if !r.inline {
		if s := r.d.Store; s != nil {
			s.Async(func(path string, err error) { r.d.Log.Warn("conversation not saved", "path", path, "err", err) })
		}
		r.wg.Add(2)
		go r.opsSaver()
		go r.reaper()
		if r.d.Index != nil {
			r.wg.Add(1)
			go r.indexer()
		}
	}
	return errors.Join(errs...)
}

// Stop ends the actors after each wrote its last state.
func (r *Runtime) Stop() {
	r.mu.Lock()
	actors := make([]*actor, 0, len(r.actors))
	for _, a := range r.actors {
		actors = append(actors, a)
	}
	r.mu.Unlock()
	if r.inline {
		for _, a := range actors {
			a.c.save(r.d.Clock.Now(), true)
		}
		r.saveOps()
		r.saveDrafts()
		return
	}
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	for _, a := range actors {
		close(a.stop)
	}
	r.wg.Wait()
	r.saveOps()
	r.saveDrafts()
	if r.d.Store != nil {
		r.d.Store.Close()
	}
}

func (r *Runtime) restore(f convFile) {
	rec := f.Conv
	rec.Session, rec.TranscriptPath, rec.AgentSessionID = f.Session, f.TranscriptPath, f.AgentSessionID
	rec.BgAgents = 0 // the agent's tasks are read again after a start
	if rec.SeenAt == 0 {
		// A record from before seenAt was kept: what it had finished then was read, or every old chat would be unread.
		rec.SeenAt = rec.LastActivityAt
	}
	c := newConv(&r.d, rec)
	c.handoffArtifact, c.handoffArtifactClientID = f.HandoffArtifact, f.HandoffArtifactClientID
	c.deletedAt = f.DeletedAt
	c.rewinds, c.left = f.Rewinds, f.Left
	c.rec.Rewinds = c.marks()
	c.box.Restore(f.Outbox)
	for i := range f.Questions {
		q := f.Questions[i]
		c.addGate(&gate{q: &q})
	}
	for i := range f.Permissions {
		p := f.Permissions[i]
		c.addGate(&gate{p: &p})
	}
	a := r.add(c)
	_ = r.on(a, func(c *conv, now time.Time) { c.restarted(now) }) // a fresh mailbox is not full
}

// restarted: the actor's first turn after a start.
func (c *conv) restarted(now time.Time) {
	if p := c.rec.PendingModel; p != nil && p.Applying {
		// What was being typed when the runtime went down was not seen taken: it is asked of the agent again.
		again := *p
		again.Applying = false
		c.rec.PendingModel = &again
	}
	was := c.rec.State
	// A failed first launch has no process or transcript to rediscover after a
	// runtime restart. Keep that state so the persisted, paused first prompt can
	// still be retried through freshLaunch. Turning it into unknown strands the
	// prompt: there is neither a session to observe nor a failed-launch state to
	// launch anew from.
	if was.K != wire.StateLaunchFailed || c.rec.AgentSessionID != "" || c.rec.TranscriptPath != "" {
		c.input(agentstate.Input{K: agentstate.Restart}, now)
	}
	c.cutAtRestart(was, now)
	c.feedUpdate(c.fd.Load(), false, now)
	for _, m := range c.box.Messages(now) {
		if m.State.EntryID != nil {
			c.fd.SetClientID(*m.State.EntryID, m.ClientID)
		}
	}
	var look *typist.Screen
	if c.rec.Session != "" {
		if p := c.d.Machine.Pane(c.rec.Session); p != nil {
			s := typist.Look(p, c.rec.Provider)
			look = &s
		}
	}
	inBox := func(text string) bool { return look != nil && typist.StillInBox(*look, c.rec.Provider, text) }
	c.change(c.box.Restarted(c.fd.FindUser, inBox, now), now)
	c.observe(now)
	c.pump(now)
}

func (r *Runtime) add(c *conv) *actor {
	a := &actor{c: c, mail: make(chan func(), 64), stop: make(chan struct{})}
	a.publish()
	if r.inline {
		c.spawn = func(work func() any, done func(any)) { r.pending = append(r.pending, job{work, done}) }
	} else {
		c.spawn = func(work func() any, done func(any)) {
			go func() {
				res := work()
				select {
				case a.mail <- func() { done(res) }:
				case <-a.stop:
				}
			}()
		}
	}
	r.mu.Lock()
	r.actors[c.rec.ID] = a
	if c.rec.Session != "" {
		r.bySession[c.rec.Session] = c.rec.ID
	}
	r.mu.Unlock()
	if !r.inline {
		r.wg.Add(1)
		go r.loop(a)
	}
	return a
}

// loop is one actor: its mail in order, its clock, a look and a read every LookEvery.
func (r *Runtime) loop(a *actor) {
	defer r.wg.Done()
	c := a.c
	next := r.d.Clock.Now()
	// The actor looks only after its first message, the one its maker sends: that one reads the transcript as history,
	// and a look before it would take the history for news (a turn a resumed session left open would be running).
	told := false
	for {
		now := r.d.Clock.Now()
		wake := c.wake(now)
		if told && next.Before(wake) {
			wake = next
		}
		select {
		case f := <-a.mail:
			f()
			told = true
			a.publish()
		case <-r.d.Clock.After(wake.Sub(now)):
			now = r.d.Clock.Now()
			if told && !now.Before(next) {
				c.read(now)
				c.observe(now)
				next = now.Add(c.lookEvery())
			}
			c.tick(now)
			c.save(now, false)
			a.publish()
		case <-a.stop:
			c.save(r.d.Clock.Now(), true)
			return
		}
	}
}

// on runs f on the actor; inline, the work it spawned runs after it, in order, until none is left.
//
// It returns ErrBusy when the mailbox stayed full for MailWait (a stuck actor: the message is not taken and the caller
// says so), ErrNotFound when the conversation went.
func (r *Runtime) on(a *actor, f func(c *conv, now time.Time)) error {
	if r.inline {
		r.mu.Lock()
		defer r.mu.Unlock()
		f(a.c, r.d.Clock.Now())
		r.drain()
		return nil
	}
	t := time.NewTimer(MailWait)
	defer t.Stop()
	select {
	case a.mail <- func() { f(a.c, r.d.Clock.Now()) }:
		return nil
	case <-a.stop:
		return ErrNotFound
	case <-t.C:
		r.d.Log.Warn("conversation mailbox full, a message not taken", "conv", a.c.rec.ID)
		return ErrBusy
	}
}

// MailWait is how long a message that needs no answer waits for room in a full mailbox before it is dropped.
var MailWait = 5 * time.Second

func (r *Runtime) drain() {
	for len(r.pending) > 0 {
		j := r.pending[0]
		r.pending = r.pending[1:]
		j.done(j.work())
	}
}

// call runs f on the conversation's actor and waits for it within ctx.
func (r *Runtime) call(ctx context.Context, id string, f func(c *conv, now time.Time)) error {
	a := r.actor(id)
	if a == nil {
		return ErrNotFound
	}
	if r.inline {
		return r.on(a, f)
	}
	done := make(chan struct{})
	select {
	case a.mail <- func() { f(a.c, r.d.Clock.Now()); close(done) }:
	case <-ctx.Done():
		return ctx.Err()
	case <-a.stop:
		return ErrNotFound
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) actor(id string) *actor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actors[id]
}

func (r *Runtime) all() []*actor {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*actor, 0, len(r.actors))
	for _, a := range r.actors {
		out = append(out, a)
	}
	return out
}

// Advance runs every actor's clock turn and look: what the runner does every LookEvery (tests drive it).
func (r *Runtime) Advance() {
	for _, a := range r.all() {
		// A full mailbox is logged by on, and a gone actor needs no turn; either way the rest still advance.
		_ = r.on(a, func(c *conv, now time.Time) {
			c.read(now)
			c.observe(now)
			c.tick(now)
			c.save(now, false)
		})
	}
}

// --- the agent's signals ---

// Hook is one of the agent's hooks, as POST /api/status/hook carries it.
type Hook struct {
	// Kind: session-start, prompt-submit, notification, stop, stop-failure, pre-compact, post-compact.
	Kind string
	// Notification is the notification's type (permission_prompt, idle_prompt, …).
	Notification string
	// Failure is what a stop-failure says went wrong.
	Failure wire.TurnFailureCode
}

// Hook feeds a hook to its conversation: the transcript is read first, so the entry the hook is about is there.
func (r *Runtime) Hook(id string, h Hook) error {
	a := r.actor(id)
	if a == nil {
		return ErrNotFound
	}
	return r.on(a, func(c *conv, now time.Time) {
		c.read(now)
		in := agentstate.Input{}
		switch h.Kind {
		case "session-start":
			in.K = agentstate.SessionStart
		case "prompt-submit":
			in.K = agentstate.PromptSubmit
		case "stop":
			in = c.stopInput("")
		case "stop-failure":
			in.K, in.TurnFailure = agentstate.StopFailure, h.Failure
			if in.TurnFailure == "" {
				in.TurnFailure = wire.TurnFailedUnknown
			}
		case "pre-compact":
			in.K = agentstate.PreCompact
		case "post-compact":
			in.K = agentstate.PostCompact
		case "notification":
			if h.Notification == "permission_prompt" {
				// The prompt itself is read off the screen: its options and the call it is about.
				c.observe(now)
			}
			return
		default:
			return
		}
		c.input(in, now)
		if in.K == agentstate.PreCompact {
			// Claude compacts on its own only mid-turn: a turn ended on silence is still at work.
			c.resumeQuiet(now)
		}
		c.observe(now)
	})
}

// LaunchStep, LaunchReady and LaunchFailed are what the launcher reports of a conversation it starts.
func (r *Runtime) LaunchStep(id string, step wire.LaunchStep) {
	r.feedInput(id, agentstate.Input{K: agentstate.LaunchStep, Step: step})
}

func (r *Runtime) LaunchReady(id string) {
	r.feedInput(id, agentstate.Input{K: agentstate.LaunchReady})
}

func (r *Runtime) LaunchFailed(id string, f wire.LaunchFailure) {
	r.feedInput(id, agentstate.Input{K: agentstate.LaunchFailed, Failure: f})
}

func (r *Runtime) feedInput(id string, in agentstate.Input) {
	if a := r.actor(id); a != nil {
		_ = r.on(a, func(c *conv, now time.Time) { c.input(in, now) }) // a stuck actor was logged; its waits end on their terms
	}
}

// Has tells whether the runtime holds a conversation (a deleted one too, until it is purged).
func (r *Runtime) Has(id string) bool { return r.actor(id) != nil }

// Busy is how many conversations have an agent at work, from what each actor left after its last turn.
func (r *Runtime) Busy() int {
	n := 0
	for _, a := range r.all() {
		if v := a.last.Load(); !v.deleted && v.rec.State.K == wire.StateWorking {
			n++
		}
	}
	return n
}

// Record is a conversation as its actor last left it, with its tmux session and folder; false for one the runtime does
// not have or that is deleted.
func (r *Runtime) Record(id string) (wire.Conversation, bool) {
	a := r.actor(id)
	if a == nil {
		return wire.Conversation{}, false
	}
	v := a.last.Load()
	if v == nil || v.deleted {
		return wire.Conversation{}, false
	}
	return v.rec, true
}

// ConvOfSession is the conversation that runs in a tmux session, "" for none.
func (r *Runtime) ConvOfSession(session string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bySession[session]
}

// TerminalOutput gives the conversation the bytes its agent printed to its PTY. Unknown sessions are ignored.
func (r *Runtime) TerminalOutput(session string, data []byte) {
	r.mu.Lock()
	id := r.bySession[session]
	a := r.actors[id]
	r.mu.Unlock()
	if a == nil || len(data) == 0 {
		return
	}
	chunk := append([]byte(nil), data...)
	_ = r.on(a, func(c *conv, now time.Time) {
		if c.terminalFailures == nil {
			c.terminalFailures = terminalfailure.NewDetector()
		}
		failure, ok := c.terminalFailures.Write(chunk)
		if !ok {
			return
		}
		c.failTerminal(c.fd.OpenTurn(), string(failure.Kind), failure.Detail, now)
	})
}

// Attach names the tmux session and the transcript of a conversation once the launcher knows them. What it is not
// told ("" for the transcript or the agent's session) stays as it was.
func (r *Runtime) Attach(id, session, transcriptPath, agentSessionID string) error {
	a := r.actor(id)
	if a == nil {
		return ErrNotFound
	}
	if session != "" {
		r.mu.Lock()
		r.bySession[session] = id
		r.mu.Unlock()
	}
	return r.on(a, func(c *conv, now time.Time) {
		if session != "" {
			c.rec.Session = session
		}
		if agentSessionID != "" {
			c.rec.AgentSessionID = agentSessionID
		}
		if transcriptPath != "" && transcriptPath != c.rec.TranscriptPath {
			// The first transcript of a conversation that began in v2 may hold a turn already (the agent writes it after
			// the prompt, and the sync finds it later): its user entry is our message's, and its end is not news.
			first := c.rec.TranscriptPath == ""
			c.rec.TranscriptPath = transcriptPath
			c.openFeed()
			c.feedUpdateOf(c.fd.Load(), first, true, now)
		}
		c.dirty = true
		c.observe(now)
	})
}

// --- persistence ---

func (c *conv) file() convFile {
	return convFile{Conv: c.rec, Session: c.rec.Session, TranscriptPath: c.rec.TranscriptPath,
		AgentSessionID: c.rec.AgentSessionID, HandoffArtifact: c.handoffArtifact,
		HandoffArtifactClientID: c.handoffArtifactClientID, Outbox: c.box.State(), Questions: c.questions(), Permissions: c.permissions(),
		DeletedAt: c.deletedAt, Rewinds: c.rewinds, Left: c.left}
}

// save writes the conversation when it changed, at most every SaveEvery; a conversation deleted longer than
// DeletedKept goes from the disk.
func (c *conv) save(now time.Time, force bool) {
	s := c.d.Store
	if s == nil || !c.dirty || !force && now.Sub(c.savedAt) < SaveEvery {
		return
	}
	if err := s.saveConv(c.file()); err != nil {
		c.d.Log.Warn("conversation not saved", "conv", c.rec.ID, "err", err)
		return
	}
	c.dirty, c.savedAt = false, now
}

func (r *Runtime) opsSaver() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stop:
			return
		case <-r.d.Clock.After(time.Second):
			r.saveOps()
			r.saveDrafts()
		}
	}
}

func (r *Runtime) saveOps() {
	r.opsMu.Lock()
	if !r.opsDirty || r.d.Store == nil {
		r.opsMu.Unlock()
		return
	}
	snapshot := make(map[string]opRecord, len(r.ops))
	for k, v := range r.ops {
		snapshot[k] = v
	}
	r.opsDirty = false
	r.opsMu.Unlock()
	if err := r.d.Store.saveOps(snapshot); err != nil {
		r.d.Log.Warn("op answers not saved", "err", err)
	}
}

// --- Service ---

func (r *Runtime) Conversations(ctx context.Context) (wire.ConvsSnapshot, error) {
	out := []wire.Conversation{}
	for _, a := range r.all() {
		wait, cancel := context.WithTimeout(ctx, listWait)
		v, err := live(r, wait, a.c.rec.ID, func(c *conv, now time.Time) (recView, error) { return recView{rec: c.rec}, nil })
		cancel()
		switch {
		case errors.Is(err, ErrNotFound):
			continue // deleted
		case err != nil && ctx.Err() != nil:
			return wire.ConvsSnapshot{}, ctx.Err()
		case err != nil:
			last := a.last.Load()
			if last.deleted {
				continue
			}
			v = *last
		}
		out = append(out, v.rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].LastActivityAt > out[j].LastActivityAt
	})
	r.mu.Lock()
	projects := append([]wire.Project{}, r.projects...)
	r.mu.Unlock()
	snap := wire.ConvsSnapshot{Conversations: out, Projects: projects, DefaultCwd: r.d.DefaultCwd}
	r.favMu.Lock()
	if len(r.favs.Groups) > 0 || len(r.favs.Loose) > 0 {
		favs := r.favs.Clone()
		snap.Favorites = &favs
	}
	r.favMu.Unlock()
	snap.Drafts = r.draftList()
	return snap, nil
}

// result is what an actor answers a read with, handed over a channel: a read that gave up on a busy actor must not
// share variables with the turn that answers it later.
type result[T any] struct {
	v   T
	err error
}

// live runs f on the live conversation's actor and waits for its answer within ctx.
func live[T any](r *Runtime, ctx context.Context, id string, f func(c *conv, now time.Time) (T, error)) (T, error) {
	res := make(chan result[T], 1)
	err := r.call(ctx, id, func(c *conv, now time.Time) {
		if c.deletedAt != 0 {
			res <- result[T]{err: ErrNotFound}
			return
		}
		v, err := f(c, now)
		res <- result[T]{v, err}
	})
	if err != nil {
		var zero T
		return zero, err
	}
	out := <-res
	return out.v, out.err
}

func (r *Runtime) Conversation(ctx context.Context, id string) (wire.ConvSnapshot, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.ConvSnapshot, error) { return c.snapshot(now), nil })
}

func (r *Runtime) ConversationAfter(ctx context.Context, id, afterEntryID string) (wire.ConvSnapshot, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.ConvSnapshot, error) {
		ts, es, ok := c.fd.After(afterEntryID)
		if !ok {
			return wire.ConvSnapshot{}, ErrUnknownCursor
		}
		snap := c.snapshot(now)
		snap.Turns, snap.Entries, snap.BeforeTurnID = ts, es, nil
		return snap, nil
	})
}

func (r *Runtime) Turns(ctx context.Context, id, beforeTurnID string, limit int) (wire.TurnsPage, error) {
	if limit < 1 {
		limit = wire.TurnsPageDefault
	}
	limit = min(limit, wire.TurnsPageMax)
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.TurnsPage, error) {
		p, ok := c.fd.Turns(beforeTurnID, limit)
		if !ok {
			return wire.TurnsPage{}, ErrNotFound
		}
		return p, nil
	})
}

func (r *Runtime) TurnEntries(ctx context.Context, id, turnID string) (wire.EntriesPage, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.EntriesPage, error) {
		p, ok := c.fd.TurnEntries(turnID)
		if !ok {
			return wire.EntriesPage{}, ErrNotFound
		}
		return p, nil
	})
}

func (r *Runtime) EntryOutput(ctx context.Context, id, entryID string) (wire.OutputPage, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.OutputPage, error) {
		p, ok := c.fd.Output(entryID)
		if !ok {
			return wire.OutputPage{}, ErrNotFound
		}
		return p, nil
	})
}

// Tasks is the agent's task list of a conversation, in the order the tasks were made.
// Context is how full the agent's window is: the tokens its last answer was asked with, of its model's window.
func (r *Runtime) Context(ctx context.Context, id string) (wire.ConvContext, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.ConvContext, error) {
		used := c.fd.Context()
		limit := int64(wire.ContextWindow)
		if m := c.rec.Model; m != nil && strings.HasSuffix(m.ID, "[1m]") {
			limit = wire.ContextWindowLong
		}
		if r.d.Window != nil {
			// What the agent reports is its real window (a model may run with the long one without the suffix).
			reportedUsed, reported := r.d.Window(c.rec.Provider, c.rec.AgentSessionID, c.rec.TranscriptPath)
			if reported > 0 {
				limit = reported
			}
			if used == 0 {
				used = reportedUsed
			}
		}
		if used > limit {
			limit = max(wire.ContextWindowLong, used)
		}
		return wire.ConvContext{UsedTokens: used, LimitTokens: limit}, nil
	})
}

// MessageStatus is the conversation's word on its messages by their client ids, one per id in order: known (the record
// and where a typed message went), unknown, or expired (its record let go long ago).
func (r *Runtime) MessageStatus(ctx context.Context, id string, ids []string) (wire.MessageStatuses, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.MessageStatuses, error) {
		return wire.MessageStatuses{Messages: c.box.Status(ids)}, nil
	})
}

// ShotPath is the file of a picture the conversation lists in its snapshot, and nothing else: an id it does not list is
// wire.ErrNoShot, whatever the caller meant.
func (r *Runtime) ShotPath(ctx context.Context, id, shot string) (string, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (string, error) {
		if p, ok := c.shotPaths[shot]; ok {
			return p, nil
		}
		return "", wire.ErrNoShot
	})
}

// PicturePath is the file of a picture the conversation's messages name, as its snapshot lists it, and the folders it
// may be shown from: the file is checked against them again when it is opened. An id it does not list is
// wire.ErrNoShot.
func (r *Runtime) PicturePath(ctx context.Context, id, picture string) (string, []string, error) {
	type found struct {
		path  string
		roots []string
	}
	got, err := live(r, ctx, id, func(c *conv, now time.Time) (found, error) {
		if p, ok := c.pics.paths[picture]; ok {
			return found{p, c.pictureRoots()}, nil
		}
		return found{}, wire.ErrNoShot
	})
	return got.path, got.roots, err
}

// Touched is whether the conversation's agent wrote, edited or read the file at this path in one of its turns.
func (r *Runtime) Touched(ctx context.Context, id, path string) (bool, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) (bool, error) { return c.fd.Touched(path), nil })
}

func (r *Runtime) Tasks(ctx context.Context, id string) ([]wire.Task, error) {
	return live(r, ctx, id, func(c *conv, now time.Time) ([]wire.Task, error) { return c.fd.Tasks(), nil })
}

// Do answers a command once per opId: the same body gets the same answer for OpsKept, another body conflict. It
// answers within the budget; a command still running then answers refused{timeout}, and its retry waits for the
// first one's answer.
func (r *Runtime) Do(ctx context.Context, device string, cmd wire.Command) wire.CmdResult {
	if cmd == nil || cmd.Validate() != nil {
		return wire.Refused(wire.RefuseBadRequest)
	}
	if m, ok := cmd.(wire.DraftSetCmd); ok {
		return r.draftSet(device, m)
	}
	budget := time.Duration(r.d.Limits.CmdBudgetMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	raw, _ := json.Marshal(cmd)
	sum := sha256.Sum256(append([]byte(string(cmd.Op())+"\x00"), raw...))
	hash, key := hex.EncodeToString(sum[:]), cmd.OpKey()
	for {
		r.opsMu.Lock()
		if op, ok := r.ops[key]; ok {
			r.opsMu.Unlock()
			if op.Hash != hash {
				return wire.Refused(wire.RefuseConflict)
			}
			return op.Result
		}
		wait, running := r.inflight[key]
		if !running {
			r.inflight[key] = make(chan struct{})
			r.opsMu.Unlock()
			break
		}
		r.opsMu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return wire.Refused(wire.RefuseTimeout)
		}
	}

	result := make(chan wire.CmdResult, 1)
	finish := func(res wire.CmdResult) {
		r.opsMu.Lock()
		// An internal failure or a timeout is not the command's answer: the same opId may try again.
		if res.R != wire.ResultRefused || res.Code != wire.RefuseInternal && res.Code != wire.RefuseTimeout {
			r.ops[key] = opRecord{Hash: hash, Result: res, At: ms(r.d.Clock.Now())}
			r.pruneOps()
			r.opsDirty = true
		}
		close(r.inflight[key])
		delete(r.inflight, key)
		r.opsMu.Unlock()
		result <- res
	}
	run := func() {
		if err := r.route(device, cmd, finish); err != nil {
			finish(wire.Refused(refusal(err)))
		}
	}
	// What runs on this goroutine (a launch, a project) must not hold the answer past the budget: it runs beside it.
	if r.inline {
		run()
	} else {
		go run()
	}
	select {
	case res := <-result:
		return res
	case <-ctx.Done():
		return wire.Refused(wire.RefuseTimeout)
	}
}

func refusal(err error) wire.RefuseCode {
	switch {
	case errors.Is(err, ErrNotFound):
		return wire.RefuseNotFound
	case errors.Is(err, ErrBusy):
		return wire.RefuseTimeout
	}
	return wire.RefuseInternal
}

func (r *Runtime) pruneOps() {
	now := r.d.Clock.Now()
	for k, op := range r.ops {
		if now.Sub(time.UnixMilli(op.At)) >= OpsKept {
			delete(r.ops, k)
		}
	}
	if len(r.ops) <= OpsMax {
		return
	}
	keys := make([]string, 0, len(r.ops))
	for k := range r.ops {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return r.ops[keys[i]].At < r.ops[keys[j]].At })
	for _, k := range keys[:len(keys)-OpsMax] {
		delete(r.ops, k)
	}
}

// route sends the command to the actor it is about, or runs it on the runtime; finish takes its answer once.
func (r *Runtime) route(device string, cmd wire.Command, finish func(wire.CmdResult)) error {
	switch m := cmd.(type) {
	case wire.ConversationCreateCmd:
		finish(r.create(m))
		return nil
	case wire.ProjectCreateCmd:
		finish(r.createProject(m))
		return nil
	case wire.ProjectUpdateCmd:
		finish(r.updateProject(m))
		return nil
	case wire.ProjectDeleteCmd:
		finish(r.deleteProject(device, m))
		return nil
	case wire.FavGroupCreateCmd, wire.FavGroupRenameCmd, wire.FavGroupDeleteCmd, wire.FavGroupMoveCmd:
		finish(r.favGroup(cmd))
		return nil
	case wire.FavPlaceCmd:
		finish(r.favPlace(m))
		return nil
	case wire.ConversationForkCmd:
		finish(r.fork(m))
		return nil
	case wire.ConversationHandoffCmd:
		finish(r.handoff(m))
		return nil
	case wire.ConversationMergeCmd:
		return r.merge(device, m, finish)
	case wire.ConversationResumeCmd:
		if m.ID == "" {
			finish(r.resumeSession(m))
			return nil
		}
	case wire.SeenCmd:
		if m.ConversationID == wire.SeenAll {
			finish(r.seenAll(m))
			return nil
		}
	case wire.ConversationMoveCmd:
		if m.ProjectID != nil && !r.hasProject(*m.ProjectID) {
			finish(wire.Refused(wire.RefuseNotFound))
			return nil
		}
	}
	id := convOf(cmd)
	a := r.actor(id)
	if a == nil {
		return ErrNotFound
	}
	// The agent takes seconds to answer a /model and a process to end: the actor starts the work and the answer comes
	// when it is done.
	switch m := cmd.(type) {
	case wire.ModelCmd:
		return r.on(a, func(c *conv, now time.Time) { c.model(m, finish, now) })
	case wire.BgStopCmd:
		return r.on(a, func(c *conv, now time.Time) { c.bgStop(m, finish, now) })
	case wire.ConversationRetitleCmd:
		return r.on(a, func(c *conv, now time.Time) { c.retitle(finish, now) })
	case wire.TerminalStopCmd:
		return r.on(a, func(c *conv, now time.Time) {
			if c.deletedAt != 0 {
				finish(wire.Refused(wire.RefuseNotFound))
				return
			}
			c.stopTerminal(wire.TerminalStopPerson, 0, m.Force != nil && *m.Force, finish)
		})
	case wire.RewindCmd:
		return r.on(a, func(c *conv, now time.Time) {
			if c.deletedAt != 0 {
				finish(wire.Refused(wire.RefuseNotFound))
				return
			}
			c.rewind(m, finish, now)
		})
	case wire.RewindUndoCmd:
		return r.on(a, func(c *conv, now time.Time) {
			if c.deletedAt != 0 {
				finish(wire.Refused(wire.RefuseNotFound))
				return
			}
			c.rewindUndo(finish, now)
		})
	}
	// A mailbox that stays full ends the command here: its answer is refused{timeout}, and a retry may go.
	return r.on(a, func(c *conv, now time.Time) { finish(c.do(device, cmd, now)) })
}

// convOf is the conversation a command is about.
func convOf(cmd wire.Command) string {
	switch m := cmd.(type) {
	case wire.SendCmd:
		return m.ConversationID
	case wire.CancelCmd:
		return m.ConversationID
	case wire.RetryCmd:
		return m.ConversationID
	case wire.PromoteCmd:
		return m.ConversationID
	case wire.QueueMoveCmd:
		return m.ConversationID
	case wire.AnswerCmd:
		return m.ConversationID
	case wire.DecideCmd:
		return m.ConversationID
	case wire.InterruptCmd:
		return m.ConversationID
	case wire.QueueResumeCmd:
		return m.ConversationID
	case wire.SeenCmd:
		return m.ConversationID
	case wire.ConversationRenameCmd:
		return m.ID
	case wire.ConversationPinCmd:
		return m.ID
	case wire.ConversationMoveCmd:
		return m.ID
	case wire.ConversationDeleteCmd:
		return m.ID
	case wire.ConversationRestoreCmd:
		return m.ID
	case wire.ConversationResumeCmd:
		return m.ID
	case wire.TerminalStopCmd:
		return m.ID
	case wire.ModelCmd:
		return m.ID
	case wire.BgStopCmd:
		return m.ConversationID
	case wire.GoalCmd:
		return m.ID
	case wire.ConversationRetitleCmd:
		return m.ID
	case wire.RewindCmd:
		return m.ID
	case wire.RewindUndoCmd:
		return m.ID
	case wire.LimitCmd:
		return m.ID
	}
	return ""
}

func (r *Runtime) seenAll(m wire.SeenCmd) wire.CmdResult {
	res := wire.Accepted(wire.ChannelConvs, 0)
	for _, a := range r.all() {
		// A conversation removed since all() has nothing to mark seen.
		_ = r.call(context.Background(), a.c.rec.ID, func(c *conv, now time.Time) {
			if c.deletedAt != 0 {
				return
			}
			got := c.seen(m.UpTo, now)
			if res.Seq == 0 || got.Seq > 0 && got.Seq < res.Seq {
				res.Seq = got.Seq
			}
		})
	}
	return res
}

func (r *Runtime) hasProject(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		if p.ID == id {
			return true
		}
	}
	return false
}

func (r *Runtime) project(id string) (wire.Project, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		if p.ID == id {
			return p, true
		}
	}
	return wire.Project{}, false
}

// createProject makes a folder a project and publishes it as project_upsert; a folder that already is one answers with
// its own record published again, so the page waits for the same event either way.
func (r *Runtime) createProject(m wire.ProjectCreateCmd) wire.CmdResult {
	if m.Path == "" {
		return r.createGroup(strings.TrimSpace(m.Name))
	}
	path := filepath.Clean(m.Path)
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return wire.Refused(wire.RefuseNotFound)
	}
	p, seq := r.addProject(wire.Project{Path: path, Name: strings.TrimSpace(m.Name)})
	res := wire.Accepted(wire.ChannelConvs, seq)
	res.ID = &p.ID
	return res
}

// createGroup makes a project of a name alone: its chats start in the default folder, and its id is its own, so several
// such projects share that folder the way the ones taken over from Legacy do.
func (r *Runtime) createGroup(name string) wire.CmdResult {
	if r.d.DefaultCwd == "" {
		return wire.Refused(wire.RefuseUnsupported)
	}
	sum := sha256.Sum256([]byte(name + "\x00" + strconv.FormatInt(r.d.Clock.Now().UnixNano(), 10)))
	p, seq := r.addProject(wire.Project{ID: "prj-" + hex.EncodeToString(sum[:6]), Path: r.d.DefaultCwd, Name: name})
	res := wire.Accepted(wire.ChannelConvs, seq)
	res.ID = &p.ID
	return res
}

// updateProject changes a project's folder or name and publishes it again as project_upsert. The folder is where its
// next chats start; the chats it has keep theirs. The id stays: it was never the path's to be read from.
func (r *Runtime) updateProject(m wire.ProjectUpdateCmd) wire.CmdResult {
	path := ""
	if m.Path != "" {
		path = filepath.Clean(m.Path)
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			return wire.Refused(wire.RefuseNotFound)
		}
	}
	git := false
	if path != "" {
		_, err := os.Stat(filepath.Join(path, ".git"))
		git = err == nil
	}
	r.mu.Lock()
	i := slices.IndexFunc(r.projects, func(p wire.Project) bool { return p.ID == m.ID })
	if i < 0 {
		r.mu.Unlock()
		return wire.Refused(wire.RefuseNotFound)
	}
	p := r.projects[i]
	if path != "" {
		p.Path, p.Git = path, git
	}
	if name := strings.TrimSpace(m.Name); name != "" {
		p.Name = name
	}
	r.projects[i] = p
	saved := slices.Clone(r.projects)
	r.mu.Unlock()
	seq := r.d.Publisher.Publish(wire.ChannelConvs, wire.ProjectUpsertEv{Project: p})
	if r.d.Store != nil {
		if err := r.d.Store.saveProjects(saved); err != nil {
			r.d.Log.Warn("projects not saved", "err", err)
		}
	}
	return wire.Accepted(wire.ChannelConvs, seq)
}

// deleteProject removes a project. Its chats go first, each as its own upsert (keep: out to the common list) or
// removal (delete: forced, a working agent stopped too); then project_removed, the seq the page waits for. A chat
// deleted with it, or before, is out of any project too, so a restore brings it back to the common list.
func (r *Runtime) deleteProject(device string, m wire.ProjectDeleteCmd) wire.CmdResult {
	if !r.hasProject(m.ID) {
		return wire.Refused(wire.RefuseNotFound)
	}
	force := true
	for _, a := range r.all() {
		// A conversation removed since all() has no project left to leave.
		_ = r.call(context.Background(), a.c.rec.ID, func(c *conv, now time.Time) {
			if c.rec.ProjectID == nil || *c.rec.ProjectID != m.ID {
				return
			}
			c.rec.ProjectID = nil
			switch {
			case c.deletedAt != 0:
				c.touch()
			case m.Chats == wire.ProjectChatsDelete:
				c.touch()
				c.do(device, wire.ConversationDeleteCmd{OpID: m.OpID + "-" + c.rec.ID, ID: c.rec.ID, Force: &force}, now)
			default:
				c.recResult(now)
			}
		})
	}
	r.mu.Lock()
	r.projects = slices.DeleteFunc(r.projects, func(p wire.Project) bool { return p.ID == m.ID })
	saved := slices.Clone(r.projects)
	r.mu.Unlock()
	seq := r.d.Publisher.Publish(wire.ChannelConvs, wire.ProjectRemovedEv{ID: m.ID})
	if r.d.Store != nil {
		if err := r.d.Store.saveProjects(saved); err != nil {
			r.d.Log.Warn("projects not saved", "err", err)
		}
	}
	return wire.Accepted(wire.ChannelConvs, seq)
}

// addProject publishes the project of want.Path, made from want when the folder is no project yet: its id taken
// from the path, its name want.Name or the folder's. A project with its own id (one taken over from Legacy, where
// projects are groups and several share a folder) is that project, whatever folder others have.
func (r *Runtime) addProject(want wire.Project) (wire.Project, int64) {
	r.mu.Lock()
	var p wire.Project
	var saved []wire.Project
	for _, have := range r.projects {
		if want.ID == "" && have.Path == want.Path || want.ID != "" && have.ID == want.ID {
			p = have
		}
	}
	if p.ID == "" {
		p = wire.Project{ID: want.ID, MachineID: r.d.MachineID, Path: want.Path, Name: want.Name, CreatedAt: ms(r.d.Clock.Now())}
		if p.ID == "" {
			sum := sha256.Sum256([]byte(want.Path))
			p.ID = "prj-" + hex.EncodeToString(sum[:6])
		}
		if p.Name == "" {
			p.Name = filepath.Base(want.Path)
		}
		if _, err := os.Stat(filepath.Join(want.Path, ".git")); err == nil {
			p.Git = true
		}
		r.projects = append(r.projects, p)
		saved = append(saved, r.projects...)
	}
	r.mu.Unlock()
	seq := r.d.Publisher.Publish(wire.ChannelConvs, wire.ProjectUpsertEv{Project: p})
	if r.d.Store != nil && saved != nil {
		if err := r.d.Store.saveProjects(saved); err != nil {
			r.d.Log.Warn("projects not saved", "err", err)
		}
	}
	return p, seq
}

// create opens a conversation and asks the launcher for its agent; its first message waits in the queue as
// queued{launching}.
// worktree makes the folder a conversation of a launch on several models works in.
func (r *Runtime) worktree(cwd, name string) (string, error) {
	if r.d.Worktree != nil {
		return r.d.Worktree(cwd, name)
	}
	return makeWorktree(cwd, name)
}

func (r *Runtime) create(m wire.ConversationCreateCmd) wire.CmdResult {
	cwd := m.Cwd
	var projectID *string
	if m.ProjectID != "" {
		p, ok := r.project(m.ProjectID)
		if !ok {
			return wire.Refused(wire.RefuseNotFound)
		}
		cwd, projectID = p.Path, &p.ID
	}
	var parentID *string
	if m.ParentID != "" {
		if _, ok := r.Record(m.ParentID); !ok {
			return wire.Refused(wire.RefuseNotFound)
		}
		parentID = &m.ParentID
	}
	if r.d.Launcher == nil {
		return wire.Refused(wire.RefuseUnsupported)
	}
	model, code := r.onProfile(m.Provider, m.Model, m.Profile)
	if code != "" {
		return wire.Refused(code)
	}
	m.Model = model
	id := "tab-" + newID()
	if m.Worktree {
		// One of several chats started by a prompt works in a tree of its own, so they do not step on each other.
		parts := []string{newID()[:6]}
		if m.Launch != nil {
			parts = []string{m.Launch.ID}
		}
		if m.Model != nil {
			parts = append(parts, m.Model.ID)
		}
		made, err := r.worktree(cwd, worktreeName(append(parts, id[len(id)-4:])...))
		if err != nil {
			r.d.Log.Warn("worktree not made", "cwd", cwd, "err", err)
			return wire.Refused(wire.RefuseInternal)
		}
		cwd = made
	}
	now := r.d.Clock.Now()
	rec := wire.Conversation{ID: id, MachineID: r.d.MachineID, ProjectID: projectID, Cwd: cwd, Launch: m.Launch,
		Provider: m.Provider, Model: m.Model, TitleSource: wire.TitleFromFirstMessage, CreatedAt: ms(now),
		LastActivityAt: ms(now), LastPromptAt: ms(now), SeenAt: ms(now), State: wire.AgentState{K: wire.StateUnknown, Since: ms(now)},
		ParentID: parentID, Rev: 1}
	if m.First != nil {
		rec.Title = titleOf(firstMessageTitle(m.First.Text))
	}
	if t := strings.TrimSpace(m.Title); t != "" {
		rec.Title, rec.TitleSource = t, wire.TitleFromUser
	}
	return r.openConv(rec, LaunchRequest{Cwd: cwd, Provider: m.Provider, Model: m.Model}, m.First, false, "")
}

// onProfile is the model a conversation starts with: model, with the profile on it when one is named. The profile has to
// be one the machine knows, run through the CLI of the provider and have a key saved; the model and its effort have to
// be ones the profile lists (ultracode stays Claude's own). Without a profile the model must not be one a profile owns:
// a conversation on the CLI's own account asking for a vendor's model would run on the wrong account.
func (r *Runtime) onProfile(p wire.Provider, model *wire.Model, profile string) (*wire.Model, wire.RefuseCode) {
	if profile == "" {
		if model != nil && r.d.Profiles != nil && r.d.Profiles.OwnsModel(model.ID) {
			return nil, wire.RefuseOptionUnknown
		}
		return model, ""
	}
	if r.d.Profiles == nil {
		return nil, wire.RefuseUnsupported
	}
	via, configured, models, ok := r.d.Profiles.Lookup(profile)
	switch {
	case !ok:
		return nil, wire.RefuseNotFound
	case via != p || !configured || len(models) == 0:
		return nil, wire.RefuseUnsupported
	}
	if model == nil {
		model = &wire.Model{ID: models[0]}
	}
	capabilities, known := r.d.Profiles.Model(profile, model.ID)
	if !slices.Contains(models, model.ID) || !known || model.Ultracode ||
		(model.Effort != nil && !slices.Contains(capabilities.Efforts, *model.Effort)) {
		return nil, wire.RefuseOptionUnknown
	}
	on := *model
	on.Profile = profile
	return &on, ""
}

// openConv adds a conversation and asks the launcher for its agent: a fresh one whose first message (first, when
// given) waits in the queue as queued{launching} and goes with the launch, or one resumed on a session it kept, whose
// history the feed shows at once.
func (r *Runtime) openConv(rec wire.Conversation, req LaunchRequest, first *wire.FirstMessage, requireCarry bool, handoffArtifact string) wire.CmdResult {
	if requireCarry && (first == nil || strings.TrimSpace(first.Text) == "" || jscompat.Len(first.Text) > outbox.MaxCarriedText ||
		len(first.AttachmentIDs) > 0) {
		return wire.Refused(wire.RefuseInternal)
	}
	c := newConv(&r.d, rec)
	if handoffArtifact != "" && first != nil {
		c.handoffArtifact, c.handoffArtifactClientID = handoffArtifact, first.ClientID
	}
	r.add(c)
	id := rec.ID
	req.ID = id
	var res wire.CmdResult
	// The conversation was added a line above, so only a stop of its actor fails this call; it is then gone.
	_ = r.call(context.Background(), id, func(c *conv, now time.Time) {
		c.mark, c.markConvs = 0, 0
		c.input(agentstate.Input{K: agentstate.LaunchRequested}, now)
		if rec.TranscriptPath != "" {
			c.feedUpdate(c.fd.Load(), false, now)
		}
		c.recDirty = true
		c.flush(now, true)
		res = wire.Accepted(wire.ChannelConvs, c.convsSeq())
		if first != nil {
			c.send(wire.SendCmd{OpID: first.ClientID, ClientID: first.ClientID, ConversationID: id, Text: first.Text,
				AttachmentIDs: first.AttachmentIDs, Mode: wire.ModeQueue, Kind: wire.KindPrompt}, now)
			// The first message goes with the launch, as the agent's first prompt: the starting terminal takes no keys
			// for a while and drops what is typed into it, a prompt of the command line it does not.
			ch, carried := c.box.Carry(first.ClientID, now)
			c.change(ch, now)
			if carried {
				req.First = first.Text
			}
		}
		if err := r.d.Launcher.Launch(req); err != nil {
			c.d.Log.Warn("launch failed", "conv", id, "err", err)
			c.input(agentstate.Input{K: agentstate.LaunchFailed, Failure: wire.LaunchFailedNoProcess}, now)
		}
		c.save(now, true)
	})
	res.ID = &id
	return res
}

// Transcripts are the agent transcripts of the conversations, as their actors last left them: read without asking an
// actor anything, so a reader beside the runtime (the design links' backfill) never wakes or holds one.
func (r *Runtime) Transcripts() []string {
	var out []string
	for _, a := range r.all() {
		if v := a.last.Load(); v != nil && !v.deleted && v.rec.TranscriptPath != "" {
			out = append(out, v.rec.TranscriptPath)
		}
	}
	return out
}

// AttachedSessions are the agent sessions the conversations hold, by the agent's own session id.
func (r *Runtime) AttachedSessions() map[string]string {
	out := map[string]string{}
	for _, a := range r.all() {
		v := a.last.Load()
		if v == nil || v.deleted {
			continue
		}
		if v.rec.AgentSessionID != "" {
			out[v.rec.AgentSessionID] = v.rec.ID
		}
		if v.rec.TranscriptPath != "" {
			out[strings.TrimSuffix(filepath.Base(v.rec.TranscriptPath), ".jsonl")] = v.rec.ID
		}
		for _, s := range v.left {
			out[s] = v.rec.ID
		}
	}
	return out
}

// resumeSession opens a conversation on a session the agent kept outside the runtime. A session a conversation already
// holds is not opened twice: the answer is that conversation.
func (r *Runtime) resumeSession(m wire.ConversationResumeCmd) wire.CmdResult {
	if r.d.Launcher == nil || r.d.AgentSession == nil {
		return wire.Refused(wire.RefuseUnsupported)
	}
	s, ok := r.d.AgentSession(m.Provider, m.SessionID)
	if !ok {
		return wire.Refused(wire.RefuseNotFound)
	}
	cwd := filepath.Clean(m.Cwd)
	if s.Cwd != cwd {
		return wire.Refused(wire.RefuseBadRequest)
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return wire.Refused(wire.RefuseNotFound)
	}
	r.resumeMu.Lock()
	defer r.resumeMu.Unlock()
	if held, ok := r.AttachedSessions()[m.SessionID]; ok {
		var res wire.CmdResult
		if err := r.call(context.Background(), held, func(c *conv, now time.Time) { res = c.recResult(now) }); err != nil {
			return wire.Refused(refusal(err))
		}
		res.ID = &held
		return res
	}
	now := r.d.Clock.Now()
	rec := wire.Conversation{ID: "tab-" + newID(), MachineID: r.d.MachineID, Cwd: cwd, Provider: m.Provider,
		Title: titleOf(s.Title), TitleSource: wire.TitleFromFirstMessage, CreatedAt: ms(now), LastActivityAt: ms(now), LastPromptAt: ms(now), SeenAt: ms(now),
		State: wire.AgentState{K: wire.StateUnknown, Since: ms(now)}, Rev: 1,
		TranscriptPath: s.TranscriptPath, AgentSessionID: m.SessionID}
	r.mu.Lock()
	for _, p := range r.projects {
		if p.Path == cwd {
			id := p.ID
			rec.ProjectID = &id
		}
	}
	r.mu.Unlock()
	return r.openConv(rec, LaunchRequest{Cwd: cwd, Provider: m.Provider, ResumeSessionID: m.SessionID,
		TranscriptPath: s.TranscriptPath}, nil, false, "")
}

// annotationRE is a note on a design's page as a message carries it (web/app/next/features/designly/annotation.ts):
// the opening tag on its own line, the note's lines, the closing tag.
var annotationRE = regexp.MustCompile(`(?s)^<annotation\b[^>]*>\n(.*?)\n</annotation>\s*`)

// firstMessageTitle is what names a conversation started by a message: the words typed, or, for a message that is
// only notes on a design's page, the first note's comment, else the block it is on. The notes' markup is never a title.
func firstMessageTitle(text string) string {
	text = strings.TrimSpace(text)
	var element string
	for {
		m := annotationRE.FindStringSubmatchIndex(text)
		if m == nil {
			break
		}
		for _, line := range strings.Split(text[m[2]:m[3]], "\n") {
			if c, ok := strings.CutPrefix(line, "comment: "); ok && strings.TrimSpace(c) != "" {
				return strings.TrimSpace(c)
			}
			if e, ok := strings.CutPrefix(line, "element: "); ok && element == "" {
				element = strings.TrimSpace(e)
			}
		}
		text = text[m[1]:]
	}
	if text == "" {
		return element
	}
	return text
}

// titleOf is a first message's first line, cut to a title.
func titleOf(text string) string {
	line := text
	for i, r := range text {
		if r == '\n' {
			line = text[:i]
			break
		}
	}
	rs := []rune(line)
	if len(rs) > 80 {
		rs = append(rs[:79], '…')
	}
	return string(rs)
}
