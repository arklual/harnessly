package filestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const uploadColumns = `id, member, sha, size, part_size, created, expires, handle, hash, state, error`

func scanUpload(row interface{ Scan(...any) error }) (*Upload, error) {
	var u Upload
	if err := row.Scan(&u.ID, &u.Member, &u.Sha, &u.Size, &u.PartSize, &u.Created, &u.Expires, &u.Handle, &u.Hash,
		&u.State, &u.Error); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *sqlRoom) uploadParts(ctx context.Context, u *Upload) error {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT n, sha, size, tag FROM upload_parts WHERE upload = ? ORDER BY n`), u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	u.Parts = []UploadPart{}
	for rows.Next() {
		var p UploadPart
		if err := rows.Scan(&p.N, &p.Sha, &p.Size, &p.Tag); err != nil {
			return err
		}
		u.Parts = append(u.Parts, p)
	}
	return rows.Err()
}

func (s *sqlRoom) UploadOpen(ctx context.Context, u Upload, lim OpenLimits, now int64) (Upload, bool, error) {
	if err := s.mu.lock(ctx); err != nil {
		return Upload{}, false, err
	}
	defer s.mu.unlock()
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+uploadColumns+` FROM uploads
		WHERE member = ? AND sha = ? AND size = ? AND part_size = ? AND expires > ? AND state <> ?
		ORDER BY created DESC LIMIT 1`),
		u.Member, u.Sha, u.Size, u.PartSize, now, UploadDone)
	switch got, err := scanUpload(row); {
	case err == nil:
		if err := s.uploadParts(ctx, got); err != nil {
			return Upload{}, false, err
		}
		return *got, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Upload{}, false, err
	}
	var open, mine int
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN member = ? THEN 1 ELSE 0 END), 0)
		FROM uploads WHERE expires > ? AND state <> ?`), u.Member, now, UploadDone).Scan(&open, &mine); err != nil {
		return Upload{}, false, err
	}
	if lim.Room > 0 && open >= lim.Room {
		return Upload{}, false, fmt.Errorf("%w: %d in the room", ErrTooManyUploads, open)
	}
	if lim.Member > 0 && mine >= lim.Member {
		return Upload{}, false, fmt.Errorf("%w: %d of yours", ErrTooManyUploads, mine)
	}
	u.State = UploadReceiving
	if _, err := s.db.ExecContext(ctx, s.q(`INSERT INTO uploads (`+uploadColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		u.ID, u.Member, u.Sha, u.Size, u.PartSize, u.Created, u.Expires, u.Handle, u.Hash, u.State, u.Error); err != nil {
		return Upload{}, false, err
	}
	u.Parts = []UploadPart{}
	return u, true, nil
}

func (s *sqlRoom) UploadGet(ctx context.Context, id string, now int64) (*Upload, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+uploadColumns+` FROM uploads WHERE id = ? AND expires > ?`), id, now)
	u, err := scanUpload(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, s.uploadParts(ctx, u)
}

func (s *sqlRoom) UploadBegun(ctx context.Context, id, handle string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE uploads SET handle = ? WHERE id = ?`), handle, id)
	return err
}

func (s *sqlRoom) UploadPart(ctx context.Context, id string, p UploadPart, hash string, expires int64) (bool, error) {
	if err := s.mu.lock(ctx); err != nil {
		return false, err
	}
	defer s.mu.unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, s.q(`UPDATE uploads SET received = received + 1, hash = ?, expires = ?
		WHERE id = ? AND received = ? AND state = ?`), hash, expires, id, p.N, UploadReceiving)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO upload_parts (upload, n, sha, size, tag) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (upload, n) DO UPDATE SET sha = excluded.sha, size = excluded.size, tag = excluded.tag`),
		id, p.N, p.Sha, p.Size, p.Tag); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *sqlRoom) UploadSetState(ctx context.Context, id, from, to, errText string, expires int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE uploads SET state = ?, error = ?, expires = ? WHERE id = ? AND state = ?`),
		to, errText, expires, id, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *sqlRoom) UploadDrop(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, s.q(`DELETE FROM upload_parts WHERE upload = ?`), id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM uploads WHERE id = ?`), id)
	return err
}

func (s *sqlRoom) UploadsExpired(ctx context.Context, now int64) ([]Upload, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+uploadColumns+` FROM uploads WHERE expires <= ? ORDER BY expires LIMIT 500`), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}
