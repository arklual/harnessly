package usagereport

import (
	"os"
	"reflect"
	"testing"
	"time"
)

// The facts file a collector keeps is read back as the same transcripts and facts, without reading a transcript; a
// pass in the background marks the file alive.
func TestStoredReadsTheCollectorsFactsFile(t *testing.T) {
	g := loadGolden(t)
	root, _ := layOut(t, g)
	clock := &fakeClock{t: time.UnixMilli(int64(g.Now))}
	_, c := newTestRoute(root, clock)
	s := &Stored{Path: c.cfg.CacheFile}
	if s.Alive(time.Hour, time.Now()) {
		t.Fatal("alive before any pass")
	}
	stop := make(chan struct{})
	close(stop)
	c.KeepFresh(time.Minute, stop)
	if !s.Alive(time.Minute, time.Now()) {
		t.Fatal("not alive after a pass")
	}
	if s.Alive(time.Minute, time.Now().Add(2*time.Minute)) {
		t.Fatal("alive long after the last pass")
	}
	s.Refresh()
	want, got := c.Entries(), s.Entries()
	if len(want) == 0 || len(got) != len(want) {
		t.Fatalf("entries: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].MtimeMs != want[i].MtimeMs || !reflect.DeepEqual(got[i].Facts.Messages, want[i].Facts.Messages) ||
			!reflect.DeepEqual(got[i].Facts.Turns, want[i].Facts.Turns) || got[i].Facts.Provider != want[i].Facts.Provider {
			t.Fatalf("entry %d differs:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
	// Unchanged file: the same read is kept.
	first := &s.Entries()[0]
	s.Refresh()
	if &s.Entries()[0] != first {
		t.Fatal("an unchanged file was read again")
	}
	s.Forget()
	if s.Entries() != nil {
		t.Fatal("forget kept the entries")
	}
	// A broken file leaves nothing rather than half.
	if err := os.WriteFile(c.cfg.CacheFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	if s.Entries() != nil {
		t.Fatal("entries from a broken file")
	}
}
