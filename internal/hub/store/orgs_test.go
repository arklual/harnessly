package store

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestOrgsSQLite(t *testing.T)   { exerciseOrgs(t, openTest(t)) }
func TestOrgsPostgres(t *testing.T) { exerciseOrgs(t, openPostgres(t)) }

func exerciseOrgs(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	var version int
	if err := db.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version < 4 {
		t.Fatalf("schema version %d, %v", version, err)
	}

	alice := &User{Email: "alice@example.com", Name: "Alice"}
	bob := &User{Email: "bob@example.com"}
	for _, u := range []*User{alice, bob} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	// A space made the old way has no organization and keeps working.
	old := &Space{Slug: "old-team", Name: "Old", CreatedBy: alice.ID}
	if err := s.CreateSpace(ctx, old); err != nil {
		t.Fatal(err)
	}
	if got, err := s.SpaceByID(ctx, old.ID); err != nil || got.OrgID != "" {
		t.Fatalf("old space %+v, %v", got, err)
	}
	m := &Machine{SpaceID: old.ID, Profile: "dir", Via: "command", KeyPub: "k1", EncPub: "e1", CreatedBy: alice.ID}
	if err := s.CreateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}
	if got, err := s.MachineByID(ctx, m.ID); err != nil || got.TeamID != "" || got.DisabledAt != nil {
		t.Fatalf("machine %+v, %v", got, err)
	}

	org := &Org{Slug: "acme", Name: "Acme", CreatedBy: alice.ID}
	if err := s.CreateOrg(ctx, org); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOrg(ctx, &Org{Slug: "acme", Name: "Again"}); !IsUnique(err) {
		t.Fatalf("taken org slug: %v", err)
	}
	if got, err := s.OrgBySlug(ctx, "acme"); err != nil || got.Timezone != "Europe/Moscow" || got.Currency != "RUB" || got.CreatedBy != alice.ID {
		t.Fatalf("org %+v, %v", got, err)
	}
	if err := s.SetSpaceOrg(ctx, old.ID, org.ID); err != nil {
		t.Fatal(err)
	}
	if spaces, err := s.SpacesByOrg(ctx, org.ID); err != nil || len(spaces) != 1 || spaces[0].OrgID != org.ID {
		t.Fatalf("tenants %+v, %v", spaces, err)
	}
	if mine, err := s.SpacesByUser(ctx, alice.ID); err != nil || len(mine) != 1 || mine[0].OrgID != org.ID {
		t.Fatalf("alice's spaces %+v, %v", mine, err)
	}

	team := &Team{SpaceID: old.ID, Slug: "general", Name: "Общая", CreatedBy: alice.ID}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTeam(ctx, &Team{SpaceID: old.ID, Slug: "general", Name: "x"}); !IsUnique(err) {
		t.Fatalf("taken team slug: %v", err)
	}
	second := &Team{SpaceID: old.ID, Slug: "second", Name: "Вторая"}
	if err := s.CreateTeam(ctx, second); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, tm := range []string{team.ID, second.ID} {
		if err := s.AddTeamMember(ctx, tm, bob.ID, false, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddTeamMember(ctx, team.ID, bob.ID, true, now); err != nil {
		t.Fatalf("adding twice: %v", err)
	}
	if err := s.SetPrimaryTeam(ctx, bob.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrimaryTeam(ctx, bob.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	ms, err := s.TeamsOfUser(ctx, bob.ID)
	if err != nil || len(ms) != 2 {
		t.Fatalf("bob's teams %+v, %v", ms, err)
	}
	primaries := 0
	for _, x := range ms {
		if x.IsPrimary {
			primaries++
			if x.TeamID != team.ID {
				t.Errorf("primary team %s, want %s", x.TeamID, team.ID)
			}
		}
	}
	if primaries != 1 {
		t.Errorf("%d primary teams, want 1", primaries)
	}

	if ok, err := s.Grant(ctx, RoleGrant{UserID: alice.ID, ScopeKind: ScopeOrg, ScopeID: org.ID, Role: RoleAdmin}); err != nil || !ok {
		t.Fatalf("grant %v %v", ok, err)
	}
	if ok, err := s.Grant(ctx, RoleGrant{UserID: alice.ID, ScopeKind: ScopeOrg, ScopeID: org.ID, Role: RoleAdmin}); err != nil || ok {
		t.Fatalf("grant twice %v %v", ok, err)
	}
	if _, err := s.Grant(ctx, RoleGrant{UserID: bob.ID, ScopeKind: ScopeTeam, ScopeID: team.ID, Role: RoleLead, GrantedBy: alice.ID}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountGrants(ctx, ScopeOrg, org.ID, RoleAdmin); err != nil || n != 1 {
		t.Fatalf("admins %d %v", n, err)
	}
	if orgs, err := s.OrgsForUser(ctx, bob.ID); err != nil || len(orgs) != 0 {
		t.Fatalf("bob is in no tenant yet: %+v %v", orgs, err)
	}
	if err := s.AddMember(ctx, old.ID, bob.ID, RoleMember); err != nil {
		t.Fatal(err)
	}
	if orgs, err := s.OrgsForUser(ctx, bob.ID); err != nil || len(orgs) != 1 {
		t.Fatalf("bob's orgs %+v %v", orgs, err)
	}
	if ok, err := s.Revoke(ctx, bob.ID, ScopeTeam, team.ID, RoleLead); err != nil || !ok {
		t.Fatalf("revoke %v %v", ok, err)
	}

	// The journal: a team's event is seen from the team, its tenant and its organization; a neighbour team sees nothing.
	at := time.UnixMilli(1_700_000_000_000)
	for i, e := range []AuditEvent{
		{OrgID: org.ID, ScopeKind: ScopeOrg, ScopeID: org.ID, Action: "org.create", ActorID: alice.ID, After: `{"name":"Acme"}`, At: at},
		{OrgID: org.ID, SpaceID: old.ID, TeamID: team.ID, ScopeKind: ScopeTeam, ScopeID: team.ID, Action: "grant.add", TargetKind: "user", TargetID: bob.ID, After: `{"role":"lead"}`, At: at.Add(time.Second)},
	} {
		if err := s.AddAudit(ctx, &e); err != nil {
			t.Fatalf("audit %d: %v", i, err)
		}
	}
	for _, tc := range []struct {
		f    AuditFilter
		want int
	}{
		{AuditFilter{OrgID: org.ID}, 2},
		{AuditFilter{SpaceID: old.ID}, 1},
		{AuditFilter{TeamID: team.ID}, 1},
		{AuditFilter{TeamID: second.ID}, 0},
		{AuditFilter{OrgID: org.ID, Before: at.Add(time.Second)}, 1},
	} {
		got, err := s.Audit(ctx, tc.f)
		if err != nil || len(got) != tc.want {
			t.Errorf("audit %+v: %d events, %v; want %d", tc.f, len(got), err, tc.want)
		}
	}
	if got, _ := s.Audit(ctx, AuditFilter{TeamID: team.ID}); len(got) == 1 && (got[0].After != `{"role":"lead"}` || got[0].Before != "" || got[0].Self) {
		t.Errorf("event %+v", got[0])
	}

	if err := s.SetMachineTeam(ctx, m.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	off := time.UnixMilli(1_700_000_100_000).UTC()
	if err := s.SetMachineDisabled(ctx, m.ID, &off); err != nil {
		t.Fatal(err)
	}
	if got, err := s.MachineByID(ctx, m.ID); err != nil || got.TeamID != team.ID || got.DisabledAt == nil || !got.DisabledAt.Equal(off) {
		t.Fatalf("machine %+v, %v", got, err)
	}
	// Deleting a team frees its machines rather than taking them along.
	if err := s.DeleteTeam(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.MachineByID(ctx, m.ID); err != nil || got.TeamID != "" {
		t.Fatalf("machine after its team went %+v, %v", got, err)
	}
}

// TestMigrationsAreAdditive guards what lets the previous binary run on a migrated database: from 0004 on a migration
// only creates tables and indexes and adds nullable columns. A migration that also moves data is listed in moves with
// the statements it may run: each of them leaves what the previous binary reads as it was.
func TestMigrationsAreAdditive(t *testing.T) {
	moves := map[string]*regexp.Regexp{
		// Fills the column the migration adds.
		"0049_resource_space.sql": regexp.MustCompile(`(?is)^UPDATE \w+ SET space_id = `),
		// Fills the tables the migration creates, and takes the tie to a server off the machines that named a server
		// another machine had first (they are kept in machine_resource_conflicts): the unique index needs it.
		// Fills the columns the migration adds.
		"0051_access_by_person.sql": regexp.MustCompile(`(?is)^UPDATE resource_access_(people|requests) SET user_id = `),
		"0050_provider_servers.sql": regexp.MustCompile(`(?is)^(INSERT INTO (machine_resource_conflicts|provider_servers) |UPDATE machine_resources SET external_id = '' WHERE machine_id IN \(SELECT machine_id FROM machine_resource_conflicts\))`),
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	allowed := regexp.MustCompile(`(?is)^(CREATE TABLE|CREATE INDEX|CREATE UNIQUE INDEX|ALTER TABLE \w+ ADD COLUMN \w+ \w+( REFERENCES [^;]*)?;?$|INSERT INTO prices )`)
	notNull := regexp.MustCompile(`(?i)ALTER TABLE .* NOT NULL`)
	for _, e := range entries {
		if e.Name() < "0004" {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range splitStatements(string(body)) {
			if move := moves[e.Name()]; move != nil && move.MatchString(stmt) {
				continue
			}
			if !allowed.MatchString(stmt) || notNull.MatchString(stmt) {
				t.Errorf("%s: not additive: %.80s", e.Name(), stmt)
			}
		}
	}
}

// TestNoSelectStar: every query names its columns, so that a column added by a later migration does not move what an
// older binary scans.
func TestNoSelectStar(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	star := regexp.MustCompile(`(?i)SELECT\s+\*`)
	for _, f := range files {
		if filepath.Ext(f) != ".go" || regexp.MustCompile(`_test\.go$`).MatchString(f) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if star.Match(b) {
			t.Errorf("%s uses SELECT *", f)
		}
	}
}
