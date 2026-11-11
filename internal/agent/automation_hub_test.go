package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The hub the machine belongs to starts its automation's conversation over the link, and the conversation's report
// reaches the hub over the same link.
func TestHubStartsAnAutomationAndHearsItsReport(t *testing.T) {
	var started atomic.Int32
	r := startHubAgent(t, rigOptions{runtimeRoute: func(w http.ResponseWriter, req *http.Request) bool {
		if !strings.HasSuffix(req.URL.Path, "/api/agent/automations/start") {
			return false
		}
		started.Add(1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		return true
	}})
	c := r.connect()
	c.mustCall("link.subscribe", map[string]any{"events": []string{"automations.result"}})
	c.mustCall("conversations.start", map[string]any{"prompt": "p", "runId": "run-1", "resultToken": "tok-1",
		"env": map[string]string{"AUTOMATION_TOKEN": "tok-1"}})
	if started.Load() != 1 {
		t.Fatalf("runtime asked %d times", started.Load())
	}
	if rec := post(r.agent, "/automations/report", "", `{"runId":"run-1","resultToken":"tok-1","summary":"done"}`); rec.Code != 200 {
		t.Fatalf("report answered %d", rec.Code)
	}
	ev, _ := c.waitEvent("automations.result", 0, 5*time.Second)
	if ev == nil {
		t.Fatal("the report did not reach the hub")
	}
	var run, summary string
	_ = json.Unmarshal(ev["runId"], &run)
	_ = json.Unmarshal(ev["summary"], &summary)
	if run != "run-1" || summary != "done" {
		t.Fatalf("event %v", ev)
	}
}

// Anything but a run of an automation, and any start while the policy keeps the hub off the runtime, is refused.
func TestHubAutomationStartIsRefusedOutsideItsCase(t *testing.T) {
	r := startHubAgent(t, rigOptions{policy: "runtime_session = false\n"})
	c := r.connect()
	for _, params := range []map[string]any{
		nil,
		{"prompt": "p"},
		{"prompt": "p", "runId": "run-1", "resultToken": "tok-1"},
	} {
		if _, e := c.call("conversations.start", params); !strings.HasPrefix(e, "policy_denied: ") {
			t.Errorf("%v: %q", params, e)
		}
	}
}

// A machine run by its own cluster (an observe link) starts the hub's automations only when the operator opted in with
// AGENT_HUB_AUTOMATIONS=on; the report then reaches the hub over the same link.
func TestHubAutomationOnAnObserveLinkNeedsTheOptIn(t *testing.T) {
	params := map[string]any{"prompt": "p", "runId": "run-2", "resultToken": "tok-2", "env": map[string]string{"AUTOMATION_TOKEN": "tok-2"}}

	t.Setenv("AGENT_HUB_AUTOMATIONS", "")
	off := startHubAgent(t, rigOptions{observe: true})
	if _, e := off.connect().call("conversations.start", params); !strings.HasPrefix(e, "policy_denied: ") || !strings.Contains(e, "AGENT_HUB_AUTOMATIONS") {
		t.Fatalf("without the opt-in: %q", e)
	}

	t.Setenv("AGENT_HUB_AUTOMATIONS", "on")
	var started atomic.Int32
	on := startHubAgent(t, rigOptions{observe: true, runtimeRoute: func(w http.ResponseWriter, req *http.Request) bool {
		if !strings.HasSuffix(req.URL.Path, "/api/agent/automations/start") {
			return false
		}
		started.Add(1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		return true
	}})
	c := on.connect()
	c.mustCall("link.subscribe", map[string]any{"events": []string{"automations.result"}})
	for _, bad := range []map[string]any{nil, {"prompt": "p"}} {
		if _, e := c.call("conversations.start", bad); !strings.HasPrefix(e, "policy_denied: ") {
			t.Errorf("%v: %q", bad, e)
		}
	}
	c.mustCall("conversations.start", params)
	if started.Load() != 1 {
		t.Fatalf("runtime asked %d times", started.Load())
	}
	on.agent.hub.emitAutomationResult(evAutomationResult{Event: "automations.result", RunID: "run-2", ResultToken: "tok-2", Summary: "ok"})
	if ev, _ := c.waitEvent("automations.result", 0, 5*time.Second); ev == nil {
		t.Fatal("the report did not reach the hub over the observe link")
	}
}

// The kill switch stops the opted-in observe link too.
func TestHubAutomationOptInDoesNotOutliveTheKillSwitch(t *testing.T) {
	t.Setenv("AGENT_HUB_AUTOMATIONS", "on")
	r := startHubAgent(t, rigOptions{observe: true})
	h := r.agent.hub
	if !h.automationsServed(hubproto.Policy{}) {
		t.Fatal("opted in, not served")
	}
	t.Setenv("AGENT_HUB_AUTOMATIONS", "off")
	if h.automationsServed(hubproto.Policy{}) {
		t.Fatal("opt-in withdrawn, still served")
	}
}
