package preview

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/preview/devtools"
)

// A site that sets a cookie and redirects to another site (a Teleport application sends a person to its proxy's
// launcher with the state of the exchange in a cookie) keeps that cookie, tagged for the slot, on the offer page.
func TestRedirectToAnotherSiteKeepsItsCookies(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "__Host-grv_app_auth_state_b0", Value: "256a", Path: "/", MaxAge: 60, HttpOnly: true, Secure: true, SameSite: http.SameSiteNoneMode})
		w.Header().Set("Location", "https://example.ru:443/web/launch/app.example.ru?path=%2Fproject%2F1&state=256a_b0")
		w.WriteHeader(302)
	}))
	defer site.Close()
	secret := []byte("s3cret-s3cret-s3cret-s3cret-s3cret")
	p := New(Config{
		Path: "/proxy", ClaimHeader: "x-acme-preview", Audience: "acme-preview-proxy", Secret: secret,
		PrivateHeaderPrefix: "x-acme-", Store: devtools.NewStore(),
		Resolve: func(string) Resolved { return Resolved{Address: "198.51.100.7", Family: 4} },
		Dial: func(ctx context.Context, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(site.URL, "http://"))
		},
	})
	claim := `{"sub":"alice","target":{"kind":"url","origin":"http://app.example.ru"},"slotOrigin":"https://ai-alice-7.example.org","portSlots":[],"parentOrigin":"https://app.example.org"}`
	r := httptest.NewRequest("GET", "/proxy/project/1", nil)
	r.Header.Set("x-acme-preview", signClaim(secret, []byte(claim)))
	r.Header.Set("sec-fetch-dest", "iframe")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "https://example.ru/web/launch/app.example.ru") {
		t.Fatalf("offer: %d %q", w.Code, w.Body.String())
	}
	tag := CookieTag(devtools.Target{Kind: "url", Origin: "http://app.example.ru"}, secret)
	got := w.Result().Header.Values("Set-Cookie")
	want := "__t_" + tag + "___Host-grv_app_auth_state_b0=256a; Path=/; Max-Age=60; HttpOnly; Secure; SameSite=None; Partitioned"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Set-Cookie %q, want %q", got, want)
	}
	// Back on the site, the slot's cookie reaches it under its own name.
	if c := targetCookies("x=1; "+strings.SplitN(want, ";", 2)[0], tag); c != "__Host-grv_app_auth_state_b0=256a" {
		t.Errorf("cookie back to the site: %q", c)
	}
}
