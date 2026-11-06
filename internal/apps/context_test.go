package apps

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestContextText(t *testing.T) {
	ok := json.RawMessage(`{"content":[{"type":"text","text":" a "},{"type":"image"},{"type":"text","text":"b"}]}`)
	if got := ContextText(ok); got != "a\n\nb" {
		t.Errorf("got %q", got)
	}
	if got := ContextText(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"boom"}]}`)); got != "" {
		t.Errorf("an error result is no context: %q", got)
	}
	long, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": strings.Repeat("я", ContextMaxBytes)}}})
	if got := ContextText(long); len(got) > ContextMaxBytes {
		t.Errorf("not cut: %d", len(got))
	}
}

func TestContextItemsAreOnActivationOnly(t *testing.T) {
	m := &Manifest{Agent: &Agent{Context: []ContextItem{{ID: "a", Tool: "s.a", Mode: "on-activation"}, {ID: "b", Tool: "s.b", Mode: "on-demand"}}}}
	if got := ContextItems(m); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("got %+v", got)
	}
}

func TestGuardSettingsAddsContextHooks(t *testing.T) {
	path := t.TempDir() + "/s.json"
	if err := os.WriteFile(path, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"own"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := Snapshot{Apps: []SnapshotApp{{ID: "a.b", Context: []ContextItem{{ID: "q", Tool: "s.q", Mode: "on-activation"}}}}}
	body, err := guardSettings(path, "/bin/h hook app-guard --policy 'p'", snap)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	_ = json.Unmarshal(body, &s)
	for _, ev := range []string{"SessionStart", "UserPromptSubmit"} {
		last := s.Hooks[ev][len(s.Hooks[ev])-1].Hooks[0].Command
		if last != "/bin/h hook app-context --policy 'p'" {
			t.Errorf("%s: %q", ev, last)
		}
	}
	if len(s.Hooks["SessionStart"]) != 2 {
		t.Errorf("the chat's own hook must stay: %+v", s.Hooks["SessionStart"])
	}
}
