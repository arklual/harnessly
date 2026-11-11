package hlcli

import (
	"strings"
	"testing"
)

// `app enable` says what the running agent has at once and what waits for its next start.
func TestAppEnabledWords(t *testing.T) {
	yes, no := true, false
	live := appEnabledWords("acme.form", &yes, "")
	for _, want := range []string{"acme.form is on in this chat", "has it now, with no restart", "tools of its open window at once", "next turn", "slash commands wait"} {
		if !strings.Contains(live, want) {
			t.Fatalf("live lacks %q: %s", want, live)
		}
	}
	if strings.Contains(live, "gets it when it starts next") {
		t.Fatalf("live: %s", live)
	}
	waits := appEnabledWords("acme.form", &no, "Форма needs rules for the agent's tools")
	if !strings.Contains(waits, "only when it starts next: Форма needs rules") {
		t.Fatalf("waits: %s", waits)
	}
	if got := appEnabledWords("acme.form", &no, ""); !strings.Contains(got, "when it starts next") {
		t.Fatalf("not live: %s", got)
	}
	// A runtime that does not say (an older one): both cases named, neither promised.
	if got := appEnabledWords("acme.form", nil, ""); !strings.Contains(got, "at once") || !strings.Contains(got, "starts next") {
		t.Fatalf("no word of the runtime: %s", got)
	}
}
