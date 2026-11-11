package store

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"
)

// onEachDatabase runs a test on SQLite and, where HUB_TEST_POSTGRES_URL names one, on Postgres. The tables are read
// from the migrated database itself, not from the snapshots of the repository.
func onEachDatabase(t *testing.T, fn func(t *testing.T, db *DB, schema Schema)) {
	for name, open := range map[string]func(*testing.T) *DB{"sqlite": openTest, "postgres": openPostgres} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			schema, err := IntrospectSchema(context.Background(), db, db.Driver())
			if err != nil {
				t.Fatal(err)
			}
			if len(schema.Tables) == 0 {
				t.Fatal("the migrated database shows no tables")
			}
			fn(t, db, schema)
		})
	}
}

// Every table is either deleted with its space or says why it is kept, and a table with a space_id is always deleted.
func TestSpaceDeleteNamesEveryTable(t *testing.T) {
	inCascade := map[string]bool{}
	for _, step := range SpaceCascade {
		if inCascade[step.Table] {
			t.Errorf("%s is in the cascade twice", step.Table)
		}
		inCascade[step.Table] = true
	}
	onEachDatabase(t, func(t *testing.T, _ *DB, s Schema) {
		var missing []string
		for name, tbl := range s.Tables {
			_, kept := keptOnSpaceDelete[name]
			if kept && inCascade[name] {
				t.Errorf("%s is both deleted with a space and kept", name)
			}
			if !kept && !inCascade[name] {
				missing = append(missing, name)
			}
			for _, c := range tbl.Columns {
				if c.Name == "space_id" && !inCascade[name] {
					t.Errorf("%s has a space_id and is not deleted with its space", name)
				}
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("tables a space's deletion does not know: %v. Add a step to SpaceCascade, or a line to keptOnSpaceDelete that says why the rows stay", missing)
		}
		for name := range inCascade {
			if _, ok := s.Tables[name]; !ok {
				t.Errorf("the cascade names %s, which is not a table of this database", name)
			}
		}
		for name := range keptOnSpaceDelete {
			if _, ok := s.Tables[name]; !ok {
				t.Errorf("%s is listed as kept and is not a table of this database", name)
			}
		}
	})
}

// A deleted space leaves no row behind in any table of the cascade, and takes nothing of another space.
func TestDeleteSpaceDataLeavesNoRow(t *testing.T) {
	onEachDatabase(t, deleteSpaceDataLeavesNoRow)
}

func deleteSpaceDataLeavesNoRow(t *testing.T, db *DB, schema Schema) {
	ctx := context.Background()
	s := db.S()
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		must(s.q.QueryRow(ctx, q, args...).Scan(&n))
		return n
	}
	counts := func() map[string]int {
		out := map[string]int{}
		for name := range schema.Tables {
			out[name] = count(`SELECT COUNT(*) FROM ` + name)
		}
		return out
	}

	owner, guest := &User{Email: "owner@example.com"}, &User{Email: "guest@example.com"}
	must(s.CreateUser(ctx, owner))
	must(s.CreateUser(ctx, guest))

	// The neighbour: a space of the same people that must come through untouched.
	kept := &Space{Slug: "south", Name: "South", CreatedBy: owner.ID}
	must(s.CreateSpace(ctx, kept))
	must(s.AddMember(ctx, kept.ID, guest.ID, RoleMember))
	keptTeam := &Team{SpaceID: kept.ID, Slug: "core", Name: "Core", CreatedBy: owner.ID}
	must(s.CreateTeam(ctx, keptTeam))
	must(s.AddTeamMember(ctx, keptTeam.ID, guest.ID, false, now))
	keptMachine := &Machine{SpaceID: kept.ID, Name: "desk", KeyPub: "south-desk", CreatedBy: owner.ID}
	must(s.CreateMachine(ctx, keptMachine))
	must(s.SetMachineAccess(ctx, keptMachine.ID, []MachineAccessEntry{{Kind: AccessUser, RefID: guest.ID}}, now))
	_, err := s.Grant(ctx, RoleGrant{UserID: guest.ID, ScopeKind: ScopeTenant, ScopeID: kept.ID, Role: "admin", GrantedBy: owner.ID})
	must(err)
	must(s.AddAudit(ctx, &AuditEvent{SpaceID: kept.ID, ActorID: owner.ID, ScopeKind: ScopeTenant, ScopeID: kept.ID, Action: "member.add", TargetKind: "user", TargetID: guest.ID}))
	for _, q := range []string{
		`INSERT INTO sessions (token_hash, kind, user_id, machine_id, created_at, expires_at, last_seen_at) VALUES ('south-run', 'run', '` + guest.ID + `', '` + keptMachine.ID + `', 1, 2, 1)`,
		`INSERT INTO resource_access_people (kind, id, email, role, granted_at, space_id) VALUES ('doc', 'south-doc', 'guest@example.com', 'viewer', 1, '` + kept.ID + `')`,
		`INSERT INTO limit_rules (id, org_key, scope_kind, scope_id, user_id, period, unit, set_at) VALUES ('south-limit', 'space:` + kept.ID + `', 'tenant', '` + kept.ID + `', '` + guest.ID + `', 'day', 'usd', 1)`,
		`INSERT INTO env_person_group (user_id, scope, group_id, turned_on, at) VALUES ('` + guest.ID + `', '` + kept.ID + `', 'g', 1, 1)`,
	} {
		_, err := s.q.Exec(ctx, q)
		must(err)
	}
	base := counts()

	// The space that goes: one row in every table of the cascade, each tied to the space the way the table ties its rows.
	gone := &Space{Slug: "north", Name: "North", CreatedBy: owner.ID}
	must(s.CreateSpace(ctx, gone))
	userish := map[string]bool{"user_id": true, "created_by": true, "owner_id": true, "actor_id": true, "granted_by": true, "set_by": true,
		"updated_by": true, "decided_by": true, "author_id": true, "addressee_id": true, "person_id": true, "created_by_id": true,
		"connected_by": true, "delivered_by": true, "owner_user_id": true}
	var named func(table string, c Column) (any, bool)
	refs := map[string]string{"machine_id": "machines-id", "team_id": "teams-id", "invite_id": "invites-id", "site_id": "sites-id",
		"thread_id": "doc_threads-id", "comment_id": "doc_comments-id", "account_id": "accounts-id", "rule_id": "automation_rules-id",
		"item_id": "catalog_items-id", "slug": "north-doc", "org_key": "space:" + gone.ID, "scope_kind": "tenant", "scope_id": gone.ID,
		"layer": "tenant", "owner_layer": "tenant", "owner_scope": gone.ID, "scope": gone.ID, "space_id": gone.ID, "space": gone.Slug}
	value := func(table string, c Column) (any, bool) {
		v, ok := named(table, c)
		if !ok {
			return nil, false
		}
		// A name that usually holds an id is a number or a flag in some tables: the column's type decides.
		typ := strings.ToUpper(c.Type)
		if _, isText := v.(string); isText && !strings.Contains(typ, "TEXT") && !strings.Contains(typ, "CHAR") {
			if strings.Contains(typ, "BOOL") {
				return true, true
			}
			return 1, true
		}
		return v, true
	}
	named = func(table string, c Column) (any, bool) {
		switch {
		case c.Name == "id":
			return table + "-id", true
		case c.Name == "link_id" && strings.HasPrefix(table, "design_"):
			return "design_share_links-id", true
		case c.Name == "link_id":
			return "north-link", true
		case userish[c.Name]:
			return owner.ID, true
		}
		if v, ok := refs[c.Name]; ok {
			return v, true
		}
		if !c.NotNull {
			return nil, false
		}
		typ := strings.ToUpper(c.Type)
		switch {
		case strings.Contains(typ, "BOOL"):
			return true, true
		case strings.Contains(typ, "INT"), strings.Contains(typ, "REAL"), strings.Contains(typ, "DOUBLE"), strings.Contains(typ, "NUM"):
			return 1, true
		case strings.Contains(typ, "BLOB"), strings.Contains(typ, "BYTEA"):
			return []byte{1}, true
		}
		return "v-" + c.Name, true
	}
	for i := len(SpaceCascade) - 1; i >= 0; i-- {
		table := SpaceCascade[i].Table
		if count(`SELECT COUNT(*) FROM `+table) > base[table] {
			continue // made with the space
		}
		var cols, marks []string
		var args []any
		for _, c := range schema.Tables[table].Columns {
			if v, ok := value(table, c); ok {
				cols, marks, args = append(cols, c.Name), append(marks, "?"), append(args, v)
			}
		}
		if _, err := s.q.Exec(ctx, `INSERT INTO `+table+` (`+strings.Join(cols, ", ")+`) VALUES (`+strings.Join(marks, ", ")+`)`, args...); err != nil {
			t.Fatalf("a row for %s: %v", table, err)
		}
	}
	filled := counts()
	for _, step := range SpaceCascade {
		if filled[step.Table] <= base[step.Table] {
			t.Fatalf("%s got no row of the space to delete", step.Table)
		}
	}

	var removed map[string]int64
	must(db.Tx(ctx, func(st Store) error {
		var err error
		removed, err = st.DeleteSpaceData(ctx, *gone)
		return err
	}))
	after := counts()
	for name := range schema.Tables {
		if after[name] != base[name] {
			t.Errorf("%s: %d rows after the deletion, %d before the space was made", name, after[name], base[name])
		}
		for _, c := range schema.Tables[name].Columns {
			if c.Name == "space_id" {
				if n := count(`SELECT COUNT(*) FROM `+name+` WHERE space_id = ?`, gone.ID); n != 0 {
					t.Errorf("%s still holds %d rows of the deleted space", name, n)
				}
			}
		}
	}
	for _, step := range SpaceCascade {
		if removed[step.Table] == 0 {
			t.Errorf("the deletion reports no row removed from %s", step.Table)
		}
	}
	if _, err := s.SpaceByID(ctx, gone.ID); !IsNotFound(err) {
		t.Fatalf("the space is still there: %v", err)
	}
}

// A read by an administrator of the installation is written once per window, however often it repeats.
func TestOperatorReadCollapses(t *testing.T) {
	onEachDatabase(t, operatorReadCollapses)
}

func operatorReadCollapses(t *testing.T, db *DB, _ Schema) {
	ctx := context.Background()
	s := db.S()
	at := time.Now().UTC()
	line := func(at time.Time, target string) bool {
		t.Helper()
		wrote, err := s.AddOperatorRead(ctx, &AuditEvent{ActorID: "admin", ScopeKind: ScopeTenant, ScopeID: "sp", SpaceID: "sp",
			Action: "operator.read", TargetKind: "tenant", TargetID: target, At: at}, at.Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return wrote
	}
	if !line(at, "sp") || line(at.Add(time.Minute), "sp") {
		t.Fatal("a repeat within the window must not write a second line")
	}
	if !line(at.Add(time.Minute), "other") {
		t.Fatal("another target is a line of its own")
	}
	if !line(at.Add(2*time.Hour), "sp") {
		t.Fatal("a read after the window is a new line")
	}
}
