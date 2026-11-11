package resaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// Guest links open a resource without signing in. Only the hash of the token is kept, so the address is shown once,
// when the link is made.

const (
	guestWindow    = 10 * time.Minute
	guestPerWindow = 10
)

type link struct {
	ID, Title, Rights, Pinned, CreatedBy string
	Ref
	Expires, Revoked, LastOpened *time.Time
	Opens, Comments              int64
	CreatedAt                    time.Time
}

func (l link) json(url any) map[string]any {
	version := "latest"
	if l.Pinned != "" {
		version = "pinned"
	}
	return map[string]any{"id": l.ID, "title": l.Title, "rights": l.Rights, "pinnedVersion": nullStr(l.Pinned), "version": version,
		"expiresAt": iso(l.Expires), "revokedAt": iso(l.Revoked), "opens": l.Opens, "lastOpenedAt": iso(l.LastOpened),
		"createdBy": l.CreatedBy, "createdAt": isoOf(l.CreatedAt), "comments": l.Comments, "url": url}
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

const linkCols = `l.link_id, l.kind, l.id, COALESCE(l.space_id, ''), l.title, l.rights, l.pinned_version, l.created_by, l.created_at, l.expires_at, l.revoked_at, l.opens, l.last_opened_at,
	(SELECT COUNT(*) FROM resource_guest_comments c WHERE c.link_id = l.link_id)`

func scanLink(row interface{ Scan(...any) error }) (link, error) {
	var l link
	var created int64
	var exp, rev, opened sql.NullInt64
	if err := row.Scan(&l.ID, &l.Kind, &l.Ref.ID, &l.Space, &l.Title, &l.Rights, &l.Pinned, &l.CreatedBy, &created, &exp, &rev, &l.Opens, &opened, &l.Comments); err != nil {
		return link{}, err
	}
	l.CreatedAt = fromMs(created)
	for _, p := range []struct {
		v sql.NullInt64
		t **time.Time
	}{{exp, &l.Expires}, {rev, &l.Revoked}, {opened, &l.LastOpened}} {
		if p.v.Valid {
			x := fromMs(p.v.Int64)
			*p.t = &x
		}
	}
	return l, nil
}

func (t *txn) links(ref Ref) ([]link, error) {
	rows, err := t.q.Query(t.ctx, `SELECT `+linkCols+` FROM resource_guest_links l WHERE l.kind = ? AND l.id = ? AND COALESCE(l.space_id, '') = ? ORDER BY l.created_at DESC, l.link_id`, ref.Kind, ref.ID, ref.Space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []link
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (t *txn) oneLink(ref Ref, id string) (link, error) {
	l, err := scanLink(t.q.QueryRow(t.ctx, `SELECT `+linkCols+` FROM resource_guest_links l WHERE l.link_id = ? AND l.kind = ? AND l.id = ? AND COALESCE(l.space_id, '') = ?`, id, ref.Kind, ref.ID, ref.Space))
	if errors.Is(err, sql.ErrNoRows) {
		return l, fail("not_found", "Ссылка не найдена")
	}
	return l, err
}

type linkInput struct {
	Rights, Title, Pinned string
	Term                  ttl
}

func (t *txn) createLink(ref Ref, actor string, in linkInput) (link, string, error) {
	if in.Rights != roleView && in.Rights != roleComment {
		return link{}, "", fail("bad_request", "Права ссылки: view или comment")
	}
	exp, err := expiry(in.Term, t.now)
	if err != nil {
		return link{}, "", err
	}
	var live int
	if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM resource_guest_links WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND revoked_at IS NULL`, ref.Kind, ref.ID, ref.Space).Scan(&live); err != nil {
		return link{}, "", err
	}
	if live >= maxLinks {
		return link{}, "", fail("conflict", "Слишком много действующих ссылок: отключите ненужные")
	}
	token, id := newToken(), newID()
	if _, err := t.q.Exec(t.ctx, `INSERT INTO resource_guest_links (link_id, kind, id, space_id, token_hash, rights, pinned_version, title, created_by, created_at, expires_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?)`, id, ref.Kind, ref.ID, ref.Space, hashToken(token), in.Rights, in.Pinned, clip(in.Title, maxTitle), actor, ms(t.now), nullTime(exp)); err != nil {
		return link{}, "", err
	}
	if err := t.log(ref, actor, "link_create", id, map[string]any{"rights": in.Rights}); err != nil {
		return link{}, "", err
	}
	l, err := t.oneLink(ref, id)
	return l, token, err
}

// updateLink changes a link's name, rights or term; term is nil to leave it.
func (t *txn) updateLink(ref Ref, actor, id string, title *string, rights string, term *ttl) (link, error) {
	l, err := t.oneLink(ref, id)
	if err != nil {
		return l, err
	}
	if l.Revoked != nil {
		return l, fail("gone", "Ссылка отключена")
	}
	if title != nil {
		l.Title = clip(*title, maxTitle)
	}
	if rights != "" {
		if rights != roleView && rights != roleComment {
			return l, fail("bad_request", "Права ссылки: view или comment")
		}
		l.Rights = rights
	}
	if term != nil {
		if l.Expires, err = expiry(*term, t.now); err != nil {
			return l, err
		}
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_guest_links SET title = ?, rights = ?, expires_at = ? WHERE link_id = ? AND COALESCE(space_id, '') = ?`, l.Title, l.Rights, nullTime(l.Expires), id, ref.Space); err != nil {
		return l, err
	}
	return l, t.log(ref, actor, "link_update", id, nil)
}

func (t *txn) revokeLink(ref Ref, actor, id string) error {
	l, err := t.oneLink(ref, id)
	if err != nil {
		return err
	}
	if l.Revoked != nil {
		return nil
	}
	if _, err := t.q.Exec(t.ctx, `UPDATE resource_guest_links SET revoked_at = ? WHERE link_id = ? AND COALESCE(space_id, '') = ?`, ms(t.now), id, ref.Space); err != nil {
		return err
	}
	return t.log(ref, actor, "link_revoke", id, nil)
}

func (t *txn) revokeAllLinks(ref Ref, actor string) (int64, error) {
	res, err := t.q.Exec(t.ctx, `UPDATE resource_guest_links SET revoked_at = ? WHERE kind = ? AND id = ? AND COALESCE(space_id, '') = ? AND revoked_at IS NULL`, ms(t.now), ref.Kind, ref.ID, ref.Space)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return n, t.log(ref, actor, "link_revoke_all", "", map[string]any{"count": n})
	}
	return 0, nil
}

// Guest is what a token opens: the resource, the rights and the version the link shows.
type Guest struct {
	LinkID, Title, Rights, Pinned string
	Ref
	Expires *time.Time
}

// Resolve finds the link a token opens, and with it the space of the resource: the token is the one key that names a
// link without its space. A token nobody made is not_found; one switched off or run out is gone.
func (s *Service) Resolve(ctx context.Context, token string) (*Guest, error) {
	if len(token) < 20 || len(token) > 128 {
		return nil, fail("not_found", "Ссылка не найдена")
	}
	t := s.read(ctx)
	l, err := scanLink(t.q.QueryRow(ctx, `SELECT `+linkCols+` FROM resource_guest_links l WHERE l.token_hash = ?`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fail("not_found", "Ссылка не найдена")
	}
	if err != nil {
		return nil, err
	}
	if l.Revoked != nil || (l.Expires != nil && !l.Expires.After(t.now)) {
		return nil, fail("gone", "Ссылка отключена или её срок вышел")
	}
	return &Guest{LinkID: l.ID, Title: l.Title, Rights: l.Rights, Pinned: l.Pinned, Ref: l.Ref, Expires: l.Expires}, nil
}

// RecordOpen counts one opening of a link.
func (s *Service) RecordOpen(ctx context.Context, linkID string) {
	_, _ = s.db.S().Q().Exec(ctx, `UPDATE resource_guest_links SET opens = opens + 1, last_opened_at = ? WHERE link_id = ?`, ms(s.now()), linkID)
}

// GuestCommentAllowed refuses a guest who comments too often through one link.
func (s *Service) GuestCommentAllowed(ctx context.Context, linkID string) error {
	var n int
	since := ms(s.now().Add(-guestWindow))
	if err := s.db.S().Q().QueryRow(ctx, `SELECT COUNT(*) FROM resource_guest_comments WHERE link_id = ? AND created_at > ?`, linkID, since).Scan(&n); err != nil {
		return err
	}
	if n >= guestPerWindow {
		return fail("rate_limited", "Слишком много комментариев подряд: попробуйте через несколько минут")
	}
	return nil
}

// NoteGuestComment ties the thread a guest opened to the link, so the owner sees where it came from.
func (s *Service) NoteGuestComment(ctx context.Context, linkID, threadID, name string) error {
	_, err := s.db.S().Q().Exec(ctx, `INSERT INTO resource_guest_comments (guest_comment_id, link_id, thread_id, guest_name, created_at) VALUES (?, ?, ?, ?, ?)`,
		newID(), linkID, threadID, clip(name, 60), ms(s.now()))
	return err
}

func (t *txn) guestComments(ref Ref) ([]map[string]any, error) {
	rows, err := t.q.Query(t.ctx, `SELECT g.guest_comment_id, g.link_id, l.title, c.body, g.guest_name, g.created_at
		FROM resource_guest_comments g
		JOIN resource_guest_links l ON l.link_id = g.link_id
		JOIN doc_comments c ON c.id = (SELECT id FROM doc_comments WHERE thread_id = g.thread_id ORDER BY created_at, id LIMIT 1)
		WHERE l.kind = ? AND l.id = ? AND COALESCE(l.space_id, '') = ? ORDER BY g.created_at DESC LIMIT 200`, ref.Kind, ref.ID, ref.Space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, linkID, title, text, name string
		var at int64
		if err := rows.Scan(&id, &linkID, &title, &text, &name, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "linkId": linkID, "linkTitle": title, "text": text, "name": name, "anchor": nil, "at": isoOf(fromMs(at))})
	}
	return out, rows.Err()
}
