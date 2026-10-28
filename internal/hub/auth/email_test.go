package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func TestEmailSignInAndOut(t *testing.T) {
	e := newEnv(t)
	res := e.post("/api/auth/email/start", map[string]string{"email": "  Ann@Example.COM ", "intent": "signup"})
	if res.StatusCode != 200 {
		t.Fatalf("start: %d %s", res.StatusCode, res.Body)
	}
	var started struct{ ResendAfter, ExpiresIn int }
	res.json(&started)
	if started.ResendAfter != 60 || started.ExpiresIn != 600 {
		t.Errorf("start answer: %+v", started)
	}
	code := e.mail.code("ann@example.com")
	if len(code) != 6 {
		t.Fatalf("code %q", code)
	}

	// before signing in
	if r := e.get("/api/me"); r.StatusCode != 401 || r.errCode() != "unauthenticated" {
		t.Errorf("me signed out: %d %s", r.StatusCode, r.Body)
	}
	res = e.post("/api/auth/email/verify", map[string]string{"email": "ann@example.com", "code": code})
	if res.StatusCode != 200 {
		t.Fatalf("verify: %d %s", res.StatusCode, res.Body)
	}
	var in SignedIn
	res.json(&in)
	if !in.IsNew || in.Next != "/new" || in.User.Email != "ann@example.com" || !in.User.IsAdmin {
		t.Errorf("signed in: %+v", in)
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "hl_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Secure {
		t.Errorf("cookie: %+v", cookie)
	}
	// only the hash of the token is stored
	if _, err := e.db.S().SessionByHash(context.Background(), cookie.Value); err == nil {
		t.Error("the session token itself is in the database")
	}
	if _, err := e.db.S().SessionByHash(context.Background(), store.HashToken(cookie.Value)); err != nil {
		t.Errorf("hash not found: %v", err)
	}

	var me struct {
		User       UserView
		Identities []struct{ Provider, Email string }
		Spaces     []SpaceRef
	}
	e.get("/api/me").json(&me)
	if me.User.Email != "ann@example.com" || len(me.Identities) != 1 || me.Identities[0].Provider != "email" || len(me.Spaces) != 0 {
		t.Errorf("me: %+v", me)
	}

	// the same code does not work twice
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "ann@example.com", "code": code}); r.StatusCode != 410 {
		t.Errorf("code used twice: %d", r.StatusCode)
	}

	if r := e.post("/api/auth/logout", map[string]string{}); r.StatusCode != 204 {
		t.Errorf("logout: %d", r.StatusCode)
	}
	if r := e.get("/api/me"); r.StatusCode != 401 {
		t.Errorf("me after logout: %d", r.StatusCode)
	}
}

func TestEmailStartIsNeutral(t *testing.T) {
	e := newEnv(t)
	e.signIn("known@example.com")
	known := e.post("/api/auth/email/start", map[string]string{"email": "known@example.com", "intent": "signin"})
	unknown := e.post("/api/auth/email/start", map[string]string{"email": "stranger@example.com", "intent": "signin"})
	if known.StatusCode != 200 || unknown.StatusCode != 200 || string(known.Body) != string(unknown.Body) {
		t.Errorf("answers differ: %d %s / %d %s", known.StatusCode, known.Body, unknown.StatusCode, unknown.Body)
	}
	for _, bad := range []string{"", "nope", "a@b", "a b@example.com", "<a@example.com>"} {
		if r := e.post("/api/auth/email/start", map[string]string{"email": bad}); r.StatusCode != 400 || r.errCode() != "invalid_request" {
			t.Errorf("%q: %d %s", bad, r.StatusCode, r.Body)
		}
	}
}

func TestEmailResendGapAndLimits(t *testing.T) {
	e := newEnv(t)
	start := func(email string) response {
		return e.post("/api/auth/email/start", map[string]string{"email": email})
	}
	if r := start("a@example.com"); r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	r := start("a@example.com")
	if r.StatusCode != 429 || r.errCode() != "rate_limited" || r.Header.Get("Retry-After") != "60" {
		t.Errorf("resend within a minute: %d %s retry-after %q", r.StatusCode, r.Body, r.Header.Get("Retry-After"))
	}
	// five codes an hour per address
	for i := 0; i < 4; i++ {
		e.clock.Advance(61 * time.Second)
		if r := start("a@example.com"); r.StatusCode != 200 {
			t.Fatalf("code %d: %d %s", i+2, r.StatusCode, r.Body)
		}
	}
	e.clock.Advance(61 * time.Second)
	if r := start("a@example.com"); r.StatusCode != 429 {
		t.Errorf("sixth code in an hour: %d", r.StatusCode)
	}
	e.clock.Advance(time.Hour)
	if r := start("a@example.com"); r.StatusCode != 200 {
		t.Errorf("after the hour: %d", r.StatusCode)
	}
}

func TestEmailLimitPerIPv6Prefix(t *testing.T) {
	e := newEnv(t, withEnv("HUB_TRUSTED_PROXIES", "127.0.0.1/32"))
	// Twenty addresses of one /64 are one sender.
	for i := 0; i < 20; i++ {
		ip := fmt.Sprintf("2001:db8:aa:bb:%x::1", i+1)
		r := e.post("/api/auth/email/start", map[string]string{"email": fmt.Sprintf("u%d@example.com", i)}, "X-Forwarded-For", ip)
		if r.StatusCode != 200 {
			t.Fatalf("%d: %d %s", i, r.StatusCode, r.Body)
		}
	}
	r := e.post("/api/auth/email/start", map[string]string{"email": "late@example.com"}, "X-Forwarded-For", "2001:db8:aa:bb:ffff::9")
	if r.StatusCode != 429 {
		t.Errorf("21st code from one /64: %d", r.StatusCode)
	}
	r = e.post("/api/auth/email/start", map[string]string{"email": "other@example.com"}, "X-Forwarded-For", "2001:db8:aa:cc::1")
	if r.StatusCode != 200 {
		t.Errorf("another /64: %d", r.StatusCode)
	}
}

func TestEmailVerifyTriesAndExpiry(t *testing.T) {
	e := newEnv(t)
	e.post("/api/auth/email/start", map[string]string{"email": "t@example.com"})
	code := e.mail.code("t@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 1; i <= 4; i++ {
		r := e.post("/api/auth/email/verify", map[string]string{"email": "t@example.com", "code": wrong})
		var b struct{ Details struct{ AttemptsLeft int } }
		r.json(&b)
		if r.StatusCode != 400 || r.errCode() != "invalid_code" || b.Details.AttemptsLeft != 5-i {
			t.Fatalf("try %d: %d %s", i, r.StatusCode, r.Body)
		}
	}
	// the fifth wrong try ends the code, even if the next one is right
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "t@example.com", "code": wrong}); r.StatusCode != 410 {
		t.Errorf("fifth wrong try: %d %s", r.StatusCode, r.Body)
	}
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "t@example.com", "code": code}); r.StatusCode != 410 {
		t.Errorf("right code after the limit: %d", r.StatusCode)
	}

	// a code is good for ten minutes
	e.clock.Advance(2 * time.Minute)
	e.post("/api/auth/email/start", map[string]string{"email": "t@example.com"})
	code = e.mail.code("t@example.com")
	e.clock.Advance(10*time.Minute + time.Second)
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "t@example.com", "code": code}); r.StatusCode != 410 || r.errCode() != "expired" {
		t.Errorf("after the TTL: %d %s", r.StatusCode, r.Body)
	}
	// no code asked for at all
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "never@example.com", "code": "123456"}); r.StatusCode != 410 {
		t.Errorf("no challenge: %d", r.StatusCode)
	}
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "t@example.com", "code": "12"}); r.StatusCode != 400 {
		t.Errorf("short code: %d", r.StatusCode)
	}
}

func TestNewCodeReplacesTheOld(t *testing.T) {
	e := newEnv(t)
	e.post("/api/auth/email/start", map[string]string{"email": "n@example.com"})
	first := e.mail.code("n@example.com")
	e.clock.Advance(61 * time.Second)
	e.post("/api/auth/email/start", map[string]string{"email": "n@example.com"})
	second := e.mail.code("n@example.com")
	if first != second {
		if r := e.post("/api/auth/email/verify", map[string]string{"email": "n@example.com", "code": first}); r.StatusCode != 400 {
			t.Errorf("old code: %d", r.StatusCode)
		}
	}
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "n@example.com", "code": second}); r.StatusCode != 200 {
		t.Errorf("new code: %d %s", r.StatusCode, r.Body)
	}
}

func TestMailFailureIsReported(t *testing.T) {
	e := newEnv(t)
	e.mail.failing = true
	r := e.post("/api/auth/email/start", map[string]string{"email": "m@example.com"})
	if r.StatusCode != 500 || r.errCode() != "internal" {
		t.Errorf("%d %s", r.StatusCode, r.Body)
	}
	// the failed attempt does not leave a code behind that blocks the next try
	e.mail.failing = false
	e.clock.Advance(time.Second)
	if r := e.post("/api/auth/email/start", map[string]string{"email": "m@example.com"}); r.StatusCode != 200 {
		t.Errorf("retry: %d %s", r.StatusCode, r.Body)
	}
}

func TestFirstAccountIsAdminOthersAreNot(t *testing.T) {
	e := newEnv(t)
	first := e.signIn("first@example.com")
	second := e.signInWith(e.newClient(), "second@example.com")
	if !first.User.IsAdmin || second.User.IsAdmin {
		t.Errorf("admin flags: %v %v", first.User.IsAdmin, second.User.IsAdmin)
	}

	e = newEnv(t, withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))
	a := e.signIn("someone@example.com")
	b := e.signInWith(e.newClient(), "Boss@Example.com")
	if a.User.IsAdmin || !b.User.IsAdmin {
		t.Errorf("with HUB_ADMIN_EMAILS: %v %v", a.User.IsAdmin, b.User.IsAdmin)
	}
}

func TestInviteOnlySignup(t *testing.T) {
	e := newEnv(t, withEnv("HUB_SIGNUP", "invite"), withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))
	start := func(email, next string) response {
		return e.post("/api/auth/email/start", map[string]string{"email": email, "intent": "signup", "next": next})
	}
	// the empty table is no door when an administrator is named: the first stranger is outside, as every one after
	if r := start("stranger@example.com", ""); r.StatusCode != 403 || r.errCode() != "signup_closed" {
		t.Fatalf("first stranger: %d %s", r.StatusCode, r.Body)
	}
	// the administrator comes in first, and strangers are still not let in
	e.signIn("boss@example.com")
	if r := start("stranger2@example.com", ""); r.StatusCode != 403 || r.errCode() != "signup_closed" {
		t.Errorf("stranger: %d %s", r.StatusCode, r.Body)
	}
	if r := start("stranger2@example.com", "/invite/nonexistent"); r.StatusCode != 403 {
		t.Errorf("unknown invite: %d", r.StatusCode)
	}
	// an invitation to the address opens the door
	ctx := context.Background()
	st := e.db.S()
	boss, _ := st.UserByEmail(ctx, "boss@example.com")
	sp := &store.Space{Slug: "acme", Name: "Acme", CreatedBy: boss.ID}
	if err := st.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	now := e.clock.Now()
	one := 1
	if err := st.CreateInvite(ctx, &store.Invite{SpaceID: sp.ID, Kind: "email", Email: "invited@example.com", TokenHash: store.HashToken("tok-email"), Role: "member", MaxUses: &one, ExpiresAt: now.Add(time.Hour), CreatedBy: boss.ID}); err != nil {
		t.Fatal(err)
	}
	if r := start("invited@example.com", ""); r.StatusCode != 200 {
		t.Errorf("invited address: %d %s", r.StatusCode, r.Body)
	}
	// a link invitation opens the door for whoever arrives with it
	if err := st.CreateInvite(ctx, &store.Invite{SpaceID: sp.ID, Kind: "link", TokenHash: store.HashToken("tok-link"), Role: "member", ExpiresAt: now.Add(time.Hour), CreatedBy: boss.ID}); err != nil {
		t.Fatal(err)
	}
	if r := start("walkin@example.com", "/invite/tok-link"); r.StatusCode != 200 {
		t.Errorf("link invite: %d %s", r.StatusCode, r.Body)
	}
	// an email invitation does not work for another address
	if r := start("thief@example.com", "/invite/tok-email"); r.StatusCode != 403 {
		t.Errorf("someone else's email invite: %d", r.StatusCode)
	}
	// an expired one does not work
	e.clock.Advance(2 * time.Hour)
	if r := start("late@example.com", "/invite/tok-link"); r.StatusCode != 403 {
		t.Errorf("expired link invite: %d", r.StatusCode)
	}
}

func TestCSRF(t *testing.T) {
	e := newEnv(t)
	n := 0
	e.signIn("c@example.com")
	do := func(method, ct, body string, headers ...string) int {
		req, _ := http.NewRequest(method, e.srv.URL+"/api/auth/logout", strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		res, err := e.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for name, tc := range map[string]struct {
		ct, body string
		headers  []string
		want     int
	}{
		"json same origin":     {"application/json", "{}", []string{"Sec-Fetch-Site", "same-origin"}, 204},
		"json no browser hdrs": {"application/json", "{}", nil, 204},
		"cross-site fetch":     {"application/json", "{}", []string{"Sec-Fetch-Site", "cross-site"}, 403},
		"foreign origin":       {"application/json", "{}", []string{"Origin", "https://evil.example"}, 403},
		"html form":            {"application/x-www-form-urlencoded", "a=b", nil, 403},
		"text/plain form":      {"text/plain", "{}", nil, 403},
		"no content type":      {"", "{}", nil, 403},
	} {
		// sign in again before each: the logout of the previous one ended its session
		n++
		e.signIn(fmt.Sprintf("c%d@example.com", n))
		if got := do("POST", tc.ct, tc.body, tc.headers...); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
	// a body-less DELETE needs no content type; one with a body does
	e.signIn("c-delete@example.com")
	req, _ := http.NewRequest("DELETE", e.srv.URL+"/api/me/identities/github", nil)
	res, _ := e.client.Do(req)
	res.Body.Close()
	if res.StatusCode == 403 {
		t.Errorf("body-less DELETE refused: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("DELETE", e.srv.URL+"/api/me/identities/github", strings.NewReader("x"))
	res, _ = e.client.Do(req)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Errorf("DELETE with a non-JSON body: %d", res.StatusCode)
	}
	// reads are never refused for being cross-site: they change nothing
	if r := e.get("/api/me", "Sec-Fetch-Site", "cross-site"); r.StatusCode != 200 {
		t.Errorf("cross-site GET: %d", r.StatusCode)
	}
	// the agent endpoints carry no cookie and are exempt
	h := e.svc.CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }))
	rec := newRecorder()
	req, _ = http.NewRequest("POST", "http://hub/agent/register", strings.NewReader("{}"))
	h.ServeHTTP(rec, req)
	if rec.code != 201 {
		t.Errorf("agent endpoint: %d", rec.code)
	}
}

type recorder struct {
	code int
	h    http.Header
}

func newRecorder() *recorder                    { return &recorder{code: 200, h: http.Header{}} }
func (r *recorder) Header() http.Header         { return r.h }
func (r *recorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *recorder) WriteHeader(c int)           { r.code = c }

func TestSessionLifetime(t *testing.T) {
	e := newEnv(t)
	e.signIn("s@example.com")
	// idle for 15 days: gone
	e.clock.Advance(15 * 24 * time.Hour)
	if r := e.get("/api/me"); r.StatusCode != 401 {
		t.Errorf("after 15 idle days: %d", r.StatusCode)
	}
	// used every week: lives until the absolute 30 days
	e.signIn("s@example.com")
	for i := 0; i < 4; i++ {
		e.clock.Advance(7 * 24 * time.Hour)
		if r := e.get("/api/me"); r.StatusCode != 200 {
			t.Fatalf("week %d: %d", i+1, r.StatusCode)
		}
	}
	e.clock.Advance(3 * 24 * time.Hour)
	if r := e.get("/api/me"); r.StatusCode != 401 {
		t.Errorf("after 31 days: %d", r.StatusCode)
	}
}

func TestLastLoginMethodStays(t *testing.T) {
	e := newEnv(t)
	e.signIn("l@example.com")
	if r := e.do(e.client, "DELETE", "/api/me/identities/email", nil); r.StatusCode != 409 || !strings.Contains(string(r.Body), "last_method") {
		t.Errorf("removing the only method: %d %s", r.StatusCode, r.Body)
	}
	if r := e.do(e.client, "DELETE", "/api/me/identities/github", nil); r.StatusCode != 404 {
		t.Errorf("removing what is not there: %d", r.StatusCode)
	}
}

func TestPatchMe(t *testing.T) {
	e := newEnv(t)
	e.signIn("p@example.com")
	r := e.do(e.client, "PATCH", "/api/me", map[string]string{"name": "  Анна  "})
	var out struct{ User UserView }
	r.json(&out)
	if r.StatusCode != 200 || out.User.Name != "Анна" {
		t.Errorf("%d %s", r.StatusCode, r.Body)
	}
	if r := e.do(e.client, "PATCH", "/api/me", map[string]string{"name": strings.Repeat("я", 101)}); r.StatusCode != 400 {
		t.Errorf("long name: %d", r.StatusCode)
	}
}
