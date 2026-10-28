package agent

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// relayed posts to a route in the background, takes the event the agent sends to the cluster and settles it with the answer.
func relayed(t *testing.T, a *Agent, path, body string, answer DocResult) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	l := &link{control: make(chan []byte, 4), data: make(chan []byte, 4), done: make(chan struct{})}
	a.mu.Lock()
	a.clients[l] = struct{}{}
	a.mu.Unlock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- post(a, path, "local-secret", body) }()
	var ev map[string]any
	select {
	case frame := <-l.control:
		if err := json.Unmarshal(frame, &ev); err != nil {
			t.Fatalf("event frame: %s", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event reached the cluster")
	}
	a.settleDoc(ev["requestId"].(string), answer)
	select {
	case rec := <-done:
		return rec, ev
	case <-time.After(2 * time.Second):
		t.Fatal("the route did not answer")
	}
	return nil, nil
}

func TestDocRoutesNeedTheTokenAndALink(t *testing.T) {
	a := testAgent(t)
	for _, path := range []string{"/docs/publish", "/docs/acl"} {
		if rec := post(a, path, "", `{}`); rec.Code != 403 {
			t.Errorf("%s without a token: %d", path, rec.Code)
		}
		if rec := post(a, path, "local-secret", `{"slug":"x"}`); rec.Code != 503 {
			t.Errorf("%s without a link: %d", path, rec.Code)
		}
	}
}

func TestDocAclIsRelayedToTheCluster(t *testing.T) {
	a := testAgent(t)
	rec, ev := relayed(t, a, "/docs/acl", `{"slug":"plan","method":"put","route":"/agents/api/example/docs/plan/acl/team","body":{"level":"view"}}`,
		DocResult{Status: 200, Body: json.RawMessage(`{"team":"view"}`)})
	if ev["event"] != "doc.acl" || ev["slug"] != "plan" || ev["method"] != "PUT" || ev["route"] != "/agents/api/example/docs/plan/acl/team" {
		t.Errorf("event: %v", ev)
	}
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":200`) || !strings.Contains(rec.Body.String(), `"team":"view"`) {
		t.Errorf("answer: %d %s", rec.Code, rec.Body)
	}
	// A refusal of the cluster is passed on as it was.
	rec, _ = relayed(t, a, "/docs/acl", `{"slug":"plan","method":"GET","route":"/x"}`, DocResult{Status: 403, Body: json.RawMessage(`{"error":"no"}`)})
	if !strings.Contains(rec.Body.String(), `"status":403`) {
		t.Errorf("refusal: %s", rec.Body)
	}
	if rec := post(a, "/docs/acl", "local-secret", `{"slug":"plan","method":"GET","route":"no-slash"}`); rec.Code != 400 && rec.Code != 503 {
		t.Errorf("bad route: %d", rec.Code)
	}
}

func TestDocPublishChecksSecretsBeforeAnythingLeaves(t *testing.T) {
	a := testAgent(t)
	l := &link{control: make(chan []byte, 4), data: make(chan []byte, 4), done: make(chan struct{})}
	a.clients[l] = struct{}{}
	token := "glp" + "at-" + strings.Repeat("x", 24)
	body, _ := json.Marshal(map[string]string{"title": "Plan", "format": "md", "content": "# Plan\n\nfirst\n" + token + "\n"})
	rec := post(a, "/docs/publish", "local-secret", string(body))
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"line":4`) || !strings.Contains(rec.Body.String(), `"kind":"gitlab"`) {
		t.Errorf("secret: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Error("the answer repeats the secret")
	}
	select {
	case frame := <-l.control:
		t.Fatalf("a document with a secret was sent: %s", frame)
	default:
	}
	if rec := post(a, "/docs/publish", "local-secret", `{"title":"x","format":"pdf","content":"x"}`); rec.Code != 400 {
		t.Errorf("format: %d", rec.Code)
	}
}

func TestDocPublishIsRelayedWithItsSource(t *testing.T) {
	a := testAgent(t)
	rec, ev := relayed(t, a, "/docs/publish",
		`{"title":"Plan","format":"md","content":"# Plan","source":{"path":"/w/docs/plan.md","conversation":"tab-1"}}`,
		DocResult{Status: 201, Body: json.RawMessage(`{"slug":"plan-abc","version":1}`)})
	if ev["event"] != "doc.publish" || ev["visibility"] != "private" || ev["title"] != "Plan" {
		t.Errorf("event: %v", ev)
	}
	src, _ := ev["source"].(map[string]any)
	if src["path"] != "/w/docs/plan.md" || src["conversation"] != "tab-1" {
		t.Errorf("source: %v", ev["source"])
	}
	if !strings.Contains(rec.Body.String(), `"slug":"plan-abc"`) {
		t.Errorf("answer: %s", rec.Body)
	}
}
