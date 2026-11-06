//go:build unix

package supervise

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// reaper collects every ended process of the container, ours and orphans alike. It is the only caller of wait, so the
// supervisor hands it the pids it started instead of calling Wait on them.
type reaper struct {
	mu      sync.Mutex
	watched map[int]chan waitResult
	early   map[int]waitResult // ended before anyone watched it
	sig     chan os.Signal
	quit    chan struct{}
}

func startReaper() *reaper {
	r := &reaper{
		watched: map[int]chan waitResult{},
		early:   map[int]waitResult{},
		sig:     make(chan os.Signal, 16),
		quit:    make(chan struct{}),
	}
	signal.Notify(r.sig, syscall.SIGCHLD)
	go r.loop()
	return r
}

func (r *reaper) stop() {
	signal.Stop(r.sig)
	close(r.quit)
}

func (r *reaper) loop() {
	for {
		r.collect()
		select {
		case <-r.sig:
		case <-r.quit:
			return
		}
	}
}

func (r *reaper) collect() {
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
		if pid <= 0 || err != nil {
			return
		}
		res := waitResult{Code: ws.ExitStatus()}
		if ws.Signaled() {
			res = waitResult{Signal: ws.Signal()}
		}
		r.mu.Lock()
		if ch, ok := r.watched[pid]; ok {
			delete(r.watched, pid)
			ch <- res
		} else {
			// Either an orphan (nobody will ask) or a child that ended before watch was called.
			if len(r.early) > 256 {
				r.early = map[int]waitResult{}
			}
			r.early[pid] = res
		}
		r.mu.Unlock()
	}
}

// watch returns a channel that gets the end of pid.
func (r *reaper) watch(pid int) chan waitResult {
	ch := make(chan waitResult, 1)
	r.mu.Lock()
	defer r.mu.Unlock()
	if res, ok := r.early[pid]; ok {
		delete(r.early, pid)
		ch <- res
		return ch
	}
	r.watched[pid] = ch
	return ch
}
