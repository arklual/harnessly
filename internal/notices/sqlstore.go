package notices

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// DB is what the SQL store runs on: the hub's store.Q, or a *sql.DB through Wrap. Queries use ? placeholders.
type DB interface {
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) *sql.Row
}

// Wrap makes a *sql.DB a DB, writing ? as $1, $2… when pg is set (Postgres).
func Wrap(db *sql.DB, pg bool) DB { return wrapped{db: db, pg: pg} }

type wrapped struct {
	db *sql.DB
	pg bool
}

func rebind(q string) string {
	var b strings.Builder
	n := 0
	for _, c := range q {
		if c == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

func (w wrapped) q(s string) string {
	if w.pg {
		return rebind(s)
	}
	return s
}
func (w wrapped) Exec(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return w.db.ExecContext(ctx, w.q(q), a...)
}
func (w wrapped) Query(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	return w.db.QueryContext(ctx, w.q(q), a...)
}
func (w wrapped) QueryRow(ctx context.Context, q string, a ...any) *sql.Row {
	return w.db.QueryRowContext(ctx, w.q(q), a...)
}

// Schema is the tables, for a store that makes its own (the runtime's); the hub makes them with its migration. The
// text is valid for SQLite and Postgres alike.
const Schema = `
CREATE TABLE IF NOT EXISTS notice_items (
    person   TEXT NOT NULL,
    item_key TEXT NOT NULL,
    chat     TEXT NOT NULL DEFAULT '',
    item_at  BIGINT NOT NULL,
    doc      TEXT NOT NULL,
    read_at  BIGINT,
    PRIMARY KEY (person, item_key)
);
CREATE INDEX IF NOT EXISTS notice_items_person_at ON notice_items (person, item_at);
CREATE TABLE IF NOT EXISTS notice_settings (
    person     TEXT PRIMARY KEY,
    doc        TEXT NOT NULL,
    rev        BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
`

// EnsureSchema makes the tables when they are not there.
func EnsureSchema(ctx context.Context, db *sql.DB) error {
	for _, stmt := range strings.Split(Schema, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// SQL is the Store over SQLite or Postgres. A read is a row of notice_items with read_at set: an item that was read
// before it arrived (another device took it away first) is kept as a read row with an empty doc.
type SQL struct{ db DB }

func NewSQL(db DB) *SQL { return &SQL{db: db} }

func (s *SQL) Put(ctx context.Context, person string, it Item) (bool, error) {
	if person == "" || it.Key == "" || len(it.Key) > MaxKey || len(it.Doc) > MaxDoc || !json.Valid(it.Doc) {
		return false, ErrArgs
	}
	var have string
	err := s.db.QueryRow(ctx, `SELECT doc FROM notice_items WHERE person = ? AND item_key = ?`, person, it.Key).Scan(&have)
	switch {
	case err == nil && have != "":
		return false, nil
	case err == nil:
		// A read that came before the item: the item is kept, already read.
		_, err = s.db.Exec(ctx, `UPDATE notice_items SET doc = ?, chat = ?, item_at = ? WHERE person = ? AND item_key = ?`,
			string(it.Doc), it.Chat, it.At, person, it.Key)
		return false, err
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	res, err := s.db.Exec(ctx, `INSERT INTO notice_items (person, item_key, chat, item_at, doc) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (person, item_key) DO NOTHING`, person, it.Key, it.Chat, it.At, string(it.Doc))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *SQL) Items(ctx context.Context, person string, limit int) ([]Item, error) {
	rows, err := s.db.Query(ctx, `SELECT item_key, chat, item_at, doc FROM notice_items
		WHERE person = ? AND read_at IS NULL AND doc <> '' ORDER BY item_at DESC, item_key LIMIT ?`, person, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var doc string
		if err := rows.Scan(&it.Key, &it.Chat, &it.At, &doc); err != nil {
			return nil, err
		}
		it.Doc = json.RawMessage(doc)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *SQL) MarkRead(ctx context.Context, person string, keys []string, at int64) ([]string, error) {
	var marked []string
	for _, k := range keys {
		if k == "" || len(k) > MaxKey {
			continue
		}
		res, err := s.db.Exec(ctx, `UPDATE notice_items SET read_at = ? WHERE person = ? AND item_key = ? AND read_at IS NULL`, at, person, k)
		if err != nil {
			return marked, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			marked = append(marked, k)
			continue
		}
		// Not there yet (the device that took it away was faster than the one that made it): remember the read.
		res, err = s.db.Exec(ctx, `INSERT INTO notice_items (person, item_key, chat, item_at, doc, read_at) VALUES (?, ?, '', ?, '', ?)
			ON CONFLICT (person, item_key) DO NOTHING`, person, k, at, at)
		if err != nil {
			return marked, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			marked = append(marked, k)
		}
	}
	return marked, nil
}

func (s *SQL) readWhere(ctx context.Context, person, extra string, at int64, args ...any) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT item_key FROM notice_items WHERE person = ? AND read_at IS NULL AND doc <> ''`+extra,
		append([]any{person}, args...)...)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.MarkRead(ctx, person, keys, at)
}

func (s *SQL) ReadChat(ctx context.Context, person, chat string, at int64) ([]string, error) {
	return s.readWhere(ctx, person, ` AND chat = ?`, at, chat)
}

func (s *SQL) ReadAll(ctx context.Context, person string, at int64) ([]string, error) {
	return s.readWhere(ctx, person, ``, at)
}

func (s *SQL) Reads(ctx context.Context, person string, limit int) ([]Read, error) {
	rows, err := s.db.Query(ctx, `SELECT item_key, read_at FROM notice_items WHERE person = ? AND read_at IS NOT NULL
		ORDER BY read_at DESC, item_key LIMIT ?`, person, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Read{}
	for rows.Next() {
		var r Read
		if err := rows.Scan(&r.Key, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQL) Settings(ctx context.Context, person string) (Settings, error) {
	var st Settings
	var doc string
	err := s.db.QueryRow(ctx, `SELECT doc, rev FROM notice_settings WHERE person = ?`, person).Scan(&doc, &st.Rev)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{Doc: json.RawMessage("null")}, nil
	}
	st.Doc = json.RawMessage(doc)
	return st, err
}

func (s *SQL) SaveSettings(ctx context.Context, person string, doc json.RawMessage, fromRev int64, at int64) (Settings, error) {
	if person == "" || len(doc) > MaxDoc || !json.Valid(doc) {
		return Settings{}, ErrArgs
	}
	var res sql.Result
	var err error
	if fromRev == 0 {
		res, err = s.db.Exec(ctx, `INSERT INTO notice_settings (person, doc, rev, updated_at) VALUES (?, ?, 1, ?)
			ON CONFLICT (person) DO NOTHING`, person, string(doc), at)
	} else {
		res, err = s.db.Exec(ctx, `UPDATE notice_settings SET doc = ?, rev = ?, updated_at = ? WHERE person = ? AND rev = ?`,
			string(doc), fromRev+1, at, person, fromRev)
	}
	if err != nil {
		return Settings{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Settings{}, ErrConflict
	}
	return Settings{Doc: doc, Rev: fromRev + 1}, nil
}

func (s *SQL) Prune(ctx context.Context, before int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM notice_items WHERE read_at IS NOT NULL AND read_at < ?`, before)
	if err != nil {
		return err
	}
	// Unread items older than a month are no longer news either.
	_, err = s.db.Exec(ctx, `DELETE FROM notice_items WHERE read_at IS NULL AND item_at < ?`, before-30*24*3600*1000)
	return err
}
