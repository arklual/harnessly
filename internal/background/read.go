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
	ended   []Ended
	team    []Teammate
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
	running, _ := r.ReadAll(path)
	return running
}

// ReadAll is Read and the agents that ended, the latest first: a person who comes back to a chat sees what its
// subagents did, not only the ones still running.
func (r *Reader) ReadAll(path string) ([]Task, []Ended) {
	c := r.cursorOf(path)
	c.reading.Lock()
	defer c.reading.Unlock()
	return r.advance(path, c)
}

// Teammates are the named teammates the transcript started, the latest first, in the state its lead last saw them.
func (r *Reader) Teammates(path string) []Teammate {
	c := r.cursorOf(path)
	c.reading.Lock()
	defer c.reading.Unlock()
	r.advance(path, c)
	return c.team
}

func (r *Reader) cursorOf(path string) *cursor {
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
	return c
}

// Forget drops the cursor of a transcript no tab shows any more.
func (r *Reader) Forget(path string) {
	r.mu.Lock()
	delete(r.cursors, path)
	r.mu.Unlock()
}

func (r *Reader) advance(path string, c *cursor) ([]Task, []Ended) {
	now := r.now()
	st, err := os.Stat(path)
	if err != nil {
		r.Forget(path)
		*c = cursor{}
		return []Task{}, nil
	}
	if c.scan != nil && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		out := []Task{}
		for _, t := range c.tasks {
			if pending(t, now) {
				out = append(out, t)
			}
		}
		return out, c.ended
	}
	// A rewritten or replaced transcript, or more new text than a cold read takes, starts over from the tail.
	fromTail := false
	if c.scan == nil || c.ino != inode(st) || st.Size() < c.offset || st.Size()-c.offset > tailBytes {
		c.ino, c.offset, c.scan, c.tasks, c.ended, c.team = inode(st), max(0, st.Size()-tailBytes), newScan(), nil, nil, nil
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
	c.ended = c.scan.endedAgents()
	c.team = c.scan.team.list()
	return c.tasks, c.ended
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

// OutputFile is where the agent writes a shell's or monitor's output: the path its result named, else the folder the
// agent keeps task output in.
func (r *Reader) OutputFile(transcript string, t Task) string {
	if t.Output != nil {
		return *t.Output
	}
	tmp := r.TempDir
	if tmp == "" {
		tmp = os.TempDir()
	}
	return filepath.Join(tmp, "claude-"+strconv.Itoa(r.UID), filepath.Base(filepath.Dir(transcript)),
		strings.TrimSuffix(filepath.Base(transcript), ".jsonl"), "tasks", t.ID+".output")
}

// aliveOnly drops shells and monitors whose output file the agent removed: their end never reached the transcript.
func (r *Reader) aliveOnly(tasks []Task, transcript string) []Task {
	out := []Task{}
	for _, t := range tasks {
		if t.Kind == Wakeup || t.Kind == Agent || t.Kind == Workflow {
			out = append(out, t)
			continue
		}
		if _, err := os.Stat(r.OutputFile(transcript, t)); err == nil {
			out = append(out, t)
		}
	}
	return out
}
