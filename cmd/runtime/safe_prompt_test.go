package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithSafeAppendsTheBlockAfterTheWorkspacePrompt(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "claude-prompt.md")
	if err := os.WriteFile(ws, []byte("Workspace rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt := func(string) string { return ws }
	if got := withSafe(false, dir, prompt)("w"); got != ws {
		t.Fatalf("off: %s", got)
	}
	got := withSafe(true, filepath.Join(dir, "launch"), prompt)("w")
	raw, err := os.ReadFile(got)
	if err != nil || !strings.HasPrefix(string(raw), "Workspace rules\n\n## Secrets (the safe)") {
		t.Fatalf("%s: %q %v", got, raw, err)
	}
	if again := withSafe(true, filepath.Join(dir, "launch"), prompt)("w"); again != got {
		t.Fatal("the same text is one file")
	}
	// Without a workspace prompt, the block alone.
	none := withSafe(true, filepath.Join(dir, "launch"), func(string) string { return filepath.Join(dir, "missing.md") })("w")
	if raw, _ := os.ReadFile(none); !strings.HasPrefix(string(raw), "## Secrets") {
		t.Fatalf("%q", raw)
	}
}
