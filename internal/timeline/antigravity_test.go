package timeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeAntigravityPathAndIncremental(t *testing.T) {
	root := t.TempDir()
	id := "eee57bb1-4450-4f9d-a50b-cf660151d184"
	dir := filepath.Join(root, id, ".system_generated", "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "transcript.jsonl")
	line := `{"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"hello"}` + "\n"
	if err := os.WriteFile(path, []byte(line+"{partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if !nativeAntigravityPath(root, path) {
		t.Fatal("native path rejected")
	}
	full := filepath.Join(dir, "transcript_full.jsonl")
	if err := os.WriteFile(full, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if !nativeAntigravityPath(root, full) {
		t.Fatal("official full transcript rejected")
	}
	backup := filepath.Join(dir, "transcript_backup.jsonl")
	if err := os.WriteFile(backup, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if nativeAntigravityPath("", path) || nativeAntigravityPath(root, backup) {
		t.Fatal("unconfigured/arbitrary backup accepted")
	}
	inc := antigravityIncremental(path, 0)
	if len(inc.NewEntries) != 1 || inc.NewOffset != int64(len(line)) {
		t.Fatal(inc)
	}
	if next := antigravityIncremental(path, inc.NewOffset); len(next.NewEntries) != 0 {
		t.Fatal(next)
	}
	outside := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(outside, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if nativeAntigravityPath(root, path) {
		t.Fatal("symlink escape accepted")
	}
}
