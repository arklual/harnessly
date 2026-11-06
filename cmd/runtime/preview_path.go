package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
)

// The preview by path (docs/superpowers/specs/2026-11-05-preview-path-mode-design.md): where a deployment has one host,
// one certificate and port 443 and nothing else (a company's load balancer), a slot cannot have an origin of its own.
// It lives under <appBase>/__pv/<slot>/ of the page's own origin instead, and the slot's answers carry a CSP sandbox
// without allow-same-origin: the previewed page runs on an opaque origin, so it sees neither the page's cookies nor its
// storage, and every request it makes is cross-site, which carries no Lax session and which guard refuses.

const (
	previewPathSegment = "/__pv/"
	// previewSandbox is everything a page of a dev server may need but the page's origin.
	previewSandbox = "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads allow-pointer-lock"
	// previewReloadMessage is what a previewed frame that reloaded past its prefix tells the pane.
	previewReloadMessage = "hl-preview-reload"
)

// previewModeSetting is RUNTIME_PREVIEW_MODE: auto (default), ports, origins or path.
func previewModeSetting() string {
	switch m := strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_PREVIEW_MODE"))); m {
	case "ports", "origins", "path":
		return m
	}
	return "auto"
}

// pathOnlySlots are slots without listeners: the preview by path only.
func pathOnlySlots() *previewSlots {
	return &previewSlots{spent: map[string]int64{}, mode: "path"}
}

// usesPath says whether a preview opened from app goes by path: forced, no listeners to give out, or an https page
// without public origins for the slots (which a page cannot frame over plain http).
func (s *previewSlots) usesPath(app *url.URL) bool {
	switch {
	case s.mode == "path" || len(s.listeners) == 0 && s.ports == nil:
		return true
	case s.mode == "ports" || s.mode == "origins" || s.public != nil:
		return false
	}
	return app.Scheme != "http"
}

// pathPrefix is the slot's prefix on the page's origin: <appBase>/__pv/<slot>-<key>. The key is the slot's whole
// authority: new with every open, 128 random bits, known only to the pane that opened it and the page it shows. A
// cookie cannot be it: an opaque-origin page loads its module scripts, workers and fonts in CORS mode, which carries no
// cookies, and a SameSite=None cookie would ride along in another site's sandboxed frame as well.
func pathPrefix(slot int, key string) string {
	return appBase() + previewPathSegment + strconv.Itoa(slot) + "-" + key
}

// newPathKey is a slot address's key: 128 random bits, lower-case hex.
func newPathKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var pathKeyRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// prefixOf is the path of a slot origin that has one ("" for an origin of its own).
func prefixOf(slotOrigin string) string {
	u, err := url.Parse(slotOrigin)
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.Path, "/")
}

// splitPreviewPath is the slot, its address key and the rest of a path under <appBase>/__pv/: slot 0 when the path is
// not one, -1 when it is a malformed one.
func splitPreviewPath(path string) (int, string, string) {
	rest, ok := strings.CutPrefix(path, appBase()+previewPathSegment)
	if !ok {
		return 0, "", ""
	}
	head, tail := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		head, tail = rest[:i], rest[i:]
	}
	digit, key, _ := strings.Cut(head, "-")
	n, err := strconv.Atoi(digit)
	if err != nil || len(digit) != 1 || n < 1 || n > previewSlotCount || !pathKeyRe.MatchString(key) {
		return -1, "", ""
	}
	if tail == "" {
		tail = "/"
	}
	return n, key, tail
}

// mount puts the slots by path in front of the page: <appBase>/__pv/<n>/… goes to the slot with the prefix taken off,
// a previewed frame that reloaded past its prefix gets the page that asks the pane to reopen it, and everything else
// passes guard.
func (s *previewSlots) mount(next http.Handler) http.Handler {
	if s == nil {
		return next
	}
	guarded := s.guard(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slot, key, tail := splitPreviewPath(r.URL.Path)
		if slot == 0 {
			if previewReloadNavigation(r) {
				previewReloadPage(w, r)
				return
			}
			guarded.ServeHTTP(w, r)
			return
		}
		if slot < 0 || slot > previewPortSlots {
			previewDeny(w, 404, locale.Pick("Not found", "Не найдено"))
			return
		}
		prefix := pathPrefix(slot, key)
		sess := s.byPath.get(key)
		if sess == nil || sess.slot != slot {
			// A key the runtime does not know (it restarted, or the slot expired): the pane opens the page again.
			previewReloadPage(w, r)
			return
		}
		out := r.Clone(r.Context())
		u := *r.URL
		u.Path = tail
		u.RawPath = ""
		if raw, ok := strings.CutPrefix(r.URL.RawPath, prefix); ok {
			u.RawPath = raw
		}
		out.URL = &u
		out.RequestURI = u.RequestURI()
		s.servePath(w, out, sess)
	})
}

// previewReloadNavigation: a frame navigating to a page of the origin from a cross-site context. The runtime's own pages
// are framed only by themselves (frame-ancestors 'self'), so this is a previewed page that reloaded or followed a link
// past its prefix (the bridge gave it the path without the prefix).
func previewReloadNavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Sec-Fetch-Dest") == "iframe" &&
		r.Header.Get("Sec-Fetch-Site") == "cross-site"
}

// previewReloadPage asks the pane to open the path again in the frame's slot. It holds nothing of the runtime.
func previewReloadPage(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; "+previewSandbox)
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	path, _ := json.Marshal(r.URL.RequestURI())
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><script>parent.postMessage({type:"` + previewReloadMessage +
		`",path:` + string(path) + `},"*")</script>`))
}

// opaqueRequest: a request from an opaque origin (a previewed page by path) or a cross-site one that is not a page
// navigation. guard refuses them on every path of the page.
func opaqueRequest(r *http.Request) bool {
	if r.Header.Get("Origin") == "null" {
		return true
	}
	return r.Header.Get("Sec-Fetch-Site") == "cross-site" && r.Header.Get("Sec-Fetch-Mode") != "navigate"
}

// pathSlotHeaders are what a slot by path adds to every answer: the sandbox, no referrer, and CORS for the opaque
// origin, whose every request to the page's host is cross-origin (module scripts, fonts, fetch).
func pathSlotHeaders(h http.Header) {
	// What a target says about its origin would be said about the page's: its storage, its transport, its workers.
	for _, name := range pathOriginHeaders {
		h.Del(name)
	}
	h.Add("Content-Security-Policy", previewSandbox)
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Access-Control-Allow-Origin", "null")
	h.Set("Access-Control-Allow-Credentials", "true")
	h.Add("Vary", "Origin")
}

// pathOriginHeaders are a target's answer headers that act on the whole origin (HSTS, Clear-Site-Data and the like):
// by path that origin is the page's own.
var pathOriginHeaders = []string{"Clear-Site-Data", "Strict-Transport-Security", "Service-Worker-Allowed", "Origin-Agent-Cluster",
	"Cross-Origin-Opener-Policy", "Cross-Origin-Embedder-Policy", "Cross-Origin-Resource-Policy", "Alt-Svc", "NEL", "Report-To",
	"Reporting-Endpoints", "Set-Login", "Supports-Loading-Mode", "Accept-CH", "Critical-CH", "Speculation-Rules", "No-Vary-Search",
	"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers",
	"Access-Control-Expose-Headers", "Access-Control-Max-Age", "Access-Control-Allow-Private-Network"}

// pathSessions are the open slots by path, by key. In memory: a key outlives neither the runtime nor its 12 hours.
type pathSessions struct {
	mu   sync.Mutex
	open map[string]pathEntry
}

type pathEntry struct {
	sess *previewSession
	exp  time.Time
}

// pathSessionsKept bounds the open keys: every open makes one, the oldest go first.
const pathSessionsKept = 256

func (p *pathSessions) put(key string, sess *previewSession) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.open == nil {
		p.open = map[string]pathEntry{}
	}
	oldest, oldestKey := now.Add(previewSlotTTL), ""
	for k, e := range p.open {
		if now.After(e.exp) {
			delete(p.open, k)
		} else if e.exp.Before(oldest) {
			oldest, oldestKey = e.exp, k
		}
	}
	if len(p.open) >= pathSessionsKept && oldestKey != "" {
		delete(p.open, oldestKey)
	}
	p.open[key] = pathEntry{sess: sess, exp: now.Add(previewSlotTTL)}
}

func (p *pathSessions) get(key string) *previewSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.open[key]
	if !ok || time.Now().After(e.exp) {
		return nil
	}
	return e.sess
}

// openByPath is POST /api/preview/open when the slot goes by path: no ticket and no cookie, the key in the address is
// the slot's authority, so the pane frames the page itself.
func (s *previewSlots) openByPath(d deps, r *http.Request, app *url.URL, slot int, path string, target devtools.Target,
	neighbours []int, set *previewSet) (int, any, http.Header) {
	refuse := func(status int, msg string) (int, any, http.Header) {
		return answerJSON(r, status, previewError{msg}, previewNoStore())
	}
	// Every slot by path shares the page's origin: only the person's own servers, never a site of the outside world.
	if target.Kind != "port" || slot > previewPortSlots {
		return refuse(400, "invalid slot")
	}
	key := newPathKey()
	origin := app.Scheme + "://" + app.Host + pathPrefix(slot, key)
	portSlots := map[string]string{strconv.Itoa(target.Port): origin}
	for _, n := range neighbours {
		portSlots[strconv.Itoa(n)] = origin + devtools.NeighbourPrefix + strconv.Itoa(n)
	}
	sub := "user"
	if c := d.Claims(r); c != nil {
		if v, isString := c["sub"].(string); isString && v != "" {
			sub = v
		}
	}
	s.byPath.put(key, &previewSession{sub: sub, origin: origin, app: app.Scheme + "://" + app.Host, slot: slot, target: target,
		targetJSON: previewTargetJSON(target), portSlots: portSlots, cleared: true})
	set.store.Touch(origin, target)
	return answerJSON(r, 200, struct {
		Slot     int    `json:"slot"`
		Origin   string `json:"origin"`
		EnterURL string `json:"enterUrl"`
		ByPath   bool   `json:"byPath"`
	}{slot, origin, origin + path, true}, previewNoStore())
}

// servePath answers a slot by path: its session came with the key, the rest is the slot's as on its own listener.
func (s *previewSlots) servePath(w http.ResponseWriter, r *http.Request, sess *previewSession) {
	set, secret, ok := s.ready()
	if !ok {
		previewDeny(w, 503, locale.Pick("The preview is unavailable", "Превью недоступно"))
		return
	}
	if pathPreflight(w, r) {
		return
	}
	if !previewPathAllowed(r.RequestURI) {
		previewDeny(w, 400, locale.Pick("Invalid address", "Некорректный адрес"))
		return
	}
	if r.URL.Path == previewStoragePath {
		servePreviewStorage(w, r, set.storageDir, sess)
		return
	}
	if previewReservedOddly(r.URL.Path) || r.URL.Path == previewEnterPath {
		previewDeny(w, 404, locale.Pick("Not found", "Не найдено"))
		return
	}
	// The page by path is an opaque origin: its own requests say Origin: null; the pane is the only other page that
	// drives it.
	if from := r.Header.Get("Origin"); from != "" && from != "null" && !sess.drives(from) {
		previewDeny(w, 403, locale.Pick("A request from another address", "Запрос с чужого адреса"))
		return
	}
	if r.Header.Get("Service-Worker") == "script" {
		previewDeny(w, 403, locale.Pick("Service workers are off in the preview", "Service worker в превью отключён"))
		return
	}
	s.forward(w, r, set, secret, sess)
}

// pathPreflight answers a CORS preflight of a slot by path itself: on localhost the page and its server were one
// origin and the server never saw one.
func pathPreflight(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
		return false
	}
	h := w.Header()
	pathSlotHeaders(h)
	h.Set("Access-Control-Allow-Methods", r.Header.Get("Access-Control-Request-Method"))
	if asked := r.Header.Get("Access-Control-Request-Headers"); asked != "" {
		h.Set("Access-Control-Allow-Headers", asked)
	}
	h.Set("Access-Control-Max-Age", "600")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
	return true
}
