package eventbus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Subscription states.
const (
	StateActive  = "active"
	StatePaused  = "paused"
	StateFailing = "failing" // the consumer refused the same event FailAfter times; still retried, slower
)

// Subscription reads the journal from Cursor on, for one consumer.
type Subscription struct {
	ID string `json:"id"`
	// Owner is whose events the subscription sees; "" is the hub's own subscription that sees every owner's.
	Owner string `json:"owner"`
	// Consumer names who takes the events: rules, inbox, notify, widget, job, app:<id>, webhook.
	Consumer  string    `json:"consumer"`
	Name      string    `json:"name,omitempty"`
	Filter    Filter    `json:"filter"`
	Cursor    int64     `json:"cursor"`
	State     string    `json:"state"`
	Failures  int       `json:"failures,omitempty"`
	LastError string    `json:"lastError,omitempty"`
	RetryAt   time.Time `json:"retryAt,omitzero"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
}

// ErrNoSubscription: the subscription is not there, or not the owner's.
var ErrNoSubscription = errors.New("eventbus: no such subscription")

var consumerRe = regexp.MustCompile(`^[a-z][a-z0-9-]*(:[a-z0-9_.-]+)?$`)

const subCols = `id, owner_id, consumer, name, filter, cursor, state, failures, last_error, retry_at, created_at, updated_at`

// FromEnd starts a new subscription at the current end of the journal: it sees what comes after.
const FromEnd int64 = -1

// Subscribe makes a subscription, or gives back the one of the same owner, consumer and name as it is (with its
// cursor): subscribing again on every start of a consumer is safe. from is the seq to read after, or FromEnd.
func (b *Bus) Subscribe(ctx context.Context, s Subscription, from int64) (Subscription, error) {
	if !consumerRe.MatchString(s.Consumer) || len(s.Consumer) > 80 || len(s.Name) > 80 || len(s.Owner) > idMax {
		return s, invalid("consumer %q or name", s.Consumer)
	}
	if err := s.Filter.valid(); err != nil {
		return s, err
	}
	if got, err := b.subscriptionByName(ctx, s.Owner, s.Consumer, s.Name); err == nil {
		return got, nil
	} else if !errors.Is(err, ErrNoSubscription) {
		return s, err
	}
	if from == FromEnd {
		head, err := b.Head(ctx)
		if err != nil {
			return s, err
		}
		from = head
	}
	filter, _ := json.Marshal(s.Filter)
	now := b.now().UTC().Truncate(time.Millisecond)
	s.ID, s.Cursor, s.State, s.Created, s.Updated = store.NewID(), max(from, 0), StateActive, now, now
	_, err := b.db.Exec(ctx, `INSERT INTO bus_subscriptions (`+subCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, 0, '', 0, ?, ?)
		ON CONFLICT (owner_id, consumer, name) DO NOTHING`,
		s.ID, s.Owner, s.Consumer, s.Name, string(filter), s.Cursor, s.State, store.Ms(now), store.Ms(now))
	if err != nil {
		return s, err
	}
	// Another replica may have made it in between: the one in the table wins.
	return b.subscriptionByName(ctx, s.Owner, s.Consumer, s.Name)
}

// Subscriptions lists an owner's subscriptions.
func (b *Bus) Subscriptions(ctx context.Context, owner string) ([]Subscription, error) {
	return b.subscriptions(ctx, `WHERE owner_id = ? ORDER BY created_at, id`, owner)
}

// Subscription is one of an owner's subscriptions.
func (b *Bus) Subscription(ctx context.Context, owner, id string) (Subscription, error) {
	return one(b.db.QueryRow(ctx, `SELECT `+subCols+` FROM bus_subscriptions WHERE id = ? AND owner_id = ?`, id, owner))
}

// Unsubscribe drops an owner's subscription.
func (b *Bus) Unsubscribe(ctx context.Context, owner, id string) error {
	res, err := b.db.Exec(ctx, `DELETE FROM bus_subscriptions WHERE id = ? AND owner_id = ?`, id, owner)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSubscription
	}
	return nil
}

// Rewind moves a subscription's cursor (to replay what it read, or to skip ahead) and makes it active again.
func (b *Bus) Rewind(ctx context.Context, owner, id string, cursor int64) error {
	res, err := b.db.Exec(ctx, `UPDATE bus_subscriptions SET cursor = ?, state = ?, failures = 0, last_error = '', retry_at = 0, updated_at = ?
		WHERE id = ? AND owner_id = ?`, max(cursor, 0), StateActive, store.Ms(b.now()), id, owner)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSubscription
	}
	return nil
}

// SetPaused pauses or resumes a subscription.
func (b *Bus) SetPaused(ctx context.Context, owner, id string, paused bool) error {
	state := StateActive
	if paused {
		state = StatePaused
	}
	res, err := b.db.Exec(ctx, `UPDATE bus_subscriptions SET state = ?, retry_at = 0, updated_at = ? WHERE id = ? AND owner_id = ?`,
		state, store.Ms(b.now()), id, owner)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSubscription
	}
	return nil
}

func (b *Bus) subscriptionByName(ctx context.Context, owner, consumer, name string) (Subscription, error) {
	return one(b.db.QueryRow(ctx, `SELECT `+subCols+` FROM bus_subscriptions WHERE owner_id = ? AND consumer = ? AND name = ?`, owner, consumer, name))
}

func (b *Bus) subscriptions(ctx context.Context, where string, args ...any) ([]Subscription, error) {
	rows, err := b.db.Query(ctx, `SELECT `+subCols+` FROM bus_subscriptions `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		s, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func one(r *sql.Row) (Subscription, error) {
	s, err := scanSub(r)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNoSubscription
	}
	return s, err
}

func scanSub(r scanner) (Subscription, error) {
	var s Subscription
	var filter string
	var retry, created, updated int64
	if err := r.Scan(&s.ID, &s.Owner, &s.Consumer, &s.Name, &filter, &s.Cursor, &s.State, &s.Failures, &s.LastError, &retry, &created, &updated); err != nil {
		return s, err
	}
	_ = json.Unmarshal([]byte(filter), &s.Filter)
	if retry > 0 {
		s.RetryAt = time.UnixMilli(retry).UTC()
	}
	s.Created, s.Updated = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return s, nil
}
