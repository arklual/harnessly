package convo

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/fork"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A rewind goes on from before one of the person's messages with other words: the agent's transcript up to that message
// becomes a new session of the agent (internal/fork), the chat's tab is bound to it, and the edited message goes into
// the queue, which resumes the agent on it. The transcript the rewind left stays as it was; the feed folds its turns
// where it was cut, and rewind.undo binds the chat to it again for a short while. With files, the chat's folder goes
// back to the snapshot taken at the end of the turn before the message (internal/checkpoints).

// Rewinder binds a chat's tab to another session of its agent and keeps the snapshots of the chat's folder.
type Rewinder interface {
	// Bind makes the chat's tab resume on the agent session sessionID, whose transcript is at transcript.
	Bind(c wire.Conversation, sessionID, transcript string) error
	// Snapshot is the newest snapshot of the chat's folder taken in [from, to]; false when there is none.
	Snapshot(c wire.Conversation, from, to wire.Ms) (Snapshot, bool)
	// Changes is what returning the folder to s changes: at most wire.RewindChangesMax of the files, and how many more.
	Changes(s Snapshot) ([]wire.RewindChange, int, error)
	// Restore returns the folder to s, after a snapshot of it as it is (which an undo returns to).
	Restore(s Snapshot) (before Snapshot, err error)
}

// Snapshot is a snapshot of a chat's folder: which repository holds it (the tab and the folder) and its id.
type Snapshot struct {
	Tab string  `json:"tab"`
	Cwd string  `json:"cwd"`
	ID  string  `json:"id"`
	At  wire.Ms `json:"at"`
}

// rewindRec is one rewind as the conversation keeps it: its mark, and what its undo needs.
type rewindRec struct {
	Mark wire.RewindMark `json:"mark"`
	// EntryID is the message the rewind went back to, in the transcript it left.
	EntryID        string `json:"entryId"`
	FromSession    string `json:"fromSession"`
	FromTranscript string `json:"fromTranscript"`
	ToSession      string `json:"toSession"`
	// ClientID is the message the rewind put in the queue.
	ClientID string `json:"clientId"`
	// Before is the folder as it was before the rewind returned it; nil when the rewind left the folder alone.
	Before *Snapshot `json:"before,omitempty"`
}

// rewindable: why the conversation cannot be rewound now ("" when it can).
func (c *conv) rewindable() wire.RefuseCode {
	switch {
	case c.d.Rewinder == nil || c.d.Terminals == nil || c.rec.TranscriptPath == "":
		return wire.RefuseUnsupported
	case c.rewinding || c.stoppingTerm || c.switching || c.typing || c.busyGate:
		return wire.RefuseAgentWorking
	case c.box.Summary().Count > 0:
		// What waits in the queue was written to the conversation as it is; it would go to the other branch.
		return wire.RefuseAgentWorking
	}
	switch c.rec.State.K {
	case wire.StateWorking, wire.StateWaitingQuestion, wire.StateWaitingPermission, wire.StateStarting:
		return wire.RefuseAgentWorking
	}
	return ""
}

// plan is what a rewind to entryID would do. The snapshot's changes are read off the actor.
func (c *conv) plan(entryID string, finish func(wire.RewindPlan, error)) {
	if code := c.rewindable(); code == wire.RefuseUnsupported {
		finish(wire.RewindPlan{}, ErrNotFound)
		return
	}
	p, ok := c.fd.RewindPoint(entryID)
	if !ok {
		finish(wire.RewindPlan{}, ErrNotFound)
		return
	}
	plan := wire.RewindPlan{EntryID: entryID, Turns: p.Turns}
	if p.AfterEnd == nil {
		finish(plan, nil)
		return
	}
	rw, rec, from := c.d.Rewinder, c.rec, *p.AfterEnd
	c.spawn(func() any {
		s, ok := rw.Snapshot(rec, from, p.At)
		if !ok {
			return plan
		}
		changes, more, err := rw.Changes(s)
		if err != nil {
			return err
		}
		plan.Files = &wire.RewindFiles{At: s.At, Changes: changes, More: more}
		return plan
	}, func(r any) {
		if err, failed := r.(error); failed {
			finish(wire.RewindPlan{}, err)
			return
		}
		finish(r.(wire.RewindPlan), nil)
	})
}

type rewound struct {
	code    wire.RefuseCode
	stopped bool
	forked  fork.Session
	before  *Snapshot
}

// rewind goes on from before the message m.EntryID with m.Text; finish gets the answer of the queued message.
func (c *conv) rewind(m wire.RewindCmd, finish func(wire.CmdResult), now time.Time) {
	if code := c.rewindable(); code != "" {
		finish(wire.Refused(code))
		return
	}
	p, ok := c.fd.RewindPoint(m.EntryID)
	if !ok {
		finish(wire.Refused(wire.RefuseNotFound))
		return
	}
	if m.Files && p.AfterEnd == nil {
		finish(wire.Refused(wire.RefuseNotFound))
		return
	}
	rw, terms, rec := c.d.Rewinder, c.d.Terminals, c.rec
	sessionID := rec.AgentSessionID
	if sessionID == "" {
		sessionID = fork.SessionID(rec.TranscriptPath)
	}
	stop := rec.Session != "" && rec.TerminalStop == nil && agentUp(rec.State.K)
	c.rewinding = true
	c.spawn(func() any {
		var snap Snapshot
		if m.Files {
			s, ok := rw.Snapshot(rec, *p.AfterEnd, p.At)
			if !ok {
				return rewound{code: wire.RefuseNotFound}
			}
			snap = s
		}
		cut, err := fork.Cut(string(rec.Provider), rec.TranscriptPath, m.EntryID)
		switch {
		case errors.Is(err, fork.ErrNothingKept):
			return rewound{code: wire.RefuseUnsupported}
		case errors.Is(err, fork.ErrNotFound):
			return rewound{code: wire.RefuseNotFound}
		case err != nil:
			c.d.Log.Warn("rewind: transcript not read", "conv", rec.ID, "err", err)
			return rewound{code: wire.RefuseInternal}
		}
		s, err := fork.Write(string(rec.Provider), rec.TranscriptPath, sessionID, cut, time.Now())
		if err != nil {
			c.d.Log.Warn("rewind: session not written", "conv", rec.ID, "err", err)
			return rewound{code: wire.RefuseInternal}
		}
		var out rewound
		if stop {
			if !terms.Owned(rec.Session) {
				return rewound{code: wire.RefuseUnsupported}
			}
			unlock := typist.Lock(rec.Session)
			_, err := terms.Stop(rec.Session)
			unlock()
			if err != nil {
				c.d.Log.Warn("rewind: terminal not stopped", "conv", rec.ID, "err", err)
				return rewound{code: wire.RefuseInternal}
			}
			out.stopped = true
		}
		if m.Files {
			before, err := rw.Restore(snap)
			if err != nil {
				c.d.Log.Warn("rewind: folder not returned", "conv", rec.ID, "err", err)
				out.code = wire.RefuseInternal
				return out
			}
			out.before = &before
		}
		if err := rw.Bind(rec, s.ID, s.Path); err != nil {
			c.d.Log.Warn("rewind: tab not bound", "conv", rec.ID, "err", err)
			if out.before != nil {
				_, _ = rw.Restore(*out.before)
			}
			out.code = wire.RefuseInternal
			return out
		}
		out.forked = s
		return out
	}, func(r any) {
		c.rewinding = false
		res := r.(rewound)
		now := c.d.Clock.Now()
		c.mark, c.markConvs = 0, 0
		if res.stopped {
			c.lookSeq++
			c.looking, c.lookAgain = false, false
			c.input(agentstate.Input{K: agentstate.Shell, Resumable: true}, now)
		}
		if res.code != "" {
			c.flush(now, true)
			finish(wire.Refused(res.code))
			return
		}
		mark := wire.RewindMark{At: ms(now), AfterTurnID: p.After, Turns: p.Turns, Files: m.Files, UndoUntil: ms(now) + wire.RewindUndoMs}
		c.rewinds = append(c.rewinds, rewindRec{Mark: mark, EntryID: m.EntryID, FromSession: sessionID,
			FromTranscript: rec.TranscriptPath, ToSession: res.forked.ID, ClientID: m.OpID, Before: res.before})
		if len(c.rewinds) > wire.RewindsMax {
			c.rewinds = slices.Clone(c.rewinds[len(c.rewinds)-wire.RewindsMax:])
		}
		c.left = appendNew(c.left, sessionID)
		c.switchTranscript(res.forked.Path, res.forked.ID, now)
		c.d.Log.Info("rewound", "conv", c.rec.ID, "turns", p.Turns, "files", m.Files, "session", res.forked.ID)
		finish(c.send(wire.SendCmd{OpID: m.OpID, ClientID: m.OpID, ConversationID: c.rec.ID, Text: m.Text,
			AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt}, now))
	})
}

// rewindUndo binds the conversation again to the session its last rewind left, while that rewind can be undone.
func (c *conv) rewindUndo(finish func(wire.CmdResult), now time.Time) {
	if len(c.rewinds) == 0 || c.d.Rewinder == nil || c.d.Terminals == nil {
		finish(wire.Refused(wire.RefuseNotFound))
		return
	}
	last := c.rewinds[len(c.rewinds)-1]
	if ms(now) > last.Mark.UndoUntil {
		finish(wire.Refused(wire.RefuseTooLate))
		return
	}
	if c.rewinding || c.stoppingTerm {
		finish(wire.Refused(wire.RefuseAgentWorking))
		return
	}
	rw, terms, rec := c.d.Rewinder, c.d.Terminals, c.rec
	// The rewind's own message may already be at work: the undo ends it with the terminal.
	stop := rec.Session != "" && rec.TerminalStop == nil && (agentUp(rec.State.K) || rec.State.K == wire.StateStarting)
	c.rewinding = true
	c.spawn(func() any {
		var out rewound
		if stop && terms.Owned(rec.Session) {
			unlock := typist.Lock(rec.Session)
			_, err := terms.Stop(rec.Session)
			unlock()
			if err != nil {
				c.d.Log.Warn("rewind undo: terminal not stopped", "conv", rec.ID, "err", err)
				return rewound{code: wire.RefuseInternal}
			}
			out.stopped = true
		}
		if last.Before != nil {
			if _, err := rw.Restore(*last.Before); err != nil {
				c.d.Log.Warn("rewind undo: folder not returned", "conv", rec.ID, "err", err)
				out.code = wire.RefuseInternal
				return out
			}
		}
		if err := rw.Bind(rec, last.FromSession, last.FromTranscript); err != nil {
			c.d.Log.Warn("rewind undo: tab not bound", "conv", rec.ID, "err", err)
			out.code = wire.RefuseInternal
		}
		return out
	}, func(r any) {
		c.rewinding = false
		res := r.(rewound)
		now := c.d.Clock.Now()
		c.mark, c.markConvs = 0, 0
		if res.stopped {
			c.lookSeq++
			c.looking, c.lookAgain = false, false
			c.input(agentstate.Input{K: agentstate.Shell, Resumable: true}, now)
		}
		if res.code != "" {
			c.flush(now, true)
			finish(wire.Refused(res.code))
			return
		}
		if ch, code := c.box.Cancel(last.ClientID, now); code == "" {
			c.change(ch, now)
		}
		c.rewinds = c.rewinds[:len(c.rewinds)-1]
		c.left = appendNew(slices.DeleteFunc(c.left, func(s string) bool { return s == last.FromSession }), last.ToSession)
		c.switchTranscript(last.FromTranscript, last.FromSession, now)
		c.d.Log.Info("rewind undone", "conv", c.rec.ID, "session", last.FromSession)
		finish(wire.Accepted(wire.ChannelConvs, c.convsSeq()))
	})
}

// switchTranscript puts the conversation on another transcript of its agent: the feed is read anew from it and the
// pages that show it take it whole (feed_reset), the record says what rewinds there were.
func (c *conv) switchTranscript(path, sessionID string, now time.Time) {
	c.rec.TranscriptPath, c.rec.AgentSessionID = path, sessionID
	c.rec.Rewinds = c.marks()
	c.rec.TerminalStop = nil
	c.openFeed()
	c.fd.Load()
	for _, m := range c.box.Messages(now) {
		if m.State.EntryID != nil {
			c.fd.SetClientID(*m.State.EntryID, m.ClientID)
		}
	}
	ts, es, before := c.fd.Snapshot(snapshotTurns)
	fit := wire.FitConv(wire.ConvSnapshot{Turns: ts, Entries: es, BeforeTurnID: before})
	c.emit(wire.FeedResetEv{Turns: fit.Turns, Entries: fit.Entries, BeforeTurnID: fit.BeforeTurnID})
	c.lastTurn()
	c.touch()
	c.flush(now, true)
	c.save(now, true)
}

func (c *conv) marks() []wire.RewindMark {
	if len(c.rewinds) == 0 {
		return nil
	}
	out := make([]wire.RewindMark, len(c.rewinds))
	for i, r := range c.rewinds {
		out[i] = r.Mark
	}
	return out
}

func appendNew(list []string, s string) []string {
	if s == "" || slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// --- the runtime's side ---

// Plan is what a rewind to the person's message entryID would do (wire.RewindSource).
func (r *Runtime) Plan(ctx context.Context, id, entryID string) (wire.RewindPlan, error) {
	a := r.actor(id)
	if a == nil {
		return wire.RewindPlan{}, ErrNotFound
	}
	type answer struct {
		p   wire.RewindPlan
		err error
	}
	ch := make(chan answer, 1)
	if err := r.on(a, func(c *conv, now time.Time) {
		if c.deletedAt != 0 {
			ch <- answer{err: ErrNotFound}
			return
		}
		c.plan(entryID, func(p wire.RewindPlan, err error) { ch <- answer{p, err} })
	}); err != nil {
		return wire.RewindPlan{}, err
	}
	select {
	case got := <-ch:
		return got.p, got.err
	case <-ctx.Done():
		return wire.RewindPlan{}, ctx.Err()
	}
}

// rewoundTurns is how many of the turns a rewind left behind its page shows, the newest of them.
const rewoundTurns = 33

// Rewound is the turns the conversation's rewind n left behind, by their faces (wire.RewindSource).
func (r *Runtime) Rewound(ctx context.Context, id string, n int) (wire.TurnsPage, error) {
	var rec rewindRec
	var provider wire.Provider
	var src func(wire.Conversation) feed.Source
	found := false
	if err := r.call(ctx, id, func(c *conv, now time.Time) {
		if c.deletedAt == 0 && n >= 0 && n < len(c.rewinds) {
			rec, provider, src, found = c.rewinds[n], c.rec.Provider, c.d.Source, true
		}
	}); err != nil {
		return wire.TurnsPage{}, err
	}
	if !found {
		return wire.TurnsPage{}, ErrNotFound
	}
	old := wire.Conversation{ID: id, Provider: provider, TranscriptPath: rec.FromTranscript}
	var s feed.Source = &feed.ClaudeSource{Path: rec.FromTranscript}
	switch {
	case src != nil:
		s = src(old)
	case provider == wire.ProviderCodex:
		s = &feed.CodexSource{Path: rec.FromTranscript}
	}
	f := feed.New(id, provider, s)
	f.Load()
	page, ok := f.From(rec.EntryID, rewoundTurns)
	if !ok {
		return wire.TurnsPage{}, ErrNotFound
	}
	return page, nil
}
