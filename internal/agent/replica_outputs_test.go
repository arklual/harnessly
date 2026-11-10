package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
)

// HRN-879: the cluster's copy gets a tool's whole output, so «show all» opens with the machine off.
func TestReplicaSendsTheWholeOutputOfACutToolOnce(t *testing.T) {
	g := newReplicaRig(t)
	g.r.outQ = make(chan outputJob, outputQueue)
	g.r.outSeen = map[string]int64{}
	whole := strings.Repeat("x", 5000)
	var asked atomic.Int32
	g.r.output = func(conv, entry string) (json.RawMessage, error) {
		asked.Add(1)
		if conv != "c1" || entry != "e1" {
			t.Errorf("asked for %s %s", conv, entry)
		}
		b, _ := json.Marshal(map[string]any{"entryId": entry, "output": map[string]any{"text": whole, "truncated": nil}})
		return b, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.r.shipOutputs(ctx)
	g.r.hello("ep-1")
	g.r.start(true, nil, nil, nil)

	cut := `{"type":"entry_update","entry":{"t":"tool","id":"e1","turnId":"t1","rev":3,"output":{"text":"xxxx","truncated":{"shown":4,"total":5000}}}}`
	g.r.event("conv:c1", 1, raw(cut))
	g.r.event("conv:c1", 2, raw(cut)) // the same revision again: not read twice
	g.r.event("conv:c1", 3, raw(`{"type":"entry_update","entry":{"t":"tool","id":"e2","rev":1,"output":{"text":"ok","truncated":null}}}`))
	g.until(func() bool { return len(g.backfills()) == 1 })

	var ev struct {
		Type    string `json:"type"`
		Entries []struct {
			ID     string `json:"id"`
			TurnID string `json:"turnId"`
			Rev    int64  `json:"rev"`
			Output struct {
				Text      string          `json:"text"`
				Truncated json.RawMessage `json:"truncated"`
			} `json:"output"`
		} `json:"entries"`
	}
	b := g.backfills()[0]
	if b.Ch != "conv:c1" || json.Unmarshal(b.Events[0], &ev) != nil || ev.Type != "feed_reset" || len(ev.Entries) != 1 {
		t.Fatalf("backfill: %+v", b)
	}
	e := ev.Entries[0]
	if e.ID != "e1" || e.TurnID != "t1" || e.Rev != 3 || e.Output.Text != whole || string(e.Output.Truncated) != "null" {
		t.Fatalf("entry: %+v", e)
	}
	if asked.Load() != 1 {
		t.Fatalf("the runtime was asked %d times", asked.Load())
	}
}

func TestWholeOutputKeepsAtMostAMegabyteAndSaysHowMuch(t *testing.T) {
	text := strings.Repeat("я", outputShipMax) // two bytes each
	page, _ := json.Marshal(map[string]any{"output": map[string]any{"text": text}})
	ev, ok := wholeOutput(raw(`{"t":"tool","id":"e1","rev":1}`), page)
	if !ok {
		t.Fatal("not made")
	}
	var got struct {
		Entries []struct {
			Output struct {
				Text      string `json:"text"`
				Truncated struct {
					Shown, Total int
				} `json:"truncated"`
			} `json:"output"`
		} `json:"entries"`
	}
	if json.Unmarshal(ev, &got) != nil || len(got.Entries) != 1 {
		t.Fatalf("event: %s", ev[:200])
	}
	o := got.Entries[0].Output
	if len(o.Text) > outputShipMax || o.Truncated.Shown != len(o.Text) || o.Truncated.Total != len(text) || !strings.HasSuffix(o.Text, "я") {
		t.Fatalf("cut: %d bytes, %+v", len(o.Text), o.Truncated)
	}
}
