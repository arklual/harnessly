package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/usageoutbox"
)

// The machine's spend facts for the hub (docs/hub/usage.md): the agent keeps an outbox of model calls and sessions made
// from the CLIs' transcripts, tells the hub when there is something new, and answers usage.read and usage.ack. The facts
// hold numbers and ids and no text of a conversation.

const (
	// usageEvery is how often the transcripts are looked at for new calls.
	usageEvery = 45 * time.Second
	// usageNotifyEvery is the least time between two usage.available events.
	usageNotifyEvery = 10 * time.Second
	usageOutboxDir   = "usage-outbox"
)

// usage is the outbox of a hub-mode agent and what keeps it filled.
type usage struct {
	box  *usageoutbox.Outbox
	feed UsageFeed

	// begun: the feed runs. It starts when the hub first reads (or at once when the outbox remembers it did), so a hub
	// that never reads makes the machine neither scan its transcripts nor fill the outbox.
	begun sync.Once

	mu    sync.Mutex
	last  time.Time // when usage.available was last sent
	timer *time.Timer
}

// UsageFeed fills the usage outbox from the transcripts' facts. Its package reads the runtime's usage report, which
// imports this one, so cmd/agent builds it and hands it over in Config.UsageFeed.
type UsageFeed interface {
	// Run looks at the transcripts now and then every interval until ctx is done; written is told the epoch and newest
	// seq of the outbox after a pass that wrote something.
	Run(ctx context.Context, every time.Duration, written func(epoch string, last int64))
}

// UsageDeps is what a feed is built from.
type UsageDeps struct {
	Box *usageoutbox.Outbox
	// Dir is the outbox's folder, where the feed keeps what it has written.
	Dir           string
	Home, DataDir string
	Hostname      string
	// Getenv is a variable as a CLI started by the agent would see it (provider is claude or codex).
	Getenv func(provider, name string) string
}

// startUsage opens the outbox and starts the feed. A machine that cannot keep the outbox does not say it can: `usage`
// stays out of its hello and the hub never asks.
func (h *hub) startUsage() {
	cfg := h.a.cfg
	if cfg.UsageFeed == nil {
		return
	}
	dir := filepath.Join(cfg.DataDir, usageOutboxDir)
	box, err := usageoutbox.Open(dir, nil)
	if err != nil {
		Log("warn", "usage outbox not opened; this machine reports no spend to the hub", F{"error", err.Error()})
		return
	}
	feed, err := cfg.UsageFeed(UsageDeps{Box: box, Dir: dir, Home: cfg.Home, DataDir: cfg.DataDir, Hostname: h.a.hello.Hostname, Getenv: h.cliEnv})
	if err != nil {
		_ = box.Close()
		Log("warn", "usage feed not started; this machine reports no spend to the hub", F{"error", err.Error()})
		return
	}
	h.usage = &usage{box: box, feed: feed}
	if box.Asked() {
		h.usageBegin()
	}
}

// usageBegin starts the feed, once.
func (h *hub) usageBegin() {
	u := h.usage
	u.begun.Do(func() {
		if h.ctx.Err() != nil {
			return
		}
		h.goBG(func() { u.feed.Run(h.ctx, usageEvery, func(string, int64) { h.usageWritten() }) })
	})
}

// cliEnv is a variable as a CLI started by the agent would see it: the agent's env files over its own environment.
func (h *hub) cliEnv(provider, name string) string {
	if v, ok := h.env.Merged(provider)[name]; ok && v != "" {
		return v
	}
	return os.Getenv(name)
}

func (h *hub) stopUsage() {
	u := h.usage
	if u == nil {
		return
	}
	u.mu.Lock()
	if u.timer != nil {
		u.timer.Stop()
	}
	u.mu.Unlock()
	_ = u.box.Close()
}

// usageWritten tells the hub there are new records, at most every usageNotifyEvery: a call that comes sooner is told
// when that time is over.
func (h *hub) usageWritten() {
	u := h.usage
	u.mu.Lock()
	defer u.mu.Unlock()
	wait := usageNotifyEvery - time.Since(u.last)
	if wait <= 0 {
		u.sendLocked(h)
		return
	}
	if u.timer == nil {
		u.timer = time.AfterFunc(wait, func() {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.sendLocked(h)
		})
	}
}

// usageAnnounce is the event after a connect: the hub may have missed what came while the link was down.
func (h *hub) usageAnnounce() {
	u := h.usage
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sendLocked(h)
}

func (u *usage) sendLocked(h *hub) {
	u.last, u.timer = time.Now(), nil
	h.emitFlat(hubproto.EventUsageAvailable, hubproto.UsageAvailableEvent{Epoch: u.box.Epoch(), Seq: u.box.Last()})
}

// emitFlat sends an event whose fields sit beside `event`, as the usage events of the contract do.
func (h *hub) emitFlat(event string, fields any) {
	raw, err := json.Marshal(fields)
	if err != nil {
		Log("error", "event not encoded", F{"event", event}, F{"error", err.Error()})
		return
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	m["event"], _ = json.Marshal(event)
	frame, err := marshal(m)
	if err != nil {
		return
	}
	for _, l := range h.hubLinks() {
		if l.wants(event) {
			l.sendText(frame)
		}
	}
}

// opUsage answers usage.read and usage.ack.
func (h *hub) opUsage(op string, req map[string]json.RawMessage) (any, func(), error) {
	u := h.usage
	if u == nil {
		return nil, nil, hubproto.Errf(hubproto.CodeUnsupported, "this machine keeps no usage outbox")
	}
	epoch, _ := field[string](req, "epoch")
	if op == hubproto.OpUsageAck {
		seq, ok := field[int64](req, "seq")
		if !ok || seq < 0 {
			return nil, nil, invalid("seq must be a number")
		}
		if err := u.box.Ack(epoch, seq); err != nil {
			return nil, nil, err
		}
		return empty(), nil, nil
	}
	after, _ := field[int64](req, "after")
	limit, ok := field[int](req, "max")
	if !ok {
		limit = hubproto.UsageReadMax
	}
	res, err := u.box.Read(epoch, after, limit)
	if err != nil {
		return nil, nil, err
	}
	h.usageBegin()
	return res, nil, nil
}
