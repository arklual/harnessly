package link

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/coder/websocket"
)

// Refuse makes the dialer take no new links while the ones it has go on: the hub is being taken out of rotation. An
// agent that dials is answered 409, which it reads as «another node holds this machine» and calls again within a
// second, by then through a replica that takes links.
func (d *Dialer) Refuse() { d.refusing.Store(true) }

// Drain ends the links this process holds one at a time, as a hub that goes away does, so each agent dials again at
// once. The closes are spread over window with a random pause between them: the agents reach the next replica one by
// one and not as a herd. It stops early when ctx ends and returns how many links it closed; CloseAll ends the rest.
func (r *Registry) Drain(ctx context.Context, window time.Duration) int {
	return r.drain(ctx, window, func(ctx context.Context, d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		}
	})
}

func (r *Registry) drain(ctx context.Context, window time.Duration, sleep func(context.Context, time.Duration) bool) int {
	r.mu.Lock()
	links := make([]*Link, 0, len(r.links))
	for _, l := range r.links {
		if !l.Closed() {
			links = append(links, l)
		}
	}
	r.mu.Unlock()
	if len(links) == 0 {
		return 0
	}
	step := window / time.Duration(len(links))
	closed := 0
	for i, l := range links {
		if i > 0 && step > 0 {
			// between half a step and a step and a half: the same mean, never the same beat
			if !sleep(ctx, step/2+rand.N(step)) {
				return closed
			}
		}
		if ctx.Err() != nil {
			return closed
		}
		l.Close(websocket.StatusGoingAway, "hub restarting")
		closed++
	}
	return closed
}
