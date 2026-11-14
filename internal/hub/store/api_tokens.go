package store

import (
	"context"
	"database/sql"
	"time"
)

// APIToken is a personal token of one of a person's own apps (docs/hub/client-api.md). Only its hash is kept.
type APIToken struct {
	ID         string
	UserID     string
	TokenHash  string
	Client     string
	Device     string
	Scopes     string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	IP         string
}

const apiTokenCols = `id, user_id, token_hash, client, device, scopes, created_at, last_used_at, expires_at, revoked_at, ip`

func scanAPIToken(row interface{ Scan(...any) error }) (APIToken, error) {
	var t APIToken
	var created, used, expires int64
	var revoked sql.NullInt64
	if err := row.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.Client, &t.Device, &t.Scopes, &created, &used, &expires, &revoked, &t.IP); err != nil {
		return APIToken{}, err
	}
	t.CreatedAt, t.LastUsedAt, t.ExpiresAt, t.RevokedAt = fromMs(created), fromMs(used), fromMs(expires), fromNullMs(revoked)
	return t, nil
}

func (s Store) CreateAPIToken(ctx context.Context, t APIToken) error {
	_, err := s.q.Exec(ctx, `INSERT INTO api_tokens (`+apiTokenCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.TokenHash, t.Client, t.Device, t.Scopes, Ms(t.CreatedAt), Ms(t.LastUsedAt), Ms(t.ExpiresAt), nullMs(t.RevokedAt), t.IP)
	return err
}

// APITokenByHash is the token with the hash, revoked or expired ones too: the caller decides.
func (s Store) APITokenByHash(ctx context.Context, hash string) (APIToken, error) {
	return scanAPIToken(s.q.QueryRow(ctx, `SELECT `+apiTokenCols+` FROM api_tokens WHERE token_hash = ?`, hash))
}

// UserAPITokens are a person's tokens that still work, newest use first.
func (s Store) UserAPITokens(ctx context.Context, userID string, now time.Time) ([]APIToken, error) {
	rows, err := s.q.Query(ctx, `SELECT `+apiTokenCols+` FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ? ORDER BY last_used_at DESC, id`, userID, Ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TouchAPIToken moves a token's last use and its end (a sliding term).
func (s Store) TouchAPIToken(ctx context.Context, id string, at, expires time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE api_tokens SET last_used_at = ?, expires_at = ? WHERE id = ? AND revoked_at IS NULL`, Ms(at), Ms(expires), id)
	return err
}

// RevokeAPIToken stops a person's token; false when the person has no such working token.
func (s Store) RevokeAPIToken(ctx context.Context, userID, id string, at time.Time) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, Ms(at), id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RevokeUserAPITokens stops every token of a person (signing out everywhere).
func (s Store) RevokeUserAPITokens(ctx context.Context, userID string, at time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, Ms(at), userID)
	return err
}

// SetChallengeData replaces the data of a challenge that is still there; false when it is gone.
func (s Store) SetChallengeData(ctx context.Context, id, data string) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE auth_challenges SET data = ? WHERE id = ?`, data, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
