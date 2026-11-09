package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PersonSettings is a hub user's settings document (the JSON of the runtime's wire.PersonSettings) at a revision.
type PersonSettings struct {
	Settings string
	Rev      int64
}

// PersonSettingsOf is the user's saved settings; ErrNotFound when the user never changed one (the default settings at
// revision 1 are theirs then).
func (s Store) PersonSettingsOf(ctx context.Context, userID string) (PersonSettings, error) {
	var ps PersonSettings
	err := s.q.QueryRow(ctx, `SELECT settings, rev FROM person_settings WHERE user_id = ?`, userID).Scan(&ps.Settings, &ps.Rev)
	if errors.Is(err, sql.ErrNoRows) {
		return PersonSettings{}, ErrNotFound
	}
	return ps, err
}

// SavePersonSettings saves the settings over the revision the caller read, as revision fromRev+1, and says whether it
// did: false when the user's record is at another revision by now. The first save (fromRev 1, no record yet) makes
// the record.
func (s Store) SavePersonSettings(ctx context.Context, userID, settings string, fromRev int64, now time.Time) (bool, error) {
	var res sql.Result
	var err error
	if fromRev == 1 {
		res, err = s.q.Exec(ctx, `INSERT INTO person_settings (user_id, settings, rev, updated_at) VALUES (?, ?, 2, ?)
			ON CONFLICT (user_id) DO NOTHING`, userID, settings, now.UnixMilli())
	} else {
		res, err = s.q.Exec(ctx, `UPDATE person_settings SET settings = ?, rev = ?, updated_at = ? WHERE user_id = ? AND rev = ?`,
			settings, fromRev+1, now.UnixMilli(), userID, fromRev)
	}
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
