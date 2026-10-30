package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ly-solutions/harnessly/internal/gate"
)

const testIndex = `<!doctype html><html><head><base href="/" /><meta name="api-base" content="" />` +
	`<meta name="platform-api" content="/api/platform" /></head><body></body></html>`

func webDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(testIndex), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "assets", "app-1a2b.js"), []byte("console.log(1)"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "logo.svg"), []byte("<svg/>"), 0o644)
	return dir
}

func serveWeb(c *webConfig, method, target string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		r.Header[k] = v
	}
	w := httptest.NewRecorder()
	if !c.matches(r) {
		w.Code = -1
		return w
	}
	c.handler()(w, r)
	return w
}

func TestWebPageStandalone(t *testing.T) {
	t.Setenv("RUNTIME_WEB_DIR", webDir(t))
	t.Setenv("RUNTIME_WEB", "")
	t.Setenv("RUNTIME_APP_BASE_PATH", "")
	c := webConfigFromEnv(true)
	if c == nil {
		t.Fatal("a standalone runtime with a page serves it")
	}
	if c.base != "/agents" || c.api != "/agents" {
		t.Fatalf("base %q api %q", c.base, c.api)
	}

	if w := serveWeb(c, "GET", "/", nil); w.Code != http.StatusFound || w.Header().Get("Location") != "/agents/" {
		t.Fatalf("/: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := serveWeb(c, "GET", "/agents", nil); w.Code != http.StatusFound {
		t.Fatalf("/agents: %d", w.Code)
	}
	for _, p := range []string{"/agents/", "/agents/w/ws-a", "/agents/index.html"} {
		w := serveWeb(c, "GET", p, nil)
		body := w.Body.String()
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(body, `<base href="/agents/" />`) || !strings.Contains(body, `<meta name="api-base" content="/agents" />`) ||
			!strings.Contains(body, `<meta name="platform-api" content="/api/platform" />`) {
			t.Fatalf("%s: %d %s", p, w.Code, body)
		}
		if w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: the page is revalidated, got %q", p, w.Header().Get("Cache-Control"))
		}
	}
	if w := serveWeb(c, "GET", "/agents/assets/app-1a2b.js", nil); w.Code != 200 || w.Body.String() != "console.log(1)" ||
		!strings.Contains(w.Header().Get("Cache-Control"), "immutable") || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("asset: %d %v", w.Code, w.Header())
	}
	if w := serveWeb(c, "HEAD", "/agents/logo.svg", nil); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("head: %d %v", w.Code, w.Header())
	}
	if w := serveWeb(c, "GET", "/agents/missing.js", nil); w.Code != 404 || w.Body.String() != `{"error":"Not found"}` {
		t.Fatalf("a missing file: %d %s", w.Code, w.Body)
	}
	if w := serveWeb(c, "GET", "/agents/../../etc/passwd", nil); w.Code == 200 && !strings.Contains(w.Body.String(), "<base") {
		t.Fatalf("a path out of the page: %d %s", w.Code, w.Body)
	}

	// The API, sockets, other methods and paths outside the base are never the page's.
	for _, tc := range []struct {
		method, path string
		header       http.Header
	}{
		{"GET", "/api/health", nil},
		{"GET", "/agents/api/health", nil},
		{"GET", "/api", nil},
		{"POST", "/agents/", nil},
		{"GET", "/agents/", http.Header{"Upgrade": {"websocket"}}},
		{"GET", "/agentsx/", nil},
		{"GET", "/other", nil},
	} {
		if w := serveWeb(c, tc.method, tc.path, tc.header); w.Code != -1 {
			t.Fatalf("%s %s was taken by the page: %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestWebPageBasePaths(t *testing.T) {
	dir := webDir(t)
	t.Setenv("RUNTIME_WEB_DIR", dir)
	t.Setenv("RUNTIME_WEB", "")

	t.Setenv("RUNTIME_APP_BASE_PATH", "/")
	c := webConfigFromEnv(true)
	if c.base != "" || c.api != "" {
		t.Fatalf("root: base %q api %q", c.base, c.api)
	}
	if w := serveWeb(c, "GET", "/", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `<base href="/" />`) {
		t.Fatalf("root page: %d %s", w.Code, w.Body)
	}
	if w := serveWeb(c, "GET", "/api/x", nil); w.Code != -1 {
		t.Fatal("the API under a root page is not the page's")
	}

	t.Setenv("RUNTIME_APP_BASE_PATH", "ui/")
	c = webConfigFromEnv(true)
	if c.base != "/ui" || c.api != "/ui" {
		t.Fatalf("ui: base %q api %q", c.base, c.api)
	}
	if w := serveWeb(c, "GET", "/ui/", nil); !strings.Contains(w.Body.String(), `<meta name="api-base" content="/ui" />`) {
		t.Fatalf("ui page: %s", w.Body)
	}
	if w := serveWeb(c, "GET", "/ui/api/health", nil); w.Code != -1 {
		t.Fatal("the API under the page's base is not the page's")
	}
}

func TestAPIAliasUnderAppBase(t *testing.T) {
	var seen []string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+" "+r.RequestURI)
	})
	h := apiAlias("/ui", next)
	for _, target := range []string{"/ui/api/health?x=1", "/ui/api", "/ui/apix", "/ui/", "/api/health", "/agents/api/workspace"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}
	want := []string{
		"/agents/api/health /agents/api/health?x=1", "/agents/api /agents/api", "/ui/apix /ui/apix", "/ui/ /ui/",
		"/api/health /api/health", "/agents/api/workspace /agents/api/workspace",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(seen, "\n"))
	}
	if apiAlias("/agents", next) == nil || apiAlias("", next) == nil {
		t.Fatal("no handler")
	}
}

func TestStandaloneTmuxConf(t *testing.T) {
	data := t.TempDir()
	cfg := gate.Config{DataDir: data}
	t.Setenv("RUNTIME_TMUX_CONF", "")
	os.Unsetenv("RUNTIME_TMUX_CONF")
	f := tmuxConfigFile(cfg)
	if raw, _ := os.ReadFile(f); f != filepath.Join(data, "tmux.conf") || !strings.Contains(string(raw), "set -g status off") {
		t.Fatalf("standalone default: %q %q", f, raw)
	}
	_ = os.WriteFile(f, []byte("set -g status on\n"), 0o644)
	if tmuxConfigFile(cfg); func() string { raw, _ := os.ReadFile(f); return string(raw) }() != "set -g status on\n" {
		t.Fatal("an edited tmux.conf is overwritten")
	}
	if got := tmuxConfigFile(gate.Config{DataDir: data, Upstream: "127.0.0.1:1"}); got != "" {
		t.Fatalf("in front of the previous runtime: %q", got)
	}
	t.Setenv("RUNTIME_TMUX_CONF", "")
	if got := tmuxConfigFile(cfg); got != "" {
		t.Fatalf("set but empty: %q", got)
	}
	t.Setenv("RUNTIME_TMUX_CONF", "/etc/acme/tmux.conf")
	if got := tmuxConfigFile(cfg); got != "/etc/acme/tmux.conf" {
		t.Fatalf("named: %q", got)
	}
}

func TestWebPageOnlyWhereAsked(t *testing.T) {
	t.Setenv("RUNTIME_WEB_DIR", webDir(t))
	t.Setenv("RUNTIME_WEB", "")
	if webConfigFromEnv(false) != nil {
		t.Fatal("in front of the previous runtime the pages are its own unless RUNTIME_WEB=on")
	}
	t.Setenv("RUNTIME_WEB", "on")
	if webConfigFromEnv(false) == nil {
		t.Fatal("RUNTIME_WEB=on serves the page in front of the previous runtime too")
	}
	t.Setenv("RUNTIME_WEB", "off")
	if webConfigFromEnv(true) != nil {
		t.Fatal("RUNTIME_WEB=off serves no page")
	}
	t.Setenv("RUNTIME_WEB", "")
	t.Setenv("RUNTIME_WEB_DIR", t.TempDir())
	if webConfigFromEnv(true) != nil {
		t.Fatal("a folder without index.html serves no page")
	}
}

func TestWebPageNameAndMark(t *testing.T) {
	const head = `<link rel="icon" href="./logo.svg" type="image/svg+xml" />` +
		`<meta name="apple-mobile-web-app-title" content="klodik.tech" /><title>klodik.tech</title>`
	files := fstest.MapFS{"index.html": {Data: []byte(`<!doctype html><html><head>` + head + `</head></html>`)}}

	t.Run("left as built without settings", func(t *testing.T) {
		t.Setenv("RUNTIME_APP_NAME", "")
		t.Setenv("RUNTIME_APP_MARK_URL", "")
		raw, err := newWebConfig(files).page()
		if err != nil || !strings.Contains(string(raw), head) {
			t.Fatalf("%v %s", err, raw)
		}
	})
	t.Run("the installation's own", func(t *testing.T) {
		t.Setenv("RUNTIME_APP_NAME", "Acme & Co")
		t.Setenv("RUNTIME_APP_MARK_URL", "/agents/icon.svg")
		raw, err := newWebConfig(files).page()
		page := string(raw)
		for _, want := range []string{`<link rel="icon" href="/agents/icon.svg" />`, `content="Acme &amp; Co"`, `<title>Acme &amp; Co</title>`} {
			if err != nil || !strings.Contains(page, want) {
				t.Fatalf("no %s in %s (%v)", want, page, err)
			}
		}
		if strings.Contains(page, "logo.svg") || strings.Contains(page, ">klodik.tech<") {
			t.Fatalf("built tags left: %s", page)
		}
	})
}
