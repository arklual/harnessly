package screen

import (
	"os"
	"testing"
)

func TestClaudeAside(t *testing.T) {
	for file, want := range map[string]Aside{
		"claude-2.1.312-login-method.ansi.txt":  AsideLogin,
		"claude-2.1.312-login-code.ansi.txt":    AsideLogin,
		"claude-model-picker.txt":               AsideModel,
		"claude-2.1.310-effort-dialog.ansi.txt": AsideEffort,
		// Esc quits Claude here, and does nothing on the first-run login: never closed.
		"claude-trust-folder.txt":                      NoAside,
		"claude-2.1.312-first-run-login-code.ansi.txt": NoAside,
		"claude-idle-placeholder.ansi.txt":             NoAside,
		"claude-switch-model-confirm.txt":              NoAside,
		"claude-ask-preview-question.txt":              NoAside,
		"claude-2.1.310-switch-model-confirm.ansi.txt": NoAside,
	} {
		raw, err := os.ReadFile("testdata/tui/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if got := ClaudeAside(StripANSI(string(raw))); got != want {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
}

func TestTheAgentsOwnWordsAboveTheBoxAreNoAside(t *testing.T) {
	raw, err := os.ReadFile("testdata/tui/claude-idle-placeholder.ansi.txt")
	if err != nil {
		t.Fatal(err)
	}
	idle := StripANSI(string(raw))
	printed := "● The login screen reads:\n   Select login method:\n   ❯ 1. Claude account with subscription\n   Esc to cancel\n"
	if got := ClaudeAside(printed + idle); got != NoAside {
		t.Fatalf("%q", got)
	}
}
