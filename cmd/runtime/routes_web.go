package main

import (
	"bytes"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/web"
)

// The browser front-end (web/). A standalone runtime serves it at its app base path (RUNTIME_APP_BASE_PATH, default
// /agents) and redirects "/" there; in front of the previous runtime the pages are someone else's, so the gate leaves
// those paths alone unless RUNTIME_WEB=on. The files come from RUNTIME_WEB_DIR, else from the binary (webui tag).

type webConfig struct {
	files fs.FS
	// base is where the page lives ("" for the root); api is the prefix its API calls take.
	base, api string
	// meta is the page config written into <meta name="hl-config"> (web_config.go).
	meta string
	// name and mark are the product name and the URL of its mark (RUNTIME_APP_NAME, RUNTIME_APP_MARK_URL) for the
	// browser's own tags: the tab icon and the name an installed app takes.
	name, mark string
	// static keeps the tags and gzipped copies of the page's files.
	static staticCache
}

// webConfigFromEnv is nil when this process serves no page.
func webConfigFromEnv(standalone bool) *webConfig {
	switch os.Getenv("RUNTIME_WEB") {
	case "off":
		return nil
	case "on":
	default:
		if !standalone {
			return nil
		}
	}
	files := web.Dist()
	if dir := os.Getenv("RUNTIME_WEB_DIR"); dir != "" {
		files = os.DirFS(dir)
	}
	if files == nil {
		return nil
	}
	if _, err := fs.Stat(files, "index.html"); err != nil {
		agent.Log("warn", "web: no index.html to serve; the page is off", agent.F{K: "err", V: err.Error()})
		return nil
	}
	// The API answers under the page's base too (apiAlias), so one proxy prefix covers both.
	return newWebConfig(files)
}

// newWebConfig serves files with the settings of this installation.
func newWebConfig(files fs.FS) *webConfig {
	base := appBase()
	pc := pageConfigFromEnv()
	c := &webConfig{files: files, base: base, api: base, meta: pageConfigMeta(pc), name: pc.ProductName}
	if pc.MarkURL != nil {
		c.mark = *pc.MarkURL
	}
	return c
}

// appBase is RUNTIME_APP_BASE_PATH as a path prefix: "/agents" by default, "" for the root.
func appBase() string {
	base := strings.TrimRight(env("RUNTIME_APP_BASE_PATH", "/agents"), "/")
	if base != "" && !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return base
}

// apiAlias lets a standalone runtime answer its API under its app base path as well: <base>/api/… is served as the
// /agents/api/… form every route already knows. Nothing changes for the default base, the root or other paths.
func apiAlias(base string, next http.Handler) http.Handler {
	if base == "" || base == "/agents" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, base)
		if ok && (rest == "/api" || strings.HasPrefix(rest, "/api/")) {
			r2 := new(http.Request)
			*r2 = *r
			u := *r.URL
			u.Path = "/agents" + rest
			if u.RawPath != "" {
				if raw, ok := strings.CutPrefix(u.RawPath, base); ok {
					u.RawPath = "/agents" + raw
				} else {
					u.RawPath = ""
				}
			}
			r2.URL = &u
			r2.RequestURI = u.RequestURI()
			r = r2
		}
		next.ServeHTTP(w, r)
	})
}

func (c *webConfig) matches(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead || isUpgrade(r) {
		return false
	}
	p := r.URL.Path
	if p == "/" {
		return true
	}
	rest, ok := strings.CutPrefix(p, c.base)
	if !ok || rest != "" && !strings.HasPrefix(rest, "/") {
		return false
	}
	return rest != "/api" && !strings.HasPrefix(rest, "/api/") && p != "/api" && !strings.HasPrefix(p, "/api/")
}

func (c *webConfig) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c.base != "" && (r.URL.Path == "/" || r.URL.Path == c.base) {
			http.Redirect(w, r, c.base+"/", http.StatusFound)
			return
		}
		name := strings.TrimPrefix(path.Clean(strings.TrimPrefix(r.URL.Path, c.base)), "/")
		if name != "" && name != "index.html" {
			if c.serveStatic(w, r, name) {
				return
			}
			if path.Ext(name) != "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Not found"}`))
				return
			}
		}
		// Any other path is the page's own route.
		raw, err := c.page()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		serveHTML(w, r, raw)
	}
}

// page is index.html with the base, the API prefixes and the page's settings of this installation filled in.
func (c *webConfig) page() ([]byte, error) {
	raw, err := fs.ReadFile(c.files, "index.html")
	if err != nil {
		return nil, err
	}
	raw = bytes.Replace(raw, []byte(`<base href="/" />`), []byte(`<base href="`+html.EscapeString(c.base)+`/" />`), 1)
	raw = bytes.Replace(raw, []byte(`<meta name="api-base" content="" />`),
		[]byte(`<meta name="api-base" content="`+html.EscapeString(c.api)+`" />`), 1)
	raw = bytes.Replace(raw, []byte(`<meta name="platform-api" content="/api/platform" />`),
		[]byte(`<meta name="platform-api" content="`+html.EscapeString(platformAPI)+`" />`), 1)
	if c.meta != "" {
		raw = bytes.Replace(raw, []byte(`<meta name="hl-config" content="{}" />`), []byte(`<meta name="hl-config" content="`+c.meta+`" />`), 1)
	}
	if c.mark != "" {
		raw = bytes.Replace(raw, []byte(`<link rel="icon" href="./logo.svg" type="image/svg+xml" />`),
			[]byte(`<link rel="icon" href="`+html.EscapeString(c.mark)+`" />`), 1)
	}
	if c.name != "" {
		name := html.EscapeString(c.name)
		raw = bytes.Replace(raw, []byte(`<meta name="apple-mobile-web-app-title" content="harnessly" />`),
			[]byte(`<meta name="apple-mobile-web-app-title" content="`+name+`" />`), 1)
		raw = bytes.Replace(raw, []byte(`<title>harnessly</title>`), []byte(`<title>`+name+`</title>`), 1)
	}
	return raw, nil
}
