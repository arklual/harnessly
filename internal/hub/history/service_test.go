package history

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/repl"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// ackCaller keeps the acks the service sends to a machine.
type ackCaller struct {
	mu   sync.Mutex
	acks []hubproto.HistoryAck
}

func (c *ackCaller) Do(_ context.Context, _, op string, params any, _ time.Duration) (json.RawMessage, error) {
	if ack, ok := params.(hubproto.HistoryAck); ok && op == hubproto.OpHistoryAck {
		c.mu.Lock()
		c.acks = append(c.acks, ack)
		c.mu.Unlock()
	}
	return nil, nil
}

func (c *ackCaller) last(t *testing.T) hubproto.HistoryAck {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.acks) == 0 {
		t.Fatal("no ack was sent")
	}
	return c.acks[len(c.acks)-1]
}

func TestAFrameThatWasNotStoredIsAskedForAgainAndStoredOnce(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	reg := &ackCaller{}
	s := New(Deps{DB: db, Reg: reg, Now: func() time.Time { return now }})
	m := store.Machine{ID: mine.MachineID, CreatedBy: mine.UserID}
	first := j(live("convs", 1, upsert("tab-a", 1, 10)))
	final := j(live("convs", 2, upsert("tab-b", 1, 20)))

	s.Live(ctx, m, first)
	if ack := reg.last(t); ack.AckSeq != 1 || ack.Gap {
		t.Fatalf("the first frame: %+v", ack)
	}
	// The write of the next frame fails: the hub says where the channel stands and that the frame is to come again.
	fail := true
	s.apply = func(ctx context.Context, db *store.DB, o Owner, f hubproto.HistoryEvents, at time.Time) (FrameResult, error) {
		if fail {
			return FrameResult{}, errors.New("database is locked")
		}
		return ApplyFrame(ctx, db, o, f, at)
	}
	s.Live(ctx, m, final)
	if ack := reg.last(t); !ack.Gap || ack.AckSeq != 1 || ack.Epoch != "ep-1" || ack.Ch != "convs" {
		t.Fatalf("a frame not stored: %+v", ack)
	}
	if s.Lag(m.ID) != 1 {
		t.Fatalf("lag %d, want 1", s.Lag(m.ID))
	}
	if chats, err := List(ctx, db, mine.UserID, []string{m.ID}); err != nil || len(chats) != 1 {
		t.Fatalf("before the resend: %v %v", chats, err)
	}
	// The agent sends it again, and it is stored.
	fail = false
	s.Live(ctx, m, final)
	if ack := reg.last(t); ack.Gap || ack.AckSeq != 2 {
		t.Fatalf("the resend: %+v", ack)
	}
	if s.Lag(m.ID) != 0 {
		t.Fatalf("lag %d after the frame was stored", s.Lag(m.ID))
	}
	// A resend of what is stored (its ack was lost) is acknowledged and writes nothing twice.
	s.Live(ctx, m, final)
	if ack := reg.last(t); ack.Gap || ack.AckSeq != 2 {
		t.Fatalf("a repeat: %+v", ack)
	}
	chats, err := List(ctx, db, mine.UserID, []string{m.ID})
	if err != nil || len(chats) != 2 {
		t.Fatalf("after the resend: %v %v", chats, err)
	}
}

func TestAFrameOfAChannelNeverStoredIsAskedForFromItsStart(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	reg := &ackCaller{}
	s := New(Deps{DB: db, Reg: reg, Now: func() time.Time { return now }})
	s.apply = func(context.Context, *store.DB, Owner, hubproto.HistoryEvents, time.Time) (FrameResult, error) {
		return FrameResult{}, errors.New("database is locked")
	}
	m := store.Machine{ID: mine.MachineID, CreatedBy: mine.UserID}
	s.Live(ctx, m, j(live("convs", 7, upsert("tab-a", 1, 10))))
	if ack := reg.last(t); !ack.Gap || ack.AckSeq != 6 || ack.Epoch != "ep-1" {
		t.Fatalf("%+v", ack)
	}
	// A frame that can never be stored gets no answer: the agent would send it for ever.
	before := len(reg.acks)
	s.apply = ApplyFrame
	s.Live(ctx, m, j(live("terminal", 1, upsert("tab-a", 1, 10))))
	if len(reg.acks) != before {
		t.Fatalf("a bad frame was answered: %+v", reg.last(t))
	}
}

// A frame that came as a repl.batch and was not stored is the batch's to send again: no gap ack of the channel; once
// stored it is acked as before. A frame that can never be stored is repl.ErrBad, so the batch is skipped.
func TestABatchNotStoredIsSentAgainByTheBatch(t *testing.T) {
	ctx := context.Background()
	reg := &ackCaller{}
	s := New(Deps{Reg: reg, Now: func() time.Time { return now }})
	m := store.Machine{ID: mine.MachineID, CreatedBy: mine.UserID}
	var cause error
	s.apply = func(context.Context, *store.DB, Owner, hubproto.HistoryEvents, time.Time) (FrameResult, error) {
		if cause != nil {
			return FrameResult{}, cause
		}
		return FrameResult{Epoch: "e", AckSeq: 1}, nil
	}
	frame := j(live("convs", 1, upsert("tab-a", 1, 10)))
	cause = errors.New("database is locked")
	if err := s.LiveBatch(ctx, m, frame); err == nil || errors.Is(err, repl.ErrBad) {
		t.Fatalf("err %v, want one to send again", err)
	}
	if len(reg.acks) != 0 || s.Lag(m.ID) != 1 {
		t.Fatalf("acks %+v, lag %d", reg.acks, s.Lag(m.ID))
	}
	cause = ErrBadFrame
	if err := s.LiveBatch(ctx, m, frame); !errors.Is(err, repl.ErrBad) {
		t.Fatalf("err %v, want repl.ErrBad", err)
	}
	if err := s.LiveBatch(ctx, m, json.RawMessage(`[`)); !errors.Is(err, repl.ErrBad) {
		t.Fatalf("err %v for a frame that is not JSON", err)
	}
	cause = nil
	if err := s.LiveBatch(ctx, m, frame); err != nil {
		t.Fatal(err)
	}
	if ack := reg.last(t); ack.AckSeq != 1 || ack.Gap || s.Lag(m.ID) != 0 {
		t.Fatalf("ack %+v, lag %d", ack, s.Lag(m.ID))
	}
	if err := s.BackfillBatch(ctx, m, json.RawMessage(`[`)); !errors.Is(err, repl.ErrBad) {
		t.Fatalf("backfill err %v", err)
	}
	if err := s.BackfillBatch(ctx, store.Machine{ID: "x"}, json.RawMessage(`[`)); err != nil {
		t.Fatalf("a machine without an owner: %v", err)
	}
}
