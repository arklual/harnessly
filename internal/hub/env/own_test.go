package env_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func (a acme) me(t *testing.T, p *hubtest.Person) env.MeResponse {
	t.Helper()
	r := p.Do("GET", "/api/me/env?space="+a.tenant, nil)
	if r.Code != 200 {
		t.Fatalf("GET /api/me/env: %s", r)
	}
	var out env.MeResponse
	r.JSON(&out)
	return out
}

func meNames(rs env.MeResponse) (on, own []string) {
	for _, it := range rs.Items {
		if !it.Off && !it.Shadowed {
			on = append(on, it.Name)
		}
		if it.Own {
			own = append(own, it.Name)
		}
	}
	return on, own
}

// machineOf registers a machine of the tenant (only the space's owner hands out codes) and makes it p's.
func (a acme) machineOf(t *testing.T, p *hubtest.Person) *hubtest.Agent {
	t.Helper()
	m := a.anna.Machine(a.tenant, nil)
	if _, err := a.s.DB.Exec(context.Background(), `UPDATE machines SET created_by = ? WHERE id = ?`, p.ID, m.MachineID); err != nil {
		t.Fatal(err)
	}
	return m
}

// pushedTo is the list the pusher sends a machine now.
func pushedTo(t *testing.T, a acme, machineID string) []string {
	t.Helper()
	link := &fakeLink{id: machineID, status: "ok"}
	p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env", env.FeatureOwn}), Now: a.s.Clock.Now, Every: time.Hour})
	defer p.Close()
	p.Connected(machineID, hubproto.Hello{Features: []string{hubproto.FeatureEnvironment}})
	if err := p.Push(context.Background()); err != nil || link.count() != 1 {
		t.Fatalf("push: %v, sent %d", err, link.count())
	}
	var out []string
	for _, it := range link.sent[0].Items {
		out = append(out, it.Name)
	}
	slices.Sort(out)
	return out
}

func TestAPersonsOwnItemsReachOnlyTheirMachinesAndNeverReplaceTheOrganizations(t *testing.T) {
	a := newAcme(t, hubtest.Env("HUB_FEATURES_FORCE", env.FeatureOwn))
	review := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	if code, _ := a.entry(t, a.anna, "org", a.org, review, nil); code != 201 {
		t.Fatalf("org entry: %d", code)
	}
	vm := a.machineOf(t, a.vera)
	dm := a.machineOf(t, a.dina)

	// Vera adds her own skill and her own MCP server; a name the organization gives is not hers to take.
	r := a.vera.Do("POST", "/api/me/env/own?space="+a.tenant, map[string]any{"kind": "skill", "name": "Заметки", "source": map[string]any{"body": "Записывай решения."}})
	if r.Code != 201 {
		t.Fatalf("own skill: %s", r)
	}
	var notes env.MeItem
	r.JSON(&notes)
	if !notes.Own || notes.Layer != store.EnvLayerUser || notes.ItemID == "" {
		t.Fatalf("own skill = %+v", notes)
	}
	if r := a.vera.Do("POST", "/api/me/env/own?space="+a.tenant, map[string]any{"kind": "mcp", "name": "Мой трекер",
		"source": map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp"}}); r.Code != 201 {
		t.Fatalf("own mcp: %s", r)
	}
	if r := a.vera.Do("POST", "/api/me/env/own?space="+a.tenant, map[string]any{"kind": "skill", "name": "ревью  кода", "source": map[string]any{"body": "x"}}); r.Code != 409 {
		t.Fatalf("the organization's name: %s", r)
	}
	if r := a.vera.Do("POST", "/api/me/env/own?space="+a.tenant, map[string]any{"kind": "script", "name": "Скрипт", "source": map[string]any{"body": "x"}}); r.Code != 400 {
		t.Fatalf("a script of one's own: %s", r)
	}

	// Hers alone: her view and her machine have them; Dina's and the organization's catalog do not.
	if on, own := meNames(a.me(t, a.vera)); len(own) != 2 || !slices.Contains(on, "Ревью кода") || !slices.Contains(on, "Заметки") {
		t.Fatalf("vera: on %v own %v", on, own)
	}
	if !a.me(t, a.vera).Own {
		t.Fatal("the own layer is not said to be on")
	}
	if _, own := meNames(a.me(t, a.dina)); len(own) != 0 {
		t.Fatalf("dina sees another's own: %v", own)
	}
	if got := pushedTo(t, a, vm.MachineID); !slices.Equal(got, []string{"Заметки", "Мой трекер", "Ревью кода"}) {
		t.Fatalf("vera's machine = %v", got)
	}
	if got := pushedTo(t, a, dm.MachineID); !slices.Equal(got, []string{"Ревью кода"}) {
		t.Fatalf("dina's machine = %v", got)
	}
	var cat struct{ Items []env.CatalogItemView }
	if r := a.anna.Do("GET", a.url("/catalog"), nil); r.Code != 200 {
		t.Fatalf("catalog: %s", r)
	} else {
		r.JSON(&cat)
	}
	for _, c := range cat.Items {
		if c.ID == notes.ItemID {
			t.Fatal("the organization's catalog lists a person's own item")
		}
	}
	if r := a.anna.Do("PATCH", a.url("/catalog/"+notes.ItemID), map[string]any{"name": "Чужое"}); r.Code != 404 {
		t.Fatalf("an admin changed a person's own item: %s", r)
	}
	if r := a.dina.Do("DELETE", "/api/me/env/own/"+notes.ItemID+"?space="+a.tenant, nil); r.Code != 404 {
		t.Fatalf("dina removed vera's item: %s", r)
	}
	if code, _ := a.entry(t, a.anna, "tenant", a.tenant, notes.ItemID, nil); code != 400 {
		t.Fatalf("an admin put a person's own item into a layer: %d", code)
	}

	// What she switches off leaves her machine; a pinned item she cannot switch off.
	if r := a.vera.Do("PUT", "/api/me/env/"+review+"?space="+a.tenant, map[string]any{"off": true}); r.Code != 200 {
		t.Fatalf("switch off: %s", r)
	}
	if got := pushedTo(t, a, vm.MachineID); slices.Contains(got, "Ревью кода") {
		t.Fatalf("a switched-off item stayed on her machine: %v", got)
	}
	if got := pushedTo(t, a, dm.MachineID); !slices.Equal(got, []string{"Ревью кода"}) {
		t.Fatalf("her switch reached dina's machine: %v", got)
	}

	// The organization later gives a skill of her own skill's name: the given one is on the machine, hers is left out
	// and shown so.
	notesOrg := a.item(t, "skill", "ЗАМЕТКИ", map[string]any{"body": "Как принято у нас."})
	if code, _ := a.entry(t, a.anna, "org", a.org, notesOrg, map[string]any{"pinned": true}); code != 201 {
		t.Fatalf("org notes: %d", code)
	}
	sent := pushedTo(t, a, vm.MachineID)
	if !slices.Contains(sent, "ЗАМЕТКИ") || slices.Contains(sent, "Заметки") {
		t.Fatalf("her own replaced the organization's: %v", sent)
	}
	shadowed := false
	for _, it := range a.me(t, a.vera).Items {
		if it.ItemID == notes.ItemID {
			shadowed = it.Shadowed
		}
	}
	if !shadowed {
		t.Fatal("the left-out own item is not shown as such")
	}

	// Removed, it is gone from her machine.
	if r := a.vera.Do("DELETE", "/api/me/env/own/"+notes.ItemID+"?space="+a.tenant, nil); r.Code != 204 {
		t.Fatalf("remove: %s", r)
	}
	if _, own := meNames(a.me(t, a.vera)); len(own) != 1 {
		t.Fatalf("after remove: %v", own)
	}
}

func TestAMachineChangesOnlyItsOwnersOwnLayer(t *testing.T) {
	a := newAcme(t, hubtest.Env("HUB_FEATURES_FORCE", env.FeatureOwn))
	own := func(m store.Machine, req hubproto.EnvOwnRequest) hubproto.EnvOwnResult {
		req.RequestID = "r1"
		raw, _ := json.Marshal(req)
		return a.s.Hub.Env.Own(context.Background(), m, raw)
	}
	name := "Мой трекер"
	src := json.RawMessage(`{"transport":"http","url":"https://tracker.example.org/mcp"}`)
	vera := store.Machine{ID: "m1", SpaceID: a.tenant, CreatedBy: a.vera.ID}

	res := own(vera, hubproto.EnvOwnRequest{Action: hubproto.EnvOwnAdd, Kind: "mcp", Name: &name, Source: src})
	if res.Status != 201 || res.ItemID == "" || res.RequestID != "r1" {
		t.Fatalf("add = %+v", res)
	}
	if _, mine := meNames(a.me(t, a.vera)); !slices.Equal(mine, []string{name}) {
		t.Fatalf("vera's own = %v", mine)
	}
	// Another person's machine does not reach it, a machine without an owner changes nothing.
	if r := own(store.Machine{ID: "m2", SpaceID: a.tenant, CreatedBy: a.dina.ID}, hubproto.EnvOwnRequest{Action: hubproto.EnvOwnRemove, ItemID: res.ItemID}); r.Status != 404 {
		t.Fatalf("dina's machine removed vera's item: %+v", r)
	}
	if r := own(store.Machine{ID: "m3", SpaceID: a.tenant}, hubproto.EnvOwnRequest{Action: hubproto.EnvOwnAdd, Kind: "skill", Name: &name, Source: json.RawMessage(`{"body":"x"}`)}); r.Status != 403 {
		t.Fatalf("an ownerless machine = %+v", r)
	}
	if r := own(vera, hubproto.EnvOwnRequest{Action: hubproto.EnvOwnSwitch, ItemID: res.ItemID, Off: true}); r.Status != 200 {
		t.Fatalf("switch = %+v", r)
	}
	if r := own(vera, hubproto.EnvOwnRequest{Action: hubproto.EnvOwnRemove, ItemID: res.ItemID}); r.Status != 200 {
		t.Fatalf("remove = %+v", r)
	}
}

func TestWithoutTheFlagThereIsNoOwnLayer(t *testing.T) {
	a := newAcme(t)
	if r := a.vera.Do("POST", "/api/me/env/own?space="+a.tenant, map[string]any{"kind": "skill", "name": "Заметки", "source": map[string]any{"body": "x"}}); r.Code != 404 {
		t.Fatalf("own without the flag: %s", r)
	}
	if a.me(t, a.vera).Own {
		t.Fatal("the own layer is said to be on")
	}
	name := "x"
	raw, _ := json.Marshal(hubproto.EnvOwnRequest{RequestID: "r", Action: hubproto.EnvOwnAdd, Kind: "skill", Name: &name, Source: json.RawMessage(`{"body":"x"}`)})
	if r := a.s.Hub.Env.Own(context.Background(), store.Machine{ID: "m1", SpaceID: a.tenant, CreatedBy: a.vera.ID}, raw); r.Status != 404 {
		t.Fatalf("machine own without the flag = %+v", r)
	}
}
