package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

// ProtocolV1 is the `protocol` of GET /v1/describe: the machine adapter protocol of docs/hub/provider.md, «Protocol v1».
// The older protocol, which has no describe, is v0.
const ProtocolV1 = "harnessly.machine-provider/v1"

// Capabilities an adapter lists in describe. A call whose capability is not listed is never sent: the hub answers
// not_supported for it.
const (
	CapCreate  = "create"
	CapAttach  = "attach"
	CapState   = "state"
	CapPower   = "power"
	CapResize  = "resize"
	CapConsole = "console"
	// CapPlans adds GET /v1/machines/{id}/plans and POST /v1/machines/{id}/quote.
	CapPlans = "plans"
)

// Description is the answer of GET /v1/describe, the same for every kind of extension (internal/hub/ext).
type Description = ext.Description

// Bootstrap is what a created machine needs to join the hub: the address machines reach it at and a registration code
// (docs/hub/architecture.md). The adapter starts the machine's agent with them; the hub never reaches into the machine.
type Bootstrap struct {
	HubURL string `json:"hubUrl"`
	Code   string `json:"code"`
}

// CreateRequest is POST /v1/machines.
type CreateRequest struct {
	Name string `json:"name"`
	// Config is validated by the adapter against its config_schema; the hub passes it on as it came.
	Config    map[string]any `json:"config,omitempty"`
	Bootstrap *Bootstrap     `json:"bootstrap,omitempty"`
}

// CreateRef is the answer of create: the operation to watch and, when the adapter already knows it, the server's id.
type CreateRef struct {
	Op      string `json:"op"`
	Machine string `json:"machine,omitempty"`
}

// AttachRequest is POST /v1/machines/{id}/attach: «this server is the hub's machine HubMachine».
type AttachRequest struct {
	HubMachine string `json:"hubMachine"`
	Name       string `json:"name,omitempty"`
}

// MachineState is GET /v1/machines/{id}.
type MachineState struct {
	ID string `json:"id"`
	// Status is the adapter's own word: running, stopped, creating and so on. The hub shows it and does not branch on it.
	Status string `json:"status"`
	CPU    int    `json:"cpu,omitempty"`
	RAMGB  int    `json:"ramGB,omitempty"`
	DiskGB int    `json:"diskGB,omitempty"`
	Plan   string `json:"plan,omitempty"`
	Price  *Price `json:"price,omitempty"`
}

// Lifecycle is what protocol v1 adds to the port: making a server, taking over an existing one, and reading its state.
// A Provider that speaks only v0 does not have it (see LifecycleOf).
type Lifecycle interface {
	// Describe is what the adapter says of itself.
	Describe(ctx context.Context) (Description, error)
	Create(ctx context.Context, req CreateRequest) (CreateRef, error)
	Attach(ctx context.Context, ref string, req AttachRequest) (MachineState, error)
	State(ctx context.Context, ref string) (MachineState, error)
}

// LifecycleOf is the lifecycle calls of p, when p speaks protocol v1 (or may turn out to).
func LifecycleOf(p Provider) (Lifecycle, bool) {
	l, ok := p.(Lifecycle)
	return l, ok
}

// HTTPv1 is a Provider that speaks protocol v1. It shares the transport of HTTP: the token file, the attempts, the
// timeout and the Idempotency-Key rules are the same as in v0.
type HTTPv1 struct {
	h    *HTTP
	desc Description
}

// NewHTTPv1 asks the adapter to describe itself and refuses one that does not speak protocol v1.
func NewHTTPv1(ctx context.Context, c HTTPConfig) (*HTTPv1, error) {
	h, err := NewHTTP(c)
	if err != nil {
		return nil, err
	}
	return newV1(ctx, h)
}

func newV1(ctx context.Context, h *HTTP) (*HTTPv1, error) {
	v := &HTTPv1{h: h}
	d, err := v.Describe(ctx)
	if err != nil {
		return nil, err
	}
	v.desc = d
	return v, nil
}

func (v *HTTPv1) Name() string { return "http/v1" }

// Description is what describe answered when this client was made.
func (v *HTTPv1) Description() Description { return v.desc }

// Describe asks again and refuses a protocol other than v1.
func (v *HTTPv1) Describe(ctx context.Context) (Description, error) {
	return v.h.c.Describe(ctx, ProtocolV1)
}

func (v *HTTPv1) need(c string) error {
	if !v.desc.Has(c) {
		return ErrUnsupported
	}
	return nil
}

func machinePath(ref string) string { return "/v1/machines/" + url.PathEscape(ref) }

func (v *HTTPv1) Create(ctx context.Context, req CreateRequest) (CreateRef, error) {
	if err := v.need(CapCreate); err != nil {
		return CreateRef{}, err
	}
	var out CreateRef
	if err := v.h.do(ctx, http.MethodPost, "/v1/machines", req, true, &out); err != nil {
		return CreateRef{}, err
	}
	if out.Op == "" {
		return CreateRef{}, &Error{Code: "bad_answer", Message: "the answer has no op"}
	}
	return out, nil
}

func (v *HTTPv1) Attach(ctx context.Context, ref string, req AttachRequest) (MachineState, error) {
	if err := v.need(CapAttach); err != nil {
		return MachineState{}, err
	}
	var out MachineState
	if err := v.h.do(ctx, http.MethodPost, machinePath(ref)+"/attach", req, true, &out); err != nil {
		return MachineState{}, err
	}
	return out, nil
}

func (v *HTTPv1) State(ctx context.Context, ref string) (MachineState, error) {
	if err := v.need(CapState); err != nil {
		return MachineState{}, err
	}
	var out MachineState
	if err := v.h.do(ctx, http.MethodGet, machinePath(ref), nil, false, &out); err != nil {
		return MachineState{}, err
	}
	return out, nil
}

// Plans is the adapter's catalogue when it has the plans capability; otherwise the one plan the machine is on now,
// taken from its state, so that the resources screen still shows the current size.
func (v *HTTPv1) Plans(ctx context.Context, m Machine) ([]Plan, error) {
	if v.desc.Has(CapPlans) {
		var out struct {
			Plans []Plan `json:"plans"`
		}
		if err := v.h.do(ctx, http.MethodGet, machinePath(m.Ref())+"/plans", nil, false, &out); err != nil {
			return nil, err
		}
		return out.Plans, nil
	}
	st, err := v.State(ctx, m.Ref())
	if err != nil {
		return nil, err
	}
	name := st.Plan
	if name == "" {
		name = "current"
	}
	return []Plan{{ID: name, Name: name, CPU: st.CPU, RAMGB: st.RAMGB, DiskGB: st.DiskGB, Price: st.Price, Current: true}}, nil
}

func (v *HTTPv1) Quote(ctx context.Context, m Machine, want Resources) (Price, error) {
	if err := v.need(CapPlans); err != nil {
		return Price{}, err
	}
	var out struct {
		Price *Price `json:"price"`
	}
	if err := v.h.do(ctx, http.MethodPost, machinePath(m.Ref())+"/quote", want, false, &out); err != nil {
		return Price{}, err
	}
	if out.Price == nil || out.Price.Currency == "" {
		return Price{}, &Error{Code: "bad_answer", Message: "the quote has no price"}
	}
	if out.Price.Period == "" {
		out.Price.Period = "month"
	}
	return *out.Price, nil
}

// opRef reads the {"op": "…"} answer of a call that starts an operation.
func (v *HTTPv1) opCall(ctx context.Context, capability, path string, body any) (OpRef, error) {
	if err := v.need(capability); err != nil {
		return OpRef{}, err
	}
	var out struct {
		Op string `json:"op"`
	}
	if err := v.h.do(ctx, http.MethodPost, path, body, true, &out); err != nil {
		return OpRef{}, err
	}
	if out.Op == "" {
		return OpRef{}, &Error{Code: "bad_answer", Message: "the answer has no op"}
	}
	return OpRef{ID: out.Op}, nil
}

func (v *HTTPv1) Resize(ctx context.Context, m Machine, want Resources) (OpRef, error) {
	return v.opCall(ctx, CapResize, machinePath(m.Ref())+"/resize", want)
}

func (v *HTTPv1) power(ctx context.Context, m Machine, action string) (OpRef, error) {
	return v.opCall(ctx, CapPower, machinePath(m.Ref())+"/power", map[string]string{"action": action})
}

func (v *HTTPv1) Reboot(ctx context.Context, m Machine) (OpRef, error) {
	return v.power(ctx, m, "reboot")
}
func (v *HTTPv1) PowerOff(ctx context.Context, m Machine) (OpRef, error) {
	return v.power(ctx, m, "off")
}
func (v *HTTPv1) PowerOn(ctx context.Context, m Machine) (OpRef, error) { return v.power(ctx, m, "on") }

// Op reads GET /v1/ops/{op}. The states are those of v0.
func (v *HTTPv1) Op(ctx context.Context, ref OpRef) (OpStatus, error) {
	var out OpStatus
	if err := v.h.do(ctx, http.MethodGet, "/v1/ops/"+url.PathEscape(ref.ID), nil, false, &out); err != nil {
		return OpStatus{}, err
	}
	switch out.State {
	case StatePending, StateRunning, StateDone, StateFailed:
		return out, nil
	}
	return OpStatus{}, &Error{Code: "bad_answer", Message: "unknown state " + out.State}
}

func (v *HTTPv1) ConsoleURL(ctx context.Context, m Machine) (string, error) {
	if err := v.need(CapConsole); err != nil {
		return "", err
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := v.h.do(ctx, http.MethodGet, machinePath(m.Ref())+"/console", nil, false, &out); err != nil {
		return "", err
	}
	if u, err := url.Parse(out.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", &Error{Code: "bad_answer", Message: "the console address is not an http(s) link"}
	}
	return out.URL, nil
}

// isNoDescribe tells a service that does not know the describe call at all (it is v0) from an adapter that failed to
// answer it, or that speaks another protocol: only the first is v0.
func isNoDescribe(err error) bool {
	var pe *Error
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnsupported):
		return true
	case errors.As(err, &pe):
		return pe.Code != "protocol_unsupported" && (pe.Status == http.StatusMethodNotAllowed || pe.Code == "bad_answer")
	}
	return false
}
