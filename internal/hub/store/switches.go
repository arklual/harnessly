package store

import (
	"context"
	"strings"
	"time"
)

// Wildcard in a switch's person or machine matches anyone.
const Wildcard = "*"

// Switch is one rollout switch row: a feature on or off for a person on a machine.
type Switch struct {
	Feature   string
	Person    string // an email, or Wildcard
	Machine   string // a machine id, or Wildcard
	Enabled   bool
	Note      string
	UpdatedBy string
	UpdatedAt time.Time
}

const switchCols = `feature, person_email, machine_id, enabled, note, updated_by, updated_at`

// SwitchesFor are the rows that can decide a feature name (or, with prefix, every feature that starts with name) for a
// person on a machine: the exact pair and the wildcards.
func (s Store) SwitchesFor(ctx context.Context, name string, prefix bool, person, machine string) ([]Switch, error) {
	q := `SELECT ` + switchCols + ` FROM rollout_switches WHERE person_email IN (?, '*') AND machine_id IN (?, '*') AND `
	if prefix {
		return s.scanSwitches(ctx, q+`substr(feature, 1, ?) = ?`, person, machine, len(name), name)
	}
	return s.scanSwitches(ctx, q+`feature = ?`, person, machine, name)
}

// ListSwitches are all rows, optionally of the features that start with prefix.
func (s Store) ListSwitches(ctx context.Context, prefix string) ([]Switch, error) {
	if prefix == "" {
		return s.scanSwitches(ctx, `SELECT `+switchCols+` FROM rollout_switches ORDER BY feature, person_email, machine_id`)
	}
	return s.scanSwitches(ctx, `SELECT `+switchCols+` FROM rollout_switches WHERE substr(feature, 1, ?) = ? ORDER BY feature, person_email, machine_id`, len(prefix), prefix)
}

func (s Store) scanSwitches(ctx context.Context, q string, args ...any) ([]Switch, error) {
	rows, err := s.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Switch
	for rows.Next() {
		var w Switch
		var on int
		var at int64
		if err := rows.Scan(&w.Feature, &w.Person, &w.Machine, &on, &w.Note, &w.UpdatedBy, &at); err != nil {
			return nil, err
		}
		w.Enabled, w.UpdatedAt = on != 0, fromMs(at)
		out = append(out, w)
	}
	return out, rows.Err()
}

func wild(person, machine string) (string, string) {
	if person == "" {
		person = Wildcard
	}
	if machine == "" {
		machine = Wildcard
	}
	return strings.ToLower(person), machine
}

// SetSwitch keeps a row; person and machine empty mean Wildcard.
func (s Store) SetSwitch(ctx context.Context, w Switch) error {
	w.Person, w.Machine = wild(w.Person, w.Machine)
	_, err := s.q.Exec(ctx, `INSERT INTO rollout_switches (`+switchCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (feature, person_email, machine_id) DO UPDATE SET enabled = excluded.enabled, note = excluded.note, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		w.Feature, w.Person, w.Machine, boolInt(w.Enabled), w.Note, w.UpdatedBy, Ms(w.UpdatedAt))
	return err
}

// DeleteSwitch drops a row, so the next less specific one decides again.
func (s Store) DeleteSwitch(ctx context.Context, feature, person, machine string) error {
	person, machine = wild(person, machine)
	_, err := s.q.Exec(ctx, `DELETE FROM rollout_switches WHERE feature = ? AND person_email = ? AND machine_id = ?`, feature, person, machine)
	return err
}
