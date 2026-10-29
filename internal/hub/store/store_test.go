package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Open returns a fresh SQLite database in the test's temp dir.
func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// openPostgres opens a throwaway schema of HUB_TEST_POSTGRES_URL, or skips.
func openPostgres(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("HUB_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("HUB_TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "hubtest_" + RandomBase32(6)
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRebind(t *testing.T) {
	for _, tc := range []struct {
		driver Driver
		in     string
		want   string
	}{
		{SQLite, "SELECT ? , ?", "SELECT ? , ?"},
		{Postgres, "SELECT a FROM t WHERE x = ? AND y = ?", "SELECT a FROM t WHERE x = $1 AND y = $2"},
		{Postgres, "UPDATE t SET a = 'what?' WHERE b = ?", "UPDATE t SET a = 'what?' WHERE b = $1"},
		{Postgres, "no placeholders", "no placeholders"},
	} {
		if got := Rebind(tc.driver, tc.in); got != tc.want {
			t.Errorf("Rebind(%s, %q) = %q, want %q", tc.driver, tc.in, got, tc.want)
		}
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	for i := 0; i < 2; i++ {
		db, err := Open(ctx, "sqlite://"+path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		var n int
		files, _ := migrationFiles.ReadDir("migrations")
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(files) {
			t.Errorf("open %d: %d migrations of %d, %v", i, n, len(files), err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Errorf("migrate again: %v", err)
		}
		db.Close()
	}
}

func TestSplitStatements(t *testing.T) {
	got := splitStatements("-- a comment\nCREATE TABLE a (\n  x INTEGER\n);\n\n-- more\nCREATE INDEX i ON a (x);\n")
	if len(got) != 2 || !strings.HasPrefix(got[0], "CREATE TABLE a") || !strings.HasPrefix(got[1], "CREATE INDEX") {
		t.Errorf("%q", got)
	}
}

func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID()
		if len(id) != 20 || strings.Trim(id, "abcdefghijklmnopqrstuvwxyz234567") != "" || seen[id] {
			t.Fatalf("bad or repeated id %q", id)
		}
		seen[id] = true
	}
	if got := len(RandomBase32(15)); got != 24 {
		t.Errorf("15 bytes is %d characters", got)
	}
	if HashToken("a") == HashToken("b") || len(HashToken("a")) != 64 {
		t.Error("hash")
	}
}

// exercise runs the same flow over a database of either dialect.
func exercise(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()

	u := &User{Email: "ann@example.com", Name: "Ann"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser(ctx, &User{Email: "ann@example.com"}); !IsUnique(err) {
		t.Errorf("duplicate email: %v", err)
	}
	got, err := s.UserByEmail(ctx, "ann@example.com")
	if err != nil || got.ID != u.ID || got.Name != "Ann" || got.IsAdmin || got.LastLoginAt != nil {
		t.Errorf("user: %+v %v", got, err)
	}
	if _, err := s.UserByEmail(ctx, "nobody@example.com"); !IsNotFound(err) {
		t.Errorf("missing user: %v", err)
	}

	// identities and login methods
	if err := s.CreateIdentity(ctx, &Identity{UserID: u.ID, Provider: "email", Subject: "ann@example.com", Email: "ann@example.com", EmailVerified: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateIdentity(ctx, &Identity{UserID: u.ID, Provider: "email", Subject: "ann@example.com"}); !IsUnique(err) {
		t.Errorf("duplicate identity: %v", err)
	}
	if n, _ := s.LoginMethods(ctx, u.ID); n != 1 {
		t.Errorf("methods %d", n)
	}
	if err := s.CreatePasskey(ctx, &Passkey{UserID: u.ID, CredentialID: "cred1", Credential: "{}", Name: "Ноутбук"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.LoginMethods(ctx, u.ID); n != 2 {
		t.Errorf("methods %d", n)
	}

	// sessions
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.CreateSession(ctx, Session{TokenHash: "h1", Kind: "hub", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	sess, err := s.SessionByHash(ctx, "h1")
	if err != nil || sess.UserID != u.ID || !sess.ExpiresAt.Equal(now.Add(time.Hour)) || sess.MachineID != "" {
		t.Errorf("session: %+v %v", sess, err)
	}

	// challenges: attempts count up, consume once
	if err := s.CreateChallenge(ctx, Challenge{ID: "c1", Kind: "email", Email: "ann@example.com", CodeHash: "x", ExpiresAt: now.Add(time.Minute), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if n, ok, err := s.TakeAttempt(ctx, "c1", 3); err != nil || !ok || n != 1 {
		t.Errorf("attempt %d %v %v", n, ok, err)
	}
	if n, ok, err := s.TakeAttempt(ctx, "c1", 3); err != nil || !ok || n != 2 {
		t.Errorf("attempt %d %v %v", n, ok, err)
	}
	if n, ok, err := s.TakeAttempt(ctx, "c1", 3); err != nil || !ok || n != 3 {
		t.Errorf("last attempt %d %v %v", n, ok, err)
	}
	if _, ok, err := s.TakeAttempt(ctx, "c1", 3); err != nil || ok {
		t.Errorf("a fourth attempt was given: %v %v", ok, err)
	}
	if _, _, err := s.TakeAttempt(ctx, "nope", 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("attempt on a missing challenge: %v", err)
	}
	if ok, _ := s.ConsumeChallenge(ctx, "c1"); !ok {
		t.Error("first consume")
	}
	if ok, _ := s.ConsumeChallenge(ctx, "c1"); ok {
		t.Error("second consume")
	}

	// spaces: slug unique, owner added, roles
	sp := &Space{Slug: "acme", Name: "Acme", CreatedBy: u.ID}
	if err := s.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSpace(ctx, &Space{Slug: "acme", Name: "Other", CreatedBy: u.ID}); !IsUnique(err) {
		t.Errorf("duplicate slug: %v", err)
	}
	if role, _ := s.Role(ctx, sp.ID, u.ID); role != RoleOwner {
		t.Errorf("role %q", role)
	}
	if role, _ := s.Role(ctx, sp.ID, "stranger"); role != "" {
		t.Errorf("stranger role %q", role)
	}
	list, err := s.SpacesByUser(ctx, u.ID)
	if err != nil || len(list) != 1 || list[0].Slug != "acme" || list[0].Role != RoleOwner || list[0].OnboardingStep != "machines" {
		t.Errorf("spaces %+v %v", list, err)
	}

	// the SSH key of a space: made once (the second maker loses), replaced on purpose, gone with the space
	if _, err := s.SpaceSSHKeyBySpace(ctx, sp.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a key before one was made: %v", err)
	}
	if made, err := s.CreateSpaceSSHKey(ctx, SpaceSSHKey{SpaceID: sp.ID, PublicKey: "ssh-ed25519 AAAA one", PrivateKeyEnc: "enc1"}); err != nil || !made {
		t.Errorf("first key: %v %v", made, err)
	}
	if made, err := s.CreateSpaceSSHKey(ctx, SpaceSSHKey{SpaceID: sp.ID, PublicKey: "ssh-ed25519 AAAA two", PrivateKeyEnc: "enc2"}); err != nil || made {
		t.Errorf("second key: %v %v", made, err)
	}
	if k, err := s.SpaceSSHKeyBySpace(ctx, sp.ID); err != nil || k.PublicKey != "ssh-ed25519 AAAA one" || k.PrivateKeyEnc != "enc1" {
		t.Errorf("key = %+v %v", k, err)
	}
	if err := s.ReplaceSpaceSSHKey(ctx, SpaceSSHKey{SpaceID: sp.ID, PublicKey: "ssh-ed25519 AAAA three", PrivateKeyEnc: "enc3"}); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.SpaceSSHKeyBySpace(ctx, sp.ID); k.PrivateKeyEnc != "enc3" {
		t.Errorf("replaced key = %+v", k)
	}

	// invites: max uses and revoke
	two := 2
	iv := &Invite{SpaceID: sp.ID, Kind: "link", TokenHash: "t1", Role: RoleMember, MaxUses: &two, ExpiresAt: now.Add(time.Hour), CreatedBy: u.ID}
	if err := s.CreateInvite(ctx, iv); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		other := &User{Email: "u" + string(rune('a'+i)) + "@example.com"}
		if err := s.CreateUser(ctx, other); err != nil {
			t.Fatal(err)
		}
		ok, err := s.UseInvite(ctx, iv.ID, other.ID, now)
		if err != nil || ok != (i < 2) {
			t.Errorf("use %d: %v %v", i, ok, err)
		}
	}
	active, _ := s.ActiveInvites(ctx, sp.ID, now)
	if len(active) != 0 {
		t.Errorf("used-up invite is still active: %+v", active)
	}

	// machines: key unique, unclaimed local, claim once, revoke once
	m := &Machine{Profile: "dir", Via: "local", KeyPub: "pub1", EncPub: "enc1", Name: "box"}
	if err := s.CreateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMachine(ctx, &Machine{Profile: "dir", Via: "command", KeyPub: "pub1", EncPub: "e"}); !IsUnique(err) {
		t.Errorf("duplicate key: %v", err)
	}
	local, err := s.UnclaimedLocalMachine(ctx)
	if err != nil || local.ID != m.ID || local.SpaceID != "" {
		t.Errorf("local: %+v %v", local, err)
	}
	if ok, _ := s.ClaimMachine(ctx, m.ID, sp.ID, u.ID); !ok {
		t.Error("claim")
	}
	if ok, _ := s.ClaimMachine(ctx, m.ID, sp.ID, u.ID); ok {
		t.Error("claimed twice")
	}
	if _, err := s.UnclaimedLocalMachine(ctx); !IsNotFound(err) {
		t.Errorf("claimed machine is still unclaimed: %v", err)
	}
	list2, _ := s.MachinesBySpace(ctx, sp.ID)
	if len(list2) != 1 || list2[0].Info != "{}" {
		t.Errorf("machines %+v", list2)
	}
	if err := s.UpsertAgentConnection(ctx, AgentConnection{MachineID: m.ID, Provider: "claude", Installed: true, LoginState: "logged_in"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAgentConnection(ctx, AgentConnection{MachineID: m.ID, Provider: "claude", Installed: true, LoginState: "logged_out"}); err != nil {
		t.Fatal(err)
	}
	conns, _ := s.AgentConnections(ctx, m.ID)
	if len(conns) != 1 || conns[0].LoginState != "logged_out" {
		t.Errorf("connections %+v", conns)
	}
	if err := s.RecordEnv(ctx, m.ID, "GH_TOKEN", u.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEnv(ctx, m.ID, "GH_TOKEN", u.ID, now); err != nil {
		t.Fatal(err)
	}
	if names, _ := s.EnvNames(ctx, m.ID); len(names) != 1 || names[0] != "GH_TOKEN" {
		t.Errorf("env %v", names)
	}
	if ok, _ := s.RevokeMachine(ctx, m.ID, now); !ok {
		t.Error("revoke")
	}
	if ok, _ := s.RevokeMachine(ctx, m.ID, now); ok {
		t.Error("revoked twice")
	}

	// registration codes are used once
	code := RegCode{CodeHash: "rc1", SpaceID: sp.ID, Profile: "dir", ExpiresAt: now.Add(30 * time.Minute)}
	if err := s.CreateRegCode(ctx, code); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.ConsumeRegCode(ctx, "rc1", "m1", now); !ok {
		t.Error("first use")
	}
	if ok, _ := s.ConsumeRegCode(ctx, "rc1", "m2", now); ok {
		t.Error("second use")
	}

	// a transaction rolls back
	errBoom := errors.New("boom")
	err = db.Tx(ctx, func(s Store) error {
		if err := s.CreateUser(ctx, &User{Email: "rolled@example.com"}); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Errorf("tx error: %v", err)
	}
	if _, err := s.UserByEmail(ctx, "rolled@example.com"); !IsNotFound(err) {
		t.Errorf("rolled back user exists: %v", err)
	}

	// deleting a user takes their credentials with them (cascade)
	if err := s.WipeUserCredentials(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.LoginMethods(ctx, u.ID); n != 0 {
		t.Errorf("methods after wipe %d", n)
	}
	if _, err := s.SessionByHash(ctx, "h1"); !IsNotFound(err) {
		t.Errorf("session after wipe: %v", err)
	}
}

func TestSQLite(t *testing.T)   { exercise(t, openTest(t)) }
func TestPostgres(t *testing.T) { exercise(t, openPostgres(t)) }

func TestPurgeExpired(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	s := db.S()
	u := &User{Email: "a@example.com"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, Session{TokenHash: "old", Kind: "hub", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(-time.Minute), LastSeenAt: now})
	_ = s.CreateSession(ctx, Session{TokenHash: "new", Kind: "hub", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Minute), LastSeenAt: now})
	_ = s.CreateChallenge(ctx, Challenge{ID: "old", Kind: "email", ExpiresAt: now.Add(-time.Minute), CreatedAt: now})
	if err := s.PurgeExpired(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByHash(ctx, "old"); !IsNotFound(err) {
		t.Error("expired session survived")
	}
	if _, err := s.SessionByHash(ctx, "new"); err != nil {
		t.Error("live session was purged")
	}
	if _, err := s.ChallengeByID(ctx, "old"); !IsNotFound(err) {
		t.Error("expired challenge survived")
	}
}

// Parallel requests that all see an unspent challenge must still share its tries between them.
func takeAttemptsInParallel(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.CreateChallenge(ctx, Challenge{ID: "race", Kind: "email", Email: "r@example.com", CodeHash: "x", ExpiresAt: now.Add(time.Minute), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	const workers, max = 40, 5
	var mu sync.Mutex
	taken, seen := 0, map[int]bool{}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, ok, err := db.S().TakeAttempt(ctx, "race", max)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				mu.Lock()
				taken++
				seen[n] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if taken != max || len(seen) != max {
		t.Errorf("%d tries were given out (%d distinct counts), want %d", taken, len(seen), max)
	}
}

func TestTakeAttemptIsAtomicSQLite(t *testing.T)   { takeAttemptsInParallel(t, openTest(t)) }
func TestTakeAttemptIsAtomicPostgres(t *testing.T) { takeAttemptsInParallel(t, openPostgres(t)) }

// The first account is the one that finds the table empty. Under READ COMMITTED two transactions that count and then
// insert would both read zero; the account lock makes the second wait for the first and see its row.
func TestLockAccountCreationSerialisesTheFirstAccount(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *DB{"sqlite": openTest, "postgres": openPostgres} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			const racers = 6
			var wg sync.WaitGroup
			firsts := make(chan string, racers)
			errs := make(chan error, racers)
			for i := 0; i < racers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					email := fmt.Sprintf("u%d@example.com", i)
					errs <- db.Tx(ctx, func(s Store) error {
						if err := s.LockAccountCreation(ctx); err != nil {
							return err
						}
						n, err := s.CountUsers(ctx)
						if err != nil {
							return err
						}
						// the window a race needs: time between the count and the insert
						time.Sleep(30 * time.Millisecond)
						if n == 0 {
							firsts <- email
						}
						return s.CreateUser(ctx, &User{Email: email, IsAdmin: n == 0})
					})
				}(i)
			}
			wg.Wait()
			close(firsts)
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var got []string
			for e := range firsts {
				got = append(got, e)
			}
			if len(got) != 1 {
				t.Fatalf("%d transactions found the table empty: %v", len(got), got)
			}
			var admins int
			if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&admins); err != nil || admins != 1 {
				t.Fatalf("administrators: %d %v", admins, err)
			}
		})
	}
	// outside a transaction there is nothing to hold the lock for
	if err := openTest(t).S().LockAccountCreation(context.Background()); err == nil {
		t.Error("the lock outside a transaction was granted")
	}
}

// The database file is private whatever the umask: SQLite alone makes it 0644 under the usual 022.
func TestSQLiteFileIsPrivate(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "hub.db")
	db, err := Open(context.Background(), "sqlite://"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, f := range []string{path, path + "-wal"} {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if mode := st.Mode().Perm(); mode != 0o600 {
			t.Fatalf("%s: mode %o, want 600", filepath.Base(f), mode)
		}
	}
}
