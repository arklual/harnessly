package usagereport

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
	_ "modernc.org/sqlite"
)

// The facts file as rows. The JSON facts file is rewritten whole on every pass that read a transcript
// (7 MB a minute on a busy machine). With HARNESSLY_USAGE_SQLITE=1 the facts live in a SQLite database (WAL) beside it,
// one row per transcript, and a pass writes only the rows that changed. The JSON file stays: it is read at the first
// start (when the database is still empty), by the machine agent, and by an older runtime after a rollback; it is
// written as before but at most every jsonSnapshotEvery, so those readers see facts a little older, never a changed
// format. Off by default.

// SQLiteEnv turns the database on.
const SQLiteEnv = "HARNESSLY_USAGE_SQLITE"

// jsonSnapshotEvery is how seldom the JSON file is rewritten while the database is on.
const jsonSnapshotEvery = 15 * time.Minute

type factsDB struct {
	db *sql.DB
	// kept is what the database holds per transcript: a row is written again only when its size or time differs.
	kept map[string][2]float64
	// jsonAt is when this process last wrote the JSON file; zero before the first write.
	jsonAt time.Time
}

func sqlitePath(cacheFile string) string { return strings.TrimSuffix(cacheFile, ".json") + ".db" }

func openFactsDB(cacheFile string) (*factsDB, error) {
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+sqlitePath(cacheFile)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS facts (name TEXT PRIMARY KEY, pos INTEGER NOT NULL, size REAL NOT NULL, mtime_ms REAL NOT NULL, facts BLOB NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	return &factsDB{db: db, kept: map[string][2]float64{}}, nil
}

// load is the transcripts in order with their facts; none for an empty database.
func (f *factsDB) load() ([]string, map[string]*cachedFile) {
	rows, err := f.db.Query(`SELECT name, size, mtime_ms, facts FROM facts ORDER BY pos`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var order []string
	files := map[string]*cachedFile{}
	for rows.Next() {
		var name string
		var size, mtime float64
		var raw []byte
		if rows.Scan(&name, &size, &mtime, &raw) != nil || !valid(raw) {
			continue
		}
		facts := objectOf(raw[skipWS(raw, 0):])
		if facts == nil {
			continue
		}
		order = append(order, name)
		files[name] = &cachedFile{size: size, mtimeMs: mtime, facts: factsOf(facts)}
		f.kept[name] = [2]float64{size, mtime}
	}
	return order, files
}

// save writes the rows that changed and drops the ones that went, in one transaction. The caller holds the
// collector's lock.
func (f *factsDB) save(order []string, files map[string]*cachedFile) error {
	tx, err := f.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	up, err := tx.Prepare(`INSERT INTO facts (name, pos, size, mtime_ms, facts) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET pos = excluded.pos, size = excluded.size, mtime_ms = excluded.mtime_ms, facts = excluded.facts`)
	if err != nil {
		return err
	}
	defer up.Close()
	pos, err := tx.Prepare(`UPDATE facts SET pos = ? WHERE name = ? AND pos <> ?`)
	if err != nil {
		return err
	}
	defer pos.Close()
	next := make(map[string][2]float64, len(order))
	for i, name := range order {
		cf := files[name]
		id := [2]float64{cf.size, cf.mtimeMs}
		next[name] = id
		if f.kept[name] == id {
			if _, err := pos.Exec(i, name, i); err != nil {
				return err
			}
			continue
		}
		var w jw
		cf.facts.write(&w)
		if _, err := up.Exec(name, i, cf.size, cf.mtimeMs, w.Bytes()); err != nil {
			return err
		}
	}
	for name := range f.kept {
		if _, ok := next[name]; !ok {
			if _, err := tx.Exec(`DELETE FROM facts WHERE name = ?`, name); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	f.kept = next
	return nil
}

// sqliteOn opens the database when asked for and a facts file is kept; nil otherwise or when it cannot be opened (the
// JSON file then works as it always did).
func (c *Collector) sqliteOn() *factsDB {
	if c.sql != nil || c.sqlFailed || c.cfg.CacheFile == "" {
		return c.sql
	}
	if !c.cfg.SQLiteFacts && os.Getenv(SQLiteEnv) != "1" {
		return nil
	}
	db, err := openFactsDB(c.cfg.CacheFile)
	if err != nil {
		c.sqlFailed = true
		jsonlog.Log("warn", "usage facts database not opened", jsonlog.F{K: "error", V: err.Error()})
		return nil
	}
	c.sql = db
	return db
}
