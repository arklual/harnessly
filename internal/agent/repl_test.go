package agent

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type sentLog struct {
	mu     sync.Mutex
	frames []hubproto.ReplBatch
}

func (s *sentLog) send(b []byte) {
	var f hubproto.ReplBatch
	_ = json.Unmarshal(b, &f)
	s.mu.Lock()
	s.frames = append(s.frames, f)
	s.mu.Unlock()
}

func (s *sentLog) seqs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int64, 0, len(s.frames))
	for _, f := range s.frames {
		out = append(out, f.Seq)
	}
	return out
}

func newTestOut(t *testing.T, window int) (*replOut, *sentLog, chan struct{}) {
	t.Helper()
	log := &sentLog{}
	done := make(chan struct{})
	o := newReplOut(log.send, done, hubproto.ReplStart{Window: window, Kinds: []string{"k"}})
	t.Cleanup(o.close)
	return o, log, done
}

func replWaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Batches are numbered per kind and carry the kind's payload; a full window holds the next batch on the machine.
func TestReplWindowHoldsBatchesOnTheMachine(t *testing.T) {
	o, log, _ := newTestOut(t, 2)
	if !o.has("k") || o.has("other") || (*replOut)(nil).has("k") {
		t.Fatal("kinds")
	}
	for i := range 2 {
		if !o.push("k", json.RawMessage(`{"n":1}`), 0) {
			t.Fatalf("batch %d not sent", i+1)
		}
	}
	if o.push("k", json.RawMessage(`{}`), 0) {
		t.Fatal("a batch past the window was sent")
	}
	got := log.seqs()
	if len(got) != 2 || got[0] != 1 || got[1] != 2 || log.frames[0].Kind != "k" || string(log.frames[0].Data) != `{"n":1}` {
		t.Fatalf("sent %+v", log.frames)
	}
	// A sender that may wait goes once the hub acknowledges.
	sent := make(chan bool, 1)
	go func() { sent <- o.push("k", json.RawMessage(`{}`), time.Minute) }()
	select {
	case <-sent:
		t.Fatal("sent before there was room")
	case <-time.After(30 * time.Millisecond):
	}
	o.ack("k", 1, false)
	if !<-sent {
		t.Fatal("the waiting batch was not sent")
	}
	if got := log.seqs(); got[len(got)-1] != 3 {
		t.Fatalf("sent %v", got)
	}
}

// A waiting sender gives up when the link ends.
func TestReplWaitEndsWithTheLink(t *testing.T) {
	o, _, done := newTestOut(t, 1)
	o.push("k", json.RawMessage(`{}`), 0)
	res := make(chan bool, 1)
	go func() { res <- o.push("k", json.RawMessage(`{}`), time.Minute) }()
	time.Sleep(20 * time.Millisecond)
	close(done)
	select {
	case ok := <-res:
		if ok {
			t.Fatal("sent on a link that ended")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the sender still waits")
	}
}

// The hub's retry sends the unacknowledged batches again from the oldest; a batch nobody acknowledges goes again too.
func TestReplSendsAgainOnRetryAndOnSilence(t *testing.T) {
	old := replAckWait
	replAckWait = 40 * time.Millisecond
	t.Cleanup(func() { replAckWait = old })
	o, log, _ := newTestOut(t, 8)
	for range 3 {
		o.push("k", json.RawMessage(`{}`), 0)
	}
	o.ack("k", 1, true) // 1 stored, 2 not: 2 and 3 go again after the first pause (1 s)
	replWaitFor(t, "no resend after a retry", func() bool { return len(log.seqs()) >= 5 })
	got := log.seqs()
	if got[3] != 2 || got[4] != 3 {
		t.Fatalf("sent %v, want 2 and 3 again", got)
	}
	o.ack("k", 3, false)
	o.push("k", json.RawMessage(`{}`), 0) // 4, never acknowledged
	replWaitFor(t, "no resend on silence", func() bool { return len(log.seqs()) >= 7 })
	if got := log.seqs(); got[6] != 4 {
		t.Fatalf("sent %v, want 4 again", got)
	}
	o.ack("k", 4, false)
	n := len(log.seqs())
	time.Sleep(200 * time.Millisecond)
	if len(log.seqs()) != n {
		t.Fatalf("sent %v after everything was acknowledged", log.seqs())
	}
}

// An old ack and a retry that comes after a later ack change nothing.
func TestReplOldAcksAreIgnored(t *testing.T) {
	o, log, _ := newTestOut(t, 8)
	for range 3 {
		o.push("k", json.RawMessage(`{}`), 0)
	}
	o.ack("k", 2, false)
	o.ack("k", 1, true)
	o.mu.Lock()
	st := o.streams["k"]
	acked, left, failures := st.acked, len(st.unacked), st.failures
	o.mu.Unlock()
	if acked != 2 || left != 1 || failures != 0 {
		t.Fatalf("acked %d, unacked %d, failures %d", acked, left, failures)
	}
	if len(log.seqs()) != 3 {
		t.Fatalf("sent %v", log.seqs())
	}
}

// repl.start on a link makes the hub's kinds go as batches; a link the hub started nothing on keeps the old events.
func TestLinkStartsBatches(t *testing.T) {
	l := &link{control: make(chan []byte, 4), done: make(chan struct{})}
	if l.repl.Load().has(hubproto.ReplHistoryEvents) {
		t.Fatal("batches before repl.start")
	}
	l.startRepl(hubproto.ReplStart{Kinds: []string{hubproto.ReplHistoryEvents}})
	o := l.repl.Load()
	if !o.has(hubproto.ReplHistoryEvents) || o.has(hubproto.ReplHistoryBackfill) || o.window != hubproto.ReplWindow {
		t.Fatalf("started %+v", o)
	}
	if !o.push(hubproto.ReplHistoryEvents, json.RawMessage(`{"ch":"convs"}`), 0) {
		t.Fatal("not sent")
	}
	var f struct {
		Event string `json:"event"`
		hubproto.ReplBatch
	}
	if err := json.Unmarshal(<-l.control, &f); err != nil || f.Event != hubproto.EventReplBatch || f.Seq != 1 {
		t.Fatalf("frame %+v %v", f, err)
	}
	l.startRepl(hubproto.ReplStart{Kinds: []string{hubproto.ReplHistoryEvents}})
	if !o.closed {
		t.Fatal("the batches of the first start were not ended")
	}
}

// Events past the window wait on the machine and go, in order, as the hub acknowledges; past replPending the oldest
// is dropped.
func TestReplQueueHoldsEventsUntilTheHubTakesThem(t *testing.T) {
	o, log, _ := newTestOut(t, 2)
	for i := range 5 {
		o.queue("k", json.RawMessage(`{"n":`+string(rune('0'+i))+`}`))
	}
	if got := log.seqs(); len(got) != 2 {
		t.Fatalf("sent %v, want the window of 2", got)
	}
	o.ack("k", 2, false)
	if got := log.seqs(); len(got) != 4 || got[2] != 3 || got[3] != 4 || string(log.frames[2].Data) != `{"n":2}` {
		t.Fatalf("sent %+v", log.frames)
	}
	o.ack("k", 4, false)
	if got := log.seqs(); len(got) != 5 || string(log.frames[4].Data) != `{"n":4}` {
		t.Fatalf("sent %+v", log.frames)
	}
	o.mu.Lock()
	st := o.streams["k"]
	st.pending = make([]json.RawMessage, replPending)
	st.unacked = append(st.unacked, make([]replSent, 2)...)
	o.mu.Unlock()
	o.queue("k", json.RawMessage(`{"last":true}`))
	o.mu.Lock()
	n, last := len(st.pending), string(st.pending[len(st.pending)-1])
	o.mu.Unlock()
	if n != replPending || last != `{"last":true}` {
		t.Fatalf("pending %d, last %s", n, last)
	}
}

// An event goes as its kind's batch on a link the hub started batches for it on, as it is everywhere else.
func TestSendEventGoesAsABatchOnlyWhenTheHubNamedIt(t *testing.T) {
	l := &link{control: make(chan []byte, 4), done: make(chan struct{})}
	l.sendEvent(hubproto.EventEnvState, []byte(`{"event":"env.state"}`))
	if got := string(<-l.control); got != `{"event":"env.state"}` {
		t.Fatalf("old frame %s", got)
	}
	l.startRepl(hubproto.ReplStart{Kinds: []string{hubproto.EventEnvState}})
	l.sendEvent(hubproto.EventEnvState, []byte(`{"event":"env.state"}`))
	var f struct {
		Event string `json:"event"`
		hubproto.ReplBatch
	}
	if err := json.Unmarshal(<-l.control, &f); err != nil || f.Event != hubproto.EventReplBatch || f.Kind != hubproto.EventEnvState || string(f.Data) != `{"event":"env.state"}` {
		t.Fatalf("batch %+v %v", f, err)
	}
	l.sendEvent(hubproto.EventJob, []byte(`{"event":"job"}`))
	if got := string(<-l.control); got != `{"event":"job"}` {
		t.Fatalf("an event the hub did not name went as %s", got)
	}
	if docEventName(evDocReply{Event: "docs.reply"}) != hubproto.EventDocReply || docEventName(struct{}{}) != "" {
		t.Fatal("doc event names")
	}
}
