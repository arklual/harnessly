package gate

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"harnessly/internal/agent"
)

// An upgraded socket stays with whichever side accepted it: switching a slice routes the next request, not a socket
// that is already open. For the sockets a slice takes over (the machine agent's status and sync relays when the core
// slice flips) the gate closes them, and the agent dials again — to the side the slice now names.

type connKey struct{}

// ConnContext is for http.Server.ConnContext: it lets the gate find the connection under an upgraded request.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}

// handover tracks the proxied upgrades of the paths a slice takes over.
type handover struct {
	mu    sync.Mutex
	paths map[string]string // path without /agents → slice
	open  map[string]map[net.Conn]struct{}
	on    map[string]bool
	hooks map[string][]func(on bool)
}

func newHandover() *handover {
	return &handover{paths: map[string]string{}, open: map[string]map[net.Conn]struct{}{}, on: map[string]bool{}, hooks: map[string][]func(bool){}}
}

// HandOverSockets names upgraded paths the slice takes over: proxied ones are closed when it switches on.
func (f *Gate) HandOverSockets(slice string, paths ...string) {
	f.handover.mu.Lock()
	defer f.handover.mu.Unlock()
	for _, p := range paths {
		f.handover.paths[p] = slice
	}
}

// OnSliceChange runs fn whenever the slice switches (after the gate closed the proxied sockets it takes over); the Go
// side closes its own sockets there when the slice goes off.
func (f *Gate) OnSliceChange(slice string, fn func(on bool)) {
	f.handover.mu.Lock()
	defer f.handover.mu.Unlock()
	f.handover.hooks[slice] = append(f.handover.hooks[slice], fn)
}

// trackProxied registers a proxied upgrade of a handed-over path for the time it is open; the returned func ends that.
func (f *Gate) trackProxied(r *http.Request) func() {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return func() {}
	}
	f.handover.mu.Lock()
	slice, ok := f.handover.paths[strings.TrimPrefix(r.URL.Path, "/agents")]
	f.handover.mu.Unlock()
	conn, hasConn := r.Context().Value(connKey{}).(net.Conn)
	if !ok || !hasConn {
		return func() {}
	}
	f.handover.mu.Lock()
	if f.handover.open[slice] == nil {
		f.handover.open[slice] = map[net.Conn]struct{}{}
	}
	f.handover.open[slice][conn] = struct{}{}
	f.handover.mu.Unlock()
	return func() {
		f.handover.mu.Lock()
		delete(f.handover.open[slice], conn)
		f.handover.mu.Unlock()
	}
}

// WatchSlices notices switches of the slices with handed-over sockets or hooks, once a second, until ctx ends.
func (f *Gate) WatchSlices(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		f.checkSlices()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (f *Gate) checkSlices() {
	f.handover.mu.Lock()
	slices := map[string]bool{}
	for _, s := range f.handover.paths {
		slices[s] = true
	}
	for s := range f.handover.hooks {
		slices[s] = true
	}
	f.handover.mu.Unlock()
	for slice := range slices {
		on := f.slices.On(slice)
		f.handover.mu.Lock()
		was, seen := f.handover.on[slice]
		f.handover.on[slice] = on
		var closing []net.Conn
		if seen && was != on && on {
			for c := range f.handover.open[slice] {
				closing = append(closing, c)
			}
			f.handover.open[slice] = nil
		}
		hooks := append([]func(bool){}, f.handover.hooks[slice]...)
		f.handover.mu.Unlock()
		if !seen || was == on {
			continue
		}
		for _, c := range closing {
			_ = c.Close()
		}
		agent.Log("info", "slice switched", agent.F{K: "slice", V: slice}, agent.F{K: "on", V: on}, agent.F{K: "socketsClosed", V: len(closing)})
		for _, fn := range hooks {
			fn(on)
		}
	}
}

// SliceOn is whether a slice is switched on now.
func (f *Gate) SliceOn(slice string) bool { return f.slices.On(slice) }

// CloseProxied closes the proxied sockets of the paths a slice takes over, for a switch the gate does not see itself
// (a slice that also needs another one running).
func (f *Gate) CloseProxied(slice string) int {
	f.handover.mu.Lock()
	var closing []net.Conn
	for c := range f.handover.open[slice] {
		closing = append(closing, c)
	}
	f.handover.open[slice] = nil
	f.handover.mu.Unlock()
	for _, c := range closing {
		_ = c.Close()
	}
	return len(closing)
}
