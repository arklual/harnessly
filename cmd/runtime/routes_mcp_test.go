package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

var mcpTestCLIToken = "cli-" + strings.Repeat("Qx", 12)

// mcpUpstream is a remote MCP server with its own OAuth authorization server on one origin, and a legacy SSE endpoint.
type mcpUpstream struct {
	t    *testing.T
	base string

	mu        sync.Mutex
	access    string // the one access token /mcp takes
	deny      bool   // /mcp takes none
	refresh   string
	issued    int
	refreshes int
	seen      []http.Header // the headers of every /mcp and /messages request
	posted    []string      // the bodies POSTed to /messages
	release   chan struct{} // the second event of a "stream" call waits for it
}

func newMCPUpstream(t *testing.T) *mcpUpstream {
	u := &mcpUpstream{t: t, release: make(chan struct{})}
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	u.base = ts.URL
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"resource": u.base + "/mcp", "authorization_servers": []string{u.base}})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"issuer": u.base, "authorization_endpoint": u.base + "/authorize", "token_endpoint": u.base + "/token",
			"registration_endpoint": u.base + "/register", "code_challenge_methods_supported": []string{"S256"}})
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{"client_id": "client-1"})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		u.mu.Lock()
		defer u.mu.Unlock()
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != "good-code" {
				writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
				return
			}
		case "refresh_token":
			if r.PostForm.Get("refresh_token") != u.refresh {
				writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
				return
			}
			u.refreshes++
		default:
			writeJSON(w, 400, map[string]string{"error": "unsupported_grant_type"})
			return
		}
		u.issued++
		u.access, u.refresh = fmt.Sprintf("at-%d", u.issued), fmt.Sprintf("rt-%d", u.issued)
		writeJSON(w, 200, map[string]any{"access_token": u.access, "token_type": "Bearer", "expires_in": 3600, "refresh_token": u.refresh})
	})
	mux.HandleFunc("POST /mcp", u.serveMCP)
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		u.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: endpoint\ndata: /messages?sid=7\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("POST /messages", func(w http.ResponseWriter, r *http.Request) {
		u.record(r)
		raw, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.posted = append(u.posted, r.URL.RawQuery+" "+string(raw))
		u.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	})
	return u
}

func (u *mcpUpstream) record(r *http.Request) {
	u.mu.Lock()
	u.seen = append(u.seen, r.Header.Clone())
	u.mu.Unlock()
}

func (u *mcpUpstream) lastSeen() http.Header {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.seen[len(u.seen)-1]
}

func (u *mcpUpstream) serveMCP(w http.ResponseWriter, r *http.Request) {
	u.record(r)
	u.mu.Lock()
	ok := !u.deny && u.access != "" && r.Header.Get("Authorization") == "Bearer "+u.access
	u.mu.Unlock()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+u.base+`/.well-known/oauth-protected-resource/mcp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	var result any
	switch msg.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "sess-1")
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "fake", "title": "Fake"}}
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	case "tools/list":
		result = map[string]any{"tools": []map[string]any{{"name": "search", "description": "Search things."}}}
	case "stream":
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Mcp-Session-Id", "sess-1")
		fmt.Fprint(w, "event: message\ndata: {\"n\":1}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-u.release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "event: message\ndata: {\"n\":2}\n\n")
		return
	default:
		u.t.Errorf("unexpected method %q", msg.Method)
		return
	}
	reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(reply)
}

type mcpTestRig struct {
	d      deps
	ms     *mcpconn.Service
	routes []gate.Route
	srv    *httptest.Server
}

func newMCPRig(t *testing.T) *mcpTestRig {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte(mcpTestCLIToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	sf := safeFor(d)
	t.Cleanup(sf.files.Close)
	ms := mcpconn.New(dir, &connSecrets{store: sf.store}, nil)
	rig := &mcpTestRig{d: d, ms: ms, routes: mcpRoutesOf(d, ms, nil)}
	rig.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rt := range rig.routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(rig.srv.Close)
	return rig
}

func (rig *mcpTestRig) call(method, path, body string, h map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://app.test"+path, bytes.NewBufferString(body))
	for k, v := range h {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	for _, rt := range rig.routes {
		if rt.Match(req) {
			rt.Handler.ServeHTTP(w, req)
			return w
		}
	}
	w.Code = http.StatusTeapot
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body.String(), err)
	}
	return out
}

var (
	mcpPage = map[string]string{"X-Test-Session": "yes", "Origin": "http://app.test", "Content-Type": "application/json"}
	mcpCLI  = map[string]string{"x-cli-token": mcpTestCLIToken, "Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
)

// signInRig connects the upstream's OAuth server through the page's routes and signs in; it gives the server's id.
func signInRig(t *testing.T, rig *mcpTestRig, up *mcpUpstream) string {
	t.Helper()
	w := rig.call("POST", "/api/platform/mcp/probe", `{"url":"`+up.base+`/mcp"}`, mcpPage)
	if probe, _ := decode(t, w)["probe"].(map[string]any); w.Code != 200 || probe["auth"] != "oauth" {
		t.Fatalf("probe: %d %s", w.Code, w.Body)
	}
	w = rig.call("POST", "/api/platform/mcp", `{"url":"`+up.base+`/mcp"}`, mcpPage)
	added := decode(t, w)
	srv, _ := added["server"].(map[string]any)
	if start, _ := added["authStart"].(map[string]any); w.Code != 200 || srv["state"] != "signin" || start["auth"] != "oauth" {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	id := srv["id"].(string)
	w = rig.call("POST", "/api/platform/mcp/"+id+"/signin", `{"origin":"http://app.test","returnTo":"/agents/?settings=connections"}`, mcpPage)
	authURL, _ := decode(t, w)["authUrl"].(string)
	au, err := url.Parse(authURL)
	if w.Code != 200 || err != nil {
		t.Fatalf("signin: %d %s", w.Code, w.Body)
	}
	if got := au.Query().Get("redirect_uri"); got != "http://app.test"+platformAPI+"/mcp/callback" {
		t.Fatalf("redirect_uri %q", got)
	}
	state := au.Query().Get("state")
	// The service sends the browser back: a top-level navigation from another site, the Lax cookie with it.
	cb := "/api/platform/mcp/callback?state=" + url.QueryEscape(state) + "&code=good-code"
	if w := rig.call("GET", cb, "", map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusUnauthorized ||
		strings.Contains(w.Body.String(), "postMessage") {
		t.Fatalf("callback without a session: %d %s", w.Code, w.Body)
	}
	w = rig.call("GET", cb, "", map[string]string{"Sec-Fetch-Site": "cross-site", "X-Test-Session": "yes"})
	page := w.Body.String()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("callback: %d %s", w.Code, page)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "script-src 'nonce-") {
		t.Fatalf("CSP %q", csp)
	}
	want := `postMessage({"id":"` + id + `","ok":true,"reason":"","type":"harnessly-mcp"},"http://app.test")`
	if !strings.Contains(page, want) || !strings.Contains(page, `href="http://app.test/agents/?settings=connections"`) {
		t.Fatalf("callback page: %s", page)
	}
	got, err := rig.ms.Get(id)
	if err != nil || got.State != mcpconn.StateOK || len(got.Tools) != 1 {
		t.Fatalf("after sign-in: %+v %v", got, err)
	}
	// A state is good once.
	w = rig.call("GET", cb, "", map[string]string{"Sec-Fetch-Site": "cross-site", "X-Test-Session": "yes"})
	if !strings.Contains(w.Body.String(), "<h1>") || strings.Contains(w.Body.String(), "postMessage") {
		t.Fatalf("a used state: %s", w.Body)
	}
	return id
}

func TestMCPPageRoutesAuth(t *testing.T) {
	rig := newMCPRig(t)
	if w := rig.call("GET", "/api/platform/mcp", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", w.Code)
	}
	if w := rig.call("GET", "/api/platform/mcp", "", map[string]string{"X-Test-Session": "yes", "Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Fatalf("another site: %d", w.Code)
	}
	w := rig.call("GET", "/agents/api/platform/mcp", "", mcpPage)
	out := decode(t, w)
	if servers, _ := out["servers"].([]any); w.Code != 200 || servers == nil || len(out["catalog"].([]any)) == 0 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	// The CLI token opens the agents' list, not the page's routes.
	if w := rig.call("GET", "/api/platform/mcp", "", mcpCLI); w.Code != http.StatusUnauthorized {
		t.Fatalf("page route by token: %d", w.Code)
	}
	if w := rig.call("GET", "/api/cli/mcp", "", nil); w.Code != http.StatusForbidden {
		t.Fatalf("cli list without token: %d", w.Code)
	}
	if w := rig.call("GET", "/api/cli/mcp", "", mcpCLI); w.Code != 200 || decode(t, w)["catalog"] != nil {
		t.Fatalf("cli list: %d %s", w.Code, w.Body)
	}
	if w := rig.call("PATCH", "/api/platform/mcp/nope", `{"enabled":false}`, mcpPage); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", w.Code)
	}
}

func TestMCPSignInOrigin(t *testing.T) {
	up := newMCPUpstream(t)
	rig := newMCPRig(t)
	w := rig.call("POST", "/api/platform/mcp", `{"url":"`+up.base+`/mcp"}`, mcpPage)
	id := decode(t, w)["server"].(map[string]any)["id"].(string)
	// The origin the page names must be the one it is on.
	for _, o := range []string{"https://evil.test", "http://app.test/path", "javascript:alert(1)", ""} {
		if w := rig.call("POST", "/api/platform/mcp/"+id+"/signin", `{"origin":"`+o+`"}`, mcpPage); w.Code != http.StatusBadRequest {
			t.Fatalf("origin %q: %d", o, w.Code)
		}
	}
	// Behind a proxy without an Origin header: the forwarded host. Under the app's base: the base in the callback.
	h := map[string]string{"X-Test-Session": "yes", "X-Forwarded-Host": "public.test"}
	w = rig.call("POST", "/agents/api/platform/mcp/"+id+"/signin", `{"origin":"https://public.test"}`, h)
	au, _ := url.Parse(decode(t, w)["authUrl"].(string))
	if w.Code != 200 || au.Query().Get("redirect_uri") != "https://public.test"+appBase()+platformAPI+"/mcp/callback" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestMCPProxy(t *testing.T) {
	up := newMCPUpstream(t)
	rig := newMCPRig(t)
	id := signInRig(t, rig, up)
	proxy := "/api/cli/mcp-proxy/" + id

	if w := rig.call("POST", proxy, `{}`, nil); w.Code != http.StatusForbidden {
		t.Fatalf("no token: %d", w.Code)
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	h := map[string]string{"x-cli-token": mcpTestCLIToken, "Content-Type": "application/json", "Accept": "application/json, text/event-stream",
		"Mcp-Protocol-Version": "2025-06-18", "Cookie": "session=abc"}
	w := rig.call("POST", proxy, init, h)
	if w.Code != 200 || w.Header().Get("Mcp-Session-Id") != "sess-1" || !strings.Contains(w.Body.String(), `"serverInfo"`) {
		t.Fatalf("initialize: %d %v %s", w.Code, w.Header(), w.Body)
	}
	seen := up.lastSeen()
	if seen.Get("Authorization") != "Bearer at-1" || seen.Get("x-cli-token") != "" || seen.Get("Cookie") != "" ||
		seen.Get("Mcp-Protocol-Version") != "2025-06-18" {
		t.Fatalf("upstream headers: %v", seen)
	}

	// The server stops taking the token: the proxy refreshes once and sends the request again.
	up.mu.Lock()
	up.access = "revoked"
	up.mu.Unlock()
	w = rig.call("POST", proxy, init, h)
	if w.Code != 200 || up.refreshes != 1 || up.lastSeen().Get("Authorization") != "Bearer at-2" {
		t.Fatalf("after a refresh: %d %s, %d refreshes", w.Code, w.Body, up.refreshes)
	}
	// Refused again after the refresh: the person signs in again; the agent sees no 401 of its own.
	up.mu.Lock()
	up.deny = true
	up.mu.Unlock()
	w = rig.call("POST", proxy, init, h)
	if w.Code != http.StatusConflict || decode(t, w)["error"] != "signin-required" || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("refused twice: %d %s", w.Code, w.Body)
	}
	up.mu.Lock()
	up.deny = false
	up.mu.Unlock()

	// A launch's agent token is let in as well.
	agentTok, err := rig.d.CLI.IssueAgent("launch-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ah := map[string]string{"x-cli-token": agentTok, "Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
	if w := rig.call("POST", proxy, init, ah); w.Code != 200 || up.lastSeen().Get("x-cli-token") != "" {
		t.Fatalf("agent token: %d %s", w.Code, w.Body)
	}

	// A server turned off for agents is not reached.
	if w := rig.call("PATCH", "/api/platform/mcp/"+id, `{"enabled":false}`, mcpPage); w.Code != 200 {
		t.Fatalf("turn off: %d %s", w.Code, w.Body)
	}
	if w := rig.call("POST", proxy, init, h); w.Code != http.StatusNotFound {
		t.Fatalf("off: %d", w.Code)
	}
}

// An event stream comes through as it is written: the first event arrives while the server still holds the second.
func TestMCPProxyStreamsSSE(t *testing.T) {
	up := newMCPUpstream(t)
	rig := newMCPRig(t)
	id := signInRig(t, rig, up)
	req, _ := http.NewRequest("POST", rig.srv.URL+"/api/cli/mcp-proxy/"+id, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"stream"}`))
	for k, v := range mcpCLI {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") || res.Header.Get("Mcp-Session-Id") != "sess-1" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	br := bufio.NewReader(res.Body)
	first := make(chan string, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := br.ReadString('\n')
			b.WriteString(line)
			if err != nil || line == "\n" {
				first <- b.String()
				return
			}
		}
	}()
	select {
	case got := <-first:
		if !strings.Contains(got, `data: {"n":1}`) {
			t.Fatalf("first event %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first event was held back")
	}
	close(up.release)
	rest, _ := io.ReadAll(br)
	if !strings.Contains(string(rest), `data: {"n":2}`) {
		t.Fatalf("rest %q", rest)
	}
}

// The legacy transport: the stream's endpoint event points back at the proxy, and a POST there reaches the server's
// endpoint with the credentials.
func TestMCPProxyLegacySSE(t *testing.T) {
	up := newMCPUpstream(t)
	rig := newMCPRig(t)
	now := time.Now().UTC()
	store := map[string]any{"version": 1, "servers": []mcpconn.Server{{ID: "leg1", Name: "Legacy", Slug: "legacy", URL: up.base + "/sse",
		Transport: mcpconn.SSE, Auth: mcpconn.AuthNone, Enabled: true, State: mcpconn.StateOK, CreatedAt: now}}}
	raw, _ := json.Marshal(store)
	if err := os.WriteFile(filepath.Join(rig.d.DataDir, "mcp-connections.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", rig.srv.URL+"/api/cli/mcp-proxy/leg1", nil)
	req.Header.Set("x-cli-token", mcpTestCLIToken)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	br := bufio.NewReader(res.Body)
	var endpoint string
	for endpoint == "" {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v", err)
		}
		if d, ok := strings.CutPrefix(strings.TrimSpace(line), "data: "); ok {
			endpoint = d
		}
	}
	if endpoint != "/api/cli/mcp-proxy/leg1/message?to="+url.QueryEscape("/messages?sid=7") {
		t.Fatalf("endpoint %q", endpoint)
	}
	post, _ := http.NewRequest("POST", rig.srv.URL+endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	post.Header.Set("x-cli-token", mcpTestCLIToken)
	post.Header.Set("Content-Type", "application/json")
	pres, err := http.DefaultClient.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	pres.Body.Close()
	up.mu.Lock()
	posted := strings.Join(up.posted, "|")
	up.mu.Unlock()
	if pres.StatusCode != http.StatusAccepted || posted != `sid=7 {"jsonrpc":"2.0","id":1,"method":"ping"}` || up.lastSeen().Get("x-cli-token") != "" {
		t.Fatalf("post: %d %q", pres.StatusCode, posted)
	}
	// Only the server's own origin is reached through the message route.
	evil, _ := http.NewRequest("POST", rig.srv.URL+"/api/cli/mcp-proxy/leg1/message?to="+url.QueryEscape("http://169.254.169.254/x"), strings.NewReader(`{}`))
	evil.Header.Set("x-cli-token", mcpTestCLIToken)
	eres, err := http.DefaultClient.Do(evil)
	if err != nil {
		t.Fatal(err)
	}
	eres.Body.Close()
	if eres.StatusCode != http.StatusBadRequest {
		t.Fatalf("another origin: %d", eres.StatusCode)
	}
}

// The agents get every enabled server through the proxy: Claude a config file with the runtime's token and the allow
// rules, Codex its -c flags.
func TestMCPAgentConfigs(t *testing.T) {
	dir := t.TempDir()
	runtimeCfg := filepath.Join(dir, "preview-mcp.json")
	if err := os.WriteFile(runtimeCfg, []byte(`{"mcpServers":{"harnessly":{"type":"http","url":"http://127.0.0.1:8022/api/cli/preview/mcp","headers":{"x-cli-token":"tok"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var servers []mcpconn.Server
	enabled := func() []mcpconn.Server { return servers }
	cfg := mcpClaudeConfig(enabled, runtimeCfg, filepath.Join(dir, "mcp"))
	if path, allow := cfg(); path != "" || allow != nil {
		t.Fatalf("no servers: %q %v", path, allow)
	}
	servers = []mcpconn.Server{{ID: "abc", Slug: "linear"}, {ID: "def", Slug: "notion"}}
	path, allow := cfg()
	if path == "" || strings.Join(allow, ",") != "mcp__mcp-linear,mcp__mcp-notion" {
		t.Fatalf("%q %v", path, allow)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", info, err)
	}
	raw, _ := os.ReadFile(path)
	var got struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if s := got.Servers["mcp-linear"]; s.Type != "http" || s.URL != "http://127.0.0.1:8022/api/cli/mcp-proxy/abc" || s.Headers["x-cli-token"] != "tok" {
		t.Fatalf("%s", raw)
	}
	if again, _ := cfg(); again != path {
		t.Fatalf("not written by content: %q %q", again, path)
	}
	own := codex.MCP{Server: "harnessly", TokenHeader: "x-cli-token", TokenEnv: "HARNESSLY_CLI_TOKEN"}
	cx := mcpCodexServers(enabled, own, 8022)()
	if len(cx) != 2 || cx[0] != (codex.MCP{Server: "mcp-linear", URL: "http://127.0.0.1:8022/api/cli/mcp-proxy/abc", TokenHeader: "x-cli-token", TokenEnv: "HARNESSLY_CLI_TOKEN"}) {
		t.Fatalf("%+v", cx)
	}
	if mcpCodexServers(enabled, own, 0)() != nil {
		t.Fatal("no port, no servers")
	}
}
