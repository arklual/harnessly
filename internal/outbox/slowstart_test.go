package outbox

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A long launch on a loaded machine with slow hooks: the agent took the carried first prompt, but the
// runtime finds and reads the new session's transcript only minutes later. Until the transcript is read to its end the
// message is not failed «not confirmed»; once the entry is read it is the agent's. A transcript read with no entry
// still fails it, and so does one never read past CarryLongest.
func TestACarriedPromptWaitsForTheTranscriptOfASlowStart(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	b := New("tab-1", wire.DefaultLimits, nil)
	read, entry := false, ""
	b.Transcript = func(int64, wire.Ms, string) (string, bool) { return entry, read && entry != "" }
	b.TranscriptRead = func() bool { return read }
	if _, code := b.Send(Draft{ClientID: "first", Text: "research code mode", Mode: wire.ModeQueue}, wire.AgentState{K: wire.StateStarting}, now); code != "" {
		t.Fatalf("send: %s", code)
	}
	if _, ok := b.Carry("first", now); !ok {
		t.Fatal("not carried")
	}
	// The launch takes 85 s, hooks run; the start is over, the term passes, the transcript is not read yet.
	b.Starting = true
	b.Tick(now.Add(85 * time.Second))
	b.Starting = false
	b.Tick(now.Add(86 * time.Second))
	at := now.Add(86*time.Second + CarryDeadline + 5*time.Second)
	b.Tick(at)
	if m, _ := b.Message("first"); m.State.K != wire.MsgInInput {
		t.Fatalf("failed while the transcript was not read: %+v", m.State)
	}
	// The transcript is found and read: the agent has the message.
	read, entry = true, "c:e1#0"
	b.Tick(at.Add(time.Second))
	if m, _ := b.Message("first"); m.State.K != wire.MsgAccepted || m.State.EntryID == nil || *m.State.EntryID != "c:e1#0" {
		t.Fatalf("not accepted from the transcript: %+v", m.State)
	}

	// Never read: it fails at last, past CarryLongest.
	c := New("tab-2", wire.DefaultLimits, nil)
	c.Transcript = func(int64, wire.Ms, string) (string, bool) { return "", false }
	c.TranscriptRead = func() bool { return false }
	c.Send(Draft{ClientID: "first", Text: "x", Mode: wire.ModeQueue}, wire.AgentState{K: wire.StateStarting}, now)
	c.Carry("first", now)
	c.Tick(now.Add(CarryDeadline + CarryLongest + time.Second))
	if m, _ := c.Message("first"); m.State.K != wire.MsgFailed || m.State.Code != wire.MsgFailedUnconfirmed {
		t.Fatalf("never read, past CarryLongest: %+v", m.State)
	}
}

// Повторить on a carried first prompt the transcript holds accepts it, not a second copy (TypedOffset 0).
func TestRetryOfACarriedPromptTheAgentHasSendsNoCopy(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	b := New("tab-1", wire.DefaultLimits, nil)
	b.Send(Draft{ClientID: "first", Text: "research code mode", Mode: wire.ModeQueue}, wire.AgentState{K: wire.StateStarting}, now)
	b.Carry("first", now)
	b.Tick(now.Add(CarryDeadline + time.Second))
	if m, _ := b.Message("first"); m.State.K != wire.MsgFailed {
		t.Fatalf("not failed: %+v", m.State)
	}
	b.Transcript = func(int64, wire.Ms, string) (string, bool) { return "c:e1#0", true }
	if _, code := b.Retry("first", nil, now.Add(2*time.Minute)); code != "" {
		t.Fatalf("retry: %s", code)
	}
	if m, _ := b.Message("first"); m.State.K != wire.MsgAccepted || b.Head() != "" {
		t.Fatalf("retried as a copy: %+v, head %q", m.State, b.Head())
	}
}
