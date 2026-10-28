package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestWriteError(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		body   string
	}{
		"typed":        {ErrForbidden, 403, `"error":"forbidden"`},
		"details":      {Invalid("email", "Укажите почту"), 400, `"details":{"field":"email"}`},
		"conflict":     {Conflict("last_owner", "Последний владелец"), 409, `"reason":"last_owner"`},
		"unknown":      {errors.New("db exploded: secret"), 500, `"error":"internal"`},
		"rate limited": {RateLimited(42), 429, `"retryAfter":42`},
	} {
		w := httptest.NewRecorder()
		WriteError(w, httptest.NewRequest("GET", "/x", nil), tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Errorf("%s: an internal error text reached the client: %s", name, w.Body)
		}
	}
	w := httptest.NewRecorder()
	WriteError(w, httptest.NewRequest("GET", "/x", nil), RateLimited(7))
	if w.Header().Get("Retry-After") != "7" {
		t.Errorf("Retry-After %q", w.Header().Get("Retry-After"))
	}
}

func TestWriteErrorForACallerWhoLeft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/agent/v1", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	WriteError(w, r, context.Canceled)
	if w.Body.Len() != 0 {
		t.Errorf("answered a caller who left: %s", w.Body)
	}
	// A cancellation while the caller is still there is a failure like any other.
	w = httptest.NewRecorder()
	WriteError(w, httptest.NewRequest("GET", "/x", nil), context.Canceled)
	if w.Code != 500 {
		t.Errorf("status %d", w.Code)
	}
}

func TestDecode(t *testing.T) {
	type body struct{ Name string }
	for name, tc := range map[string]struct {
		in   string
		want string
		fail bool
	}{
		"object":  {`{"name":"a","extra":1}`, "a", false},
		"empty":   {``, "", false},
		"blank":   {"  \n", "", false},
		"garbage": {`{"name":`, "", true},
	} {
		var b body
		err := Decode(httptest.NewRequest("POST", "/x", strings.NewReader(tc.in)), &b)
		if (err != nil) != tc.fail || b.Name != tc.want {
			t.Errorf("%s: %+v %v", name, b, err)
		}
	}
}

func TestIsJSON(t *testing.T) {
	for ct, want := range map[string]bool{
		"application/json":                  true,
		"application/json; charset=utf-8":   true,
		"application/problem+json":          true,
		"text/plain":                        false,
		"application/x-www-form-urlencoded": false,
		"":                                  false,
	} {
		r := httptest.NewRequest("POST", "/x", nil)
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		if IsJSON(r) != want {
			t.Errorf("%q: %v", ct, !want)
		}
	}
}

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("10.0.0.0/8")}
	for name, tc := range map[string]struct {
		remote string
		xff    []string
		want   string
	}{
		"direct":               {"203.0.113.9:1234", nil, "203.0.113.9"},
		"direct spoof ignored": {"203.0.113.9:1234", []string{"1.2.3.4"}, "203.0.113.9"},
		"behind proxy":         {"127.0.0.1:5000", []string{"198.51.100.7"}, "198.51.100.7"},
		"chain":                {"127.0.0.1:5000", []string{"6.6.6.6, 198.51.100.7, 10.1.2.3"}, "198.51.100.7"},
		"client forged left":   {"127.0.0.1:5000", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},
		"proxy without header": {"127.0.0.1:5000", nil, "127.0.0.1"},
		"ipv6":                 {"[2001:db8::1]:80", nil, "2001:db8::1"},
		"mapped v4":            {"[::ffff:203.0.113.9]:80", nil, "203.0.113.9"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		for _, x := range tc.xff {
			r.Header.Add("X-Forwarded-For", x)
		}
		if got := ClientIP(r, trusted); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestHandlerServesErrors(t *testing.T) {
	h := Handler(func(w http.ResponseWriter, r *http.Request) error { return ErrNotFound })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "not_found") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestSafePath(t *testing.T) {
	for in, want := range map[string]string{
		"/api/invites/INVITETOKENSECRETabcdefghij0123456789": "/api/invites/:redacted",
		"/api/machines/abcdefghijklmnopqrst/open":            "/api/machines/:redacted/open",
		"/api/spaces/acme/machines":                          "/api/spaces/acme/machines",
		"/assets/app.js":                                     "/assets/app.js",
		"/":                                                  "/",
		"":                                                   "",
	} {
		if got := SafePath(in); got != want {
			t.Errorf("SafePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRouteIsThePatternNotThePath(t *testing.T) {
	mux := http.NewServeMux()
	var got string
	mux.HandleFunc("GET /api/invites/{token}", func(w http.ResponseWriter, r *http.Request) { got = Route(r) })
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/invites/SECRETSECRETSECRET1234", nil))
	if got != "/api/invites/{token}" {
		t.Errorf("Route = %q", got)
	}
	// without a route: the path with its secrets taken out
	if got := Route(httptest.NewRequest("GET", "/m/abcdefghijklmnopqrst/x", nil)); got != "/m/:redacted/x" {
		t.Errorf("Route without a pattern = %q", got)
	}
}
