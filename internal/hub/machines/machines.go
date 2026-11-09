// Package machines is the hub's registry of machines: registration codes and the registration of an agent, what the
// hub knows about each machine (hello, preflight, agents, policy, proxy), the views the API sends, and opening a
// machine's runtime.
package machines

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/machaccess"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/spaces"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/ticket"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Deps are what the service is built from.
type Deps struct {
	Config  *config.Config
	DB      *store.DB
	Auth    *auth.Service
	Spaces  *spaces.Service
	Reg     *link.Registry
	Tickets *ticket.Store
	Limiter *ratelimit.Limiter
	Log     *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// OnRevoked runs after a machine was revoked: whatever is held for it must go.
	OnRevoked func(machineID string)
	// Usage pulls the machines' spend facts (HUB_USAGE_INGEST); nil: the hub neither subscribes to usage.available nor
	// reads any outbox.
	Usage UsageSink
	// Budget sends the machines their limit lists (HUB_LIMITS_ENFORCE); nil: off.
	Budget BudgetSink
	// Env sends the machines their environment lists and keeps what they report (internal/hub/env); nil: off.
	Env EnvSink
	// Design keeps the machines' design access (internal/hub/designaccess); nil: off.
	Design DesignSink
	// Docs answers the machines' doc.publish and doc.acl (internal/hub/docs); nil: nobody answers.
	Docs DocsSink
	// History keeps the machines' chats (internal/hub/history); nil: the hub takes none.
	History HistorySink
	// Provider runs the machines' virtual servers (docs/hub/provider.md); nil means provider.None.
	Provider provider.Provider
	// Publisher carries machine.op and machine.status to the organization's live channel; nil: they go to the space's
	// own stream (the broker of Spaces).
	Publisher Publisher
	// Features says whether an organization has a feature on. nil: every feature is off, which keeps the resources,
	// load and operations of a machine dark (docs/hub/machines-resources.md).
	Features FeatureGate
}

// EnvSink keeps a machine's environment list current (internal/hub/env).
type EnvSink interface {
	Connected(machineID string, hello hubproto.Hello)
	Reported(machineID string, st hubproto.EnvState)
	// Run keeps a run of the machine's setup (the env.run event).
	Run(machineID string, run hubproto.EnvRun)
}

// HistorySink keeps the chats a machine's agent sends (internal/hub/history).
type HistorySink interface {
	Connected(m store.Machine, features []string)
	Live(ctx context.Context, m store.Machine, raw json.RawMessage)
	Backfill(ctx context.Context, m store.Machine, raw json.RawMessage)
}

// DocsSink answers the document events of a machine's agent.
type DocsSink interface {
	Event(l *link.Link, m store.Machine, ev link.Event)
}

// DesignSink is what keeps a machine's design access (internal/hub/designaccess).
type DesignSink interface {
	Connected(m store.Machine, features []string)
	Event(m store.Machine, e hubproto.DesignEvent)
}

// BudgetSink is what keeps a machine's limit list current (internal/hub/limits).
type BudgetSink interface {
	Connected(machineID string, hello hubproto.Hello)
}

// UsageSink is what reads a machine's spend outbox (internal/hub/ingest).
type UsageSink interface {
	Connected(machineID string, features []string)
	Trigger(machineID string)
}

// Service is the machine side of the hub.
type Service struct {
	cfg     *config.Config
	db      *store.DB
	auth    *auth.Service
	spaces  *spaces.Service
	reg     *link.Registry
	events  *events.Broker
	tickets *ticket.Store
	lim     *ratelimit.Limiter
	log     *slog.Logger
	now     func() time.Time
	revoked func(machineID string)
	usage   UsageSink
	budget  BudgetSink
	env     EnvSink
	design  DesignSink
	docs    DocsSink
	history HistorySink
	prov    provider.Provider
	pub     Publisher
	gate    FeatureGate
	acc     *machaccess.Checker

	// res is the state of the resources and operations side (resources.go).
	res resState

	// infoMu serialises the read-modify-write of a machine's info document: the connect routine and the agent's events
	// all update it.
	infoMu sync.Mutex
	// localMu serialises renewing the local registration code.
	localMu sync.Mutex
}

// New makes the service.
func New(d Deps) *Service {
	s := &Service{cfg: d.Config, db: d.DB, auth: d.Auth, spaces: d.Spaces, reg: d.Reg, events: d.Spaces.Events(), tickets: d.Tickets, lim: d.Limiter, log: d.Log, now: d.Now, revoked: d.OnRevoked, usage: d.Usage, budget: d.Budget, env: d.Env, design: d.Design, docs: d.Docs, history: d.History, prov: d.Provider, pub: d.Publisher, gate: d.Features}
	s.res.init()
	if s.now == nil {
		s.now = time.Now
	}
	if s.prov == nil {
		s.prov = provider.None{}
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.lim == nil {
		s.lim = ratelimit.New()
	}
	if s.tickets == nil {
		s.tickets = ticket.New(s.now)
	}
	s.acc = &machaccess.Checker{Config: s.cfg, DB: s.db, Authz: s.spaces.Authz, IsAdmin: s.auth.IsInstanceAdmin, Log: s.log}
	// Live events of a machine reach a stream only while its person may still see the machine (HUB_ACCESS_ENFORCE=on).
	s.events.SetMachineGate(func(ctx context.Context, userID, machineID string) bool {
		if !s.acc.Hides() {
			return true
		}
		m, err := s.db.S().MachineByID(ctx, machineID)
		if err != nil {
			return store.IsNotFound(err) // a gone machine has no metadata left to leak
		}
		return s.acc.Visible(ctx, userID, m)
	})
	return s
}

// Mount registers the endpoints. The ones under a space go on the spaces router (see spaces.Service.Mount).
func (s *Service) Mount(mux *http.ServeMux) {
	sub := s.spaces.Sub()
	sub.Handle("GET /api/spaces/{spaceId}/machines", s.auth.Require(s.list))
	sub.Handle("POST /api/spaces/{spaceId}/machines/codes", s.auth.Require(s.createCode))
	sub.Handle("POST /api/spaces/{spaceId}/machines/local", s.auth.Require(s.claimLocal))

	mux.Handle("GET /api/machines/{machineId}", s.auth.Require(s.get))
	mux.Handle("PATCH /api/machines/{machineId}", s.auth.Require(s.rename))
	mux.Handle("DELETE /api/machines/{machineId}", s.auth.Require(s.remove))
	mux.Handle("GET /api/machines/{machineId}/open", s.auth.Require(s.open))
	mux.Handle("POST /api/machines/{machineId}/trust", s.auth.Require(s.trust))
	mux.Handle("DELETE /api/machines/{machineId}/trust", s.auth.Require(s.trust))
	mux.Handle("POST /api/machines/{machineId}/preflight", s.auth.Require(s.rerunPreflight))

	s.mountOrg(mux)
	s.mountProvision(mux)

	mux.Handle("POST /agent/register", httpx.Handler(s.register))
}

// Authorize finds a machine for a person and checks they may act on it: any member for reading and opening, an owner
// of its space for changing. A machine the person cannot see, and an id that is no machine, are the same forbidden.
func (s *Service) Authorize(ctx context.Context, userID, machineID string, owner bool) (store.Machine, error) {
	m, err := s.db.S().MachineByID(ctx, machineID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Machine{}, httpx.ErrForbidden
	}
	if err != nil {
		return store.Machine{}, err
	}
	if m.SpaceID == "" {
		return store.Machine{}, httpx.ErrForbidden
	}
	if owner {
		err = s.spaces.Owner(ctx, userID, m.SpaceID)
	} else {
		_, err = s.spaces.Access(ctx, userID, m.SpaceID)
	}
	if err != nil {
		return store.Machine{}, err
	}
	if m.RevokedAt != nil {
		return store.Machine{}, httpx.ErrNotFound
	}
	return m, nil
}

// Visible says whether the person may see the machine in an inventory: the access list under HUB_ACCESS_ENFORCE=on.
func (s *Service) Visible(ctx context.Context, userID string, m store.Machine) bool {
	return s.acc.Visible(ctx, userID, m)
}

// View is a machine as the API sends it.
type View struct {
	ID           string            `json:"id"`
	SpaceID      *string           `json:"spaceId"`
	Name         string            `json:"name"`
	Hostname     string            `json:"hostname"`
	OS           string            `json:"os"`
	Arch         string            `json:"arch"`
	UnixUser     string            `json:"unixUser"`
	Profile      string            `json:"profile"`
	Via          string            `json:"via"`
	Status       string            `json:"status"`
	ConnectedAt  *string           `json:"connectedAt"`
	LastSeenAt   *string           `json:"lastSeenAt"`
	AgentVersion string            `json:"agentVersion"`
	EncKey       string            `json:"encKey"`
	Policy       json.RawMessage   `json:"policy"`
	Preflight    json.RawMessage   `json:"preflight"`
	Agents       []json.RawMessage `json:"agents"`
	Proxy        json.RawMessage   `json:"proxy"`
	Env          []string          `json:"env"`
	CreatedAt    string            `json:"createdAt"`
	// Runtime is whether the machine's page opens on this hub: ready, needs_trust (an owner of its space can allow it)
	// or blocked (docs/hub/api.md, «Trusting a machine»).
	Runtime string `json:"runtime"`
}

// info is what the hub remembers of a machine's last reports, in the form the API sends them.
type info struct {
	Policy    json.RawMessage `json:"policy,omitempty"`
	Preflight json.RawMessage `json:"preflight,omitempty"`
	Agents    json.RawMessage `json:"agents,omitempty"`
	Proxy     json.RawMessage `json:"proxy,omitempty"`
}

// opensRuntime says whether the hub may open the machine's runtime: not when the machine's last policy refused runtime
// sessions.
func opensRuntime(m store.Machine) bool {
	// An agent older than the field sent no word on it, and it opened then.
	var in struct {
		Policy struct {
			RuntimeSession *bool `json:"runtimeSession"`
		} `json:"policy"`
	}
	if err := json.Unmarshal([]byte(m.Info), &in); err != nil || in.Policy.RuntimeSession == nil {
		return true
	}
	return *in.Policy.RuntimeSession
}

var emptyProxy = json.RawMessage(`{"all":null,"claude":null,"codex":null}`)

// Status of a machine: online with a live link, pending when it never connected, else offline.
func (s *Service) status(m store.Machine) string {
	switch {
	case s.reg.Get(m.ID) != nil:
		return "online"
	case m.LastSeenAt == nil:
		return "pending"
	default:
		return "offline"
	}
}

// View builds the API form of a machine.
func (s *Service) View(ctx context.Context, m store.Machine) (View, error) {
	env, err := s.db.S().EnvNames(ctx, m.ID)
	if err != nil {
		return View{}, err
	}
	var in info
	_ = json.Unmarshal([]byte(m.Info), &in)
	v := View{
		ID: m.ID, Name: m.Name, Hostname: m.Hostname, OS: m.OS, Arch: m.Arch, UnixUser: m.UnixUser, Profile: m.Profile, Via: m.Via,
		Status: s.status(m), LastSeenAt: httpx.TimePtr(m.LastSeenAt), AgentVersion: m.AgentVersion, EncKey: m.EncPub,
		Policy: nullable(in.Policy), Preflight: nullable(in.Preflight), Proxy: in.Proxy, Env: env, CreatedAt: httpx.Time(m.CreatedAt),
		Agents: agentsView(in.Agents),
	}
	if m.SpaceID != "" {
		id := m.SpaceID
		v.SpaceID = &id
	}
	if v.Runtime, err = s.runtimeState(ctx, m); err != nil {
		return View{}, err
	}
	if len(v.Proxy) == 0 {
		v.Proxy = emptyProxy
	}
	if l := s.reg.Get(m.ID); l != nil {
		t := httpx.Time(l.ConnectedAt)
		v.ConnectedAt = &t
	}
	return v, nil
}

func nullable(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage("null")
	}
	return r
}

// agentsView lists claude, codex and gemini: what the agent reported, a blank for what it did not, and gemini as a
// card that says "soon".
func agentsView(stored json.RawMessage) []json.RawMessage {
	byProvider := map[string]json.RawMessage{}
	var list []json.RawMessage
	if json.Unmarshal(stored, &list) == nil {
		for _, raw := range list {
			var p struct{ Provider string }
			if json.Unmarshal(raw, &p) == nil {
				byProvider[p.Provider] = raw
			}
		}
	}
	out := make([]json.RawMessage, 0, 3)
	for _, p := range []string{"claude", "codex"} {
		if raw, ok := byProvider[p]; ok {
			out = append(out, raw)
			continue
		}
		out = append(out, BlankAgent(p, false))
	}
	return append(out, BlankAgent("gemini", true))
}

// BlankAgent is the status of an agent nobody has reported on. "soon" is on the gemini card only.
func BlankAgent(provider string, soon bool) json.RawMessage {
	m := map[string]any{
		"provider": provider, "installed": false, "version": nil, "login": "unknown",
		"authMode": nil, "overriddenBy": nil, "access": nil,
	}
	if soon {
		m["soon"] = true
	}
	b, _ := json.Marshal(m)
	return b
}

// Publish tells the space's streams about a change of a machine. A machine nobody has claimed has no space to tell.
func (s *Service) Publish(ctx context.Context, m store.Machine) {
	if m.SpaceID == "" {
		return
	}
	fresh, err := s.db.S().MachineByID(ctx, m.ID)
	if err != nil {
		return
	}
	v, err := s.View(ctx, fresh)
	if err != nil {
		s.log.Warn("machine view not built", "machine", m.ID, "error", err)
		return
	}
	s.events.PublishMachine(fresh.SpaceID, fresh.ID, "machine", map[string]any{"machine": v}, false)
}

// PublishJob tells the owners of the machine's space about a step of an install.
func (s *Service) PublishJob(m store.Machine, job json.RawMessage) {
	if m.SpaceID == "" {
		return
	}
	s.events.PublishMachine(m.SpaceID, m.ID, "job", map[string]any{"machineId": m.ID, "job": job}, true)
}

// PublishLogin tells the owners about a change of an agent login.
func (s *Service) PublishLogin(m store.Machine, login json.RawMessage) {
	if m.SpaceID == "" {
		return
	}
	s.events.PublishMachine(m.SpaceID, m.ID, "login", map[string]any{"machineId": m.ID, "login": login}, true)
}

func (s *Service) list(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	spaceID := r.PathValue("spaceId")
	if _, err := s.spaces.Access(ctx, p.User.ID, spaceID); err != nil {
		return err
	}
	ms, err := s.db.S().MachinesBySpace(ctx, spaceID)
	if err != nil {
		return err
	}
	out := make([]View, 0, len(ms))
	for _, m := range ms {
		if !s.Visible(ctx, p.User.ID, m) {
			continue
		}
		v, err := s.View(ctx, m)
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machines": out})
	return nil
}

func (s *Service) get(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.Authorize(r.Context(), p.User.ID, r.PathValue("machineId"), false)
	if err != nil {
		return err
	}
	if !s.Visible(r.Context(), p.User.ID, m) {
		return httpx.ErrNotFound // the same answer as for a machine that does not exist
	}
	v, err := s.View(r.Context(), m)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machine": v})
	return nil
}

func (s *Service) rename(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req patchRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	// The team and the access list are the organization's (patchOrg); a name alone is what it always was.
	if req.TeamID != nil || req.Access != nil {
		return s.patchOrg(w, r, p, req)
	}
	var raw string
	if req.Name != nil {
		raw = *req.Name
	}
	name, err := cleanMachineName(raw)
	if err != nil {
		return err
	}
	ctx := r.Context()
	m, err := s.Authorize(ctx, p.User.ID, r.PathValue("machineId"), true)
	if err != nil {
		return err
	}
	if err := s.db.S().RenameMachine(ctx, m.ID, name); err != nil {
		return err
	}
	m.Name = name
	v, err := s.View(ctx, m)
	if err != nil {
		return err
	}
	s.Publish(ctx, m)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machine": v})
	return nil
}

func (s *Service) remove(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.Authorize(r.Context(), p.User.ID, r.PathValue("machineId"), true)
	if err != nil {
		return err
	}
	if err := s.revokeMachine(r.Context(), m); err != nil {
		return err
	}
	s.auth.Audit(r.Context(), p.User.ID, "machine_revoked", r, m.ID)
	httpx.NoContent(w)
	return nil
}

// unlinked is a machine removing itself (Hooks.Unlink): its signature was checked, and a machine that was removed
// already was refused before this.
func (s *Service) unlinked(r *http.Request, m store.Machine) error {
	if err := s.revokeMachine(r.Context(), m); err != nil {
		return err
	}
	if m.CreatedBy != "" {
		s.auth.Audit(r.Context(), m.CreatedBy, "machine_unlinked", r, m.ID)
	}
	return nil
}

// revokeMachine takes a machine away: its key stops working, its link closes, and the space is told.
func (s *Service) revokeMachine(ctx context.Context, m store.Machine) error {
	if _, err := s.db.S().RevokeMachine(ctx, m.ID, s.now().UTC()); err != nil {
		return err
	}
	s.reg.Revoke(m.ID)
	if s.revoked != nil {
		s.revoked(m.ID)
	}
	if m.SpaceID != "" {
		s.events.PublishJSON(m.SpaceID, "machineRemoved", map[string]string{"machineId": m.ID}, false)
	}
	return nil
}

// validRuntimePath says whether p is a path inside the machine's runtime: /m/<id> or /m/<id>/…, with an optional
// query, on this site and nowhere else.
func validRuntimePath(p, machineID string) bool {
	base := "/m/" + machineID
	if p != base && !strings.HasPrefix(p, base+"/") && !strings.HasPrefix(p, base+"?") {
		return false
	}
	if auth.SafeNext(p) == "" || strings.Contains(p, "#") {
		return false
	}
	rest := strings.TrimPrefix(p, base)
	return !strings.HasPrefix(rest, "//")
}

func (s *Service) open(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	m, err := s.Authorize(ctx, p.User.ID, r.PathValue("machineId"), false)
	if err != nil {
		return err
	}
	if ok, retry := s.lim.Allow("open:"+p.User.ID, 120, time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	if m.DisabledAt != nil {
		return errDisabledMachine
	}
	// The machine's access list (HUB_ACCESS_ENFORCE): the same rule the proxy applies to every request after this one.
	if err := s.acc.Check(ctx, p.User.ID, m); err != nil {
		return err
	}
	// Without HUB_RUNTIME_DOMAIN the page would run on the hub's origin: only a machine an administrator connected may.
	if err := s.auth.RequireTrustedMachine(ctx, m); err != nil {
		return err
	}
	if !opensRuntime(m) {
		return errObserveOnly
	}
	path := "/m/" + m.ID + "/"
	if asked := r.URL.Query().Get("path"); asked != "" {
		// The UI opens a particular tab of the runtime (a login console) by its address under the base path.
		if !validRuntimePath(asked, m.ID) {
			return httpx.Invalid("path", "Адрес должен быть внутри машины")
		}
		path = asked
	}
	target := path
	if s.cfg.SubdomainMode() {
		u, err := url.Parse(path)
		if err != nil {
			return httpx.Invalid("path", "Некорректный адрес")
		}
		pub, _ := url.Parse(s.cfg.AgentURL)
		u.Scheme, u.Host = pub.Scheme, s.cfg.RuntimeHost(m.ID)
		q := u.Query()
		q.Set("hl_ticket", s.tickets.Issue(p.User.ID, m.ID))
		u.RawQuery = q.Encode()
		target = u.String()
	}
	if r.URL.Query().Get("redirect") == "1" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusFound)
		return nil
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"url": target})
	return nil
}

func (s *Service) rerunPreflight(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	m, err := s.Authorize(ctx, p.User.ID, r.PathValue("machineId"), true)
	if err != nil {
		return err
	}
	report, err := s.RunPreflight(ctx, m)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"preflight": report})
	return nil
}
