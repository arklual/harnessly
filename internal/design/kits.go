package design

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// A UI kit is installed once by version (internal/designkit) and a board loads it from /kit/<id>/<version>/<file>
// instead of carrying a copy of it in the project: the library server reads <kits>/<id>/<version>/<file>.

// kitImmutable: the files of a version never change, so the browser keeps them for good.
const kitImmutable = "public, max-age=31536000, immutable"

// kitContentTypes adds to the project files' types what a kit ships next to them: docs and source maps.
var kitContentTypes = map[string]string{
	".md": "text/markdown; charset=utf-8", ".map": "application/json; charset=utf-8",
}

func kitContentType(ext string) string {
	ext = strings.ToLower(ext)
	if t, ok := kitContentTypes[ext]; ok {
		return t
	}
	return ContentType(ext)
}

// KitsDir is the folder the kits are installed in: RUNTIME_DESIGN_KITS_DIR, else design-kits in the data folder.
func (l *Library) KitsDir() string {
	if l.cfg.KitsDir != "" {
		if abs, err := filepath.Abs(l.cfg.KitsDir); err == nil {
			return abs
		}
		return filepath.Clean(l.cfg.KitsDir)
	}
	return designkit.DefaultDir(l.cfg.DataDir)
}

// kitOriginAllowed: a page of the library server itself or of a configured platform host may read kit files across
// origins. Anything else, "null" and "*" included, may not.
func (l *Library) kitOriginAllowed(origin string, port int) bool {
	if origin == "" {
		return false
	}
	p := strconv.Itoa(port)
	for _, own := range []string{"http://127.0.0.1:" + p, "http://localhost:" + p, "http://[::1]:" + p} {
		if origin == own {
			return true
		}
	}
	for _, o := range l.cfg.KitOrigins {
		if o = NormalizeOrigin(o); o != "" && o == origin {
			return true
		}
	}
	return false
}

// NormalizeOrigin is an origin as a browser sends it (scheme://host[:port], lower case, no path or trailing slash);
// "" for anything that is not one.
func NormalizeOrigin(v string) string {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return u.Scheme + "://" + strings.ToLower(u.Host)
}

// serveKit answers /kit/<id>/<version>/<path>; segments are ["<id>", "<version>", path...] decoded.
func (l *Library) serveKit(w http.ResponseWriter, r *http.Request, segments []string, port int, head bool) {
	if len(segments) < 3 {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	file := designkit.Resolve(l.KitsDir(), segments[0], segments[1], segments[2:])
	ext := strings.ToLower(extname(file))
	typ := ""
	if file != "" {
		typ = kitContentType(ext)
	}
	st, err := os.Stat(file)
	if typ == "" || file == "" || err != nil {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	// Fonts and stylesheets are CORS requests when a board is shown from another address: the origin is echoed back
	// only for the library's own loopback origins and the configured platform hosts, any other gets no header.
	w.Header().Add("Vary", "Origin")
	if origin := r.Header.Get("Origin"); l.kitOriginAllowed(origin, port) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	l.serveStatic(w, r, file, st, typ, ext, kitImmutable, head)
}

// serveKitList answers /__lib/kits: every installed kit with its versions; with ?kit=<id>&version=<v> that version's
// files.
func (l *Library) serveKitList(w http.ResponseWriter, q url.Values, head bool) {
	dir := l.KitsDir()
	id, hasID := search(q, "kit")
	version, hasVersion := search(q, "version")
	if hasID || hasVersion {
		files := designkit.Files(dir, id, version)
		if files == nil {
			send(w, 404, "application/json", notFoundJSON, head)
			return
		}
		send(w, 200, "application/json", nodejson.Marshal(struct {
			Kit     string   `json:"kit"`
			Version string   `json:"version"`
			Files   []string `json:"files"`
		}{id, version, files}), head)
		return
	}
	send(w, 200, "application/json", nodejson.Marshal(struct {
		Kits []designkit.Kit `json:"kits"`
	}{designkit.List(dir)}), head)
}
