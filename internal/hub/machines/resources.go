package machines

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The resources of a machine, its load and the operations on it (docs/hub/machines-resources.md). All of it is dark: an
// organization sees it only when its feature "machine.resources" is on (Deps.Features), and nothing here runs for one
// that has not.

// FeatureResources is the name of the organization feature that opens resources, load and operations of machines.
const FeatureResources = "machine.resources"

// Publisher carries live events to an organization's channel (internal/hub/events, "organization" streams).
type Publisher interface {
	Publish(orgID, kind string, data any)
}

// FeatureGate says whether an organization has a feature on.
type FeatureGate interface {
	On(ctx context.Context, orgID, name string) bool
}

// Live event kinds this side publishes.
const (
	EventMachineOp     = "machine.op"
	EventMachineStatus = "machine.status"
)

var (
	errNotEnabled   = httpx.New(http.StatusNotFound, "not_enabled", "Ресурсы машин не включены для этой организации")
	errNotSupported = httpx.New(http.StatusUnprocessableEntity, "not_supported", "У хаба нет провайдера, который управляет этой машиной")
	errNotManaged   = httpx.New(http.StatusConflict, "not_managed", "Хаб не управляет ресурсами этой машины: включите управление для машины").With("capability", store.CapResources)
	errDiskShrink   = httpx.New(http.StatusUnprocessableEntity, "disk_shrink", "Диск можно только увеличить").With("field", "diskGB")
	errNoCurrent    = httpx.New(http.StatusConflict, "current_unknown", "Текущий размер машины неизвестен, менять его нельзя")
	errNoChange     = httpx.New(http.StatusConflict, "no_change", "Машина уже такого размера")
	errDrainOld     = httpx.New(http.StatusUnprocessableEntity, "drain_unsupported", "Эта версия агента не умеет ждать чаты и закрывать приём новых: обновите агента")
	errNotCancel    = httpx.New(http.StatusConflict, "not_cancellable", "Эту операцию отменить нельзя")
	errOpFinished   = httpx.New(http.StatusConflict, "op_finished", "Операция уже закончилась")
)

// resState is what the resources side keeps in memory.
type resState struct {
	mu sync.Mutex
	// features are the features each connected agent said in hello.
	features map[string][]string
	// latest is the newest load reading of each connected machine.
	latest map[string]liveSample
	// waiters are the machine restarts that wait for the agent to finish draining before the server is rebooted.
	waiters map[string]bool
	// chats is how many chats a waiting restart still waits for, by operation id.
	chats map[string]int
	// enabled caches whether a space's organization has the feature on, for the busy paths (load samples).
	enabled map[string]cachedGate
	// pollEvery is the pause between two asks of the provider about an operation.
	pollEvery time.Duration
	lastPrune time.Time
	// opMu serialises the creation of operations, so that one machine has one unfinished operation.
	opMu sync.Mutex
}

func (r *resState) init() {
	r.features = map[string][]string{}
	r.latest = map[string]liveSample{}
	r.waiters = map[string]bool{}
	r.chats = map[string]int{}
	r.enabled = map[string]cachedGate{}
	r.pollEvery = 5 * time.Second
}

type cachedGate struct {
	on    bool
	until time.Time
}

// gateTTL is how long an answer of the feature gate is kept for the busy paths.
const gateTTL = 30 * time.Second

// onCached is on for the paths that run for every load sample.
func (s *Service) onCached(ctx context.Context, spaceID string) bool {
	if s.gate == nil || spaceID == "" {
		return false
	}
	now := s.now()
	s.res.mu.Lock()
	c, ok := s.res.enabled[spaceID]
	s.res.mu.Unlock()
	if ok && now.Before(c.until) {
		return c.on
	}
	on := s.on(ctx, spaceID)
	s.res.mu.Lock()
	s.res.enabled[spaceID] = cachedGate{on: on, until: now.Add(gateTTL)}
	s.res.mu.Unlock()
	return on
}

// SetPollEvery changes the pause between two asks of the provider about an operation (tests).
func (s *Service) SetPollEvery(d time.Duration) {
	s.res.mu.Lock()
	s.res.pollEvery = d
	s.res.mu.Unlock()
}

// on says whether the space's organization has the resources feature on.
func (s *Service) on(ctx context.Context, spaceID string) bool {
	if s.gate == nil || spaceID == "" {
		return false
	}
	sp, err := s.db.S().SpaceByID(ctx, spaceID)
	if err != nil {
		return false
	}
	return s.gate.On(ctx, store.OrgKey(sp), FeatureResources)
}

// Enabled reports whether the resources feature is wired at all; without it the load is not even subscribed to.
func (s *Service) Enabled() bool { return s.gate != nil }

func (s *Service) mountResources(mux *http.ServeMux) {
	mux.Handle("GET /api/machines/{machineId}/resources", s.machineHandler(false, s.getResources))
	mux.Handle("POST /api/machines/{machineId}/resources/quote", s.machineHandler(true, s.quote))
	mux.Handle("POST /api/machines/{machineId}/resize", s.machineHandler(true, s.resize))
	mux.Handle("PUT /api/machines/{machineId}/managed", s.machineHandler(true, s.setManaged))
	mux.Handle("GET /api/machines/{machineId}/ops", s.machineHandler(false, s.listOps))
	mux.Handle("POST /api/machines/{machineId}/ops/{opId}/cancel", s.machineHandler(true, s.cancelOp))
	mux.Handle("GET /api/machines/{machineId}/metrics", s.machineHandler(false, s.metrics))
}

type machineFunc func(w http.ResponseWriter, r *http.Request, p *auth.Principal, m store.Machine) error

// machineHandler wraps an endpoint of one machine: the session and HUB_ORGS, the machine, who may read or change it, and
// the feature. A write needs the manager of the machine's tenant, a read anyone who may view the machine's scope.
func (s *Service) machineHandler(write bool, h machineFunc) httpx.Handler {
	return s.orgHandler(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		ctx := r.Context()
		st := s.db.S()
		m, err := machineOf(ctx, st, r.PathValue("machineId"))
		if err != nil {
			return err
		}
		if write {
			err = manage(ctx, st, p, m)
		} else {
			err = viewMachine(ctx, st, p, m)
		}
		if err != nil {
			return err
		}
		// A reader is also on the machine's access list, as for the machine itself (tenant.Viewer).
		if !write && !s.Visible(ctx, p.User.ID, m) {
			return httpx.ErrNotFound
		}
		if !s.on(ctx, m.SpaceID) {
			return errNotEnabled
		}
		return h(w, r, p, m)
	})
}

// viewMachine checks the person may see the machine: the tenant's view, or its team's.
func viewMachine(ctx context.Context, st store.Store, p *auth.Principal, m store.Machine) error {
	a := authz.New(st)
	_, err := a.Can(ctx, orgs.Principal(p), authz.Check{Action: authz.View, Scope: tenantOf(m)})
	if err == nil || m.TeamID == "" {
		return err
	}
	if _, terr := a.Can(ctx, orgs.Principal(p), authz.Check{Action: authz.View, Scope: authz.Scope{Kind: store.ScopeTeam, ID: m.TeamID}}); terr == nil {
		return nil
	}
	return err
}

// ---- views ----

type sizeView struct {
	CPU    *int `json:"cpu"`
	RAMGB  *int `json:"ramGB"`
	DiskGB *int `json:"diskGB"`
}

type priceView struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Period   string `json:"period"`
}

type planView struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	CPU     int        `json:"cpu"`
	RAMGB   int        `json:"ramGB"`
	DiskGB  int        `json:"diskGB"`
	Price   *priceView `json:"price"`
	Current bool       `json:"current"`
}

type HardwareView struct {
	Cores     *int    `json:"cores"`
	RAMBytes  *int64  `json:"ramBytes"`
	DiskBytes *int64  `json:"diskBytes"`
	Model     *string `json:"model"`
	Laptop    bool    `json:"laptop"`
	OS        *string `json:"os"`
	Arch      *string `json:"arch"`
	UpdatedAt string  `json:"updatedAt"`
}

type providerView struct {
	Name      string `json:"name"`
	Supported bool   `json:"supported"`
}

// OpView is one operation as the API sends it.
type OpView struct {
	ID         string          `json:"id"`
	MachineID  string          `json:"machineId"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Params     json.RawMessage `json:"params"`
	Error      *string         `json:"error"`
	CreatedBy  *string         `json:"createdBy"`
	CreatedAt  string          `json:"createdAt"`
	UpdatedAt  string          `json:"updatedAt"`
	FinishedAt *string         `json:"finishedAt"`
	// ChatsNow is how many chats a restart that waits for them still waits for; null when unknown or not waiting.
	ChatsNow *int `json:"chatsNow"`
}

// opView is the operation as the API sends it, with the chats a waiting restart still waits for.
func (s *Service) opView(o store.MachineOp) OpView {
	v := OpView{ID: o.ID, MachineID: o.MachineID, Kind: o.Kind, Status: o.Status, Params: json.RawMessage(o.Params),
		CreatedAt: httpx.Time(o.CreatedAt), UpdatedAt: httpx.Time(o.UpdatedAt), FinishedAt: httpx.TimePtr(o.FinishedAt)}
	if !json.Valid(v.Params) {
		v.Params = json.RawMessage(`{}`)
	}
	if o.Error != "" {
		e := o.Error
		v.Error = &e
	}
	if o.CreatedBy != "" {
		c := o.CreatedBy
		v.CreatedBy = &c
	}
	if o.Active() {
		s.res.mu.Lock()
		if n, ok := s.res.chats[o.ID]; ok {
			v.ChatsNow = &n
		}
		s.res.mu.Unlock()
	}
	return v
}

func (s *Service) opViews(ops []store.MachineOp) []OpView {
	out := make([]OpView, 0, len(ops))
	for _, o := range ops {
		out = append(out, s.opView(o))
	}
	return out
}

func priceOf(p provider.Price) *priceView {
	return &priceView{Amount: p.Amount, Currency: p.Currency, Period: p.Period}
}

// ResourcesResponse is GET /api/machines/{id}/resources.
type ResourcesResponse struct {
	MachineID string       `json:"machineId"`
	Managed   bool         `json:"managed"`
	Provider  providerView `json:"provider"`
	// ProviderError is set when the provider is there but did not answer; the numbers below are then what the hub has.
	ProviderError *string       `json:"providerError"`
	Current       *sizeView     `json:"current"`
	Price         *priceView    `json:"price"`
	Plans         []planView    `json:"plans"`
	Hardware      *HardwareView `json:"hardware"`
	ConsoleURL    *string       `json:"consoleUrl"`
	ActiveOps     []OpView      `json:"activeOps"`
	UpdatedAt     *string       `json:"updatedAt"`
}

func (s *Service) pmachine(ctx context.Context, m store.Machine) provider.Machine {
	pm := provider.Machine{ID: m.ID, Name: m.Name}
	if r, err := s.db.S().ResourcesByMachine(ctx, m.ID); err == nil {
		pm.ExternalID = r.ExternalID
	}
	return pm
}

// mapProviderErr turns what the provider answered into an API error.
func mapProviderErr(err error) error {
	var pe *provider.Error
	switch {
	case errors.Is(err, provider.ErrUnsupported):
		return errNotSupported
	case errors.Is(err, provider.ErrNotFound):
		return httpx.New(http.StatusNotFound, "provider_not_found", "Провайдер не знает эту машину")
	case errors.Is(err, context.DeadlineExceeded):
		return httpx.New(http.StatusGatewayTimeout, "provider_timeout", "Провайдер не ответил вовремя")
	case errors.As(err, &pe):
		e := httpx.New(http.StatusBadGateway, "provider_error", "Провайдер не выполнил запрос")
		if pe.Code != "" {
			e = e.With("providerCode", pe.Code)
		}
		if pe.Message != "" {
			e = e.With("providerMessage", pe.Message)
		}
		return e
	}
	return httpx.New(http.StatusBadGateway, "provider_error", "Провайдер недоступен")
}

const providerTimeout = 15 * time.Second

func (s *Service) getResources(w http.ResponseWriter, r *http.Request, _ *auth.Principal, m store.Machine) error {
	ctx := r.Context()
	st := s.db.S()
	out := ResourcesResponse{MachineID: m.ID, Provider: providerView{Name: s.prov.Name(), Supported: provider.Supported(s.prov)}, Plans: []planView{}}
	managed, err := st.ManagedByMachine(ctx, m.ID)
	if err != nil {
		return err
	}
	out.Managed = managed[store.CapResources].Managed
	if out.Provider.Supported {
		pctx, cancel := context.WithTimeout(ctx, providerTimeout)
		plans, perr := s.prov.Plans(pctx, s.pmachine(ctx, m))
		cancel()
		if perr != nil {
			msg := httpErrCode(mapProviderErr(perr))
			out.ProviderError = &msg
		}
		for _, p := range plans {
			v := planView{ID: p.ID, Name: p.Name, CPU: p.CPU, RAMGB: p.RAMGB, DiskGB: p.DiskGB, Current: p.Current}
			if p.Price != nil {
				v.Price = priceOf(*p.Price)
			}
			out.Plans = append(out.Plans, v)
			if p.Current {
				s.rememberPlan(ctx, m, p)
			}
		}
		cctx, ccancel := context.WithTimeout(ctx, providerTimeout)
		if u, cerr := s.prov.ConsoleURL(cctx, s.pmachine(ctx, m)); cerr == nil && u != "" {
			out.ConsoleURL = &u
		}
		ccancel()
	}
	if res, err := st.ResourcesByMachine(ctx, m.ID); err == nil {
		if res.CPU != nil || res.RAMGB != nil || res.DiskGB != nil {
			out.Current = &sizeView{CPU: res.CPU, RAMGB: res.RAMGB, DiskGB: res.DiskGB}
		}
		if res.PriceAmount != nil && res.PriceCurrency != "" {
			out.Price = &priceView{Amount: *res.PriceAmount, Currency: res.PriceCurrency, Period: res.PricePeriod}
		}
		t := httpx.Time(res.UpdatedAt)
		out.UpdatedAt = &t
	} else if !store.IsNotFound(err) {
		return err
	}
	hw, err := st.HWByMachines(ctx, []string{m.ID})
	if err != nil {
		return err
	}
	if h, ok := hw[m.ID]; ok {
		out.Hardware = hwOf(h)
	}
	ops, err := st.OpsByMachine(ctx, m.ID, true, 20)
	if err != nil {
		return err
	}
	out.ActiveOps = s.opViews(ops)
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func hwOf(h store.MachineHW) *HardwareView {
	v := &HardwareView{Laptop: h.Laptop, UpdatedAt: httpx.Time(h.UpdatedAt)}
	if h.Cores > 0 {
		c := h.Cores
		v.Cores = &c
	}
	if h.RAMTotal > 0 {
		c := h.RAMTotal
		v.RAMBytes = &c
	}
	if h.DiskTotal > 0 {
		c := h.DiskTotal
		v.DiskBytes = &c
	}
	if h.Model != "" {
		c := h.Model
		v.Model = &c
	}
	if h.OS != "" {
		c := h.OS
		v.OS = &c
	}
	if h.Arch != "" {
		c := h.Arch
		v.Arch = &c
	}
	return v
}

// rememberPlan stores what the provider says the machine is on now: its size and its price.
func (s *Service) rememberPlan(ctx context.Context, m store.Machine, p provider.Plan) {
	st := s.db.S()
	cur, err := st.ResourcesByMachine(ctx, m.ID)
	if err != nil && !store.IsNotFound(err) {
		return
	}
	cpu, ram, disk := p.CPU, p.RAMGB, p.DiskGB
	cur.MachineID, cur.Provider, cur.UpdatedAt = m.ID, s.prov.Name(), s.now().UTC()
	cur.CPU, cur.RAMGB, cur.DiskGB = &cpu, &ram, &disk
	raw, _ := json.Marshal(planView{ID: p.ID, Name: p.Name, CPU: p.CPU, RAMGB: p.RAMGB, DiskGB: p.DiskGB, Current: true})
	cur.Plan = string(raw)
	cur.PriceAmount, cur.PriceCurrency, cur.PricePeriod = nil, "", ""
	if p.Price != nil {
		a := p.Price.Amount
		cur.PriceAmount, cur.PriceCurrency, cur.PricePeriod = &a, p.Price.Currency, p.Price.Period
	}
	if err := st.PutResources(ctx, cur); err != nil {
		s.log.Warn("resources not saved", "machine", m.ID, "error", err)
	}
}

// sizeRequest is a size asked for.
type sizeRequest struct {
	CPU    int `json:"cpu"`
	RAMGB  int `json:"ramGB"`
	DiskGB int `json:"diskGB"`
}

func (q sizeRequest) check() (provider.Resources, error) {
	switch {
	case q.CPU < 1 || q.CPU > 1024:
		return provider.Resources{}, httpx.Invalid("cpu", "cpu — от 1 до 1024")
	case q.RAMGB < 1 || q.RAMGB > 16384:
		return provider.Resources{}, httpx.Invalid("ramGB", "ramGB — от 1 до 16384")
	case q.DiskGB < 1 || q.DiskGB > 1<<20:
		return provider.Resources{}, httpx.Invalid("diskGB", "diskGB — от 1 до 1048576")
	}
	return provider.Resources{CPU: q.CPU, RAMGB: q.RAMGB, DiskGB: q.DiskGB}, nil
}

// QuoteResponse is POST /api/machines/{id}/resources/quote.
type QuoteResponse struct {
	MachineID string     `json:"machineId"`
	Want      sizeView   `json:"want"`
	Price     *priceView `json:"price"`
	// Current is the price of the size the machine is on, when the hub knows it.
	Current *priceView `json:"current"`
	// Delta is Price minus Current, set only when both are in one currency.
	Delta *priceView `json:"delta"`
}

func (s *Service) quote(w http.ResponseWriter, r *http.Request, _ *auth.Principal, m store.Machine) error {
	var req sizeRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	want, err := req.check()
	if err != nil {
		return err
	}
	if !provider.Supported(s.prov) {
		return errNotSupported
	}
	ctx, cancel := context.WithTimeout(r.Context(), providerTimeout)
	defer cancel()
	price, err := s.prov.Quote(ctx, s.pmachine(ctx, m), want)
	if err != nil {
		return mapProviderErr(err)
	}
	out := QuoteResponse{MachineID: m.ID, Want: sizeView{CPU: &want.CPU, RAMGB: &want.RAMGB, DiskGB: &want.DiskGB}, Price: priceOf(price)}
	if res, err := s.db.S().ResourcesByMachine(r.Context(), m.ID); err == nil && res.PriceAmount != nil && res.PriceCurrency != "" {
		out.Current = &priceView{Amount: *res.PriceAmount, Currency: res.PriceCurrency, Period: res.PricePeriod}
		if res.PriceCurrency == price.Currency && res.PricePeriod == price.Period {
			out.Delta = &priceView{Amount: price.Amount - *res.PriceAmount, Currency: price.Currency, Period: price.Period}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// requireManaged says the hub manages the machine's resources.
func (s *Service) requireManaged(ctx context.Context, m store.Machine) error {
	managed, err := s.db.S().ManagedByMachine(ctx, m.ID)
	if err != nil {
		return err
	}
	if !managed[store.CapResources].Managed {
		return errNotManaged
	}
	return nil
}

func (s *Service) resize(w http.ResponseWriter, r *http.Request, p *auth.Principal, m store.Machine) error {
	var req sizeRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	want, err := req.check()
	if err != nil {
		return err
	}
	ctx := r.Context()
	if !provider.Supported(s.prov) {
		return errNotSupported
	}
	if err := s.requireManaged(ctx, m); err != nil {
		return err
	}
	// What the machine is on now: the hub's record, refreshed from the provider's plans when it can be.
	pctx, cancel := context.WithTimeout(ctx, providerTimeout)
	if plans, perr := s.prov.Plans(pctx, s.pmachine(ctx, m)); perr == nil {
		for _, pl := range plans {
			if pl.Current {
				s.rememberPlan(ctx, m, pl)
			}
		}
	}
	cancel()
	cur, err := s.db.S().ResourcesByMachine(ctx, m.ID)
	if err != nil && !store.IsNotFound(err) {
		return err
	}
	if cur.CPU == nil || cur.RAMGB == nil || cur.DiskGB == nil {
		return errNoCurrent
	}
	if want.DiskGB < *cur.DiskGB {
		return errDiskShrink.With("current", *cur.DiskGB).With("wanted", want.DiskGB)
	}
	if want.CPU == *cur.CPU && want.RAMGB == *cur.RAMGB && want.DiskGB == *cur.DiskGB {
		return errNoChange
	}
	op, err := s.providerOp(ctx, m, store.OpResize, map[string]any{"cpu": want.CPU, "ramGB": want.RAMGB, "diskGB": want.DiskGB}, p.User.ID,
		func(ctx context.Context, pm provider.Machine) (provider.OpRef, error) {
			return s.prov.Resize(ctx, pm, want)
		},
		func(ctx context.Context) { s.afterResize(ctx, m, want) })
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_resize", r, m.ID)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"op": s.opView(op)})
	return nil
}

func (s *Service) listOps(w http.ResponseWriter, r *http.Request, _ *auth.Principal, m store.Machine) error {
	active := false
	switch r.URL.Query().Get("active") {
	case "", "0":
	case "1":
		active = true
	default:
		return httpx.Invalid("active", "active — 1 или не указывать")
	}
	ops, err := s.db.S().OpsByMachine(r.Context(), m.ID, active, 50)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machineId": m.ID, "ops": s.opViews(ops)})
	return nil
}

func (s *Service) cancelOp(w http.ResponseWriter, r *http.Request, p *auth.Principal, m store.Machine) error {
	ctx := r.Context()
	st := s.db.S()
	op, err := st.OpByID(ctx, r.PathValue("opId"))
	if store.IsNotFound(err) || (err == nil && op.MachineID != m.ID) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !op.Active() {
		return errOpFinished
	}
	// Only a restart that still waits on the machine can be taken back; what the provider has begun cannot.
	waiting := op.Kind == store.OpDrain || op.Kind == store.OpRestartRunner || (op.Kind == store.OpRestartMachine && op.ProviderOpID == "")
	if !waiting {
		return errNotCancel
	}
	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpRuntimeRestartCancel, map[string]any{"jobId": op.ID}, callTimeout); err != nil {
		var he *httpx.Error
		if errors.As(err, &he) && he.Code == "provider_off" {
			return errDrainOld
		}
		return err
	}
	s.finishOp(ctx, m, op.ID, store.OpCancelled, "")
	s.auth.Audit(ctx, p.User.ID, "machine_op_cancel", r, m.ID+" "+op.ID)
	fresh, err := st.OpByID(ctx, op.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"op": s.opView(fresh)})
	return nil
}

// ManagedView is what the API sends of the capabilities a machine is managed for.
type ManagedView struct {
	Budget    bool `json:"budget"`
	Env       bool `json:"env"`
	Resources bool `json:"resources"`
	Access    bool `json:"access"`
}

func managedView(m map[string]store.MachineManaged) ManagedView {
	return ManagedView{Budget: m[store.CapBudget].Managed, Env: m[store.CapEnv].Managed, Resources: m[store.CapResources].Managed, Access: m[store.CapAccess].Managed}
}

func (s *Service) setManaged(w http.ResponseWriter, r *http.Request, p *auth.Principal, m store.Machine) error {
	var req struct {
		Capability string `json:"capability"`
		Managed    *bool  `json:"managed"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !oneOf(req.Capability, store.CapBudget, store.CapEnv, store.CapResources, store.CapAccess) {
		return httpx.Invalid("capability", "capability — budget, env, resources или access")
	}
	if req.Managed == nil {
		return httpx.Invalid("managed", "managed — true или false")
	}
	ctx := r.Context()
	err := s.db.Tx(ctx, func(st store.Store) error {
		cur, err := st.ManagedByMachine(ctx, m.ID)
		if err != nil {
			return err
		}
		if cur[req.Capability].Managed == *req.Managed {
			return nil
		}
		if err := st.SetManaged(ctx, m.ID, req.Capability, *req.Managed, p.User.ID, s.now().UTC()); err != nil {
			return err
		}
		return s.journalBy(ctx, st, p.User.ID, m, "machine.managed",
			map[string]any{"capability": req.Capability, "managed": !*req.Managed}, map[string]any{"capability": req.Capability, "managed": *req.Managed})
	})
	if err != nil {
		return err
	}
	cur, err := s.db.S().ManagedByMachine(ctx, m.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machineId": m.ID, "managed": managedView(cur)})
	return nil
}

// ---- metrics ----

// metricRanges are the spans GET …/metrics takes.
var metricRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

type metricPoint struct {
	At       string  `json:"at"`
	CPUAvg   float64 `json:"cpuAvg"`
	CPUMax   float64 `json:"cpuMax"`
	RAMAvg   float64 `json:"ramAvg"`
	RAMMax   float64 `json:"ramMax"`
	DiskAvg  float64 `json:"diskAvg"`
	DiskMax  float64 `json:"diskMax"`
	ChatsMax int     `json:"chatsMax"`
	Samples  int     `json:"samples"`
}

// MetricsResponse is GET /api/machines/{id}/metrics.
type MetricsResponse struct {
	MachineID   string        `json:"machineId"`
	Range       string        `json:"range"`
	StepSeconds int           `json:"stepSeconds"`
	From        string        `json:"from"`
	To          string        `json:"to"`
	Points      []metricPoint `json:"points"`
}

func (s *Service) metrics(w http.ResponseWriter, r *http.Request, _ *auth.Principal, m store.Machine) error {
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	span, ok := metricRanges[rng]
	if !ok {
		return httpx.Invalid("range", "range — "+strings.Join([]string{"1h", "6h", "24h", "7d"}, ", "))
	}
	now := s.now()
	from := now.Add(-span)
	bs, err := s.db.S().MetricsRange(r.Context(), m.ID, store.BucketMs(from.UnixMilli()), now.UnixMilli()+1)
	if err != nil {
		return err
	}
	out := MetricsResponse{MachineID: m.ID, Range: rng, StepSeconds: int(store.MetricStep / time.Second), From: httpx.Time(from), To: httpx.Time(now), Points: make([]metricPoint, 0, len(bs))}
	for _, b := range bs {
		out.Points = append(out.Points, metricPoint{At: httpx.Time(time.UnixMilli(b.Bucket)), CPUAvg: round1(b.CPUAvg), CPUMax: round1(b.CPUMax),
			RAMAvg: round1(b.RAMAvg), RAMMax: round1(b.RAMMax), DiskAvg: round1(b.DiskAvg), DiskMax: round1(b.DiskMax), ChatsMax: b.ChatsMax, Samples: b.Samples})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

func httpErrCode(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		return he.Code
	}
	return "error"
}
