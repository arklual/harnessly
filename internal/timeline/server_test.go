package timeline

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"harnessly/internal/jsvalue"
)

// What the golden scenario leaves out: the timers of ten seconds and more, and the socket cap.

func testWorld(t *testing.T) (*world, string) {
	home := t.TempDir()
	for _, d := range []string{".claude/sessions", ".claude/projects/-work-acme", ".codex/sessions"} {
		must(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	return &world{panes: map[string]*jsvalue.Object{}, procs: map[int]*jsvalue.Object{}, busy: map[string]bool{}, recover: map[string]*jsvalue.Object{}}, home
}

func dialTimeline(t *testing.T, srv *httptest.Server, query string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/timeline?"+query, nil)
	must(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

// readUntil reads messages until one of type typ (or a close) arrives.
func readUntil(t *testing.T, c *websocket.Conn, typ string, within time.Duration) *jsvalue.Object {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if typ == "close" {
				return jsvalue.NewObject("close", float64(websocket.CloseStatus(err)))
			}
			t.Fatalf("waiting for %s: %v", typ, err)
		}
		v, _ := jsvalue.Parse(data)
		if m, ok := v.(*jsvalue.Object); ok && m.String("type") == typ {
			return m
		}
	}
}

func TestAgentGoneFoundByThePIDPoll(t *testing.T) {
	w, home := testWorld(t)
	s := newTestServer(t, w, home)
	s.d.Timing.PIDPoll = 200 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(s.ServeHTTP))
	defer srv.Close()
	defer s.Shutdown()
	id := "11111111-1111-4111-8111-111111111111"
	transcriptPath := filepath.Join(home, ".claude/projects/-work-acme", id+".jsonl")
	must(t, os.WriteFile(transcriptPath, []byte(`{"type":"user","timestamp":"2026-10-18T10:00:00.000Z","message":{"role":"user","content":"hi"}}`+"\n"), 0o644))
	w.panes["hs-w-p-t"] = jsvalue.NewObject("pid", 100.0, "cwd", "/work/acme", "command", "claude")
	w.procs[100] = jsvalue.NewObject("children", []any{101.0})
	w.procs[101] = jsvalue.NewObject("children", []any{}, "args", "claude", "cwd", "/work/acme")
	c := dialTimeline(t, srv, "session=hs-w-p-t")
	readUntil(t, c, "timeline:init", 3*time.Second)
	// The pid file appears: the sessions folder watch finds the process and remembers it.
	must(t, os.WriteFile(filepath.Join(home, ".claude/sessions/101.json"), []byte(`{"pid":101,"sessionId":"`+id+`","cwd":"/work/acme"}`), 0o644))
	m := readUntil(t, c, "timeline:session-changed", 3*time.Second)
	if m.String("reason") != "new-session-started" {
		t.Fatalf("%s", jsvalue.Stringify(m, ""))
	}
	// The process ends without a word in the folder: only the poll notices.
	w.mu.Lock()
	delete(w.procs, 101)
	w.procs[100] = jsvalue.NewObject("children", []any{})
	w.mu.Unlock()
	m = readUntil(t, c, "timeline:session-changed", 3*time.Second)
	if m.String("reason") != "agent-exited" || m.String("newSessionId") != id {
		t.Fatalf("%s", jsvalue.Stringify(m, ""))
	}
}

func TestHeartbeatAndConnectionCap(t *testing.T) {
	w, home := testWorld(t)
	s := newTestServer(t, w, home)
	s.d.Timing.Heartbeat = 150 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(s.ServeHTTP))
	defer srv.Close()
	defer s.Shutdown()
	w.panes["hs-w-p-t"] = jsvalue.NewObject("pid", 100.0, "cwd", "/work/acme", "command", "zsh")
	w.procs[100] = jsvalue.NewObject("children", []any{})
	var first *websocket.Conn
	for i := 0; i < maxConnections; i++ {
		c := dialTimeline(t, srv, "session=hs-w-p-t")
		readUntil(t, c, "timeline:init", 3*time.Second)
		if first == nil {
			first = c
		}
	}
	readUntil(t, first, "timeline:heartbeat", 3*time.Second)
	over := dialTimeline(t, srv, "session=hs-w-p-t")
	if code, _ := readUntil(t, over, "close", 3*time.Second).Number("close"); code != 1013 {
		t.Fatalf("the 33rd socket closed with %v", code)
	}
}
