package agent

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The agent's end of the one way a machine's data reaches the hub (hubproto.FeatureRepl): after the hub's repl.start a
// kind it named goes as repl.batch, numbered per kind on the link. At most `window` batches of a kind wait for the
// hub's repl.ack; a sender of a kind whose window is full waits for room (the flow is held here, not dropped on the
// hub) or, when it may not wait, does not send and leaves the batch to the kind's own resend. What the hub does not
// acknowledge in time, or asks for again, is sent again from the oldest unacknowledged batch, with a growing pause.

// replAckWait is how long a batch may wait for its ack before the unacknowledged ones go again; each try in a row that
// is not acknowledged adds the link's retry pause (hubproto.ReplRetryWait) to it.
var replAckWait = 10 * time.Second

// replPending is how many events of a kind wait on the machine for room in the window; past it the oldest is dropped
// (a report is replaced by the next one anyway).
const replPending = 256

// replBackfillWait is how long a batch of what the runtime holds (a page of a chat, the chat list) waits for room in
// its window before it is not sent: the backfill is read again from the hub's cursors on the next history.start.
var replBackfillWait = time.Minute

type replSent struct {
	seq   int64
	frame []byte
}

type replStream struct {
	next     int64 // the seq of the next batch
	acked    int64
	unacked  []replSent
	failures int           // tries in a row the hub did not take
	room     chan struct{} // closed when the window frees, then replaced
	timer    *time.Timer
	armed    int  // counts the timers armed: a timer that fires after another was armed does nothing
	held     bool // a batch was not sent for want of room since the last ack: said once in the log
	pending  []json.RawMessage
}

type replOut struct {
	send   func([]byte)
	done   <-chan struct{}
	window int
	kinds  map[string]bool

	mu      sync.Mutex
	closed  bool
	streams map[string]*replStream
}

func newReplOut(send func([]byte), done <-chan struct{}, start hubproto.ReplStart) *replOut {
	w := start.Window
	if w <= 0 {
		w = hubproto.ReplWindow
	}
	kinds := make(map[string]bool, len(start.Kinds))
	for _, k := range start.Kinds {
		kinds[k] = true
	}
	return &replOut{send: send, done: done, window: w, kinds: kinds, streams: map[string]*replStream{}}
}

// has says the hub takes the kind as batches on this link.
func (o *replOut) has(kind string) bool { return o != nil && o.kinds[kind] }

func (o *replOut) stream(kind string) *replStream {
	st := o.streams[kind]
	if st == nil {
		st = &replStream{next: 1, room: make(chan struct{})}
		o.streams[kind] = st
	}
	return st
}

// push sends data as the kind's next batch. While the window is full it waits for room up to `wait` (0: not at all);
// false when the batch was not sent.
func (o *replOut) push(kind string, data json.RawMessage, wait time.Duration) bool {
	var deadline <-chan time.Time
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		deadline = t.C
	}
	o.mu.Lock()
	for {
		if o.closed {
			o.mu.Unlock()
			return false
		}
		st := o.stream(kind)
		if len(st.unacked) < o.window {
			break
		}
		if wait <= 0 {
			if !st.held {
				st.held = true
				Log("info", "hub is behind, a batch is left to its kind's own resend", F{"kind", kind}, F{"window", strconv.Itoa(o.window)})
			}
			o.mu.Unlock()
			return false
		}
		room := st.room
		o.mu.Unlock()
		select {
		case <-room:
		case <-o.done:
			return false
		case <-deadline:
			Log("warn", "hub took no batch for a while, one is not sent", F{"kind", kind})
			return false
		}
		o.mu.Lock()
	}
	ok := o.sendLocked(kind, o.stream(kind), data)
	o.mu.Unlock()
	return ok
}

// queue sends data as the kind's next batch, or keeps it on the machine until the window has room. It never waits.
func (o *replOut) queue(kind string, data json.RawMessage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	st := o.stream(kind)
	if len(st.unacked) < o.window && len(st.pending) == 0 {
		o.sendLocked(kind, st, data)
		return
	}
	if len(st.pending) >= replPending {
		st.pending = st.pending[1:]
		if !st.held {
			st.held = true
			Log("warn", "hub is far behind, the oldest waiting event is dropped", F{"kind", kind})
		}
	}
	st.pending = append(st.pending, data)
}

// sendLocked numbers data as the kind's next batch and sends it; o.mu is held and the window has room.
func (o *replOut) sendLocked(kind string, st *replStream, data json.RawMessage) bool {
	frame, err := marshal(struct {
		Event string          `json:"event"`
		Kind  string          `json:"kind"`
		Seq   int64           `json:"seq"`
		Data  json.RawMessage `json:"data"`
	}{hubproto.EventReplBatch, kind, st.next, data})
	if err != nil {
		Log("error", "batch not encoded", F{"kind", kind}, F{"error", err.Error()})
		return false
	}
	st.unacked = append(st.unacked, replSent{seq: st.next, frame: frame})
	st.next++
	if st.timer == nil {
		o.armLocked(kind, st, replAckWait)
	}
	// Sent under the lock: two senders of one kind must not swap their batches on the wire.
	o.send(frame)
	return true
}

// ack is the hub's repl.ack: the batches up to seq are stored; retry asks for the rest again after a pause.
func (o *replOut) ack(kind string, seq int64, retry bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	st := o.stream(kind)
	if seq > st.acked {
		st.acked = seq
		i := 0
		for i < len(st.unacked) && st.unacked[i].seq <= seq {
			i++
		}
		st.unacked = st.unacked[i:]
		st.failures, st.held = 0, false
		close(st.room)
		st.room = make(chan struct{})
		o.stopLocked(st)
		for len(st.pending) > 0 && len(st.unacked) < o.window {
			data := st.pending[0]
			st.pending = st.pending[1:]
			o.sendLocked(kind, st, data)
			o.stopLocked(st) // armed again below for all that wait
		}
		if len(st.unacked) > 0 {
			o.armLocked(kind, st, replAckWait)
		}
	}
	if retry && seq == st.acked && len(st.unacked) > 0 {
		st.failures++
		o.stopLocked(st)
		o.armLocked(kind, st, hubproto.ReplRetryWait(st.failures))
	}
}

func (o *replOut) armLocked(kind string, st *replStream, d time.Duration) {
	st.armed++
	n := st.armed
	st.timer = time.AfterFunc(d, func() { o.resend(kind, n) })
}

func (o *replOut) stopLocked(st *replStream) {
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
}

// resend sends the kind's unacknowledged batches again, oldest first, and waits longer for their ack each time.
func (o *replOut) resend(kind string, armed int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st := o.streams[kind]
	if o.closed || st == nil || st.timer == nil || st.armed != armed {
		return
	}
	st.timer = nil
	if len(st.unacked) == 0 {
		return
	}
	select {
	case <-o.done:
		return
	default:
	}
	st.failures++
	if st.failures == 1 || st.failures%10 == 0 {
		Log("info", "hub did not acknowledge, sending the batches again", F{"kind", kind},
			F{"from", strconv.FormatInt(st.unacked[0].seq, 10)}, F{"batches", strconv.Itoa(len(st.unacked))})
	}
	for _, b := range st.unacked {
		o.send(b.frame)
	}
	o.armLocked(kind, st, replAckWait+hubproto.ReplRetryWait(st.failures))
}

// close ends the batches of a link that is gone or was started again: waiting senders give up.
func (o *replOut) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	for _, st := range o.streams {
		o.stopLocked(st)
		close(st.room)
		st.room = make(chan struct{})
	}
}
