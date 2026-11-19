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
	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

const linearToken = "Bearer tok-linear-secret-0451"

// fakeIssues is a remote MCP server of a tracker: a reading tool, a changing one, one that echoes the sign-in it was
// given, and one with an answer past the cap.
func fakeIssues(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != linearToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		text := func(s string) any {
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}}
		}
		var result any
		switch msg.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake"}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{"name": "list_issues", "description": "Open issues", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}},
				map[string]any{"name": "whoami", "annotations": map[string]any{"readOnlyHint": true}},
				map[string]any{"name": "big", "annotations": map[string]any{"readOnlyHint": true}},
				map[string]any{"name": "create_issue", "description": "Makes an issue"},
			}}
		case "tools/call":
			mu.Lock()
			calls = append(calls, msg.Params.Name)
			mu.Unlock()
			switch msg.Params.Name {
			case "whoami":
				result = text("signed in with " + r.Header.Get("Authorization"))
			case "big":
				result = text(strings.Repeat("x", 600<<10))
			default:
				result = text("done " + msg.Params.Name)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	}))
	t.Cleanup(ts.Close)
	return ts, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), calls...) }
}

// rewrite sends what is meant for a real host to a fake one, so an API connection of the catalog is tried without the
// network.
type rewrite struct{ to *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "api.tracker.yandex.net" {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	}
	return http.DefaultTransport.RoundTrip(req)
}

// connectionsRig is an installed app that names connections, and the person's connections on a machine of its own.
type connectionsRig struct {
	reg   *apps.Registry
	conns *appConns
	ms    *mcpconn.Service
	calls func() []string
	apiAt *[]string
}

func newConnectionsRig(t *testing.T, perms []string, needs []map[string]any) *connectionsRig {
	t.Helper()
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	ts, calls := fakeIssues(t)
	var apiAt []string
	var amu sync.Mutex
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "OAuth "+trackerKey {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		amu.Lock()
		apiAt = append(apiAt, r.Method+" "+r.URL.Path)
		amu.Unlock()
		_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `","echo":"` + trackerKey + `"}`))
	}))
	t.Cleanup(tracker.Close)
	u, _ := url.Parse(tracker.URL)

	data := t.TempDir()
	ms := mcpconn.New(data, &memSecrets{}, connections.NewHTTP(rewrite{u}))
	if _, _, err := ms.Add(context.Background(), mcpconn.AddInput{URL: ts.URL + "/mcp", CatalogID: "linear",
		Header: &mcpconn.HeaderInput{Name: "Authorization", Value: linearToken}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Add(context.Background(), mcpconn.AddInput{CatalogID: "yandex-tracker",
		Env: map[string]string{"YANDEX_TRACKER_TOKEN": trackerKey, "YANDEX_ORG_ID": "1"}}); err != nil {
		t.Fatal(err)
	}

	amu.Lock()
	apiAt = nil // the check that came with connecting is not the app's
	amu.Unlock()

	reg := apps.Open(data)
	pkg := filepath.Join(t.TempDir(), "panel")
	if err := apps.Scaffold(pkg, "acme.panel", "Panel", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"], m["connections"] = perms, needs
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ps, err := reg.Install(pkg, apps.InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	return &connectionsRig{reg: reg, conns: &appConns{ms: ms, reg: reg}, ms: ms, calls: calls, apiAt: &apiAt}
}

func code(err error) string {
	var be *apps.BridgeError
	if errors.As(err, &be) {
		return be.Code
	}
	if err == nil {
		return ""
	}
	return "other: " + err.Error()
}

var readNeeds = []map[string]any{{"id": "linear", "reason": "Shows your issues"}, {"id": "yandex-tracker"}, {"category": "mail"}}

func TestConnectionsListSaysWhereEachStandsAndHoldsNoSecret(t *testing.T) {
	rig := newConnectionsRig(t, []string{"connections.read"}, readNeeds)
	ctx := context.Background()
	list, err := rig.conns.list("acme.panel")
	if err != nil || len(list) != 3 {
		t.Fatalf("%v %+v", err, list)
	}
	by := map[string]appConnInfo{}
	for _, c := range list {
		by[c.ID+"/"+c.Category] = c
	}
	// Connected, not yet agreed to: the view shows «нет согласия». A group with no connection of the person's is a line
	// with no id: «нет подключения».
	if c := by["linear/tasks"]; c.Status != connNeedsConsent || c.Access != "" || c.Declared != apps.AccessRead || c.Kind != "mcp" || c.Reason != "Shows your issues" {
		t.Errorf("linear: %+v", c)
	}
	if c := by["yandex-tracker/tasks"]; c.Status != connNeedsConsent || c.Kind != "api" {
		t.Errorf("tracker: %+v", c)
	}
	if c := by["/mail"]; c.Status != connNotConnected || c.ID != "" {
		t.Errorf("mail: %+v", c)
	}
	if err := rig.reg.GrantConnectionAccess("acme.panel", "linear", ""); err != nil {
		t.Fatal(err)
	}
	list, _ = rig.conns.list("acme.panel")
	raw, _ := json.Marshal(list)
	for _, c := range list {
		if c.ID == "linear" && (c.Status != connReady || c.Access != apps.AccessRead) {
			t.Errorf("linear after the agreement: %+v", c)
		}
	}
	for _, leak := range []string{"tok-linear-secret", "trk-key", "127.0.0.1", "http://", "Authorization"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("the list carries %q: %s", leak, raw)
		}
	}
	_ = ctx
}

func TestConnectionCallNeedsTheAgreementAndTheManifest(t *testing.T) {
	rig := newConnectionsRig(t, []string{"connections.read"}, readNeeds)
	ctx := context.Background()
	call := func(ref, tool string) error {
		_, _, err := rig.conns.call(ctx, "acme.panel", ref, tool, nil, false)
		return err
	}
	// Signed in, the manifest names it, no agreement: refused, and nothing reaches the server.
	if got := code(call("linear", "list_issues")); got != apps.CodePermissionDenied {
		t.Fatalf("no agreement: %s", got)
	}
	// A connection the manifest does not name is refused whether or not the person has it.
	for _, ref := range []string{"notion", "yandex-calendar", "mcp-1"} {
		if got := code(call(ref, "x")); got != apps.CodePermissionDenied {
			t.Errorf("%s not named: %s", ref, got)
		}
	}
	// Named and not connected: «ждёт вход», naming the connection.
	_, _, err := rig.conns.call(ctx, "acme.panel", "yandex-mail", "x", nil, false)
	if code(err) != apps.CodeAuthRequired && code(err) != apps.CodePermissionDenied {
		t.Errorf("not connected: %v", err)
	}
	if len(rig.calls()) != 0 {
		t.Fatalf("a call went out without the agreement: %v", rig.calls())
	}
	if err := rig.reg.GrantConnection("acme.panel", "linear"); err != nil {
		t.Fatal(err)
	}
	res, _, err := rig.conns.call(ctx, "acme.panel", "linear", "list_issues", nil, false)
	if err != nil || !strings.Contains(string(res), "done list_issues") {
		t.Fatalf("a reading tool with the agreement: %s %v", res, err)
	}
	// The agreement is taken back: the next call is refused again.
	if err := rig.reg.RevokeConnection("acme.panel", "linear"); err != nil {
		t.Fatal(err)
	}
	if got := code(call("linear", "list_issues")); got != apps.CodePermissionDenied {
		t.Fatalf("after revoking: %s", got)
	}
	// A person with no connection at all: the declared id is a sign-in to make.
	empty := &appConns{ms: mcpconn.New(t.TempDir(), &memSecrets{}, nil), reg: rig.reg}
	_, _, err = empty.call(ctx, "acme.panel", "linear", "list_issues", nil, false)
	var be *apps.BridgeError
	if !errors.As(err, &be) || be.Code != apps.CodeAuthRequired || be.SignIn == nil || be.SignIn.Connection != "linear" {
		t.Fatalf("no connection: want auth_required naming linear: %#v", err)
	}
	if _, err := empty.tools(ctx, "acme.panel", "linear"); code(err) != apps.CodeAuthRequired {
		t.Fatalf("tools without a connection: %v", err)
	}
}

func TestConnectionReadOnlyKeepsChangesOut(t *testing.T) {
	rig := newConnectionsRig(t, []string{"connections.read"}, readNeeds)
	ctx := context.Background()
	if err := rig.reg.GrantConnection("acme.panel", "linear"); err != nil {
		t.Fatal(err)
	}
	// create_issue carries no readOnlyHint: it may change something, so a read app cannot call it, confirmed or not.
	for _, confirmed := range []bool{false, true} {
		if _, ask, err := rig.conns.call(ctx, "acme.panel", "linear", "create_issue", nil, confirmed); code(err) != apps.CodePermissionDenied || ask != nil {
			t.Fatalf("confirmed=%v: %v %v", confirmed, err, ask)
		}
	}
	if _, _, err := rig.conns.call(ctx, "acme.panel", "linear", "nope", nil, false); code(err) != apps.CodeInvalidParams {
		t.Fatalf("a tool the server lacks: %v", err)
	}
	if got := rig.calls(); len(got) != 0 {
		t.Fatalf("nothing went out: %v", got)
	}
	tools, err := rig.conns.tools(ctx, "acme.panel", "linear")
	if err != nil || len(tools) != 4 {
		t.Fatalf("%v %+v", err, tools)
	}
	for _, tl := range tools {
		if tl.ReadOnly != (tl.Name != "create_issue") {
			t.Errorf("readOnly of %s: %v", tl.Name, tl.ReadOnly)
		}
	}
	// An API: GET and the catalog's searches read; the rest does not.
	if err := rig.reg.GrantConnection("acme.panel", "yandex-tracker"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, path string }{{"GET", "/myself"}, {"", "/issues/A-1"}, {"POST", "/issues/_search"}} {
		ans, ask, err := rig.conns.api(ctx, "acme.panel", "yandex-tracker", c.method, c.path, nil, json.RawMessage(`{"query":"x"}`), false)
		if err != nil || ask != nil || ans.Status != 200 {
			t.Fatalf("%v: %v %v %+v", c, err, ask, ans)
		}
		if strings.Contains(ans.Body, "trk-key") {
			t.Fatalf("a key came back: %s", ans.Body)
		}
	}
	for _, c := range []struct{ method, path string }{{"POST", "/issues"}, {"PATCH", "/issues/A-1"}, {"DELETE", "/issues/A-1"}, {"POST", "/issues/_bulkchange"}} {
		if _, ask, err := rig.conns.api(ctx, "acme.panel", "yandex-tracker", c.method, c.path, nil, nil, true); code(err) != apps.CodePermissionDenied || ask != nil {
			t.Errorf("%v: %v %v", c, err, ask)
		}
	}
	if _, _, err := rig.conns.api(ctx, "acme.panel", "linear", "GET", "/x", nil, nil, false); code(err) != apps.CodeInvalidParams {
		t.Errorf("api on an MCP server: %v", err)
	}
	if _, _, err := rig.conns.call(ctx, "acme.panel", "yandex-tracker", "x", nil, false); code(err) != apps.CodeInvalidParams {
		t.Errorf("call on an API: %v", err)
	}
	if got := strings.Join(*rig.apiAt, ","); got != "GET /v3/myself,GET /v3/issues/A-1,POST /v3/issues/_search" {
		t.Fatalf("what reached the API: %s", got)
	}
}

func TestConnectionChangesWaitForThePersonsYes(t *testing.T) {
	needs := []map[string]any{{"id": "linear", "access": "all"}, {"id": "yandex-tracker", "access": "all"}}
	rig := newConnectionsRig(t, []string{"connections.read", "connections.act"}, needs)
	ctx := context.Background()
	// The person allowed reading only, though the app asks for all: a change is refused.
	if err := rig.reg.GrantConnectionAccess("acme.panel", "linear", apps.AccessRead); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rig.conns.call(ctx, "acme.panel", "linear", "create_issue", nil, true); code(err) != apps.CodePermissionDenied {
		t.Fatalf("read agreement: %v", err)
	}
	if err := rig.reg.GrantConnection("acme.panel", "linear"); err != nil {
		t.Fatal(err)
	}
	if got := rig.reg.ConnectionAccess("acme.panel", "linear"); got != apps.AccessAll {
		t.Fatalf("the default is as far as the app asks: %q", got)
	}
	// The agreement is there; the call still goes out only after the person's yes to this call: what the page shows.
	res, ask, err := rig.conns.call(ctx, "acme.panel", "linear", "create_issue", map[string]any{"title": "T"}, false)
	if err != nil || res != nil || ask == nil || ask.Connection != "linear" || ask.What != "tool create_issue" || !strings.Contains(ask.Detail, `"title":"T"`) {
		t.Fatalf("needs the yes: %s %+v %v", res, ask, err)
	}
	if len(rig.calls()) != 0 {
		t.Fatalf("it went out unasked: %v", rig.calls())
	}
	res, _, err = rig.conns.call(ctx, "acme.panel", "linear", "create_issue", map[string]any{"title": "T"}, true)
	if err != nil || !strings.Contains(string(res), "done create_issue") || strings.Join(rig.calls(), ",") != "create_issue" {
		t.Fatalf("after the yes: %s %v %v", res, err, rig.calls())
	}
	// A reading tool never asks.
	if _, ask, err := rig.conns.call(ctx, "acme.panel", "linear", "list_issues", nil, false); err != nil || ask != nil {
		t.Fatalf("a reading tool asks nothing: %v %v", ask, err)
	}
	// The same for an API.
	if err := rig.reg.GrantConnection("acme.panel", "yandex-tracker"); err != nil {
		t.Fatal(err)
	}
	_, ask, err = rig.conns.api(ctx, "acme.panel", "yandex-tracker", "POST", "/issues", nil, json.RawMessage(`{"queue":"A","summary":"s"}`), false)
	if err != nil || ask == nil || ask.What != "POST /issues" || !strings.Contains(ask.Detail, `"summary":"s"`) {
		t.Fatalf("api change: %+v %v", ask, err)
	}
	if got := strings.Join(*rig.apiAt, ","); got != "" {
		t.Fatalf("the API was reached unasked: %s", got)
	}
	if ans, _, err := rig.conns.api(ctx, "acme.panel", "yandex-tracker", "POST", "/issues", nil, json.RawMessage(`{}`), true); err != nil || ans.Status != 200 {
		t.Fatalf("after the yes: %v", err)
	}
}

func TestConnectionAnswerKeepsKeysOutAndWithinSize(t *testing.T) {
	rig := newConnectionsRig(t, []string{"connections.read"}, readNeeds)
	ctx := context.Background()
	if err := rig.reg.GrantConnection("acme.panel", "linear"); err != nil {
		t.Fatal(err)
	}
	// A server that echoes the sign-in it was given: the view gets [key], never the token.
	res, _, err := rig.conns.call(ctx, "acme.panel", "linear", "whoami", nil, false)
	if err != nil || strings.Contains(string(res), "tok-linear-secret") || !strings.Contains(string(res), "[key]") {
		t.Fatalf("a token came back: %s %v", res, err)
	}
	if _, _, err := rig.conns.call(ctx, "acme.panel", "linear", "big", nil, false); code(err) != apps.CodeQuotaExceeded {
		t.Fatalf("an answer past the cap: %v", err)
	}
}

func TestConnectionCallsAreRateLimited(t *testing.T) {
	rig := newConnectionsRig(t, []string{"connections.read"}, readNeeds)
	clock := time.Now()
	rig.conns.now = func() time.Time { return clock }
	if err := rig.reg.GrantConnection("acme.panel", "linear"); err != nil {
		t.Fatal(err)
	}
	for i := range appConnBurst {
		if _, _, err := rig.conns.call(context.Background(), "acme.panel", "linear", "list_issues", nil, false); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, _, err := rig.conns.call(context.Background(), "acme.panel", "linear", "list_issues", nil, false); code(err) != apps.CodeRateLimited {
		t.Fatalf("past the burst: %v", err)
	}
	clock = clock.Add(appConnWindow + time.Second)
	if _, _, err := rig.conns.call(context.Background(), "acme.panel", "linear", "list_issues", nil, false); err != nil {
		t.Fatalf("a window later: %v", err)
	}
}

func TestConnectionRequestAsksTheCardAndGrantKeepsTheYes(t *testing.T) {
	rig := newConnectionsRig(t, []string{"connections.read", "connections.act"}, []map[string]any{{"id": "linear", "access": "all", "reason": "Files issues"}})
	level, card, err := rig.conns.request("acme.panel", "linear", "")
	if err != nil || level != "" || card == nil || card.Access != apps.AccessAll || card.Name != "Linear" || card.Reason != "Files issues" {
		t.Fatalf("the card: %q %+v %v", level, card, err)
	}
	// Asking for read is the app's to ask: less than it declared.
	if _, card, _ := rig.conns.request("acme.panel", "linear", "read"); card == nil || card.Access != apps.AccessRead {
		t.Fatalf("read card: %+v", card)
	}
	if _, _, err := rig.conns.request("acme.panel", "linear", "root"); code(err) != apps.CodeInvalidParams {
		t.Fatalf("a level that is none: %v", err)
	}
	if got, err := rig.conns.grant("acme.panel", "linear", "read"); err != nil || got != apps.AccessRead {
		t.Fatalf("grant: %q %v", got, err)
	}
	if level, card, err := rig.conns.request("acme.panel", "linear", "read"); err != nil || card != nil || level != apps.AccessRead {
		t.Fatalf("already given: %q %+v %v", level, card, err)
	}
	if _, card, _ := rig.conns.request("acme.panel", "linear", "all"); card == nil {
		t.Fatal("the app asks for more than the person gave: the card again")
	}
	// An app granted a connection it does not name gets nothing.
	if _, err := rig.conns.grant("acme.panel", "notion", ""); err == nil {
		t.Fatal("granted a connection the manifest does not name")
	}
}

// The page's route: the person's page session only, the app and its rights from the view's session, the same model.
func TestConnectionsRouteIsThePagesAndAsksBeforeItChanges(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.panel"}
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	ts, calls := fakeIssues(t)
	if _, _, err := mcpFor(rigDeps).Add(context.Background(), mcpconn.AddInput{URL: ts.URL + "/mcp", CatalogID: "linear",
		Header: &mcpconn.HeaderInput{Name: "Authorization", Value: linearToken}}); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "panel")
	if err := apps.Scaffold(pkg, "acme.panel", "Panel", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"connections.read", "connections.act"}
	m["connections"] = []map[string]any{{"id": "linear", "access": "all"}}
	raw, _ = json.Marshal(m)
	_ = os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	code, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.panel","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &sess) != nil {
		t.Fatalf("session: %d %s", code, body)
	}
	post := func(rest string, h map[string]string) (int, string) {
		return call("POST", platformAPI+"/apps/connections", `{"session":"`+sess.Session+`",`+rest+`}`, h)
	}
	if code, _ := post(`"op":"list"`, nil); code != http.StatusUnauthorized {
		t.Errorf("the route is the page's: %d", code)
	}
	if code, body := post(`"op":"list"`, page); code != http.StatusOK || !strings.Contains(body, `"status":"needs_consent"`) || strings.Contains(body, "tok-linear") {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body := post(`"op":"call","connection":"linear","tool":"list_issues"`, page); code != http.StatusForbidden || !strings.Contains(body, "permission_denied") {
		t.Fatalf("no agreement: %d %s", code, body)
	}
	if code, body := post(`"op":"request","connection":"linear"`, page); code != http.StatusOK || !strings.Contains(body, `"needsConsent"`) || !strings.Contains(body, `"access":"all"`) {
		t.Fatalf("request: %d %s", code, body)
	}
	if code, body := post(`"op":"grant","connection":"linear","access":"all"`, page); code != http.StatusOK || !strings.Contains(body, `"granted":true`) {
		t.Fatalf("grant: %d %s", code, body)
	}
	if code, body := post(`"op":"call","connection":"linear","tool":"list_issues"`, page); code != http.StatusOK || !strings.Contains(body, "done list_issues") {
		t.Fatalf("read: %d %s", code, body)
	}
	if code, body := post(`"op":"call","connection":"linear","tool":"create_issue","args":{"title":"T"}`, page); code != http.StatusOK || !strings.Contains(body, `"needsConfirm"`) || strings.Contains(body, "done create_issue") {
		t.Fatalf("change without the yes: %d %s", code, body)
	}
	if got := strings.Join(calls(), ","); got != "list_issues" {
		t.Fatalf("what the server got: %s", got)
	}
	if code, body := post(`"op":"call","connection":"linear","tool":"create_issue","args":{"title":"T"},"confirmed":true`, page); code != http.StatusOK || !strings.Contains(body, "done create_issue") {
		t.Fatalf("change with the yes: %d %s", code, body)
	}
	if code, body := post(`"op":"nope"`, page); code != http.StatusBadRequest {
		t.Errorf("unknown op: %d %s", code, body)
	}
	// Another app's session is not this one's: a session that is not there is refused.
	if code, _ := call("POST", platformAPI+"/apps/connections", `{"session":"nope","op":"list"}`, page); code != http.StatusBadGateway {
		t.Errorf("unknown session: %d", code)
	}
}

// An app without the right gets nothing from the route, whatever its manifest names.
func TestConnectionsNeedTheRight(t *testing.T) {
	data := t.TempDir()
	reg := apps.Open(data)
	pkg := filepath.Join(t.TempDir(), "panel")
	if err := apps.Scaffold(pkg, "acme.panel", "Panel", "chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["connections"] = []map[string]any{{"id": "linear"}}
	raw, _ = json.Marshal(m)
	_ = os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644)
	if _, ps, err := reg.Install(pkg, apps.InstallOptions{Dev: true, HostVersion: "0.17.0"}); err == nil {
		t.Fatalf("a manifest that names connections without the right was installed: %v", ps)
	}
}

// trackerKey is the made-up key of the fake tracker, built so the secret scan does not take the literal for a real one.
var trackerKey = "trk-" + "key-" + "98765432"
