package agent

import (
	"bytes"
	"fmt"
	base64 "github.com/segmentio/asm/base64"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	watchDebounce = 30 * time.Millisecond
	// pushMax bounds the lines sent along with a change event; more and the reader reads the file itself.
	pushMax = 1024 * 1024
)

type lineEnd struct {
	inode uint64
	end   int64
}

// transcriptWatch tells the cluster about transcripts as they change: a watcher on every directory of the two roots
// and, as a safety net, a full scan now and then.
type transcriptWatch struct {
	roots     Roots
	broadcast func(any)

	mu       sync.Mutex
	known    map[string]string
	pending  map[string]bool
	lineEnds map[string]lineEnd
	watcher  *fsnotify.Watcher
	watched  map[string]bool
}

func newTranscriptWatch(roots Roots, broadcast func(any)) *transcriptWatch {
	t := &transcriptWatch{roots: roots, broadcast: broadcast, known: map[string]string{}, pending: map[string]bool{}, lineEnds: map[string]lineEnd{}, watched: map[string]bool{}}
	if w, err := fsnotify.NewWatcher(); err == nil {
		t.watcher = w
		go t.events()
	} else {
		Log("warn", "transcript watcher unavailable, scanning only", F{"error", err.Error()})
	}
	return t
}

func key(size int64, inode uint64) string { return fmt.Sprintf("%d:%d", inode, size) }

// Scan compares every transcript with what the agent saw last and announces the changed ones.
func (t *transcriptWatch) Scan() error {
	files, err := ListTranscripts(t.roots)
	if err != nil {
		return err
	}
	next := make(map[string]string, len(files))
	for _, f := range files {
		next[f.Path] = key(f.Size, f.Inode)
	}
	t.mu.Lock()
	changed := []TranscriptFile{}
	for _, f := range files {
		if t.known[f.Path] != next[f.Path] {
			changed = append(changed, f)
		}
	}
	t.known = next
	t.mu.Unlock()
	if len(changed) > 0 {
		t.broadcast(evTranscripts{Event: "transcripts.changed", Files: changed})
	}
	return nil
}

// WatchRoots puts a watcher on every directory under the roots. A CLI that was never started has no directory yet;
// every scan tries again, so its first session is watched as soon as it appears.
func (t *transcriptWatch) WatchRoots() {
	for _, root := range []string{t.roots.Claude, t.roots.Codex} {
		t.watchTree(root)
	}
}

func (t *transcriptWatch) watchTree(root string) {
	if t.watcher == nil {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		t.mu.Lock()
		done := t.watched[path]
		t.mu.Unlock()
		if done {
			return nil
		}
		if t.watcher.Add(path) == nil {
			t.mu.Lock()
			t.watched[path] = true
			t.mu.Unlock()
		}
		return nil
	})
}

func (t *transcriptWatch) events() {
	for {
		select {
		case ev, ok := <-t.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				t.mu.Lock()
				delete(t.watched, ev.Name)
				t.mu.Unlock()
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Lstat(ev.Name); err == nil && info.IsDir() {
					t.watchTree(ev.Name)
					// A file written before the new directory was watched would otherwise wait for the next scan.
					_ = filepath.WalkDir(ev.Name, func(path string, d fs.DirEntry, err error) error {
						if err == nil && !d.IsDir() {
							t.onFile(path)
						}
						return nil
					})
					continue
				}
			}
			t.onFile(ev.Name)
		case _, ok := <-t.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (t *transcriptWatch) onFile(file string) {
	if !strings.HasSuffix(file, ".jsonl") || !t.roots.IsTranscriptPath(file) {
		return
	}
	provider := t.roots.provider(file)
	seenAt := time.Now().UnixMilli()
	t.mu.Lock()
	if t.pending[file] {
		t.mu.Unlock()
		return
	}
	t.pending[file] = true
	t.mu.Unlock()
	time.AfterFunc(watchDebounce, func() {
		t.mu.Lock()
		delete(t.pending, file)
		t.mu.Unlock()
		info, err := os.Stat(file)
		if err != nil {
			return // gone again before we looked; the next scan settles it
		}
		inode, mtime := fileID(info)
		k := key(info.Size(), inode)
		t.mu.Lock()
		if t.known[file] == k {
			t.mu.Unlock()
			return
		}
		t.known[file] = k
		t.mu.Unlock()
		appended := t.appendedSince(file, inode, info.Size())
		t.broadcast(evTranscripts{Event: "transcripts.changed", Files: []TranscriptFile{{
			Provider: provider, Path: file, Size: info.Size(), Inode: inode, MtimeMs: mtime, SeenAt: &seenAt, Appended: appended,
		}}})
	})
}

// appendedSince is the complete lines the file grew by past the last line end this agent saw, if it saw one.
func (t *transcriptWatch) appendedSince(file string, inode uint64, size int64) *Appended {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	t.mu.Lock()
	prev, had := t.lineEnds[file]
	t.mu.Unlock()
	if !had || prev.inode != inode || size < prev.end {
		last := make([]byte, 1)
		atLineEnd := size == 0
		if size > 0 {
			if _, err := f.ReadAt(last, size-1); err == nil && last[0] == '\n' {
				atLineEnd = true
			}
		}
		t.mu.Lock()
		if atLineEnd {
			t.lineEnds[file] = lineEnd{inode, size}
		} else {
			delete(t.lineEnds, file)
		}
		t.mu.Unlock()
		return nil
	}
	if size == prev.end || size-prev.end > pushMax {
		return nil
	}
	buf := make([]byte, size-prev.end)
	n, _ := f.ReadAt(buf, prev.end)
	nl := bytes.LastIndexByte(buf[:n], '\n')
	if nl < 0 {
		return nil
	}
	a := &Appended{From: prev.end, Next: prev.end + int64(nl) + 1, Data: base64.StdEncoding.EncodeToString(buf[:nl+1])}
	t.mu.Lock()
	t.lineEnds[file] = lineEnd{inode, a.Next}
	t.mu.Unlock()
	return a
}
