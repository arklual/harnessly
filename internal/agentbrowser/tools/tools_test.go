package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func TestDisabledUnknownAndStarting(t *testing.T) {
	off := agentbrowser.New(agentbrowser.Config{Off: true})
	defer off.Close()
	set := Tools{S: &Service{Manager: off}}
	server := mcpserver.Server{Sets: []mcpserver.Toolset{set}}
	_, b := server.Handle(t.Context(), jsvalue.NewObject("jsonrpc", "2.0", "id", float64(1), "method", "tools/list"))
	if strings.Contains(string(b), "browser_") {
		t.Fatal(string(b))
	}
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	manager := agentbrowser.New(agentbrowser.Config{DataDir: t.TempDir(), CDP: slow.URL})
	defer manager.Close()
	set.S = &Service{Manager: manager}
	if out := set.Call(t.Context(), "browser_tabs", jsvalue.NewObject()); !strings.Contains(string(jsvalue.Stringify(out, "")), "Cannot identify this chat") {
		t.Fatal(out)
	}
	set.Caller = "chat"
	started := time.Now()
	out := set.Call(t.Context(), "browser_tabs", jsvalue.NewObject())
	if time.Since(started) > 2*time.Second {
		t.Fatal("startup blocked")
	}
	if !strings.Contains(string(jsvalue.Stringify(out, "")), "retry in 2 seconds") {
		t.Fatal(out)
	}
}
func TestURLPolicy(t *testing.T) {
	for _, u := range []string{"http://localhost:4400", "https://example.org/path", "about:blank"} {
		if err := allowedURL(u); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []string{"file:///etc/passwd", "chrome://version", "chrome-extension://id", "view-source:https://example.org", "javascript:1", "data:text/plain,x", "devtools://x", "https://user:pass@example.org", "", "about:blank#x"} {
		if allowedURL(u) == nil {
			t.Errorf("accepted %s", u)
		}
	}
}
func TestFileContainment(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "inside"), []byte("yes"), 0600))
	must(t, os.WriteFile(filepath.Join(out, "outside"), []byte("no"), 0600))
	must(t, os.Symlink(out, filepath.Join(root, "escape")))
	if _, err := insideFile(root, "inside"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(out, "outside"), "escape/outside"} {
		if _, err := insideFile(root, p); err == nil {
			t.Fatal("accepted outside file")
		}
	}
	if err := saveImage(root, "escape/shot.jpg", []byte("x")); err == nil {
		t.Fatal("saved outside folder")
	}
	must(t, saveImage(root, "shot.jpg", []byte("x")))
	must(t, os.Symlink(filepath.Join(out, "outside"), filepath.Join(root, "link.jpg")))
	if saveImage(root, "link.jpg", []byte("x")) == nil {
		t.Fatal("overwrote symlink")
	}
}
func TestRecordingBoundsAndRedaction(t *testing.T) {
	st := &tabState{requests: map[string]string{}, pending: map[string]bool{}}
	st.remember("private-value")
	for n := 0; n < 600; n++ {
		raw, _ := json.Marshal(map[string]any{"type": "log", "args": []any{map[string]any{"value": "private-value"}}})
		st.event(agentbrowser.AgentEvent{Method: "Runtime.consoleAPICalled", Params: raw})
	}
	if len(st.console) != 500 {
		t.Fatalf("ring length %d", len(st.console))
	}
	if strings.Contains(strings.Join(st.console, ""), "private-value") {
		t.Fatal("console value leaked")
	}
	raw := json.RawMessage(`{"requestId":"r","request":{"url":"http://localhost","headers":{"aUtHoRiZaTiOn":"secret","Cookie":"secret","SET-COOKIE":"secret"},"postData":"secret"}}`)
	st.event(agentbrowser.AgentEvent{Method: "Network.requestWillBeSent", Params: raw})
	if strings.Contains(st.network[0], "secret") {
		t.Fatal("network secret leaked")
	}
}
