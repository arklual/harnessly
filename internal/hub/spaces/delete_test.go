package spaces

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A space is deleted by its owner, with its name repeated and, when other people are in it, their loss acknowledged;
// nothing of it is left, and the deletion's line outlives it in the organization's journal.
func TestDeleteSpace(t *testing.T) {
	e := newEnv(t, "HUB_ORGS", "on")
	ctx := context.Background()
	owner, a, stranger := e.signIn("owner@example.com"), e.signIn("a@example.com"), e.signIn("stranger@example.com")
	sp := owner.newSpace("Команда")
	other := owner.newSpace("Вторая")
	join(t, e, owner, a, sp.ID)
	join(t, e, owner, a, other.ID)
	st := e.db.S()
	org := &store.Org{Slug: "acme", Name: "Acme"}
	if err := st.CreateOrg(ctx, org); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSpaceOrg(ctx, sp.ID, org.ID); err != nil {
		t.Fatal(err)
	}
	var gone []string
	e.svc.machineGone = func(m store.Machine) { gone = append(gone, m.ID) }
	machine := &store.Machine{SpaceID: sp.ID, Name: "box", KeyPub: "k-box", CreatedBy: a.id}
	kept := &store.Machine{SpaceID: other.ID, Name: "desk", KeyPub: "k-desk", CreatedBy: a.id}
	for _, m := range []*store.Machine{machine, kept} {
		if err := st.CreateMachine(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CreateTeam(ctx, &store.Team{SpaceID: sp.ID, Slug: "core", Name: "Core"}); err != nil {
		t.Fatal(err)
	}
	path := "/api/spaces/" + sp.ID + "/delete"
	ok := map[string]any{"confirm": "Команда", "withMembers": true}

	if r := stranger.do("POST", path, ok); r.Code != 403 {
		t.Fatalf("a stranger: %d %s", r.Code, r.Body)
	}
	if r := a.do("POST", path, ok); r.Code != 403 {
		t.Fatalf("a plain member: %d %s", r.Code, r.Body)
	}
	if r := owner.do("POST", path, map[string]any{"confirm": "команда", "withMembers": true}); r.Code != 400 {
		t.Fatalf("a wrong name: %d %s", r.Code, r.Body)
	}
	if r := owner.do("POST", path, map[string]any{"confirm": "Команда"}); r.Code != 409 {
		t.Fatalf("other members not acknowledged: %d %s", r.Code, r.Body)
	}
	if _, err := st.SpaceByID(ctx, sp.ID); err != nil {
		t.Fatalf("a refused deletion touched the space: %v", err)
	}
	if err := e.db.Tx(ctx, func(st store.Store) error {
		_, err := st.DeleteSpaceData(ctx, store.Space{ID: "none", Slug: "none"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if r := owner.do("POST", path, ok); r.Code != 200 {
		t.Fatalf("delete: %d %s", r.Code, r.Body)
	}
	if _, err := st.SpaceByID(ctx, sp.ID); !store.IsNotFound(err) {
		t.Fatalf("the space is still there: %v", err)
	}
	if ms, err := st.Members(ctx, sp.ID); err != nil || len(ms) != 0 {
		t.Errorf("members left: %+v %v", ms, err)
	}
	if _, err := st.MachineByID(ctx, machine.ID); !store.IsNotFound(err) {
		t.Errorf("the machine is still there: %v", err)
	}
	if len(gone) != 1 || gone[0] != machine.ID {
		t.Errorf("machines told to close: %v", gone)
	}
	if _, err := st.MachineByID(ctx, kept.ID); err != nil {
		t.Errorf("a machine of another space went too: %v", err)
	}
	if role, err := st.Role(ctx, other.ID, a.id); err != nil || role == "" {
		t.Errorf("the member lost another space: %q %v", role, err)
	}
	if r := a.do("GET", "/api/spaces/"+sp.ID, nil); r.Code != 403 {
		t.Errorf("a former member reads the space: %d", r.Code)
	}
	journal, err := st.Audit(ctx, store.AuditFilter{OrgID: org.ID})
	if err != nil || len(journal) != 1 || journal[0].Action != "tenant.purge" || journal[0].TargetID != sp.ID || journal[0].ActorID != owner.id || journal[0].SpaceID != "" {
		t.Fatalf("the organization's journal: %+v %v", journal, err)
	}
	if r := owner.do("POST", path, ok); r.Code != 403 && r.Code != 404 {
		t.Errorf("deleting it again: %d %s", r.Code, r.Body)
	}
}

// An administrator of the installation deletes a space they are not in; nobody else who is not its owner does.
func TestInstallationAdminDeletesASpace(t *testing.T) {
	e := newEnv(t, "HUB_ADMIN_EMAILS", "boss@example.com")
	owner, boss := e.signIn("owner@example.com"), e.signIn("boss@example.com")
	sp := owner.newSpace("Клиент")
	path := "/api/spaces/" + sp.ID + "/delete"
	if r := boss.do("POST", path, map[string]any{"confirm": "Клиент"}); r.Code != 409 {
		t.Fatalf("the owner is a member the administrator must acknowledge: %d %s", r.Code, r.Body)
	}
	if r := boss.do("POST", path, map[string]any{"confirm": "Клиент", "withMembers": true}); r.Code != 200 {
		t.Fatalf("delete: %d %s", r.Code, r.Body)
	}
	if _, err := e.db.S().SpaceByID(context.Background(), sp.ID); !store.IsNotFound(err) {
		t.Fatalf("the space is still there: %v", err)
	}
	if r := boss.do("POST", "/api/spaces/nosuchspace/delete", map[string]any{"confirm": "x"}); r.Code != 404 {
		t.Errorf("a space that is not there: %d", r.Code)
	}
}
