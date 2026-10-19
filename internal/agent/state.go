package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Files of the runtime's data directory the cluster keeps a copy of.
var (
	RootStateFiles      = []string{"workspaces.json", "quick-prompts.json", "sidebar-items.json", "keybindings.json"}
	WorkspaceStateFiles = []string{"layout.json", "message-history.json"}

	workspaceDirName   = regexp.MustCompile(`^[\w-]+$`)
	layoutStateName    = regexp.MustCompile(`^workspaces/[\w-]+/layout\.json$`)
	messageHistoryName = regexp.MustCompile(`^workspaces/[\w-]+/message-history\.json$`)
)

// IsLayoutStateName: the files the cluster keeps the truth of as versioned documents.
func IsLayoutStateName(name string) bool {
	return name == "workspaces.json" || layoutStateName.MatchString(name)
}

// IsClusterOwnedName: files nothing on the machine writes but the cluster's own routes.
func IsClusterOwnedName(name string) bool {
	return name == "quick-prompts.json" || name == "sidebar-items.json" || name == "keybindings.json" || messageHistoryName.MatchString(name)
}

const stateDebounce = 30 * time.Millisecond

// stateFiles watches the state files and writes the ones the cluster sends. The runtime replaces its files (write a
// temporary file, rename it over), so a watch on a file dies with the old inode: directories are watched instead, and
// a once-a-second stat of the few files catches anything the watcher missed.
type stateFiles struct {
	dir       string
	rootFiles []string
	broadcast func(any)
	// layoutWritten tells the runtime the cluster changed one of its layouts.
	layoutWritten func(name string)

	mu      sync.Mutex
	seen    map[string]float64
	pending map[string]*time.Timer
	watcher *fsnotify.Watcher
	watched map[string]bool
}

func newStateFiles(dir string, extraRootFiles []string, broadcast func(any), layoutWritten func(string)) *stateFiles {
	s := &stateFiles{dir: dir, rootFiles: append(slices.Clone(RootStateFiles), extraRootFiles...), broadcast: broadcast, layoutWritten: layoutWritten, seen: map[string]float64{}, pending: map[string]*time.Timer{}, watched: map[string]bool{}}
	if w, err := fsnotify.NewWatcher(); err == nil {
		s.watcher = w
		go s.events()
	} else {
		Log("warn", "state watcher unavailable, sweeping only", F{"error", err.Error()})
	}
	return s
}

func (s *stateFiles) workspaceDirs() []string {
	entries, _ := os.ReadDir(filepath.Join(s.dir, "workspaces"))
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && workspaceDirName.MatchString(e.Name()) {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}

func (s *stateFiles) names() []string {
	names := slices.Clone(s.rootFiles)
	for _, dir := range s.workspaceDirs() {
		for _, f := range WorkspaceStateFiles {
			names = append(names, "workspaces/"+dir+"/"+f)
		}
	}
	return names
}

// List is every state file the runtime has written.
func (s *stateFiles) List() []StateFile {
	files := []StateFile{}
	for _, name := range s.names() {
		if raw, err := os.ReadFile(filepath.Join(s.dir, name)); err == nil {
			files = append(files, StateFile{Name: name, Content: string(raw)})
		}
	}
	return files
}

func mtimeOf(path string) (float64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	_, m := fileID(info)
	return m, true
}

func (s *stateFiles) push(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.pending[name]; ok {
		t.Stop()
	}
	s.pending[name] = time.AfterFunc(stateDebounce, func() {
		s.mu.Lock()
		delete(s.pending, name)
		s.mu.Unlock()
		path := filepath.Join(s.dir, name)
		var content *string
		mtime, exists := mtimeOf(path)
		if exists {
			if raw, err := os.ReadFile(path); err == nil {
				c := string(raw)
				content = &c
			}
		}
		s.mu.Lock()
		s.seen[name] = mtime
		s.mu.Unlock()
		s.broadcast(evState{Event: "state.changed", Name: name, Content: content})
	})
}

func (s *stateFiles) watch(dir string) {
	if s.watcher == nil {
		return
	}
	s.mu.Lock()
	done := s.watched[dir]
	s.mu.Unlock()
	if done {
		return
	}
	if err := s.watcher.Add(dir); err == nil {
		s.mu.Lock()
		s.watched[dir] = true
		s.mu.Unlock()
	}
}

// nameOf maps a watcher event to a state file name, or "".
func (s *stateFiles) nameOf(path string) string {
	dir, base := filepath.Dir(path), filepath.Base(path)
	if dir == s.dir {
		if slices.Contains(s.rootFiles, base) {
			return base
		}
		return ""
	}
	if filepath.Dir(dir) == filepath.Join(s.dir, "workspaces") && workspaceDirName.MatchString(filepath.Base(dir)) && slices.Contains(WorkspaceStateFiles, base) {
		return "workspaces/" + filepath.Base(dir) + "/" + base
	}
	return ""
}

func (s *stateFiles) events() {
	for {
		select {
		case ev, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				s.mu.Lock()
				delete(s.watched, ev.Name)
				s.mu.Unlock()
			}
			if name := s.nameOf(ev.Name); name != "" {
				s.push(name)
			}
		case _, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// Sweep re-arms the watchers and stats every file.
func (s *stateFiles) Sweep() {
	s.watch(s.dir)
	s.watch(filepath.Join(s.dir, "workspaces"))
	for _, dir := range s.workspaceDirs() {
		s.watch(filepath.Join(s.dir, "workspaces", dir))
	}
	names := s.names()
	for _, name := range names {
		mtime, _ := mtimeOf(filepath.Join(s.dir, name))
		s.mu.Lock()
		seen, known := s.seen[name]
		_, pending := s.pending[name]
		if !known {
			s.seen[name] = mtime
		}
		s.mu.Unlock()
		if known && mtime != seen && !pending {
			s.push(name)
		}
	}
	s.mu.Lock()
	var gone []string
	for name := range s.seen {
		if !slices.Contains(names, name) {
			delete(s.seen, name)
			gone = append(gone, name)
		}
	}
	s.mu.Unlock()
	for _, name := range gone {
		s.broadcast(evState{Event: "state.changed", Name: name, Content: nil})
	}
}

type putResult struct {
	Written bool   `json:"written"`
	Reason  string `json:"reason,omitempty"`
}

// Put writes the cluster's copy of a file it owns, the way the runtime writes its files (temporary file, rename
// over), and marks it seen. A layout or the workspace list moves only forward: an older version than the file has is
// skipped, and a copy the runtime changed while it could not reach the cluster is replaced only by the cluster's
// answer to that very copy (replacesDirty, its sha256). renumber writes whatever version the file names.
func (s *stateFiles) Put(name, content string, version *float64, replacesDirty string, renumber bool) (putResult, error) {
	layout := IsLayoutStateName(name)
	if !IsClusterOwnedName(name) && !layout {
		return putResult{}, errors.New("not a file the cluster owns")
	}
	if !json.Valid([]byte(content)) {
		return putResult{}, errors.New("content is not JSON")
	}
	file := filepath.Join(s.dir, name)
	if layout {
		if current, err := os.ReadFile(file); err == nil {
			var doc struct {
				StateVersion *float64 `json:"stateVersion"`
				StateDirty   bool     `json:"stateDirty"`
			}
			_ = json.Unmarshal(current, &doc) // a file caught mid-write: the cluster's copy replaces it
			if doc.StateDirty {
				sum := sha256.Sum256(current)
				if hex.EncodeToString(sum[:]) != replacesDirty {
					return putResult{Written: false, Reason: "dirty"}, nil
				}
			} else if !renumber && version != nil {
				have := 0.0
				if doc.StateVersion != nil {
					have = *doc.StateVersion
				}
				if have >= *version {
					return putResult{Written: false, Reason: "older"}, nil
				}
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return putResult{}, err
	}
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".put"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		_ = os.Remove(tmp)
		return putResult{}, err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return putResult{}, err
	}
	if m, ok := mtimeOf(file); ok {
		s.mu.Lock()
		s.seen[name] = m
		s.mu.Unlock()
	}
	if layout {
		go s.layoutWritten(name)
	}
	return putResult{Written: true}, nil
}

type stateMode struct {
	Owned    bool     `json:"owned"`
	Headless bool     `json:"headless"`
	GoSlices []string `json:"goSlices,omitempty"`
	At       string   `json:"at,omitempty"`
}

// WriteMode records whether the runtime writes its layouts through the cluster, answers its API without its web
// server, and which slices the Go runtime gate answers itself; the runtimes read it before each request.
func (s *stateFiles) WriteMode(owned, headless bool, goSlices []string) (stateMode, error) {
	file := filepath.Join(s.dir, "state-mode.json")
	raw, _ := marshal(stateMode{Owned: owned, Headless: headless, GoSlices: goSlices, At: isoNow()})
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return stateMode{}, err
	}
	if err := os.Rename(tmp, file); err != nil {
		return stateMode{}, err
	}
	return stateMode{Owned: owned, Headless: headless}, nil // the answer is what it always was
}
