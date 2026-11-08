package filestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The two SQL drivers share one set of queries over database/sql. A dialect only changes how placeholders are
// written and how the room is locked.
type dialect struct {
	pg bool
}

// rebind writes ? placeholders as $1, $2… for Postgres.
func (d dialect) rebind(q string) string {
	if !d.pg {
		return q
	}
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

// lockRoom takes the room's write lock for the rest of the transaction and returns the revision counter.
// Postgres: the counter row is locked FOR UPDATE, so writers of one room queue up across all replicas and a
// reader never sees revision n+1 before n. SQLite: a no-op write that takes the database write lock at once
// (a deferred transaction that read first and wrote later could fail with a stale snapshot).
func (d dialect) lockRoom(ctx context.Context, tx *sql.Tx) (int64, error) {
	var v int64
	if d.pg {
		err := tx.QueryRowContext(ctx, `SELECT v FROM counters WHERE k = 'rev' FOR UPDATE`).Scan(&v)
		return v, err
	}
	err := tx.QueryRowContext(ctx, `UPDATE counters SET v = v WHERE k = 'rev' RETURNING v`).Scan(&v)
	return v, err
}

const roomSchema = `
CREATE TABLE IF NOT EXISTS history (
  rev     BIGINT  PRIMARY KEY,
  path    TEXT    NOT NULL,
  sha     TEXT    NOT NULL DEFAULT '',
  tag     TEXT    NOT NULL DEFAULT '',
  size    BIGINT  NOT NULL DEFAULT 0,
  exec    INTEGER NOT NULL DEFAULT 0,
  deleted INTEGER NOT NULL DEFAULT 0,
  author  TEXT    NOT NULL DEFAULT '',
  machine TEXT    NOT NULL DEFAULT '',
  at      BIGINT  NOT NULL
);
CREATE TABLE IF NOT EXISTS files (
  path TEXT PRIMARY KEY,
  rev  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS files_rev ON files(rev);
CREATE INDEX IF NOT EXISTS history_path ON history(path, rev);
CREATE TABLE IF NOT EXISTS members (
  name      TEXT PRIMARY KEY,
  recipient TEXT NOT NULL,
  at        BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS room_keys (
  member  TEXT PRIMARY KEY,
  wrapped TEXT NOT NULL,
  by      TEXT NOT NULL,
  at      BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS leases (
  key        TEXT PRIMARY KEY,
  machine    TEXT NOT NULL,
  chat       TEXT NOT NULL,
  usr        TEXT NOT NULL DEFAULT '',
  chat_title TEXT NOT NULL DEFAULT '',
  turn       TEXT NOT NULL DEFAULT '',
  since      BIGINT NOT NULL,
  beat       BIGINT NOT NULL,
  edit       BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS counters (
  k TEXT PRIMARY KEY,
  v BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS gc_marks (
  sha   TEXT PRIMARY KEY,
  since BIGINT NOT NULL
);
INSERT INTO counters (k, v) SELECT 'rev', COALESCE(MAX(rev), 0) FROM history WHERE true ON CONFLICT (k) DO NOTHING;
`

// sqlRoom is one room's metadata over a database whose tables (unqualified) are this room's alone: a SQLite file of
// its own, or a Postgres schema of its own.
type sqlRoom struct {
	db *sql.DB
	d  dialect
	n  *notifier
	// A single SQLite connection serializes the statements, but a read-then-write decision spans several: one
	// writer at a time in this process. (Postgres needs no such thing; the database's row lock is the guard.)
	mu chanLock
	// afterCommit runs after a write transaction commits (Postgres: nothing; the NOTIFY went with the transaction).
	notifyPG func(ctx context.Context, tx *sql.Tx) error
}

// chanLock is a mutex that can be skipped (Postgres) and that gives up when the context ends.
type chanLock struct {
	on bool
	ch chan struct{}
}

func newChanLock(on bool) chanLock { return chanLock{on: on, ch: make(chan struct{}, 1)} }

func (l chanLock) lock(ctx context.Context) error {
	if !l.on {
		return nil
	}
	select {
	case l.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l chanLock) unlock() {
	if l.on {
		<-l.ch
	}
}

func (s *sqlRoom) q(query string) string { return s.d.rebind(query) }

func scanEntry(row interface{ Scan(...any) error }) (*Entry, error) {
	var e Entry
	var exec, deleted int
	if err := row.Scan(&e.Rev, &e.Path, &e.Sha, &e.Tag, &e.Size, &exec, &deleted, &e.Author, &e.Machine, &e.At); err != nil {
		return nil, err
	}
	e.Exec, e.Deleted = exec != 0, deleted != 0
	return &e, nil
}

const entryColumns = `h.rev, h.path, h.sha, h.tag, h.size, h.exec, h.deleted, h.author, h.machine, h.at`

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *sqlRoom) current(ctx context.Context, x queryer, path string) (*Entry, error) {
	e, err := scanEntry(x.QueryRowContext(ctx, s.q(`SELECT `+entryColumns+` FROM files f JOIN history h ON h.rev = f.rev WHERE f.path = ?`), path))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

func (s *sqlRoom) Wake() <-chan struct{} { return s.n.Wake() }

func (s *sqlRoom) Current(ctx context.Context, path string) (*Entry, error) {
	return s.current(ctx, s.db, path)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *sqlRoom) Apply(ctx context.Context, req CommitRequest, machine string, now int64, verify func(context.Context) error) (*Entry, bool, error) {
	if err := s.mu.lock(ctx); err != nil {
		return nil, false, err
	}
	defer s.mu.unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	counter, err := s.d.lockRoom(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if verify != nil && !req.Deleted {
		if err := verify(ctx); err != nil {
			return nil, false, err
		}
	}
	cur, err := s.current(ctx, tx, req.Path)
	if err != nil {
		return nil, false, err
	}
	outcome, answer := decide(cur, req)
	switch outcome {
	case commitConflict:
		return answer, true, nil
	case commitNoop:
		return answer, false, nil
	}
	// The counter can trail the history only when an older binary wrote to this database: never hand out a taken rev.
	var top int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(rev), 0) FROM history`).Scan(&top); err != nil {
		return nil, false, err
	}
	e := newEntry(req, machine, now)
	e.Rev = max(counter, top) + 1
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO history (rev, path, sha, tag, size, exec, deleted, author, machine, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		e.Rev, e.Path, e.Sha, e.Tag, e.Size, b2i(e.Exec), b2i(e.Deleted), e.Author, e.Machine, e.At); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO files (path, rev) VALUES (?, ?) ON CONFLICT (path) DO UPDATE SET rev = excluded.rev`), e.Path, e.Rev); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE counters SET v = ? WHERE k = 'rev'`), e.Rev); err != nil {
		return nil, false, err
	}
	if s.notifyPG != nil {
		if err := s.notifyPG(ctx, tx); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	s.n.Broadcast()
	return &e, false, nil
}

func (s *sqlRoom) Changes(ctx context.Context, since int64, limit int) (Changes, error) {
	out := Changes{Entries: []Entry{}}
	// One snapshot for the entries and for the cursor: the cursor jumps to the newest revision, and a write landing
	// between the two reads would otherwise be skipped for good. (SQLite: the same lock the writers take.)
	if err := s.mu.lock(ctx); err != nil {
		return out, err
	}
	defer s.mu.unlock()
	opts := &sql.TxOptions{ReadOnly: true}
	if s.d.pg {
		opts.Isolation = sql.LevelRepeatableRead
	}
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, s.q(`SELECT `+entryColumns+` FROM files f JOIN history h ON h.rev = f.rev WHERE f.rev > ? ORDER BY f.rev LIMIT ?`), since, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return out, err
		}
		if len(out.Entries) == limit {
			out.More = true
			break
		}
		out.Entries = append(out.Entries, *e)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	if n := len(out.Entries); n > 0 {
		out.Rev = out.Entries[n-1].Rev
	}
	if !out.More {
		// The cursor moves to the newest revision even when it was superseded: a file written twice is one entry.
		var max sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(rev) FROM history`).Scan(&max); err != nil {
			return out, err
		}
		// Below `since` only when the hub lost revisions (restored from an older backup): the agent sees its cursor
		// go back and asks for everything again.
		if max.Int64 > out.Rev {
			out.Rev = max.Int64
		}
	}
	return out, nil
}

func (s *sqlRoom) History(ctx context.Context, path string, limit int) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+entryColumns+` FROM history h WHERE h.path = ? ORDER BY h.rev DESC LIMIT ?`), path, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// inRoomTx runs fn in a transaction that holds the room's write lock.
func (s *sqlRoom) inRoomTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if err := s.mu.lock(ctx); err != nil {
		return err
	}
	defer s.mu.unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := s.d.lockRoom(ctx, tx); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlRoom) RegisterMember(ctx context.Context, name, recipient string, now int64) error {
	return s.inRoomTx(ctx, func(tx *sql.Tx) error {
		var old string
		err := tx.QueryRowContext(ctx, s.q(`SELECT recipient FROM members WHERE name = ?`), name).Scan(&old)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && old == recipient {
			return nil
		}
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO members (name, recipient, at) VALUES (?, ?, ?) ON CONFLICT (name) DO UPDATE SET recipient = excluded.recipient, at = excluded.at`),
			name, recipient, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, s.q(`DELETE FROM room_keys WHERE member = ?`), name)
		return err
	})
}

func (s *sqlRoom) Members(ctx context.Context) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.name, m.recipient, m.at, k.member IS NOT NULL FROM members m LEFT JOIN room_keys k ON k.member = m.name ORDER BY m.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.Name, &m.Recipient, &m.At, &m.HasKey); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqlRoom) PutKey(ctx context.Context, member, wrapped, by string, now int64) error {
	return s.inRoomTx(ctx, func(tx *sql.Tx) error {
		var keys, byHas, isMember int
		if err := tx.QueryRowContext(ctx, s.q(`SELECT COUNT(*), COUNT(*) FILTER (WHERE member = ?) FROM room_keys`), by).Scan(&keys, &byHas); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM members WHERE name = ?`), member).Scan(&isMember); err != nil {
			return err
		}
		switch {
		case isMember == 0:
			return fmt.Errorf("%w: %s is not a member", ErrForbidden, member)
		case keys == 0 && member == by:
		case byHas == 1:
		default:
			return fmt.Errorf("%w: only a member holding the key can wrap it", ErrForbidden)
		}
		_, err := tx.ExecContext(ctx, s.q(`INSERT INTO room_keys (member, wrapped, by, at) VALUES (?, ?, ?, ?) ON CONFLICT (member) DO UPDATE SET wrapped = excluded.wrapped, by = excluded.by, at = excluded.at`),
			member, wrapped, by, now)
		return err
	})
}

func (s *sqlRoom) Key(ctx context.Context, member string) (string, error) {
	var w string
	err := s.db.QueryRowContext(ctx, s.q(`SELECT wrapped FROM room_keys WHERE member = ?`), member).Scan(&w)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return w, err
}

func (s *sqlRoom) Approval(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'approve'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "auto", nil
	}
	return v, err
}

func (s *sqlRoom) RequireManualApproval(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('approve', 'manual') ON CONFLICT (key) DO UPDATE SET value = 'manual'`)
	return err
}

// --- retention and garbage collection ---

// Prune forgets old revisions of every path: a revision goes when it is not the path's current one, is not among
// the `keep` newest of its path (keep=0: no such protection), and is older than `before` (unix ms; 0: no such
// protection). Both zero prunes nothing. Returns the number of revisions removed.
func (s *sqlRoom) Prune(ctx context.Context, keep int, before int64) (int64, error) {
	if keep <= 0 && before <= 0 {
		return 0, nil
	}
	cond, args := "r.rev NOT IN (SELECT rev FROM files)", []any{}
	if keep > 0 {
		cond += " AND r.rn > ?"
		args = append(args, keep)
	}
	if before > 0 {
		cond += " AND r.at < ?"
		args = append(args, before)
	}
	var n int64
	err := s.inRoomTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, s.q(`DELETE FROM history WHERE rev IN (
  SELECT r.rev FROM (SELECT rev, at, ROW_NUMBER() OVER (PARTITION BY path ORDER BY rev DESC) AS rn FROM history) r WHERE `+cond+`)`), args...)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}

func (s *sqlRoom) ReferencedShas(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT sha FROM history WHERE sha <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		out[sha] = struct{}{}
	}
	return out, rows.Err()
}

func (s *sqlRoom) Referenced(ctx context.Context, sha string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, s.q(`SELECT 1 FROM history WHERE sha = ? LIMIT 1`), sha).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *sqlRoom) DeleteUnreferenced(ctx context.Context, sha string, del func(context.Context) error) (bool, error) {
	deleted := false
	err := s.inRoomTx(ctx, func(tx *sql.Tx) error {
		var one int
		err := tx.QueryRowContext(ctx, s.q(`SELECT 1 FROM history WHERE sha = ? LIMIT 1`), sha).Scan(&one)
		if err == nil {
			return nil // referenced: keep
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := del(ctx); err != nil {
			return err
		}
		deleted = true
		return nil
	})
	return deleted, err
}

func (s *sqlRoom) GCMarks(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sha, since FROM gc_marks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var sha string
		var since int64
		if err := rows.Scan(&sha, &since); err != nil {
			return nil, err
		}
		out[sha] = since
	}
	return out, rows.Err()
}

func (s *sqlRoom) PutGCMarks(ctx context.Context, add map[string]int64, drop []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for sha, since := range add {
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO gc_marks (sha, since) VALUES (?, ?) ON CONFLICT (sha) DO NOTHING`), sha, since); err != nil {
			return err
		}
	}
	for _, sha := range drop {
		if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM gc_marks WHERE sha = ?`), sha); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- copying a room (migrate) ---

func (s *sqlRoom) ExportHistory(ctx context.Context, after int64, limit int) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+entryColumns+` FROM history h WHERE h.rev > ? ORDER BY h.rev LIMIT ?`), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *sqlRoom) ImportHistory(ctx context.Context, es []Entry) error {
	return s.inRoomTx(ctx, func(tx *sql.Tx) error {
		var top int64
		for _, e := range es {
			if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO history (rev, path, sha, tag, size, exec, deleted, author, machine, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (rev) DO NOTHING`),
				e.Rev, e.Path, e.Sha, e.Tag, e.Size, b2i(e.Exec), b2i(e.Deleted), e.Author, e.Machine, e.At); err != nil {
				return err
			}
			top = max(top, e.Rev)
		}
		_, err := tx.ExecContext(ctx, s.q(`UPDATE counters SET v = ? WHERE k = 'rev' AND v < ?`), top, top)
		return err
	})
}

func (s *sqlRoom) ExportState(ctx context.Context) (RoomState, error) {
	st := RoomState{Files: map[string]int64{}, Settings: map[string]string{}}
	rows, err := s.db.QueryContext(ctx, `SELECT path, rev FROM files`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var p string
		var r int64
		if err := rows.Scan(&p, &r); err != nil {
			rows.Close()
			return st, err
		}
		st.Files[p] = r
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT name, recipient, at FROM members ORDER BY name`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var m MemberRow
		if err := rows.Scan(&m.Name, &m.Recipient, &m.At); err != nil {
			rows.Close()
			return st, err
		}
		st.Members = append(st.Members, m)
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, "SELECT member, wrapped, by, at FROM room_keys ORDER BY member")
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var k KeyRow
		if err := rows.Scan(&k.Member, &k.Wrapped, &k.By, &k.At); err != nil {
			rows.Close()
			return st, err
		}
		st.Keys = append(st.Keys, k)
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		st.Settings[k] = v
	}
	return st, rows.Err()
}

// ImportState merges a room's small tables: a path takes the newer revision, members and keys the source's rows,
// and "manual" approval, once set, stays.
func (s *sqlRoom) ImportState(ctx context.Context, st RoomState) error {
	return s.inRoomTx(ctx, func(tx *sql.Tx) error {
		for p, r := range st.Files {
			if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO files (path, rev) VALUES (?, ?) ON CONFLICT (path) DO UPDATE SET rev = excluded.rev WHERE files.rev < excluded.rev`), p, r); err != nil {
				return err
			}
		}
		// Members and wrapped keys are copied as a set, not merged: a key the source dropped (its member got a new
		// recipient) must not survive in the destination.
		if _, err := tx.ExecContext(ctx, `DELETE FROM room_keys`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM members`); err != nil {
			return err
		}
		for _, m := range st.Members {
			if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO members (name, recipient, at) VALUES (?, ?, ?) ON CONFLICT (name) DO UPDATE SET recipient = excluded.recipient, at = excluded.at`), m.Name, m.Recipient, m.At); err != nil {
				return err
			}
		}
		for _, k := range st.Keys {
			if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO room_keys (member, wrapped, by, at) VALUES (?, ?, ?, ?) ON CONFLICT (member) DO UPDATE SET wrapped = excluded.wrapped, by = excluded.by, at = excluded.at`), k.Member, k.Wrapped, k.By, k.At); err != nil {
				return err
			}
		}
		for k, v := range st.Settings {
			if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = CASE WHEN settings.value = 'manual' THEN 'manual' ELSE excluded.value END`), k, v); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *sqlRoom) Counts(ctx context.Context) (RoomCounts, error) {
	var c RoomCounts
	err := s.db.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM history), (SELECT COUNT(*) FROM files), (SELECT COUNT(*) FROM members),
  (SELECT COUNT(*) FROM room_keys), (SELECT COALESCE(MAX(rev), 0) FROM history)`).Scan(&c.History, &c.Files, &c.Members, &c.Keys, &c.MaxRev)
	return c, err
}

const leaseColumns = `key, machine, chat, usr, chat_title, turn, since, beat, edit`

func scanLease(row interface{ Scan(...any) error }) (*Lease, error) {
	var l Lease
	if err := row.Scan(&l.Key, &l.Machine, &l.Chat, &l.User, &l.ChatTitle, &l.Turn, &l.Since, &l.Beat, &l.Edit); err != nil {
		return nil, err
	}
	return &l, nil
}

func leaseLive(l *Lease, now int64) bool { return now-l.Beat < LeaseTTLMs }

func (s *sqlRoom) lease(ctx context.Context, tx *sql.Tx, key string) (*Lease, error) {
	l, err := scanLease(tx.QueryRowContext(ctx, s.q(`SELECT `+leaseColumns+` FROM leases WHERE key = ?`), key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (s *sqlRoom) LeaseTake(ctx context.Context, req LeaseRequest, machine string, now int64) (Lease, bool, error) {
	var out Lease
	held := false
	err := s.inRoomTx(ctx, func(tx *sql.Tx) error {
		cur, err := s.lease(ctx, tx, req.Key)
		if err != nil {
			return err
		}
		live := cur != nil && leaseLive(cur, now)
		if live && (cur.Machine != machine || cur.Chat != req.Chat) {
			out, held = *cur, true
			return nil
		}
		l := Lease{Key: req.Key, Machine: machine, Chat: req.Chat, User: req.User, ChatTitle: req.ChatTitle, Turn: req.Turn,
			Since: now, Beat: now, Edit: now}
		if live {
			l.Since, l.Edit = cur.Since, cur.Edit
			if req.Turn == "" {
				l.Turn = cur.Turn
			}
			if req.ChatTitle == "" {
				l.ChatTitle = cur.ChatTitle
			}
			if req.User == "" {
				l.User = cur.User
			}
			if req.Edit {
				l.Edit = now
			}
		}
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO leases (`+leaseColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (key) DO UPDATE SET machine = excluded.machine, chat = excluded.chat, usr = excluded.usr, chat_title = excluded.chat_title,
  turn = excluded.turn, since = excluded.since, beat = excluded.beat, edit = excluded.edit`),
			l.Key, l.Machine, l.Chat, l.User, l.ChatTitle, l.Turn, l.Since, l.Beat, l.Edit); err != nil {
			return err
		}
		out = l
		return nil
	})
	return out, held, err
}

func (s *sqlRoom) LeaseDrop(ctx context.Context, req LeaseRelease, machine string, now int64) (*Lease, error) {
	var dropped *Lease
	err := s.inRoomTx(ctx, func(tx *sql.Tx) error {
		cur, err := s.lease(ctx, tx, req.Key)
		if err != nil || cur == nil {
			return err
		}
		if !req.Force && (cur.Machine != machine || cur.Chat != req.Chat) {
			return nil
		}
		if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM leases WHERE key = ?`), req.Key); err != nil {
			return err
		}
		if leaseLive(cur, now) {
			dropped = cur
		}
		return nil
	})
	return dropped, err
}

func (s *sqlRoom) Leases(ctx context.Context, now int64) ([]Lease, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+leaseColumns+` FROM leases WHERE beat > ? ORDER BY key`), now-LeaseTTLMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lease{}
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}
