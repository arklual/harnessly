package machines

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type batchHistory struct{ live, backfill int }

func (*batchHistory) Connected(store.Machine, []string)                        {}
func (*batchHistory) Live(context.Context, store.Machine, json.RawMessage)     {}
func (*batchHistory) Backfill(context.Context, store.Machine, json.RawMessage) {}
func (h *batchHistory) LiveBatch(context.Context, store.Machine, json.RawMessage) error {
	h.live++
	return nil
}
func (h *batchHistory) BackfillBatch(context.Context, store.Machine, json.RawMessage) error {
	h.backfill++
	return errors.New("not now")
}

type oldHistory struct{}

func (oldHistory) Connected(store.Machine, []string)                        {}
func (oldHistory) Live(context.Context, store.Machine, json.RawMessage)     {}
func (oldHistory) Backfill(context.Context, store.Machine, json.RawMessage) {}

// The chats go as batches only when the agent says it can and the hub keeps them as batches; any other link keeps the
// old events, which the hub still takes.
func TestBatchesOnlyForAnAgentThatSpeaksThem(t *testing.T) {
	h := &batchHistory{}
	s := &Service{history: h}
	m := store.Machine{ID: "m1", CreatedBy: "u1"}
	both := []string{hubproto.FeatureHistory, hubproto.FeatureRepl}
	if s.replHandlers(nil, m, []string{hubproto.FeatureHistory}) != nil {
		t.Error("batches for an agent without the feature")
	}
	if hs := s.replHandlers(nil, store.Machine{ID: "m1"}, both); hs[hubproto.ReplHistoryEvents] != nil || hs[hubproto.EventMetricsSample] == nil {
		t.Error("chats as batches for a machine whose chats have no owner, or its reports not")
	}
	if hs := (&Service{history: oldHistory{}}).replHandlers(nil, m, both); hs[hubproto.ReplHistoryEvents] != nil {
		t.Error("batches for a history that cannot store them")
	}
	hs := s.replHandlers(nil, m, both)
	if len(hs) != len(replEvents)+2 || hs[hubproto.ReplHistoryEvents] == nil || hs[hubproto.ReplHistoryBackfill] == nil {
		t.Fatalf("handlers %d", len(hs))
	}
	// The windows of every kind together fit the hub's queue: an agent that keeps to them is never dropped.
	if len(hs)*hubproto.ReplWindow >= link.BulkQueue {
		t.Fatalf("%d kinds × window %d do not fit the queue of %d", len(hs), hubproto.ReplWindow, link.BulkQueue)
	}
	if err := hs[hubproto.ReplHistoryEvents](context.Background(), nil); err != nil || h.live != 1 {
		t.Fatalf("live %d %v", h.live, err)
	}
	if err := hs[hubproto.ReplHistoryBackfill](context.Background(), nil); err == nil || h.backfill != 1 {
		t.Fatalf("a backfill not stored must be sent again: %v", err)
	}
	// An event out of its batch is handled as the event: a sink the hub has not got takes nothing.
	if err := hs[hubproto.EventDesign](context.Background(), json.RawMessage(`{"event":"design","design":{"kind":"design.created"}}`)); err != nil {
		t.Fatal(err)
	}
	if !bulkEvent(hubproto.EventReplBatch) || !bulkEvent(hubproto.EventHistoryBackfill) {
		t.Error("the batches and the old frames go on the bulk queue")
	}
	if s.Repl() == nil || s.Repl() != s.Repl() {
		t.Error("one measure per service")
	}
}
