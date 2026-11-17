package agent

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A change of the person's own layer goes to the machine's own hub as env.own and comes back with what the hub answered.
func TestAnOwnLayerChangeGoesToTheHub(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	a := r.agent
	waitFor(t, func() bool { return len(a.hub.hubLinks()) > 0 })
	body := `{"action":"add","kind":"mcp","name":"Мой трекер","source":{"transport":"http","url":"https://tracker.example.org/mcp"}}`
	if rec := post(a, "/env/own", "wrong", body); rec.Code != 403 {
		t.Fatalf("without the token: %d %s", rec.Code, rec.Body)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- post(a, "/env/own", a.localToken, body) }()
	ev, _ := c.waitEvent(hubproto.EventEnvOwn, 0, 5*time.Second)
	if ev == nil {
		t.Fatal("no env.own reached the hub")
	}
	var p hubproto.EnvOwnRequest
	if json.Unmarshal(mustJSON(ev), &p) != nil || p.RequestID == "" || p.Action != hubproto.EnvOwnAdd || p.Kind != "mcp" || p.Name == nil || *p.Name != "Мой трекер" {
		t.Fatalf("env.own: %v", ev)
	}
	c.mustCall(hubproto.OpEnvOwned, map[string]any{"requestId": p.RequestID, "status": 409, "code": "given", "message": "Такое уже есть"})
	select {
	case rec := <-done:
		var res hubproto.EnvOwnResult
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &res) != nil || res.Status != 409 || res.Code != "given" {
			t.Fatalf("answer: %d %s", rec.Code, rec.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the change did not answer")
	}
}

// An observing hub does not own the machine: it is not asked.
func TestAnOwnLayerChangeIsNotSentToAnObservingHub(t *testing.T) {
	r := startHubAgent(t, rigOptions{observe: true})
	c := r.connect()
	a := r.agent
	waitFor(t, func() bool { return len(a.hub.hubLinks()) > 0 })
	rec := post(a, "/env/own", a.localToken, `{"action":"remove","itemId":"x"}`)
	var res hubproto.EnvOwnResult
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &res) != nil || res.Code != "no_hub" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if ev, _ := c.waitEvent(hubproto.EventEnvOwn, 0, 300*time.Millisecond); ev != nil {
		t.Fatalf("the observing hub was asked: %v", ev)
	}
}
