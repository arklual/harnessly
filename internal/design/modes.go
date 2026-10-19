package design

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The library is one folder several teammates' agents write into. Some mkdir implementations make a folder under a
// default ACL with setgid and the sticky bit; a sticky folder lets only its owner rename or delete the files in it,
// so another teammate's agent cannot move a board it did not create. Clearing the bit on the folders this process
// owns, once at start and again as folders appear, keeps the folder shared in fact.

const (
	stickyBit       = 0o1000
	modesWalkBudget = 50000
	modesSettle     = 200 * time.Millisecond
)

var modesSkipped = map[string]bool{"node_modules": true, ".git": true, ".claims": true}

// ClearSticky clears the sticky bit of dir when it is a folder of this user's inside the library; true when it
// changed the mode. The folder is opened without following a link, so the mode read and changed is that of the very
// folder resolved.
func (l *Library) ClearSticky(dir string) bool {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	root, err := filepath.EvalSymlinks(l.Root())
	if err != nil || !isInside(real, root) {
		return false
	}
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSticky == 0 {
		return false
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Getuid() {
		return false
	}
	mode := uint32(st.Mode().Perm())
	if st.Mode()&os.ModeSetuid != 0 {
		mode |= syscall.S_ISUID
	}
	if st.Mode()&os.ModeSetgid != 0 {
		mode |= syscall.S_ISGID
	}
	return syscall.Fchmod(int(f.Fd()), mode) == nil
}

// ClearStickyTree is dir and every folder under it, links not entered; the folders changed.
func (l *Library) ClearStickyTree(dir string) []string {
	var changed []string
	left := modesWalkBudget
	var walk func(cur string)
	walk = func(cur string) {
		if l.ClearSticky(cur) {
			changed = append(changed, cur)
		}
		entries, _ := os.ReadDir(cur)
		for _, e := range entries {
			left--
			if left < 0 {
				return
			}
			if e.IsDir() && !modesSkipped[e.Name()] {
				walk(filepath.Join(cur, e.Name()))
			}
		}
	}
	if st, err := os.Lstat(dir); err == nil && st.IsDir() {
		walk(dir)
	}
	return changed
}

type modesState struct {
	mu      sync.Mutex
	stop    func()
	pending map[string]bool
	timer   *time.Timer
}

// WatchModes normalises the library and keeps watching it: a folder that appears is looked at a moment after it
// settles. The watch starts once; the folders the first pass changed are returned.
func (l *Library) WatchModes() []string {
	root := l.Root()
	m := &l.modes
	m.mu.Lock()
	if m.stop == nil {
		stop, ok := l.Subscribe(func(rel string) {
			for _, part := range splitPath(rel) {
				if modesSkipped[part] {
					return
				}
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.pending == nil {
				m.pending = map[string]bool{}
			}
			m.pending[filepath.Join(root, rel)] = true
			if m.timer != nil {
				m.timer.Stop()
			}
			m.timer = time.AfterFunc(modesSettle, func() {
				m.mu.Lock()
				paths := m.pending
				m.pending, m.timer = nil, nil
				m.mu.Unlock()
				for p := range paths {
					l.ClearStickyTree(p)
				}
			})
		})
		if ok {
			m.stop = stop
		} else {
			stop()
		}
	}
	m.mu.Unlock()
	return l.ClearStickyTree(root)
}

func (l *Library) StopModes() {
	m := &l.modes
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
}

func splitPath(rel string) []string { return strings.Split(filepath.ToSlash(rel), "/") }
