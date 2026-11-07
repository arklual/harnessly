package preview

import (
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/preview/devtools"
)

// A url target cut for time tells where it stood: no connection (the machine's network) or no answer (the site).
func TestTimeoutSaysWhereItStood(t *testing.T) {
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	secret := []byte("s3cret-s3cret-s3cret-s3cret-s3cret")
	for name, c := range map[string]struct {
		dial func(ctx context.Context, address string) (net.Conn, error)
		want string
	}{
		"no connection": {func(ctx context.Context, address string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, "машина не смогла подключиться к 198.51.100.7:80"},
		"no answer": {func(ctx context.Context, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", silent.Addr().String())
		}, "подключение к 198.51.100.7:80 есть, ответа нет"},
	} {
		p := New(Config{
			Path: "/proxy", ClaimHeader: "x-acme-preview", Audience: "acme-preview-proxy", Secret: secret,
			PrivateHeaderPrefix: "x-acme-", Store: devtools.NewStore(),
			Resolve:  func(string) Resolved { return Resolved{Address: "198.51.100.7", Family: 4} },
			Timeouts: Timeouts{Connect: 100 * time.Millisecond, Headers: 100 * time.Millisecond, Idle: time.Second},
			Dial:     c.dial,
		})
		claim := `{"sub":"alice","target":{"kind":"url","origin":"http://site.example.ru"},"slotOrigin":"https://ai-alice-7.example.org","portSlots":[],"parentOrigin":"https://app.example.org"}`
		r := httptest.NewRequest("GET", "/proxy/", nil)
		r.Header.Set("x-acme-preview", signClaim(secret, []byte(claim)))
		r.Header.Set("accept-language", "ru")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		if w.Code != 504 || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %q, want 504 with %q", name, w.Code, w.Body.String(), c.want)
		}
		// The page in the frame tells the tab, which opens the site in the machine's own browser.
		if !strings.Contains(w.Body.String(), "type: 'tk:failed'") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-") {
			t.Errorf("%s: the failure page does not tell the tab: %q", name, w.Body.String())
		}
		// A script or an image of the page fails as before: only the document tells.
		r = httptest.NewRequest("GET", "/proxy/app.js", nil)
		r.Header.Set("x-acme-preview", signClaim(secret, []byte(claim)))
		r.Header.Set("sec-fetch-dest", "script")
		w = httptest.NewRecorder()
		p.ServeHTTP(w, r)
		if w.Code != 504 || strings.Contains(w.Body.String(), "tk:failed") {
			t.Errorf("%s: a script got %d %q, want a plain 504", name, w.Code, w.Body.String())
		}
	}
}
