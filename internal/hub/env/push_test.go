package env_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// fakeLink is the hub's link to one machine: it records what is sent and answers like an agent that applied the list.
type fakeLink struct {
	mu     sync.Mutex
	id     string
	sent   []hubproto.EnvSet
	status string // the status every item is reported with
}

func (f *fakeLink) Online() []string { return []string{f.id} }

func (f *fakeLink) DoInto(_ context.Context, machineID, op string, params, out any, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if machineID != f.id || op != hubproto.OpEnvSet {
		panic("unexpected call " + machineID + " " + op)
	}
	set := params.(hubproto.EnvSet)
	f.sent = append(f.sent, set)
	st := hubproto.EnvState{Rev: set.Rev, Hash: set.Hash}
	for _, it := range set.Items {
		st.Applied = append(st.Applied, hubproto.EnvApplied{ItemID: it.ItemID, Status: f.status})
	}
	raw, _ := json.Marshal(st)
	return json.Unmarshal(raw, out)
}

func (f *fakeLink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func TestPusherSendsOnChangeAndWhenTheMachineIsBehind(t *testing.T) {
	a := newAcme(t)
	m := a.anna.Machine(a.tenant, nil)
	if _, err := a.s.DB.Exec(context.Background(), `UPDATE machines SET team_id = ? WHERE id = ?`, a.core, m.MachineID); err != nil {
		t.Fatal(err)
	}
	skill := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	mcp := a.item(t, "mcp", "Трекер", map[string]any{"transport": "http", "url": "https://tracker.example.org/mcp", "secretRefs": []string{"TRACKER_TOKEN"}})
	doc := a.item(t, "doc", "Регламент", map[string]any{"url": "https://docs.example.org/rules"})

	link := &fakeLink{id: m.MachineID, status: "ok"}
	p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Now: a.s.Clock.Now, Every: time.Hour})
	defer p.Close()
	ctx := context.Background()
	push := func() {
		t.Helper()
		if err := p.Push(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// A machine without the feature gets nothing, whatever the layers say.
	a.entry(t, a.anna, "org", a.org, skill, nil)
	p.Connected(m.MachineID, hubproto.Hello{})
	push()
	if link.count() != 0 {
		t.Fatalf("sent to an agent without the feature: %d", link.count())
	}

	// An agent that holds nothing gets the list for its tenant and team: the org entry, the tenant one, its team's;
	// not the other team's.
	a.entry(t, a.dina, "tenant", a.tenant, mcp, nil)
	a.entry(t, a.boris, "team", a.core, doc, nil)
	a.entry(t, a.anna, "team", a.side, doc, nil) // another team
	p.Connected(m.MachineID, hubproto.Hello{Features: []string{hubproto.FeatureEnvironment}})
	push()
	if link.count() != 1 {
		t.Fatalf("sent %d, want 1", link.count())
	}
	first := link.sent[0]
	if first.Rev != 1 || len(first.Items) != 3 || first.Hash != hubproto.EnvHash(first.Items) {
		t.Fatalf("first = %+v", first)
	}
	for _, it := range first.Items {
		if string(it.Source) == "" || it.Layer == "" {
			t.Errorf("item = %+v", it)
		}
	}
	if raw, _ := json.Marshal(first); strings.Contains(string(raw), "TRACKER_TOKEN=") {
		t.Errorf("a secret value on the wire: %s", raw)
	}
	// The report is stored.
	st, err := a.s.DB.S().EnvStateByMachine(ctx, m.MachineID)
	if err != nil || st.Rev != 1 || st.Hash != first.Hash || st.SentRev != 1 || st.SentHash != first.Hash {
		t.Fatalf("state = %+v, %v", st, err)
	}
	var applied []hubproto.EnvApplied
	_ = json.Unmarshal([]byte(st.Applied), &applied)
	if len(applied) != 3 || applied[0].Status != "ok" {
		t.Errorf("applied = %+v", applied)
	}

	// It holds the list now: a pass sends nothing.
	push()
	if link.count() != 1 {
		t.Fatalf("sent again with nothing changed: %d", link.count())
	}

	// A change: a new list with the next revision. (An item two layers have is one row, the higher layer's.)
	repo := a.item(t, "repo", "Монорепо", map[string]any{"url": "https://git.example.org/acme/mono.git", "branch": "main"})
	_, row := a.entry(t, a.dina, "tenant", a.tenant, repo, nil)
	push()
	if link.count() != 2 || link.sent[1].Rev != 2 || len(link.sent[1].Items) != 4 || link.sent[1].Hash == first.Hash {
		t.Fatalf("after a change: %+v", link.sent)
	}
	// Switching it off takes the item out; the hash is the one of the list that has no such item.
	if r := a.dina.Do("PATCH", a.url("/env/entries/"+row.EntryID), map[string]any{"enabled": false}); r.Code != 200 {
		t.Fatal(r)
	}
	push()
	if link.count() != 3 || link.sent[2].Rev != 3 || len(link.sent[2].Items) != 3 {
		t.Fatalf("after a switch: %+v", link.sent)
	}

	// The machine reports another hash (it restarted from an older file): it is told again, with the same revision.
	p.Reported(m.MachineID, hubproto.EnvState{Rev: 2, Hash: link.sent[1].Hash}) // the list of before the switch
	push()
	if link.count() != 4 || link.sent[3].Rev != 3 || link.sent[3].Hash != link.sent[2].Hash {
		t.Fatalf("resend: %+v", link.sent)
	}
	// And a machine that reconnects holding the current list is left alone.
	p.Connected(m.MachineID, hubproto.Hello{Features: []string{hubproto.FeatureEnvironment}, EnvRev: 3, EnvHash: link.sent[3].Hash})
	push()
	if link.count() != 4 {
		t.Fatalf("sent to a machine that holds the list: %d", link.count())
	}
}

func TestPusherIsOffWithoutTheFlag(t *testing.T) {
	a := newAcme(t)
	m := a.anna.Machine(a.tenant, nil)
	skill := a.item(t, "skill", "Ревью кода", map[string]any{"body": "Пиши коротко."})
	a.entry(t, a.anna, "org", a.org, skill, nil)
	link := &fakeLink{id: m.MachineID, status: "ok"}
	for _, gate := range []env.FeatureGate{nil, env.StaticFeatures([]string{"catalog"})} {
		p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: gate, Every: time.Hour})
		p.Connected(m.MachineID, hubproto.Hello{Features: []string{hubproto.FeatureEnvironment}})
		if err := p.Push(context.Background()); err != nil {
			t.Fatal(err)
		}
		p.Close()
	}
	if link.count() != 0 {
		t.Errorf("sent with the feature off: %d", link.count())
	}
}

func TestPusherNothingToSendToAnEmptyMachine(t *testing.T) {
	a := newAcme(t)
	m := a.anna.Machine(a.tenant, nil)
	link := &fakeLink{id: m.MachineID, status: "ok"}
	p := env.NewPusher(env.PushDeps{DB: a.s.DB, Reg: link, Features: env.StaticFeatures([]string{"env"}), Every: time.Hour})
	defer p.Close()
	p.Connected(m.MachineID, hubproto.Hello{Features: []string{hubproto.FeatureEnvironment}})
	if err := p.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if link.count() != 0 {
		t.Errorf("an empty list was sent to a machine that holds none: %d", link.count())
	}
}
