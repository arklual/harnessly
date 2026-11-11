package designview

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A blob just fetched for a fill has no other link yet: pruning waits for the fill instead of taking it away.
func TestPruneWaitsForAFillThatLinksTheBlobs(t *testing.T) {
	v := &View{o: Options{Dir: t.TempDir()}}
	blob := filepath.Join(v.o.Dir, "blobs", "s", "ab", "abcd")
	if err := os.MkdirAll(filepath.Dir(blob), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	pruneMu.RLock() // a fill between fetching the blob and linking it
	done := make(chan struct{})
	go func() { v.pruneBlobs(); close(done) }()
	select {
	case <-done:
		pruneMu.RUnlock()
		t.Fatal("pruning ran while a fill held the blobs")
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := os.Stat(blob); err != nil {
		t.Fatalf("the blob went during the fill: %v", err)
	}
	pruneMu.RUnlock()
	<-done
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Fatalf("a lone blob stays after the fill: %v", err)
	}
}
