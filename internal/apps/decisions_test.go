package apps

import (
	"os"
	"strings"
	"testing"
)

func TestDecisionsJournal(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadDecisions(dir, "c1", "a", 50); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty journal: %v %v", got, err)
	}
	for i := 0; i < DecisionsMax+20; i++ {
		app := "a"
		if i%2 == 1 {
			app = "b"
		}
		if err := AppendDecision(dir, "c1", DecisionEntry{At: int64(i), App: app, Tool: "Bash", Rule: "r", Decision: "deny", Reason: strings.Repeat("я", 400)}); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(dir + "/c1.jsonl")
	if n := strings.Count(string(raw), "\n"); n != DecisionsMax {
		t.Fatalf("kept %d lines", n)
	}
	got, err := ReadDecisions(dir, "c1", "a", 50)
	if err != nil || len(got) != 50 {
		t.Fatalf("%d %v", len(got), err)
	}
	if got[0].At != int64(DecisionsMax+18) || got[0].App != "a" || got[0].At < got[1].At {
		t.Errorf("newest first, app a only: %+v %+v", got[0], got[1])
	}
	if n := len([]rune(got[0].Reason)); n != DecisionReasonMax {
		t.Errorf("reason %d chars", n)
	}
	if err := AppendDecision(dir, "../x", DecisionEntry{}); err == nil {
		t.Error("an unsafe conversation id was taken")
	}
}
