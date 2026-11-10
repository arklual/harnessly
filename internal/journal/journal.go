// Package journal is the machine's journal of what happened along the whole way of a request: the page
// (navigation, key clicks, its requests to the runtime, console errors, uncaught exceptions, files that did not load),
// the runtime (requests that failed or changed something, MCP tool calls, app lifecycle, agent launches), the apps'
// views and Sinopia (what their SDK and bridge saw). One trace id goes from the page through the runtime into a view and
// its tool calls, so one error reads with its whole chain: `harnessly logs` and the MCP tool harnessly_logs.
//
// It is a hint for the agent and the person, bounded in cost: lines are queued without waiting and written by one
// goroutine in batches, a full queue drops lines (counted), every hour has its own file with a size cap, and files older
// than the retention go. A line carries no body, no secret, no token, no text the person typed: only what happened,
// where, how long it took and how it ended.
package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Levels, most severe first.
const (
	Error = "error"
	Warn  = "warn"
	Info  = "info"
)

// Sources: where a line was seen.
const (
	SourceWeb     = "web"
	SourceRuntime = "runtime"
	SourceApp     = "app"
	SourceSinopia = "sinopia"
	SourceAgent   = "agent"
	// SourcePreview is a page in a preview slot that is not harnessly's own: a dev server, a site.
	SourcePreview = "preview"
)

// Entry is one line of the journal.
type Entry struct {
	// At is when it happened, in milliseconds.
	At     int64  `json:"at"`
	Level  string `json:"level"`
	Source string `json:"src"`
	// Event is what kind of thing happened: http, fetch, resource, exception, console, csp, nav, click, mcp.call,
	// app.session, app.enable, ui.initialize, view.file, storage, agent.launch, …
	Event  string `json:"ev"`
	Text   string `json:"text,omitempty"`
	Trace  string `json:"trace,omitempty"`
	Chat   string `json:"chat,omitempty"`
	App    string `json:"app,omitempty"`
	View   string `json:"view,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Method string `json:"method,omitempty"`
	// URL is a path (or an origin and a path), never a query: a query is where tokens travel.
	URL    string `json:"url,omitempty"`
	Status int    `json:"status,omitempty"`
	Dur    int64  `json:"dur,omitempty"`
}

// Options bound a journal; zero values take the defaults.
type Options struct {
	// Keep is how long files are kept (default three days).
	Keep time.Duration
	// HourBytes is the most an hour's file takes of info lines; warnings and errors may take twice as much.
	HourBytes int64
	// Queue is how many lines may wait for the writer.
	Queue int
	// Now is the clock (tests).
	Now func() time.Time
}

// Journal is a machine's journal in a directory of hourly files.
type Journal struct {
	dir  string
	opts Options

	ch      chan Entry
	dropped atomic.Int64
	done    chan struct{}
	closed  atomic.Bool
	flushed chan chan struct{}

	// OnEntry, when set, is told every warning and error after it is written (an optional export): from the writer's
	// goroutine, so it must not block.
	OnEntry func(Entry)

	mu sync.Mutex // the files: the writer's against a pruning read
}

// Open starts a journal writing under dir.
func Open(dir string, opts Options) *Journal {
	if opts.Keep <= 0 {
		opts.Keep = 72 * time.Hour
	}
	if opts.HourBytes <= 0 {
		opts.HourBytes = 8 << 20
	}
	if opts.Queue <= 0 {
		opts.Queue = 4096
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	j := &Journal{dir: dir, opts: opts, ch: make(chan Entry, opts.Queue), done: make(chan struct{}), flushed: make(chan chan struct{})}
	go j.run()
	return j
}

// Dir is where the files are.
func (j *Journal) Dir() string { return j.dir }

// Dropped is how many lines a full queue or a full hour lost.
func (j *Journal) Dropped() int64 { return j.dropped.Load() }

// Add queues a line; it never waits. A line with no time takes now, with no level info.
func (j *Journal) Add(e Entry) {
	if j == nil || j.closed.Load() {
		return
	}
	e = clean(e, j.opts.Now())
	select {
	case j.ch <- e:
	default:
		j.dropped.Add(1)
	}
}

// Flush waits until the lines queued before it are written.
func (j *Journal) Flush() {
	if j == nil || j.closed.Load() {
		return
	}
	ack := make(chan struct{})
	select {
	case j.flushed <- ack:
		<-ack
	case <-j.done:
	}
}

// Close writes what waits and stops the writer.
func (j *Journal) Close() {
	if j == nil || !j.closed.CompareAndSwap(false, true) {
		return
	}
	close(j.ch)
	<-j.done
}

func hourFile(t time.Time) string { return t.UTC().Format("2006010215") + ".jsonl" }

type hourState struct {
	name string
	f    *os.File
	w    *bufio.Writer
	size int64
}

func (j *Journal) run() {
	defer close(j.done)
	var cur hourState
	closeCur := func() {
		if cur.f != nil {
			_ = cur.w.Flush()
			_ = cur.f.Close()
		}
		cur = hourState{}
	}
	defer closeCur()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	lastPrune := time.Time{}
	write := func(e Entry) {
		name := hourFile(time.UnixMilli(e.At))
		if now := j.opts.Now(); name != cur.name {
			closeCur()
			j.mu.Lock()
			if err := os.MkdirAll(j.dir, 0o700); err == nil {
				if f, err := os.OpenFile(filepath.Join(j.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
					info, _ := f.Stat()
					cur = hourState{name: name, f: f, w: bufio.NewWriterSize(f, 32<<10)}
					if info != nil {
						cur.size = info.Size()
					}
				}
			}
			j.mu.Unlock()
			if now.Sub(lastPrune) > 10*time.Minute {
				lastPrune = now
				j.prune(now)
			}
		}
		if cur.f == nil {
			j.dropped.Add(1)
			return
		}
		b, err := json.Marshal(e)
		if err != nil {
			return
		}
		limit := j.opts.HourBytes
		if e.Level != Info {
			limit *= 2
		}
		if cur.size+int64(len(b))+1 > limit {
			j.dropped.Add(1)
			return
		}
		_, _ = cur.w.Write(b)
		_ = cur.w.WriteByte('\n')
		cur.size += int64(len(b)) + 1
		if e.Level != Info && j.OnEntry != nil {
			j.OnEntry(e)
		}
	}
	for {
		select {
		case e, ok := <-j.ch:
			if !ok {
				return
			}
			write(e)
			// What else waits goes in the same batch, then one flush.
			for more := true; more; {
				select {
				case e, ok := <-j.ch:
					if !ok {
						return
					}
					write(e)
				default:
					more = false
				}
			}
			if cur.w != nil {
				_ = cur.w.Flush()
			}
		case ack := <-j.flushed:
			for more := true; more; {
				select {
				case e, ok := <-j.ch:
					if !ok {
						close(ack)
						return
					}
					write(e)
				default:
					more = false
				}
			}
			if cur.w != nil {
				_ = cur.w.Flush()
			}
			close(ack)
		case <-tick.C:
			if cur.w != nil {
				_ = cur.w.Flush()
			}
		}
	}
}

// prune removes the hour files older than the retention.
func (j *Journal) prune(now time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	names, _ := filepath.Glob(filepath.Join(j.dir, "*.jsonl"))
	for _, p := range names {
		t, err := time.Parse("2006010215", strings.TrimSuffix(filepath.Base(p), ".jsonl"))
		if err != nil {
			continue
		}
		if now.Sub(t.Add(time.Hour)) > j.opts.Keep {
			_ = os.Remove(p)
		}
	}
}

// files are the hour files that may hold lines written between since and until, oldest first.
func (j *Journal) files(since, until time.Time) []string {
	names, _ := filepath.Glob(filepath.Join(j.dir, "*.jsonl"))
	var out []string
	for _, p := range names {
		t, err := time.Parse("2006010215", strings.TrimSuffix(filepath.Base(p), ".jsonl"))
		if err != nil {
			continue
		}
		if t.Add(time.Hour).Before(since) || (!until.IsZero() && t.After(until)) {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

var std atomic.Pointer[Journal]

// SetDefault makes j the journal Add writes to (nil: none).
func SetDefault(j *Journal) { std.Store(j) }

// Default is the journal Add writes to, nil when there is none.
func Default() *Journal { return std.Load() }

// Add queues a line in the default journal; with none it does nothing.
func Add(e Entry) { std.Load().Add(e) }
