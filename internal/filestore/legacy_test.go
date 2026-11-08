package filestore

import (
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// openLegacy writes hub.db the way the hub did before the drivers (same DDL, rev INTEGER PRIMARY KEY, no tag
// column, no counters) with one file at revision 7, and returns the function that closes it.
func openLegacy(dir string) (func(), error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "hub.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
CREATE TABLE history (rev INTEGER PRIMARY KEY, path TEXT NOT NULL, sha TEXT NOT NULL DEFAULT '', size INTEGER NOT NULL DEFAULT 0, exec INTEGER NOT NULL DEFAULT 0, deleted INTEGER NOT NULL DEFAULT 0, author TEXT NOT NULL DEFAULT '', machine TEXT NOT NULL DEFAULT '', at INTEGER NOT NULL);
CREATE TABLE files (path TEXT PRIMARY KEY, rev INTEGER NOT NULL);
CREATE INDEX files_rev ON files(rev);
CREATE INDEX history_path ON history(path, rev);
CREATE TABLE members (name TEXT PRIMARY KEY, recipient TEXT NOT NULL, at INTEGER NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE room_keys (member TEXT PRIMARY KEY, wrapped TEXT NOT NULL, by TEXT NOT NULL, at INTEGER NOT NULL);
INSERT INTO history (rev, path, sha, size, at) VALUES (7, 'p/old.html', '` + sum([]byte("old")) + `', 3, 1);
INSERT INTO files (path, rev) VALUES ('p/old.html', 7);`)
	if err != nil {
		return nil, err
	}
	return func() { db.Close() }, nil
}
