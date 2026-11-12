package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type docRuntime struct {
	mu    sync.Mutex
	paths []string
	body  map[string]any
}

func (rt *docRuntime) route(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasSuffix(r.URL.Path, "/docs/deliver") {
		return false
	}
	rt.mu.Lock()
	rt.paths = append(rt.paths, r.URL.Path)
	_ = json.NewDecoder(r.Body).Decode(&rt.body)
	rt.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]string{"mode": "typed"})
	return true
}

// On the machine's own hub link a publish goes to the hub and waits for its doc.result; the hub hands comments to the
// chat by docs.deliver; bindings and replies of documents go to the hub, a board's binding stays with the cluster.
func TestDocsGoToTheHub(t *testing.T) {
	rt := &docRuntime{}
	r := startHubAgent(t, rigOptions{runtimeRoute: rt.route})
	c := r.connect()
	a := r.agent
	waitFor(t, func() bool { return len(a.hub.hubLinks()) > 0 })

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- post(a, "/docs/publish", a.localToken, `{"title":"План","content":"# План","format":"md"}`)
	}()
	ev, n := c.waitEvent(hubproto.EventDocPublish, 0, 5*time.Second)
	if ev == nil {
		t.Fatal("no doc.publish reached the hub")
	}
	var requestID string
	_ = json.Unmarshal(ev["requestId"], &requestID)
	c.mustCall(hubproto.OpDocResult, map[string]any{"requestId": requestID, "status": 201, "body": map[string]string{"slug": "plan"}})
	select {
	case rec := <-done:
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":201`) || !strings.Contains(rec.Body.String(), `"slug":"plan"`) {
			t.Fatalf("publish answer: %d %s", rec.Code, rec.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the publish did not answer")
	}

	if rec := post(a, "/docs/bind", a.localToken, `{"slug":"plan","title":"План","cwd":"/work"}`); rec.Code != 202 {
		t.Fatalf("bind: %d %s", rec.Code, rec.Body)
	}
	bound, n := c.waitEvent(hubproto.EventDocBound, n, 5*time.Second)
	var b struct{ Binding hubproto.DocBinding }
	if bound == nil || json.Unmarshal(mustJSON(bound), &b) != nil || b.Binding.Slug != "plan" || b.Binding.Kind != "artifact" ||
		b.Binding.Cwd == nil || *b.Binding.Cwd != "/work" {
		t.Fatalf("docs.bound: %v", bound)
	}
	if rec := post(a, "/docs/reply", a.localToken, `{"threadId":"t-1","body":"Поправил","resolve":true}`); rec.Code != 202 {
		t.Fatalf("reply: %d %s", rec.Code, rec.Body)
	}
	replied, n := c.waitEvent(hubproto.EventDocReply, n, 5*time.Second)
	var rep hubproto.DocReply
	if replied == nil || json.Unmarshal(mustJSON(replied), &rep) != nil || rep.ThreadID != "t-1" || rep.Body != "Поправил" || !rep.Resolve {
		t.Fatalf("docs.reply: %v", replied)
	}
	if rec := post(a, "/docs/bind", a.localToken, `{"slug":"b1","kind":"board","key":"k","title":"Доска"}`); rec.Code != 202 {
		t.Fatalf("bind a board: %d %s", rec.Code, rec.Body)
	}
	// The hub learns where a board was drawn and never the board's key.
	boardBound, _ := c.waitEvent(hubproto.EventDocBound, n, 5*time.Second)
	var bb struct{ Binding hubproto.DocBinding }
	if boardBound == nil || json.Unmarshal(mustJSON(boardBound), &bb) != nil || bb.Binding.Slug != "b1" || bb.Binding.Kind != "board" || bb.Binding.Key != nil {
		t.Fatalf("a board's binding for the hub: %v", boardBound)
	}
	if strings.Contains(string(mustJSON(boardBound)), `"key":"k"`) {
		t.Fatalf("the board's key reached the hub: %v", boardBound)
	}

	res := decode[map[string]string](t, c.mustCall(hubproto.OpDocsDeliver, map[string]any{"tabId": "tab-1", "message": "💬 Комментарий"}))
	if res["mode"] != "typed" {
		t.Fatalf("deliver: %v", res)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.paths) != 1 || !strings.HasSuffix(rt.paths[0], "/api/agent/docs/deliver") || rt.body["tabId"] != "tab-1" || rt.body["message"] != "💬 Комментарий" {
		t.Fatalf("runtime got %v %v", rt.paths, rt.body)
	}
	if _, has := rt.body["op"]; has {
		t.Fatalf("the op went to the runtime: %v", rt.body)
	}
}

// An observe link keeps documents with the cluster unless the operator opts in on the machine.
func TestDocsOnAnObserveLinkNeedTheOptIn(t *testing.T) {
	t.Setenv(docsOptIn, "")
	o := startHubAgent(t, rigOptions{observe: true})
	c := o.connect()
	if _, e := c.call(hubproto.OpDocsDeliver, map[string]any{"message": "x"}); !strings.HasPrefix(e, "policy_denied: ") {
		t.Fatalf("deliver without the opt-in: %q", e)
	}
	if o.agent.hubKeepsDocs() {
		t.Fatal("the hub keeps documents without the opt-in")
	}

	t.Setenv(docsOptIn, "on")
	rt := &docRuntime{}
	o2 := startHubAgent(t, rigOptions{observe: true, runtimeRoute: rt.route})
	c2 := o2.connect()
	if !o2.agent.hubKeepsDocs() {
		t.Fatal("the opt-in did not hand documents to the hub")
	}
	if res := decode[map[string]string](t, c2.mustCall(hubproto.OpDocsDeliver, map[string]any{"message": "x"})); res["mode"] != "typed" {
		t.Fatalf("deliver: %v", res)
	}
}

// When the hub keeps the documents and is not linked, the routes refuse rather than send to the cluster.
func TestDocsRefuseWithoutTheHub(t *testing.T) {
	t.Setenv(docsOptIn, "on")
	o := startHubAgent(t, rigOptions{observe: true, before: func(h *fakeHub) { h.reject.Store(http.StatusServiceUnavailable) }})
	// The agent writes its token while it starts; read it from agent.json and ask over HTTP, as a CLI does.
	var local struct {
		Token string `json:"token"`
	}
	waitFor(t, func() bool {
		raw, err := os.ReadFile(filepath.Join(o.agent.cfg.DataDir, "agent.json"))
		return err == nil && json.Unmarshal(raw, &local) == nil && local.Token != ""
	})
	url := fmt.Sprintf("http://127.0.0.1:%d/docs/publish", o.agent.cfg.Port)
	var res *http.Response
	waitFor(t, func() bool {
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"title":"x","content":"x","format":"md"}`))
		req.Header.Set(o.agent.cfg.LocalTokenHeader, local.Token)
		var err error
		res, err = http.DefaultClient.Do(req)
		return err == nil
	})
	defer res.Body.Close()
	if res.StatusCode != 503 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("publish with no hub link: %d %s", res.StatusCode, body)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
