package prompts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// recordingDriver is a delivery driver over a fixed screen that records what it typed.
type recordingDriver struct {
	screen    string
	captured  bool
	confirmed bool
	calls     []string
	slept     int
	pasteErr  error
}

func newDriver(screen string) *recordingDriver {
	return &recordingDriver{screen: screen, captured: true, confirmed: true}
}

func (d *recordingDriver) Capture() (string, bool) { return d.screen, d.captured }
func (d *recordingDriver) PasteImages(paths []string) bool {
	d.calls = append(d.calls, "images:"+strings.Join(paths, ","))
	return d.confirmed
}
func (d *recordingDriver) PasteText(text string) error {
	if d.pasteErr != nil {
		return d.pasteErr
	}
	d.calls = append(d.calls, "text:"+text)
	return nil
}
func (d *recordingDriver) Enter() error        { d.calls = append(d.calls, "enter"); return nil }
func (d *recordingDriver) Sleep(time.Duration) { d.slept++ }

// lineDriver can type a short line too.
type lineDriver struct{ *recordingDriver }

func (d lineDriver) TypeLine(text string) error { d.calls = append(d.calls, "line:"+text); return nil }

func item(over func(*Item)) Item {
	it := Item{ID: "q1", Text: "Как дела", Images: []string{}, CreatedAt: 1}
	if over != nil {
		over(&it)
	}
	return it
}

func TestQueueOrderAndListener(t *testing.T) {
	q := NewQueue("", newFakeClock())
	var seen []string
	q.OnChange(func(tabID string, items []Item) { seen = append(seen, fmt.Sprintf("%s:%d", tabID, len(items))) })
	a, _ := q.Add("t1", "Привет", nil)
	_, _ = q.Add("t1", "Как дела", nil)
	_, _ = q.Add("t2", "Другая вкладка", nil)
	texts := func(id string) []string {
		var out []string
		for _, it := range q.List(id) {
			out = append(out, it.Text)
		}
		return out
	}
	if !reflect.DeepEqual(texts("t1"), []string{"Привет", "Как дела"}) {
		t.Fatalf("order: %v", texts("t1"))
	}
	if got, ok := q.Take("t1", a.ID); !ok || got.Text != "Привет" {
		t.Fatalf("take: %+v %v", got, ok)
	}
	if _, ok := q.Take("t1", a.ID); ok {
		t.Fatal("taken twice")
	}
	q.Restore("t1", a)
	q.Restore("t1", a) // already queued: nothing changes
	if !reflect.DeepEqual(texts("t1"), []string{"Привет", "Как дела"}) {
		t.Fatalf("restore: %v", texts("t1"))
	}
	want := []string{"t1:1", "t1:2", "t2:1", "t1:1", "t1:2"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("listener: %v", seen)
	}
	if !strings.HasPrefix(a.ID, "q-") || len(strings.Split(a.ID, "-")) != 3 {
		t.Errorf("id %q", a.ID)
	}
}

func TestQueueRefusals(t *testing.T) {
	q := NewQueue("", newFakeClock())
	var qe *QueueError
	if _, err := q.Add("t1", "   ", nil); !errors.As(err, &qe) || qe.Message != "Пустое сообщение" {
		t.Fatalf("blank: %v", err)
	}
	if _, err := q.Add("t1", " ", []string{"/uploads/a.png"}); err != nil {
		t.Fatalf("a picture alone is a message: %v", err)
	}
	for i := 1; i < MaxQueuedPerTab; i++ {
		if _, err := q.Add("t1", fmt.Sprintf("m%d", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.Add("t1", "one more", nil); !errors.As(err, &qe) || qe.Message != "В очереди уже слишком много сообщений" {
		t.Fatalf("full: %v", err)
	}
	long, _ := q.Add("t2", strings.Repeat("ж", MaxQueuedText+5), nil)
	if n := len([]rune(long.Text)); n != MaxQueuedText {
		t.Errorf("long text kept %d characters", n)
	}
}

func TestQueueFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state", "prompt-queue.json")
	clock := newFakeClock()
	q := NewQueue(file, clock)
	a, _ := q.Add("t1", "первое", []string{"/uploads/a.png"})
	if _, err := os.Stat(file); err == nil {
		t.Fatal("written before the debounce")
	}
	clock.Advance(saveDelay)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string][]Item
	if json.Unmarshal(raw, &data) != nil || len(data["t1"]) != 1 || data["t1"][0].ID != a.ID {
		t.Fatalf("file: %s", raw)
	}
	if !strings.Contains(string(raw), "\n  \"t1\": [") {
		t.Errorf("not indented by two spaces: %s", raw)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}

	// A broken entry is dropped, the rest loads.
	_ = os.WriteFile(file, []byte(`{"t1":[{"id":"x","text":"ok","images":[],"createdAt":5},{"id":1,"text":"bad"}],"t2":"nope"}`), 0o600)
	again := NewQueue(file, clock)
	if l := again.List("t1"); len(l) != 1 || l[0].ID != "x" || l[0].CreatedAt != 5 {
		t.Fatalf("loaded %+v", l)
	}
	if len(again.All()) != 1 {
		t.Errorf("all: %v", again.All())
	}

	// Dropping a closed tab's queue tells the pages it is empty and writes it out.
	var told []int
	again.OnChange(func(_ string, items []Item) { told = append(told, len(items)) })
	again.Drop("t1")
	again.Drop("t1")
	again.Flush()
	if !reflect.DeepEqual(told, []int{0}) || len(again.All()) != 0 {
		t.Fatalf("drop: %v %v", told, again.All())
	}
	if raw, _ := os.ReadFile(file); strings.TrimSpace(string(raw)) != "{}" {
		t.Errorf("after drop: %s", raw)
	}
}

func TestDeliverable(t *testing.T) {
	cases := []struct {
		s    *DeliveryScreen
		want Decision
	}{
		{nil, DecisionWait},
		{&DeliveryScreen{Busy: true, InputLine: true}, DecisionWait},
		{&DeliveryScreen{Menu: true, InputLine: true}, DecisionBlocked},
		{&DeliveryScreen{Input: "черновик", InputLine: true}, DecisionBlocked},
		{&DeliveryScreen{InputLine: true}, DecisionSend},
		{&DeliveryScreen{}, DecisionWait},
	}
	for i, c := range cases {
		if got := Deliverable(c.s); got != c.want {
			t.Errorf("%d: %v", i, got)
		}
	}
}

func TestDeliverIdleAndBusy(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	idle := newDriver(tui(t, "claude-idle-placeholder.ansi.txt"))
	if got := Deliver(item(nil), idle, reg, false, ""); got != OutcomeSent || !reflect.DeepEqual(idle.calls, []string{"text:Как дела", "enter"}) {
		t.Fatalf("idle: %v %v", got, idle.calls)
	}
	busy := newDriver(tui(t, "claude-busy-spinner.ansi.txt"))
	if got := Deliver(item(func(i *Item) { i.ID = "q2" }), busy, reg, false, ""); got != OutcomeWait || len(busy.calls) != 0 {
		t.Fatalf("busy: %v %v", got, busy.calls)
	}
	none := newDriver("")
	none.captured = false
	if got := Deliver(item(func(i *Item) { i.ID = "q3" }), none, reg, false, ""); got != OutcomeWait {
		t.Fatalf("no screen: %v", got)
	}
}

func TestSteerIgnoresBusyNotDraft(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	busy := newDriver(tui(t, "claude-busy-spinner.ansi.txt"))
	if got := Deliver(item(nil), busy, reg, true, ""); got != OutcomeSent {
		t.Fatalf("busy steer: %v", got)
	}
	draft := newDriver(tui(t, "claude-input-draft.ansi.txt"))
	if got := Deliver(item(func(i *Item) { i.ID = "q2" }), draft, reg, true, ""); got != OutcomeBlocked {
		t.Fatalf("draft steer: %v", got)
	}
}

func pictureBox(input string) string {
	return strings.Join([]string{"● Готово.", "", strings.Repeat("─", 60), "❯ " + input, strings.Repeat("─", 60), "  ⏵⏵ bypass permissions on"}, "\n")
}

func TestOwnPicturesLeftInTheBox(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	left := newDriver(pictureBox("[Image #1] "))
	if got := Deliver(item(func(i *Item) { i.Images = []string{"/u/a.png"} }), left, reg, true, ""); got != OutcomeSent ||
		!reflect.DeepEqual(left.calls, []string{"text: Как дела", "enter"}) {
		t.Fatalf("own pictures: %v %v", got, left.calls)
	}
	foreign := newDriver(pictureBox("[Image #1] "))
	if got := Deliver(item(func(i *Item) { i.ID = "q2" }), foreign, reg, true, ""); got != OutcomeBlocked {
		t.Fatalf("foreign pictures: %v", got)
	}
}

func TestPicturesFirstAndGiveUp(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	okd := newDriver(idle)
	if got := Deliver(item(func(i *Item) { i.Images = []string{"/u/a.png"} }), okd, reg, false, ""); got != OutcomeSent ||
		!reflect.DeepEqual(okd.calls, []string{"images:/u/a.png", "text: Как дела", "enter"}) {
		t.Fatalf("pictures: %v %v", got, okd.calls)
	}
	lost := newDriver(idle)
	lost.confirmed = false
	if got := Deliver(item(func(i *Item) { i.ID = "q2"; i.Images = []string{"/u/a.png"} }), lost, reg, false, ""); got != OutcomeFailed ||
		!reflect.DeepEqual(lost.calls, []string{"images:/u/a.png"}) {
		t.Fatalf("lost pictures: %v %v", got, lost.calls)
	}
}

func TestShortLineTypedWithItsEnter(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	cases := []struct {
		item  Item
		calls []string
		slept bool
	}{
		// The typed line skips the paste's settle; it still waits to see the message leave the input line.
		{item(nil), []string{"line:Как дела"}, true},
		{item(func(i *Item) { i.ID = "q-two"; i.Text = "первая\nвторая" }), []string{"text:первая\nвторая", "enter"}, true},
		{item(func(i *Item) { i.ID = "q-mention"; i.Text = "посмотри @src/lib" }), []string{"text:посмотри @src/lib", "enter"}, true},
		{item(func(i *Item) { i.ID = "q-pictures"; i.Images = []string{"/u/a.png"} }), []string{"images:/u/a.png", "text: Как дела", "enter"}, true},
	}
	for _, c := range cases {
		d := lineDriver{newDriver(idle)}
		if got := Deliver(c.item, d, reg, false, ""); got != OutcomeSent || !reflect.DeepEqual(d.calls, c.calls) || (d.slept > 0) != c.slept {
			t.Errorf("%s: %v %v slept %d", c.item.ID, got, d.calls, d.slept)
		}
	}
}

func TestNeverTypedTwiceWithinTheWindow(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	pump := newDriver(idle)
	if Deliver(item(nil), pump, reg, false, "t1") != OutcomeSent {
		t.Fatal("pump")
	}
	steer := newDriver(tui(t, "claude-busy-spinner.ansi.txt"))
	if got := Deliver(item(nil), steer, reg, true, "t1"); got != OutcomeSent || len(steer.calls) != 0 {
		t.Fatalf("second path typed again: %v %v", got, steer.calls)
	}
	lost := newDriver(idle)
	lost.confirmed = false
	if Deliver(item(func(i *Item) { i.ID = "q3"; i.Images = []string{"/u/a.png"} }), lost, reg, false, "") != OutcomeFailed {
		t.Fatal("lost")
	}
	retry := newDriver(idle)
	if got := Deliver(item(func(i *Item) { i.ID = "q3"; i.Images = []string{"/u/a.png"} }), retry, reg, false, ""); got != OutcomeSent ||
		!reflect.DeepEqual(retry.calls, []string{"images:/u/a.png", "text: Как дела", "enter"}) {
		t.Fatalf("retry: %v %v", got, retry.calls)
	}
}

func TestDeliverPasteErrorFails(t *testing.T) {
	d := newDriver(tui(t, "claude-idle-placeholder.ansi.txt"))
	d.pasteErr = errors.New("tmux gone")
	if got := Deliver(item(func(i *Item) { i.Text = "a\nb" }), d, NewRegistry(newFakeClock(), nil), false, ""); got != OutcomeFailed {
		t.Fatalf("got %v", got)
	}
}
