package docs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
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
	ID        string `json:"id"`
	ThreadID  string `json:"threadId"`
	Author    author `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	// DeliveredAt is when a person's comment reached the conversation the document was published from; DeliveryError
	// why it never will.
	DeliveredAt   *string `json:"deliveredAt"`
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
	Slug string
	// SpaceID is the space the document belongs to; "" for a document no space is known for.
	SpaceID    string
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

// reach is whose documents a query returns: every query of doc_meta carries its condition on the document's space. A
// document no space is known for is within everybody's reach here; who may open it is decided from its owner's
// organizations (see circle).
type reach struct {
	cond string
	args []any
}

// reachAll is the hub itself and the administrators of the installation.
func reachAll() reach { return reach{cond: "1 = 1"} }

// reachSpace is exactly one space ("" for the documents no space is known for): what a guest link opens.
func reachSpace(id string) reach { return reach{cond: "COALESCE(space_id, '') = ?", args: []any{id}} }

// reachMachine is what a machine of a space may publish over.
func reachMachine(spaceID string) reach {
	return reach{cond: "(space_id IS NULL OR space_id = ?)", args: []any{spaceID}}
}

// reachPerson is the spaces of the person's organizations, and beyond them only the documents that name the person:
// their own, and those shared with them (or with an address they have proved) or with a team of theirs.
func reachPerson(userID, email string, proved []string) reach {
	email = strings.ToLower(strings.TrimSpace(email))
	named, addrs := "g.user_id = ?", []any{userID}
	if len(proved) > 0 {
		named = "(g.user_id = ? OR (g.user_id IS NULL AND g.email IN (" + strings.TrimSuffix(strings.Repeat("?,", len(proved)), ",") + ")))"
		for _, v := range proved {
			addrs = append(addrs, v)
		}
	}
	args := append([]any{userID, userID, email}, addrs...)
	return reach{cond: `(space_id IS NULL
		OR space_id IN (SELECT s.id FROM spaces s WHERE s.id IN (SELECT space_id FROM memberships WHERE user_id = ?)
			OR s.org_id IN (SELECT s2.org_id FROM spaces s2 JOIN memberships m2 ON m2.space_id = s2.id WHERE m2.user_id = ? AND s2.org_id <> ''))
		OR lower(owner_email) = ?
		OR EXISTS (SELECT 1 FROM resource_access_people g WHERE g.kind = 'doc' AND g.id = doc_meta.slug
			AND COALESCE(g.space_id, '') = COALESCE(doc_meta.space_id, '') AND ` + named + `)
		OR EXISTS (SELECT 1 FROM resource_access_teams g JOIN team_members tm ON tm.team_id = g.team_id WHERE g.kind = 'doc'
			AND g.id = doc_meta.slug AND COALESCE(g.space_id, '') = COALESCE(doc_meta.space_id, '') AND tm.user_id = ?))`,
		args: append(args, userID)}
}

func (s *Service) reachOf(ctx context.Context, p *auth.Principal) reach {
	if p.Admin {
		return reachAll()
	}
	proved, err := s.db.S().VerifiedEmails(ctx, p.User.ID)
	if err != nil {
		s.log.Warn("addresses of a person not read", "error", err)
	}
	return reachPerson(p.User.ID, p.User.Email, proved)
}

const metaCols = `slug, COALESCE(space_id, ''), owner_email, owner_name, machine_id, source_path, title, team_level, created_at, updated_at`

func scanMeta(row interface{ Scan(...any) error }) (meta, error) {
	var m meta
	var machine, src sql.NullString
	err := row.Scan(&m.Slug, &m.SpaceID, &m.OwnerEmail, &m.OwnerName, &machine, &src, &m.Title, &m.TeamLevel, &m.CreatedAt, &m.UpdatedAt)
	m.MachineID, m.SourcePath = machine.String, src.String
	return m, err
}

// meta is the record of a document within reach. elsewhere says the hub has a record of it in a space out of reach:
// such a document is closed, not unrecorded.
func (s *Service) meta(ctx context.Context, slug string, r reach) (m *meta, elsewhere bool, err error) {
	q := s.db.S().Q()
	got, err := scanMeta(q.QueryRow(ctx, `SELECT `+metaCols+` FROM doc_meta WHERE slug = ? AND `+r.cond, append([]any{slug}, r.args...)...))
	if err == nil {
		return &got, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	var n int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM doc_meta WHERE slug = ? AND NOT `+r.cond, append([]any{slug}, r.args...)...).Scan(&n); err != nil {
		return nil, false, err
	}
	return nil, n > 0, nil
}

// metas returns the records of the slugs asked for that are within reach, and which of the others the hub has a
// record of in a space out of reach.
func (s *Service) metas(ctx context.Context, slugs []string, r reach) (map[string]meta, map[string]bool, error) {
	out, elsewhere := map[string]meta{}, map[string]bool{}
	if len(slugs) == 0 {
		return out, elsewhere, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(slugs)), ",")
	args := make([]any, 0, len(slugs)+len(r.args))
	for _, v := range slugs {
		args = append(args, v)
	}
	args = append(args, r.args...)
	q := s.db.S().Q()
	rows, err := q.Query(ctx, `SELECT `+metaCols+` FROM doc_meta WHERE slug IN (`+marks+`) AND `+r.cond, args...)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		m, err := scanMeta(rows)
		if err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		out[m.Slug] = m
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	_ = rows.Close()
	rows, err = q.Query(ctx, `SELECT slug FROM doc_meta WHERE slug IN (`+marks+`) AND NOT `+r.cond, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, nil, err
		}
		elsewhere[slug] = true
	}
	return out, elsewhere, rows.Err()
}

// putMeta records a document the first time it is published, in the space of the machine that published it; a later
// publish keeps the owner and the level. A record no space was known for takes the space of the machine its owner
// publishes it from again, and its access rows go with it.
func (s *Service) putMeta(ctx context.Context, m meta) error {
	now := s.now().UnixMilli()
	return s.db.Tx(ctx, func(st store.Store) error {
		q := st.Q()
		if _, err := q.Exec(ctx, `INSERT INTO doc_meta (slug, space_id, owner_email, owner_name, machine_id, source_path, title, team_level, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (slug) DO UPDATE SET title = excluded.title, updated_at = excluded.updated_at,
				space_id = COALESCE(doc_meta.space_id, excluded.space_id),
				machine_id = COALESCE(excluded.machine_id, doc_meta.machine_id), source_path = COALESCE(excluded.source_path, doc_meta.source_path)
			WHERE doc_meta.space_id IS NULL OR excluded.space_id IS NULL OR doc_meta.space_id = excluded.space_id`,
			m.Slug, nullIf(m.SpaceID), m.OwnerEmail, m.OwnerName, nullIf(m.MachineID), nullIf(m.SourcePath), m.Title, m.TeamLevel, now, now); err != nil {
			return err
		}
		if m.SpaceID == "" {
			return nil
		}
		return resaccess.AdoptSpace(ctx, q, resaccess.Ref{Kind: resaccess.KindDoc, ID: m.Slug, Space: m.SpaceID})
	})
}

func (s *Service) setLevel(ctx context.Context, m *meta, level string) error {
	_, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET team_level = ?, updated_at = ? WHERE slug = ? AND COALESCE(space_id, '') = ?`,
		level, s.now().UnixMilli(), m.Slug, m.SpaceID)
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
	cr, err := q.Query(ctx, `SELECT c.id, c.thread_id, c.author_kind, c.author_email, c.author_name, c.body, c.created_at, c.delivered_at, c.delivery_error
		FROM doc_comments c JOIN doc_threads t ON t.id = c.thread_id WHERE t.slug = ? ORDER BY c.created_at, c.id`, slug)
	if err != nil {
		return nil, err
	}
	defer cr.Close()
	for cr.Next() {
		var c comment
		var created int64
		var delivered sql.NullInt64
		var deliveryErr sql.NullString
		if err := cr.Scan(&c.ID, &c.ThreadID, &c.Author.Kind, &c.Author.Email, &c.Author.Name, &c.Body, &created, &delivered, &deliveryErr); err != nil {
			return nil, err
		}
		c.CreatedAt, c.DeliveredAt = ts(created), tsPtr(delivered)
		if deliveryErr.Valid {
			c.DeliveryError = &deliveryErr.String
		}
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

// addThread opens a thread with its first comment and returns the ids of both.
func (s *Service) addThread(ctx context.Context, slug string, anchor json.RawMessage, a author, body string) (string, string, error) {
	id := store.NewID()
	now := s.now().UnixMilli()
	if _, err := s.db.S().Q().Exec(ctx, `INSERT INTO doc_threads (id, slug, anchor, status, author_kind, author_email, author_name, created_at)
		VALUES (?, ?, ?, 'open', ?, ?, ?, ?)`, id, slug, string(anchor), a.Kind, a.Email, a.Name, now); err != nil {
		return "", "", err
	}
	cid, err := s.addComment(ctx, id, a, body)
	return id, cid, err
}

func (s *Service) addComment(ctx context.Context, threadID string, a author, body string) (string, error) {
	id := store.NewID()
	_, err := s.db.S().Q().Exec(ctx, `INSERT INTO doc_comments (id, thread_id, author_kind, author_email, author_name, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, threadID, a.Kind, a.Email, a.Name, body, s.now().UnixMilli())
	return id, err
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

// A search of people needs two characters at least, and a person makes so many a minute.
const (
	peopleMinQuery  = 2
	peoplePerMinute = 120
)

// people finds, among the people the person shares a space with, those whose name or address starts with q. An
// administrator of the installation finds anybody.
func (s *Service) people(ctx context.Context, p *auth.Principal, q string) ([]person, error) {
	out := []person{}
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) < peopleMinQuery {
		return out, nil
	}
	like := strings.ToLower(strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)) + "%"
	query := `SELECT email, name FROM users WHERE (lower(email) LIKE ? ESCAPE '\' OR lower(name) LIKE ? ESCAPE '\')`
	args := []any{like, like}
	if !p.Admin {
		query += ` AND id IN (SELECT theirs.user_id FROM memberships mine JOIN memberships theirs ON theirs.space_id = mine.space_id WHERE mine.user_id = ?)`
		args = append(args, p.User.ID)
	}
	rows, err := s.db.S().Q().Query(ctx, query+` ORDER BY name, email LIMIT 8`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
