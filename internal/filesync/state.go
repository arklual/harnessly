package filesync

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// known is what the agent knows about one file: the hub's revision whose contents the local file had when the
// agent last looked, and the file's size and mtime then. A local file whose size and mtime still match is unchanged
// without reading it; that is also how the agent's own downloads do not echo back as edits.
type known struct {
	Rev     int64
	Sha     string
	Size    int64
	Mtime   int64
	Deleted bool
}

type state struct{ db *sql.DB }

func openState(dir string) (*state, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "agent.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, rev INTEGER NOT NULL, sha TEXT NOT NULL, size INTEGER NOT NULL, mtime INTEGER NOT NULL, deleted INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS base (path TEXT PRIMARY KEY, contents BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS quarantine (path TEXT PRIMARY KEY, rev INTEGER NOT NULL, reason TEXT NOT NULL, since INTEGER NOT NULL);`); err != nil {
		db.Close()
		return nil, err
	}
	return &state{db: db}, nil
}

func (s *state) get(path string) (*known, error) {
	var k known
	var deleted int
	err := s.db.QueryRow(`SELECT rev, sha, size, mtime, deleted FROM files WHERE path = ?`, path).Scan(&k.Rev, &k.Sha, &k.Size, &k.Mtime, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	k.Deleted = deleted != 0
	return &k, err
}

func (s *state) put(path string, k known) error {
	d := 0
	if k.Deleted {
		d = 1
	}
	_, err := s.db.Exec(`INSERT INTO files (path, rev, sha, size, mtime, deleted) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET rev = excluded.rev, sha = excluded.sha, size = excluded.size, mtime = excluded.mtime, deleted = excluded.deleted`,
		path, k.Rev, k.Sha, k.Size, k.Mtime, d)
	return err
}

// live lists the paths believed to exist locally, under prefix ("" for all).
func (s *state) live(prefix string) ([]string, error) {
	q, args := `SELECT path FROM files WHERE deleted = 0`, []any{}
	if prefix != "" {
		q += ` AND substr(path, 1, ?) = ?`
		args = append(args, len(prefix)+1, prefix+"/")
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *state) cursor() (int64, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'cursor'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

func (s *state) setCursor(rev int64) error {
	_, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES ('cursor', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.FormatInt(rev, 10))
	return err
}

// putBase / getBase keep the last synced canonical contents of a file that merges (settings.json).
func (s *state) putBase(path string, contents []byte) error {
	_, err := s.db.Exec(`INSERT INTO base (path, contents) VALUES (?, ?) ON CONFLICT(path) DO UPDATE SET contents = excluded.contents`, path, contents)
	return err
}

func (s *state) getBase(path string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT contents FROM base WHERE path = ?`, path).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

func (s *state) count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&n)
	return n, err
}

// forget drops what the agent knows about a path (one the policy no longer syncs).
func (s *state) forget(path string) error {
	_, err := s.db.Exec(`DELETE FROM files WHERE path = ?`, path)
	if err == nil {
		_, err = s.db.Exec(`DELETE FROM base WHERE path = ?`, path)
	}
	return err
}

// Quarantined is a file the agent cannot apply (no rights on the local copy): it is set aside so the rest of the
// queue goes on, and tried again on every rescan and when a newer revision of it arrives.
type Quarantined struct {
	Path   string
	Rev    int64
	Reason string
	Since  time.Time
}

func (s *state) quarantine(path string, rev int64, reason string, now time.Time) error {
	_, err := s.db.Exec(`INSERT INTO quarantine (path, rev, reason, since) VALUES (?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET rev = excluded.rev, reason = excluded.reason`, path, rev, reason, now.Unix())
	return err
}

func (s *state) release(path string) error {
	_, err := s.db.Exec(`DELETE FROM quarantine WHERE path = ?`, path)
	return err
}

func (s *state) quarantined() ([]Quarantined, error) {
	rows, err := s.db.Query(`SELECT path, rev, reason, since FROM quarantine ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Quarantined
	for rows.Next() {
		var q Quarantined
		var since int64
		if err := rows.Scan(&q.Path, &q.Rev, &q.Reason, &since); err != nil {
			return nil, err
		}
		q.Since = time.Unix(since, 0)
		out = append(out, q)
	}
	return out, rows.Err()
}
