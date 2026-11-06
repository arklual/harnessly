// Package provider is the hub's port to whoever runs a machine's virtual server: it lists the plans a machine can move
// to, says what a size costs, changes the size, reboots and powers the server, and reports how an asked-for operation
// went. The hub keeps no price list and no formula: every amount shown to a person comes from Quote or from a Plan.
//
// Implementations: None (every call answers ErrUnsupported), HTTP (a service that speaks docs/hub/provider.md) and
// Memory (tests). Selecting one is config.Config.Provider*.
//
// TODO(openstack): a driver that talks to a cloud's compute API directly is not written; the HTTP provider is the way
// to put one behind this port until then.
package provider

import (
	"context"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

// Errors the port answers; they are those of the extension frame.
var (
	// ErrUnsupported means the machine has no provider that can do this; the API answers {"error":"not_supported"}.
	ErrUnsupported = ext.ErrUnsupported
	// ErrNotFound means the provider does not know the machine or the operation.
	ErrNotFound = ext.ErrNotFound
)

// Error is a refusal or a failure from the provider with its own code.
type Error = ext.Error

// Machine names the machine to the provider.
type Machine struct {
	// ID is the hub's id of the machine.
	ID string
	// ExternalID is the provider's own id; "" means the provider is addressed by ID.
	ExternalID string
	Name       string
}

// Ref is the id the machine is addressed by at the provider.
func (m Machine) Ref() string {
	if m.ExternalID != "" {
		return m.ExternalID
	}
	return m.ID
}

// Resources is a size: cores, memory and disk. Disk can only grow.
type Resources struct {
	CPU    int `json:"cpu"`
	RAMGB  int `json:"ramGB"`
	DiskGB int `json:"diskGB"`
}

// Price is what a size costs: an amount in minor units (kopecks, cents) of a currency for a period, always from the
// provider.
type Price struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	// Period is "month".
	Period string `json:"period"`
}

// Plan is a size the provider sells.
type Plan struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	CPU   int    `json:"cpu"`
	RAMGB int    `json:"ramGB"`
	// DiskGB is the plan's disk.
	DiskGB int `json:"diskGB"`
	// Price is nil when the provider names none.
	Price *Price `json:"price,omitempty"`
	// Current marks the plan the machine is on now.
	Current bool `json:"current,omitempty"`
}

// Resources is the plan's size.
func (p Plan) Resources() Resources { return Resources{CPU: p.CPU, RAMGB: p.RAMGB, DiskGB: p.DiskGB} }

// OpRef is a handle to an operation the provider runs.
type OpRef struct {
	ID string `json:"opId"`
}

// Op states.
const (
	StatePending = "pending"
	StateRunning = "running"
	StateDone    = "done"
	StateFailed  = "failed"
)

// OpStatus is how an operation stands.
type OpStatus struct {
	// State is pending, running, done or failed.
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// Machine is the provider's id of the server the operation made (protocol v1: the answer of a finished create).
	Machine string `json:"machine,omitempty"`
}

// Finished reports whether the operation reached an end.
func (s OpStatus) Finished() bool { return s.State == StateDone || s.State == StateFailed }

// Provider is the port.
type Provider interface {
	// Name is "none", "http" and so on, for the screens and logs.
	Name() string
	Plans(ctx context.Context, m Machine) ([]Plan, error)
	Quote(ctx context.Context, m Machine, want Resources) (Price, error)
	Resize(ctx context.Context, m Machine, want Resources) (OpRef, error)
	Reboot(ctx context.Context, m Machine) (OpRef, error)
	PowerOff(ctx context.Context, m Machine) (OpRef, error)
	PowerOn(ctx context.Context, m Machine) (OpRef, error)
	Op(ctx context.Context, ref OpRef) (OpStatus, error)
	// ConsoleURL is a link to the provider's console of the machine.
	ConsoleURL(ctx context.Context, m Machine) (string, error)
}

// WithIdempotencyKey carries the key a state-changing call is sent with, so that a retry of the same hub operation is
// the same operation at the provider. Without one the provider implementation makes a random key per call.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return ext.WithIdempotencyKey(ctx, key)
}

// IdempotencyKey reads the key WithIdempotencyKey put in ctx.
func IdempotencyKey(ctx context.Context) string { return ext.IdempotencyKey(ctx) }

// None is the provider of a hub that has none: every call is unsupported.
type None struct{}

func (None) Name() string                                             { return "none" }
func (None) Plans(context.Context, Machine) ([]Plan, error)           { return nil, ErrUnsupported }
func (None) Quote(context.Context, Machine, Resources) (Price, error) { return Price{}, ErrUnsupported }
func (None) Resize(context.Context, Machine, Resources) (OpRef, error) {
	return OpRef{}, ErrUnsupported
}
func (None) Reboot(context.Context, Machine) (OpRef, error)   { return OpRef{}, ErrUnsupported }
func (None) PowerOff(context.Context, Machine) (OpRef, error) { return OpRef{}, ErrUnsupported }
func (None) PowerOn(context.Context, Machine) (OpRef, error)  { return OpRef{}, ErrUnsupported }
func (None) Op(context.Context, OpRef) (OpStatus, error)      { return OpStatus{}, ErrUnsupported }
func (None) ConsoleURL(context.Context, Machine) (string, error) {
	return "", ErrUnsupported
}

// Supported reports whether p can do anything at all.
func Supported(p Provider) bool {
	if p == nil {
		return false
	}
	_, none := p.(None)
	return !none
}
