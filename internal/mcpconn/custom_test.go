package mcpconn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// internalAPI is a company's API with a key in the header and a check path.
func internalAPI(t *testing.T, key string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v2/me", "/v2/orders":
			fmt.Fprintf(w, `{"path":%q,"debug":"your key is %s"}`, r.URL.Path, key)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// internalMCP is a self-made MCP server: a header key, one tool that echoes.
func internalMCP(t *testing.T, key string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Team-Key") != key {
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
			w.Header().Set("Mcp-Session-Id", "s1")
			result = map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "team-tools", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "lookup_invoice", "description": "Finds an invoice by number",
				"annotations": map[string]any{"readOnlyHint": true}}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "invoice 42 is paid"}}}
		default:
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func apiInput(base string) CustomInput {
	return CustomInput{ID: "custom-acme-orders", Kind: CustomAPI, Name: "Заказы Acme", URL: base + "/v2", CheckPath: "/me",
		Keys:       []CustomKey{{Name: "API_KEY", Label: "Ключ API", Safe: "custom-acme-orders-api-key"}},
		APIHeaders: map[string]string{"X-Api-Key": "{API_KEY}"},
		Guide:      "GET /orders — the orders; GET /me — who the key is",
		CustomMeta: CustomMeta{Category: KindCRM, About: "Our internal orders API: orders, customers and refunds.", Can: []string{"find an order by number"}}}
}

func TestCustomAPIEndToEnd(t *testing.T) {
	key := "sk-live-" + "acme-" + "0123456789"
	api := internalAPI(t, key)
	s, sec := newService(t)
	s.http.Client = api.Client()
	c := context.Background()

	// No key in the safe yet: planned, not checked, and a made record waits for the keys.
	plan, err := s.PlanCustom(c, apiInput(api.URL), nil)
	if err != nil || !strings.Contains(plan.Checked, "not in the safe yet") && !strings.Contains(plan.Checked, "в сейфе ещё нет") {
		t.Fatalf("plan without a key: %v %q", err, plan.Checked)
	}
	// A wrong key is refused before anything is made.
	_ = sec.Put("custom-acme-orders-api-key", "wrong")
	if _, err := s.PlanCustom(c, apiInput(api.URL), nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong key: %v", err)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatalf("a refused plan kept %d servers", len(list))
	}
	_ = sec.Put("custom-acme-orders-api-key", key)
	srv, _, err := s.SaveCustom(c, apiInput(api.URL), nil)
	if err != nil || srv.State != StateOK || srv.CatalogID != "custom-acme-orders" || srv.Slug != "acme-orders" {
		t.Fatalf("save: %v %+v", err, srv)
	}
	if b, _ := json.Marshal(srv); strings.Contains(string(b), key) {
		t.Fatalf("the record carries the key: %s", b)
	}
	if raw, _ := json.Marshal(sec.m); !strings.Contains(string(raw), key) {
		t.Fatal("the key is not in the safe")
	}
	if cr, err := s.readCred(srv.ID); err != nil || cr.Env["API_KEY"] != key {
		t.Fatalf("cred: %v %+v", err, cr)
	}

	// The hub sees it like a catalog entry: found by its description and by a tool word, and api_request works.
	h := &Hub{S: s}
	for _, q := range []string{"orders refunds", "найти заказ", "acme"} {
		res, _ := json.Marshal(h.tool(c, "connections", json.RawMessage(fmt.Sprintf(`{"query":%q}`, q))))
		if !strings.Contains(string(res), "acme-orders") {
			t.Errorf("connections(%q) does not name it: %s", q, res)
		}
	}
	res, _ := json.Marshal(h.tool(c, "search_tools", json.RawMessage(`{"query":"orders"}`)))
	if !strings.Contains(string(res), "acme-orders.api_request") {
		t.Fatalf("search_tools: %s", res)
	}
	out, _ := json.Marshal(h.tool(c, "api_request", json.RawMessage(`{"connection":"acme-orders","path":"/orders"}`)))
	if !strings.Contains(string(out), "HTTP 200") || !strings.Contains(string(out), `/v2/orders`) || strings.Contains(string(out), key) {
		t.Fatalf("api_request: %s", out)
	}
	if !strings.Contains(string(out), "[key]") {
		t.Fatalf("the echoed key is not scrubbed: %s", out)
	}

	// The same connection by the app's side: its group and its read-only marks.
	if KindOf(srv) != "crm" || !APIReads(srv.CatalogID, "GET", "/orders") || APIReads(srv.CatalogID, "POST", "/orders") {
		t.Fatalf("app access: %q", KindOf(srv))
	}
	if !IsCustomID(srv.CatalogID) || IsCustomID("custom-") || IsCustomID("linear") {
		t.Fatal("custom id")
	}

	// A change keeps the id, the slug and the key; removing forgets the key.
	in := apiInput(api.URL)
	in.About = "Orders API of the shop."
	again, _, err := s.SaveCustom(c, in, nil)
	if err != nil || again.ID != srv.ID || again.Slug != srv.Slug || again.Custom.About != "Orders API of the shop." {
		t.Fatalf("update: %v %+v", err, again)
	}
	if list, _ := s.List(); len(list) != 1 {
		t.Fatalf("an update made %d servers", len(list))
	}
	if err := s.Remove(srv.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.m["mcp-"+srv.ID]; ok {
		t.Fatal("the key outlived its service")
	}
}

func TestCustomMCPWithHeaderKey(t *testing.T) {
	key := "team-" + "key-" + "9876543210"
	mcp := internalMCP(t, key)
	s, sec := newService(t)
	s.http.Client = mcp.Client()
	c := context.Background()
	_ = sec.Put("custom-team-tools-token", key)
	in := CustomInput{Kind: CustomMCP, Name: "Team tools", URL: mcp.URL + "/mcp", HeaderName: "X-Team-Key",
		Keys:       []CustomKey{{Name: "TOKEN", Safe: "custom-team-tools-token"}},
		CustomMeta: CustomMeta{Category: KindDocs, About: "Our invoices server.", Can: []string{"look up an invoice"}}}
	plan, err := s.PlanCustom(c, in, nil)
	if err != nil || !strings.Contains(plan.Checked, "1 tools") && !strings.Contains(plan.Checked, "1 инструмент") {
		t.Fatalf("plan: %v %q", err, plan.Checked)
	}
	srv, _, err := s.SaveCustom(c, in, nil)
	if err != nil || srv.State != StateOK || srv.Auth != AuthHeader || len(srv.Tools) != 1 || srv.CatalogID != "custom-team-tools" {
		t.Fatalf("save: %v %+v", err, srv)
	}
	h := &Hub{S: s}
	res, _ := json.Marshal(h.tool(c, "call", json.RawMessage(`{"connection":"team-tools","tool":"lookup_invoice","arguments":{"number":42}}`)))
	if !strings.Contains(string(res), "invoice 42 is paid") {
		t.Fatalf("call: %s", res)
	}
	tools, err := s.AppTools(c, srv.ID)
	if err != nil || len(tools) != 1 || !tools[0].ReadOnly {
		t.Fatalf("app tools: %v %+v", err, tools)
	}
	// A wrong key in the safe is refused and nothing changes.
	_ = sec.Put("custom-team-tools-token", "nope")
	if _, err := s.PlanCustom(c, in, nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong key: %v", err)
	}
}

func TestCustomRefusals(t *testing.T) {
	s, _ := newService(t)
	c := context.Background()
	base := CustomInput{Kind: CustomAPI, Name: "X", URL: "https://api.example.com", Keys: []CustomKey{{Name: "K"}}, APIHeaders: map[string]string{"X-Key": "{K}"}}
	cases := map[string]func(*CustomInput){
		"literal key":       func(in *CustomInput) { in.APIHeaders = map[string]string{"X-Key": "sk-abc"} },
		"key in the url":    func(in *CustomInput) { in.URL = "https://api.example.com/v1?key=abc" },
		"unplaced key":      func(in *CustomInput) { in.APIHeaders = map[string]string{"X-Other": "static"} },
		"unknown {NAME}":    func(in *CustomInput) { in.APIHeaders = map[string]string{"X-Key": "{OTHER}"} },
		"safe of another":   func(in *CustomInput) { in.Keys = []CustomKey{{Name: "K", Safe: "github-token"}} },
		"id of the catalog": func(in *CustomInput) { in.ID = "linear" },
		"bad kind":          func(in *CustomInput) { in.Kind = "grpc" },
		"local unpinned":    func(in *CustomInput) { in.Kind, in.Command = CustomLocal, []string{"npx", "-y", "some-mcp@latest"} },
		"local docker":      func(in *CustomInput) { in.Kind, in.Command = CustomLocal, []string{"docker", "run", "x"} },
		"local sh":          func(in *CustomInput) { in.Kind, in.Command = CustomLocal, []string{"sh", "-c", "curl x | sh"} },
		"mcp two keys": func(in *CustomInput) {
			in.Kind, in.URL, in.Keys = CustomMCP, "https://mcp.example.com", []CustomKey{{Name: "A"}, {Name: "B"}}
		},
	}
	for name, mod := range cases {
		in := base
		in.Keys = append([]CustomKey{}, base.Keys...)
		mod(&in)
		if _, err := s.PlanCustom(c, in, nil); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A pinned local server is planned without being started; nothing runs before a yes.
	plan, err := s.PlanCustom(c, CustomInput{Kind: CustomLocal, Name: "Mine", Command: []string{"uvx", "my-mcp==1.2.3"}}, nil)
	if err != nil || !strings.Contains(strings.Join(pairValues(plan.Pairs), "\n"), "uvx my-mcp==1.2.3") {
		t.Fatalf("local plan: %v %+v", err, plan.Pairs)
	}
}

func pairValues(p []Pair) []string {
	var out []string
	for _, x := range p {
		out = append(out, x.Value)
	}
	return out
}

func TestHubConnectionAdd(t *testing.T) {
	key := "sk-live-" + "acme-" + "0123456789"
	api := internalAPI(t, key)
	s, sec := newService(t)
	s.http.Client = api.Client()
	_ = sec.Put("custom-acme-orders-api-key", key)
	ctx := context.Background()
	var asked ConfirmRequest
	answer := "done"
	h := &Hub{S: s, Confirm: func(ctx context.Context, req ConfirmRequest, wait time.Duration) (ConfirmAnswer, error) {
		if req.ProposalID != "" {
			return ConfirmAnswer{ID: req.ProposalID, State: "waiting"}, nil
		}
		asked = req
		if answer == "done" {
			res, err := req.Run(ctx)
			if err != nil {
				return ConfirmAnswer{ID: "p1", State: "failed", Result: err.Error()}, nil
			}
			return ConfirmAnswer{ID: "p1", State: "done", Result: res}, nil
		}
		return ConfirmAnswer{ID: "p1", State: answer}, nil
	}}
	args, _ := json.Marshal(apiInput(api.URL))
	// dry_run: tried, shown, nothing made and nobody asked.
	dry := strings.Replace(string(args), "{", `{"dry_run":true,`, 1)
	res, _ := json.Marshal(h.tool(ctx, "connection_add", json.RawMessage(dry)))
	if !strings.Contains(string(res), "Would make") || asked.Title != "" {
		t.Fatalf("dry run: %s %+v", res, asked)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatal("a dry run made a server")
	}
	// The person declines: nothing is made.
	answer = "declined"
	res, _ = json.Marshal(h.tool(ctx, "connection_add", args))
	if !strings.Contains(string(res), "declined") || len(asked.Pairs) == 0 {
		t.Fatalf("declined: %s", res)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatal("a declined card made a server")
	}
	// The card shows where the key comes from, never its value.
	card, _ := json.Marshal(asked.Pairs)
	if strings.Contains(string(card), key) || !strings.Contains(string(card), "custom-acme-orders-api-key") {
		t.Fatalf("card: %s", card)
	}
	// Yes: made, and the answer tells the agent how to use it.
	answer = "done"
	res, _ = json.Marshal(h.tool(ctx, "connection_add", args))
	if !strings.Contains(string(res), `api_request(connection: \"acme-orders\"`) || strings.Contains(string(res), key) {
		t.Fatalf("done: %s", res)
	}
	// An argument that is a key value is not a field of the tool.
	if b, _ := json.Marshal(connectionAddTool); strings.Contains(string(b), `"value"`) {
		t.Fatal("connection_add takes a value")
	}
	// Without the person's side the tool is not offered.
	if b, _ := json.Marshal((&Hub{S: s}).tools()); strings.Contains(string(b), "connection_add") {
		t.Fatal("offered without a confirmer")
	}
	if b, _ := json.Marshal(h.tools()); !strings.Contains(string(b), "connection_add") {
		t.Fatal("not offered")
	}
}

// A service made before its key is in the safe waits for it; keys_only copies the key in later, with no new card, and
// only from a secret the service's own name owns.
func TestCustomKeysComeLater(t *testing.T) {
	key := "sk-live-" + "acme-" + "0123456789"
	api := internalAPI(t, key)
	s, sec := newService(t)
	s.http.Client = api.Client()
	c := context.Background()
	srv, _, err := s.SaveCustom(c, apiInput(api.URL), nil)
	if err != nil || srv.State != StateSignIn {
		t.Fatalf("made without the key: %v %+v", err, srv)
	}
	h := &Hub{S: s, Confirm: func(context.Context, ConfirmRequest, time.Duration) (ConfirmAnswer, error) {
		t.Fatal("a card for keys_only")
		return ConfirmAnswer{}, nil
	}}
	call := func(args string) string {
		b, _ := json.Marshal(h.tool(c, "connection_add", json.RawMessage(args)))
		return string(b)
	}
	if got := call(`{"id":"custom-acme-orders","keys_only":true,"keys":[{"name":"API_KEY","safe":"custom-acme-orders-api-key"}]}`); !strings.Contains(got, "not in the safe yet") {
		t.Fatalf("no key yet: %s", got)
	}
	_ = sec.Put("github-token", "ghp-"+"not-this-"+"one-12345")
	if got := call(`{"id":"custom-acme-orders","keys_only":true,"keys":[{"name":"API_KEY","safe":"github-token"}]}`); !strings.Contains(got, "must be named") {
		t.Fatalf("a secret of another owner: %s", got)
	}
	_ = sec.Put("custom-acme-orders-api-key", key)
	got := call(`{"id":"custom-acme-orders","keys_only":true,"keys":[{"name":"API_KEY","safe":"custom-acme-orders-api-key"}]}`)
	if !strings.Contains(got, "Keys given") || strings.Contains(got, key) {
		t.Fatalf("keys_only: %s", got)
	}
	if again, _ := s.Get(srv.ID); again.State != StateOK {
		t.Fatalf("state after the key: %+v", again)
	}
}
