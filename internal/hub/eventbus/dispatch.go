package eventbus

import (
	"context"
	"fmt"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// Handler takes one event for a subscription. It must be idempotent by the event's ID: the bus delivers at least
// once, and a handler that failed, or a hub that stopped between the handler and the cursor, gets the event again. An
// error leaves the cursor before the event and retries it later; the events after it wait (order is kept).
type Handler func(ctx context.Context, s Subscription, e Event) error

// Consume registers the consumer of this hub process: its subscriptions are served by Run. A consumer without a handler
// here (an app that reads over the API) keeps its cursor itself.
func (b *Bus) Consume(consumer string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[consumer] = h
}

func (b *Bus) handler(consumer string) Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.handlers[consumer]
}

const (
	// PassEvery is how often the dispatcher looks at the journal when nothing woke it.
	PassEvery = time.Second
	// SweepEvery is how often the journal drops what is past its retention.
	SweepEvery = time.Hour
	// FailAfter refusals of one event put a subscription in StateFailing.
	FailAfter = 8
	batch     = 200
	// handlerTimeout bounds one delivery.
	handlerTimeout = 30 * time.Second
)

// backoff is the pause before the next try of an event the consumer refused n times.
func backoff(n int) time.Duration {
	switch {
	case n <= 1:
		return time.Second
	case n == 2:
		return 5 * time.Second
	case n == 3:
		return 30 * time.Second
	default:
		return 5 * time.Minute
	}
}

// Run delivers the journal to the subscriptions of the consumers this process handles, until ctx ends. Only one
// replica must run it (the hub runs it under its leader): two would deliver the same event twice.
func (b *Bus) Run(ctx context.Context) {
	tick := time.NewTicker(PassEvery)
	defer tick.Stop()
	lastSweep := time.Time{}
	for ctx.Err() == nil {
		if b.now().Sub(lastSweep) >= SweepEvery {
			lastSweep = b.now()
			if n, err := b.Sweep(ctx); err != nil {
				b.log.Warn("event bus sweep", "error", err)
			} else if n > 0 {
				b.log.Info("event bus swept", "events", n)
			}
		}
		if b.Pass(ctx) {
			continue
		}
		select {
		case <-ctx.Done():
		case <-b.wake:
		case <-tick.C:
		}
	}
}

// Pass serves every due subscription once, a batch each, and says whether any delivered something (then there may be
// more right away).
func (b *Bus) Pass(ctx context.Context) bool {
	subs, err := b.subscriptions(ctx, `WHERE state <> ? AND retry_at <= ? ORDER BY created_at, id`, StatePaused, store.Ms(b.now()))
	if err != nil {
		b.log.Warn("event bus: subscriptions not read", "error", err)
		return false
	}
	worked := false
	for _, s := range subs {
		if ctx.Err() != nil {
			return worked
		}
		h := b.handler(s.Consumer)
		if h == nil {
			continue
		}
		if b.serve(ctx, s, h) {
			worked = true
		}
	}
	return worked
}

// serve delivers one batch to one subscription and moves its cursor. It says whether it delivered or skipped any
// event.
func (b *Bus) serve(ctx context.Context, s Subscription, h Handler) bool {
	events, err := b.List(ctx, Query{Owner: s.Owner, All: s.Owner == "", After: s.Cursor, Limit: batch})
	if err != nil {
		b.log.Warn("event bus: journal not read", "subscription", s.ID, "error", err)
		return false
	}
	cursor := s.Cursor
	for _, e := range events {
		if s.Filter.Match(e) {
			if err := b.deliver(ctx, s, h, e); err != nil {
				b.failed(ctx, s, cursor, e, err)
				return cursor != s.Cursor
			}
		}
		cursor = e.Seq
	}
	if cursor == s.Cursor && s.Failures == 0 {
		return false
	}
	if _, err := b.db.Exec(ctx, `UPDATE bus_subscriptions SET cursor = ?, state = ?, failures = 0, last_error = '', retry_at = 0, updated_at = ?
		WHERE id = ? AND cursor = ?`, cursor, StateActive, store.Ms(b.now()), s.ID, s.Cursor); err != nil {
		b.log.Warn("event bus: cursor not saved", "subscription", s.ID, "error", err)
	}
	return cursor != s.Cursor
}

func (b *Bus) deliver(ctx context.Context, s Subscription, h Handler, e Event) (err error) {
	ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return h(ctx, s, e)
}

// failed keeps what was delivered before the refused event and puts the subscription off by its backoff.
func (b *Bus) failed(ctx context.Context, s Subscription, cursor int64, e Event, cause error) {
	n := s.Failures + 1
	if cursor != s.Cursor {
		n = 1 // a new event refused: count from one
	}
	state := StateActive
	if n >= FailAfter {
		state = StateFailing
	}
	msg := clip(cause.Error(), 500)
	b.log.Warn("event bus: consumer refused an event", "subscription", s.ID, "consumer", s.Consumer, "seq", e.Seq, "failures", n, "error", msg)
	if _, err := b.db.Exec(ctx, `UPDATE bus_subscriptions SET cursor = ?, state = ?, failures = ?, last_error = ?, retry_at = ?, updated_at = ?
		WHERE id = ? AND cursor = ?`, cursor, state, n, msg, store.Ms(b.now().Add(backoff(n))), store.Ms(b.now()), s.ID, s.Cursor); err != nil {
		b.log.Warn("event bus: failure not saved", "subscription", s.ID, "error", err)
	}
}

// Start runs the dispatcher in the background; used by tests and by a hub without a leader.
func (b *Bus) Start(ctx context.Context) {
	safego.Go("event bus", func() { b.Run(ctx) })
}
