package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A backend checks the hub's platform token with a stock OIDC library: discovery by the issuer, the keys from it,
// EdDSA, its own origin as the audience.
func TestPlatformTokenChecksByDiscovery(t *testing.T) {
	s := hubtest.New(t)
	alice := s.SignIn("alice@example.com")
	aid, _ := alice.NewSpace("Acme")
	am := alice.Machine(aid, nil)
	tok, err := runtimeTokens(t, Config{Machine: hubMachine(s, am)}).Token(hubproto.AppTokenRequest{
		Kind: hubproto.AppTokenPlatform, App: "boards", Server: "boards", Audience: "https://boards.internal", Origin: "https://boards.internal"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := http.Get(s.URL + apptoken.DiscoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.NewDecoder(res.Body).Decode(&doc)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "public, max-age=300" ||
		doc["issuer"] != s.Cfg.PublicURL || doc["jwks_uri"] != s.Cfg.PublicURL+apptoken.JWKSPath {
		t.Fatalf("discovery: %d %q %v", res.StatusCode, res.Header.Get("Cache-Control"), doc)
	}

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, s.Cfg.PublicURL)
	if err != nil {
		t.Fatal(err)
	}
	v := provider.Verifier(&oidc.Config{ClientID: "https://boards.internal", SupportedSigningAlgs: []string{oidc.EdDSA}, Now: s.Clock.Now})
	id, err := v.Verify(ctx, tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Email string `json:"email"`
		Space string `json:"space"`
		App   string `json:"app"`
	}
	if err := id.Claims(&c); err != nil || id.Subject == "" || c.Email != "alice@example.com" || c.Space != aid || c.App != "boards" {
		t.Fatalf("claims %+v sub %q: %v", c, id.Subject, err)
	}
	other := provider.Verifier(&oidc.Config{ClientID: "https://other.internal", SupportedSigningAlgs: []string{oidc.EdDSA}, Now: s.Clock.Now})
	if _, err := other.Verify(ctx, tok.Token); err == nil {
		t.Fatal("verified for another backend")
	}
}

// A hub that issues no tokens has neither the keys nor the discovery document.
func TestDiscoveryOffWithoutTokens(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_APP_TOKEN", "off"))
	for _, p := range []string{apptoken.DiscoveryPath, apptoken.JWKSPath} {
		res, err := http.Get(s.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, res.StatusCode)
		}
	}
}
