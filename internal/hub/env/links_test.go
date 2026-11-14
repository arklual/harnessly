package env_test

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The organization's links for the chat ride on env.set only to a machine that keeps them, under a hash a
// keeper recomputes; a change of them goes to the machine again, and clearing them brings the items' hash back.
func TestLinksGoOnlyToMachinesThatKeepThem(t *testing.T) {
	a := newAcme(t)
	skill := a.item(t, "skill", "Ревью", map[string]any{"body": "Пиши коротко."})
	if code, _ := a.entry(t, a.anna, "org", a.org, skill, map[string]any{"pinned": true}); code != 201 {
		t.Fatalf("entry: %d", code)
	}
	connect := func(features ...string) (*fakeLink, *env.Pusher) {
		t.Helper()
		m := a.anna.Machine(a.tenant, nil)
		link := &fakeLink{id: m.MachineID, status: "ok"}
		p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour})
		t.Cleanup(p.Close)
		p.Connected(m.MachineID, hubproto.Hello{Features: append([]string{hubproto.FeatureEnvironment}, features...)})
		return link, p
	}
	keepsLink, keeps := connect(hubproto.FeatureEnvLinks)
	oldLink, old := connect()
	push := func(link *fakeLink, p *env.Pusher) hubproto.EnvSet {
		t.Helper()
		if err := p.Push(context.Background()); err != nil {
			t.Fatal(err)
		}
		if link.count() == 0 {
			t.Fatal("nothing was sent")
		}
		return link.sent[link.count()-1]
	}
	first := push(keepsLink, keeps)
	push(oldLink, old)
	if first.Links != nil || first.Hash != hubproto.EnvHash(first.Items) {
		t.Fatalf("no links, no field and the items' hash: %+v", first)
	}

	links := map[string]any{"standDomain": "preview.example.com", "trackerUrl": "https://tracker.example.com"}
	if r := a.anna.Do("PATCH", "/api/orgs/"+a.org, map[string]any{"links": links}); r.Code != 200 {
		t.Fatalf("patch: %s", r)
	}
	got := push(keepsLink, keeps)
	if got.Links == nil || got.Links.StandDomain != "preview.example.com" || got.Links.TrackerURL != "https://tracker.example.com" {
		t.Fatalf("the machine that keeps links got %+v", got.Links)
	}
	if got.Hash != hubproto.EnvSetHashWith(hubproto.EnvHash(got.Items), "", got.Links) || got.Hash == first.Hash || got.Rev <= first.Rev {
		t.Fatalf("the links come with their hash and a new revision: %+v", got)
	}
	sentOld := oldLink.count()
	if err := old.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if oldLink.count() != sentOld {
		t.Fatalf("an older machine got the list again for links it cannot keep: %+v", oldLink.sent[oldLink.count()-1])
	}

	// Already sent: a pass sends nothing more (the hash the hub waits for is the one the machine holds).
	n := keepsLink.count()
	if err := keeps.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keepsLink.count() != n {
		t.Fatalf("the same links were sent again")
	}

	if r := a.anna.Do("PATCH", "/api/orgs/"+a.org, map[string]any{"links": map[string]any{}}); r.Code != 200 {
		t.Fatalf("clear: %s", r)
	}
	back := push(keepsLink, keeps)
	if back.Links != nil || back.Hash != first.Hash {
		t.Fatalf("cleared links bring the items' hash back: %+v", back)
	}
}
