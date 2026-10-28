package typist

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/screen"
)

func TestALoginLeftOpenInTheTerminalIsClosedBeforeTheMessage(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	for _, file := range []string{"claude-2.1.312-login-method.ansi.txt", "claude-2.1.312-login-code.ansi.txt"} {
		p := newPane(tui(t, file))
		p.onSend = func(call string) {
			if call == "keys:Escape" {
				p.set(idle)
			}
		}
		res := Type(Message{Text: "go on"}, claude, p, newClock())
		if res.Outcome != Sent || res.Cleared != screen.AsideLogin {
			t.Fatalf("%s: %+v %v", file, res, p.sent())
		}
		got := p.sent()
		if got[0] != "keys:Escape" || got[len(got)-1] != "line:go on" {
			t.Fatalf("%s: %v", file, got)
		}
	}
}

func TestAnAsideThatStaysUpGetsTwoEscapesAndNothingIsTyped(t *testing.T) {
	p := newPane(tui(t, "claude-2.1.312-login-method.ansi.txt"))
	res := Type(Message{Text: "go on"}, claude, p, newClock())
	if res.Outcome != BlockedMenu {
		t.Fatalf("%+v", res)
	}
	if got := p.sent(); len(got) != 2 || got[0] != "keys:Escape" || got[1] != "keys:Escape" {
		t.Fatalf("%v", got)
	}
}

func TestTheTrustDialogIsNeverEscaped(t *testing.T) {
	p := newPane(tui(t, "claude-trust-folder.txt"))
	Type(Message{Text: "go on"}, claude, p, newClock())
	for _, call := range p.sent() {
		if call == "keys:Escape" {
			t.Fatalf("%v", p.sent())
		}
	}
}
