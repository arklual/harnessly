package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/preview"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
	"github.com/ly-solutions/harnessly/web"
)

// A standalone runtime opens the right pane's preview itself, as the fleet gateway does in front of it: eight slot
// listeners on ports of their own (8023–8030 next to the default 8022), so a previewed page runs on an origin that is not
// the app's. POST /api/preview/open hands the pane a one-time ticket for a slot; the slot's /__tk/enter spends it and
// sets the slot's cookie; every later request of the slot goes to the preview proxy with a claim signed for it.

const (
	previewSlotCount      = 8
	previewPortSlots      = 6 // slots 1–6 show ports, 7–8 external sites
	previewMaxNeighbours  = 7
	previewMaxPath        = 2048
	previewEnterPath      = "/__tk/enter"
	previewTicketAudience = "harnessly-preview-ticket"
	previewSlotAudience   = "harnessly-preview-slot"
	previewTicketTTL      = 60 * time.Second
	previewSlotTTL        = 12 * time.Hour
	previewClaimTTL       = 300 * time.Second
	previewCookieName     = "hl_preview_"
)

// previewSlots are the slot listeners of a standalone runtime; nil when none could be opened or they are off.
type previewSlots struct {
	listeners []net.Listener
	ports     []int
	// public are the slots' origins behind a reverse proxy (RUNTIME_PREVIEW_PUBLIC_ORIGINS), in slot order; nil when
	// a slot's origin is the app's host with the slot's port.
	public []string
	set    atomic.Pointer[previewSet]
	secret atomic.Pointer[[]byte]

	mu    sync.Mutex
	spent map[string]int64
}

// standaloneSlots is what run() opened before building the gate; the preview slice attaches its proxy to it.
var standaloneSlots *previewSlots

// listenPreviewSlots binds the slot listeners: RUNTIME_PREVIEW_LISTEN names the first (host:port, the rest follow it),
// "off" opens none; by default they take the ports after the gate's own on its host. A port that is taken costs the
// preview, never the runtime.
func listenPreviewSlots(gateListen string, gatePort int) *previewSlots {
	asked := strings.TrimSpace(os.Getenv("RUNTIME_PREVIEW_LISTEN"))
	if asked == "off" {
		return nil
	}
	host, first := "", 0
	if asked != "" {
		h, p, err := net.SplitHostPort(asked)
		n, perr := strconv.Atoi(p)
		if err != nil || perr != nil || n < 0 || n > 65535-previewSlotCount {
			agent.Log("warn", "preview: RUNTIME_PREVIEW_LISTEN is not host:port; the preview is off", agent.F{K: "value", V: asked})
			return nil
		}
		host, first = h, n
	} else {
		host, _, _ = net.SplitHostPort(gateListen)
		if gatePort > 0 {
			first = gatePort + 1
		}
	}
	s := &previewSlots{spent: map[string]int64{}}
	for i := 0; i < previewSlotCount; i++ {
		port := 0
		if first > 0 {
			port = first + i
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			for _, l := range s.listeners {
				_ = l.Close()
			}
			agent.Log("warn", "preview: a slot port is taken; the preview is off (set RUNTIME_PREVIEW_LISTEN to move the slots)",
				agent.F{K: "error", V: err.Error()})
			return nil
		}
		s.listeners = append(s.listeners, ln)
		s.ports = append(s.ports, ln.Addr().(*net.TCPAddr).Port)
	}
	if raw := os.Getenv("RUNTIME_PREVIEW_PUBLIC_ORIGINS"); raw != "" {
		list := splitList(raw)
		ok := len(list) == previewSlotCount
		for i, o := range list {
			o = strings.TrimRight(strings.ToLower(o), "/")
			list[i] = o
			ok = ok && previewOriginRe.MatchString(o)
		}
		if ok {
			s.public = list
		} else {
			agent.Log("warn", "preview: RUNTIME_PREVIEW_PUBLIC_ORIGINS needs 8 origins; the slots use the app's host",
				agent.F{K: "value", V: raw})
		}
	}
	return s
}

var previewOriginRe = regexp.MustCompile(`^https?://(?:[a-z0-9-]+(?:\.[a-z0-9-]+)*|\[[0-9a-f:.]+\])(?::\d{1,5})?$`)

// attach gives the slots the preview slice's proxy and the pair's secret.
func (s *previewSlots) attach(set *previewSet, secret []byte) {
	if s == nil {
		return
	}
	s.set.Store(set)
	s.secret.Store(&secret)
}

func (s *previewSlots) ready() (*previewSet, []byte, bool) {
	if s == nil {
		return nil, nil, false
	}
	set, secret := s.set.Load(), s.secret.Load()
	if set == nil || secret == nil || len(*secret) == 0 {
		return nil, nil, false
	}
	return set, *secret, true
}

// serve answers on every slot until ctx ends.
func (s *previewSlots) serve(ctx context.Context) {
	if s == nil {
		return
	}
	servers := make([]*http.Server, len(s.listeners))
	for i, ln := range s.listeners {
		srv := &http.Server{Handler: s.slotHandler(i + 1), ReadHeaderTimeout: 10 * time.Second}
		servers[i] = srv
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				agent.Log("warn", "preview: a slot stopped", agent.F{K: "error", V: err.Error()})
			}
		}()
	}
	agent.Log("info", "preview slots listening", agent.F{K: "ports", V: s.ports})
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for _, srv := range servers {
			_ = srv.Shutdown(shutdown)
		}
	}()
}

// close lets the slot ports go when the runtime does not start.
func (s *previewSlots) close() {
	if s == nil {
		return
	}
	for _, ln := range s.listeners {
		_ = ln.Close()
	}
}

// hidden are the slot ports, which are never a dev server to show.
func (s *previewSlots) hidden() []int {
	if s == nil {
		return nil
	}
	return s.ports
}

// isSlotOrigin: a request from a previewed page. Cookies ignore ports, so the app's session goes along with whatever
// a slot page asks of the app's own host; only the Origin (or the Referer) tells them apart.
func (s *previewSlots) isSlotOrigin(origin string) bool {
	if s == nil || origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	o := strings.ToLower(u.Scheme + "://" + u.Host)
	for _, p := range s.public {
		if o == p {
			return true
		}
	}
	port, _ := strconv.Atoi(u.Port())
	for _, p := range s.ports {
		if port == p {
			return true
		}
	}
	return false
}

// guard refuses on the app's own listener what a previewed page sends there, sockets (the terminals) above all.
func (s *previewSlots) guard(next http.Handler) http.Handler {
	if s == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from := r.Header.Get("Origin")
		if from == "" {
			from = r.Header.Get("Referer")
		}
		if s.isSlotOrigin(from) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"Forbidden"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// slotOrigin is where a slot is reached from a page at app: the public origin named for it, else the app's host with
// the slot's port. An https page cannot frame a plain http slot, so it needs public origins.
func (s *previewSlots) slotOrigin(slot int, app *url.URL) (string, bool) {
	if s.public != nil {
		return s.public[slot-1], true
	}
	if app.Scheme != "http" {
		return "", false
	}
	return "http://" + net.JoinHostPort(app.Hostname(), strconv.Itoa(s.ports[slot-1])), true
}

// appOrigin is the origin of the page that asked: its Origin, else the scheme and Host it was reached at.
func appOrigin(r *http.Request) (*url.URL, bool) {
	raw := r.Header.Get("Origin")
	if raw == "" || raw == "null" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		raw = scheme + "://" + r.Host
	}
	raw = strings.ToLower(raw)
	if !previewOriginRe.MatchString(raw) {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	return u, true
}

func previewHasDotSegment(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		l := strings.ToLower(seg)
		l = strings.ReplaceAll(l, "%2e", ".")
		if l == "." || l == ".." {
			return true
		}
	}
	return false
}

// previewPathAllowed: a path of the slot itself, never a host (`//x`) nor a way out of it (dot segments).
func previewPathAllowed(raw string) bool {
	p, _, _ := strings.Cut(raw, "?")
	return strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !previewHasDotSegment(p)
}

// previewPathValid is the path the pane asks to open: printable ASCII, no backslash, at most 2048 characters.
func previewPathValid(v string) bool {
	if len(v) > previewMaxPath || strings.Contains(v, `\`) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x21 || v[i] > 0x7e {
			return false
		}
	}
	return previewPathAllowed(v)
}

var previewNeighbourPathRe = regexp.MustCompile(`^/__tk/p/[1-9]\d{0,4}(/|$)`)

// previewReservedOddly: /__tk/ belongs to the runtime in every spelling; an unknown or oddly spelled one is answered
// here and never reaches a dev server.
func previewReservedOddly(path string) bool {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return true
	}
	decoded = strings.ToLower(decoded)
	if decoded != "/__tk" && !strings.HasPrefix(decoded, "/__tk/") {
		return false
	}
	switch path {
	case previewEnterPath, preview.BridgePath, "/__tk/devtools/config", "/__tk/devtools/report":
		return false
	}
	return !previewNeighbourPathRe.MatchString(path)
}

func previewIsPort(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f < 1 || f > 65535 {
		return 0, false
	}
	return int(f), true
}

func previewTargetJSON(t devtools.Target) map[string]any {
	if t.Kind == "port" {
		return map[string]any{"kind": "port", "port": t.Port}
	}
	return map[string]any{"kind": "url", "origin": t.Origin}
}

func newJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// spend marks a ticket used; false when it was.
func (s *previewSlots) spend(jti string, exp float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	for id, until := range s.spent {
		if until < now {
			delete(s.spent, id)
		}
	}
	if _, used := s.spent[jti]; used {
		return false
	}
	s.spent[jti] = int64(exp)
	return true
}

// open is POST /api/preview/open of a standalone runtime: the checks of the fleet gateway, then a ticket for the slot.
func (s *previewSlots) open(d deps, r *http.Request, body any) (int, any, http.Header) {
	refuse := func(status int, msg string) (int, any, http.Header) {
		return answerJSON(r, status, previewError{msg}, previewNoStore())
	}
	set, secret, ok := s.ready()
	if !ok {
		return refuse(503, "preview slots unavailable")
	}
	o, isObject := body.(*jsvalue.Object)
	if !isObject {
		return refuse(400, "invalid body")
	}
	rawTarget, _ := o.Get("target")
	target, ok := preview.ParseTarget(rawTarget)
	if !ok {
		return refuse(400, "invalid target")
	}
	rawSlot, _ := o.Get("slot")
	slot, ok := previewIsPort(rawSlot)
	if !ok || slot > previewSlotCount || target.Kind == "port" && slot > previewPortSlots || target.Kind == "url" && slot <= previewPortSlots {
		return refuse(400, "invalid slot")
	}
	path := "/"
	if raw, given := o.Get("path"); given {
		p, isString := raw.(string)
		if !isString || !previewPathValid(p) {
			return refuse(400, "invalid path")
		}
		path = p
	}
	if _, given := o.Get("related"); given {
		return refuse(400, "invalid neighbours")
	}
	if _, given := o.Get("slots"); given {
		return refuse(400, "invalid neighbours")
	}
	var neighbours []int
	if raw, given := o.Get("neighbours"); given {
		list, isList := raw.([]any)
		if !isList || len(list) > previewMaxNeighbours || target.Kind == "url" && len(list) > 0 {
			return refuse(400, "invalid neighbours")
		}
		seen := map[int]bool{target.Port: true}
		for _, item := range list {
			port, ok := previewIsPort(item)
			if !ok || seen[port] {
				return refuse(400, "invalid neighbours")
			}
			seen[port] = true
			neighbours = append(neighbours, port)
		}
	}
	app, ok := appOrigin(r)
	if !ok {
		return refuse(400, "invalid origin")
	}
	origin, ok := s.slotOrigin(slot, app)
	if !ok {
		return refuse(503, "preview slots need https origins")
	}
	portSlots := map[string]string{}
	if target.Kind == "port" {
		portSlots[strconv.Itoa(target.Port)] = origin
		for _, n := range neighbours {
			portSlots[strconv.Itoa(n)] = origin + devtools.NeighbourPrefix + strconv.Itoa(n)
		}
	}
	sub := "user"
	if c := d.Claims(r); c != nil {
		if v, isString := c["sub"].(string); isString && v != "" {
			sub = v
		}
	}
	ticket := agent.SignPayload(secret, previewTicketAudience, map[string]any{
		"sub": sub, "slot": slot, "path": path, "target": previewTargetJSON(target), "origin": origin, "portSlots": portSlots,
		"app": app.Scheme + "://" + app.Host, "jti": newJTI(),
	}, previewTicketTTL)
	set.store.Touch(origin, target)
	return answerJSON(r, 200, struct {
		Slot     int    `json:"slot"`
		Origin   string `json:"origin"`
		EnterURL string `json:"enterUrl"`
	}{slot, origin, origin + previewEnterPath + "?t=" + url.QueryEscape(ticket)}, previewNoStore())
}

// previewSession is a slot cookie's (or a ticket's) content.
type previewSession struct {
	sub, origin, app string
	slot             int
	target           devtools.Target
	targetJSON       any
	portSlots        any
	cleared          bool
}

func previewSessionOf(p map[string]any) (*previewSession, bool) {
	s := &previewSession{targetJSON: p["target"], portSlots: p["portSlots"]}
	var ok bool
	if s.sub, ok = p["sub"].(string); !ok {
		return nil, false
	}
	if s.origin, ok = p["origin"].(string); !ok {
		return nil, false
	}
	if s.app, ok = p["app"].(string); !ok {
		return nil, false
	}
	if s.slot, ok = previewIsPort(p["slot"]); !ok {
		return nil, false
	}
	t, _ := p["target"].(map[string]any)
	switch t["kind"] {
	case "port":
		port, ok := previewIsPort(t["port"])
		if !ok {
			return nil, false
		}
		s.target = devtools.Target{Kind: "port", Port: port}
	case "url":
		origin, ok := t["origin"].(string)
		if !ok {
			return nil, false
		}
		s.target = devtools.Target{Kind: "url", Origin: origin}
	default:
		return nil, false
	}
	s.cleared, _ = p["cleared"].(bool)
	return s, true
}

func (s *previewSession) fields() map[string]any {
	return map[string]any{"sub": s.sub, "slot": s.slot, "target": s.targetJSON, "origin": s.origin, "portSlots": s.portSlots,
		"app": s.app, "cleared": s.cleared}
}

// slotCookie is the name of a slot's cookie: one host carries every slot (ports share cookies), so the port is in it.
func (s *previewSlots) slotCookie(slot int, secure bool) string {
	name := previewCookieName + strconv.Itoa(s.ports[slot-1])
	if secure {
		return "__Host-" + name
	}
	return name
}

func isSlotCookieLine(line string) bool {
	return strings.HasPrefix(line, previewCookieName) || strings.HasPrefix(line, "__Host-"+previewCookieName)
}

// session is the slot's cookie when it is valid and names this slot.
func (s *previewSlots) session(r *http.Request, slot int, secret []byte) *previewSession {
	for _, secure := range []bool{false, true} {
		name := s.slotCookie(slot, secure)
		for _, c := range r.Cookies() {
			if c.Name != name {
				continue
			}
			p, ok := agent.VerifiedAudience(secret, c.Value, previewSlotAudience, time.Now())
			if !ok {
				continue
			}
			if sess, ok := previewSessionOf(p); ok && sess.slot == slot {
				return sess
			}
		}
	}
	return nil
}

func previewDeny(w http.ResponseWriter, status int, text string) {
	preview.WritePage(w, status, text)
}

func previewExpiredText() string {
	return locale.Pick("Open the preview from the app", "Откройте превью из приложения")
}

// ownOrigin is the slot's origin as this request reached it.
func (s *previewSlots) ownOrigin(r *http.Request, slot int) string {
	if s.public != nil {
		return s.public[slot-1]
	}
	return "http://" + strings.ToLower(r.Host)
}

func (s *previewSlots) slotHandler(slot int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		set, secret, ok := s.ready()
		if !ok {
			previewDeny(w, 503, locale.Pick("The preview is unavailable", "Превью недоступно"))
			return
		}
		upgrade := r.Header.Get("Upgrade") != ""
		own := s.ownOrigin(r, slot)
		from := r.Header.Get("Origin")
		if !previewPathAllowed(r.RequestURI) {
			previewDeny(w, 400, locale.Pick("Invalid address", "Некорректный адрес"))
			return
		}
		if r.URL.Path == previewEnterPath && !upgrade {
			s.enter(w, r, slot, secret, from)
			return
		}
		if previewReservedOddly(r.URL.Path) {
			previewDeny(w, 404, locale.Pick("Not found", "Не найдено"))
			return
		}
		sess := s.session(r, slot, secret)
		if sess == nil {
			previewDeny(w, 401, previewExpiredText())
			return
		}
		// Only the slot itself and the app drive a slot: a sibling slot is another target, maybe an external site.
		if from != "" && from != own && from != sess.app {
			previewDeny(w, 403, locale.Pick("A request from another address", "Запрос с чужого адреса"))
			return
		}
		// A service worker would outlive the target it came with; dev servers keep theirs off in dev mode anyway.
		if r.Header.Get("Service-Worker") == "script" {
			previewDeny(w, 403, locale.Pick("Service workers are off in the preview", "Service worker в превью отключён"))
			return
		}
		s.forward(w, r, set, secret, sess)
	})
}

// enter spends a ticket of the pane: the slot's cookie for its target, then the page it asked for.
func (s *previewSlots) enter(w http.ResponseWriter, r *http.Request, slot int, secret []byte, from string) {
	p, ok := agent.VerifiedAudience(secret, r.URL.Query().Get("t"), previewTicketAudience, time.Now())
	ticket, valid := previewSessionOf(p)
	jti, _ := p["jti"].(string)
	exp, _ := p["exp"].(float64)
	if !ok || !valid || ticket.slot != slot || jti == "" || from != "" && from != ticket.app || !s.spend(jti, exp) {
		previewDeny(w, 401, previewExpiredText())
		return
	}
	path, _ := p["path"].(string)
	if !previewPathValid(path) {
		previewDeny(w, 400, locale.Pick("Invalid path", "Некорректный путь"))
		return
	}
	previous := s.session(r, slot, secret)
	h := w.Header()
	// Storage, IndexedDB and a service worker of whatever the slot showed before stay with the slot's origin: wiped
	// unless the slot already showed this target after a wipe. Not "cookies": those are the app's host's too.
	if previous == nil || previous.target != ticket.target || !previous.cleared {
		h.Set("Clear-Site-Data", `"cache", "storage"`)
	}
	ticket.cleared = true
	secure := strings.HasPrefix(ticket.origin, "https:")
	cookie := &http.Cookie{Name: s.slotCookie(slot, secure), Value: agent.SignPayload(secret, previewSlotAudience, ticket.fields(), previewSlotTTL),
		Path: "/", HttpOnly: true, MaxAge: int(previewSlotTTL / time.Second), SameSite: http.SameSiteLaxMode}
	if secure {
		cookie.Secure, cookie.SameSite, cookie.Partitioned = true, http.SameSiteNoneMode, true
	}
	http.SetCookie(w, cookie)
	h.Set("Location", path)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Content-Security-Policy", previewFrameAncestors(ticket.app))
	w.WriteHeader(http.StatusFound)
}

func previewFrameAncestors(app string) string { return "frame-ancestors " + app + " 'self'" }

// forward hands the request to the preview proxy with a claim for the slot's target, as the gateway would.
func (s *previewSlots) forward(w http.ResponseWriter, r *http.Request, set *previewSet, secret []byte, sess *previewSession) {
	cfg := set.cfg
	claim := map[string]any{"sub": sess.sub, "target": sess.targetJSON, "slotOrigin": sess.origin, "portSlots": sess.portSlots}
	if cfg.Bridge != nil {
		if _, ok := cfg.Bridge(); ok {
			claim["parentOrigin"] = sess.app
		}
	}
	out := r.Clone(r.Context())
	for name := range out.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-forwarded-") || lower == "forwarded" ||
			cfg.PrivateHeaderPrefix != "" && strings.HasPrefix(lower, cfg.PrivateHeaderPrefix) {
			out.Header.Del(name)
		}
	}
	var kept []string
	for _, c := range r.Cookies() {
		if strings.HasPrefix(c.Name, "__t_") {
			kept = append(kept, c.Name+"="+c.Value)
		}
	}
	out.Header.Del("Cookie")
	if len(kept) > 0 {
		out.Header.Set("Cookie", strings.Join(kept, "; "))
	}
	out.Header.Set(cfg.ClaimHeader, agent.SignPayload(secret, cfg.Audience, claim, previewClaimTTL))
	out.RequestURI = cfg.Path + r.RequestURI
	u := *r.URL
	u.Path = cfg.Path + r.URL.Path
	if u.RawPath != "" {
		u.RawPath = cfg.Path + r.URL.RawPath
	}
	out.URL = &u
	set.proxy.ServeHTTP(&slotWriter{ResponseWriter: w, frame: previewFrameAncestors(sess.app)}, out)
}

// slotWriter puts the slot's own headers on what the proxy answers: no shared cache, no indexing, framed only by the
// app, and no slot cookie set by a target.
type slotWriter struct {
	http.ResponseWriter
	frame string
	fixed bool
}

func (w *slotWriter) fix() {
	if w.fixed {
		return
	}
	w.fixed = true
	h := w.Header()
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Cache-Control", previewPrivateCacheControl(strings.Join(h.Values("Cache-Control"), ", ")))
	h.Add("Content-Security-Policy", w.frame)
	if lines := h.Values("Set-Cookie"); len(lines) > 0 {
		h.Del("Set-Cookie")
		for _, line := range lines {
			if !isSlotCookieLine(line) {
				h.Add("Set-Cookie", line)
			}
		}
	}
}

func (w *slotWriter) WriteHeader(code int) {
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.fix()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *slotWriter) Write(b []byte) (int, error) {
	w.fix()
	return w.ResponseWriter.Write(b)
}

func (w *slotWriter) Flush() {
	w.fix()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *slotWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w *slotWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

var previewSharedCacheRe = regexp.MustCompile(`(?i)^(public|private|s-maxage(=.*)?|proxy-revalidate)$`)

// previewPrivateCacheControl: a slot is one person's, so no shared cache may keep its answers; no-store stays as sent.
func previewPrivateCacheControl(value string) string {
	var parts []string
	for _, p := range strings.Split(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	for _, p := range parts {
		if strings.EqualFold(p, "no-store") {
			return strings.Join(parts, ", ")
		}
	}
	out := []string{"private"}
	for _, p := range parts {
		if !previewSharedCacheRe.MatchString(p) {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// previewOpenRoute is POST /api/preview/open of a standalone runtime.
func previewOpenRoute(d deps) gate.JSONRoute {
	return previewSessionRoute(d, "/api/preview/open", []string{"POST"}, func(r *http.Request, body any) (int, any, http.Header) {
		return standaloneSlots.open(d, r, body)
	})
}

// previewBridgeFromWeb is the page bridge's script built next to the page's files (web/dist/preview-bridge.js).
func previewBridgeFromWeb() func() ([]byte, bool) {
	if dir := os.Getenv("RUNTIME_WEB_DIR"); dir != "" {
		return previewBridgeScript(dir + "/preview-bridge.js")
	}
	files := web.Dist()
	if files == nil {
		return nil
	}
	script, err := fs.ReadFile(files, "preview-bridge.js")
	if err != nil {
		return nil
	}
	return func() ([]byte, bool) { return script, true }
}
