package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// HRN-852: a view opens a terminal only with terminal.pty, in a chat the app is on in; the shell answers over the
// socket, ends with its code, and the opening and the end are journaled (what is typed is not).
func TestAppTerminalRoute(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	conv.Cwd = t.TempDir()
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"terminal.pty"}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	code, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &sess) != nil {
		t.Fatalf("session: %d %s", code, body)
	}
	// terminal.read is another right: the app does not hold it.
	if code, body := call("POST", platformAPI+"/apps/terminal/read", `{"session":"`+sess.Session+`"}`, page); code < 400 || !strings.Contains(body, "terminal.read") {
		t.Errorf("terminal.read without the right: %d %s", code, body)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rt := range rigRoutes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	addr := "ws" + strings.TrimPrefix(srv.URL, "http") + platformAPI + "/apps/terminal?session=" + sess.Session + "&cols=80&rows=24"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dial := func(url string, session bool) (*websocket.Conn, *http.Response, error) {
		h := http.Header{}
		if session {
			h.Set("X-Test-Session", "yes")
		}
		return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	}
	if _, res, err := dial(addr, false); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no page session: %v %v", res, err)
	}
	if _, res, err := dial(addr+"&cwd=../x", true); err == nil || res == nil || res.StatusCode < 400 {
		t.Fatalf("cwd out of the folder: %v %v", res, err)
	}
	if _, err := exec.LookPath("bwrap"); err != nil || appBoundary().Ready(t.TempDir()) != nil {
		t.Skip("no wall on this machine")
	}
	ws, _, err := dial(addr, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"cols":100,"rows":30}`)); err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(ctx, websocket.MessageBinary, []byte("stty size; echo $((6*7)); exit 3\n")); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	exit := -100
	for exit == -100 {
		kind, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (%q)", err, out.String())
		}
		if kind == websocket.MessageBinary {
			out.Write(data)
			continue
		}
		var end struct{ Exit int }
		if json.Unmarshal(data, &end) == nil {
			exit = end.Exit
		}
	}
	if exit != 3 || !strings.Contains(out.String(), "30 100") || !strings.Contains(out.String(), "42") {
		t.Fatalf("the shell: exit %d, %q", exit, out.String())
	}
	// The end is journaled as the socket closes, just after its last frame.
	var list []apps.JournalEntry
	var opened, closed bool
	for deadline := time.Now().Add(3 * time.Second); !closed && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		list, err = apps.Open(decRigDir).ReadJournal("acme.demo", 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			closed = closed || (e.What == "terminal.close" && e.Detail == "3")
		}
	}
	for _, e := range list {
		opened = opened || (e.What == "terminal.open" && e.OK)
		if strings.Contains(e.Detail, "stty") {
			t.Errorf("what is typed is journaled: %+v", e)
		}
	}
	if !opened || !closed {
		t.Errorf("the journal: %+v", list)
	}
	conv.Apps = nil
	if _, res, err := dial(addr, true); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("the app off in the chat: %v %v", res, err)
	}
}
