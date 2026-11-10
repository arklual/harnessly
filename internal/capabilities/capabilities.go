// Package capabilities is the one answer to «what does this server do»: GET /api/capabilities, served by the hub and by
// the runtime in the same shape (HRN-737). It reads the mechanisms that already exist (the hub's feature flags, the
// runtime's RUNTIME_WEB_FEATURES and service settings) and breaks none of them: it only says, per capability, whether it
// is on, off, or on once something is configured, and which setting that is.
package capabilities

import "sort"

// Status is where a capability stands on this server.
type Status string

const (
	// On: the server serves it.
	On Status = "on"
	// Off: the server does not serve it, and nothing a person sets in it changes that (or it was switched off).
	Off Status = "off"
	// NeedsConfig: the server can serve it once the setting named in Configure is made.
	NeedsConfig Status = "needs_config"
)

// Capability is one thing the web can ask the server to do.
type Capability struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
	// Reason says why it is not on; empty for a capability that is on.
	Reason string `json:"reason,omitempty"`
	// Configure names the setting that turns it on (an environment variable, a flag), when there is one.
	Configure string `json:"configure,omitempty"`
}

// Document is the body of GET /api/capabilities.
type Document struct {
	// Server is "hub" or "runtime".
	Server       string       `json:"server"`
	Capabilities []Capability `json:"capabilities"`
}

// Set collects the capabilities of a server.
type Set struct {
	server string
	items  []Capability
}

// New starts the set of the named server.
func New(server string) *Set { return &Set{server: server} }

// Add puts a capability in, as it is.
func (s *Set) Add(c Capability) *Set {
	s.items = append(s.items, c)
	return s
}

// Switch adds a capability that is on or off by one setting.
func (s *Set) Switch(id string, on bool, reason, configure string) *Set {
	if on {
		return s.Add(Capability{ID: id, Status: On})
	}
	return s.Add(Capability{ID: id, Status: NeedsConfig, Reason: reason, Configure: configure})
}

// Absent adds a capability this server does not have at all.
func (s *Set) Absent(id, reason string) *Set {
	return s.Add(Capability{ID: id, Status: Off, Reason: reason})
}

// Document is the sorted answer.
func (s *Set) Document() Document {
	out := append([]Capability{}, s.items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return Document{Server: s.server, Capabilities: out}
}
