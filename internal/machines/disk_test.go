package machines

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fill(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitMeasured(t *testing.T, s *DiskScanner) DiskLayout {
	t.Helper()
	for i := 0; i < 200; i++ {
		if l := s.Layout(); l.MeasuredAt != 0 {
			return l
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the walk did not end")
	return DiskLayout{}
}

func partOf(l DiskLayout, id string) (int64, bool) {
	for _, p := range l.Parts {
		if p.ID == id {
			return p.Bytes, true
		}
	}
	return 0, false
}

func TestDiskLayoutCountsTheFoldersItKnows(t *testing.T) {
	home := t.TempDir()
	fill(t, filepath.Join(home, "work", "repo", "a.bin"), 1<<20)
	fill(t, filepath.Join(home, "work", ".git", "pack"), 1<<20)
	fill(t, filepath.Join(home, ".cache", "x"), 2<<20)
	fill(t, filepath.Join(home, ".claude", "log"), 3<<20)
	fill(t, filepath.Join(home, ".stranger", "log"), 4<<20)
	s := NewDiskScanner(DiskDeps{Path: home, Rest: func(time.Duration) {}, Specs: []DiskSpec{
		{ID: DiskWorkspace, Roots: []string{filepath.Join(home, "work")}},
		{ID: DiskCaches, Roots: []string{filepath.Join(home, ".cache")}},
		{ID: DiskAgents, Roots: []string{filepath.Join(home, ".claude")}},
	}})
	first := s.Layout()
	if first.MeasuredAt != 0 {
		t.Fatal("nothing is measured before the first walk ends")
	}
	if _, ok := partOf(first, DiskFree); !ok || first.Parts[len(first.Parts)-1].ID != DiskFree {
		t.Fatalf("free is the last part: %+v", first.Parts)
	}
	l := waitMeasured(t, s)
	for id, min := range map[string]int64{DiskWorkspace: 2 << 20, DiskCaches: 2 << 20, DiskAgents: 3 << 20} {
		if got, ok := partOf(l, id); !ok || got < min || got > min+(1<<19) {
			t.Fatalf("%s is %d (want about %d): %+v", id, got, min, l.Parts)
		}
	}
	if l.Partial {
		t.Fatal("a walk that ended in time is not partial")
	}
}

func TestDiskWorkspaceAtHomeSkipsHiddenFolders(t *testing.T) {
	home := t.TempDir()
	fill(t, filepath.Join(home, "repo", ".git", "pack"), 1<<20)
	fill(t, filepath.Join(home, ".cache", "x"), 5<<20)
	fill(t, filepath.Join(home, ".stranger", "x"), 6<<20)
	s := NewDiskScanner(DiskDeps{Path: home, Rest: func(time.Duration) {}, Specs: []DiskSpec{
		{ID: DiskWorkspace, Roots: []string{home}, SkipHiddenTop: true},
		{ID: DiskCaches, Roots: []string{filepath.Join(home, ".cache")}},
	}})
	l := waitMeasured(t, s)
	ws, _ := partOf(l, DiskWorkspace)
	if ws < 1<<20 || ws > (1<<20)+(1<<18) {
		t.Fatalf("workspace %d: the hidden folders of home are not its own, the repo's .git is", ws)
	}
	if c, _ := partOf(l, DiskCaches); c < 5<<20 {
		t.Fatalf("caches %d", c)
	}
}

func TestDiskCountsAFileWithTwoNamesOnce(t *testing.T) {
	home := t.TempDir()
	fill(t, filepath.Join(home, "work", "a"), 1<<20)
	if err := os.Link(filepath.Join(home, "work", "a"), filepath.Join(home, "work", "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "work", "a"), filepath.Join(home, "work", "c")); err != nil {
		t.Fatal(err)
	}
	s := NewDiskScanner(DiskDeps{Path: home, Rest: func(time.Duration) {}, Specs: []DiskSpec{{ID: DiskWorkspace, Roots: []string{filepath.Join(home, "work")}}}})
	l := waitMeasured(t, s)
	if ws, _ := partOf(l, DiskWorkspace); ws > (1<<20)+(1<<17) {
		t.Fatalf("%d: the second name and the link are not more bytes", ws)
	}
}

func TestDiskWalkOutOfBudgetIsPartial(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < 50; i++ {
		fill(t, filepath.Join(home, "work", "d", string(rune('a'+i%26)), "f"+string(rune('a'+i/26))), 1024)
	}
	s := NewDiskScanner(DiskDeps{Path: home, Budget: time.Nanosecond, Rest: func(time.Duration) {}, Specs: []DiskSpec{{ID: DiskWorkspace, Roots: []string{filepath.Join(home, "work")}}}})
	if l := waitMeasured(t, s); !l.Partial {
		t.Fatal("a walk cut short must say so")
	}
}

func TestDiskPartsNeverExceedTheUsedPart(t *testing.T) {
	home := t.TempDir()
	fill(t, filepath.Join(home, "a"), 1<<20)
	s := NewDiskScanner(DiskDeps{Path: home, Rest: func(time.Duration) {}, Specs: []DiskSpec{{ID: DiskWorkspace, Roots: []string{home}}}})
	l := waitMeasured(t, s)
	var sum int64
	for _, p := range l.Parts {
		if p.Bytes < 0 {
			t.Fatalf("negative part %+v", p)
		}
		sum += p.Bytes
	}
	if l.Total > 0 && sum > l.Total+(1<<20) {
		t.Fatalf("the parts (%d) are more than the disk (%d)", sum, l.Total)
	}
}
