package sites

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTicketsHMAC(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	g := NewHMACTickets([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now })
	tk := g.Issue("user-1", "shop--acme.sites.test")

	if _, ok := g.Redeem(tk, "other--acme.sites.test"); ok {
		t.Fatal("a ticket opens only the host it was made for")
	}
	// A refused redeem does not burn the ticket for the right host? It must: one use, whatever the outcome.
	if _, ok := g.Redeem(tk, "shop--acme.sites.test"); ok {
		t.Fatal("a ticket is single use, even after a wrong try")
	}

	tk = g.Issue("user-1", "shop--acme.sites.test")
	if u, ok := g.Redeem(tk, "shop--acme.sites.test"); !ok || u != "user-1" {
		t.Fatalf("redeem: %q %v", u, ok)
	}
	if _, ok := g.Redeem(tk, "shop--acme.sites.test"); ok {
		t.Fatal("second use")
	}

	tk = g.Issue("user-1", "shop--acme.sites.test")
	now = now.Add(61 * time.Second)
	if _, ok := g.Redeem(tk, "shop--acme.sites.test"); ok {
		t.Fatal("expired")
	}

	other := NewHMACTickets([]byte("another-secret-another-secret-12"), func() time.Time { return now })
	if _, ok := other.Redeem(g.Issue("u", "h"), "h"); ok {
		t.Fatal("a ticket signed with another key")
	}
	if _, ok := g.Redeem("garbage", "h"); ok {
		t.Fatal("garbage")
	}
	if _, ok := g.Redeem("", "h"); ok {
		t.Fatal("empty")
	}
}

func TestSessionCookie(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := []byte("0123456789abcdef0123456789abcdef")
	s := NewSessions(key, true, func() time.Time { return now })

	w := httptest.NewRecorder()
	s.Set(w, "shop--acme.sites.test", "user-1")
	res := w.Result()
	cs := res.Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookies %v", cs)
	}
	c := cs[0]
	if c.Name != "__Host-hl_site" || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie attributes: %+v", c)
	}
	if c.MaxAge != int((12 * time.Hour).Seconds()) {
		t.Fatalf("max age %d", c.MaxAge)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	if u, ok := s.Read(req, "shop--acme.sites.test"); !ok || u != "user-1" {
		t.Fatalf("read: %q %v", u, ok)
	}
	if _, ok := s.Read(req, "shop--other.sites.test"); ok {
		t.Fatal("a cookie belongs to one host")
	}

	tampered := *c
	tampered.Value = c.Value[:len(c.Value)-2] + "xx"
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(&tampered)
	if _, ok := s.Read(req2, "shop--acme.sites.test"); ok {
		t.Fatal("tampered")
	}

	now = now.Add(13 * time.Hour)
	if _, ok := s.Read(req, "shop--acme.sites.test"); ok {
		t.Fatal("expired")
	}

	plain := NewSessions(key, false, func() time.Time { return now })
	w = httptest.NewRecorder()
	plain.Set(w, "h", "u")
	if n := w.Result().Cookies()[0].Name; n != "hl_site" {
		t.Fatalf("plain http cookie name %q", n)
	}
}
