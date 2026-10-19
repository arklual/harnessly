package sockets

import (
	"net/http"
	"regexp"

	"harnessly/internal/jscompat"
)

// StatusSource is what the status socket reads and where the page's messages go.
type StatusSource interface {
	// RateLimits is the last limits content, or nil before the first one.
	RateLimits() any
	// Tabs is every tab as the page sees it; Background every tab with background work.
	Tabs() any
	Background() any
	// Queues and Intents are read after the snapshot; they may take a moment.
	Queues() any
	Intents() any
	// History is the clipped session history, sent only when a page asks (it is large and one panel reads it).
	History() any
	// Dismiss: a page acknowledged a finished turn. A page's own socket gets no echo; the relay does, because it
	// carries the messages of every page reading the cluster stream.
	Dismiss(tabID string, exclude *Client)
	Ack(tabID string, seq float64)
	IntentsApplied(tabID string, ids []string, device string)
	Warn(msg string)
}

var (
	intentTab = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	intentID  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// StatusServer is /api/status.
type StatusServer struct {
	Hub    *Hub
	Source StatusSource
}

// ServeHTTP upgrades and serves one status socket. Authentication is the caller's.
func (s *StatusServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Hub.accept(w, r, s.open, s.message, func(c *Client) { s.sync(c, true) })
}

func (s *StatusServer) open(c *Client) {
	if rl := s.Source.RateLimits(); rl != nil {
		c.SendJSON(map[string]any{"type": "rate-limits:update", "data": rl})
	}
	s.sync(c, false)
}

// sync sends the snapshot: tabs, then background work, then (read asynchronously) the queue and the pane intents.
// A repeated sync carries the limits too: a page catching up from the cluster stream, trimmed at the last sync,
// would otherwise see none until they change.
func (s *StatusServer) sync(c *Client, withLimits bool) {
	c.SendJSON(map[string]any{"type": "status:sync", "tabs": s.Source.Tabs()})
	c.SendJSON(map[string]any{"type": "background:sync", "tabs": s.Source.Background()})
	if withLimits {
		if rl := s.Source.RateLimits(); rl != nil {
			c.SendJSON(map[string]any{"type": "rate-limits:update", "data": rl})
		}
	}
	go c.SendJSON(map[string]any{"type": "prompt-queue:sync", "queues": s.Source.Queues()})
	go c.SendJSON(map[string]any{"type": "pane-intents:sync", "intents": s.Source.Intents()})
}

func (s *StatusServer) message(c *Client, data []byte) {
	m, ok := decode(data)
	if !ok {
		return
	}
	tabID, _ := m["tabId"].(string)
	switch m["type"] {
	case "status:tab-dismissed":
		var exclude *Client
		if !c.Relay {
			exclude = c
		}
		s.Source.Dismiss(tabID, exclude)
	case "status:ack-notification":
		seq, _ := m["seq"].(float64)
		s.Source.Ack(tabID, seq)
	case "status:request-sync":
		s.sync(c, true)
	case "session-history:request":
		go c.SendJSON(map[string]any{"type": "session-history:sync", "entries": s.Source.History()})
	case "pane-intents:applied":
		var ids []string
		raw, _ := m["ids"].([]any)
		for _, v := range raw {
			if id, ok := v.(string); ok && intentID.MatchString(id) && len(ids) < 20 {
				ids = append(ids, id)
			}
		}
		device, _ := m["device"].(string)
		tab := jscompat.String(m["tabId"]) // a number id is tested as its digits, as before
		if !intentTab.MatchString(tab) || len(ids) == 0 || (device != "desktop" && device != "phone") {
			return
		}
		s.Source.IntentsApplied(tab, ids, device)
	default:
		t, _ := m["type"].(string)
		s.Source.Warn("Unknown event: " + t)
	}
}
