package convo

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A runtime held an actor — a goroutine, its timers, the conversation's state — for every conversation it ever saw:
// on a machine of 150 chats, 137 of them stopped, most of what it held was of chats nobody had touched for days. A
// conversation at rest that nobody asked for in UnloadAfter is parked: its file is written, its actor ends, and the
// runtime keeps only the record it left, which is what the lists show. The first page, hook, message or command that
// names the conversation gives it an actor again from its file, the way a start of the runtime does for a stopped
// conversation (rest.go) — without the look a start needs, because the runtime never stopped watching.

// UnloadAfter is how long a conversation at rest goes unasked for before its actor is let go.
var UnloadAfter = 6 * time.Hour

// notAtRest is why the conversation's actor cannot be let go now, "" when it can: everything it holds is in its file,
// and nothing it waits for is due.
func (c *conv) notAtRest(now time.Time) string {
	switch c.rec.State.K {
	case wire.StateStopped:
	case wire.StateGone:
		if c.lostTmux() {
			return "terminal may come back"
		}
	default:
		return "agent " + string(c.rec.State.K)
	}
	switch {
	case c.jobs > 0:
		return "work under way"
	case c.saveFails > 0 || c.rec.NotSavedSince != 0:
		return "not saved"
	case c.recDirty || c.stateDirty:
		return "record not sent"
	case c.rec.Queue.Count > 0 || !c.box.Wake().IsZero():
		return "queue"
	case c.typing || c.switching || c.busyGate || c.interrupting || c.stoppingTerm || c.rewinding || c.titling || c.pumping:
		return "typing"
	case len(c.gates) > 0 || c.rec.Gate != nil:
		return "gate"
	case c.rec.PendingModel != nil || c.rec.State.Wait != nil:
		return "waiting"
	case !c.limitWake().IsZero() || !c.restartWake().IsZero() || !c.throttle.Wake().IsZero() || c.restartCut:
		return "timer"
	case c.deletedAt == 0 && !c.titleDueAt.IsZero():
		return "title due"
	case c.rec.BgAgents > 0 || c.bgWake > ms(now):
		return "background"
	case c.fd != nil && !c.fd.Released():
		return "feed read"
	}
	for _, t := range c.bg {
		if t.Status == wire.BgStatusRunning {
			return "background"
		}
	}
	return ""
}

// forget lets go of what the readers beside the conversation keep for it: the cursors in its transcript, and — when
// nobody looks at its session any more — what the looks kept of the session. Each comes back by itself if the
// conversation is read or looked at again.
func (c *conv) forget(session bool) {
	if p := c.rec.TranscriptPath; p != "" {
		if c.d.Meta != nil {
			c.d.Meta.Forget(p)
		}
		if c.d.Shots != nil {
			c.d.Shots.Forget(p)
		}
		if f, ok := c.d.Background.(TranscriptForgetter); ok {
			f.Forget(c.rec)
		}
		if c.fd != nil {
			c.fd.Close()
		}
	}
	if !session {
		return
	}
	if f, ok := c.d.Machine.(SessionForgetter); ok && c.rec.Session != "" {
		f.ForgetSession(c.rec.Session)
	}
	if f, ok := c.d.Publisher.(ChannelForgetter); ok && c.deletedAt != 0 {
		f.Forget(wire.ConvChannel(c.rec.ID))
	}
}

// Unload is one sweep: it parks the conversations at rest nobody asked for in UnloadAfter and returns their ids.
func (r *Runtime) Unload() []string {
	if UnloadAfter <= 0 || r.d.Store == nil {
		return nil // without a store there is no file to come back from
	}
	var out []string
	for _, a := range r.all() {
		v := a.last.Load()
		now := r.d.Clock.Now()
		if v == nil || now.Sub(time.Unix(0, a.used.Load())) < UnloadAfter {
			continue
		}
		if k := v.rec.State.K; k != wire.StateStopped && k != wire.StateGone {
			continue
		}
		if t := r.d.Terminals; t != nil && t.Watched(v.rec.ID, v.rec.Session) {
			// A page that holds the chat open asks nothing while nothing happens, and is still someone using it.
			a.used.Store(now.UnixNano())
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), reapAsk)
		parked := make(chan bool, 1)
		err := r.callOn(ctx, a, func(c *conv, now time.Time) { parked <- r.park(a, now) })
		cancel()
		if err == nil && <-parked {
			out = append(out, v.rec.ID)
		}
	}
	return out
}

// park ends the actor's hold of its conversation, on the actor's own turn: the file is written, the record stays
// with the runtime, the loop ends after this turn. false, and nothing changed, when the conversation is not at rest,
// was asked for since the sweep looked, or the runtime stops.
func (r *Runtime) park(a *actor, now time.Time) bool {
	c := a.c
	if c.notAtRest(now) != "" {
		return false
	}
	// The file takes the conversation as it stands now, with what it knows of its transcript: that is what it comes
	// back from. The writer holds the state until it is on the disk, and a conversation asked for meanwhile reads it
	// from there.
	c.dirty = true
	c.save(now, true)
	if c.dirty {
		return false
	}
	a.publish()
	id := c.rec.ID
	if !r.inline {
		// Inline, the caller holds the lock around the actor's turn.
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	// Whoever asks for a conversation by its id marks the use under this lock before sending anything: a use since
	// the sweep looked keeps the actor, and one after this turn finds the conversation parked and wakes it.
	if r.actors[id] != a || now.Sub(time.Unix(0, a.used.Load())) < UnloadAfter || len(a.mail) > 0 || r.stopping() {
		return false
	}
	delete(r.actors, id)
	r.parked[id] = a.last.Load()
	a.gone = true
	if !r.inline {
		close(a.stop)
	}
	c.forget(true)
	r.d.Log.Info("conversation parked: its actor is let go until it is asked for", "conv", id, "state", string(c.rec.State.K))
	return true
}

// revive gives a parked conversation its actor back from its file. nil when the file cannot be read: the
// conversation stays parked, and the next one who asks tries again.
func (r *Runtime) revive(id string) *actor {
	r.reviveMu.Lock()
	defer r.reviveMu.Unlock()
	r.mu.Lock()
	a := r.actors[id]
	_, parked := r.parked[id]
	r.mu.Unlock()
	if a != nil || !parked {
		return a // someone else woke it first, or it is not a conversation of the runtime
	}
	f, err := r.d.Store.loadConv(id)
	if err != nil || f.Conv.ID != id {
		r.d.Log.Warn("parked conversation not read back", "conv", id, "err", err)
		return nil
	}
	r.d.Log.Info("parked conversation asked for: it has an actor again", "conv", id)
	return r.wake(f, true)
}

// Loaded is how many conversations have an actor now, Parked how many are held by their record alone.
func (r *Runtime) Loaded() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.actors)
}

func (r *Runtime) Parked() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.parked)
}
