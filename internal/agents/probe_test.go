package agents

import "testing"

func TestSections(t *testing.T) {
	out := "profile noise\nHL-VERSION\n2.1.310 (Claude Code)\nHL-ENV ANTHROPIC_API_KEY\nHL-STATUS\n{\n  \"loggedIn\": true\n}\nHL-EXIT 0\n"
	got := Sections(out)
	want := map[string]string{
		"VERSION": "2.1.310 (Claude Code)",
		"ENV":     "ANTHROPIC_API_KEY",
		"STATUS":  "{\n  \"loggedIn\": true\n}",
		"EXIT":    "0",
	}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q want %q", k, got[k], v)
		}
	}
	if len(Sections("")) != 0 || len(Sections("only noise")) != 0 {
		t.Error("sections out of nothing")
	}
	// A JSON line that happens to start with HL- is not a label.
	if s := Sections("HL-STATUS\nHL-\"x\": 1\n"); s["STATUS"] != "HL-\"x\": 1" {
		t.Errorf("%v", s)
	}
}
