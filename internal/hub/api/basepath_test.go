package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// behindProxy stands in for nginx: it serves the hub at <front>/harnessly, with or without cutting the prefix off, and
// keeps the Host the browser sent (proxy_set_header Host $host).
func behindProxy(t *testing.T, strip bool) (*hubtest.Stack, string) {
	t.Helper()
	var target *url.URL
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/harnessly") {
			http.NotFound(w, r)
			return
		}
		rp := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host
			if strip {
				pr.Out.URL.Path = strings.TrimPrefix(pr.Out.URL.Path, "/harnessly")
				if pr.Out.URL.Path == "" {
					pr.Out.URL.Path = "/"
				}
			}
		}}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	dir := t.TempDir()
	index := `<!doctype html><html><head><meta name="hub-base" content="" /><script>window.__hubBase = 1;</script>` +
		`<script type="module" crossorigin src="/assets/app.js"></script><link rel="stylesheet" href="/assets/app.css"></head><body><div id="root"></div></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	s := hubtest.New(t, hubtest.Env("HUB_PUBLIC_URL", front.URL+"/harnessly"), hubtest.Env("HUB_WEB_DIR", dir))
	target, _ = url.Parse(s.URL)
	return s, front.URL + "/harnessly"
}

func TestHubUnderAPath(t *testing.T) {
	for _, strip := range []bool{true, false} {
		name := map[bool]string{true: "proxy cuts the prefix", false: "proxy keeps the prefix"}[strip]
		t.Run(name, func(t *testing.T) {
			s, base := behindProxy(t, strip)
			if s.Cfg.BasePath != "/harnessly" || !strings.HasSuffix(s.Cfg.PublicURL, "/harnessly") || strings.HasSuffix(s.Cfg.Origin, "/harnessly") {
				t.Fatalf("config: base %q public %q origin %q", s.Cfg.BasePath, s.Cfg.PublicURL, s.Cfg.Origin)
			}
			p := s.Browser()

			// The bare path goes to its slash; the page carries the base and its files are named under it.
			// (A proxy that cuts the prefix turns the bare path into "/", which is the page itself.)
			if r := p.DoAt(base, "", "GET", "", nil); !strip && (r.Code != http.StatusFound || r.Header.Get("Location") != "/harnessly/") {
				t.Fatalf("GET /harnessly: %s, Location %q", r, r.Header.Get("Location"))
			}
			r := p.DoAt(base, "", "GET", "/s/team/machines", nil)
			page := string(r.Body)
			for _, want := range []string{`content="/harnessly"`, `src="/harnessly/assets/app.js"`, `href="/harnessly/assets/app.css"`} {
				if r.Code != 200 || !strings.Contains(page, want) {
					t.Fatalf("page: %s lacks %s\n%s", r, want, page)
				}
			}
			if csp := r.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "'sha256-") {
				t.Fatalf("the page's inline script has no hash in %q", csp)
			}
			if r := p.DoAt(base, "", "GET", "/healthz", nil); r.Code != 200 {
				t.Fatalf("healthz: %s", r)
			}

			// Sign in through the proxy: the cookie is for the path, with the prefix a browser can still hold it to.
			if r := p.DoAt(base, "", "POST", "/api/auth/email/start", map[string]string{"email": "a@example.com", "intent": "signin"}); r.Code != 200 {
				t.Fatalf("start: %s", r)
			}
			r = p.DoAt(base, "", "POST", "/api/auth/email/verify", map[string]string{"email": "a@example.com", "code": s.Mail.Code("a@example.com")})
			if r.Code != 200 {
				t.Fatalf("verify: %s", r)
			}
			cookie := r.Header.Get("Set-Cookie")
			if !strings.Contains(cookie, "hl_session=") || !strings.Contains(cookie, "Path=/harnessly") || strings.Contains(cookie, "__Host-") {
				t.Fatalf("session cookie: %q", cookie)
			}
			if r := p.DoAt(base, "", "GET", "/api/me", nil); r.Code != 200 {
				t.Fatalf("signed in, /api/me: %s", r)
			}
			// A redirect to a place of the hub keeps the base: a published site's entry sends a stranger to /signin.
			if r := p.DoAt(base, "", "GET", "/m/nope/", nil); r.Code == http.StatusFound && !strings.HasPrefix(r.Header.Get("Location"), "/harnessly/") && !strings.HasPrefix(r.Header.Get("Location"), "http") {
				t.Fatalf("redirect without the base: %q", r.Header.Get("Location"))
			}
		})
	}
}

// At the root of a host nothing changes: no base in the page, the cookie for /, the prefixed name on https.
func TestHubAtTheRootIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	index := `<!doctype html><meta name="hub-base" content=""><script type="module" src="/assets/app.js"></script>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	s := hubtest.New(t, hubtest.Env("HUB_WEB_DIR", dir))
	if s.Cfg.BasePath != "" || s.Cfg.CookiePath() != "/" || s.Cfg.PublicURL != s.Cfg.Origin {
		t.Fatalf("config: %+v", s.Cfg)
	}
	p := s.Browser()
	page := string(p.Do("GET", "/anything", nil).Body)
	if !strings.Contains(page, `content=""`) || !strings.Contains(page, `src="/assets/app.js"`) {
		t.Fatalf("page: %s", page)
	}
	if r := p.Do("GET", "/harnessly/", nil); strings.Contains(string(r.Body), "/harnessly") {
		t.Fatalf("a path the hub was not given is no base: %s", r)
	}
}
