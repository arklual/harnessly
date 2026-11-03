package provider

import (
	"context"
	"fmt"
	"sync"
)

// Memory is a provider that lives in the process, for tests of the hub and of anything built on the port. Prices come
// from PriceFn, which a test sets: there is no price rule here either.
type Memory struct {
	mu sync.Mutex
	// Catalog is what Plans answers; the plan of a machine is the one whose Current is true unless the machine was resized.
	Catalog []Plan
	// PriceFn answers Quote; nil makes Quote fail.
	PriceFn func(Resources) Price
	// Console is the link ConsoleURL answers.
	Console string
	// Auto finishes an operation at the first Op poll; otherwise a test calls Complete.
	Auto bool

	sizes map[string]Resources
	ops   map[string]*memOp
	calls []string
	n     int
	// failNext makes the next state-changing call fail with this error.
	failNext error
}

type memOp struct {
	status OpStatus
	kind   string
	ref    string
}

// NewMemory makes an empty in-memory provider.
func NewMemory() *Memory {
	return &Memory{sizes: map[string]Resources{}, ops: map[string]*memOp{}}
}

func (p *Memory) Name() string { return "memory" }

// Calls lists the state-changing calls made, as "kind:ref" (resize carries the size: "resize:ref:4/8/80").
func (p *Memory) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

// FailNext makes the next state-changing call return err.
func (p *Memory) FailNext(err error) {
	p.mu.Lock()
	p.failNext = err
	p.mu.Unlock()
}

// Size reports the size the machine has been resized to (ok false if never).
func (p *Memory) Size(ref string) (Resources, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.sizes[ref]
	return r, ok
}

func (p *Memory) Plans(_ context.Context, m Machine) ([]Plan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Plan, len(p.Catalog))
	copy(out, p.Catalog)
	if size, ok := p.sizes[m.Ref()]; ok {
		for i := range out {
			out[i].Current = out[i].Resources() == size
		}
	}
	return out, nil
}

func (p *Memory) Quote(_ context.Context, _ Machine, want Resources) (Price, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.PriceFn == nil {
		return Price{}, &Error{Code: "no_price", Message: "the provider has no price for this size"}
	}
	return p.PriceFn(want), nil
}

func (p *Memory) start(kind string, m Machine, detail string) (OpRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failNext; err != nil {
		p.failNext = nil
		return OpRef{}, err
	}
	p.n++
	id := fmt.Sprintf("op-%d", p.n)
	p.ops[id] = &memOp{status: OpStatus{State: StateRunning}, kind: kind, ref: m.Ref()}
	call := kind + ":" + m.Ref()
	if detail != "" {
		call += ":" + detail
	}
	p.calls = append(p.calls, call)
	return OpRef{ID: id}, nil
}

func (p *Memory) Resize(_ context.Context, m Machine, want Resources) (OpRef, error) {
	ref, err := p.start("resize", m, fmt.Sprintf("%d/%d/%d", want.CPU, want.RAMGB, want.DiskGB))
	if err == nil {
		p.mu.Lock()
		p.sizes[m.Ref()] = want
		p.mu.Unlock()
	}
	return ref, err
}

func (p *Memory) Reboot(_ context.Context, m Machine) (OpRef, error) { return p.start("reboot", m, "") }
func (p *Memory) PowerOff(_ context.Context, m Machine) (OpRef, error) {
	return p.start("power-off", m, "")
}
func (p *Memory) PowerOn(_ context.Context, m Machine) (OpRef, error) {
	return p.start("power-on", m, "")
}

func (p *Memory) Op(_ context.Context, ref OpRef) (OpStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	op, ok := p.ops[ref.ID]
	if !ok {
		return OpStatus{}, ErrNotFound
	}
	if p.Auto && !op.status.Finished() {
		op.status = OpStatus{State: StateDone}
	}
	return op.status, nil
}

// Complete finishes an operation: done when failure is "", failed otherwise.
func (p *Memory) Complete(opID, failure string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if op, ok := p.ops[opID]; ok {
		if failure == "" {
			op.status = OpStatus{State: StateDone}
		} else {
			op.status = OpStatus{State: StateFailed, Error: failure}
		}
	}
}

func (p *Memory) ConsoleURL(_ context.Context, _ Machine) (string, error) {
	if p.Console == "" {
		return "", ErrUnsupported
	}
	return p.Console, nil
}
