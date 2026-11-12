package eval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fakeLinker struct {
	present bool
	calls   []string
	params  []any
	answer  json.RawMessage
	err     error
}

func (f *fakeLinker) Get(string) *link.Link {
	if f.present {
		return &link.Link{}
	}
	return nil
}

func (f *fakeLinker) Do(_ context.Context, _, op string, params any, _ time.Duration) (json.RawMessage, error) {
	f.calls = append(f.calls, op)
	f.params = append(f.params, params)
	return f.answer, f.err
}

func TestHubLinkConnectedNeedsLinkAndFeature(t *testing.T) {
	f := &fakeLinker{present: true}
	offers := true
	l := &HubLink{Links: f, Offers: func(_, feature string) bool { return offers && feature == hubproto.FeatureEvalRun }}
	if !l.Connected("m") {
		t.Fatal("a linked machine that offers eval.run is reachable")
	}
	offers = false
	if l.Connected("m") {
		t.Fatal("a machine that does not offer eval.run takes no runs")
	}
	offers, f.present = true, false
	if l.Connected("m") {
		t.Fatal("a machine that is not linked takes no runs")
	}
}

func TestHubLinkStartSaysWhenNothingWasTaken(t *testing.T) {
	req := RunRequest{RunID: "r", Task: json.RawMessage(`{"key":"t"}`), Arm: json.RawMessage(`{"key":"a"}`)}
	for name, c := range map[string]struct {
		err          error
		notDelivered bool
	}{
		"offline": {httpx.ErrMachineOffline, true},
		"policy":  {httpx.New(http.StatusForbidden, "machine_policy", "x"), true},
		"busy":    {httpx.Conflict("busy", "x"), true},
		"timeout": {httpx.ErrMachineTimeout, false},
		"other":   {errors.New("boom"), false},
	} {
		f := &fakeLinker{err: c.err}
		err := (&HubLink{Links: f}).Start(context.Background(), "m", req)
		if err == nil || errors.Is(err, ErrNotDelivered) != c.notDelivered {
			t.Errorf("%s: got %v", name, err)
		}
	}
	f := &fakeLinker{answer: json.RawMessage(`{}`)}
	if err := (&HubLink{Links: f}).Start(context.Background(), "m", req); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(f.params[0])
	if f.calls[0] != hubproto.OpEvalRunStart || string(b) != `{"runId":"r","task":{"key":"t"},"arm":{"key":"a"}}` {
		t.Fatalf("got %s %s", f.calls[0], b)
	}
}

func TestHubLinkPollSplitsAndDecodes(t *testing.T) {
	f := &fakeLinker{answer: json.RawMessage(`{"runs":{"a":{"runId":"a","state":"running"}}}`)}
	ids := make([]string, hubproto.EvalRunPollMax+1)
	for i := range ids {
		ids[i] = "a"
	}
	got, err := (&HubLink{Links: f}).Poll(context.Background(), "m", ids)
	if err != nil || len(f.calls) != 2 || got["a"].State != "running" {
		t.Fatalf("got %v %v calls=%d", got, err, len(f.calls))
	}
	f.answer = json.RawMessage(`nonsense`)
	if _, err := (&HubLink{Links: f}).Poll(context.Background(), "m", ids[:1]); err == nil {
		t.Fatal("an unreadable answer must be an error")
	}
	f.err = httpx.ErrMachineTimeout
	if _, err := (&HubLink{Links: f}).Poll(context.Background(), "m", ids[:1]); err == nil {
		t.Fatal("a poll that did not get through must be an error")
	}
}
