package paneintents

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// waitIntents waits for the lift, which runs off the caller.
func waitIntents(t *testing.T, s *Store, tab string, n int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for len(s.List(tab)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d intents, want %d", tab, len(s.List(tab)), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A delegated chat's `open` opens in the chats above it too, quietly and named by the subchat it came through
// a `show` stays the subchat's; the same address within LiftRepeat is not asked of the parent again.
func TestLiftToRoot(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "a.json"), nil, false)
	parents := map[string]Parent{
		"tab-grand": {TabID: "tab-kid", FromID: "tab-grand", Title: "Внук"},
		"tab-kid":   {TabID: "tab-root", FromID: "tab-kid", Title: "Правка доски"},
		// A loop of parents stops.
		"tab-root": {TabID: "tab-grand", FromID: "tab-root", Title: "Корень"},
	}
	var mu sync.Mutex
	told := map[string]int{}
	s.SetLift(func(tab string) (Parent, bool) { p, ok := parents[tab]; return p, ok }, func(tab string) {
		mu.Lock()
		told[tab]++
		mu.Unlock()
	})
	s.Add("tab-grand", cmd("kind", "open", "tab", "design", "target", "d1"))
	waitIntents(t, s, "tab-root", 1)
	waitIntents(t, s, "tab-kid", 1)
	root := s.List("tab-root")[0].Object("command")
	if auto, _ := root.Get("auto"); auto != true || root.String("target") != "d1" || root.Object("from").String("title") != "Правка доски" {
		t.Fatalf("root got %v", root)
	}
	if kid := s.List("tab-kid")[0].Object("command"); kid.Object("from").String("id") != "tab-grand" {
		t.Fatalf("kid got %v", kid)
	}
	if len(s.List("tab-grand")) != 1 {
		t.Fatal("the loop of parents came back to the asking chat")
	}

	s.Add("tab-kid", cmd("kind", "open", "tab", "design", "target", "d1"))
	s.Add("tab-kid", cmd("kind", "show"))
	s.Add("tab-kid", cmd("kind", "open", "tab", "files", "target", "/w/a.md"))
	waitIntents(t, s, "tab-root", 2)
	time.Sleep(50 * time.Millisecond)
	list := s.List("tab-root")
	if len(list) != 2 || list[1].Object("command").String("target") != "/w/a.md" {
		t.Fatalf("root: %d intents, want the design once and the file", len(list))
	}
	mu.Lock()
	defer mu.Unlock()
	if told["tab-root"] != 2 {
		t.Fatalf("root told %d times, want 2", told["tab-root"])
	}
}
