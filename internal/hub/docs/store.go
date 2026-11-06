package docs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Who may do what with a document besides its owner and the administrators of the installation.
const (
	levelNone    = "none" // only the owner and the administrators
	levelView    = "view"
	levelComment = "comment"
	levelEdit    = "edit"
)

var levelRank = map[string]int{levelNone: 0, levelView: 1, levelComment: 2, levelEdit: 3}

type author struct {
	Kind  string `json:"kind"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type comment struct {
	ID          string  `json:"id"`
	ThreadID    string  `json:"threadId"`
	Author      author  `json:"author"`
	Body        string  `json:"body"`
	CreatedAt   string  `json:"createdAt"`
	DeliveredAt *string `json:"deliveredAt"`
	// DeliveryError is always null: this hub has no conversation to hand a comment to.
	DeliveryError *string `json:"deliveryError"`
}

type thread struct {
	ID         string          `json:"id"`
	ExternalID *string         `json:"externalId"`
	Slug       string          `json:"slug"`
	Anchor     json.RawMessage `json:"anchor"`
	Status     string          `json:"status"`
	CreatedBy  author          `json:"createdBy"`
	CreatedAt  string          `json:"createdAt"`
	ResolvedAt *string         `json:"resolvedAt"`
	Comments   []comment       `json:"comments"`
}

type meta struct {
	Slug       string
	OwnerEmail string
	OwnerName  string
	MachineID  string
	SourcePath string
	Title      string
	TeamLevel  string
	CreatedAt  int64
	UpdatedAt  int64
}

func ts(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }

func tsPtr(ms sql.NullInt64) *string {
	if !ms.Valid {
		return nil
	}
	s := ts(ms.Int64)
	return &s
}

func (s *Service) meta(ctx context.Context, slug string) (*meta, error) {
	var m meta
	var machine, src sql.NullString
	err := s.db.S().Q().QueryRow(ctx, `SELECT slug, owner_email, owner_name, machine_id, source_path, title, team_level, created_at, updated_at
		FROM doc_meta WHERE slug = ?`, slug).Scan(&m.Slug, &m.OwnerEmail, &m.OwnerName, &machine, &src, &m.Title, &m.TeamLevel, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.MachineID, m.SourcePath = machine.String, src.String
	return &m, nil
}

// metas returns the records of the slugs asked for.
func (s *Service) metas(ctx context.Context, slugs []string) (map[string]meta, error) {
	out := map[string]meta{}
	if len(slugs) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(slugs)), ",")
	args := make([]any, len(slugs))
	for i, v := range slugs {
		args[i] = v
	}
	rows, err := s.db.S().Q().Query(ctx, `SELECT slug, owner_email, owner_name, machine_id, source_path, title, team_level, created_at, updated_at
		FROM doc_meta WHERE slug IN (`+marks+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m meta
		var machine, src sql.NullString
		if err := rows.Scan(&m.Slug, &m.OwnerEmail, &m.OwnerName, &machine, &src, &m.Title, &m.TeamLevel, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.MachineID, m.SourcePath = machine.String, src.String
		out[m.Slug] = m
	}
	return out, rows.Err()
}

// putMeta records a document the first time it is published; a later publish keeps the owner and the level.
func (s *Service) putMeta(ctx context.Context, m meta) error {
	now := s.now().UnixMilli()
	_, err := s.db.S().Q().Exec(ctx, `INSERT INTO doc_meta (slug, owner_email, owner_name, machine_id, source_path, title, team_level, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (slug) DO UPDATE SET title = excluded.title, updated_at = excluded.updated_at,
			machine_id = COALESCE(excluded.machine_id, doc_meta.machine_id), source_path = COALESCE(excluded.source_path, doc_meta.source_path)`,
		m.Slug, m.OwnerEmail, m.OwnerName, nullIf(m.MachineID), nullIf(m.SourcePath), m.Title, m.TeamLevel, now, now)
	return err
}

func (s *Service) setLevel(ctx context.Context, slug, level string) error {
	_, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET team_level = ?, updated_at = ? WHERE slug = ?`, level, s.now().UnixMilli(), slug)
	return err
}

func nullIf(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// threads returns the threads of a document, oldest first, with their comments.
func (s *Service) threads(ctx context.Context, slug string) ([]thread, error) {
	q := s.db.S().Q()
	rows, err := q.Query(ctx, `SELECT id, anchor, status, author_kind, author_email, author_name, created_at, resolved_at
		FROM doc_threads WHERE slug = ? ORDER BY created_at, id`, slug)
	if err != nil {
		return nil, err
	}
	out := []thread{}
	byID := map[string]int{}
	for rows.Next() {
		t := thread{Slug: slug, Comments: []comment{}}
		var anchor string
		var created int64
		var resolved sql.NullInt64
		if err := rows.Scan(&t.ID, &anchor, &t.Status, &t.CreatedBy.Kind, &t.CreatedBy.Email, &t.CreatedBy.Name, &created, &resolved); err != nil {
			_ = rows.Close()
			return nil, err
		}
		t.Anchor = json.RawMessage(anchor)
		t.CreatedAt, t.ResolvedAt = ts(created), tsPtr(resolved)
		byID[t.ID] = len(out)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	cr, err := q.Query(ctx, `SELECT c.id, c.thread_id, c.author_kind, c.author_email, c.author_name, c.body, c.created_at
		FROM doc_comments c JOIN doc_threads t ON t.id = c.thread_id WHERE t.slug = ? ORDER BY c.created_at, c.id`, slug)
	if err != nil {
		return nil, err
	}
	defer cr.Close()
	for cr.Next() {
		var c comment
		var created int64
		if err := cr.Scan(&c.ID, &c.ThreadID, &c.Author.Kind, &c.Author.Email, &c.Author.Name, &c.Body, &created); err != nil {
			return nil, err
		}
		c.CreatedAt = ts(created)
		if i, ok := byID[c.ThreadID]; ok {
			out[i].Comments = append(out[i].Comments, c)
		}
	}
	return out, cr.Err()
}

func (s *Service) thread(ctx context.Context, slug, id string) (*thread, error) {
	ts, err := s.threads(ctx, slug)
	if err != nil {
		return nil, err
	}
	for i := range ts {
		if ts[i].ID == id {
			return &ts[i], nil
		}
	}
	return nil, nil
}

func (s *Service) addThread(ctx context.Context, slug string, anchor json.RawMessage, a author, body string) (string, error) {
	id := store.NewID()
	now := s.now().UnixMilli()
	if _, err := s.db.S().Q().Exec(ctx, `INSERT INTO doc_threads (id, slug, anchor, status, author_kind, author_email, author_name, created_at)
		VALUES (?, ?, ?, 'open', ?, ?, ?, ?)`, id, slug, string(anchor), a.Kind, a.Email, a.Name, now); err != nil {
		return "", err
	}
	return id, s.addComment(ctx, id, a, body)
}

func (s *Service) addComment(ctx context.Context, threadID string, a author, body string) error {
	_, err := s.db.S().Q().Exec(ctx, `INSERT INTO doc_comments (id, thread_id, author_kind, author_email, author_name, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, store.NewID(), threadID, a.Kind, a.Email, a.Name, body, s.now().UnixMilli())
	return err
}

func (s *Service) setStatus(ctx context.Context, threadID, status string) error {
	var resolved any
	if status == "resolved" {
		resolved = s.now().UnixMilli()
	}
	_, err := s.db.S().Q().Exec(ctx, `UPDATE doc_threads SET status = ?, resolved_at = ? WHERE id = ?`, status, resolved, threadID)
	return err
}

// counts returns, for each slug, the open threads and the comments the person has not read (their own do not count).
func (s *Service) counts(ctx context.Context, email string, slugs []string) (open, unread map[string]int, last map[string]int64, err error) {
	open, unread, last = map[string]int{}, map[string]int{}, map[string]int64{}
	if len(slugs) == 0 {
		return
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(slugs)), ",")
	args := make([]any, 0, len(slugs))
	for _, v := range slugs {
		args = append(args, v)
	}
	q := s.db.S().Q()
	rows, err := q.Query(ctx, `SELECT slug, COUNT(*) FROM doc_threads WHERE status = 'open' AND slug IN (`+marks+`) GROUP BY slug`, args...)
	if err != nil {
		return
	}
	for rows.Next() {
		var slug string
		var n int
		if err = rows.Scan(&slug, &n); err != nil {
			_ = rows.Close()
			return
		}
		open[slug] = n
	}
	_ = rows.Close()
	uargs := append([]any{email}, args...)
	uargs = append(uargs, email)
	rows, err = q.Query(ctx, `SELECT t.slug, COUNT(*), MAX(c.created_at) FROM doc_comments c JOIN doc_threads t ON t.id = c.thread_id
		LEFT JOIN doc_reads r ON r.slug = t.slug AND r.email = ?
		WHERE t.slug IN (`+marks+`) AND c.author_email <> ? AND c.created_at > COALESCE(r.read_at, 0) GROUP BY t.slug`, uargs...)
	if err != nil {
		return
	}
	for rows.Next() {
		var slug string
		var n int
		var at int64
		if err = rows.Scan(&slug, &n, &at); err != nil {
			_ = rows.Close()
			return
		}
		unread[slug], last[slug] = n, at
	}
	_ = rows.Close()
	return
}

func (s *Service) markRead(ctx context.Context, slug, email string) error {
	_, err := s.db.S().Q().Exec(ctx, `INSERT INTO doc_reads (slug, email, read_at) VALUES (?, ?, ?)
		ON CONFLICT (slug, email) DO UPDATE SET read_at = excluded.read_at`, slug, email, s.now().UnixMilli())
	return err
}

type person struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Initial string `json:"initial"`
}

// people finds the people of the installation whose name or address starts with q.
func (s *Service) people(ctx context.Context, q string) ([]person, error) {
	like := strings.ToLower(strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(q))) + "%"
	rows, err := s.db.S().Q().Query(ctx, `SELECT email, name FROM users WHERE lower(email) LIKE ? ESCAPE '\' OR lower(name) LIKE ? ESCAPE '\'
		ORDER BY name, email LIMIT 8`, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []person{}
	for rows.Next() {
		var email, name string
		if err := rows.Scan(&email, &name); err != nil {
			return nil, err
		}
		label := name
		if label == "" {
			label = email
		}
		out = append(out, person{ID: email, Name: label, Initial: strings.ToUpper(string([]rune(label)[:1]))})
	}
	return out, rows.Err()
}
