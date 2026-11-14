package chatgpt

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// memVault is a safe in memory.
type memVault struct {
	mu sync.Mutex
	m  map[string]string
}

func (v *memVault) PutIntegration(i, n, val string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.m[i+"/"+n] = val
	return nil
}

func (v *memVault) RevealIntegration(i, n string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	val, ok := v.m[i+"/"+n]
	if !ok {
		return "", &secrets.Refusal{Reason: secrets.ReasonNotFound}
	}
	return val, nil
}

func (v *memVault) RemoveIntegration(i, n string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.m, i+"/"+n)
	return nil
}

// fakeOpenAI is OpenAI's auth server and API, enough for the flow.
type fakeOpenAI struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu sync.Mutex
	// issued is the client a new registration gets; subject and email the account's.
	issued, subject, email string
	// codes are the authorization codes handed out: code → what it was asked with.
	codes map[string]codeGrant
	// refreshes counts refresh grants; refresh is the current refresh token; refreshFail the error to answer.
	refreshes   int
	refresh     string
	refreshFail string
	revoked     []url.Values
	// responses is what POST /v1/responses answers: a status and an event stream (or a JSON body).
	respStatus int
	respBody   string
	requests   []map[string]any
	scope      string
	idAudience string // overrides the ID token's audience
	idNonce    string // overrides the ID token's nonce
}

type codeGrant struct {
	challenge, redirect, client, nonce string
}

func newFake(t *testing.T) *fakeOpenAI {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOpenAI{t: t, key: key, issued: "oaiapp_test1", subject: "user-123", email: "person@example.com", codes: map[string]codeGrant{}, refresh: "rt-1", scope: scopes, respStatus: 200}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOpenAI) endpoints() Endpoints { return Endpoints{Auth: f.srv.URL, API: f.srv.URL + "/v1"} }

func (f *fakeOpenAI) idToken(aud, nonce string) string {
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: f.key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		f.t.Fatal(err)
	}
	now := time.Now()
	claims := map[string]any{
		"iss": f.srv.URL, "sub": f.subject, "aud": aud, "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
		"nonce": nonce, "email": f.email,
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus"},
	}
	raw, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		f.t.Fatal(err)
	}
	return raw
}

func (f *fakeOpenAI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": f.srv.URL, "jwks_uri": f.srv.URL + "/jwks", "revocation_endpoint": f.srv.URL + "/oauth/revoke"})
	case "/jwks":
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	case "/api/accounts/oauth/token":
		_ = r.ParseForm()
		f.token(w, r.PostForm)
	case "/oauth/revoke":
		_ = r.ParseForm()
		f.revoked = append(f.revoked, r.PostForm)
		w.WriteHeader(http.StatusOK)
	case "/v1/models":
		if r.Header.Get("Authorization") != "Bearer at-"+fmt.Sprint(f.refreshes) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"models":[
			{"slug":"gpt-6.1-sol","display_name":"GPT-6.1 Sol","visibility":"list","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"}],"default_reasoning_level":"medium","input_modalities":["text","image"]},
			{"slug":"hidden-one","display_name":"Hidden","visibility":"hide"},
			{"slug":"gpt-mini","display_name":"GPT mini","visibility":"list"}]}`)
	case "/v1/responses":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["_auth"] = r.Header.Get("Authorization")
		f.requests = append(f.requests, body)
		if f.respStatus != 200 {
			w.WriteHeader(f.respStatus)
			_, _ = io.WriteString(w, f.respBody)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, f.respBody)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeOpenAI) token(w http.ResponseWriter, form url.Values) {
	bad := func(code string) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
	}
	if form.Get("resource") != resource {
		bad("invalid_target")
		return
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		g, ok := f.codes[form.Get("code")]
		if !ok {
			bad("invalid_grant")
			return
		}
		delete(f.codes, form.Get("code"))
		if challengeOf(form.Get("code_verifier")) != g.challenge || form.Get("redirect_uri") != g.redirect {
			bad("invalid_grant")
			return
		}
		client := g.client
		if client == dynamicClient {
			client = f.issued
		}
		if form.Get("client_id") != client {
			bad("invalid_client")
			return
		}
		aud, nonce := client, g.nonce
		if f.idAudience != "" {
			aud = f.idAudience
		}
		if f.idNonce != "" {
			nonce = f.idNonce
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-0", "refresh_token": f.refresh, "id_token": f.idToken(aud, nonce), "token_type": "Bearer", "expires_in": 3600, "scope": f.scope})
	case "refresh_token":
		if f.refreshFail != "" {
			bad(f.refreshFail)
			return
		}
		if form.Get("refresh_token") != f.refresh || form.Get("client_id") != f.issued {
			bad("refresh_token_reused")
			return
		}
		f.refreshes++
		f.refresh = fmt.Sprintf("rt-%d", f.refreshes+1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("at-%d", f.refreshes), "refresh_token": f.refresh, "token_type": "Bearer", "expires_in": 3600, "scope": f.scope})
	default:
		bad("unsupported_grant_type")
	}
}

// authorize stands in for the person's browser at the authorize endpoint: it reads the address and hands out a code,
// answering with the callback address the browser would land on.
func (f *fakeOpenAI) authorize(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	f.mu.Lock()
	code := "code-" + randomToken(6)
	f.codes[code] = codeGrant{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), client: q.Get("client_id"), nonce: q.Get("nonce")}
	f.mu.Unlock()
	cb := url.Values{"code": {code}, "state": {q.Get("state")}, "scope": {scopes}}
	if q.Get("client_id") == dynamicClient {
		cb.Set("client_id", f.issued)
	}
	return q.Get("redirect_uri") + "?" + cb.Encode()
}

// newTestService is a service on the fake, its callback on a free loopback port.
func newTestService(t *testing.T, f *fakeOpenAI) (*Service, *memVault) {
	v := &memVault{m: map[string]string{}}
	s := New(Config{Dir: t.TempDir(), Vault: v, Endpoints: f.endpoints(), Listen: func(int) (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	}})
	return s, v
}

func streamOf(deltas ...string) string {
	var b strings.Builder
	b.WriteString("event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
	for _, d := range deltas {
		raw, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": d})
		b.WriteString("event: response.output_text.delta\ndata: " + string(raw) + "\n\n")
	}
	b.WriteString("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{}}\n\n")
	return b.String()
}
