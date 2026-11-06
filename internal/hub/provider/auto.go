package provider

import (
	"context"
	"sync"
	"time"
)

// Modes of HUB_PROVIDER_PROTOCOL.
const (
	// ProtoV0 speaks only the protocol without a version: the behaviour before protocol v1, and the default.
	ProtoV0 = "v0"
	// ProtoAuto asks GET /v1/describe and speaks v1 when the service answers it, v0 when it does not know the call.
	ProtoAuto = "auto"
	// ProtoV1 requires the service to speak v1.
	ProtoV1 = "v1"
)

// describeTTL is how long a chosen protocol is kept before the adapter is asked again, so that an adapter replaced by a
// newer version (new capabilities) is noticed without a restart of the hub.
const describeTTL = 10 * time.Minute

// Auto is a Provider that picks the protocol from what the service says of itself. The choice is made on the first call
// and not at the start of the hub: an adapter that is down while the hub starts does not stop the hub, and the hub
// finds it when it is back.
type Auto struct {
	cfg  HTTPConfig
	mode string
	now  func() time.Time

	mu    sync.Mutex
	impl  Provider
	at    time.Time
	proto string
}

// NewAuto makes the provider for ProtoAuto or ProtoV1. The address and the token file are checked now.
func NewAuto(c HTTPConfig, mode string) (*Auto, error) {
	if _, err := NewHTTP(c); err != nil {
		return nil, err
	}
	return &Auto{cfg: c, mode: mode, now: time.Now}, nil
}

// Protocol is the protocol chosen so far: "v0", "v1", or "" before the first answer.
func (a *Auto) Protocol() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.proto
}

func (a *Auto) Name() string {
	if p := a.Protocol(); p != "" {
		return "http/" + p
	}
	return "http"
}

// pick returns the provider to call now.
func (a *Auto) pick(ctx context.Context) (Provider, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.impl != nil && a.now().Sub(a.at) < describeTTL {
		return a.impl, nil
	}
	h, err := NewHTTP(a.cfg)
	if err != nil {
		return nil, err
	}
	v1, err := newV1(ctx, h)
	switch {
	case err == nil:
		a.impl, a.proto, a.at = v1, "v1", a.now()
	case a.mode == ProtoAuto && isNoDescribe(err):
		a.impl, a.proto, a.at = h, "v0", a.now()
	case a.impl != nil:
		// The adapter does not answer describe right now: go on with what was chosen.
		a.at = a.now()
	default:
		return nil, err
	}
	return a.impl, nil
}

// Lifecycle is the lifecycle calls of the chosen protocol; ErrUnsupported on v0.
func (a *Auto) lifecycle(ctx context.Context) (Lifecycle, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return nil, err
	}
	l, ok := p.(Lifecycle)
	if !ok {
		return nil, ErrUnsupported
	}
	return l, nil
}

func (a *Auto) Describe(ctx context.Context) (Description, error) {
	l, err := a.lifecycle(ctx)
	if err != nil {
		return Description{}, err
	}
	return l.Describe(ctx)
}

func (a *Auto) Create(ctx context.Context, req CreateRequest) (CreateRef, error) {
	l, err := a.lifecycle(ctx)
	if err != nil {
		return CreateRef{}, err
	}
	return l.Create(ctx, req)
}

func (a *Auto) Attach(ctx context.Context, ref string, req AttachRequest) (MachineState, error) {
	l, err := a.lifecycle(ctx)
	if err != nil {
		return MachineState{}, err
	}
	return l.Attach(ctx, ref, req)
}

func (a *Auto) State(ctx context.Context, ref string) (MachineState, error) {
	l, err := a.lifecycle(ctx)
	if err != nil {
		return MachineState{}, err
	}
	return l.State(ctx, ref)
}

func (a *Auto) Plans(ctx context.Context, m Machine) ([]Plan, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return nil, err
	}
	return p.Plans(ctx, m)
}

func (a *Auto) Quote(ctx context.Context, m Machine, want Resources) (Price, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return Price{}, err
	}
	return p.Quote(ctx, m, want)
}

func (a *Auto) Resize(ctx context.Context, m Machine, want Resources) (OpRef, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return OpRef{}, err
	}
	return p.Resize(ctx, m, want)
}

func (a *Auto) Reboot(ctx context.Context, m Machine) (OpRef, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return OpRef{}, err
	}
	return p.Reboot(ctx, m)
}

func (a *Auto) PowerOff(ctx context.Context, m Machine) (OpRef, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return OpRef{}, err
	}
	return p.PowerOff(ctx, m)
}

func (a *Auto) PowerOn(ctx context.Context, m Machine) (OpRef, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return OpRef{}, err
	}
	return p.PowerOn(ctx, m)
}

func (a *Auto) Op(ctx context.Context, ref OpRef) (OpStatus, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return OpStatus{}, err
	}
	return p.Op(ctx, ref)
}

func (a *Auto) ConsoleURL(ctx context.Context, m Machine) (string, error) {
	p, err := a.pick(ctx)
	if err != nil {
		return "", err
	}
	return p.ConsoleURL(ctx, m)
}
