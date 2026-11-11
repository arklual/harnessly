package env_test

import (
	"context"
	"strings"
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

// An app item carries the installation's addresses when it has any, and is sent as the catalog has it when it has none.
func TestAppItemsCarryTheInstallationsAddresses(t *testing.T) {
	a := newAcme(t)
	kit := a.item(t, "app", "Доски", map[string]any{"repoUrl": "https://git.example.org/team/boards.git", "path": "app"})
	if code, _ := a.entry(t, a.anna, "org", a.org, kit, nil); code != 201 {
		t.Fatalf("entry: %d", code)
	}
	send := func(install map[string]string) string {
		t.Helper()
		m := a.anna.Machine(a.tenant, nil)
		link := &fakeLink{id: m.MachineID, status: "ok"}
		p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour, Install: install})
		defer p.Close()
		p.Connected(m.MachineID, hubproto.Hello{Features: []string{hubproto.FeatureEnvironment, hubproto.FeatureEnvSetup, hubproto.FeatureEnvApps}})
		if err := p.Push(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, it := range link.sent[0].Items {
			if it.Kind == hubproto.EnvKindApp {
				return string(it.Source)
			}
		}
		t.Fatal("no app item")
		return ""
	}
	if got := send(env.InstallValues("", "")); got != `{"from":"repo","path":"app","repoUrl":"https://git.example.org/team/boards.git"}` {
		t.Errorf("without addresses: %s", got)
	}
	if got := send(env.InstallValues("https://boards.example", "")); got != `{"from":"repo","install":{"boardsUrl":"https://boards.example"},"path":"app","repoUrl":"https://git.example.org/team/boards.git"}` {
		t.Errorf("with the boards address: %s", got)
	}
}

func TestAppSourceKeepsTheOrganizationsOffSwitch(t *testing.T) {
	src := func(off any) []byte {
		return []byte(`{"from":"repo","repoUrl":"https://git.example.com/acme/apps.git","path":"boards","off":` + string(raw(off)) + `}`)
	}
	got, err := env.CleanSource("app", src(true))
	if err != nil || !strings.Contains(string(got), `"off":true`) {
		t.Fatalf("off: %s %v", got, err)
	}
	// false is the same as absent: the source and its hash stay as before.
	got, err = env.CleanSource("app", src(false))
	if err != nil || strings.Contains(string(got), "off") {
		t.Fatalf("on: %s %v", got, err)
	}
	if _, err := env.CleanSource("app", src("yes")); err == nil {
		t.Fatal("a string for off is refused")
	}
	if _, err := env.CleanSource("mcp", []byte(`{"url":"https://x.example.com","off":true}`)); err == nil {
		t.Fatal("off is only an app's")
	}
}
