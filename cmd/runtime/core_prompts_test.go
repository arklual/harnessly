package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/terminal"
	"harnessly/internal/tmux"
)

// The core with its prompt routes on a private tmux server: a prompt posted to a tab is typed into its pane, and the
// routes refuse callers as the previous runtime did.
func TestCorePromptRoutes(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, home := t.TempDir(), t.TempDir()
	socket := "hs-test-core-prompts-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	_ = os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"A","directories":["`+home+`"]}],"sidebarCollapsed":false,"sidebarWidth":240}`), 0o600)
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), MetaKey: "meta",
		Prompts: promptConfigFromEnv(dir)})
	// The core slice is on for this machine.
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["core"]}`), 0o600)
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	person := func(r *http.Request) bool { return r.Header.Get("Cookie") == "session-token=ok" }
	token := func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" }
	refusal := func(r *http.Request) (int, any, http.Header, bool) {
		if person(r) || token(r) {
			return 0, nil, nil, false
		}
		return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
	}
	f.Add(c.routes(func(r *http.Request) bool { return person(r) || token(r) }, token)...)
	f.Add(c.promptRoutes(person, refusal)...)
	srv := httptest.NewServer(f)
	defer srv.Close()

	type answer struct {
		status int
		header http.Header
		body   string
	}
	call := func(method, path, body string, headers ...string) answer {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return answer{res.StatusCode, res.Header, string(raw)}
	}
	asPerson := []string{"Cookie", "session-token=ok", "Content-Type", "application/json"}
	asToken := []string{"x-cli-token", "ok", "Content-Type", "application/json"}

	c.start()
	defer c.stop()
	var tabs struct {
		Tabs []struct {
			TabID       string `json:"tabId"`
			TmuxSession string `json:"tmuxSession"`
		} `json:"tabs"`
	}
	if a := call("GET", "/api/internal/core/tabs", "", "x-cli-token", "ok"); a.status != 200 || json.Unmarshal([]byte(a.body), &tabs) != nil || len(tabs.Tabs) != 1 {
		t.Fatalf("tabs: %+v", a)
	}
	tabID, session := tabs.Tabs[0].TabID, tabs.Tabs[0].TmuxSession

	// The pane runs cat, which shows what is typed.
	if err := tm.SendKeys(session, "exec cat"); err != nil {
		t.Fatal(err)
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				s, _ := tm.Capture(session)
				t.Fatalf("timed out waiting for %s; screen:\n%s", what, s)
			}
		}
	}
	waitFor("cat", func() bool { return tm.PaneCurrentCommand(session) == "cat" })

	a := call("POST", platformAPI+"/prompt-text", `{"tabId":"`+tabID+`","text":"hello from the route","messageId":"m-1"}`, asPerson...)
	if a.status != 200 || !strings.Contains(a.body, `"via":"paste"`) || a.header.Get("ETag") == "" {
		t.Fatalf("prompt-text: %+v", a)
	}
	waitFor("the typed prompt", func() bool { s, _ := tm.Capture(session); return strings.Contains(s, "hello from the route") })
	if a := call("POST", "/agents"+platformAPI+"/prompt-text", `{"tabId":"`+tabID+`","text":"hello from the route","messageId":"m-1"}`, asPerson...); a.status != 200 ||
		!strings.Contains(a.body, `"via":"duplicate"`) {
		t.Fatalf("the same message id again: %+v", a)
	}

	// Refusals, in the previous runtime's order and shapes.
	if a := call("POST", platformAPI+"/prompt-text", `{"tabId":"x","text":"y"}`, "Content-Type", "application/json"); a.status != 401 || a.body != `{"error":"Unauthorized"}` {
		t.Errorf("no session, no token: %+v", a)
	}
	if a := call("POST", platformAPI+"/prompt-text", `{"tabId":"x","text":"y"}`, asToken...); a.status != 403 || a.body != "" {
		t.Errorf("a token is no person: %+v", a)
	}
	if a := call("GET", platformAPI+"/prompt-queue", "", asPerson...); a.status != 405 || a.body != "" || a.header.Get("Allow") != "" {
		t.Errorf("composer 405: %+v", a)
	}
	if a := call("POST", platformAPI+"/switch-model", `{}`, asToken...); a.status != 403 || a.body != `{"error":"Forbidden"}` || a.header.Get("Cache-Control") != "" {
		t.Errorf("switch-model 403: %+v", a)
	}
	if a := call("GET", platformAPI+"/switch-model", "", asPerson...); a.status != 405 || a.header.Get("Allow") != "POST" || a.header.Get("Cache-Control") != "no-store" {
		t.Errorf("switch-model 405: %+v", a)
	}
	if a := call("POST", platformAPI+"/commands/run", `{}`, "Cookie", "session-token=ok", "Content-Type", "text/plain"); a.status != 415 || a.body != `{"error":"Нужен JSON"}` {
		t.Errorf("commands/run 415: %+v", a)
	}
	if a := call("POST", platformAPI+"/prompt-text", `{"tabId":`, asPerson...); a.status != 400 || a.body != "Invalid JSON" {
		t.Errorf("broken JSON: %+v", a)
	}
	big := `{"tabId":"` + strings.Repeat("x", promptBodyLimit) + `"}`
	if a := call("POST", platformAPI+"/prompt-text", big, asPerson...); a.status != 413 || a.body != "Body exceeded 1mb limit" {
		t.Errorf("too big: %d %.60s", a.status, a.body)
	}

	// The raw tmux routes take the cli-token too, and only the runtime's own sessions.
	if a := call("GET", "/api/tmux/capture?session="+session, "", asToken...); a.status != 200 || !strings.Contains(a.body, "hello from the route") {
		t.Errorf("capture: %+v", a)
	}
	if a := call("GET", "/api/tmux/capture?session=someone-else", "", asToken...); a.status != 404 {
		t.Errorf("a foreign session: %+v", a)
	}
	if a := call("POST", "/api/tmux/capture", "{}", asToken...); a.status != 405 || a.header.Get("Allow") != "GET" {
		t.Errorf("capture 405: %+v", a)
	}

	// A queued message for this terminal tab stays queued (no agent to take it) and the pages hear of it.
	if a := call("POST", platformAPI+"/prompt-queue", `{"tabId":"`+tabID+`","action":"add","text":"later"}`, asPerson...); a.status != 200 || !strings.Contains(a.body, `"delivery":"queued"`) {
		t.Fatalf("queue add: %+v", a)
	}
	if q := c.promptService().Queue().List(tabID); len(q) != 1 {
		t.Fatalf("queue %v", q)
	}
	// Stop: Escape once, and the tab settles.
	if a := call("POST", platformAPI+"/interrupt", `{"tabId":"`+tabID+`"}`, asPerson...); a.status != 204 || a.body != "" {
		t.Errorf("interrupt: %+v", a)
	}
	// Closing the tab drops its queue.
	c.manager().RemoveTab(tabID)
	if q := c.promptService().Queue().All(); len(q) != 0 {
		t.Errorf("a closed tab keeps its queue: %v", q)
	}

	c.stop()
	if a := call("POST", platformAPI+"/prompt-text", `{"tabId":"x","text":"y"}`, asPerson...); a.status != 503 {
		t.Errorf("a stopped core: %+v", a)
	}
}

func TestIsUploadImage(t *testing.T) {
	for p, want := range map[string]bool{
		"/data/uploads/ws/tab/a.png":  true,
		"/data/uploads/ws/tab/a.JPEG": true,
		"/data/uploads/ws/tab/a.pdf":  false,
		"/data/uploads":               false,
		"/data/uploads/../etc/x.png":  false,
		"/data/uploadsx/a.png":        false,
		"/data/uploads/a.png\x00.txt": false,
	} {
		if got := isUploadImage("/data/uploads", p); got != want {
			t.Errorf("%q: %v", p, got)
		}
	}
}
