package env_test

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The organization's rule for apps rides on env.set to a machine that keeps it, and its hash changes with
// the rule; an older machine gets the list as before, without the field and under the items' hash.
func TestAppsConsentRuleGoesWithTheList(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "Ревью", map[string]any{"body": "Пиши коротко."})
	if code, _ := a.entry(t, a.anna, "org", a.org, skill, map[string]any{"pinned": true}); code != 201 {
		t.Fatalf("entry: %d", code)
	}
	rule := func(v string) {
		t.Helper()
		if r := a.anna.Do("PATCH", "/api/orgs/"+a.org, map[string]any{"appsConsent": v}); r.Code != 200 {
			t.Fatalf("patch: %s", r)
		}
	}
	type machine struct {
		link *fakeLink
		p    *env.Pusher
	}
	connect := func(features ...string) machine {
		t.Helper()
		m := a.anna.Machine(a.tenant, nil)
		link := &fakeLink{id: m.MachineID, status: "ok"}
		p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour})
		t.Cleanup(p.Close)
		p.Connected(m.MachineID, hubproto.Hello{Features: append([]string{hubproto.FeatureEnvironment}, features...)})
		return machine{link, p}
	}
	push := func(m machine) hubproto.EnvSet {
		t.Helper()
		if err := m.p.Push(context.Background()); err != nil {
			t.Fatal(err)
		}
		if m.link.count() == 0 {
			t.Fatal("nothing was sent")
		}
		return m.link.sent[m.link.count()-1]
	}

	keeps := connect(hubproto.FeatureEnvAppsConsent)
	old := connect()
	first := push(keeps)
	if first.AppsConsent != "" || first.Hash != hubproto.EnvHash(first.Items) {
		t.Fatalf("the rule ask is not sent and leaves the items' hash: %+v", first)
	}
	push(old)

	rule("auto")
	got := push(keeps)
	if got.AppsConsent != hubproto.AppsConsentAuto || got.Hash != hubproto.EnvSetHash(hubproto.EnvHash(got.Items), hubproto.AppsConsentAuto) || got.Hash == first.Hash || got.Rev <= first.Rev {
		t.Fatalf("the rule auto comes with a new hash and revision: %+v (first %s)", got, first.Hash)
	}
	sentOld := old.link.count()
	if err := old.p.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if old.link.count() != sentOld {
		t.Fatalf("an older machine got the list again for a rule it cannot keep: %+v", old.link.sent[old.link.count()-1])
	}

	rule("ask")
	back := push(keeps)
	if back.AppsConsent != "" || back.Hash != first.Hash {
		t.Fatalf("the rule turned off brings the items' hash back: %+v", back)
	}
}
