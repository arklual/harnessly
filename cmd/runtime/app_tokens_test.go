package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

// fakeHub mints platform tokens with its own key, as the hub does, and counts the requests.
type fakeHub struct {
	mu     sync.Mutex
	signer *apptoken.Signer
	asked  int
	fail   error
}

func (h *fakeHub) Token(req hubproto.AppTokenRequest) (agent.AppToken, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked++
	if h.fail != nil {
		return agent.AppToken{}, h.fail
	}
	raw, c, err := h.signer.Mint(apptoken.Claims{Subject: "u-ivan", Email: "ivan@example.com", Audience: req.Audience, App: req.App, Server: req.Server}, 0)
	return agent.AppToken{Token: raw, ExpiresAt: c.Expiry}, err
}

// backend is an app's MCP server that checks the platform's token the way the docs say: the hub's JWKS, its issuer,
// its own audience. revoked tokens it takes no more.
func backend(t *testing.T, jwks []byte, audience func() string, revoked *sync.Map) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := apptoken.Verify(raw, jwks, "https://hub.example", audience(), time.Now())
		if _, gone := revoked.Load(raw); err != nil || gone {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		if msg.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "hello " + c.Email + " in " + c.App}}}})
	}))
}

func TestViewCallCarriesThePlatformToken(t *testing.T) {
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := apptoken.NewSigner("https://hub.example", key)
	hub := &fakeHub{signer: signer}
	var revoked sync.Map
	var origin string
	ts := backend(t, signer.JWKS(), func() string { return origin }, &revoked)
	defer ts.Close()
	origin = ts.URL
	decl := apps.MCPServer{URL: ts.URL + "/mcp", Auth: &apps.ServerAuth{Type: apps.AuthPlatform}}
	tokens := newAppTokenSource(hub, t.TempDir())

	out, err := callWithAppToken(context.Background(), tokens, "crm", "api", decl, decl, "whoami", nil)
	if err != nil || !strings.Contains(string(out), "hello ivan@example.com in crm") {
		t.Fatalf("%s %v", out, err)
	}
	// The cached token serves the next call; one the backend revoked is renewed once and the call goes through.
	_, _ = callWithAppToken(context.Background(), tokens, "crm", "api", decl, decl, "whoami", nil)
	if hub.asked != 1 {
		t.Fatalf("asked the hub %d times", hub.asked)
	}
	cached, _ := tokens.Token(apps.AuthPlatform, "crm", "api", origin, "", "", false)
	revoked.Store(cached, true)
	if _, err := callWithAppToken(context.Background(), tokens, "crm", "api", decl, decl, "whoami", nil); err != nil || hub.asked != 2 {
		t.Fatalf("renewed: %v, asked %d", err, hub.asked)
	}

	// A package that names another backend's audience still gets a token for its own address only.
	sneaky := apps.MCPServer{URL: ts.URL + "/mcp", Auth: &apps.ServerAuth{Type: apps.AuthPlatform, Audience: "https://crm.internal"}}
	if _, err := callWithAppToken(context.Background(), tokens, "evil", "api", sneaky, sneaky, "whoami", nil); err != nil {
		t.Fatal(err)
	}
	tok, _ := tokens.Token(apps.AuthPlatform, "evil", "api", origin, "https://crm.internal", "", false)
	if _, err := apptoken.Verify(tok, signer.JWKS(), "https://hub.example", "https://crm.internal", time.Now()); err == nil {
		t.Fatal("a token for another backend")
	}
}

func TestTokenRefusalsBecomeAuthCodes(t *testing.T) {
	for _, c := range []struct {
		hubCode, want string
	}{
		{hubproto.AppTokenDenied, apps.CodeAuthDenied},
		{hubproto.AppTokenIdPUnreachable, apps.CodeIdPUnreachable},
		{hubproto.AppTokenOff, apps.CodeHostUnavailable},
		{agent.AppTokenUnavailable, apps.CodeHostUnavailable},
	} {
		tokens := newAppTokenSource(&fakeHub{fail: &agent.AppTokenError{Code: c.hubCode, Err: errors.New("x")}}, t.TempDir())
		_, err := tokens.Token(apps.AuthOIDC, "crm", "api", "https://crm.internal", "crm-backend", "", false)
		var be *apps.BridgeError
		if !errors.As(err, &be) || be.Code != c.want {
			t.Errorf("%s: want %s, got %v", c.hubCode, c.want, err)
		}
	}
}

// A machine without a hub signs a platform token with its own key and writes the JWKS for the backend's admin.
func TestNoHubSignsLocally(t *testing.T) {
	dir := t.TempDir()
	tokens := newAppTokenSource(&fakeHub{fail: &agent.AppTokenError{Code: agent.AppTokenNoHub, Err: errors.New("no hub")}}, dir)
	tok, err := tokens.Token(apps.AuthPlatform, "crm", "api", "https://crm.internal", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := os.ReadFile(filepath.Join(dir, "app-token.jwks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apptoken.Verify(tok, jwks, tokens.issuer, "https://crm.internal", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Token(apps.AuthOIDC, "crm", "api", "https://crm.internal", "crm-backend", "", false); err == nil {
		t.Fatal("exchanged without a hub")
	}
}

// An app's server with auth oauth that nobody signed in to: auth_required naming the app and the server, for «Войти».
func TestAppOAuthServerWithoutSignIn(t *testing.T) {
	ms := mcpconn.New(t.TempDir(), &memSecrets{}, nil)
	srv := apps.MCPServer{URL: "https://crm.example/mcp", Auth: &apps.ServerAuth{Type: apps.AuthOAuth}}
	_, err := callAppOAuthTool(context.Background(), ms, "crm", "api", srv, srv, "whoami", nil)
	var be *apps.BridgeError
	if !errors.As(err, &be) || be.Code != apps.CodeAuthRequired || be.SignIn == nil || be.SignIn.App != "crm" || be.SignIn.Server != "api" {
		t.Fatalf("%#v", err)
	}
}

// The person's own connection to the same address is not the app's: an oauth server of an app uses only the sign-in
// made for it, so a package cannot borrow a connection by naming its url.
func TestAppOAuthServerDoesNotBorrowTheConnection(t *testing.T) {
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	ts, calls := fakeLinear(t, map[string]string{"Bearer tok-sergey": "sergey"})
	ms := signedIn(t, ts, "Bearer tok-sergey")
	srv := apps.MCPServer{URL: ts.URL + "/mcp", Auth: &apps.ServerAuth{Type: apps.AuthOAuth}}
	_, err := callAppOAuthTool(context.Background(), ms, "evil", "api", srv, srv, "whoami", nil)
	var be *apps.BridgeError
	if !errors.As(err, &be) || be.Code != apps.CodeAuthRequired || len(*calls) != 0 {
		t.Fatalf("borrowed the connection: %v %v", err, *calls)
	}
}
