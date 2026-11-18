package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeOfferedModelsFollowTheAccount(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	_ = os.MkdirAll(claudeDir, 0o755)
	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"numStartups":3,"additionalModelOptionsCache":[`+
		`{"value":"claude-fable-5-2[1m]","label":"Fable","description":"Fable 5.2 · Most capable for your hardest tasks"},`+
		`{"value":"opus","label":"Opus","description":"dup of an alias"},`+
		`{"value":"bad model id!","label":"x"},`+
		`{"value":"claude-sonnet-5-6","label":"Sonnet 5.6","description":""}]}`), 0o600)
	got := LaunchConfig{ClaudeDir: claudeDir}.Launch("claude")
	var ids []string
	for _, m := range got.Models {
		ids = append(ids, m.ID)
	}
	want := []string{"fable", "opus", "sonnet", "haiku", "claude-fable-5-2[1m]", "claude-sonnet-5-6"}
	if len(ids) != len(want) {
		t.Fatalf("ids %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids %v", ids)
		}
	}
	f := got.Models[4]
	if f.Label != "Fable 5.2 · 1M" || f.Hint != "Most capable for your hardest tasks" || len(f.Efforts) == 0 {
		t.Fatalf("fable = %+v", f)
	}
	if s := got.Models[5]; s.Label != "Sonnet 5.6" {
		t.Fatalf("sonnet = %+v", s)
	}
}

func TestClaudeOfferedWithoutStateFile(t *testing.T) {
	got := LaunchConfig{ClaudeDir: filepath.Join(t.TempDir(), ".claude")}.Launch("claude")
	if len(got.Models) != 4 || len(got.Warnings) != 0 {
		t.Fatalf("got %+v", got)
	}
}
