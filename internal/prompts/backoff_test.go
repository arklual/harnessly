package prompts

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// logLines records what the service logs, "level msg on=..." per line.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) log(level, msg string, kv ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := level + " " + msg
	for i := 0; i+1 < len(kv); i += 2 {
		if k := fmt.Sprint(kv[i]); k == "on" || k == "reason" {
			line += fmt.Sprintf(" %s=%v", k, kv[i+1])
		}
	}
	l.lines = append(l.lines, line)
}

func (l *logLines) with(prefix string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

func TestAMessageThatKeepsFailingBacksOffThenFails(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	logs := &logLines{}
	r.svc.cfg.Log = logs.log
	r.panes.failOn = "line:"
	_, _ = r.svc.Queue().Add("tab-1", "не доходит", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	var waits []time.Duration
	for range maxDeliveryTries - 1 {
		next := r.clock.pending()
		if len(next) != 1 {
			t.Fatalf("timers %d after %v", len(next), waits)
		}
		wait := next[0].at.Sub(r.clock.Now())
		waits = append(waits, wait)
		r.clock.Advance(wait)
	}
	want := []time.Duration{1500 * time.Millisecond, 3 * time.Second, 6 * time.Second, 12 * time.Second, 24 * time.Second}
	if !reflect.DeepEqual(waits, want) {
		t.Fatalf("waits %v", waits)
	}
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{"failed:not-delivered"}) {
		t.Fatalf("statuses %v", got)
	}
	if u := lastUpdate(t, r); len(u.Items) != 1 || u.Items[0].Reason != ReasonNotDelivered {
		t.Fatalf("pages told %+v", u)
	}
	if got := len(logs.with("warn queued prompt did not reach the agent; it stays")); got != maxDeliveryTries-1 {
		t.Errorf("tries logged %d", got)
	}
	if got := logs.with("warn queued prompt did not reach the agent; given up"); len(got) != 1 {
		t.Errorf("give up logged %v", got)
	}
	// Retried by the person, it gets all its tries again and goes once typing works.
	r.panes.failOn = ""
	r.panes.onSend = func(call string) {
		if strings.HasPrefix(call, "line:") {
			r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	id := r.svc.Queue().List("tab-1")[0].ID
	if _, ok := r.svc.Retry("tab-1", id); !ok {
		t.Fatal("not retried")
	}
	r.clock.Advance(0)
	if got := r.svc.Queue().List("tab-1"); len(got) != 0 {
		t.Fatalf("left %+v", got)
	}
}

func TestATriedAgainMessageStartsItsBackoffOver(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	r.panes.failOn = "line:"
	_, _ = r.svc.Queue().Add("tab-1", "первое", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	r.clock.Advance(1500 * time.Millisecond)
	// It went on the third try: the next message starts from the shortest wait, not the first one's.
	r.panes.failOn = ""
	r.clock.Advance(3 * time.Second)
	if got := queueTexts(r, "tab-1"); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
	r.panes.failOn = "line:"
	_, _ = r.svc.Queue().Add("tab-1", "второе", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if next := r.clock.pending(); len(next) != 1 || next[0].at.Sub(r.clock.Now()) != retryDelay {
		t.Fatalf("next %v", next)
	}
}

func TestSomeoneElsesTextInTheBoxFailsTheQueueAfterAWhile(t *testing.T) {
	r := newRig(t, tui(t, "claude-input-draft.ansi.txt"), Tab{})
	logs := &logLines{}
	r.svc.cfg.Log = logs.log
	_, _ = r.svc.Queue().Add("tab-1", "первое", nil)
	_, _ = r.svc.Queue().Add("tab-1", "второе", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	r.clock.Advance(occupiedLimit - blockedRetry)
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{":", ":"}) {
		t.Fatalf("given up early %v", got)
	}
	r.clock.Advance(blockedRetry)
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{"failed:input-occupied", "failed:input-occupied"}) {
		t.Fatalf("statuses %v", got)
	}
	if len(r.panes.sent()) != 0 {
		t.Errorf("typed over the draft: %v", r.panes.sent())
	}
	// The wait is told once, not on every look.
	if got := logs.with("info queued prompt waits"); !reflect.DeepEqual(got, []string{"info queued prompt waits on=blocked"}) {
		t.Errorf("logged %v", got)
	}
}

func TestAMenuInTheBoxIsWaitedOutWithoutGivingUp(t *testing.T) {
	r := newRig(t, tui(t, "claude-switch-model-confirm.txt"), Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "после меню", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	r.clock.Advance(2 * occupiedLimit)
	if got := statuses(r, "tab-1"); !reflect.DeepEqual(got, []string{":"}) {
		t.Fatalf("statuses %v", got)
	}
}

// picturesThenText is a pane the pictures go into (their references show in the box, wrapped as on the live screen)
// and whose text paste is refused while failText is set; an Enter with the text in empties the box.
func picturesThenText(t *testing.T, r *rig, failText *bool) {
	t.Helper()
	refs := wrappedPictureBox("[Image #100] [Image #101] [Image #102] [Image #103]", "[Image #104]")
	r.panes.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "paste:/uploads/"):
			r.panes.setScreen(refs)
		case call == "keys:Enter":
			r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	r.panes.mu.Lock()
	r.panes.failOn = "paste: длинный"
	if !*failText {
		r.panes.failOn = ""
	}
	r.panes.mu.Unlock()
}

func TestPicturesThenALongTextThatFailsGetOnlyTheTextOnTheRetry(t *testing.T) {
	paths := []string{"/uploads/a.png", "/uploads/b.png", "/uploads/c.png", "/uploads/d.png", "/uploads/e.png"}
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	fail := true
	picturesThenText(t, r, &fail)
	_, _ = r.svc.Queue().Add("tab-1", long, paths)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	pictures := 0
	for _, call := range r.panes.sent() {
		if strings.HasPrefix(call, "paste:/uploads/") {
			pictures++
		}
	}
	if pictures == 0 || len(queueTexts(r, "tab-1")) != 1 {
		t.Fatalf("first try %v", r.panes.sent())
	}
	// The text goes on the next try, after the five pictures it left: no picture pasted twice.
	fail = false
	picturesThenText(t, r, &fail)
	before := len(r.panes.sent())
	r.clock.Advance(retryDelay)
	retry := r.panes.sent()[before:]
	for _, call := range retry {
		if strings.HasPrefix(call, "paste:/uploads/") {
			t.Fatalf("pictures pasted again: %v", retry)
		}
	}
	if len(retry) < 2 || retry[0] != "paste: "+long || retry[len(retry)-1] != "keys:Enter" {
		t.Fatalf("retry %v", retry)
	}
	if got := queueTexts(r, "tab-1"); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
}

func TestARestartedRuntimeFinishesAMessageItsPicturesLeft(t *testing.T) {
	paths := []string{"/uploads/a.png", "/uploads/b.png", "/uploads/c.png", "/uploads/d.png", "/uploads/e.png"}
	long := "длинный текст\n" + strings.Repeat("x", 2300)
	file := filepath.Join(t.TempDir(), "prompt-queue.json")
	q := NewQueue(file, newFakeClock())
	_, _ = q.Add("tab-1", long, paths)
	q.Flush()
	// The new runtime: its own service on the same file, the screen the old one left, the keeper's first look.
	r := newRig(t, wrappedPictureBox("[Image #100] [Image #101] [Image #102] [Image #103]", "[Image #104]"), Tab{})
	r.svc = New(Config{
		Panes: r.panes, Tabs: r.tabs, Agents: r.agents, Events: r.events, Broadcast: r.bc, Clock: r.clock, QueueFile: file,
		IsUploadImage: func(p string) bool { return strings.HasPrefix(p, "/uploads/") },
		Procs:         r.svc.cfg.Procs,
	})
	fail := false
	picturesThenText(t, r, &fail)
	if got := queueTexts(r, "tab-1"); len(got) != 1 {
		t.Fatalf("loaded %v", got)
	}
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	sent := r.panes.sent()
	if len(sent) < 2 || sent[0] != "paste: "+long || sent[len(sent)-1] != "keys:Enter" {
		t.Fatalf("sent %v", sent)
	}
	if got := queueTexts(r, "tab-1"); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
}
