package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChatCommands(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "commands", "review.md"), []byte("---\ndescription: \"Review the diff\"\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{ID: "acme.tools", Chat: &Chat{Commands: []string{"/review", "/plain"}}}
	got := m.ChatCommands(dir)
	if len(got) != 2 {
		t.Fatalf("got %d commands", len(got))
	}
	if got[0].Name != "review" || got[0].Slash != "/acme-tools:review" || got[0].Description != "Review the diff" || got[0].Kind != CommandPrompt {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Description != "" {
		t.Errorf("second has a description: %+v", got[1])
	}
	if n := (&Manifest{}).ChatCommands(dir); n == nil || len(n) != 0 {
		t.Errorf("no chat must be an empty list, got %v", n)
	}
}
