package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A runtime stand-in that records what the agent relays to it.
func relayTarget(t *testing.T, a *Agent) *[]string {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen = append(seen, r.URL.Path+" "+string(raw)+" cookie="+strconv.FormatBool(strings.Contains(r.Header.Get("cookie"), "=")))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	a.cfg.RuntimePort, _ = strconv.Atoi(u.Port())
	a.cfg.RuntimeAPI = "/api/plat"
	a.cfg.RuntimeCookie = "session-token"
	a.secret = []byte(strings.Repeat("s", 32))
	a.client = srv.Client()
	return &seen
}

func TestAgentRelaysDesignOpsToTheRuntime(t *testing.T) {
	a := testAgent(t)
	seen := relayTarget(t, a)
	access := `{"op":"design.access","enforce":true,"at":"2026-10-25T10:00:00Z","projects":{"p1":{"role":"edit","owner":"a@b.c","ownerName":"A","until":null}},"team":{"p1":"none"}}`
	notify := `{"op":"design.notify","title":"t","body":"b","url":"/agents/design/p1","tag":"g"}`
	for _, raw := range []string{access, notify} {
		var req map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			t.Fatal(err)
		}
		res, _, err := a.handle(nil, req)
		if err != nil || string(res.(json.RawMessage)) != `{"ok":true}` {
			t.Fatalf("%s: %v %v", raw, res, err)
		}
	}
	if len(*seen) != 2 || !strings.HasPrefix((*seen)[0], "/api/plat/design/access ") || !strings.HasPrefix((*seen)[1], "/api/plat/design/notify ") {
		t.Fatalf("relayed: %v", *seen)
	}
	if !strings.Contains((*seen)[0], `"enforce":true`) || !strings.Contains((*seen)[0], `"ownerName":"A"`) || strings.Contains((*seen)[0], `"op"`) {
		t.Errorf("snapshot body: %s", (*seen)[0])
	}
}

func TestDesignEventRoute(t *testing.T) {
	a := testAgent(t)
	if rec := post(a, "/design/event", "", `{"kind":"design.created","project":"p1"}`); rec.Code != 403 {
		t.Errorf("without a token: %d", rec.Code)
	}
	if rec := post(a, "/design/event", "local-secret", `{"kind":"docs.bound","project":"p1"}`); rec.Code != 400 {
		t.Errorf("foreign kind: %d", rec.Code)
	}
	// No cluster link: accepted, not sent, never an error for the runtime.
	rec := post(a, "/design/event", "local-secret", `{"kind":"design.created","project":"p1"}`)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"sent":false`) {
		t.Errorf("without a link: %d %s", rec.Code, rec.Body)
	}
	l := &link{control: make(chan []byte, 4), data: make(chan []byte, 4), done: make(chan struct{})}
	a.clients[l] = struct{}{}
	for _, kind := range []string{"design.created", "design.claim"} {
		if rec := post(a, "/design/event", "local-secret", `{"kind":"`+kind+`","project":"p1"}`); rec.Code != 202 || !strings.Contains(rec.Body.String(), `"sent":true`) {
			t.Fatalf("%s with a link: %d %s", kind, rec.Code, rec.Body)
		}
		select {
		case frame := <-l.control:
			if string(frame) != `{"event":"`+kind+`","project":"p1"}` {
				t.Errorf("event frame: %s", frame)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s was not sent", kind)
		}
	}
}

func TestEmitDesignEventNeverFails(t *testing.T) {
	// No agent.json, a broken one, a dead port: nothing blocks and nothing panics.
	EmitDesignEvent("", "", DesignCreated, "p1")
	EmitDesignEvent(t.TempDir(), "", DesignClaim, "p1")
	dir := t.TempDir()
	writeFile(t, dir+"/agent.json", `{"port":1,"token":"x"}`)
	done := make(chan struct{})
	go func() { emitDesignEvent(dir, "", DesignClaim, "p1"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the emitter hung")
	}
	writeFile(t, dir+"/agent.json", `not json`)
	emitDesignEvent(dir, "", DesignClaim, "p1")
}
