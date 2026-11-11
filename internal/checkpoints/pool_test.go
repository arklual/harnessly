package checkpoints

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func poolStore(t *testing.T, pool bool) (*Store, string, *int64) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"a.txt", "b.txt", "src/c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(strings.Repeat(name+"\n", 50+i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli()
	s := &Store{Dir: filepath.Join(base, "data", "checkpoints"), Home: filepath.Join(base, "home"), Namespace: "acme", TmpTag: "hs", Pool: pool}
	s.Now = func() int64 { return now }
	return s, root, &now
}

// looseObjects counts the objects a repository holds as files of their own.
func looseObjects(t *testing.T, repo string) int {
	t.Helper()
	n := 0
	dirs, _ := os.ReadDir(filepath.Join(repo, "objects"))
	for _, d := range dirs {
		if len(d.Name()) != 2 {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(repo, "objects", d.Name()))
		n += len(files)
	}
	return n
}

func packs(t *testing.T, repo string) int {
	t.Helper()
	found, _ := filepath.Glob(filepath.Join(repo, "objects", "pack", "*.pack"))
	return len(found)
}

func take(t *testing.T, s *Store, tab, root string) (*Scope, Checkpoint) {
	t.Helper()
	sc, err := s.Scope(tab, root)
	if err != nil {
		t.Fatal(err)
	}
	c, reason := sc.Take(s.now())
	if reason != "" {
		t.Fatalf("take %s: %s", tab, reason)
	}
	return sc, c
}

func TestPoolSharesObjectsBetweenTabs(t *testing.T) {
	s, root, now := poolStore(t, true)
	a, first := take(t, s, "tab-a", root)
	if n := looseObjects(t, a.Dir); n != 0 {
		t.Errorf("tab-a keeps %d loose objects after the hand-over", n)
	}
	if n := packs(t, a.Dir); n != 0 {
		t.Errorf("tab-a keeps %d packs after the hand-over", n)
	}
	if n := packs(t, a.poolDir()); n != 1 {
		t.Fatalf("the pool has %d packs, want 1", n)
	}

	// A second tab of the folder writes its commit and nothing else: the tree and the files are in the pool.
	*now += 1000
	b, _ := s.Scope("tab-b", root)
	if err := b.ensureRepo(*now); err != nil {
		t.Fatal(err)
	}
	unlock := b.lock()
	_, reason := b.createLocked(KindTurn, *now)
	unlock()
	if reason != "" {
		t.Fatal(reason)
	}
	// Its commit at most: taken within the second of tab-a's, it is the same object and the pool has it too.
	if n := looseObjects(t, b.Dir); n > 1 {
		t.Errorf("tab-b wrote %d objects for a folder already in the pool, want its commit at most", n)
	}

	// The rewind still works over objects that live in the pool.
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	*now += 5000
	take(t, s, "tab-a", root)
	res, err := a.Rewind(first.ID, Hooks{})
	if err != nil || res.Reason != "" {
		t.Fatalf("rewind: %v %q", err, res.Reason)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if !strings.HasPrefix(string(got), "a.txt\n") {
		t.Errorf("a.txt after the rewind = %q", got)
	}
}

func TestSweepPacksOldRepositoriesAndCollects(t *testing.T) {
	s, root, now := poolStore(t, false)
	old, c := take(t, s, "tab-old", root)
	if looseObjects(t, old.Dir) == 0 {
		t.Fatal("a repository without the pool should hold loose objects")
	}
	before, _ := os.Stat(old.Dir)

	s.Pool = true
	*now += 60_000
	other, _ := take(t, s, "tab-new", root)
	s.Sweep(0)
	if n := looseObjects(t, old.Dir) + packs(t, old.Dir); n != 0 {
		t.Errorf("the old repository keeps %d loose objects and packs after the sweep", n)
	}
	if after, _ := os.Stat(old.Dir); !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the sweep moved the repository's time from %v to %v", before.ModTime(), after.ModTime())
	}
	list, err := old.List()
	if err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list after the sweep: %v %v", list, err)
	}
	pool := old.poolDir()
	if n := packs(t, pool); n != 1 {
		t.Errorf("the collected pool has %d packs, want 1", n)
	}
	if out, _ := gitAt(pool, nil, nil, "for-each-ref", tabRefs("tab-old")); out == "" {
		t.Error("the pool has no copy of the old repository's refs")
	}

	// A repository that is gone gives its refs up; the last one takes the pool with it.
	if err := os.RemoveAll(old.Dir); err != nil {
		t.Fatal(err)
	}
	if err := s.collect(pool, *now); err != nil {
		t.Fatal(err)
	}
	if out, _ := gitAt(pool, nil, nil, "for-each-ref", tabRefs("tab-old")); out != "" {
		t.Errorf("refs of a removed repository stay in the pool: %s", out)
	}
	if list, err := other.List(); err != nil || len(list) != 1 {
		t.Fatalf("the other tab after the collection: %v %v", list, err)
	}
	if err := os.RemoveAll(other.Dir); err != nil {
		t.Fatal(err)
	}
	if err := s.collect(pool, *now); err != nil {
		t.Fatal(err)
	}
	if exists(pool) {
		t.Error("a pool nobody borrows from stays")
	}
}
