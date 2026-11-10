package apps

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// hl.files.watch (HRN-850): a view hears which files of its masks changed, not what is in them; it reads them again
// with hl.files.read when it wants to. One watch per view session; it ends with hl.files.unwatch, with the session, or
// when the machine has too many.

// FileChange is a batch of changed files of one view's watch.
type FileChange struct {
	Session string   `json:"session"`
	Paths   []string `json:"paths"`
}

const (
	// MaxFileWatches is how many views of the machine watch files at once.
	MaxFileWatches = 32
	// maxWatchedDirs bounds the folders one watch follows: a deeper tree is followed to that many.
	maxWatchedDirs = 2000
	// maxChangedPaths is how many paths one change names; more are said as the count's first ones.
	maxChangedPaths = 200
	watchSettle     = 300 * time.Millisecond
	watchAliveEvery = 5 * time.Second
)

// FileWatches are the watches of a machine's views.
type FileWatches struct {
	mu      sync.Mutex
	byToken map[string]*fileWatch
	subs    map[int]func(FileChange)
	next    int
}

type fileWatch struct {
	stop chan struct{}
	once sync.Once
}

func (w *fileWatch) end() { w.once.Do(func() { close(w.stop) }) }

// NewFileWatches is a machine with no watch.
func NewFileWatches() *FileWatches {
	return &FileWatches{byToken: map[string]*fileWatch{}, subs: map[int]func(FileChange){}}
}

// OnChange hears every change of every watch until the returned stop.
func (fw *FileWatches) OnChange(fn func(FileChange)) (stop func()) {
	fw.mu.Lock()
	id := fw.next
	fw.next++
	fw.subs[id] = fn
	fw.mu.Unlock()
	return func() {
		fw.mu.Lock()
		delete(fw.subs, id)
		fw.mu.Unlock()
	}
}

func (fw *FileWatches) tell(c FileChange) {
	fw.mu.Lock()
	subs := make([]func(FileChange), 0, len(fw.subs))
	for _, fn := range fw.subs {
		subs = append(subs, fn)
	}
	fw.mu.Unlock()
	for _, fn := range subs {
		fn(c)
	}
}

// Watching says whether a session watches.
func (fw *FileWatches) Watching(token string) bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.byToken[token] != nil
}

// Stop ends a session's watch; a session with none is no error.
func (fw *FileWatches) Stop(token string) {
	fw.mu.Lock()
	w := fw.byToken[token]
	delete(fw.byToken, token)
	fw.mu.Unlock()
	if w != nil {
		w.end()
	}
}

// Start follows the files of scope for a session. alive is asked now and then; the watch ends when it says no (the
// session is gone). A second Start of the same session takes the place of the first.
func (fw *FileWatches) Start(token string, scope FileScope, alive func() bool) error {
	if len(scope.ReadMasks) == 0 {
		return refuseFile("the app has no right to read files (files.read:<mask>)")
	}
	root, err := scope.root()
	if err != nil {
		return err
	}
	fw.mu.Lock()
	if fw.byToken[token] == nil && len(fw.byToken) >= MaxFileWatches {
		fw.mu.Unlock()
		return &BridgeError{Code: CodeRateLimited, Message: "too many views watch files on this machine"}
	}
	fw.mu.Unlock()
	nw, err := fsnotify.NewWatcher()
	if err != nil {
		return &BridgeError{Code: CodeHostUnavailable, Message: "files cannot be watched here", Detail: err.Error()}
	}
	dirs := 0
	add := func(dir string) {
		if dirs < maxWatchedDirs && nw.Add(dir) == nil {
			dirs++
		}
	}
	follow := func(top string) {
		_ = filepath.WalkDir(top, func(p string, e fs.DirEntry, err error) error {
			if err != nil || !e.IsDir() {
				if e != nil && e.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if rel != "." && (secret(rel) || !reachable(scope.ReadMasks, rel) || scope.hostOwn(p) ||
				(slices.Contains(skippedDirs, e.Name()) && !named(scope.ReadMasks, rel))) {
				return fs.SkipDir
			}
			if dirs >= maxWatchedDirs {
				return fs.SkipAll
			}
			add(p)
			return nil
		})
	}
	follow(root)

	w := &fileWatch{stop: make(chan struct{})}
	fw.mu.Lock()
	if old := fw.byToken[token]; old != nil {
		old.end()
	}
	fw.byToken[token] = w
	fw.mu.Unlock()

	go func() {
		defer nw.Close()
		defer func() {
			fw.mu.Lock()
			if fw.byToken[token] == w {
				delete(fw.byToken, token)
			}
			fw.mu.Unlock()
		}()
		pending := map[string]bool{}
		settle := time.NewTimer(time.Hour)
		settle.Stop()
		check := time.NewTicker(watchAliveEvery)
		defer check.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-check.C:
				if !alive() {
					return
				}
			case ev, ok := <-nw.Events:
				if !ok {
					return
				}
				rel, err := filepath.Rel(root, ev.Name)
				if err != nil || rel == "." {
					continue
				}
				rel = filepath.ToSlash(rel)
				if secret(rel) {
					continue
				}
				// A new folder the masks reach is followed too.
				if ev.Has(fsnotify.Create) && reachable(scope.ReadMasks, rel) {
					if info, err := os.Lstat(ev.Name); err == nil && info.IsDir() {
						follow(ev.Name)
					}
				}
				if !anyMask(scope.ReadMasks, rel) {
					continue
				}
				if len(pending) == 0 {
					settle.Reset(watchSettle)
				}
				if len(pending) < maxChangedPaths {
					pending[rel] = true
				}
			case err := <-nw.Errors:
				// An overflow or a folder gone: the watch goes on with what it still follows.
				_ = err
			case <-settle.C:
				// What is told is judged again at the moment it is told: an app turned off hears nothing more.
				if !alive() {
					return
				}
				paths := make([]string, 0, len(pending))
				for p := range pending {
					paths = append(paths, p)
				}
				slices.Sort(paths)
				clear(pending)
				fw.tell(FileChange{Session: token, Paths: paths})
			}
		}
	}()
	return nil
}
