package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// remoteStub is an app's remote MCP server: two tools, and a call that echoes its tool and arguments.
func remoteStub(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Error(err)
		}
		if msg.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch msg.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{"name": "whoami", "description": "Who the person is.",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"full": map[string]any{"type": "boolean"}}}},
				map[string]any{"name": "ping"},
			}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "called " + string(msg.Params)}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	}
}

func TestListToolsOfARemoteServer(t *testing.T) {
	srv := httptest.NewServer(remoteStub(t))
	defer srv.Close()
	withNetwork(t, DeployNetwork{Internal: []string{"127.0.0.1"}})
	tools, err := ListTools(context.Background(), MCPServer{URL: srv.URL})
	if err != nil || len(tools) != 2 || tools[0].Name != "whoami" || !strings.Contains(string(tools[0].InputSchema), "full") {
		t.Fatalf("tools: %v %+v", err, tools)
	}
}

type proxyCall struct{ app, server, tool, args string }

func proxyFixture(on *[]string, calls *[]proxyCall, lists *int) AppMCP {
	pkgs := map[string]*Package{
		"example.crm": {Manifest: &Manifest{ID: "example.crm", Name: "CRM", Version: "1.0.0"}, MCP: map[string]MCPServer{
			"api":   {Type: "http", URL: "https://crm.example.com/mcp"},
			"local": {Command: "crm-mcp"},
		}},
	}
	return AppMCP{
		Apps: func() []string { return *on },
		Package: func(app string) (*Package, error) {
			if p, ok := pkgs[app]; ok {
				return p, nil
			}
			return nil, errors.New("not installed")
		},
		ListServer: func(_ context.Context, app, server string) ([]RemoteTool, error) {
			*lists++
			return []RemoteTool{{Name: "whoami", Description: "Who the person is.", InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
		},
		CallServer: func(_ context.Context, app, server, tool string, args map[string]any) (json.RawMessage, error) {
			raw, _ := json.Marshal(args)
			*calls = append(*calls, proxyCall{app, server, tool, string(raw)})
			return json.RawMessage(`{"content":[{"type":"text","text":"Sergey"}]}`), nil
		},
		Cache: NewRemoteTools(),
	}
}

func TestAppMCPListsTheRemoteServersOfTheAppsOn(t *testing.T) {
	on := []string{"example.crm"}
	var calls []proxyCall
	lists := 0
	a := proxyFixture(&on, &calls, &lists)
	tools := a.Tools()
	if len(tools) != 1 {
		t.Fatalf("one remote tool, the local server left out: %s", jsvalue.Stringify(tools, ""))
	}
	def := string(jsvalue.Stringify(tools[0], ""))
	if !strings.Contains(def, `"name":"example-crm_api_whoami"`) || !strings.Contains(def, `[CRM] Who the person is.`) {
		t.Fatalf("definition: %s", def)
	}
	// The list is kept: the second tools/list does not ask the server again.
	_ = a.Tools()
	if lists != 1 {
		t.Fatalf("listed %d times", lists)
	}
	out := a.Call(context.Background(), "example-crm_api_whoami", jsvalue.NewObject("full", true))
	if s := string(jsvalue.Stringify(out, "")); !strings.Contains(s, "Sergey") {
		t.Fatalf("result: %s", s)
	}
	if len(calls) != 1 || calls[0] != (proxyCall{"example.crm", "api", "whoami", `{"full":true}`}) {
		t.Fatalf("calls: %+v", calls)
	}
	// Turned off: its tools are gone and a call by the old name is refused.
	on = nil
	if len(a.Tools()) != 0 || a.Has("example-crm_api_whoami") {
		t.Fatal("an app turned off keeps its tools")
	}
	if failed, _ := a.Call(context.Background(), "example-crm_api_whoami", nil).Get("isError"); failed != true {
		t.Fatal("a call of an app turned off went through")
	}
	if len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestAppMCPToldOfAServerThatFails(t *testing.T) {
	on := []string{"example.crm"}
	var calls []proxyCall
	lists := 0
	a := proxyFixture(&on, &calls, &lists)
	a.ListServer = func(context.Context, string, string) ([]RemoteTool, error) {
		lists++
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: ToolUnreachable, Detail: "dial tcp: refused"}
	}
	var told []string
	a.Failed = func(_ context.Context, app, what string, err error) { told = append(told, app+" "+what) }
	if len(a.Tools()) != 0 || len(a.Tools()) != 0 {
		t.Fatal("a server that fails lists tools")
	}
	if lists != 1 || len(told) != 1 || told[0] != "example.crm tools/list api" {
		t.Fatalf("lists %d, told %v", lists, told)
	}
	// A call that fails tells the agent the code and the word, never the transport's text.
	a = proxyFixture(&on, &calls, &lists)
	a.CallServer = func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		return nil, &BridgeError{Code: CodeAuthRequired, Message: AuthRequiredMessage, Detail: "secret body"}
	}
	s := string(jsvalue.Stringify(a.Call(context.Background(), "example-crm_api_whoami", nil), ""))
	if !strings.Contains(s, CodeAuthRequired) || strings.Contains(s, "secret body") {
		t.Fatalf("result: %s", s)
	}
}

func TestProxyToolName(t *testing.T) {
	valid := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	for _, c := range []struct{ app, server, tool string }{
		{"example.crm", "api", "whoami"},
		{"ticks.app", "my server", "a.b/c"},
		{"a.very.long.application.identifier", "the-backend-server", "list_every_record_of_the_organization"},
	} {
		n := ProxyToolName(c.app, c.server, c.tool)
		if !valid.MatchString(n) {
			t.Errorf("%v: %q", c, n)
		}
	}
	if ProxyToolName("example.crm", "api", "whoami") != "example-crm_api_whoami" {
		t.Error(ProxyToolName("example.crm", "api", "whoami"))
	}
	long1 := ProxyToolName("a.very.long.application.identifier", "the-backend-server", "list_every_record_of_the_organization")
	long2 := ProxyToolName("a.very.long.application.identifier", "the-backend-server", "list_every_record_of_the_organizatioN")
	if long1 == long2 {
		t.Error("two long names are one")
	}
}

func TestLaunchNeedsOnlyLocalServers(t *testing.T) {
	remote := &Package{Manifest: &Manifest{ID: "x.y"}, MCP: map[string]MCPServer{"api": {URL: "https://x.example/mcp"}, "c": {Connection: "linear"}}}
	if needs := LaunchNeeds(remote); needs != "" {
		t.Fatalf("remote servers go live: %q", needs)
	}
	local := &Package{Manifest: &Manifest{ID: "x.y"}, MCP: map[string]MCPServer{"api": {URL: "https://x.example/mcp"}, "cli": {Command: "x"}}}
	if needs := LaunchNeeds(local); !strings.Contains(needs, "local MCP servers") {
		t.Fatalf("a local server needs a launch: %q", needs)
	}
	if got := ProxiedServers(local); len(got) != 1 || got[0] != "api" {
		t.Fatalf("proxied: %v", got)
	}
}
