package store

import (
	"context"
	"testing"
	"time"
)

// A person who leaves a space keeps nothing of it: no role, no team, no line in a machine's list, no limit, no role in
// its documents, no session on its machines, and the machines they registered are revoked. What they hold in another
// space, and what the others hold in this one, stays.
func TestRemoveMemberTakesWhatTheSpaceGave(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	s := db.S()
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := s.q.Exec(ctx, q, args...)
		must(err)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		must(s.q.QueryRow(ctx, q, args...).Scan(&n))
		return n
	}
	owner, guest := &User{Email: "owner@example.com"}, &User{Email: "guest@example.com"}
	must(s.CreateUser(ctx, owner))
	must(s.CreateUser(ctx, guest))
	type world struct {
		space          *Space
		team           *Team
		shared, laptop *Machine
		ownersOwn      *Machine
	}
	build := func(slug string) world {
		w := world{space: &Space{Slug: slug, Name: slug, CreatedBy: owner.ID}}
		must(s.CreateSpace(ctx, w.space))
		must(s.AddMember(ctx, w.space.ID, guest.ID, RoleMember))
		w.team = &Team{SpaceID: w.space.ID, Slug: "core", Name: "Core", CreatedBy: owner.ID}
		must(s.CreateTeam(ctx, w.team))
		for _, u := range []*User{owner, guest} {
			must(s.AddTeamMember(ctx, w.team.ID, u.ID, false, now))
			for _, sc := range [][2]string{{ScopeTenant, w.space.ID}, {ScopeTeam, w.team.ID}} {
				_, err := s.Grant(ctx, RoleGrant{UserID: u.ID, ScopeKind: sc[0], ScopeID: sc[1], Role: "admin", GrantedBy: owner.ID})
				must(err)
				exec(`INSERT INTO limit_rules (id, org_key, scope_kind, scope_id, user_id, period, unit, set_at) VALUES (?, ?, ?, ?, ?, 'day', 'usd', 1)`,
					NewID(), w.space.ID, sc[0], sc[1], u.ID)
			}
		}
		w.shared = &Machine{SpaceID: w.space.ID, Name: "shared", KeyPub: slug + "-shared", CreatedBy: owner.ID}
		w.ownersOwn = &Machine{SpaceID: w.space.ID, Name: "desk", KeyPub: slug + "-desk", CreatedBy: owner.ID}
		w.laptop = &Machine{SpaceID: w.space.ID, Name: "laptop", KeyPub: slug + "-laptop", CreatedBy: guest.ID}
		for _, m := range []*Machine{w.shared, w.ownersOwn, w.laptop} {
			must(s.CreateMachine(ctx, m))
		}
		must(s.SetMachineAccess(ctx, w.shared.ID, []MachineAccessEntry{{Kind: AccessUser, RefID: guest.ID}, {Kind: AccessUser, RefID: owner.ID}}, now))
		for i, u := range []*User{owner, guest} {
			exec(`INSERT INTO sessions (token_hash, kind, user_id, machine_id, created_at, expires_at, last_seen_at) VALUES (?, 'run', ?, ?, 1, 2, 1)`,
				slug+u.ID, u.ID, w.shared.ID)
			exec(`INSERT INTO resource_access_people (kind, id, email, role, granted_at, space_id, user_id) VALUES ('doc', ?, ?, 'viewer', 1, ?, ?)`,
				slug+"-held", u.Email, w.space.ID, u.ID)
			exec(`INSERT INTO resource_access_people (kind, id, email, role, granted_at, space_id) VALUES ('doc', ?, ?, 'viewer', 1, ?)`,
				slug+"-invited", u.Email, w.space.ID)
			exec(`INSERT INTO resource_access_requests (request_id, kind, id, email, level, created_at, space_id, user_id) VALUES (?, 'doc', 'held', ?, 'viewer', 1, ?, ?)`,
				NewID(), u.Email, w.space.ID, []any{owner.ID, guest.ID}[i])
		}
		return w
	}
	left, kept := build("north"), build("south")

	var revoked []Machine
	must(db.Tx(ctx, func(st Store) error {
		if err := st.RemoveMember(ctx, left.space.ID, guest.ID); err != nil {
			return err
		}
		var err error
		revoked, err = st.RevokeMachinesOf(ctx, left.space.ID, guest.ID, now)
		return err
	}))
	if len(revoked) != 1 || revoked[0].ID != left.laptop.ID {
		t.Fatalf("revoked machines: %+v", revoked)
	}
	// what one person holds in one space, by table
	held := func(u *User, w world) map[string]int {
		return map[string]int{
			"memberships":  count(`SELECT COUNT(*) FROM memberships WHERE space_id = ? AND user_id = ?`, w.space.ID, u.ID),
			"role_grants":  count(`SELECT COUNT(*) FROM role_grants WHERE user_id = ? AND scope_id IN (?, ?)`, u.ID, w.space.ID, w.team.ID),
			"team_members": count(`SELECT COUNT(*) FROM team_members WHERE user_id = ? AND team_id = ?`, u.ID, w.team.ID),
			"access":       count(`SELECT COUNT(*) FROM machine_access WHERE ref_id = ? AND machine_id = ?`, u.ID, w.shared.ID),
			"limits":       count(`SELECT COUNT(*) FROM limit_rules WHERE user_id = ? AND scope_id IN (?, ?)`, u.ID, w.space.ID, w.team.ID),
			"people":       count(`SELECT COUNT(*) FROM resource_access_people WHERE space_id = ? AND email = ?`, w.space.ID, u.Email),
			"requests":     count(`SELECT COUNT(*) FROM resource_access_requests WHERE space_id = ? AND email = ?`, w.space.ID, u.Email),
			"sessions":     count(`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND machine_id = ?`, u.ID, w.shared.ID),
		}
	}
	full := map[string]int{"memberships": 1, "role_grants": 2, "team_members": 1, "access": 1, "limits": 2, "people": 2, "requests": 1, "sessions": 1}
	for name, tc := range map[string]struct {
		u    *User
		w    world
		want int // 0: nothing is left, 1: everything is
	}{
		"the leaver, the space left":     {guest, left, 0},
		"the leaver, the other space":    {guest, kept, 1},
		"the other person, either space": {owner, left, 1},
	} {
		for table, n := range held(tc.u, tc.w) {
			if want := full[table] * tc.want; n != want {
				t.Errorf("%s: %s has %d rows, want %d", name, table, n, want)
			}
		}
	}
	for name, tc := range map[string]struct {
		m       *Machine
		revoked bool
	}{
		"the leaver's machine":              {left.laptop, true},
		"the leaver's machine in the other": {kept.laptop, false},
		"another person's machine":          {left.ownersOwn, false},
		"the shared machine":                {left.shared, false},
	} {
		m, err := s.MachineByID(ctx, tc.m.ID)
		must(err)
		if (m.RevokedAt != nil) != tc.revoked {
			t.Errorf("%s: revoked %v, want %v", name, m.RevokedAt != nil, tc.revoked)
		}
	}
	if ms, err := s.MachinesBySpace(ctx, left.space.ID); err != nil || len(ms) != 2 {
		t.Errorf("the space left lists %d machines (%v), want the two that stay", len(ms), err)
	}
}

// Switching an account off ends every session it holds; ending the other sessions keeps the one named.
func TestDisabledAccountHoldsNoSession(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	s := db.S()
	now := time.Now().UTC()
	u := &User{Email: "person@example.com"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	open := func(hashes ...string) {
		t.Helper()
		for _, h := range hashes {
			if err := s.CreateSession(ctx, Session{TokenHash: h, Kind: "hub", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}); err != nil {
				t.Fatal(err)
			}
		}
	}
	left := func() int {
		t.Helper()
		var n int
		if err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, u.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	open("a", "b", "c")
	if err := s.DeleteOtherSessions(ctx, u.ID, "b"); err != nil || left() != 1 {
		t.Fatalf("the other sessions: %v, %d left", err, left())
	}
	if _, err := s.SessionByHash(ctx, "b"); err != nil {
		t.Errorf("the session that was kept: %v", err)
	}
	open("d")
	if ok, err := s.DisableUser(ctx, u.ID, now); err != nil || !ok || left() != 0 {
		t.Fatalf("switched off: %v %v, %d sessions left", ok, err, left())
	}
	if got, err := s.UserByID(ctx, u.ID); err != nil || got.DisabledAt == nil {
		t.Errorf("the account after it was switched off: %+v %v", got, err)
	}
	if ok, err := s.DisableUser(ctx, u.ID, now); err != nil || ok {
		t.Errorf("switched off twice: %v %v", ok, err)
	}
	if ok, err := s.EnableUser(ctx, u.ID); err != nil || !ok {
		t.Errorf("switched back on: %v %v", ok, err)
	}
	if got, err := s.UserByID(ctx, u.ID); err != nil || got.DisabledAt != nil {
		t.Errorf("the account after it was switched back on: %+v %v", got, err)
	}
}
