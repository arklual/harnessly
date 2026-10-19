package design

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"harnessly/internal/nodejson"
)

// Which files of a design changed, for the page script to apply them without a reload: a stream of the paths as they
// change, and a list for a page that polls.

const (
	changesMaxPaths   = 50
	changesMaxEntries = 6000
	changesSettle     = 120 * time.Millisecond
)

var changesSkipped = map[string]bool{"node_modules": true, ".git": true}

// changedSince are the project files modified after since (ms), relative with forward slashes; at most fifty.
func (l *Library) changedSince(ref string, since float64) []string {
	dir := l.ProjectDirOf(ref)
	found := []string{}
	if dir == "" {
		return found
	}
	left := changesMaxEntries
	var walk func(cur string) bool
	walk = func(cur string) bool {
		entries, _ := os.ReadDir(cur)
		for _, e := range entries {
			left--
			if left < 0 || len(found) >= changesMaxPaths {
				return false
			}
			full := filepath.Join(cur, e.Name())
			if e.IsDir() {
				if !changesSkipped[e.Name()] && !walk(full) {
					return false
				}
			} else if e.Type().IsRegular() {
				if st, err := os.Stat(full); err == nil && mtimeMs(st) > since {
					rel, _ := filepath.Rel(dir, full)
					found = append(found, filepath.ToSlash(rel))
				}
			}
		}
		return true
	}
	walk(dir)
	return found
}

// watchDesign calls fn with the paths of a design that changed, a moment after an editor stops writing; nil when the
// library cannot be watched.
func (l *Library) watchDesign(ref string, fn func(paths []string)) func() {
	if l.ProjectDirOf(ref) == "" {
		return nil
	}
	prefix := ref + "/" + ProjectDirname + "/"
	var mu sync.Mutex
	var pending []string
	seen := map[string]bool{}
	var timer *time.Timer
	stop, ok := l.Subscribe(func(rel string) {
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, prefix) {
			return
		}
		file := rel[len(prefix):]
		for _, part := range strings.Split(file, "/") {
			if changesSkipped[part] {
				return
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if !seen[file] {
			seen[file] = true
			pending = append(pending, file)
		}
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(changesSettle, func() {
			mu.Lock()
			paths := pending
			if len(paths) > changesMaxPaths {
				paths = paths[:changesMaxPaths]
			}
			pending, seen, timer = nil, map[string]bool{}, nil
			mu.Unlock()
			fn(paths)
		})
	})
	if !ok {
		stop()
		return nil
	}
	return func() {
		stop()
		mu.Lock()
		if timer != nil {
			timer.Stop()
		}
		mu.Unlock()
	}
}

// serveEvents is the stream of a design's changes and of the commands an agent sends to its open pages; a comment
// every twenty seconds keeps the preview proxy from calling it idle.
func (l *Library) serveEvents(w http.ResponseWriter, r *http.Request, id string, head bool) {
	if !IsLibraryID(id) || l.ReadArtifact(id) == nil {
		send(w, 404, "application/json", notFoundJSON, head)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	if head {
		return
	}
	fl, _ := w.(http.Flusher)
	out := make(chan []byte, 64)
	push := func(b []byte) {
		select {
		case out <- b:
		default:
		}
	}
	stopWatch := l.watchDesign(id, func(paths []string) {
		push([]byte("data: " + string(nodejson.Marshal(struct {
			Paths   []string `json:"paths"`
			Version int64    `json:"version"`
		}{paths, nowMs()})) + "\n\n"))
	})
	stopControl := l.OnControl(id, func(c Control) { push([]byte("event: control\ndata: " + string(controlJSON(c)) + "\n\n")) })
	defer func() {
		if stopWatch != nil {
			stopWatch()
		}
		stopControl()
	}()
	first := "event: unwatchable\ndata: {}\n\n"
	if stopWatch != nil {
		first = ": ok\n\n"
	}
	write := func(b []byte) bool {
		if _, err := w.Write(b); err != nil {
			return false
		}
		if fl != nil {
			fl.Flush()
		}
		return true
	}
	if !write([]byte(first)) {
		return
	}
	beat := time.NewTicker(heartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-out:
			if !write(b) {
				return
			}
		case <-beat.C:
			if !write([]byte(": beat\n\n")) {
				return
			}
		}
	}
}
