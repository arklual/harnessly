package env_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestCleanSourceOfSetupKinds(t *testing.T) {
	long := func(n int) string { return string(make([]byte, 0)) + stringsOf('x', n) }
	good := []struct {
		kind string
		src  any
	}{
		{"script", map[string]any{"from": "text", "body": "echo hi"}},
		{"script", map[string]any{"from": "file", "body": "echo hi", "fileName": "setup.sh", "rerun": "check", "check": "test -d ~/x", "secrets": []string{"npm-token"}}},
		{"script", map[string]any{"from": "repo", "repo": "item-1", "path": "scripts/setup.sh", "run": "bash scripts/setup.sh"}},
		{"script", map[string]any{"from": "repo", "repoUrl": "ssh://git@git.example.org/a/b.git", "branch": "main", "path": "x.sh"}},
		{"script", map[string]any{"from": "url", "url": "https://example.org/setup.sh"}},
		{"access", map[string]any{"service": "gitlab", "host": "git.example.org:8443", "secret": "gitlab-token"}},
		{"access", map[string]any{"service": "github"}},
		{"repo", map[string]any{"url": "https://git.example.org/a/b.git", "branch": "main", "dir": "~/code/b"}},
		{"repo", map[string]any{"url": "https://git.example.org/a/b.git", "dir": "code/b"}},
	}
	for _, c := range good {
		if _, err := env.CleanSource(c.kind, raw(c.src)); err != nil {
			t.Errorf("%s %v: %v", c.kind, c.src, err)
		}
	}
	bad := []struct {
		name  string
		kind  string
		src   any
		field string
	}{
		{"no from", "script", map[string]any{"body": "echo"}, "source.from"},
		{"bad from", "script", map[string]any{"from": "ftp"}, "source.from"},
		{"text without body", "script", map[string]any{"from": "text"}, "source.body"},
		{"file without name", "script", map[string]any{"from": "file", "body": "echo"}, "source.fileName"},
		{"repo without repo", "script", map[string]any{"from": "repo", "path": "a.sh"}, "source.repo"},
		{"repo without path", "script", map[string]any{"from": "repo", "repo": "i"}, "source.path"},
		{"path outside", "script", map[string]any{"from": "repo", "repo": "i", "path": "../a.sh"}, "source.path"},
		{"url without url", "script", map[string]any{"from": "url"}, "source.url"},
		{"url with login", "script", map[string]any{"from": "url", "url": "https://u:p@example.org/a.sh"}, "source.url"},
		{"url scheme", "script", map[string]any{"from": "url", "url": "ftp://example.org/a.sh"}, "source.url"},
		{"url http", "script", map[string]any{"from": "url", "url": "http://example.org/a.sh"}, "source.url"},
		{"repoUrl http", "script", map[string]any{"from": "repo", "repoUrl": "http://example.org/a.git", "path": "a"}, "source.repoUrl"},
		{"ssh with password", "script", map[string]any{"from": "repo", "repoUrl": "ssh://git:pw@git.example.org/a.git", "path": "a"}, "source.repoUrl"},
		{"rerun", "script", map[string]any{"from": "text", "body": "x", "rerun": "always"}, "source.rerun"},
		{"check missing", "script", map[string]any{"from": "text", "body": "x", "rerun": "check"}, "source.check"},
		{"secret name", "script", map[string]any{"from": "text", "body": "x", "secrets": []string{"a b"}}, "source.secrets"},
		{"too many secrets", "script", map[string]any{"from": "text", "body": "x", "secrets": []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q"}}, "source.secrets"},
		{"body too long", "script", map[string]any{"from": "text", "body": long(48<<10 + 1)}, "source.body"},
		{"run too long", "script", map[string]any{"from": "text", "body": "x", "run": long(4<<10 + 1)}, "source.run"},
		{"check too long", "script", map[string]any{"from": "text", "body": "x", "check": long(4<<10 + 1)}, "source.check"},
		{"fileName too long", "script", map[string]any{"from": "file", "body": "x", "fileName": long(513)}, "source.fileName"},
		{"brew in body", "script", map[string]any{"from": "text", "body": "set -e\nbrew install jq"}, "source.body"},
		{"brew after pipe", "script", map[string]any{"from": "text", "body": "true && brew update"}, "source.body"},
		{"brew in run", "script", map[string]any{"from": "repo", "repo": "i", "path": "a", "run": "brew bundle"}, "source.run"},
		{"foreign key", "script", map[string]any{"from": "text", "body": "x", "token": "t"}, "source.token"},
		{"access without service", "access", map[string]any{}, "source.service"},
		{"access service", "access", map[string]any{"service": "bitbucket"}, "source.service"},
		{"access host url", "access", map[string]any{"service": "gitlab", "host": "https://git.example.org"}, "source.host"},
		{"access host path", "access", map[string]any{"service": "gitlab", "host": "git.example.org/x"}, "source.host"},
		{"access secret", "access", map[string]any{"service": "github", "secret": "a b"}, "source.secret"},
		{"dir absolute", "repo", map[string]any{"url": "https://git.example.org/a.git", "dir": "/opt/x"}, "source.dir"},
		{"dir dotdot", "repo", map[string]any{"url": "https://git.example.org/a.git", "dir": "~/a/../../b"}, "source.dir"},
		{"dir other home", "repo", map[string]any{"url": "https://git.example.org/a.git", "dir": "~root/x"}, "source.dir"},
		{"dir long", "repo", map[string]any{"url": "https://git.example.org/a.git", "dir": long(513)}, "source.dir"},
	}
	for _, c := range bad {
		_, err := env.CleanSource(c.kind, raw(c.src))
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if got := fieldOf(err); got != c.field {
			t.Errorf("%s: field %q, want %q (%v)", c.name, got, c.field, err)
		}
	}
}

func stringsOf(r rune, n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = r
	}
	return string(b)
}

func fieldOf(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		f, _ := he.Details["field"].(string)
		return f
	}
	return ""
}

func TestSetupKindsInTheCatalogAndWhoOwnsThem(t *testing.T) {
	a := newAcme(t)
	script := map[string]any{"from": "text", "body": "echo hi"}
	access := map[string]any{"service": "gitlab", "host": "git.example.org"}
	scriptID := a.item(t, "script", "Установка", script)
	a.item(t, "access", "GitLab", access)

	add := func(p *hubtest.Person, name string, owner any) hubtest.Resp {
		body := map[string]any{"kind": "script", "name": name, "source": script}
		if owner != nil {
			body["owner"] = owner
		}
		return p.Do("POST", a.url("/catalog"), body)
	}
	team := func(id string) map[string]string { return map[string]string{"layer": "team", "scopeId": id} }
	// A lead makes items for their team; nothing for the organization, the tenant or another team.
	r := add(a.boris, "Скрипт Ядра", team(a.core))
	if r.Code != 201 {
		t.Fatalf("lead owner team: %s", r)
	}
	var made struct{ Item env.CatalogItemView }
	r.JSON(&made)
	if made.Item.Owner == nil || made.Item.Owner.Layer != "team" || made.Item.Owner.ScopeID != a.core {
		t.Errorf("owner = %+v", made.Item.Owner)
	}
	for name, resp := range map[string]hubtest.Resp{
		"lead, org":         add(a.boris, "Org", nil),
		"lead, org own":     add(a.boris, "Org2", map[string]string{"layer": "org", "scopeId": a.org}),
		"lead, tenant":      add(a.boris, "Tenant", map[string]string{"layer": "tenant", "scopeId": a.tenant}),
		"lead, side":        add(a.boris, "Side", team(a.side)),
		"member, team":      add(a.vera, "V", team(a.core)),
		"tenant admin, org": add(a.dina, "D", nil),
	} {
		if resp.Code != 403 {
			t.Errorf("%s: %s", name, resp)
		}
	}
	if r := add(a.dina, "Скрипт тенанта", map[string]string{"layer": "tenant", "scopeId": a.tenant}); r.Code != 201 {
		t.Errorf("tenant admin owner tenant: %s", r)
	}
	if r := add(a.dina, "Скрипт в команде", team(a.core)); r.Code != 201 { // a tenant admin manages the tenant's teams
		t.Errorf("tenant admin owner team: %s", r)
	}
	if r := add(a.anna, "Анна в команде", team(a.side)); r.Code != 201 {
		t.Errorf("org admin owner team: %s", r)
	}
	// Changing: the owner layer's managers and organization admins.
	id := made.Item.ID
	if r := a.boris.Do("PATCH", a.url("/catalog/"+id), map[string]any{"hint": "новое"}); r.Code != 200 {
		t.Errorf("lead patches own: %s", r)
	}
	if r := a.boris.Do("PATCH", a.url("/catalog/"+scriptID), map[string]any{"hint": "x"}); r.Code != 403 {
		t.Errorf("lead patches org item: %s", r)
	}
	if r := a.boris.Do("DELETE", a.url("/catalog/"+scriptID), nil); r.Code != 403 {
		t.Errorf("lead deletes org item: %s", r)
	}
	if r := a.vera.Do("PATCH", a.url("/catalog/"+id), map[string]any{"hint": "x"}); r.Code != 403 {
		t.Errorf("member patches: %s", r)
	}
	if r := a.anna.Do("PATCH", a.url("/catalog/"+id), map[string]any{"hint": "анна"}); r.Code != 200 {
		t.Errorf("org admin patches team item: %s", r)
	}
	if r := a.boris.Do("PATCH", a.url("/catalog/"+id), map[string]any{"source": map[string]any{"from": "text", "body": "brew install x"}}); r.Code != 400 || r.Detail("field") != "source.body" {
		t.Errorf("brew: %s", r)
	}
	if r := a.boris.Do("DELETE", a.url("/catalog/"+id), nil); r.Code != 204 {
		t.Errorf("lead archives own: %s", r)
	}
	// A script that takes its file from a repository item needs the item.
	if r := a.anna.Do("POST", a.url("/catalog"), map[string]any{"kind": "script", "name": "Из репо", "source": map[string]any{"from": "repo", "repo": "no-such", "path": "a.sh"}}); r.Code != 400 {
		t.Errorf("unknown repo: %s", r)
	}
	repo := a.item(t, "repo", "Монорепо", map[string]any{"url": "https://git.example.org/a/mono.git", "dir": "~/code/mono"})
	if r := a.anna.Do("POST", a.url("/catalog"), map[string]any{"kind": "script", "name": "Из репо", "source": map[string]any{"from": "repo", "repo": repo, "path": "a.sh"}}); r.Code != 201 {
		t.Errorf("known repo: %s", r)
	}
	// The kinds are counted on the screen.
	a.entry(t, a.anna, "org", a.org, scriptID, nil)
	if all := a.layers(t, a.anna, ""); all.Summary.ByKind.Script != 1 || layer(all, "org", a.org).Counts.Script != 1 {
		t.Errorf("counts = %+v", all.Summary.ByKind)
	}
}

// sendTo runs a pusher for one machine with a fake link and the features the machine's hello says.
func sendTo(t *testing.T, a acme, features ...string) (hubproto.EnvSet, bool) {
	t.Helper()
	m := a.anna.Machine(a.tenant, nil)
	link := &fakeLink{id: m.MachineID, status: "ok"}
	p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour})
	defer p.Close()
	p.Connected(m.MachineID, hubproto.Hello{Features: append([]string{hubproto.FeatureEnvironment}, features...)})
	if err := p.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if link.count() == 0 {
		return hubproto.EnvSet{}, false
	}
	return link.sent[0], true
}

func TestOrderOfScriptsAndWhatOldAgentsGet(t *testing.T) {
	a := newAcme(t)
	mk := func(name string) string {
		return a.item(t, "script", name, map[string]any{"from": "text", "body": "echo " + name})
	}
	s1, s2, s3, st := mk("Один"), mk("Два"), mk("Три"), mk("Тенантный")
	skill := a.item(t, "skill", "Ревью", map[string]any{"body": "x"})
	gl := a.item(t, "access", "GitLab", map[string]any{"service": "gitlab"})
	var rows []env.EntryItem
	for _, id := range []string{s1, s2, s3} {
		_, row := a.entry(t, a.anna, "org", a.org, id, nil)
		rows = append(rows, row)
	}
	_, tenantRow := a.entry(t, a.dina, "tenant", a.tenant, st, nil)
	a.entry(t, a.anna, "org", a.org, skill, nil)
	a.entry(t, a.anna, "org", a.org, gl, nil)

	orderOf := func(set hubproto.EnvSet) map[string]int {
		out := map[string]int{}
		for _, it := range set.Items {
			if it.Order != 0 {
				out[it.ItemID] = it.Order
			}
		}
		return out
	}
	set, ok := sendTo(t, a, hubproto.FeatureEnvSetup)
	if !ok {
		t.Fatal("nothing sent")
	}
	if got := orderOf(set); got[s1] != 1 || got[s2] != 2 || got[s3] != 3 || got[st] != 4 || len(got) != 4 {
		t.Errorf("order = %v", got)
	}
	if len(set.Items) != 6 || set.Hash != hubproto.EnvHash(set.Items) {
		t.Errorf("set = %d items, hash ok %v", len(set.Items), set.Hash == hubproto.EnvHash(set.Items))
	}

	// An agent without the feature gets neither the scripts nor the accesses, nor any order; its hash is the filtered list's.
	old, ok := sendTo(t, a)
	if !ok {
		t.Fatal("nothing sent to the old agent")
	}
	if len(old.Items) != 1 || old.Items[0].ItemID != skill {
		t.Errorf("old agent items = %+v", old.Items)
	}
	for _, it := range old.Items {
		if it.Order != 0 || it.Kind == "script" || it.Kind == "access" {
			t.Errorf("old agent got %+v", it)
		}
	}
	if old.Hash != hubproto.EnvHash(old.Items) || old.Hash == set.Hash {
		t.Errorf("old hash %s", old.Hash)
	}

	// The order: all the script entries of the layer, each once; by the layer's managers.
	ids := []string{rows[2].EntryID, rows[0].EntryID, rows[1].EntryID}
	put := func(p *hubtest.Person, layer, scope string, entries []string) hubtest.Resp {
		return p.Do("PUT", a.url("/env/order"), map[string]any{"layer": layer, "scopeId": scope, "entryIds": entries})
	}
	if r := put(a.anna, "org", a.org, ids[:2]); r.Code != 400 {
		t.Errorf("a script missing: %s", r)
	}
	if r := put(a.anna, "org", a.org, []string{ids[0], ids[0], ids[1]}); r.Code != 400 {
		t.Errorf("a script twice: %s", r)
	}
	if r := put(a.anna, "org", a.org, []string{ids[0], ids[1], tenantRow.EntryID}); r.Code != 400 {
		t.Errorf("another layer's entry: %s", r)
	}
	if r := put(a.dina, "org", a.org, ids); r.Code != 403 {
		t.Errorf("tenant admin orders the org: %s", r)
	}
	r := put(a.anna, "org", a.org, ids)
	var res struct{ EntryIDs []string }
	r.JSON(&res)
	if r.Code != 200 || !slices.Equal(res.EntryIDs, ids) {
		t.Fatalf("order: %s", r)
	}
	set2, _ := sendTo(t, a, hubproto.FeatureEnvSetup)
	if got := orderOf(set2); got[s3] != 1 || got[s1] != 2 || got[s2] != 3 || got[st] != 4 {
		t.Errorf("order after = %v", got)
	}
	if set2.Hash == set.Hash {
		t.Error("a changed order is a changed list")
	}
	if r := put(a.dina, "tenant", a.tenant, []string{tenantRow.EntryID}); r.Code != 200 {
		t.Errorf("tenant admin orders the tenant: %s", r)
	}
}

// setupAgent is a machine of the person with the setup feature that answers env.set with what it was given (every
// item with `status`) and env.run with an acceptance.
func setupAgent(t *testing.T, a acme, who *hubtest.Person, space string, features []string, status *string) *hubtest.Agent {
	t.Helper()
	ag := a.s.NewAgent(nil)
	ag.Hello.Features = append(slices.Clone(hubproto.FeaturesV2), features...)
	ag.Handle(hubproto.OpEnvSet, func(c hubtest.Call) (any, error) {
		var set hubproto.EnvSet
		set.Hash = c.Str("hash")
		_ = json.Unmarshal(c.Params["rev"], &set.Rev)
		_ = json.Unmarshal(c.Params["items"], &set.Items)
		st := hubproto.EnvState{Rev: set.Rev, Hash: set.Hash}
		for _, it := range set.Items {
			st.Applied = append(st.Applied, hubproto.EnvApplied{ItemID: it.ItemID, Status: *status})
		}
		return st, nil
	})
	ag.Handle(hubproto.OpEnvRun, func(c hubtest.Call) (any, error) {
		return map[string]any{"runId": c.Str("runId"), "accepted": true}, nil
	})
	if r := ag.Register(who.NewCode(space, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 201 {
		t.Fatalf("register: %s", r)
	}
	ag.Connect()
	return ag
}

var withSetup = []string{hubproto.FeatureEnvironment, hubproto.FeatureEnvSetup}

func (a acme) runFrame(ag *hubtest.Agent, id, mode, status string, log string) {
	ag.Emit(map[string]any{"event": "env.run", "run": hubproto.EnvRun{RunID: id, Mode: mode, Hash: "h1", Status: status,
		StartedAt: a.s.Clock.Now().UnixMilli(), FinishedAt: a.s.Clock.Now().UnixMilli() + 5000,
		Steps: []hubproto.EnvStep{{ItemID: "i1", Kind: "script", Name: "Установка", Status: status, Message: "boom", Ms: 120, Log: log}}}})
}

func TestRunsAreStoredListedAndNotStolen(t *testing.T) {
	a := newAcme(t)
	ok := "ok"
	annaBox := setupAgent(t, a, a.anna, a.tenant, withSetup, &ok)
	dinaBox := setupAgent(t, a, a.dina, a.tenant, withSetup, &ok)
	ctx := context.Background()

	a.runFrame(annaBox, "apply-run-0001", "apply", "running", "")
	a.runFrame(annaBox, "apply-run-0001", "apply", "error", "line 1\nerror: no space left")
	a.s.Eventually("the run is stored", func() bool {
		r, err := a.s.DB.S().EnvRunByID(ctx, a.org, "apply-run-0001")
		return err == nil && r.Status == "error"
	})
	row, _ := a.s.DB.S().EnvRunByID(ctx, a.org, "apply-run-0001")
	if row.MachineID != annaBox.MachineID || row.UserID != a.anna.ID || row.SpaceID != a.tenant || row.Hash != "h1" || row.FinishedAt.IsZero() {
		t.Errorf("row = %+v", row)
	}

	// The list: for the layer's managers, without logs; errors=1 keeps the failed ones.
	var list struct{ Runs []env.EnvRunRow }
	if r := a.dina.Do("GET", a.url("/env/runs?layer=tenant&scopeId="+a.tenant), nil); r.Code != 200 {
		t.Fatalf("list: %s", r)
	} else {
		r.JSON(&list)
	}
	if len(list.Runs) != 1 || list.Runs[0].ID != "apply-run-0001" || list.Runs[0].Status != "error" || list.Runs[0].Machine.ID != annaBox.MachineID ||
		list.Runs[0].Person.Name == "" || list.Runs[0].Steps[0].Log != "" || list.Runs[0].Steps[0].Message == nil {
		t.Errorf("list = %+v", list.Runs)
	}
	if r := a.dina.Do("GET", a.url("/env/runs?layer=tenant&scopeId="+a.tenant+"&errors=1&limit=5"), nil); r.Code != 200 {
		t.Errorf("errors: %s", r)
	}
	for name, path := range map[string]string{
		"member":       "/env/runs?layer=tenant&scopeId=" + a.tenant,
		"lead, tenant": "/env/runs?layer=tenant&scopeId=" + a.tenant,
	} {
		who := a.vera
		if name == "lead, tenant" {
			who = a.boris
		}
		if r := who.Do("GET", a.url(path), nil); r.Code != 403 {
			t.Errorf("%s: %s", name, r)
		}
	}
	if r := a.boris.Do("GET", a.url("/env/runs?layer=team&scopeId="+a.core), nil); r.Code != 200 {
		t.Errorf("lead, own team: %s", r)
	}
	// One run, with the logs: the owner and the managers; not a member of the tenant.
	var one struct{ Run env.EnvRunRow }
	if r := a.anna.Do("GET", a.url("/env/runs/apply-run-0001"), nil); r.Code != 200 {
		t.Fatalf("run: %s", r)
	} else {
		r.JSON(&one)
	}
	if one.Run.Steps[0].Log != "line 1\nerror: no space left" {
		t.Errorf("log = %q", one.Run.Steps[0].Log)
	}
	if r := a.vera.Do("GET", a.url("/env/runs/apply-run-0001"), nil); r.Code != 403 {
		t.Errorf("member reads: %s", r)
	}
	if r := a.anna.Do("GET", a.url("/env/runs/nope-nope-nope"), nil); r.Code != 404 {
		t.Errorf("missing: %s", r)
	}

	// Another machine cannot take a run id: its frame changes nothing, and a run the hub never asked it for is not kept.
	a.runFrame(dinaBox, "apply-run-0001", "apply", "ok", "stolen")
	a.runFrame(dinaBox, "update-run-001", "update", "ok", "invented")
	a.runFrame(dinaBox, "apply-run-0002", "apply", "ok", "own")
	a.s.Eventually("the machine's own run", func() bool {
		_, err := a.s.DB.S().EnvRunByID(ctx, a.org, "apply-run-0002")
		return err == nil
	})
	kept, _ := a.s.DB.S().EnvRunByID(ctx, a.org, "apply-run-0001")
	if kept.MachineID != annaBox.MachineID || kept.Status != "error" || kept.UserID != a.anna.ID {
		t.Errorf("stolen: %+v", kept)
	}
	if _, err := a.s.DB.S().EnvRunByID(ctx, a.org, "update-run-001"); !store.IsNotFound(err) {
		t.Errorf("an update run nobody asked for was kept: %v", err)
	}
	// And a run of this organization is not found under another one.
	if _, err := a.s.DB.S().EnvRunByID(ctx, "other-org", "apply-run-0001"); !store.IsNotFound(err) {
		t.Errorf("foreign org: %v", err)
	}
}

func TestTryRunsTheLayerWithTheDraftOnTheAskersMachine(t *testing.T) {
	a := newAcme(t)
	ok := "ok"
	exist := a.item(t, "script", "Существующий", map[string]any{"from": "text", "body": "echo old"})
	_, row := a.entry(t, a.anna, "org", a.org, exist, nil)
	older := a.item(t, "script", "Тенантный", map[string]any{"from": "text", "body": "echo tenant"})
	a.entry(t, a.dina, "tenant", a.tenant, older, nil)

	try := func(p *hubtest.Person, body map[string]any) hubtest.Resp {
		return p.Do("POST", a.url("/env/try"), body)
	}
	// No machine yet.
	if r := try(a.anna, map[string]any{"layer": "org", "scopeId": a.org}); r.Code != 409 || r.Detail("reason") != "no_machine" {
		t.Errorf("no machine: %s", r)
	}
	// A machine without the feature does not count.
	setupAgent(t, a, a.anna, a.tenant, []string{hubproto.FeatureEnvironment}, &ok)
	if r := try(a.anna, map[string]any{"layer": "org", "scopeId": a.org}); r.Code != 409 {
		t.Errorf("old machine: %s", r)
	}
	box := setupAgent(t, a, a.anna, a.tenant, withSetup, &ok)
	if r := try(a.vera, map[string]any{"layer": "org", "scopeId": a.org}); r.Code != 403 {
		t.Errorf("member: %s", r)
	}
	if r := try(a.dina, map[string]any{"layer": "tenant", "scopeId": a.tenant}); r.Code != 409 {
		t.Errorf("dina has no machine: %s", r)
	}
	if r := try(a.anna, map[string]any{"layer": "org", "scopeId": a.org, "machineId": "someone-elses"}); r.Code != 409 {
		t.Errorf("unknown machine: %s", r)
	}
	if r := try(a.anna, map[string]any{"layer": "org", "scopeId": a.org, "draft": map[string]any{"kind": "script", "name": "Плохой", "source": map[string]any{"from": "text", "body": "brew install x"}}}); r.Code != 400 {
		t.Errorf("brew draft: %s", r)
	}

	// A new draft script goes last, after the org's and the tenant's.
	r := try(a.anna, map[string]any{"layer": "tenant", "scopeId": a.tenant, "machineId": box.MachineID,
		"draft": map[string]any{"kind": "script", "name": "Проба", "source": map[string]any{"from": "text", "body": "echo draft"}}})
	if r.Code != 202 {
		t.Fatalf("try: %s", r)
	}
	var res struct {
		RunID   string
		Machine env.NameRef
	}
	r.JSON(&res)
	if !hubproto.EnvRunIDRe.MatchString(res.RunID) || len(res.RunID) < 20 || res.Machine.ID != box.MachineID {
		t.Errorf("answer = %+v", res)
	}
	calls := box.Calls(hubproto.OpEnvRun)
	if len(calls) != 1 || calls[0].Str("runId") != res.RunID || calls[0].Str("mode") != "sandbox" {
		t.Fatalf("calls = %+v", calls)
	}
	var items []hubproto.EnvItem
	_ = json.Unmarshal(calls[0].Params["items"], &items)
	order := map[string]int{}
	for _, it := range items {
		order[it.Name] = it.Order
	}
	if len(items) != 3 || order["Существующий"] != 1 || order["Тенантный"] != 2 || order["Проба"] != 3 {
		t.Errorf("items = %+v", items)
	}
	// The row is there at once, running; the machine's frames replace it.
	got, err := a.s.DB.S().EnvRunByID(context.Background(), a.org, res.RunID)
	if err != nil || got.Status != "running" || got.Mode != "sandbox" || got.UserID != a.anna.ID {
		t.Fatalf("placeholder = %+v, %v", got, err)
	}
	a.runFrame(box, res.RunID, "sandbox", "ok", "done")
	a.s.Eventually("the sandbox run is replaced", func() bool {
		r, _ := a.s.DB.S().EnvRunByID(context.Background(), a.org, res.RunID)
		return r.Status == "ok"
	})

	// Putting the draft in place of an entry: the list has the draft's source, not the old one.
	r = try(a.anna, map[string]any{"layer": "org", "scopeId": a.org,
		"draft": map[string]any{"entryId": row.EntryID, "kind": "script", "name": "Существующий", "source": map[string]any{"from": "text", "body": "echo new"}}})
	if r.Code != 202 {
		t.Fatalf("replace: %s", r)
	}
	calls = box.Calls(hubproto.OpEnvRun)
	items = nil
	_ = json.Unmarshal(calls[len(calls)-1].Params["items"], &items)
	if len(items) != 1 || items[0].Name != "Существующий" || string(items[0].Source) != `{"body":"echo new","from":"text"}` {
		t.Errorf("org list with a replaced entry = %+v", items)
	}
	if r := try(a.anna, map[string]any{"layer": "org", "scopeId": a.org, "draft": map[string]any{"entryId": "nope", "kind": "script", "name": "X", "source": map[string]any{"from": "text", "body": "x"}}}); r.Code != 400 {
		t.Errorf("unknown entry: %s", r)
	}
}

func TestMeMachinesOutdatedAndRun(t *testing.T) {
	a := newAcme(t)
	status := "ok"
	box := setupAgent(t, a, a.anna, a.tenant, withSetup, &status)
	me := func(p *hubtest.Person, path string) hubtest.Resp {
		return p.Do("GET", "/api/me/env"+path+"?space="+a.tenant, nil)
	}
	type machines struct{ Machines []env.MeMachine }
	read := func() []env.MeMachine {
		var out machines
		me(a.anna, "/machines").JSON(&out)
		return out.Machines
	}

	if ms := read(); len(ms) != 1 || !ms[0].Online || !ms[0].Setup || !ms[0].Current || ms[0].Failed != 0 || ms[0].LastRun != nil || ms[0].ID != box.MachineID {
		t.Fatalf("empty environment: %+v", ms)
	}
	var env0 env.MeResponse
	me(a.anna, "").JSON(&env0)
	if env0.Outdated || len(env0.Machines) != 1 {
		t.Errorf("me = outdated %v, machines %d", env0.Outdated, len(env0.Machines))
	}

	// Something is put into the environment: the pusher sends it, the machine says it holds it.
	script := a.item(t, "script", "Установка", map[string]any{"from": "text", "body": "echo hi"})
	a.entry(t, a.anna, "org", a.org, script, nil)
	a.s.Eventually("the machine holds the list", func() bool { ms := read(); return len(ms) == 1 && ms[0].Current })
	var cur env.MeResponse
	me(a.anna, "").JSON(&cur)
	if cur.Outdated {
		t.Errorf("current but outdated: %+v", cur.Machines)
	}

	// A new item: until the machine reports it, the list is outdated; a failed setup item shows as failed.
	status = "error"
	other := a.item(t, "script", "Вторая", map[string]any{"from": "text", "body": "echo 2"})
	a.entry(t, a.anna, "org", a.org, other, nil)
	a.s.Eventually("failed items", func() bool { ms := read(); return len(ms) == 1 && ms[0].Current && ms[0].Failed == 2 })
	me(a.anna, "").JSON(&cur)
	if !cur.Outdated {
		t.Error("a failed item does not make the environment outdated")
	}
	// Other people's machines are not in the list.
	var theirs machines
	a.carol.Do("GET", "/api/me/env/machines?space="+a.tenant, nil).JSON(&theirs)
	if len(theirs.Machines) != 0 {
		t.Errorf("carol sees %+v", theirs.Machines)
	}

	// Asking for a run: update on every machine of the person, check on the one named.
	run := func(p *hubtest.Person, body map[string]any) hubtest.Resp {
		body["space"] = a.tenant
		return p.Do("POST", "/api/me/env/run?space="+a.tenant, body)
	}
	if r := run(a.carol, map[string]any{"mode": "update"}); r.Code != 409 || r.Detail("reason") != "no_machine" {
		t.Errorf("carol: %s", r)
	}
	if r := run(a.anna, map[string]any{"mode": "sandbox"}); r.Code != 400 {
		t.Errorf("mode: %s", r)
	}
	if r := run(a.anna, map[string]any{"mode": "check", "machineId": "nope"}); r.Code != 409 {
		t.Errorf("unknown machine: %s", r)
	}
	r := run(a.anna, map[string]any{"mode": "update"})
	var started struct {
		Runs []struct{ RunID, MachineID string }
	}
	r.JSON(&started)
	if r.Code != 202 || len(started.Runs) != 1 || started.Runs[0].MachineID != box.MachineID {
		t.Fatalf("run: %s", r)
	}
	calls := box.Calls(hubproto.OpEnvRun)
	if len(calls) != 1 || calls[0].Str("mode") != "update" || calls[0].Str("runId") != started.Runs[0].RunID || calls[0].Params["items"] != nil {
		t.Errorf("calls = %+v", calls)
	}
	if r := run(a.anna, map[string]any{"mode": "check", "machineId": box.MachineID}); r.Code != 202 {
		t.Errorf("check on one: %s", r)
	}
	// The machine's frames come back as the person's runs and as the machine's last run.
	a.runFrame(box, started.Runs[0].RunID, "update", "ok", "fine")
	var runs struct{ Runs []env.EnvRunRow }
	a.s.Eventually("the run is finished", func() bool {
		runs.Runs = nil
		a.anna.Do("GET", "/api/me/env/runs?space="+a.tenant, nil).JSON(&runs)
		return slices.ContainsFunc(runs.Runs, func(r env.EnvRunRow) bool { return r.ID == started.Runs[0].RunID && r.Status == "ok" })
	})
	if ms := read(); len(ms) != 1 || ms[0].LastRun == nil {
		t.Errorf("last run: %+v", ms)
	}
	if len(runs.Runs) != 2 {
		t.Errorf("runs = %+v", runs.Runs)
	}
	var carolRuns struct{ Runs []env.EnvRunRow }
	a.carol.Do("GET", "/api/me/env/runs?space="+a.tenant, nil).JSON(&carolRuns)
	if len(carolRuns.Runs) != 0 {
		t.Errorf("carol sees runs %+v", carolRuns.Runs)
	}
}

func TestOldRunsAreSwept(t *testing.T) {
	a := newAcme(t)
	ctx := context.Background()
	now := a.s.Clock.Now()
	st := a.s.DB.S()
	for id, age := range map[string]time.Duration{"run-fresh-001": 24 * time.Hour, "run-old-00001": (hubproto.EnvRunKeepDays*24 + 1) * time.Hour} {
		if _, err := st.UpsertEnvRun(ctx, store.EnvRun{ID: id, OrgID: a.org, MachineID: "m", Mode: "apply", Status: "ok", StartedAt: now.Add(-age), UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: &fakeLink{id: "m"}, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour})
	defer p.Close()
	p.Sweep(ctx)
	if _, err := st.EnvRunByID(ctx, a.org, "run-old-00001"); !store.IsNotFound(err) {
		t.Errorf("old run stays: %v", err)
	}
	if _, err := st.EnvRunByID(ctx, a.org, "run-fresh-001"); err != nil {
		t.Errorf("fresh run: %v", err)
	}
}

// An item a team or a tenant owns can be carried only by that layer's entries: otherwise its editor (a lead) would change
// what runs on machines of wider layers.
func TestOwnedItemsStayInTheirLayer(t *testing.T) {
	a := newAcme(t)
	src := map[string]any{"from": "text", "body": "echo hi"}
	mk := func(p *hubtest.Person, name string, owner map[string]string) string {
		r := p.Do("POST", a.url("/catalog"), map[string]any{"kind": "script", "name": name, "source": src, "owner": owner})
		if r.Code != 201 {
			t.Fatalf("%s: %s", name, r)
		}
		var out struct{ Item env.CatalogItemView }
		r.JSON(&out)
		return out.Item.ID
	}
	teamItem := mk(a.boris, "Ядро", map[string]string{"layer": "team", "scopeId": a.core})
	tenantItem := mk(a.dina, "Тенант", map[string]string{"layer": "tenant", "scopeId": a.tenant})
	orgItem := a.item(t, "script", "Общий", src)

	for name, c := range map[string]struct {
		who                *hubtest.Person
		layer, scope, item string
	}{
		"team item to org":      {a.anna, "org", a.org, teamItem},
		"team item to tenant":   {a.dina, "tenant", a.tenant, teamItem},
		"team item to a team":   {a.anna, "team", a.side, teamItem},
		"tenant item to org":    {a.anna, "org", a.org, tenantItem},
		"tenant item to a team": {a.boris, "team", a.core, tenantItem},
	} {
		r := c.who.Do("POST", a.url("/env/entries"), map[string]any{"layer": c.layer, "scopeId": c.scope, "itemId": c.item})
		if r.Code != 400 || r.Detail("field") != "itemId" {
			t.Errorf("%s: %s", name, r)
		}
	}
	if code, _ := a.entry(t, a.boris, "team", a.core, teamItem, nil); code != 201 {
		t.Errorf("team item in its own team: %d", code)
	}
	if code, _ := a.entry(t, a.dina, "tenant", a.tenant, tenantItem, nil); code != 201 {
		t.Errorf("tenant item in its own tenant: %d", code)
	}
	// An organization's item goes anywhere.
	if code, _ := a.entry(t, a.boris, "team", a.core, orgItem, nil); code != 201 {
		t.Errorf("org item in a team: %d", code)
	}
	// An organization admin still edits any item.
	if r := a.anna.Do("PATCH", a.url("/catalog/"+teamItem), map[string]any{"hint": "анна"}); r.Code != 200 {
		t.Errorf("org admin edits: %s", r)
	}
	// Defense in depth: even an entry that got in some other way (a row written by hand) is not in the machines' list.
	if _, _, err := a.s.DB.S().AddEnvEntry(context.Background(), store.EnvEntry{OrgID: a.org, Layer: "org", ScopeID: a.org, ItemID: teamItem, Enabled: true, UpdatedAt: a.s.Clock.Now()}); err != nil {
		t.Fatal(err)
	}
	set, ok := sendTo(t, a, hubproto.FeatureEnvSetup)
	if !ok {
		t.Fatal("nothing sent")
	}
	for _, it := range set.Items {
		if it.ItemID == teamItem {
			t.Errorf("a foreign-layer item is in the list: %+v", it)
		}
	}
}

func TestStuckRunsAreEnded(t *testing.T) {
	a := newAcme(t)
	ctx := context.Background()
	st := a.s.DB.S()
	now := a.s.Clock.Now()
	put := func(id string, quiet time.Duration, status string) {
		t.Helper()
		if _, err := st.UpsertEnvRun(ctx, store.EnvRun{ID: id, OrgID: a.org, MachineID: "m", Mode: "update", Status: status, StartedAt: now.Add(-quiet),
			Steps: `[{"itemId":"i","kind":"script","name":"Установка","status":"running","ms":0}]`, UpdatedAt: now.Add(-quiet)}); err != nil {
			t.Fatal(err)
		}
	}
	put("run-stuck-001", 31*time.Minute, "running")
	put("run-fresh-002", 10*time.Minute, "running")
	put("run-done-0003", 3*time.Hour, "ok")
	p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: &fakeLink{id: "m"}, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour})
	defer p.Close()
	p.Sweep(ctx)
	stuck, _ := st.EnvRunByID(ctx, a.org, "run-stuck-001")
	if stuck.Status != "error" || stuck.FinishedAt.IsZero() || !strings.Contains(stuck.Steps, "машина не ответила") {
		t.Errorf("stuck = %+v", stuck)
	}
	if r, _ := st.EnvRunByID(ctx, a.org, "run-fresh-002"); r.Status != "running" {
		t.Errorf("fresh = %+v", r)
	}
	if r, _ := st.EnvRunByID(ctx, a.org, "run-done-0003"); r.Status != "ok" {
		t.Errorf("done = %+v", r)
	}
}

// Secrets of the safe and accesses are an organization admin's to connect; a lead copies everything else for their team.
func TestSecretsAndAccessesAreTheOrgAdminsAlone(t *testing.T) {
	a := newAcme(t)
	team := map[string]string{"layer": "team", "scopeId": a.core}
	post := func(p *hubtest.Person, kind, name string, src any, owner any) hubtest.Resp {
		body := map[string]any{"kind": kind, "name": name, "source": src}
		if owner != nil {
			body["owner"] = owner
		}
		return p.Do("POST", a.url("/catalog"), body)
	}
	withSecrets := map[string]any{"from": "text", "body": "echo hi", "secrets": []string{"npm-token"}}
	if r := post(a.boris, "script", "С секретом", withSecrets, team); r.Code != 400 || r.Detail("field") != "source.secrets" {
		t.Errorf("lead script with secrets: %s", r)
	}
	if r := post(a.boris, "access", "GitLab", map[string]any{"service": "gitlab"}, team); r.Code != 400 || r.Detail("field") != "kind" {
		t.Errorf("lead access: %s", r)
	}
	if r := post(a.dina, "script", "С секретом", withSecrets, map[string]string{"layer": "tenant", "scopeId": a.tenant}); r.Code != 400 {
		t.Errorf("tenant admin script with secrets: %s", r)
	}
	// Everything else a lead can copy for the team.
	for kind, src := range map[string]any{
		"repo":   map[string]any{"url": "https://git.example.org/a/b.git"},
		"script": map[string]any{"from": "text", "body": "echo hi"},
		"skill":  map[string]any{"body": "Пиши коротко."},
		"mcp":    map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp", "secretRefs": []string{"TRACKER_TOKEN"}},
		"doc":    map[string]any{"url": "https://docs.example.org/a"},
	} {
		if r := post(a.boris, kind, "Копия "+kind, src, team); r.Code != 201 {
			t.Errorf("lead copies %s: %s", kind, r)
		}
	}
	// The org admin writes them in the organization's items; the owned ones stay free of them on every edit.
	orgScript := post(a.anna, "script", "Орг с секретом", withSecrets, nil)
	if orgScript.Code != 201 {
		t.Errorf("org admin secrets: %s", orgScript)
	}
	if r := post(a.anna, "access", "GitLab орг", map[string]any{"service": "gitlab"}, nil); r.Code != 201 {
		t.Errorf("org admin access: %s", r)
	}
	if r := post(a.anna, "script", "Анна, команда, секрет", withSecrets, team); r.Code != 400 {
		t.Errorf("owned item with secrets, even by an org admin: %s", r)
	}
	var mine struct{ Item env.CatalogItemView }
	post(a.boris, "script", "Своя", map[string]any{"from": "text", "body": "echo 1"}, team).JSON(&mine)
	for _, who := range []*hubtest.Person{a.boris, a.anna} {
		if r := who.Do("PATCH", a.url("/catalog/"+mine.Item.ID), map[string]any{"source": withSecrets}); r.Code != 400 || r.Detail("field") != "source.secrets" {
			t.Errorf("patch adds secrets: %s", r)
		}
	}
	// The drafts of a try: the lead's, with secrets, is refused before any machine is looked for.
	draft := map[string]any{"kind": "script", "name": "Проба", "source": withSecrets}
	if r := a.boris.Do("POST", a.url("/env/try"), map[string]any{"layer": "team", "scopeId": a.core, "draft": draft}); r.Code != 400 || r.Detail("field") != "source.secrets" {
		t.Errorf("lead draft: %s", r)
	}
	if r := a.boris.Do("POST", a.url("/env/try"), map[string]any{"layer": "team", "scopeId": a.core, "draft": map[string]any{"kind": "access", "name": "G", "source": map[string]any{"service": "github"}}}); r.Code != 400 {
		t.Errorf("lead access draft: %s", r)
	}
	// The org admin's own draft passes the check (and then finds no machine).
	if r := a.anna.Do("POST", a.url("/env/try"), map[string]any{"layer": "org", "scopeId": a.org, "draft": draft}); r.Code != 409 {
		t.Errorf("org admin draft: %s", r)
	}
	// Replacing the entry of an owned item with a draft that has secrets is refused even for the org admin.
	_, row := a.entry(t, a.boris, "team", a.core, mine.Item.ID, nil)
	if r := a.anna.Do("POST", a.url("/env/try"), map[string]any{"layer": "team", "scopeId": a.core, "draft": map[string]any{"entryId": row.EntryID, "kind": "script", "name": "Своя", "source": withSecrets}}); r.Code != 400 {
		t.Errorf("owned entry draft: %s", r)
	}
}

func TestMeMachineTeamFailedAndOutdated(t *testing.T) {
	a := newAcme(t)
	ctx := context.Background()
	status := "ok"
	box := setupAgent(t, a, a.anna, a.tenant, withSetup, &status)
	if _, err := a.s.DB.Exec(ctx, `UPDATE machines SET team_id = ? WHERE id = ?`, a.core, box.MachineID); err != nil {
		t.Fatal(err)
	}
	script := a.item(t, "script", "Установка", map[string]any{"from": "text", "body": "echo hi"})
	a.entry(t, a.anna, "org", a.org, script, nil)
	read := func() env.MeMachine {
		var out struct{ Machines []env.MeMachine }
		a.anna.Do("GET", "/api/me/env/machines?space="+a.tenant, nil).JSON(&out)
		if len(out.Machines) != 1 {
			t.Fatalf("machines = %+v", out.Machines)
		}
		return out.Machines[0]
	}
	a.s.Eventually("current", func() bool { return read().Current })
	if m := read(); m.Team == nil || m.Team.ID != a.core || m.Team.Name != "Ядро" {
		t.Errorf("team = %+v", m.Team)
	}
	// Only an error and a missing-token ask are failures; a script that waits for a broken one, or is still going, is not.
	hash := func() string { s, _ := a.s.DB.S().EnvStateByMachine(ctx, box.MachineID); return s.Hash }
	box.Emit(map[string]any{"event": "env.state", "state": hubproto.EnvState{Rev: 1, Hash: hash(), Applied: []hubproto.EnvApplied{
		{ItemID: script, Status: "pending", Message: "положите токен npm-token в сейф"},
		{ItemID: "x", Status: "pending", Message: "ждёт, пока починят Установка"},
		{ItemID: script, Status: "pending", Message: "машина ещё настраивается"},
		{ItemID: script, Status: "error", Message: "exit 1"},
	}}})
	a.s.Eventually("failed counted", func() bool { return read().Failed == 2 })

	// A machine that cannot do the setup is not behind on it, whatever it holds.
	old := a.s.NewAgent(nil)
	old.Hello.Features = append(append([]string{}, hubproto.FeaturesV2...), hubproto.FeatureEnvironment)
	if r := old.Register(a.anna.NewCode(a.tenant, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != 201 {
		t.Fatal(r)
	}
	old.Connect()
	skill := a.item(t, "skill", "Ревью", map[string]any{"body": "x"})
	a.entry(t, a.anna, "org", a.org, skill, nil)
	var all struct{ Machines []env.MeMachine }
	a.anna.Do("GET", "/api/me/env/machines?space="+a.tenant, nil).JSON(&all)
	for _, m := range all.Machines {
		if m.ID == old.MachineID && (m.Setup || m.Failed != 0) {
			t.Errorf("old machine = %+v", m)
		}
	}
	// With the failed machine fixed, only the old one is behind: outdated stays off.
	box.Emit(map[string]any{"event": "env.state", "state": hubproto.EnvState{Rev: 1, Hash: hash(), Applied: []hubproto.EnvApplied{{ItemID: script, Status: "ok"}}}})
	a.s.Eventually("not outdated", func() bool {
		var me env.MeResponse
		a.anna.Do("GET", "/api/me/env?space="+a.tenant, nil).JSON(&me)
		return !me.Outdated && len(me.Machines) == 2
	})
}

func TestDeliveryIsNullForBigScreens(t *testing.T) {
	a := newAcme(t)
	a.anna.Machine(a.tenant, nil)
	a.anna.Machine(a.tenant, nil)
	if d := a.layers(t, a.anna, "?scope="+a.tenant).Delivery; d == nil || d.Machines != 2 {
		t.Fatalf("delivery = %+v", d)
	}
	defer env.SetMaxDeliveryMachines(1)()
	rs := a.layers(t, a.anna, "?scope="+a.tenant)
	if rs.Delivery != nil {
		t.Errorf("delivery = %+v", rs.Delivery)
	}
	r := a.anna.Do("GET", a.url("/env?scope="+a.tenant), nil)
	var generic map[string]any
	r.JSON(&generic)
	if v, ok := generic["delivery"]; !ok || v != nil {
		t.Errorf("delivery field = %v, %v", v, ok)
	}
}

func TestMachineInventedRunsAreBounded(t *testing.T) {
	a := newAcme(t)
	ctx := context.Background()
	ok := "ok"
	box := setupAgent(t, a, a.anna, a.tenant, withSetup, &ok)
	for i := 0; i < 32; i++ {
		a.runFrame(box, fmt.Sprintf("apply-run-%04d", i), "apply", "ok", "")
	}
	// A big run: the logs of the oldest steps go first.
	big := make([]hubproto.EnvStep, 60)
	for i := range big {
		big[i] = hubproto.EnvStep{ItemID: fmt.Sprintf("i%d", i), Kind: "script", Name: "S", Status: "ok", Log: strings.Repeat("x", hubproto.EnvLogMax)}
	}
	box.Emit(map[string]any{"event": "env.run", "run": hubproto.EnvRun{RunID: "apply-big-0001", Mode: "apply", Status: "ok", StartedAt: a.s.Clock.Now().UnixMilli(), Steps: big}})
	a.s.Eventually("the bound holds", func() bool {
		rs, _ := a.s.DB.S().EnvRunsOf(ctx, store.EnvRunQuery{OrgID: a.org, Since: a.s.Clock.Now().Add(-time.Hour), Limit: 200})
		return len(rs) >= 30
	})
	time.Sleep(300 * time.Millisecond)
	rs, _ := a.s.DB.S().EnvRunsOf(ctx, store.EnvRunQuery{OrgID: a.org, Since: a.s.Clock.Now().Add(-time.Hour), Limit: 200})
	if len(rs) != 30 {
		t.Errorf("kept %d runs, want 30", len(rs))
	}
	if _, err := a.s.DB.S().EnvRunByID(ctx, a.org, "apply-big-0001"); !store.IsNotFound(err) {
		t.Errorf("a run over the hourly bound was kept: %v", err)
	}
	// Past the hour the bound lets runs through again, and the big one is trimmed.
	a.s.Clock.Advance(61 * time.Minute)
	box.Emit(map[string]any{"event": "env.run", "run": hubproto.EnvRun{RunID: "apply-big-0002", Mode: "apply", Status: "ok", StartedAt: a.s.Clock.Now().UnixMilli(), Steps: big}})
	a.s.Eventually("the big run", func() bool { _, err := a.s.DB.S().EnvRunByID(ctx, a.org, "apply-big-0002"); return err == nil })
	row, _ := a.s.DB.S().EnvRunByID(ctx, a.org, "apply-big-0002")
	var steps []hubproto.EnvStep
	_ = json.Unmarshal([]byte(row.Steps), &steps)
	if len(row.Steps) > 512<<10 || len(steps) != 60 || steps[0].Log != "" || steps[59].Log == "" {
		t.Errorf("trimmed: %d bytes, %d steps, first log %d, last log %d", len(row.Steps), len(steps), len(steps[0].Log), len(steps[59].Log))
	}
}
