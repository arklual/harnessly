package env_test

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestPresets(t *testing.T) {
	a := newAcme(t)
	core := a.item(t, "repo", "Ядро", map[string]any{"url": "https://git.example.org/a/core.git"})
	back := a.item(t, "repo", "Бэкенд", map[string]any{"url": "https://git.example.org/a/back.git"})
	front := a.item(t, "repo", "Фронт", map[string]any{"url": "https://git.example.org/a/front.git"})
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
	gFront := group(map[string]any{"name": "Фронт", "defaultOn": false})
	gInfra := group(map[string]any{"name": "Инфра", "defaultOn": false})
	for item, g := range map[string]env.GroupView{core: gCore, back: gBack, front: gFront, infra: gInfra} {
		a.entry(t, a.anna, "org", a.org, item, map[string]any{"groupId": g.ID})
	}
	// A group of another layer.
	other := a.dina.Do("POST", a.url("/env/groups"), map[string]any{"layer": "tenant", "scopeId": a.tenant, "name": "Свои"})
	if other.Code != 201 {
		t.Fatalf("tenant group: %s", other)
	}
	var tg struct{ Group env.GroupView }
	other.JSON(&tg)

	preset := func(body map[string]any) env.PresetView {
		t.Helper()
		body["layer"], body["scopeId"] = "org", a.org
		r := a.anna.Do("POST", a.url("/env/presets"), body)
		if r.Code != 201 {
			t.Fatalf("preset: %s", r)
		}
		var out struct{ Preset env.PresetView }
		r.JSON(&out)
		return out.Preset
	}
	pFront := preset(map[string]any{"name": "Фронтендер", "hint": "без инфры", "groupIds": []string{gFront.ID, gCore.ID}})
	pBack := preset(map[string]any{"name": "Бэкендер", "groupIds": []string{gBack.ID}})

	if r := a.anna.Do("POST", a.url("/env/presets"), map[string]any{"layer": "org", "scopeId": a.org, "name": "фронтендер"}); r.Code != 409 {
		t.Errorf("same name: %s", r)
	}
	if r := a.anna.Do("POST", a.url("/env/presets"), map[string]any{"layer": "org", "scopeId": a.org, "name": strings.Repeat("я", 41)}); r.Code != 400 {
		t.Errorf("long name: %s", r)
	}
	if r := a.vera.Do("POST", a.url("/env/presets"), map[string]any{"layer": "org", "scopeId": a.org, "name": "Чужой"}); r.Code != 403 {
		t.Errorf("a member makes no preset: %s", r)
	}
	r := a.anna.Do("POST", a.url("/env/presets"), map[string]any{"layer": "org", "scopeId": a.org, "name": "Не тот слой", "groupIds": []string{tg.Group.ID}})
	if r.Code != 400 || !strings.Contains(r.String(), "groupIds") {
		t.Errorf("a group of another layer: %s", r)
	}
	if r := a.anna.Do("PATCH", a.url("/env/presets/"+pBack.ID), map[string]any{"groupIds": []string{"nope"}}); r.Code != 400 {
		t.Errorf("an unknown group: %s", r)
	}
	if r := a.anna.Do("PATCH", a.url("/env/presets/"+pBack.ID), map[string]any{"hint": "сервисы"}); r.Code != 200 {
		t.Errorf("patch: %s", r)
	}
	if l := layer(a.layers(t, a.anna, ""), "org", a.org); len(l.Presets) != 2 || l.Presets[1].Hint != "сервисы" {
		t.Errorf("presets of the layer = %+v", l.Presets)
	}

	mine := func() env.MeResponse {
		var m env.MeResponse
		a.anna.Do("GET", "/api/me/env?space="+a.tenant, nil).JSON(&m)
		return m
	}
	active := func(m env.MeResponse) map[string]bool {
		out := map[string]bool{}
		for _, p := range m.Presets {
			out[p.Name] = p.Active
		}
		return out
	}
	// By default: backend on, front and infra off: the backend preset is the current one.
	if got := active(mine()); len(got) != 2 || got["Фронтендер"] || !got["Бэкендер"] {
		t.Fatalf("defaults: %v", got)
	}
	if r := a.anna.Do("PUT", "/api/me/env/presets/"+pFront.ID+"?space="+a.tenant, map[string]any{}); r.Code != 200 {
		t.Fatalf("apply: %s", r)
	}
	m := mine()
	if got := active(m); !got["Фронтендер"] || got["Бэкендер"] {
		t.Errorf("after the front preset: %v", got)
	}
	off := map[string]bool{}
	for _, it := range m.Items {
		off[it.Name] = it.Off
	}
	if off["Ядро"] || !off["Бэкенд"] || off["Фронт"] || !off["Инфра"] {
		t.Errorf("items after the preset: %v", off)
	}
	if set, ok := sendTo(t, a, hubproto.FeatureEnvSetup); !ok {
		t.Error("the machine gets the list")
	} else {
		names := map[string]bool{}
		for _, it := range set.Items {
			names[it.Name] = true
		}
		if !names["Ядро"] || !names["Фронт"] || names["Бэкенд"] || names["Инфра"] {
			t.Errorf("machine: %v", names)
		}
	}
	if r := a.anna.Do("PUT", "/api/me/env/presets/nope?space="+a.tenant, map[string]any{}); r.Code != 404 {
		t.Errorf("unknown preset: %s", r)
	}
	// A deleted group leaves the presets.
	if r := a.anna.Do("DELETE", a.url("/env/groups/"+gFront.ID), nil); r.Code != 204 {
		t.Fatalf("delete group: %s", r)
	}
	if l := layer(a.layers(t, a.anna, ""), "org", a.org); len(l.Presets[0].GroupIDs) != 1 {
		t.Errorf("a deleted group stays in a preset: %+v", l.Presets)
	}
	if r := a.anna.Do("DELETE", a.url("/env/presets/"+pFront.ID), nil); r.Code != 204 {
		t.Errorf("delete: %s", r)
	}
}
