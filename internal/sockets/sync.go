package sockets

import (
	"net/http"
	"sync"
	"time"
)

// toastTTL: a toast is replayed to sockets that connect later (the relay reconnects), but not for the life of the
// process as before.
const toastTTL = 24 * time.Hour

type pendingToast struct {
	msg any
	at  time.Time
}

// SyncServer is /api/sync: what changed (layouts, workspaces), toasts, and commands for the page. The page sends
// nothing but its heartbeat.
type SyncServer struct {
	Hub *Hub

	mu     sync.Mutex
	order  []string
	toasts map[string]pendingToast
}

// ServeHTTP upgrades and serves one sync socket. Authentication is the caller's.
func (s *SyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Hub.accept(w, r, func(c *Client) {
		for _, t := range s.liveToasts() {
			c.SendJSON(t)
		}
	}, nil, nil)
}

// Broadcast sends an event to every sync socket.
func (s *SyncServer) Broadcast(event any) { s.Hub.Broadcast(event, nil) }

// Toast is shown now and to every socket that connects later; the key replaces an earlier toast of the same kind.
func (s *SyncServer) Toast(key string, toast any) {
	s.mu.Lock()
	if s.toasts == nil {
		s.toasts = map[string]pendingToast{}
	}
	if _, has := s.toasts[key]; !has {
		s.order = append(s.order, key)
	}
	s.toasts[key] = pendingToast{msg: toast, at: time.Now()}
	s.mu.Unlock()
	s.Broadcast(toast)
}

func (s *SyncServer) liveToasts() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []any
	kept := s.order[:0]
	for _, k := range s.order {
		t := s.toasts[k]
		if time.Since(t.at) > toastTTL {
			delete(s.toasts, k)
			continue
		}
		kept = append(kept, k)
		out = append(out, t.msg)
	}
	s.order = kept
	return out
}
