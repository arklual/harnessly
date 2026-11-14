package searchindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// SQL is the index over a database with the table search_docs. Postgres is the hub's, SQLite the runtime's own.
type SQL struct {
	DB DB
	// Postgres turns on what only it has: the trigram indexes and the typo-tolerant second look (Ensure).
	Postgres bool

	trgm bool
}

// Table is the table of the runtime's own database (the hub has it from its migration 0068).
const Table = `CREATE TABLE IF NOT EXISTS search_docs (
    space_id   TEXT   NOT NULL,
    kind       TEXT   NOT NULL,
    doc_id     TEXT   NOT NULL,
    parent     TEXT   NOT NULL DEFAULT '',
    title      TEXT   NOT NULL DEFAULT '',
    body       TEXT   NOT NULL DEFAULT '',
    norm       TEXT   NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (space_id, kind, doc_id)
);
CREATE INDEX IF NOT EXISTS search_docs_parent ON search_docs (space_id, parent);`

// OpenSQLite opens the runtime's own index in a file (":memory:" for tests) and makes its table.
func OpenSQLite(ctx context.Context, path string) (*SQL, *sql.DB, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1) // one writer, and ":memory:" is one connection's
	if _, err := db.ExecContext(ctx, Table); err != nil {
		db.Close()
		return nil, nil, err
	}
	return &SQL{DB: dbQ{db}}, db, nil
}

// dbQ gives a sql.DB the context-first methods of DB.
type dbQ struct{ *sql.DB }

func (d dbQ) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, q, args...)
}
func (d dbQ) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, q, args...)
}

// FromSQL is the index over an opened *sql.DB whose table exists.
func FromSQL(db *sql.DB, postgres bool) *SQL { return &SQL{DB: dbQ{db}, Postgres: postgres} }

// Ensure makes what Postgres needs beyond the migration: the pg_trgm extension and a trigram index on the normalized
// text, which serve the substring and prefix questions and the look for a mistyped word. It never fails the hub: a
// database that does not let the role create the extension still answers, by a scan of the space's rows.
func (s *SQL) Ensure(ctx context.Context) error {
	if !s.Postgres {
		return nil
	}
	if _, err := s.DB.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
		return fmt.Errorf("pg_trgm: %w", err)
	}
	if _, err := s.DB.Exec(ctx, `CREATE INDEX IF NOT EXISTS search_docs_norm_trgm ON search_docs USING gin (norm gin_trgm_ops)`); err != nil {
		return fmt.Errorf("trigram index: %w", err)
	}
	if _, err := s.DB.Exec(ctx, `CREATE INDEX IF NOT EXISTS search_docs_title_trgm ON search_docs USING gin (lower(title) gin_trgm_ops)`); err != nil {
		return fmt.Errorf("title index: %w", err)
	}
	s.trgm = true
	return nil
}

const upsert = `INSERT INTO search_docs (space_id, kind, doc_id, parent, title, body, norm, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (space_id, kind, doc_id) DO UPDATE SET parent = excluded.parent, title = excluded.title, body = excluded.body,
	norm = excluded.norm, updated_at = excluded.updated_at`

func (s *SQL) put(ctx context.Context, d Doc) error {
	if d.Kind == "" || d.ID == "" {
		return nil
	}
	title, body := clip(d.Title, maxTitle), clip(d.Body, maxBody)
	norm := Normalize(title + " " + d.ID + " " + body)
	_, err := s.DB.Exec(ctx, upsert, d.Space, d.Kind, d.ID, d.Parent, title, body, norm, d.UpdatedAt)
	return err
}

// Put implements Index.
func (s *SQL) Put(ctx context.Context, docs ...Doc) error {
	for _, d := range docs {
		if err := s.put(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

// Remove implements Index.
func (s *SQL) Remove(ctx context.Context, space, kind, id string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM search_docs WHERE space_id = ? AND kind = ? AND doc_id = ?`, space, kind, id)
	return err
}

// Replace implements Index.
func (s *SQL) Replace(ctx context.Context, space, kind, parent string, docs []Doc) error {
	keep := map[string]bool{}
	for _, d := range docs {
		d.Space, d.Kind = space, kind
		if parent != "" {
			d.Parent = parent
		}
		keep[d.ID] = true
		if err := s.put(ctx, d); err != nil {
			return err
		}
	}
	q, args := `SELECT doc_id FROM search_docs WHERE space_id = ? AND kind = ?`, []any{space, kind}
	if parent != "" {
		q, args = q+` AND parent = ?`, append(args, parent)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range gone {
		if err := s.Remove(ctx, space, kind, id); err != nil {
			return err
		}
	}
	return nil
}

// Count implements Index.
func (s *SQL) Count(ctx context.Context, space string) (int, error) {
	rows, err := s.DB.Query(ctx, `SELECT COUNT(*) FROM search_docs WHERE space_id = ?`, space)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	if rows.Next() {
		err = rows.Scan(&n)
	}
	return n, err
}

// like escapes a word for LIKE ... ESCAPE '\'.
func like(w string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(w) + "%"
}

const pageRows = 200

// Search implements Index: every word must be somewhere in the title, the id or the body; a title that holds the word
// counts most, one that starts with it more, a newer document wins a tie.
func (s *SQL) Search(ctx context.Context, q Query) ([]Hit, error) {
	words := Words(q.Text)
	if len(words) == 0 {
		return nil, ErrEmpty
	}
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	hits, err := s.run(ctx, q, words, limit, false)
	if err == nil && len(hits) == 0 && s.Postgres && s.trgm {
		// A mistyped word: the title is looked at by its trigrams.
		if typo, terr := s.run(ctx, q, words, limit, true); terr == nil {
			hits = typo
		}
	}
	return hits, err
}

func (s *SQL) run(ctx context.Context, q Query, words []string, limit int, fuzzy bool) ([]Hit, error) {
	var where, score []string
	args := []any{}
	scoreArgs := []any{}
	where = append(where, "space_id = ?")
	args = append(args, q.Space)
	if len(q.Kinds) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(q.Kinds)), ",")
		where = append(where, "kind IN ("+marks+")")
		for _, k := range q.Kinds {
			args = append(args, k)
		}
	}
	for _, w := range words {
		if fuzzy {
			where = append(where, "(word_similarity(?, lower(title)) > 0.4 OR norm LIKE ? ESCAPE '\\')")
			args = append(args, w, like(w))
		} else {
			where = append(where, `norm LIKE ? ESCAPE '\'`)
			args = append(args, like(w))
		}
		score = append(score, `(CASE WHEN lower(title) LIKE ? ESCAPE '\' THEN 2 ELSE 0 END)`,
			`(CASE WHEN lower(title) LIKE ? ESCAPE '\' THEN 3 ELSE 0 END)`) // holds the word; starts with it
		scoreArgs = append(scoreArgs, like(w), strings.TrimPrefix(like(w), "%"))
	}
	var out []Hit
	for off := 0; off < 10*pageRows && len(out) < limit; off += pageRows {
		sqlText := `SELECT kind, doc_id, parent, title, updated_at FROM search_docs WHERE ` + strings.Join(where, " AND ") +
			` ORDER BY (` + strings.Join(score, " + ") + `) DESC, updated_at DESC, doc_id LIMIT ? OFFSET ?`
		// The placeholders run in the order of the text: the score's come first in ORDER BY only after WHERE's.
		all := append(append(append([]any{}, args...), scoreArgs...), pageRows, off)
		rows, err := s.DB.Query(ctx, sqlText, all...)
		if err != nil {
			return nil, err
		}
		n := 0
		for rows.Next() {
			var h Hit
			h.Space = q.Space
			if err := rows.Scan(&h.Kind, &h.ID, &h.Parent, &h.Title, &h.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			n++
			if q.Allow == nil || q.Allow(h) {
				out = append(out, h)
				if len(out) >= limit {
					break
				}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if n < pageRows {
			break
		}
	}
	return out, nil
}
