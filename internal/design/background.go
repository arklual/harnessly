package design

import (
	"math/rand/v2"
	"sync"
	"time"
)

// What this runtime keeps doing for the shared library while its design slice runs, whether or not anyone has the
// library open: the claims of its chats are refreshed or let go (only while the tab keeper runs: without it every
// chat would look closed), the files are saved to the history, and the PDFs this runtime keeps for the guests of its
// decks are built again when their decks change (deckpdfauto.go).

// The claims are swept every sweepEvery while a chat of this runtime holds a board, and after one was just taken. A
// runtime that holds nothing has nothing to refresh or let go of: it looks again every sweepIdleEvery (spread, so the
// machines of a space do not ask together), which finds what a restart left behind. A center that does not answer is
// asked again every sweepOfflineEvery.
const (
	sweepEvery        = 10 * time.Second
	sweepIdleEvery    = 3 * time.Minute
	sweepOfflineEvery = 30 * time.Second
)

type background struct {
	mu      sync.Mutex
	stop    chan struct{}
	wake    chan struct{}
	sweepMu sync.Mutex
	missing map[string]int64
	warn    func(msg string, err error)
}

// keeperMissing is the keeper's memory of missing chats, nil before it started.
func (l *Library) keeperMissing() map[string]int64 {
	l.bg.mu.Lock()
	defer l.bg.mu.Unlock()
	if l.bg.stop == nil {
		return nil
	}
	return l.bg.missing
}

// StartBackground starts the claim keeper and the history; again while running it only retries the history (the
// library folder may have appeared since).
func (l *Library) StartBackground() {
	if l.cfg.Passive {
		return
	}
	warn := l.warn
	b := &l.bg
	b.mu.Lock()
	if b.stop == nil {
		b.stop = make(chan struct{})
		b.missing = map[string]int64{}
		b.warn = warn
		b.wake = make(chan struct{}, 1)
		go l.keepClaims(b.stop, b.wake)
		go l.keepDeckPdfs(b.stop)
	}
	b.mu.Unlock()
	id, err := l.Identity()
	if err != nil {
		return
	}
	l.startHistory(id.Runtime, func(msg string) {
		if warn != nil {
			warn("design history snapshot failed: "+msg, nil)
		}
	})
}

func (l *Library) StopBackground() {
	b := &l.bg
	b.mu.Lock()
	if b.stop != nil {
		close(b.stop)
		b.stop = nil
	}
	b.mu.Unlock()
	l.stopHistory()
}

// claimTaken tells the keeper that a chat of this runtime has just taken a board.
func (b *background) claimTaken() {
	b.mu.Lock()
	wake := b.wake
	b.mu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// sweepDelay is the wait before the next pass after one that answered res.
func sweepDelay(res SweepResult, err error) time.Duration {
	switch {
	case err != nil:
		return sweepEvery
	case res.Offline:
		return sweepOfflineEvery + rand.N(sweepOfflineEvery/2+1)
	case res.Own == 0:
		return sweepIdleEvery + rand.N(sweepIdleEvery/2+1)
	}
	return sweepEvery
}

func (l *Library) keepClaims(stop, wake chan struct{}) {
	t := time.NewTimer(sweepEvery)
	defer t.Stop()
	short := true
	for {
		select {
		case <-stop:
			return
		case <-wake:
			// Only a long wait is cut short: claims taken one after another must not put the pass off.
			if !short {
				short = true
				t.Reset(sweepEvery)
			}
			continue
		case <-t.C:
		}
		delay := sweepEvery
		if _, ok := l.Tabs(); ok {
			res, err := l.SweepOwnClaims(l.keeperMissing())
			if err != nil && l.bg.warn != nil {
				l.bg.warn("design claim sweep failed", err)
			}
			delay = sweepDelay(res, err)
		}
		short = delay <= sweepEvery
		t.Reset(delay)
	}
}
