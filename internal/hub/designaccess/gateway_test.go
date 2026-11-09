package designaccess

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// the designs' access moves from the gateway to the hub once, for everyone, as a release step: a dry run says
// what would move and writes nothing; the move copies what the hub lacks, keeps what it has, never runs twice for a
// project, and finds the hub machine of every gateway pair or says why not.

func gatewayFixture(r *spaceRig) GatewayData {
	link := GwLink{ID: "11111111-1111-4111-8111-111111111111", TokenHash: strings.Repeat("a", 64), Title: "Guests", Rights: "comment", Version: "latest",
		Pages: sql.NullString{String: `["Main"]`, Valid: true}, CreatedBy: r.ownerU.Email, CreatedAt: 1, Opens: 3,
		Views:    []GwView{{GuestID: "g1", Board: "Main", Ms: 1200, At: 5}},
		Comments: []GwComment{{Board: "Main", Text: "Nice", GuestID: "g1", GuestName: "Guest", X: sql.NullFloat64{Float64: 0.5, Valid: true}, CreatedAt: 6}}}
	return GatewayData{
		Projects: []GwProject{
			{ID: "fresh", Owner: r.ownerU.Email, OwnerName: "Owner", HomeTeam: "team", TeamLevel: roleView, Title: "Fresh", CreatedAt: 1,
				Members: []GwMember{{Email: r.mateU.Email, Role: roleEdit, GrantedBy: r.ownerU.Email, GrantedAt: 2}, {Email: r.ownerU.Email, Role: roleOwner},
					{Email: r.strangeU.Email, Role: roleOwner}},
				Teams:    []GwTeamGrant{{Team: "team", Role: roleComment, GrantedBy: r.ownerU.Email, GrantedAt: 2}},
				Requests: []GwRequest{{ID: "22222222-2222-4222-8222-222222222222", Email: r.strangeU.Email, Level: roleView, Status: "open", CreatedAt: 3}},
				Invites:  []GwInvite{{ID: "33333333-3333-4333-8333-333333333333", TokenHash: strings.Repeat("b", 64), Role: roleView, ExpiresAt: 9e12, CreatedBy: r.ownerU.Email, CreatedAt: 3}},
				Links:    []GwLink{link}},
			{ID: "known", Owner: r.mateU.Email, HomeTeam: "team", TeamLevel: roleEdit, CreatedAt: 1,
				Members: []GwMember{{Email: r.strangeU.Email, Role: roleView, GrantedBy: r.mateU.Email, GrantedAt: 2}}},
			{ID: "theirs", Owner: r.strangeU.Email, HomeTeam: "team", TeamLevel: roleEdit, CreatedAt: 1,
				Members: []GwMember{{Email: r.mateU.Email, Role: roleEdit, GrantedBy: r.strangeU.Email, GrantedAt: 2}}},
			{ID: "solo", Owner: "nobody@elsewhere.org", HomeTeam: "elsewhere", TeamLevel: roleEdit, CreatedAt: 1},
			{ID: "Not An Id", Owner: r.ownerU.Email},
		},
		Runtimes: []GwPair{
			{Person: r.ownerU.Email, Machine: "runner-2"},
			{Person: r.ownerU.Email, Machine: "dev-department"},
			{Person: r.mateU.Email, Machine: "dev-department"},
			{Person: r.mateU.Email, Machine: "runner-2"},
			{Person: r.strangeU.Email, Machine: "dev-department"},
		},
		Linked: map[string]string{pairKey(r.strangeU.Email, "dev-department"): r.stranger.ID},
		Off:    map[string]bool{pairKey(r.ownerU.Email, "dev-department"): true},
	}
}

func TestGatewayImportMovesOnceAndKeepsTheHubs(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	st := r.db.S()
	team := store.Team{SpaceID: r.owner.SpaceID, Slug: "general", Name: "General"}
	if err := st.CreateTeam(ctx, &team); err != nil {
		t.Fatal(err)
	}
	mk := func(name, key string, by store.User) store.Machine {
		m := store.Machine{SpaceID: r.owner.SpaceID, Name: name, Profile: "p", Via: "v", KeyPub: key, EncPub: key + "e", CreatedBy: by.ID}
		if err := st.CreateMachine(ctx, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	runner := mk("acme-runner-2-owner", "k4", r.ownerU)
	devOwner := mk("acme-dev-department-owner", "k5", r.ownerU)
	mk("acme-dev-department-a", "k6", r.mateU)
	mk("acme-dev-department.b", "k7", r.mateU)
	// The hub knows two projects already: the mate's, recorded by its machine, and the stranger's, moved before.
	for _, p := range []struct {
		m     store.Machine
		id, o string
	}{{r.mate, "known", r.mateU.Email}, {r.stranger, "theirs", r.strangeU.Email}} {
		if _, err := r.svc.registerCreated(ctx, p.m, p.id, p.o); err != nil {
			t.Fatal(err)
		}
	}
	data := gatewayFixture(r)
	opt := ImportOptions{Space: "team", By: "test", DryRun: true, MachinePrefix: "acme-"}

	dry, err := r.svc.ImportGateway(ctx, data, opt)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ProjectImport{}
	for _, p := range dry.Projects {
		got[p.ID] = p
	}
	if f := got["fresh"]; f.Outcome != "imported" || f.Members != 1 || f.Teams != 1 || f.Requests != 1 || f.Invites != 1 || f.Links != 1 || f.Extras != 2 || (f.Machine != r.owner.ID && f.Machine != runner.ID && f.Machine != devOwner.ID) {
		t.Fatalf("dry run of a new project: %+v", f)
	}
	if got["fresh"].Refused != 1 {
		t.Fatalf("a grant of «owner» taken: %+v", got["fresh"])
	}
	if got["known"].Outcome != "merged" || got["known"].Members != 1 || got["theirs"].Outcome != "kept" || got["theirs"].Members != 0 ||
		got["Not An Id"].Outcome != "skipped" || got["solo"].Outcome != "imported" {
		t.Fatalf("dry run: %s", dry.Summary())
	}
	if tx, _ := r.svc.read(ctx, r.owner); mustProject(t, tx, "fresh") != nil {
		t.Fatal("a dry run wrote a project")
	}

	opt.DryRun = false
	rep, err := r.svc.ImportGateway(ctx, data, opt)
	if err != nil || rep.Failed() {
		t.Fatalf("import: %v\n%s", err, rep.Summary())
	}
	pairs := map[string]PairLink{}
	for _, l := range rep.Pairs {
		pairs[l.Person+"@"+l.Machine] = l
	}
	for key, want := range map[string]string{
		r.ownerU.Email + "@runner-2":         "linked:" + runner.ID,
		r.ownerU.Email + "@dev-department":   "off:" + devOwner.ID,
		r.mateU.Email + "@dev-department":    "ambiguous:",
		r.mateU.Email + "@runner-2":          "missing:",
		r.strangeU.Email + "@dev-department": "kept:" + r.stranger.ID,
	} {
		if l := pairs[key]; l.Outcome+":"+l.HubMachine != want {
			t.Fatalf("pair %s: %+v, want %s", key, l, want)
		}
	}

	if g := mustView(t, r, r.mate)["fresh"]; !g.See || !g.Edit {
		t.Fatalf("the mate given «edit» in the gateway: %+v", g)
	}
	if g := mustView(t, r, r.stranger)["fresh"]; g.See {
		t.Fatalf("a person outside the project's team sees it: %+v", g)
	}
	tx, err := r.svc.read(ctx, r.owner)
	if err != nil {
		t.Fatal(err)
	}
	if p := mustProject(t, tx, "solo"); p == nil || p.TeamLevel != levelNone || p.HomeTeam != "" {
		t.Fatalf("a project of a team the hub lacks is not personal: %+v", p)
	}
	var links, views, comments, theirs int
	q := st.Q()
	_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM design_share_links WHERE token_hash = ? AND token_enc IS NULL`, strings.Repeat("a", 64)).Scan(&links)
	_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM design_share_views`).Scan(&views)
	_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM design_guest_comments`).Scan(&comments)
	_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM design_members WHERE project_id = 'theirs'`).Scan(&theirs)
	if links != 1 || views != 1 || comments != 1 || theirs != 0 {
		t.Fatalf("links %d views %d comments %d members of a kept project %d", links, views, comments, theirs)
	}

	// The owner takes the mate's role in the hub; the next start does not give it back, nor copy anything twice.
	if _, err := q.Exec(ctx, `DELETE FROM design_members WHERE project_id = 'fresh'`); err != nil {
		t.Fatal(err)
	}
	again, err := r.svc.ImportGateway(ctx, data, opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range again.Projects {
		if p.Outcome != "done" && p.Outcome != "skipped" {
			t.Fatalf("a second run moved %s again: %+v", p.ID, p)
		}
	}
	var members int
	_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM design_members WHERE project_id = 'fresh'`).Scan(&members)
	_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM design_share_views`).Scan(&views)
	if members != 0 || views != 1 {
		t.Fatalf("a second run undid the hub's change or copied again: members %d views %d", members, views)
	}
}

func mustProject(t *testing.T, tx *txn, id string) *project {
	t.Helper()
	if tx == nil {
		t.Fatal("no transaction")
	}
	p, err := tx.getProject(id, false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGatewayTeamsStayInTheSpaceUnlessNamed(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	st := r.db.S()
	other := store.Space{Slug: "acme", Name: "Other", CreatedBy: r.ownerU.ID}
	if err := st.CreateSpace(ctx, &other); err != nil {
		t.Fatal(err)
	}
	foreign := store.Team{SpaceID: other.ID, Slug: "general", Name: "General"}
	if err := st.CreateTeam(ctx, &foreign); err != nil {
		t.Fatal(err)
	}
	projects := []GwProject{{ID: "p", HomeTeam: "acme"}}
	got, notes, err := r.svc.gatewayTeams(ctx, r.owner.SpaceID, projects, nil)
	if err != nil || got["acme"] != "" || len(notes) != 1 {
		t.Fatalf("a team of another space taken by its slug: %v %v %v", got, notes, err)
	}
	if got, _, err := r.svc.gatewayTeams(ctx, r.owner.SpaceID, projects, map[string]string{"acme": foreign.ID}); err != nil || got["acme"] != foreign.ID {
		t.Fatalf("a named team: %v %v", got, err)
	}
	if maskEmail("ann.lee@example.org") != "a…@example.org" {
		t.Fatal(maskEmail("ann.lee@example.org"))
	}
}
