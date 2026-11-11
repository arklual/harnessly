package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/metrics"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type rig struct {
	stats  *Stats
	r      *Receiver
	acks   chan hubproto.ReplAck
	stored []string
	fail   error
}

func newRig(t *testing.T) *rig {
	t.Helper()
	g := &rig{stats: NewStats(), acks: make(chan hubproto.ReplAck, 64)}
	h := func(_ context.Context, data json.RawMessage) error {
		if g.fail != nil {
			return g.fail
		}
		g.stored = append(g.stored, string(data))
		return nil
	}
	g.r = NewReceiver(g.stats, "m1", map[string]Handler{"k": h}, func(_ context.Context, a hubproto.ReplAck) error {
		g.acks <- a
		return nil
	}, nil)
	t.Cleanup(g.r.Close)
	return g
}

func (g *rig) batch(kind string, seq int64, data string) {
	raw, _ := json.Marshal(hubproto.ReplBatch{Kind: kind, Seq: seq, Data: json.RawMessage(data)})
	g.r.Handle(context.Background(), raw, time.Now())
}

func (g *rig) ack(t *testing.T) hubproto.ReplAck {
	t.Helper()
	select {
	case a := <-g.acks:
		return a
	case <-time.After(2 * time.Second):
		t.Fatal("no ack")
		return hubproto.ReplAck{}
	}
}

func (g *rig) noAck(t *testing.T) {
	t.Helper()
	select {
	case a := <-g.acks:
		t.Fatalf("unexpected ack %+v", a)
	case <-time.After(50 * time.Millisecond):
	}
}

// Batches are stored in order and each is acknowledged; one sent again is acknowledged without being stored twice.
func TestBatchesAreStoredInOrderAndAcknowledged(t *testing.T) {
	g := newRig(t)
	g.batch("k", 1, `1`)
	if a := g.ack(t); a != (hubproto.ReplAck{Kind: "k", Seq: 1}) {
		t.Fatalf("ack %+v", a)
	}
	g.batch("k", 2, `2`)
	g.ack(t)
	g.batch("k", 1, `1`)
	if a := g.ack(t); a.Seq != 2 || a.Retry {
		t.Fatalf("ack of a duplicate %+v, want seq 2", a)
	}
	if strings.Join(g.stored, ",") != "1,2" {
		t.Fatalf("stored %v", g.stored)
	}
}

// A batch after a hole is not stored and not acknowledged: the agent's wait for the ack sends again from the hole.
func TestBatchAfterAHoleIsNotStored(t *testing.T) {
	g := newRig(t)
	g.batch("k", 1, `1`)
	g.ack(t)
	g.batch("k", 3, `3`)
	g.noAck(t)
	if len(g.stored) != 1 {
		t.Fatalf("stored %v", g.stored)
	}
	if lag := g.stats.Lag(time.Now().Add(time.Second)); lag["k"] < time.Second {
		t.Fatalf("lag %v, want the batch after the hole counted", lag)
	}
	g.batch("k", 2, `2`)
	g.ack(t)
	g.batch("k", 3, `3`)
	g.ack(t)
	if strings.Join(g.stored, ",") != "1,2,3" {
		t.Fatalf("stored %v", g.stored)
	}
}

// A batch the hub could not store is answered with where the kind stands and a retry; the lag runs until it is stored.
func TestBatchNotStoredAsksForARetryAndCountsTheLag(t *testing.T) {
	g := newRig(t)
	g.batch("k", 1, `1`)
	g.ack(t)
	g.fail = errors.New("database down")
	g.batch("k", 2, `2`)
	if a := g.ack(t); a != (hubproto.ReplAck{Kind: "k", Seq: 1, Retry: true}) {
		t.Fatalf("ack %+v, want seq 1 with retry", a)
	}
	if lag := g.stats.Lag(time.Now().Add(5 * time.Second)); lag["k"] < 5*time.Second {
		t.Fatalf("lag %v while a batch waits", lag)
	}
	g.fail = nil
	g.batch("k", 2, `2`)
	if a := g.ack(t); a.Seq != 2 || a.Retry {
		t.Fatalf("ack %+v", a)
	}
	if lag := g.stats.Lag(time.Now()); lag["k"] != 0 {
		t.Fatalf("lag %v after the batch was stored", lag)
	}
}

// A batch that can never be stored, and one of a kind the hub did not name, are acknowledged so the kind goes on.
func TestBadBatchesAreAcknowledgedAndSkipped(t *testing.T) {
	g := newRig(t)
	g.fail = fmt.Errorf("frame: %w", ErrBad)
	g.batch("k", 1, `1`)
	if a := g.ack(t); a.Seq != 1 || a.Retry {
		t.Fatalf("ack %+v", a)
	}
	g.batch("other", 1, `"x"`)
	if a := g.ack(t); a.Kind != "other" || a.Seq != 1 {
		t.Fatalf("ack %+v", a)
	}
	// A kind the machine made up keeps no state and is counted under one label.
	g.r.mu.Lock()
	_, kept := g.r.streams["other"]
	g.r.mu.Unlock()
	g.stats.mu.Lock()
	_, labelled := g.stats.batches["other"]
	unknown := g.stats.batches[KindUnknown][ResultBad]
	g.stats.mu.Unlock()
	if kept || labelled || unknown != 1 {
		t.Fatalf("state kept %v, labelled %v, unknown %d", kept, labelled, unknown)
	}
}

// A new repl.start numbers every kind from 1 again.
func TestStartNumbersAgain(t *testing.T) {
	g := newRig(t)
	g.batch("k", 1, `1`)
	g.ack(t)
	st := g.r.Start()
	if st.Window != hubproto.ReplWindow || len(st.Kinds) != 1 || st.Kinds[0] != "k" {
		t.Fatalf("start %+v", st)
	}
	g.batch("k", 1, `"again"`)
	g.ack(t)
	if len(g.stored) != 2 {
		t.Fatalf("stored %v", g.stored)
	}
}

// The measure has a lag line for every measured kind and the batches by result.
func TestWriteHasEveryKind(t *testing.T) {
	g := newRig(t)
	g.batch("k", 1, `1`)
	g.ack(t)
	var b strings.Builder
	err := metrics.Write(&b, func(w *metrics.Writer) {
		g.stats.Write(w, time.Now(), Measured(), map[string]time.Duration{"usage": 3 * time.Second})
	})
	if err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`harnessly_hub_repl_lag_seconds{kind="history.events"} 0`,
		`harnessly_hub_repl_lag_seconds{kind="history.backfill"} 0`,
		`harnessly_hub_repl_lag_seconds{kind="usage"} 3`,
		`harnessly_hub_repl_batches_total{kind="k",result="stored"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	g.r.Close()
	if len(g.stats.Lag(time.Now())) != 0 {
		t.Error("a closed receiver is still measured")
	}
}

// Every data kind a machine owns says how it reaches the hub, and every kind of the map is a kind of the register.
func TestEveryMachineKindHasATransport(t *testing.T) {
	inMap := map[string]bool{}
	for _, k := range Kinds {
		if k.DataKind != "" {
			inMap[k.DataKind] = true
		}
		if k.Transport == "" || len(k.Wire) == 0 {
			t.Errorf("kind %q says no transport", k.Name)
		}
		if k.Transport == TransportBatches && k.Legacy == "" {
			t.Errorf("batched kind %q says nothing of agents without batches", k.Name)
		}
	}
	register := map[string]store.DataOwner{}
	for _, d := range store.DataKinds {
		register[d.Name] = d.Owner
		if d.Owner == store.OwnerMachine && !inMap[d.Name] {
			t.Errorf("data kind %q is owned by the machine and has no transport in repl.Kinds", d.Name)
		}
	}
	for name := range inMap {
		if _, ok := register[name]; !ok {
			t.Errorf("repl.Kinds names %q, which is not in store.DataKinds", name)
		}
	}
	if len(Batched()) < 2 {
		t.Errorf("batched kinds %v", Batched())
	}
}

func TestRetryWaitGrowsToItsCeiling(t *testing.T) {
	if hubproto.ReplRetryWait(1) != time.Second || hubproto.ReplRetryWait(3) != 4*time.Second || hubproto.ReplRetryWait(100) != time.Minute {
		t.Fatal(hubproto.ReplRetryWait(1), hubproto.ReplRetryWait(3), hubproto.ReplRetryWait(100))
	}
}
