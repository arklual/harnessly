package env_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestGroups(t *testing.T) {
	a := newAcme(t)
	core := a.item(t, "repo", "Ядро", map[string]any{"url": "https://git.example.org/a/core.git"})
	back := a.item(t, "repo", "Бэкенд", map[string]any{"url": "https://git.example.org/a/back.git"})
	infra := a.item(t, "repo", "Инфра", map[string]any{"url": "https://git.example.org/a/infra.git"})

	group := func(body map[string]any) env.GroupView {
		t.Helper()
		body["layer"], body["scopeId"] = "org", a.org
		r := a.anna.Do("POST", a.url("/env/groups"), body)
		if r.Code != 201 {
			t.Fatalf("group: %s", r)
		}
		var out struct{ Group env.GroupView }
		r.JSON(&out)
		return out.Group
	}
	gCore := group(map[string]any{"name": "Основа", "required": true})
	gBack := group(map[string]any{"name": "Бэкенд"})
	gInfra := group(map[string]any{"name": "Инфра", "defaultOn": false})
	if r := a.anna.Do("POST", a.url("/env/groups"), map[string]any{"layer": "org", "scopeId": a.org, "name": "Основа"}); r.Code != 409 {
		t.Errorf("same name: %s", r)
	}
	if r := a.vera.Do("POST", a.url("/env/groups"), map[string]any{"layer": "org", "scopeId": a.org, "name": "Чужая"}); r.Code != 403 {
		t.Errorf("a member makes no org group: %s", r)
	}
	a.entry(t, a.anna, "org", a.org, core, map[string]any{"groupId": gCore.ID})
	_, backRow := a.entry(t, a.anna, "org", a.org, back, nil)
	a.entry(t, a.anna, "org", a.org, infra, map[string]any{"groupId": gInfra.ID})
	if r := a.anna.Do("PATCH", a.url("/env/entries/"+backRow.EntryID), map[string]any{"groupId": gBack.ID}); r.Code != 200 {
		t.Fatalf("into a group: %s", r)
	}
	if r := a.dina.Do("POST", a.url("/env/entries"), map[string]any{"layer": "tenant", "scopeId": a.tenant, "itemId": back, "groupId": gBack.ID}); r.Code != 400 {
		t.Errorf("a group of another layer: %s", r)
	}
	if l := layer(a.layers(t, a.anna, ""), "org", a.org); len(l.Groups) != 3 {
		t.Errorf("groups of the layer = %+v", l.Groups)
	}

	me := func() env.MeResponse {
		var m env.MeResponse
		a.anna.Do("GET", "/api/me/env?space="+a.tenant, nil).JSON(&m)
		return m
	}
	off := func(m env.MeResponse) map[string]bool {
		out := map[string]bool{}
		for _, it := range m.Items {
			out[it.Name] = it.Off
		}
		return out
	}
	m := me()
	if got := off(m); got["Ядро"] || got["Бэкенд"] || !got["Инфра"] || len(m.Groups) != 3 {
		t.Fatalf("defaults: %v %+v", got, m.Groups)
	}
	onMachine := func() map[string]bool {
		set, ok := sendTo(t, a, hubproto.FeatureEnvSetup)
		if !ok {
			t.Fatal("nothing sent")
		}
		out := map[string]bool{}
		for _, it := range set.Items {
			out[it.Name] = true
		}
		return out
	}
	if got := onMachine(); !got["Ядро"] || !got["Бэкенд"] || got["Инфра"] {
		t.Errorf("machine by default: %v", got)
	}
	// The person chooses: back off, infra on; the required group cannot be switched off.
	put := func(id string, on bool) int {
		return a.anna.Do("PUT", "/api/me/env/groups/"+id+"?space="+a.tenant, map[string]any{"on": on}).Code
	}
	if c := put(gCore.ID, false); c != 409 {
		t.Errorf("required: %d", c)
	}
	if put(gBack.ID, false) != 200 || put(gInfra.ID, true) != 200 {
		t.Fatal("choices refused")
	}
	if got := off(me()); !got["Бэкенд"] || got["Инфра"] {
		t.Errorf("after the choices: %v", got)
	}
	if got := onMachine(); got["Бэкенд"] || !got["Инфра"] || !got["Ядро"] {
		t.Errorf("machine after the choices: %v", got)
	}
	// A deleted group leaves its items in, without a group.
	if r := a.anna.Do("DELETE", a.url("/env/groups/"+gBack.ID), nil); r.Code != 204 {
		t.Fatalf("delete: %s", r)
	}
	if got := off(me()); got["Бэкенд"] {
		t.Errorf("the item of a deleted group: %v", got)
	}
}
