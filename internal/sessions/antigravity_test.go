package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAntigravityListerVerifiedNativeSessions(t *testing.T) {
	home := t.TempDir()
	l := NewAntigravityLister(home)
	id := "eee57bb1-4450-4f9d-a50b-cf660151d184"
	dir := filepath.Join(l.Root, id, ".system_generated", "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	native := `{"source":"SYSTEM","type":"SYSTEM_MESSAGE","content":"not a user"}` + "\n" + `{"source":"USER_EXPLICIT","type":"USER_INPUT","content":"<USER_REQUEST>First request</USER_REQUEST><ADDITIONAL_METADATA>hidden</ADDITIONAL_METADATA>"}` + "\n"
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	r, ok := l.Find(id)
	if !ok || r.Title != "First request" || r.Turns != 1 || r.Path != path || r.Cwd != "" {
		t.Fatal(r, ok)
	}
	if _, ok := l.Find("../" + id); ok {
		t.Fatal("accepted traversal")
	}
	if got := l.Recent(nil, 5); len(got) != 1 {
		t.Fatal(got)
	}
	if got := l.Recent(func(string) bool { return true }, 5); len(got) != 0 {
		t.Fatal(got)
	}
	full := filepath.Join(dir, "transcript_full.jsonl")
	if err := os.WriteFile(full, []byte(native), 0600); err != nil {
		t.Fatal(err)
	}
	if r, ok := l.Find(id); !ok || r.Path != path {
		t.Fatal("canonical not preferred", r, ok)
	}
	if got := l.Recent(nil, 5); len(got) != 1 {
		t.Fatal("duplicate native session", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if r, ok := l.Find(id); !ok || r.Path != full {
		t.Fatal("native full transcript not found", r, ok)
	}
	if err := os.Rename(full, filepath.Join(dir, "transcript_backup.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Find(id); ok {
		t.Fatal("arbitrary backup accepted")
	}
	if err := os.Symlink(filepath.Join(dir, "transcript_backup.jsonl"), path); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Find(id); ok {
		t.Fatal("accepted symlink transcript")
	}
}
