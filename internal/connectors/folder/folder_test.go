package folder

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// home makes a fake home with a vault in it and points the connector at it.
func home(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	Home = func() (string, error) { return h, nil }
	t.Cleanup(func() { Home = os.UserHomeDir })
	return h
}

func write(t *testing.T, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func inst(t *testing.T, path, preset string) connectors.Instance {
	return connectors.Instance{ID: "cx1", Name: "Vault", Dir: t.TempDir(),
		Config: map[string]string{"path": path, "preset": preset}}
}

func TestCheckKeepsInsideTheHome(t *testing.T) {
	h := home(t)
	write(t, filepath.Join(h, "Notes", "a.md"), "x")
	acc, err := folderConn{}.Check(context.Background(), inst(t, "~/Notes", PresetObsidian))
	if err != nil || acc.Label != "~/Notes" {
		t.Fatalf("%v %v", acc, err)
	}
	outside := t.TempDir()
	if _, err := (folderConn{}).Check(context.Background(), inst(t, outside, PresetPlain)); err == nil {
		t.Fatal("a folder outside the home must be refused")
	}
	// A symlink in the home that leads outside is refused too.
	if err := os.Symlink(outside, filepath.Join(h, "out")); err != nil {
		t.Fatal(err)
	}
	if _, err := (folderConn{}).Check(context.Background(), inst(t, "~/out", PresetPlain)); err == nil {
		t.Fatal("a symlink out of the home must be refused")
	}
	if _, err := (folderConn{}).Check(context.Background(), inst(t, "~/missing", PresetPlain)); err == nil {
		t.Fatal("a missing folder must be refused")
	}
	// A file read cannot leave the folder by .. or a symlink.
	write(t, filepath.Join(h, "secret.txt"), "no")
	if err := os.Symlink(filepath.Join(h, "secret.txt"), filepath.Join(h, "Notes", "link.md")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../secret.txt", "link.md", "/etc/passwd"} {
		if _, err := (folderConn{}).Read(context.Background(), inst(t, "~/Notes", PresetPlain), "file", map[string]string{"path": p}); err == nil {
			t.Fatalf("%s must not be read", p)
		}
	}
}

func TestDiffGivesChangesAndObsidianEvents(t *testing.T) {
	h := home(t)
	vault := filepath.Join(h, "Vault")
	write(t, filepath.Join(vault, "a.md"), "hello\n")
	write(t, filepath.Join(vault, ".obsidian", "workspace.json"), "{}")
	write(t, filepath.Join(vault, "node_modules", "x.md"), "x")
	in := inst(t, vault, PresetObsidian)
	r, _ := root(in)
	cur, err := scan(r, in)
	if err != nil || len(cur) != 1 {
		t.Fatalf("scan: %v %v", cur, err)
	}
	_, _, snap := diff(r, in, snapshot{}, cur, time.Now())

	// A touch without a change gives nothing.
	later := time.Now().Add(time.Second)
	_ = os.Chtimes(filepath.Join(vault, "a.md"), later, later)
	cur, _ = scan(r, in)
	if ch, evs, _ := diff(r, in, snap, cur, time.Now()); len(ch) != 0 || len(evs) != 0 {
		t.Fatalf("touch: %v %v", ch, evs)
	}

	write(t, filepath.Join(vault, "a.md"), "hello\n- [ ] call Ann 📅 2026-11-20\n- [ ] no date\n- [x] done 2026-11-01\n")
	write(t, filepath.Join(vault, "b.md"), "new note")
	cur, _ = scan(r, in)
	changes, evs, next := diff(r, in, snap, cur, time.Now())
	if len(changes) != 2 {
		t.Fatalf("changes: %+v", changes)
	}
	kinds := map[string]int{}
	for _, e := range evs {
		kinds[e.Type]++
		if e.Type == EventNoteTask && (e.Facts["due"] != "2026-11-20" || e.Facts["path"] != "a.md" || e.Title != "") {
			t.Fatalf("task: %+v", e)
		}
	}
	if kinds[EventChanged] != 2 || kinds[EventNoteCreated] != 1 || kinds[EventNoteTask] != 1 {
		t.Fatalf("events: %v", kinds)
	}
	// The same task after an unrelated edit is not new again; a deleted file is a change.
	write(t, filepath.Join(vault, "a.md"), "hello!\n- [ ] call Ann 📅 2026-11-20\n")
	_ = os.Remove(filepath.Join(vault, "b.md"))
	cur, _ = scan(r, in)
	changes, evs, _ = diff(r, in, next, cur, time.Now())
	for _, e := range evs {
		if e.Type == EventNoteTask {
			t.Fatalf("old task again: %+v", e)
		}
	}
	if len(changes) != 2 || changes[1].Change != changeDeleted {
		t.Fatalf("changes: %+v", changes)
	}
}

func TestBulkIsOneEvent(t *testing.T) {
	h := home(t)
	dir := filepath.Join(h, "Many")
	for i := range bulkMax + 10 {
		write(t, filepath.Join(dir, "f"+strconv.Itoa(i)+".txt"), "x")
	}
	in := inst(t, dir, PresetPlain)
	r, _ := root(in)
	cur, _ := scan(r, in)
	_, evs, _ := diff(r, in, snapshot{}, cur, time.Now())
	if len(evs) != 1 || evs[0].Facts["change"] != changeBulk {
		t.Fatalf("bulk: %d events", len(evs))
	}
}

// TestWatchLearnsThenEmitsOnceAndSurvivesRestart: the first run emits nothing, a new file gives one batch, a restart
// from the kept snapshot gives only what changed while it was down.
func TestWatchLearnsThenEmitsOnceAndSurvivesRestart(t *testing.T) {
	h := home(t)
	dir := filepath.Join(h, "Docs")
	write(t, filepath.Join(dir, "old.txt"), "x")
	in := inst(t, dir, PresetPlain)

	var mu sync.Mutex
	var got []connectors.Event
	var cur connectors.Cursor
	emit := func(evs []connectors.Event, next connectors.Cursor) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, evs...)
		cur = next
		return nil
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(got) }
	run := func(from connectors.Cursor) (context.CancelFunc, chan struct{}) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = folderConn{}.Watch(ctx, in, from, emit); close(done) }()
		return cancel, done
	}
	cancel, done := run("")
	time.Sleep(300 * time.Millisecond)
	if count() != 0 {
		t.Fatalf("first run emitted %d", count())
	}
	write(t, filepath.Join(dir, "new.txt"), "hello")
	deadline := time.Now().Add(10 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	<-done
	if count() != 1 || got[0].Facts["path"] != "new.txt" || got[0].Facts["change"] != changeCreated {
		t.Fatalf("after a new file: %+v", got)
	}
	write(t, filepath.Join(dir, "down.txt"), "while down")
	mu.Lock()
	from := cur
	mu.Unlock()
	cancel, done = run(from)
	deadline = time.Now().Add(5 * time.Second)
	for count() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done
	if count() != 2 || got[1].Facts["path"] != "down.txt" {
		t.Fatalf("after restart: %+v", got)
	}
	res, err := folderConn{}.Read(context.Background(), in, "changes", map[string]string{"since": "1h"})
	if err != nil || !strings.Contains(res.Data, "new.txt") || !strings.Contains(res.Data, "down.txt") {
		t.Fatalf("changes: %+v %v", res, err)
	}
	res, err = folderConn{}.Read(context.Background(), in, "search", map[string]string{"query": "WHILE"})
	if err != nil || !strings.Contains(res.Data, "down.txt:1: while down") {
		t.Fatalf("search: %+v %v", res, err)
	}
}
