package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/codex"
)

// A token-shaped value is never written out literally (see .gitleaks.toml), so it is joined from parts.
var fakeToken = "xai-" + "session-0000-abcdef"

const codexRequest = `{
  "model": "grok-4.7",
  "instructions": "You are Codex.",
  "client_metadata": {"x": "y"},
  "tools": [
    {"type": "function", "name": "shell", "parameters": {"type": "object"}},
    {"type": "web_search", "external_web_access": true},
    {"type": "namespace", "name": "mcp__rt_preview__", "description": "The workspace's tools.",
     "tools": [{"type": "function", "name": "open", "description": "Open a thing", "parameters": {"type": "object"}}]}
  ],
  "input": [
    {"type": "reasoning", "summary": [], "content": null, "encrypted_content": "abc"},
    {"type": "function_call", "namespace": "mcp__rt_preview__", "name": "open", "call_id": "c1", "arguments": "{}"},
    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hi"}]}
  ]
}`

func TestARequestOfCodexIsRewrittenToOneXAITakes(t *testing.T) {
	out, names, err := RewriteRequest([]byte(codexRequest))
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		Instructions   string           `json:"instructions"`
		ClientMetadata any              `json:"client_metadata"`
		Tools          []map[string]any `json:"tools"`
		Input          []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatal(err)
	}
	if b.ClientMetadata != nil {
		t.Error("client_metadata goes up")
	}
	flat := FlatName("mcp__rt_preview__", "open")
	if flat != "mcp__rt_preview__open" {
		t.Fatalf("flat name %q", flat)
	}
	if len(b.Tools) != 3 || b.Tools[0]["name"] != "shell" || b.Tools[2]["name"] != flat || b.Tools[2]["type"] != "function" {
		t.Fatalf("tools %v", b.Tools)
	}
	if ws := b.Tools[1]; ws["type"] != "web_search" || len(ws) != 1 {
		t.Fatalf("web_search %v", ws)
	}
	if !strings.Contains(b.Instructions, "You are Codex.") || !strings.Contains(b.Instructions, "The workspace's tools.") {
		t.Fatalf("instructions %q", b.Instructions)
	}
	if _, ok := b.Input[0]["content"]; ok || b.Input[0]["encrypted_content"] != "abc" {
		t.Fatalf("reasoning %v", b.Input[0])
	}
	if call := b.Input[1]; call["name"] != flat || call["namespace"] != nil {
		t.Fatalf("call %v", call)
	}
	if names[flat] != (flatTool{"mcp__rt_preview__", "open"}) {
		t.Fatalf("names %v", names)
	}
}

func TestAFlatNameStaysWithinSixtyFourAndApart(t *testing.T) {
	long := strings.Repeat("n", 40)
	a, b := FlatName(long, strings.Repeat("a", 40)), FlatName(long, strings.Repeat("a", 39)+"b")
	if len(a) > 64 || len(b) > 64 || a == b {
		t.Fatalf("%q %q", a, b)
	}
}

// upstream is the vendor as the relay sees it: it records the request and answers with a stream or a body.
type upstream struct {
	auth, path, ctype string
	body              map[string]any
	stream            bool
}

func (u *upstream) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.auth, u.path, u.ctype = r.Header.Get("Authorization"), r.URL.Path, r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		u.body = nil
		_ = json.Unmarshal(raw, &u.body)
		call := `{"type":"function_call","name":"mcp__rt_preview__open","call_id":"c2","arguments":"{}"}`
		if !u.stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"r1","output":[`+call+`]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":"+call+"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":["+call+"]}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTheRelayPutsTheCredentialOnAndGivesCallsTheirNamespaceBack(t *testing.T) {
	for _, stream := range []bool{true, false} {
		u := &upstream{stream: stream}
		srv := u.server(t)
		rl := &Relay{Upstream: srv.URL + "/v1", Prefix: "/api/cli/providers/xai/v1",
			Credential: func(context.Context) (string, error) { return fakeToken, nil }}
		req := httptest.NewRequest(http.MethodPost, "/api/cli/providers/xai/v1/responses", strings.NewReader(codexRequest))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer must-not-go-up")
		rec := httptest.NewRecorder()
		rl.ServeHTTP(rec, req)
		if rec.Code != 200 || u.auth != "Bearer "+fakeToken || u.path != "/v1/responses" || u.ctype != "application/json" {
			t.Fatalf("stream=%v: %d %q %q %q", stream, rec.Code, u.auth, u.path, u.ctype)
		}
		if tools, _ := u.body["tools"].([]any); len(tools) != 3 {
			t.Fatalf("the request went up unrewritten: %v", u.body["tools"])
		}
		got := rec.Body.String()
		if strings.Contains(got, "mcp__rt_preview__open") || !strings.Contains(got, `"namespace":"mcp__rt_preview__"`) || !strings.Contains(got, `"name":"open"`) {
			t.Fatalf("stream=%v: %s", stream, got)
		}
		if stream && (strings.Count(got, `"namespace"`) != 2 || !strings.Contains(got, "data: [DONE]\n\n") || !strings.Contains(got, "event: response.completed\n")) {
			t.Fatalf("the stream changed shape: %s", got)
		}
	}
}

func TestTheRelayWithoutACredentialSaysSo(t *testing.T) {
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Store(true) }))
	defer srv.Close()
	rl := &Relay{Upstream: srv.URL, Prefix: "/p", Credential: func(context.Context) (string, error) { return "", ErrNoLogin }}
	rec := httptest.NewRecorder()
	rl.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p/models", nil))
	if rec.Code != http.StatusUnauthorized || hit.Load() || !strings.Contains(rec.Body.String(), `"type":"relay"`) {
		t.Fatalf("%d %v %s", rec.Code, hit.Load(), rec.Body)
	}
}

func TestTheRelaysServeOnlyTheRelayedProfiles(t *testing.T) {
	st := newStore(t)
	h := Relays(st, RelayPrefix)
	for _, path := range []string{RelayPrefix + "/kimi/v1/responses", RelayPrefix + "/xai/v2/models", RelayPrefix + "/nope/v1/x", RelayPrefix + "/xai/v1/../x"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	// xai is served, and without a key or a sign-in it has no credential.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, RelayPrefix+"/xai/v1/models", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d", rec.Code)
	}
}

// grokAuth writes a Grok CLI auth.json with one sign-in of xAI's issuer.
func grokAuth(t *testing.T, path, key string, expires time.Time, refresh bool) {
	t.Helper()
	e := map[string]any{"key": key, "auth_mode": "oidc", "email": "me@example.com", "expires_at": expires.UTC().Format(time.RFC3339)}
	if refresh {
		e["refresh_token"] = "r-" + key
	}
	other := map[string]any{"key": "other", "email": "other@example.com", "expires_at": expires.Add(time.Hour).UTC().Format(time.RFC3339)}
	raw, _ := json.Marshal(map[string]any{"https://auth.x.ai::1": e, "https://other.example::2": other})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheGrokSignInIsReadAndRenewedThroughTheCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	renews := 0
	g := &GrokLogin{Path: path, Now: func() time.Time { return now }, Renew: func(context.Context) error {
		renews++
		grokAuth(t, path, fakeToken+"-new", now.Add(6*time.Hour), true)
		return nil
	}}
	if _, ok := g.Account(); ok {
		t.Fatal("no file, no sign-in")
	}
	if _, err := g.Token(context.Background()); !errors.Is(err, ErrNoLogin) {
		t.Fatal(err)
	}

	grokAuth(t, path, fakeToken, now.Add(time.Hour), true)
	if acc, ok := g.Account(); !ok || acc != "me@example.com" {
		t.Fatalf("xAI's own sign-in wins: %q %v", acc, ok)
	}
	if tok, err := g.Token(context.Background()); err != nil || tok != fakeToken || renews != 0 {
		t.Fatalf("fresh: %v %d", err, renews)
	}

	grokAuth(t, path, fakeToken, now.Add(5*time.Minute), true)
	if tok, err := g.Token(context.Background()); err != nil || tok != fakeToken+"-new" || renews != 1 {
		t.Fatalf("near its end it is renewed: %v %d", err, renews)
	}

	// The CLI fails: a token that has not run out yet still goes; one that has does not.
	g.Renew = func(context.Context) error { return errors.New("offline") }
	grokAuth(t, path, fakeToken, now.Add(5*time.Minute), true)
	if tok, err := g.Token(context.Background()); err != nil || tok != fakeToken {
		t.Fatalf("last resort: %v", err)
	}
	grokAuth(t, path, fakeToken, now.Add(-time.Minute), true)
	if _, err := g.Token(context.Background()); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("run out: %v", err)
	}
	if _, ok := g.Account(); !ok {
		t.Fatal("a sign-in with a refresh token can still be renewed")
	}
	grokAuth(t, path, fakeToken, now.Add(-time.Minute), false)
	if _, ok := g.Account(); ok {
		t.Fatal("run out and nothing to renew it with")
	}
}

// signedIn is a sign-in of a vendor's CLI for the tests.
type signedIn struct{ token string }

func (s signedIn) Account() (string, bool)               { return "me@example.com", true }
func (s signedIn) Token(context.Context) (string, error) { return s.token, nil }

func TestAProfileRunsOnTheSavedKeyElseOnTheSignIn(t *testing.T) {
	st := newStore(t)
	if st.Ready("xai") {
		t.Fatal("nothing yet")
	}
	if _, err := st.Credential(context.Background(), "xai"); !errors.Is(err, ErrNoKey) {
		t.Fatal(err)
	}
	st.Logins = map[string]Login{"xai": signedIn{fakeToken}}
	if !st.Ready("xai") || st.Configured("xai") {
		t.Fatal("ready on the sign-in, no key saved")
	}
	if acc, ok := st.LoginOf("xai"); !ok || acc != "me@example.com" {
		t.Fatal(acc, ok)
	}
	if tok, err := st.Credential(context.Background(), "xai"); err != nil || tok != fakeToken {
		t.Fatal(err)
	}
	if _, err := st.Set("xai", fakeKey); err != nil {
		t.Fatal(err)
	}
	if tok, _ := st.Credential(context.Background(), "xai"); tok != fakeKey {
		t.Fatal("the saved key goes before the sign-in")
	}
	infos, err := (&Source{Store: st}).Profiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range infos.Profiles {
		if (p.ID == "xai") != (p.Login != nil) {
			t.Fatalf("%s: %v", p.ID, p.Login)
		}
	}
}

func TestACodexChatOfARelayedProfileGoesThroughTheRelay(t *testing.T) {
	st := newStore(t)
	st.Logins = map[string]Login{"xai": signedIn{fakeToken}}
	c := codexCfg(t, st, false)
	c.Profiles = Launcher{Store: st, Relay: &RelayEndpoint{Base: codex.MCPURL(8022, RelayPrefix), Header: "x-rt-token", TokenEnv: "RT_PREVIEW_TOKEN"}}
	p, _ := Find("xai")
	l := choose(p.Models[0].ID, "xai")
	line, err := c.LauncherLine("ws", "", l)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, p.KeyEnv) || strings.Contains(line, fakeToken) {
		t.Fatalf("no key goes to Codex: %q", line)
	}
	args, _, err := c.RuntimeArgs("ws", "", l)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(args, " ")
	for _, w := range []string{`model_provider="xai"`, `base_url="http://127.0.0.1:8022` + RelayPrefix + `/xai/v1"`, `env_http_headers={"x-rt-token"="RT_PREVIEW_TOKEN"}`, `wire_api="responses"`} {
		if !strings.Contains(all, w) {
			t.Errorf("no %s in %q", w, all)
		}
	}
	if strings.Contains(all, "env_key") {
		t.Fatalf("env_key: %q", all)
	}
}
