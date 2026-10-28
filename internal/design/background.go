package design

import (
	"sync"
	"time"
)

// What this runtime keeps doing for the shared library while its design slice runs, whether or not anyone has the
// library open: the claims of its chats are refreshed or let go (only while the tab keeper runs: without it every
// chat would look closed), the files are saved to the history, and the PDFs this runtime keeps for the guests of its
// decks are built again when their decks change (deckpdfauto.go).

const sweepEvery = 5 * time.Second

type background struct {
	mu      sync.Mutex
	stop    chan struct{}
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
	warn := l.warn
	b := &l.bg
	b.mu.Lock()
	if b.stop == nil {
		b.stop = make(chan struct{})
		b.missing = map[string]int64{}
		b.warn = warn
		go l.keepClaims(b.stop)
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

func (l *Library) keepClaims(stop chan struct{}) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if _, ok := l.Tabs(); !ok {
				continue
			}
			if _, err := l.SweepOwnClaims(l.keeperMissing()); err != nil && l.bg.warn != nil {
				l.bg.warn("design claim sweep failed", err)
			}
		}
	}
}
