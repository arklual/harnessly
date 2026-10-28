package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvSourcePrefix(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := EnvSourcePrefix(dir, Claude); got != "" {
		t.Fatalf("no files: %q", got)
	}
	if got := EnvSourcePrefix("", Claude); got != "" {
		t.Fatalf("no folder: %q", got)
	}
	write("all.env", "A='1'\n")
	want := "set -a; . '" + dir + "/all.env'; set +a; "
	if got := EnvSourcePrefix(dir, Claude); got != want {
		t.Fatalf("shared only: %q", got)
	}
	write("claude.env", "ANTHROPIC_API_KEY='x'\n")
	want = "set -a; . '" + dir + "/all.env'; . '" + dir + "/claude.env'; set +a; "
	if got := EnvSourcePrefix(dir, Claude); got != want {
		t.Fatalf("both: %q", got)
	}
	// Codex does not source Claude's file.
	want = "set -a; . '" + dir + "/all.env'; set +a; "
	if got := EnvSourcePrefix(dir, Codex); got != want {
		t.Fatalf("codex: %q", got)
	}
	// An emptied file adds nothing.
	write("all.env", "")
	write("claude.env", "")
	if got := EnvSourcePrefix(dir, Claude); got != "" {
		t.Fatalf("empty files: %q", got)
	}
}

func TestEnvSourcePrefixQuotesTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "it's here")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "all.env"), []byte("A='1'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := EnvSourcePrefix(dir, Codex)
	want := "set -a; . " + ShellQuote(filepath.Join(dir, "all.env")) + "; set +a; "
	if got != want {
		t.Fatalf("%q", got)
	}
}
