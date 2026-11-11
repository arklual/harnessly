// Package hookspool is the file the agents' lifecycle hooks leave their events in before they call the runtime: an
// event told while the runtime was away, restarting or too busy to answer is read from the file once it is back, so the
// end of a turn is not lost with the request that carried it. The hook appends a line and delivers; the runtime
// applies the lines in the order they were written, each once, and empties the file.
package hookspool

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// File is the spool's name in the runtime's data folder.
const File = "hook-spool.jsonl"

const (
	// maxBytes: a spool nobody reads (the runtime is gone for good) stops growing.
	maxBytes = 4 << 20
	// lockWait is how long a hook waits for the file: the runtime holds it for one read.
	lockWait = 200 * time.Millisecond
	// seenMax is how many applied events are remembered to tell a second delivery by.
	seenMax = 4096
)

// ErrFull is a spool at its limit; ErrBusy, one whose lock did not come in time.
var (
	ErrFull = errors.New("hookspool: the spool is full")
	ErrBusy = errors.New("hookspool: the spool is locked")
)

// NewID names an event.
func NewID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// Append adds one event (a JSON object, with its "id") as a line of the spool.
func Append(path string, event []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := lock(f, lockWait); err != nil {
		return err
	}
	defer unlock(f)
	if st, err := f.Stat(); err == nil && st.Size()+int64(len(event)) > maxBytes {
		return ErrFull
	}
	line := append(bytes.TrimRight(event, "\n"), '\n')
	_, err = f.Write(line)
	return err
}

// take reads the spool and empties it.
func take(path string) ([]byte, error) {
	if st, err := os.Stat(path); err != nil || st.Size() == 0 {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if err := lock(f, lockWait); err != nil {
		return nil, err
	}
	defer unlock(f)
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		return nil, err
	}
	return raw, nil
}

// Queue is the runtime's end of the spool.
type Queue struct {
	// Path is the spool file.
	Path string
	// Apply takes one event; it is called for the events in the order they were written, never twice for an id.
	Apply func(event map[string]any)
	// Ready says whether events can be applied now (nil: always). Until then they stay in the file.
	Ready func() bool

	mu   sync.Mutex
	seen map[string]struct{}
	ring []string
	next int
}

func (q *Queue) ready() bool { return q.Ready == nil || q.Ready() }

func (q *Queue) known(id string) bool {
	_, ok := q.seen[id]
	return ok
}

func (q *Queue) mark(id string) {
	if q.seen == nil {
		q.seen = map[string]struct{}{}
		q.ring = make([]string, seenMax)
	}
	if old := q.ring[q.next]; old != "" {
		delete(q.seen, old)
	}
	q.ring[q.next] = id
	q.next = (q.next + 1) % len(q.ring)
	q.seen[id] = struct{}{}
}

// Drain applies what the spool holds and empties it; it returns how many events it applied.
func (q *Queue) Drain() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.ready() {
		return 0
	}
	return q.drain()
}

func (q *Queue) drain() int {
	raw, err := take(q.Path)
	if err != nil || len(raw) == 0 {
		return 0
	}
	applied := 0
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var event map[string]any
		if len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &event) != nil {
			continue // a line cut short by a hook that was killed
		}
		id, _ := event["id"].(string)
		if id == "" || q.known(id) {
			continue
		}
		q.mark(id)
		q.Apply(event)
		applied++
	}
	return applied
}

// Deliver is an event the hook brought itself. What the spool holds goes first, so an event the runtime missed is
// applied before the one that came after it; the event is then applied unless the spool had it. False: events cannot
// be applied yet, and the event waits in the spool.
func (q *Queue) Deliver(id string, event map[string]any) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.ready() {
		return false
	}
	q.drain()
	if !q.known(id) {
		q.mark(id)
		q.Apply(event)
	}
	return true
}
