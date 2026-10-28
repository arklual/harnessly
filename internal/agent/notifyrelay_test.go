package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNotifyRouteNeedsTheTokenAKnownActionAndALink(t *testing.T) {
	a := testAgent(t)
	if rec := post(a, "/notify/call", "", `{"action":"status"}`); rec.Code != 403 {
		t.Errorf("without a token: %d", rec.Code)
	}
	if rec := post(a, "/notify/call", "local-secret", `{"action":"drop-table"}`); rec.Code != 400 {
		t.Errorf("unknown action: %d", rec.Code)
	}
	if rec := post(a, "/notify/call", "local-secret", `{"action":"status"}`); rec.Code != 503 {
		t.Errorf("without a link: %d", rec.Code)
	}
}

func TestNotifyCallIsRelayedToTheCluster(t *testing.T) {
	a := testAgent(t)
	rec, ev := relayed(t, a, "/notify/call", `{"action":"send","parts":[{"html":"<b>done</b>","text":"done"}],"silent":true,"person":"x@y"}`,
		DocResult{Status: 200, Body: json.RawMessage(`{"chat":"Анна","messages":1}`)})
	if ev["event"] != "notify.call" || ev["action"] != "send" || ev["silent"] != true || ev["person"] != nil {
		t.Errorf("event: %v", ev)
	}
	parts, _ := ev["parts"].([]any)
	if len(parts) != 1 {
		t.Errorf("parts: %v", ev["parts"])
	}
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":200`) || !strings.Contains(rec.Body.String(), `"messages":1`) {
		t.Errorf("answer: %d %s", rec.Code, rec.Body)
	}
	_, ev = relayed(t, a, "/notify/call", `{"action":"instruction","instruction":"когда закончил"}`, DocResult{Status: 200, Body: json.RawMessage(`{}`)})
	if ev["instruction"] != "когда закончил" || ev["parts"] != nil {
		t.Errorf("instruction event: %v", ev)
	}
}

func TestNotifyResultSettlesTheWait(t *testing.T) {
	a := testAgent(t)
	ch := make(chan DocResult, 1)
	a.mu.Lock()
	a.pendingDocs["r1"] = ch
	a.mu.Unlock()
	if _, _, err := a.handle(nil, map[string]json.RawMessage{"op": json.RawMessage(`"notify.result"`), "requestId": json.RawMessage(`"r1"`),
		"status": json.RawMessage(`409`), "body": json.RawMessage(`{"error":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.Status != 409 {
		t.Errorf("settled: %+v", got)
	}
}
