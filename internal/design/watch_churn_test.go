package design

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A folder tree that is made, moved and removed over and over while the watcher adds the folders it sees must not
// stall the watcher: closing it has to return.
func TestWatcherSurvivesFolderChurnAndCloses(t *testing.T) {
	l, library := historyLibrary(t)
	if !l.watching() {
		t.Fatal("the library is not watched")
	}
	stopChurn := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stopChurn:
					return
				default:
				}
				base := filepath.Join(library, fmt.Sprintf("c%d-%d", w, i%3))
				for d := 0; d < 12; d++ {
					_ = os.MkdirAll(filepath.Join(base, fmt.Sprintf("d%d", d), "x", "y"), 0o777)
				}
				moved := base + "-moved"
				_ = os.Rename(base, moved)
				_ = os.RemoveAll(moved)
				_ = os.RemoveAll(base)
			}
		}(w)
	}
	time.Sleep(1500 * time.Millisecond)
	close(stopChurn)
	wg.Wait()

	closed := make(chan struct{})
	go func() {
		l.StopWatching()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(20 * time.Second):
		t.Fatal("closing the library's watcher never returned")
	}
}
