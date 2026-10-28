package spaces

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

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type capMail struct {
	mu      sync.Mutex
	codes   map[string]string
	invites []sentInvite
	fail    bool
}

type sentInvite struct{ to, inviter, space, link string }

func (m *capMail) Mode() string { return "smtp" }
func (m *capMail) SendCode(_ context.Context, to, code string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes = map[string]string{}
	}
	m.codes[to] = code
	return nil
}
func (m *capMail) SendInvite(_ context.Context, to, inviter, space, link string, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return false, io.ErrClosedPipe
	}
	m.invites = append(m.invites, sentInvite{to, inviter, space, link})
	return true, nil
}
func (m *capMail) code(to string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[to]
}

type env struct {
	t    *testing.T
	srv  *httptest.Server
	db   *store.DB
	mail *capMail
	svc  *Service
	au   *auth.Service
	ev   *events.Broker
	now  time.Time
	mu   sync.Mutex
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

func newEnv(t *testing.T, kv ...string) *env {
	t.Helper()
	e := &env{t: t, mail: &capMail{}, now: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
	var h http.Handler
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(e.srv.Close)
	vals := map[string]string{"HUB_PUBLIC_URL": e.srv.URL, "HUB_DATA_DIR": t.TempDir(), "HUB_PASSKEY": "off"}
	for i := 0; i+1 < len(kv); i += 2 {
		vals[kv[i]] = kv[i+1]
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vals[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	lim := ratelimit.New().WithClock(e.clock)
	e.au, err = auth.New(auth.Deps{Config: cfg, DB: db, Mailer: e.mail, Limiter: lim, Log: log, Now: e.clock})
	if err != nil {
		t.Fatal(err)
	}
	e.ev = events.NewBroker()
	t.Cleanup(e.ev.Close)
	e.svc = New(Deps{Config: cfg, DB: db, Auth: e.au, Mailer: e.mail, Events: e.ev, Limiter: lim, Log: log, Now: e.clock})
	mux := http.NewServeMux()
	e.au.Mount(mux)
	e.svc.Mount(mux)
	h = e.au.CSRF(mux)
	e.db = db
	return e
}

type person struct {
	e      *env
	client *http.Client
	email  string
	id     string
}

type resp struct {
	Code int
	Body []byte
}

func (r resp) into(v any) {
	if err := json.Unmarshal(r.Body, v); err != nil {
		panic("not json: " + string(r.Body))
	}
}

func (r resp) errCode() string {
	var b struct{ Error string }
	_ = json.Unmarshal(r.Body, &b)
	return b.Error
}

func (r resp) reason() string {
	var b struct{ Details struct{ Reason string } }
	_ = json.Unmarshal(r.Body, &b)
	return b.Details.Reason
}

// signIn is a new browser signed in by email code.
func (e *env) signIn(email string) *person {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	p := &person{e: e, email: email, client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if r := p.do("POST", "/api/auth/email/start", map[string]string{"email": email, "intent": "signin"}); r.Code != 200 {
		e.t.Fatalf("start %s: %d %s", email, r.Code, r.Body)
	}
	r := p.do("POST", "/api/auth/email/verify", map[string]string{"email": email, "code": e.mail.code(email)})
	if r.Code != 200 {
		e.t.Fatalf("verify %s: %d %s", email, r.Code, r.Body)
	}
	var out struct{ User struct{ ID string } }
	r.into(&out)
	p.id = out.User.ID
	return p
}

func (p *person) do(method, path string, body any) resp {
	p.e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.e.srv.URL+path, rd)
	if err != nil {
		p.e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.client.Do(req)
	if err != nil {
		p.e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{Code: res.StatusCode, Body: b}
}

func (p *person) newSpace(name string) SpaceView {
	p.e.t.Helper()
	r := p.do("POST", "/api/spaces", map[string]string{"name": name})
	if r.Code != 201 {
		p.e.t.Fatalf("create space: %d %s", r.Code, r.Body)
	}
	var out struct{ Space SpaceView }
	r.into(&out)
	return out.Space
}

func tokenOf(link string) string { return link[strings.LastIndex(link, "/")+1:] }

func TestSlug(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"Моя команда", "moya-komanda"},
		{"  Ёжики & Щуки!  ", "ezhiki-shchuki"},
		{"Acme Corp", "acme-corp"},
		{"Я", "ya-space"},
		{"日本", "space"},
		{"--", "space"},
		{"ab", "ab-space"},
		{strings.Repeat("долгое-имя ", 10), "dolgoe-imya-dolgoe-imya-dolgoe-imya-dolg"},
	} {
		got := slugBase(c.name)
		if got != c.want {
			t.Errorf("slugBase(%q) = %q, want %q", c.name, got, c.want)
		}
		if len(got) < slugMin || len(got) > slugMax || strings.HasPrefix(got, "-") || strings.Contains(got, "--") {
			t.Errorf("slugBase(%q) = %q is not a slug", c.name, got)
		}
	}
	long := strings.Repeat("a", 40)
	if got := slugCandidate(long, 12); len(got) > slugMax || !strings.HasSuffix(got, "-12") {
		t.Errorf("slugCandidate: %q", got)
	}
	if got := slugCandidate(long, 30); len(got) > slugMax {
		t.Errorf("slugCandidate random tail: %q", got)
	}
}

func TestCreateSpaceAndSlugs(t *testing.T) {
	e := newEnv(t)
	a, b := e.signIn("a@example.com"), e.signIn("b@example.com")
	sp := a.newSpace("Моя команда")
	if sp.Slug != "moya-komanda" || sp.Role != "owner" || sp.OnboardingStep != "machines" || sp.Name != "Моя команда" {
		t.Errorf("space: %+v", sp)
	}
	if again := b.newSpace("моя  команда"); again.Slug != "moya-komanda-2" {
		t.Errorf("second slug: %q", again.Slug)
	}
	if third := a.newSpace("Моя команда"); third.Slug != "moya-komanda-3" {
		t.Errorf("third slug: %q", third.Slug)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("я", 61), "ab\x00c"} {
		if r := a.do("POST", "/api/spaces", map[string]string{"name": bad}); r.Code != 400 || r.errCode() != "invalid_request" {
			t.Errorf("name %q: %d %s", bad, r.Code, r.Body)
		}
	}
	if r := (&person{e: e, client: &http.Client{}}).do("POST", "/api/spaces", map[string]string{"name": "x"}); r.Code != 401 {
		t.Errorf("signed out: %d", r.Code)
	}
}

func TestOwnedSpaceCap(t *testing.T) {
	e := newEnv(t)
	a := e.signIn("a@example.com")
	for i := 0; i < maxOwnedSpaces; i++ {
		a.newSpace("Команда")
	}
	r := a.do("POST", "/api/spaces", map[string]string{"name": "Ещё"})
	if r.Code != 409 || r.reason() != "space_limit" {
		t.Errorf("over the cap: %d %s", r.Code, r.Body)
	}
}

func TestAccessRules(t *testing.T) {
	e := newEnv(t)
	owner, stranger := e.signIn("owner@example.com"), e.signIn("stranger@example.com")
	sp := owner.newSpace("Закрытое")

	if r := owner.do("GET", "/api/spaces/"+sp.ID, nil); r.Code != 200 {
		t.Errorf("owner get: %d", r.Code)
	}
	if r := owner.do("GET", "/api/spaces/by-slug/"+sp.Slug, nil); r.Code != 200 {
		t.Errorf("owner by slug: %d", r.Code)
	}
	// A stranger learns nothing: forbidden by id (also for ids that do not exist), not found by slug.
	if r := stranger.do("GET", "/api/spaces/"+sp.ID, nil); r.Code != 403 || r.errCode() != "forbidden" {
		t.Errorf("stranger get: %d %s", r.Code, r.Body)
	}
	if r := stranger.do("GET", "/api/spaces/aaaaaaaaaaaaaaaaaaaa", nil); r.Code != 403 {
		t.Errorf("unknown id: %d", r.Code)
	}
	if r := stranger.do("GET", "/api/spaces/by-slug/"+sp.Slug, nil); r.Code != 404 {
		t.Errorf("stranger by slug: %d", r.Code)
	}
	if r := stranger.do("GET", "/api/spaces/by-slug/net-takogo", nil); r.Code != 404 {
		t.Errorf("unknown slug: %d", r.Code)
	}
	for _, path := range []string{"/members", "/events", "/invites"} {
		if r := stranger.do("GET", "/api/spaces/"+sp.ID+path, nil); r.Code != 403 {
			t.Errorf("stranger %s: %d", path, r.Code)
		}
	}
	if r := stranger.do("PATCH", "/api/spaces/"+sp.ID, map[string]string{"name": "Взлом"}); r.Code != 403 {
		t.Errorf("stranger patch: %d", r.Code)
	}
}

func TestPatchSpace(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Старое")
	r := owner.do("PATCH", "/api/spaces/"+sp.ID, map[string]string{"name": "  Новое  имя ", "onboardingStep": "agents"})
	var out struct{ Space SpaceView }
	r.into(&out)
	if r.Code != 200 || out.Space.Name != "Новое имя" || out.Space.OnboardingStep != "agents" || out.Space.Slug != sp.Slug {
		t.Errorf("patch: %d %s", r.Code, r.Body)
	}
	if r := owner.do("PATCH", "/api/spaces/"+sp.ID, map[string]string{"onboardingStep": "nowhere"}); r.Code != 400 {
		t.Errorf("bad step: %d", r.Code)
	}
	if r := owner.do("PATCH", "/api/spaces/"+sp.ID, map[string]string{"name": ""}); r.Code != 400 {
		t.Errorf("empty name: %d", r.Code)
	}
	var got struct{ Space SpaceView }
	owner.do("GET", "/api/spaces/"+sp.ID, nil).into(&got)
	if got.Space.OnboardingStep != "agents" {
		t.Errorf("not saved: %+v", got.Space)
	}
}

func TestEmailInviteFlow(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")

	r := owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"New@Example.com", "new@example.com", " second@example.com "}})
	var made struct{ Invites []InviteView }
	r.into(&made)
	if r.Code != 201 || len(made.Invites) != 2 {
		t.Fatalf("invite: %d %s", r.Code, r.Body)
	}
	iv := made.Invites[0]
	if iv.Kind != "email" || *iv.Email != "new@example.com" || iv.Role != "member" || iv.Sent == nil || !*iv.Sent || iv.MaxUses == nil || *iv.MaxUses != 1 {
		t.Errorf("invite view: %+v", iv)
	}
	if !strings.HasPrefix(iv.URL, e.srv.URL+"/invite/") {
		t.Errorf("url: %q", iv.URL)
	}
	if len(e.mail.invites) != 2 || e.mail.invites[0].to != "new@example.com" || e.mail.invites[0].space != "Команда" || e.mail.invites[0].link != iv.URL {
		t.Errorf("mail: %+v", e.mail.invites)
	}

	// The listing never carries the address.
	var list struct{ Invites []InviteView }
	owner.do("GET", "/api/spaces/"+sp.ID+"/invites", nil).into(&list)
	if len(list.Invites) != 2 || list.Invites[0].URL != "" {
		t.Errorf("list: %+v", list.Invites)
	}
	// Only the hash is in the database.
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM invites WHERE token_hash = ?`, tokenOf(iv.URL)).Scan(&n); err != nil || n != 0 {
		t.Errorf("token stored in clear: %d %v", n, err)
	}

	token := tokenOf(iv.URL)
	var peek struct {
		SpaceName, InviterName, Kind string
		Email                        *string
		Valid                        bool
	}
	pr := (&person{e: e, client: &http.Client{}}).do("GET", "/api/invites/"+token, nil)
	pr.into(&peek)
	if pr.Code != 200 || peek.SpaceName != "Команда" || peek.Kind != "email" || peek.Email == nil || *peek.Email != "n***@example.com" || !peek.Valid || peek.InviterName != "o***@example.com" {
		t.Errorf("peek: %d %s", pr.Code, pr.Body)
	}
	if strings.Contains(string(pr.Body), "new@example.com") {
		t.Errorf("peek leaks the address: %s", pr.Body)
	}
	if r := (&person{e: e, client: &http.Client{}}).do("GET", "/api/invites/"+strings.Repeat("a", 26), nil); r.Code != 404 {
		t.Errorf("unknown token: %d", r.Code)
	}

	// Someone else cannot use it.
	other := e.signIn("other@example.com")
	if r := other.do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 403 || r.reason() != "email_mismatch" {
		t.Errorf("wrong address: %d %s", r.Code, r.Body)
	}
	if r := (&person{e: e, client: &http.Client{}}).do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 401 {
		t.Errorf("signed out: %d", r.Code)
	}

	// The right one joins, once.
	guest := e.signIn("new@example.com")
	ar := guest.do("POST", "/api/invites/"+token+"/accept", map[string]any{})
	var acc struct{ Space auth.SpaceRef }
	ar.into(&acc)
	if ar.Code != 200 || acc.Space.ID != sp.ID || acc.Space.Role != "member" || acc.Space.Slug != sp.Slug {
		t.Fatalf("accept: %d %s", ar.Code, ar.Body)
	}
	if r := guest.do("GET", "/api/spaces/"+sp.ID, nil); r.Code != 200 {
		t.Errorf("member cannot see the space: %d", r.Code)
	}
	if r := guest.do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 200 {
		t.Errorf("accept again by a member: %d", r.Code)
	}
	var after struct{ Valid bool }
	(&person{e: e, client: &http.Client{}}).do("GET", "/api/invites/"+token, nil).into(&after)
	if after.Valid {
		t.Error("a used email invitation is still valid")
	}
	// A member sees members, but not invites and not owner actions.
	if r := guest.do("GET", "/api/spaces/"+sp.ID+"/invites", nil); r.Code != 403 {
		t.Errorf("member lists invites: %d", r.Code)
	}
	if r := guest.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"x@example.com"}}); r.Code != 403 {
		t.Errorf("member invites: %d", r.Code)
	}
	var ms struct{ Members []MemberView }
	guest.do("GET", "/api/spaces/"+sp.ID+"/members", nil).into(&ms)
	if len(ms.Members) != 2 || ms.Members[0].Role != "owner" || ms.Members[1].Email != "new@example.com" {
		t.Errorf("members: %+v", ms.Members)
	}
	// Inviting a member again is quiet.
	r = owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"new@example.com"}})
	made.Invites = nil
	r.into(&made)
	if r.Code != 201 || len(made.Invites) != 0 {
		t.Errorf("invite a member: %d %s", r.Code, r.Body)
	}
}

func TestEmailInviteNeedsAVerifiedAddress(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")
	var made struct{ Invites []InviteView }
	owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"yandex@example.com"}}).into(&made)
	token := tokenOf(made.Invites[0].URL)

	// An account whose email only Yandex claimed (unverified) is not the mailbox's owner.
	ctx := context.Background()
	u := store.User{Email: "yandex@example.com", CreatedAt: e.clock()}
	if err := e.db.S().CreateUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	if err := e.db.S().CreateIdentity(ctx, &store.Identity{UserID: u.ID, Provider: "yandex", Subject: "42", Email: u.Email}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}
	tok := "session-token-for-yandex-user"
	if err := e.db.S().CreateSession(ctx, store.Session{TokenHash: store.HashToken(tok), Kind: auth.KindHub, UserID: u.ID, CreatedAt: e.clock(), ExpiresAt: e.clock().Add(time.Hour), LastSeenAt: e.clock()}); err != nil {
		t.Fatal(err)
	}
	su, _ := http.NewRequest("GET", e.srv.URL, nil)
	cl.Jar.SetCookies(su.URL, []*http.Cookie{{Name: e.au.CookieName(), Value: tok, Path: "/"}})
	yandex := &person{e: e, client: cl, email: u.Email, id: u.ID}
	if r := yandex.do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 403 {
		t.Errorf("unverified owner of the address joined: %d %s", r.Code, r.Body)
	}
}

func TestInviteExpiryAndRevoke(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")
	var made struct{ Invites []InviteView }
	owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"late@example.com"}}).into(&made)
	token := tokenOf(made.Invites[0].URL)
	e.signIn("late@example.com")

	e.advance(7*24*time.Hour + time.Minute)
	late := e.signIn("late@example.com")
	if r := late.do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 410 || r.errCode() != "expired" {
		t.Errorf("expired: %d %s", r.Code, r.Body)
	}
	var peek struct{ Valid bool }
	(&person{e: e, client: &http.Client{}}).do("GET", "/api/invites/"+token, nil).into(&peek)
	if peek.Valid {
		t.Error("expired invitation looks valid")
	}

	// Revoked ones stop working at once, and an unknown id is not found.
	var fresh struct{ Invites []InviteView }
	owner = e.signIn("owner@example.com")
	owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"late@example.com"}}).into(&fresh)
	if r := owner.do("DELETE", "/api/spaces/"+sp.ID+"/invites/"+fresh.Invites[0].ID, nil); r.Code != 204 {
		t.Fatalf("revoke: %d %s", r.Code, r.Body)
	}
	if r := owner.do("DELETE", "/api/spaces/"+sp.ID+"/invites/"+fresh.Invites[0].ID, nil); r.Code != 404 {
		t.Errorf("revoke twice: %d", r.Code)
	}
	if r := late.do("POST", "/api/invites/"+tokenOf(fresh.Invites[0].URL)+"/accept", map[string]any{}); r.Code != 410 {
		t.Errorf("revoked: %d", r.Code)
	}
}

func TestSecondEmailInviteReplacesTheFirst(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")
	var first, second struct{ Invites []InviteView }
	owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"a@example.com"}}).into(&first)
	owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"a@example.com"}}).into(&second)
	var list struct{ Invites []InviteView }
	owner.do("GET", "/api/spaces/"+sp.ID+"/invites", nil).into(&list)
	if len(list.Invites) != 1 || list.Invites[0].ID != second.Invites[0].ID {
		t.Errorf("active invites: %+v", list.Invites)
	}
	guest := e.signIn("a@example.com")
	if r := guest.do("POST", "/api/invites/"+tokenOf(first.Invites[0].URL)+"/accept", map[string]any{}); r.Code != 410 {
		t.Errorf("the replaced link still works: %d", r.Code)
	}
	if r := guest.do("POST", "/api/invites/"+tokenOf(second.Invites[0].URL)+"/accept", map[string]any{}); r.Code != 200 {
		t.Errorf("the new link: %d", r.Code)
	}
}

func TestInviteValidation(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")
	many := make([]string, 21)
	for i := range many {
		many[i] = "u" + strings.Repeat("x", i+1) + "@example.com"
	}
	for name, body := range map[string]any{
		"none":     map[string]any{"emails": []string{}},
		"missing":  map[string]any{},
		"bad":      map[string]any{"emails": []string{"not an email"}},
		"too many": map[string]any{"emails": many},
	} {
		if r := owner.do("POST", "/api/spaces/"+sp.ID+"/invites", body); r.Code != 400 || r.errCode() != "invalid_request" {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
	for name, body := range map[string]any{
		"days 0": map[string]any{"days": 0}, "days 8": map[string]any{"days": 8},
		"uses 0": map[string]any{"maxUses": 0}, "uses huge": map[string]any{"maxUses": 100000},
	} {
		if r := owner.do("POST", "/api/spaces/"+sp.ID+"/invites/link", body); r.Code != 400 {
			t.Errorf("%s: %d %s", name, r.Code, r.Body)
		}
	}
}

func TestMailFailureStillGivesTheLink(t *testing.T) {
	e := newEnv(t)
	e.mail.fail = true
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")
	r := owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": []string{"a@example.com"}})
	var made struct{ Invites []InviteView }
	r.into(&made)
	if r.Code != 201 || len(made.Invites) != 1 || made.Invites[0].URL == "" || made.Invites[0].Sent == nil || *made.Invites[0].Sent {
		t.Errorf("mail down: %d %s", r.Code, r.Body)
	}
}

func TestLinkInvite(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")

	r := owner.do("POST", "/api/spaces/"+sp.ID+"/invites/link", map[string]any{})
	var made struct{ Invite InviteView }
	r.into(&made)
	iv := made.Invite
	if r.Code != 201 || iv.Kind != "link" || iv.Email != nil || iv.Role != "member" || iv.MaxUses == nil || *iv.MaxUses != 20 || iv.URL == "" {
		t.Fatalf("link: %d %s", r.Code, r.Body)
	}
	if exp, _ := time.Parse(time.RFC3339, iv.ExpiresAt); !exp.Equal(e.clock().Add(7 * 24 * time.Hour)) {
		t.Errorf("expires %s", iv.ExpiresAt)
	}

	// Asking again with an empty body returns the same invitation, with its address while the hub remembers it.
	r = owner.do("POST", "/api/spaces/"+sp.ID+"/invites/link", map[string]any{})
	var again struct{ Invite InviteView }
	r.into(&again)
	if r.Code != 200 || again.Invite.ID != iv.ID || again.Invite.URL != iv.URL {
		t.Errorf("repeat: %d %s", r.Code, r.Body)
	}
	// After a restart the address is gone; the invitation is still there.
	e.svc.linkURLs = map[string]string{}
	var forgotten struct{ Invite InviteView }
	owner.do("POST", "/api/spaces/"+sp.ID+"/invites/link", map[string]any{}).into(&forgotten)
	if forgotten.Invite.ID != iv.ID || forgotten.Invite.URL != "" {
		t.Errorf("after restart: %+v", forgotten.Invite)
	}
	// Explicit settings make a new one.
	var custom struct{ Invite InviteView }
	r = owner.do("POST", "/api/spaces/"+sp.ID+"/invites/link", map[string]any{"maxUses": 2, "days": 1})
	r.into(&custom)
	if r.Code != 201 || custom.Invite.ID == iv.ID || *custom.Invite.MaxUses != 2 {
		t.Errorf("custom: %d %s", r.Code, r.Body)
	}

	token := tokenOf(custom.Invite.URL)
	var peek struct {
		Kind  string
		Email *string
		Valid bool
	}
	(&person{e: e, client: &http.Client{}}).do("GET", "/api/invites/"+token, nil).into(&peek)
	if peek.Kind != "link" || peek.Email != nil || !peek.Valid {
		t.Errorf("peek link: %+v", peek)
	}

	// Two uses, then it is used up. The same person counts once.
	g1, g2, g3 := e.signIn("g1@example.com"), e.signIn("g2@example.com"), e.signIn("g3@example.com")
	for i, g := range []*person{g1, g2} {
		if r := g.do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 200 {
			t.Fatalf("guest %d: %d %s", i, r.Code, r.Body)
		}
	}
	if r := g1.do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 200 {
		t.Errorf("same person again: %d", r.Code)
	}
	if r := g3.do("POST", "/api/invites/"+token+"/accept", map[string]any{}); r.Code != 410 {
		t.Errorf("used up: %d %s", r.Code, r.Body)
	}
	var list struct{ Invites []InviteView }
	owner.do("GET", "/api/spaces/"+sp.ID+"/invites", nil).into(&list)
	if len(list.Invites) != 1 || list.Invites[0].ID != iv.ID {
		t.Errorf("the used-up invitation is still listed: %+v", list.Invites)
	}
}

func TestMemberRolesAndLastOwner(t *testing.T) {
	e := newEnv(t)
	owner, guest := e.signIn("owner@example.com"), e.signIn("guest@example.com")
	sp := owner.newSpace("Команда")
	join(t, e, owner, guest, sp.ID)

	// Last owner cannot be demoted or removed.
	if r := owner.do("PATCH", "/api/spaces/"+sp.ID+"/members/"+owner.id, map[string]string{"role": "member"}); r.Code != 409 || r.reason() != "last_owner" {
		t.Errorf("demote last owner: %d %s", r.Code, r.Body)
	}
	if r := owner.do("DELETE", "/api/spaces/"+sp.ID+"/members/"+owner.id, nil); r.Code != 409 || r.reason() != "last_owner" {
		t.Errorf("leave as last owner: %d %s", r.Code, r.Body)
	}
	// A member cannot change roles or remove others.
	if r := guest.do("PATCH", "/api/spaces/"+sp.ID+"/members/"+guest.id, map[string]string{"role": "owner"}); r.Code != 403 {
		t.Errorf("self-promotion: %d", r.Code)
	}
	if r := guest.do("DELETE", "/api/spaces/"+sp.ID+"/members/"+owner.id, nil); r.Code != 403 {
		t.Errorf("member removes owner: %d", r.Code)
	}
	if r := owner.do("PATCH", "/api/spaces/"+sp.ID+"/members/"+guest.id, map[string]string{"role": "boss"}); r.Code != 400 {
		t.Errorf("bad role: %d", r.Code)
	}
	if r := owner.do("PATCH", "/api/spaces/"+sp.ID+"/members/"+"nobodynobodynobodyno", map[string]string{"role": "owner"}); r.Code != 404 {
		t.Errorf("unknown member: %d", r.Code)
	}

	// Promote, then the first owner may leave.
	r := owner.do("PATCH", "/api/spaces/"+sp.ID+"/members/"+guest.id, map[string]string{"role": "owner"})
	var out struct{ Member MemberView }
	r.into(&out)
	if r.Code != 200 || out.Member.Role != "owner" || out.Member.Email != "guest@example.com" {
		t.Fatalf("promote: %d %s", r.Code, r.Body)
	}
	if r := owner.do("DELETE", "/api/spaces/"+sp.ID+"/members/"+owner.id, nil); r.Code != 204 {
		t.Errorf("owner leaves: %d %s", r.Code, r.Body)
	}
	if r := owner.do("GET", "/api/spaces/"+sp.ID, nil); r.Code != 403 {
		t.Errorf("left the space but still sees it: %d", r.Code)
	}
	if r := guest.do("GET", "/api/spaces/"+sp.ID+"/invites", nil); r.Code != 200 {
		t.Errorf("new owner: %d", r.Code)
	}
}

func TestMemberCanLeaveAndOwnerCanRemove(t *testing.T) {
	e := newEnv(t)
	owner, a, b := e.signIn("owner@example.com"), e.signIn("a@example.com"), e.signIn("b@example.com")
	sp := owner.newSpace("Команда")
	join(t, e, owner, a, sp.ID)
	join(t, e, owner, b, sp.ID)
	if r := a.do("DELETE", "/api/spaces/"+sp.ID+"/members/"+a.id, nil); r.Code != 204 {
		t.Errorf("leave: %d", r.Code)
	}
	if r := owner.do("DELETE", "/api/spaces/"+sp.ID+"/members/"+b.id, nil); r.Code != 204 {
		t.Errorf("remove: %d", r.Code)
	}
	if r := owner.do("DELETE", "/api/spaces/"+sp.ID+"/members/"+b.id, nil); r.Code != 404 {
		t.Errorf("remove twice: %d", r.Code)
	}
	var ms struct{ Members []MemberView }
	owner.do("GET", "/api/spaces/"+sp.ID+"/members", nil).into(&ms)
	if len(ms.Members) != 1 {
		t.Errorf("members: %+v", ms.Members)
	}
}

// join puts guest into a space by a link invitation.
func join(t *testing.T, e *env, owner, guest *person, spaceID string) {
	t.Helper()
	var made struct{ Invite InviteView }
	owner.do("POST", "/api/spaces/"+spaceID+"/invites/link", map[string]any{"maxUses": 50}).into(&made)
	if r := guest.do("POST", "/api/invites/"+tokenOf(made.Invite.URL)+"/accept", map[string]any{}); r.Code != 200 {
		t.Fatalf("join: %d %s", r.Code, r.Body)
	}
}

func TestSpaceStreamAndRoleChange(t *testing.T) {
	e := newEnv(t)
	owner, guest := e.signIn("owner@example.com"), e.signIn("guest@example.com")
	sp := owner.newSpace("Команда")
	join(t, e, owner, guest, sp.ID)

	open := func(p *person) (lines chan string, cancel func()) {
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/spaces/"+sp.ID+"/events", nil)
		res, err := p.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("stream: %d %v", res.StatusCode, res.Header)
		}
		lines = make(chan string, 64)
		go func() {
			defer res.Body.Close()
			buf := make([]byte, 4096)
			var acc string
			for {
				n, err := res.Body.Read(buf)
				acc += string(buf[:n])
				for {
					i := strings.Index(acc, "\n\n")
					if i < 0 {
						break
					}
					lines <- acc[:i]
					acc = acc[i+2:]
				}
				if err != nil {
					close(lines)
					return
				}
			}
		}()
		return lines, cancel
	}
	next := func(ch chan string) string {
		select {
		case s, ok := <-ch:
			if !ok {
				return "<closed>"
			}
			return s
		case <-time.After(3 * time.Second):
			return "<timeout>"
		}
	}
	waitSubs := func(n int) {
		deadline := time.Now().Add(3 * time.Second)
		for e.ev.Count(sp.ID) != n && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		if e.ev.Count(sp.ID) != n {
			t.Fatalf("%d streams, want %d", e.ev.Count(sp.ID), n)
		}
	}

	ownerLines, c1 := open(owner)
	defer c1()
	guestLines, c2 := open(guest)
	defer c2()
	waitSubs(2)
	if s := next(ownerLines); s != ": ok" {
		t.Errorf("owner hello: %q", s)
	}
	if s := next(guestLines); s != ": ok" {
		t.Errorf("guest hello: %q", s)
	}

	e.ev.PublishJSON(sp.ID, "job", map[string]string{"machineId": "m"}, true)
	e.ev.PublishJSON(sp.ID, "machine", map[string]string{"machineId": "m"}, false)
	if s := next(ownerLines); !strings.HasPrefix(s, "event: job") {
		t.Errorf("owner first: %q", s)
	}
	if s := next(ownerLines); !strings.HasPrefix(s, "event: machine") {
		t.Errorf("owner second: %q", s)
	}
	if s := next(guestLines); !strings.HasPrefix(s, "event: machine") {
		t.Errorf("a member must not see the job event, got: %q", s)
	}

	// Promoted: the old stream ends so the owner events can flow on a new one.
	if r := owner.do("PATCH", "/api/spaces/"+sp.ID+"/members/"+guest.id, map[string]string{"role": "owner"}); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if s := next(guestLines); s != "<closed>" && !strings.HasPrefix(s, "event: members") {
		t.Errorf("guest stream after role change: %q", s)
	}
	// Removed: no stream can be opened again.
	if r := owner.do("DELETE", "/api/spaces/"+sp.ID+"/members/"+guest.id, nil); r.Code != 204 {
		t.Fatal(r.Code)
	}
	if r := guest.do("GET", "/api/spaces/"+sp.ID+"/events", nil); r.Code != 403 {
		t.Errorf("removed member opens a stream: %d", r.Code)
	}
}

func TestInviteMailRateLimit(t *testing.T) {
	e := newEnv(t)
	owner := e.signIn("owner@example.com")
	sp := owner.newSpace("Команда")
	var last resp
	for i := 0; i < 5; i++ {
		emails := make([]string, 15)
		for j := range emails {
			emails[j] = "p" + strings.Repeat("a", i) + strings.Repeat("b", j+1) + "@example.com"
		}
		last = owner.do("POST", "/api/spaces/"+sp.ID+"/invites", map[string]any{"emails": emails})
	}
	if last.Code != 429 || last.errCode() != "rate_limited" {
		t.Errorf("mail flood: %d %s", last.Code, last.Body)
	}
}

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{"r.ivanov@company.ru": "r***@company.ru", "я@почта.рф": "я***@почта.рф", "weird": "***"} {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
