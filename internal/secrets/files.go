package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// DefaultFileTTL is how long a secret's file lives when the command names no time; MaxFileTTL the longest it may.
const (
	DefaultFileTTL = 15 * time.Minute
	MaxFileTTL     = 2 * time.Hour
)

// Files writes a secret into a file only its owner reads, for a tool that takes a key by path, and removes it when its
// time is up (or when the runtime starts again: the folder is swept then). The folder is in memory when the machine has
// /dev/shm, so a value does not reach the disk.
type Files struct {
	Dir string

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// NewFiles is the files in base/harnessly-safe-<uid>, whatever an earlier runtime left there removed.
func NewFiles(base string) *Files {
	if base == "" {
		base = os.TempDir()
		if st, err := os.Stat("/dev/shm"); err == nil && st.IsDir() {
			base = "/dev/shm"
		}
	}
	dir := filepath.Join(base, "harnessly-safe-"+strconv.Itoa(os.Getuid()))
	_ = os.RemoveAll(dir)
	return &Files{Dir: dir, timers: map[string]*time.Timer{}}
}

// Write puts value into a new file named after the secret and returns its path; it is removed after ttl.
func (f *Files) Write(name, value string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = DefaultFileTTL
	}
	ttl = min(ttl, MaxFileTTL)
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return "", refuse(ReasonFailed, err)
	}
	if err := os.Chmod(f.Dir, 0o700); err != nil {
		return "", refuse(ReasonFailed, err)
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	sub := filepath.Join(f.Dir, hex.EncodeToString(b))
	if err := os.Mkdir(sub, 0o700); err != nil {
		return "", refuse(ReasonFailed, err)
	}
	path := filepath.Join(sub, name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		_ = os.RemoveAll(sub)
		return "", refuse(ReasonFailed, err)
	}
	f.mu.Lock()
	f.timers[sub] = time.AfterFunc(ttl, func() { f.remove(sub) })
	f.mu.Unlock()
	return path, nil
}

func (f *Files) remove(sub string) {
	f.mu.Lock()
	if t := f.timers[sub]; t != nil {
		t.Stop()
	}
	delete(f.timers, sub)
	f.mu.Unlock()
	_ = os.RemoveAll(sub)
}

// Close removes every file now.
func (f *Files) Close() {
	f.mu.Lock()
	subs := make([]string, 0, len(f.timers))
	for sub := range f.timers {
		subs = append(subs, sub)
	}
	f.mu.Unlock()
	for _, sub := range subs {
		f.remove(sub)
	}
}
