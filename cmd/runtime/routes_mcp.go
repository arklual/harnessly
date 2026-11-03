package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// MCP servers as connections (internal/mcpconn): the person's remote MCP servers, connected in Settings → Connections,
// and the proxy agents reach them through. Who may call what:
//
//	GET|POST <platform>/mcp, POST <platform>/mcp/probe, POST <platform>/mcp/<id>/signin|check,
//	PUT <platform>/mcp/<id>/header, PATCH|DELETE <platform>/mcp/<id>
//	                                     the person's page: a page session, same origin
//	GET <platform>/mcp/callback          the service's sign-in sends the person's browser back here (a top-level
//	                                     navigation from another site): a page session, no same-origin check; the
//	                                     state of a sign-in this runtime started is what it proves
//	GET /api/cli/mcp                     an agent of this machine: the cli-token or a launch's agent token; the list
//	ANY /api/cli/mcp-proxy/<id>[/message] the same tokens: the server's MCP endpoint with the person's credentials added
//
// Credentials come in only by the page (a typed header) or by the service's sign-in, and are kept in the safe as
// mcp-<id>; no answer and no log line carries one, and the agent's own token never goes upstream.

const (
	// mcpCallMax bounds a page route's calls to a server (a probe, a check, a sign-in).
	mcpCallMax = 45 * time.Second
	// mcpProxyBodyMax bounds a request the proxy buffers: JSON-RPC messages are small, and a buffered body can be sent
	// again after a refreshed sign-in.
	mcpProxyBodyMax = 4 << 20
	// mcpSignInTTL is how long a started sign-in waits for its callback (the service keeps its flow as long).
	mcpSignInTTL = 10 * time.Minute
)

var (
	mcpsMu  sync.Mutex
	mcpsFor = map[*gate.CLIToken]*mcpconn.Service{}
)

// mcpFor is the one MCP service of a runtime; its credentials are in the safe the connections use.
func mcpFor(d deps) *mcpconn.Service {
	mcpsMu.Lock()
	defer mcpsMu.Unlock()
	if s := mcpsFor[d.CLI]; s != nil {
		return s
	}
	sec := &connSecrets{store: safeFor(d).store}
	s := mcpconn.New(d.DataDir, sec, nil)
	sec.titleOf = func(name string) string {
		id := strings.TrimPrefix(name, "mcp-")
		if srv, err := s.Get(id); err == nil && srv.Name != "" {
			return "MCP server " + srv.Name
		}
		return "MCP server " + id
	}
	mcpsFor[d.CLI] = s
	return s
}

func mcpRoutes(d deps) []gate.Route { return mcpRoutesOf(d, mcpFor(d), nil) }

// mcpStatus is the status of a reason: never 401 or 403, which the page reads as its own session.
func mcpStatus(reason string) int {
	switch reason {
	case "denied", "oauth-failed":
		return http.StatusFailedDependency
	case "not-found":
		return http.StatusNotFound
	case "bad-request":
		return http.StatusBadRequest
	case "signin-required":
		return http.StatusConflict
	}
	return http.StatusBadGateway
}

func mcpRefusal(w http.ResponseWriter, err error, extra map[string]any) {
	reason := mcpconn.Reason(err)
	body := map[string]any{"error": reason}
	for k, v := range extra {
		body[k] = v
	}
	connJSON(w, mcpStatus(reason), body)
}

// mcpPending is a sign-in started from the page: where its answer goes back to.
type mcpPending struct {
	origin   string
	returnTo string
	at       time.Time
}

// mcpPages keeps the sign-ins started from the page by their state.
type mcpPages struct {
	mu sync.Mutex
	m  map[string]mcpPending
}

func (p *mcpPages) put(state string, v mcpPending) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = map[string]mcpPending{}
	}
	for k, old := range p.m {
		if time.Since(old.at) > mcpSignInTTL {
			delete(p.m, k)
		}
	}
	p.m[state] = v
}

func (p *mcpPages) take(state string) (mcpPending, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.m[state]
	delete(p.m, state)
	if ok && time.Since(v.at) > mcpSignInTTL {
		return mcpPending{}, false
	}
	return v, ok
}

// pageOrigin checks the origin the page sends (location.origin) against the request: the Origin header when the
// browser sent one, else the host it asked (directly or through a proxy). "" when it does not hold.
func pageOrigin(r *http.Request, sent string) string {
	u, err := url.Parse(strings.TrimSpace(sent))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	origin := u.Scheme + "://" + strings.ToLower(u.Host)
	if o := r.Header.Get("Origin"); o != "" {
		if !strings.EqualFold(strings.TrimRight(o, "/"), origin) {
			return ""
		}
		return origin
	}
	fwd, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	if strings.EqualFold(u.Host, r.Host) || fwd != "" && strings.EqualFold(u.Host, strings.TrimSpace(fwd)) {
		return origin
	}
	return ""
}

// browserAPIPrefix is the prefix the browser puts before /api: the app base path (RUNTIME_APP_BASE_PATH, /m/<machine>
// in a hub) when the request came under it (apiAlias serves it as /agents/api/…), "" when it came to /api directly.
func browserAPIPrefix(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/agents/api/") {
		if base := appBase(); base != "" {
			return base
		}
		return "/agents"
	}
	return ""
}

// mcpReturnTo is the path the callback page links back to: a path of this origin, the app's root by default.
func mcpReturnTo(r *http.Request, sent string) string {
	sent = strings.TrimSpace(sent)
	if strings.HasPrefix(sent, "/") && !strings.HasPrefix(sent, "//") && !strings.ContainsAny(sent, "\\\r\n") {
		return sent
	}
	if p := browserAPIPrefix(r); p != "" {
		return p + "/"
	}
	return "/"
}

func mcpRoutesOf(d deps, ms *mcpconn.Service, transport http.RoundTripper) []gate.Route {
	base, cliBase, proxyBase := platformAPI+"/mcp", "/api/cli/mcp", strings.TrimSuffix(mcpProxyPath, "/")
	under := func(r *http.Request, p string) (string, bool) {
		for _, prefix := range []string{p + "/", "/agents" + p + "/"} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && rest != "" {
				return rest, true
			}
		}
		return "", false
	}
	pages := &mcpPages{}
	notAllowed := func(w http.ResponseWriter) {
		connJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
	badBody := func(w http.ResponseWriter) {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
	}
	ctxOf := func(r *http.Request) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), mcpCallMax)
	}
	list := func(w http.ResponseWriter, withCatalog bool) {
		servers, err := ms.List()
		if err != nil {
			agent.Log("warn", "mcp: the list is unreadable")
			mcpRefusal(w, err, nil)
			return
		}
		if servers == nil {
			servers = []mcpconn.Server{}
		}
		out := map[string]any{"servers": servers}
		if withCatalog {
			out["catalog"] = ms.Catalog()
		}
		connJSON(w, http.StatusOK, out)
	}

	page := func(w http.ResponseWriter, r *http.Request) {
		rest, sub := "", pathIs(r, base)
		if !sub {
			rest, _ = under(r, base)
		}
		if rest == "callback" {
			mcpCallback(w, r, d, ms, pages)
			return
		}
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			connJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		switch {
		case sub:
			switch r.Method {
			case http.MethodGet:
				list(w, true)
			case http.MethodPost:
				var in mcpconn.AddInput
				if !readSecretBody(r, &in) || strings.TrimSpace(in.URL) == "" {
					badBody(w)
					return
				}
				ctx, cancel := ctxOf(r)
				defer cancel()
				srv, start, err := ms.Add(ctx, in)
				if err != nil {
					agent.Log("info", "mcp: add refused", agent.F{K: "reason", V: mcpconn.Reason(err)})
					mcpRefusal(w, err, nil)
					return
				}
				agent.Log("info", "mcp: added", agent.F{K: "id", V: srv.ID}, agent.F{K: "auth", V: string(srv.Auth)},
					agent.F{K: "state", V: string(srv.State)})
				// authStart is how the person signs in next; null when the server was connected at once.
				connJSON(w, http.StatusOK, map[string]any{"server": srv, "authStart": start})
			default:
				notAllowed(w)
			}
			return
		case rest == "probe":
			if r.Method != http.MethodPost {
				notAllowed(w)
				return
			}
			var b struct {
				URL string `json:"url"`
			}
			if !readSecretBody(r, &b) || strings.TrimSpace(b.URL) == "" {
				badBody(w)
				return
			}
			ctx, cancel := ctxOf(r)
			defer cancel()
			p, err := ms.Probe(ctx, b.URL)
			if err != nil {
				agent.Log("info", "mcp: probe failed", agent.F{K: "reason", V: mcpconn.Reason(err)})
				mcpRefusal(w, err, nil)
				return
			}
			agent.Log("info", "mcp: probed", agent.F{K: "transport", V: string(p.Transport)}, agent.F{K: "auth", V: string(p.Auth)})
			connJSON(w, http.StatusOK, map[string]any{"probe": p})
			return
		}
		id, what, _ := strings.Cut(rest, "/")
		if id == "" {
			connJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		switch what {
		case "":
			switch r.Method {
			case http.MethodPatch:
				var b struct {
					Name    *string `json:"name"`
					Enabled *bool   `json:"enabled"`
				}
				if !readSecretBody(r, &b) {
					badBody(w)
					return
				}
				srv, err := ms.Get(id)
				if err == nil && b.Name != nil {
					srv, err = ms.Rename(id, *b.Name)
				}
				if err == nil && b.Enabled != nil {
					srv, err = ms.SetEnabled(id, *b.Enabled)
				}
				if err != nil {
					mcpRefusal(w, err, nil)
					return
				}
				agent.Log("info", "mcp: changed", agent.F{K: "id", V: id}, agent.F{K: "enabled", V: srv.Enabled})
				connJSON(w, http.StatusOK, map[string]any{"server": srv})
			case http.MethodDelete:
				if err := ms.Remove(id); err != nil {
					mcpRefusal(w, err, nil)
					return
				}
				agent.Log("info", "mcp: removed", agent.F{K: "id", V: id})
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusNoContent)
			default:
				notAllowed(w)
			}
		case "signin":
			if r.Method != http.MethodPost {
				notAllowed(w)
				return
			}
			var b struct {
				Origin   string `json:"origin"`
				ReturnTo string `json:"returnTo"`
			}
			if !readSecretBody(r, &b) {
				badBody(w)
				return
			}
			origin := pageOrigin(r, b.Origin)
			if origin == "" {
				agent.Log("info", "mcp: sign-in refused, the page's origin does not hold", agent.F{K: "id", V: id})
				badBody(w)
				return
			}
			redirect := origin + browserAPIPrefix(r) + platformAPI + "/mcp/callback"
			ctx, cancel := ctxOf(r)
			defer cancel()
			authURL, err := ms.StartSignIn(ctx, id, redirect)
			if err != nil {
				agent.Log("info", "mcp: sign-in not started", agent.F{K: "id", V: id}, agent.F{K: "reason", V: mcpconn.Reason(err)})
				mcpRefusal(w, err, nil)
				return
			}
			u, err := url.Parse(authURL)
			state := ""
			if err == nil {
				state = u.Query().Get("state")
			}
			if state == "" {
				mcpRefusal(w, mcpconn.ErrOAuthFailed, nil)
				return
			}
			pages.put(state, mcpPending{origin: origin, returnTo: mcpReturnTo(r, b.ReturnTo), at: time.Now()})
			agent.Log("info", "mcp: sign-in started", agent.F{K: "id", V: id})
			connJSON(w, http.StatusOK, map[string]any{"authUrl": authURL})
		case "header":
			if r.Method != http.MethodPut {
				notAllowed(w)
				return
			}
			var b mcpconn.HeaderInput
			if !readSecretBody(r, &b) || strings.TrimSpace(b.Value) == "" {
				badBody(w)
				return
			}
			ctx, cancel := ctxOf(r)
			defer cancel()
			srv, err := ms.SetHeader(ctx, id, b.Name, b.Value)
			if err != nil {
				agent.Log("info", "mcp: header refused", agent.F{K: "id", V: id}, agent.F{K: "reason", V: mcpconn.Reason(err)})
				mcpRefusal(w, err, nil)
				return
			}
			agent.Log("info", "mcp: header set", agent.F{K: "id", V: id}, agent.F{K: "state", V: string(srv.State)})
			connJSON(w, http.StatusOK, map[string]any{"server": srv})
		case "check":
			if r.Method != http.MethodPost {
				notAllowed(w)
				return
			}
			ctx, cancel := ctxOf(r)
			defer cancel()
			srv, err := ms.Check(ctx, id)
			if err != nil {
				mcpRefusal(w, err, nil)
				return
			}
			agent.Log("info", "mcp: checked", agent.F{K: "id", V: id}, agent.F{K: "state", V: string(srv.State)})
			connJSON(w, http.StatusOK, map[string]any{"server": srv})
		default:
			connJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		}
	}

	cli := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			connJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		if r.Method != http.MethodGet {
			notAllowed(w)
			return
		}
		list(w, false)
	}

	px := &mcpProxy{d: d, ms: ms, client: mcpProxyClient(transport)}
	return []gate.Route{
		{Match: func(r *http.Request) bool { _, ok := under(r, base); return pathIs(r, base) || ok }, Handler: http.HandlerFunc(page)},
		{Match: func(r *http.Request) bool { return pathIs(r, cliBase) }, Handler: http.HandlerFunc(cli)},
		{Match: func(r *http.Request) bool { _, ok := under(r, proxyBase); return ok }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rest, _ := under(r, proxyBase)
			px.serve(w, r, rest)
		})},
	}
}

// mcpCallback finishes a sign-in: the service's authorization server sends the person's browser here with the code
// and the state. The answer is a tiny page that tells the page which opened the sign-in (window.opener, the origin it
// was started from) how it went, and closes; opened without a page behind it, it links back to the settings.
func mcpCallback(w http.ResponseWriter, r *http.Request, d deps, ms *mcpconn.Service, pages *mcpPages) {
	if r.Method != http.MethodGet {
		connJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	if !d.SessionOK(r) {
		// The state stays unused: the person signs in to the runtime and starts again.
		mcpCallbackPage(w, http.StatusUnauthorized, mcpPending{}, "", false, "unauthorized")
		return
	}
	pending, known := pages.take(state)
	ctx, cancel := context.WithTimeout(r.Context(), mcpCallMax)
	defer cancel()
	srv, err := ms.FinishSignIn(ctx, state, q.Get("code"), q.Get("error"))
	if !known {
		// A sign-in this page did not start (or one older than its wait): nothing to tell an opener.
		pending = mcpPending{}
	}
	if err != nil {
		reason := mcpconn.Reason(err)
		agent.Log("info", "mcp: sign-in failed", agent.F{K: "id", V: srv.ID}, agent.F{K: "reason", V: reason})
		mcpCallbackPage(w, http.StatusOK, pending, srv.ID, false, reason)
		return
	}
	agent.Log("info", "mcp: signed in", agent.F{K: "id", V: srv.ID}, agent.F{K: "state", V: string(srv.State)})
	mcpCallbackPage(w, http.StatusOK, pending, srv.ID, true, "")
}

// mcpCallbackPage writes the callback's page: no outside resources, its one script and style allowed by a nonce.
func mcpCallbackPage(w http.ResponseWriter, status int, p mcpPending, id string, ok bool, reason string) {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	nonce := hex.EncodeToString(raw[:])
	title := locale.Pick("Signed in", "Вход выполнен")
	text := locale.Pick("You can close this window and go back to harnessly.", "Окно можно закрыть и вернуться в harnessly.")
	if !ok {
		title = locale.Pick("The sign-in did not finish", "Вход не завершён")
		text = locale.Pick("Go back to Settings → Connections and try again.", "Вернитесь в Настройки → Подключения и попробуйте ещё раз.")
	}
	if reason == "unauthorized" {
		text = locale.Pick("Sign in to harnessly in this browser first, then connect the server again.",
			"Сначала войдите в harnessly в этом браузере, затем подключите сервер ещё раз.")
	}
	script := ""
	if p.origin != "" {
		msg, _ := json.Marshal(map[string]any{"type": "harnessly-mcp", "id": id, "ok": ok, "reason": reason})
		target, _ := json.Marshal(p.origin)
		// The message goes only to the origin the sign-in was started from.
		script = `<script nonce="` + nonce + `">(function(){try{if(window.opener&&!window.opener.closed){window.opener.postMessage(` +
			string(msg) + `,` + string(target) + `);window.close();}}catch(e){}})();</script>`
	}
	link := ""
	if p.origin != "" {
		link = `<p><a href="` + html.EscapeString(p.origin+p.returnTo) + `">` + html.EscapeString(locale.Pick("Back to harnessly", "Назад в harnessly")) + `</a></p>`
	}
	body := `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<meta name="referrer" content="no-referrer"><title>` + html.EscapeString(title) + `</title>` +
		`<style nonce="` + nonce + `">body{font:15px/1.5 system-ui,sans-serif;margin:0;padding:48px 24px;color:#1f2328;background:#fff}` +
		`main{max-width:420px;margin:0 auto}h1{font-size:20px;margin:0 0 8px}a{color:inherit}` +
		`@media (prefers-color-scheme:dark){body{color:#e6e6e6;background:#141414}}</style></head><body><main><h1>` +
		html.EscapeString(title) + `</h1><p>` + html.EscapeString(text) + `</p>` + link + `</main>` + script + `</body></html>`
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// mcpProxy forwards an agent's MCP requests to a server with the person's credentials.
type mcpProxy struct {
	d      deps
	ms     *mcpconn.Service
	client *http.Client
}

// mcpProxyClient reaches the servers: no overall timeout (a stream lasts as long as the agent keeps it), no redirects
// followed (credentials stay with the server they are for), and no link-local (cloud metadata) addresses.
func mcpProxyClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		_, metadata, _ := net.ParseCIDR("169.254.0.0/16")
		dialer := &net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); ip != nil && metadata.Contains(ip) {
					return errors.New("refused address " + host)
				}
				return nil
			},
		}
		transport = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     90 * time.Second,
		}
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// What passes between the agent and the server besides the body: the transport's own headers. Nothing else of the
// agent's request goes upstream (its token, cookies), and nothing else of the server's answer comes back (its
// sign-in challenge, which the agent would try to answer itself).
var (
	mcpProxyRequestHeaders  = []string{"Accept", "Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-Id"}
	mcpProxyResponseHeaders = []string{"Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Retry-After"}
)

func (p *mcpProxy) refuse(w http.ResponseWriter, status int, reason string) {
	connJSON(w, status, map[string]string{"error": reason})
}

// serve answers ANY /api/cli/mcp-proxy/<id>[/message?to=…]. rest is what follows the proxy's path.
func (p *mcpProxy) serve(w http.ResponseWriter, r *http.Request, rest string) {
	if p.d.CLI == nil || !p.d.CLI.OK(r) {
		p.refuse(w, http.StatusForbidden, "Forbidden")
		return
	}
	id, sub, _ := strings.Cut(rest, "/")
	srv, err := p.ms.Get(id)
	if err != nil {
		p.refuse(w, http.StatusNotFound, "not-found")
		return
	}
	if !srv.Enabled {
		p.refuse(w, http.StatusNotFound, "off")
		return
	}
	target, err := url.Parse(srv.URL)
	if err != nil {
		p.refuse(w, http.StatusBadGateway, "unreachable")
		return
	}
	legacy := srv.Transport == mcpconn.SSE
	switch {
	case sub == "":
	case legacy && sub == "message":
		// The legacy transport's POST endpoint, as the stream announced it: only on the server's own origin.
		to, err := target.Parse(r.URL.Query().Get("to"))
		if err != nil || to.Scheme != target.Scheme || !strings.EqualFold(to.Host, target.Host) {
			p.refuse(w, http.StatusBadRequest, "bad-request")
			return
		}
		target = to
	default:
		p.refuse(w, http.StatusNotFound, "not-found")
		return
	}
	var body []byte
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body, err = io.ReadAll(io.LimitReader(r.Body, mcpProxyBodyMax+1))
		if err != nil {
			p.refuse(w, http.StatusBadRequest, "bad-request")
			return
		}
		if len(body) > mcpProxyBodyMax {
			p.refuse(w, http.StatusRequestEntityTooLarge, "too-large")
			return
		}
	}
	auth, err := p.ms.Authorize(r.Context(), id)
	if err != nil {
		p.authRefusal(w, id, err)
		return
	}
	res, err := p.do(r, target, body, auth)
	if err == nil && res.StatusCode == http.StatusUnauthorized && srv.Auth != mcpconn.AuthNone {
		// A token the server no longer takes: sign in afresh once and send the request again.
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		_ = res.Body.Close()
		if auth, err = p.ms.Reauthorize(r.Context(), id); err != nil {
			p.authRefusal(w, id, err)
			return
		}
		res, err = p.do(r, target, body, auth)
	}
	if err != nil {
		if r.Context().Err() == nil {
			agent.Log("info", "mcp proxy: the server did not answer", agent.F{K: "id", V: id})
			p.refuse(w, http.StatusBadGateway, "unreachable")
		}
		return
	}
	defer res.Body.Close()
	agent.Log("debug", "mcp proxy", agent.F{K: "id", V: id}, agent.F{K: "status", V: res.StatusCode})
	if res.StatusCode == http.StatusUnauthorized {
		// Still refused after a fresh sign-in: the person signs in again on the page. Not a 401, which the agent's
		// CLI would take for its own sign-in to start.
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		p.refuse(w, http.StatusConflict, "signin-required")
		return
	}
	h := w.Header()
	for _, k := range mcpProxyResponseHeaders {
		if vs := res.Header.Values(k); len(vs) > 0 {
			h[k] = vs
		}
	}
	h.Set("Cache-Control", "no-store")
	stream := strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "text/event-stream")
	if stream {
		h.Set("X-Accel-Buffering", "no")
		h.Del("Content-Length")
	} else if res.ContentLength >= 0 {
		h.Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	}
	w.WriteHeader(res.StatusCode)
	rc := http.NewResponseController(w)
	if !stream {
		_, _ = io.Copy(w, res.Body)
		return
	}
	_ = rc.Flush()
	if legacy && sub == "" {
		proxyPath := strings.TrimSuffix(r.URL.Path, "/") + "/message"
		copySSERewritingEndpoint(w, rc, res.Body, func(endpoint string) string {
			return proxyPath + "?to=" + url.QueryEscape(endpoint)
		})
		return
	}
	copyFlushing(w, rc, res.Body)
}

// do sends one request upstream with the transport's headers of the agent's request and the credentials.
func (p *mcpProxy) do(r *http.Request, target *url.URL, body []byte, auth http.Header) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), rd)
	if err != nil {
		return nil, err
	}
	for _, k := range mcpProxyRequestHeaders {
		if vs := r.Header.Values(k); len(vs) > 0 {
			req.Header[k] = vs
		}
	}
	req.Header.Set("User-Agent", "harnessly-mcp/1")
	for k, vs := range auth {
		req.Header[k] = vs
	}
	return p.client.Do(req)
}

// authRefusal answers a request the person's credentials cannot sign: 409 when they have to sign in again.
func (p *mcpProxy) authRefusal(w http.ResponseWriter, id string, err error) {
	reason := mcpconn.Reason(err)
	agent.Log("info", "mcp proxy: no credentials", agent.F{K: "id", V: id}, agent.F{K: "reason", V: reason})
	p.refuse(w, mcpStatus(reason), reason)
}

// copyFlushing copies a stream, flushing every read.
func copyFlushing(w io.Writer, rc *http.ResponseController, src io.Reader) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// copySSERewritingEndpoint copies an event stream line by line, flushing at every event's end, with the data of each
// `endpoint` event (the legacy transport's address to POST to) replaced by rewrite of it.
func copySSERewritingEndpoint(w io.Writer, rc *http.ResponseController, src io.Reader, rewrite func(string) string) {
	br := bufio.NewReaderSize(src, 64<<10)
	event := ""
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				event = ""
			case strings.HasPrefix(trimmed, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case event == "endpoint" && strings.HasPrefix(trimmed, "data:"):
				line = "data: " + rewrite(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))) + "\n"
			}
			if _, werr := io.WriteString(w, line); werr != nil {
				return
			}
			if trimmed == "" || err != nil {
				if rc.Flush() != nil {
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}
