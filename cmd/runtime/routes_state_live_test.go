package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// The routes of the state slice that read the running machine, through the whole gate: tmux, the core's agents and
// layouts, the release's files and the machine agent's notifications.
func TestStateSliceOnTheWholeGate(t *testing.T) {
	g := newLiveGate(t)
	do := func(method, path, body string, header map[string]string) (int, http.Header, string) {
		req, _ := http.NewRequest(method, g.url+path, strings.NewReader(body))
		for k, v := range header {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header, string(b)
	}
	page := map[string]string{"Cookie": g.cookie, "Content-Type": "application/json"}
	socket := func(path string, header map[string]string) string {
		conn, err := net.Dial("tcp", strings.TrimPrefix(g.url, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		req, _ := http.NewRequest("GET", g.url+path, nil)
		for k, v := range header {
			req.Header.Set(k, v)
		}
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		_ = req.Write(conn)
		line, _ := bufio.NewReader(conn).ReadString('\n')
		return strings.TrimSpace(line)
	}

	// tmux: the sessions, one session's pane.
	if out, err := exec.Command("tmux", "-L", g.socket, "-f", "/dev/null", "new-session", "-d", "-s", "hs-info", "-x", "90", "-y", "30", "sleep 3600").CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v %s", err, out)
	}
	code, _, body := do("GET", "/api/system/tmux-sessions", "", page)
	var count struct{ Count int }
	if code != 200 || json.Unmarshal([]byte(body), &count) != nil || count.Count < 1 {
		t.Errorf("tmux-sessions: %d %s", code, body)
	}
	code, _, body = do("GET", "/api/tmux/info?session=hs-info", "", page)
	if code != 200 || !strings.HasPrefix(body, `{"cwd":`) || !strings.Contains(body, `"width":90,"height":30,"sessionCreated":`) ||
		!strings.HasSuffix(body, `"sessionName":"hs-info"}`) {
		t.Errorf("tmux info: %d %s", code, body)
	}
	if code, _, body := do("GET", "/api/tmux/info?session=hs-none", "", page); code != 404 || body != `{"error":"Session not found"}` {
		t.Errorf("tmux info of no session: %d %s", code, body)
	}
	// A terminal socket that names no session gets a new one.
	before := count.Count
	if line := socket("/api/terminal", map[string]string{"Cookie": g.cookie}); line != "HTTP/1.1 101 Switching Protocols" {
		t.Errorf("terminal without a session: %s", line)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		_, _, body = do("GET", "/api/system/tmux-sessions", "", page)
		if json.Unmarshal([]byte(body), &count) == nil && count.Count == before+1 {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("no session was made for the terminal: %s (before %d)", body, before)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The core's background feed and the desktop app's browser tabs.
	if code, h, body := do("GET", platformAPI+"/background", "", page); code != 200 || !strings.HasPrefix(body, `{"tabs":`) || h.Get("Cache-Control") != "no-store" {
		t.Errorf("background: %d %s", code, body)
	}
	if code, _, _ := do("GET", platformAPI+"/background", "", map[string]string{"x-cli-token": "nope"}); code != 401 {
		t.Errorf("background without a session: %d", code)
	}
	// An empty machine's first page load makes its first workspace in the layout store.
	code, _, body = do("GET", platformAPI+"/bootstrap", "", page)
	var boot struct {
		InitialWorkspace struct {
			Workspaces []struct{ ID, Name string } `json:"workspaces"`
		} `json:"initialWorkspace"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &boot) != nil || len(boot.InitialWorkspace.Workspaces) != 1 {
		t.Fatalf("bootstrap: %d %s", code, body)
	}
	code, _, body = do("GET", "/api/workspace", "", page)
	var list struct {
		Workspaces []struct{ ID string } `json:"workspaces"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &list) != nil || len(list.Workspaces) == 0 {
		t.Fatalf("workspaces: %d %s", code, body)
	}
	ws := list.Workspaces[0].ID
	token := map[string]string{"x-cli-token": cliTokenOf(t, g.data)}
	for path, want := range map[string]string{
		"/api/cli/tabs/nope/browser/url?workspaceId=" + ws: `404 {"error":"Tab not found"}`,
		"/api/cli/tabs/nope/browser/url":                   `400 {"error":"workspaceId is required"}`,
		"/api/cli/tabs/nope/browser/url?workspaceId=ws-x":  `404 {"error":"Tab not found"}`,
	} {
		code, _, body := do("GET", path, "", token)
		if got := strconvItoa(code) + " " + body; got != want {
			t.Errorf("%s: %s, want %s", path, got, want)
		}
	}
	if code, _, body := do("GET", "/api/cli/tabs/nope/browser/url?workspaceId="+ws, "", nil); code != 403 || body != `{"error":"Forbidden"}` {
		t.Errorf("browser tab without the token: %d %s", code, body)
	}
	if code, h, _ := do("GET", "/api/cli/tabs/nope/browser/eval", "", token); code != 405 || h.Get("Allow") != "POST" {
		t.Errorf("browser eval by GET: %d", code)
	}

	// The release's files: the CLI's guide and the app manifest.
	if code, h, body := do("GET", "/api/cli/api-guide", "", token); code != 200 || body != "# API\n" || h.Get("Content-Type") != "text/markdown; charset=utf-8" || h.Get("ETag") == "" {
		t.Errorf("api guide: %d %s %v", code, body, h)
	}
	if code, _, _ := do("GET", "/api/cli/api-guide", "", nil); code != 403 {
		t.Errorf("api guide without the token: %d", code)
	}
	if code, _, body := do("GET", "/api/manifest", "", nil); code != 200 || body != `{"name":"Acme"}` {
		t.Errorf("manifest: %d %s", code, body)
	}

	// The machine agent's document notification: only from the agent, only to the document reader.
	agentCookie := map[string]string{"Cookie": "session-token=" + agent.SignHS256(g.secret, "machine-agent", "", time.Hour), "Content-Type": "application/json"}
	if code, _, body := do("POST", platformAPI+"/docs/notify", `{"url":"/agents/docs/a","title":" A  title ","body":"b"}`, agentCookie); code != 200 || body != `{"sent":0}` {
		t.Errorf("docs notify: %d %s", code, body)
	}
	if code, _, body := do("POST", platformAPI+"/docs/notify", `{"url":"/elsewhere","title":"A"}`, agentCookie); code != 400 || body != `{"error":"Нужны title и url документа"}` {
		t.Errorf("docs notify elsewhere: %d %s", code, body)
	}
	if code, _, body := do("POST", platformAPI+"/docs/notify", `{"url":"/agents/docs/a","title":"A"}`, page); code != 403 || body != `{"error":"Forbidden"}` {
		t.Errorf("docs notify from a page: %d %s", code, body)
	}
	if code, h, _ := do("GET", platformAPI+"/docs/notify", "", agentCookie); code != 405 || h.Get("Allow") != "POST" {
		t.Errorf("docs notify by GET: %d", code)
	}

	// The design library's notification and access snapshot: only from the agent; a notification only into the library.
	if code, _, body := do("POST", platformAPI+"/design/notify", `{"url":"/agents/design/p1","title":"A","body":"b"}`, agentCookie); code != 200 || body != `{"sent":0}` {
		t.Errorf("design notify: %d %s", code, body)
	}
	if code, _, body := do("POST", platformAPI+"/design/notify", `{"url":"/agents/docs/a","title":"A"}`, agentCookie); code != 400 {
		t.Errorf("design notify into the documents: %d %s", code, body)
	}
	for _, url := range []string{"/agents/design/../docs/a", "/agents/design/%2e%2e/x"} {
		if code, _, body := do("POST", platformAPI+"/design/notify", `{"url":"`+url+`","title":"A"}`, agentCookie); code != 400 || !strings.Contains(body, "библиотеки") {
			t.Errorf("design notify %s: %d %s", url, code, body)
		}
	}
	if code, _, body := do("POST", platformAPI+"/design/notify", `{"url":"/agents/design/a..b","title":"A"}`, agentCookie); code != 200 {
		t.Errorf("design notify with dots inside a name: %d %s", code, body)
	}
	if code, _, body := do("POST", platformAPI+"/docs/notify", `{"url":"/agents/docs/a..b","title":"A"}`, agentCookie); code != 200 {
		t.Errorf("docs notify with dots inside a name: %d %s", code, body)
	}
	if code, _, body := do("POST", platformAPI+"/docs/notify", `{"url":"/agents/docs/x/../a","title":"A"}`, agentCookie); code != 400 {
		t.Errorf("docs notify traversal: %d %s", code, body)
	}
	if code, _, body := do("POST", platformAPI+"/design/notify", `{"url":"/agents/design/p1","title":"A"}`, page); code != 403 || body != `{"error":"Forbidden"}` {
		t.Errorf("design notify from a page: %d %s", code, body)
	}
	snap := `{"enforce":true,"at":"2026-10-20T09:00:00Z","projects":{},"team":{}}`
	if code, _, body := do("POST", platformAPI+"/design/access", snap, page); code != 403 {
		t.Errorf("design access from a page: %d %s", code, body)
	}
	if code, _, body := do("POST", platformAPI+"/design/access", snap, agentCookie); code != 200 || body != `{"ok":true}` {
		t.Errorf("design access from the agent: %d %s", code, body)
	}
	future := `{"enforce":true,"at":"` + time.Now().Add(10*time.Minute).UTC().Format(time.RFC3339) + `","projects":{},"team":{}}`
	if code, _, body := do("POST", platformAPI+"/design/access", future, agentCookie); code != 200 {
		t.Errorf("design access from the future: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(g.data, "design-access.json")); err != nil {
		t.Errorf("snapshot not kept: %v", err)
	}

	// The settings the page reads, through the gate.
	if code, _, body := do("PATCH", "/agents/api/config", `{"acmeLayout":{"left":1000}}`, page); code != 200 || body != `{"ok":true}` {
		t.Errorf("config: %d %s", code, body)
	}
	if code, _, body := do("GET", "/api/config", "", page); code != 200 || !strings.Contains(body, `"acmeLayout":{"left":480,"collapsed":false}`) {
		t.Errorf("config read: %d %s", code, body)
	}
	// The core's own calls to the previous runtime's internal API go to it directly; no request passed through to it.
	for _, h := range g.hits() {
		if !strings.Contains(h, " /api/internal/") {
			t.Errorf("reached the previous runtime: %s", h)
		}
	}
}

// cliTokenOf is the runtime's cli-token in its data folder, made on first use.
func cliTokenOf(t *testing.T, data string) string {
	raw, err := os.ReadFile(filepath.Join(data, "cli-token"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func strconvItoa(n int) string { return strconv.Itoa(n) }
