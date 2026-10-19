package main

import (
	"context"
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

	"harnessly/internal/agents"
	"harnessly/internal/gate"
	"harnessly/internal/jsvalue"
	"harnessly/internal/terminal"
	"harnessly/internal/tmux"
)

func TestTimelineSocketNeedsTheSliceAndTheCore(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, home := t.TempDir(), t.TempDir()
	socket := "hs-test-timeline-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tmuxNames = tm
	_ = os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"A","directories":["`+home+`"]}],"sidebarCollapsed":false,"sidebarWidth":240}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["timeline"]}`), 0o600)
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	defer old.Close()
	c := newCore(coreConfig{DataDir: dir, Home: home, Tmux: tm, Term: terminal.New(terminal.Config{Tmux: tm, Home: home}), MetaKey: "meta"})
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	c.sliceOn = f.SliceOn
	f.Add(c.timelineSocketRoutes(func(r *http.Request) bool { return r.Header.Get("x-test-session") == "ok" })...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/agents/api/timeline?session="

	dial := func(session string, header bool) (*websocket.Conn, *http.Response, error) {
		h := http.Header{}
		if header {
			h.Set("x-test-session", "ok")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return websocket.Dial(ctx, url+session, &websocket.DialOptions{HTTPHeader: h})
	}
	// The core is off: the old runtime gets the socket.
	if _, res, err := dial("x", true); err == nil || res == nil || res.StatusCode != http.StatusTeapot {
		t.Fatalf("an off core still served the socket: %v %v", err, res)
	}
	c.start()
	defer c.stop()
	if c.timelineOn() != true {
		t.Fatal("the slice is on and the core runs")
	}
	if _, res, err := dial("x", false); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a socket without a page session: %v %v", err, res)
	}
	var session string
	for _, wt := range c.store.WorkspaceTabs() {
		session = wt.Tab.String("sessionName")
	}
	if session == "" || !tm.HasSession(session) {
		t.Fatalf("no tab session: %q", session)
	}
	ws, _, err := dial(session, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := jsvalue.Parse(data)
	if m, ok := v.(*jsvalue.Object); !ok || m.String("type") != "timeline:init" {
		t.Fatalf("first message %s", data)
	}
	// A Codex hook's session reaches the Go timeline, not the old runtime.
	codexSink{c}.CodexSession(session, agents.SessionInfo{Status: agents.Running, SessionID: "x"}, false)
	// The core stopping closes the Go sockets: the cluster's proxy dials again, to the old runtime.
	c.stop()
	if _, _, err := ws.Read(ctx); err == nil {
		t.Fatal("the socket outlived the core")
	}
}
