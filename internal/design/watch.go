package design

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// One watcher of the library's root for everything that wants to know when a file of it changed: the listing's
// caches, the mode normaliser and the history. inotify is a watch per folder, so folders are added as they appear;
// the folders nobody needs (dependencies, git data, claims that change every few seconds) are not watched.

var unwatched = map[string]bool{"node_modules": true, ".git": true, ".claims": true}

const maxWatchedDirs = 20000

type rootWatch struct {
	root     string
	w        *fsnotify.Watcher
	mu       sync.Mutex
	dirs     int
	subs     map[int]func(rel string)
	next     int
	done     chan struct{}
	stopping bool
}

type watchState struct {
	mu        sync.Mutex
	cur       *rootWatch
	triedAt   time.Time
	listeners map[int]func(rel string)
	next      int
}

// watching is whether the root's watcher runs; it is started on first need and again a while after it failed.
func (l *Library) watching() bool {
	ws := &l.listing.watch
	ws.mu.Lock()
	defer ws.mu.Unlock()
	root := l.Root()
	if ws.cur != nil && ws.cur.root == root {
		return true
	}
	if ws.cur != nil {
		ws.cur.close()
		ws.cur = nil
	}
	if time.Since(ws.triedAt) < 5*time.Second {
		return false
	}
	ws.triedAt = time.Now()
	rw, err := startRootWatch(root, l)
	if err != nil {
		return false
	}
	ws.cur = rw
	return true
}

// Subscribe calls fn with the path (relative to the root) of every change while the watcher runs; the returned func
// ends it. ok is false when the root cannot be watched now (it is not there yet).
func (l *Library) Subscribe(fn func(rel string)) (stop func(), ok bool) {
	ok = l.watching()
	ws := &l.listing.watch
	ws.mu.Lock()
	if ws.listeners == nil {
		ws.listeners = map[int]func(string){}
	}
	ws.next++
	id := ws.next
	ws.listeners[id] = fn
	ws.mu.Unlock()
	return func() {
		ws.mu.Lock()
		delete(ws.listeners, id)
		ws.mu.Unlock()
	}, ok
}

func (l *Library) notify(rel string) {
	l.listing.invalidatePath(rel)
	ws := &l.listing.watch
	ws.mu.Lock()
	fns := make([]func(string), 0, len(ws.listeners))
	for _, fn := range ws.listeners {
		fns = append(fns, fn)
	}
	ws.mu.Unlock()
	for _, fn := range fns {
		fn(rel)
	}
}

// lost: the watcher ended (the root went away); everything cached is read again.
func (l *Library) lost(rw *rootWatch) {
	ws := &l.listing.watch
	ws.mu.Lock()
	if ws.cur == rw {
		ws.cur = nil
	}
	ws.mu.Unlock()
	l.listing.invalidateAll()
}

// StopWatching closes the watcher (a test's cleanup).
func (l *Library) StopWatching() {
	ws := &l.listing.watch
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.cur != nil {
		ws.cur.close()
		ws.cur = nil
	}
}

func startRootWatch(root string, l *Library) (*rootWatch, error) {
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return nil, errors.New("no library folder")
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	rw := &rootWatch{root: root, w: w, done: make(chan struct{})}
	if err := rw.addTree(root); err != nil {
		_ = w.Close()
		return nil, err
	}
	go rw.loop(l)
	return rw, nil
}

// addTree watches dir and the folders under it, links not entered.
func (rw *rootWatch) addTree(dir string) error {
	var first error
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				first = err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != dir && unwatched[d.Name()] {
			return filepath.SkipDir
		}
		rw.mu.Lock()
		full := rw.dirs >= maxWatchedDirs
		if !full {
			rw.dirs++
		}
		rw.mu.Unlock()
		if full {
			return filepath.SkipAll
		}
		if err := rw.w.Add(p); err != nil && p == dir {
			first = err
		}
		return nil
	})
	return first
}

func (rw *rootWatch) close() {
	rw.mu.Lock()
	rw.stopping = true
	rw.mu.Unlock()
	_ = rw.w.Close()
}

func (rw *rootWatch) loop(l *Library) {
	defer close(rw.done)
	for {
		select {
		case ev, ok := <-rw.w.Events:
			if !ok {
				rw.mu.Lock()
				stopping := rw.stopping
				rw.mu.Unlock()
				if !stopping {
					l.lost(rw)
				}
				return
			}
			if ev.Name == rw.root && ev.Has(fsnotify.Remove|fsnotify.Rename) {
				_ = rw.w.Close()
				l.lost(rw)
				return
			}
			rel, err := filepath.Rel(rw.root, ev.Name)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if st, err := os.Lstat(ev.Name); err == nil && st.IsDir() && !unwatched[filepath.Base(ev.Name)] {
					_ = rw.addTree(ev.Name)
				}
			}
			l.notify(rel)
		case err, ok := <-rw.w.Errors:
			if !ok {
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				l.listing.invalidateAll()
				l.notify(".")
			}
		}
	}
}

// listingCache keeps what the listing reads from disk until the watcher sees it change.
type listingCache struct {
	watch watchState

	mu       sync.Mutex
	gen      uint64
	refGen   map[string]uint64
	versions map[string]versionEntry
	arts     map[string]artEntry
	refsList []string
	refsGen  uint64
	refsAt   uint64
	refsOK   bool
}

type versionEntry struct {
	v   int64
	at  time.Time
	gen uint64
}

type artEntry struct {
	a   *Artifact
	gen uint64
}

func (c *listingCache) init() {
	if c.versions == nil {
		c.versions = map[string]versionEntry{}
		c.arts = map[string]artEntry{}
		c.refGen = map[string]uint64{}
	}
}

// genOf is the generation of a ref: a change under it or anywhere at once moves it on.
func (c *listingCache) genOf(ref string) uint64 { return c.gen + c.refGen[ref] }

func (c *listingCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	c.gen++
}

func (c *listingCache) invalidatePath(rel string) {
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.init()
	if rel == "." || first == "" {
		c.gen++
		return
	}
	if !strings.Contains(filepath.ToSlash(rel), "/") {
		c.refsGen++
	}
	if IsLibraryID(first) {
		c.refGen[first]++
	}
}

func (c *listingCache) version(ref string, maxAge time.Duration, watching bool, compute func() int64) int64 {
	c.mu.Lock()
	c.init()
	g := c.genOf(ref)
	e, ok := c.versions[ref]
	c.mu.Unlock()
	if ok && ((watching && e.gen == g) || (!watching && time.Since(e.at) < maxAge)) {
		return e.v
	}
	v := compute()
	c.mu.Lock()
	c.versions[ref] = versionEntry{v: v, at: time.Now(), gen: g}
	c.mu.Unlock()
	return v
}

func (c *listingCache) artifact(ref string, watching bool, compute func() *Artifact) *Artifact {
	if !watching {
		return compute()
	}
	c.mu.Lock()
	c.init()
	g := c.genOf(ref)
	e, ok := c.arts[ref]
	c.mu.Unlock()
	if ok && e.gen == g {
		return e.a
	}
	a := compute()
	c.mu.Lock()
	c.arts[ref] = artEntry{a: a, gen: g}
	c.mu.Unlock()
	return a
}

func (c *listingCache) refs(watching bool, compute func() []string) []string {
	if !watching {
		return compute()
	}
	c.mu.Lock()
	c.init()
	g := c.gen + c.refsGen
	if c.refsOK && c.refsAt == g {
		out := c.refsList
		c.mu.Unlock()
		return out
	}
	c.mu.Unlock()
	refs := compute()
	c.mu.Lock()
	c.refsList, c.refsOK, c.refsAt = refs, true, g
	c.mu.Unlock()
	return refs
}
