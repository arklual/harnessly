// Package api assembles the hub: it builds every service from one configuration and one database, routes the requests
// among them, and runs the background work (the local machine's code, the sweeping of what expired).
package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/ly-solutions/harnessly/internal/apptoken"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/automations"
	"github.com/ly-solutions/harnessly/internal/hub/buildinfo"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hub/dist"
	"github.com/ly-solutions/harnessly/internal/hub/docs"
	"github.com/ly-solutions/harnessly/internal/hub/efficiency"
	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hub/eval"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/hubsafe"
	"github.com/ly-solutions/harnessly/internal/hub/ingest"
	"github.com/ly-solutions/harnessly/internal/hub/limits"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/mail"
	"github.com/ly-solutions/harnessly/internal/hub/metrics"
	"github.com/ly-solutions/harnessly/internal/hub/migration"
	"github.com/ly-solutions/harnessly/internal/hub/mrsource"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/proxy"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/relay"
	"github.com/ly-solutions/harnessly/internal/hub/repl"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
	"github.com/ly-solutions/harnessly/internal/hub/switches"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
	"github.com/ly-solutions/harnessly/internal/hub/ticket"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
	"github.com/ly-solutions/harnessly/internal/hub/vaultsvc"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
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
	// StoreMeta and StoreBlobs replace the store's configured drivers (tests); the hub does not close them.
	StoreMeta  filestore.MetaStore
	StoreBlobs filestore.BlobStore
}

// Hub is the assembled hub.
type Hub struct {
	Handler http.Handler
	// Internal serves what the hub's replicas say to one another (HUB_REPLICAS); nil without them. It gets a listener
	// of its own (HUB_REPLICA_LISTEN) and is never mounted on Handler.
	Internal http.Handler
	// Metrics serves the hub's numbers at /metrics in the Prometheus text format, unauthenticated. It gets a listener
	// of its own (HUB_METRICS_LISTEN) and is never mounted on Handler.
	Metrics http.Handler

	Auth      *auth.Service
	Spaces    *spaces.Service
	Orgs      *orgs.Service
	Analytics *analytics.Service
	// Env is the environment and the catalog of the organizations; its endpoints answer 404 while the features are off.
	Env *env.Service
	// Features are the organizations' flags.
	Features *features.Store
	// Switches are the rollout switches by person and machine.
	Switches *switches.Store
	Machines *machines.Service
	// Ingest pulls the machines' spend facts; nil unless HUB_USAGE_INGEST=on.
	Ingest *ingest.Service
	// History keeps the people's chats (HUB_HISTORY); nil when it is off.
	History *history.Service
	// historySweep removes the copies of chats deleted on their machines after their term; nil: nothing is removed.
	historySweep *history.Sweeper
	// Limits sends the machines their limit lists; nil while HUB_LIMITS_ENFORCE=off.
	Limits *limits.Pusher
	// EnvPush sends the machines their environment lists; nil without a feature on.
	EnvPush *env.Pusher
	Relay   *relay.Service
	Proxy   *proxy.Service
	// Docs is the documents and boards of the installation (HUB_DOCS).
	Docs *docs.Service
	// Efficiency serves the figures of the «Эффективность» screen behind the organization flag.
	Efficiency *efficiency.Service
	// Automations is the rules and runs of the installation (HUB_AUTOMATIONS).
	Automations *automations.Service
	// Design is who may see and change a machine's designs, and the guest links to them.
	Design   *designaccess.Service
	Dist     *dist.Handler
	SSH      *sshinstall.Service
	Dialer   *link.Dialer
	Registry *link.Registry
	Events   *events.Broker
	// OrgEvents is the bus of the organizations' live stream (GET /api/orgs/{org}/events).
	OrgEvents *events.MemoryBus
	// Fleet is the bus of the workspace page's live channel (GET /api/v2/machines/events).
	Fleet   *events.FleetBus
	Tickets *ticket.Store
	// Sites is the published-sites module; nil when HARNESSLY_SITES is off.
	Sites *sites.Service
	// Ext is the registry of the extension adapters (docs/hub/extensions.md); nil unless HUB_EXTENSIONS=on.
	Ext *ext.Registry
	// Safe keeps the machines' safes on the hub's server; nil when HUB_SECRETS_VAULT_ADDR is not set.
	Safe *hubsafe.Broker
	// appTokens signs the platform's token for apps' backends (apptoken.go); nil when HUB_APP_TOKEN=off.
	appTokens *appTokens
	// Store keeps the spaces' collections the machines sync with (docs/hub/store.md); nil when HUB_STORE=off.
	Store *storehub.Store
	// Migration is the journal of the move of the machines' data into the store (docs/migrations); nil with
	// the store off.
	Migration *migration.Service

	cfg     *config.Config
	db      *store.DB
	log     *slog.Logger
	limiter *ratelimit.Limiter

	// relay passes this replica's events to the other replicas' streams; nil for a single process.
	relay *events.Relay
	// draining is set once the hub is being stopped: it answers that it is not ready while it still serves.
	draining atomic.Bool
}

// AppsView serves the apps from the store (appsview.Hub): guest links, and a space's apps to its members with every
// machine off; the hub's main sets it, as DesignView. Nil: an app opens only on a machine.
var AppsView func(st *storehub.Store, dataDir string, log *slog.Logger) storehub.AppsViewer

// DesignView makes the store's view of design projects (designview.Hub): guest links and the team's reads and edits
// through the hub with no machine. The hub's main sets it; this package does not import the view, whose design code
// imports the agent, whose tests import this package. Nil: projects are read from the machines only.
var DesignView func(st *storehub.Store, dataDir string, app fs.FS, log *slog.Logger) designaccess.Viewer

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

	if cfg.TunnelMemory > 0 {
		link.SetTunnelMemory(cfg.TunnelMemory)
	}
	h := &Hub{cfg: cfg, db: db, log: log, limiter: o.Limiter, Registry: link.NewRegistry(), Events: events.NewBroker(), Tickets: ticket.NewShared(db, o.Now)}
	if cfg.Replicas {
		id := cfg.ReplicaID
		if id == "" {
			id = link.NewID()
		}
		c := &link.Cluster{DB: db, ID: id, Addr: cfg.ReplicaAddr, Secret: cfg.ReplicaSecret, Log: log}
		h.Registry.SetCluster(c)
		h.Internal = recoverer(log, c.Handler())
	}
	h.Auth, err = auth.New(auth.Deps{Config: cfg, DB: db, Mailer: o.Mailer, Limiter: o.Limiter, Log: log, Now: o.Now, HTTPClient: o.HTTPClient, Endpoints: o.Endpoints})
	if err != nil {
		return nil, err
	}
	var px *proxy.Service
	orgBus := events.NewMemoryBus(o.Now)
	h.OrgEvents = orgBus
	h.Fleet = events.NewFleetBus()
	if c := h.Registry.Cluster(); c != nil {
		// What one replica tells its event streams reaches the streams open on the others.
		if db.CanListen() {
			h.relay = events.NewRelay(c.ID, db, h.Events, h.Fleet, orgBus, log)
		} else {
			log.Warn("HUB_REPLICAS=on without a database connection that can LISTEN (a pooler in transaction mode needs HUB_DB_MIGRATE_URL): a person's event stream hears only the replica it is open on")
		}
	}
	// The workspace page follows the machines of the spaces its person may see: every event of a machine in a space
	// tells the pages that watch the space to read their list again.
	h.Events.SetTap(func(spaceID string, e events.Event) {
		if e.Name == "machine" || e.Name == "machineRemoved" {
			h.Fleet.TouchSpace(spaceID)
		}
	})
	h.Spaces = spaces.New(spaces.Deps{
		Config: cfg, DB: db, Auth: h.Auth, Mailer: o.Mailer, Events: h.Events, Limiter: o.Limiter, Log: log, Now: o.Now,
		OnMemberGone: func(spaceID, userID string) { px.Kick(spaceID, userID) },
		OnMachineGone: func(m store.Machine) {
			if h.Machines != nil {
				h.Machines.Revoked(m)
			}
		},
		OrgEvents: h.OrgEvents,
		OnPeople:  h.Fleet.TouchUser,
	})
	var budgetSink machines.BudgetSink
	limitsChanged := func() {}
	if cfg.LimitsEnforce != hubproto.EnforceOff {
		h.Limits = limits.NewPusher(limits.PushDeps{DB: db, Reg: h.Registry, Mode: cfg.LimitsEnforce, Log: log, Now: o.Now})
		budgetSink, limitsChanged = h.Limits, h.Limits.Kick
	}
	rb := &rates.Book{DB: db, URL: cfg.RatesURL, Client: o.HTTPClient, Log: log, Now: o.Now}
	h.Features = features.New(db, cfg.Features, o.Now)
	h.Switches = switches.New(db, o.Now)
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
	if h.appTokens, err = newAppTokens(cfg.AppToken, cfg.PublicURL, o.HTTPClient); err != nil {
		return nil, err
	}
	if h.appTokens != nil && o.Now != nil {
		h.appTokens.signer.SetClock(o.Now)
	}
	h.Orgs = orgs.New(orgs.Deps{Config: cfg, DB: db, Auth: h.Auth, Limiter: o.Limiter, Log: log, Now: o.Now, LimitsChanged: limitsChanged, Rates: rb, Features: h.Features, Events: h.OrgEvents, PeopleChanged: h.Fleet.TouchUser,
		EnvChanged: func() {
			if h.EnvPush != nil {
				h.EnvPush.Kick()
			}
		}})
	h.Analytics = analytics.New(analytics.Deps{DB: db, Gate: h.Orgs.Gate, Log: log, Now: o.Now, Rates: rb})
	// A part is on for an organization when the instance switches it on for all (HUB_ENV_FEATURES, HUB_MACHINE_RESOURCES)
	// or the organization's flags say so (features.Store: HUB_FEATURES_FORCE, the org's choice, HUB_FEATURES_DEFAULT).
	envFeatures := o.EnvFeatures
	if envFeatures == nil {
		envFeatures = anyGate{env.StaticFeatures(cfg.EnvFeatures), h.Features}
	}
	installValues := env.InstallValues(cfg.BoardsPublicURL, cfg.ArtifactsPublicURL)
	h.Env = env.New(env.Deps{DB: db, Gate: h.Orgs.Gate, Features: envFeatures, Recommended: cfg.EnvRecommended, Install: installValues, Publisher: orgEvents{db: db, broker: h.Events}, Reg: h.Registry, Log: log, Now: o.Now,
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
		safego.Go("price stored calls", func() {
			pctx, pcancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer pcancel()
			if n, err := usage.PriceUnpriced(pctx, db.S()); err != nil {
				log.Warn("pricing stored calls", "err", err)
			} else if n > 0 {
				log.Info("stored calls priced", "calls", n)
			}
		})
		h.Ingest = ingest.New(ingest.Deps{DB: db, Reg: h.Registry, Log: log, Now: o.Now})
		usageSink = h.Ingest
	}
	var envSink machines.EnvSink
	// An organization may switch the environment on at any time, so the pusher is always there; it pushes nothing to the
	// machines of an organization whose feature is off.
	h.EnvPush = env.NewPusher(env.PushDeps{DB: db, Reg: h.Registry, Features: envFeatures, Recommended: cfg.EnvRecommended, Install: installValues, Publisher: orgEvents{db: db, broker: h.Events}, Log: log, Now: o.Now})
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
	// The key the preview slot hosts sign their cookies with; none without the hub key, and then they stay shut.
	var slotKey []byte
	if linkKey != nil {
		sum := sha256.Sum256(append([]byte("harnessly-hub/preview-slot/v1\x00"), linkKey...))
		slotKey = sum[:]
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
		ShareAliases: cfg.DesignShareAliases, PlatformAPI: cfg.DesignPlatformAPI, Transport: func(id string) http.RoundTripper { return px.Transport(id) },
		LinkKey: linkKey, Notice: notice, Shared: !cfg.Store.Off, Kits: kitAccess(cfg, db, o),
	})
	h.Efficiency = &efficiency.Service{DB: db, OrgGate: h.Orgs.OrgGate, Features: h.Features, Switches: h.Switches, Now: o.Now}
	h.Docs = docs.New(docs.Deps{Config: cfg, DB: db, Auth: h.Auth, Log: log, Now: o.Now, Links: h.Registry, Gate: h.Switches, Notice: func(ctx context.Context, to, subject, body, link string) error {
		if n, ok := o.Mailer.(mail.Noticer); ok {
			_, err := n.SendNotice(ctx, to, subject, body, link)
			return err
		}
		return nil
	}})
	h.Automations = automations.New(automations.Deps{Config: cfg, DB: db, Auth: h.Auth, Log: log, Now: o.Now, Links: h.Registry})
	var historySink machines.HistorySink
	if cfg.History {
		h.History = history.New(history.Deps{DB: db, Reg: h.Registry, Log: log, Now: o.Now})
		historySink = h.History
		if cfg.HistoryDeletedKeep > 0 {
			h.historySweep = history.NewSweeper(history.SweepDeps{DB: db, Log: log, Now: o.Now, Keep: cfg.HistoryDeletedKeep})
		}
	}
	h.Machines = machines.New(machines.Deps{
		Config: cfg, DB: db, Auth: h.Auth, Spaces: h.Spaces, Reg: h.Registry, Tickets: h.Tickets, Limiter: o.Limiter, Log: log, Now: o.Now,
		OnRevoked: func(machineID string) { px.Forget(machineID) }, Usage: usageSink, Budget: budgetSink, Env: envSink,
		Provider: prov, Features: machineFeatures, Publisher: o.Publisher, Design: h.Design, Docs: h.Docs, Automations: h.Automations,
		History: historySink, Switches: h.Switches, Notice: noticeOf(o.Mailer),
	})
	h.Analytics.UseMachines(h.Machines)
	h.Analytics.UseEvents(h.OrgEvents)
	h.Analytics.UseFeatures(h.Features)
	h.Relay = relay.New(relay.Deps{Auth: h.Auth, DB: db, Machines: h.Machines, Reg: h.Registry, Log: log, Now: o.Now, Timeout: o.AgentTimeout})
	px = proxy.New(proxy.Deps{Config: cfg, DB: db, Auth: h.Auth, Spaces: h.Spaces, Reg: h.Registry, Tickets: h.Tickets, Log: log, Now: o.Now, Design: h.Design, Features: h.Features, Workspace: o.Workspace, History: cfg.History, VoiceTakes: cfg.VoiceTakes, Fleet: h.Fleet, Switches: h.Switches, SlotKey: slotKey})
	// A person whose sessions were all ended at once holds nothing open: neither the live streams of their spaces nor
	// what they had open on the machines.
	h.Auth.OnSessionsEnded(func(userID string) {
		spaces, err := db.S().SpacesByUser(context.Background(), userID)
		if err != nil {
			log.Warn("open streams not ended", "user", userID, "error", err)
			return
		}
		for _, sp := range spaces {
			h.Events.Kick(sp.ID, userID)
			px.Kick(sp.ID, userID)
		}
	})
	// The workspace's documents are the hub's own: no machine has to be up to open one.
	px.UseDocs(h.Docs.Handler(), h.Docs.Prefix())
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
	var appsView storehub.AppsViewer
	if err := h.newStore(o); err != nil {
		return nil, err
	}
	if cfg.VaultKeys != "" {
		// The hub's central password safe (docs/hub/vault.md); without HUB_VAULT_KEYS the page's /vault calls go to a machine as before.
		if kr, err := vaultsvc.ParseKeys(cfg.VaultKeys); err != nil {
			log.Warn("the password safe is off: HUB_VAULT_KEYS is not valid", "error", err)
		} else {
			px.UseVault(vaultsvc.New(db, vaultsvc.Config{Keyring: kr, SharedMembers: vaultsvc.Emails(cfg.VaultSharedMembers), SharedEditors: vaultsvc.Emails(cfg.VaultSharedEditors)}, log, o.Now))
		}
	}
	if h.Store != nil {
		px.UseUploads(h.Store)
		h.Migration = migration.NewService(migration.New(h.db, o.Now), h.Auth, h.Store.SessionMachine, h.mayOpenMachine,
			h.mayChangeMachine, h.Store)
		if h.History != nil {
			now := o.Now
			if now == nil {
				now = time.Now
			}
			h.Migration.UseSidebars(h.db, func() int64 { return now().UnixMilli() })
		}
		// guest links read their projects from the store, put into folders of the hub; no machine takes part.
		if DesignView != nil {
			if v := DesignView(h.Store, cfg.DataDir, o.Workspace, log); v != nil {
				h.Design.UseView(v)
			}
		}
		// an app's guest link opens from the store, with the machine that made it off.
		if AppsView != nil && appsView == nil {
			appsView = AppsView(h.Store, cfg.DataDir, log)
		}
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

	if u, ok := page.(*ui); ok && h.extUIOn() {
		u.frames = h.Ext.FrameOrigins()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.Handle("GET /api/config", httpx.Handler(h.config))
	mux.Handle("GET /api/capabilities", httpx.Handler(h.capabilities))
	mux.Handle("GET /api/ext/ui", httpx.Handler(h.extUI))
	h.Auth.Mount(mux)
	h.Spaces.Mount(mux)
	h.Orgs.Mount(mux)
	h.Analytics.Mount(mux)
	h.Env.Mount(mux)
	h.Machines.Mount(mux)
	h.Docs.Mount(mux)
	h.Efficiency.Mount(mux)
	h.Automations.Mount(mux)
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
	h.Dist.MountAdmin(mux, func(f func(http.ResponseWriter, *http.Request, bool) error) httpx.Handler {
		return h.Auth.Require(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error { return f(w, r, p.Admin) })
	})
	h.mountSwitches(mux)
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
	if h.Store != nil {
		h.Store.Mount(mux)
		h.Migration.Mount(mux)
	}
	// Nothing under /internal/ is for the outside: the replicas' hop has a listener of its own (Internal).
	mux.Handle("/internal/", http.NotFoundHandler())
	mux.Handle("GET /agent/v1", h.Dialer)
	mux.Handle("POST "+hubproto.SecretsPath, httpx.Handler(h.agentSecrets))
	mux.Handle("POST "+hubproto.AppTokenPath, httpx.Handler(h.agentAppToken))
	mux.HandleFunc("GET "+apptoken.JWKSPath, h.jwks)
	mux.HandleFunc("GET "+apptoken.DiscoveryPath, h.discovery)
	mux.HandleFunc("DELETE /agent/v1", h.Dialer.ServeUnlink)
	// Messages between agents of different machines (docs/hub/peers.md): off unless HUB_PEERS=on, and then the paths
	// fall through to the 404 below, which the agent reads as "this hub does not carry them".
	if h.cfg.Peers {
		mux.Handle("POST /agent/peers/send", httpx.Handler(h.peersSend))
		mux.Handle("POST /agent/peers/directory", httpx.Handler(h.peersDirectory))
	}
	// A person's agent memory for all of the person's machines (docs/hub/memory-sync.md): off unless HUB_MEMORY_SYNC=on.
	if h.cfg.Memory {
		mux.Handle("POST "+hubproto.MemoryPullPath, httpx.Handler(h.memoryPull))
		mux.Handle("POST "+hubproto.MemoryPutPath, httpx.Handler(h.memoryPut))
	}
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, httpx.ErrNotFound) })
	if appsView != nil {
		mux.Handle("/apps/guest/", appsView)                   // apps.GuestPrefix
		mux.HandleFunc("/apps/space/", h.appsMember(appsView)) // apps.MemberPrefix
		h.Spaces.Sub().Handle("GET /api/spaces/{spaceId}/apps", tenant.Guard{Auth: h.Auth, Spaces: h.Spaces}.Space(tenant.Member, h.spaceApps(appsView)))
	}
	mux.Handle("/api/", notFound)
	mux.Handle("/agent/", notFound)
	mux.Handle("/dl/", notFound)
	mux.Handle("/", page)

	csrf := h.Auth.CSRF(mux)
	// A separate origin for the guests of apps (HUB_APPS_GUEST_URL): it serves the guest links and nothing of the hub.
	guestHost := ""
	if u, err := url.Parse(cfg.AppsGuestURL); err == nil && cfg.AppsGuestURL != "" {
		guestHost = strings.ToLower(u.Host)
	}
	isGuestHost := func(host string) bool { return guestHost != "" && strings.EqualFold(host, guestHost) }
	shares := h.Design.Public()
	legacyDocs := docs.LegacyRedirect(cfg.PublicURL)
	routed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case isGuestHost(r.Host):
			if appsView == nil || !strings.HasPrefix(r.URL.Path, "/apps/guest/") {
				httpx.WriteError(w, r, httpx.ErrNotFound)
				return
			}
			appsView.ServeHTTP(w, r)
		case guestHost != "" && strings.HasPrefix(r.URL.Path, "/apps/guest/"):
			// With a host of their own the guest links are not served next to the hub's session.
			httpx.WriteError(w, r, httpx.ErrNotFound)
		case h.Design.IsShareAlias(r.Host) && !(cfg.DesignShareDocs && docGuestLink(mux, r)):
			// A former address of the guest links: /s/<token> goes on to the share host, nothing else is there.
			h.Design.ShareRedirect(w, r)
		case cfg.DesignShareDocs && (h.Design.IsShareHost(r.Host) || h.Design.IsShareAlias(r.Host)) && docGuestLink(mux, r):
			// HUB_DESIGN_SHARE_DOCS: a document's guest link (/d/<token>) opens under the address it was handed out at.
			// The page sets its own sandbox; the hub's session is not on this origin.
			mux.ServeHTTP(w, r)
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
		case h.Proxy.IsSlotHost(r.Host):
			// A preview slot runs on an origin of its own: its page and nothing of the hub, let in by its own cookie.
			h.Proxy.ServeSlotHost(w, r)
		case cfg.IsDocsLegacyHost(r.Host):
			// The old document addresses: a redirect to the document's page, which decides who may read it.
			legacyDocs.ServeHTTP(w, r)
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
		if (h.Sites != nil && h.Sites.IsSiteHost(r.Host)) || h.Design.IsShareHost(r.Host) || h.Design.IsShareAlias(r.Host) || isGuestHost(r.Host) || h.Proxy.IsSlotHost(r.Host) {
			// A published page sets its own headers; the hub's (frame ban, referrer policy) are for the hub's pages.
			routed.ServeHTTP(w, r)
			return
		}
		secured.ServeHTTP(w, r)
	})
	h.Handler = underBase(cfg.BasePath, recoverer(log, accessLog(log, h.Auth.ClientIP, guarded)))
	h.Metrics = recoverer(log, metrics.Handler(h.collectMetrics))
	return h, nil
}

// collectMetrics writes the gauges read from the hub's state at the moment of a scrape.
func (h *Hub) collectMetrics(w *metrics.Writer) {
	st := h.Registry.Stats()
	w.Gauge("links", "Agent links this hub process holds.", float64(st.Links))
	w.Gauge("tunnels_open", "Tunnels to machines open over the links.", float64(st.Tunnels))
	w.Gauge("machine_calls_pending", "Requests to agents waiting for an answer.", float64(st.Calls))
	tm := link.TunnelMemory()
	w.Gauge("tunnel_memory_bytes", "Bytes of machines' answers the tunnels hold or have promised to take, for readers that have not read them yet.", float64(tm.Used))
	w.Gauge("tunnel_memory_limit_bytes", "The most the tunnels may hold together (HUB_TUNNEL_MEMORY_MB).", float64(tm.Limit))
	w.Counter("tunnels_refused_total", "Tunnels not opened because the memory for tunnels was full.", float64(tm.Refused))
	w.Counter("tunnels_cut_total", "Tunnels ended because their reader fell too far behind or the machine sent past its window.", float64(tm.Cut))
	const queueHelp = "Events of machines read and not handled yet, by queue (bulk: frames of the chats' history)."
	w.Gauge("link_event_queue_length", queueHelp, float64(st.Events), "queue", "events")
	w.Gauge("link_event_queue_length", queueHelp, float64(st.Bulk), "queue", "bulk")
	const oldestHelp = "Age of the oldest event of machines read and not handled yet, by queue, in seconds."
	w.Gauge("link_event_queue_oldest_seconds", oldestHelp, st.OldestEvent.Seconds(), "queue", "events")
	w.Gauge("link_event_queue_oldest_seconds", oldestHelp, st.OldestBulk.Seconds(), "queue", "bulk")
	// The frames of the chats' history are the bulk queue: its oldest frame is how far the hub's copy is behind what
	// it was sent.
	w.Gauge("history_lag_seconds", "Age of the oldest history frame the hub has read and not stored yet, in seconds.", st.OldestBulk.Seconds())
	var usageLag time.Duration
	var usageWaiting int
	if h.Ingest != nil {
		usageLag, usageWaiting = h.Ingest.Lag()
	}
	w.Gauge("usage_lag_seconds", "Longest wait of a machine's spend the hub was told of and has not stored yet, in seconds.", usageLag.Seconds())
	w.Gauge("usage_machines_waiting", "Machines whose spend the hub was told of and has not stored yet.", float64(usageWaiting))
	// One measure of how far the hub's copy of each kind of a machine's data is behind: the batches' own, the old
	// history frames' queue and the spend's pull.
	if h.Machines != nil {
		h.Machines.Repl().Write(w, time.Now(), repl.Measured(), map[string]time.Duration{
			"usage": usageLag, hubproto.ReplHistoryEvents: st.OldestBulk, hubproto.ReplHistoryBackfill: st.OldestBulk})
	}
	// The hub's own queues, read from the database at the scrape: how much waits and for how long.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Now()
	queue := func(name, rowsHelp, ageHelp string, read func(context.Context) (store.QueueStat, error)) {
		st, err := read(ctx)
		if err != nil {
			return
		}
		var age float64
		if st.OldestMs > 0 {
			age = max(0, now.Sub(time.UnixMilli(st.OldestMs)).Seconds())
		}
		w.Gauge(name+"_rows", rowsHelp, float64(st.Rows))
		w.Gauge(name+"_oldest_seconds", ageHelp, age)
	}
	q := h.db.S()
	queue("usage_quarantine", "Records of machines' spend set aside because the hub could not read or store them.",
		"Age of the oldest record of spend set aside, in seconds.", q.UsageQuarantineStat)
	queue("offline_prompts_waiting", "Messages written to machines that were off and not handed over yet.",
		"Age of the oldest message that waits for its machine, in seconds.", q.OfflinePromptsStat)
	queue("automation_runs_queued", "Automation runs that wait to be started on a machine.",
		"Age of the oldest automation run that waits to be started, in seconds.", q.AutomationRunsStat)
	w.Counter("background_panics_total", "Panics of background work held since the hub started.", float64(safego.Panics()))
	services := h.Docs.Services()
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		up := 0.0
		if services[name].Up {
			up = 1
		}
		w.Gauge("app_service_up", "Whether the documents or boards service answered its last /readyz (1) or not (0).", up, "service", name)
	}
}

func (h *Hub) healthz(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "version": buildinfo.Version})
}

func (h *Hub) readyz(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "draining": true})
		return
	}
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
	safego.Go("local machine code", func() { h.Machines.RunLocalCode(ctx) })
	if h.Machines.Enabled() {
		safego.Go("machine operations", func() { h.runMachineOps(ctx) })
	}
	if h.relay != nil {
		safego.Go("events relay", func() { h.relay.Run(ctx) })
	}
	safego.Go("live analytics", func() { h.Analytics.RunLive(ctx, h.OrgEvents.Active) })
	safego.Go("design", func() { h.Design.Run(ctx) })
	// messages written to a machine that was off, handed over once it is back
	safego.Go("outbox", func() { h.Proxy.RunOutbox(ctx) })
	// new chats started while no machine answered, made once their machine is back
	safego.Go("outbox chats", func() { h.Proxy.RunOutboxChats(ctx) })
	// comments not yet handed to their conversations
	safego.Go("docs", func() { h.Docs.Run(ctx) })
	if h.historySweep != nil {
		// the copies of chats deleted on their machines, removed after their term (HUB_HISTORY_DELETED_DAYS)
		safego.Go("history sweep", func() { h.historySweep.Run(ctx) })
	}
	// queued automation runs, started on their owners' machines (HUB_AUTOMATIONS)
	safego.Go("automations", func() { h.Automations.Run(ctx) })
	if h.Ext != nil {
		safego.Go("extensions", func() { h.Ext.Run(ctx, ext.CheckEvery) })
	}
	if h.Sites != nil {
		safego.Go("sites", func() { h.Sites.Run(ctx) })
	}
	if raw := h.cfg.Switches; raw != "" {
		if rows, err := switches.ParseSeeds(raw); err != nil {
			h.log.Error("switches from HUB_SWITCHES", "err", err)
		} else if n, err := h.Switches.Apply(ctx, rows); err != nil {
			h.log.Error("switches from HUB_SWITCHES", "err", err)
		} else {
			h.log.Info("switches from HUB_SWITCHES", "stored", n)
		}
	}
	if raw := h.cfg.EfficiencyMarkers; raw != "" {
		if n, err := efficiency.ImportMarkers(ctx, h.db.S(), raw, time.Now()); err != nil {
			h.log.Error("efficiency markers", "err", err)
		} else {
			h.log.Info("efficiency markers", "stored", n)
		}
	}
	if raw := h.cfg.EfficiencyRegressions; raw != "" {
		if n, err := efficiency.ImportRegressions(ctx, h.db.S(), raw); err != nil {
			h.log.Error("efficiency regressions", "err", err)
		} else {
			h.log.Info("efficiency regressions", "read", n)
		}
	}
	if c := h.efficiencyCollector(); c != nil {
		safego.Go("efficiency collector", func() { c.Run(ctx) })
	}
	// the evaluation's scheduler: under the hub-eval rollout switch; it runs a task only on a machine whose agent
	// offers eval.run (the operator opted in there with AGENT_HUB_EVAL=on)
	evalLink := &eval.HubLink{Links: h.Registry}
	if h.Machines != nil {
		evalLink.Offers = h.Machines.Offers
	}
	evalSched := eval.New(eval.Deps{DB: h.db, Gate: h.Switches, Link: evalLink, Log: h.log})
	safego.Go("eval", func() { evalSched.Run(ctx) })
	if p := mrsource.New(h.cfg.MRSourceURL, h.cfg.MRSourceGroups, h.cfg.MRSourceToken, h.db.S(), h.log); p != nil {
		safego.Go("merge request source", func() { p.Run(ctx) })
	}
	// warm the sums of the binaries before the first machine asks
	safego.Go("dist manifest", func() { _, _ = h.Dist.Manifest() })
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		if err := h.Auth.Housekeeping(ctx); err != nil && ctx.Err() == nil {
			h.log.Warn("housekeeping failed", "error", err)
		}
		h.Tickets.Sweep()
		h.Dialer.Sweep()
		// uploads in parts nobody finished (the store throttles this to once an hour)
		h.Store.SweepUploads(ctx)
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

// BeginDrain takes the hub out of rotation without stopping it: /readyz answers 503 (and /healthz stays 200, so the
// process is not restarted), and a machine that dials is turned away to dial again. Requests and the links already
// taken go on.
func (h *Hub) BeginDrain() {
	h.draining.Store(true)
	if h.Dialer != nil {
		h.Dialer.Refuse()
	}
}

// DrainLinks asks the machines linked to this process to dial again, one at a time over window, and returns how many
// it asked.
func (h *Hub) DrainLinks(ctx context.Context, window time.Duration) int {
	return h.Registry.Drain(ctx, window)
}

// Shutdown ends the open event streams, agent links and SSH installs, which would otherwise hold the server from stopping.
func (h *Hub) Shutdown() {
	defer h.Store.Close()
	h.Events.Close()
	h.OrgEvents.Close()
	h.Fleet.Close()
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

// efficiencyCollector reads the sources of the «Эффективность» screen. It uses the merge request source of the hub
// (HUB_MR_SOURCE_GITLAB_*) and runs only when the instance turns the flag on for everyone or forces it (HUB_FEATURES_*);
// nothing is collected for a hub that does not show the screen. Nil when there is nothing to do.
func (h *Hub) efficiencyCollector() *efficiency.Collector {
	if !slices.Contains(h.Features.Instance(), features.Efficiency) {
		return nil
	}
	gl := efficiency.NewGitLab(h.cfg.MRSourceURL, h.cfg.MRSourceGroups, h.cfg.MRSourceToken)
	if gl == nil {
		return nil
	}
	gl.SkillsProject = h.cfg.MRSourceSkillsProject
	col := &efficiency.Collector{Store: h.db.S(), Code: gl, Log: h.log, Now: time.Now}
	if yt := efficiency.NewYouTrack(h.cfg.TrackerURL, h.cfg.TrackerToken, h.cfg.TrackerProject); yt != nil {
		col.Tracker = yt
	}
	if ar := efficiency.NewArtifacts(h.cfg.ArtifactsURL, h.cfg.ArtifactsToken); ar != nil {
		col.Documents = ar
	}
	return col
}

// noticeOf is the mailer's short notices as a function; nil when the mailer sends none.
func noticeOf(m any) func(ctx context.Context, to, subject, body, link string) error {
	n, ok := m.(mail.Noticer)
	if !ok {
		return nil
	}
	return func(ctx context.Context, to, subject, body, link string) error {
		_, err := n.SendNotice(ctx, to, subject, body, link)
		return err
	}
}

// docGuestLink is a request the documents' guest page answers (GET /d/<token>): the route is looked up in the mux,
// so with documents or their access off nothing is served and the request is not the hub's page by accident.
func docGuestLink(mux *http.ServeMux, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	_, pattern := mux.Handler(r)
	return pattern == "GET /d/{token}"
}
