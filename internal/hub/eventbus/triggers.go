package eventbus

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The catalog of triggers (docs/hub/event-bus.md, «Каталог триггеров»): every kind of event a person's
// connection sends — its source, connection and type — is a trigger the first time it comes, with the mode
// ModeDigest. The person changes the mode; nothing has to be set up for an event to be journaled and digested.

// Modes of a trigger.
const (
	ModeDigest = "digest" // journaled, and the digest takes it (the default)
	ModeNotify = "notify" // and a notice at once
	ModeAgent  = "agent"  // and the agent takes it by itself
	ModeOff    = "off"    // dropped before the journal
)

var modes = []string{ModeDigest, ModeNotify, ModeAgent, ModeOff}

// ErrOff says an event was not journaled: its trigger is switched off by its owner.
var ErrOff = errors.New("eventbus: the trigger is off")

// Trigger is one row of the catalog.
type Trigger struct {
	Source     string    `json:"source"`
	Connection string    `json:"connection,omitempty"`
	Type       string    `json:"type"`
	Mode       string    `json:"mode"`
	Seen       int64     `json:"seen"`
	First      time.Time `json:"first"`
	Last       time.Time `json:"last"`
}

// touchTrigger counts an event in its trigger, making the trigger on its first event, and gives its mode. It runs in
// the transaction that writes the event.
func touchTrigger(ctx context.Context, q store.Q, e Event, now time.Time) (string, error) {
	ms := store.Ms(now)
	if _, err := q.Exec(ctx, `INSERT INTO bus_triggers (owner_id, source, connection, type, mode, seen, first_at, last_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT (owner_id, source, connection, type) DO UPDATE SET seen = bus_triggers.seen + 1, last_at = excluded.last_at`,
		e.Owner, e.Source, e.Connection, e.Type, ModeDigest, ms, ms, ms); err != nil {
		return "", err
	}
	var mode string
	err := q.QueryRow(ctx, `SELECT mode FROM bus_triggers WHERE owner_id = ? AND source = ? AND connection = ? AND type = ?`,
		e.Owner, e.Source, e.Connection, e.Type).Scan(&mode)
	return mode, err
}

// ModeOf is the mode of an event's trigger; ModeDigest when the catalog has none (an event journaled before it).
func (b *Bus) ModeOf(ctx context.Context, e Event) string {
	var mode string
	if err := b.db.QueryRow(ctx, `SELECT mode FROM bus_triggers WHERE owner_id = ? AND source = ? AND connection = ? AND type = ?`,
		e.Owner, e.Source, e.Connection, e.Type).Scan(&mode); err != nil {
		return ModeDigest
	}
	return mode
}

// Triggers lists an owner's catalog, the most recent first.
func (b *Bus) Triggers(ctx context.Context, owner string) ([]Trigger, error) {
	rows, err := b.db.Query(ctx, `SELECT source, connection, type, mode, seen, first_at, last_at FROM bus_triggers
		WHERE owner_id = ? ORDER BY last_at DESC, source, type`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Trigger{}
	for rows.Next() {
		var t Trigger
		var first, last int64
		if err := rows.Scan(&t.Source, &t.Connection, &t.Type, &t.Mode, &t.Seen, &first, &last); err != nil {
			return nil, err
		}
		t.First, t.Last = time.UnixMilli(first).UTC(), time.UnixMilli(last).UTC()
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetMode changes the mode of one of an owner's triggers. A trigger not seen yet is made with it, so a person can
// switch a kind off before its first event.
func (b *Bus) SetMode(ctx context.Context, owner string, t Trigger) error {
	if !slices.Contains(modes, t.Mode) {
		return invalid("mode %q: digest, notify, agent or off", t.Mode)
	}
	if !typeRe.MatchString(t.Type) || !sourceRe.MatchString(t.Source) || len(t.Connection) > idMax {
		return invalid("trigger %s/%s", t.Source, t.Type)
	}
	ms := store.Ms(b.now())
	_, err := b.db.Exec(ctx, `INSERT INTO bus_triggers (owner_id, source, connection, type, mode, seen, first_at, last_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)
		ON CONFLICT (owner_id, source, connection, type) DO UPDATE SET mode = excluded.mode, updated_at = excluded.updated_at`,
		owner, t.Source, t.Connection, t.Type, t.Mode, ms, ms, ms)
	return err
}

func (b *Bus) listTriggers(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	ts, err := b.Triggers(r.Context(), pr.User.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"triggers": ts})
	return nil
}

// putTrigger is PUT /events/triggers {source, connection, type, mode}: the person's own catalog only; a token may
// read the catalog but not change it.
func (b *Bus) putTrigger(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	if pr.Token != nil {
		return errScope
	}
	var t Trigger
	if err := httpx.Decode(r, &t); err != nil {
		return err
	}
	if err := b.SetMode(r.Context(), pr.User.ID, t); err != nil {
		if errors.Is(err, ErrInvalid) {
			return httpx.New(http.StatusBadRequest, "invalid_request", err.Error())
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
