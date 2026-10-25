package typist

import (
	"reflect"
	"strings"
	"testing"
)

// The box is cleared of our long text row by row (ctrl+u takes one row), looking after every key.
func TestClearOwnGoesRowByRow(t *testing.T) {
	msg := strings.Repeat("word ", 30) + "end"
	rows := []string{msg[:60], msg[60:130], msg[130:]}
	p := newPane(withInput(t, strings.Join(rows, "\n")))
	p.onSend = func(call string) {
		if call == "keys:C-u" {
			rows = rows[:len(rows)-1]
			p.set(withInput(t, strings.Join(rows, "\n")))
		}
	}
	if !ClearOwn(p, newClock(), claude, msg, false) || len(p.sent()) != 3 {
		t.Fatalf("%v", p.sent())
	}
}

// The person starts typing while the box is cleared: their text is not ours, nothing more is pressed.
func TestClearOwnStopsAtThePersonsText(t *testing.T) {
	msg := strings.Repeat("word ", 30) + "end"
	p := newPane(withInput(t, msg[:60]+"\n"+msg[60:]))
	p.onSend = func(string) { p.set(withInput(t, msg[:60]+" и моё")) }
	if ClearOwn(p, newClock(), claude, msg, false) || len(p.sent()) != 1 {
		t.Fatalf("%v", p.sent())
	}
	p = newPane(withInput(t, "a draft of the person"))
	if ClearOwn(p, newClock(), claude, msg, false) || len(p.sent()) != 0 {
		t.Fatalf("%v", p.sent())
	}
}

// A «/model sonnet» of ours that did not go through, left in the box: the next message clears it, then goes.
func TestAHarnessCommandLeftInTheBoxIsClearedFirst(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p := newPane(withInput(t, "/model sonnet"))
	p.onSend = func(call string) {
		if call == "keys:C-u" || strings.HasPrefix(call, "line:") {
			p.set(idle)
		}
	}
	res := Type(Message{Text: "hello there"}, claude, p, newClock())
	if res.Outcome != Sent || !reflect.DeepEqual(p.sent(), []string{"keys:C-u", "line:hello there"}) {
		t.Fatalf("%+v %v", res.Outcome, p.sent())
	}
	// A slash-looking draft that is not a setting command of ours is the person's.
	p = newPane(withInput(t, "/review this branch"))
	if res := Type(Message{Text: "hello there"}, claude, p, newClock()); res.Outcome != BlockedText || len(p.sent()) != 0 {
		t.Fatalf("%+v %v", res.Outcome, p.sent())
	}
}

// A setting command that never says it took effect does not stay in the box; the next one clears an old one first.
func TestASlashCommandThatDidNotGoThroughLeavesTheBoxClean(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	p := newPane(idle)
	p.onSend = func(call string) {
		switch {
		case strings.HasPrefix(call, "line:"):
			p.set(withInput(t, strings.TrimPrefix(call, "line:")))
		case call == "keys:C-u":
			p.set(idle)
		}
	}
	if err := SetModel(p, newClock(), "sonnet"); err != ErrCommandNotApplied {
		t.Fatal(err)
	}
	if got := p.sent(); !reflect.DeepEqual(got, []string{"line:/model sonnet", "keys:C-u"}) {
		t.Fatalf("%v", got)
	}
	p.calls = nil
	p.set(withInput(t, "/effort high"))
	p.onSend = func(call string) {
		switch call {
		case "keys:C-u":
			p.set(idle)
		case "line:/model sonnet":
			p.set(afterLine(t, "Set model to Sonnet 5"))
		}
	}
	if err := SetModel(p, newClock(), "sonnet"); err != nil {
		t.Fatal(err)
	}
	if got := p.sent(); !reflect.DeepEqual(got, []string{"keys:C-u", "line:/model sonnet"}) {
		t.Fatalf("%v", got)
	}
}
