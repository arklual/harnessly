package filesync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A rescan of a folder whose files did not change sends nothing; a file edited, added or deleted between two scans is
// still seen while the scan reads the state from one snapshot.
func TestRescanFromOneSnapshotStillSeesEditsAndDeletes(t *testing.T) {
	c := newCounted(t)
	a, root := c.agent(t, nil)
	for i := range 20 {
		write(t, root, fmt.Sprintf("admin/project/f%02d.dc.html", i), fmt.Sprintf("<x-dc>%d</x-dc>", i))
	}
	must(t, a.ScanAll(ctx))
	commits := c.n("commit OK")
	if commits != 20 {
		t.Fatalf("first scan: %d commits, want 20", commits)
	}
	must(t, a.ScanAll(ctx))
	if c.n("commit OK") != commits || c.n("POST delete") != 0 {
		t.Fatalf("unchanged rescan sent something: %v", c.hits)
	}
	if a.scanKnown != nil || a.scanSeen != nil {
		t.Fatal("the scan's snapshot outlives the scan")
	}
	// An edit keeps the size but moves the time: the snapshot does not hide it.
	p := filepath.Join(root, "admin/project/f03.dc.html")
	write(t, root, "admin/project/f03.dc.html", "<x-dc>X</x-dc>")
	later := time.Now().Add(time.Minute)
	must(t, os.Chtimes(p, later, later))
	write(t, root, "admin/project/new.dc.html", "<x-dc>new</x-dc>")
	must(t, os.Remove(filepath.Join(root, "admin/project/f07.dc.html")))
	must(t, a.ScanAll(ctx))
	if got := c.n("commit OK"); got < commits+2 {
		t.Fatalf("after an edit, an add and a delete: %d commits, want at least %d (%v)", got, commits+2, c.hits)
	}
	if k, _ := a.state.get("admin/project/f07.dc.html"); k == nil || !k.Deleted {
		t.Fatalf("the deleted file is not deleted in the state: %+v", k)
	}
}

// BenchmarkRescanUnchanged is the periodic rescan of a big folder nobody touched: what the runtime pays every two
// minutes for the design library (22 000 files on the machines).
func BenchmarkRescanUnchanged(b *testing.B) {
	c := newCounted(b)
	a, root := c.agent(b, nil)
	for i := range 3000 {
		write(b, root, fmt.Sprintf("p%02d/project/f%04d.dc.html", i%30, i), fmt.Sprintf("<x-dc>%d</x-dc>", i))
	}
	if err := a.ScanAll(ctx); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := a.ScanAll(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
