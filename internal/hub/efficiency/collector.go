package efficiency

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Collector names, the source column of eff_source_state.
const (
	CollectorMRs      = "mrs"
	CollectorComments = "tracker"
)

const (
	// Every is how often the sources are read.
	Every = time.Hour
	// Overlap is how far before the newest stored fact a pass looks, so that a request updated late is not missed.
	Overlap     = 48 * time.Hour
	FirstWindow = 60 * 24 * time.Hour
	maxBackoff  = 4 * time.Hour
)

// Collector reads the configured sources into the store. A source that is not configured is recorded as such and leaves
// the others alone.
type Collector struct {
	Store   store.Store
	Code    CodeHost
	Tracker Tracker
	Log     *slog.Logger
	Now     func() time.Time
}

// Run collects until ctx ends, backing off after a failed pass.
func (c *Collector) Run(ctx context.Context) {
	wait := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if c.Once(ctx) {
			wait = Every
			continue
		}
		wait = min(max(wait*2, Every), maxBackoff)
	}
}

// Once makes one pass over every source and says whether all of them succeeded.
func (c *Collector) Once(ctx context.Context) bool {
	now := c.now()
	ok := true
	if c.Code != nil {
		n, err := c.mrs(ctx, now)
		ok = c.record(ctx, CollectorMRs, now, n, err) && ok
	} else {
		_ = c.Store.RecordEffSource(ctx, store.EffSourceState{Source: CollectorMRs, CollectedAt: now.UnixMilli(), Error: "not configured"})
	}
	if c.Tracker != nil {
		n, err := c.comments(ctx, now)
		ok = c.record(ctx, CollectorComments, now, n, err) && ok
	}
	return ok
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Collector) record(ctx context.Context, name string, now time.Time, n int, err error) bool {
	st := store.EffSourceState{Source: name, CollectedAt: now.UnixMilli(), OK: err == nil, Facts: n}
	if err != nil {
		st.Error = err.Error()
		if c.Log != nil {
			c.Log.Warn("efficiency source failed", "source", name, "error", st.Error)
		}
	}
	if e := c.Store.RecordEffSource(ctx, st); e != nil && c.Log != nil {
		c.Log.Warn("efficiency source state not stored", "error", e.Error())
	}
	return err == nil
}

func (c *Collector) mrs(ctx context.Context, now time.Time) (int, error) {
	since := now.Add(-FirstWindow)
	if newest, err := c.Store.NewestEffFactAt(ctx, KindMR); err != nil {
		return 0, err
	} else if newest > 0 {
		since = time.UnixMilli(newest).Add(-Overlap)
	}
	list, srcErr := c.Code.MergedMRs(ctx, since)
	facts := make([]store.EffFact, 0, len(list))
	for _, m := range list {
		b, err := json.Marshal(m)
		if err != nil {
			return 0, err
		}
		facts = append(facts, store.EffFact{Source: KindMR, Key: m.Key(), At: m.MergedAt.UnixMilli(), Data: string(b)})
	}
	// What was read is kept even when a group failed: the next pass starts from the newest stored merge.
	if err := c.Store.UpsertEffFacts(ctx, facts, now.UnixMilli()); err != nil {
		return 0, err
	}
	return len(facts), srcErr
}

func (c *Collector) comments(ctx context.Context, now time.Time) (int, error) {
	from := now.Add(-FirstWindow)
	if newest, err := c.Store.NewestEffFactAt(ctx, KindComment); err != nil {
		return 0, err
	} else if newest > 0 {
		from = time.UnixMilli(newest).Add(-time.Hour)
	}
	list, err := c.Tracker.Comments(ctx, from, now)
	if err != nil {
		return 0, err
	}
	facts := make([]store.EffFact, 0, len(list))
	for _, m := range list {
		m.Head = clip(m.Head, 200)
		b, err := json.Marshal(m)
		if err != nil {
			return 0, err
		}
		facts = append(facts, store.EffFact{Source: KindComment, Key: m.Issue + "@" + m.At.UTC().Format(time.RFC3339Nano), At: m.At.UnixMilli(), Data: string(b)})
	}
	return len(facts), c.Store.UpsertEffFacts(ctx, facts, now.UnixMilli())
}
