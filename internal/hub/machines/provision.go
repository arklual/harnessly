package machines

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Machine adapter protocol v1 (docs/hub/provider.md): making a server at the provider, taking over one that exists, and
// reading its state. The endpoints are mounted only when HUB_PROVIDER_PROTOCOL is auto or v1, so a hub that is not asked
// for it answers 404 here as before.
//
// The hub keeps no record of a server it asked for: the adapter's operation says when it exists, the server's agent
// registers with the code it was given like any machine, and attach ties the two together (provider id <-> hub id).

func (s *Service) mountProvision(mux *http.ServeMux) {
	if s.cfg.Machines.ProviderProtocol == config.ProviderProtocolV0 || !provider.Supported(s.prov) {
		return
	}
	sub := s.spaces.Sub()
	sub.Handle("POST /api/spaces/{spaceId}/machines/provision", s.auth.Require(s.provision))
	sub.Handle("GET /api/spaces/{spaceId}/machines/provision/{opId}", s.auth.Require(s.provisionOp))
	mux.Handle("POST /api/machines/{machineId}/provider/attach", s.auth.Require(s.providerAttach))
	mux.Handle("GET /api/machines/{machineId}/provider", s.auth.Require(s.providerState))
	mux.Handle("POST /api/machines/{machineId}/provider/power", s.auth.Require(s.providerPower))
	mux.Handle("GET /api/machines/{machineId}/provider/ops/{opId}", s.auth.Require(s.providerOpStatus))
}

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
func (s *Service) provision(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
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
	spaceID := r.PathValue("spaceId")
	if err := s.spaces.Owner(ctx, p.User.ID, spaceID); err != nil {
		return err
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
	s.auth.Audit(ctx, p.User.ID, "machine_provision", r, spaceID+" "+ref.Op)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"op": ref.Op, "machine": ref.Machine})
	return nil
}

func (s *Service) provisionOp(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if err := s.spaces.Owner(r.Context(), p.User.ID, r.PathValue("spaceId")); err != nil {
		return err
	}
	return s.writeOp(w, r)
}

func (s *Service) writeOp(w http.ResponseWriter, r *http.Request) error {
	pctx, cancel := context.WithTimeout(r.Context(), providerTimeout)
	defer cancel()
	st, err := s.prov.Op(pctx, provider.OpRef{ID: r.PathValue("opId")})
	if err != nil {
		return mapProviderErr(err)
	}
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

// providerAttach ties a registered machine to the server at the provider: {externalId}.
func (s *Service) providerAttach(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
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
	m, err := s.Authorize(ctx, p.User.ID, r.PathValue("machineId"), true)
	if err != nil {
		return err
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
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_attach", r, m.ID+" "+ext)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machineId": m.ID, "externalId": ext, "state": st})
	return nil
}

func (s *Service) attached(ctx context.Context, userID, machineID string, owner bool) (store.Machine, string, error) {
	m, err := s.Authorize(ctx, userID, machineID, owner)
	if err != nil {
		return m, "", err
	}
	rs, err := s.db.S().ResourcesByMachine(ctx, m.ID)
	if store.IsNotFound(err) || (err == nil && rs.ExternalID == "") {
		return m, "", httpx.New(http.StatusConflict, "not_attached", "Машина не привязана к серверу у провайдера")
	}
	return m, rs.ExternalID, err
}

func (s *Service) providerState(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	l, err := s.lifecycle()
	if err != nil {
		return err
	}
	ctx := r.Context()
	_, ext, err := s.attached(ctx, p.User.ID, r.PathValue("machineId"), false)
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
func (s *Service) providerPower(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		Action string `json:"action"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	m, ext, err := s.attached(ctx, p.User.ID, r.PathValue("machineId"), true)
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
	s.auth.Audit(ctx, p.User.ID, "machine_provider_power", r, m.ID+" "+req.Action)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"op": op.ID})
	return nil
}

func (s *Service) providerOpStatus(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if _, _, err := s.attached(r.Context(), p.User.ID, r.PathValue("machineId"), false); err != nil {
		return err
	}
	return s.writeOp(w, r)
}
