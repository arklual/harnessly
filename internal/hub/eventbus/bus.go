package eventbus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Bus is the journal and its subscriptions on the hub's database.
type Bus struct {
	db  *store.DB
	log *slog.Logger
	now func() time.Time
	// Retention is how long the journal keeps an event; 0 keeps it for DefaultRetention.
	Retention time.Duration

	mu       sync.RWMutex
	handlers map[string]Handler
	wake     chan struct{}
}

// DefaultRetention is how long an event stays in the journal by default.
const DefaultRetention = 30 * 24 * time.Hour

// New makes the bus. now and log may be nil.
func New(db *store.DB, log *slog.Logger, now func() time.Time) *Bus {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	return &Bus{db: db, log: log, now: now, handlers: map[string]Handler{}, wake: make(chan struct{}, 1)}
}

const eventCols = `seq, id, owner_id, org_id, type, source, connection, ext, fingerprint, at, received_at, actor, url, thread, ref, mine, facts, via, machine_id, space_id, visibility`

// eventSelect reads eventCols; the columns of 0084 are nullable, and a row without them is the narrowest: no space,
// visible to its owner only.
const eventSelect = `seq, id, owner_id, org_id, type, source, connection, ext, fingerprint, at, received_at, actor, url, thread, ref, mine, facts, via, machine_id, COALESCE(space_id, ''), COALESCE(visibility, 'person')`

// Append writes an event to the journal and gives it back as written. An event whose fingerprint the owner already has
// is not written again: the one in the journal is given back with dup true, with the new way added to its via. The
// subscriptions see an appended event at their next pass; a duplicate is not delivered again.
func (b *Bus) Append(ctx context.Context, e Event) (Event, bool, error) {
	now := b.now()
	e, err := e.normalize(now)
	if err != nil {
		return e, false, err
	}
	facts, _ := json.Marshal(e.Facts)
	dup := false
	err = b.db.TxRetry(ctx, func(s store.Store) error {
		q := s.Q()
		dup = false
		// The counter first: its row lock orders the writers, so seq follows the order of the commits, and two writers
		// of the same fingerprint see each other.
		res, err := q.Exec(ctx, `UPDATE bus_seq SET n = n + 1 WHERE id = 1`)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// The first event of the journal: the counter's row is made here. Two first writers: the loser's
			// statement waits for the winner's commit and then does nothing; its update below counts on.
			if _, err := q.Exec(ctx, `INSERT INTO bus_seq (id, n) VALUES (1, 0) ON CONFLICT (id) DO NOTHING`); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `UPDATE bus_seq SET n = n + 1 WHERE id = 1`); err != nil {
				return err
			}
		}
		var seq int64
		var via string
		switch err := q.QueryRow(ctx, `SELECT seq, via FROM bus_events WHERE owner_id = ? AND fingerprint = ?`, e.Owner, e.Fingerprint).Scan(&seq, &via); {
		case err == nil:
			dup = true
			have := splitVia(via)
			merged := slices.Compact(slices.Sorted(slices.Values(append(have, e.Via...))))
			if !slices.Equal(merged, have) {
				if _, err := q.Exec(ctx, `UPDATE bus_events SET via = ? WHERE seq = ?`, strings.Join(merged, ","), seq); err != nil {
					return err
				}
			}
			e.Seq = seq
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		// The catalog counts the event and says what its owner wants done with it.
		mode, err := touchTrigger(ctx, q, e, now)
		if err != nil {
			return err
		}
		if mode == ModeOff {
			return ErrOff
		}
		if err := q.QueryRow(ctx, `SELECT n FROM bus_seq WHERE id = 1`).Scan(&e.Seq); err != nil {
			return err
		}
		e.ID, e.Received = store.NewID(), now.UTC().Truncate(time.Millisecond)
		_, err = q.Exec(ctx, `INSERT INTO bus_events (`+eventCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Seq, e.ID, e.Owner, e.Org, e.Type, e.Source, e.Connection, e.Ext, e.Fingerprint, store.Ms(e.At), store.Ms(e.Received),
			e.Actor, e.URL, e.Thread, e.Ref, boolInt(e.Mine), string(facts), strings.Join(e.Via, ","), e.Machine, e.Space, e.Visibility)
		return err
	})
	if err != nil {
		return e, false, err
	}
	if dup {
		got, err := b.Get(ctx, e.Owner, e.Seq)
		return got, true, err
	}
	b.Wake()
	return e, false, nil
}

// Wake tells the dispatcher of this replica that the journal grew. The leader of another replica sees it at its next
// tick (PassEvery).
func (b *Bus) Wake() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// Get is one event of an owner by its seq.
func (b *Bus) Get(ctx context.Context, owner string, seq int64) (Event, error) {
	return scanEvent(b.db.QueryRow(ctx, `SELECT `+eventSelect+` FROM bus_events WHERE owner_id = ? AND seq = ?`, owner, seq))
}

// Query selects a page of the journal.
type Query struct {
	// Owner is whose events; All reads every owner's (the hub's own subscriptions, an admin).
	Owner string
	All   bool
	// Spaces and Org widen a person's view (Owner) to the team events of these spaces, or of every space of the
	// organization: what Scope resolved for them. The person's own events of every visibility stay in.
	Spaces []string
	Org    string
	After  int64 // seq: only later events, oldest first
	// Before reads backwards, newest first: only events below this seq; -1 is from the newest. Ignored with After.
	Before int64
	Limit  int // at most 500; 0 is 100
	Filter
}

// List reads the journal in the order of seq.
func (b *Bus) List(ctx context.Context, q Query) ([]Event, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	q.Limit = min(q.Limit, 500)
	back := q.After == 0 && q.Before != 0
	sqlq, args := `SELECT `+eventSelect+` FROM bus_events WHERE seq > ?`, []any{q.After}
	if back && q.Before > 0 {
		sqlq += ` AND seq < ?`
		args = append(args, q.Before)
	}
	switch {
	case q.All:
	case q.Org != "":
		sqlq += ` AND (owner_id = ? OR (org_id = ? AND visibility = ?))`
		args = append(args, q.Owner, q.Org, VisTeam)
	case len(q.Spaces) > 0:
		sqlq += ` AND (owner_id = ? OR (space_id IN (` + strings.TrimSuffix(strings.Repeat("?, ", len(q.Spaces)), ", ") + `) AND visibility = ?))`
		args = append(args, q.Owner)
		for _, s := range q.Spaces {
			args = append(args, s)
		}
		args = append(args, VisTeam)
	default:
		sqlq += ` AND owner_id = ?`
		args = append(args, q.Owner)
	}
	if len(q.Types) == 1 && !strings.HasSuffix(q.Types[0], ".*") {
		sqlq += ` AND type = ?`
		args = append(args, q.Types[0])
	}
	if len(q.Sources) == 1 {
		sqlq += ` AND source = ?`
		args = append(args, q.Sources[0])
	}
	// The rest of the filter is applied here: a page may then hold fewer than Limit events, and the caller pages on.
	order := ` ORDER BY seq LIMIT ?`
	if back {
		order = ` ORDER BY seq DESC LIMIT ?`
	}
	rows, err := b.db.Query(ctx, sqlq+order, append(args, q.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		if q.Filter.Match(e) {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// Head is the last seq written.
func (b *Bus) Head(ctx context.Context) (int64, error) {
	var n int64
	err := b.db.QueryRow(ctx, `SELECT n FROM bus_seq WHERE id = 1`).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil // nothing written yet
	}
	return n, err
}

// Sweep drops the events older than the retention, a batch at a time, and says how many went. A subscription behind
// by more than the retention loses what was dropped: the journal is not a store of record.
func (b *Bus) Sweep(ctx context.Context) (int64, error) {
	keep := b.Retention
	if keep <= 0 {
		keep = DefaultRetention
	}
	before := store.Ms(b.now().Add(-keep))
	var total int64
	for {
		res, err := b.db.Exec(ctx, `DELETE FROM bus_events WHERE seq IN (SELECT seq FROM bus_events WHERE received_at < ? ORDER BY seq LIMIT 1000)`, before)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 1000 || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

type scanner interface{ Scan(...any) error }

func scanEvent(r scanner) (Event, error) {
	var e Event
	var at, received int64
	var mine int
	var facts, via string
	if err := r.Scan(&e.Seq, &e.ID, &e.Owner, &e.Org, &e.Type, &e.Source, &e.Connection, &e.Ext, &e.Fingerprint, &at, &received,
		&e.Actor, &e.URL, &e.Thread, &e.Ref, &mine, &facts, &via, &e.Machine, &e.Space, &e.Visibility); err != nil {
		return e, err
	}
	e.V, e.At, e.Received, e.Mine, e.Via = Version, time.UnixMilli(at).UTC(), time.UnixMilli(received).UTC(), mine == 1, splitVia(via)
	if facts != "" && facts != "{}" && facts != "null" {
		_ = json.Unmarshal([]byte(facts), &e.Facts)
	}
	return e, nil
}

func splitVia(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
