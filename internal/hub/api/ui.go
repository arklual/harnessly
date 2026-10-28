package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ui serves the hub's page: index.html for each route of the app, and the files under /assets/. A route is any path
// that is not a file; a path that looks like a file and is not there is a 404.
type ui struct {
	fsys fs.FS
	// reload re-reads index.html on each request (HUB_WEB_DIR, for development); otherwise it is read once.
	reload bool

	mu    sync.Mutex
	index []byte
	csp   string
}

var inlineScript = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)

// contentSecurityPolicy allows the page's own files, and the inline scripts index.html carries (the theme before the
// first paint), each by its hash: nothing else may run. The page reaches only its own origin.
func contentSecurityPolicy(index []byte) string {
	scripts := []string{"'self'"}
	for _, m := range inlineScript.FindAllSubmatch(index, -1) {
		if bytes.Contains(bytes.ToLower(m[1]), []byte("src=")) {
			continue
		}
		sum := sha256.Sum256(m[2])
		scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(scripts, " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

func newUI(fsys fs.FS, reload bool) *ui { return &ui{fsys: fsys, reload: reload} }

func (u *ui) loadIndex() ([]byte, string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.index != nil && !u.reload {
		return u.index, u.csp, nil
	}
	b, err := fs.ReadFile(u.fsys, "index.html")
	if err != nil {
		return nil, "", err
	}
	u.index, u.csp = b, contentSecurityPolicy(b)
	return u.index, u.csp, nil
}

func (u *ui) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if u.serveFile(w, r, name) {
			return
		}
		if strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
	}
	u.serveIndex(w, r)
}

func (u *ui) serveIndex(w http.ResponseWriter, r *http.Request) {
	index, csp, err := u.loadIndex()
	if err != nil {
		http.Error(w, "The hub page is not available", http.StatusServiceUnavailable)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", csp)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(index)
	}
}

// serveFile serves one file of the build; false when there is none.
func (u *ui) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	f, err := u.fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		b, err := io.ReadAll(f)
		if err != nil {
			return false
		}
		rs = bytes.NewReader(b)
	}
	if strings.HasPrefix(name, "assets/") {
		// Built files carry a hash in their name: they never change.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, path.Base(name), time.Time{}, rs)
	return true
}

// noUI is what the hub answers at / when it was built without the page and HUB_WEB_DIR is not set.
const noUI = `<!doctype html><meta charset="utf-8"><title>harnessly hub</title>
<p style="font:16px system-ui;max-width:36rem;margin:3rem auto;padding:0 1rem">The hub is running, but this build has no web page.
Build it with <code>-tags hubui</code> after <code>pnpm build:hub</code>, or point <code>HUB_WEB_DIR</code> at a built <code>dist-hub</code> folder.
The API is up: <a href="/healthz">/healthz</a>.</p>`
