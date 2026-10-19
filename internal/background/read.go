package background

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// tailBytes is how much of a transcript a cold read takes: one line can be megabytes long, the start is history.
const tailBytes = 6 << 20

type cursor struct {
	ino     uint64
	offset  int64 // always right after a newline, so a line being written is read whole next time
	mtime   time.Time
	size    int64
	scan    *scan
	tasks   []Task
	reading sync.Mutex
}

// Reader keeps one cursor per transcript: read once from its tail, then only what was appended since, so a busy
// agent's every write costs the new lines, not the whole tail again.
type Reader struct {
	// TempDir is where the agent keeps task output (<TempDir>/claude-<uid>/<project>/<session>/tasks/<id>.output).
	TempDir string
	UID     int
	Now     func() float64 // epoch milliseconds

	mu      sync.Mutex
	cursors map[string]*cursor
}

func (r *Reader) now() float64 {
	if r.Now != nil {
		return r.Now()
	}
	return float64(time.Now().UnixMilli())
}

func inode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

// Read is the background work of a transcript still running. Reads of one transcript take turns: two at once would
// both consume the same appended lines.
func (r *Reader) Read(path string) []Task {
	r.mu.Lock()
	if r.cursors == nil {
		r.cursors = map[string]*cursor{}
	}
	c := r.cursors[path]
	if c == nil {
		c = &cursor{}
		r.cursors[path] = c
	}
	r.mu.Unlock()
	c.reading.Lock()
	defer c.reading.Unlock()
	return r.advance(path, c)
}

// Forget drops the cursor of a transcript no tab shows any more.
func (r *Reader) Forget(path string) {
	r.mu.Lock()
	delete(r.cursors, path)
	r.mu.Unlock()
}

func (r *Reader) advance(path string, c *cursor) []Task {
	now := r.now()
	st, err := os.Stat(path)
	if err != nil {
		r.Forget(path)
		*c = cursor{}
		return []Task{}
	}
	if c.scan != nil && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		out := []Task{}
		for _, t := range c.tasks {
			if pending(t, now) {
				out = append(out, t)
			}
		}
		return out
	}
	// A rewritten or replaced transcript, or more new text than a cold read takes, starts over from the tail.
	fromTail := false
	if c.scan == nil || c.ino != inode(st) || st.Size() < c.offset || st.Size()-c.offset > tailBytes {
		c.ino, c.offset, c.scan, c.tasks = inode(st), max(0, st.Size()-tailBytes), newScan(), nil
		c.mtime, c.size = time.Time{}, 0
		fromTail = c.offset > 0
	}
	chunk := readChunk(path, c.offset, st.Size())
	start := 0
	if fromTail {
		start = bytes.IndexByte(chunk, '\n') + 1 // the tail starts inside a line
	}
	complete := bytes.LastIndexByte(chunk, '\n') + 1
	if complete > start {
		var lines []string
		for _, l := range strings.Split(strings.ToValidUTF8(string(chunk[start:complete]), "�"), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
		c.scan.lines(lines, now)
	}
	c.offset += int64(complete)
	c.mtime, c.size = st.ModTime(), st.Size()
	c.tasks = r.aliveOnly(c.scan.tasks(now), path)
	return c.tasks
}

func readChunk(path string, from, to int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, to-from)
	n, _ := f.ReadAt(buf, from)
	return buf[:n]
}

// aliveOnly drops shells and monitors whose output file the agent removed: their end never reached the transcript.
func (r *Reader) aliveOnly(tasks []Task, transcript string) []Task {
	tmp := r.TempDir
	if tmp == "" {
		tmp = os.TempDir()
	}
	dir := filepath.Join(tmp, "claude-"+strconv.Itoa(r.UID), filepath.Base(filepath.Dir(transcript)),
		strings.TrimSuffix(filepath.Base(transcript), ".jsonl"), "tasks")
	out := []Task{}
	for _, t := range tasks {
		if t.Kind == Wakeup || t.Kind == Agent {
			out = append(out, t)
			continue
		}
		file := filepath.Join(dir, t.ID+".output")
		if t.Output != nil {
			file = *t.Output
		}
		if _, err := os.Stat(file); err == nil {
			out = append(out, t)
		}
	}
	return out
}
