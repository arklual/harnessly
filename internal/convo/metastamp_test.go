package convo

import (
	"os"
	"path/filepath"
	"testing"
)

// A transcript at rest stamps the same twice; growing it or a new subagent changes the stamp.
func TestMetaStampSeesTheTranscriptAndItsSubagents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if stampOf(path).ok {
		t.Fatal("a missing transcript stamped")
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := stampOf(path)
	if !a.ok || a != stampOf(path) {
		t.Fatalf("an unchanged transcript stamped twice differently: %+v", a)
	}
	if err := os.WriteFile(path, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := stampOf(path)
	if b == a {
		t.Fatal("a grown transcript kept its stamp")
	}
	if err := os.MkdirAll(filepath.Join(dir, "s", "subagents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if stampOf(path) == b {
		t.Fatal("a subagents folder that appeared kept the stamp")
	}
}
