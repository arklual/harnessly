package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// EntryTicket is a pass to open a machine's origin or a site, as the database keeps it.
type EntryTicket struct {
	UserID  string
	Scope   string
	Expires time.Time
}

// AddEntryTicket keeps a pass under the hash of its token.
func (s Store) AddEntryTicket(ctx context.Context, tokenHash string, t EntryTicket, issued time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO entry_tickets (token_hash, user_id, scope, issued_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, t.UserID, t.Scope, Ms(issued), Ms(t.Expires))
	return err
}

// UseEntryTicket marks a pass used and returns it; ok is false when there is no such pass or it was used before.
// The mark is one conditional update, so of two replicas shown the same pass at once only one gets it.
func (s Store) UseEntryTicket(ctx context.Context, tokenHash string, now time.Time) (t EntryTicket, ok bool, err error) {
	res, err := s.q.Exec(ctx, `UPDATE entry_tickets SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`, Ms(now), tokenHash)
	if err != nil {
		return t, false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t, false, nil
	}
	var expires int64
	err = s.q.QueryRow(ctx, `SELECT user_id, scope, expires_at FROM entry_tickets WHERE token_hash = ?`, tokenHash).Scan(&t.UserID, &t.Scope, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	t.Expires = fromMs(expires)
	return t, true, nil
}

// EntryTicketSince says whether the person was given a pass for the scope at or after since.
func (s Store) EntryTicketSince(ctx context.Context, userID, scope string, since time.Time) (bool, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM entry_tickets WHERE user_id = ? AND scope = ? AND issued_at > ? LIMIT 1) t`,
		userID, scope, Ms(since)).Scan(&n)
	return n > 0, err
}

// DeleteEntryTicketsBefore drops the passes given before a moment.
func (s Store) DeleteEntryTicketsBefore(ctx context.Context, before time.Time) error {
	_, err := s.q.Exec(ctx, `DELETE FROM entry_tickets WHERE issued_at <= ?`, Ms(before))
	return err
}

// CountLiveEntryTickets is how many passes can still be traded at now.
func (s Store) CountLiveEntryTickets(ctx context.Context, now time.Time) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM entry_tickets WHERE used_at IS NULL AND expires_at > ?`, Ms(now)).Scan(&n)
	return n, err
}
