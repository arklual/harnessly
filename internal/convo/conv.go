package convo

import (
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/chatguard"
	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/terminalfailure"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	// LookEvery is how often an actor looks at its pane and reads its transcript when nothing told it to.
	LookEvery = time.Second
	// BootSettle is how long a freshly started agent's screen must stand unchanged, its input box drawn, before it
	// counts as ready for the first message.
	BootSettle = 1500 * time.Millisecond
	// QuietAfter is how long a working or unknown agent must look quiet — not busy, its prompt box up, no menu, the
	// transcript not growing — before the actor takes it as done.
	QuietAfter = 15 * time.Second
	// QuietUnmarked is how long a Claude turn the transcript keeps open must stay quiet to count as done when the screen
	// does not say it ended: a reply the model is slow to start writes nothing (96 s seen) and its spinner may be
	// unread, so silence alone is a turn at work. Past it silence counts again: a lost Stop must not keep a turn open
	// forever.
	QuietUnmarked = 10 * time.Minute
	// LaunchNoAgent is how long a starting agent's pane may show a bare shell before the launch counts as failed: the
	// launch line is typed into a shell, so a shell is expected for a moment, but one that stays means the agent
	// exited or never ran (a broken CLI). Without it the launch waited out its whole deadline, and a «Continue
	// sending» pressed meanwhile found the conversation still starting and launched nothing.
	LaunchNoAgent = 15 * time.Second
	// CompactHold is how long a compaction (PreCompact, and neither PostCompact nor the transcript's compaction line
	// yet) keeps a working agent from counting as quiet. Claude writes nothing to the transcript while it compacts
	// (45–95 s seen), and its screen may not read busy. Past the hold silence counts again: a lost PostCompact must not
	// keep a turn open forever.
	CompactHold = 10 * time.Minute
	// StateCoalesce: conversation_state and conversation_upsert of one conversation go at most this often.
	StateCoalesce = 100 * time.Millisecond
	// SaveEvery: a conversation is written at most five times a second.
	SaveEvery = 200 * time.Millisecond
	// GateKept is how long a gate stays in the snapshot after it closed or was answered.
	GateKept = 10 * time.Minute
	// DeletedKept is how long a deleted conversation can be restored.
	DeletedKept = 7 * 24 * time.Hour
	// snapshotTurns: a conv snapshot holds the last three turns whole and the answers of 30 before them.
	snapshotTurns = 33
)

// lookEvery is how often a conversation is looked at by the clock alone, by what it is doing: an agent at work or
// waiting is watched closely, one at rest is told about by its hooks and looked at now and then, one gone only to see
// whether its session came back. Every look is a few tmux calls, and a runtime holds many conversations.
func (c *conv) lookEvery() time.Duration {
	switch c.rec.State.K {
	case wire.StateIdle, wire.StateReady:
		return 3 * LookEvery
	case wire.StateStopped, wire.StateGone, wire.StateLaunchFailed:
		return 10 * LookEvery
	}
	return LookEvery
}

// conv is one conversation's actor state. Only its actor touches it: every method runs on the actor's goroutine (or
// on the test's, which plays the actor).
type conv struct {
	lastWroteAt wire.Ms // the newest real message of the person the feed showed; 0 until one (see personWroteAt)
	d           *Deps
	rec         wire.Conversation
	box         *outbox.Box
	fd          *feed.Feed
	// handoffArtifact is private context used by the first prompt. It stays through launch and delivery retries, and is
	// released only when that prompt's turn is terminal. Both fields are persisted for the same behavior after restart.
	handoffArtifact, handoffArtifactClientID string
	// indexSeen is the size of the transcript the search index followed the feed to (see indexUpdate).
	indexSeen int64

	gates map[string]*gate
	order []string

	throttle feed.NowThrottle
	nowVal   *wire.Now
	look     *typist.Screen
	// bootPlain and bootAt: the screen of a starting agent and since when it has stood unchanged (see BootSettle).
	bootPlain string
	bootAt    time.Time
	bootFirst time.Time
	probe     Probe

	quietSince time.Time
	quietSize  int64
	// quietTurn: the turn ended on silence, not by the transcript; the agent seen at work again opens it again.
	quietTurn string
	// compactAt: when the agent's state took compacting (see CompactHold).
	compactAt time.Time
	// interrupting: we pressed Escape for a Stop that pauses the queue; the interrupt it brings pauses it.
	interrupting bool
	interruptAt  time.Time

	typing bool
	// switching: a /model or /effort is being typed into the agent's box (off the actor); nothing else types meanwhile.
	// pendingTries counts the times the box was not ready for the waiting model change, pendingAt is when it is tried
	// again.
	switching    bool
	pendingTries int
	pendingAt    time.Time
	// handable: the agent's running tool is one Claude can hand to the background, as the actor saw it last; the
	// typist reads it off the actor right before a chord.
	handable atomic.Bool
	// looking: a look at the process and the pane is under way off the actor (lookSeq tells it from a stale one);
	// lookAgain: someone asked for another meanwhile. pumping/pumpAgain are the same for the look before a try.
	looking, lookAgain bool
	lookSeq            int
	// stateGen counts the changes of the agent's state: a look that began before the last one saw the screen of an
	// older state (a "busy" screen from before the Stop hook) and must not move the state back.
	stateGen           int
	lookAt             time.Time
	pumping, pumpAgain bool
	// busyGate: keys of a dialog are on their way; nothing else types meanwhile.
	busyGate bool
	// stoppingTerm: the terminal is being stopped off the actor (reap.go).
	stoppingTerm bool
	// rewinding: a rewind or its undo is under way off the actor (rewind.go); rewinds are the rewinds kept, left the
	// agent sessions the conversation left behind by them (no conversation of their own).
	rewinding bool
	rewinds   []rewindRec
	left      []string
	// restartCut: the agent was at work when the runtime went down and was not running when it came back (limit.go);
	// restartAt is when the runtime came back.
	restartCut, restartSent bool
	restartAt               time.Time
	// limitLine is the line of a usage limit on Codex's screen last taken for a hold: the same line seen again is old.
	limitLine string
	// terminalFailureTurn is the turn whose terminal provider failure already ended and interrupted it: the transcript
	// and the PTY may report the same failure, and the agent may write it more than once while its interrupt arrives.
	terminalFailureTurn string
	terminalFailures    *terminalfailure.Detector

	// bg are the agent's background tasks as the page has them, bgStopped those we ended and the agent still lists,
	// bgReading a read of them off the actor, bgAt when the last one began.
	bg        []wire.BackgroundTask
	bgStopped map[string]bool
	bgReading bool
	bgAt      time.Time
	// bgWake is when the agent's scheduled wakeup comes, as the last read saw it; 0 when none is pending.
	bgWake wire.Ms
	// bgOpened: a person asked for this chat's snapshot, so its ended subagents are worth reading even when its agent is
	// not running; bgDeadRead: they were, once, since the agent stopped.
	bgOpened, bgDeadRead bool

	// metaReading is a read of the transcript's word on the effort and the goal off the actor, metaAt when the last
	// began; launchUltracode: the tab was started on ultracode (a Legacy tab's launch), which the transcript never says.
	metaReading     bool
	metaAt          time.Time
	launchUltracode bool
	// shots are the pictures the agent looked at, newest first, and shotPaths the files they are, by id.
	shots     []wire.Shot
	shotPaths map[string]string
	// pics is what the messages name and may be shown (pictures.go).
	pics pictureState

	// titleDueAt is when a conversation still named by its first message gets a title made for it (the agent had until
	// then to write its own); titling is that work off the actor, titleTried that it was done once.
	titleDueAt time.Time
	titling    bool
	titleTried bool

	deletedAt wire.Ms

	// recDirty/stateDirty: the record changed since the last conversation_upsert/conversation_state.
	recDirty, stateDirty bool
	flushedAt            time.Time
	dirty                bool
	savedAt              time.Time

	// mark is the first seq published on the conv channel during a command, markConvs on convs.
	mark, markConvs int64
	seq, seqConvs   int64

	// spawn runs work off the actor and gives its result back on it.
	spawn func(work func() any, done func(any))
}

// gate is an open or recent question or permission.
type gate struct {
	q        *wire.Question
	p        *wire.Permission
	closedAt time.Time
}

func (g *gate) id() string {
	if g.q != nil {
		return g.q.ID
	}
	return g.p.ID
}

func (g *gate) state() *wire.GateState {
	if g.q != nil {
		return &g.q.State
	}
	return &g.p.State
}

func ms(t time.Time) wire.Ms { return t.UnixMilli() }

func newConv(d *Deps, rec wire.Conversation) *conv {
	c := &conv{d: d, rec: rec, gates: map[string]*gate{}}
	c.box = outbox.New(rec.ID, d.Limits, d.Log)
	c.box.Transcript = func(offset int64, at wire.Ms, text string) (string, bool) { return c.fd.FindUserText(offset, at, text) }
	c.openFeed()
	return c
}

func (c *conv) openFeed() {
	var src feed.Source
	switch {
	case c.d.Source != nil:
		src = c.d.Source(c.rec)
	case c.rec.Provider == wire.ProviderCodex:
		src = &feed.CodexSource{Path: c.rec.TranscriptPath}
	default:
		src = &feed.ClaudeSource{Path: c.rec.TranscriptPath}
	}
	c.fd = feed.New(c.rec.ID, c.rec.Provider, src)
	if c.onProfile() {
		c.fd.OnVendor()
	}
}

// onProfile: the conversation runs on a backend profile (a vendor's API behind the agent CLI), not on the CLI's own plan.
func (c *conv) onProfile() bool { return c.rec.Model != nil && c.rec.Model.Profile != "" }

// --- publishing ---

func (c *conv) emit(events ...wire.Event) {
	for _, e := range events {
		seq := c.d.Publisher.Publish(wire.ConvChannel(c.rec.ID), e)
		c.seq = seq
		if c.mark == 0 {
			c.mark = seq
		}
	}
}

func (c *conv) emitConvs(e wire.Event) int64 {
	seq := c.d.Publisher.Publish(wire.ChannelConvs, e)
	c.seqConvs = seq
	if c.markConvs == 0 {
		c.markConvs = seq
	}
	return seq
}

// change publishes what the box did and keeps the record's queue summary.
func (c *conv) change(ch outbox.Change, now time.Time) {
	c.emit(ch.Events...)
	// A message the outbox found in the transcript by itself (a stall that looked first, a retry) marks its entry too:
	// the feed draws the message once, and the stack over the field lets it go by the entry as well.
	for _, e := range ch.Events {
		var st wire.MessageState
		var client string
		switch ev := e.(type) {
		case wire.MessageUpsertEv:
			st, client = ev.Message.State, ev.Message.ClientID
		case wire.PromptStateEv:
			st, client = ev.State, ev.ClientID
		default:
			continue
		}
		c.releaseHandoffArtifact(client, st)
		if st.EntryID == nil {
			continue
		}
		if entry, changed := c.fd.SetClientID(*st.EntryID, client); changed {
			c.emit(wire.EntryUpdateEv{Entry: entry})
		}
	}
	if len(ch.Events) > 0 {
		c.dirty = true
	}
	if s := c.box.Summary(); !reflect.DeepEqual(s, c.rec.Queue) {
		c.rec.Queue = s
		c.touch()
	}
	if ch.Resume {
		c.resume(now)
	}
}

func (c *conv) releaseHandoffArtifact(client string, st wire.MessageState) {
	if c.handoffArtifact == "" || client != c.handoffArtifactClientID || c.d.Handoffs == nil {
		return
	}
	terminal := st.K == wire.MsgDone || st.K == wire.MsgCancelled || st.K == wire.MsgFailed && !st.Retryable
	if !terminal {
		return
	}
	if err := c.d.Handoffs.Release(c.rec.ID, c.handoffArtifact); err != nil {
		c.d.Log.Warn("handoff artifact not released", "conv", c.rec.ID, "err", err)
		return
	}
	c.handoffArtifact, c.handoffArtifactClientID = "", ""
	c.dirty = true
}

// touch marks the record changed: a conversation_upsert goes within StateCoalesce.
func (c *conv) touch() {
	c.rec.Rev++
	c.recDirty, c.dirty = true, true
}

// flush lets out the coalesced record change; force skips the wait (a command answers with its seq).
func (c *conv) flush(now time.Time, force bool) {
	if !c.recDirty && !c.stateDirty {
		return
	}
	if !force && now.Sub(c.flushedAt) < StateCoalesce {
		return
	}
	if c.deletedAt != 0 {
		c.recDirty, c.stateDirty = false, false
		return
	}
	if c.recDirty {
		c.emitConvs(wire.ConversationUpsertEv{Conv: c.rec})
	} else {
		c.emitConvs(wire.ConversationStateEv{ID: c.rec.ID, State: c.rec.State, Rev: c.rec.Rev})
	}
	c.recDirty, c.stateDirty, c.flushedAt = false, false, now
}

// --- the automaton ---

func (c *conv) input(in agentstate.Input, now time.Time) {
	if in.At == 0 {
		in.At = ms(now)
	}
	before := c.rec.State
	st, effects := agentstate.Next(before, in)
	if !reflect.DeepEqual(st, before) {
		c.rec.State = st
		c.rec.Rev++
		c.stateGen++
		c.stateDirty, c.dirty = true, true
		if st.K != wire.StateWorking || before.K != wire.StateWorking || st.Compacting != before.Compacting {
			c.quietSince = time.Time{}
		}
		if st.Compacting && !before.Compacting {
			c.compactAt = now
		}
		if c.rec.TerminalStop != nil && agentUp(st.K) {
			// The agent runs again: the stop is history.
			c.rec.TerminalStop = nil
			c.touch()
		}
	}
	c.effects(effects, before, now)
	if p := c.box.Paused(); p != nil && p.Reason == wire.PausedAgentGone && c.rec.State.K != wire.StateGone {
		// The pause was for an agent that is gone; one that is back (found again, a restart) takes its queue — or, held
		// on its usage limit, waits for the reset.
		if c.rec.Limited != nil {
			c.change(c.box.Pause(wire.PausedLimited, now), now)
		} else if ch, ok := c.box.Resume(now); ok {
			c.change(ch, now)
			c.pump(now)
		}
	}
}

func (c *conv) effects(effects []agentstate.Effect, before wire.AgentState, now time.Time) {
	at := ms(now)
	for _, e := range effects {
		switch e.K {
		case agentstate.WakeQueue:
			c.pump(now)
		case agentstate.PauseQueue:
			c.change(c.box.Pause(e.Pause, now), now)
		case agentstate.PromptSubmitted:
			c.change(c.box.PromptSubmitted(c.rec.State.TurnID, before.K == wire.StateWorking, now), now)
		case agentstate.TurnStarted:
			c.terminalFailureTurn = ""
			c.terminalFailures = terminalfailure.NewDetector()
			if c.look != nil {
				c.terminalFailures.PrimeScreen(c.look.Plain)
			}
			if c.rec.TerminalFailure != nil {
				c.rec.TerminalFailure = nil
				c.touch()
			}
			if t, ok := c.fd.Turn(e.ID); ok && c.rec.Limited != nil && t.StartedAt > c.rec.Limited.At {
				// The agent took new work after all (the person typed into its terminal): the limit is behind it.
				c.release(now)
			}
			c.throttle.Reset()
			c.change(c.box.TurnStarted(e.ID, now), now)
		case agentstate.TurnFinished:
			c.endTurn(e.ID, wire.TurnDone, nil, at, now)
		case agentstate.TurnInterrupted:
			c.endTurn(e.ID, wire.TurnInterrupted, nil, at, now)
		case agentstate.TurnFailed:
			failure := &wire.TurnFailure{Code: e.Turn}
			if e.Detail != "" {
				failure.Detail = &e.Detail
			}
			c.endTurn(e.ID, wire.TurnFailed, failure, at, now)
		case agentstate.AskQuestion:
			if g := c.gates[e.ID]; g != nil && g.q != nil {
				c.emit(wire.QuestionAskedEv{Question: *g.q})
				c.setGate(&wire.GateRef{Kind: wire.GateQuestion, ID: e.ID})
			}
		case agentstate.RequestPermission:
			if g := c.gates[e.ID]; g != nil && g.p != nil {
				c.emit(wire.PermissionRequestedEv{Permission: *g.p})
				c.setGate(&wire.GateRef{Kind: wire.GatePermission, ID: e.ID})
			}
		case agentstate.ProposePlan:
			if g := c.gates[e.ID]; g != nil && g.p != nil {
				c.emit(wire.PlanProposedEv{Permission: *g.p})
				c.setGate(&wire.GateRef{Kind: wire.GatePlan, ID: e.ID})
			}
		case agentstate.GateMovedOn:
			c.moveOn(e.ID, now)
		case agentstate.CloseGate:
			c.closeGate(e.ID, e.Reason, now)
		case agentstate.CompactionStarted, agentstate.CompactionFinished:
			// The page reads compacting off the state; the compaction entry comes as a step of the transcript.
		case agentstate.NotifyPerson:
			// The push of autonomous mode is the sender's (internal/push); it reads the turn's end off convs.
		}
	}
}

func (c *conv) endTurn(id string, st wire.TurnStatus, failure *wire.TurnFailure, at wire.Ms, now time.Time) {
	if id == "" {
		id = c.fd.OpenTurn()
	}
	var limit *feed.Limit
	if id != "" && id != c.terminalFailureTurn {
		st, failure, limit = c.limitEnd(id, st, failure)
	}
	if limit != nil {
		// Paused before the effects wake the queue: what waits goes after the reset, not into the refusal.
		c.hold(*limit, now)
	}
	if id != "" {
		if t, ok := c.fd.EndTurn(id, st, failure, at); ok {
			if t.Status == wire.TurnFailed {
				c.emit(wire.TurnFailedEv{Turn: t})
			} else {
				c.emit(wire.TurnFinishedEv{Turn: t})
			}
		}
		c.change(c.box.TurnEnded(id, now), now)
	}
	if st == wire.TurnInterrupted {
		c.change(c.box.Interrupted(now), now)
	}
	c.nowVal = nil
	c.throttle.Reset()
	c.lastTurn()
}

// lastTurn keeps the record's summary of the newest turn.
func (c *conv) lastTurn() {
	t := c.fd.LastTurn()
	if t == nil {
		return
	}
	s := &wire.TurnSummary{ID: t.ID, Status: t.Status, StartedAt: t.StartedAt, EndedAt: t.EndedAt, DurationMs: t.DurationMs,
		ToolCount: t.ToolCount, Snippet: c.fd.Snippet(*t)}
	if !reflect.DeepEqual(s, c.rec.LastTurn) {
		c.rec.LastTurn = s
		c.touch()
	}
}

// --- gates ---

func (c *conv) setGate(ref *wire.GateRef) {
	if !reflect.DeepEqual(ref, c.rec.Gate) {
		c.rec.Gate = ref
		c.touch()
	}
}

func (c *conv) addGate(g *gate) bool {
	if _, ok := c.gates[g.id()]; ok {
		return false
	}
	c.gates[g.id()] = g
	c.order = append(c.order, g.id())
	c.dirty = true
	return true
}

func (c *conv) openGate(id string) *gate {
	g := c.gates[id]
	if g == nil {
		return nil
	}
	if k := g.state().K; k != wire.GateStateOpen && k != wire.GateStateAnswering {
		return nil
	}
	return g
}

func (c *conv) setGateState(g *gate, st wire.GateState, now time.Time) {
	*g.state() = st
	if st.K == wire.GateStateAnswered || st.K == wire.GateStateClosed {
		g.closedAt = now
		if c.rec.Gate != nil && c.rec.Gate.ID == g.id() {
			c.setGate(nil)
		}
	}
	c.dirty = true
	c.emit(wire.GateUpdatedEv{ID: g.id(), State: st})
}

// moveOn: the agent went past a gate. Answered when we were answering it, else closed{agent-moved-on}.
func (c *conv) moveOn(id string, now time.Time) {
	g := c.openGate(id)
	if g == nil {
		return
	}
	st := *g.state()
	if st.K == wire.GateStateAnswering {
		c.setGateState(g, wire.GateState{K: wire.GateStateAnswered, OpID: st.OpID, At: ms(now), ByDevice: st.ByDevice}, now)
		return
	}
	c.setGateState(g, wire.GateState{K: wire.GateStateClosed, At: ms(now), Reason: wire.ClosedAgentMovedOn}, now)
}

func (c *conv) closeGate(id string, reason wire.GateCloseReason, now time.Time) {
	g := c.openGate(id)
	if g == nil {
		return
	}
	if reason == "" {
		reason = wire.ClosedTurnEnded
	}
	c.setGateState(g, wire.GateState{K: wire.GateStateClosed, At: ms(now), Reason: reason}, now)
}

// --- what the actor sees ---

// read takes what the transcript gained: its events go out in order, its signals feed the queue and the automaton.
func (c *conv) read(now time.Time) {
	size := c.fd.Size()
	u := c.fd.Poll()
	if c.fd.Size() != size {
		c.quietSince = time.Time{}
	}
	c.feedUpdate(u, true, now)
	c.handable.Store(c.probe.HandOver || c.handOverTool())
}

// promptedAt moves lastPromptAt forward to at: the time of the person's last message only grows.
func (c *conv) promptedAt(at wire.Ms) {
	if at > c.rec.LastPromptAt {
		c.rec.LastPromptAt = at
		c.touch()
	}
}

// personWroteAt keeps the time of the newest real message of the person that the feed showed (not seeded by the
// record's creation time, unlike lastPromptAt). A recap written before it is the one for an earlier absence: the
// person came back and the chat went on, so it is no longer what is happening and is dropped.
func (c *conv) personWroteAt(at wire.Ms) {
	if at > c.lastWroteAt {
		c.lastWroteAt = at
		if !c.recapStands(c.rec.Recap) {
			c.rec.Recap = nil
			c.touch()
		}
	}
}

// recapStands says whether a recap still describes the chat: it does until the person writes after it was written.
func (c *conv) recapStands(r *wire.Recap) bool {
	return r == nil || r.At == 0 || r.At >= c.lastWroteAt
}

// promptsOf takes the person's messages of a feed update: a user entry is a real human line (the feed leaves out
// meta lines, compaction summaries, hook and task notifications, tool results and reminders). A conversation that has
// none yet keeps its creation time.
func (c *conv) promptsOf(u feed.Update) {
	for _, st := range u.Steps {
		if st.Entry.T == wire.EntryUser {
			c.promptedAt(st.Entry.At)
			c.personWroteAt(st.Entry.At)
		}
	}
	if c.rec.LastPromptAt == 0 {
		c.promptedAt(c.rec.CreatedAt)
	}
}

func (c *conv) feedUpdate(u feed.Update, live bool, now time.Time) {
	c.feedUpdateOf(u, live, !live, now)
}

// feedUpdateOf: live says whether the entries may be our messages on their way (the user entries go to the outbox);
// past, that the update is a transcript read as a whole, whose turns that are over open nothing and end nothing now.
func (c *conv) feedUpdateOf(u feed.Update, live, past bool, now time.Time) {
	c.promptsOf(u)
	c.indexUpdate(u)
	// The user entries first: an entry that is our message goes out carrying its client id.
	var signals []feed.Signal
	var changes []outbox.Change
	var resolved []string
	open := c.fd.OpenTurn()
	for _, s := range u.Signals {
		if past && (s.K == feed.SignalTurnOpened && s.TurnID != open || s.K == feed.SignalInterrupt || s.K == feed.SignalTurnEnded) {
			continue
		}
		if s.K != feed.SignalUser {
			signals = append(signals, s)
			continue
		}
		if !live {
			continue
		}
		var turn *string
		if s.TurnID != "" {
			turn = &s.TurnID
		}
		offset := s.Offset
		if offset < 0 {
			// A transcript read whole does not know where its entries began; the message in flight takes the first.
			offset = math.MaxInt64
		}
		text := s.Text
		if e, ok := c.fd.Entry(s.EntryID); ok && text == "" {
			text = e.Text
		}
		client, ch := c.box.UserEntryText(s.EntryID, text, offset, turn, now)
		if client != "" {
			c.fd.SetClientID(s.EntryID, client)
		}
		changes = append(changes, ch)
		if past && client != "" && s.TurnID != open {
			// Our message, in a turn that ended before the transcript was found: it was taken and answered.
			changes = append(changes, c.box.TurnEnded(s.TurnID, now))
		}
	}
	if c.quietTurn != "" && live {
		for _, st := range u.Steps {
			if st.Entry.T != wire.EntryUser && st.Entry.TurnID != nil && *st.Entry.TurnID == c.quietTurn {
				// The turn ended on silence takes the agent's lines: it was working, it opens again.
				c.resumeQuiet(now)
				break
			}
		}
	}
	if live {
		for _, e := range u.Events {
			switch ev := e.(type) {
			case wire.TurnFailedEv:
				// A terminal failure ended the turn ahead of the CLI. Its trailing retry/error lines regrow that turn,
				// but do not announce the same terminal outcome again.
				if ev.Turn.ID != c.terminalFailureTurn {
					c.emit(ev)
				}
			case wire.EntryUpdateEv:
				c.emit(ev)
				if ev.Entry.T == wire.EntryTool && ev.Entry.OK != nil && ev.Entry.ToolUseID != "" {
					// A call with its result went past its permission prompt.
					resolved = append(resolved, wire.PermissionID(ev.Entry.ToolUseID))
				}
			case wire.StepEv:
				if fresh, ok := c.fd.Entry(ev.Entry.ID); ok {
					ev.Entry = fresh
				}
				if ev.Entry.At > c.rec.LastActivityAt {
					c.rec.LastActivityAt = ev.Entry.At
					c.touch()
				}
				c.emit(ev)
			default:
				c.emit(e)
			}
		}
	}
	for _, ch := range changes {
		c.change(ch, now)
	}
	for _, s := range signals {
		c.signal(s, now)
	}
	for _, id := range resolved {
		if c.openGate(id) != nil {
			c.signal(feed.Signal{K: feed.SignalGateResolved, GateID: id}, now)
		}
	}
	if live && c.rec.State.Compacting {
		for _, st := range u.Steps {
			if st.Entry.T == wire.EntryCompaction {
				// The compaction's line (compact_boundary) is written when it is done: the end, should its PostCompact
				// hook be lost or not configured.
				c.input(agentstate.Input{K: agentstate.PostCompact}, now)
				break
			}
		}
	}
	if m := c.fd.Model(); m != "" && c.rec.Model == nil && modelIDRe.MatchString(m) {
		// A chat no one picked a model for (a Legacy tab, a resumed session) shows the one its agent answers with.
		c.rec.Model = &wire.Model{ID: m}
		c.touch()
	}
	if len(u.Events) > 0 {
		c.lastTurn()
		c.updateNow(now)
	}
}

// resumeQuiet opens again the turn ended on silence (quietTurn) and the agent works.
func (c *conv) resumeQuiet(now time.Time) {
	id := c.quietTurn
	if id == "" {
		return
	}
	c.quietTurn = ""
	t, ok := c.fd.Resume(id)
	if !ok {
		return
	}
	c.emit(wire.TurnStartedEv{Turn: t})
	c.input(agentstate.Input{K: agentstate.TurnOpened, TurnID: id, At: ms(now)}, now)
	c.lastTurn()
}

func (c *conv) signal(s feed.Signal, now time.Time) {
	if s.K == feed.SignalTurnOpened || s.K == feed.SignalTurnEnded || s.K == feed.SignalInterrupt {
		c.quietTurn = ""
	}
	switch s.K {
	case feed.SignalTurnOpened:
		c.input(agentstate.Input{K: agentstate.TurnOpened, TurnID: s.TurnID, At: s.At}, now)
	case feed.SignalQuestion:
		q := *s.Question
		if c.addGate(&gate{q: &q}) {
			c.input(agentstate.Input{K: agentstate.QuestionAsked, GateID: q.ID}, now)
		}
	case feed.SignalPlan:
		p := *s.Permission
		if c.look != nil && c.look.Menu == typist.MenuPlan {
			p.Options = permissionOptions(c.look.Options, true)
		}
		if c.addGate(&gate{p: &p}) {
			c.input(agentstate.Input{K: agentstate.PlanProposed, GateID: p.ID}, now)
		}
	case feed.SignalPermission:
		p := *s.Permission
		if c.addGate(&gate{p: &p}) {
			c.input(agentstate.Input{K: agentstate.PermissionPrompt, GateID: p.ID}, now)
		}
	case feed.SignalGateResolved:
		if c.rec.State.QuestionID == s.GateID || c.rec.State.PermissionID == s.GateID {
			c.input(agentstate.Input{K: agentstate.GateResolved}, now)
		} else {
			c.moveOn(s.GateID, now)
		}
	case feed.SignalTurnEnded:
		c.input(c.stopInput(s.TurnID), now)
		c.armTitle(now)
	case feed.SignalInterrupt:
		pause := c.interrupting
		c.interrupting = false
		c.input(agentstate.Input{K: agentstate.Interrupted, PauseQueue: pause}, now)
	case feed.SignalFailure:
		c.failTerminal(s.TurnID, s.Failure, s.Detail, now)
	}
}

// failTerminal stops a turn on a provider failure the CLI cannot make progress on. Ctrl-C is the terminal's SIGINT
// character, delivered by tmux to the foreground process group. The queue stays paused for a person's target choice.
func (c *conv) failTerminal(turnID, failure, detail string, now time.Time) {
	if failure != string(terminalfailure.QuotaExhausted) || turnID == "" || c.terminalFailureTurn == turnID {
		return
	}
	c.terminalFailureTurn = turnID
	marker := wire.TerminalFailure{Kind: wire.FailureQuotaExhausted, At: ms(now), Detail: detail, Terminal: true, NeedsTarget: true}
	c.rec.TerminalFailure = &marker
	c.touch()
	c.emit(wire.QuotaExhaustedEv{Failure: marker})
	c.change(c.box.Pause(wire.PausedNeedsTarget, now), now)
	c.input(agentstate.Input{K: agentstate.StopFailure, TurnFailure: wire.TurnFailedRateLimit, TurnDetail: detail}, now)
	pane := c.d.Machine.Pane(c.rec.Session)
	if pane == nil {
		return
	}
	session := c.rec.Session
	c.spawn(func() any {
		unlock := typist.Lock(session)
		defer unlock()
		return pane.Keys("C-c")
	}, func(r any) {
		if err, _ := r.(error); err != nil {
			c.d.Log.Warn("terminal failure interrupt failed", "conv", c.rec.ID, "err", err)
		}
	})
}

// stopInput is the end of a turn ("" the open one), by the Stop hook or by the transcript's own line. Claude ends a
// turn whose call to the model was refused (its login expired) as any other, with the refusal as its last line: that
// turn failed with what Claude said, it did not answer.
func (c *conv) stopInput(turnID string) agentstate.Input {
	if detail, ok := c.fd.ErrorEnd(turnID); ok {
		return agentstate.Input{K: agentstate.StopFailure, TurnFailure: wire.TurnFailedApiError, TurnDetail: detail}
	}
	return agentstate.Input{K: agentstate.Stop}
}

// lookStale is how long a look may take before another is allowed to start beside it.
const lookStale = 10 * time.Second

// seen is one look at a conversation's process and pane.
type seen struct {
	probe Probe
	// look is the screen, nil when the process says there is none to read (no session, a bare shell) or no pane.
	look *typist.Screen
}

// takeLook asks tmux and the process table: it runs off the actor, as any call that can wait on a loaded machine.
func takeLook(m Machine, session string, provider wire.Provider) seen {
	pr := m.Probe(session)
	if !pr.Session || pr.Shell {
		return seen{probe: pr}
	}
	pane := m.Pane(session)
	if pane == nil {
		return seen{probe: pr}
	}
	l := typist.Look(pane, provider)
	return seen{probe: pr, look: &l}
}

// observe looks at the process and the pane — off the actor, so a tmux that is slow to answer never holds the mail:
// the agent gone or exited, a menu up, busy or quiet.
func (c *conv) observe(now time.Time) {
	if c.rec.Session == "" || c.deletedAt != 0 {
		return
	}
	if c.looking && now.Sub(c.lookAt) < lookStale {
		c.lookAgain = true
		return
	}
	c.looking, c.lookAt = true, now
	c.lookSeq++
	seq, gen, session, provider, m := c.lookSeq, c.stateGen, c.rec.Session, c.rec.Provider, c.d.Machine
	c.spawn(func() any { return takeLook(m, session, provider) }, func(r any) {
		if seq != c.lookSeq {
			return
		}
		c.looking = false
		now := c.d.Clock.Now()
		if gen != c.stateGen {
			// The state moved while tmux answered (a hook, the transcript): the screen is of the state before; look again.
			c.lookAgain = false
			c.observe(now)
			return
		}
		if session == c.rec.Session {
			c.seeing(r.(seen), now)
		}
		if c.lookAgain {
			c.lookAgain = false
			c.observe(now)
		}
	})
}

// lostTmux: the agent was taken for gone because its tmux session was not found, which a later look may disprove.
func (c *conv) lostTmux() bool {
	return c.rec.State.K == wire.StateGone && c.rec.State.Reason == string(wire.GoneTmuxLost)
}

// seeing acts on a look: the agent gone or exited, a menu up, busy or quiet.
func (c *conv) seeing(s seen, now time.Time) {
	defer c.restartDue(now)
	c.probe = s.probe
	switch {
	case !c.probe.Session && c.rec.TerminalStop != nil:
		// The terminal was stopped on purpose: no session is what a stopped chat has, and its agent resumes on the next
		// message (the resume makes the session again).
		if c.rec.State.K != wire.StateStarting {
			c.input(agentstate.Input{K: agentstate.Shell, Resumable: true}, now)
		}
		return
	case !c.probe.Session:
		if c.rec.State.K != wire.StateStarting {
			c.input(agentstate.Input{K: agentstate.Gone, GoneReason: wire.GoneTmuxLost}, now)
		}
		return
	case c.lostTmux():
		// The pane was taken for lost (tmux that failed to answer twice) and is there after all: the agent is back,
		// unchecked, and the queue paused for it goes on.
		c.input(agentstate.Input{K: agentstate.Restart}, now)
	case c.probe.Shell && c.rec.State.K == wire.StateStarting:
		if ms(now)-c.rec.State.Since >= LaunchNoAgent.Milliseconds() {
			c.input(agentstate.Input{K: agentstate.LaunchFailed, Failure: wire.LaunchFailedExited}, now)
		}
		return
	case c.probe.Shell:
		c.input(agentstate.Input{K: agentstate.Shell, Resumable: c.probe.Resumable || c.rec.AgentSessionID != ""}, now)
		return
	}
	if s.look == nil {
		return
	}
	look := *s.look
	c.look = &look
	if !look.Readable {
		return
	}
	if turnID := c.fd.OpenTurn(); turnID != "" {
		if c.terminalFailures == nil {
			c.terminalFailures = terminalfailure.NewDetector()
		}
		if failure, ok := c.terminalFailures.ObserveScreen(look.Plain); ok {
			c.failTerminal(turnID, string(failure.Kind), failure.Detail, now)
			return
		}
	}
	k := c.rec.State.K
	if (k == wire.StateStarting || k == wire.StateStopped || k == wire.StateLaunchFailed) && look.PromptReady() && c.probe.Foreground {
		// The input box is drawn before the TUI takes keys (it drops an Enter typed in the first moments), so the
		// agent is ready only once the screen has stood unchanged for BootSettle, or has shown the box for three times
		// that (a screen that never stands still must not fail the launch).
		if c.bootFirst.IsZero() {
			c.bootFirst = now
		}
		if c.bootPlain != look.Plain || c.bootAt.IsZero() {
			c.bootPlain, c.bootAt = look.Plain, now
		}
		if now.Sub(c.bootAt) < BootSettle && now.Sub(c.bootFirst) < 3*BootSettle {
			return
		}
		c.bootPlain, c.bootAt, c.bootFirst = "", time.Time{}, time.Time{}
		c.input(agentstate.Input{K: agentstate.LaunchReady}, now)
		return
	}
	c.bootPlain, c.bootAt, c.bootFirst = "", time.Time{}, time.Time{}
	switch look.Menu {
	case typist.MenuPermission:
		c.permissionOnScreen(look, now)
	case typist.MenuPlan:
		if id := c.rec.State.PermissionID; id != "" {
			if g := c.openGate(id); g != nil && g.p != nil && len(g.p.Options) == 0 {
				g.p.Options = permissionOptions(look.Options, true)
				c.dirty = true
			}
		}
	}
	if look.Menu == typist.NoMenu && k == wire.StateWaitingPermission && (look.Busy || look.InputLine && c.fd.OpenTurn() == "") {
		// The prompt left and the agent works: it went past the permission. A prompt asked after the turn (a background
		// agent's) leaves the input box behind, with no spinner of the chat's own.
		c.input(agentstate.Input{K: agentstate.GateResolved}, now)
	}
	if look.Busy {
		c.quietSince = time.Time{}
		c.resumeQuiet(now)
		c.input(agentstate.Input{K: agentstate.ScreenBusy}, now)
	} else if look.PromptReady() && look.Menu == typist.NoMenu && (k == wire.StateWorking || k == wire.StateUnknown) {
		if c.compacting(now) {
			// A compaction writes nothing for a minute and more, its spinner maybe unread: no silence.
			c.quietSince = time.Time{}
		} else if c.quietSince.IsZero() || c.fd.Size() != c.quietSize {
			c.quietSince, c.quietSize = now, c.fd.Size()
		} else if now.Sub(c.quietSince) >= QuietAfter && !c.typing && c.quietOver(look, k, now) {
			c.quietSince = time.Time{}
			open := c.fd.OpenTurn()
			c.input(agentstate.Input{K: agentstate.Quiet, TurnEnded: open == ""}, now)
			if open != "" && c.fd.OpenTurn() == "" {
				c.quietTurn = open
			}
		}
	} else {
		c.quietSince = time.Time{}
	}
	if look.PromptReady() && look.Menu == typist.NoMenu {
		c.limitOnScreen(look.Plain, now)
	}
	c.updateNow(now)
}

// quietOver: the silence ends the turn. Claude says so under the turn (its summary, its stop mark); without that the
// turn waits on the model — no line in the transcript for a minute and more, the spinner maybe unread — and silence
// alone ends it only past QuietUnmarked. The transcript's end, or the Stop hook, ends it before any silence.
func (c *conv) quietOver(look typist.Screen, k wire.AgentStateKind, now time.Time) bool {
	if k != wire.StateWorking || c.rec.Provider == wire.ProviderCodex || c.fd.OpenTurn() == "" || screen.TurnDoneLast(look.Plain) {
		return true
	}
	if now.Sub(c.quietSince) < QuietUnmarked {
		return false
	}
	c.d.Log.Warn("turn ended on silence without its end on screen", "conv", c.rec.ID, "quiet", now.Sub(c.quietSince).String())
	return true
}

// compacting: the agent compacts its context, as its hooks said, within CompactHold.
func (c *conv) compacting(now time.Time) bool {
	return c.rec.State.K == wire.StateWorking && c.rec.State.Compacting && now.Sub(c.compactAt) < CompactHold
}

// screenGateFrom are the states a permission prompt on the screen opens a gate in: at work, unknown, and idle or ready —
// the turn is over but a background agent of the chat still runs and asks.
var screenGateFrom = map[wire.AgentStateKind]bool{wire.StateWorking: true, wire.StateUnknown: true, wire.StateIdle: true,
	wire.StateReady: true}

// permissionOnScreen: Claude's permission prompt is up. Its gate is the pending tool call's; the hook said so first,
// or the screen does now.
func (c *conv) permissionOnScreen(look typist.Screen, now time.Time) {
	if c.rec.State.PermissionID == "" {
		if _, ok := c.fd.PendingTool(); !ok {
			// The menu is drawn after the call is written, and the read that came before the look may be older than
			// the call: read again, so the gate is the call's own and not one made up from the screen.
			c.read(now)
		}
	}
	if id := c.rec.State.PermissionID; id != "" {
		if g := c.openGate(id); g != nil && g.p != nil {
			if len(g.p.Options) == 0 {
				g.p.Options = permissionOptions(look.Options, false)
				c.dirty = true
			}
			c.gateOnScreen(g, look, now)
		}
		return
	}
	// A chat whose turn is over asks too: its background agents run on and their prompts come up on the same screen.
	if !screenGateFrom[c.rec.State.K] {
		return
	}
	p := wire.Permission{ConversationID: c.rec.ID, Kind: wire.PermTool, AskedAt: ms(now),
		Options: permissionOptions(look.Options, false), State: wire.GateState{K: wire.GateStateOpen}}
	if e, ok := c.fd.PendingTool(); ok {
		p.ID = wire.PermissionID(e.ToolUseID)
		tool := e.Tool
		p.Tool = &tool
		if e.Summary != "" {
			sum := e.Summary
			p.Command = &sum
		}
	} else {
		p.ID = wire.PermissionID(c.rec.ID + ":" + itoa(c.seq+1))
		// No call of the chat's own is pending (a background agent asks): the card shows the command the dialog draws.
		if drawn := typist.DrawnCommand(look.Plain); drawn != "" {
			sum := "$ " + drawn
			p.Command = &sum
		}
	}
	// The card stands in its turn: the open one, else the last (a background agent asks after it ended).
	if open := c.fd.OpenTurn(); open != "" {
		p.TurnID = &open
	} else if last := c.fd.LastTurn(); last != nil {
		id := last.ID
		p.TurnID = &id
	}
	if g := c.gates[p.ID]; g != nil {
		return
	}
	g := &gate{p: &p}
	c.addGate(g)
	c.input(agentstate.Input{K: agentstate.PermissionPrompt, GateID: p.ID}, now)
	c.gateOnScreen(g, look, now)
}

// autoDenyDrift is how far the countdown on the screen may differ from the one the gate carries before it is said again.
const autoDenyDrift = 3 * time.Second

// gateOnScreen keeps an open permission's state true to the screen: «refused» while the menu on it is not the one the
// card shows (no key would go in, so the card offers the terminal at once), and the moment Claude denies the request by
// itself when its prompt says so.
func (c *conv) gateOnScreen(g *gate, look typist.Screen, now time.Time) {
	st := *g.state()
	if st.K != wire.GateStateOpen || g.p == nil || g.p.Kind != wire.PermTool {
		return
	}
	next := wire.GateState{K: wire.GateStateOpen}
	for i, o := range g.p.Options {
		want := typist.PickWant{N: i + 1, Label: o.Label, Cwd: c.rec.Cwd}
		if n, err := strconv.Atoi(o.ID); err == nil {
			want.N = n
		}
		if g.p.Command != nil {
			want.Command = *g.p.Command
		}
		if err := typist.Shows(look, want); err != nil {
			refused := wire.GateRefusedMenuUnmatched
			next.Refused = &refused
			break
		}
	}
	if secs, ok := screen.AutoDenySeconds(look.Plain); ok {
		at := ms(now.Add(time.Duration(secs) * time.Second))
		if st.AutoDenyAt != nil && absMs(*st.AutoDenyAt-at) < wire.Ms(autoDenyDrift/time.Millisecond) {
			at = *st.AutoDenyAt
		}
		next.AutoDenyAt = &at
	}
	if (st.Refused == nil) != (next.Refused == nil) || (st.AutoDenyAt == nil) != (next.AutoDenyAt == nil) ||
		st.AutoDenyAt != nil && *st.AutoDenyAt != *next.AutoDenyAt {
		c.setGateState(g, next, now)
	}
}

func absMs(d wire.Ms) wire.Ms {
	if d < 0 {
		return -d
	}
	return d
}

func (c *conv) updateNow(now time.Time) {
	word := ""
	if c.look != nil && c.look.Busy {
		word = c.look.Word
	}
	n := c.fd.Now(word, ms(now))
	c.nowVal = n
	if ev := c.throttle.Offer(n, now); ev != nil {
		c.emit(*ev)
	}
}

// --- the queue ---

// pump gives the head of the queue its chance: a fresh look at the box (off the actor), then the try, typed off the
// actor too.
func (c *conv) pump(now time.Time) {
	if c.typing || c.switching || c.busyGate || c.deletedAt != 0 {
		return
	}
	// A model change asked while the agent was away or at work goes in before the next message.
	if c.applyPending(now) {
		return
	}
	var pane typist.Pane
	if c.rec.Session != "" {
		pane = c.d.Machine.Pane(c.rec.Session)
	}
	if pane == nil || len(c.box.Queue().Items) == 0 && !c.box.NeedsLook(now) {
		c.pumpWith(pane, nil, now)
		return
	}
	if c.pumping {
		c.pumpAgain = true
		return
	}
	c.pumping = true
	provider := c.rec.Provider
	c.spawn(func() any { return typist.Look(pane, provider) }, func(r any) {
		c.pumping = false
		now := c.d.Clock.Now()
		look := r.(typist.Screen)
		c.look = &look
		c.pumpWith(pane, &look, now)
		if c.pumpAgain {
			c.pumpAgain = false
			c.pump(now)
		}
	})
}

// handOverTool: the tool the agent runs now is one Claude can hand to the background (a Bash command, an MCP call).
func (c *conv) handOverTool() bool {
	e, ok := c.fd.PendingTool()
	return ok && (e.Tool == "Bash" || strings.HasPrefix(e.Tool, "mcp__"))
}

func (c *conv) pumpWith(pane typist.Pane, look *typist.Screen, now time.Time) {
	if c.typing || c.switching || c.busyGate || c.deletedAt != 0 {
		return
	}
	a := outbox.Agent{State: c.rec.State, Foreground: c.probe.Foreground, HandOver: c.probe.HandOver || c.handOverTool(),
		Provider: c.rec.Provider, Screen: look}
	job, ch := c.box.Pump(a, c.fd.Size(), now)
	c.change(ch, now)
	if job == nil {
		return
	}
	c.handable.Store(a.HandOver)
	// The chord is pressed only while the tool still runs: the text takes a moment to type, and a tool that ended
	// meanwhile leaves the model writing, which the chord would cut short.
	job.Message.HandOverOK = c.handable.Load
	if pane == nil {
		c.change(c.box.Typed(job.ClientID, typist.Result{Outcome: typist.Failed}, now), now)
		return
	}
	if c.terminalOff() && chatguard.BashMode(job.Message.Text) {
		// Queued before the terminal went off, or by something that does not go through send: not typed, and it fails
		// as a message that cannot be typed does, after its tries.
		c.d.Log.Warn("a queued message would start the agent's bash mode and the terminal is off; not typed", "conv", c.rec.ID)
		c.change(c.box.Typed(job.ClientID, typist.Result{Outcome: typist.Failed, Err: chatguard.ErrTerminalOff}, now), now)
		return
	}
	c.typing = true
	session, provider, clock := c.rec.Session, c.rec.Provider, c.d.Clock
	c.spawn(func() any {
		unlock := typist.Lock(session)
		defer unlock()
		return typist.Type(job.Message, provider, pane, clock)
	}, func(r any) {
		c.typing = false
		now := c.d.Clock.Now()
		c.change(c.box.Typed(job.ClientID, r.(typist.Result), now), now)
		c.read(now)
		c.pump(now)
	})
}

func (c *conv) resume(now time.Time) {
	if c.d.Launcher == nil {
		return
	}
	if err := c.d.Launcher.Resume(c.rec); err != nil {
		c.d.Log.Warn("resume failed", "conv", c.rec.ID, "err", err)
		return
	}
	c.ultracodeAgain(now)
	c.input(agentstate.Input{K: agentstate.LaunchRequested}, now)
}

// freshLaunch starts the agent anew for a conversation whose first launch failed: it has no session to resume (the
// agent never got as far as keeping one), so the queue's pause, which only a running agent lifts, would hold the
// messages for good. «Continue sending», a retry and a new message all come here: the pause of the failed launch is
// lifted and the agent is launched again with the first message of the line as its first prompt, the way a new
// conversation starts. Nothing happens for any other state, a conversation with a session (that one is resumed) or a
// queue paused by the person.
func (c *conv) freshLaunch(now time.Time) {
	if c.d.Launcher == nil || c.rec.State.K != wire.StateLaunchFailed || c.rec.AgentSessionID != "" || c.rec.TranscriptPath != "" {
		return
	}
	if p := c.box.Paused(); p != nil {
		if p.Reason != wire.PausedLaunchFailed {
			return
		}
		ch, _ := c.box.Resume(now)
		c.change(ch, now)
	}
	c.input(agentstate.Input{K: agentstate.LaunchRequested}, now)
	req := LaunchRequest{ID: c.rec.ID, Cwd: c.rec.Cwd, Provider: c.rec.Provider, Model: c.rec.Model}
	if head := c.box.Head(); head != "" {
		ch, carried := c.box.Carry(head, now)
		c.change(ch, now)
		if m, ok := c.box.Message(head); ok && carried {
			req.First = m.Text
		}
	}
	if err := c.d.Launcher.Launch(req); err != nil {
		c.d.Log.Warn("launch failed", "conv", c.rec.ID, "err", err)
		c.input(agentstate.Input{K: agentstate.LaunchFailed, Failure: wire.LaunchFailedNoProcess}, now)
	}
}

// ultracodeAgain: Claude Code keeps ultracode for one process only (it never saves it), and a resumed agent starts
// without it. The record says so, and the mode is asked of the agent again as a waiting change, applied before the
// next message like any other.
func (c *conv) ultracodeAgain(now time.Time) {
	m := c.rec.Model
	if m == nil || !m.Ultracode || c.rec.Provider != wire.ProviderClaude {
		return
	}
	off := *m
	off.Ultracode = false
	c.rec.Model = &off
	if c.rec.PendingModel == nil {
		c.rec.PendingModel = &wire.PendingModel{ID: m.ID, Effort: m.Effort, Ultracode: true, Since: ms(now)}
		c.pendingTries, c.pendingAt = 0, time.Time{}
	}
	c.touch()
}

// --- the clock ---

// tick is the clock's turn: terms that ran out, the queue's timers, the held-back now, the coalesced record, the
// gates kept past their time.
func (c *conv) tick(now time.Time) {
	c.input(agentstate.Input{K: agentstate.Tick}, now)
	c.change(c.box.Tick(now), now)
	for _, id := range slices.Clone(c.order) {
		g := c.gates[id]
		st := g.state()
		if st.K == wire.GateStateAnswering && st.Wait != nil && ms(now) >= st.Wait.DeadlineAt && !c.busyGate {
			// The keys went in and the agent did not move on within the term: the gate is open again, the page
			// shows the answer did not take.
			c.setGateState(g, wire.GateState{K: wire.GateStateOpen}, now)
		}
		if !g.closedAt.IsZero() && now.Sub(g.closedAt) > GateKept {
			delete(c.gates, id)
			c.order = slices.DeleteFunc(c.order, func(x string) bool { return x == id })
			c.dirty = true
		}
	}
	if ev := c.throttle.Due(now); ev != nil {
		c.emit(*ev)
	}
	if w := c.box.Wake(); !w.IsZero() && !now.Before(w) {
		c.pump(now)
	}
	c.applyPending(now)
	c.limitDue(now)
	c.restartDue(now)
	c.watchBackground(now)
	c.watchMeta(now)
	c.watchPictures(now)
	c.watchTitle(now)
	c.flush(now, false)
}

// wake is when the actor next has something to do by the clock, at most LookEvery away.
func (c *conv) wake(now time.Time) time.Time {
	next := now.Add(LookEvery)
	consider := func(t time.Time) {
		if !t.IsZero() && t.Before(next) {
			next = t
		}
	}
	consider(c.box.Wake())
	consider(c.throttle.Wake())
	consider(c.limitWake())
	consider(c.restartWake())
	if c.rec.PendingModel != nil {
		consider(c.pendingAt)
	}
	if w := c.rec.State.Wait; w != nil {
		consider(time.UnixMilli(w.DeadlineAt))
	}
	for _, g := range c.gates {
		if st := g.state(); st.K == wire.GateStateAnswering && st.Wait != nil {
			consider(time.UnixMilli(st.Wait.DeadlineAt))
		}
	}
	if c.recDirty || c.stateDirty {
		consider(c.flushedAt.Add(StateCoalesce))
	}
	if !c.titling {
		consider(c.titleDueAt)
	}
	if next.Before(now) {
		return now
	}
	return next
}

// --- reads ---

func (c *conv) questions() []wire.Question {
	out := []wire.Question{}
	for _, id := range c.order {
		if g := c.gates[id]; g.q != nil {
			out = append(out, *g.q)
		}
	}
	return out
}

func (c *conv) permissions() []wire.Permission {
	out := []wire.Permission{}
	for _, id := range c.order {
		if g := c.gates[id]; g.p != nil {
			out = append(out, *g.p)
		}
	}
	return out
}

func (c *conv) snapshot(now time.Time) wire.ConvSnapshot {
	c.bgOpened = true
	ts, es, before := c.fd.Snapshot(snapshotTurns)
	return wire.ConvSnapshot{Turns: ts, Entries: es, BeforeTurnID: before, Queue: c.box.Queue(), Messages: c.box.Messages(now),
		Questions: c.questions(), Permissions: c.permissions(), Background: c.background(), Shots: slices.Clone(c.shots), Pictures: slices.Clone(c.pics.list), Now: c.nowVal}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// agentUp: the agent of a state runs in its terminal (at its prompt, at work or asking).
func agentUp(k wire.AgentStateKind) bool {
	switch k {
	case wire.StateIdle, wire.StateReady, wire.StateWorking, wire.StateWaitingQuestion, wire.StateWaitingPermission:
		return true
	}
	return false
}
