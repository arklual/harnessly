package prompts

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func statuses(r *rig, tabID string) []string {
	out := []string{}
	for _, it := range r.svc.Queue().List(tabID) {
		out = append(out, it.Status+":"+it.Reason)
	}
	return out
}

func lastUpdate(t *testing.T, r *rig) QueueUpdate {
	t.Helper()
	r.bc.mu.Lock()
	defer r.bc.mu.Unlock()
	for i := len(r.bc.msgs) - 1; i >= 0; i-- {
		if u, ok := r.bc.msgs[i].(QueueUpdate); ok {
			return u
		}
	}
	t.Fatal("no queue update")
	return QueueUpdate{}
}

func TestALaunchThatGaveUpFailsItsQueue(t *testing.T) {
	r := newRig(t, "agent@host:~$ ", Tab{LaunchFailed: true, HasAgentSession: true})
	r.agents.running = false
	_, _ = r.svc.Queue().Add("tab-1", "первое", nil)
	_, _ = r.svc.Queue().Add("tab-1", "второе", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{"failed:launch-failed", "failed:launch-failed"}) {
		t.Fatalf("statuses %v", got)
	}
	if u := lastUpdate(t, r); len(u.Items) != 2 || u.Items[0].Status != StatusFailed || u.Items[0].Reason != ReasonLaunchFailed {
		t.Fatalf("pages told %+v", u)
	}
	// Given up, the queue is not looked at again: the watchdog's next look schedules nothing, nothing is resumed.
	r.svc.Schedule("tab-1", 0)
	if len(r.clock.pending()) != 0 || r.agents.resumed != 0 || len(r.panes.sent()) != 0 {
		t.Fatalf("still retried: %v resumed %d", r.clock.pending(), r.agents.resumed)
	}
	// A message the person sends afterwards is told the same at once.
	_, _ = r.svc.Queue().Add("tab-1", "третье", nil)
	r.svc.TakeNewPrompt("tab-1", "", false)
	r.clock.Advance(0)
	if got := statuses(r, "tab-1"); len(got) != 3 || got[2] != "failed:launch-failed" {
		t.Fatalf("new message %v", got)
	}
}

func TestNoSessionToResumeFailsTheQueue(t *testing.T) {
	r := newRig(t, "agent@host:~$ ", Tab{})
	r.agents.running = false
	_, _ = r.svc.Queue().Add("tab-1", "x", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{"failed:no-session"}) || !reflect.DeepEqual(r.agents.gaveUp, []string{"tab-1"}) {
		t.Fatalf("statuses %v, gave up %v", got, r.agents.gaveUp)
	}
	if len(r.clock.pending()) != 0 {
		t.Fatal("retried")
	}
}

func TestATabNobodyKnowsFailsItsQueueAfterAGrace(t *testing.T) {
	r := newRig(t, "", Tab{})
	_, _ = r.svc.Queue().Add("tab-gone", "x", nil)
	r.svc.Schedule("tab-gone", 0)
	r.clock.Advance(0)
	if got := statuses(r, "tab-gone"); !reflect.DeepEqual(got, []string{":"}) {
		t.Fatalf("failed at the first look: %v", got)
	}
	r.clock.Advance(noTabGrace)
	if got := statuses(r, "tab-gone"); !reflect.DeepEqual(got, []string{"failed:no-tab"}) || len(r.clock.pending()) != 0 {
		t.Fatalf("after the grace %v, pending %v", got, r.clock.pending())
	}
	// A tab found again within the grace starts its count over.
	r2 := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy})
	saved, _ := r2.tabs.Tab("tab-1")
	_, _ = r2.svc.Queue().Add("tab-1", "x", nil)
	r2.tabs.set("tab-1", func(tab *Tab) { tab.Session = "" })
	r2.svc.Schedule("tab-1", 0)
	r2.clock.Advance(0)
	r2.tabs.set("tab-1", func(tab *Tab) { *tab = saved })
	r2.clock.Advance(noTabGrace)
	r2.tabs.set("tab-1", func(tab *Tab) { tab.Session = "" })
	r2.svc.Schedule("tab-1", 0)
	r2.clock.Advance(0)
	if got := statuses(r2, "tab-1"); !reflect.DeepEqual(got, []string{":"}) {
		t.Fatalf("the count did not start over: %v", got)
	}
}

func TestAMessageWaitingADayExpires(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy})
	_, _ = r.svc.Queue().Add("tab-1", "старое", nil)
	r.clock.Sleep(QueuedLifetime / 2)
	_, _ = r.svc.Queue().Add("tab-1", "новое", nil)
	r.clock.Sleep(QueuedLifetime/2 + time.Millisecond)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{"failed:expired", ":"}) {
		t.Fatalf("statuses %v", got)
	}
}

func TestPumpPassesOverAFailedMessage(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	q := r.svc.Queue()
	first, _ := q.Add("tab-1", "упало", nil)
	q.Fail("tab-1", ReasonLaunchFailed, nil)
	_, _ = q.Add("tab-1", "дальше", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:дальше"}) || !reflect.DeepEqual(queueTexts(r, "tab-1"), []string{"упало"}) {
		t.Fatalf("sent %v, left %v", got, queueTexts(r, "tab-1"))
	}
	// A message the screen refused goes back before the next waiting one, behind the failed ones.
	_, _ = q.Add("tab-1", "b", nil)
	c, _ := q.Add("tab-1", "c", nil)
	taken, _ := q.Take("tab-1", c.ID)
	q.Restore("tab-1", taken)
	if got := queueTexts(r, "tab-1"); !reflect.DeepEqual(got, []string{"упало", "c", "b"}) || q.List("tab-1")[0].ID != first.ID {
		t.Fatalf("order %v", got)
	}
}

func TestRetryTriesAFailedLaunchAgain(t *testing.T) {
	r := newRig(t, "agent@host:~$ ", Tab{LaunchFailed: true, HasAgentSession: true})
	r.agents.running = false
	it, _ := r.svc.Queue().Add("tab-1", "ещё раз", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	r.clock.Sleep(time.Hour)
	res := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "retry", "id": it.ID}))
	got := asJSON(t, res)
	item, _ := got["item"].(map[string]any)
	if res.Status != 200 || item["status"] != nil || item["retriedAt"] != float64(r.clock.Now().UnixMilli()) {
		t.Fatalf("retry: %d %v", res.Status, got)
	}
	r.clock.Advance(0)
	if r.agents.resumed != 1 || !reflect.DeepEqual(statuses(r, "tab-1"), []string{":"}) {
		t.Fatalf("resumed %d, statuses %v", r.agents.resumed, statuses(r, "tab-1"))
	}
	// The retried message's lifetime counts from the retry.
	r.svc.expire("tab-1")
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{":"}) {
		t.Fatalf("expired after a retry: %v", got)
	}
	again := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "retry", "id": it.ID}))
	if again.Status != 200 {
		t.Fatalf("a retry of a waiting message: %d %v", again.Status, asJSON(t, again))
	}
	gone := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "retry", "id": "q-none"}))
	if got := asJSON(t, gone); gone.Status != 404 || got["code"] != CodeNotQueued {
		t.Fatalf("retry of nothing: %d %v", gone.Status, got)
	}
}

func TestNotQueuedHasACode(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	for _, action := range []string{"remove", "take", "steer"} {
		res := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": action, "id": "q-none"}))
		if got := asJSON(t, res); got["code"] != CodeNotQueued || got["error"] != msgNotQueued.String() {
			t.Errorf("%s: %d %v", action, res.Status, got)
		}
	}
}

func TestAnAddRepeatingItsClientIDQueuesOnce(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy})
	add := func(clientID string) map[string]any {
		res := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "один раз", "images": []any{}, "clientId": clientID}))
		if res.Status != 200 {
			t.Fatalf("add: %d %v", res.Status, asJSON(t, res))
		}
		return asJSON(t, res)
	}
	first := add("c-1")
	second := add("c-1")
	firstItem, secondItem := first["item"].(map[string]any), second["item"].(map[string]any)
	if firstItem["id"] != secondItem["id"] || second["repeated"] != true || first["repeated"] != nil || len(queueTexts(r, "tab-1")) != 1 {
		t.Fatalf("first %v, second %v, queue %v", first, second, queueTexts(r, "tab-1"))
	}
	if firstItem["clientId"] != "c-1" {
		t.Errorf("client id not kept: %v", firstItem)
	}
	// It went out meanwhile: a late repeat still queues nothing.
	r.svc.Queue().Take("tab-1", firstItem["id"].(string))
	if late := add("c-1"); late["repeated"] != true || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("late repeat %v, queue %v", late, queueTexts(r, "tab-1"))
	}
	// Past the window, and for other ids, an add is a new message.
	r.clock.Sleep(clientIDWindow)
	if again := add("c-1"); again["repeated"] != nil {
		t.Fatalf("remembered past the window: %v", again)
	}
	if other := add("c-2"); other["repeated"] != nil || len(queueTexts(r, "tab-1")) != 2 {
		t.Fatalf("another id: %v", other)
	}
	// One the person took back is new when it is sent again.
	taken := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "take", "id": r.svc.Queue().List("tab-1")[1].ID}))
	if taken.Status != 200 {
		t.Fatal("take")
	}
	if back := add("c-2"); back["repeated"] != nil {
		t.Fatalf("taken back, then repeated: %v", back)
	}
	// Without a client id nothing is a repeat.
	before := len(queueTexts(r, "tab-1"))
	add("")
	add("")
	if len(queueTexts(r, "tab-1")) != before+2 {
		t.Fatal("adds without a client id were merged")
	}
}

func TestFailedMessagesSurviveARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "prompt-queue.json")
	clock := newFakeClock()
	q := NewQueue(file, clock)
	it, _, _ := q.AddOnce("t1", "c-1", "x", nil)
	q.Fail("t1", ReasonNoTab, nil)
	q.Flush()
	raw, _ := os.ReadFile(file)
	if !strings.Contains(string(raw), `"status": "failed"`) || !strings.Contains(string(raw), `"clientId": "c-1"`) {
		t.Fatalf("file %s", raw)
	}
	l := NewQueue(file, clock).List("t1")
	if len(l) != 1 || l[0].ID != it.ID || l[0].Status != StatusFailed || l[0].Reason != ReasonNoTab || l[0].ClientID != "c-1" {
		t.Fatalf("loaded %+v", l)
	}
	// A file from an earlier runtime has none of the new fields; one of a wrong type is ignored, not the entry.
	_ = os.WriteFile(file, []byte(`{"t1":[{"id":"a","text":"x","images":[],"createdAt":1},{"id":"b","text":"y","images":[],"createdAt":2,"status":7,"clientId":false}]}`), 0o600)
	l = NewQueue(file, clock).List("t1")
	if len(l) != 2 || !l[0].Waiting() || !l[1].Waiting() || l[1].ClientID != "" {
		t.Fatalf("loaded %+v", l)
	}
	// A waiting message is written without the new fields, as an earlier runtime wrote it.
	q2 := NewQueue(filepath.Join(t.TempDir(), "q.json"), clock)
	_, _ = q2.Add("t1", "x", nil)
	q2.Flush()
	raw, _ = os.ReadFile(q2.file)
	for _, key := range []string{"status", "reason", "clientId", "retriedAt"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("%s written for a plain message: %s", key, raw)
		}
	}
}

func queueUpdates(r *rig) []QueueUpdate {
	r.bc.mu.Lock()
	defer r.bc.mu.Unlock()
	var out []QueueUpdate
	for _, m := range r.bc.msgs {
		if u, ok := m.(QueueUpdate); ok {
			out = append(out, u)
		}
	}
	return out
}

func TestARefusedTryDoesNotTellThePagesTheMessageLeft(t *testing.T) {
	r := newRig(t, tui(t, "claude-input-draft.ansi.txt"), Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "моё", nil)
	before := len(queueUpdates(r))
	for i := 0; i < 3; i++ {
		r.svc.Schedule("tab-1", 0)
		r.clock.Advance(0)
	}
	if got := queueUpdates(r)[before:]; len(got) != 0 {
		t.Fatalf("pages told %+v", got)
	}
	// The one that goes in is told once, gone.
	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if got := queueUpdates(r)[before:]; len(got) != 1 || len(got[0].Items) != 0 {
		t.Fatalf("pages told %+v", got)
	}
}
