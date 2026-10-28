// Package proxy carries a person's browser to the runtime of a machine, through the machine's agent link
// (docs/hub/architecture.md, "Reaching a runtime through the hub").
//
// The page a person gets is the runtime's own. The hub checks who they are (the hub session in path mode, the machine
// origin's own cookie in subdomain mode) and that they belong to the machine's space, asks the agent for a runtime
// session for them, and forwards each request through a tunnel with the browser's cookies replaced by that session.
// The browser never holds a runtime token, and nothing of the hub's own session reaches the machine.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/ticket"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

const (
	grantTimeout = 15 * time.Second
	dialTimeout  = 10 * time.Second
	// grantMargin is how long before a runtime session ends the hub asks for a new one.
	grantMargin = 10 * time.Minute
	// authzTTL is how long "this person may open this machine" is believed, so that the dozens of requests of a page do
	// not each ask the database.
	authzTTL   = 5 * time.Second
	maxGrants  = 5000
	maxAuthz   = 10000
	idleTunnel = 30 * time.Second
)

var machineID = regexp.MustCompile(`^[a-z2-7]{20}$`)

// Deps are what the service is built from.
type Deps struct {
	Config  *config.Config
	DB      *store.DB
	Auth    *auth.Service
	Spaces  *spaces.Service
	Reg     *link.Registry
	Tickets *ticket.Store
	Log     *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Service is the runtime proxy.
type Service struct {
	cfg     *config.Config
	db      *store.DB
	auth    *auth.Service
	spaces  *spaces.Service
	reg     *link.Registry
	tickets *ticket.Store
	log     *slog.Logger
	now     func() time.Time

	mu         sync.Mutex
	transports map[string]*http.Transport
	grants     map[string]grantEntry
	calls      map[string]*grantCall
	authz      map[string]time.Time
	trust      map[string]time.Time
	tracked    map[string]map[uint64]context.CancelFunc
	nextTrack  uint64
}

type grantEntry struct {
	grant hubproto.SessionGrant
	until time.Time
}

type grantCall struct {
	done  chan struct{}
	grant hubproto.SessionGrant
	err   error
}

// New makes the proxy.
func New(d Deps) *Service {
	s := &Service{
		cfg: d.Config, db: d.DB, auth: d.Auth, spaces: d.Spaces, reg: d.Reg, tickets: d.Tickets, log: d.Log, now: d.Now,
		transports: map[string]*http.Transport{}, grants: map[string]grantEntry{}, calls: map[string]*grantCall{},
		authz: map[string]time.Time{}, trust: map[string]time.Time{}, tracked: map[string]map[uint64]context.CancelFunc{},
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s
}

// Mount registers the hub-origin side: /m/<id>/… (path mode runs it, subdomain mode sends people to the machine's own
// origin).
func (s *Service) Mount(mux *http.ServeMux) { mux.Handle("/m/", s) }

var (
	errOffline = errors.New("machine offline")
)

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if id := s.cfg.MachineIDFromHost(r.Host); id != "" {
		s.serveRuntimeHost(w, r, id)
		return
	}
	id, ok := pathMachine(r.URL.Path)
	if !ok {
		s.page(w, r, http.StatusNotFound, "Не найдено", "Такой машины нет.")
		return
	}
	if s.cfg.SubdomainMode() {
		// The machine's page must not run on the hub's origin: send the person to the machine's own.
		http.Redirect(w, r, "/api/machines/"+id+"/open?redirect=1&path="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	s.servePath(w, r, id)
}

// pathMachine reads the machine id out of /m/<id> or /m/<id>/….
func pathMachine(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/m/")
	if !ok {
		return "", false
	}
	id, _, _ := strings.Cut(rest, "/")
	return id, machineID.MatchString(id)
}

// ---- who may come in ----

// admit finds the machine and checks the person may open it, and, in path mode, that the machine may run on the hub's
// origin at all. Whether a machine is one of those is told only to a person who belongs to its space.
func (s *Service) admit(ctx context.Context, userID, id string) (store.Machine, error) {
	m, err := s.db.S().MachineByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Machine{}, httpx.ErrNotFound
	}
	if err != nil {
		return store.Machine{}, err
	}
	if m.RevokedAt != nil || m.SpaceID == "" {
		return store.Machine{}, httpx.ErrNotFound
	}
	key := userID + "/" + m.SpaceID
	s.mu.Lock()
	until, ok := s.authz[key]
	s.mu.Unlock()
	if !ok || !s.now().Before(until) {
		if _, err := s.spaces.Access(ctx, userID, m.SpaceID); err != nil {
			return store.Machine{}, err
		}
		s.mu.Lock()
		if len(s.authz) >= maxAuthz {
			s.authz = map[string]time.Time{}
		}
		s.authz[key] = s.now().Add(authzTTL)
		s.mu.Unlock()
	}
	if err := s.trusted(ctx, m); err != nil {
		return store.Machine{}, err
	}
	return m, nil
}

// trusted is the rule of path mode (auth.Service.RequireTrustedMachine): only a machine an administrator connected runs on
// the hub's origin. What it found is believed for authzTTL, per machine and creator, like the access check.
func (s *Service) trusted(ctx context.Context, m store.Machine) error {
	if s.cfg.SubdomainMode() {
		return nil
	}
	key := m.ID + "/" + m.CreatedBy
	s.mu.Lock()
	until, ok := s.trust[key]
	s.mu.Unlock()
	if ok && s.now().Before(until) {
		return nil
	}
	if err := s.auth.RequireTrustedMachine(ctx, m.CreatedBy); err != nil {
		return err
	}
	s.mu.Lock()
	if len(s.trust) >= maxAuthz {
		s.trust = map[string]time.Time{}
	}
	s.trust[key] = s.now().Add(authzTTL)
	s.mu.Unlock()
	return nil
}

func isNavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Upgrade") == ""
}

// refuseCrossSite stops a request that a page of another origin made on a person's behalf. The browser attaches a
// person's cookie to a request of any page of the same site, and the machines' origins are siblings of each other (and
// of the hub, unless HUB_RUNTIME_DOMAIN is a domain of its own), so a page of one machine could otherwise write to
// another's, or open its sockets, as the person who has both open. Only a navigation of a whole page, a person
// following a link or the hub's own button, may come from elsewhere: that is not something a page can read or write
// through. A navigation of a frame (iframe, frame, embed, object) is the same mode but not the same thing: a page of
// another origin could show the machine under its own buttons and have the person click through (clickjacking), so
// only Sec-Fetch-Dest: document passes. Without Fetch Metadata the Origin header says the same for the requests that
// carry one, and frameHeaders closes the rest.
func refuseCrossSite(w http.ResponseWriter, r *http.Request) bool {
	if !sameorigin.CrossSite(r) {
		return false
	}
	if isNavigation(r) && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document" {
		return false
	}
	httpx.WriteError(w, r, httpx.ErrCrossSite)
	return true
}

// servePath is path mode: the hub session decides.
func (s *Service) servePath(w http.ResponseWriter, r *http.Request, id string) {
	if refuseCrossSite(w, r) {
		return
	}
	p, err := s.auth.Authenticate(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p == nil {
		if isNavigation(r) {
			http.Redirect(w, r, "/signin?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		httpx.WriteError(w, r, httpx.ErrUnauthenticated)
		return
	}
	m, err := s.admit(r.Context(), p.User.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/m/"+id+"/api/auth/logout" {
		s.logout(w, r, "")
		return
	}
	s.forward(w, r, p.User, m)
}

// serveRuntimeHost is subdomain mode: the machine's own origin, the only thing that runs there.
func (s *Service) serveRuntimeHost(w http.ResponseWriter, r *http.Request, id string) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/m/"+id+"/", http.StatusFound)
		return
	}
	if pid, ok := pathMachine(r.URL.Path); !ok || pid != id {
		s.page(w, r, http.StatusNotFound, "Не найдено", "Здесь открывается только машина "+id+".")
		return
	}
	if refuseCrossSite(w, r) {
		return
	}
	ctx := r.Context()
	var p *auth.Principal
	if c, err := r.Cookie(s.auth.RunCookieName()); err == nil {
		var err error
		if p, err = s.auth.RunSession(ctx, c.Value); err != nil {
			s.fail(w, r, err)
			return
		}
		if p != nil && p.Session.MachineID != id {
			p = nil
		}
	}
	if p == nil {
		if tk := r.URL.Query().Get("hl_ticket"); tk != "" && isNavigation(r) {
			s.redeem(w, r, id, tk)
			return
		}
		if isNavigation(r) {
			http.Redirect(w, r, s.cfg.PublicURL+"/api/machines/"+id+"/open?redirect=1&path="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		httpx.WriteError(w, r, httpx.ErrUnauthenticated)
		return
	}
	m, err := s.admit(ctx, p.User.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/m/"+id+"/api/auth/logout" {
		s.logout(w, r, p.Session.TokenHash)
		return
	}
	s.forward(w, r, p.User, m)
}

// redeem trades a ticket for the machine origin's cookie and sends the person on to the same address without it.
func (s *Service) redeem(w http.ResponseWriter, r *http.Request, id, tk string) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	userID, ok := s.tickets.Redeem(tk, id)
	if ok {
		if _, err := s.admit(r.Context(), userID, id); err != nil {
			ok = false
		}
	}
	if !ok {
		// A ticket is single use: a reload of the ticket URL, or an old one, goes round again for a new one.
		http.Redirect(w, r, s.cfg.PublicURL+"/api/machines/"+id+"/open?redirect=1&path="+url.QueryEscape(stripTicket(r.URL)), http.StatusFound)
		return
	}
	token, expires, err := s.auth.CreateRunSession(r.Context(), userID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Strict keeps the machine's session off requests that other sites start (a link on a page elsewhere, a form, an
	// image) and, with the __Host- prefix on https, a sibling machine's script cannot plant a cookie for this host. It
	// does not keep a sibling machine's own requests away: machines under one registrable domain are the same site, so
	// that separation is the job of refuseCrossSite (Fetch Metadata and Origin) and of HUB_RUNTIME_DOMAIN being a
	// domain of its own.
	http.SetCookie(w, &http.Cookie{
		Name: s.auth.RunCookieName(), Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.Secure,
		SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(auth.RunSessionTTL.Seconds()),
	})
	s.landing(w, stripTicket(r.URL))
}

// landing sends the browser on to target with a page that refreshes into it, not with a redirect. The ticket arrived on a
// navigation that began on another origin (the hub's page), and a browser holds that origin against every request of
// the redirect chain that follows, so a Strict cookie would not go with a 302 to the clean address, and the machine
// would send the person round for another ticket for good. The refresh is a navigation of the machine's own page: its
// request is same-site, and the cookie goes with it.
func (s *Service) landing(w http.ResponseWriter, target string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	t := html.EscapeString(target)
	fmt.Fprintf(w, landingTemplate, t, t)
}

const landingTemplate = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><meta http-equiv="refresh" content="0;url=%s">
<title>harnessly</title></head>
<body><a href="%s">Продолжить</a></body></html>
`

func stripTicket(u *url.URL) string {
	c := *u
	q := c.Query()
	q.Del("hl_ticket")
	c.RawQuery = q.Encode()
	return c.RequestURI()
}

// logout: the runtime page's own «Выйти» is the hub's, so it ends nothing on the machine. In subdomain mode it ends the
// machine origin's session too.
func (s *Service) logout(w http.ResponseWriter, r *http.Request, runToken string) {
	if sameorigin.CrossSite(r) {
		httpx.WriteError(w, r, httpx.ErrCrossSite)
		return
	}
	if runToken != "" {
		_ = s.db.S().DeleteSession(r.Context(), runToken)
		http.SetCookie(w, &http.Cookie{Name: s.auth.RunCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail answers an error the way the caller can read it: a page for a person navigating, JSON for a script.
func (s *Service) fail(w http.ResponseWriter, r *http.Request, err error) {
	var he *httpx.Error
	if !errors.As(err, &he) {
		slog.Error("proxy request failed", "path", httpx.SafePath(r.URL.Path), "error", err)
		he = httpx.ErrInternal
	}
	if isNavigation(r) && strings.Contains(r.Header.Get("Accept"), "text/html") {
		title, text := "Нет доступа", "У вас нет доступа к этой машине. Попросите владельца пространства добавить вас."
		if he.Code == httpx.ErrMachineUntrusted.Code {
			title, text = "Эту машину здесь не открыть", he.Message
		} else if he.Status == http.StatusNotFound {
			title, text = "Не найдено", "Такой машины нет или её отключили."
		} else if he.Status >= 500 {
			title, text = "Что-то пошло не так", "Попробуйте ещё раз через минуту."
		}
		s.page(w, r, he.Status, title, text)
		return
	}
	httpx.WriteError(w, r, he)
}

// ---- forwarding ----

// grant is the runtime session for a person on a machine: the cookie the runtime takes for a signed-in page. It is
// cached per person and machine, and asked for again shortly before it ends.
func (s *Service) grant(ctx context.Context, u store.User, m store.Machine) (hubproto.SessionGrant, error) {
	key := u.ID + "/" + m.ID
	s.mu.Lock()
	if g, ok := s.grants[key]; ok && s.now().Before(g.until) {
		s.mu.Unlock()
		return g.grant, nil
	}
	if c, ok := s.calls[key]; ok {
		s.mu.Unlock()
		select {
		case <-c.done:
			return c.grant, c.err
		case <-ctx.Done():
			return hubproto.SessionGrant{}, ctx.Err()
		}
	}
	c := &grantCall{done: make(chan struct{})}
	s.calls[key] = c
	s.mu.Unlock()

	// Asked for on the hub's behalf, not the first caller's: if that caller leaves, the others still need the answer.
	gctx, cancel := context.WithTimeout(context.Background(), grantTimeout)
	defer cancel()
	var g hubproto.SessionGrant
	err := s.reg.DoInto(gctx, m.ID, hubproto.OpRuntimeSess, map[string]any{"subject": u.Email, "name": u.Name}, &g, grantTimeout)
	if err == nil && (g.Token == "" || g.CookieName == "" || strings.ContainsAny(g.Token+g.CookieName, "; \r\n,")) {
		err = httpx.New(http.StatusBadGateway, "machine_error", "Машина выдала непонятный сеанс")
	}
	s.mu.Lock()
	delete(s.calls, key)
	if err == nil {
		until := s.now().Add(time.Hour)
		if g.ExpiresAt > 0 {
			until = time.UnixMilli(g.ExpiresAt).Add(-grantMargin)
		}
		if len(s.grants) >= maxGrants {
			s.grants = map[string]grantEntry{}
		}
		s.grants[key] = grantEntry{grant: g, until: until}
	}
	s.mu.Unlock()
	c.grant, c.err = g, err
	close(c.done)
	return g, err
}

func (s *Service) dropGrant(userID, machineID string) {
	s.mu.Lock()
	delete(s.grants, userID+"/"+machineID)
	s.mu.Unlock()
}

// transport is the machine's HTTP client over tunnels. Connections are shared between the people opening the machine:
// a request carries its own credentials, and nothing is kept on the connection between requests.
func (s *Service) transport(id string) *http.Transport {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.transports[id]; t != nil {
		return t
	}
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			l := s.reg.Get(id)
			if l == nil {
				return nil, errOffline
			}
			dctx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()
			tn, err := l.OpenTunnel(dctx)
			if err != nil {
				return nil, err
			}
			return tn, nil
		},
		DisableCompression:  true,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     idleTunnel,
	}
	s.transports[id] = t
	return t
}

// Forget drops what the proxy holds for a machine: its idle tunnels and the sessions it asked for.
func (s *Service) Forget(id string) {
	s.mu.Lock()
	t := s.transports[id]
	delete(s.transports, id)
	for k := range s.grants {
		if strings.HasSuffix(k, "/"+id) {
			delete(s.grants, k)
		}
	}
	s.mu.Unlock()
	if t != nil {
		t.CloseIdleConnections()
	}
}

// track makes a request cancellable by Kick.
func (s *Service) track(key string, cancel context.CancelFunc) (untrack func()) {
	s.mu.Lock()
	s.nextTrack++
	n := s.nextTrack
	if s.tracked[key] == nil {
		s.tracked[key] = map[uint64]context.CancelFunc{}
	}
	s.tracked[key][n] = cancel
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.tracked[key], n)
		if len(s.tracked[key]) == 0 {
			delete(s.tracked, key)
		}
		s.mu.Unlock()
	}
}

// Kick ends what a person has open on the machines of a space (they were removed, or lost the right): requests in
// flight, sockets and terminals included, and the belief that they may come in.
func (s *Service) Kick(spaceID, userID string) {
	key := userID + "/" + spaceID
	s.mu.Lock()
	cancels := s.tracked[key]
	delete(s.tracked, key)
	delete(s.authz, key)
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// runtimeCookie is the cookie header the runtime takes for a signed-in person.
func runtimeCookie(g hubproto.SessionGrant) string { return g.CookieName + "=" + g.Token }

func (s *Service) forward(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) {
	if s.reg.Get(m.ID) == nil {
		s.offline(w, r)
		return
	}
	g, err := s.grant(r.Context(), u, m)
	if err != nil {
		s.upstreamFailed(w, r, err)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer s.track(u.ID+"/"+m.SpaceID, cancel)()

	secure := s.cfg.Secure
	clientIP := s.auth.ClientIP(r)
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = "http", "runtime"
			pr.Out.Host = pr.In.Host
			// A ticket is the hub's business and ends here.
			if q := pr.Out.URL.Query(); q.Has("hl_ticket") {
				q.Del("hl_ticket")
				pr.Out.URL.RawQuery = q.Encode()
			}
			h := pr.Out.Header
			h.Del("Authorization")
			h.Del("Proxy-Authorization")
			h.Set("Cookie", runtimeCookie(g))
			h.Set("X-Forwarded-Host", pr.In.Host)
			h.Set("X-Forwarded-Proto", map[bool]string{true: "https", false: "http"}[secure])
			h.Set("X-Forwarded-For", clientIP)
		},
		Transport:     s.transport(m.ID),
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			// The runtime's cookies are the hub's business: none of them reaches the browser. In path mode a cookie from
			// here would be the hub's origin's, and a machine could plant a session of its choosing.
			res.Header.Del("Set-Cookie")
			frameHeaders(res.Header)
			if res.StatusCode == http.StatusUnauthorized {
				s.dropGrant(u.ID, m.ID)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() != nil {
				return // the person went away, or was shown out
			}
			s.upstreamFailed(w, r, err)
		},
	}
	rp.ServeHTTP(w, r.WithContext(ctx))
}

// frameHeaders keeps a machine's pages out of frames of other origins, whatever the machine says: a page of another
// site cannot show the person's machine under its own buttons. The runtime's own frames (a preview of the person's app
// inside the page) are the same origin and stay. The CSP is added next to the machine's own; the older header is set
// only when the machine did not (a stricter DENY of its own stays).
func frameHeaders(h http.Header) {
	h.Add("Content-Security-Policy", "frame-ancestors 'self'")
	if h.Get("X-Frame-Options") == "" {
		h.Set("X-Frame-Options", "SAMEORIGIN")
	}
}

// ---- pages ----

func (s *Service) upstreamFailed(w http.ResponseWriter, r *http.Request, err error) {
	var he *httpx.Error
	var ae *hubproto.Error
	switch {
	case errors.Is(err, errOffline), errors.Is(err, link.ErrClosed), errors.As(err, &he) && he.Code == "machine_offline":
		s.offline(w, r)
	case errors.As(err, &he) && he.Code == "machine_policy", errors.As(err, &ae) && ae.Code == hubproto.CodePolicyDenied:
		s.reply(w, r, http.StatusForbidden, "machine_policy", "Запрещено настройками машины",
			"Владелец машины выключил открытие этой машины из harnessly в её настройках (policy.toml).")
	default:
		s.log.Info("machine did not take the request", "path", httpx.SafePath(r.URL.Path), "error", err)
		s.reply(w, r, http.StatusBadGateway, "machine_error", "Машина не отвечает", "Попробуйте ещё раз через минуту.")
	}
}

func (s *Service) offline(w http.ResponseWriter, r *http.Request) {
	s.reply(w, r, http.StatusBadGateway, "machine_offline", "Машина не на связи",
		"Агент на машине сейчас не подключён к harnessly. Проверьте, что машина включена, и обновите страницу.")
}

// reply is a page for a browser that navigates, JSON for everything else.
func (s *Service) reply(w http.ResponseWriter, r *http.Request, status int, code, title, text string) {
	if isNavigation(r) && strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.page(w, r, status, title, text)
		return
	}
	httpx.WriteError(w, r, httpx.New(status, code, title))
}

func (s *Service) page(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	fmt.Fprintf(w, pageTemplate, html.EscapeString(title), html.EscapeString(title), html.EscapeString(text), html.EscapeString(s.cfg.PublicURL+"/"))
}

const pageTemplate = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f6f5f2;color:#1c1b19;font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:30rem;padding:2rem 1.5rem}
h1{font-size:1.5rem;margin:0 0 .75rem}p{margin:0 0 1.5rem;color:#55524c}
a{color:#1c1b19;font-weight:600}
@media (prefers-color-scheme:dark){body{background:#161614;color:#ecebe6}p{color:#a8a59d}a{color:#ecebe6}}
</style></head>
<body><main><h1>%s</h1><p>%s</p><a href="%s">Вернуться в harnessly</a></main></body></html>
`
