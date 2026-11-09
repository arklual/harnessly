package apptoken

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSigner(t *testing.T, issuer string) (*Signer, ed25519.PrivateKey) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	s, err := NewSigner(issuer, key)
	if err != nil {
		t.Fatal(err)
	}
	return s, key
}

func TestMintedTokenVerifiesForItsAudienceOnly(t *testing.T) {
	s, _ := newSigner(t, "https://hub.example")
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	tok, c, err := s.Mint(Claims{Subject: "u-1", Audience: "https://crm.internal", Email: "a@b.c", Org: "o-1", App: "crm", Server: "api"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Expiry.Equal(now.Add(TTL)) {
		t.Fatalf("expiry %v", c.Expiry)
	}
	got, err := Verify(tok, s.JWKS(), "https://hub.example", "https://crm.internal", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "u-1" || got.App != "crm" || got.Org != "o-1" || got.Email != "a@b.c" || got.Server != "api" {
		t.Fatalf("claims %+v", got)
	}
	for name, check := range map[string]func() error{
		"another audience": func() error {
			_, err := Verify(tok, s.JWKS(), "https://hub.example", "https://other.internal", now)
			return err
		},
		"another issuer": func() error {
			_, err := Verify(tok, s.JWKS(), "https://evil.example", "https://crm.internal", now)
			return err
		},
		"expired": func() error {
			_, err := Verify(tok, s.JWKS(), "https://hub.example", "https://crm.internal", now.Add(TTL+2*time.Minute))
			return err
		},
		"no audience asked": func() error {
			_, err := Verify(tok, s.JWKS(), "https://hub.example", "", now)
			return err
		},
		"another key": func() error {
			other, _ := newSigner(t, "https://hub.example")
			_, err := Verify(tok, other.JWKS(), "https://hub.example", "https://crm.internal", now)
			return err
		},
		"tampered": func() error {
			parts := strings.Split(tok, ".")
			_, err := Verify(parts[0]+"."+parts[1]+"x."+parts[2], s.JWKS(), "https://hub.example", "https://crm.internal", now)
			return err
		},
	} {
		if err := check(); err == nil {
			t.Errorf("%s: verified", name)
		}
	}
}

func TestMintRefusesWithoutAudienceOrSubjectAndCapsTTL(t *testing.T) {
	s, _ := newSigner(t, "https://hub.example")
	if _, _, err := s.Mint(Claims{Subject: "u"}, 0); !errors.Is(err, ErrNoAudience) {
		t.Fatalf("no audience: %v", err)
	}
	if _, _, err := s.Mint(Claims{Audience: "a"}, 0); !errors.Is(err, ErrNoSubject) {
		t.Fatalf("no subject: %v", err)
	}
	_, c, _ := s.Mint(Claims{Subject: "u", Audience: "a"}, time.Hour)
	if c.Expiry.Sub(c.IssuedAt) != MaxTTL {
		t.Fatalf("ttl %v", c.Expiry.Sub(c.IssuedAt))
	}
}

// After a rotation the old key stays in the set: a token it signed a minute ago still verifies.
func TestRotatedKeyStaysPublished(t *testing.T) {
	old, oldKey := newSigner(t, "https://hub.example")
	tok, _, _ := old.Mint(Claims{Subject: "u", Audience: "a"}, 0)
	_, newKey, _ := ed25519.GenerateKey(rand.Reader)
	rotated, _ := NewSigner("https://hub.example", newKey, oldKey.Public().(ed25519.PublicKey))
	var set struct {
		Keys []struct {
			Kid, Kty, Crv, Alg string
		}
	}
	_ = json.Unmarshal(rotated.JWKS(), &set)
	if len(set.Keys) != 2 || set.Keys[0].Kid != KeyID(newKey.Public().(ed25519.PublicKey)) || set.Keys[0].Crv != "Ed25519" || set.Keys[0].Alg != "EdDSA" {
		t.Fatalf("set %s", rotated.JWKS())
	}
	if _, err := Verify(tok, rotated.JWKS(), "https://hub.example", "a", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestKeyFileIsMadeOnceAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "app-token.pem")
	a, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreateKey(path)
	if err != nil || !a.Equal(b) {
		t.Fatalf("read back another key: %v", err)
	}
}

// A fake corporate provider that trusts the hub as an issuer: it checks the platform JWT by the hub's JWKS and answers
// its own token for the audience, the way Keycloak's token exchange does.
func fakeIdP(t *testing.T, hub *Signer) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, secret, _ := r.BasicAuth()
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if id != "hub" || secret != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		if r.PostForm.Get("grant_type") != GrantTokenExchange || r.PostForm.Get("subject_token_type") != TokenTypeJWT {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unsupported_grant_type"}`))
			return
		}
		aud := r.PostForm.Get("audience")
		if aud == "forbidden-backend" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"not for you"}`))
			return
		}
		// The provider checks the subject token for itself as its audience.
		c, err := Verify(r.PostForm.Get("subject_token"), hub.JWKS(), hub.Issuer(), "https://idp.example", time.Now())
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "idp-token-for-" + c.Subject + "@" + aud,
			"issued_token_type": TokenTypeAccess, "token_type": "Bearer", "expires_in": 300})
	}))
}

func TestExchangeGetsTheProvidersTokenForTheBackend(t *testing.T) {
	hub, _ := newSigner(t, "https://hub.example")
	idp := fakeIdP(t, hub)
	defer idp.Close()
	cfg := ExchangeConfig{TokenURL: idp.URL, ClientID: "hub", ClientSecret: "s3cret"}
	subject, _, _ := hub.Mint(Claims{Subject: "ivan", Audience: "https://idp.example", App: "crm"}, time.Minute)
	now := time.Now()

	got, err := Exchange(context.Background(), nil, cfg, subject, "crm-backend", "", now)
	if err != nil || got.AccessToken != "idp-token-for-ivan@crm-backend" || !got.ExpiresAt.Equal(now.Add(300*time.Second)) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := Exchange(context.Background(), nil, cfg, subject, "forbidden-backend", "", now); !errors.Is(err, ErrExchangeDenied) {
		t.Fatalf("refused audience: %v", err)
	}
	stranger, _ := newSigner(t, "https://hub.example")
	forged, _, _ := stranger.Mint(Claims{Subject: "ivan", Audience: "https://idp.example"}, time.Minute)
	if _, err := Exchange(context.Background(), nil, cfg, forged, "crm-backend", "", now); !errors.Is(err, ErrExchangeDenied) {
		t.Fatalf("forged subject: %v", err)
	}
	bad := cfg
	bad.ClientSecret = "wrong"
	if _, err := Exchange(context.Background(), nil, bad, subject, "crm-backend", "", now); !errors.Is(err, ErrExchangeDenied) {
		t.Fatalf("bad client: %v", err)
	}
	idp.Close()
	if _, err := Exchange(context.Background(), nil, cfg, subject, "crm-backend", "", now); !errors.Is(err, ErrIdPUnreachable) {
		t.Fatalf("provider down: %v", err)
	}
	if _, err := Exchange(context.Background(), nil, ExchangeConfig{}, subject, "crm-backend", "", now); !errors.Is(err, ErrExchangeMisconfig) {
		t.Fatalf("not set up: %v", err)
	}
}
