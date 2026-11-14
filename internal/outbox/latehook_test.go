package outbox

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// a message typed into a new chat on a loaded machine. The agent took it, but its word came late: the hook
// after the confirmation's term, the transcript file (written after the hook) found later still. At the term the
// message goes back to the line; these are the orders in which the agent's word and the next look may come then. A
// message the agent has is never typed into it a second time on a guess: it is typed again only when the transcript is
// read to its end and holds no entry of it. While the transcript is not read the message waits in the line, looked at
// again every few seconds, and stalls for the person only when it is still not read twice the term after the print.
func TestAMessageBackByItsTermIsNotTypedTwice(t *testing.T) {
	const text = "проверь логи"
	cases := []struct {
		name string
		// hook: the agent's prompt-submit hook names the prompt (Claude); false is Codex's path.
		hook bool
		read bool
		// busy: the agent shows a turn at work when the term ends.
		busy bool
		// late is the agent's word that comes after the term (nil: none comes); duringRetype makes it come while the
		// second print is typed.
		late         func(w *lateWorld)
		duringRetype bool
		// entryAtTerm: the entry is in the transcript, read, when the term ends.
		entryAtTerm bool
		// atTerm: the message's state right after the term; prints: the times it was typed, then and in the end.
		atTerm      wire.QueueReason
		termPrints  int
		final       wire.MessageStateKind
		finalPrints int
		entryID     string
	}{
		{
			// (a) The agent looks free (its turn ended, or the screen does not show it) before the late hook: the box
			// cannot tell from a transcript it has not read that the agent has the message, and does not retype it.
			name: "a: no transcript yet, agent looks free, the hook comes late",
			hook: true, read: false,
			late:   func(w *lateWorld) { w.r.hook(text) },
			atTerm: wire.QueuedAgentBusy, termPrints: 1,
			final: wire.MsgAccepted, finalPrints: 1,
		},
		{
			name: "a: no transcript yet and no word ever: stalled for the person, not typed again",
			hook: true, read: false,
			atTerm: wire.QueuedAgentBusy, termPrints: 1,
			final: wire.MsgQueued, finalPrints: 1,
		},
		{
			// The transcript is read to its end and has no entry: the agent did not take it, and the one retype goes.
			name: "a: transcript read without its entry: the one retype",
			hook: true, read: true,
			atTerm: "", termPrints: 2,
			final: wire.MsgQueued, finalPrints: 2,
		},
		{
			name: "a: agent at work at the term: waits in the line, the late hook accepts it",
			hook: true, read: false, busy: true,
			late:   func(w *lateWorld) { w.r.hook(text) },
			atTerm: wire.QueuedAgentBusy, termPrints: 1,
			final: wire.MsgAccepted, finalPrints: 1,
		},
		{
			// (b) The transcript file is found at last; its user entry with the message's text is read while the
			// message is queued (stalled or waiting on the busy agent): it is the message's, accepted, not typed again.
			name: "b: entry read while stalled",
			hook: true, read: false,
			late:   func(w *lateWorld) { w.userEntry("c:bb#0") },
			atTerm: wire.QueuedAgentBusy, termPrints: 1,
			final: wire.MsgAccepted, finalPrints: 1, entryID: "c:bb#0",
		},
		{
			name: "b: entry read while queued behind the busy agent",
			hook: true, read: false, busy: true,
			late:   func(w *lateWorld) { w.userEntry("c:bb#0") },
			atTerm: wire.QueuedAgentBusy, termPrints: 1,
			final: wire.MsgAccepted, finalPrints: 1, entryID: "c:bb#0",
		},
		{
			name: "b: entry in the transcript at the term: accepted by the look before the retype",
			hook: true, read: true,
			entryAtTerm: true,
			atTerm:      "", termPrints: 1,
			final: wire.MsgAccepted, finalPrints: 1, entryID: "c:bb#0",
		},
		{
			// (c) The retype was due (the transcript said nothing), and the first print's late hook comes while the
			// second is typed: it confirms the send on its way, the second print's own hook confirms nothing, and no
			// third print follows. The two prints are the price of a transcript that lagged behind the agent — the
			// reason the retype waits for the transcript read to its end.
			name: "c: the late hook comes while it is typed again",
			hook: true, read: true, duringRetype: true,
			late:   func(w *lateWorld) { w.r.hook(text) },
			atTerm: "", termPrints: 2,
			final: wire.MsgAccepted, finalPrints: 2,
		},
		{
			// (d) A hook that names no prompt (an old Claude) is the message on its way's; with none on its way it
			// confirms nothing, and the stalled message waits for the person.
			name: "d: hook without a hash, nothing on its way",
			hook: true, read: false,
			late:   func(w *lateWorld) { w.r.record(w.r.box.PromptSubmitted(nil, false, w.r.clock.Now())) },
			atTerm: wire.QueuedAgentBusy, termPrints: 1,
			final: wire.MsgQueued, finalPrints: 1,
		},
		{
			name: "d: hook without a hash for the retype on its way",
			hook: true, read: true,
			late:   func(w *lateWorld) { w.r.record(w.r.box.PromptSubmitted(nil, false, w.r.clock.Now())) },
			atTerm: "", termPrints: 2,
			final: wire.MsgAccepted, finalPrints: 2,
		},
		{
			// Codex: no hook confirms, so the box never types a message again by itself — it stalls at the term.
			name: "d: Codex, a late hook without a hash",
			hook: false, read: false,
			late:   func(w *lateWorld) { w.r.record(w.r.box.PromptSubmitted(nil, false, w.r.clock.Now())) },
			atTerm: wire.QueuedStalled, termPrints: 1,
			final: wire.MsgQueued, finalPrints: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), wire.StateIdle)
			r.box.HookConfirms = tc.hook
			w := &lateWorld{r: r, read: tc.read}
			r.box.Transcript = func(int64, wire.Ms, string) (string, bool) { return w.entry, w.read && w.entry != "" }
			r.box.TranscriptRead = func() bool { return w.read }
			r.leaves()
			r.size = 100
			r.send("m1", text, wire.ModeQueue)
			r.pump()
			if st := r.state("m1"); st.K != wire.MsgInInput || r.lines(text) != 1 {
				t.Fatalf("typed: %+v, %v", st, r.typed())
			}
			r.size = 400 // the agent wrote its turn
			if tc.entryAtTerm {
				w.entry = "c:bb#0"
			}
			if tc.duringRetype {
				leave := r.pane.onSend
				r.pane.onSend = func(call string) {
					leave(call)
					if strings.HasPrefix(call, "line:") && r.lines(text) == 2 {
						tc.late(w)
					}
				}
			}
			if tc.busy {
				r.agent.State = wire.AgentState{K: wire.StateWorking, Since: 2}
			}
			r.advance(ConfirmWindow)
			st := r.state("m1")
			if r.lines(text) != tc.termPrints {
				t.Fatalf("at the term typed %d times (%s)", r.lines(text), r.trail("m1"))
			}
			if tc.atTerm != "" && (st.K != wire.MsgQueued || st.Reason != tc.atTerm) {
				t.Fatalf("at the term: %+v (%s)", st, r.trail("m1"))
			}
			if tc.late != nil && !tc.duringRetype {
				tc.late(w)
			}
			r.agent.State = wire.AgentState{K: wire.StateIdle, Since: 3}
			for range 5 {
				r.advance(time.Hour)
			}
			st = r.state("m1")
			if st.K != tc.final || r.lines(text) != tc.finalPrints {
				t.Fatalf("in the end: %+v, typed %d times (%s)", st, r.lines(text), r.trail("m1"))
			}
			if tc.entryID != "" && (st.EntryID == nil || *st.EntryID != tc.entryID) {
				t.Fatalf("entry %v, want %s", st.EntryID, tc.entryID)
			}
			if tc.final == wire.MsgAccepted && len(r.box.Queue().Items) != 0 {
				t.Fatalf("accepted but still queued: %v", r.box.Queue().Items)
			}
			if tc.final == wire.MsgAccepted {
				// The second print's own hook, or a repeat of the late one, confirms nothing more and types nothing.
				r.hook(text)
				r.advance(time.Hour)
				if r.lines(text) != tc.finalPrints {
					t.Fatalf("a later hook typed it again: %d", r.lines(text))
				}
			}
		})
	}
}

// lateWorld is what the agent's side of the box shows: the transcript (found and read or not, the message's entry).
type lateWorld struct {
	r     *rig
	read  bool   // the transcript is found and read to its end
	entry string // the message's entry in the transcript, "" none
}

// userEntry is the message's entry read off the transcript once its file is found.
func (w *lateWorld) userEntry(id string) {
	w.read, w.entry = true, id
	_, ch := w.r.box.UserEntryText(id, "проверь логи", w.r.size, nil, w.r.clock.Now())
	w.r.record(ch)
}
