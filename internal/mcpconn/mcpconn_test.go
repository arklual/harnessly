package mcpconn

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memSecrets is the safe in memory.
type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func newSecrets() *memSecrets { return &memSecrets{m: map[string]string{}} }

func (m *memSecrets) Put(name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[name] = value
	return nil
}

func (m *memSecrets) Reveal(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *memSecrets) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.m[name]; !ok {
		return ErrNotFound
	}
	delete(m.m, name)
	return nil
}

// fakeMCP answers streamable HTTP MCP at /mcp.
type fakeMCP struct {
	t *testing.T
	// allow says whether a request is signed in; nil allows all.
	allow func(r *http.Request) bool
	// challenge is the WWW-Authenticate of a 401.
	challenge string
	// sse answers requests with an event stream instead of JSON.
	sse bool
}

func (f *fakeMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if f.allow != nil && !f.allow(r) {
		if f.challenge != "" {
			w.Header().Set("WWW-Authenticate", f.challenge)
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Cursor string `json:"cursor"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		f.t.Errorf("Accept = %q", r.Header.Get("Accept"))
	}
	var result any
	switch msg.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "sess-1")
		result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fake", "title": "Fake Server", "version": "1"},
		}
	case "notifications/initialized":
		if r.Header.Get("Mcp-Session-Id") != "sess-1" {
			f.t.Errorf("initialized without session id")
		}
		w.WriteHeader(http.StatusAccepted)
		return
	case "tools/list":
		if r.Header.Get("Mcp-Session-Id") != "sess-1" {
			f.t.Errorf("tools/list without session id")
		}
		if r.Header.Get("MCP-Protocol-Version") != protocolVersion {
			f.t.Errorf("tools/list protocol version %q", r.Header.Get("MCP-Protocol-Version"))
		}
		switch msg.Params.Cursor {
		case "":
			result = map[string]any{"tools": []map[string]any{
				{"name": "search", "description": "Search things.\nMore lines here."},
				{"name": "read", "description": "Read one thing."},
			}, "nextCursor": "p2"}
		case "p2":
			result = map[string]any{"tools": []map[string]any{{"name": "write", "description": "Write one thing."}}}
		default:
			f.t.Errorf("cursor %q", msg.Params.Cursor)
		}
	default:
		f.t.Errorf("unexpected method %q", msg.Method)
		return
	}
	reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	if f.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, ": comment\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", reply)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(reply)
}

// fakeAS is an authorization server with dynamic registration, PKCE and refresh.
type fakeAS struct {
	t *testing.T
	// base is the server's URL, set once it listens.
	base string
	// noRegister leaves registration out of the metadata.
	noRegister bool
	// openID serves only /.well-known/openid-configuration.
	openID bool
	// pathPrefix is the issuer's path ("" or "/tenant").
	pathPrefix string
	// expiresIn of issued tokens: first for the code exchange, then for refreshes.
	expiresIn, refreshExpiresIn int64
	resource                    string
	// issueSecret makes registration hand out a client secret.
	issueSecret bool

	mu         sync.Mutex
	registered int
	clients    map[string]string // id → secret
	codes      map[string]codeGrant
	access     map[string]bool
	refresh    map[string]bool
	refreshes  atomic.Int32
	n          int
	codeN      int
}

type codeGrant struct {
	challenge, redirect, resource, clientID string
}

func newAS(t *testing.T) *fakeAS {
	return &fakeAS{t: t, expiresIn: 3600, refreshExpiresIn: 3600, clients: map[string]string{}, codes: map[string]codeGrant{},
		access: map[string]bool{}, refresh: map[string]bool{}}
}

func (a *fakeAS) issuer() string { return a.base + a.pathPrefix }

func (a *fakeAS) metadata() map[string]any {
	m := map[string]any{
		"issuer":                           a.issuer(),
		"authorization_endpoint":           a.base + a.pathPrefix + "/authorize",
		"token_endpoint":                   a.base + a.pathPrefix + "/token",
		"code_challenge_methods_supported": []string{"S256"},
	}
	if !a.noRegister {
		m["registration_endpoint"] = a.base + a.pathPrefix + "/register"
	}
	return m
}

func (a *fakeAS) mount(mux *http.ServeMux) {
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if a.openID {
		mux.HandleFunc("GET /.well-known/openid-configuration"+a.pathPrefix, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, a.metadata())
		})
	} else {
		mux.HandleFunc("GET /.well-known/oauth-authorization-server"+a.pathPrefix, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, a.metadata())
		})
	}
	mux.HandleFunc("POST "+a.pathPrefix+"/register", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClientName   string   `json:"client_name"`
			RedirectURIs []string `json:"redirect_uris"`
			GrantTypes   []string `json:"grant_types"`
			AuthMethod   string   `json:"token_endpoint_auth_method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.ClientName != "harnessly" || len(in.RedirectURIs) != 1 || in.AuthMethod != "none" || len(in.GrantTypes) != 2 {
			a.t.Errorf("registration %+v", in)
		}
		a.mu.Lock()
		a.registered++
		id := fmt.Sprintf("client-%d", a.registered)
		out := map[string]any{"client_id": id}
		if a.issueSecret {
			a.clients[id] = "shh-" + id
			out["client_secret"] = a.clients[id]
			out["token_endpoint_auth_method"] = "client_secret_post"
		} else {
			a.clients[id] = ""
		}
		a.mu.Unlock()
		writeJSON(w, 201, out)
	})
	mux.HandleFunc("POST "+a.pathPrefix+"/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		a.mu.Lock()
		defer a.mu.Unlock()
		fail := func(code string) { writeJSON(w, 400, map[string]string{"error": code}) }
		clientID := r.PostForm.Get("client_id")
		secret, known := a.clients[clientID]
		if !known || r.PostForm.Get("client_secret") != secret {
			writeJSON(w, 401, map[string]string{"error": "invalid_client"})
			return
		}
		if r.PostForm.Get("resource") != a.resource {
			a.t.Errorf("token resource %q, want %q", r.PostForm.Get("resource"), a.resource)
		}
		var expires int64
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			g, ok := a.codes[r.PostForm.Get("code")]
			delete(a.codes, r.PostForm.Get("code"))
			if !ok {
				fail("invalid_grant")
				return
			}
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
				a.t.Errorf("code_verifier does not match the challenge")
				fail("invalid_grant")
				return
			}
			if g.redirect != r.PostForm.Get("redirect_uri") || g.clientID != clientID {
				a.t.Errorf("redirect or client changed")
			}
			expires = a.expiresIn
		case "refresh_token":
			rt := r.PostForm.Get("refresh_token")
			if !a.refresh[rt] {
				fail("invalid_grant")
				return
			}
			delete(a.refresh, rt)
			a.refreshes.Add(1)
			expires = a.refreshExpiresIn
		default:
			fail("unsupported_grant_type")
			return
		}
		a.n++
		at, rt := fmt.Sprintf("at-%d", a.n), fmt.Sprintf("rt-%d", a.n)
		a.access[at], a.refresh[rt] = true, true
		writeJSON(w, 200, map[string]any{"access_token": at, "token_type": "Bearer", "expires_in": expires, "refresh_token": rt})
	})
}

// authorize plays the person signing in at the authorization URL: it checks the request and answers a code.
func (a *fakeAS) authorize(authURL string) (state, code string) {
	u, err := url.Parse(authURL)
	if err != nil {
		a.t.Fatal(err)
	}
	q := u.Query()
	if u.Path != a.pathPrefix+"/authorize" || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		a.t.Fatalf("authorize URL %s", authURL)
	}
	if len(q.Get("code_challenge")) != 43 || len(q.Get("state")) < 40 {
		a.t.Fatalf("challenge or state too short in %s", authURL)
	}
	if q.Get("resource") != a.resource {
		a.t.Fatalf("resource %q, want %q", q.Get("resource"), a.resource)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.clients[q.Get("client_id")]; !ok {
		a.t.Fatalf("unknown client %q", q.Get("client_id"))
	}
	a.codeN++
	code = fmt.Sprintf("code-%d", a.codeN)
	a.codes[code] = codeGrant{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), resource: q.Get("resource"), clientID: q.Get("client_id")}
	return q.Get("state"), code
}

func (a *fakeAS) valid(r *http.Request) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.access[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
}

func newService(t *testing.T) (*Service, *memSecrets) {
	t.Helper()
	sec := newSecrets()
	return New(t.TempDir(), sec, nil), sec
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// oauthServer is an MCP server at /mcp with its protected resource metadata and authorization server on one origin.
func oauthServer(t *testing.T, as *fakeAS) *httptest.Server {
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	as.base = ts.URL
	as.resource = ts.URL + "/mcp"
	as.mount(mux)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource": ts.URL + "/mcp", "authorization_servers": []string{as.issuer()}, "scopes_supported": []string{"read", "write"},
		})
	})
	mux.Handle("/mcp", &fakeMCP{t: t, allow: as.valid, challenge: `Bearer realm="mcp"`})
	return ts
}

func TestProbeNone(t *testing.T) {
	ts := httptest.NewServer(&fakeMCP{t: t})
	defer ts.Close()
	s, _ := newService(t)
	p, err := s.Probe(ctx(t), ts.URL+"/mcp#frag")
	if err != nil {
		t.Fatal(err)
	}
	if p.Auth != AuthNone || p.Transport != HTTP || p.Name != "Fake Server" || p.URL != ts.URL+"/mcp" {
		t.Fatalf("probe %+v", p)
	}
}

func TestProbeHeader(t *testing.T) {
	ts := httptest.NewServer(&fakeMCP{t: t, allow: func(*http.Request) bool { return false }})
	defer ts.Close()
	s, _ := newService(t)
	p, err := s.Probe(ctx(t), ts.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if p.Auth != AuthHeader || p.HeaderName != "Authorization" || p.OAuthUnavailable {
		t.Fatalf("probe %+v", p)
	}
}

func TestProbeOAuthViaChallenge(t *testing.T) {
	as := newAS(t)
	as.pathPrefix = "/tenant"
	asMux := http.NewServeMux()
	asSrv := httptest.NewServer(asMux)
	defer asSrv.Close()
	as.base = asSrv.URL
	as.mount(asMux)

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()
	mux.HandleFunc("GET /meta/prm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": ts.URL, "authorization_servers": []string{as.issuer()}})
	})
	mux.Handle("/mcp", &fakeMCP{t: t, allow: as.valid,
		challenge: `Bearer realm="x", resource_metadata="` + ts.URL + `/meta/prm", scope="files:read"`})

	s, _ := newService(t)
	p, err := s.Probe(ctx(t), ts.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if p.Auth != AuthOAuth || p.oauth == nil {
		t.Fatalf("probe %+v", p)
	}
	d := p.oauth
	if d.AuthServer != as.issuer() || d.Token != as.issuer()+"/token" || d.Register != as.issuer()+"/register" {
		t.Fatalf("discovery %+v", d)
	}
	// The PRM's resource is the origin, which covers /mcp; the scope comes from the challenge.
	if d.Resource != ts.URL || d.Scope != "files:read" {
		t.Fatalf("resource %q scope %q", d.Resource, d.Scope)
	}
}

func TestProbeOAuthViaWellKnownOnly(t *testing.T) {
	as := newAS(t)
	as.openID = true
	ts := oauthServer(t, as)
	s, _ := newService(t)
	p, err := s.Probe(ctx(t), ts.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if p.Auth != AuthOAuth || p.oauth.Resource != ts.URL+"/mcp" || p.oauth.Scope != "read write" {
		t.Fatalf("probe %+v %+v", p, p.oauth)
	}
}

func TestProbeLegacyOriginFallback(t *testing.T) {
	as := newAS(t)
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()
	as.base = ts.URL
	as.mount(mux)
	mux.Handle("/mcp", &fakeMCP{t: t, allow: as.valid, challenge: "Bearer"})
	s, _ := newService(t)
	p, err := s.Probe(ctx(t), ts.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if p.Auth != AuthOAuth || p.oauth.AuthServer != ts.URL || p.oauth.Authorize != ts.URL+"/authorize" {
		t.Fatalf("probe %+v %+v", p, p.oauth)
	}
}

func TestProbeNoDCR(t *testing.T) {
	as := newAS(t)
	as.noRegister = true
	ts := oauthServer(t, as)
	s, _ := newService(t)
	p, err := s.Probe(ctx(t), ts.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if p.Auth != AuthHeader || !p.OAuthUnavailable || p.HeaderName != "Authorization" {
		t.Fatalf("probe %+v", p)
	}
}

func TestProbeBadURL(t *testing.T) {
	s, _ := newService(t)
	for _, raw := range []string{"ftp://x", "not a url", "https://user:pw@example.com/mcp"} {
		if _, err := s.Probe(ctx(t), raw); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestProbeNotMCP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html></html>")
	}))
	defer ts.Close()
	s, _ := newService(t)
	if _, err := s.Probe(ctx(t), ts.URL); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err %v", err)
	}
}

// legacySSE is an MCP 2024-11-05 server: GET /sse streams, POST /messages?session=1 answers on the stream.
func legacySSE(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	sessions := map[string]chan string{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		out := make(chan string, 16)
		mu.Lock()
		id := fmt.Sprint(len(sessions) + 1)
		sessions[id] = out
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fmt.Fprintf(w, "event: endpoint\ndata: /messages?session=%s\n\n", id)
		fl.Flush()
		for {
			select {
			case msg := <-out:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("POST /sse", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "method", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("POST /messages", func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		mu.Lock()
		out, ok := sessions[r.URL.Query().Get("session")]
		mu.Unlock()
		if !ok {
			http.Error(w, "no session", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		var result any
		switch msg.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "legacy"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "ping", "description": "Ping."}}}
		default:
			return
		}
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
		out <- string(raw)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestLegacySSE(t *testing.T) {
	ts := legacySSE(t)
	s, _ := newService(t)
	p, err := s.Probe(ctx(t), ts.URL+"/sse")
	if err != nil {
		t.Fatal(err)
	}
	if p.Transport != SSE || p.Auth != AuthNone || p.Name != "legacy" {
		t.Fatalf("probe %+v", p)
	}
	srv, start, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/sse"})
	if err != nil || start != nil {
		t.Fatalf("add %v %+v", err, start)
	}
	if srv.State != StateOK || len(srv.Tools) != 1 || srv.Tools[0].Name != "ping" || srv.Transport != SSE {
		t.Fatalf("server %+v", srv)
	}
}

func TestAddNoneChecksToolsJSONAndSSE(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprint("sse=", sse), func(t *testing.T) {
			ts := httptest.NewServer(&fakeMCP{t: t, sse: sse})
			defer ts.Close()
			s, _ := newService(t)
			srv, start, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/mcp"})
			if err != nil || start != nil {
				t.Fatalf("add %v %+v", err, start)
			}
			if srv.State != StateOK || srv.Name != "Fake Server" || srv.Slug != "fake-server" || !srv.Enabled {
				t.Fatalf("server %+v", srv)
			}
			want := []Tool{{"search", "Search things."}, {"read", "Read one thing."}, {"write", "Write one thing."}}
			if fmt.Sprint(srv.Tools) != fmt.Sprint(want) {
				t.Fatalf("tools %+v", srv.Tools)
			}
			again, err := s.Check(ctx(t), srv.ID)
			if err != nil || again.State != StateOK || len(again.Tools) != 3 {
				t.Fatalf("check %v %+v", err, again)
			}
		})
	}
}

func TestHeaderAuth(t *testing.T) {
	ts := httptest.NewServer(&fakeMCP{t: t, allow: func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer good" }})
	defer ts.Close()
	s, sec := newService(t)

	if _, _, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/mcp", Header: &HeaderInput{Value: "bad"}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("bad token: %v", err)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatalf("a refused server was kept: %+v", list)
	}

	srv, start, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/mcp"})
	if err != nil || start == nil || start.Auth != AuthHeader || srv.State != StateSignIn {
		t.Fatalf("add %v %+v %+v", err, start, srv)
	}
	if _, err := s.Authorize(ctx(t), srv.ID); !errors.Is(err, ErrSignInRequired) {
		t.Fatalf("authorize before header: %v", err)
	}
	if _, err := s.SetHeader(ctx(t), srv.ID, "", "bad"); !errors.Is(err, ErrDenied) {
		t.Fatalf("set bad header: %v", err)
	}
	srv, err = s.SetHeader(ctx(t), srv.ID, "", "good")
	if err != nil || srv.State != StateOK || len(srv.Tools) != 3 || srv.HeaderName != "Authorization" {
		t.Fatalf("set header %v %+v", err, srv)
	}
	h, err := s.Authorize(ctx(t), srv.ID)
	if err != nil || h.Get("Authorization") != "Bearer good" {
		t.Fatalf("authorize %v %v", err, h)
	}
	if _, err := s.SetHeader(ctx(t), srv.ID, "Host", "x"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("host header: %v", err)
	}
	if err := s.Remove(srv.ID); err != nil {
		t.Fatal(err)
	}
	if len(sec.m) != 0 {
		t.Fatalf("secrets left: %v", sec.m)
	}
}

func TestHeaderOf(t *testing.T) {
	for _, c := range []struct{ name, value, wantName, wantValue string }{
		{"", "tok", "Authorization", "Bearer tok"},
		{"authorization", "Token abc", "Authorization", "Token abc"},
		{"X-Api-Key", "k", "X-Api-Key", "k"},
		{"CONTEXT7_API_KEY", "k", "Context7_api_key", "k"},
	} {
		n, v, err := headerOf(c.name, c.value)
		if err != nil || n != c.wantName || v != c.wantValue {
			t.Errorf("headerOf(%q, %q) = %q %q %v", c.name, c.value, n, v, err)
		}
	}
	for _, bad := range [][2]string{{"Bad Name", "v"}, {"X", "a\nb"}, {"Cookie", "v"}, {"X", " "}} {
		if _, _, err := headerOf(bad[0], bad[1]); !errors.Is(err, ErrBadRequest) {
			t.Errorf("headerOf(%q) = %v", bad, err)
		}
	}
}

const redirect = "https://app.example.com/api/platform/mcp/callback"

// signIn adds the server and runs a whole sign-in.
func signIn(t *testing.T, s *Service, as *fakeAS, ts *httptest.Server) Server {
	t.Helper()
	srv, start, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/mcp"})
	if err != nil || start == nil || start.Auth != AuthOAuth || srv.State != StateSignIn || srv.Auth != AuthOAuth {
		t.Fatalf("add %v %+v %+v", err, start, srv)
	}
	authURL, err := s.StartSignIn(ctx(t), srv.ID, redirect)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(authURL)
	if q.Query().Get("redirect_uri") != redirect || q.Query().Get("scope") != "read write" {
		t.Fatalf("authorize URL %s", authURL)
	}
	state, code := as.authorize(authURL)
	srv, err = s.FinishSignIn(ctx(t), state, code, "")
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestSignIn(t *testing.T) {
	for _, secret := range []bool{false, true} {
		t.Run(fmt.Sprint("secret=", secret), func(t *testing.T) {
			as := newAS(t)
			as.issueSecret = secret
			ts := oauthServer(t, as)
			s, sec := newService(t)
			srv := signIn(t, s, as, ts)
			if srv.State != StateOK || len(srv.Tools) != 3 {
				t.Fatalf("server %+v", srv)
			}
			var c cred
			if err := json.Unmarshal([]byte(sec.m["mcp-"+srv.ID]), &c); err != nil || c.OAuth == nil {
				t.Fatalf("cred %v", err)
			}
			if c.OAuth.AccessToken != "at-1" || c.OAuth.RefreshToken != "rt-1" || c.OAuth.Resource != ts.URL+"/mcp" || c.OAuth.TokenEndpoint != ts.URL+"/token" {
				t.Fatalf("oauth cred %+v", c.OAuth)
			}
			if secret && (c.OAuth.ClientSecret == "" || c.OAuth.AuthMethod != authPost) {
				t.Fatalf("client secret not kept: %+v", c.OAuth)
			}
			// The file holds no token.
			raw, _ := os.ReadFile(filepath.Join(filepath.Dir(s.file), "mcp-connections.json"))
			if strings.Contains(string(raw), "at-1") || strings.Contains(string(raw), "shh-") {
				t.Fatalf("a secret is in the file: %s", raw)
			}
			if st, _ := os.Stat(s.file); st.Mode().Perm() != 0o600 {
				t.Fatalf("file mode %v", st.Mode())
			}
			h, err := s.Authorize(ctx(t), srv.ID)
			if err != nil || h.Get("Authorization") != "Bearer at-1" {
				t.Fatalf("authorize %v %v", err, h)
			}
			// A second sign-in reuses the registration.
			if _, err := s.StartSignIn(ctx(t), srv.ID, redirect); err != nil {
				t.Fatal(err)
			}
			if as.registered != 1 {
				t.Fatalf("registered %d times", as.registered)
			}
		})
	}
}

func TestSignInStateOnceAndExpiry(t *testing.T) {
	as := newAS(t)
	ts := oauthServer(t, as)
	s, _ := newService(t)
	srv := signIn(t, s, as, ts)

	authURL, err := s.StartSignIn(ctx(t), srv.ID, redirect)
	if err != nil {
		t.Fatal(err)
	}
	state, code := as.authorize(authURL)
	if _, err := s.FinishSignIn(ctx(t), state, code, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishSignIn(ctx(t), state, code, ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("reused state: %v", err)
	}
	if _, err := s.FinishSignIn(ctx(t), "made-up", "x", ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("unknown state: %v", err)
	}

	authURL, _ = s.StartSignIn(ctx(t), srv.ID, redirect)
	state, code = as.authorize(authURL)
	s.now = func() time.Time { return time.Now().Add(flowTTL + time.Minute) }
	if _, err := s.FinishSignIn(ctx(t), state, code, ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("expired state: %v", err)
	}
	s.now = time.Now

	authURL, _ = s.StartSignIn(ctx(t), srv.ID, redirect)
	state, _ = as.authorize(authURL)
	got, err := s.FinishSignIn(ctx(t), state, "", "access_denied")
	if !errors.Is(err, ErrDenied) || got.ID != srv.ID {
		t.Fatalf("declined: %v %+v", err, got)
	}
}

func TestRefreshOnExpiry(t *testing.T) {
	as := newAS(t)
	as.expiresIn = 30 // inside the 60 s skew: the first use refreshes
	ts := oauthServer(t, as)
	s, _ := newService(t)
	srv := signIn(t, s, as, ts)

	var wg sync.WaitGroup
	tokens := make([]string, 8)
	for i := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := s.Authorize(ctx(t), srv.ID)
			if err != nil {
				t.Error(err)
				return
			}
			tokens[i] = h.Get("Authorization")
		}()
	}
	wg.Wait()
	if n := as.refreshes.Load(); n != 1 {
		t.Fatalf("refreshed %d times", n)
	}
	for _, tok := range tokens {
		if tok != "Bearer at-2" {
			t.Fatalf("tokens %v", tokens)
		}
	}
	// The MCP server takes the refreshed token.
	got, err := s.Check(ctx(t), srv.ID)
	if err != nil || got.State != StateOK {
		t.Fatalf("check %v %+v", err, got)
	}
	// A forced refresh rotates again.
	h, err := s.Reauthorize(ctx(t), srv.ID)
	if err != nil || h.Get("Authorization") != "Bearer at-3" {
		t.Fatalf("reauthorize %v %v", err, h)
	}
}

func TestInvalidGrantNeedsSignIn(t *testing.T) {
	as := newAS(t)
	as.expiresIn = 1
	ts := oauthServer(t, as)
	s, _ := newService(t)
	srv := signIn(t, s, as, ts)
	as.mu.Lock()
	clear(as.refresh)
	as.mu.Unlock()
	if _, err := s.Authorize(ctx(t), srv.ID); !errors.Is(err, ErrSignInRequired) {
		t.Fatalf("authorize: %v", err)
	}
	got, _ := s.Get(srv.ID)
	if got.State != StateSignIn {
		t.Fatalf("state %s", got.State)
	}
	if list := s.Enabled(); len(list) != 0 {
		t.Fatalf("enabled lists a server waiting for sign-in: %+v", list)
	}
	got, err := s.Check(ctx(t), srv.ID)
	if err != nil || got.State != StateSignIn {
		t.Fatalf("check %v %+v", err, got)
	}
}

func TestCheckRevokedTokenRefreshes(t *testing.T) {
	as := newAS(t)
	ts := oauthServer(t, as)
	s, _ := newService(t)
	srv := signIn(t, s, as, ts)
	as.mu.Lock()
	clear(as.access)
	as.mu.Unlock()
	got, err := s.Check(ctx(t), srv.ID)
	if err != nil || got.State != StateOK || as.refreshes.Load() != 1 {
		t.Fatalf("check %v %+v refreshes %d", err, got, as.refreshes.Load())
	}
}

func TestSlugsRenameEnable(t *testing.T) {
	ts := httptest.NewServer(&fakeMCP{t: t})
	defer ts.Close()
	s, _ := newService(t)
	a, _, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/a", Name: "My Server!"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/b", Name: "my server"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != "my-server" || b.Slug != "my-server-2" {
		t.Fatalf("slugs %q %q", a.Slug, b.Slug)
	}
	same, _, err := s.Add(ctx(t), AddInput{URL: ts.URL + "/a"})
	if err != nil || same.ID != a.ID {
		t.Fatalf("re-adding the same URL: %v %+v", err, same)
	}
	b, err = s.Rename(b.ID, "Другое имя ✓ Tools")
	if err != nil || b.Slug != "tools" {
		t.Fatalf("rename %v %+v", err, b)
	}
	if _, err := s.Rename(b.ID, "  "); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("empty rename: %v", err)
	}
	if _, err := s.SetEnabled(a.ID, false); err != nil {
		t.Fatal(err)
	}
	if en := s.Enabled(); len(en) != 1 || en[0].ID != b.ID {
		t.Fatalf("enabled %+v", en)
	}
	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if err := s.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(); len(list) != 1 {
		t.Fatalf("list %+v", list)
	}
}

func TestCatalog(t *testing.T) {
	s, _ := newService(t)
	seen := map[string]bool{}
	for _, e := range s.Catalog() {
		if seen[e.ID] || e.Name == "" || e.Blurb == "" || e.BlurbRu == "" || !httpURL(e.URL) {
			t.Errorf("entry %+v", e)
		}
		seen[e.ID] = true
		if _, _, err := canonical(e.URL); err != nil {
			t.Errorf("%s: %v", e.ID, err)
		}
	}
	if len(seen) != 18 {
		t.Fatalf("%d entries", len(seen))
	}
}

func TestReadEventsMultiline(t *testing.T) {
	var got []string
	err := readEvents(strings.NewReader("event: message\r\ndata: {\"a\":\r\ndata: 1}\r\n\r\ndata: tail"), func(event, data string) (bool, error) {
		got = append(got, event+"|"+data)
		return false, nil
	})
	if err != nil || len(got) != 2 || got[0] != "message|{\"a\":\n1}" || got[1] != "|tail" {
		t.Fatalf("%v %q", err, got)
	}
}
