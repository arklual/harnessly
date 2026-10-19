package preview

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"harnessly/internal/jsvalue"
	"harnessly/internal/nodejson"
	"harnessly/internal/preview/devtools"
	"harnessly/internal/preview/weburl"
)

// Config is what the deployment and the gate give the proxy.
type Config struct {
	// Path is the proxy's own path; the rest of a request's path is the upstream's.
	Path string
	// ClaimHeader carries the gateway's claim; Audience is the claim's.
	ClaimHeader string
	Audience    string
	Secret      []byte
	// PrivateHeaderPrefix marks the headers only the gateway sets; none of them reaches an upstream.
	PrivateHeaderPrefix string
	// Forbidden are the ports never opened: the runtime's own and the platform's.
	Forbidden func() map[int]bool
	Stands    Stands
	// SelfIPs are the machine's public addresses no interface carries (behind NAT), comma-separated.
	SelfIPs string
	Store   *devtools.Store
	// Cache keeps stand assets; nil keeps none.
	Cache *AssetCache
	// Bridge is the page bridge's script; with none the request goes to Fallback (the previous runtime, which has it).
	Bridge   func() ([]byte, bool)
	Fallback http.HandlerFunc

	// Tests: where names point, the address of a port, the CA of an https target, shorter timeouts.
	Resolve      func(host string) Resolved
	UpstreamHost string
	RootCAs      *x509.CertPool
	Timeouts     Timeouts
	// Dial connects to a checked address; tests point a stand's port 443 at their own server.
	Dial func(ctx context.Context, address string) (net.Conn, error)
}

// Timeouts of a url target: connect, until the headers, and a body that stops flowing. A WebSocket, once upgraded, has
// none.
type Timeouts struct {
	Connect, Headers, Idle time.Duration
}

// Proxy is the preview's reverse proxy.
type Proxy struct {
	cfg       Config
	resolver  *resolveCache
	transport *http.Transport
}

type dialKey struct{}

type dialTarget struct {
	address    string // host:port to connect to
	serverName string // TLS name; "" for an address target
}

func New(cfg Config) *Proxy {
	if cfg.Timeouts.Connect == 0 {
		cfg.Timeouts = Timeouts{Connect: 5 * time.Second, Headers: 15 * time.Second, Idle: 60 * time.Second}
	}
	if cfg.Dial == nil {
		cfg.Dial = func(ctx context.Context, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: cfg.Timeouts.Connect}).DialContext(ctx, "tcp", address)
		}
	}
	p := &Proxy{cfg: cfg}
	resolve := cfg.Resolve
	if resolve == nil {
		resolve = func(host string) Resolved { return ResolvePublic(host, nil, SelfAddresses(cfg.SelfIPs)) }
	}
	p.resolver = &resolveCache{entries: map[string]resolveEntry{}, ttl: 5 * time.Second, resolve: resolve}
	p.transport = &http.Transport{
		Proxy:               nil,
		DisableCompression:  true,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     5 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if t, ok := ctx.Value(dialKey{}).(dialTarget); ok {
				addr = t.address
			}
			return cfg.Dial(ctx, addr)
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			t, _ := ctx.Value(dialKey{}).(dialTarget)
			if t.address == "" {
				t.address = addr
			}
			raw, err := cfg.Dial(ctx, t.address)
			if err != nil {
				return nil, err
			}
			name := t.serverName
			if name == "" {
				name, _, _ = net.SplitHostPort(t.address)
			}
			conn := tls.Client(raw, &tls.Config{ServerName: name, RootCAs: cfg.RootCAs, NextProtos: []string{"http/1.1"}})
			if err := conn.HandshakeContext(ctx); err != nil {
				_ = raw.Close()
				return nil, err
			}
			return conn, nil
		},
	}
	return p
}

// Matches: the request is for the proxy. The path is matched as sent, before anything is taken off it.
func (p *Proxy) Matches(r *http.Request) bool {
	u := r.RequestURI
	return u == p.cfg.Path || strings.HasPrefix(u, p.cfg.Path+"/") || strings.HasPrefix(u, p.cfg.Path+"?")
}

func (p *Proxy) upstreamPath(r *http.Request) string {
	rest := r.RequestURI[len(p.cfg.Path):]
	if rest == "" {
		return "/"
	}
	if strings.HasPrefix(rest, "?") {
		return "/" + rest
	}
	return rest
}

// route is where one request goes: a port (the main one, or a neighbour the claim names under /__tk/p/<port> of the
// main slot) or an external site with the address its name resolved to when it was checked.
type route struct {
	kind     string
	port     int
	path     string
	basePath string // the neighbour prefix its absolute-path redirects must keep; "" for the main port
	url      *weburl.URL
	address  string
}

// neighbourPath is `^/__tk/p/([1-9]\d{0,4})(?=[/?]|$)`: the port and the length of the prefix.
func neighbourPath(path string) (string, int) {
	if !strings.HasPrefix(path, devtools.NeighbourPrefix) {
		return "", 0
	}
	rest := path[len(devtools.NeighbourPrefix):]
	if rest == "" || rest[0] < '1' || rest[0] > '9' {
		return "", 0
	}
	n := 1
	for n < len(rest) && n < 5 && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	if n < len(rest) && rest[n] != '/' && rest[n] != '?' {
		return "", 0
	}
	return rest[:n], len(devtools.NeighbourPrefix) + n
}

// routeOf is nil for a neighbour the claim does not name; a padded, glued or out-of-range port is a path of the main
// port.
func routeOf(c *Claim, mainPort int, path string) *route {
	main := &route{kind: "port", port: mainPort, path: path}
	digits, n := neighbourPath(path)
	if n == 0 {
		return main
	}
	port, _ := strconv.Atoi(digits)
	if port > 65535 || port == mainPort {
		return main
	}
	if _, ok := c.PortSlots[digits]; !ok {
		return nil
	}
	// A bare neighbour origin ends with '/', so base + '/x' arrives as '//x', which a dev server would read as a host.
	rest := strings.TrimLeft(path[n:], "/")
	return &route{kind: "port", port: port, path: "/" + rest, basePath: path[:n]}
}

func joinHeader(vs []string) string { return strings.Join(vs, ", ") }

// header is a request header as the previous runtime's HTTP library read it.
func header(r *http.Request, name string) (string, bool) {
	vs := r.Header.Values(name)
	if len(vs) == 0 {
		return "", false
	}
	if strings.EqualFold(name, "cookie") {
		return strings.Join(vs, "; "), true
	}
	if firstOnly[strings.ToLower(name)] {
		return vs[0], true
	}
	return joinHeader(vs), true
}

var followedDestinations = map[string]bool{"script": true, "style": true, "worker": true}

// neighbourRedirect: a script, style or worker asked for by a page under /__tk/p/<n>/ of this slot belongs to that
// neighbour (a Vite remote imports /@vite/client and /src/… from the root, which is the shell's).
func neighbourRedirect(r *http.Request, c *Claim, rt *route) string {
	if rt.kind != "port" || rt.basePath != "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
		return ""
	}
	dest, _ := header(r, "Sec-Fetch-Dest")
	if !followedDestinations[dest] {
		return ""
	}
	refRaw, ok := header(r, "Referer")
	if !ok {
		return ""
	}
	ref, ok := weburl.Parse(refRaw)
	if !ok || ref.Origin() != c.SlotOrigin {
		return ""
	}
	from := routeOf(c, rt.port, ref.Path)
	if from == nil || from.basePath == "" {
		return ""
	}
	return from.basePath + rt.path
}

var (
	dropped = map[string]bool{"connection": true, "keep-alive": true, "proxy-connection": true, "transfer-encoding": true, "te": true,
		"trailer": true, "upgrade": true, "host": true, "cookie": true, "origin": true, "referer": true, "accept-encoding": true, "forwarded": true}
	conditional = map[string]bool{"if-none-match": true, "if-modified-since": true, "if-match": true, "if-unmodified-since": true, "if-range": true}
)

func identityOf(rt *route) (host, origin string) {
	if rt.kind == "port" {
		h := "localhost:" + strconv.Itoa(rt.port)
		return h, "http://" + h
	}
	return rt.url.Host(), rt.url.Origin()
}

func (p *Proxy) cookieTag(c *Claim) string { return CookieTag(c.Target, p.cfg.Secret) }

func (p *Proxy) isStandRoute(rt *route) bool {
	return rt.kind == "url" && p.cfg.Stands.IsOrigin(rt.url.Origin())
}

// upstreamHeaders is what the upstream should believe it is serving: localhost:<port> for a dev server, the site itself
// for a url target.
func (p *Proxy) upstreamHeaders(r *http.Request, c *Claim, rt *route, upgrade, noCache bool) Headers {
	host, origin := identityOf(rt)
	in := HeadersOf(r.Header)
	out := Headers{}
	private := strings.ToLower(p.cfg.PrivateHeaderPrefix)
	for name, v := range in {
		if dropped[name] || private != "" && strings.HasPrefix(name, private) || strings.HasPrefix(name, "x-forwarded-") || noCache && conditional[name] {
			continue
		}
		out[name] = v
	}
	if noCache {
		out.Set("cache-control", "no-cache")
		out.Set("pragma", "no-cache")
	}
	out.Set("host", host)
	if rt.kind == "url" && !upgrade {
		out.Set("accept-encoding", upstreamAcceptEncoding(in.Get("accept-encoding")))
	} else {
		out.Set("accept-encoding", "identity")
	}
	own := targetCookies(in.Get("cookie"), p.cookieTag(c))
	cookie := own
	if p.isStandRoute(rt) {
		session, _ := header(r, p.cfg.Stands.SessionHeader)
		if p.cfg.Stands.SessionHeader == "" {
			session = ""
		}
		cookie = standCookie(own, session)
	}
	if cookie != "" {
		out.Set("cookie", cookie)
	}
	if in.Get("origin") != "" {
		out.Set("origin", origin)
	}
	if in.Has("referer") {
		out.Set("referer", strings.Replace(in.Get("referer"), c.SlotOrigin, origin, 1))
	}
	if upgrade {
		out.Set("connection", "Upgrade")
		if up := in.Get("upgrade"); in.Has("upgrade") {
			out.Set("upgrade", up)
		} else {
			out.Set("upgrade", "websocket")
		}
	}
	return out
}

// upstreamHostFor: a dev server bound only to ::1 refuses 127.0.0.1; probe first so request bodies are never replayed.
func (p *Proxy) upstreamHostFor(port int) string {
	if p.cfg.UpstreamHost != "" {
		return p.cfg.UpstreamHost
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 5*time.Second)
	if err == nil {
		_ = conn.Close()
		return "127.0.0.1"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "::1"
	}
	return "127.0.0.1"
}

func writePageHeaders(h http.Header) {
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
}

// reply answers with one of the runtime's own pages; past the headers it can only cut the connection.
func reply(w *trackedWriter, status int, text string) {
	if w.wrote {
		panic(http.ErrAbortHandler)
	}
	writePageHeaders(w.Header())
	w.WriteHeader(status)
	_, _ = io.WriteString(w, statusPage(status, text))
}

var documentDestinations = map[string]bool{"document": true, "iframe": true, "frame": true}

// isDocumentRequest: documents only, an HTML fragment fetched by the app must stay as it is. Without Sec-Fetch-Dest it
// is a document.
func isDocumentRequest(r *http.Request) bool {
	dest, ok := header(r, "Sec-Fetch-Dest")
	return !ok || documentDestinations[dest]
}

var htmlType = regexp.MustCompile(`(?i)^text/html\b`)

// refuse answers a request DevTools refuse (blocked, offline): a document gets a page saying why, anything else an
// empty error the page sees as a failed load.
func refuse(w *trackedWriter, r *http.Request, status int, text, why string) {
	if isDocumentRequest(r) {
		reply(w, status, text)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("X-Tk-Devtools", why)
	w.WriteHeader(status)
}

const offlineText = "Нет сети — включён режим «Офлайн» в DevTools"

func blockedText(pattern string) string {
	return "Запрос заблокирован в DevTools: " + pattern
}

// targetURLOf is the address a request really goes to, as DevTools show it.
func targetURLOf(rt *route, websocket bool) string {
	if rt.kind == "port" {
		scheme := "http"
		if websocket {
			scheme = "ws"
		}
		return scheme + "://localhost:" + strconv.Itoa(rt.port) + rt.path
	}
	if !websocket {
		return rt.url.Origin() + rt.path
	}
	scheme := "ws"
	if rt.url.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + rt.url.Host() + rt.path
}

type admission struct {
	claim  *Claim
	route  *route
	status int
	text   string
}

func (p *Proxy) admit(r *http.Request) admission {
	token, _ := header(r, p.cfg.ClaimHeader)
	c := VerifyClaim(token, p.cfg.Secret, p.cfg.Audience, time.Now())
	if c == nil {
		return admission{status: 401, text: "Сессия превью устарела — нажмите «Перезагрузить» в окне превью"}
	}
	path := p.upstreamPath(r)
	if c.Target.Kind == "url" {
		u, _ := weburl.Parse(c.Target.Origin)
		res := p.resolver.get(u.Hostname)
		switch res.Error {
		case "forbidden":
			return admission{status: 403, text: "Адрес во внутренней сети — открыть нельзя"}
		case "":
		default:
			return admission{status: 502, text: "Не удалось найти адрес " + u.Host()}
		}
		return admission{claim: c, route: &route{kind: "url", url: u, address: res.Address, path: path}}
	}
	rt := routeOf(c, c.Target.Port, path)
	if rt == nil {
		return admission{status: 404, text: "Этот порт не открыт в превью"}
	}
	forbidden := map[int]bool{}
	if p.cfg.Forbidden != nil {
		forbidden = p.cfg.Forbidden()
	}
	for _, port := range []int{c.Target.Port, rt.port} {
		if forbidden[port] {
			return admission{status: 403, text: "Порт " + strconv.Itoa(port) + " служебный, его нельзя открыть в превью"}
		}
	}
	return admission{claim: c, route: rt}
}

// trackedWriter knows whether the headers went out and counts nothing else.
type trackedWriter struct {
	http.ResponseWriter
	wrote  bool
	status int
}

func (t *trackedWriter) WriteHeader(code int) {
	if !t.wrote {
		t.wrote, t.status = true, code
		t.ResponseWriter.WriteHeader(code)
	}
}

func (t *trackedWriter) Write(b []byte) (int, error) {
	if !t.wrote {
		t.WriteHeader(200)
	}
	n, err := t.ResponseWriter.Write(b)
	if f, ok := t.ResponseWriter.(http.Flusher); ok && err == nil {
		f.Flush()
	}
	return n, err
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Upgrade") != "" && headerHasToken(r.Header, "Connection", "upgrade") {
		p.serveUpgrade(w, r)
		return
	}
	p.serveRequest(&trackedWriter{ResponseWriter: w}, r)
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

func (p *Proxy) serveRequest(w *trackedWriter, r *http.Request) {
	a := p.admit(r)
	if a.claim == nil {
		reply(w, a.status, a.text)
		return
	}
	c, rt := a.claim, a.route
	slot := c.SlotOrigin
	store := p.cfg.Store
	store.Touch(slot, c.Target)
	ownPath, _, _ := strings.Cut(p.upstreamPath(r), "?")
	if ownPath == BridgePath {
		p.serveBridge(w, r)
		return
	}
	if isDevtoolsPath(ownPath) {
		p.AnswerDevtools(w, r, ownPath, slot, c.Target)
		return
	}
	settings := store.Settings(slot)
	target := targetURLOf(rt, false)
	log := p.logRequest(slot, r, target, devtools.IsThrottled(settings.Throttle))
	defer func() {
		if v := recover(); v != nil {
			log.close(w, false)
			panic(v)
		}
		log.close(w, w.wrote)
	}()
	if devtools.IsOffline(settings.Throttle) {
		log.fail("offline", false)
		refuse(w, r, 503, offlineText, "offline")
		return
	}
	if pattern := devtools.BlockedBy(target, settings.Blocked); pattern != "" {
		log.fail("blocked: "+pattern, true)
		refuse(w, r, 403, blockedText(pattern), "blocked")
		return
	}
	if to := neighbourRedirect(r, c, rt); to != "" {
		h := w.Header()
		h.Set("Location", to)
		h.Set("Cache-Control", "no-store")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		w.WriteHeader(307)
		return
	}
	if ms := settings.Throttle.LatencyMs; ms > 0 {
		t := time.NewTimer(time.Duration(ms) * time.Millisecond)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			return
		}
	}
	noStore := settings.DisableCache
	var cache *AssetCache
	if !noStore && rt.kind == "url" && p.cfg.Cache != nil && p.cfg.Stands.IsHost(rt.url.Hostname) {
		cache = p.cfg.Cache
	}
	in := HeadersOf(r.Header)
	key := ""
	if cache != nil && requestAllowsCache(r.Method, in) {
		key = assetCacheKey(c.Sub, upstreamAcceptEncoding(in.Get("accept-encoding")), rt.url.Origin()+rt.path)
	}
	if key != "" {
		if status, h, body, ok := cache.Lookup(key); ok {
			defer body.Close()
			if nm, ok := notModifiedFor(in, h); ok {
				_ = body.Close()
				p.deliver(w, r, c, rt, answer{status: 304, headers: nm, body: http.NoBody}, delivery{cache: "hit", noStore: noStore, log: log})
				return
			}
			p.deliver(w, r, c, rt, answer{status: status, headers: h, body: body}, delivery{cache: "hit", noStore: noStore, log: log})
			return
		}
	}
	res, cancel, err := p.roundTrip(r, c, rt, noStore, log)
	if err != nil {
		cancel()
		log.fail(nodeCode(err), false)
		status, text := failure(err, rt)
		reply(w, status, text)
		return
	}
	defer cancel()
	defer res.Body.Close()
	ans := answer{status: res.StatusCode, headers: HeadersOf(res.Header), body: res.Body}
	if rt.kind == "url" && p.answerRedirect(w, r, c, rt, &ans) {
		return
	}
	d := delivery{noStore: noStore, log: log}
	if key != "" {
		if ttl, ok := storeVerdict(r.Method, ans.status, ans.headers); ok {
			ans.body = cache.Tee(key, ans.status, ans.headers, ttl, ans.body)
			d.cache = "store"
		}
	}
	p.deliver(w, r, c, rt, ans, d)
}

// serveBridge answers the page bridge's script.
func (p *Proxy) serveBridge(w *trackedWriter, r *http.Request) {
	script, ok := []byte(nil), false
	if p.cfg.Bridge != nil {
		script, ok = p.cfg.Bridge()
	}
	if !ok {
		if p.cfg.Fallback != nil {
			p.cfg.Fallback(w.ResponseWriter, r)
			return
		}
		reply(w, 502, "Скрипт превью недоступен")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Content-Length", strconv.Itoa(len(script)))
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(script)
	}
}

// entityTags are the tags of an If-None-Match or ETag value, weak marks dropped.
func entityTags(v string) []string {
	var out []string
	for _, t := range strings.Split(v, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// notModifiedFor: the browser holds this very file; say so without reading it from disk.
func notModifiedFor(req Headers, cached Headers) (Headers, bool) {
	wanted := entityTags(req.Get("if-none-match"))
	tags := entityTags(cached.Get("etag"))
	if len(tags) == 0 {
		return nil, false
	}
	match := false
	for _, w := range wanted {
		if w == "*" || w == tags[0] {
			match = true
		}
	}
	if !match {
		return nil, false
	}
	out := Headers{}
	for _, name := range []string{"etag", "cache-control", "expires", "last-modified", "vary"} {
		if cached.Has(name) {
			out[name] = cached[name]
		}
	}
	return out, true
}

type answer struct {
	status  int
	headers Headers
	body    io.Reader
}

type delivery struct {
	cache   string
	noStore bool
	log     *requestLog
}

func hasBody(method string, status int) bool {
	return method != http.MethodHead && status >= 200 && status != 204 && status != 304
}

var (
	rewrittenTypes = regexp.MustCompile(`(?i)^(text/(html|css|javascript)|application/(javascript|json|manifest\+json|importmap\+json)|[a-z]+/[a-z.+-]*\+json)\b`)
	noTransform    = regexp.MustCompile(`(?i)\bno-transform\b`)
)

type bodyPlan struct {
	rewrite        bool
	decode, encode string
	changed        bool
}

func planOf(r *http.Request, a answer) bodyPlan {
	coding := codingOf(a.headers)
	rewrite := rewrittenTypes.MatchString(a.headers.Get("content-type")) && coding != ""
	decode := ""
	if rewrite && coding != "identity" {
		decode = coding
	}
	kept := coding != "identity" && decode == ""
	length := math.NaN()
	if a.headers.Has("content-length") {
		length = jsNumber(a.headers.Get("content-length"))
	}
	finite := !math.IsNaN(length) && !math.IsInf(length, 0)
	worthIt := rewrite || !finite || length >= minCompressBytes
	body := hasBody(r.Method, a.status)
	transformable := body && a.status == 200 && !a.headers.Has("content-range") && !noTransform.MatchString(a.headers.Get("cache-control"))
	encode := ""
	if !kept && transformable && worthIt && isCompressible(a.headers.Get("content-type")) {
		accept, _ := header(r, "Accept-Encoding")
		encode = outgoingCoding(accept)
	}
	return bodyPlan{rewrite: rewrite, decode: decode, encode: encode, changed: body && (rewrite || encode != "")}
}

// bridgeDevtools: the page's conditions from its first request on; without any, the bridge asks for them as it goes.
func (p *Proxy) bridgeDevtools(c *Claim) (string, bool) {
	s := p.cfg.Store.Settings(c.SlotOrigin)
	if len(s.Blocked) == 0 && !devtools.IsThrottled(s.Throttle) {
		return "", false
	}
	t := c.Target
	return string(nodejson.Marshal(bridgeConfigOf(s, &t))), true
}

func (p *Proxy) deliver(w *trackedWriter, r *http.Request, c *Claim, rt *route, a answer, d delivery) {
	ctx := rewriteCtxOf(c)
	tag := p.cookieTag(c)
	plan := planOf(r, a)
	html := htmlType.MatchString(a.headers.Get("content-type"))
	bridge := plan.rewrite && c.ParentOrigin != "" && html && isDocumentRequest(r)
	basePath := ""
	if rt.kind == "port" {
		basePath = rt.basePath
	}
	h := ctx.rewriteResponseHeaders(a.headers, p.cfg.Stands, headerOptions{bodyRewritten: plan.rewrite, basePath: basePath, cookieTag: tag, stand: p.isStandRoute(rt)})
	if d.noStore {
		h.Set("cache-control", "no-store")
	} else {
		h.Set("cache-control", slotCacheControl(a.headers, html))
	}
	if bridge || d.noStore {
		markDocument(h)
	}
	if plan.changed {
		describeBody(h, plan.encode, plan.encode != "" || plan.decode != "")
	}
	if d.cache != "" {
		h.Set("x-tk-asset-cache", d.cache)
	}
	d.log.answered(h.Get("content-type"), h.Has("content-type"))
	h.writeTo(w.Header())
	if !h.Has("content-type") {
		w.Header()["Content-Type"] = nil
	}
	if !h.Has("date") {
		w.Header()["Date"] = nil
	}
	w.WriteHeader(a.status)
	if !hasBody(r.Method, a.status) {
		_, _ = io.Copy(io.Discard, a.body)
		if t, ok := a.body.(*teeReader); ok {
			t.Abandon()
		}
		return
	}
	pacer := p.cfg.Store.NewPacer(c.SlotOrigin, true, w, r.Context().Done(), d.log.sent)
	var out io.Writer = pacer
	var closers []io.Closer
	if plan.encode != "" {
		enc := encoderFor(plan.encode, out)
		out, closers = enc, append(closers, enc)
	}
	if bridge {
		devJSON, has := p.bridgeDevtools(c)
		j := &bridgeInjector{w: out, tag: []byte(bridgeTag(c.ParentOrigin, tag, devJSON, has))}
		out, closers = j, append(closers, j)
	}
	if plan.rewrite {
		rw := newOriginRewriter(out, ctx)
		out, closers = rw, append(closers, rw)
	}
	body := a.body
	if plan.decode != "" {
		dec, err := decoderFor(plan.decode, body)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		body = dec
	}
	if _, err := io.Copy(out, body); err != nil {
		if t, ok := a.body.(*teeReader); ok {
			t.Abandon()
		}
		panic(http.ErrAbortHandler)
	}
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].Close(); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

const (
	signInRequired = "Стенд закрыт входом через Keycloak"
)

var redirects = map[int]bool{301: true, 302: true, 303: true, 307: true, 308: true}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// needsSignIn: a stand without the person's session sends them to its sign-in; that chain needs the person's browser
// and is never followed in the slot.
func (p *Proxy) needsSignIn(rt *route, a *answer) bool {
	if !p.cfg.Stands.IsOrigin(rt.url.Origin()) {
		return false
	}
	if a.status == 401 || a.status == 403 {
		return strings.HasPrefix(rt.path, signInPath)
	}
	if !redirects[a.status] || !a.headers.Has("location") {
		return false
	}
	base, ok := weburl.ParseRef(rt.path, rt.url)
	if !ok {
		return false
	}
	next, ok := weburl.ParseRef(a.headers.Get("location"), base)
	return ok && p.cfg.Stands.isSignInRedirect(rt.url.Origin(), next)
}

func writeOffer(w *trackedWriter, nonce, page string) {
	h := w.Header()
	writePageHeaders(h)
	h.Set("Content-Security-Policy", offerPolicy(nonce))
	w.WriteHeader(200)
	_, _ = io.WriteString(w, page)
}

var schemeOrDoubleSlash = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:|^//`)

// answerRedirect: within the site a redirect stays on the slot; to another origin it is never followed here. True when
// it was answered here instead of passed on.
func (p *Proxy) answerRedirect(w *trackedWriter, r *http.Request, c *Claim, rt *route, a *answer) bool {
	if p.needsSignIn(rt, a) {
		_, _ = io.Copy(io.Discard, a.body)
		if !isDocumentRequest(r) {
			reply(w, 401, signInRequired)
			return true
		}
		url := p.cfg.Stands.signInURL(rt.url.Origin() + rt.path)
		if url == "" {
			reply(w, 502, "Не удалось собрать адрес входа на стенд")
			return true
		}
		nonce := newNonce()
		writeOffer(w, nonce, signInPage(url, c.ParentOrigin, nonce))
		return true
	}
	if !redirects[a.status] || !a.headers.Has("location") {
		return false
	}
	location := a.headers.Get("location")
	var next *weburl.URL
	base, ok := weburl.ParseRef(rt.path, rt.url)
	if ok {
		next, ok = weburl.ParseRef(location, base)
	}
	invalid := !ok || next.Scheme != "https" && next.Scheme != "http"
	if !invalid && next.Origin() == rt.url.Origin() {
		if schemeOrDoubleSlash.MatchString(location) {
			a.headers.Set("location", next.Path+next.Search()+next.Hash())
		}
		return false
	}
	_, _ = io.Copy(io.Discard, a.body)
	if invalid {
		reply(w, 502, "Сайт перенаправляет на адрес, который нельзя открыть в превью")
		return true
	}
	if !isDocumentRequest(r) {
		reply(w, 502, "Сайт перенаправляет на "+next.Origin())
		return true
	}
	nonce := newNonce()
	writeOffer(w, nonce, offerPage(next, c.ParentOrigin, nonce))
	return true
}

// watchdog cuts a url target's request when it stops making progress: until the headers, then while the body flows.
type watchdog struct {
	mu      sync.Mutex
	timer   *time.Timer
	limit   time.Duration
	expired bool
}

func (d *watchdog) arm(limit time.Duration, cancel func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.limit = limit
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(limit, func() {
		d.mu.Lock()
		d.expired = true
		d.mu.Unlock()
		cancel()
	})
}

func (d *watchdog) progress() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil && !d.expired {
		d.timer.Reset(d.limit)
	}
}

func (d *watchdog) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
	}
}

func (d *watchdog) timedOut() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.expired
}

var errTimedOut = errors.New("preview upstream timed out")

// idleBody re-arms the watchdog with every read.
type idleBody struct {
	io.ReadCloser
	dog *watchdog
}

func (b idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.dog.progress()
	}
	if err != nil && err != io.EOF && b.dog.timedOut() {
		err = errTimedOut
	}
	return n, err
}

func (p *Proxy) roundTrip(r *http.Request, c *Claim, rt *route, noStore bool, log *requestLog) (*http.Response, context.CancelFunc, error) {
	h := p.upstreamHeaders(r, c, rt, false, noStore)
	ctx, cancel := context.WithCancel(r.Context())
	u := &url.URL{Scheme: "http"}
	path, query, _ := strings.Cut(rt.path, "?")
	u.Opaque, u.RawQuery = path, query
	if strings.Contains(rt.path, "?") && query == "" {
		u.ForceQuery = true
	}
	dog := &watchdog{}
	if rt.kind == "port" {
		u.Host = net.JoinHostPort(p.upstreamHostFor(rt.port), strconv.Itoa(rt.port))
	} else {
		port := rt.url.Port
		if port == "" {
			port = "80"
			if rt.url.Scheme == "https" {
				port = "443"
			}
		}
		if rt.url.Scheme == "https" {
			u.Scheme = "https"
		}
		name := strings.Trim(rt.url.Hostname, "[]")
		u.Host = net.JoinHostPort(name, port)
		target := dialTarget{address: net.JoinHostPort(rt.address, port)}
		if net.ParseIP(name) == nil {
			target.serverName = name
		}
		ctx = context.WithValue(ctx, dialKey{}, target)
		dog.arm(p.cfg.Timeouts.Connect+p.cfg.Timeouts.Headers, cancel)
	}
	var upload io.Reader = http.NoBody
	if r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
		pr, pw := io.Pipe()
		go func() {
			pacer := p.cfg.Store.NewPacer(c.SlotOrigin, false, pw, ctx.Done(), func(int) { dog.progress() })
			_, err := io.Copy(pacer, r.Body)
			pw.CloseWithError(err)
		}()
		upload = pr
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, "http://placeholder/", upload)
	if err != nil {
		return nil, cancel, err
	}
	req.URL = u
	req.Host = h.Get("host")
	req.ContentLength = r.ContentLength
	req.Header = http.Header{}
	for name, vs := range h {
		switch name {
		case "host", "content-length":
			continue
		}
		for _, v := range vs {
			req.Header.Add(name, v)
		}
	}
	if !h.Has("user-agent") {
		req.Header["User-Agent"] = []string{""}
	}
	res, err := p.transport.RoundTrip(req)
	if err != nil {
		if dog.timedOut() {
			err = errTimedOut
		}
		dog.stop()
		return nil, cancel, err
	}
	if rt.kind == "url" {
		dog.arm(p.cfg.Timeouts.Idle, cancel)
		res.Body = idleBody{res.Body, dog}
		log.progress = dog.progress
	}
	return res, func() { dog.stop(); cancel() }, nil
}

var tlsCode = regexp.MustCompile(`CERT|SSL|TLS|ISSUER|SIGNATURE`)

// nodeCode is the error code the previous runtime's HTTP library would have named.
func nodeCode(err error) string {
	var certInvalid x509.CertificateInvalidError
	var unknown x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var tlsErr *tls.CertificateVerificationError
	switch {
	case errors.Is(err, errTimedOut), errors.Is(err, os.ErrDeadlineExceeded):
		return "ETIMEDOUT"
	case errors.As(err, &hostErr):
		return "ERR_TLS_CERT_ALTNAME_INVALID"
	case errors.As(err, &certInvalid):
		if certInvalid.Reason == x509.Expired {
			return "CERT_HAS_EXPIRED"
		}
		return "CERT_INVALID"
	case errors.As(err, &unknown):
		return "UNABLE_TO_VERIFY_LEAF_SIGNATURE"
	case errors.As(err, &tlsErr):
		return "UNABLE_TO_VERIFY_LEAF_SIGNATURE"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "ECONNREFUSED"
	case errors.Is(err, syscall.ECONNRESET):
		return "ECONNRESET"
	case errors.Is(err, syscall.EHOSTUNREACH):
		return "EHOSTUNREACH"
	case errors.Is(err, syscall.ENETUNREACH):
		return "ENETUNREACH"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "ECONNRESET"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "ETIMEDOUT"
	}
	return "failed"
}

func failure(err error, rt *route) (int, string) {
	if rt.kind == "port" {
		return 502, "Сервер на порту " + strconv.Itoa(rt.port) + " не отвечает"
	}
	host := rt.url.Host()
	code := nodeCode(err)
	switch {
	case code == "ETIMEDOUT":
		return 504, host + " не ответил вовремя"
	case tlsCode.MatchString(code):
		return 502, "Сертификат " + host + " не прошёл проверку"
	}
	return 502, host + " не отвечает"
}

// requestLog is the proxy's side of the request log: one entry per slot request, filled in when its response ends.
type requestLog struct {
	store       *devtools.Store
	slot        string
	id          int64
	at          time.Time
	destination string
	hasDest     bool
	bytes       int64
	failure     *string
	blocked     bool
	contentType string
	hasType     bool
	progress    func()
}

var destinationTypes = map[string]string{"document": "document", "iframe": "document", "frame": "document", "script": "script", "worker": "script",
	"sharedworker": "script", "serviceworker": "script", "audioworklet": "script", "paintworklet": "script", "style": "style", "image": "image",
	"font": "font", "audio": "media", "video": "media", "track": "media", "manifest": "manifest", "empty": "fetch"}

var mimeTypes = []struct {
	re  *regexp.Regexp
	typ string
}{
	{regexp.MustCompile(`^text/html\b`), "document"},
	{regexp.MustCompile(`javascript|ecmascript`), "script"},
	{regexp.MustCompile(`^text/css\b`), "style"},
	{regexp.MustCompile(`^image/`), "image"},
	{regexp.MustCompile(`^font/|font-woff|vnd\.ms-fontobject`), "font"},
	{regexp.MustCompile(`^(audio|video)/`), "media"},
	{regexp.MustCompile(`manifest\+json`), "manifest"},
	{regexp.MustCompile(`json|xml|^text/plain\b|event-stream`), "fetch"},
}

// resourceTypeOf: by Sec-Fetch-Dest when the browser sent it, else by what came back.
func resourceTypeOf(dest string, hasDest bool, mime *string) string {
	if hasDest {
		if t, ok := destinationTypes[dest]; ok {
			return t
		}
		return "other"
	}
	if mime != nil {
		for _, m := range mimeTypes {
			if m.re.MatchString(*mime) {
				return m.typ
			}
		}
	}
	return "other"
}

func mimeOf(contentType string) *string {
	m := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if m == "" {
		return nil
	}
	return &m
}

func hostOf(u string) string {
	if parsed, ok := weburl.Parse(u); ok {
		return parsed.Host()
	}
	return ""
}

func (p *Proxy) logRequest(slot string, r *http.Request, target string, throttled bool) *requestLog {
	dest, hasDest := header(r, "Sec-Fetch-Dest")
	l := &requestLog{store: p.cfg.Store, slot: slot, at: p.cfg.Store.Now(), destination: dest, hasDest: hasDest}
	method := r.Method
	l.id = l.store.RecordRequest(slot, devtools.NetworkEntry{At: l.at.UnixMilli(), Source: "proxy", Method: method, URL: target, Host: hostOf(target),
		Type: resourceTypeOf(dest, hasDest, nil), Throttled: throttled})
	return l
}

func (l *requestLog) sent(n int) {
	l.bytes += int64(n)
	if l.progress != nil {
		l.progress()
	}
}

func (l *requestLog) answered(contentType string, has bool) {
	l.contentType, l.hasType = contentType, has
}

func (l *requestLog) fail(err string, blocked bool) { l.failure, l.blocked = &err, blocked }

// close fills the entry in; finished says the response went out whole.
func (l *requestLog) close(w *trackedWriter, finished bool) {
	var mime *string
	if l.failure == nil && l.hasType {
		mime = mimeOf(l.contentType)
	}
	answered := l.failure == nil && w.wrote
	l.store.Finish(l.slot, l.id, func(e *devtools.NetworkEntry) {
		e.Status, e.Size = nil, nil
		if answered {
			st, size := int64(w.status), l.bytes
			e.Status, e.Size = &st, &size
		}
		e.Mime = mime
		d := l.store.Now().Sub(l.at).Milliseconds()
		e.DurationMs = &d
		e.Blocked = l.blocked
		switch {
		case l.failure != nil:
			e.Error = l.failure
		case !finished:
			aborted := "aborted"
			e.Error = &aborted
		default:
			e.Error = nil
		}
		if !l.hasDest {
			e.Type = resourceTypeOf("", false, mime)
		}
	})
}

// parseJSON is JSON.parse into JavaScript-ordered values.
func parseJSON(s string) (any, bool) {
	v, err := jsvalue.Parse([]byte(s))
	return v, err == nil
}
