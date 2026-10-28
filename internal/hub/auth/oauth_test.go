package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// fakeIdP plays GitHub, Yandex ID and an OpenID provider at once, on one server, and checks what a real one checks:
// the PKCE verifier against the challenge the hub sent, and the client secret.
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]*grant
}

type grant struct {
	challenge, nonce string
	p                profile
}

type profile struct {
	sub, email, name string
	verified         bool
	ghEmails         []map[string]any // overrides GitHub's /user/emails when set
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{t: t, key: key, codes: map[string]*grant{}}
	mux := http.NewServeMux()
	// token endpoints (GitHub, Yandex and OpenID share the logic)
	token := func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		g := f.codes[r.Form.Get("code")]
		f.mu.Unlock()
		verifier := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if g == nil || r.Form.Get("client_secret") != "secret" || base64.RawURLEncoding.EncodeToString(verifier[:]) != g.challenge {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		out := map[string]any{"access_token": "at-" + r.Form.Get("code"), "token_type": "bearer"}
		if strings.HasPrefix(r.URL.Path, "/oidc") {
			out["id_token"] = f.idToken(g)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
	mux.HandleFunc("POST /login/oauth/access_token", token)
	mux.HandleFunc("POST /yandex/token", token)
	mux.HandleFunc("POST /oidc/token", token)
	grantOf := func(r *http.Request) *grant {
		tok := strings.TrimPrefix(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "OAuth ")
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.codes[strings.TrimPrefix(tok, "at-")]
	}
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		g := grantOf(r)
		if g == nil {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 4242, "login": "octo", "name": g.p.name})
	})
	mux.HandleFunc("GET /api/v3/user/emails", func(w http.ResponseWriter, r *http.Request) {
		g := grantOf(r)
		if g == nil {
			w.WriteHeader(401)
			return
		}
		emails := g.p.ghEmails
		if emails == nil {
			emails = []map[string]any{{"email": "noreply@users.github.com", "primary": false, "verified": true}, {"email": g.p.email, "primary": true, "verified": g.p.verified}}
		}
		_ = json.NewEncoder(w).Encode(emails)
	})
	mux.HandleFunc("GET /yandex/info", func(w http.ResponseWriter, r *http.Request) {
		g := grantOf(r)
		if g == nil {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": g.p.sub, "default_email": g.p.email, "real_name": g.p.name})
	})
	mux.HandleFunc("GET /oidc/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL + "/oidc", "authorization_endpoint": f.srv.URL + "/oidc/authorize", "token_endpoint": f.srv.URL + "/oidc/token",
			"jwks_uri": f.srv.URL + "/oidc/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}, "subject_types_supported": []string{"public"},
		})
	})
	mux.HandleFunc("GET /oidc/jwks", func(w http.ResponseWriter, r *http.Request) {
		e := big.NewInt(int64(f.key.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e),
		}}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) idToken(g *grant) string {
	b64 := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	claims := map[string]any{
		"iss": f.srv.URL + "/oidc", "aud": "oidc-client", "sub": g.p.sub, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		"nonce": g.nonce, "email": g.p.email, "email_verified": g.p.verified, "name": g.p.name,
	}
	signing := b64(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "k1"}) + "." + b64(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// oauthEnv is an environment with GitHub, Yandex and one OpenID provider ("kc") pointed at the fake.
func oauthEnv(t *testing.T, f *fakeIdP, opts ...envOpt) *testEnv {
	t.Helper()
	all := append([]envOpt{
		withEnv("HUB_GITHUB_CLIENT_ID", "gh-id"), withEnv("HUB_GITHUB_CLIENT_SECRET", "secret"), withEnv("HUB_GITHUB_URL", f.srv.URL),
		withEnv("HUB_YANDEX_CLIENT_ID", "ya-id"), withEnv("HUB_YANDEX_CLIENT_SECRET", "secret"),
		withEnv("HUB_OIDC_PROVIDERS", "kc"), withEnv("HUB_OIDC_KC_ISSUER", f.srv.URL+"/oidc"), withEnv("HUB_OIDC_KC_CLIENT_ID", "oidc-client"), withEnv("HUB_OIDC_KC_CLIENT_SECRET", "secret"),
		func(_ map[string]string, d *Deps) {
			d.Endpoints = Endpoints{YandexAuth: f.srv.URL + "/yandex/authorize", YandexToken: f.srv.URL + "/yandex/token", YandexInfo: f.srv.URL + "/yandex/info"}
		},
	}, opts...)
	return newEnv(t, all...)
}

// oauth runs the browser's half of a sign-in: start, the provider's page (here: the fake issues a code for p), and the
// callback. It returns the callback's answer and the authorization URL the hub sent the browser to.
func (e *testEnv) oauth(c *http.Client, f *fakeIdP, route, query string, p profile, tamper func(state *string, code *string)) (response, *url.URL) {
	e.t.Helper()
	start := e.do(c, "GET", "/api/auth/oauth/"+route+"/start"+query, nil)
	if start.StatusCode != 302 {
		return start, nil
	}
	loc, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		e.t.Fatal(err)
	}
	if !strings.HasPrefix(loc.Path, "/login/oauth/authorize") && !strings.Contains(loc.Path, "authorize") {
		return start, loc
	}
	q := loc.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("redirect_uri") != e.srv.URL+"/api/auth/oauth/"+route+"/callback" {
		e.t.Fatalf("authorization URL lacks PKCE/state/redirect: %s", loc)
	}
	code := "code-" + store.RandomBase32(6)
	f.mu.Lock()
	f.codes[code] = &grant{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), p: p}
	f.mu.Unlock()
	state := q.Get("state")
	if tamper != nil {
		tamper(&state, &code)
	}
	cb := e.do(c, "GET", "/api/auth/oauth/"+route+"/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	return cb, loc
}

func location(r response) string { return r.Header.Get("Location") }

func TestGitHubSignUpAndBack(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	cb, authURL := e.oauth(e.client, f, "github", "?intent=signup", profile{email: "Octo@Example.com", verified: true, name: "Octo Cat"}, nil)
	if cb.StatusCode != 302 || location(cb) != "/new" {
		t.Fatalf("callback: %d %s", cb.StatusCode, location(cb))
	}
	if !strings.HasPrefix(authURL.String(), f.srv.URL+"/login/oauth/authorize") || authURL.Query().Get("scope") != "read:user user:email" || authURL.Query().Get("client_id") != "gh-id" {
		t.Errorf("authorization URL: %s", authURL)
	}
	var me struct {
		User       UserView
		Identities []struct{ Provider, Email string }
	}
	e.get("/api/me").json(&me)
	if me.User.Email != "octo@example.com" || me.User.Name != "Octo Cat" || len(me.Identities) != 1 || me.Identities[0].Provider != "github" {
		t.Errorf("me: %+v", me)
	}

	// signing in again with GitHub is the same account
	other := e.newClient()
	cb, _ = e.oauth(other, f, "github", "", profile{email: "octo@example.com", verified: true}, nil)
	if cb.StatusCode != 302 {
		t.Fatalf("second sign-in: %d", cb.StatusCode)
	}
	var again struct{ User UserView }
	e.do(other, "GET", "/api/me", nil).json(&again)
	if again.User.ID != me.User.ID {
		t.Errorf("a new account was made: %s vs %s", again.User.ID, me.User.ID)
	}
	if n, _ := e.db.S().CountUsers(context.Background()); n != 1 {
		t.Errorf("%d users", n)
	}
}

func TestGitHubVerifiedEmailJoinsTheExistingAccount(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	byMail := e.signIn("joe@example.com")
	other := e.newClient()
	cb, _ := e.oauth(other, f, "github", "", profile{email: "joe@example.com", verified: true}, nil)
	if cb.StatusCode != 302 || strings.Contains(location(cb), "error") {
		t.Fatalf("callback: %d %s", cb.StatusCode, location(cb))
	}
	var me struct {
		User       UserView
		Identities []struct{ Provider string }
	}
	e.do(other, "GET", "/api/me", nil).json(&me)
	if me.User.ID != byMail.User.ID || len(me.Identities) != 2 {
		t.Errorf("did not join: %+v", me)
	}
}

func TestGitHubWithoutAVerifiedPrimaryEmail(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	for name, emails := range map[string][]map[string]any{
		"primary but unverified":   {{"email": "a@example.com", "primary": true, "verified": false}},
		"verified but not primary": {{"email": "a@example.com", "primary": false, "verified": true}},
		"none":                     {},
	} {
		cb, _ := e.oauth(e.newClient(), f, "github", "", profile{ghEmails: emails}, nil)
		if cb.StatusCode != 302 || location(cb) != "/signin?error=oauth_failed" {
			t.Errorf("%s: %d %s", name, cb.StatusCode, location(cb))
		}
	}
	if n, _ := e.db.S().CountUsers(context.Background()); n != 0 {
		t.Errorf("%d users were made", n)
	}
}

func TestYandexNeverJoinsByEmail(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	e.signIn("anna@example.com")
	c := e.newClient()
	cb, _ := e.oauth(c, f, "yandex", "", profile{sub: "ya-1", email: "anna@example.com", name: "Анна"}, nil)
	if cb.StatusCode != 302 || location(cb) != "/signin?error=account_exists" {
		t.Fatalf("collision: %d %s", cb.StatusCode, location(cb))
	}
	var me response = e.do(c, "GET", "/api/me", nil)
	if me.StatusCode != 401 {
		t.Errorf("signed in anyway: %d", me.StatusCode)
	}
	if _, err := e.db.S().IdentityBySubject(context.Background(), "yandex", "ya-1"); err == nil {
		t.Error("an identity was attached")
	}

	// with a free address it makes an account
	cb, _ = e.oauth(c, f, "yandex", "?intent=signup", profile{sub: "ya-2", email: "ivan@yandex.ru", name: "Иван"}, nil)
	if cb.StatusCode != 302 || location(cb) != "/new" {
		t.Fatalf("new account: %d %s", cb.StatusCode, location(cb))
	}
	var out struct{ User UserView }
	e.do(c, "GET", "/api/me", nil).json(&out)
	if out.User.Email != "ivan@yandex.ru" || out.User.Name != "Иван" {
		t.Errorf("%+v", out)
	}
}

// A person who made an account with an email they never proved must not keep a hold on it when the real owner signs
// in: that is what pre-hijacking is.
func TestPreHijackedAccountIsEmptiedByTheVerifiedOwner(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	e.signIn("owner@example.com") // the first account is the administrator's, and has to prove its address
	attacker := e.newClient()
	cb, _ := e.oauth(attacker, f, "yandex", "?intent=signup", profile{sub: "ya-evil", email: "victim@example.com"}, nil)
	if cb.StatusCode != 302 || location(cb) != "/new" {
		t.Fatalf("attacker sign-up: %d %s", cb.StatusCode, location(cb))
	}
	if r := e.do(attacker, "GET", "/api/me", nil); r.StatusCode != 200 {
		t.Fatalf("attacker session: %d", r.StatusCode)
	}

	victim := e.newClient()
	in := e.signInWith(victim, "victim@example.com")
	if in.IsNew {
		t.Error("the victim got a second account")
	}
	if r := e.do(attacker, "GET", "/api/me", nil); r.StatusCode != 401 {
		t.Errorf("the attacker's session survived: %d", r.StatusCode)
	}
	if _, err := e.db.S().IdentityBySubject(context.Background(), "yandex", "ya-evil"); err == nil {
		t.Error("the attacker's Yandex identity survived")
	}
	// and signing in with Yandex again does not give the account back
	cb, _ = e.oauth(e.newClient(), f, "yandex", "", profile{sub: "ya-evil", email: "victim@example.com"}, nil)
	if location(cb) != "/signin?error=account_exists" {
		t.Errorf("attacker returns: %s", location(cb))
	}
}

func TestOAuthStateIsBoundToTheBrowserAndUsedOnce(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	p := profile{email: "s@example.com", verified: true}

	// another state than the one this browser started
	cb, _ := e.oauth(e.client, f, "github", "", p, func(state, _ *string) { *state = "forged" })
	if location(cb) == "/new" || !strings.Contains(location(cb), "error=") {
		t.Errorf("forged state: %s", location(cb))
	}
	// the state of one browser in another (no cookie)
	victim := e.newClient()
	start := e.do(victim, "GET", "/api/auth/oauth/github/start", nil)
	loc, _ := url.Parse(start.Header.Get("Location"))
	attackerBrowser := e.newClient()
	f.mu.Lock()
	f.codes["c1"] = &grant{challenge: loc.Query().Get("code_challenge"), p: p}
	f.mu.Unlock()
	cb = e.do(attackerBrowser, "GET", "/api/auth/oauth/github/callback?code=c1&state="+loc.Query().Get("state"), nil)
	if location(cb) != "/signin?error=oauth_failed" {
		t.Errorf("state without the cookie: %s", location(cb))
	}
	if r := e.do(attackerBrowser, "GET", "/api/me", nil); r.StatusCode != 401 {
		t.Errorf("signed in without the cookie: %d", r.StatusCode)
	}
	// the victim's own callback still works, once
	cb = e.do(victim, "GET", "/api/auth/oauth/github/callback?code=c1&state="+loc.Query().Get("state"), nil)
	if location(cb) != "/new" {
		t.Fatalf("honest callback: %s", location(cb))
	}
	cb = e.do(victim, "GET", "/api/auth/oauth/github/callback?code=c1&state="+loc.Query().Get("state"), nil)
	if !strings.Contains(location(cb), "error=") {
		t.Errorf("state used twice: %s", location(cb))
	}
	// a state made for one provider does not finish another
	other := e.newClient()
	start = e.do(other, "GET", "/api/auth/oauth/github/start", nil)
	loc, _ = url.Parse(start.Header.Get("Location"))
	if cb := e.do(other, "GET", "/api/auth/oauth/yandex/callback?code=c1&state="+loc.Query().Get("state"), nil); !strings.Contains(location(cb), "error=") {
		t.Errorf("a state of another provider: %s", location(cb))
	}
	// the user denied access at the provider
	c := e.newClient()
	e.do(c, "GET", "/api/auth/oauth/github/start", nil)
	if cb := e.do(c, "GET", "/api/auth/oauth/github/callback?error=access_denied&state=x", nil); !strings.Contains(location(cb), "error=") {
		t.Errorf("denied: %s", location(cb))
	}
}

func TestPKCEVerifierIsChecked(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	// the fake provider remembers a challenge that is not the hub's: the exchange must fail
	start := e.do(e.client, "GET", "/api/auth/oauth/github/start", nil)
	loc, _ := url.Parse(start.Header.Get("Location"))
	f.mu.Lock()
	f.codes["c2"] = &grant{challenge: "not-the-challenge", p: profile{email: "p@example.com", verified: true}}
	f.mu.Unlock()
	cb := e.do(e.client, "GET", "/api/auth/oauth/github/callback?code=c2&state="+loc.Query().Get("state"), nil)
	if location(cb) != "/signin?error=oauth_failed" {
		t.Errorf("%s", location(cb))
	}
}

func TestProviderOff(t *testing.T) {
	e := newEnv(t)
	for _, route := range []string{"github", "yandex", "oidc-kc", "nonsense"} {
		r := e.get("/api/auth/oauth/" + route + "/start")
		if r.StatusCode != 302 || location(r) != "/signin?error=provider_off" {
			t.Errorf("%s: %d %s", route, r.StatusCode, location(r))
		}
	}
	if e.svc.PasskeyOn() == false {
		t.Error("passkeys should be on by default for a localhost hub")
	}
}

func TestOIDC(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	// verified email: signs up, with PKCE and a nonce in the request
	cb, authURL := e.oauth(e.client, f, "oidc-kc", "?intent=signup", profile{sub: "kc-1", email: "dora@corp.example", verified: true, name: "Дора"}, nil)
	if location(cb) != "/new" {
		t.Fatalf("oidc sign-up: %d %s", cb.StatusCode, location(cb))
	}
	q := authURL.Query()
	if q.Get("nonce") == "" || q.Get("scope") != "openid email profile" || q.Get("response_type") != "code" {
		t.Errorf("authorization URL: %s", authURL)
	}
	var me struct {
		User       UserView
		Identities []struct{ Provider string }
	}
	e.get("/api/me").json(&me)
	if me.User.Email != "dora@corp.example" || me.Identities[0].Provider != "oidc:kc" {
		t.Errorf("me: %+v", me)
	}

	// an ID token for another nonce is refused
	start := e.do(e.newClient(), "GET", "/api/auth/oauth/oidc-kc/start", nil)
	if start.StatusCode != 302 {
		t.Fatal(start.StatusCode)
	}
	c := e.newClient()
	cb, _ = e.oauth(c, f, "oidc-kc", "", profile{sub: "kc-2", email: "x@corp.example", verified: true}, func(state, code *string) {
		f.mu.Lock()
		f.codes[*code].nonce = "someone-elses-nonce"
		f.mu.Unlock()
	})
	if location(cb) != "/signin?error=oauth_failed" {
		t.Errorf("nonce mismatch: %s", location(cb))
	}

	// email_verified=false never joins an existing account
	e.signInWith(e.newClient(), "erik@corp.example")
	cb, _ = e.oauth(e.newClient(), f, "oidc-kc", "", profile{sub: "kc-3", email: "erik@corp.example", verified: false}, nil)
	if location(cb) != "/signin?error=account_exists" {
		t.Errorf("unverified collision: %s", location(cb))
	}
	// ... and the same for a verified one does
	c = e.newClient()
	cb, _ = e.oauth(c, f, "oidc-kc", "", profile{sub: "kc-4", email: "erik@corp.example", verified: true}, nil)
	if location(cb) == "" || strings.Contains(location(cb), "error") {
		t.Errorf("verified join: %s", location(cb))
	}
}

func TestOIDCDiscoveryFailureDoesNotStopTheHub(t *testing.T) {
	e := newEnv(t,
		withEnv("HUB_OIDC_PROVIDERS", "dead"), withEnv("HUB_OIDC_DEAD_ISSUER", "http://127.0.0.1:1/realm"),
		withEnv("HUB_OIDC_DEAD_CLIENT_ID", "a"), withEnv("HUB_OIDC_DEAD_CLIENT_SECRET", "b"))
	r := e.get("/api/auth/oauth/oidc-dead/start")
	if r.StatusCode != 302 || location(r) != "/signin?error=oauth_failed" {
		t.Errorf("%d %s", r.StatusCode, location(r))
	}
}

func TestLinkingWhileSignedIn(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	e.signIn("lin@example.com")

	// linking a provider whose email differs is fine: the person is signed in
	cb, _ := e.oauth(e.client, f, "github", "?intent=link", profile{email: "other-address@example.com", verified: true}, nil)
	if cb.StatusCode != 302 || location(cb) != "/settings" {
		t.Fatalf("link: %d %s", cb.StatusCode, location(cb))
	}
	var me struct {
		User       UserView
		Identities []struct{ Provider, Email string }
	}
	e.get("/api/me").json(&me)
	if me.User.Email != "lin@example.com" || len(me.Identities) != 2 {
		t.Errorf("after link: %+v", me)
	}

	// that GitHub account cannot be linked to a second person
	second := e.newClient()
	e.signInWith(second, "second@example.com")
	cb, _ = e.oauth(second, f, "github", "?intent=link", profile{email: "other-address@example.com", verified: true}, nil)
	if location(cb) != "/settings?error=identity_taken" {
		t.Errorf("taken identity: %s", location(cb))
	}

	// two methods: one can go; the last cannot
	if r := e.do(e.client, "DELETE", "/api/me/identities/github", nil); r.StatusCode != 204 {
		t.Errorf("remove github: %d %s", r.StatusCode, r.Body)
	}
	if r := e.do(e.client, "DELETE", "/api/me/identities/email", nil); r.StatusCode != 409 {
		t.Errorf("remove the last: %d", r.StatusCode)
	}

	// linking needs a session
	r := e.do(e.newClient(), "GET", "/api/auth/oauth/github/start?intent=link", nil)
	if r.StatusCode != 302 || !strings.Contains(location(r), "error=") {
		t.Errorf("link without a session: %d %s", r.StatusCode, location(r))
	}
}

func TestOAuthCannotCreateAccountsInInviteMode(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_SIGNUP", "invite"), withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))
	e.signIn("boss@example.com")
	cb, _ := e.oauth(e.newClient(), f, "github", "?intent=signup", profile{email: "walkin@example.com", verified: true}, nil)
	if location(cb) != "/signin?error=signup_closed" {
		t.Errorf("%s", location(cb))
	}
	// an existing account can still use it
	cb, _ = e.oauth(e.newClient(), f, "github", "", profile{email: "boss@example.com", verified: true}, nil)
	if strings.Contains(location(cb), "error") {
		t.Errorf("existing account refused: %s", location(cb))
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/s/acme":              "/s/acme",
		"/invite/abc?x=1":      "/invite/abc?x=1",
		"":                     "",
		"https://evil.example": "",
		"//evil.example":       "",
		"/\\evil.example":      "",
		"/ok\r\nX: y":          "",
		"relative":             "",
	} {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"/invite/tok": "tok", "/invite/tok?x=1": "tok", "/invite/": "", "/s/x": "", "": ""} {
		if got := inviteToken(in); got != want {
			t.Errorf("inviteToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	for in, want := range map[string]string{
		" Ann@Example.com ":             "ann@example.com",
		"a.b+c@mail.example.org":        "a.b+c@mail.example.org",
		"":                              "",
		"a@b":                           "",
		"a@.com":                        "",
		"two@example.com,x@example.com": "",
		"Ann <ann@example.com>":         "",
	} {
		got, ok := NormalizeEmail(in)
		if got != want || ok != (want != "") {
			t.Errorf("NormalizeEmail(%q) = %q, %v", in, got, ok)
		}
	}
}

// On https the cookie that ties a sign-in to its browser is `__Host-hl_oauth`: Secure, Path=/, no Domain. A machine's
// page on a sibling origin of the same site can set a cookie for the whole site, so a plain `hl_oauth` would let it plant
// a state of its own and have the person finish the attacker's sign-in. The browser refuses a `__Host-` cookie that
// does not come from the host itself; the hub in turn never reads the plain name there.
func TestOAuthCookieIsHostPrefixedOnHTTPS(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_PUBLIC_URL", "https://localhost:8443"), withEnv("HUB_SMTP_URL", "smtps://mail.example.com"))
	if got := e.svc.oauthCookieName(); got != "__Host-hl_oauth" {
		t.Fatalf("cookie name on https: %q", got)
	}
	if got := e.svc.RunCookieName(); got != "__Host-hl_run" {
		t.Fatalf("machine cookie name on https: %q", got)
	}
	// a client with no jar: the test sets the Cookie header itself, as a browser would
	bare := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	start := e.do(bare, "GET", "/api/auth/oauth/github/start", nil)
	if start.StatusCode != 302 {
		t.Fatalf("start: %d %s", start.StatusCode, start.Body)
	}
	var set *http.Cookie
	for _, c := range start.Cookies() {
		if c.Name == "hl_oauth" {
			t.Errorf("the unprefixed cookie is set on https: %v", c)
		}
		if c.Name == "__Host-hl_oauth" {
			set = c
		}
	}
	if set == nil {
		t.Fatalf("no __Host-hl_oauth cookie: %v", start.Header["Set-Cookie"])
	}
	if !set.Secure || !set.HttpOnly || set.Path != "/" || set.Domain != "" || set.SameSite != http.SameSiteLaxMode {
		t.Errorf("a __Host- cookie needs Secure, Path=/ and no Domain: %v", set)
	}
	loc, _ := url.Parse(start.Header.Get("Location"))
	state := loc.Query().Get("state")
	if state != set.Value {
		t.Fatalf("the cookie holds %q, the state is %q", set.Value, state)
	}
	f.mu.Lock()
	f.codes["host-1"] = &grant{challenge: loc.Query().Get("code_challenge"), p: profile{email: "h@example.com", verified: true}}
	f.mu.Unlock()
	callback := "/api/auth/oauth/github/callback?code=host-1&state=" + url.QueryEscape(state)

	// a page on a sibling origin tossed the state into the plain name: it is not the browser's own cookie
	if cb := e.do(bare, "GET", callback, nil, "Cookie", "hl_oauth="+state); location(cb) != "/signin?error=oauth_failed" {
		t.Errorf("a tossed hl_oauth was honoured: %d %s", cb.StatusCode, location(cb))
	}
	// the refusal did not use the flow up: the state is only spent by the callback that carries the real cookie
	cb := e.do(bare, "GET", callback, nil, "Cookie", "__Host-hl_oauth="+state)
	if location(cb) != "/new" {
		t.Fatalf("the honest callback: %d %s", cb.StatusCode, location(cb))
	}
	cleared := false
	for _, c := range cb.Cookies() {
		cleared = cleared || (c.Name == "__Host-hl_oauth" && c.MaxAge < 0 && c.Path == "/")
	}
	if !cleared {
		t.Errorf("the flow's cookie is not cleared with the same name and path: %v", cb.Header["Set-Cookie"])
	}
}

// Without https there is no prefix to have: the plain names stay, and the flow still works.
func TestOAuthCookieKeepsItsPlainNameWithoutHTTPS(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f)
	if e.svc.oauthCookieName() != "hl_oauth" || e.svc.RunCookieName() != "hl_run" {
		t.Errorf("names without https: %q %q", e.svc.oauthCookieName(), e.svc.RunCookieName())
	}
	cb, _ := e.oauth(e.client, f, "github", "", profile{email: "p@example.com", verified: true}, nil)
	if location(cb) != "/new" {
		t.Errorf("sign-in over http: %s", location(cb))
	}
}
