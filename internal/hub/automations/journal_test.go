package automations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/eventbus"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// With the bus on, a machine's connector event is journaled for its owner, once per fingerprint, without its text,
// and the rules still take it as before.
func TestConnectorEventIsJournaled(t *testing.T) {
	s := newSvc(t, noon())
	s.bus = eventbus.New(s.db, nil, s.now)
	ctx := context.Background()
	if _, ok, err := s.addRule(ctx, Rule{OwnerID: "u1", Name: "feed", Trigger: Trigger{Source: "feed"}, PromptTemplate: "x", Model: "sonnet", Enabled: true}); err != nil || !ok {
		t.Fatal(err)
	}
	m := store.Machine{ID: "m1", CreatedBy: "u1"}
	raw := []byte(`{"source":"feed","fingerprint":"rss:c1:g1","data":{"type":"feed.item","connector":"rss","connection":"c1","id":"g1",
		"at":"2026-01-05T08:00:00Z","actor":"Ann","title":"secret subject","excerpt":"the whole letter","ref":"item id=g1","mine":false,
		"facts":{"list":"true"}}}`)
	for range 2 {
		results, err := s.machineEvent(ctx, m, raw)
		if err != nil || len(results) != 1 {
			t.Fatalf("rules: %v %v", results, err)
		}
	}
	events, err := s.bus.List(ctx, eventbus.Query{Owner: "u1"})
	if err != nil || len(events) != 1 {
		t.Fatalf("journal: %+v %v", events, err)
	}
	e := events[0]
	if e.Type != "feed.item" || e.Source != "rss" || e.Connection != "c1" || e.Ext != "g1" || e.Machine != "m1" ||
		e.Ref != "item id=g1" || e.Facts["list"] != "true" || !e.At.Equal(time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("event: %+v", e)
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "secret subject") || strings.Contains(string(b), "the whole letter") {
		t.Fatalf("the journal kept text: %s", b)
	}
	// Another owner sees nothing of it.
	if other, _ := s.bus.List(ctx, eventbus.Query{Owner: "u2"}); len(other) != 0 {
		t.Fatalf("u2 sees %+v", other)
	}
}

// Without the bus nothing changes and nothing is written.
func TestNoBusNoJournal(t *testing.T) {
	s := newSvc(t, noon())
	m := store.Machine{ID: "m1", CreatedBy: "u1"}
	if _, err := s.machineEvent(context.Background(), m, []byte(`{"source":"feed","fingerprint":"f","data":{"type":"feed.item","id":"g"}}`)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM bus_events`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}

// A trigger in the mode «agent» queues one run per event, under one rule per trigger that no outside event matches.
func TestAgentForEvent(t *testing.T) {
	s := newSvc(t, noon())
	ctx := context.Background()
	e := eventbus.Event{ID: "ev1", Owner: "u1", Type: "issue.assigned", Source: "youtrack", Connection: "c1"}
	for range 2 {
		if err := s.AgentForEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := s.rules(ctx, "u1", true)
	if err != nil || len(rules) != 1 || rules[0].Trigger.Source != SourceBus {
		t.Fatalf("rules: %+v %v", rules, err)
	}
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?`, rules[0].ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("runs %d %v", n, err)
	}
	// An outside event may not pass for the bus.
	if _, err := s.machineEvent(ctx, store.Machine{ID: "m1", CreatedBy: "u1"}, []byte(`{"source":"bus","fingerprint":"x","data":{"type":"issue.assigned"}}`)); err == nil {
		t.Fatal("an outside event took the bus's source")
	}
}
