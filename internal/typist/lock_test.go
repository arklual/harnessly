package typist

import (
	"sync"
	"testing"
)

// A pane's lock is kept only while it is held or waited for: the panes of ended sessions leave nothing behind.
func TestPaneLocksAreFreed(t *testing.T) {
	before := Locks()
	unlock := Lock("lock-test-a")
	if _, ok := TryLock("lock-test-a"); ok {
		t.Fatal("the lock was taken twice")
	}
	if Locks() != before+1 {
		t.Fatalf("locks %d", Locks()-before)
	}

	// Writers that wait hold the same lock, one at a time.
	var wg sync.WaitGroup
	inside := 0
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := Lock("lock-test-a")
			inside++
			if inside != 1 {
				t.Error("two writers in the pane")
			}
			inside--
			u()
		}()
	}
	unlock()
	unlock() // safe twice
	wg.Wait()

	if u, ok := TryLock("lock-test-a"); !ok {
		t.Fatal("the lock is free and was not taken")
	} else {
		u()
	}
	for _, pane := range []string{"lock-test-b", "lock-test-c"} {
		Lock(pane)()
	}
	if Locks() != before {
		t.Fatalf("%d locks left behind", Locks()-before)
	}
}
