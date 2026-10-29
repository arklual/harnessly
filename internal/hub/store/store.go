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
}

// Driver says which dialect this database speaks.
func (db *DB) Driver() Driver { return db.driver }

// Open connects to a sqlite:///path or postgres:// URL and applies the migrations.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
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
		db = &DB{sql: raw, driver: SQLite}
	case "postgres", "postgresql":
		raw, err := sql.Open("pgx", databaseURL)
		if err != nil {
			return nil, err
		}
		raw.SetMaxOpenConns(20)
		raw.SetConnMaxLifetime(30 * time.Minute)
		db = &DB{sql: raw, driver: Postgres}
	default:
		return nil, fmt.Errorf("database url: unsupported scheme %q", u.Scheme)
	}
	if err := db.sql.PingContext(ctx); err != nil {
		db.sql.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.sql.Close()
		return nil, err
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

// Migrate applies the embedded migrations not applied yet, in order.
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at BIGINT NOT NULL)`); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := db.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		version, _, _ := strings.Cut(name, "_")
		v, err := strconv.Atoi(version)
		if err != nil {
			return fmt.Errorf("migration %s: the name must start with a number", name)
		}
		if applied[v] {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		err = db.Tx(ctx, func(s Store) error {
			for _, stmt := range splitStatements(string(body)) {
				if _, err := s.q.Exec(ctx, stmt); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
			_, err := s.q.Exec(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, v, Ms(time.Now()))
			return err
		})
		if err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	}
	return nil
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
