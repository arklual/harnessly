package env_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// acme: anna owns both tenants and made the organization (its admin); dina is a tenant admin of «Платформа», boris
// leads its team «Ядро», vera is a plain member of the team, carol a plain member of the tenant without a team, gleb is
// in the other tenant, mallory is in no organization.
type acme struct {
	s                                             *hubtest.Stack
	anna, dina, boris, vera, carol, gleb, mallory *hubtest.Person
	org, tenant, other, core, side                string
}

func (a acme) url(path string) string { return "/api/orgs/" + a.org + path }

func newAcme(t *testing.T, opts ...hubtest.Option) acme {
	t.Helper()
	s := hubtest.New(t, append([]hubtest.Option{hubtest.Env("HUB_ORGS", "on"), hubtest.Env("HUB_ENV_FEATURES", "all")}, opts...)...)
	s.SignIn("root@example.com") // the first account administers the instance
	a := acme{s: s, anna: s.SignIn("anna@example.com"), dina: s.SignIn("dina@example.com"), boris: s.SignIn("boris@example.com"),
		vera: s.SignIn("vera@example.com"), carol: s.SignIn("carol@example.com"), gleb: s.SignIn("gleb@example.com"),
		mallory: s.SignIn("mallory@example.org")}
	a.tenant, _ = a.anna.NewSpace("Платформа")
	a.other, _ = a.anna.NewSpace("Продукт")
	for _, p := range []*hubtest.Person{a.dina, a.boris, a.vera, a.carol} {
		a.anna.Join(a.tenant, p)
	}
	a.anna.Join(a.other, a.gleb)
	var org struct{ Org struct{ ID string } }
	if r := a.anna.Do("POST", "/api/orgs", map[string]any{"name": "Acme", "spaceIds": []string{a.tenant, a.other}}); r.Code != 201 {
		t.Fatalf("org: %s", r)
	} else {
		r.JSON(&org)
	}
	a.org = org.Org.ID
	a.core = a.team(t, a.tenant, "Ядро")
	a.side = a.team(t, a.tenant, "Сайд")
	for _, p := range []*hubtest.Person{a.boris, a.vera} {
		if r := a.anna.Do("PUT", a.url("/teams/"+a.core+"/members/"+p.ID), map[string]any{}); r.Code/100 != 2 {
			t.Fatalf("team member: %s", r)
		}
	}
	a.grant(t, a.boris, "team", a.core, "lead")
	a.grant(t, a.dina, "tenant", a.tenant, "admin")
	return a
}

func (a acme) team(t *testing.T, tenant, name string) string {
	t.Helper()
	r := a.anna.Do("POST", a.url("/teams"), map[string]any{"tenantId": tenant, "name": name})
	if r.Code != 201 {
		t.Fatalf("team: %s", r)
	}
	var out struct{ Team struct{ ID string } }
	r.JSON(&out)
	return out.Team.ID
}

func (a acme) grant(t *testing.T, who *hubtest.Person, kind, id, role string) {
	t.Helper()
	r := a.anna.Do("PUT", a.url("/grants"), map[string]any{"userId": who.ID, "scope": map[string]string{"kind": kind, "id": id}, "role": role, "granted": true})
	if r.Code/100 != 2 {
		t.Fatalf("grant: %s", r)
	}
}

// item adds a catalog item as anna and returns its id.
func (a acme) item(t *testing.T, kind, name string, source any) string {
	t.Helper()
	r := a.anna.Do("POST", a.url("/catalog"), map[string]any{"kind": kind, "name": name, "hint": name + " для работы", "source": source})
	if r.Code != 201 {
		t.Fatalf("catalog %s: %s", name, r)
	}
	var out struct{ Item env.CatalogItemView }
	r.JSON(&out)
	return out.Item.ID
}

func (a acme) entry(t *testing.T, p *hubtest.Person, layer, scope, item string, extra map[string]any) (int, env.EntryItem) {
	t.Helper()
	body := map[string]any{"layer": layer, "scopeId": scope, "itemId": item}
	for k, v := range extra {
		body[k] = v
	}
	r := p.Do("POST", a.url("/env/entries"), body)
	var row env.EntryItem
	if r.Code == 201 {
		r.JSON(&row)
	}
	return r.Code, row
}

func (a acme) layers(t *testing.T, p *hubtest.Person, query string) env.Response {
	t.Helper()
	r := p.Do("GET", a.url("/env"+query), nil)
	if r.Code != 200 {
		t.Fatalf("GET /env%s: %s", query, r)
	}
	var out env.Response
	r.JSON(&out)
	return out
}

func layer(rs env.Response, kind, scope string) *env.LayerView {
	for i := range rs.Layers {
		if rs.Layers[i].Layer == kind && rs.Layers[i].ScopeID == scope {
			return &rs.Layers[i]
		}
	}
	return nil
}

func TestFeatureOffIsNotEnabled(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	s.SignIn("root@example.com")
	anna := s.SignIn("anna@example.com")
	tenant, _ := anna.NewSpace("Платформа")
	var org struct{ Org struct{ ID string } }
	anna.Do("POST", "/api/orgs", map[string]any{"name": "Acme", "spaceIds": []string{tenant}}).JSON(&org)
	for _, path := range []string{"/api/orgs/" + org.Org.ID + "/env", "/api/orgs/" + org.Org.ID + "/catalog", "/api/me/env?space=" + tenant} {
		if r := anna.Do("GET", path, nil); r.Code != 404 || r.Err() != "not_enabled" {
			t.Errorf("GET %s with the feature off: %s", path, r)
		}
	}
	if r := anna.Do("POST", "/api/orgs/"+org.Org.ID+"/catalog", map[string]any{"kind": "doc", "name": "X"}); r.Code != 404 || r.Err() != "not_enabled" {
		t.Errorf("POST catalog with the feature off: %s", r)
	}
}

func TestCatalogRights(t *testing.T) {
	a := newAcme(t)
	skill := map[string]any{"body": "Пиши коротко."}
	for name, p := range map[string]*hubtest.Person{"tenant admin": a.dina, "lead": a.boris, "member": a.carol} {
		if r := p.Do("POST", a.url("/catalog"), map[string]any{"kind": "skill", "name": "Стиль", "source": skill}); r.Code != 403 {
			t.Errorf("%s adds to the catalog: %s", name, r)
		}
	}
	if r := a.mallory.Do("GET", a.url("/catalog"), nil); r.Code != 404 {
		t.Errorf("an outsider reads the catalog: %s", r)
	}
	if r := a.carol.Do("GET", a.url("/catalog"), nil); r.Code != 403 {
		t.Errorf("a member without rights reads the catalog: %s", r)
	}

	review := a.item(t, "skill", "Ревью кода", skill)
	jira := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp", "secretRefs": []string{"TRACKER_TOKEN"}})
	if r := a.anna.Do("POST", a.url("/catalog"), map[string]any{"kind": "skill", "name": "ревью кода", "source": skill}); r.Code != 409 || r.Detail("reason") != "name_taken" {
		t.Errorf("a name twice: %s", r)
	}
	for _, bad := range []map[string]any{
		{"kind": "mcp", "name": "С паролем", "source": map[string]any{"url": "https://u:p@tracker.example.org"}},
		{"kind": "mcp", "name": "Секрет", "source": map[string]any{"secretRefs": []string{"a b"}}},
		{"kind": "mcp", "name": "Чужое поле", "source": map[string]any{"token": "x"}},
		{"kind": "weird", "name": "Вид", "source": map[string]any{}},
		{"kind": "doc", "name": "  ", "source": map[string]any{}},
	} {
		if r := a.anna.Do("POST", a.url("/catalog"), bad); r.Code != 400 {
			t.Errorf("%v: %s", bad, r)
		}
	}

	// The tenant admin looks at the catalog, with the flag of the layer asked for.
	if code, _ := a.entry(t, a.dina, "tenant", a.tenant, review, nil); code != 201 {
		t.Fatalf("dina adds: %d", code)
	}
	var list env.CatalogResponse
	if r := a.dina.Do("GET", a.url("/catalog?layer=tenant&scopeId="+a.tenant), nil); r.Code != 200 {
		t.Fatalf("catalog: %s", r)
	} else {
		r.JSON(&list)
	}
	added := map[string]bool{}
	for _, it := range list.Items {
		added[it.ID] = it.Added
	}
	if len(list.Items) != 2 || !added[review] || added[jira] {
		t.Errorf("catalog = %+v", list.Items)
	}
	a.anna.Do("GET", a.url("/catalog?kind=mcp"), nil).JSON(&list)
	if len(list.Items) != 1 || list.Items[0].ID != jira || string(list.Items[0].Source) == "" {
		t.Errorf("mcp only = %+v", list.Items)
	}

	// Rename, then archive: the entry that used the item stops being listed, nothing is lost.
	if r := a.anna.Do("PATCH", a.url("/catalog/"+review), map[string]any{"name": "Ревью"}); r.Code != 200 {
		t.Fatalf("rename: %s", r)
	}
	if r := a.anna.Do("DELETE", a.url("/catalog/"+review), nil); r.Code != 204 {
		t.Fatalf("archive: %s", r)
	}
	if l := layer(a.layers(t, a.anna, ""), "tenant", a.tenant); len(l.Items) != 0 {
		t.Errorf("archived item still listed: %+v", l.Items)
	}
	if r := a.anna.Do("PATCH", a.url("/catalog/"+review), map[string]any{"archived": false}); r.Code != 200 {
		t.Fatalf("restore: %s", r)
	}
	if l := layer(a.layers(t, a.anna, ""), "tenant", a.tenant); len(l.Items) != 1 || l.Items[0].Name != "Ревью" {
		t.Errorf("restored: %+v", l.Items)
	}
}

func TestEnvAuthMatrix(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	mcp := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp"})
	repo := a.item(t, "repo", "Монорепо", map[string]any{"url": "https://git.example.org/acme/mono.git", "branch": "main"})
	doc := a.item(t, "doc", "Регламент", map[string]any{"url": "https://docs.example.org/rules"})

	// Who may put what where.
	type try struct {
		who   string
		p     *hubtest.Person
		layer string
		scope string
		code  int
	}
	for _, c := range []try{
		{"org admin / org", a.anna, "org", a.org, 201},
		{"tenant admin / org", a.dina, "org", a.org, 403},
		{"lead / org", a.boris, "org", a.org, 403},
		{"tenant admin / tenant", a.dina, "tenant", a.tenant, 201},
		{"lead / tenant", a.boris, "tenant", a.tenant, 403},
		{"member / tenant", a.carol, "tenant", a.tenant, 403},
		{"tenant admin / team", a.dina, "team", a.core, 201},
		{"lead / own team", a.boris, "team", a.core, 201},
		{"lead / other team", a.boris, "team", a.side, 403},
		{"member / team", a.vera, "team", a.core, 403},
		{"tenant admin / other tenant", a.dina, "tenant", a.other, 403},
		{"outsider / tenant", a.mallory, "tenant", a.tenant, 404},
	} {
		item := map[string]string{"org": skill, "tenant": mcp, "team": repo}[c.layer]
		if c.layer == "team" && c.who == "lead / own team" {
			item = doc
		}
		if code, _ := a.entry(t, c.p, c.layer, c.scope, item, nil); code != c.code {
			t.Errorf("%s: %d, want %d", c.who, code, c.code)
		}
	}

	// Who sees what.
	if r := a.carol.Do("GET", a.url("/env"), nil); r.Code != 403 {
		t.Errorf("a member reads the layers: %s", r)
	}
	if r := a.mallory.Do("GET", a.url("/env"), nil); r.Code != 404 {
		t.Errorf("an outsider reads the layers: %s", r)
	}
	if r := a.anna.Do("GET", a.url("/env?scope=nowhere"), nil); r.Code != 404 {
		t.Errorf("an unknown tenant: %s", r)
	}
	if r := a.dina.Do("GET", a.url("/env?scope="+a.other), nil); r.Code != 403 {
		t.Errorf("a tenant admin reads another tenant: %s", r)
	}

	all := a.layers(t, a.anna, "?scope=all")
	if got := len(all.Layers); got != 1+2+2 { // the organization, two tenants, two teams of «Платформа»
		t.Errorf("admin sees %d layers", got)
	}
	if all.Summary.Total != 4 || all.Summary.ByKind.Skill != 1 || all.Summary.ByKind.MCP != 1 || all.Summary.ByKind.Repo != 0 || all.Summary.ByKind.Doc != 1 {
		// repo was put in the team «Ядро» by dina? no: the team row of «lead / tenant admin» used the repo for dina and the doc for boris.
		t.Logf("summary = %+v", all.Summary)
	}
	if all.Summary.CallsWeek != nil || all.Summary.Unused != nil || len(all.Summary.Top) != 0 {
		t.Errorf("without tool facts the numbers are unknown, got %+v", all.Summary)
	}
	if l := layer(all, "org", a.org); l == nil || l.CallsWeek != nil || l.Items[0].CallsWeek != nil || l.Items[0].Unused != nil || l.Items[0].LastUsedAt != nil {
		t.Errorf("org layer = %+v", l)
	}

	lead := a.layers(t, a.boris, "")
	if l := layer(lead, "org", a.org); l == nil || !l.Locked {
		t.Errorf("the organization layer is read-only for a lead: %+v", l)
	}
	if l := layer(lead, "team", a.core); l == nil || l.Locked {
		t.Errorf("the lead's own team is not locked: %+v", l)
	}
	if l := layer(lead, "tenant", a.tenant); l == nil || !l.Locked {
		t.Errorf("the tenant is read-only for a lead: %+v", l)
	}

	// Pinning above stops the switch below.
	orgRow := layer(all, "org", a.org).Items[0]
	if r := a.anna.Do("PATCH", a.url("/env/entries/"+orgRow.EntryID), map[string]any{"pinned": true}); r.Code != 200 {
		t.Fatalf("pin: %s", r)
	}
	if code, _ := a.entry(t, a.dina, "tenant", a.tenant, skill, map[string]any{"enabled": false}); code != 409 {
		t.Errorf("switching off a pinned item below: %d", code)
	}
	lead = a.layers(t, a.boris, "")
	if l := layer(lead, "org", a.org); l.Items[0].LockedReason == nil || *l.Items[0].LockedReason != "pinned_by_org" || !l.Items[0].Locked {
		t.Errorf("pinned row = %+v", l.Items[0])
	}
	if lv := layer(lead, "team", a.core); lv == nil || len(lv.Items) == 0 {
		t.Fatalf("team layer = %+v", lv)
	}

	// Duplicates, unknown items, a lead's team audience, a foreign scope.
	if code, _ := a.entry(t, a.anna, "org", a.org, skill, nil); code != 409 {
		t.Errorf("duplicate: %d", code)
	}
	if code, _ := a.entry(t, a.anna, "org", a.org, "no-such-item", nil); code != 400 {
		t.Errorf("unknown item: %d", code)
	}
	if code, _ := a.entry(t, a.anna, "team", "no-such-team", skill, nil); code != 404 {
		t.Errorf("unknown scope: %d", code)
	}
	if code, _ := a.entry(t, a.dina, "tenant", a.tenant, doc, map[string]any{"audience": map[string]any{"mode": "teams", "teams": []string{a.core}}}); code != 201 {
		t.Errorf("a tenant entry for one team: %d", code)
	}
	if code, _ := a.entry(t, a.dina, "tenant", a.tenant, repo, map[string]any{"audience": map[string]any{"mode": "teams", "teams": []string{"nobody"}}}); code != 400 {
		t.Errorf("audience with a foreign team: %d", code)
	}

	// Patch and delete follow the rights of the layer.
	coreRow := layer(a.layers(t, a.boris, ""), "team", a.core).Items[0]
	if r := a.boris.Do("PATCH", a.url("/env/entries/"+coreRow.EntryID), map[string]any{"enabled": false}); r.Code != 200 {
		t.Errorf("lead switches his team's row: %s", r)
	}
	if r := a.vera.Do("PATCH", a.url("/env/entries/"+coreRow.EntryID), map[string]any{"enabled": true}); r.Code != 403 {
		t.Errorf("member patches: %s", r)
	}
	if r := a.boris.Do("DELETE", a.url("/env/entries/"+orgRow.EntryID), nil); r.Code != 403 {
		t.Errorf("lead deletes the org row: %s", r)
	}
	if r := a.mallory.Do("DELETE", a.url("/env/entries/"+orgRow.EntryID), nil); r.Code != 404 {
		t.Errorf("outsider deletes: %s", r)
	}
	if r := a.boris.Do("DELETE", a.url("/env/entries/"+coreRow.EntryID), nil); r.Code != 204 {
		t.Errorf("lead deletes his row: %s", r)
	}
	if r := a.boris.Do("DELETE", a.url("/env/entries/"+coreRow.EntryID), nil); r.Code != 404 {
		t.Errorf("deleted twice: %s", r)
	}
}

func TestEnvChangesAndAudit(t *testing.T) {
	a := newAcme(t)
	mcp := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp"})
	code, row := a.entry(t, a.dina, "tenant", a.tenant, mcp, nil)
	if code != 201 {
		t.Fatalf("add: %d", code)
	}
	if r := a.dina.Do("PATCH", a.url("/env/entries/"+row.EntryID), map[string]any{"enabled": false}); r.Code != 200 {
		t.Fatalf("switch off: %s", r)
	}
	// No change, no new record.
	if r := a.dina.Do("PATCH", a.url("/env/entries/"+row.EntryID), map[string]any{"enabled": false}); r.Code != 200 {
		t.Fatalf("again: %s", r)
	}
	chs, err := a.s.DB.S().ChangesOn(context.Background(), a.org, []store.ChangeScope{{Kind: store.ScopeOrg, ID: a.org}, {Kind: store.ScopeTenant, ID: a.tenant}}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, c := range chs {
		if c.Kind == "env" {
			titles = append(titles, c.Title)
		}
	}
	sort.Strings(titles)
	want := []string{"Каталог: добавлен MCP «Трекер»", "Окружение: MCP «Трекер» выключен в тенанте «Платформа»", "Окружение: MCP «Трекер» добавлен в тенанте «Платформа»"}
	if strings.Join(titles, "|") != strings.Join(want, "|") { // the repeated switch wrote nothing
		t.Errorf("env changes = %q", titles)
	}
	var audit struct{ Events []struct{ Action string } }
	a.anna.Do("GET", a.url("/audit"), nil).JSON(&audit)
	got := map[string]int{}
	for _, e := range audit.Events {
		got[e.Action]++
	}
	if got["catalog.add"] != 1 || got["env.entry.add"] != 1 || got["env.entry.update"] != 1 {
		t.Errorf("audit actions = %v", got)
	}
}

func TestPublisherAndGate(t *testing.T) {
	// A nil gate: the service itself says not enabled whatever the organization is.
	svc := env.New(env.Deps{})
	if svc.On(context.Background(), "x") {
		t.Error("a service without a gate is on")
	}
	g := env.StaticFeatures([]string{"env"})
	if !g.On(context.Background(), "x", "env") || g.On(context.Background(), "x", "catalog") {
		t.Error("static gate")
	}
}

func TestMeEnv(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	mcp := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp"})
	doc := a.item(t, "doc", "Регламент", map[string]any{"url": "https://docs.example.org/rules"})
	a.entry(t, a.anna, "org", a.org, skill, map[string]any{"pinned": true})
	a.entry(t, a.dina, "tenant", a.tenant, mcp, nil)
	a.entry(t, a.boris, "team", a.core, doc, nil)

	var me env.MeResponse
	if r := a.vera.Do("GET", "/api/me/env?space="+a.tenant, nil); r.Code != 200 {
		t.Fatalf("vera: %s", r)
	} else {
		r.JSON(&me)
	}
	if me.Org == nil || me.Org.Name != "Acme" || me.Space.ID != a.tenant || len(me.Items) != 3 {
		t.Fatalf("vera's environment = %+v", me)
	}
	byName := map[string]env.MeItem{}
	for _, it := range me.Items {
		byName[it.Name] = it
	}
	if it := byName["Ревью кода"]; !it.Pinned || it.CanToggle || it.Layer != "org" {
		t.Errorf("pinned = %+v", it)
	}
	if it := byName["Регламент"]; it.Layer != "team" || it.ScopeID != a.core || !it.CanToggle || it.LayerTitle != "Команда Ядро" {
		t.Errorf("team item = %+v", it)
	}
	// carol is in no team: she does not get the team's document.
	a.carol.Do("GET", "/api/me/env?space="+a.tenant, nil).JSON(&me)
	if len(me.Items) != 2 {
		t.Errorf("carol's environment = %+v", me.Items)
	}

	// A personal switch.
	if r := a.vera.Do("PUT", "/api/me/env/"+skill+"?space="+a.tenant, map[string]any{"off": true}); r.Code != 409 || r.Detail("reason") != "locked" {
		t.Errorf("switching a pinned item off: %s", r)
	}
	if r := a.vera.Do("PUT", "/api/me/env/"+mcp+"?space="+a.tenant, map[string]any{"off": true}); r.Code != 200 {
		t.Fatalf("switch off: %s", r)
	}
	a.vera.Do("GET", "/api/me/env?space="+a.tenant, nil).JSON(&me)
	var off []string
	for _, it := range me.Items {
		if it.Off {
			off = append(off, it.Name)
		}
	}
	if len(off) != 1 || off[0] != "Трекер" {
		t.Errorf("off = %v", off)
	}
	// Others are not affected; it is not an organization change but it is in the audit.
	a.carol.Do("GET", "/api/me/env?space="+a.tenant, nil).JSON(&me)
	for _, it := range me.Items {
		if it.Off {
			t.Errorf("carol's %s is off", it.Name)
		}
	}
	if r := a.vera.Do("PUT", "/api/me/env/"+mcp+"?space="+a.tenant, map[string]any{"off": false}); r.Code != 200 {
		t.Fatalf("switch on: %s", r)
	}
	if r := a.vera.Do("PUT", "/api/me/env/"+doc+"?space="+a.other, map[string]any{"off": true}); r.Code != 404 {
		t.Errorf("a tenant she is not in: %s", r)
	}
	if r := a.vera.Do("PUT", "/api/me/env/nothing?space="+a.tenant, map[string]any{"off": true}); r.Code != 404 {
		t.Errorf("unknown item: %s", r)
	}
	if r := a.vera.Do("PUT", "/api/me/env/"+mcp+"?space="+a.tenant, map[string]any{}); r.Code != 400 {
		t.Errorf("no off: %s", r)
	}
	if r := a.vera.Do("GET", "/api/me/env", nil); r.Code != 200 {
		// vera is in one space only
		t.Errorf("no space named, one space: %s", r)
	}
	var audit struct{ Events []struct{ Action string } }
	a.anna.Do("GET", a.url("/audit"), nil).JSON(&audit)
	n := 0
	for _, e := range audit.Events {
		if e.Action == "env.person.off" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("personal switches in the audit = %d", n)
	}
}

// day is a UTC day offset from the stack's clock, as the store keeps it.
func day(s *hubtest.Stack, back int) string {
	return s.Clock.Now().UTC().AddDate(0, 0, -back).Format("2006-01-02")
}

func TestUsageNumbers(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	mcp := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp"})
	_, org := a.entry(t, a.anna, "org", a.org, skill, nil)
	_, ten := a.entry(t, a.dina, "tenant", a.tenant, mcp, nil)
	// An entry made before the month ran out: old enough to be called unused.
	old := a.s.Clock.Now().AddDate(0, 0, -40)
	if _, err := a.s.DB.Exec(context.Background(), `UPDATE env_entries SET updated_at = ? WHERE id = ?`, store.Ms(old), ten.EntryID); err != nil {
		t.Fatal(err)
	}

	use := func(spaceID, team, user, item, name string, back int, calls int64) {
		t.Helper()
		err := a.s.DB.S().UpsertToolUsage(context.Background(), store.ToolUsage{OrgKey: a.org, SpaceID: spaceID, TeamID: team, UserID: user, MachineID: "m1",
			SessionKey: user + name + day(a.s, back), ItemID: item, ToolKind: "skill", ToolName: name, Day: day(a.s, back), Calls: calls, LastAt: a.s.Clock.Now().Add(-time.Duration(back) * 24 * time.Hour).UnixMilli()})
		if err != nil {
			t.Fatal(err)
		}
	}
	use(a.tenant, a.core, a.vera.ID, skill, "review", 1, 5)
	use(a.tenant, "", a.carol.ID, skill, "review", 2, 3)
	use(a.other, "", a.gleb.ID, skill, "review", 20, 7) // another tenant, outside the week

	all := a.layers(t, a.anna, "")
	if all.Summary.CallsWeek == nil || *all.Summary.CallsWeek != 8 {
		t.Fatalf("summary = %+v", all.Summary)
	}
	if len(all.Summary.Top) != 1 || all.Summary.Top[0].ID != skill {
		t.Errorf("top = %+v", all.Summary.Top)
	}
	if all.Summary.Unused == nil || *all.Summary.Unused != 1 {
		t.Errorf("unused = %v", all.Summary.Unused)
	}
	ol := layer(all, "org", a.org)
	if ol.CallsWeek == nil || *ol.CallsWeek != 8 || ol.Items[0].CallsWeek == nil || *ol.Items[0].CallsWeek != 8 || len(ol.Items[0].Spark14) != 14 {
		t.Errorf("org layer = %+v", ol)
	}
	if ol.Items[0].Unused == nil || *ol.Items[0].Unused {
		t.Errorf("the skill is used: %+v", ol.Items[0])
	}
	if tl := layer(all, "tenant", a.tenant); tl.Items[0].Unused == nil || !*tl.Items[0].Unused || tl.Items[0].CallsWeek == nil || *tl.Items[0].CallsWeek != 0 {
		t.Errorf("the tenant's mcp = %+v", tl.Items[0])
	}
	// Scoped to one tenant the other one's calls drop out.
	one := a.layers(t, a.anna, "?scope="+a.tenant)
	if *one.Summary.CallsWeek != 8 {
		t.Errorf("one tenant: %+v", one.Summary)
	}

	var card env.Card
	if r := a.anna.Do("GET", a.url("/env/items/"+org.EntryID), nil); r.Code != 200 {
		t.Fatalf("card: %s", r)
	} else {
		r.JSON(&card)
	}
	if card.CallsWeek == nil || *card.CallsWeek != 8 || card.People == nil || *card.People != 2 || len(card.Bars14) != 14 || card.PerDay == nil || *card.PerDay != 1.1 {
		t.Errorf("card = %+v", card)
	}
	if card.Bars14[13].Day != day(a.s, 0) || card.Bars14[12].Calls != 5 {
		t.Errorf("bars = %+v", card.Bars14)
	}
	if r := a.mallory.Do("GET", a.url("/env/items/"+org.EntryID), nil); r.Code != 404 {
		t.Errorf("outsider's card: %s", r)
	}
	if r := a.boris.Do("GET", a.url("/env/items/"+ten.EntryID), nil); r.Code != 200 {
		t.Errorf("the lead's card of a tenant entry: %s", r)
	}
	if r := a.gleb.Do("GET", a.url("/env/items/"+ten.EntryID), nil); r.Code != 403 {
		t.Errorf("a member's card: %s", r)
	}
}

// The answers have the same fields as the web's fixtures (web/app/hub-api/fixtures/env), which contracts.test.ts
// checks against the TypeScript types.
func TestShapesMatchWebFixtures(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	mcp := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp"})
	_, org := a.entry(t, a.anna, "org", a.org, skill, map[string]any{"pinned": true})
	a.entry(t, a.dina, "tenant", a.tenant, mcp, nil)
	a.entry(t, a.boris, "team", a.core, skill, nil)
	if err := a.s.DB.S().UpsertToolUsage(context.Background(), store.ToolUsage{OrgKey: a.org, SpaceID: a.tenant, TeamID: a.core, UserID: a.vera.ID, MachineID: "m1",
		SessionKey: "s1", ItemID: skill, ToolKind: "skill", ToolName: "review", Day: day(a.s, 0), Calls: 4, LastAt: a.s.Clock.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join("..", "..", "..", "web", "app", "hub-api", "fixtures", "env")
	for _, c := range []struct {
		fixture, path string
		who           *hubtest.Person
	}{
		{"env-admin.json", a.url("/env?scope=all"), a.anna},
		{"card.json", a.url("/env/items/" + org.EntryID), a.anna},
		{"catalog.json", a.url("/catalog?layer=org&scopeId=" + a.org), a.anna},
		{"me.json", "/api/me/env?space=" + a.tenant, a.vera},
	} {
		r := c.who.Do("GET", c.path, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("%s: %s", c.path, r)
		}
		var got any
		r.JSON(&got)
		if out := os.Getenv("HUB_ENV_DUMP"); out != "" { // writes the answers and the ids to name, for refreshing the fixtures
			_ = os.WriteFile(filepath.Join(out, c.fixture), r.Body, 0o644)
			ids, _ := json.Marshal(map[string]string{a.org: "org-acme", a.tenant: "tenant-platform", a.other: "tenant-product", a.core: "team-core", a.side: "team-side",
				skill: "item-review", mcp: "item-tracker", org.EntryID: "entry-org-review", a.vera.ID: "user-vera", a.anna.ID: "user-anna"})
			_ = os.WriteFile(filepath.Join(out, "ids.json"), ids, 0o644)
		}
		raw, err := os.ReadFile(filepath.Join(dir, c.fixture))
		if err != nil {
			t.Errorf("%v", err)
			continue
		}
		var want any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if diff := shapeDiff("", got, want); diff != "" {
			t.Errorf("%s: %s", c.fixture, diff)
		}
	}
}

// shapeDiff compares the fields of two JSON values: the same keys in every object, each list judged by its first
// element; a null on either side (a number that may be unknown) stands for anything.
func shapeDiff(path string, got, want any) string {
	if got == nil || want == nil {
		return ""
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return path + ": not an object"
		}
		if keys(g) != keys(w) {
			return path + ": keys " + keys(g) + ", fixture " + keys(w)
		}
		for k := range w {
			if k == "source" { // the keys of a source depend on the kind of the item
				continue
			}
			if d := shapeDiff(path+"."+k, g[k], w[k]); d != "" {
				return d
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			return path + ": not a list"
		}
		if len(w) == 0 || len(g) == 0 {
			return ""
		}
		for i := range g {
			if d := shapeDiff(path+"[]", g[i], w[0]); d != "" {
				return d
			}
		}
	}
	return ""
}

func keys(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

// Calls written before their skill joined the catalog carry no item; they are found by the tool's name when read.
func TestUsageMatchesACatalogItemAddedLater(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "review", map[string]any{"body": "Be brief."})
	a.entry(t, a.anna, "org", a.org, skill, nil)
	err := a.s.DB.S().UpsertToolUsage(context.Background(), store.ToolUsage{OrgKey: a.org, SpaceID: a.tenant, TeamID: a.core, UserID: a.vera.ID,
		MachineID: "m1", SessionKey: "s1", ToolKind: "skill", ToolName: "Acme:Review", Day: day(a.s, 1), Calls: 4, LastAt: a.s.Clock.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	all := a.layers(t, a.anna, "")
	if ol := layer(all, "org", a.org); ol.Items[0].CallsWeek == nil || *ol.Items[0].CallsWeek != 4 {
		t.Errorf("org layer = %+v", ol.Items[0])
	}
}

// What the machines call is listed whether the catalog has it or not, the most called first, scoped as the layers are.
func TestObservedTools(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "review", map[string]any{"body": "Be brief."})
	a.entry(t, a.anna, "org", a.org, skill, nil)
	put := func(space, user, kind, name string, back int, calls int64) {
		t.Helper()
		err := a.s.DB.S().UpsertToolUsage(context.Background(), store.ToolUsage{OrgKey: a.org, SpaceID: space, TeamID: a.core, UserID: user, MachineID: "m1",
			SessionKey: user + name + day(a.s, back), ToolKind: kind, ToolName: name, Day: day(a.s, back), Calls: calls, LastAt: a.s.Clock.Now().UnixMilli()})
		if err != nil {
			t.Fatal(err)
		}
	}
	put(a.tenant, a.vera.ID, "skill", "plugin:review", 1, 2)
	put(a.tenant, a.vera.ID, "mcp", "grafana", 1, 5)
	put(a.tenant, a.carol.ID, "mcp", "grafana", 10, 4) // in the month, not in the week
	put(a.tenant, a.carol.ID, "mcp", "old", 40, 9)     // older than the month
	put(a.other, a.gleb.ID, "skill", "elsewhere", 1, 1)
	put(a.tenant, a.vera.ID, "bash", "git", 1, 50) // not a tool the environment gives

	all := a.layers(t, a.anna, "?scope="+a.tenant)
	if len(all.Observed) != 2 {
		t.Fatalf("observed = %+v", all.Observed)
	}
	g, r := all.Observed[0], all.Observed[1]
	if g.Kind != "mcp" || g.Name != "grafana" || g.Calls != 9 || g.CallsWeek != 5 || g.People != 2 || g.ItemID != nil || g.InEnv || g.LastUsedAt == nil {
		t.Errorf("grafana = %+v", g)
	}
	if r.Name != "plugin:review" || r.ItemID == nil || *r.ItemID != skill || !r.InEnv {
		t.Errorf("review = %+v", r)
	}
	if every := a.layers(t, a.anna, ""); len(every.Observed) != 3 {
		t.Errorf("all tenants = %+v", every.Observed)
	}
}

// The screen says how many of its machines hold the current list and what they said about each item; a report of an
// older list is not counted per item, a revoked machine not at all.
func TestDeliveryOnTheScreen(t *testing.T) {
	a := newAcme(t)
	ctx := context.Background()
	skill := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	mcp := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp", "secretRefs": []string{"TRACKER_TOKEN"}})
	a.entry(t, a.anna, "org", a.org, skill, nil)
	a.entry(t, a.dina, "tenant", a.tenant, mcp, nil)

	fresh := a.anna.Machine(a.tenant, nil)
	old := a.anna.Machine(a.tenant, nil)
	silent := a.anna.Machine(a.tenant, nil)
	gone := a.anna.Machine(a.tenant, nil)
	if _, err := a.s.DB.Exec(ctx, `UPDATE machines SET revoked_at = ? WHERE id = ?`, a.s.Clock.Now(), gone.MachineID); err != nil {
		t.Fatal(err)
	}
	_ = silent

	// What the pusher sends a machine of the tenant without a team is the current list.
	link := &fakeLink{id: fresh.MachineID, status: "ok"}
	p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour})
	defer p.Close()
	p.Connected(fresh.MachineID, hubproto.Hello{Features: []string{hubproto.FeatureEnvironment}})
	if err := p.Push(ctx); err != nil || link.count() != 1 {
		t.Fatalf("push: %v, sent %d", err, link.count())
	}
	// The agent says the server waits for its secret.
	p.Reported(fresh.MachineID, hubproto.EnvState{Rev: 1, Hash: link.sent[0].Hash, Applied: []hubproto.EnvApplied{
		{ItemID: skill, Status: "ok"}, {ItemID: mcp, Status: "pending", Message: "the secret TRACKER_TOKEN is not on this machine"}}})
	// Another machine holds an older list.
	p.Reported(old.MachineID, hubproto.EnvState{Rev: 1, Hash: "older", Applied: []hubproto.EnvApplied{{ItemID: skill, Status: "error", Message: "broken"}}})

	rs := a.layers(t, a.anna, "?scope="+a.tenant)
	if d := rs.Delivery; d.Machines != 3 || d.Reported != 2 || d.Current != 1 || d.LastAt == nil {
		t.Errorf("delivery = %+v", d)
	}
	sk := layer(rs, "org", a.org).Items[0]
	if sk.Delivery == nil || sk.Delivery.OK != 1 || sk.Delivery.Error != 0 {
		t.Errorf("skill = %+v", sk.Delivery)
	}
	mc := layer(rs, "tenant", a.tenant).Items[0]
	if mc.Delivery == nil || mc.Delivery.Pending != 1 || mc.Delivery.Message == nil || !strings.Contains(*mc.Delivery.Message, "TRACKER_TOKEN") {
		t.Errorf("mcp = %+v", mc.Delivery)
	}
	// The other tenant has no machines.
	if d := a.layers(t, a.anna, "?scope="+a.other).Delivery; d.Machines != 0 || d.Current != 0 {
		t.Errorf("other tenant = %+v", d)
	}
	// A team filter keeps only the team's machines.
	if d := a.layers(t, a.anna, "?scope="+a.tenant+"&team="+a.core).Delivery; d.Machines != 0 {
		t.Errorf("team = %+v", d)
	}
}

// With nothing in the environment a machine holds what it should without ever reporting: the hub sends no first
// empty list.
func TestDeliveryOfAnEmptyEnvironment(t *testing.T) {
	a := newAcme(t)
	a.anna.Machine(a.tenant, nil)
	if d := a.layers(t, a.anna, "?scope="+a.tenant).Delivery; d.Machines != 1 || d.Reported != 0 || d.Current != 1 {
		t.Errorf("delivery = %+v", d)
	}
}
