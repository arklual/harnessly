package settingsctl

import (
	"context"
	"sync"
	"time"
)

// watch tells the pages that something moved (a setting was changed, a proposal came or was answered), so that they
// read the journal and the proposals when there is something to read instead of asking every few seconds.
type watch struct {
	mu  sync.Mutex
	rev uint64
	ch  chan struct{}
}

// start gives the revision its first value; the caller holds mu. It is the time, so that a page that watched the
// runtime before its restart sees another revision and reads again.
func (w *watch) start() {
	if w.ch == nil {
		w.rev = uint64(time.Now().UnixMilli())
		w.ch = make(chan struct{})
	}
}

func (s *Service) bump() {
	w := &s.watch
	w.mu.Lock()
	defer w.mu.Unlock()
	w.start()
	w.rev++
	close(w.ch)
	w.ch = make(chan struct{})
}

// Rev is the revision of the journal and the proposals: it moves with every change of either.
func (s *Service) Rev() uint64 {
	w := &s.watch
	w.mu.Lock()
	defer w.mu.Unlock()
	w.start()
	return w.rev
}

// WaitRev answers the revision as soon as it is not `after`, or after d when nothing moved.
func (s *Service) WaitRev(ctx context.Context, after uint64, d time.Duration) uint64 {
	w := &s.watch
	w.mu.Lock()
	w.start()
	rev, ch := w.rev, w.ch
	w.mu.Unlock()
	if rev != after {
		return rev
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
		// A proposal that ran out of time while nobody asked moves the revision here, so its card leaves the page.
		s.mu.Lock()
		s.prune(s.now())
		s.mu.Unlock()
	case <-ctx.Done():
	}
	return s.Rev()
}

// Touch moves the revision without a change of a setting: a page that watches reads what else it watches (the
// agents' looks, cmd/runtime routes_looks.go).
func (s *Service) Touch() { s.bump() }
