package orgs_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func featureState(t *testing.T, p *hubtest.Person, org, name string) (orgs.FeatureView, bool) {
	t.Helper()
	r := p.Do("GET", "/api/orgs/"+org+"/features", nil)
	if r.Code != http.StatusOK {
		t.Fatalf("features: %s", r)
	}
	var out orgs.FeaturesResponse
	r.JSON(&out)
	if len(out.Features) != len(features.Known) {
		t.Fatalf("features = %+v", out.Features)
	}
	for _, f := range out.Features {
		if f.Name == name {
			return f, true
		}
	}
	return orgs.FeatureView{}, false
}

func TestFeaturesAreOffUntilAnAdministratorSwitchesThemOn(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"))
	root := s.SignIn("root@example.com")
	anna := s.SignIn("anna@example.com")
	boris := s.SignIn("boris@example.com")
	gleb := s.SignIn("gleb@example.com")
	tenant, _ := anna.NewSpace("Платформа")
	anna.Join(tenant, boris)
	org := create(t, anna, "Acme", tenant).Org.ID

	// Dark by default, in the list and in the organization's view.
	for _, p := range []*hubtest.Person{anna, boris} {
		if f, ok := featureState(t, p, org, features.Env); !ok || f.Enabled || f.Forced {
			t.Errorf("env = %+v", f)
		}
	}
	var v orgs.View
	anna.Do("GET", "/api/orgs/"+org, nil).JSON(&v)
	if v.Features == nil || len(v.Features) != 0 {
		t.Errorf("view features = %#v", v.Features)
	}
	// Somebody outside the organization does not see it exists.
	if r := gleb.Do("GET", "/api/orgs/"+org+"/features", nil); r.Code != http.StatusNotFound {
		t.Errorf("outsider: %s", r)
	}

	// Only an instance administrator switches a flag; the organization's own admin may not.
	if r := anna.Do("PUT", "/api/orgs/"+org+"/features/"+features.Env, map[string]any{"enabled": true}); r.Code != http.StatusForbidden {
		t.Errorf("org admin: %s", r)
	}
	if r := boris.Do("PUT", "/api/orgs/"+org+"/features/"+features.Env, map[string]any{"enabled": true}); r.Code != http.StatusForbidden {
		t.Errorf("member: %s", r)
	}
	if r := root.Do("PUT", "/api/orgs/"+org+"/features/nope", map[string]any{"enabled": true}); r.Code != http.StatusNotFound {
		t.Errorf("unknown flag: %s", r)
	}
	if r := root.Do("PUT", "/api/orgs/"+org+"/features/"+features.Env, map[string]any{}); r.Code != http.StatusBadRequest {
		t.Errorf("no enabled: %s", r)
	}
	r := root.Do("PUT", "/api/orgs/"+org+"/features/"+features.Env, map[string]any{"enabled": true})
	if r.Code != http.StatusOK {
		t.Fatalf("switch on: %s", r)
	}
	var put struct{ Feature orgs.FeatureView }
	r.JSON(&put)
	if put.Feature != (orgs.FeatureView{Name: "env", Enabled: true}) {
		t.Errorf("put = %+v", put)
	}

	// The change is seen at once by this process, in the list and in the view.
	if f, _ := featureState(t, boris, org, features.Env); !f.Enabled {
		t.Errorf("env after = %+v", f)
	}
	anna.Do("GET", "/api/orgs/"+org, nil).JSON(&v)
	if !slices.Equal(v.Features, []string{"env"}) {
		t.Errorf("view features = %v", v.Features)
	}
	if !s.Hub.Features.On(t.Context(), org, features.Env) || s.Hub.Features.On(t.Context(), org, features.Catalog) {
		t.Error("the gate disagrees with the list")
	}

	// It is in the journal, with who and what changed; switching to the same state adds nothing.
	root.Do("PUT", "/api/orgs/"+org+"/features/"+features.Env, map[string]any{"enabled": true})
	ev, err := s.DB.S().Audit(t.Context(), store.AuditFilter{OrgID: org, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ev {
		if e.Action == "org.feature" {
			n++
			if e.TargetID != "env" || e.After != `{"enabled":true}` {
				t.Errorf("journal = %+v", e)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d journal entries", n)
	}
	root.Do("PUT", "/api/orgs/"+org+"/features/"+features.Env, map[string]any{"enabled": false})
	if f, _ := featureState(t, anna, org, features.Env); f.Enabled {
		t.Errorf("env switched off = %+v", f)
	}
}

func TestInstanceListsDecideBeforeAndAfterTheOrganization(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"),
		hubtest.Env("HUB_FEATURES_FORCE", "catalog,-org.events"), hubtest.Env("HUB_FEATURES_DEFAULT", "ui.hx, env"))
	root := s.SignIn("root@example.com")
	tenant, _ := root.NewSpace("Платформа")
	org := create(t, root, "Acme", tenant).Org.ID

	want := map[string]orgs.FeatureView{
		"ui.hx":             {Name: "ui.hx", Enabled: true},                 // defaulted on
		"env":               {Name: "env", Enabled: true},                   // defaulted on
		"catalog":           {Name: "catalog", Enabled: true, Forced: true}, // forced on
		"machine.resources": {Name: "machine.resources"},                    // nothing says
		"org.events":        {Name: "org.events", Forced: true},             // forced off
	}
	for name, w := range want {
		if f, _ := featureState(t, root, org, name); f != w {
			t.Errorf("%s = %+v, want %+v", name, f, w)
		}
	}
	// An organization overrides a default, never a forced flag.
	root.Do("PUT", "/api/orgs/"+org+"/features/env", map[string]any{"enabled": false})
	root.Do("PUT", "/api/orgs/"+org+"/features/org.events", map[string]any{"enabled": true})
	root.Do("PUT", "/api/orgs/"+org+"/features/catalog", map[string]any{"enabled": false})
	if f, _ := featureState(t, root, org, "env"); f.Enabled {
		t.Errorf("env = %+v", f)
	}
	if f, _ := featureState(t, root, org, "org.events"); f.Enabled || !f.Forced {
		t.Errorf("org.events = %+v", f)
	}
	if f, _ := featureState(t, root, org, "catalog"); !f.Enabled || !f.Forced {
		t.Errorf("catalog = %+v", f)
	}
	// /api/config tells what the instance itself switches on.
	var cfg struct{ Features []string }
	s.Browser().Do("GET", "/api/config", nil).JSON(&cfg)
	if !slices.Equal(cfg.Features, []string{"ui.hx", "env", "catalog"}) {
		t.Errorf("config features = %v", cfg.Features)
	}
}

// A second replica's change is seen once the cache of five seconds is over.
func TestFeaturesCacheExpires(t *testing.T) {
	s := hubtest.New(t)
	ctx := t.Context()
	g := features.New(s.DB, s.Cfg.Features, s.Clock.Now)
	if g.On(ctx, "o1", features.Env) {
		t.Fatal("on by itself")
	}
	if err := s.DB.S().SetOrgFeature(ctx, "o1", features.Env, true, "", s.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if g.On(ctx, "o1", features.Env) {
		t.Error("the cache was not used")
	}
	s.Clock.Advance(features.CacheTTL + time.Second)
	if !g.On(ctx, "o1", features.Env) {
		t.Error("the cache did not expire")
	}
	if err := g.Set(ctx, "o1", features.Env, false, ""); err != nil || g.On(ctx, "o1", features.Env) {
		t.Errorf("set drops the cache: %v", err)
	}
}

func TestFeaturesAndViewMatchWebFixtures(t *testing.T) {
	a := newAcme(t)
	read := func(name string) map[string]any {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "app", "hub-api", "fixtures", "orgs", name))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	var got map[string]any
	a.anna.Do("GET", a.url("/features"), nil).JSON(&got)
	want := read("features.json")
	if keys(got) != keys(want) {
		t.Errorf("features keys %s, fixture %s", keys(got), keys(want))
	}
	gl, _ := got["features"].([]any)
	wl, _ := want["features"].([]any)
	if len(gl) == 0 || len(wl) == 0 {
		t.Fatal("empty features")
	}
	if keys(gl[0].(map[string]any)) != keys(wl[0].(map[string]any)) {
		t.Errorf("feature keys %s, fixture %s", keys(gl[0].(map[string]any)), keys(wl[0].(map[string]any)))
	}
	if len(gl) != len(wl) {
		t.Errorf("the hub knows %d flags, the fixture shows %d", len(gl), len(wl))
	}
	got = nil
	a.anna.Do("GET", a.url(""), nil).JSON(&got)
	if keys(got) != keys(read("view-admin.json")) {
		t.Errorf("view keys %s, fixture %s", keys(got), keys(read("view-admin.json")))
	}
}
