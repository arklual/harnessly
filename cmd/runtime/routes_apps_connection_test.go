package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSecrets) Put(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[name] = value
	return nil
}
func (s *memSecrets) Reveal(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[name]; ok {
		return v, nil
	}
	return "", mcpconn.ErrSignInRequired
}
func (s *memSecrets) Remove(name string) error { return nil }

// fakeLinear is a remote MCP server that takes the one token and answers tools/call with the token's owner.
func fakeLinear(t *testing.T, owners map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, ok := owners[r.Header.Get("Authorization")]
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		var result any
		switch msg.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake"}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{}}
		case "tools/call":
			mu.Lock()
			calls = append(calls, owner)
			mu.Unlock()
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "hello " + owner}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	}))
	t.Cleanup(ts.Close)
	return ts, &calls
}

func signedIn(t *testing.T, ts *httptest.Server, token string) *mcpconn.Service {
	t.Helper()
	ms := mcpconn.New(t.TempDir(), &memSecrets{}, nil)
	if _, _, err := ms.Add(context.Background(), mcpconn.AddInput{URL: ts.URL + "/mcp", CatalogID: "linear",
		Header: &mcpconn.HeaderInput{Name: "Authorization", Value: token}}); err != nil {
		t.Fatal(err)
	}
	return ms
}

func TestViewToolCallGoesThroughThePersonsConnection(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1")
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{u.Hostname()}})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	ts, calls := fakeLinear(t, map[string]string{"Bearer tok-sergey": "sergey", "Bearer tok-roma": "roma"})
	decl := apps.MCPServer{Type: "http", Connection: "linear"}

	// Two people, two machines, one app: each goes under their own sign-in.
	sergey, roma := signedIn(t, ts, "Bearer tok-sergey"), signedIn(t, ts, "Bearer tok-roma")
	for _, c := range []struct {
		ms   *mcpconn.Service
		want string
	}{{sergey, "hello sergey"}, {roma, "hello roma"}} {
		raw, err := callConnectionTool(context.Background(), c.ms, true, decl, "whoami", nil)
		if err != nil || !strings.Contains(string(raw), c.want) {
			t.Fatalf("%s: %s %v", c.want, raw, err)
		}
	}
	if strings.Join(*calls, ",") != "sergey,roma" {
		t.Fatalf("calls: %v", *calls)
	}

	// No connection on this machine: the app waits for it, the call is refused, nothing goes out.
	empty := mcpconn.New(t.TempDir(), &memSecrets{}, nil)
	_, err := callConnectionTool(context.Background(), empty, true, decl, "whoami", nil)
	var be *apps.BridgeError
	if !errors.As(err, &be) || be.Message != apps.ToolRefused || !strings.Contains(be.Detail, "waits for a connection") {
		t.Fatalf("want a refusal that waits for the connection: %v", err)
	}
	if _, err := callConnectionTool(context.Background(), sergey, false, decl, "whoami", nil); !errors.As(err, &be) || !strings.Contains(be.Detail, "waits for a connection") {
		t.Fatalf("signed in but not agreed to: refused, got %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("nothing went out without the agreement: %v", *calls)
	}
	if got := waitingConnectionsOf(empty, []string{"linear", "asana"}); strings.Join(got, ",") != "linear,asana" {
		t.Fatalf("waiting: %v", got)
	}
	if got := waitingConnectionsOf(sergey, []string{"linear"}); len(got) != 0 {
		t.Fatalf("signed in, nothing to wait for: %v", got)
	}
}

func TestAppProxyGoesToTheConnectionOnlyWithTheAgreement(t *testing.T) {
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	ts, calls := fakeLinear(t, map[string]string{"Bearer tok": "sergey"})
	data := t.TempDir()
	cli := &gate.CLIToken{DataDir: data, Headers: []string{"x-cli-token"}}
	d := deps{DataDir: data, Home: t.TempDir(), CLI: cli}
	sf := safeFor(d)
	t.Cleanup(sf.files.Close)
	if _, _, err := mcpFor(d).Add(context.Background(), mcpconn.AddInput{URL: ts.URL + "/mcp", CatalogID: "linear",
		Header: &mcpconn.HeaderInput{Name: "Authorization", Value: "Bearer tok"}}); err != nil {
		t.Fatal(err)
	}
	reg := apps.Open(data)
	pkg := filepath.Join(t.TempDir(), "tracker")
	if err := apps.Scaffold(pkg, "acme.tracker", "Tracker", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(`{"mcpServers":{"linear":{"type":"http","connection":"linear"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ps, err := reg.Install(pkg, apps.InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	grant := filepath.Join(data, "app-launch", "aaaaaaaaaaaaaaaa")
	_ = os.MkdirAll(grant, 0o700)
	_ = os.WriteFile(filepath.Join(grant, "grant.json"), []byte(`{"apps":["acme.tracker"]}`), 0o600)
	tok, _ := cli.IssueAgent("aaaaaaaaaaaaaaaa", time.Hour)
	route := appMCPProxyRoute(d, reg)
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/cli/apps/mcp/acme.tracker/linear", strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"whoami"}}`))
		r.Header.Set("x-cli-token", tok)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		route.Handler.ServeHTTP(w, r)
		return w
	}
	if w := post(); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "waiting-connection") || len(*calls) != 0 {
		t.Fatalf("without the agreement: %d %s %v", w.Code, w.Body.String(), *calls)
	}
	if err := reg.GrantConnection("acme.tracker", "linear"); err != nil {
		t.Fatal(err)
	}
	if w := post(); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "hello sergey") {
		t.Fatalf("with the agreement: %d %s", w.Code, w.Body.String())
	}
}

// «Настройка» of an app lists the connections its servers go through and lets the person allow and take back each; the
// page route needs the page's session (the agent has none).
func TestSetupAllowsTheAppAConnection(t *testing.T) {
	data := t.TempDir()
	d := deps{DataDir: data, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: data, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	sf := safeFor(d)
	t.Cleanup(sf.files.Close)
	reg := apps.Open(data)
	pkg := filepath.Join(t.TempDir(), "tracker")
	if err := apps.Scaffold(pkg, "acme.tracker", "Tracker", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, ".mcp.json"), []byte(`{"mcpServers":{"linear":{"type":"http","connection":"linear"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ps, err := reg.Install(pkg, apps.InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	routes := appSetupRoutes(d, reg, apps.OpenStorage(data))
	post := func(path, body string, session bool) (int, setupAnswer) {
		r := httptest.NewRequest(http.MethodPost, platformAPI+path, strings.NewReader(body))
		if session {
			r.Header.Set("X-Test-Session", "yes")
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(r) {
				rt.Handler.ServeHTTP(w, r)
				break
			}
		}
		var out setupAnswer
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	code, got := post("/apps/setup", `{"id":"acme.tracker"}`, true)
	if code != http.StatusOK || len(got.Connections) != 1 || got.Connections[0].ID != "linear" || got.Connections[0].Granted || got.Connections[0].Connected {
		t.Fatalf("read: %d %+v", code, got.Connections)
	}
	if code, _ := post("/apps/setup/connection", `{"id":"acme.tracker","name":"linear"}`, false); code != http.StatusUnauthorized || reg.ConnectionGranted("acme.tracker", "linear") {
		t.Fatalf("without a session: %d", code)
	}
	if code, got := post("/apps/setup/connection", `{"id":"acme.tracker","name":"linear"}`, true); code != http.StatusOK || !got.Connections[0].Granted {
		t.Fatalf("allow: %d %+v", code, got.Connections)
	}
	if code, _ := post("/apps/setup/connection", `{"id":"acme.tracker","name":"github"}`, true); code != http.StatusNotFound {
		t.Fatalf("a connection the app does not name: %d", code)
	}
	if code, got := post("/apps/setup/connection", `{"id":"acme.tracker","name":"linear","remove":true}`, true); code != http.StatusOK || got.Connections[0].Granted {
		t.Fatalf("take back: %d %+v", code, got.Connections)
	}
}
