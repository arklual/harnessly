package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/connectors"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

const (
	acmeKey = "sk-live-" + "acme-" + "0123456789"
	teamKey = "team-" + "key-" + "9876543210"
)

// acmeAPI is a company's internal API: an OpenAPI file, a key in X-Api-Key, two paths.
func acmeAPI(t *testing.T) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Acme orders"},"paths":{"/v2/orders":{"get":{"summary":"List orders"}},"/v2/me":{"get":{"summary":"Who the key is"}}}}`))
			return
		}
		if r.Header.Get("X-Api-Key") != acmeKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"path":%q,"orders":[{"id":7,"status":"paid"}],"debug":"key %s"}`, r.URL.Path, acmeKey)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// teamMCP is a self-made MCP server with a header key and one reading tool.
func teamMCP(t *testing.T) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Team-Key") != teamKey {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "team"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "lookup_invoice", "description": "Finds an invoice by number",
				"annotations": map[string]any{"readOnlyHint": true}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "invoice 42 is paid, key " + r.Header.Get("X-Team-Key")}}}
		default:
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}))
	t.Cleanup(ts.Close)
	return ts
}

// connectAsAgent runs the hub's connection_add the way an agent does and answers the card the way the person does.
func connectAsAgent(t *testing.T, h *mcpconn.Hub, svc *connectors.Service, args map[string]any, yes bool) (reply string, card []connectors.Pair) {
	t.Helper()
	call := func(arguments map[string]any) string {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "connection_add", "arguments": arguments}})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body))))
		return rec.Body.String()
	}
	first := make(chan string, 1)
	go func() { first <- call(args) }()
	deadline := time.Now().Add(10 * time.Second)
	var p connectors.Proposal
	for time.Now().Before(deadline) {
		if list := svc.Proposals(); len(list) > 0 {
			p = list[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if p.ID == "" {
		t.Fatalf("no card was shown: %s", <-first)
	}
	if p.Conv != "chat-1" || p.Title.En == "" {
		t.Fatalf("the card is not the chat's: %+v", p)
	}
	if _, err := svc.Answer(context.Background(), p.ID, yes); err != nil {
		t.Fatal(err)
	}
	out := <-first
	var message struct {
		Result struct {
			Content []struct{ Text string }
		}
	}
	if err := json.Unmarshal([]byte(out), &message); err != nil {
		t.Fatal(err)
	}
	for _, block := range message.Result.Content {
		if strings.HasPrefix(block.Text, "waiting:") {
			if !strings.Contains(block.Text, fmt.Sprintf("proposal=%q", p.ID)) {
				t.Fatalf("waiting reply did not identify the answered card: %s", out)
			}
			out = call(map[string]any{"proposal": p.ID})
			break
		}
	}
	return out, p.Args
}

// TestOwnServicesEndToEnd: a company's API with an OpenAPI file and a self-made MCP server are connected by an agent
// (the hub's connection_add, the person's yes on a card, the keys from the safe), found and used through the hub,
// and an app that names them in its manifest reads them after the person's agreement. No key leaks anywhere.
func TestOwnServicesEndToEnd(t *testing.T) {
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	api, team := acmeAPI(t), teamMCP(t)
	sec := &memSecrets{}
	_ = sec.Put("custom-acme-orders-api-key", acmeKey) // what `harnessly secret ask` put in the safe
	_ = sec.Put("custom-team-tools-token", teamKey)
	data := t.TempDir()
	ms := mcpconn.New(data, sec, nil)
	cx := connectors.New(filepath.Join(data, "connectors"), sec, nil)
	h := &mcpconn.Hub{S: ms, ConfirmWait: 5 * time.Second,
		Confirm: proposeConfirmer(cx, func(context.Context) connectors.By { return connectors.By{Conv: "chat-1", Title: "chat"} })}

	// The agent read the API's OpenAPI file (/openapi.json) and writes the record; the person says yes.
	reply, card := connectAsAgent(t, h, cx, map[string]any{
		"id": "custom-acme-orders", "kind": "api", "name": "Заказы Acme", "url": api.URL + "/v2", "checkPath": "/me",
		"keys":       []any{map[string]any{"name": "API_KEY", "label": "Ключ API", "safe": "custom-acme-orders-api-key"}},
		"apiHeaders": map[string]any{"X-Api-Key": "{API_KEY}"},
		"guide":      "GET /orders — list the orders; GET /me — who the key is",
		"category":   "crm", "about": "Internal orders API: orders, customers, refunds.", "can": []any{"find an order by number"},
		"why": "the person asked to connect the orders API",
	}, true)
	if !strings.Contains(reply, "Made custom-acme-orders") || !strings.Contains(reply, "acme-orders") {
		t.Fatalf("api reply: %s", reply)
	}
	cardJSON, _ := json.Marshal(card)
	if !strings.Contains(string(cardJSON), "custom-acme-orders-api-key") || !strings.Contains(string(cardJSON), "GET /me") {
		t.Fatalf("the card does not show what will be made: %s", cardJSON)
	}

	reply, _ = connectAsAgent(t, h, cx, map[string]any{
		"kind": "mcp", "name": "Team tools", "url": team.URL + "/mcp", "headerName": "X-Team-Key",
		"keys":     []any{map[string]any{"name": "TOKEN", "safe": "custom-team-tools-token"}},
		"category": "docs", "about": "Our invoices server.", "can": []any{"look up an invoice"},
	}, true)
	if !strings.Contains(reply, "Made custom-team-tools") {
		t.Fatalf("mcp reply: %s", reply)
	}

	// A declined card makes nothing.
	before, _ := ms.List()
	reply, _ = connectAsAgent(t, h, cx, map[string]any{"kind": "api", "name": "Nope", "url": api.URL}, false)
	if after, _ := ms.List(); len(after) != len(before) || !strings.Contains(reply, "declined") {
		t.Fatalf("declined: %s", reply)
	}

	// Found and used through the hub like a catalog entry; no key in any answer, record or journal.
	tool := func(name string, args map[string]any) string {
		raw, _ := json.Marshal(args)
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": json.RawMessage(raw)}})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body))))
		return rec.Body.String()
	}
	if got := tool("search_tools", map[string]any{"query": "find an invoice"}); !strings.Contains(got, "team-tools.lookup_invoice") {
		t.Fatalf("search_tools: %s", got)
	}
	if got := tool("connections", map[string]any{"query": "refunds"}); !strings.Contains(got, "acme-orders") || !strings.Contains(got, "guide:") {
		t.Fatalf("connections: %s", got)
	}
	got := tool("api_request", map[string]any{"connection": "acme-orders", "path": "/orders"})
	if !strings.Contains(got, "HTTP 200") || !strings.Contains(got, "paid") || strings.Contains(got, acmeKey) {
		t.Fatalf("api_request: %s", got)
	}
	got = tool("call", map[string]any{"connection": "team-tools", "tool": "lookup_invoice", "arguments": map[string]any{"number": 42}})
	if !strings.Contains(got, "invoice 42 is paid") {
		t.Fatalf("call: %s", got)
	}
	servers, _ := ms.List()
	rawServers, _ := json.Marshal(servers)
	journal, _ := os.ReadFile(filepath.Join(data, "connectors", "sent.jsonl"))
	for _, leak := range []string{acmeKey, teamKey} {
		if strings.Contains(string(rawServers), leak) || strings.Contains(string(journal), leak) {
			t.Fatalf("%s is in a record or the journal", leak)
		}
	}
	if !strings.Contains(string(journal), "custom-acme-orders") {
		t.Fatalf("the journal does not say what was agreed to: %s", journal)
	}

	// An app names them in its manifest: by id and by group. It reads after the person agrees, and sees no key.
	reg := apps.Open(data)
	pkg := filepath.Join(t.TempDir(), "panel")
	if err := apps.Scaffold(pkg, "acme.panel", "Panel", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"connections.read"}
	m["connections"] = []map[string]any{{"id": "custom-acme-orders", "access": "read", "reason": "Shows orders"}, {"id": "custom-team-tools"}, {"category": "docs"}}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ps, err := reg.Install(pkg, apps.InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("a manifest naming custom-… does not install: %v %v", err, ps)
	}
	conns := &appConns{ms: ms, reg: reg}
	ctx := context.Background()
	list, err := conns.list("acme.panel")
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %+v", err, list)
	}
	for _, c := range list {
		if c.Status != connNeedsConsent || c.Category == "" {
			t.Errorf("before the agreement: %+v", c)
		}
	}
	if _, _, err := conns.api(ctx, "acme.panel", "custom-acme-orders", "GET", "/orders", nil, nil, false); code(err) != apps.CodePermissionDenied {
		t.Fatalf("no agreement: %v", err)
	}
	for _, id := range []string{"custom-acme-orders", "custom-team-tools"} {
		if err := reg.GrantConnection("acme.panel", id); err != nil {
			t.Fatal(err)
		}
	}
	ans, ask, err := conns.api(ctx, "acme.panel", "custom-acme-orders", "GET", "/orders", nil, nil, false)
	if err != nil || ask != nil || ans.Status != 200 || !strings.Contains(ans.Body, "paid") || strings.Contains(ans.Body, acmeKey) {
		t.Fatalf("hl.connections.api: %v %+v", err, ans)
	}
	if _, ask, err := conns.api(ctx, "acme.panel", "custom-acme-orders", "POST", "/orders", nil, nil, true); code(err) != apps.CodePermissionDenied || ask != nil {
		t.Fatalf("a change with read access: %v %v", err, ask)
	}
	out, _, err := conns.call(ctx, "acme.panel", "custom-team-tools", "lookup_invoice", map[string]any{"number": 42}, false)
	if err != nil || !strings.Contains(string(out), "invoice 42 is paid") || strings.Contains(string(out), teamKey) {
		t.Fatalf("hl.connections.call: %s %v", out, err)
	}
	list, _ = conns.list("acme.panel")
	rawList, _ := json.Marshal(list)
	for _, c := range list {
		if c.Status != connReady {
			t.Errorf("after the agreement: %+v", c)
		}
	}
	for _, leak := range []string{acmeKey, teamKey, "127.0.0.1", "X-Api-Key"} {
		if strings.Contains(string(rawList), leak) {
			t.Fatalf("the app's list carries %q", leak)
		}
	}
}
