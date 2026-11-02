package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/usageoutbox"
)

// fakeFeed writes what a test tells it to into the outbox and says so, as the real feed does after a pass.
type fakeFeed struct {
	deps    UsageDeps
	written func(string, int64)
	ready   chan struct{} // closed when Run starts
}

func (f *fakeFeed) Run(ctx context.Context, _ time.Duration, written func(string, int64)) {
	f.written = written
	close(f.ready)
	<-ctx.Done()
}

func (f *fakeFeed) add(t *testing.T, n int) {
	t.Helper()
	recs := make([]hubproto.UsageRecord, n)
	for i := range recs {
		recs[i] = hubproto.UsageRecord{Kind: hubproto.UsageCall, Call: &hubproto.UsageCallFact{Provider: "claude", MsgKey: "r:m" + string(rune('a'+i)), SessionKey: "s", Model: "m", Input: 3}}
	}
	last, err := f.deps.Box.Append(recs)
	if err != nil {
		t.Fatal(err)
	}
	f.written(f.deps.Box.Epoch(), last)
}

func usageRig(t *testing.T) (*hubRig, *fakeFeed) {
	t.Helper()
	feed := &fakeFeed{ready: make(chan struct{})}
	r := startHubAgent(t, rigOptions{usageFeed: func(d UsageDeps) (UsageFeed, error) {
		feed.deps = d
		return feed, nil
	}})
	return r, feed
}

func TestHubUsageAdvertisedAndAnnouncedAfterConnect(t *testing.T) {
	r, feed := usageRig(t)
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	found := false
	for _, f := range hello.Features {
		found = found || f == hubproto.FeatureUsage
	}
	if !found {
		t.Fatalf("features %v", hello.Features)
	}
	if want := filepath.Join(r.rig.Home, ".harnessly", "usage-outbox"); feed.deps.Dir != want {
		t.Errorf("outbox in %s, want %s", feed.deps.Dir, want)
	}
	// Once after the connect, unasked, even with nothing in the outbox.
	ev, _ := c.waitEvent(hubproto.EventUsageAvailable, 0, 20*time.Second)
	if ev == nil {
		t.Fatal("no usage.available after connect")
	}
	var epoch string
	var seq int64
	_ = json.Unmarshal(ev["epoch"], &epoch)
	_ = json.Unmarshal(ev["seq"], &seq)
	if epoch == "" || epoch != feed.deps.Box.Epoch() || seq != 0 {
		t.Errorf("epoch %q seq %d", epoch, seq)
	}
}

func TestHubUsageReadAndAck(t *testing.T) {
	r, feed := usageRig(t)
	c := r.connect()
	_, next := c.waitEvent(hubproto.EventUsageAvailable, 0, 20*time.Second)

	// Empty: last = after. The first read is what starts the feed.
	empty := decode[hubproto.UsageReadResult](t, c.mustCall(hubproto.OpUsageRead, map[string]any{"epoch": "", "after": 0, "max": 10}))
	if len(empty.Records) != 0 || empty.Last != 0 || empty.More || empty.Epoch != feed.deps.Box.Epoch() {
		t.Fatalf("%+v", empty)
	}
	select {
	case <-feed.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the first read did not start the feed")
	}
	feed.add(t, 5)
	// The new records are announced, throttled to one event per ten seconds: this is the first since the connect one, so
	// it comes when the time is over; only the read is checked here.
	got := decode[hubproto.UsageReadResult](t, c.mustCall(hubproto.OpUsageRead, map[string]any{"epoch": empty.Epoch, "after": 0, "max": 3}))
	if len(got.Records) != 3 || got.Last != 3 || !got.More || got.Records[0].Seq != 1 {
		t.Fatalf("%+v", got)
	}
	again := decode[hubproto.UsageReadResult](t, c.mustCall(hubproto.OpUsageRead, map[string]any{"epoch": empty.Epoch, "after": 0, "max": 3}))
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Errorf("the same read answered twice differently:\n%s\n%s", a, b)
	}
	// An epoch the hub does not know: the batch starts at the beginning of the current one.
	other := decode[hubproto.UsageReadResult](t, c.mustCall(hubproto.OpUsageRead, map[string]any{"epoch": "old", "after": 4, "max": 10}))
	if other.Epoch != empty.Epoch || len(other.Records) != 5 || other.Records[0].Seq != 1 {
		t.Fatalf("%+v", other)
	}
	// max is clamped.
	one := decode[hubproto.UsageReadResult](t, c.mustCall(hubproto.OpUsageRead, map[string]any{"epoch": empty.Epoch, "after": 0, "max": 0}))
	if len(one.Records) != 1 {
		t.Errorf("max 0 gave %d records", len(one.Records))
	}

	c.mustCall(hubproto.OpUsageAck, map[string]any{"epoch": empty.Epoch, "seq": 3})
	if feed.deps.Box.Pending() != 2 {
		t.Errorf("pending %d after ack 3 of 5", feed.deps.Box.Pending())
	}
	rest := decode[hubproto.UsageReadResult](t, c.mustCall(hubproto.OpUsageRead, map[string]any{"epoch": empty.Epoch, "after": 0, "max": 10}))
	if len(rest.Records) != 2 || rest.Records[0].Seq != 4 || rest.More {
		t.Fatalf("%+v", rest)
	}
	if _, e := c.call(hubproto.OpUsageAck, map[string]any{"epoch": empty.Epoch}); !strings.HasPrefix(e, "invalid: ") {
		t.Errorf("ack without seq: %q", e)
	}

	// The records come with the event: after the throttle's wait the hub is told once, with the newest seq.
	ev, _ := c.waitEvent(hubproto.EventUsageAvailable, next, 30*time.Second)
	if ev == nil {
		t.Fatal("no usage.available after new records")
	}
	var seq int64
	_ = json.Unmarshal(ev["seq"], &seq)
	if seq != 5 {
		t.Errorf("event seq %d", seq)
	}
}

func TestHubUsageWithoutAFeedIsNotAdvertised(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	for _, f := range hello.Features {
		if f == hubproto.FeatureUsage {
			t.Fatalf("usage advertised: %v", hello.Features)
		}
	}
	if _, e := c.call(hubproto.OpUsageRead, map[string]any{"epoch": "", "after": 0, "max": 1}); !strings.HasPrefix(e, "unsupported: ") {
		t.Errorf("%q", e)
	}
}

func TestHubUsageFeedThatFailsLeavesTheFeatureOut(t *testing.T) {
	r := startHubAgent(t, rigOptions{usageFeed: func(UsageDeps) (UsageFeed, error) { return nil, errors.New("no transcripts here") }})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	for _, f := range hello.Features {
		if f == hubproto.FeatureUsage {
			t.Fatalf("usage advertised: %v", hello.Features)
		}
	}
}

// A hub that never reads gets no scanning and no records: the feed does not run.
func TestHubUsageFeedWaitsForTheFirstRead(t *testing.T) {
	r, feed := usageRig(t)
	c := r.connect()
	if ev, _ := c.waitEvent(hubproto.EventUsageAvailable, 0, 20*time.Second); ev == nil {
		t.Fatal("no usage.available after connect")
	}
	select {
	case <-feed.ready:
		t.Fatal("the feed runs although the hub never read")
	case <-time.After(500 * time.Millisecond):
	}
	if feed.deps.Box.Last() != 0 || feed.deps.Box.Asked() {
		t.Errorf("last %d asked %v", feed.deps.Box.Last(), feed.deps.Box.Asked())
	}
	// The first read starts it, and the outbox remembers that.
	c.mustCall(hubproto.OpUsageRead, map[string]any{"epoch": "", "after": 0, "max": 1})
	select {
	case <-feed.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the first read did not start the feed")
	}
	if !feed.deps.Box.Asked() {
		t.Error("no marker after the first read")
	}
}

// After a restart the outbox remembers the hub asked: the feed runs without waiting for a read.
func TestHubUsageFeedResumesAtOnceAfterARead(t *testing.T) {
	feed := &fakeFeed{ready: make(chan struct{})}
	r := startHubAgent(t, rigOptions{
		beforeStart: func(dataDir string) {
			box, err := usageoutbox.Open(filepath.Join(dataDir, usageOutboxDir), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := box.Read("", 0, 1); err != nil {
				t.Fatal(err)
			}
			box.Close()
		},
		usageFeed: func(d UsageDeps) (UsageFeed, error) { feed.deps = d; return feed, nil },
	})
	_ = r
	select {
	case <-feed.ready:
	case <-time.After(20 * time.Second):
		t.Fatal("the feed did not start although the hub had read before")
	}
}
