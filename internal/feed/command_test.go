package feed

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A bash-mode line («! cmd») fires no prompt-submit hook and is written as a command, not a user message: its
// entry signals the outbox with the line as it was typed, and is found by its words, so the message is not «не дошло».
func TestABashModeLineIsTheUserLineOfItsMessage(t *testing.T) {
	text := conversation(1) +
		`{"type":"system","subtype":"turn_duration","durationMs":1000,"timestamp":"2026-10-12T10:00:01.000Z"}` + "\n" +
		`{"type":"user","uuid":"00000000-0000-4000-8000-0000000000cc","timestamp":"2026-10-12T10:01:00.000Z","message":{"role":"user","content":"<bash-input> tctl create -f role.yaml</bash-input>"}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	u := f.Load()
	validate(t, f)
	var got *Signal
	for i, s := range u.Signals {
		if s.K == SignalUser && s.Text != "" {
			got = &u.Signals[i]
		}
	}
	if got == nil || got.Text != "! tctl create -f role.yaml" {
		t.Fatalf("signals: %+v", u.Signals)
	}
	id, ok := f.FindUserText(0, 0, "! tctl create -f role.yaml")
	if !ok || id != got.EntryID {
		t.Fatalf("found %q %v, want %q", id, ok, got.EntryID)
	}
	if _, ok := f.FindUserText(0, 0, "! tctl get roles"); ok {
		t.Fatal("another command's words took the entry")
	}
}

func TestCommandLine(t *testing.T) {
	for _, c := range []struct{ name, args, want string }{
		{"!", "ls -la", "! ls -la"},
		{"compact", "", "/compact"},
		{"model", "opus", "/model opus"},
	} {
		if got := CommandLine(c.name, c.args); got != c.want {
			t.Fatalf("%q %q: %q, want %q", c.name, c.args, got, c.want)
		}
	}
}
