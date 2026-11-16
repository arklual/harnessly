package automations

import (
	"context"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A connector's event from a machine (automations.event) queues a run of its owner's rule only, once per fingerprint,
// and the hub keeps no text of the letter or message even when a machine sends it.
func TestMachineConnectorEvent(t *testing.T) {
	s := newSvc(t, noon())
	ctx := context.Background()
	mine, ok, err := s.addRule(ctx, Rule{OwnerID: "u1", Name: "feed", Trigger: Trigger{Source: "feed", Filter: map[string]any{"type": "feed.item"}}, PromptTemplate: "read it", Model: "sonnet", Enabled: true})
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, ok, err := s.addRule(ctx, Rule{OwnerID: "u2", Name: "other", Trigger: Trigger{Source: "feed"}, PromptTemplate: "x", Model: "sonnet", Enabled: true}); err != nil || !ok {
		t.Fatal(err)
	}
	m := store.Machine{ID: "m1", CreatedBy: "u1"}
	raw := []byte(`{"source":"feed","fingerprint":"rss:c1:g1","data":{"type":"feed.item","connection":"c1","title":"secret subject","excerpt":"the whole letter","ref":"harnessly connector read c1 item id=g1"}}`)
	results, err := s.machineEvent(ctx, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0]["ruleId"] != mine.ID || results[0]["queued"] != true {
		t.Fatalf("results: %v", results)
	}
	again, err := s.machineEvent(ctx, m, raw)
	if err != nil || len(again) != 1 || again[0]["queued"] == true {
		t.Fatalf("the same fingerprint queued twice: %v %v", again, err)
	}
	run, err := s.runByID(ctx, results[0]["runId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	got := string(run.EventPayload)
	if strings.Contains(got, "secret subject") || strings.Contains(got, "the whole letter") || !strings.Contains(got, `"ref"`) {
		t.Fatalf("the hub kept text: %s", got)
	}
	if _, err := s.machineEvent(ctx, m, []byte(`{"source":"schedule","fingerprint":"x","data":{}}`)); err == nil {
		t.Fatal("a reserved source was taken")
	}
	if _, err := s.machineEvent(ctx, store.Machine{ID: "m2"}, raw); err == nil {
		t.Fatal("a machine without an owner was taken")
	}
}

// «A brief before a meeting»: a rule takes calendar.starting from a calendar connector by source and type, and its run
// gets the event's ref and facts (time, people's names, the call's service) but not the meeting's title.
func TestBriefBeforeMeetingRule(t *testing.T) {
	s := newSvc(t, noon())
	ctx := context.Background()
	rule, ok, err := s.addRule(ctx, Rule{OwnerID: "u1", Name: "brief", Trigger: Trigger{Source: "calendar", Filter: map[string]any{"type": "calendar.starting"}},
		PromptTemplate: "A meeting starts soon: {{event}}. Read it by its ref and brief me on the people and the topic.", Model: "sonnet", Enabled: true})
	if err != nil || !ok {
		t.Fatal(err)
	}
	m := store.Machine{ID: "m1", CreatedBy: "u1"}
	changed := []byte(`{"source":"calendar","fingerprint":"caldav:c1:changed:x","data":{"type":"calendar.changed","connection":"c1","facts":{"change":"moved"}}}`)
	if res, err := s.machineEvent(ctx, m, changed); err != nil || (len(res) > 0 && res[0]["queued"] == true) {
		t.Fatalf("calendar.changed took the brief rule: %v %v", res, err)
	}
	raw := []byte(`{"source":"calendar","fingerprint":"caldav:c1:starting:a:1","data":{"type":"calendar.starting","connector":"caldav","connection":"c1","title":"Board meeting","thread":"uid-1",` +
		`"ref":"harnessly connector read c1 event id=/cal/a.ics","facts":{"minutes":"15","people":"Ann, Bob","attendees":"2","call":"telemost"}}}`)
	results, err := s.machineEvent(ctx, m, raw)
	if err != nil || len(results) != 1 || results[0]["ruleId"] != rule.ID || results[0]["queued"] != true {
		t.Fatalf("results: %v %v", results, err)
	}
	run, err := s.runByID(ctx, results[0]["runId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	got := string(run.EventPayload)
	if strings.Contains(got, "Board meeting") || !strings.Contains(got, "event id=/cal/a.ics") || !strings.Contains(got, "Ann, Bob") {
		t.Fatalf("payload: %s", got)
	}
}
