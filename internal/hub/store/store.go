// Package store is the hub's database: database/sql over SQLite (pure Go) or Postgres (pgx), one schema, embedded
// migrations, queries written with ? placeholders and rebound for Postgres.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Driver names the SQL dialect behind a DB.
type Driver string

const (
	SQLite   Driver = "sqlite"
	Postgres Driver = "postgres"
)

// ErrNotFound is what a lookup that finds nothing returns.
var ErrNotFound = sql.ErrNoRows

// Q is what a query runs on: the database or a transaction. Queries use ? placeholders.
type Q interface {
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) *sql.Row
}

// DB is an open database.
type DB struct {
	sql    *sql.DB
	driver Driver
	opts   Options
}

// Driver says which dialect this database speaks.
func (db *DB) Driver() Driver { return db.driver }

// Open connects to a sqlite:///path or postgres:// URL and applies the migrations, with the default Options.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	return OpenWith(ctx, databaseURL, Options{})
}

// OpenWith is Open with the Postgres settings of Options (schema, connection pooler, direct URL for migrations,
// how strict the checks after the migrations are). The options change nothing for SQLite.
func OpenWith(ctx context.Context, databaseURL string, opts Options) (*DB, error) {
	db, err := ConnectWith(ctx, databaseURL, opts)
	if err != nil {
		return nil, err
	}
	if err := db.migrateAll(ctx); err != nil {
		db.sql.Close()
		return nil, err
	}
	return db, nil
}

// Connect is Open without the migrations: it connects and pings, and changes nothing (`hub doctor` reads the state of
// a database this way). A sqlite file that does not exist is made.
func Connect(ctx context.Context, databaseURL string) (*DB, error) {
	return ConnectWith(ctx, databaseURL, Options{})
}

// ConnectWith is Connect with the Postgres settings of Options.
func ConnectWith(ctx context.Context, databaseURL string, opts Options) (*DB, error) {
	opts, err := opts.normalized()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database url: %w", err)
	}
	var db *DB
	switch u.Scheme {
	case "sqlite":
		path := u.Host + u.Path
		if path == "" {
			return nil, errors.New("database url: sqlite needs a file path")
		}
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
		}
		// SQLite makes the file with the process umask (0644 under the usual 022), and its -wal and -shm files take
		// the mode of the main file: so the file is made 0600 first.
		if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err != nil {
			return nil, err
		} else if err := f.Close(); err != nil {
			return nil, err
		}
		dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
		raw, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		raw.SetMaxOpenConns(8)
		db = &DB{sql: raw, driver: SQLite, opts: opts}
	case "postgres", "postgresql":
		raw, err := connectPostgres(databaseURL, opts, opts.Pooler == PoolerTransaction)
		if err != nil {
			return nil, err
		}
		db = &DB{sql: raw, driver: Postgres, opts: opts}
	default:
		return nil, fmt.Errorf("database url: unsupported scheme %q", u.Scheme)
	}
	if err := db.sql.PingContext(ctx); err != nil {
		db.sql.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return db, nil
}

func (db *DB) Close() error { return db.sql.Close() }

// Ping says whether the database answers.
func (db *DB) Ping(ctx context.Context) error { return db.sql.PingContext(ctx) }

// Rebind turns ? placeholders into $1, $2, … for Postgres; SQLite keeps them. Quoted text is left alone.
func Rebind(d Driver, query string) string {
	if d != Postgres || !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	n, quoted := 0, false
	for i := 0; i < len(query); i++ {
		switch c := query[i]; {
		case c == '\'':
			quoted = !quoted
			b.WriteByte(c)
		case c == '?' && !quoted:
			n++
			b.WriteString("$" + strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (db *DB) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return db.sql.ExecContext(ctx, Rebind(db.driver, q), args...)
}

func (db *DB) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return db.sql.QueryContext(ctx, Rebind(db.driver, q), args...)
}

func (db *DB) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return db.sql.QueryRowContext(ctx, Rebind(db.driver, q), args...)
}

type txQ struct {
	t      *sql.Tx
	driver Driver
}

func (t txQ) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.t.ExecContext(ctx, Rebind(t.driver, q), args...)
}

func (t txQ) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.t.QueryContext(ctx, Rebind(t.driver, q), args...)
}

func (t txQ) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return t.t.QueryRowContext(ctx, Rebind(t.driver, q), args...)
}

// Store is the typed queries over a Q.
type Store struct{ q Q }

// Q is what the typed queries run on, for packages that keep tables of their own (sites) and want the same
// transaction.
func (s Store) Q() Q { return s.q }

// S returns the queries over the database.
func (db *DB) S() Store { return Store{q: db} }

// Tx runs fn in one transaction: committed when fn returns nil, rolled back otherwise.
func (db *DB) Tx(ctx context.Context, fn func(s Store) error) error {
	t, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(Store{q: txQ{t: t, driver: db.driver}}); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

// PendingMigrations names the embedded migrations not applied yet, and Unknown the applied versions this build has no
// file for (the database was last written by a newer hub). It writes nothing.
func (db *DB) PendingMigrations(ctx context.Context) (pending []string, unknown []int, err error) {
	applied := map[int]bool{}
	rows, qerr := db.Query(ctx, `SELECT version FROM schema_migrations`)
	if qerr != nil {
		// No table yet: nothing is applied (a new database); any other failure shows at the next step.
		if _, perr := db.Query(ctx, `SELECT 1`); perr != nil {
			return nil, nil, qerr
		}
	} else {
		for rows.Next() {
			var v int
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return nil, nil, err
			}
			applied[v] = true
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, nil, err
	}
	known := map[int]bool{}
	for _, e := range entries {
		version, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(version)
		if err != nil {
			return nil, nil, fmt.Errorf("migration %s: the name must start with a number", e.Name())
		}
		known[v] = true
		if !applied[v] {
			pending = append(pending, e.Name())
		}
	}
	for v := range applied {
		if !known[v] {
			unknown = append(unknown, v)
		}
	}
	sort.Strings(pending)
	sort.Ints(unknown)
	return pending, unknown, nil
}

// splitStatements cuts a migration at the semicolons that end a line, dropping comment lines.
func splitStatements(sqlText string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		cur.WriteString(line + "\n")
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// IsUnique says whether err is a unique or primary key violation, on either database.
func IsUnique(err error) bool {
	if err == nil {
		return false
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == 2067 || se.Code() == 1555
	}
	var pg interface{ SQLState() string }
	if errors.As(err, &pg) {
		return pg.SQLState() == "23505"
	}
	return false
}

// Ms is a time as epoch milliseconds; the zero time is 0.
func Ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromMs(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func fromNullMs(ms sql.NullInt64) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := fromMs(ms.Int64)
	return &t
}

func nullMs(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var base32Lower = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID makes an id: 20 characters [a-z2-7], 100 random bits. Safe in URLs and as a DNS label.
func NewID() string {
	var b [13]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return strings.ToLower(base32Lower.EncodeToString(b[:]))[:20]
}

// RandomBase32 is n random bytes as lower-case base32 without padding.
func RandomBase32(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.ToLower(base32Lower.EncodeToString(b))
}

// RandomHex is n random bytes as hex.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// HashToken is the SHA-256 of a secret token, hex: what the database keeps instead of the token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
