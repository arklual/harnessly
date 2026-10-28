package sites

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// TicketParam is the query parameter a ticket arrives in.
const TicketParam = "hl_site_ticket"

// PathCSP is what the path fallback sends with every page: the site runs on the server's own origin, so it is sandboxed
// into an opaque one (no allow-same-origin) and cannot touch the server's cookies or API.
const PathCSP = "sandbox allow-scripts allow-forms allow-popups allow-modals"

var mimeTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".json": "application/json",
	".map": "application/json", ".webmanifest": "application/manifest+json", ".svg": "image/svg+xml",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".avif": "image/avif", ".ico": "image/x-icon", ".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf",
	".otf": "font/otf", ".wasm": "application/wasm", ".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8",
	".csv": "text/csv; charset=utf-8", ".xml": "application/xml", ".pdf": "application/pdf", ".mp4": "video/mp4",
	".webm": "video/webm", ".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav", ".zip": "application/zip",
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if t, ok := mimeTypes[ext]; ok {
		return t
	}
	return mime.TypeByExtension(ext)
}

// hashed matches the file names a bundler gives its output (index-3f9a1c2b.js, app.4b2e9a7c1d.css): the name changes when
// the content does, so the file can be cached for good.
var hashed = regexp.MustCompile(`[-._][0-9A-Za-z]{8,}\.[A-Za-z0-9]+$`)

// IsSiteHost says whether a request host is an address of this service (a label under its domain).
func (s *Service) IsSiteHost(host string) bool {
	if s.cfg.Domain == "" {
		return false
	}
	h := HostName(host)
	return h == s.cfg.domainHost() || strings.HasSuffix(h, "."+s.cfg.domainHost())
}

// sanitize drops from the request what a site must not see or a visitor must not smuggle in: every cookie but the
// site's own session, and the headers a gateway in front uses to carry identity.
func (s *Service) sanitize(r *http.Request) {
	for k := range r.Header {
		l := strings.ToLower(k)
		for _, prefix := range s.cfg.StripHeaderPrefixes {
			if strings.HasPrefix(l, prefix) {
				delete(r.Header, k)
				break
			}
		}
	}
	keep := ""
	if c, err := r.Cookie(s.sessions.CookieName()); err == nil {
		keep = c.Name + "=" + c.Value
	}
	if keep == "" {
		r.Header.Del("Cookie")
	} else {
		r.Header.Set("Cookie", keep)
	}
}

// Edge serves the sites at their own addresses (<site>--<space>.<domain>). The host that is the bare domain, or a label
// that is not a site, gets the 404 page with the complaint link.
func (s *Service) Edge() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.sanitize(r)
		addr, ok := ParseHost(r.Host, s.cfg.Domain)
		if !ok {
			s.notFoundPage(w, r, true)
			return
		}
		s.serve(w, r, addr, r.URL.Path, false)
	})
}

// PathEdge serves the path fallback: /sites/<site>--<space>/… on the server's own origin.
func (s *Service) PathEdge() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/sites/")
		if !ok {
			s.notFoundPage(w, r, false)
			return
		}
		label, sub, _ := strings.Cut(rest, "/")
		addr, ok := ParseHost(label+".x", "x")
		if !ok {
			s.notFoundPage(w, r, false)
			return
		}
		if !strings.Contains(rest, "/") {
			http.Redirect(w, r, "/sites/"+label+"/", http.StatusMovedPermanently)
			return
		}
		s.serve(w, r, addr, "/"+sub, true)
	})
}

func (s *Service) cachedSite(ctx context.Context, space, name string) (Site, error) {
	key := space + "/" + name
	now := s.now()
	s.mu.Lock()
	c, ok := s.siteCache[key]
	s.mu.Unlock()
	if ok && now.Before(c.until) {
		return c.site, nil
	}
	site, err := s.repo.site(ctx, space, name)
	if err != nil {
		return Site{}, err
	}
	s.mu.Lock()
	if len(s.siteCache) > 4096 {
		s.siteCache = map[string]cachedSite{}
	}
	s.siteCache[key] = cachedSite{site: site, until: now.Add(siteCacheTTL)}
	s.mu.Unlock()
	return site, nil
}

type loaded struct {
	m  Manifest
	at time.Time
}

func (s *Service) manifestOf(ctx context.Context, site Site, version int) (Manifest, time.Time, error) {
	key := fmt.Sprintf("%s#%d", site.ID, version)
	s.mu.Lock()
	l, ok := s.manifests[key]
	s.mu.Unlock()
	if ok {
		return l.m, l.at, nil
	}
	d, err := s.repo.deployment(ctx, site.ID, version)
	if err != nil {
		return nil, time.Time{}, err
	}
	s.mu.Lock()
	if len(s.manifests) >= maxManifests {
		s.manifests = map[string]loaded{}
	}
	s.manifests[key] = loaded{m: d.Manifest, at: d.CreatedAt}
	s.mu.Unlock()
	return d.Manifest, d.CreatedAt, nil
}

// viewer decides whether the request may see a team-only site. It answers true, or has already answered the request
// (a ticket redeemed, a redirect to sign in, a refusal) and says false.
func (s *Service) viewer(w http.ResponseWriter, r *http.Request, site Site, host string, pathMode bool) bool {
	ctx := r.Context()
	nav := (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Upgrade") == ""

	var userID string
	if pathMode {
		// On the server's own origin the person's own session is the credential.
		p, err := s.auth.Person(r)
		if err != nil {
			s.log.Warn("sites: session lookup failed", "error", err)
			http.Error(w, "Что-то пошло не так", http.StatusInternalServerError)
			return false
		}
		if p != nil {
			userID = p.ID
		}
	} else {
		userID, _ = s.sessions.Read(r, host)
		if userID == "" && nav {
			if tk := r.URL.Query().Get(TicketParam); tk != "" {
				w.Header().Set("Referrer-Policy", "no-referrer")
				w.Header().Set("Cache-Control", "no-store")
				if uid, ok := s.auth.Redeem(tk, host); ok {
					if can, err := s.auth.CanView(ctx, uid, site.Space); err == nil && can {
						s.sessions.Set(w, host, uid)
						q := r.URL.Query()
						q.Del(TicketParam)
						http.Redirect(w, r, localURI(r.URL.Path, q.Encode()), http.StatusFound)
						return false
					}
					s.denied(w, r)
					return false
				}
				// One use, and it was spent (a reload of the ticket address): round again for a new one.
			}
		}
	}
	if userID == "" {
		if nav && strings.Contains(r.Header.Get("Accept"), "text/html") || nav && r.Header.Get("Accept") == "" {
			ret := s.scheme() + "://" + r.Host + localURI(r.URL.Path, r.URL.RawQuery)
			if pathMode {
				ret = localURI(r.URL.Path, r.URL.RawQuery)
			}
			w.Header().Set("Cache-Control", "no-store")
			entryHost := host
			if pathMode {
				entryHost = ""
			}
			http.Redirect(w, r, s.auth.ViewerEntry(entryHost, ret), http.StatusFound)
			return false
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "Нужно войти", http.StatusUnauthorized)
		return false
	}
	can, err := s.auth.CanView(ctx, userID, site.Space)
	if err != nil {
		s.log.Warn("sites: access check failed", "error", err)
		http.Error(w, "Что-то пошло не так", http.StatusInternalServerError)
		return false
	}
	if !can {
		s.denied(w, r)
		return false
	}
	return true
}

func (s *Service) denied(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.page(w, http.StatusForbidden, "Нет доступа", "Этот сайт открыт только участникам пространства. Попросите владельца добавить вас или открыть сайт всем, у кого есть ссылка.", "")
}

// serve answers one request for a deployment of a site.
func (s *Service) serve(w http.ResponseWriter, r *http.Request, addr Addr, urlPath string, pathMode bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	site, err := s.cachedSite(ctx, addr.Space, addr.Site)
	if errors.Is(err, ErrNotFound) {
		s.notFoundPage(w, r, !pathMode)
		return
	}
	if err != nil {
		s.log.Warn("sites: site lookup failed", "error", err)
		http.Error(w, "Что-то пошло не так", http.StatusInternalServerError)
		return
	}
	host := r.Host
	if site.Access == AccessTeam && !s.viewer(w, r, site, host, pathMode) {
		return
	}

	version := 0
	switch addr.Kind {
	case KindCurrent:
		version = site.CurrentVersion
	case KindVersion:
		version = addr.N
	case KindPreview:
		// Reserved for the previews of branches and pull requests.
		s.notFoundPage(w, r, !pathMode)
		return
	}
	if version == 0 {
		s.page(w, http.StatusNotFound, "Сайт пока пуст", "В этот сайт ещё ничего не опубликовано.", "")
		return
	}
	m, modified, err := s.manifestOf(ctx, site, version)
	if errors.Is(err, ErrNotFound) {
		s.notFoundPage(w, r, !pathMode)
		return
	}
	if err != nil {
		s.log.Warn("sites: manifest lookup failed", "error", err)
		http.Error(w, "Что-то пошло не так", http.StatusInternalServerError)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	trailing := strings.HasSuffix(urlPath, "/") || rel == ""
	file, name, status, redirect := s.pick(m, rel, trailing, site.SPA)
	if redirect {
		http.Redirect(w, r, localURI(r.URL.Path+"/", r.URL.RawQuery), http.StatusMovedPermanently)
		return
	}
	if status == http.StatusNotFound && file.Hash == "" {
		s.notFoundPage(w, r, !pathMode)
		return
	}

	rc, size, err := s.blobs.Open(ctx, file.Hash)
	if err != nil {
		s.log.Warn("sites: blob not served", "site", site.Name, "path", name, "error", err)
		http.Error(w, "Что-то пошло не так", http.StatusBadGateway)
		return
	}
	defer rc.Close()

	h := w.Header()
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	if pathMode {
		h.Set("Content-Security-Policy", PathCSP)
	}
	if ct := contentType(name); ct != "" {
		h.Set("Content-Type", ct)
	}
	h.Set("ETag", `"`+file.Hash+`"`)
	vis := "public"
	if site.Access == AccessTeam {
		vis = "private"
	}
	switch {
	case status != http.StatusOK:
		h.Set("Cache-Control", "no-cache")
	case addr.Kind == KindVersion:
		// A numbered version never changes.
		h.Set("Cache-Control", vis+", max-age=31536000, immutable")
	case hashed.MatchString(name):
		h.Set("Cache-Control", vis+", max-age=31536000, immutable")
	default:
		h.Set("Cache-Control", vis+", max-age=0, must-revalidate")
	}
	if status != http.StatusOK {
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, rc)
		}
		return
	}
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, modified, rs)
		return
	}
	h.Set("Content-Length", fmt.Sprint(size))
	if !modified.IsZero() {
		h.Set("Last-Modified", modified.UTC().Format(http.TimeFormat))
	}
	if r.Header.Get("If-None-Match") == `"`+file.Hash+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, rc)
	}
}

// pick finds the file for a request path: the file itself, a folder's index.html, name.html, the single-page fallback,
// and last the site's 404.html. redirect says that a folder was asked for without the trailing slash.
func (s *Service) pick(m Manifest, rel string, trailing, spa bool) (f File, name string, status int, redirect bool) {
	if !trailing {
		if f, ok := m[rel]; ok {
			return f, rel, http.StatusOK, false
		}
	}
	dir := rel
	if dir != "" {
		dir += "/"
	}
	if f, ok := m[dir+"index.html"]; ok {
		if !trailing {
			return File{}, "", 0, true
		}
		return f, dir + "index.html", http.StatusOK, false
	}
	if !trailing {
		if f, ok := m[rel+".html"]; ok {
			return f, rel + ".html", http.StatusOK, false
		}
	}
	if spa && !strings.Contains(path.Base(rel), ".") {
		if f, ok := m["index.html"]; ok {
			return f, "index.html", http.StatusOK, false
		}
	}
	if f, ok := m["404.html"]; ok {
		return f, "404.html", http.StatusNotFound, false
	}
	return File{}, "", http.StatusNotFound, false
}

// ---- pages the service writes itself ----

const pageHead = `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
	`<meta name="robots" content="noindex"><title>%s</title><style>` +
	`:root{color-scheme:light dark}body{margin:0;min-height:100vh;display:grid;place-items:center;font:16px/1.5 system-ui,sans-serif;background:#fff;color:#1a1a1a}` +
	`@media(prefers-color-scheme:dark){body{background:#141414;color:#eee}a{color:#8ab4ff}}` +
	`main{max-width:28rem;padding:2rem}h1{font-size:1.5rem;margin:0 0 .5rem}p{margin:.5rem 0;opacity:.8}</style></head><body><main>`

func (s *Service) page(w http.ResponseWriter, status int, title, text, extra string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
	w.WriteHeader(status)
	fmt.Fprintf(w, pageHead+`<h1>%s</h1><p>%s</p>%s</main></body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(text), extra)
}

func (s *Service) notFoundPage(w http.ResponseWriter, r *http.Request, bare bool) {
	extra := ""
	if bare && s.cfg.AbuseURL != "" {
		if u, err := url.Parse(s.cfg.AbuseURL); err == nil && (u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "mailto") {
			extra = `<p><a href="` + html.EscapeString(u.String()) + `" rel="noopener">Пожаловаться на сайт</a></p>`
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	s.page(w, http.StatusNotFound, "Такого сайта нет", "Адрес не ведёт ни на один сайт. Возможно, его удалили.", extra)
}

// localURI is a path (and query) that stays on this host when it is sent in a Location header: any run of slashes at
// its start becomes one, so `//evil.example` can never read as another host, and a backslash is no slash a browser may
// take for one.
func localURI(p, rawQuery string) string {
	out := "/" + strings.TrimLeft(strings.ReplaceAll(p, "\\", "/"), "/")
	if rawQuery != "" {
		out += "?" + rawQuery
	}
	return out
}
