package filestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	_ "modernc.org/sqlite"
)

// sqliteMeta keeps the metadata in a folder: the room "design" in hub.db (the layout of a design-sync hub), another
// room in rooms/<room>/hub.db. One process owns the folder.
type sqliteMeta struct {
	dir string

	mu    sync.Mutex
	rooms map[string]*sqlRoom
	locks map[string]*os.File
}

func sqliteDSN(file string) string {
	return "file:" + file + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
}

func openSQLiteMeta(dir string) (*sqliteMeta, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &sqliteMeta{dir: dir, rooms: map[string]*sqlRoom{}, locks: map[string]*os.File{}}, nil
}

func (m *sqliteMeta) roomFile(room string) string {
	if room == DefaultRoom {
		return filepath.Join(m.dir, "hub.db")
	}
	return filepath.Join(m.dir, "rooms", room, "hub.db")
}

func (m *sqliteMeta) Room(ctx context.Context, room string) (RoomMeta, error) {
	if room != DefaultRoom && !ValidRoom(room) {
		return nil, os.ErrNotExist
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rooms[room]; ok {
		return r, nil
	}
	file := m.roomFile(room)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteDSN(file))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, roomSchema); err != nil {
		db.Close()
		return nil, err
	}
	// Hubs before 0.2 have no fingerprint column.
	var hasTag int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('history') WHERE name = 'tag'`).Scan(&hasTag); err != nil {
		db.Close()
		return nil, err
	}
	if hasTag == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE history ADD COLUMN tag TEXT NOT NULL DEFAULT ''`); err != nil {
			db.Close()
			return nil, err
		}
	}
	r := &sqlRoom{db: db, n: newNotifier(), mu: newChanLock(true)}
	m.rooms[room] = r
	return r, nil
}

func (m *sqliteMeta) Rooms(ctx context.Context) ([]string, error) {
	var out []string
	if _, err := os.Stat(m.roomFile(DefaultRoom)); err == nil {
		out = append(out, DefaultRoom)
	}
	entries, err := os.ReadDir(filepath.Join(m.dir, "rooms"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !ValidRoom(e.Name()) {
			continue
		}
		if _, err := os.Stat(m.roomFile(e.Name())); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// TryLock is an flock on a file in the folder: it holds across processes (a `gc` run next to the hub).
func (m *sqliteMeta) TryLock(_ context.Context, name string) (func(), bool, error) {
	f, err := os.OpenFile(filepath.Join(m.dir, ".lock-"+name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true, nil
}

func (m *sqliteMeta) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for _, r := range m.rooms {
		if err := r.db.Close(); err != nil && first == nil {
			first = err
		}
	}
	giveBack(m.dir)
	return first
}

// giveBack hands the database files back to the folder's owner when this process runs as root next to a hub that
// runs as its own (dynamic) user: files root created (a -wal that did not exist) would otherwise lock the hub out of
// its own data after a `backup` or `gc` run by an administrator.
func giveBack(dir string) {
	if os.Geteuid() != 0 {
		return
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() && p != dir && p != filepath.Join(dir, "rooms") && filepath.Dir(p) != filepath.Join(dir, "rooms") {
			return filepath.SkipDir // blobs/ is the blob store's business
		}
		if !d.IsDir() && (strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db-wal") || strings.HasSuffix(name, ".db-shm") || strings.HasPrefix(name, ".lock-")) {
			_ = os.Lchown(p, int(st.Uid), int(st.Gid))
		}
		return nil
	})
}

// Snapshot writes a consistent copy of a room's database to file (VACUUM INTO) while the hub keeps writing.
func (m *sqliteMeta) Snapshot(ctx context.Context, room, file string) error {
	r, err := m.Room(ctx, room)
	if err != nil {
		return err
	}
	return vacuumInto(ctx, r.(*sqlRoom).db, file)
}

func vacuumInto(ctx context.Context, db *sql.DB, file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp := file + ".new"
	_ = os.Remove(tmp) // VACUUM INTO refuses an existing file
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		return fmt.Errorf("vacuum into %s: %w", tmp, err)
	}
	return os.Rename(tmp, file)
}
