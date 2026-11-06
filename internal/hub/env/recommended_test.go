package env_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestRecommendedIsOffUntilThePersonTurnsItOn(t *testing.T) {
	a := newAcme(t, hubtest.Env("HUB_ENV_RECOMMENDED", "on"))
	send := func(features ...string) map[string]string {
		t.Helper()
		m := a.anna.Machine(a.tenant, nil)
		link := &fakeLink{id: m.MachineID, status: "ok"}
		p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now,
			Every: time.Hour, Recommended: true})
		defer p.Close()
		p.Connected(m.MachineID, hubproto.Hello{Features: append([]string{hubproto.FeatureEnvironment}, features...)})
		if err := p.Push(context.Background()); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		if link.count() > 0 {
			for _, it := range link.sent[0].Items {
				out[it.Name] = it.Kind
			}
		}
		return out
	}

	// The organization's layer shows the group, its items locked: the platform's own.
	l := layer(a.layers(t, a.anna, ""), "org", a.org)
	if len(l.Groups) != 1 || l.Groups[0].ID != env.RecommendedGroupID || !l.Groups[0].BuiltIn || l.Groups[0].DefaultOn {
		t.Fatalf("groups = %+v", l.Groups)
	}
	var builtin env.EntryItem
	for _, it := range l.Items {
		if it.Name == "Superpowers" {
			builtin = it
		}
	}
	if !builtin.Locked || builtin.LockedReason == nil || *builtin.LockedReason != "built_in" || builtin.Kind != "plugin" {
		t.Fatalf("the built-in entry = %+v", builtin)
	}
	if r := a.anna.Do("PATCH", a.url("/env/entries/"+builtin.EntryID), map[string]any{"enabled": false}); r.Code != 404 {
		t.Errorf("a built-in entry changed: %s", r)
	}
	if r := a.anna.Do("PATCH", a.url("/env/groups/"+env.RecommendedGroupID), map[string]any{"required": true}); r.Code != 409 {
		t.Errorf("the built-in group changed: %s", r)
	}
	if r := a.anna.Do("DELETE", a.url("/env/groups/"+env.RecommendedGroupID), nil); r.Code != 409 {
		t.Errorf("the built-in group removed: %s", r)
	}

	// Off by default: nothing of it goes to the machine.
	if got := send(hubproto.FeatureEnvSetup, hubproto.FeatureEnvClaude); got["Superpowers"] != "" {
		t.Fatalf("off by default, sent: %v", got)
	}
	// The person turns it on: a machine that does the new kinds gets them, an older one does not.
	if r := a.anna.Do("PUT", "/api/me/env/groups/"+env.RecommendedGroupID+"?space="+a.tenant, map[string]any{"on": true}); r.Code != 200 {
		t.Fatalf("turn on: %s", r)
	}
	if got := send(hubproto.FeatureEnvSetup, hubproto.FeatureEnvClaude); got["Superpowers"] != "plugin" || got["Экономные настройки Claude"] != "claude-settings" {
		t.Fatalf("on, sent: %v", got)
	}
	if got := send(hubproto.FeatureEnvSetup); len(got) != 0 {
		t.Errorf("an agent without environment.claude got: %v", got)
	}

	// The admin puts an entry of their own into the group.
	own := a.item(t, "skill", "Наш процесс", map[string]any{"body": "x"})
	if code, _ := a.entry(t, a.anna, "org", a.org, own, map[string]any{"groupId": env.RecommendedGroupID}); code != 201 {
		t.Errorf("own entry into the group: %d", code)
	}
	if got := send(hubproto.FeatureEnvSetup, hubproto.FeatureEnvClaude); got["Наш процесс"] != "skill" {
		t.Errorf("own entry in the group, sent: %v", got)
	}
}

func TestClaudeKindsAreCheckedAndForAdminsOnly(t *testing.T) {
	a := newAcme(t)
	post := func(p *hubtest.Person, kind string, src map[string]any, owner map[string]any) hubtest.Resp {
		body := map[string]any{"kind": kind, "name": "X", "source": src}
		for k, v := range owner {
			body[k] = v
		}
		return p.Do("POST", a.url("/catalog"), body)
	}
	ok := []struct {
		kind string
		src  map[string]any
	}{
		{"plugin", map[string]any{"marketplace": "anthropics/claude-plugins-official", "plugin": "superpowers@claude-plugins-official"}},
		{"plugin", map[string]any{"marketplace": "https://git.example.org/team/plugins.git", "plugin": "ours@team"}},
		{"claude-settings", map[string]any{"always": map[string]any{"env": map[string]string{"CLAUDE_CODE_SUBAGENT_MODEL": "sonnet"}}, "claudeMd": "## Правила"}},
	}
	for i, c := range ok {
		body := map[string]any{"kind": c.kind, "name": fmt.Sprintf("X%d", i), "source": c.src}
		if r := a.anna.Do("POST", a.url("/catalog"), body); r.Code != 201 {
			t.Errorf("%s %v: %s", c.kind, c.src, r)
		}
	}
	bad := []map[string]any{
		{"marketplace": "anthropics/claude-plugins-official", "plugin": "no-marketplace"},
		{"marketplace": "http://plain.example.org/x.git", "plugin": "a@b"},
		{"always": map[string]any{"hooks": map[string]any{}}},
		{"always": map[string]any{"env": map[string]string{"ANTHROPIC_BASE_URL": "https://elsewhere"}}},
		{"ifAbsent": map[string]any{"model": map[string]any{"deep": true}}},
	}
	for i, src := range bad {
		kind := "claude-settings"
		if i < 2 {
			kind = "plugin"
		}
		if r := post(a.anna, kind, src, nil); r.Code != 400 {
			t.Errorf("%v: %s", src, r)
		}
	}
	// A team's lead cannot bring a plugin: its hooks run in every chat of the team.
	if r := post(a.boris, "plugin", ok[0].src, map[string]any{"ownerLayer": "team", "ownerScope": a.core}); r.Code != 400 && r.Code != 403 {
		t.Errorf("a lead's plugin: %s", r)
	}
}
