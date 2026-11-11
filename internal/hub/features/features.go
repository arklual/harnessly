// Package features is the organizations' flags: parts of the hub that are switched on for an organization at a time
// (docs/hub/api.md, «Feature flags»). Everything new in the hub ships dark behind one of them.
//
// A flag is on for an organization when, in this order: the instance forces it (HUB_FEATURES_FORCE, `name` on, `-name`
// off); else the organization chose (table org_features); else the instance defaults it on (HUB_FEATURES_DEFAULT);
// else it is off.
package features

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The flags the hub knows.
const (
	// UIHX is the new interface; the old one stays until everything is switched over.
	UIHX = "ui.hx"
	// Env is the machines' environment screen.
	Env = "env"
	// Catalog is the catalog of tools and skills.
	Catalog = "catalog"
	// MachineResources is editing a machine's resources and showing their price.
	MachineResources = "machine.resources"
	// OrgEvents is the organization's live stream (GET /api/orgs/{org}/events).
	OrgEvents = "org.events"
	// UIHXLive is the lively hub: the common motion on every screen, free periods, sortable tables and the tools screen.
	UIHXLive = "ui.hx.live"
	// RuntimeFleet is one runtime for all of a person's machines: GET /api/me/machines and the /api/v2 proxy of the hub.
	RuntimeFleet = "runtime.fleet"
	// SecretsVault is the hub's safe: the machines of the organization keep their secrets on the hub's server
	// (HUB_SECRETS_VAULT_*, docs/safe.md).
	SecretsVault = "secrets.vault"
	// Efficiency is the «Эффективность» screen's figures: GET /api/orgs/{org}/efficiency/summary.
	Efficiency = "efficiency"
	// ChatResume is continuing a chat on another machine of the person while the one it ran on is off, from the resume
	// snapshot its machine left in the hub's store after each turn (docs/hub/resume.md).
	ChatResume = "chat.resume"
	// MachineSeats shows the machines of one server (one hostname in a tenant, a Linux user per person) as one server
	// with the people's seats in it; the server's load and hardware are counted once.
	MachineSeats = "machine.seats"
	// EnvPacks lets an organization put environment packs (tools, hooks, MCP and the safe's rules in a versioned
	// repository folder) in its catalog (docs/hub/packs.md).
	EnvPacks = "env.packs"
)

// Known are the flag names in the order they are shown.
var Known = []string{UIHX, Env, Catalog, MachineResources, OrgEvents, UIHXLive, RuntimeFleet, SecretsVault, Efficiency, ChatResume, MachineSeats, EnvPacks}

// IsKnown says that name is a flag of the hub.
func IsKnown(name string) bool { return slices.Contains(Known, name) }

// Gate answers whether a flag is on for an organization (its id, or `space:<id>` for a space that is in none).
type Gate interface {
	On(ctx context.Context, orgID, name string) bool
}

// CacheTTL is how long an organization's flags are kept before the table is read again; a change made by this process
// is seen at once, one made by another replica within this time.
const CacheTTL = 5 * time.Second

type entry struct {
	rows map[string]bool
	at   time.Time
}

// Store is the Gate over the org_features table.
type Store struct {
	db    *store.DB
	conf  config.Features
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]entry
}

// New makes the gate; now is the clock, nil means time.Now.
func New(db *store.DB, conf config.Features, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, conf: conf, now: now, cache: map[string]entry{}}
}

// rows are the organization's choices, from the cache while it is fresh. ok is false when the table could not be read.
func (s *Store) rows(ctx context.Context, orgID string) (map[string]bool, bool) {
	now := s.now()
	s.mu.Lock()
	e, hit := s.cache[orgID]
	s.mu.Unlock()
	if hit && now.Sub(e.at) < CacheTTL && now.After(e.at.Add(-CacheTTL)) {
		return e.rows, true
	}
	fs, err := s.db.S().OrgFeatures(ctx, orgID)
	if err != nil {
		return nil, false
	}
	m := make(map[string]bool, len(fs))
	for _, f := range fs {
		m[f.Feature] = f.Enabled
	}
	s.mu.Lock()
	s.cache[orgID] = entry{rows: m, at: now}
	s.mu.Unlock()
	return m, true
}

// decide is a flag's state from the instance's lists and the organization's choices.
func decide(conf config.Features, rows map[string]bool, name string) bool {
	if on, ok := conf.Force[name]; ok {
		return on
	}
	if on, ok := rows[name]; ok {
		return on
	}
	return conf.Default[name]
}

// On is whether the flag is on for the organization. A table that cannot be read leaves only the instance's say.
func (s *Store) On(ctx context.Context, orgID, name string) bool {
	rows, _ := s.rows(ctx, orgID)
	return decide(s.conf, rows, name)
}

// List are the flags that are on for the organization, in the order of Known.
func (s *Store) List(ctx context.Context, orgID string) []string {
	rows, _ := s.rows(ctx, orgID)
	out := []string{}
	for _, n := range Known {
		if decide(s.conf, rows, n) {
			out = append(out, n)
		}
	}
	return out
}

// Instance are the flags that are on for an organization that chose nothing: what the instance forces or defaults on.
func (s *Store) Instance() []string {
	out := []string{}
	for _, n := range Known {
		if decide(s.conf, nil, n) {
			out = append(out, n)
		}
	}
	return out
}

// Forced says that the instance decides the flag, so an organization's choice changes nothing.
func (s *Store) Forced(name string) bool {
	_, ok := s.conf.Force[name]
	return ok
}

// Set keeps the organization's choice and drops its cached flags.
func (s *Store) Set(ctx context.Context, orgID, name string, enabled bool, by string) error {
	if err := s.db.S().SetOrgFeature(ctx, orgID, name, enabled, by, s.now()); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, orgID)
	s.mu.Unlock()
	return nil
}

// Static is a Gate with fixed flags, for tests of other packages: Static{"org": {"env": true}}.
type Static map[string]map[string]bool

// On implements Gate.
func (g Static) On(_ context.Context, orgID, name string) bool { return g[orgID][name] }

// AllOn is a Gate with every flag on.
type AllOn struct{}

// On implements Gate.
func (AllOn) On(context.Context, string, string) bool { return true }
