package mcpconn

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFakeLocalServer is not a test: started by a local server's command (the test binary itself), it is a tiny MCP
// server on stdin/stdout whose one tool answers with its arguments and the key FAKE_KEY of its environment.
func TestFakeLocalServer(t *testing.T) {
	if os.Getenv("HARNESSLY_FAKE_MCP") != "1" {
		t.Skip("a helper process")
	}
	fmt.Println("a banner a careless server prints") // not JSON: must be skipped
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	out := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || len(m.ID) == 0 {
			continue
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "fake", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "echo", "description": "Echoes its text back",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}}}}}
		case "tools/call":
			var p struct {
				Arguments struct {
					Text string `json:"text"`
					Big  int    `json:"big"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(m.Params, &p)
			text := p.Arguments.Text + " key=" + os.Getenv("FAKE_KEY")
			if p.Arguments.Big > 0 {
				text = strings.Repeat("x", p.Arguments.Big)
			}
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
		default:
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "no"}})
			continue
		}
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
}

// withEntry puts e in the catalog for the test.
func withEntry(t *testing.T, e CatalogEntry) {
	t.Helper()
	old := catalog
	catalog = append(append([]CatalogEntry{}, catalog...), e)
	t.Cleanup(func() { catalog = old })
}

func fakeLocalEntry(t *testing.T) CatalogEntry {
	t.Setenv("HARNESSLY_FAKE_MCP", "1")
	return CatalogEntry{ID: "fake-local", Name: "Fake Local", Kind: KindUtility, Transport: Stdio, Auth: AuthEnv,
		Command: []string{os.Args[0], "-test.run=^TestFakeLocalServer$"},
		Env: []EnvField{{Name: "FAKE_KEY", Label: "Ключ", Secret: true},
			{Name: "HARNESSLY_FAKE_MCP", Label: "помощник", Optional: true, Default: "1"}}, Blurb: "x", BlurbRu: "икс"}
}

func TestLocalServerAddCallAndHub(t *testing.T) {
	withEntry(t, fakeLocalEntry(t))
	s, sec := newService(t)
	ctx := context.Background()

	if _, _, err := s.Add(ctx, AddInput{CatalogID: "fake-local"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("a missing required key: %v", err)
	}
	srv, start, err := s.Add(ctx, AddInput{CatalogID: "fake-local", Env: map[string]string{"FAKE_KEY": "s3cret-value", "OTHER": "x"}})
	if err != nil || start != nil {
		t.Fatalf("add: %v %v", err, start)
	}
	t.Cleanup(func() { runner.drop(srv.ID) })
	if srv.State != StateOK || len(srv.Tools) != 1 || srv.Tools[0].Name != "echo" || srv.URL != "stdio:fake-local" {
		t.Fatalf("server: %+v", srv)
	}
	raw, _ := sec.Reveal(secretName(srv.ID))
	if !strings.Contains(raw, "s3cret-value") || strings.Contains(raw, "OTHER\"") {
		t.Fatalf("the safe keeps only the fields: %s", raw)
	}
	file, _ := os.ReadFile(filepath.Join(filepath.Dir(s.file), "mcp-connections.json"))
	if strings.Contains(string(file), "s3cret") {
		t.Fatal("a key reached the servers' file")
	}

	// Two agents, both with id 1: each gets its own answer under its own id.
	for _, who := range []string{"a", "b"} {
		out, err := s.ServeLocal(ctx, srv.ID, []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		if err != nil || !strings.Contains(string(out), `"fake"`) {
			t.Fatalf("initialize %s: %s %v", who, out, err)
		}
		out, err = s.ServeLocal(ctx, srv.ID, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"`+who+`"}}}`))
		if err != nil || !strings.Contains(string(out), who+" key=s3cret-value") || !strings.Contains(string(out), `"id":1`) {
			t.Fatalf("call %s: %s %v", who, out, err)
		}
	}
	if out, err := s.ServeLocal(ctx, srv.ID, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil || out != nil {
		t.Fatalf("a notification has no answer: %s %v", out, err)
	}

	// A process that died is started again at the next call.
	runner.drop(srv.ID)
	h := &Hub{S: s, Results: t.TempDir(), Inline: 100}
	res := h.tool(ctx, "call", json.RawMessage(`{"connection":"fake-local","tool":"echo","arguments":{"text":"hi"}}`))
	if b, _ := json.Marshal(res); !strings.Contains(string(b), "hi key=s3cret-value") {
		t.Fatalf("hub call: %s", b)
	}
	// A big answer is saved to a file.
	res = h.tool(ctx, "call", json.RawMessage(`{"connection":"fake-local","tool":"echo","arguments":{"big":5000}}`))
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), "saved to") || len(b) > 1000 {
		t.Fatalf("spill: %s", b[:min(len(b), 300)])
	}
	res = h.tool(ctx, "search_tools", json.RawMessage(`{"query":"echo text"}`))
	if b, _ := json.Marshal(res); !strings.Contains(string(b), "fake-local.echo") {
		t.Fatalf("search: %s", b)
	}
	res = h.tool(ctx, "tool_schema", json.RawMessage(`{"connection":"fake-local","tool":"echo"}`))
	if b, _ := json.Marshal(res); !strings.Contains(string(b), "inputSchema") {
		t.Fatalf("schema: %s", b)
	}
	if b, _ := json.Marshal(h.tools()); !strings.Contains(string(b), "Connected now: Fake Local [fake-local]") {
		t.Fatalf("the tools' descriptions do not name the connections: %s", b)
	}
	res = h.tool(ctx, "search_tools", json.RawMessage(`{"query":"биржа акции"}`))
	if b, _ := json.Marshal(res); len(localCatalog) > 3 && !strings.Contains(string(b), "Not connected") {
		t.Fatalf("offer: %s", b)
	}

	// New keys restart the server with them; an empty field keeps its key.
	if srv, err = s.SetEnv(ctx, srv.ID, map[string]string{"FAKE_KEY": "next-value"}); err != nil || srv.State != StateOK {
		t.Fatalf("set env: %v %+v", err, srv)
	}
	out, _ := s.ServeLocal(ctx, srv.ID, []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"echo","arguments":{"text":"z"}}}`))
	if !strings.Contains(string(out), "key=next-value") {
		t.Fatalf("after new keys: %s", out)
	}
	if srv, err = s.SetEnv(ctx, srv.ID, map[string]string{"FAKE_KEY": ""}); err != nil || srv.State != StateOK {
		t.Fatalf("keep key: %v", err)
	}
	if err := s.Remove(srv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sec.Reveal(secretName(srv.ID)); err == nil {
		t.Fatal("the keys outlived the server")
	}
}

func TestLocalServerNoRuntime(t *testing.T) {
	withEntry(t, CatalogEntry{ID: "nowhere", Name: "Nowhere", Kind: KindUtility, Transport: Stdio, Auth: AuthNone,
		Command: []string{"surely-not-a-program-xyz"}, Blurb: "x", BlurbRu: "икс"})
	s, _ := newService(t)
	srv, _, err := s.Add(context.Background(), AddInput{CatalogID: "nowhere"})
	if err != nil || srv.State != StateUnreachable || !strings.Contains(srv.Problem, "not installed") {
		t.Fatalf("%v %+v", err, srv)
	}
}

func TestAPIConnection(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "good-key-123" || r.Header.Get("X-Token") != "T good-key-123" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, `{"path":%q,"echo":"good-key-123"}`, r.URL.Path)
	}))
	defer api.Close()
	withEntry(t, CatalogEntry{ID: "fake-api", Name: "Fake API", Kind: KindFinance, Transport: API, Auth: AuthEnv,
		URL: api.URL + "/v1", CheckPath: "/me", APIQuery: map[string]string{"key": "{KEY}"},
		APIHeaders: map[string]string{"X-Token": "T {KEY}"}, Env: []EnvField{{Name: "KEY", Label: "Ключ", Secret: true}},
		Guide: "GET /items", Blurb: "x", BlurbRu: "икс"})
	s, _ := newService(t)
	s.http.Client = api.Client()
	ctx := context.Background()
	if _, _, err := s.Add(ctx, AddInput{CatalogID: "fake-api", Env: map[string]string{"KEY": "bad"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("bad key: %v", err)
	}
	srv, _, err := s.Add(ctx, AddInput{CatalogID: "fake-api", Env: map[string]string{"KEY": "good-key-123"}})
	if err != nil || srv.State != StateOK {
		t.Fatalf("add: %v %+v", err, srv)
	}
	a, err := s.CallAPI(ctx, srv.ID, APIRequest{Path: "/items/1"})
	if err != nil || a.Status != 200 || !strings.Contains(string(a.Body), `"/v1/items/1"`) || strings.Contains(string(a.Body), "good-key-123") {
		t.Fatalf("call: %v %d %s", err, a.Status, a.Body)
	}
	for _, bad := range []string{"https://evil.example/x", "//evil.example/x", "/../admin", "/items/../../x"} {
		if _, err := s.CallAPI(ctx, srv.ID, APIRequest{Path: bad}); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%q left the base: %v", bad, err)
		}
	}
	h := &Hub{S: s}
	res := h.tool(ctx, "api_request", json.RawMessage(`{"connection":"fake-api","path":"/items/2","query":{"n":3}}`))
	if b, _ := json.Marshal(res); !strings.Contains(string(b), "HTTP 200") {
		t.Fatalf("hub api: %s", b)
	}
	res = h.tool(ctx, "call", json.RawMessage(`{"connection":"fake-api","tool":"x"}`))
	if b, _ := json.Marshal(res); !strings.Contains(string(b), "api_request") {
		t.Fatalf("call on an API: %s", b)
	}
}

func TestHubProtocol(t *testing.T) {
	s, _ := newService(t)
	srv := httptest.NewServer(&Hub{S: s})
	defer srv.Close()
	post := func(body string) map[string]any {
		res, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusAccepted {
			return nil
		}
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return out
	}
	init := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if r, _ := init["result"].(map[string]any); r == nil || r["instructions"] == nil {
		t.Fatalf("initialize: %v", init)
	}
	if post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`) != nil {
		t.Fatal("a notification has an answer")
	}
	list := post(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	b, _ := json.Marshal(list)
	for _, name := range []string{"connections", "search_tools", "tool_schema", "call", "api_request", "manage"} {
		if !strings.Contains(string(b), `"name":"`+name+`"`) {
			t.Errorf("no tool %s", name)
		}
	}
	got := post(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"connections","arguments":{"query":"почта"}}}`)
	if b, _ := json.Marshal(got); !strings.Contains(string(b), "Nothing is connected") {
		t.Fatalf("connections: %s", b)
	}
}
