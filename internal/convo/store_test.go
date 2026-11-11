package convo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

var errDiskFull = errors.New("no space left on device")

// failingDisk makes the rig's store fail its writes while full is set.
func failingDisk(r *rig) *bool {
	full := new(bool)
	s := r.rt.d.Store
	s.write = func(path string, raw []byte) error {
		if *full {
			return errDiskFull
		}
		return writeRaw(path, raw)
	}
	return full
}

// A conversation whose writes fail several times in a row says so on its record, with an upsert on convs; the writes
// go on with a pause that grows, and the first that reaches the disk takes the word back.
func TestWriteFailuresShowOnTheRecordUntilAWriteSucceeds(t *testing.T) {
	r := newRig(t)
	r.open()
	r.idle()
	full := failingDisk(r)
	*full = true

	r.do(wire.ConversationRenameCmd{OpID: "rename-1", ID: r.id, Title: "kept in memory"})
	r.tick(time.Second)
	if r.rt.actor(r.id).c.saveFails == 0 {
		t.Fatal("the write did not fail")
	}
	if got := r.record().NotSavedSince; got != 0 {
		t.Fatalf("one failed write already says not saved (since %d)", got)
	}
	upserts := len(r.events(wire.EvConversationUpsert))
	for range 20 {
		r.tick(WriteRetryMax)
	}
	since := r.record().NotSavedSince
	if since == 0 {
		t.Fatal("the record does not say the conversation is not saved")
	}
	if len(r.events(wire.EvConversationUpsert)) == upserts {
		t.Fatal("no upsert told the page")
	}

	// The pause grows: the failures stay far below one per tick of the clock.
	r.tick(time.Millisecond)
	fails := r.rt.actor(r.id).c.saveFails
	r.tick(time.Millisecond)
	if got := r.rt.actor(r.id).c.saveFails; got != fails {
		t.Fatalf("a failed write was tried again at once: %d then %d failures", fails, got)
	}

	*full = false
	for range 3 {
		r.tick(WriteRetryMax)
	}
	if got := r.record().NotSavedSince; got != 0 {
		t.Fatalf("still not saved (since %d) after a write that succeeded", got)
	}

	// What the disk holds is the state the failures kept in memory, without the mark itself.
	files, err := r.rt.d.Store.loadConvs()
	if err != nil || len(files) != 1 {
		t.Fatalf("files %d, err %v", len(files), err)
	}
	if files[0].Conv.Title != "kept in memory" || files[0].Conv.NotSavedSince != 0 {
		t.Fatalf("written: title %q, notSavedSince %d", files[0].Conv.Title, files[0].Conv.NotSavedSince)
	}
}

// The writer keeps a state it could not write and tries it again; it tells of every failure and of the recovery.
func TestStoreWriterRetriesAFailedWrite(t *testing.T) {
	defer func(d time.Duration) { WriteRetry = d }(WriteRetry)
	WriteRetry = 5 * time.Millisecond
	s := NewStore(filepath.Join(t.TempDir(), "v2"))
	var mu sync.Mutex
	full, results := true, []int{}
	s.write = func(path string, raw []byte) error {
		mu.Lock()
		defer mu.Unlock()
		if full {
			return errDiskFull
		}
		return writeRaw(path, raw)
	}
	recovered := make(chan struct{})
	s.Async(func(path string, fails int, err error) {
		mu.Lock()
		defer mu.Unlock()
		if s.convOfPath(path) != "tab-one" {
			t.Errorf("result for %s", path)
		}
		results = append(results, fails)
		if fails == 0 {
			close(recovered)
		}
	})
	defer s.Close()
	if err := s.saveConv(convFile{Conv: wire.Conversation{ID: "tab-one", Title: "queued", State: wire.AgentState{K: wire.StateStopped}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err == nil {
		t.Fatal("a flush of a file that cannot be written answered nil")
	}
	mu.Lock()
	full = false
	mu.Unlock()
	select {
	case <-recovered:
	case <-time.After(5 * time.Second):
		t.Fatal("the write was not tried again")
	}
	mu.Lock()
	defer mu.Unlock()
	for i, fails := range results[:len(results)-1] {
		if fails != i+1 {
			t.Fatalf("failures in a row: %v", results)
		}
	}
	var f convFile
	if err := readJSON(s.convPath("tab-one"), &f); err != nil || f.Conv.Title != "queued" {
		t.Fatalf("on disk: %+v, %v", f.Conv, err)
	}
}

// A write that hangs (a file system that does not answer) holds neither a flush nor the close past their term.
func TestFlushReturnsAtItsTerm(t *testing.T) {
	defer func(d time.Duration) { FlushWait = d }(FlushWait)
	FlushWait = 50 * time.Millisecond
	s := NewStore(filepath.Join(t.TempDir(), "v2"))
	hang := make(chan struct{})
	defer close(hang)
	s.write = func(string, []byte) error { <-hang; return nil }
	s.Async(nil)
	if err := s.saveConv(convFile{Conv: wire.Conversation{ID: "tab-one", State: wire.AgentState{K: wire.StateStopped}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := s.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("flush: %v", err)
	}
	if err := s.Close(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the flush and the close took %s", took)
	}
}

// A start removes the temporary files a killed write left: the store's own by their name, and only old ones.
func TestStartSweepsOldTemporaryFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "v2")
	convs := filepath.Join(dir, "convs")
	if err := os.MkdirAll(convs, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-2 * tempKept)
	files := map[string]struct {
		at   time.Time
		gone bool
	}{
		filepath.Join(convs, ".tab-one.json.123456789"): {old, true},
		filepath.Join(dir, ".ops.json.42"):              {old, true},
		filepath.Join(convs, ".tab-two.json.987"):       {now, false}, // a write that may still be going
		filepath.Join(convs, "tab-one.json"):            {old, false},
		filepath.Join(convs, ".tab-one.json.bak"):       {old, false},
		filepath.Join(convs, ".json.123"):               {old, false},
		filepath.Join(convs, ".notes.txt.123"):          {old, false},
		filepath.Join(dir, ".hidden"):                   {old, false},
	}
	for path, f := range files {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, f.at, f.at); err != nil {
			t.Fatal(err)
		}
	}
	if n := NewStore(dir).sweepTemps(now); n != 2 {
		t.Fatalf("removed %d files", n)
	}
	for path, f := range files {
		_, err := os.Stat(path)
		if gone := errors.Is(err, os.ErrNotExist); gone != f.gone {
			t.Errorf("%s: gone %v, want %v", filepath.Base(path), gone, f.gone)
		}
	}
}
