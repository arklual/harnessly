package convo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/ly-solutions/harnessly/internal/safego"
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
	// parked are the conversations at rest whose actors were let go (unload.go): the record each left, which is what
	// the lists answer with. Asked for by its id, a parked conversation gets an actor again; reviveMu makes that one
	// actor.
	parked   map[string]*recView
	reviveMu sync.Mutex
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

	// restartLoads holds a place for each conversation reading its transcript at the start.
	restartLoads chan struct{}

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
	// told: the actor took its first message; kept here so a loop that starts again after a panic goes on looking.
	told bool
	// used is when the conversation was last asked for by its id (a page, a hook, a message), in unix nanoseconds; the
	// lists and the sweeps do not count. gone: the actor parked itself in this turn, and its loop ends.
	used atomic.Int64
	gone bool
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
	d.epoch = newID()
	d.startResumes = make(chan struct{}, startResumes)
	return &Runtime{d: d, epoch: d.epoch, actors: map[string]*actor{}, parked: map[string]*recView{}, bySession: map[string]string{}, ops: map[string]opRecord{}, drafts: draftBook{},
		inflight: map[string]chan struct{}{}, stop: make(chan struct{}), restartLoads: make(chan struct{}, restartLoads)}
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
		if n := s.sweepTemps(time.Now()); n > 0 {
			r.d.Log.Info("temporary files of unfinished writes removed", "files", n)
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
			s.Async(r.saveResult)
		}
		r.wg.Add(2)
		r.keep("convo ops saver", r.opsSaver)
		r.keep("convo reaper", r.reaper)
		if r.d.Index != nil {
			r.wg.Add(1)
			r.keep("convo indexer", r.indexer)
		}
	}
	return errors.Join(errs...)
}

// Stop ends the actors after each wrote its last state.
func (r *Runtime) Stop() {
	// The stop is said before the actors are listed, under the lock an actor parks itself under: none parks (and
	// closes its own stop) after it is on the list.
	r.mu.Lock()
	if !r.inline && !r.stopping() {
		close(r.stop)
	}
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
	for _, a := range actors {
		close(a.stop)
	}
	r.wg.Wait()
	// The last writes have a term of their own: a file system that hangs must not hold the runtime's stop for good.
	ctx, cancel := context.WithTimeout(context.Background(), FlushWait)
	defer cancel()
	saved := make(chan struct{})
	safego.Go("convo last writes", func() {
		defer close(saved)
		r.saveOps()
		r.saveDrafts()
		if r.d.Store != nil {
			if err := r.d.Store.CloseWithin(ctx); err != nil {
				r.d.Log.Warn("conversations not all written at the stop", "err", err)
			}
		}
	})
	select {
	case <-saved:
	case <-ctx.Done():
		r.d.Log.Warn("the stop did not wait for the last writes", "waited", FlushWait.String())
	}
}

func (r *Runtime) restore(f convFile) { r.wake(f, false) }

// wake gives a written conversation its actor: at a start of the runtime, or (parked) when a conversation whose
// actor was let go is asked for again.
func (r *Runtime) wake(f convFile, parked bool) *actor {
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
	atRest := c.wakeAtRest(f.Rest)
	if s, ok := r.d.Publisher.(SeqTeller); ok && parked {
		// The runtime did not stop: the conversation's channel goes on from where it is.
		c.seq = s.Seq(wire.ConvChannel(rec.ID))
	}
	a := r.add(c)
	_ = r.on(a, func(c *conv, now time.Time) { // a fresh mailbox is not full
		if atRest && parked {
			// The runtime did not stop meanwhile and the transcript is the one the conversation was written over: it
			// stands as it was parked, and nothing is looked up again.
			return
		}
		if atRest {
			// A stopped conversation whose transcript did not change reads nothing: it holds no place of the few.
			c.restarted(now)
			return
		}
		// Any other conversation reads its transcript's tail at a start: a few at a time, and each lets go of it after,
		// so a machine of hundreds of chats does not hold them all at once (10 GB at a start, 3 GB after).
		r.restartLoads <- struct{}{}
		defer func() { <-r.restartLoads }()
		c.restarted(now)
		c.fd.Release()
	})
	return a
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
	if !c.unread {
		// A conversation that came back at rest (wakeAtRest) has its feed where it stood, with the client ids it held.
		c.feedUpdate(c.fd.Load(), false, now) // bounded with the others by r.restartLoads around restarted (HRN-901)
		for _, m := range c.box.Messages(now) {
			if m.State.EntryID != nil {
				c.fd.SetClientID(*m.State.EntryID, m.ClientID)
			}
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
	a.used.Store(r.d.Clock.Now().UnixNano())
	a.publish()
	if r.inline {
		c.spawn = func(work func() any, done func(any)) {
			c.jobs++
			r.pending = append(r.pending, job{work, func(res any) { c.jobs--; done(res) }})
		}
	} else {
		c.spawn = func(work func() any, done func(any)) {
			id := c.rec.ID
			c.jobs++
			back := func(res any) { c.jobs--; done(res) }
			safego.Go("conversation "+id+" work", func() {
				var res any
				if p := safego.Try("conversation "+id+" work", func() { res = work() }); p != nil {
					// The work is lost, not the chat: its end is still told, with nothing read, so what waits on it goes on.
					r.d.Log.Warn("conversation work panicked", "conv", id, "panic", fmt.Sprint(p.Value), "stack", string(p.Stack))
					res = nil
				}
				select {
				case a.mail <- func() { back(res) }:
				case <-a.stop:
				}
			})
		}
	}
	r.mu.Lock()
	r.actors[c.rec.ID] = a
	delete(r.parked, c.rec.ID)
	if c.rec.Session != "" {
		r.bySession[c.rec.Session] = c.rec.ID
	}
	r.mu.Unlock()
	if !r.inline {
		r.wg.Add(1)
		safego.Go("conversation "+c.rec.ID, func() { r.supervise(a) })
	}
	return a
}

// keep runs one of the runtime's loops, which ends with wg.Done; a loop that panicked starts again after a pause that
// grows, so one bad turn of it does not end the saving or the sweeping for good.
func (r *Runtime) keep(name string, loop func()) {
	safego.Go(name, func() {
		for pause := time.Second; !safego.Run(name, loop); pause = min(2*pause, time.Minute) {
			select {
			case <-r.stop:
				return
			case <-time.After(pause):
			}
			r.wg.Add(1)
		}
	})
}

// ActorRestartAfter is how long an actor that panicked waits before it runs again; the wait doubles with every panic
// in a row, up to ActorRestartMax.
var (
	ActorRestartAfter = 5 * time.Second
	ActorRestartMax   = 5 * time.Minute
)

// supervise runs the actor's loop and holds its panics: a line of a transcript or a screen the code did not expect
// stops one conversation for a while, never the runtime with every conversation of the profile. The conversation
// goes unknown, as after a restart of the runtime — the page shows that, and the first look of the actor's next run
// finds what its agent really does.
func (r *Runtime) supervise(a *actor) {
	defer r.wg.Done()
	c := a.c
	pause, last := ActorRestartAfter, time.Time{}
	for {
		name := "conversation " + c.rec.ID
		p := safego.Try(name, func() { r.loop(a) })
		if p == nil {
			return
		}
		if time.Since(last) > 2*ActorRestartMax {
			pause = ActorRestartAfter // the panics before were long ago: not a row of them
		}
		last = time.Now()
		r.d.Log.Warn("conversation actor panicked; it starts again after a pause", "conv", c.rec.ID, "pause", pause.String(),
			"panic", fmt.Sprint(p.Value), "stack", string(p.Stack))
		safego.Run(name, func() {
			now := r.d.Clock.Now()
			c.input(agentstate.Input{K: agentstate.Restart}, now)
			c.flush(now, true)
			a.publish()
		})
		select {
		case <-r.d.Clock.After(pause):
		case <-a.stop:
			safego.Run(name, func() { c.save(r.d.Clock.Now(), true) })
			return
		}
		pause = min(2*pause, ActorRestartMax)
	}
}

// loop is one actor: its mail in order, its clock, a look and a read every LookEvery.
func (r *Runtime) loop(a *actor) {
	c := a.c
	next := r.d.Clock.Now()
	// The actor looks only after its first message, the one its maker sends: that one reads the transcript as history,
	// and a look before it would take the history for news (a turn a resumed session left open would be running).
	looked := next // when the clock last looked
	spun := false  // the last turn left its clock due: said once in the log, not at every slowed turn
	for {
		now := r.d.Clock.Now()
		wake := c.wake(now)
		if a.told && next.Before(wake) {
			wake = next
		}
		select {
		case f := <-a.mail:
			f()
			if a.gone {
				return
			}
			a.told = true
			a.publish()
			// The message may have changed the pace (a launch or resume makes the agent starting): the next look is
			// due by the new one, not one the state before set (10 s for a stopped chat).
			if due := looked.Add(c.lookEvery()); due.Before(next) {
				next = due
			}
		case <-r.d.Clock.After(wake.Sub(now)):
			now = r.d.Clock.Now()
			if a.told && !now.Before(next) {
				c.read(now)
				c.observe(now)
				looked, next = now, now.Add(c.lookEvery())
			}
			c.tick(now)
			c.save(now, false)
			a.publish()
			if w := c.wake(r.d.Clock.Now()); !w.After(now) {
				// The turn did not move the time it woke for: something holds it that the clock does not. The
				// next turn waits a little rather than none — a time of the actor that never moves would be a core.
				if !spun {
					r.d.Log.Warn("conversation clock due again right after its turn; slowed", "conv", c.rec.ID)
				}
				spun = true
				select {
				case <-r.d.Clock.After(spinPause):
				case <-a.stop:
					c.save(r.d.Clock.Now(), true)
					return
				}
			} else {
				spun = false
			}
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
		a.publish() // what the loop does after every turn
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

// spinPause is how long an actor waits when its clock is due again right after a turn: a deadline nothing moves
// costs four turns a second, not a core.
const spinPause = 250 * time.Millisecond

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
	return r.callOn(ctx, r.actor(id), f)
}

// callOn is call on an actor already in hand: what a list or a sweep uses, which must not count as someone asking
// for the conversation. nil, or an actor that ended meanwhile, is ErrNotFound.
func (r *Runtime) callOn(ctx context.Context, a *actor, f func(c *conv, now time.Time)) error {
	if a == nil {
		return ErrNotFound
	}
	if r.inline {
		return r.on(a, f)
	}
	done := make(chan struct{})
	// taken: the actor began the turn (1), or the caller gave the message up before it did (2).
	var taken atomic.Int32
	turn := func() {
		if !taken.CompareAndSwap(0, 1) {
			return
		}
		f(a.c, r.d.Clock.Now())
		close(done)
	}
	select {
	case a.mail <- turn:
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
	case <-a.stop:
		// The actor ends (the runtime stops, or the conversation was parked). A turn it has begun is waited for, as
		// before; one still in the mail is taken back, so it never runs beside the caller that was told it did not.
		if taken.CompareAndSwap(0, 2) {
			return ErrNotFound
		}
		<-done
		return nil
	}
}

// actor is the conversation's actor for someone who asks for the conversation by its id: it counts as a use, and a
// parked conversation gets its actor back. nil for a conversation the runtime does not have.
func (r *Runtime) actor(id string) *actor {
	r.mu.Lock()
	a := r.actors[id]
	_, parked := r.parked[id]
	if a != nil {
		a.used.Store(r.d.Clock.Now().UnixNano())
	}
	r.mu.Unlock()
	if a == nil && parked {
		return r.revive(id)
	}
	return a
}

// loaded is the conversation's actor when it has one now; it wakes nothing and counts as no use.
func (r *Runtime) loaded(id string) *actor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actors[id]
}

// view is the record the conversation last left, with or without an actor; nil for one the runtime does not have.
func (r *Runtime) view(id string) *recView {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.actors[id]; a != nil {
		return a.last.Load()
	}
	return r.parked[id]
}

// views are the records of every conversation, the parked ones too, read without asking an actor anything.
func (r *Runtime) views() []*recView {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*recView, 0, len(r.actors)+len(r.parked))
	for _, a := range r.actors {
		if v := a.last.Load(); v != nil {
			out = append(out, v)
		}
	}
	for _, v := range r.parked {
		out = append(out, v)
	}
	return out
}

func (r *Runtime) stopping() bool {
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
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

// PermissionRequest opens the person's card for what the agent's hook asks and waits for the decision. false, at any
// time, leaves it to the agent's own dialog: the gate ended some other way, the hook went, nothing is to be asked here.
func (r *Runtime) PermissionRequest(ctx context.Context, id string, ask PermissionAsk) (PermissionReply, bool) {
	a := r.actor(id)
	if a == nil || !ask.asksThePerson() {
		return PermissionReply{}, false
	}
	reply := make(chan PermissionReply, 1)
	if err := r.on(a, func(c *conv, now time.Time) { c.askPermission(ask, reply, now) }); err != nil {
		return PermissionReply{}, false
	}
	select {
	case answer, ok := <-reply:
		return answer, ok
	case <-ctx.Done():
		_ = r.on(a, func(c *conv, now time.Time) { c.hookGone(reply, now) })
		return PermissionReply{}, false
	}
}

// Hook is one of the agent's hooks, as POST /api/status/hook carries it.
type Hook struct {
	// Kind: session-start, prompt-submit, notification, stop, stop-failure, pre-compact, post-compact.
	Kind string
	// Notification is the notification's type (permission_prompt, idle_prompt, …).
	Notification string
	// Failure is what a stop-failure says went wrong.
	Failure wire.TurnFailureCode
	// PromptHash of a prompt-submit names the prompt the agent took, "" when the hook did not say.
	PromptHash string
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
			c.promptHash = h.PromptHash
			defer func() { c.promptHash = "" }()
		case "stop":
			if c.inputTurns {
				// The structured channel tells this conversation's turn ends, each once.
				return
			}
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
				// The prompt itself is read off the screen: its options and the call it is about. The agent has said it
				// waits, so if the screen cannot be read as any menu the card still comes (promptUnseen).
				c.promptAsked = now
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

// InputTurnEnded is the end of a turn as the conversation's structured channel tells it: failed says the turn ended on
// an error, detail what the agent said of it.
func (r *Runtime) InputTurnEnded(id string, failed bool, detail string) error {
	a := r.actor(id)
	if a == nil {
		return ErrNotFound
	}
	return r.on(a, func(c *conv, now time.Time) {
		if !c.inputTurns {
			return
		}
		c.read(now)
		in := c.stopInput("")
		if failed {
			in = agentstate.Input{K: agentstate.StopFailure, TurnFailure: wire.TurnFailedUnknown, TurnDetail: detail}
		}
		c.input(in, now)
		c.observe(now)
	})
}

// InputLost: the conversation's structured channel ended. The turn ends are the hooks' to tell again; the next message
// tries the channel anew.
func (r *Runtime) InputLost(id string) {
	if a := r.loaded(id); a != nil {
		_ = r.on(a, func(c *conv, _ time.Time) { c.inputTurns = false })
	}
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
func (r *Runtime) Has(id string) bool { return r.view(id) != nil }

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
	v := r.view(id)
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
	rec := c.rec
	rec.NotSavedSince = 0 // says what the disk does now, never what it did before a restart
	return convFile{Conv: rec, Session: c.rec.Session, TranscriptPath: c.rec.TranscriptPath,
		AgentSessionID: c.rec.AgentSessionID, HandoffArtifact: c.handoffArtifact,
		HandoffArtifactClientID: c.handoffArtifactClientID, Outbox: c.box.State(), Questions: c.questions(), Permissions: c.permissions(),
		DeletedAt: c.deletedAt, Rewinds: c.rewinds, Left: c.left, Rest: c.rest()}
}

// save writes the conversation when it changed, at most every SaveEvery; a conversation deleted longer than
// DeletedKept goes from the disk.
func (c *conv) save(now time.Time, force bool) {
	s := c.d.Store
	if s == nil || !c.dirty || !force && now.Sub(c.savedAt) < SaveEvery {
		return
	}
	if !force && now.Before(c.saveRetryAt) {
		return
	}
	if err := s.saveConv(c.file()); err != nil {
		// The state stays dirty and is written again after a pause that grows with the failures.
		c.saveRetryAt = now.Add(retryPause(c.saveFails + 1))
		c.saveResult(c.saveFails+1, err, now)
		return
	}
	c.dirty, c.savedAt, c.saveRetryAt = false, now, time.Time{}
	if !s.isAsync() {
		c.saveResult(0, nil, now)
	}
}

// NotSavedAfter is how many writes of a conversation must fail in a row before its record says it is not saved.
var NotSavedAfter = 3

// saveResult takes how a write of the conversation ended: fails failures in a row with err, or (0, nil) for a write
// that reached the disk. From NotSavedAfter failures the record says so, and the first good write takes it back.
func (c *conv) saveResult(fails int, err error, now time.Time) {
	if fails > 0 {
		c.d.Log.Warn("conversation not saved", "conv", c.rec.ID, "failsInARow", fails, "err", err)
	}
	c.saveFails = fails
	switch {
	case fails >= NotSavedAfter && c.rec.NotSavedSince == 0:
		c.rec.NotSavedSince = ms(now)
	case fails == 0 && c.rec.NotSavedSince != 0:
		c.d.Log.Info("conversation saved again", "conv", c.rec.ID, "notSavedFor", now.Sub(time.UnixMilli(c.rec.NotSavedSince)).Round(time.Second).String())
		c.rec.NotSavedSince = 0
	default:
		return
	}
	c.touch()
	c.flush(now, true)
}

// saveResult is the store's word on a write of its writer: it goes to the conversation's actor, when it has one.
func (r *Runtime) saveResult(path string, fails int, err error) {
	id := r.d.Store.convOfPath(path)
	a := r.loaded(id)
	if a == nil {
		r.d.Log.Warn("conversation not saved", "path", path, "failsInARow", fails, "err", err)
		return
	}
	_ = r.on(a, func(c *conv, now time.Time) { c.saveResult(fails, err, now) })
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
	// The parked conversations are listed from the records they left, and before the actors are asked: one that parks
	// while the list is made is then found among them again below.
	r.mu.Lock()
	actors := make([]*actor, 0, len(r.actors))
	for _, a := range r.actors {
		actors = append(actors, a)
	}
	for _, v := range r.parked {
		if !v.deleted {
			out = append(out, v.rec)
		}
	}
	r.mu.Unlock()
	// Every actor is asked at once and the whole list waits listWait, not each actor in turn: with hundreds of
	// conversations a few dozen busy ones (reading a long transcript) made the list take as many quarters of a second,
	// longer than the page waits for it. A busy one is listed by the record it published last.
	views := make([]*recView, len(actors))
	wait, cancel := context.WithTimeout(ctx, listWait)
	ask := func(i int, a *actor) {
		v, err := liveOn(r, wait, a, func(c *conv, now time.Time) (recView, error) { return recView{rec: c.rec}, nil })
		if err == nil {
			views[i] = &v
			return
		}
		// Deleted, parked a moment ago, or busy: the record it left is the answer.
		if last := a.last.Load(); last != nil && !last.deleted {
			views[i] = last
		}
	}
	if r.inline {
		for i, a := range actors {
			ask(i, a)
		}
	} else {
		var wg sync.WaitGroup
		for i, a := range actors {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ask(i, a)
			}()
		}
		wg.Wait()
	}
	cancel()
	if err := ctx.Err(); err != nil {
		return wire.ConvsSnapshot{}, err
	}
	for _, v := range views {
		if v != nil {
			out = append(out, v.rec)
		}
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
	return liveOn(r, ctx, r.actor(id), f)
}

// liveOn is live on an actor already in hand (see callOn).
func liveOn[T any](r *Runtime, ctx context.Context, a *actor, f func(c *conv, now time.Time) (T, error)) (T, error) {
	res := make(chan result[T], 1)
	err := r.callOn(ctx, a, func(c *conv, now time.Time) {
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

// TurnsWithSteps is Turns with the steps of every turn of the page (wire.StepsPager).
func (r *Runtime) TurnsWithSteps(ctx context.Context, id, beforeTurnID string, limit int) (wire.TurnsPage, error) {
	if limit < 1 {
		limit = wire.TurnsPageDefault
	}
	limit = min(limit, wire.TurnsPageMax)
	return live(r, ctx, id, func(c *conv, now time.Time) (wire.TurnsPage, error) {
		p, ok := c.fd.TurnsWithSteps(beforeTurnID, limit)
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

// sendLost: a send was answered «I have it», and the conversation knows nothing of that message now — not waiting, not
// ended, not let go: its record was lost after the answer was kept (the runtime stopped between the two writes). The
// same send is then done again instead of being answered from memory, so the person's Повторить with the same clientId
// brings the message back; a message the conversation knows is never queued twice.
func (r *Runtime) sendLost(ctx context.Context, cmd wire.Command, res wire.CmdResult) bool {
	m, ok := cmd.(wire.SendCmd)
	if !ok || res.R == wire.ResultRefused {
		return false
	}
	st, err := r.MessageStatus(ctx, m.ConversationID, []string{m.ClientID})
	return err == nil && len(st.Messages) == 1 && st.Messages[0].K == wire.StatusUnknown
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

// Delivered is the sha256 of the file at this path as the agent handed it over in this conversation (deliver_file).
func (r *Runtime) Delivered(ctx context.Context, id, path string) (string, bool, error) {
	type got struct {
		sum string
		ok  bool
	}
	g, err := live(r, ctx, id, func(c *conv, now time.Time) (got, error) {
		sum, ok := c.fd.Delivered(path)
		return got{sum, ok}, nil
	})
	return g.sum, g.ok, err
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
			if r.sendLost(ctx, cmd, op.Result) {
				r.opsMu.Lock()
				if cur, ok := r.ops[key]; ok && cur.At == op.At {
					delete(r.ops, key)
					r.opsDirty = true
				}
				r.opsMu.Unlock()
				continue
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
		safego.Go("convo command", func() { run() })
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
	case wire.ConversationSummarizeCmd:
		return r.on(a, func(c *conv, now time.Time) { finish(c.summarize(m.TurnID, now)) })
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
	case wire.ConversationTestCmd:
		return m.ID
	case wire.ConversationAppsCmd:
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
	case wire.ConversationSummarizeCmd:
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
	mark := func(c *conv, now time.Time) {
		if c.deletedAt != 0 {
			return
		}
		got := c.seen(m.UpTo, now)
		if res.Seq == 0 || got.Seq > 0 && got.Seq < res.Seq {
			res.Seq = got.Seq
		}
	}
	// Marking everything read is no use of each conversation: the actors are asked as a list asks them.
	r.mu.Lock()
	actors := make([]*actor, 0, len(r.actors))
	for _, a := range r.actors {
		actors = append(actors, a)
	}
	var unread []string
	for id, v := range r.parked {
		// A parked conversation is woken only when it has something unread to mark.
		if t := v.rec.LastTurn; !v.deleted && t != nil && t.EndedAt != nil && *t.EndedAt > v.rec.SeenAt && m.UpTo > v.rec.SeenAt {
			unread = append(unread, id)
		}
	}
	r.mu.Unlock()
	for _, a := range actors {
		// A conversation removed since has nothing to mark seen.
		_ = r.callOn(context.Background(), a, mark)
	}
	for _, id := range unread {
		_ = r.call(context.Background(), id, mark)
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

// Project returns local project metadata without entering a conversation actor.
func (r *Runtime) Project(id string) (wire.Project, bool) { return r.project(id) }

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
		ParentID: parentID, Test: m.Test, Rev: 1}
	if r.d.AlwaysApps != nil {
		if ids := r.d.AlwaysApps(); len(ids) > 0 && (r.d.AppsOK == nil || r.d.AppsOK(ids) == "") {
			rec.Apps = append([]string(nil), ids...)
		}
	}
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
// be ones the profile lists (ultracode stays Claude's own). Without a profile the model must not be a vendor's only (a
// profile of this CLI owns it and the CLI's own account does not list it): it would run on the wrong account.
func (r *Runtime) onProfile(p wire.Provider, model *wire.Model, profile string) (*wire.Model, wire.RefuseCode) {
	if model != nil && model.Permission != "" && !model.Permission.Known() {
		return nil, wire.RefuseBadRequest
	}
	if profile == "" {
		if model != nil && r.d.vendorOnly(p, model.ID) {
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
	if !slices.Contains(models, model.ID) || !known || model.Ultracode || model.Fast && !capabilities.Fast ||
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
	// The chat's apps ride with its first launch as with every relaunch (conv.go): a chat that lists an app always runs
	// its agent with it.
	req.Apps = rec.Apps
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
				AttachmentIDs: first.AttachmentIDs, Refs: first.Refs, Mode: wire.ModeQueue, Kind: wire.KindPrompt}, now)
			// The first message goes with the launch, as the agent's first prompt: the starting terminal takes no keys
			// for a while and drops what is typed into it, a prompt of the command line it does not.
			// A first message with places carries nothing: their block is under its words, which the launch line lacks.
			ch, carried := outbox.Change{}, false
			if len(first.Refs) == 0 {
				ch, carried = c.box.Carry(first.ClientID, now)
			}
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
	for _, v := range r.views() {
		if !v.deleted && v.rec.TranscriptPath != "" {
			out = append(out, v.rec.TranscriptPath)
		}
	}
	return out
}

// AttachedSessions are the agent sessions the conversations hold, by the agent's own session id.
func (r *Runtime) AttachedSessions() map[string]string {
	out := map[string]string{}
	for _, v := range r.views() {
		if v.deleted {
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
