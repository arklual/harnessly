package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/secrets"
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
	ctx, cancel := context.WithTimeout(t.Context(), 1200*time.Millisecond)
	defer cancel()
	out := set.Call(ctx, "browser_tabs", jsvalue.NewObject())
	if time.Since(started) < time.Second {
		t.Fatal("startup still uses the old short deadline")
	}
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

func TestSnapshotContinuationAfterFrameChanges(t *testing.T) {
	st := &tabState{refs: map[string]nodeRef{}, epochs: map[string]uint64{}, snapshot: []snapshotLine{{text: "first page"}}}
	for n := 0; n < 1000; n++ {
		frame := fmt.Sprintf("changed-frame-%d", n)
		st.epochs[frame] = 1
		st.snapshot = append(st.snapshot, snapshotLine{text: "obsolete", ref: nodeRef{frame: frame}})
	}
	st.snapshot = append(st.snapshot, snapshotLine{text: `[last] button "Main document"`, id: "last", ref: nodeRef{frame: "main"}})
	offset := 1
	for pages := 0; pages < 20; pages++ {
		out, err := (Tools{}).snapshot(t.Context(), nil, st, arguments{Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > resultLimit || strings.Contains(out, "obsolete") || strings.Contains(out, "truncated") {
			t.Fatal("continuation leaked obsolete rows or exceeded the page limit")
		}
		if strings.Contains(out, "next_offset: null") {
			if _, ok := st.refs["last"]; !ok {
				t.Fatal("main document ID was lost")
			}
			return
		}
		match := regexp.MustCompile(`next_offset: (\d+)`).FindStringSubmatch(out)
		if len(match) != 2 {
			t.Fatal("missing continuation")
		}
		next, err := strconv.Atoi(match[1])
		if err != nil || next <= offset {
			t.Fatal("pagination did not advance")
		}
		offset = next
	}
	t.Fatal("pagination did not terminate")
}

func TestPreparingDoesNotWaitForDownload(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("RUNTIME_BROWSER_CHROME", "")
	t.Setenv("RUNTIME_BROWSER_CHROME_URL", server.URL)
	t.Setenv("RUNTIME_BROWSER_DOWNLOAD", "")
	manager := agentbrowser.New(agentbrowser.Config{DataDir: t.TempDir()})
	defer manager.Close()
	manager.Ready(t.Context())
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not start")
	}
	set := Tools{S: &Service{Manager: manager}, Caller: "chat"}
	start := time.Now()
	out := set.Call(t.Context(), "browser_tabs", jsvalue.NewObject())
	if time.Since(start) > time.Second || !strings.Contains(string(jsvalue.Stringify(out, "")), "Preparing browser") {
		t.Fatalf("download blocked the tool: %s", jsvalue.Stringify(out, ""))
	}
}

func TestRecordingOverlappingProfileValues(t *testing.T) {
	short := strings.Repeat("q7", 6)
	long := short + strings.Repeat("r2", 8)
	var profile []string
	mask := func(s string) string { return secrets.Mask(s, profile) }
	register := func(s string) { profile = append(profile, s) }
	first := &tabState{mask: mask, register: register}
	second := &tabState{mask: mask, register: register}
	first.remember(short)
	second.remember(long)
	for _, st := range []*tabState{first, second} {
		raw, _ := json.Marshal(map[string]any{"type": "log", "args": []any{map[string]any{"value": long}}})
		st.event(agentbrowser.AgentEvent{Method: "Runtime.consoleAPICalled", Params: raw})
		if st.redact(long) != "[redacted]" || strings.Contains(st.console[0], strings.Repeat("r2", 8)) {
			t.Fatal("tab recording exposed a longer sibling value's suffix")
		}
	}
	set := Tools{Caller: "chat", S: &Service{tabs: map[string]*tabState{"profile/chat/one": first, "profile/chat/two": second}}}
	if set.redact(long) != "[redacted]" {
		t.Fatal("response masking exposed an overlapping tab value")
	}
}
