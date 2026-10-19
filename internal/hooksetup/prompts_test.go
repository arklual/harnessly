package hooksetup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspacePrompts(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "prompts.json")
	_ = os.WriteFile(file, []byte(`{"claude":"# ws {{workspaceId}}\n","codex":"id {{workspaceId}} {{workspaceId}}","other":"x"}`), 0o600)
	p, err := LoadPrompts(file)
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if err := p.Write(data, "ws-a_1"); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		raw, _ := os.ReadFile(filepath.Join(data, "workspaces", "ws-a_1", name))
		return string(raw)
	}
	if read("claude-prompt.md") != "# ws ws-a_1\n" || read("codex-prompt.md") != "id ws-a_1 ws-a_1" {
		t.Fatalf("%q %q", read("claude-prompt.md"), read("codex-prompt.md"))
	}
	past := time.Now().Add(-time.Hour)
	f := filepath.Join(data, "workspaces", "ws-a_1", "claude-prompt.md")
	_ = os.Chtimes(f, past, past)
	_ = p.Write(data, "ws-a_1")
	if st, _ := os.Stat(f); !st.ModTime().Equal(past) {
		t.Fatal("an unchanged prompt is left alone")
	}
	if err := p.Write(data, "../x"); err == nil {
		t.Fatal("a workspace id that is not one")
	}
	if _, err := LoadPrompts(filepath.Join(dir, "none.json")); err == nil {
		t.Fatal("a missing file")
	}
	only := Prompts{"claude": "c"}
	_ = only.Write(data, "ws-b")
	if _, err := os.Stat(filepath.Join(data, "workspaces", "ws-b", "codex-prompt.md")); err == nil {
		t.Fatal("no template, no file")
	}
}
