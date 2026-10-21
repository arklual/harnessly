package prompts

import (
	"reflect"
	"strings"
	"testing"
)

// swallowingDriver is an idle Claude pane whose input line keeps a typed message until the agent has taken `lost`
// Enters more than it should: the first Enters land while it redraws and are swallowed.
type swallowingDriver struct {
	*recordingDriver
	idle, stuck string
	lost        int
	typed       bool
	enters      int
}

func (d *swallowingDriver) TypeLine(text string) error {
	d.calls = append(d.calls, "line:"+text)
	d.typed = true
	d.enters++
	return nil
}

func (d *swallowingDriver) Enter() error {
	d.calls = append(d.calls, "enter")
	d.enters++
	return nil
}

func (d *swallowingDriver) Capture() (string, bool) {
	if d.typed && d.enters <= d.lost {
		return d.stuck, true
	}
	return d.idle, true
}

func withInput(t *testing.T, text string) string {
	t.Helper()
	draft := tui(t, "claude-input-draft.ansi.txt")
	if !strings.Contains(draft, "half-typed text") {
		t.Fatal("the draft fixture changed")
	}
	return strings.Replace(draft, "half-typed text", text, 1)
}

func TestALostEnterIsPressedAgain(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	d := &swallowingDriver{recordingDriver: newDriver(""), idle: tui(t, "claude-idle-placeholder.ansi.txt"), stuck: withInput(t, "Как дела"), lost: 1}
	if got := Deliver(item(nil), d, reg, false, ""); got != OutcomeSent || !reflect.DeepEqual(d.calls, []string{"line:Как дела", "enter"}) {
		t.Fatalf("%v %v", got, d.calls)
	}
}

func TestAMessageStuckInTheInputIsNotCountedAsSent(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	d := &swallowingDriver{recordingDriver: newDriver(""), idle: tui(t, "claude-idle-placeholder.ansi.txt"), stuck: withInput(t, "Как дела"), lost: 100}
	if got := Deliver(item(nil), d, reg, false, ""); got != OutcomeFailed || !reflect.DeepEqual(d.calls, []string{"line:Как дела", "enter", "enter"}) {
		t.Fatalf("stuck: %v %v", got, d.calls)
	}
	// The next try finds the text as its own: it presses Enter, never types the message a second time.
	d.calls, d.lost = nil, d.enters
	if got := Deliver(item(nil), d, reg, false, ""); got != OutcomeSent || !reflect.DeepEqual(d.calls, []string{"enter"}) {
		t.Fatalf("retry: %v %v", got, d.calls)
	}
}

func TestSomeoneElsesDraftStillBlocks(t *testing.T) {
	reg := NewRegistry(newFakeClock(), nil)
	d := lineDriver{newDriver(withInput(t, "half-typed text"))}
	if got := Deliver(item(func(i *Item) { i.Text = "x" }), d, reg, false, ""); got != OutcomeBlocked || len(d.calls) != 0 {
		t.Fatalf("%v %v", got, d.calls)
	}
}

func TestAnAgentOfUnknownStateTakesTheQueueWhenItsScreenIsReady(t *testing.T) {
	deliver := func(screen, foreground string) ([]string, int) {
		r := newRig(t, tui(t, screen), Tab{CliState: StateUnknown})
		r.panes.mu.Lock()
		r.panes.current = foreground
		r.panes.mu.Unlock()
		it, _ := r.svc.Queue().Add("tab-1", "сразу", nil)
		r.svc.TakeNewPrompt("tab-1", it.ID, false)
		r.clock.Advance(0)
		return r.panes.sent(), len(queueTexts(r, "tab-1"))
	}
	if sent, left := deliver("claude-idle-placeholder.ansi.txt", "claude"); !reflect.DeepEqual(sent, []string{"line:сразу"}) || left != 0 {
		t.Fatalf("agent ready: sent %v, left %d", sent, left)
	}
	// A working screen still waits, whatever the state says.
	if sent, left := deliver("claude-busy-spinner.ansi.txt", "claude"); len(sent) != 0 || left != 1 {
		t.Fatalf("busy screen: sent %v", sent)
	}
	// A bare shell, or a pane tmux says nothing about, never gets the text: it would run as a command.
	for _, shell := range []string{"zsh", "bash", ""} {
		if sent, left := deliver("claude-idle-placeholder.ansi.txt", shell); len(sent) != 0 || left != 1 {
			t.Fatalf("foreground %q: sent %v", shell, sent)
		}
	}
}

// questionScreen is Claude's AskUserQuestion dialog with three options, the cursor on row `at` (3 = «Type something.»,
// 4 = «Chat about this»).
func questionScreen(at int) string {
	rows := []string{"1. Red", "2. Green", "3. Blue", "4. Type something."}
	s := "☐ Color\nPick a color\n"
	for i, r := range rows {
		mark := "  "
		if i == at {
			mark = "❯ "
		}
		s += mark + r + "\n"
	}
	chat := "  5. Chat about this"
	if at == 4 {
		chat = "❯ 5. Chat about this"
	}
	return s + "────────\n" + chat + "\nEnter to select · ↑/↓ to navigate · Esc to cancel\n"
}

// A message written while the agent asks a question is the person's answer in words: the dialog is closed by «Chat
// about this» (the cursor walked to it) and the message goes in, instead of waiting behind the dialog for ever.
func TestAMessageWrittenDuringAQuestionClosesItAndGoesIn(t *testing.T) {
	r := newRig(t, questionScreen(0), Tab{CliState: StateNeedsInput})
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	at := 0
	r.panes.mu.Lock()
	r.panes.onSend = func(call string) {
		switch {
		case call == "keys:Down" && at < 4:
			at++
			r.panes.setScreen(questionScreen(at))
		case call == "keys:Enter" && at == 4:
			at = -1
			r.panes.setScreen(idle)
		}
	}
	r.panes.mu.Unlock()
	it, _ := r.svc.Queue().Add("tab-1", "иконки надо", nil)
	r.svc.TakeNewPrompt("tab-1", it.ID, false)
	r.clock.Advance(0)
	want := []string{"keys:Down", "keys:Down", "keys:Down", "keys:Down", "keys:Enter", "line:иконки надо"}
	if got := r.panes.sent(); !reflect.DeepEqual(got, want) || len(queueTexts(r, "tab-1")) != 0 {
		t.Fatalf("sent %v, queue %v", got, queueTexts(r, "tab-1"))
	}
}

// «Обсудить в чате» on the question card: the same walk, nothing typed.
func TestChatAboutThisFromTheCard(t *testing.T) {
	r := newRig(t, questionScreen(2), Tab{CliState: StateNeedsInput})
	at := 2
	r.panes.mu.Lock()
	r.panes.onSend = func(call string) {
		if call == "keys:Down" && at < 4 {
			at++
			r.panes.setScreen(questionScreen(at))
		}
		if call == "keys:Enter" && at == 4 {
			r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
		}
	}
	r.panes.mu.Unlock()
	if res := r.svc.AnswerQuestion([]byte(`{"session":"` + testSession + `","chat":true}`)); res.Status != 200 {
		t.Fatalf("answer: %+v", res)
	}
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"keys:Down", "keys:Down", "keys:Enter"}) {
		t.Fatalf("sent %v", got)
	}
}
