package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/services"
)

// fakeKind is a product only these tests know: an instance at https://fake.test that takes the token "good-token".
const fakeKind connections.Kind = "fake-tracker"

var connFakeToken = "good" + "-token-" + strings.Repeat("Zq", 8)

type fakeProvider struct{}

func (fakeProvider) Kind() connections.Kind { return fakeKind }

func (fakeProvider) Detect(_ context.Context, _ *connections.Prober, u *url.URL) (*connections.Detection, error) {
	if u.Hostname() != "fake.test" {
		return nil, nil
	}
	return &connections.Detection{Kind: fakeKind, BaseURL: "https://fake.test", Deployment: connections.Cloud, Title: "Fake",
		Methods:      []connections.Method{{ID: "pat", Fields: []connections.Field{{ID: connections.FieldToken, Secret: true}}, Recommended: true}},
		Capabilities: []connections.Capability{connections.Issues, connections.Pages}}, nil
}

func (fakeProvider) Open(conn connections.Conn, cred connections.Credentials, _ *connections.HTTP) connections.Client {
	return &fakeClient{ok: cred[connections.FieldToken] == connFakeToken}
}

func (fakeProvider) Link(conn connections.Conn, u *url.URL) (connections.LinkTarget, bool) {
	if k, ok := strings.CutPrefix(u.Path, "/browse/"); ok {
		return connections.LinkTarget{Kind: "issue", Key: k}, true
	}
	if id, ok := strings.CutPrefix(u.Path, "/pages/"); ok {
		return connections.LinkTarget{Kind: "page", Key: id}, true
	}
	return connections.LinkTarget{}, false
}

type fakeClient struct{ ok bool }

func (c *fakeClient) Me(context.Context) (*connections.Person, error) {
	if !c.ok {
		return nil, connections.ErrDenied
	}
	return &connections.Person{ID: "u1", Login: "ann", Name: "Ann Lee"}, nil
}

func (c *fakeClient) Projects(context.Context) ([]connections.Project, error) {
	if !c.ok {
		return nil, connections.ErrDenied
	}
	return []connections.Project{{Key: "FK", Name: "Fake"}}, nil
}

func (c *fakeClient) Search(_ context.Context, q connections.Query) ([]connections.Issue, error) {
	if !c.ok {
		return nil, connections.ErrDenied
	}
	if q.Text == "none" {
		return nil, nil
	}
	return []connections.Issue{{Key: "FK-1", Title: "First " + q.Project, URL: "https://fake.test/browse/FK-1",
		Status: connections.Status{Name: "Open", Category: connections.Todo}}}, nil
}

func (c *fakeClient) Issue(_ context.Context, key string) (*connections.Issue, error) {
	if !c.ok {
		return nil, connections.ErrDenied
	}
	if key != "FK-1" {
		return nil, connections.ErrNotFound
	}
	return &connections.Issue{Key: "FK-1", Title: "First", URL: "https://fake.test/browse/FK-1", Description: "Body"}, nil
}

func (c *fakeClient) SearchPages(context.Context, connections.Query) ([]connections.Page, error) {
	return []connections.Page{{ID: "7", Title: "Runbook", URL: "https://fake.test/pages/7"}}, nil
}

func (c *fakeClient) Page(_ context.Context, id string) (*connections.Page, error) {
	return &connections.Page{ID: id, Title: "Runbook", URL: "https://fake.test/pages/" + id, Body: "steps"}, nil
}

func init() { connections.Register(fakeProvider{}) }

type connCall func(method, path, body string, h map[string]string) (int, map[string]any)

func connRig(t *testing.T, mutate func(*services.Config)) (connCall, deps) {
	t.Helper()
	dir, state := t.TempDir(), t.TempDir()
	t.Setenv("RUNTIME_SECRETS_KEY_FILE", filepath.Join(state, "safe.key"))
	t.Setenv("RUNTIME_SECRETS_FILES_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	cfg := services.Config{Home: d.Home}
	if mutate != nil {
		mutate(&cfg)
	}
	sf := safeFor(d)
	t.Cleanup(sf.files.Close)
	sec := &connSecrets{store: sf.store}
	cs := connections.New(dir, sec, nil, envConnections(cfg, testNow)...)
	sec.svc = cs
	routes := connectionRoutesOf(d, cs)
	return func(method, path, body string, h map[string]string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		for k, v := range h {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(req) {
				rt.Handler.ServeHTTP(w, req)
				var out map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &out)
				return w.Code, out
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, nil
	}, d
}

var (
	connPage = map[string]string{"X-Test-Session": "yes"}
	connCLI  = map[string]string{"x-cli-token": "tok"}
	testNow  = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
)

func TestConnectionRoutesConnectReadAndForget(t *testing.T) {
	logs := captureStdio(t)
	call, d := connRig(t, nil)
	base := platformAPI + "/connections"

	if code, out := call("POST", base+"/detect", `{"url":"fake.test/browse/FK-1"}`, connPage); code != 200 ||
		out["detection"].(map[string]any)["kind"] != string(fakeKind) {
		t.Fatalf("detect: %d %v", code, out)
	}
	if code, out := call("POST", base+"/detect", `{"url":"ftp://x"}`, connPage); code != 422 || out["error"] != "unknown-product" {
		t.Fatalf("not a web address: %d %v", code, out)
	}
	det := `{"kind":"fake-tracker","baseUrl":"https://fake.test","deployment":"cloud","methods":[{"id":"pat","fields":[{"id":"token","secret":true}]}],"capabilities":["issues","pages"]}`
	if code, out := call("POST", base, `{"detection":`+det+`,"method":"pat","credentials":{"token":"wrong"}}`, connPage); code != 424 || out["error"] != "denied" {
		t.Fatalf("wrong token: %d %v", code, out)
	}
	code, out := call("POST", base, `{"detection":`+det+`,"method":"pat","credentials":{"token":"`+connFakeToken+`"}}`, connPage)
	if code != 200 {
		t.Fatalf("connect: %d %v", code, out)
	}
	conn := out["connection"].(map[string]any)
	id := conn["id"].(string)
	if conn["state"] != "ok" || conn["name"] != "fake.test" {
		t.Fatalf("connection %v", conn)
	}
	// The credentials are in the safe under conn-<id>, nowhere else.
	if !safeFor(d).store.Has("conn-" + id) {
		t.Fatal("no secret in the safe")
	}
	raw, _ := os.ReadFile(filepath.Join(d.DataDir, "connections.json"))
	if strings.Contains(string(raw), connFakeToken) {
		t.Fatal("the token is in connections.json")
	}

	code, out = call("GET", base, "", connPage)
	if code != 200 || len(out["connections"].([]any)) != 1 || len(out["kinds"].([]any)) == 0 {
		t.Fatalf("list: %d %v", code, out)
	}
	if code, out := call("GET", base+"/"+id+"/issues?q=x&project=FK&mine=1", "", connPage); code != 200 ||
		out["issues"].([]any)[0].(map[string]any)["title"] != "First FK" {
		t.Fatalf("issues: %d %v", code, out)
	}
	if code, out := call("GET", base+"/"+id+"/issues?q=none", "", connPage); code != 200 || out["issues"] == nil {
		t.Fatalf("no issues is an empty list: %d %v", code, out)
	}
	if code, out := call("GET", base+"/"+id+"/issue?key=FK-9", "", connPage); code != 404 || out["error"] != "not-found" {
		t.Fatalf("missing issue: %d %v", code, out)
	}
	if code, out := call("GET", base+"/"+id+"/projects", "", connPage); code != 200 || len(out["projects"].([]any)) != 1 {
		t.Fatalf("projects: %d %v", code, out)
	}
	if code, out := call("GET", base+"/"+id+"/page?id=7", "", connPage); code != 200 || out["page"].(map[string]any)["body"] != "steps" {
		t.Fatalf("page: %d %v", code, out)
	}
	if code, out := call("GET", base+"/resolve?url="+url.QueryEscape("https://fake.test/browse/FK-1"), "", connPage); code != 200 ||
		out["issue"].(map[string]any)["key"] != "FK-1" || out["target"].(map[string]any)["kind"] != "issue" {
		t.Fatalf("resolve: %d %v", code, out)
	}
	if code, _ := call("GET", base+"/resolve?url="+url.QueryEscape("https://elsewhere.test/browse/FK-1"), "", connPage); code != 404 {
		t.Fatalf("a link of no connection: %d", code)
	}
	if code, out := call("PATCH", base+"/"+id, `{"name":"Mine","primary":true}`, connPage); code != 200 ||
		out["connection"].(map[string]any)["name"] != "Mine" || out["connection"].(map[string]any)["primary"] != true {
		t.Fatalf("patch: %d %v", code, out)
	}
	if code, out := call("POST", base+"/"+id+"/check", "", connPage); code != 200 || out["connection"].(map[string]any)["state"] != "ok" {
		t.Fatalf("check: %d %v", code, out)
	}

	// The CLI reads, and only reads.
	if code, out := call("GET", "/api/cli/connections", "", connCLI); code != 200 || len(out["connections"].([]any)) != 1 {
		t.Fatalf("cli list: %d %v", code, out)
	}
	if code, out := call("GET", "/api/cli/connections/"+id+"/issue?key=FK-1", "", connCLI); code != 200 || out["issue"].(map[string]any)["description"] != "Body" {
		t.Fatalf("cli issue: %d %v", code, out)
	}
	if code, _ := call("DELETE", "/api/cli/connections/"+id, "", connCLI); code != 405 {
		t.Fatalf("cli delete: %d", code)
	}
	if code, _ := call("GET", "/api/cli/connections", "", map[string]string{"x-cli-token": "nope"}); code != 403 {
		t.Fatalf("cli without its token: %d", code)
	}
	// The page's routes want a session of the same origin; the cli-token is not one.
	if code, _ := call("GET", base, "", connCLI); code != 401 {
		t.Fatalf("page route with the cli token: %d", code)
	}
	if code, _ := call("DELETE", base+"/"+id, "", map[string]string{"X-Test-Session": "yes", "Sec-Fetch-Site": "cross-site"}); code != 403 {
		t.Fatalf("cross-site delete: %d", code)
	}

	if code, _ := call("DELETE", base+"/"+id, "", connPage); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if safeFor(d).store.Has("conn-" + id) {
		t.Fatal("the secret stayed")
	}
	if code, _ := call("GET", base+"/"+id+"/issues", "", connPage); code != 404 {
		t.Fatalf("a removed connection: %d", code)
	}
	if text := logs(); strings.Contains(text, connFakeToken) || strings.Contains(text, "wrong") {
		t.Fatalf("a credential reached the log: %s", text)
	}
}

func TestConnectionRoutesListTheDeploymentsTrackerReadOnly(t *testing.T) {
	t.Setenv("YOUTRACK_TOKEN", "")
	t.Setenv("GITLAB_SELF_TOKEN", "")
	call, _ := connRig(t, func(c *services.Config) {
		c.TrackerURL = "https://tracker.example.com/"
		c.GitLabHosts = []string{"gitlab.example.com", "gitlab.com"}
	})
	base := platformAPI + "/connections"
	code, out := call("GET", base, "", connPage)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	list := out["connections"].([]any)
	if len(list) != 2 {
		t.Fatalf("%v", list)
	}
	tr := list[0].(map[string]any)
	if tr["id"] != envTrackerID || tr["kind"] != "youtrack" || tr["baseUrl"] != "https://tracker.example.com" || tr["env"] != true ||
		tr["name"] != "tracker.example.com" || tr["deployment"] != "self-hosted" {
		t.Fatalf("tracker %v", tr)
	}
	if gl := list[1].(map[string]any); gl["id"] != envGitLabID || gl["kind"] != "gitlab" || gl["baseUrl"] != "https://gitlab.example.com" {
		t.Fatalf("gitlab %v", gl)
	}
	if code, out := call("DELETE", base+"/"+envTrackerID, "", connPage); code != 409 || out["error"] != "read-only" {
		t.Fatalf("delete env: %d %v", code, out)
	}
	if code, out := call("PATCH", base+"/"+envTrackerID, `{"name":"x"}`, connPage); code != 409 {
		t.Fatalf("rename env: %d %v", code, out)
	}
	// It can still be the primary tracker.
	if code, out := call("PATCH", base+"/"+envTrackerID, `{"primary":true}`, connPage); code != 200 || out["connection"].(map[string]any)["primary"] != true {
		t.Fatalf("primary env: %d %v", code, out)
	}
	code, out = call("GET", base, "", connPage)
	if code != 200 || out["connections"].([]any)[0].(map[string]any)["primary"] != true {
		t.Fatalf("primary is kept: %v", out)
	}
}

func TestEnvConnectionsReadTheTrackersToken(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config/tok"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config/tok/youtrack-token"), []byte("perm:file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOUTRACK_TOKEN", "")
	cfg := services.Config{Home: home, SecretDirs: []string{".config/tok"}, TrackerURL: "https://acme.youtrack.cloud"}
	env := envConnections(cfg, testNow)
	if len(env) != 1 || env[0].Conn.Deployment != connections.Cloud {
		t.Fatalf("%+v", env)
	}
	if got := env[0].Cred()[connections.FieldToken]; got != "perm:file" {
		t.Fatalf("file token %q", got)
	}
	t.Setenv("YOUTRACK_TOKEN", "perm:env")
	if got := env[0].Cred()[connections.FieldToken]; got != "perm:env" {
		t.Fatalf("a rotated token is taken at once: %q", got)
	}
	if len(envConnections(services.Config{TrackerURL: "https://user:pw@tracker.example.com"}, testNow)) != 0 {
		t.Fatal("an address with credentials is no connection")
	}
}

func TestWebConfigCarriesConnectionHosts(t *testing.T) {
	if c := pageConfigFromEnv(); c.ConnectionHosts == nil || len(c.ConnectionHosts) != 0 {
		t.Fatalf("%v", c.ConnectionHosts)
	}
	cfg := services.Config{TrackerURL: "https://Tracker.example.com:8443", GitLabHosts: []string{"gitlab.example.com"}}
	cs := connections.New(t.TempDir(), nil, nil, envConnections(cfg, testNow)...)
	hosts := connectionHosts(cs)
	if strings.Join(hosts, ",") != "tracker.example.com:8443,gitlab.example.com" {
		t.Fatalf("%v", hosts)
	}
	w := newWebConfig(fstest.MapFS{"index.html": {Data: []byte(`<meta name="hl-config" content="{}" />`)}})
	w.connHosts = func() []string { return hosts }
	raw, err := w.page()
	if err != nil || !strings.Contains(string(raw), `&#34;connectionHosts&#34;:[&#34;tracker.example.com:8443&#34;,&#34;gitlab.example.com&#34;]`) {
		t.Fatalf("%s %v", raw, err)
	}
}
