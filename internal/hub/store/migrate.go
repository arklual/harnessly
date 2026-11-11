package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// Pooler modes of Options.Pooler.
const (
	PoolerOff         = ""
	PoolerTransaction = "transaction"
)

// Check modes of Options.Check: what the hub does when, after taking the migration lock, the database is newer than
// the binary or its schema differs from the one the migrations produce.
const (
	CheckWarn   = "warn"   // log and go on: what the hub did before the checks existed (the default)
	CheckStrict = "strict" // refuse to start with a StartupError
	CheckOff    = "off"    // do not look
)

// Options are the Postgres settings of a hub database. The zero value is the behaviour from before they existed.
type Options struct {
	// Schema is the Postgres schema the hub's tables live in instead of the one the connection's search_path names
	// (usually public). It is created when missing. Go through a pooler: it needs the pooler to pass search_path on
	// (PgBouncer 1.22+ with track_extra_parameters = search_path) or the role to carry it (ALTER ROLE … SET
	// search_path), and the hub checks that the connections really use it.
	Schema string
	// Pooler says a connection pooler in transaction mode sits in front of the database (PoolerTransaction): the hub
	// speaks the simple protocol, with no prepared statements, and migrates under transaction-scoped advisory locks
	// because a session lock does not survive between the pooler's transactions.
	Pooler string
	// MigrateURL is a direct (not pooled) URL used for the migrations only; with it the migrations hold a session
	// advisory lock for the whole run. Empty: migrate over the main URL.
	MigrateURL string
	// Check is CheckWarn (the default), CheckStrict or CheckOff.
	Check string
	// LockTimeout is how long a migration waits for another replica that migrates; 0 is two minutes.
	LockTimeout time.Duration
	// Log gets the migration progress; nil is slog.Default().
	Log *slog.Logger
	// Last stops the migrations after this version and skips the schema check; 0 applies them all. It is for the
	// tests of a migration that moves data: they fill a database as it was before the migration and then open it again.
	Last int
}

var schemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

func (o Options) normalized() (Options, error) {
	o.Schema = strings.TrimSpace(o.Schema)
	if o.Schema != "" && !schemaName.MatchString(o.Schema) {
		return o, fmt.Errorf("database schema %q: use lower-case letters, digits and _, starting with a letter or _ (63 characters at most)", o.Schema)
	}
	switch o.Pooler {
	case PoolerOff, PoolerTransaction:
	default:
		return o, fmt.Errorf("database pooler %q: use %q or leave it empty", o.Pooler, PoolerTransaction)
	}
	switch o.Check {
	case "":
		o.Check = CheckWarn
	case CheckWarn, CheckStrict, CheckOff:
	default:
		return o, fmt.Errorf("database check %q: use warn, strict or off", o.Check)
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = 2 * time.Minute
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return o, nil
}

// Startup error codes: the hub prints the code and exits with ExitCode, so a supervisor and a person reading the log
// can tell a refusal from a crash.
const (
	CodeDBNewerThanBinary   = "DB_NEWER_THAN_BINARY"
	CodeDBSchemaDrift       = "DB_SCHEMA_DRIFT"
	CodeDBSchemaMismatch    = "DB_SCHEMA_MISMATCH"
	CodeDBMigrateLockTimout = "DB_MIGRATE_LOCK_TIMEOUT"
)

// StartupError is a refusal to start that has a code: a structured error, not a stack of text.
type StartupError struct {
	Code    string
	Message string
	Err     error
}

func (e *StartupError) Error() string { return e.Code + ": " + e.Message }
func (e *StartupError) Unwrap() error { return e.Err }

// ExitCode is the process exit code of the refusal (70 and up; 2 is a bad configuration, 1 anything else).
func (e *StartupError) ExitCode() int {
	switch e.Code {
	case CodeDBNewerThanBinary:
		return 70
	case CodeDBSchemaDrift:
		return 71
	case CodeDBSchemaMismatch:
		return 72
	case CodeDBMigrateLockTimout:
		return 73
	}
	return 1
}

// connectPostgres opens a pgx pool for the options. pooled forces the simple protocol.
func connectPostgres(databaseURL string, opts Options, pooled bool) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database url: %w", redactURLErr(err))
	}
	if opts.Schema != "" {
		if cfg.RuntimeParams == nil {
			cfg.RuntimeParams = map[string]string{}
		}
		cfg.RuntimeParams["search_path"] = opts.Schema
	}
	if pooled {
		// No prepared statements and no statement cache: a pooler in transaction mode hands every transaction to
		// another server connection, which has none of them.
		cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		cfg.StatementCacheCapacity = 0
		cfg.DescriptionCacheCapacity = 0
	}
	raw := sql.OpenDB(stdlib.GetConnector(*cfg))
	raw.SetMaxOpenConns(20)
	raw.SetConnMaxLifetime(30 * time.Minute)
	return raw, nil
}

// redactURLErr keeps a parse error from echoing the password of the URL.
func redactURLErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return errors.New("cannot parse the URL")
	}
	return errors.New(strings.ReplaceAll(err.Error(), "\n", " "))
}

// migration is one embedded .sql file.
type migration struct {
	version int
	name    string
	body    string
	compat  bool // «-- compat: previous»: the previous release still runs on the database after it
}

var compatMarker = regexp.MustCompile(`(?m)^--\s*compat:\s*previous\s*$`)

func loadMigrations() ([]migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		version, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(version)
		if err != nil {
			return nil, fmt.Errorf("migration %s: the name must start with a number", e.Name())
		}
		body, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name(), body: string(body), compat: compatMarker.Match(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("migrations %s and %s have the same number", out[i-1].name, out[i].name)
		}
	}
	return out, nil
}

// Migrate applies the embedded migrations not applied yet, in order, and then checks the schema (see Options.Check).
// Open calls it already; calling it again is harmless.
func (db *DB) Migrate(ctx context.Context) error { return db.migrateAll(ctx) }

func (db *DB) migrateAll(ctx context.Context) error {
	raw, sessionSafe, closeRaw := db.sql, db.opts.Pooler == PoolerOff, func() {}
	if db.driver == Postgres && db.opts.MigrateURL != "" {
		direct, err := connectPostgres(db.opts.MigrateURL, db.opts, false)
		if err != nil {
			return fmt.Errorf("HUB_DB_MIGRATE_URL: %w", err)
		}
		if err := direct.PingContext(ctx); err != nil {
			direct.Close()
			return fmt.Errorf("HUB_DB_MIGRATE_URL: %w", err)
		}
		raw, sessionSafe, closeRaw = direct, true, func() { direct.Close() }
	}
	defer closeRaw()
	if err := migrateOn(ctx, raw, db.driver, sessionSafe, db.opts); err != nil {
		return err
	}
	if db.driver == Postgres && db.opts.Schema != "" {
		// A pooler that drops the search_path startup parameter would run the hub in the wrong schema, silently.
		var path string
		if err := db.sql.QueryRowContext(ctx, `SELECT current_setting('search_path')`).Scan(&path); err != nil {
			return fmt.Errorf("database: %w", err)
		}
		if first, _, _ := strings.Cut(path, ","); strings.Trim(strings.TrimSpace(first), `"`) != db.opts.Schema {
			return schemaMismatch(path, db.opts.Schema)
		}
	}
	return nil
}

func schemaMismatch(path, want string) error {
	return &StartupError{Code: CodeDBSchemaMismatch, Message: fmt.Sprintf(
		"the hub's connections use search_path %q, not schema %q (HUB_DB_SCHEMA). Behind a pooler the search_path startup parameter is dropped: "+
			"use PgBouncer 1.22+ with track_extra_parameters = search_path (and without search_path in ignore_startup_parameters), "+
			"or run ALTER ROLE <role> SET search_path = %s", path, want, want)}
}

type beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// lockKey is the advisory lock number of a database schema's migrations.
func lockKey(schema string) int64 {
	h := fnv.New64a()
	h.Write([]byte("harnessly-hub/migrate/" + schema))
	return int64(h.Sum64())
}

func migrateOn(ctx context.Context, raw *sql.DB, driver Driver, sessionSafe bool, opts Options) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	var b beginner = raw
	xactLock := false
	if driver == Postgres {
		key := lockKey(opts.Schema)
		if sessionSafe {
			conn, err := raw.Conn(ctx)
			if err != nil {
				return fmt.Errorf("migrations: %w", err)
			}
			defer conn.Close()
			if err := sessionLock(ctx, conn, key, opts.LockTimeout); err != nil {
				return err
			}
			defer func() {
				uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				_, _ = conn.ExecContext(uctx, `SELECT pg_advisory_unlock($1)`, key)
			}()
			b = conn
		} else {
			xactLock = true
		}
	}
	// step runs fn in one transaction, under the transaction-scoped lock when the session lock is not available.
	step := func(fn func(q Q) error) error {
		tx, err := b.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		q := txQ{t: tx, driver: driver}
		if xactLock {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL lock_timeout = '%dms'`, opts.LockTimeout.Milliseconds())); err != nil {
				_ = tx.Rollback()
				return err
			}
			if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey(opts.Schema)); err != nil {
				_ = tx.Rollback()
				return lockError(err, opts.LockTimeout)
			}
		}
		if err := fn(q); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}

	if err := step(func(q Q) error { return bootstrap(ctx, q, driver, opts) }); err != nil {
		return wrapMigrationErr(err)
	}

	known := map[int]migration{}
	for _, m := range ms {
		known[m.version] = m
	}
	if opts.Check != CheckOff {
		err := step(func(q Q) error { return checkNotNewer(ctx, q, known, opts) })
		if err != nil {
			return wrapMigrationErr(err)
		}
	}

	for _, m := range ms {
		if opts.Last > 0 && m.version > opts.Last {
			return nil
		}
		err := step(func(q Q) error {
			// Looked at again under the lock: a replica that held it before has applied this one already.
			var n int
			if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return nil
			}
			for _, stmt := range splitStatements(m.body) {
				if _, err := q.Exec(ctx, stmt); err != nil {
					return fmt.Errorf("%s: %w", m.name, err)
				}
			}
			if _, err := q.Exec(ctx, `INSERT INTO schema_migrations (version, applied_at, compat_prev) VALUES (?, ?, ?)`,
				m.version, Ms(time.Now()), boolInt(m.compat)); err != nil {
				return err
			}
			opts.Log.Info("migration applied", "version", m.version, "file", m.name)
			return nil
		})
		if err != nil {
			return wrapMigrationErr(err)
		}
	}

	if opts.Check != CheckOff {
		err := step(func(q Q) error {
			diffs, err := VerifySchema(ctx, q, driver)
			if err != nil {
				return err
			}
			if len(diffs) == 0 {
				return nil
			}
			msg := "the database schema differs from the one the migrations produce: " + strings.Join(diffs, "; ") +
				". Someone changed the schema by hand, or a migration was edited after it was applied"
			if opts.Check == CheckStrict {
				return &StartupError{Code: CodeDBSchemaDrift, Message: msg}
			}
			opts.Log.Warn("database schema drift (HUB_DB_CHECK=strict refuses to start)", "code", CodeDBSchemaDrift, "diff", diffs)
			return nil
		})
		if err != nil {
			return wrapMigrationErr(err)
		}
	}
	return nil
}

func wrapMigrationErr(err error) error {
	var se *StartupError
	if errors.As(err, &se) {
		return err
	}
	return fmt.Errorf("migrations: %w", err)
}

func lockError(err error, wait time.Duration) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "55P03" {
		return lockTimeout(wait)
	}
	return err
}

func lockTimeout(wait time.Duration) error {
	return &StartupError{Code: CodeDBMigrateLockTimout, Message: fmt.Sprintf(
		"another hub replica held the migration lock for more than %s; if no hub is migrating, find the session that holds advisory lock %s in pg_locks",
		wait, "harnessly-hub/migrate")}
}

// sessionLock takes a session-level advisory lock on conn, polling so that the wait has an end.
func sessionLock(ctx context.Context, conn *sql.Conn, key int64, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		var ok bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&ok); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return lockTimeout(wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// bootstrap makes the schema (when HUB_DB_SCHEMA names one) and the table that lists the applied migrations.
func bootstrap(ctx context.Context, q Q, driver Driver, opts Options) error {
	if driver == Postgres && opts.Schema != "" {
		if _, err := q.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS "`+opts.Schema+`"`); err != nil {
			return err
		}
		// Before anything is created: a pooler that drops the search_path startup parameter would put the tables
		// in the wrong schema, or fail with «no schema has been selected».
		var path string
		if err := q.QueryRow(ctx, `SELECT current_setting('search_path')`).Scan(&path); err != nil {
			return err
		}
		if first, _, _ := strings.Cut(path, ","); strings.Trim(strings.TrimSpace(first), `"`) != opts.Schema {
			return schemaMismatch(path, opts.Schema)
		}
	}
	if _, err := q.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at BIGINT NOT NULL)`); err != nil {
		return err
	}
	// compat_prev: the migration is marked «-- compat: previous»; a binary that does not know the version reads it to
	// decide whether it may run on this database. Rows written by an older release take the default 0.
	has, err := hasColumn(ctx, q, driver, "schema_migrations", "compat_prev")
	if err != nil {
		return err
	}
	if !has {
		if _, err := q.Exec(ctx, `ALTER TABLE schema_migrations ADD COLUMN compat_prev INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

func hasColumn(ctx context.Context, q Q, driver Driver, table, column string) (bool, error) {
	var n int
	var err error
	if driver == Postgres {
		err = q.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, column).Scan(&n)
	} else {
		err = q.QueryRow(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	}
	return n > 0, err
}

// checkNotNewer refuses (strict) or warns when the database carries migrations this binary does not know and that are
// not marked as compatible with the previous release.
func checkNotNewer(ctx context.Context, q Q, known map[int]migration, opts Options) error {
	rows, err := q.Query(ctx, `SELECT version, compat_prev FROM schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var blocking, compatible []int
	maxKnown := 0
	for v := range known {
		if v > maxKnown {
			maxKnown = v
		}
	}
	for rows.Next() {
		var v, compat int
		if err := rows.Scan(&v, &compat); err != nil {
			return err
		}
		if _, ok := known[v]; ok {
			continue
		}
		if compat == 1 {
			compatible = append(compatible, v)
		} else {
			blocking = append(blocking, v)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(compatible) > 0 {
		opts.Log.Info("the database has newer migrations marked compatible with this release", "versions", compatible)
	}
	if len(blocking) == 0 {
		return nil
	}
	msg := fmt.Sprintf("the database has migrations %s that this hub (it knows up to %04d) does not, and they are not marked compatible with the previous release. "+
		"A newer hub has migrated it, and rolling the binary back does not roll the data back. Start the newer hub version again; "+
		"to go back for real restore the database from the backup made before the upgrade (docs/hub/migrations.md)", versionList(blocking), maxKnown)
	if opts.Check == CheckStrict {
		return &StartupError{Code: CodeDBNewerThanBinary, Message: msg}
	}
	opts.Log.Warn("database is newer than this hub (HUB_DB_CHECK=strict refuses to start)", "code", CodeDBNewerThanBinary, "message", msg)
	return nil
}

func versionList(vs []int) string {
	var s []string
	for _, v := range vs {
		s = append(s, fmt.Sprintf("%04d", v))
	}
	return strings.Join(s, ", ")
}
