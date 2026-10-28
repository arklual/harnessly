package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A provider that does not verify emails must not be able to claim the administrator's address, however it is named.
func TestUnverifiedIdentityIsNeverTheAdmin(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))

	attacker := e.newClient()
	cb, _ := e.oauth(attacker, f, "yandex", "?intent=signup", profile{sub: "ya-evil", email: "Boss@Example.com"}, nil)
	if location(cb) != "/new" {
		t.Fatalf("sign-up: %d %s", cb.StatusCode, location(cb))
	}
	u, err := e.db.S().UserByEmail(context.Background(), "boss@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin {
		t.Error("the account was made an administrator")
	}
	var me struct{ User UserView }
	e.do(attacker, "GET", "/api/me", nil).json(&me)
	if me.User.IsAdmin {
		t.Error("/api/me says administrator")
	}
	p, err := e.svc.Authenticate(requestWithSession(t, e, attacker))
	if err != nil || p == nil || p.Admin {
		t.Errorf("principal: %+v %v", p, err)
	}
	// signing in again does not promote it either
	cb, _ = e.oauth(e.newClient(), f, "yandex", "", profile{sub: "ya-evil", email: "boss@example.com"}, nil)
	if location(cb) == "" {
		t.Fatalf("second sign-in: %d", cb.StatusCode)
	}
	if u, _ := e.db.S().UserByEmail(context.Background(), "boss@example.com"); u.IsAdmin {
		t.Error("the second sign-in made it an administrator")
	}

	// the real owner of the address proves it and is the administrator
	boss := e.signInWith(e.newClient(), "boss@example.com")
	if !boss.User.IsAdmin || boss.IsNew {
		t.Errorf("boss: %+v", boss)
	}
}

// An OpenID provider that says email_verified=false is the same as Yandex ID.
func TestUnverifiedOIDCIsNotAdminEither(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_ADMIN_EMAILS", "boss@corp.example"))
	c := e.newClient()
	if cb, _ := e.oauth(c, f, "oidc-kc", "?intent=signup", profile{sub: "kc-1", email: "boss@corp.example", verified: false}, nil); location(cb) != "/new" {
		t.Fatalf("sign-up: %s", location(cb))
	}
	var me struct{ User UserView }
	e.do(c, "GET", "/api/me", nil).json(&me)
	if me.User.IsAdmin {
		t.Error("an unverified OpenID identity became the administrator")
	}
	c2 := e.newClient()
	if cb, _ := e.oauth(c2, f, "oidc-kc", "?intent=signup", profile{sub: "kc-2", email: "boss2@corp.example", verified: true}, nil); location(cb) != "/new" {
		t.Fatalf("sign-up of a verified one: %s", location(cb))
	}
}

// A verified identity on an admin address is the administrator, whichever provider proved it.
func TestVerifiedProviderOnAnAdminAddressIsTheAdmin(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))
	c := e.newClient()
	if cb, _ := e.oauth(c, f, "github", "?intent=signup", profile{email: "boss@example.com", verified: true}, nil); location(cb) != "/new" {
		t.Fatalf("sign-up: %s", location(cb))
	}
	var me struct{ User UserView }
	e.do(c, "GET", "/api/me", nil).json(&me)
	if !me.User.IsAdmin {
		t.Error("not the administrator")
	}
}

// With no administrator named the first account is the administrator, so it has to be a proven one, whatever the
// signup mode is.
func TestTheFirstAccountMustProveItsEmail(t *testing.T) {
	for _, mode := range []string{"open", "invite"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeIdP(t)
			e := oauthEnv(t, f, withEnv("HUB_SIGNUP", mode))
			cb, _ := e.oauth(e.newClient(), f, "yandex", "?intent=signup", profile{sub: "ya-1", email: "first@example.com"}, nil)
			if location(cb) != "/signin?error=first_account_unverified" {
				t.Fatalf("first account from Yandex ID: %s", location(cb))
			}
			if n, _ := e.db.S().CountUsers(context.Background()); n != 0 {
				t.Fatalf("%d users were made", n)
			}
			first := e.signInWith(e.newClient(), "first@example.com")
			if !first.User.IsAdmin {
				t.Error("the first proven account is not the administrator")
			}
			if mode == "open" {
				cb, _ = e.oauth(e.newClient(), f, "yandex", "?intent=signup", profile{sub: "ya-2", email: "second@example.com"}, nil)
				if location(cb) != "/new" {
					t.Errorf("second account from Yandex ID: %s", location(cb))
				}
				if u, _ := e.db.S().UserByEmail(context.Background(), "second@example.com"); u.IsAdmin {
					t.Error("the second account is an administrator")
				}
			}
		})
	}
}

// When administrators are named, the first account need not be one of them, and may come from any provider.
func TestTheFirstAccountMayBeUnverifiedWhenAdminsAreNamed(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))
	c := e.newClient()
	if cb, _ := e.oauth(c, f, "yandex", "?intent=signup", profile{sub: "ya-1", email: "someone@yandex.ru"}, nil); location(cb) != "/new" {
		t.Fatalf("%s", location(cb))
	}
	var me struct{ User UserView }
	e.do(c, "GET", "/api/me", nil).json(&me)
	if me.User.IsAdmin {
		t.Error("an account that is not on the list is an administrator")
	}
}

// When HUB_ADMIN_EMAILS names someone, an empty user table is no door in invite mode: the first stranger to prove an
// address before the administrator signs in gets nothing, and the administrator still comes in first.
func TestInviteModeIsClosedToStrangersWhileTheTableIsEmptyAndAnAdminIsNamed(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_SIGNUP", "invite"), withEnv("HUB_ADMIN_EMAILS", "a@example.com"))

	// by email code: refused before a code is sent
	if r := e.post("/api/auth/email/start", map[string]string{"email": "b@example.com", "intent": "signup"}); r.StatusCode != 403 || r.errCode() != "signup_closed" {
		t.Fatalf("a stranger's code request: %d %s", r.StatusCode, r.Body)
	}
	if e.mail.code("b@example.com") != "" {
		t.Error("a code was mailed to the stranger")
	}
	// by a provider that proves the address
	cb, _ := e.oauth(e.newClient(), f, "github", "?intent=signup", profile{email: "b@example.com", verified: true}, nil)
	if location(cb) != "/signin?error=signup_closed" {
		t.Errorf("a stranger's verified GitHub sign-in: %s", location(cb))
	}
	// and by one that does not
	cb, _ = e.oauth(e.newClient(), f, "yandex", "?intent=signup", profile{sub: "ya-b", email: "b@yandex.ru"}, nil)
	if location(cb) != "/signin?error=signup_closed" {
		t.Errorf("a stranger's Yandex sign-in: %s", location(cb))
	}
	if n, _ := e.db.S().CountUsers(context.Background()); n != 0 {
		t.Fatalf("%d accounts were made", n)
	}

	admin := e.signInWith(e.newClient(), "a@example.com")
	if !admin.IsNew || !admin.User.IsAdmin {
		t.Errorf("the named administrator: %+v", admin)
	}
	// the table is not empty now: the stranger is still outside
	if r := e.post("/api/auth/email/start", map[string]string{"email": "b@example.com", "intent": "signup"}); r.StatusCode != 403 {
		t.Errorf("a stranger once the administrator is in: %d %s", r.StatusCode, r.Body)
	}
}

// With no administrator named, the first proven account is let in even in invite mode, and is the administrator.
func TestInviteModeLetsTheFirstProvenAccountInWhenNoAdminIsNamed(t *testing.T) {
	e := newEnv(t, withEnv("HUB_SIGNUP", "invite"))
	first := e.signInWith(e.newClient(), "first@example.com")
	if !first.IsNew || !first.User.IsAdmin {
		t.Errorf("first: %+v", first)
	}
	if r := e.post("/api/auth/email/start", map[string]string{"email": "second@example.com", "intent": "signup"}); r.StatusCode != 403 {
		t.Errorf("second: %d %s", r.StatusCode, r.Body)
	}
}

// In invite mode an invitation to an address is for whoever proves the address, not for whoever claims it.
func TestInviteModeNeedsAProvenAddressForAnEmailInvite(t *testing.T) {
	f := newFakeIdP(t)
	e := oauthEnv(t, f, withEnv("HUB_SIGNUP", "invite"), withEnv("HUB_ADMIN_EMAILS", "boss@example.com"))
	e.signIn("boss@example.com")
	ctx := context.Background()
	st := e.db.S()
	boss, _ := st.UserByEmail(ctx, "boss@example.com")
	sp := &store.Space{Slug: "acme", Name: "Acme", CreatedBy: boss.ID}
	if err := st.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	now := e.clock.Now()
	if err := st.CreateInvite(ctx, &store.Invite{SpaceID: sp.ID, Kind: "email", Email: "invited@example.com", TokenHash: store.HashToken("tok-email"), Role: "member", ExpiresAt: now.Add(time.Hour), CreatedBy: boss.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateInvite(ctx, &store.Invite{SpaceID: sp.ID, Kind: "link", TokenHash: store.HashToken("tok-link"), Role: "member", ExpiresAt: now.Add(time.Hour), CreatedBy: boss.ID}); err != nil {
		t.Fatal(err)
	}

	// someone who only says they are invited@example.com: neither the address nor the email invitation's own link
	for _, next := range []string{"", "/invite/tok-email"} {
		cb, _ := e.oauth(e.newClient(), f, "yandex", "?intent=signup&next="+next, profile{sub: "ya-1", email: "invited@example.com"}, nil)
		if location(cb) != "/signin?error=signup_closed" {
			t.Errorf("unverified claim of an invited address (next=%q): %s", next, location(cb))
		}
	}
	cb, _ := e.oauth(e.newClient(), f, "oidc-kc", "?intent=signup", profile{sub: "kc-1", email: "invited@example.com", verified: false}, nil)
	if location(cb) != "/signin?error=signup_closed" {
		t.Errorf("unverified OpenID claim: %s", location(cb))
	}
	// a link invitation is for whoever arrives with it
	cb, _ = e.oauth(e.newClient(), f, "yandex", "?intent=signup&next=/invite/tok-link", profile{sub: "ya-2", email: "walkin@yandex.ru"}, nil)
	if location(cb) == "/signin?error=signup_closed" || location(cb) == "" {
		t.Errorf("link invitation with an unverified identity: %s", location(cb))
	}
	// the person who proves the address is let in
	cb, _ = e.oauth(e.newClient(), f, "github", "?intent=signup", profile{email: "invited@example.com", verified: true}, nil)
	if location(cb) != "/new" {
		t.Errorf("verified invited address: %s", location(cb))
	}
}

// The comparison of a code is not where the tries are counted: parallel guesses share the five tries of a code.
func TestParallelGuessesShareTheTries(t *testing.T) {
	e := newEnv(t)
	e.post("/api/auth/email/start", map[string]string{"email": "p@example.com"})
	code := e.mail.code("p@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 9; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := e.post("/api/auth/email/verify", map[string]string{"email": "p@example.com", "code": wrong}, "X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i+1))
			mu.Lock()
			codes[r.StatusCode]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if codes[400] > 4 || codes[400]+codes[410] != 9 {
		t.Errorf("answers: %v (at most four wrong tries may be answered as wrong, the fifth ends the code)", codes)
	}
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "p@example.com", "code": code}); r.StatusCode != 410 {
		t.Errorf("the right code after the tries were spent: %d %s", r.StatusCode, r.Body)
	}
}

// Guesses from many addresses still meet the limit of the address they are for.
func TestVerifyIsLimitedPerEmail(t *testing.T) {
	e := newEnv(t)
	e.post("/api/auth/email/start", map[string]string{"email": "v@example.com"})
	code := e.mail.code("v@example.com")
	last := 0
	for i := 0; i < verifyPerEmail+1; i++ {
		r := e.post("/api/auth/email/verify", map[string]string{"email": "v@example.com", "code": "12345" + fmt.Sprint(i%10)}, "X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		last = r.StatusCode
	}
	if last != 429 {
		t.Errorf("after %d checks of one address from as many addresses: %d", verifyPerEmail+1, last)
	}
	// even the right code waits
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "v@example.com", "code": code}, "X-Forwarded-For", "198.51.100.99"); r.StatusCode != 429 {
		t.Errorf("right code while limited: %d", r.StatusCode)
	}
	e.clock.Advance(11 * time.Minute)
	if r := e.post("/api/auth/email/verify", map[string]string{"email": "other@example.com", "code": "123456"}); r.StatusCode == 429 {
		t.Error("another address is limited")
	}
}

// requestWithSession is a request carrying the cookies a client holds.
func requestWithSession(t *testing.T, e *testEnv, c *http.Client) *http.Request {
	t.Helper()
	u, err := url.Parse(e.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/me", nil)
	for _, ck := range c.Jar.Cookies(u) {
		r.AddCookie(ck)
	}
	return r
}
