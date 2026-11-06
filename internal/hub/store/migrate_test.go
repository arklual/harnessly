package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Postgres tests run when HUB_TEST_POSTGRES_URL is set; the pooler ones also need HUB_TEST_PGBOUNCER_URL, a
// PgBouncer in transaction mode in front of the same database (CI starts both as services).

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) count(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.b.String(), s)
}

func testLog(w *lockedBuf) *slog.Logger { return slog.New(slog.NewTextHandler(w, nil)) }

func envURL(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skip(name + " is not set")
	}
	return v
}

// freshSchema makes an empty schema in the database behind admin and drops it afterwards.
func freshSchema(t *testing.T, adminURL string) (admin *sql.DB, schema string) {
	t.Helper()
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	schema = "hubmig_" + strings.ToLower(RandomBase32(6))
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); admin.Close() })
	return admin, schema
}

// poolRole makes a login role for one test and returns baseURL with its credentials. With withPath the role carries
// search_path = schema (ALTER ROLE … SET), which is what survives any pooler; without it the role has the default
// search_path. A pooler keeps one server-connection pool per role, so tests with different schemas do not share
// server connections (PgBouncer does not re-send a tracked parameter reliably when clients with different values
// share a pool; a real install has one schema per role).
func poolRole(t *testing.T, admin *sql.DB, baseURL, schema string, withPath bool) string {
	t.Helper()
	role := "r_" + schema
	stmts := []string{
		`CREATE ROLE ` + role + ` LOGIN PASSWORD 'pw'`,
		`GRANT CREATE, CONNECT ON DATABASE hub TO ` + role,
	}
	if withPath {
		stmts = append(stmts, `ALTER ROLE `+role+` SET search_path = `+schema)
	}
	for _, q := range stmts {
		if _, err := admin.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`)
		_, _ = admin.Exec(`DROP OWNED BY ` + role)
		_, _ = admin.Exec(`DROP ROLE IF EXISTS ` + role)
	})
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, "pw")
	return u.String()
}

func migrationCount(t *testing.T) int {
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	return len(ms)
}

func TestLoadMigrations(t *testing.T) {
	ms, err := loadMigrations()
	if err != nil || len(ms) == 0 {
		t.Fatal(ms, err)
	}
	for i := 1; i < len(ms); i++ {
		if ms[i].version != ms[i-1].version+1 {
			t.Errorf("migration numbers have a gap between %s and %s", ms[i-1].name, ms[i].name)
		}
	}
}

func TestSchemaSnapshotSQLite(t *testing.T) {
	db := openTest(t)
	diffs, err := VerifySchema(context.Background(), db, SQLite)
	if err != nil || len(diffs) > 0 {
		t.Fatalf("the committed snapshot is out of date (run `make hub-schema`): %v %v", diffs, err)
	}
}

func TestSchemaSnapshotPostgres(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	_, schema := freshSchema(t, dsn)
	db, err := OpenWith(context.Background(), dsn, Options{Schema: schema, Check: CheckStrict})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	diffs, err := VerifySchema(context.Background(), db, Postgres)
	if err != nil || len(diffs) > 0 {
		t.Fatalf("the committed snapshot is out of date (run `make hub-schema`): %v %v", diffs, err)
	}
}

func TestDiffSchemaFindsDrift(t *testing.T) {
	want, err := ExpectedSchema(SQLite)
	if err != nil {
		t.Fatal(err)
	}
	have, _ := ExpectedSchema(SQLite)
	if d := DiffSchema(want, have); len(d) != 0 {
		t.Fatal(d)
	}
	delete(have.Tables, "spaces")
	if d := DiffSchema(want, have); len(d) == 0 || !strings.Contains(d[0], "spaces") {
		t.Fatal(d)
	}
}

func TestDriftIsAStartupError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	ctx := context.Background()
	db, err := Open(ctx, "sqlite://"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DROP INDEX IF EXISTS `+firstIndex(t, db)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var buf lockedBuf
	// warn (the default): starts, says so.
	db, err = OpenWith(ctx, "sqlite://"+path, Options{Log: testLog(&buf)})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if buf.count(CodeDBSchemaDrift) == 0 {
		t.Fatal("no warning in the log:", buf.b.String())
	}
	// strict: refuses with a code.
	_, err = OpenWith(ctx, "sqlite://"+path, Options{Check: CheckStrict, Log: testLog(&buf)})
	var se *StartupError
	if !errors.As(err, &se) || se.Code != CodeDBSchemaDrift || se.ExitCode() != 71 {
		t.Fatalf("want %s, got %v", CodeDBSchemaDrift, err)
	}
	// off: does not look.
	db, err = OpenWith(ctx, "sqlite://"+path, Options{Check: CheckOff})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func firstIndex(t *testing.T, db *DB) string {
	t.Helper()
	var name string
	if err := db.QueryRow(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL ORDER BY name LIMIT 1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestDatabaseNewerThanBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	ctx := context.Background()
	db, err := Open(ctx, "sqlite://"+path)
	if err != nil {
		t.Fatal(err)
	}
	// A newer release applied 0900 (not marked) and 0901 (compatible with the previous release).
	for _, c := range []struct{ v, compat int }{{900, 0}, {901, 1}} {
		if _, err := db.Exec(ctx, `INSERT INTO schema_migrations (version, applied_at, compat_prev) VALUES (?, ?, ?)`, c.v, 1, c.compat); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	var buf lockedBuf
	_, err = OpenWith(ctx, "sqlite://"+path, Options{Check: CheckStrict, Log: testLog(&buf)})
	var se *StartupError
	if !errors.As(err, &se) || se.Code != CodeDBNewerThanBinary || se.ExitCode() != 70 {
		t.Fatalf("want %s, got %v", CodeDBNewerThanBinary, err)
	}
	if !strings.Contains(se.Message, "0900") || strings.Contains(se.Message, "0901") {
		t.Errorf("the message should name 0900 and not the compatible 0901: %s", se.Message)
	}
	// warn (the default) starts as before.
	db, err = OpenWith(ctx, "sqlite://"+path, Options{Log: testLog(&buf)})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Only a compatible migration newer than the binary: strict starts.
	path2 := filepath.Join(t.TempDir(), "hub.db")
	db, err = Open(ctx, "sqlite://"+path2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO schema_migrations (version, applied_at, compat_prev) VALUES (901, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenWith(ctx, "sqlite://"+path2, Options{Check: CheckStrict, Log: testLog(&buf)})
	if err != nil {
		t.Fatal("a compatible newer migration must not stop the start:", err)
	}
	db.Close()
}

func TestCompatMarker(t *testing.T) {
	if !compatMarker.MatchString("-- compat: previous\nALTER TABLE x ADD COLUMN y TEXT;") {
		t.Error("marker not recognised")
	}
	if compatMarker.MatchString("ALTER TABLE x DROP COLUMN y; -- compat: previous") {
		t.Error("the marker is a line of its own")
	}
}

func TestOptionsValidated(t *testing.T) {
	for _, o := range []Options{{Schema: "Bad-Name"}, {Pooler: "session"}, {Check: "maybe"}} {
		if _, err := o.normalized(); err == nil {
			t.Errorf("%+v accepted", o)
		}
	}
}

// parallelOpen opens n hubs on one database at the same moment, as n replicas that start together.
func parallelOpen(t *testing.T, n int, dsn string, opts Options) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			db, err := OpenWith(context.Background(), dsn, opts)
			if err == nil {
				db.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertAppliedOnce(t *testing.T, admin *sql.DB, schema string, buf *lockedBuf) {
	t.Helper()
	var n, distinct int
	if err := admin.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT version) FROM `+schema+`.schema_migrations`).Scan(&n, &distinct); err != nil {
		t.Fatal(err)
	}
	want := migrationCount(t)
	if n != want || distinct != want {
		t.Errorf("schema_migrations has %d rows (%d distinct), want %d", n, distinct, want)
	}
	if got := buf.count("migration applied"); got != want {
		t.Errorf("the migrations ran %d times in total, want %d (each once)", got, want)
	}
}

func TestParallelReplicasMigrateOncePostgres(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	admin, schema := freshSchema(t, dsn)
	var buf lockedBuf
	parallelOpen(t, 6, dsn, Options{Schema: schema, Check: CheckStrict, Log: testLog(&buf)})
	assertAppliedOnce(t, admin, schema, &buf)
}

func TestParallelReplicasMigrateOnceWithDirectURL(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	admin, schema := freshSchema(t, dsn)
	pooled := poolRole(t, admin, envURL(t, "HUB_TEST_PGBOUNCER_URL"), schema, true)
	var buf lockedBuf
	parallelOpen(t, 6, pooled, Options{Schema: schema, Pooler: PoolerTransaction, MigrateURL: dsn, Check: CheckStrict, Log: testLog(&buf)})
	assertAppliedOnce(t, admin, schema, &buf)
}

func TestPgBouncerThatDropsSearchPathIsRefused(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	admin, schema := freshSchema(t, dsn)
	// The bouncer ignores the search_path startup parameter and the role has none: the hub must notice.
	plain := poolRole(t, admin, envURL(t, "HUB_TEST_PGBOUNCER_PLAIN_URL"), schema, false)
	_, err := OpenWith(context.Background(), plain, Options{Schema: schema, Pooler: PoolerTransaction})
	var se *StartupError
	if !errors.As(err, &se) || se.Code != CodeDBSchemaMismatch || se.ExitCode() != 72 {
		t.Fatalf("want %s, got %v", CodeDBSchemaMismatch, err)
	}
	// Nothing was created in the wrong place.
	var n int
	if err := admin.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'schema_migrations' AND table_schema IN ('public', $1)`, schema).Scan(&n); err != nil || n != 0 {
		t.Fatalf("tables were created: %d %v", n, err)
	}
}

func TestParallelReplicasMigrateOnceThroughPgBouncer(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	admin, schema := freshSchema(t, dsn)
	// The role carries search_path, so the schema reaches the server connections whatever the pooler does with
	// startup parameters.
	pooled := poolRole(t, admin, envURL(t, "HUB_TEST_PGBOUNCER_URL"), schema, true)
	var buf lockedBuf
	parallelOpen(t, 6, pooled, Options{Schema: schema, Pooler: PoolerTransaction, Check: CheckStrict, Log: testLog(&buf)})
	assertAppliedOnce(t, admin, schema, &buf)
}

func TestPgBouncerRunsTheHub(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	admin, schema := freshSchema(t, dsn)
	pooled := poolRole(t, admin, envURL(t, "HUB_TEST_PGBOUNCER_URL"), schema, true)
	db, err := OpenWith(context.Background(), pooled, Options{Schema: schema, Pooler: PoolerTransaction, Check: CheckStrict})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The whole store works over the simple protocol and transaction pooling, with many connections at once.
	exercise(t, db)
}

func TestSchemaOptionPutsTablesInTheSchema(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	admin, schema := freshSchema(t, dsn)
	db, err := OpenWith(context.Background(), dsn, Options{Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	var n int
	if err := admin.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'spaces'`, schema).Scan(&n); err != nil || n != 1 {
		t.Fatalf("spaces is not in %s: %d %v", schema, n, err)
	}
	if err := admin.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'schema_migrations'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the hub put schema_migrations in public: %d %v", n, err)
	}
}

func TestPostgresNewerAndDrift(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	admin, schema := freshSchema(t, dsn)
	ctx := context.Background()
	db, err := OpenWith(ctx, dsn, Options{Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := admin.Exec(`INSERT INTO ` + schema + `.schema_migrations (version, applied_at, compat_prev) VALUES (900, 1, 0)`); err != nil {
		t.Fatal(err)
	}
	_, err = OpenWith(ctx, dsn, Options{Schema: schema, Check: CheckStrict})
	var se *StartupError
	if !errors.As(err, &se) || se.Code != CodeDBNewerThanBinary {
		t.Fatalf("want %s, got %v", CodeDBNewerThanBinary, err)
	}
	if _, err := admin.Exec(`DELETE FROM ` + schema + `.schema_migrations WHERE version = 900`); err != nil {
		t.Fatal(err)
	}

	if _, err := admin.Exec(`ALTER TABLE ` + schema + `.spaces DROP COLUMN name`); err != nil {
		t.Fatal(err)
	}
	_, err = OpenWith(ctx, dsn, Options{Schema: schema, Check: CheckStrict})
	if !errors.As(err, &se) || se.Code != CodeDBSchemaDrift {
		t.Fatalf("want %s, got %v", CodeDBSchemaDrift, err)
	}
}

func TestMigrateLockTimeout(t *testing.T) {
	dsn := envURL(t, "HUB_TEST_POSTGRES_URL")
	_, schema := freshSchema(t, dsn)
	ctx := context.Background()
	holder, err := connectPostgres(dsn, Options{}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	conn, err := holder.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey(schema)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = OpenWith(ctx, dsn, Options{Schema: schema, LockTimeout: 600 * time.Millisecond})
	var se *StartupError
	if !errors.As(err, &se) || se.Code != CodeDBMigrateLockTimout {
		t.Fatalf("want %s, got %v", CodeDBMigrateLockTimout, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("the wait did not end")
	}
}

func TestDBURLErrorsDoNotLeakThePassword(t *testing.T) {
	_, err := OpenWith(context.Background(), "postgres://u:s3cret-pass@127.0.0.1:1/db?sslmode=bogus%zz", Options{})
	if err != nil && strings.Contains(err.Error(), "s3cret-pass") {
		t.Fatal("the error echoes the password:", err)
	}
	_ = url.URL{}
}
