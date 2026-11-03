package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const proxyHost = "ai.test"

type proxyKeys struct {
	key   *rsa.PrivateKey
	other *rsa.PrivateKey
	ec    *ecdsa.PrivateKey
	url   string
}

func newProxyKeys(t *testing.T) *proxyKeys {
	t.Helper()
	k := &proxyKeys{}
	var err error
	if k.key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		t.Fatal(err)
	}
	if k.other, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		t.Fatal(err)
	}
	if k.ec, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &k.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
			{Key: &k.ec.PublicKey, KeyID: "e1", Algorithm: "ES256", Use: "sig"},
		}})
	}))
	t.Cleanup(srv.Close)
	k.url = srv.URL
	return k
}

func (k *proxyKeys) sign(t *testing.T, key any, now time.Time, claims map[string]any) string {
	t.Helper()
	alg, kid := jose.RS256, "k1"
	if _, ok := key.(*ecdsa.PrivateKey); ok {
		alg, kid = jose.ES256, "e1"
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", kid))
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"iss": "tl.test", "aud": "gateway", "sub": "Roman@Example.com", "nbf": now.Add(-time.Second).Unix(), "exp": now.Add(time.Minute).Unix()}
	for name, v := range claims {
		if v == nil {
			delete(base, name)
		} else {
			base[name] = v
		}
	}
	raw, err := jwt.Signed(signer).Claims(base).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func proxyEnv(t *testing.T, extra ...envOpt) (*testEnv, *proxyKeys) {
	k := newProxyKeys(t)
	opts := append([]envOpt{
		withEnv("HUB_PROXY_JWT_JWKS_URL", k.url), withEnv("HUB_PROXY_JWT_AUDIENCE", "gateway"),
		withEnv("HUB_PROXY_JWT_ISSUER", "tl.test"), withEnv("HUB_PROXY_JWT_HOST", proxyHost),
	}, extra...)
	return newEnv(t, opts...), k
}

// viaGateway asks the hub as the gateway does: on its address, with the assertion and without cookies.
func (e *testEnv) viaGateway(method, path, assertion string, body any) response {
	e.t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	c.Transport = hostTransport{host: proxyHost}
	if assertion == "" {
		return e.do(c, method, path, body)
	}
	return e.do(c, method, path, body, "Teleport-Jwt-Assertion", assertion)
}

type hostTransport struct{ host string }

func (h hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = h.host
	return http.DefaultTransport.RoundTrip(r)
}

func TestProxyAssertionSignsPersonIn(t *testing.T) {
	e, k := proxyEnv(t, withEnv("HUB_ADMIN_EMAILS", "roman@example.com"))
	now := e.clock.Now()
	res := e.viaGateway("GET", "/api/me", k.sign(t, k.key, now, nil), nil)
	if res.StatusCode != 200 {
		t.Fatalf("me: %d %s", res.StatusCode, res.Body)
	}
	if c := res.Header.Values("Set-Cookie"); len(c) != 0 {
		t.Fatalf("a cookie on the gateway's address: %v", c)
	}
	var me struct{ User UserView }
	res.json(&me)
	if me.User.Email != "roman@example.com" || !me.User.IsAdmin {
		t.Fatalf("me: %+v", me.User)
	}
	// The same person next time is the same account, not a second one.
	res = e.viaGateway("GET", "/api/me", k.sign(t, k.key, now, nil), nil)
	var again struct{ User UserView }
	res.json(&again)
	if again.User.ID != me.User.ID {
		t.Fatalf("a second account: %s vs %s", again.User.ID, me.User.ID)
	}
}

// Teleport signs its assertions with ES256.
func TestProxyAssertionSignedWithEllipticKey(t *testing.T) {
	e, k := proxyEnv(t)
	if res := e.viaGateway("GET", "/api/me", k.sign(t, k.ec, e.clock.Now(), nil), nil); res.StatusCode != 200 {
		t.Fatalf("ES256 assertion: %d %s", res.StatusCode, res.Body)
	}
}

func TestProxyAssertionJoinsExistingAccount(t *testing.T) {
	e, k := proxyEnv(t)
	signed := e.signIn("roman@example.com")
	res := e.viaGateway("GET", "/api/me", k.sign(t, k.key, e.clock.Now(), nil), nil)
	var me struct{ User UserView }
	res.json(&me)
	if me.User.ID != signed.User.ID {
		t.Fatalf("the gateway's person is %s, the account is %s", me.User.ID, signed.User.ID)
	}
}

func TestProxyAssertionRefused(t *testing.T) {
	e, k := proxyEnv(t)
	now := e.clock.Now()
	for name, raw := range map[string]string{
		"none":           "",
		"another key":    k.sign(t, k.other, now, nil),
		"expired":        k.sign(t, k.key, now, map[string]any{"exp": now.Add(-time.Minute).Unix()}),
		"not yet":        k.sign(t, k.key, now, map[string]any{"nbf": now.Add(10 * time.Minute).Unix()}),
		"other audience": k.sign(t, k.key, now, map[string]any{"aud": "elsewhere"}),
		"other issuer":   k.sign(t, k.key, now, map[string]any{"iss": "evil"}),
		"no address":     k.sign(t, k.key, now, map[string]any{"sub": "roman"}),
		"garbage":        "not-a-jwt",
	} {
		if res := e.viaGateway("GET", "/api/me", raw, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d %s", name, res.StatusCode, res.Body)
		}
	}
}

func TestProxyAssertionOnlyOnGatewayAddress(t *testing.T) {
	e, k := proxyEnv(t)
	// The hub's own address does not take the assertion: a request there is signed in by its cookie or not at all.
	if res := e.get("/api/me", "Teleport-Jwt-Assertion", k.sign(t, k.key, e.clock.Now(), nil)); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("hub address took the assertion: %d", res.StatusCode)
	}
}

func TestProxyAddressIgnoresCookieAndOwnSignIn(t *testing.T) {
	e, k := proxyEnv(t)
	e.signIn("someone@example.com")
	var cookie string
	for _, c := range e.client.Jar.Cookies(mustURL(t, e.srv.URL)) {
		if c.Name == e.svc.CookieName() {
			cookie = c.Value
		}
	}
	c := &http.Client{Transport: hostTransport{host: proxyHost}}
	res := e.do(c, "GET", "/api/me", nil, "Cookie", e.svc.CookieName()+"="+cookie)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a cookie signed someone in on the gateway's address: %d", res.StatusCode)
	}
	for _, path := range []string{"/api/auth/email/start", "/api/auth/logout"} {
		if res := e.viaGateway("POST", path, k.sign(t, k.key, e.clock.Now(), nil), map[string]string{"email": "x@example.com"}); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s on the gateway's address: %d", path, res.StatusCode)
		}
	}
}

func TestProxyAssertionHonoursInviteSignup(t *testing.T) {
	e, k := proxyEnv(t, withEnv("HUB_SIGNUP", "invite"), withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))
	if res := e.viaGateway("GET", "/api/me", k.sign(t, k.key, e.clock.Now(), nil), nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a stranger got an account in invite mode: %d %s", res.StatusCode, res.Body)
	}
	res := e.viaGateway("GET", "/api/me", k.sign(t, k.key, e.clock.Now(), map[string]any{"sub": "boss@example.com"}), nil)
	if res.StatusCode != 200 {
		t.Fatalf("the admin: %d %s", res.StatusCode, res.Body)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
