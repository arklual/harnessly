// Package api assembles the hub: it builds every service from one configuration and one database, routes the requests
// among them, and runs the background work (the local machine's code, the sweeping of what expired).
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/buildinfo"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/dist"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/mail"
	"github.com/ly-solutions/harnessly/internal/hub/proxy"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/relay"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/ticket"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/sites"
)

// Options are what the hub is built from. Only Config, DB and Mailer are required.
type Options struct {
	Config *config.Config
	DB     *store.DB
	Mailer mail.Mailer
	Log    *slog.Logger
	// Now is the clock; nil means time.Now.
	Now     func() time.Time
	Limiter *ratelimit.Limiter
	// HTTPClient and Endpoints are for the calls to sign-in providers; tests move them.
	HTTPClient *http.Client
	Endpoints  auth.Endpoints
	// UI is the built hub page (index.html and assets/); nil serves a note instead. Config.WebDir, when set, wins.
	UI fs.FS
	// SSHDial opens the connections of the SSH install way; nil uses the network. Tests move it.
	SSHDial sshinstall.Dialer
	// SSHOnlineWait shortens the last step of an SSH install (tests).
	SSHOnlineWait time.Duration
	// SiteBlobs replaces the store of the sites' files (tests); nil: the configured folder or bucket.
	SiteBlobs sites.Blobs
	// AgentTimeout shortens every wait for an agent's answer in the owner endpoints (tests).
	AgentTimeout time.Duration
}

// Hub is the assembled hub.
type Hub struct {
	Handler http.Handler

	Auth     *auth.Service
	Spaces   *spaces.Service
	Machines *machines.Service
	Relay    *relay.Service
	Proxy    *proxy.Service
	Dist     *dist.Handler
	SSH      *sshinstall.Service
	Dialer   *link.Dialer
	Registry *link.Registry
	Events   *events.Broker
	Tickets  *ticket.Store
	// Sites is the published-sites module; nil when HARNESSLY_SITES is off.
	Sites *sites.Service

	cfg *config.Config
	db  *store.DB
	log *slog.Logger
}

// New builds the hub.
func New(o Options) (*Hub, error) {
	cfg, db := o.Config, o.DB
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	if o.Limiter == nil {
		o.Limiter = ratelimit.New()
		if o.Now != nil {
			o.Limiter = o.Limiter.WithClock(o.Now)
		}
	}
	host, err := hubproto.DialHost(cfg.PublicURL)
	if err != nil {
		return nil, err
	}

	h := &Hub{cfg: cfg, db: db, log: log, Registry: link.NewRegistry(), Events: events.NewBroker(), Tickets: ticket.New(o.Now)}
	h.Auth, err = auth.New(auth.Deps{Config: cfg, DB: db, Mailer: o.Mailer, Limiter: o.Limiter, Log: log, Now: o.Now, HTTPClient: o.HTTPClient, Endpoints: o.Endpoints})
	if err != nil {
		return nil, err
	}
	var px *proxy.Service
	h.Spaces = spaces.New(spaces.Deps{
		Config: cfg, DB: db, Auth: h.Auth, Mailer: o.Mailer, Events: h.Events, Limiter: o.Limiter, Log: log, Now: o.Now,
		OnMemberGone: func(spaceID, userID string) { px.Kick(spaceID, userID) },
	})
	h.Machines = machines.New(machines.Deps{
		Config: cfg, DB: db, Auth: h.Auth, Spaces: h.Spaces, Reg: h.Registry, Tickets: h.Tickets, Limiter: o.Limiter, Log: log, Now: o.Now,
		OnRevoked: func(machineID string) { px.Forget(machineID) },
	})
	h.Relay = relay.New(relay.Deps{Auth: h.Auth, DB: db, Machines: h.Machines, Reg: h.Registry, Log: log, Now: o.Now, Timeout: o.AgentTimeout})
	px = proxy.New(proxy.Deps{Config: cfg, DB: db, Auth: h.Auth, Spaces: h.Spaces, Reg: h.Registry, Tickets: h.Tickets, Log: log, Now: o.Now})
	h.Proxy = px
	h.Dist = dist.New(cfg.DistDir, cfg.PublicURL, buildinfo.Version, cfg.ContainerImage)
	if cfg.SSH {
		// The way is optional: a hub whose data folder cannot hold the key runs without it, and the page hides it.
		h.SSH, err = sshinstall.New(sshinstall.Deps{
			Config: cfg, DB: db, Auth: h.Auth, Spaces: h.Spaces, Machines: h.Machines, Reg: h.Registry, Events: h.Events,
			Limiter: o.Limiter, Log: log, Now: o.Now, Dial: o.SSHDial, OnlineWait: o.SSHOnlineWait,
		})
		if err != nil {
			log.Warn("SSH install is off: the key that protects the spaces' SSH keys is not available (HUB_SECRET_KEY, or a writable data folder)", "error", err)
			h.SSH = nil
		}
	}
	h.Dialer = &link.Dialer{
		DB: db, Reg: h.Registry, Limiter: o.Limiter, Log: log, Now: o.Now, Hooks: h.Machines.Hooks(), Host: host,
		MinAgentVersion: cfg.MinAgentVersion, ClientIP: h.Auth.ClientIP,
	}

	if err := h.newSites(o); err != nil {
		return nil, err
	}

	var page http.Handler
	switch {
	case cfg.WebDir != "":
		page = newUI(os.DirFS(cfg.WebDir), true)
	case o.UI != nil:
		page = newUI(o.UI, false)
	default:
		page = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(noUI))
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.Handle("GET /api/config", httpx.Handler(h.config))
	h.Auth.Mount(mux)
	h.Spaces.Mount(mux)
	h.Machines.Mount(mux)
	if h.SSH != nil {
		h.SSH.Mount(h.Spaces.Sub())
	} else {
		// Off by setting or for want of a key: the route answers what the contract says rather than not existing.
		off := h.Auth.Require(func(http.ResponseWriter, *http.Request, *auth.Principal) error { return httpx.ErrProviderOff })
		h.Spaces.Sub().Handle("GET /api/spaces/{spaceId}/machines/ssh", off)
		h.Spaces.Sub().Handle("POST /api/spaces/{spaceId}/machines/ssh", off)
	}
	h.Relay.Mount(mux)
	h.Dist.Mount(mux)
	h.Proxy.Mount(mux)
	if h.Sites != nil {
		h.Sites.Mount(mux)
		mux.Handle("GET /api/site-entry", httpx.Handler(h.siteEntry))
		mux.Handle("GET /api/site-check", httpx.Handler(h.siteCheck))
		mux.Handle("POST /agent/sites", httpx.Handler(h.siteSession))
		if h.cfg.FleetSitesSecret != "" {
			mux.Handle("POST /agent/fleet-sites", httpx.Handler(h.fleetSiteSession))
		}
		if !h.Sites.Subdomains() {
			mux.Handle("/sites/", h.Sites.PathEdge())
		}
	}
	mux.Handle("GET /agent/v1", h.Dialer)
	mux.HandleFunc("DELETE /agent/v1", h.Dialer.ServeUnlink)
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, httpx.ErrNotFound) })
	mux.Handle("/api/", notFound)
	mux.Handle("/agent/", notFound)
	mux.Handle("/dl/", notFound)
	mux.Handle("/", page)

	csrf := h.Auth.CSRF(mux)
	routed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case h.Sites != nil && h.Sites.IsSiteHost(r.Host):
			// A published site runs on its own origin and nothing of the hub: no API, no page, no sign-in.
			h.Sites.Edge().ServeHTTP(w, r)
		case cfg.MachineIDFromHost(r.Host) != "":
			// A machine's own origin runs the machine and nothing of the hub: no API, no page, no sign-in.
			h.Proxy.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			csrf.ServeHTTP(w, r)
		default:
			mux.ServeHTTP(w, r)
		}
	})
	secured := securityHeaders(cfg.Secure, routed)
	guarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.Sites != nil && h.Sites.IsSiteHost(r.Host) {
			// A published page sets its own headers; the hub's (frame ban, referrer policy) are for the hub's pages.
			routed.ServeHTTP(w, r)
			return
		}
		secured.ServeHTTP(w, r)
	})
	h.Handler = recoverer(log, accessLog(log, h.Auth.ClientIP, guarded))
	return h, nil
}

func (h *Hub) healthz(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "version": buildinfo.Version})
}

func (h *Hub) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Run does the hub's background work until ctx ends: the local machine's registration code, and the sweeping of what
// expired (sessions, challenges, tickets, remembered nonces).
func (h *Hub) Run(ctx context.Context) {
	go h.Machines.RunLocalCode(ctx)
	if h.Sites != nil {
		go h.Sites.Run(ctx)
	}
	go func() { _, _ = h.Dist.Manifest() }() // warm the sums of the binaries before the first machine asks
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		if err := h.Auth.Housekeeping(ctx); err != nil && ctx.Err() == nil {
			h.log.Warn("housekeeping failed", "error", err)
		}
		h.Tickets.Sweep()
		h.Dialer.Sweep()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Shutdown ends the open event streams, agent links and SSH installs, which would otherwise hold the server from stopping.
func (h *Hub) Shutdown() {
	h.Events.Close()
	h.Registry.CloseAll()
	if h.SSH != nil {
		h.SSH.Close()
	}
}
