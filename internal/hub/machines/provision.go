package machines

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Machine adapter protocol v1 (docs/hub/provider.md): making a server at the provider, taking over one that exists, and
// reading its state. The endpoints are mounted only when HUB_PROVIDER_PROTOCOL is auto or v1, so a hub that is not asked
// for it answers 404 here as before.
//
// The hub records the server it asked for with the space that asked (provider_servers) and the operations it started
// (provider_ops): the adapter's operation says when the server exists, the server's agent registers with the code it
// was given like any machine, and attach ties the two together (provider id <-> hub id). A machine is tied only to a
// server of its own space, a server to one machine, and an operation is read only by the space that started it. A
// server the hub did not make is taken over by an administrator of the installation.

func (s *Service) mountProvision(mux *http.ServeMux) {
	if s.cfg.Machines.ProviderProtocol == config.ProviderProtocolV0 || !provider.Supported(s.prov) {
		return
	}
	sub, g := s.spaces.Sub(), s.guard()
	sub.Handle("POST /api/spaces/{spaceId}/machines/provision", g.Space(tenant.Owner, s.provision))
	sub.Handle("GET /api/spaces/{spaceId}/machines/provision/{opId}", g.Space(tenant.Owner, s.provisionOp))
	mux.Handle("POST /api/machines/{machineId}/provider/attach", g.Machine(tenant.Owner, s.providerAttach))
	mux.Handle("GET /api/machines/{machineId}/provider", g.Machine(tenant.Viewer, s.providerState))
	mux.Handle("POST /api/machines/{machineId}/provider/power", g.Machine(tenant.Owner, s.providerPower))
	mux.Handle("GET /api/machines/{machineId}/provider/ops/{opId}", g.Machine(tenant.Viewer, s.providerOpStatus))
	s.reportConflicts()
}

// reportConflicts names, at the start, the machines that lost their tie to a server because another machine named the
// same server first: somebody ties them again, or removes them.
func (s *Service) reportConflicts() {
	ctx, cancel := context.WithTimeout(context.Background(), providerTimeout)
	defer cancel()
	list, err := s.db.S().ResourceConflicts(ctx)
	if err != nil {
		s.log.Warn("machines tied to one server not read", "error", err)
		return
	}
	for _, c := range list {
		s.log.Warn("a machine lost its tie to a provider's server that another machine named first",
			"machine", c.MachineID, "provider", c.Provider, "server", c.ExternalID, "kept", c.KeptMachineID)
	}
}

var (
	errNoServer    = httpx.New(http.StatusNotFound, "server_not_found", "У этого пространства нет такого сервера у провайдера")
	errServerLimit = httpx.New(http.StatusConflict, "server_limit", "У этого пространства уже много серверов у провайдера")
	errServerTaken = httpx.New(http.StatusConflict, "server_attached", "Этот сервер уже привязан к другой машине")
)

func (s *Service) lifecycle() (provider.Lifecycle, error) {
	l, ok := provider.LifecycleOf(s.prov)
	if !ok {
		return nil, errNotSupported
	}
	return l, nil
}

func idemKey(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get("Idempotency-Key")); k != "" && len(k) <= 128 {
		return k
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// provision asks the adapter to make a server that joins this space: the body is {name, config}; the adapter gets a
// registration code to start the machine's agent with.
func (s *Service) provision(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	var req struct {
		Name   string         `json:"name"`
		Config map[string]any `json:"config"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, ok := printable(req.Name, 100)
	if !ok || name == "" {
		return httpx.Invalid("name", "Укажите имя машины")
	}
	l, err := s.lifecycle()
	if err != nil {
		return err
	}
	ctx := r.Context()
	spaceID := t.SpaceID
	// A server costs money at the provider from the moment it is asked for: the space's count is looked at first.
	if n, err := s.db.S().CountProviderServers(ctx, spaceID); err != nil {
		return err
	} else if n >= s.cfg.MaxServers() {
		return errServerLimit
	}
	_, code, err := s.IssueCode(ctx, spaceID, p.User.ID, string(hubproto.ProfileDir))
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(provider.WithIdempotencyKey(ctx, idemKey(r)), providerTimeout)
	defer cancel()
	ref, err := l.Create(pctx, provider.CreateRequest{Name: name, Config: req.Config, Bootstrap: &provider.Bootstrap{HubURL: s.cfg.AgentURL, Code: code}})
	if err != nil {
		return mapProviderErr(err)
	}
	st := s.db.S()
	if err := st.AddProviderOp(ctx, store.ProviderOp{Provider: s.prov.Name(), OpID: ref.Op, SpaceID: spaceID, Kind: "create", CreatedBy: p.User.ID, CreatedAt: s.now().UTC()}); err != nil {
		return err
	}
	if err := st.AddProviderServer(ctx, &store.ProviderServer{SpaceID: spaceID, Provider: s.prov.Name(), ExternalID: ref.Machine, OpID: ref.Op, Name: name, CreatedBy: p.User.ID, CreatedAt: s.now().UTC()}); err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_provision", r, spaceID+" "+ref.Op)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"op": ref.Op, "machine": ref.Machine})
	return nil
}

func (s *Service) provisionOp(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	ctx := r.Context()
	spaceID, opID := t.SpaceID, r.PathValue("opId")
	if _, err := s.db.S().ProviderOpOfSpace(ctx, spaceID, s.prov.Name(), opID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return httpx.ErrNotFound
		}
		return err
	}
	st, err := s.opStatus(ctx, opID)
	if err != nil {
		return err
	}
	if st.Machine != "" {
		// The operation made the server: from now on the space may tie a machine to it.
		if err := s.db.S().NameProviderServer(ctx, spaceID, s.prov.Name(), opID, st.Machine); err != nil {
			s.log.Warn("the server an operation made was not recorded", "space", spaceID, "op", opID, "error", err)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

func (s *Service) opStatus(ctx context.Context, opID string) (provider.OpStatus, error) {
	pctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	st, err := s.prov.Op(pctx, provider.OpRef{ID: opID})
	if err != nil {
		return st, mapProviderErr(err)
	}
	return st, nil
}

// providerAttach ties a registered machine to the server at the provider: {externalId}.
func (s *Service) providerAttach(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	var req struct {
		ExternalID string `json:"externalId"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ext, ok := printable(req.ExternalID, 200)
	if !ok || ext == "" {
		return httpx.Invalid("externalId", "Укажите id сервера у провайдера")
	}
	l, err := s.lifecycle()
	if err != nil {
		return err
	}
	ctx := r.Context()
	m := *t.Machine
	db := s.db.S()
	mine, err := db.ProviderServerOfSpace(ctx, m.SpaceID, s.prov.Name(), ext)
	if err != nil {
		return err
	}
	if !mine {
		// A server no space has is taken over by an administrator of the installation, for the machine's space.
		known, err := db.ProviderServerKnown(ctx, s.prov.Name(), ext)
		if err != nil {
			return err
		}
		if known || !p.Admin {
			return errNoServer
		}
		err = db.AddProviderServer(ctx, &store.ProviderServer{SpaceID: m.SpaceID, Provider: s.prov.Name(), ExternalID: ext, Name: m.Name, CreatedBy: p.User.ID, CreatedAt: s.now().UTC()})
		if store.IsUnique(err) {
			return errNoServer
		}
		if err != nil {
			return err
		}
	}
	pctx, cancel := context.WithTimeout(provider.WithIdempotencyKey(ctx, idemKey(r)), providerTimeout)
	defer cancel()
	st, err := l.Attach(pctx, ext, provider.AttachRequest{HubMachine: m.ID, Name: m.Name})
	if err != nil {
		return mapProviderErr(err)
	}
	rs, err := s.db.S().ResourcesByMachine(ctx, m.ID)
	if err != nil && !store.IsNotFound(err) {
		return err
	}
	rs.MachineID, rs.Provider, rs.ExternalID, rs.UpdatedAt = m.ID, s.prov.Name(), ext, s.now().UTC()
	if st.CPU > 0 {
		rs.CPU, rs.RAMGB, rs.DiskGB = &st.CPU, &st.RAMGB, &st.DiskGB
	}
	if err := s.db.S().PutResources(ctx, rs); err != nil {
		if store.IsUnique(err) {
			return errServerTaken
		}
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_attach", r, m.ID+" "+ext)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machineId": m.ID, "externalId": ext, "state": st})
	return nil
}

func (s *Service) attached(ctx context.Context, m store.Machine) (string, error) {
	rs, err := s.db.S().ResourcesByMachine(ctx, m.ID)
	if store.IsNotFound(err) || (err == nil && rs.ExternalID == "") {
		return "", httpx.New(http.StatusConflict, "not_attached", "Машина не привязана к серверу у провайдера")
	}
	return rs.ExternalID, err
}

func (s *Service) providerState(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	l, err := s.lifecycle()
	if err != nil {
		return err
	}
	ctx := r.Context()
	ext, err := s.attached(ctx, *t.Machine)
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	st, err := l.State(pctx, ext)
	if err != nil {
		return mapProviderErr(err)
	}
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

// providerPower is {action: on | off | reboot}; it answers the adapter's operation.
func (s *Service) providerPower(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	var req struct {
		Action string `json:"action"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	m := *t.Machine
	ext, err := s.attached(ctx, m)
	if err != nil {
		return err
	}
	pm := provider.Machine{ID: m.ID, ExternalID: ext, Name: m.Name}
	pctx, cancel := context.WithTimeout(provider.WithIdempotencyKey(ctx, idemKey(r)), providerTimeout)
	defer cancel()
	var op provider.OpRef
	switch req.Action {
	case "on":
		op, err = s.prov.PowerOn(pctx, pm)
	case "off":
		op, err = s.prov.PowerOff(pctx, pm)
	case "reboot":
		op, err = s.prov.Reboot(pctx, pm)
	default:
		return httpx.Invalid("action", "action — on, off или reboot")
	}
	if err != nil {
		return mapProviderErr(err)
	}
	if err := s.db.S().AddProviderOp(ctx, store.ProviderOp{Provider: s.prov.Name(), OpID: op.ID, SpaceID: m.SpaceID, MachineID: m.ID, Kind: req.Action, CreatedBy: p.User.ID, CreatedAt: s.now().UTC()}); err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_provider_power", r, m.ID+" "+req.Action)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"op": op.ID})
	return nil
}

func (s *Service) providerOpStatus(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	ctx := r.Context()
	m := *t.Machine
	if _, err := s.attached(ctx, m); err != nil {
		return err
	}
	opID := r.PathValue("opId")
	if ok, err := s.db.S().ProviderOpOfMachine(ctx, m, s.prov.Name(), opID); err != nil {
		return err
	} else if !ok {
		return httpx.ErrNotFound
	}
	st, err := s.opStatus(ctx, opID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}
