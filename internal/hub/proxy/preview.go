package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A runtime shows the right pane's preview by path when its slots have no origins of their own (a company's one host,
// one certificate): /m/<machine>/__pv/<slot>-<key>/… on the hub's origin. The page there runs sandboxed on an opaque
// origin, so none of its requests carries the hub's session, and all of them are cross-site. The key in the address is
// the slot's whole authority: the runtime made it for a signed-in person's open and answers nothing without it. So the
// hub passes these paths without its session and its cross-site check, carrying only the target's own cookies
// (`__t_…`) both ways, under the slot's path.

var previewSegment = regexp.MustCompile(`^[1-6]-[0-9a-f]{32}$`)

// previewPath says whether path is a slot by path of machine id.
func previewPath(path, id string) bool {
	rest, ok := strings.CutPrefix(path, "/m/"+id+"/__pv/")
	if !ok {
		return false
	}
	seg, _, _ := strings.Cut(rest, "/")
	return previewSegment.MatchString(seg)
}

// servePreview forwards a slot by path to the machine without a person: the runtime decides by the key.
func (s *Service) servePreview(w http.ResponseWriter, r *http.Request, id string) {
	m, err := s.db.S().MachineByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || err == nil && (m.RevokedAt != nil || m.SpaceID == "") {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// The same trust as the machine's page: in path mode only a machine an administrator connected runs here.
	if err := s.trusted(r.Context(), m); err != nil {
		s.fail(w, r, err)
		return
	}
	// A service worker would outlive the page and serve the whole origin's paths: never on the hub's origin.
	if r.Header.Get("Service-Worker") != "" {
		httpx.WriteError(w, r, httpx.ErrForbidden)
		return
	}
	if s.reg.Get(m.ID) == nil {
		s.offline(w, r)
		return
	}
	prefix := "/m/" + id + "/__pv/"
	clientIP := s.auth.ClientIP(r)
	secure := s.cfg.Secure
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = "http", "runtime"
			pr.Out.Host = pr.In.Host
			h := pr.Out.Header
			h.Del("Authorization")
			h.Del("Proxy-Authorization")
			h.Del("Cookie")
			if own := targetCookies(pr.In); own != "" {
				h.Set("Cookie", own)
			}
			h.Set("X-Forwarded-Host", pr.In.Host)
			h.Set("X-Forwarded-Proto", map[bool]string{true: "https", false: "http"}[secure])
			h.Set("X-Forwarded-For", clientIP)
		},
		Transport:     s.transport(m.ID),
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			lines := res.Header.Values("Set-Cookie")
			res.Header.Del("Set-Cookie")
			for _, line := range lines {
				if previewCookieKept(line, prefix) {
					res.Header.Add("Set-Cookie", line)
				}
			}
			previewHeaders(res.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() != nil {
				return
			}
			s.upstreamFailed(w, r, err)
		},
	}
	rp.ServeHTTP(w, r)
}

// previewSandbox is the hub's own sandbox for a slot's answers: whatever the machine sends, nothing it serves under
// /__pv/ runs on the hub's origin, since these paths come in without the hub's session.
const previewSandbox = "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads allow-pointer-lock"

// previewOriginHeaders act on the whole origin, which here is the hub's.
var previewOriginHeaders = []string{"Clear-Site-Data", "Strict-Transport-Security", "Service-Worker-Allowed", "Origin-Agent-Cluster",
	"Cross-Origin-Opener-Policy", "Cross-Origin-Embedder-Policy", "Alt-Svc", "NEL", "Report-To", "Reporting-Endpoints", "Set-Login",
	"Supports-Loading-Mode", "Accept-CH", "Critical-CH", "Speculation-Rules", "No-Vary-Search"}

// previewHeaders enforce the slot's terms at the hub: the sandbox, no referrer, no word on the hub's origin, framed only
// by the hub's own pages.
func previewHeaders(h http.Header) {
	for _, name := range previewOriginHeaders {
		h.Del(name)
	}
	h.Add("Content-Security-Policy", previewSandbox)
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	frameHeaders(h)
}

// targetCookies are the request's cookies a previewed target set (`__t_<tag>_…`): never the hub's session.
func targetCookies(r *http.Request) string {
	var kept []string
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, "__t_") {
			kept = append(kept, c.Name+"="+c.Value)
		}
	}
	return strings.Join(kept, "; ")
}

// previewCookieKept: a target's cookie the runtime scoped under the machine's slots, and nothing else.
func previewCookieKept(line, prefix string) bool {
	if !strings.HasPrefix(line, "__t_") {
		return false
	}
	scoped := false
	for _, attr := range strings.Split(line, ";")[1:] {
		k, v, _ := strings.Cut(strings.TrimSpace(attr), "=")
		switch {
		case strings.EqualFold(k, "domain"):
			return false
		case strings.EqualFold(k, "path"):
			scoped = strings.HasPrefix(strings.TrimSpace(v), prefix)
		}
	}
	return scoped
}

// ReloadAsked: a frame navigating to a page of the hub from a cross-site context. Nothing of the hub or of a machine is
// framed by another site (frame-ancestors), so this is a previewed page by path that reloaded past its prefix.
func ReloadAsked(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Sec-Fetch-Dest") == "iframe" &&
		r.Header.Get("Sec-Fetch-Site") == "cross-site"
}

// ServeReload asks the pane to open the path again in the frame's slot; the page holds nothing of the hub.
func ServeReload(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; sandbox allow-scripts")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	path, _ := json.Marshal(r.URL.RequestURI())
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><script>parent.postMessage({type:"hl-preview-reload",path:` +
		string(path) + `},"*")</script>`))
}
