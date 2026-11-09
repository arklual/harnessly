package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// runtimeTokens is the token source the runtime of a machine builds, over a loopback agent with this configuration.
func runtimeTokens(t *testing.T, cfg Config) LocalAppTokens {
	t.Helper()
	cfg.LocalTokenHeader = "x-agent-local-token"
	a := &Agent{cfg: cfg, localToken: "local"}
	srv := httptest.NewServer(http.HandlerFunc(a.handler))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	data := t.TempDir()
	info, _ := json.Marshal(map[string]string{"port": u.Port(), "token": "local"})
	if err := os.WriteFile(filepath.Join(data, "agent.json"), info, 0o600); err != nil {
		t.Fatal(err)
	}
	return LocalAppTokens{DataDir: data}
}

func hubJWKS(t *testing.T, s *hubtest.Stack) []byte {
	t.Helper()
	res, err := http.Get(s.URL + apptoken.JWKSPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("jwks: %d %s", res.StatusCode, raw)
	}
	return raw
}

// The runtime asks its agent, the agent asks the hub signed with the machine's key, and the hub answers a token for the
// machine's owner, sealed; a backend checks it by the hub's JWKS and its own audience.
func TestPlatformTokenEndToEnd(t *testing.T) {
	s := hubtest.New(t)
	alice := s.SignIn("alice@example.com")
	aid, _ := alice.NewSpace("Acme")
	am := alice.Machine(aid, nil)

	tokens := runtimeTokens(t, Config{Machine: hubMachine(s, am)})
	tok, err := tokens.Token(hubproto.AppTokenRequest{Kind: hubproto.AppTokenPlatform, App: "crm", Server: "api", Audience: "https://crm.internal", Origin: "https://crm.internal"})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(tok.ExpiresAt); d <= 0 || d > apptoken.MaxTTL+time.Minute {
		t.Fatalf("expires in %v", d)
	}
	c, err := apptoken.Verify(tok.Token, hubJWKS(t, s), s.Cfg.PublicURL, "https://crm.internal", s.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if c.Email != "alice@example.com" || c.App != "crm" || c.Server != "api" || c.Space != aid || c.Machine != am.MachineID || c.Subject == "" {
		t.Fatalf("claims %+v", c)
	}
	if _, err := apptoken.Verify(tok.Token, hubJWKS(t, s), s.Cfg.PublicURL, "https://other.internal", s.Clock.Now()); err == nil {
		t.Fatal("verified for another backend")
	}

	// A platform token for another backend than the one it is sent to is refused.
	if _, err := tokens.Token(hubproto.AppTokenRequest{Kind: hubproto.AppTokenPlatform, App: "evil", Audience: "https://crm.internal", Origin: "https://evil.example"}); err == nil {
		t.Fatal("minted a token for another backend")
	}

	// A signature of another kind (the dial's) mints nothing.
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	dial, _ := hubproto.SignDial(am.Priv, am.MachineID, s.Clock.Now(), host)
	if r := s.Browser().Do("POST", hubproto.AppTokenPath, hubproto.AppTokenRequest{Kind: "platform", App: "crm", Audience: "x", Origin: "x"}, "Authorization", dial); r.Code != http.StatusUnauthorized {
		t.Fatalf("a dial signature minted a token: %s", r)
	}

	// oidc without an exchange set up: refused as off, not as a failure.
	_, err = tokens.Token(hubproto.AppTokenRequest{Kind: hubproto.AppTokenOIDC, App: "crm", Audience: "crm-backend", Origin: "https://crm.internal"})
	var te *AppTokenError
	if !errors.As(err, &te) || te.Code != hubproto.AppTokenOff {
		t.Fatalf("oidc without exchange: %v", err)
	}

	// A machine with no hub says so; the runtime then signs with its own key.
	_, err = runtimeTokens(t, Config{}).Token(hubproto.AppTokenRequest{Kind: hubproto.AppTokenPlatform, App: "crm", Audience: "a", Origin: "a"})
	if !errors.As(err, &te) || te.Code != AppTokenNoHub {
		t.Fatalf("no hub: %v", err)
	}
}

// With an exchange set up, the hub hands the corporate provider its own token for it and returns the provider's token
// for the backend; a refusal and a provider that is down come back as their codes.
func TestOIDCTokenExchangeThroughTheHub(t *testing.T) {
	var hubKeys []byte
	var issuer string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("audience") == "forbidden" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"access_denied"}`))
			return
		}
		c, err := apptoken.Verify(r.PostForm.Get("subject_token"), hubKeys, issuer, "bank-idp", time.Now())
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "bank:" + c.Email + ":" + r.PostForm.Get("audience"), "expires_in": 120})
	}))
	defer idp.Close()
	s := hubtest.New(t, hubtest.Env("HUB_APP_TOKEN_EXCHANGE_URL", idp.URL), hubtest.Env("HUB_APP_TOKEN_EXCHANGE_CLIENT_ID", "harnessly-hub"),
		hubtest.Env("HUB_APP_TOKEN_EXCHANGE_CLIENT_SECRET", "s"), hubtest.Env("HUB_APP_TOKEN_EXCHANGE_AUDIENCE", "bank-idp"),
		hubtest.Env("HUB_APP_TOKEN_EXCHANGE_AUDIENCES", "crm-backend=https://crm.internal,forbidden=https://crm.internal"))
	hubKeys, issuer = hubJWKS(t, s), s.Cfg.PublicURL
	ivan := s.SignIn("ivan@example.com")
	id, _ := ivan.NewSpace("Bank")
	tokens := runtimeTokens(t, Config{Machine: hubMachine(s, ivan.Machine(id, nil))})

	crm := func(aud, origin string) hubproto.AppTokenRequest {
		return hubproto.AppTokenRequest{Kind: hubproto.AppTokenOIDC, App: "crm", Audience: aud, Origin: origin}
	}
	tok, err := tokens.Token(crm("crm-backend", "https://crm.internal"))
	if err != nil || tok.Token != "bank:ivan@example.com:crm-backend" {
		t.Fatalf("%+v %v", tok, err)
	}
	var te *AppTokenError
	if _, err := tokens.Token(crm("forbidden", "https://crm.internal")); !errors.As(err, &te) || te.Code != hubproto.AppTokenDenied {
		t.Fatalf("refused: %v", err)
	}
	// The bank's token for the CRM, asked to be sent elsewhere, or an audience the admin never listed: refused before
	// the provider is asked.
	for _, req := range []hubproto.AppTokenRequest{crm("crm-backend", "https://evil.example"), crm("payments", "https://crm.internal")} {
		if _, err := tokens.Token(req); !errors.As(err, &te) || te.Code != hubproto.AppTokenDenied {
			t.Fatalf("%+v: %v", req, err)
		}
	}
	idp.Close()
	if _, err := tokens.Token(crm("crm-backend", "https://crm.internal")); !errors.As(err, &te) || te.Code != hubproto.AppTokenIdPUnreachable {
		t.Fatalf("provider down: %v", err)
	}
}
