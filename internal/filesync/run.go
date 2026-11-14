package filesync

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// settle is how long a path must stay quiet before it is sent: an agent may write a file in pieces.
	settle = 300 * time.Millisecond
	// rescanEvery catches what the watcher missed (an overflowed queue, a folder it could not watch).
	rescanEvery = 2 * time.Minute
	pollWait    = 25 * time.Second
)

var (
	// watchlessScan is how often the folder is walked while the system gives no watcher.
	watchlessScan = 5 * time.Second
	// newWatcher makes the watcher of the folder; a variable for the test of a system that gives none.
	newWatcher = fsnotify.NewWatcher
)

// Run syncs until ctx ends: a first full sync, then the watcher, the long poll and a periodic rescan.
//
// The watcher is on the folder before the first sync reads it: an edit made while the first sync runs is then either
// read by it or told by the watcher, and not left for the rescan two minutes later. A system that gives no watcher
// (every inotify instance of the user is taken: 128 by default, and a machine runs many runtimes) does not stop the
// sync: Run used to return the error, the caller started it again ten seconds later with one more full first sync,
// and nothing arrived from the store in between. The folder is walked every watchlessScan instead until a watcher
// can be made.
func (a *Agent) Run(ctx context.Context) error {
	w, werr := newWatcher()
	if werr == nil {
		a.watchTree(w, ".")
	}
	defer func() {
		if w != nil {
			w.Close()
		}
	}()
	for {
		err := a.SyncOnce(ctx)
		if err == nil {
			break
		}
		a.log.Warn("first sync", "err", err)
		if !sleep(ctx, 5*time.Second) {
			return ctx.Err()
		}
	}
	a.log.Info("in sync", "root", a.cfg.Root)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.pollLoop(ctx) }()
	go func() { defer wg.Done(); a.rescanLoop(ctx) }()
	if werr != nil {
		a.log.Warn("no file watcher: the folder is walked instead until there is one", "err", werr, "every", watchlessScan)
		w = a.scanUntilWatched(ctx)
	}
	if w != nil {
		a.watchLoop(ctx, w)
	}
	wg.Wait()
	return ctx.Err()
}

// scanUntilWatched walks the folder every watchlessScan until a watcher can be made (it is returned, on the tree) or
// ctx ends (nil).
func (a *Agent) scanUntilWatched(ctx context.Context) *fsnotify.Watcher {
	for sleep(ctx, watchlessScan) {
		w, err := newWatcher()
		if err == nil {
			a.watchTree(w, ".")
		}
		// With a watcher too: what changed before it was on the tree is read by this walk.
		if err := a.ScanAll(ctx); err != nil {
			a.log.Warn("scan", "err", err)
		}
		if w != nil {
			a.log.Info("the file watcher is back")
			return w
		}
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (a *Agent) watchTree(w *fsnotify.Watcher, rel string) {
	_ = fs.WalkDir(a.root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != "." && a.policy.SkippedDir(p) || p == a.tmp {
			return fs.SkipDir
		}
		if err := w.Add(filepath.Join(a.cfg.Root, filepath.FromSlash(p))); err != nil {
			a.log.Warn("watch", "dir", p, "err", err)
		}
		return nil
	})
}

func (a *Agent) watchLoop(ctx context.Context, w *fsnotify.Watcher) {
	pending := map[string]time.Time{}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	prefix := filepath.Clean(a.cfg.Root) + string(filepath.Separator)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			rel, found := strings.CutPrefix(ev.Name, prefix)
			if !found {
				continue
			}
			rel = filepath.ToSlash(rel)
			if rel == a.tmp || strings.HasPrefix(rel, a.tmp+"/") || a.cfg.ConflictDir != "" && strings.HasPrefix(rel, a.cfg.ConflictDir+"/") {
				continue
			}
			if fi, err := a.root.Lstat(rel); err == nil && fi.IsDir() && a.policy.SkippedDir(rel) {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if fi, err := a.root.Lstat(rel); err == nil && fi.IsDir() {
					a.watchTree(w, rel)
				}
			}
			pending[rel] = time.Now()
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			// An overflow loses events: look at everything again.
			a.log.Warn("watcher", "err", err)
			pending["."] = time.Now()
		case now := <-tick.C:
			for rel, at := range pending {
				if now.Sub(at) < settle {
					continue
				}
				delete(pending, rel)
				var err error
				if rel == "." {
					err = a.ScanAll(ctx)
				} else {
					err = a.Scan(ctx, rel)
				}
				if err != nil {
					a.log.Warn("send", "path", rel, "err", err)
					// The hub may be away: try again later, the edit stays on disk.
					pending[rel] = now.Add(5 * time.Second)
				}
			}
		}
	}
}

func (a *Agent) pollLoop(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		more, err := a.Pull(ctx, pollWait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.log.Warn("pull", "err", err, "retry_in", backoff)
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		_ = more
	}
}

func (a *Agent) rescanLoop(ctx context.Context) {
	for sleep(ctx, rescanEvery) {
		if err := a.ScanAll(ctx); err != nil {
			a.log.Warn("rescan", "err", err)
		}
		if err := a.RetryQuarantine(ctx); err != nil {
			a.log.Warn("quarantine retry", "err", err)
		}
		if q, err := a.Quarantined(); err == nil && len(q) > 0 {
			a.log.Warn("files in quarantine, not synced", "count", len(q), "first", q[0].Path, "state", a.cfg.StateDir)
		}
	}
}
