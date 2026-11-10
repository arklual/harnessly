package orgs_test

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func (v veraCase) machinesList(t *testing.T, p *hubtest.Person, query string) []machines.HubMachine {
	t.Helper()
	r := p.Do("GET", v.url("/machines"+query), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("machines %s: %s", query, r)
	}
	var out analytics.MachinesResponse
	r.JSON(&out)
	return out.Machines
}

func machineByName(ms []machines.HubMachine, name string) *machines.HubMachine {
	for i := range ms {
		if ms[i].Name == name {
			return &ms[i]
		}
	}
	return nil
}

func (v veraCase) patchMachine(p *hubtest.Person, id string, body map[string]any) hubtest.Resp {
	return p.Do("PATCH", "/api/machines/"+id, body)
}

func TestMachinesList(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	a := v.anna.Machine(v.tenant, nil)
	if err := st.RenameMachine(ctx, a.MachineID, "Рабочая"); err != nil {
		t.Fatal(err)
	}

	// A machine that was seen an hour ago and talks to nobody: offline, and nobody has used it for more than a week.
	seen := v.s.Clock.Now().Add(-time.Hour)
	if err := st.CreateMachine(ctx, &store.Machine{ID: "m-off", SpaceID: v.otherTenant, Name: "Старая", KeyPub: "k", EncPub: "e",
		AgentVersion: "1.0.0", CreatedAt: seen.Add(-30 * 24 * time.Hour), LastSeenAt: &seen}); err != nil {
		t.Fatal(err)
	}
	now := v.s.Clock.Now()
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "chat1", MachineID: a.MachineID, UserID: v.vera.ID,
		SpaceID: v.tenant, TeamID: v.team.ID, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-time.Minute).UnixMilli(),
		Metrics: "{}", Seq: 1, UpdatedAt: now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}

	ms := v.machinesList(t, v.anna, "")
	if len(ms) != 2 || ms[0].Name != "Старая" {
		t.Fatalf("problems first: %+v", ms)
	}
	old, work := ms[0], machineByName(ms, "Рабочая")
	if old.Status != "offline" || old.TenantID != v.otherTenant || !slices.Equal(old.Problems, []string{"offline", "idle_week"}) ||
		old.LastActiveAt != nil || old.TeamID != nil || len(old.People) != 0 || len(old.Access) != 0 || old.Cloud != nil {
		t.Errorf("old = %+v", old)
	}
	if work == nil || work.Status != "online" || work.TenantID != v.tenant || work.ChatsRunning != 1 || len(work.Problems) != 0 ||
		len(work.People) != 1 || work.People[0].UserID != v.vera.ID || work.LastActiveAt == nil || work.LastSeenAt == nil || work.DisabledAt != nil {
		t.Errorf("work = %+v", work)
	}

	// Only the problems; only a tenant; only a team.
	if ps := v.machinesList(t, v.anna, "?problems=1"); len(ps) != 1 || ps[0].Name != "Старая" {
		t.Errorf("problems=1: %+v", ps)
	}
	if ts := v.machinesList(t, v.anna, "?scope=tenant:"+v.tenant); len(ts) != 1 || ts[0].Name != "Рабочая" {
		t.Errorf("tenant scope: %+v", ts)
	}
	// A lead's screen is their team, and a machine of the whole tenant is one they may open too (HRN-970: the lead's
	// «Машины» said «0 машин» while a member of the same tenant saw the machine). The admin's team view stays the team.
	if ts := v.machinesList(t, v.boris, "?scope=team:"+v.team.ID); len(ts) != 1 || ts[0].Name != "Рабочая" || ts[0].TeamID != nil {
		t.Errorf("lead, tenant machine in the team scope: %+v", ts)
	}
	if ts := v.machinesList(t, v.anna, "?scope=team:"+v.team.ID); len(ts) != 0 {
		t.Errorf("admin, team scope without the team's machines: %+v", ts)
	}
	if err := st.SetMachineTeam(ctx, a.MachineID, v.team.ID); err != nil {
		t.Fatal(err)
	}
	if ts := v.machinesList(t, v.boris, "?scope=team:"+v.team.ID); len(ts) != 1 || ts[0].Name != "Рабочая" || ts[0].TeamID == nil || *ts[0].TeamID != v.team.ID {
		t.Errorf("team scope: %+v", ts)
	}
	if ts := v.machinesList(t, v.anna, "?scope=team:"+v.team.ID+"&problems=1"); len(ts) != 0 {
		t.Errorf("team problems: %+v", ts)
	}

	// A week later the chat is a week old: the working machine is idle too. A disabled machine has only that problem.
	v.s.Clock.Advance(8 * 24 * time.Hour)
	if w := machineByName(v.machinesList(t, v.anna, ""), "Рабочая"); w == nil || !slices.Equal(w.Problems, []string{"idle_week"}) || w.ChatsRunning != 0 || len(w.People) != 1 {
		t.Errorf("idle = %+v", w)
	}

	// Who sees what: a member and a lead without an organization role see the machines of their own tenants only,
	// never the other tenant's (HRN-970: a person put in a team saw no machine until they were made a lead).
	for _, p := range []*hubtest.Person{v.vera, v.boris} {
		if ms := v.machinesList(t, p, ""); len(ms) != 1 || ms[0].Name != "Рабочая" {
			t.Errorf("%s on the organization: %+v", p.Email, ms)
		}
	}
	if r := v.anna.Do("GET", v.url("/machines?problems=2"), nil); r.Code != http.StatusBadRequest {
		t.Errorf("bad problems: %s", r)
	}
}

func TestMachinesPatchTeamAndAccess(t *testing.T) {
	v := newVera(t)
	a := v.anna.Machine(v.tenant, nil)
	other := v.acme.team(t, v.anna, v.otherTenant, "Продукт")

	patch := func(p *hubtest.Person, body map[string]any) (hubtest.Resp, machines.HubMachine) {
		r := v.patchMachine(p, a.MachineID, body)
		var out struct{ Machine machines.HubMachine }
		if r.Code == http.StatusOK {
			r.JSON(&out)
		}
		return r, out.Machine
	}

	// The tenant's admin puts the machine in a team and shares it.
	r, m := patch(v.anna, map[string]any{"teamId": v.team.ID})
	if r.Code != http.StatusOK || m.TeamID == nil || *m.TeamID != v.team.ID || m.TenantID != v.tenant {
		t.Fatalf("team: %s", r)
	}
	access := []map[string]string{{"kind": "user", "id": v.vera.ID}, {"kind": "team", "id": v.team.ID}}
	r, m = patch(v.anna, map[string]any{"access": access})
	if r.Code != http.StatusOK || len(m.Access) != 2 {
		t.Fatalf("access: %s", r)
	}
	if t0, u0 := m.Access[0], m.Access[1]; t0.Kind != "team" || t0.ID != v.team.ID || t0.Label != "Платформа" || u0.Kind != "user" || u0.ID != v.vera.ID || u0.Label == "" {
		t.Errorf("access = %+v", m.Access)
	}

	// The lead of the machine's team changes who may use it, and nothing else.
	r, m = patch(v.boris, map[string]any{"access": []map[string]string{{"kind": "team", "id": v.team.ID}}})
	if r.Code != http.StatusOK || len(m.Access) != 1 || m.Access[0].Kind != "team" {
		t.Errorf("lead access: %s", r)
	}
	if r, _ := patch(v.boris, map[string]any{"teamId": nil}); r.Code != http.StatusForbidden {
		t.Errorf("lead moves the machine: %s", r)
	}
	if r, _ := patch(v.boris, map[string]any{"name": "Моя", "access": []map[string]string{}}); r.Code != http.StatusForbidden {
		t.Errorf("lead renames with access: %s", r)
	}
	if r, _ := patch(v.vera, map[string]any{"access": []map[string]string{}}); r.Code != http.StatusForbidden {
		t.Errorf("member: %s", r)
	}
	if r, _ := patch(v.gleb, map[string]any{"teamId": nil}); r.Code != http.StatusForbidden {
		t.Errorf("a person of another tenant: %s", r)
	}

	// A team of another tenant, a person of another tenant, a team that is not there.
	if r, _ := patch(v.anna, map[string]any{"teamId": other.ID}); r.Code != http.StatusBadRequest {
		t.Errorf("foreign team: %s", r)
	}
	if r, _ := patch(v.anna, map[string]any{"access": []map[string]string{{"kind": "user", "id": v.gleb.ID}}}); r.Code != http.StatusBadRequest {
		t.Errorf("foreign person: %s", r)
	}
	if r, _ := patch(v.anna, map[string]any{"access": []map[string]string{{"kind": "team", "id": other.ID}}}); r.Code != http.StatusBadRequest {
		t.Errorf("foreign team in access: %s", r)
	}
	if r, _ := patch(v.anna, map[string]any{"access": []map[string]string{{"kind": "group", "id": "x"}}}); r.Code != http.StatusBadRequest {
		t.Errorf("bad kind: %s", r)
	}
	if r, _ := patch(v.anna, map[string]any{"teamId": "nope"}); r.Code != http.StatusBadRequest {
		t.Errorf("no such team: %s", r)
	}

	// Name and team in one go; null takes the team away and an empty list opens the machine to the tenant.
	r, m = patch(v.anna, map[string]any{"name": "  Сервер  сборки ", "teamId": nil, "access": []map[string]string{}})
	if r.Code != http.StatusOK || m.Name != "Сервер сборки" || m.TeamID != nil || len(m.Access) != 0 {
		t.Errorf("all at once: %s", r)
	}

	// The name alone keeps its old answer: the machine as the space sees it.
	r = v.patchMachine(v.anna, a.MachineID, map[string]any{"name": "Сборка"})
	var legacy struct{ Machine map[string]any }
	r.JSON(&legacy)
	if r.Code != http.StatusOK || legacy.Machine["name"] != "Сборка" || legacy.Machine["spaceId"] != v.tenant {
		t.Errorf("legacy rename: %s", r)
	}

	// The journal has the team and the access changes, about the machine.
	evs, _ := v.audit(t, v.anna, "?scope=tenant:"+v.tenant+"&limit=100")
	got := actions(evs)
	if got["machine.team"] != 2 || got["machine.access"] != 3 {
		t.Errorf("journal = %v", got)
	}
	for _, e := range evs {
		if e.Action == "machine.team" && (e.Target == nil || e.Target.Kind != "machine" || e.Target.ID != a.MachineID || e.Target.Label == "") {
			t.Errorf("target = %+v", e.Target)
		}
	}
}

func TestMachineDisableEnable(t *testing.T) {
	v := newVera(t, hubtest.Env("HUB_RUNTIME_DOMAIN", "m.example.test"))
	a := v.anna.Machine(v.tenant, nil)
	id := a.MachineID

	if r := v.anna.Do("GET", "/api/machines/"+id+"/open", nil); r.Code != http.StatusOK {
		t.Fatalf("open before: %s", r)
	}
	for _, p := range []*hubtest.Person{v.vera, v.boris, v.gleb} {
		if r := p.Do("POST", "/api/machines/"+id+"/disable", map[string]any{}); r.Code != http.StatusForbidden {
			t.Errorf("disable by a person without rights: %s", r)
		}
	}

	r := v.anna.Do("POST", "/api/machines/"+id+"/disable", map[string]any{})
	var out struct{ Machine machines.HubMachine }
	r.JSON(&out)
	if r.Code != http.StatusOK || out.Machine.DisabledAt == nil || !slices.Equal(out.Machine.Problems, []string{"disabled"}) || out.Machine.Status == "online" {
		t.Fatalf("disable: %s", r)
	}
	if code := a.WaitClosed(); int(code) != hubproto.CloseSwitchedOff {
		t.Errorf("close code %d", code)
	}
	m, err := v.s.DB.S().MachineByID(context.Background(), id)
	if err != nil || m.RevokedAt != nil || m.KeyPub == "" || m.DisabledAt == nil {
		t.Errorf("the key stays: %+v %v", m, err)
	}

	// Off: it cannot be opened, restarted or dialed (not as a revoked machine: the agent would forget its key).
	if r := v.anna.Do("GET", "/api/machines/"+id+"/open", nil); r.Code != http.StatusLocked || r.Err() != "machine_disabled" {
		t.Errorf("open while off: %s", r)
	}
	if r := v.vera.Do("GET", "/api/machines/"+id+"/open", nil); r.Code != http.StatusLocked {
		t.Errorf("a member opens while off: %s", r)
	}
	if r := v.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "now"}); r.Code != http.StatusLocked {
		t.Errorf("restart while off: %s", r)
	}
	if code, _ := a.TryConnect(); code != http.StatusLocked {
		t.Errorf("dial while off: %d", code)
	}
	if ms := v.machinesList(t, v.anna, "?problems=1"); len(ms) != 1 || ms[0].DisabledAt == nil {
		t.Errorf("list: %+v", ms)
	}
	// Twice is the same.
	if r := v.anna.Do("POST", "/api/machines/"+id+"/disable", map[string]any{}); r.Code != http.StatusOK {
		t.Errorf("disable twice: %s", r)
	}

	r = v.anna.Do("POST", "/api/machines/"+id+"/enable", map[string]any{})
	out.Machine = machines.HubMachine{}
	r.JSON(&out)
	if r.Code != http.StatusOK || out.Machine.DisabledAt != nil || slices.Contains(out.Machine.Problems, "disabled") {
		t.Fatalf("enable: %s", r)
	}
	a.Connect()
	v.s.Eventually("the link", func() bool { return v.s.Hub.Registry.Get(id) != nil })
	if r := v.anna.Do("GET", "/api/machines/"+id+"/open", nil); r.Code != http.StatusOK {
		t.Errorf("open after: %s", r)
	}
	if ms := v.machinesList(t, v.anna, ""); len(ms) != 1 || ms[0].Status != "online" || ms[0].DisabledAt != nil {
		t.Errorf("list after: %+v", ms)
	}

	evs, _ := v.audit(t, v.anna, "?scope=tenant:"+v.tenant+"&limit=100")
	if got := actions(evs); got["machine.disable"] != 1 || got["machine.enable"] != 1 {
		t.Errorf("journal = %v", got)
	}
}

func TestMachineRestart(t *testing.T) {
	v := newVera(t)
	a := v.anna.Machine(v.tenant, nil)
	id := a.MachineID
	a.Handle(hubproto.OpRuntimeRestart, func(c hubtest.Call) (any, error) { return map[string]any{}, nil })

	r := v.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "after_chats"})
	var out struct{ JobID string }
	r.JSON(&out)
	if r.Code != http.StatusAccepted || out.JobID == "" {
		t.Fatalf("restart: %s", r)
	}
	calls := a.Calls(hubproto.OpRuntimeRestart)
	if len(calls) != 1 || calls[0].Str("jobId") != out.JobID || calls[0].Str("when") != "after_chats" {
		t.Errorf("calls = %+v", calls)
	}

	// The virtual machine is not ours to restart yet.
	if r := v.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "vm", "when": "now"}); r.Code != http.StatusNotImplemented ||
		r.Err() != "not_implemented" {
		t.Errorf("vm: %s", r)
	}
	for _, body := range []map[string]any{{"what": "disk", "when": "now"}, {"what": "runner", "when": "tomorrow"}, {}} {
		if r := v.anna.Do("POST", "/api/machines/"+id+"/restart", body); r.Code != http.StatusBadRequest {
			t.Errorf("%v: %s", body, r)
		}
	}
	for _, p := range []*hubtest.Person{v.vera, v.boris} {
		if r := p.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "now"}); r.Code != http.StatusForbidden {
			t.Errorf("restart by a person without rights: %s", r)
		}
	}

	// An agent that does not know the operation: a clear refusal.
	b := v.anna.Machine(v.tenant, nil)
	b.Handle(hubproto.OpRuntimeRestart, func(c hubtest.Call) (any, error) { return nil, hubtest.Fail(hubproto.CodeUnsupported, "no") })
	if r := v.anna.Do("POST", "/api/machines/"+b.MachineID+"/restart", map[string]any{"what": "runner", "when": "now"}); r.Code != http.StatusUnprocessableEntity ||
		r.Err() != "restart_unsupported" {
		t.Errorf("old agent: %s", r)
	}
}

func TestMachinesOrgFlagOff(t *testing.T) {
	v := newVera(t)
	a := v.anna.Machine(v.tenant, nil)
	id := a.MachineID
	v.s.Cfg.Orgs = "off"
	for _, path := range []string{"/disable", "/enable", "/restart"} {
		if r := v.anna.Do("POST", "/api/machines/"+id+path, map[string]any{"what": "runner"}); r.Code != http.StatusNotFound {
			t.Errorf("%s: %s", path, r)
		}
	}
	if r := v.patchMachine(v.anna, id, map[string]any{"teamId": nil}); r.Code != http.StatusNotFound {
		t.Errorf("team: %s", r)
	}
	if r := v.patchMachine(v.anna, id, map[string]any{"name": "Старое имя"}); r.Code != http.StatusOK {
		t.Errorf("rename: %s", r)
	}
}
