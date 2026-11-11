package design

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A machine with more agent chats than the cache's floor still reads each transcript once: a second pass in the
// same order finds every index instead of evicting the one it needs next.
func TestTouchedKeepsAnIndexPerChatBeyondTheFloor(t *testing.T) {
	base := t.TempDir()
	claude := filepath.Join(base, "claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	lib := New(Config{LibraryDir: filepath.Join(base, "lib"), ClaudeProjects: claude})
	n := maxSourceIndexes*2 + 7
	chats := make([]Chat, 0, n)
	for i := range n {
		p := filepath.Join(claude, fmt.Sprintf("c%03d.jsonl", i))
		if err := os.WriteFile(p, []byte(toolUse(t, "Bash", map[string]any{"command": "ls /srv/x"})+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		chats = append(chats, Chat{TabID: fmt.Sprintf("t%d", i), PanelType: "claude-code", Transcript: p})
	}
	lib.scanTouched(map[string]bool{}, chats)
	first := map[string]*sourceIndex{}
	for k, v := range lib.sources.indexes {
		first[k] = v
	}
	if len(first) != n {
		t.Fatalf("indexes after one pass = %d, want %d", len(first), n)
	}
	lib.scanTouched(map[string]bool{}, chats)
	for _, c := range chats {
		if lib.sources.indexes[c.Transcript] != first[c.Transcript] {
			t.Fatalf("%s was read again from the start", c.Transcript)
		}
	}
}

// The listing does not wait out a slow scan of the transcripts (a cold one after a restart takes minutes on a loaded
// machine, past the page's limit): it answers without agents, and a later call takes the finished scan.
func TestTouchedDoesNotWaitOutASlowScan(t *testing.T) {
	base := t.TempDir()
	claude := filepath.Join(base, "claude")
	libDir := filepath.Join(base, "lib")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	lib := New(Config{LibraryDir: libDir, ClaudeProjects: claude})
	ref := "d-slowscan"
	p := filepath.Join(claude, "c.jsonl")
	line := toolUse(t, "Bash", map[string]any{"command": "ls " + filepath.Join(lib.Root(), ref, "index.html")})
	if err := os.WriteFile(p, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chats := []Chat{{TabID: "t1", PanelType: "claude-code", Transcript: p}}
	known := map[string]bool{ref: true}

	// Another reader holds the transcript, as a cold scan does for minutes.
	fm := &sync.Mutex{}
	fm.Lock()
	lib.sources.mu.Lock()
	lib.sources.file, lib.sources.indexes = map[string]*sync.Mutex{p: fm}, map[string]*sourceIndex{}
	lib.sources.mu.Unlock()

	start := time.Now()
	got := lib.touchedByTab(known, chats)
	if d := time.Since(start); d > touchedWait+time.Second {
		t.Fatalf("waited %s for a held transcript", d)
	}
	if len(got.refs) != 0 {
		t.Fatalf("refs while the scan is held = %v, want none", got.refs)
	}

	fm.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got = lib.touchedByTab(known, chats)
		if len(got.refs["t1"]) == 1 && got.refs["t1"][0] == ref {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("refs after the scan = %v, want t1 → %s", got.refs, ref)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
