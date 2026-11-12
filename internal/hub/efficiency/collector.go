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
	CollectorCommits  = "commits"
	CollectorSkills   = "skills"
	CollectorProgress = "tracker-progress"
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
	// Documents is the source of the documents agents published; nil reads none.
	Documents ArtifactSource
	Log       *slog.Logger
	Now       func() time.Time
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
		// Revert commits go first and the requests last: the newest stored request marks how far the window has been read,
		// so a failure in between repeats the same window.
		if ch, isHost := c.Code.(CommitHost); isHost {
			n, err := c.reverts(ctx, ch, now)
			ok = c.record(ctx, CollectorCommits, now, n, err) && ok
			n, err = c.skills(ctx, ch, now)
			ok = c.record(ctx, CollectorSkills, now, n, err) && ok
		}
		n, err := c.mrs(ctx, now)
		ok = c.record(ctx, CollectorMRs, now, n, err) && ok
	} else {
		_ = c.Store.RecordEffSource(ctx, store.EffSourceState{Source: CollectorMRs, CollectedAt: now.UnixMilli(), Error: "not configured"})
	}
	if c.Tracker != nil {
		n, err := c.comments(ctx, now)
		ok = c.record(ctx, CollectorComments, now, n, err) && ok
		if pt, isProgress := c.Tracker.(ProgressTracker); isProgress {
			n, err = c.progress(ctx, pt, now)
			ok = c.record(ctx, CollectorProgress, now, n, err) && ok
		}
	}
	if c.Documents != nil {
		n, err := c.artifacts(ctx, c.Documents, now)
		ok = c.record(ctx, CollectorArtifacts, now, n, err) && ok
	}
	return ok
}

// artifacts replaces the window of first publications with what the service reports now.
func (c *Collector) artifacts(ctx context.Context, src ArtifactSource, now time.Time) (int, error) {
	list, srcErr := src.Published(ctx, now.Add(-ArtifactsWindow))
	if srcErr != nil && len(list) == 0 {
		return 0, srcErr
	}
	facts := make([]store.EffFact, 0, len(list))
	for _, a := range list {
		b, err := json.Marshal(a)
		if err != nil {
			return 0, err
		}
		facts = append(facts, store.EffFact{Source: KindArtifact, Key: a.Slug, At: a.At.UnixMilli(), Data: string(b)})
	}
	// A partial read must not delete what the failed part would have brought back, so it only adds.
	if srcErr != nil {
		return len(facts), firstErr(c.Store.UpsertEffFacts(ctx, facts, now.UnixMilli()), srcErr)
	}
	return len(facts), c.Store.ReplaceEffFacts(ctx, KindArtifact, facts, now.UnixMilli())
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
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

// mrSince is where the pass over the code host starts: a window before the newest stored request.
func (c *Collector) mrSince(ctx context.Context, now time.Time) (time.Time, error) {
	since := now.Add(-FirstWindow)
	if newest, err := c.Store.NewestEffFactAt(ctx, KindMR); err != nil {
		return since, err
	} else if newest > 0 {
		since = time.UnixMilli(newest).Add(-Overlap)
	}
	return since, nil
}

// reverts stores the revert commits of the same window as the requests.
func (c *Collector) reverts(ctx context.Context, h CommitHost, now time.Time) (int, error) {
	since, err := c.mrSince(ctx, now)
	if err != nil {
		return 0, err
	}
	list, srcErr := h.RevertCommits(ctx, since)
	facts := make([]store.EffFact, 0, len(list))
	for _, m := range list {
		b, err := json.Marshal(m)
		if err != nil {
			return 0, err
		}
		facts = append(facts, store.EffFact{Source: KindRevert, Key: m.Project + "@" + m.SHA, At: m.At.UnixMilli(), Data: string(b)})
	}
	if err := c.Store.UpsertEffFacts(ctx, facts, now.UnixMilli()); err != nil {
		return 0, err
	}
	return len(facts), srcErr
}

// SkillsWindow is how far back the commits that changed a skill are read.
const SkillsWindow = 35 * 24 * time.Hour

// skills stores the commits that changed a skill within the window.
func (c *Collector) skills(ctx context.Context, h CommitHost, now time.Time) (int, error) {
	list, srcErr := h.SkillCommits(ctx, now.Add(-SkillsWindow))
	facts := make([]store.EffFact, 0, len(list))
	for _, m := range list {
		b, err := json.Marshal(m)
		if err != nil {
			return 0, err
		}
		facts = append(facts, store.EffFact{Source: KindSkill, Key: m.SHA, At: m.At.UnixMilli(), Data: string(b)})
	}
	if err := c.Store.UpsertEffFacts(ctx, facts, now.UnixMilli()); err != nil {
		return 0, err
	}
	return len(facts), srcErr
}

func (c *Collector) mrs(ctx context.Context, now time.Time) (int, error) {
	since, err := c.mrSince(ctx, now)
	if err != nil {
		return 0, err
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
	if err := c.Store.UpsertEffFacts(ctx, facts, now.UnixMilli()); err != nil {
		return 0, err
	}
	return len(facts), c.Store.PruneEffFacts(ctx, KindComment, now.Add(-CommentKeep).UnixMilli())
}

// progress replaces the issues in progress with the current list.
func (c *Collector) progress(ctx context.Context, t ProgressTracker, now time.Time) (int, error) {
	list, err := t.InProgress(ctx)
	if err != nil {
		return 0, err
	}
	facts := make([]store.EffFact, 0, len(list))
	for _, p := range list {
		b, err := json.Marshal(p)
		if err != nil {
			return 0, err
		}
		facts = append(facts, store.EffFact{Source: KindProgress, Key: p.Issue, At: p.LastActivityAt.UnixMilli(), Data: string(b)})
	}
	return len(facts), c.Store.ReplaceEffFacts(ctx, KindProgress, facts, now.UnixMilli())
}
