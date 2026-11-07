// Package api assembles the hub: it builds every service from one configuration and one database, routes the requests
// among them, and runs the background work (the local machine's code, the sweeping of what expired).
package api

import (
	"context"
	"fmt"
	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/buildinfo"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hub/dist"
	"github.com/ly-solutions/harnessly/internal/hub/docs"
	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/hubsafe"
	"github.com/ly-solutions/harnessly/internal/hub/ingest"
	"github.com/ly-solutions/harnessly/internal/hub/limits"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/mail"
	"github.com/ly-solutions/harnessly/internal/hub/mrsource"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/proxy"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/relay"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/ticket"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/secrets/vaultkv"
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
	// Workspace is the runtime's built page (web/dist): the fleet's workspace at /agents/ (internal/hub/proxy/workspace.go).
	// nil leaves /agents/ to the hub's page.
	Workspace fs.FS
	// SSHDial opens the connections of the SSH install way; nil uses the network. Tests move it.
	SSHDial sshinstall.Dialer
	// SSHOnlineWait shortens the last step of an SSH install (tests).
	SSHOnlineWait time.Duration
	// SiteBlobs replaces the store of the sites' files (tests); nil: the configured folder or bucket.
	SiteBlobs sites.Blobs
	// AgentTimeout shortens every wait for an agent's answer in the owner endpoints (tests).
	AgentTimeout time.Duration
	// EnvFeatures says which parts of the environment section are on for an organization; nil: the ones HUB_ENV_FEATURES
	// names, for every organization.
	EnvFeatures env.FeatureGate
	// Provider replaces the machines' provider that the settings (HUB_PROVIDER*) name (tests).
	Provider provider.Provider
	// Features says whether an organization has a feature on; nil: the resources, load and operations of machines are
	// on for every organization when HUB_MACHINE_RESOURCES=on, else dark (docs/hub/machines-resources.md).
	Features machines.FeatureGate
	// Ext replaces the registry the settings (HUB_EXTENSIONS*) name (tests).
	Ext *ext.Registry
	// Publisher carries the machines' live events to the organization's channel; nil: the space's own stream.
	Publisher machines.Publisher
	// SafeKV replaces the server of the machines' safes that HUB_SECRETS_VAULT_* name (tests).
	SafeKV secrets.KV
}

// Hub is the assembled hub.
type Hub struct {
	Handler http.Handler

	Auth      *auth.Service
	Spaces    *spaces.Service
	Orgs      *orgs.Service
	Analytics *analytics.Service
	// Env is the environment and the catalog of the organizations; its endpoints answer 404 while the features are off.
	Env *env.Service
	// Features are the organizations' flags.
	Features *features.Store
	Machines *machines.Service
	// Ingest pulls the machines' spend facts; nil unless HUB_USAGE_INGEST=on.
	Ingest *ingest.Service
	// Limits sends the machines their limit lists; nil while HUB_LIMITS_ENFORCE=off.
	Limits *limits.Pusher
	// EnvPush sends the machines their environment lists; nil without a feature on.
	EnvPush *env.Pusher
	Relay   *relay.Service
	Proxy   *proxy.Service
	// Docs is the documents and boards of the installation (HUB_DOCS).
	Docs *docs.Service
	// Design is who may see and change a machine's designs, and the guest links to them.
	Design   *designaccess.Service
	Dist     *dist.Handler
	SSH      *sshinstall.Service
	Dialer   *link.Dialer
	Registry *link.Registry
	Events   *events.Broker
	// OrgEvents is the bus of the organizations' live stream (GET /api/orgs/{org}/events).
	OrgEvents *events.MemoryBus
	Tickets   *ticket.Store
	// Sites is the published-sites module; nil when HARNESSLY_SITES is off.
	Sites *sites.Service
	// Ext is the registry of the extension adapters (docs/hub/extensions.md); nil unless HUB_EXTENSIONS=on.
	Ext *ext.Registry
	// Safe keeps the machines' safes on the hub's server; nil when HUB_SECRETS_VAULT_ADDR is not set.
	Safe *hubsafe.Broker

	cfg     *config.Config
	db      *store.DB
	log     *slog.Logger
	limiter *ratelimit.Limiter
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
	host, err := hubproto.DialHost(cfg.AgentURL)
	if err != nil {
		return nil, err
	}

	h := &Hub{cfg: cfg, db: db, log: log, limiter: o.Limiter, Registry: link.NewRegistry(), Events: events.NewBroker(), Tickets: ticket.New(o.Now)}
	if cfg.Replicas {
		id := cfg.ReplicaID
		if id == "" {
			id = link.NewID()
		}
		h.Registry.SetCluster(&link.Cluster{DB: db, ID: id, Addr: cfg.ReplicaAddr, Secret: cfg.ReplicaSecret, Log: log})
	}
	h.Auth, err = auth.New(auth.Deps{Config: cfg, DB: db, Mailer: o.Mailer, Limiter: o.Limiter, Log: log, Now: o.Now, HTTPClient: o.HTTPClient, Endpoints: o.Endpoints})
	if err != nil {
		return nil, err
	}
	var px *proxy.Service
	h.Spaces = spaces.New(spaces.Deps{
		Config: cfg, DB: db, Auth: h.Auth, Mailer: o.Mailer, Events: h.Events, Limiter: o.Limiter, Log: log, Now: o.Now,
		OnMemberGone: func(spaceID, userID string) { px.Kick(spaceID, userID) },
	})
	var budgetSink machines.BudgetSink
	limitsChanged := func() {}
	if cfg.LimitsEnforce != hubproto.EnforceOff {
		h.Limits = limits.NewPusher(limits.PushDeps{DB: db, Reg: h.Registry, Mode: cfg.LimitsEnforce, Log: log, Now: o.Now})
		budgetSink, limitsChanged = h.Limits, h.Limits.Kick
	}
	rb := &rates.Book{DB: db, URL: cfg.RatesURL, Client: o.HTTPClient, Log: log, Now: o.Now}
	h.Features = features.New(db, cfg.Features, o.Now)
	if kv := o.SafeKV; kv != nil || cfg.SafeVault != nil {
		if kv == nil {
			c, err := vaultkv.New(*cfg.SafeVault)
			if err != nil {
				return nil, fmt.Errorf("HUB_SECRETS_VAULT_*: %w", err)
			}
			kv = c
		}
		prefix := cfg.SafePrefix
		if prefix == "" {
			prefix = "harnessly"
		}
		h.Safe = hubsafe.New(kv, prefix, cfg.SafeRefPrefixes)
	}
	h.OrgEvents = events.NewMemoryBus(o.Now)
	h.Orgs = orgs.New(orgs.Deps{Config: cfg, DB: db, Auth: h.Auth, Limiter: o.Limiter, Log: log, Now: o.Now, LimitsChanged: limitsChanged, Rates: rb, Features: h.Features, Events: h.OrgEvents})
	h.Analytics = analytics.New(analytics.Deps{DB: db, Gate: h.Orgs.Gate, Log: log, Now: o.Now, Rates: rb})
	// A part is on for an organization when the instance switches it on for all (HUB_ENV_FEATURES, HUB_MACHINE_RESOURCES)
	// or the organization's flags say so (features.Store: HUB_FEATURES_FORCE, the org's choice, HUB_FEATURES_DEFAULT).
	envFeatures := o.EnvFeatures
	if envFeatures == nil {
		envFeatures = anyGate{env.StaticFeatures(cfg.EnvFeatures), h.Features}
	}
	h.Env = env.New(env.Deps{DB: db, Gate: h.Orgs.Gate, Features: envFeatures, Recommended: cfg.EnvRecommended, Publisher: orgEvents{db: db, broker: h.Events}, Reg: h.Registry, Log: log, Now: o.Now,
		Changed: func() {
			if h.EnvPush != nil {
				h.EnvPush.Kick()
			}
		}})
	var usageSink machines.UsageSink
	if cfg.UsageIngest {
		// The price list starts from the runtime's own numbers; a row an administrator corrected is never overwritten.
		sctx, scancel := context.WithTimeout(context.Background(), 30*time.Second)
		n, err := usage.Seed(sctx, db.S())
		scancel()
		if err != nil {
			return nil, fmt.Errorf("price list: %w", err)
		}
		if n > 0 {
			log.Info("price list seeded", "rows", n)
		}
		// Calls that came before their model had a price get it now; in the background, so a long backlog does not hold
		// the start.
		go func() {
			pctx, pcancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer pcancel()
			if n, err := usage.PriceUnpriced(pctx, db.S()); err != nil {
				log.Warn("pricing stored calls", "err", err)
			} else if n > 0 {
				log.Info("stored calls priced", "calls", n)
			}
		}()
		h.Ingest = ingest.New(ingest.Deps{DB: db, Reg: h.Registry, Log: log, Now: o.Now})
		usageSink = h.Ingest
	}
	var envSink machines.EnvSink
	// An organization may switch the environment on at any time, so the pusher is always there; it pushes nothing to the
	// machines of an organization whose feature is off.
	h.EnvPush = env.NewPusher(env.PushDeps{DB: db, Reg: h.Registry, Features: envFeatures, Recommended: cfg.EnvRecommended, Publisher: orgEvents{db: db, broker: h.Events}, Log: log, Now: o.Now})
	h.Env.UsePusher(h.EnvPush)
	envSink = h.EnvPush
	machineFeatures := o.Features
	if machineFeatures == nil {
		var static []string
		if cfg.Machines.Resources {
			static = []string{machines.FeatureResources}
		}
		machineFeatures = anyGate{env.StaticFeatures(static), h.Features}
	}
	h.Ext = o.Ext
	if h.Ext == nil && cfg.Extensions.On {
		specs, err := ext.LoadSpecs(cfg.Extensions.File)
		if err != nil {
			return nil, err
		}
		if h.Ext, err = ext.NewRegistry(specs, ext.Config{}); err != nil {
			return nil, err
		}
	}
	prov := o.Provider
	if prov == nil {
		if prov, err = newProvider(cfg); err != nil {
			return nil, err
		}
	}
	linkKey, err := sshinstall.MasterKey(cfg.DataDir, cfg.SecretKey)
	if err != nil {
		log.Warn("design access: no hub key, guest link addresses are not kept", "error", err)
		linkKey = nil
	}
	var notice designaccess.Notice
	if n, ok := o.Mailer.(mail.Noticer); ok {
		notice = func(ctx context.Context, to, subject, body, link string) error {
			_, err := n.SendNotice(ctx, to, subject, body, link)
			return err
		}
	}
	h.Design = designaccess.New(designaccess.Deps{
		DB: db, Reg: h.Registry, Log: log, Now: o.Now, HubURL: cfg.PublicURL, ShareURL: cfg.DesignShareURL,
		PlatformAPI: cfg.DesignPlatformAPI, Transport: func(id string) http.RoundTripper { return px.Transport(id) },
		LinkKey: linkKey, Notice: notice,
	})
	h.Docs = docs.New(docs.Deps{Config: cfg, DB: db, Auth: h.Auth, Log: log, Now: o.Now, Links: h.Registry})
	h.Machines = machines.New(machines.Deps{
		Config: cfg, DB: db, Auth: h.Auth, Spaces: h.Spaces, Reg: h.Registry, Tickets: h.Tickets, Limiter: o.Limiter, Log: log, Now: o.Now,
		OnRevoked: func(machineID string) { px.Forget(machineID) }, Usage: usageSink, Budget: budgetSink, Env: envSink,
		Provider: prov, Features: machineFeatures, Publisher: o.Publisher, Design: h.Design, Docs: h.Docs,
	})
	h.Analytics.UseMachines(h.Machines)
	h.Analytics.UseEvents(h.OrgEvents)
	h.Analytics.UseFeatures(h.Features)
	h.Relay = relay.New(relay.Deps{Auth: h.Auth, DB: db, Machines: h.Machines, Reg: h.Registry, Log: log, Now: o.Now, Timeout: o.AgentTimeout})
	px = proxy.New(proxy.Deps{Config: cfg, DB: db, Auth: h.Auth, Spaces: h.Spaces, Reg: h.Registry, Tickets: h.Tickets, Log: log, Now: o.Now, Design: h.Design, Features: h.Features, Workspace: o.Workspace})
	h.Proxy = px
	h.Dist = dist.New(cfg.DistDir, cfg.AgentURL, buildinfo.Version, cfg.ContainerImage)
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
		page = newUI(os.DirFS(cfg.WebDir), true, cfg.BasePath)
	case o.UI != nil:
		page = newUI(o.UI, false, cfg.BasePath)
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
	mux.Handle("GET /api/capabilities", httpx.Handler(h.capabilities))
	h.Auth.Mount(mux)
	h.Spaces.Mount(mux)
	h.Orgs.Mount(mux)
	h.Analytics.Mount(mux)
	h.Env.Mount(mux)
	h.Machines.Mount(mux)
	h.Docs.Mount(mux)
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
	if c := h.Registry.Cluster(); c != nil {
		mux.Handle("/internal/replica/v1/", c.Handler())
	}
	mux.Handle("GET /agent/v1", h.Dialer)
	mux.Handle("POST "+hubproto.SecretsPath, httpx.Handler(h.agentSecrets))
	mux.HandleFunc("DELETE /agent/v1", h.Dialer.ServeUnlink)
	// Messages between agents of different machines (docs/hub/peers.md): off unless HUB_PEERS=on, and then the paths
	// fall through to the 404 below, which the agent reads as "this hub does not carry them".
	if h.cfg.Peers {
		mux.Handle("POST /agent/peers/send", httpx.Handler(h.peersSend))
		mux.Handle("POST /agent/peers/directory", httpx.Handler(h.peersDirectory))
	}
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, httpx.ErrNotFound) })
	mux.Handle("/api/", notFound)
	mux.Handle("/agent/", notFound)
	mux.Handle("/dl/", notFound)
	mux.Handle("/", page)

	csrf := h.Auth.CSRF(mux)
	shares := h.Design.Public()
	routed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case h.Design.IsShareHost(r.Host):
			// A guest link runs on its own origin: the page of one project and nothing of the hub.
			shares.ServeHTTP(w, r)
		case h.Sites != nil && h.Sites.IsSiteHost(r.Host):
			// A published site runs on its own origin and nothing of the hub: no API, no page, no sign-in.
			h.Sites.Edge().ServeHTTP(w, r)
		case proxy.ReloadAsked(r) && !strings.HasPrefix(r.URL.Path, "/m/"):
			// A machine's preview by path reloaded past its slot's prefix (its path is the target's, off /m/): the pane
			// opens it again. A frame of a machine's own page from elsewhere stays refused below.
			proxy.ServeReload(w, r)
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
		if (h.Sites != nil && h.Sites.IsSiteHost(r.Host)) || h.Design.IsShareHost(r.Host) {
			// A published page sets its own headers; the hub's (frame ban, referrer policy) are for the hub's pages.
			routed.ServeHTTP(w, r)
			return
		}
		secured.ServeHTTP(w, r)
	})
	h.Handler = underBase(cfg.BasePath, recoverer(log, accessLog(log, h.Auth.ClientIP, guarded)))
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
	if h.Machines.Enabled() {
		go h.runMachineOps(ctx)
	}
	go h.Analytics.RunLive(ctx, h.OrgEvents.Active)
	go h.Design.Run(ctx)
	go h.Docs.Run(ctx) // comments not yet handed to their conversations
	if h.Ext != nil {
		go h.Ext.Run(ctx, ext.CheckEvery)
	}
	if h.Sites != nil {
		go h.Sites.Run(ctx)
	}
	if p := mrsource.New(h.cfg.MRSourceURL, h.cfg.MRSourceGroups, h.cfg.MRSourceToken, h.db.S(), h.log); p != nil {
		go p.Run(ctx)
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

// runMachineOps picks up the machines' unfinished operations and keeps their upkeep (docs/hub/machines-resources.md).
func (h *Hub) runMachineOps(ctx context.Context) {
	h.Machines.ResumeOps(ctx)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			h.Machines.Maintain(ctx)
		}
	}
}

// newProvider is the provider the settings name.
func newProvider(cfg *config.Config) (provider.Provider, error) {
	if cfg.Machines.Provider != "http" {
		return provider.None{}, nil
	}
	hc := provider.HTTPConfig{BaseURL: cfg.Machines.ProviderURL, TokenFile: cfg.Machines.ProviderTokenFile}
	if cfg.Machines.ProviderProtocol != config.ProviderProtocolV0 {
		p, err := provider.NewAuto(hc, cfg.Machines.ProviderProtocol)
		if err != nil {
			return nil, fmt.Errorf("provider: %w", err)
		}
		return p, nil
	}
	p, err := provider.NewHTTP(hc)
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	return p, nil
}

// Shutdown ends the open event streams, agent links and SSH installs, which would otherwise hold the server from stopping.
func (h *Hub) Shutdown() {
	h.Events.Close()
	h.OrgEvents.Close()
	// Before the services the links' routines call (usage, environment): the dialer ends the links and waits for them.
	if h.Dialer != nil {
		h.Dialer.Close()
	} else {
		h.Registry.CloseAll()
	}
	if h.Ingest != nil {
		h.Ingest.Close()
	}
	if h.Limits != nil {
		h.Limits.Close()
	}
	if h.EnvPush != nil {
		h.EnvPush.Close()
	}
	if h.SSH != nil {
		h.SSH.Close()
	}
}

// anyGate is on for a feature when any of its gates is.
type anyGate []env.FeatureGate

func (g anyGate) On(ctx context.Context, orgID, name string) bool {
	for _, x := range g {
		if x != nil && x.On(ctx, orgID, name) {
			return true
		}
	}
	return false
}
