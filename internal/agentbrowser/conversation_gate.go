package agentbrowser

import (
	"context"
	"errors"
	"sync"
)

// ErrControlInterrupted asks an agent to retry after a person changes browser control.
var ErrControlInterrupted = errors.New("Interrupted: the person changed browser control; retry")

type conversationGate struct {
	mu      sync.Mutex
	changed chan struct{}
	busy    bool
	people  int
	cancel  context.CancelCauseFunc
}

func (i *Instance) conversationGate(conv string) *conversationGate {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.conversationGates == nil {
		i.conversationGates = make(map[string]*conversationGate)
	}
	g := i.conversationGates[conv]
	if g == nil {
		g = &conversationGate{changed: make(chan struct{})}
		i.conversationGates[conv] = g
	}
	return g
}

func (g *conversationGate) notify() { close(g.changed); g.changed = make(chan struct{}) }

func (g *conversationGate) lock(ctx context.Context, cancel context.CancelCauseFunc, person bool) (func(), error) {
	g.mu.Lock()
	if person {
		g.people++
		if g.cancel != nil {
			g.cancel(ErrControlInterrupted)
		}
	}
	for g.busy || !person && g.people > 0 {
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		g.mu.Lock()
		if ctx.Err() != nil {
			if person {
				g.people--
				g.notify()
			}
			g.mu.Unlock()
			return nil, context.Cause(ctx)
		}
	}
	if person {
		g.people--
	}
	g.busy, g.cancel = true, cancel
	g.mu.Unlock()
	return func() {
		g.mu.Lock()
		g.busy, g.cancel = false, nil
		g.notify()
		g.mu.Unlock()
	}, nil
}

// LockConversation serializes a non-cancellable action with tools and handoffs.
// Release it exactly once; never acquire it from an action already holding it.
func (i *Instance) LockConversation(conv string) func() {
	unlock, _ := i.conversationGate(conv).lock(context.Background(), nil, false)
	return unlock
}

// LockToolConversation keeps seal checks and dispatch together. Returning control
// cancels this tool before waiting for its gate; queued tools yield to the person.
func (i *Instance) LockToolConversation(ctx context.Context, conv string) (context.Context, func(), error) {
	ctx, cancel := context.WithCancelCause(ctx)
	unlock, err := i.conversationGate(conv).lock(ctx, cancel, false)
	if err != nil {
		cancel(nil)
		return ctx, nil, err
	}
	return ctx, func() { cancel(nil); unlock() }, nil
}

func (i *Instance) lockControlConversation(conv string) func() {
	unlock, _ := i.conversationGate(conv).lock(context.Background(), nil, true)
	return unlock
}
