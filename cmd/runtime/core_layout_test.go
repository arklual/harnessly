package main

import (
	"context"
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

	"github.com/coder/websocket"

	"harnessly/internal/gate"
	"harnessly/internal/jsvalue"
	"harnessly/internal/terminal"
	"harnessly/internal/tmux"
)

// The layout routes inside a started core: the core's own store, keeper, sync socket and pane intents.
func TestLayoutRoutesInsideTheCore(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, home := t.TempDir(), t.TempDir()
	socket := "hs-test-corelayout-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, ConfigFile: "/dev/null", Home: home,
		Env: []string{"HOME=" + home, "SHELL=/bin/sh"}})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	_ = os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"A","directories":["`+home+`"]}],"sidebarCollapsed":false,"sidebarWidth":240}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"owned":false,"goSlices":["core"]}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600)
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), MetaKey: "meta"})
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	token := func(r *http.Request) bool { return r.Header.Get("x-test-token") == "tok" }
	d := deps{Home: home, DataDir: dir, TokenHeaders: []string{"x-test-token"}, SessionOK: func(*http.Request) bool { return false }}
	f.Add(c.routes(token, token)...)
	f.Add(c.layoutRoutes(f, d)...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	call := func(method, path, body string) (int, *jsvalue.Object) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("x-test-token", "tok")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		v, _ := jsvalue.Parse(raw)
		o, _ := v.(*jsvalue.Object)
		return res.StatusCode, o
	}
	c.start()
	defer c.stop()
	m := c.manager()

	code, layout := call("GET", "/api/layout?workspace=ws-a", "")
	if code != 200 {
		t.Fatalf("%d", code)
	}
	pane := layout.Object("root").String("id")
	code, tab := call("POST", "/api/layout/pane/"+pane+"/tabs?workspace=ws-a", `{"panelType":"claude-code","name":"Agent"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, tab)
	}
	info, ok := m.Tab(tab.String("id"))
	if !ok || info.CliState != "inactive" || info.PanelType != "claude-code" || info.TmuxSession != tab.String("sessionName") {
		t.Fatalf("the keeper did not learn the tab: %+v %v", info, ok)
	}
	if !tm.HasSession(tab.String("sessionName")) {
		t.Fatal("no session")
	}
	// The written layout is the core store's, the one the keeper reads.
	if l := c.store.Layout("ws-a"); len(l.Object("root").Array("tabs")) != 2 {
		t.Fatalf("%s", jsvalue.Stringify(l, ""))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/status", &websocket.DialOptions{HTTPHeader: http.Header{"x-test-token": {"tok"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	for deadline := time.Now().Add(5 * time.Second); c.statusHub.Count() == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	updates := make(chan string, 1)
	go func() {
		for {
			_, msg, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if strings.Contains(string(msg), `"pane-intents:update"`) {
				updates <- string(msg)
				return
			}
		}
	}()

	// The agent's pane command finds its tab through the keeper and is kept with the conversation.
	code, answer := call("POST", "/api/cli/panel", `{"session":"`+tab.String("sessionName")+`","command":{"kind":"open","tab":"files","target":"/w/a.md"}}`)
	if code != 200 {
		t.Fatalf("%d %s", code, jsvalue.Stringify(answer, ""))
	}
	snap, _ := json.Marshal(statusSource{c}.Intents())
	if !strings.Contains(string(snap), tab.String("id")) || !strings.Contains(string(snap), `"target":"/w/a.md"`) {
		t.Fatalf("the status socket does not see the intent: %s", snap)
	}
	select {
	case u := <-updates:
		if !strings.HasPrefix(u, `{"type":"pane-intents:update","tabId":"`+tab.String("id")+`","intents":[{"id":"`) {
			t.Fatalf("%s", u)
		}
	case <-ctx.Done():
		t.Fatal("no pane-intents:update on the status socket")
	}
	if code, _ := call("POST", "/api/cli/panel", `{"session":"hs-none","command":{"kind":"show"}}`); code != 404 {
		t.Fatalf("%d", code)
	}

	code, status := call("GET", "/api/cli/tabs/"+tab.String("id")+"/status?workspaceId=ws-a", "")
	if code != 200 || status.String("tabId") != tab.String("id") {
		t.Fatalf("%d %s", code, jsvalue.Stringify(status, ""))
	}
	if code, _ := call("DELETE", "/api/layout/pane/"+pane+"/tabs/"+tab.String("id")+"?workspace=ws-a", ""); code != 204 {
		t.Fatalf("%d", code)
	}
	if _, ok := m.Tab(tab.String("id")); ok {
		t.Fatal("the keeper still has the closed tab")
	}
	if code, _ := call("POST", "/api/tmux/reset", ""); code != 200 {
		t.Fatalf("reset %d", code)
	}
}
