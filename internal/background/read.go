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

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

// tailBytes is how much of a transcript a cold read takes: one line can be megabytes long, the start is history.
const tailBytes = 6 << 20

type cursor struct {
	progress
	reading sync.Mutex
	// sub is the cursor's place in the transcript index, nil without one; fresh says the index handed over lines the
	// cursor's lists do not count yet. Both under reading.
	sub   *transcriptindex.Sub
	fresh bool
}

// progress is how far a cursor has read its transcript and what it found. A transcript that is gone starts it over;
// the lock of the read under way stays held, so it is never part of what is reset.
type progress struct {
	ino    uint64
	offset int64 // always right after a newline, so a line being written is read whole next time
	mtime  time.Time
	size   int64
	scan   *scan
	tasks  []Task
	ended  []Ended
	team   []Teammate
}

// Reader keeps one cursor per transcript: read once from its tail, then only what was appended since, so a busy
// agent's every write costs the new lines, not the whole tail again.
type Reader struct {
	// TempDir is where the agent keeps task output (<TempDir>/claude-<uid>/<project>/<session>/tasks/<id>.output).
	TempDir string
	UID     int
	Now     func() float64 // epoch milliseconds
	// Index, when set, reads what a transcript gains for the reader and for the transcript's other readers at once:
	// the reader reads a transcript's tail itself and takes the lines after it from the index. nil has it read every
	// transcript itself.
	Index *transcriptindex.Index

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
	if r.Index != nil {
		return r.indexed(path, c)
	}
	c.reading.Lock()
	defer c.reading.Unlock()
	return r.advance(path, c)
}

// indexed is ReadAll with the index: the lines the transcript gained come from the index's one read of them, folded
// into the cursor's scan as they are read (take). Where the index does not serve the cursor — it is new, the file
// was replaced, cut or is gone — the cursor reads the file itself, as without an index, and takes its place again.
func (r *Reader) indexed(path string, c *cursor) ([]Task, []Ended) {
	c.reading.Lock()
	if c.sub == nil {
		c.sub = r.Index.Subscribe(path, "background", transcriptindex.SinkFunc(func(b *transcriptindex.Batch) bool { return r.take(c, b) }))
	}
	sub := c.sub
	c.reading.Unlock()
	// The look hands the lines to take, which locks the cursor: the cursor is not held over it.
	st, _ := sub.Sync(false)
	c.reading.Lock()
	if st != transcriptindex.Fresh {
		tasks, ended := r.advance(path, c)
		c.fresh = false
		read, offset, ino := c.scan != nil, c.offset, c.ino
		if !read && c.sub == sub {
			c.sub = nil // the transcript is gone and the cursor with it
		}
		c.reading.Unlock()
		// The index is told with the cursor let go: its looks hold the file and then take the cursor (take).
		if read {
			sub.Seat(offset, ino)
		} else {
			sub.Close()
		}
		return tasks, ended
	}
	defer c.reading.Unlock()
	now := r.now()
	if !c.fresh {
		out := []Task{}
		for _, t := range c.tasks {
			if pending(t, now) {
				out = append(out, t)
			}
		}
		return out, c.ended
	}
	c.fresh = false
	c.tasks = r.aliveOnly(c.scan.tasks(now), path)
	c.ended = c.scan.endedAgents()
	c.team = c.scan.team.list()
	return c.tasks, c.ended
}

// take folds the lines the index read into the cursor's scan.
func (r *Reader) take(c *cursor, b *transcriptindex.Batch) bool {
	c.reading.Lock()
	defer c.reading.Unlock()
	if c.scan == nil || b.From != c.offset {
		return false // not where the cursor stands: it reads for itself
	}
	now := r.now()
	b.Each(func(l *transcriptindex.Line) {
		var decoded *lineEntry
		if f := l.Fields(); f != nil {
			decoded = &lineEntry{Type: f.Type, IsSidechain: f.IsSidechain, Timestamp: f.Timestamp}
			decoded.Message.Content = f.Message.Content
		}
		c.scan.line(l.Text(), decoded, now)
	})
	// The file's time and size are not the cursor's any more: a read of its own starts with the file as it is.
	c.offset, c.mtime, c.size, c.fresh = b.To, time.Time{}, 0, true
	return true
}

// Teammates are the named teammates the transcript started, the latest first, in the state its lead last saw them.
func (r *Reader) Teammates(path string) []Teammate {
	c := r.cursorOf(path)
	if r.Index != nil {
		r.indexed(path, c)
		c.reading.Lock()
		defer c.reading.Unlock()
		return c.team
	}
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
	c := r.cursors[path]
	delete(r.cursors, path)
	r.mu.Unlock()
	if c == nil {
		return
	}
	c.reading.Lock()
	sub := c.sub
	c.sub = nil
	c.reading.Unlock()
	if sub != nil {
		sub.Close()
	}
}

func (r *Reader) advance(path string, c *cursor) ([]Task, []Ended) {
	now := r.now()
	st, err := os.Stat(path)
	tally.Stat()
	if err != nil {
		r.mu.Lock()
		delete(r.cursors, path)
		r.mu.Unlock()
		c.progress = progress{}
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
	// One line at a time: the chunk is megabytes, and a copy of it as text, another made valid and a slice of its
	// lines were three more of them held at once for every transcript read cold.
	one := make([]string, 1)
	for rest := chunk[start:max(start, complete)]; len(rest) > 0; {
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i], rest[i+1:]
		} else {
			rest = nil
		}
		if len(line) == 0 {
			continue
		}
		one[0] = strings.ToValidUTF8(string(line), "�")
		c.scan.lines(one, now)
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
	tally.Open()
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, to-from)
	n, _ := f.ReadAt(buf, from)
	tally.Read(n)
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
