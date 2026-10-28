// Package hubtest is a whole hub in a test: the real services on a real SQLite file behind a real HTTP server, people
// who sign in by email code, and a fake agent that registers, dials with its key and answers like a machine does. The
// tests of the services and the end-to-end tests of the hub share it.
package hubtest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Mail remembers what the hub would have mailed.
type Mail struct {
	mu      sync.Mutex
	codes   map[string]string
	Invites []Invite
	// Fail makes sending an invitation fail.
	Fail bool
}

// Invite is a mailed invitation.
type Invite struct{ To, Inviter, Space, Link string }

func (m *Mail) Mode() string { return "smtp" }

func (m *Mail) SendCode(_ context.Context, to, code string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes = map[string]string{}
	}
	m.codes[to] = code
	return nil
}

func (m *Mail) SendInvite(_ context.Context, to, inviter, space, link string, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail {
		return false, io.ErrClosedPipe
	}
	m.Invites = append(m.Invites, Invite{to, inviter, space, link})
	return true, nil
}

// Code is the last sign-in code mailed to an address.
func (m *Mail) Code(to string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[to]
}

// Clock is a time the test moves.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Stack is a running hub.
type Stack struct {
	T      *testing.T
	Cfg    *config.Config
	DB     *store.DB
	Hub    *api.Hub
	Server *httptest.Server
	Mail   *Mail
	Clock  *Clock
	// Logs is everything the hub logged, debug level included.
	Logs *syncBuffer
	// URL is the hub's public URL.
	URL string
	// DistDir is the folder the hub serves binaries and the installer from.
	DistDir string
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Option changes how a stack is built.
type Option func(env map[string]string, o *api.Options)

// Env sets a configuration variable.
func Env(k, v string) Option { return func(env map[string]string, _ *api.Options) { env[k] = v } }

// WithOptions changes the options the hub is built with.
func WithOptions(f func(o *api.Options)) Option {
	return func(_ map[string]string, o *api.Options) { f(o) }
}

// New starts a hub. Its clock starts at the real time and moves only when a test advances it.
func New(t *testing.T, opts ...Option) *Stack {
	t.Helper()
	s := &Stack{T: t, Mail: &Mail{}, Clock: &Clock{t: time.Now().UTC().Truncate(time.Second)}, Logs: &syncBuffer{}, DistDir: t.TempDir()}
	var handler http.Handler
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(s.Server.Close)
	s.URL = s.Server.URL
	// HUB_SMTP_URL stands for the stack's own mailer (s.Mail takes the place of the SMTP server): a hub on https or in the
	// cloud edition does not start without one.
	env := map[string]string{"HUB_PUBLIC_URL": s.URL, "HUB_DATA_DIR": t.TempDir(), "HUB_PASSKEY": "off", "HUB_DIST_DIR": s.DistDir,
		"HUB_SMTP_URL": "smtp://mail.hubtest.invalid:587"}
	o := api.Options{Mailer: s.Mail, Now: s.Clock.Now, Log: slog.New(slog.NewTextHandler(s.Logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	for _, opt := range opts {
		opt(env, &o)
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	s.Cfg = cfg
	db := openDB(t)
	s.DB = db
	o.Config, o.DB = cfg, db
	hub, err := api.New(o)
	if err != nil {
		t.Fatal(err)
	}
	s.Hub = hub
	handler = hub.Handler
	t.Cleanup(hub.Shutdown)
	return s
}

// openDB is a SQLite file of its own. With HUB_TEST_POSTGRES_URL set it is a throwaway schema of that Postgres
// instead, so the services' tests and the end-to-end tests run on both databases:
//
//	HUB_TEST_POSTGRES_URL='postgres://user@127.0.0.1:5432/db?sslmode=disable' go test ./internal/hub/...
func openDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("HUB_TEST_POSTGRES_URL")
	if dsn == "" {
		db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "hubtest_" + store.RandomBase32(8)
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// WriteDist puts a file in the folder the hub serves binaries and the installer from.
func (s *Stack) WriteDist(name, content string) {
	s.T.Helper()
	if err := os.WriteFile(filepath.Join(s.DistDir, name), []byte(content), 0o755); err != nil {
		s.T.Fatal(err)
	}
}

// Resp is an answer.
type Resp struct {
	Code   int
	Body   []byte
	Header http.Header
}

// JSON decodes the body into v.
func (r Resp) JSON(v any) {
	if err := json.Unmarshal(r.Body, v); err != nil {
		panic("not json: " + string(r.Body))
	}
}

// Err is the `error` code of an API error, or "".
func (r Resp) Err() string {
	var b struct{ Error string }
	_ = json.Unmarshal(r.Body, &b)
	return b.Error
}

// Detail is a field of the `details` of an API error.
func (r Resp) Detail(key string) any {
	var b struct{ Details map[string]any }
	_ = json.Unmarshal(r.Body, &b)
	return b.Details[key]
}

func (r Resp) String() string { return http.StatusText(r.Code) + " " + string(r.Body) }

// Person is a browser: it keeps its cookies and does not follow redirects.
type Person struct {
	S      *Stack
	Client *http.Client
	Email  string
	ID     string
}

// Browser is a person nobody signed in.
func (s *Stack) Browser() *Person {
	jar, _ := cookiejar.New(nil)
	return &Person{S: s, Client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// SignIn is a new browser signed in by an email code.
func (s *Stack) SignIn(email string) *Person {
	s.T.Helper()
	p := s.Browser()
	p.Email = email
	if r := p.Do("POST", "/api/auth/email/start", map[string]string{"email": email, "intent": "signin"}); r.Code != 200 {
		s.T.Fatalf("sign-in start %s: %s", email, r)
	}
	r := p.Do("POST", "/api/auth/email/verify", map[string]string{"email": email, "code": s.Mail.Code(email)})
	if r.Code != 200 {
		s.T.Fatalf("sign-in verify %s: %s", email, r)
	}
	var out struct{ User struct{ ID string } }
	r.JSON(&out)
	p.ID = out.User.ID
	return p
}

// Do makes a request to the hub; a body is sent as JSON.
func (p *Person) Do(method, path string, body any, headers ...string) Resp {
	p.S.T.Helper()
	return p.DoAt(p.S.URL, "", method, path, body, headers...)
}

// DoAt is Do to another address (a machine's own origin), with the Host header the request would carry there.
func (p *Person) DoAt(base, host, method, path string, body any, headers ...string) Resp {
	p.S.T.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		p.S.T.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if body != nil {
		if _, isJSON := body.(map[string]string); isJSON || !isRaw(body) {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := p.Client.Do(req)
	if err != nil {
		p.S.T.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return Resp{Code: res.StatusCode, Body: b, Header: res.Header}
}

func isRaw(body any) bool {
	switch body.(type) {
	case []byte, string:
		return true
	}
	return false
}

// NewSpace makes a space the person owns and returns its id and slug.
func (p *Person) NewSpace(name string) (id, slug string) {
	p.S.T.Helper()
	r := p.Do("POST", "/api/spaces", map[string]string{"name": name})
	if r.Code != 201 {
		p.S.T.Fatalf("create space: %s", r)
	}
	var out struct{ Space struct{ ID, Slug string } }
	r.JSON(&out)
	return out.Space.ID, out.Space.Slug
}

// Join makes guest a member of the owner's space, by a link invitation.
func (p *Person) Join(spaceID string, guest *Person) {
	p.S.T.Helper()
	var made struct{ Invite struct{ URL string } }
	r := p.Do("POST", "/api/spaces/"+spaceID+"/invites/link", map[string]any{"maxUses": 50})
	if r.Code != 201 {
		p.S.T.Fatalf("link invite: %s", r)
	}
	r.JSON(&made)
	token := made.Invite.URL[strings.LastIndex(made.Invite.URL, "/")+1:]
	if r := guest.Do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 200 {
		p.S.T.Fatalf("accept: %s", r)
	}
}
