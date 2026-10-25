package prompts

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Two messages typed during a running Bash command and handed over by the chord: Claude Code 2.1.310 injects them as
// `queued_command` attachments with `remove`, never as `user` records.
const (
	chordFirst  = "M1: also say OWL-TWO"
	chordSecond = "M2: and also say OWL-THREE"
)

var (
	chordBefore = time.Date(2026, 10, 24, 1, 53, 30, 0, time.UTC).UnixMilli()
	chordAfter  = time.Date(2026, 10, 24, 1, 54, 0, 0, time.UTC).UnixMilli()
)

func queued(id, text string, createdAt int64) Item {
	return Item{ID: id, Text: text, CreatedAt: createdAt}
}

func userLine(text, at string) string {
	return record(map[string]any{"type": "user", "timestamp": at, "message": map[string]any{"role": "user", "content": text}})
}

func enqueueLine(text, at string) string {
	return record(map[string]any{"type": "queue-operation", "operation": "enqueue", "content": text, "timestamp": at})
}

func TestRecordedItemIDs(t *testing.T) {
	chord := transcript(t, "claude-2.1.310-chord-two-queued.jsonl")
	ms := func(s string) int64 { tm, _ := time.Parse(time.RFC3339, s); return tm.UnixMilli() }

	t.Run("a message injected into the running turn is an attachment, and counts as delivered", func(t *testing.T) {
		items := []Item{queued("a", chordFirst, chordBefore), queued("b", chordSecond, chordBefore+1)}
		if got := recordedItemIDs(items, chord); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("while it waits in Claude's own queue it counts too, so the queue does not type it a second time", func(t *testing.T) {
		if got := recordedItemIDs([]Item{queued("a", chordFirst, chordBefore)}, chord[:3]); !reflect.DeepEqual(got, []string{"a"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("a message queued after the record is a new message, however alike", func(t *testing.T) {
		if got := recordedItemIDs([]Item{queued("a", chordFirst, chordAfter)}, chord); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("a turn that started with the message (dequeue, then the prompt) is one record, not two", func(t *testing.T) {
		lines := []string{enqueueLine("продолжай", "2026-10-12T18:52:22.000Z"),
			record(map[string]any{"type": "queue-operation", "operation": "dequeue", "timestamp": "2026-10-12T18:52:30.000Z"}),
			userLine("продолжай", "2026-10-12T18:52:30.100Z")}
		base := ms("2026-10-12T18:52:20Z")
		got := recordedItemIDs([]Item{queued("a", "продолжай", base), queued("b", "продолжай", base+10)}, lines)
		if !reflect.DeepEqual(got, []string{"a"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("each record answers for one message: two identical messages need two records", func(t *testing.T) {
		base := ms("2026-10-12T18:52:20Z")
		items := []Item{queued("a", "ок", base), queued("b", "ок", base+1)}
		one := []string{userLine("ок", "2026-10-12T18:52:25.000Z")}
		if got := recordedItemIDs(items, one); !reflect.DeepEqual(got, []string{"a"}) {
			t.Fatalf("one record: %v", got)
		}
		two := append(one, userLine("ок", "2026-10-12T18:52:26.000Z"))
		if got := recordedItemIDs(items, two); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Fatalf("two records: %v", got)
		}
	})
	t.Run("a short message is not taken for a longer one that merely contains it", func(t *testing.T) {
		lines := []string{userLine("да, но сначала проверь тесты", "2026-10-12T18:52:25.000Z")}
		if got := recordedItemIDs([]Item{queued("a", "да", ms("2026-10-12T18:52:20Z"))}, lines); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("paste wrappers, picture tokens and spacing do not make a record differ", func(t *testing.T) {
		lines := []string{userLine("[Image #1]  смотри\n<pasted_content id=\"1\">на   это</pasted_content id=\"1\">", "2026-10-12T18:52:25.000Z")}
		if got := recordedItemIDs([]Item{queued("a", "смотри на это", ms("2026-10-12T18:52:20Z"))}, lines); !reflect.DeepEqual(got, []string{"a"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("tool answers, a broken line and a message of pictures only are not messages", func(t *testing.T) {
		base := ms("2026-10-12T18:52:20Z")
		lines := []string{answer("t1"), `{"type":"user","timestamp":"2026-10-12T18:52:2`}
		if got := recordedItemIDs([]Item{queued("a", "ok", base)}, lines); len(got) != 0 {
			t.Fatalf("tool answer: %v", got)
		}
		if got := recordedItemIDs([]Item{{ID: "a", Images: []string{"/p.png"}, CreatedAt: base}}, []string{userLine("", "2026-10-12T18:52:25.000Z")}); len(got) != 0 {
			t.Fatalf("pictures only: %v", got)
		}
	})
	t.Run("a record stamped a moment before the queue entry still belongs to it", func(t *testing.T) {
		// Claude stamps an injected message with the moment Enter was pressed, before the runtime's clock read it.
		at := ms("2026-10-12T18:52:25Z")
		lines := []string{userLine("ок", "2026-10-12T18:52:24.800Z")}
		if got := recordedItemIDs([]Item{queued("a", "ок", at)}, lines); !reflect.DeepEqual(got, []string{"a"}) {
			t.Fatalf("got %v", got)
		}
	})
}

func TestProbeSeesAnInjectedMessageAsDelivered(t *testing.T) {
	chord := transcript(t, "claude-2.1.310-chord-two-queued.jsonl")
	since := float64(chordBefore)
	if probeOf(t, chord[:7], chordFirst, since).Delivered {
		t.Error("a message still in Claude's queue counts as delivered")
	}
	if !probeOf(t, chord, chordFirst, since).Delivered || !probeOf(t, chord, chordSecond, since).Delivered {
		t.Error("an injected message is not delivered")
	}
	if probeOf(t, chord, chordFirst, float64(chordAfter)).Delivered {
		t.Error("an injection before the steer counts")
	}
}

// queueRig is a Claude tab on an idle prompt whose transcript the test writes.
func queueRig(t *testing.T, lines *[]string) *rig {
	t.Helper()
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{TranscriptPath: "/t.jsonl"})
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return *lines, true }
	return r
}

func TestDropRecordedTakesWhatTheAgentHoldsAndLeavesTheRest(t *testing.T) {
	chord := transcript(t, "claude-2.1.310-chord-two-queued.jsonl")
	r := queueRig(t, &chord)
	_, _ = r.svc.Queue().Add("tab-1", chordFirst, nil)
	_, _ = r.svc.Queue().Add("tab-1", "ещё не дошло", nil)
	if n := r.svc.dropRecorded("tab-1"); n != 1 {
		t.Fatalf("dropped %d", n)
	}
	if left := queueTexts(r, "tab-1"); !reflect.DeepEqual(left, []string{"ещё не дошло"}) {
		t.Fatalf("left %v", left)
	}
	// Without a transcript nothing is judged.
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return nil, false }
	_, _ = r.svc.Queue().Add("tab-1", chordSecond, nil)
	if n := r.svc.dropRecorded("tab-1"); n != 0 || len(queueTexts(r, "tab-1")) != 2 {
		t.Fatalf("dropped %d without a transcript", n)
	}
}

func TestQueueDeliveryDoesNotTypeAMessageTheAgentHolds(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{TranscriptPath: "/t.jsonl"})
	now := r.clock.Now()
	stamp := func(d time.Duration) string { return now.Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }
	var lines []string
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return lines, true }

	_, _ = r.svc.Queue().Add("tab-1", "уже у агента", nil)
	_, _ = r.svc.Queue().Add("tab-1", "ещё нет", nil)
	r.clock.Sleep(time.Second)
	// The first went into the running turn: an attachment and a remove, no user record.
	lines = []string{
		enqueueLine("уже у агента", stamp(100*time.Millisecond)),
		record(map[string]any{"type": "queue-operation", "operation": "remove", "content": "уже у агента", "timestamp": stamp(time.Second)}),
		record(map[string]any{"type": "attachment", "timestamp": stamp(100 * time.Millisecond), "attachment": map[string]any{
			"type": "queued_command", "prompt": "уже у агента", "commandMode": "prompt", "timestamp": stamp(100 * time.Millisecond)}}),
	}
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:ещё нет"}) {
		t.Fatalf("typed %v", got)
	}
	if left := queueTexts(r, "tab-1"); len(left) != 0 {
		t.Fatalf("still queued: %v", left)
	}
}

func TestWatchdogLookCleansAWorkingAgentsQueue(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy, TranscriptPath: "/t.jsonl"})
	now := r.clock.Now()
	stamp := func(d time.Duration) string { return now.Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }
	var lines []string
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return lines, true }
	_, _ = r.svc.Queue().Add("tab-1", "потом", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if len(queueTexts(r, "tab-1")) != 1 {
		t.Fatal("a working agent's message left the queue without a record")
	}
	// The agent took it mid-turn; the keeper's watchdog looks again while the agent still works.
	lines = []string{userLine("потом", stamp(2*time.Second))}
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if len(queueTexts(r, "tab-1")) != 0 || len(r.panes.sent()) != 0 {
		t.Fatalf("queue %v, typed %v", queueTexts(r, "tab-1"), r.panes.sent())
	}
}

func TestSteerAnswersDeliveredForAMessageTheAgentHolds(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy, TranscriptPath: "/t.jsonl"})
	now := r.clock.Now()
	stamp := func(d time.Duration) string { return now.Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }
	item, _ := r.svc.Queue().Add("tab-1", "сразу", nil)
	lines := []string{enqueueLine("сразу", stamp(time.Second))}
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return lines, true }
	r.clock.Sleep(2 * time.Second)

	res := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "steer", "id": item.ID}))
	got := asJSON(t, res)
	if res.Status != 200 || got["ok"] != true || got["outcome"] != SteerDelivered {
		t.Fatalf("steer: %d %v", res.Status, got)
	}
	if len(r.panes.sent()) != 0 || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("typed %v, queued %v", r.panes.sent(), queueTexts(r, "tab-1"))
	}
}

func TestSteerOfAFreshMessageSaysSent(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{CliState: StateIdle})
	item, _ := r.svc.Queue().Add("tab-1", "сейчас", nil)
	res := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "steer", "id": item.ID}))
	if got := asJSON(t, res); res.Status != 200 || got["outcome"] != SteerSent {
		t.Fatalf("steer: %d %v", res.Status, got)
	}
	if !strings.HasPrefix(strings.Join(r.panes.sent(), "|"), "line:сейчас") {
		t.Fatalf("typed %v", r.panes.sent())
	}
}

func TestFailedTryThatReachedTheAgentIsNotQueuedAgain(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{TranscriptPath: "/t.jsonl"})
	_, _ = r.svc.Queue().Add("tab-1", "дошло", nil)
	// The keys died with tmux, but the agent had the text already.
	lines := []string{userLine("дошло", r.clock.Now().Add(time.Second).UTC().Format("2006-01-02T15:04:05.000Z"))}
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return lines, true }
	r.panes.failOn = "line:"
	tab, _ := r.tabs.Tab("tab-1")
	out := r.svc.Pump("tab-1", true, r.svc.deliveryDriver(tab))
	if out != PumpSent || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("out %v, queued again: %v", out, queueTexts(r, "tab-1"))
	}
	// The same failure with nothing in the transcript puts the message back.
	_, _ = r.svc.Queue().Add("tab-1", "не дошло", nil)
	lines = nil
	if out := r.svc.Pump("tab-1", true, r.svc.deliveryDriver(tab)); out != PumpFailed || !reflect.DeepEqual(queueTexts(r, "tab-1"), []string{"не дошло"}) {
		t.Fatalf("out %v, queue %v", out, queueTexts(r, "tab-1"))
	}
}
