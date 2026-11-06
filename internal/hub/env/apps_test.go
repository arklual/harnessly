package env_test

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestAppKindIsCheckedForAdminsOnlyAndGoesToMachinesThatDoIt(t *testing.T) {
	a := newAcme(t)
	post := func(src map[string]any, owner map[string]any) int {
		t.Helper()
		body := map[string]any{"kind": "app", "name": "Кит", "source": src}
		for k, v := range owner {
			body[k] = v
		}
		return a.anna.Do("POST", a.url("/catalog"), body).Code
	}
	for _, src := range []map[string]any{
		{"repoUrl": "https://git.example.org/team/kit.git"},                         // no folder
		{"repoUrl": "https://git.example.org/team/kit.git", "path": "../elsewhere"}, // outside the repository
		{"repoUrl": "https://git.example.org/team/kit.git", "path": "/abs"},         // absolute
		{"path": "apps/kit"}, // no repository
		{"from": "url", "repoUrl": "https://git.example.org/team/kit.git", "path": "x"}, // not from a repository
		{"repoUrl": "http://plain.example.org/kit.git", "path": "apps/kit"},             // not https
	} {
		if code := post(src, nil); code != 400 {
			t.Errorf("%v: %d", src, code)
		}
	}
	// A team's lead cannot bring an app: it works in every chat of the people it reaches.
	lead := a.boris.Do("POST", a.url("/catalog"), map[string]any{"kind": "app", "name": "Кит команды",
		"source": map[string]any{"repoUrl": "https://git.example.org/team/kit.git", "path": "apps/kit"}, "ownerLayer": "team", "ownerScope": a.core})
	if lead.Code != 400 && lead.Code != 403 {
		t.Errorf("a lead's app: %s", lead)
	}

	kit := a.item(t, "app", "Кит", map[string]any{"repoUrl": "https://git.example.org/team/kit.git", "branch": "main", "path": "apps/kit"})
	if code, _ := a.entry(t, a.anna, "org", a.org, kit, map[string]any{"pinned": true}); code != 201 {
		t.Fatalf("entry: %d", code)
	}
	send := func(features ...string) []hubproto.EnvItem {
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
			return nil
		}
		return link.sent[0].Items
	}
	find := func(items []hubproto.EnvItem) *hubproto.EnvItem {
		for i := range items {
			if items[i].Kind == hubproto.EnvKindApp {
				return &items[i]
			}
		}
		return nil
	}
	if it := find(send(hubproto.FeatureEnvSetup, hubproto.FeatureEnvClaude)); it != nil {
		t.Errorf("an agent without environment.apps got the app: %+v", it)
	}
	it := find(send(hubproto.FeatureEnvSetup, hubproto.FeatureEnvApps))
	if it == nil || !it.Pinned || it.Name != "Кит" {
		t.Fatalf("the app on a machine that does it: %+v", it)
	}
	if got := string(it.Source); got != `{"branch":"main","from":"repo","path":"apps/kit","repoUrl":"https://git.example.org/team/kit.git"}` {
		t.Errorf("source = %s", got)
	}
}
