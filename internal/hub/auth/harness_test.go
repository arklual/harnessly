package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// captureMailer remembers the last code and invite sent to each address.
type captureMailer struct {
	mu      sync.Mutex
	codes   map[string]string
	sends   int
	failing bool
}

func (m *captureMailer) Mode() string { return "smtp" }
func (m *captureMailer) SendCode(_ context.Context, to, code string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failing {
		return io.ErrClosedPipe
	}
	if m.codes == nil {
		m.codes = map[string]string{}
	}
	m.codes[to] = code
	m.sends++
	return nil
}
func (m *captureMailer) SendInvite(context.Context, string, string, string, string, time.Time) (bool, error) {
	return true, nil
}
func (m *captureMailer) code(to string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[to]
}

// clock is a settable time.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type testEnv struct {
	t      *testing.T
	svc    *Service
	db     *store.DB
	srv    *httptest.Server
	mail   *captureMailer
	clock  *clock
	logs   *bytes.Buffer
	client *http.Client
}

type envOpt func(env map[string]string, d *Deps)

func withEnv(k, v string) envOpt { return func(env map[string]string, _ *Deps) { env[k] = v } }

// newEnv starts a hub's account side behind a real HTTP server with a cookie jar client.
func newEnv(t *testing.T, opts ...envOpt) *testEnv {
	t.Helper()
	e := &testEnv{t: t, mail: &captureMailer{}, clock: &clock{t: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{}}
	var handler http.Handler
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(e.srv.Close)

	// WebAuthn needs a domain for the RP ID, so the hub is "localhost", not 127.0.0.1.
	e.srv.URL = strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	env := map[string]string{"HUB_PUBLIC_URL": e.srv.URL, "HUB_DATA_DIR": t.TempDir()}
	d := Deps{Mailer: e.mail, Now: e.clock.Now, Log: slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	for _, o := range opts {
		o(env, &d)
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	d.Config, d.DB = cfg, db
	d.Limiter = ratelimit.New().WithClock(e.clock.Now)
	svc, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc.Mount(mux)
	handler = svc.CSRF(mux)
	e.svc, e.db = svc, db
	e.client = e.newClient()
	return e
}

// newClient is a browser: it keeps cookies and does not follow redirects.
func (e *testEnv) newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type response struct {
	*http.Response
	Body []byte
}

func (r response) json(v any) {
	if err := json.Unmarshal(r.Body, v); err != nil {
		panic("not json: " + string(r.Body))
	}
}

func (r response) errCode() string {
	var b struct{ Error string }
	_ = json.Unmarshal(r.Body, &b)
	return b.Error
}

func (e *testEnv) do(c *http.Client, method, path string, body any, headers ...string) response {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{Response: res, Body: b}
}

func (e *testEnv) post(path string, body any, headers ...string) response {
	return e.do(e.client, "POST", path, body, headers...)
}
func (e *testEnv) get(path string, headers ...string) response {
	return e.do(e.client, "GET", path, nil, headers...)
}

// signIn signs the default client in by email code and returns the answer.
func (e *testEnv) signIn(email string) SignedIn {
	e.t.Helper()
	return e.signInWith(e.client, email)
}

func (e *testEnv) signInWith(c *http.Client, email string) SignedIn {
	e.t.Helper()
	if res := e.do(c, "POST", "/api/auth/email/start", map[string]string{"email": email, "intent": "signin"}); res.StatusCode != 200 {
		e.t.Fatalf("start: %d %s", res.StatusCode, res.Body)
	}
	code := e.mail.code(strings.ToLower(email))
	res := e.do(c, "POST", "/api/auth/email/verify", map[string]string{"email": email, "code": code})
	if res.StatusCode != 200 {
		e.t.Fatalf("verify: %d %s", res.StatusCode, res.Body)
	}
	var out SignedIn
	res.json(&out)
	e.clock.Advance(2 * time.Minute) // past the resend gap, for the next sign-in
	return out
}
