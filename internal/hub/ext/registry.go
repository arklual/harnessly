package ext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Spec registers one adapter. It is what the hub's extensions file lists (HUB_EXTENSIONS_FILE, a JSON array).
type Spec struct {
	// Name is the adapter's name in the hub: lowercase letters, digits and dashes, unique.
	Name string `json:"name"`
	// Kind is what the adapter is for: machine-provider, credential-provider and so on. A label for people and the
	// capabilities API; the hub takes the meaning from Protocol.
	Kind string `json:"kind"`
	// URL is the adapter's address.
	URL string `json:"url"`
	// TokenFile is a file with the bearer token; the hub reads it again when it changes.
	TokenFile string `json:"tokenFile,omitempty"`
	// Protocol is the protocol version the hub expects, `harnessly.<kind>/v<major>`. An adapter that answers describe
	// with another one is refused.
	Protocol string `json:"protocol"`
	// TimeoutSeconds bounds one attempt of a call; zero means 15.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

var (
	nameRE     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
	protocolRE = regexp.MustCompile(`^harnessly\.[a-z][a-z0-9-]*/v[1-9][0-9]*$`)
)

// Validate says what is wrong with the spec.
func (s Spec) Validate() error {
	switch {
	case !nameRE.MatchString(s.Name):
		return fmt.Errorf("extension %q: the name is lowercase letters, digits and dashes", s.Name)
	case s.URL == "":
		return fmt.Errorf("extension %q: no url", s.Name)
	case !protocolRE.MatchString(s.Protocol):
		return fmt.Errorf("extension %q: protocol %q is not harnessly.<kind>/v<major>", s.Name, s.Protocol)
	case s.TimeoutSeconds < 0:
		return fmt.Errorf("extension %q: timeoutSeconds is negative", s.Name)
	}
	return nil
}

// LoadSpecs reads the extensions file.
func LoadSpecs(path string) ([]Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("extensions file: %w", err)
	}
	var specs []Spec
	if err := json.Unmarshal(b, &specs); err != nil {
		return nil, fmt.Errorf("extensions file %s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, s := range specs {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("extensions file: the name %q is listed twice", s.Name)
		}
		seen[s.Name] = true
	}
	return specs, nil
}

// Health of an adapter as the last describe saw it.
const (
	// HealthPending: not asked yet.
	HealthPending = "pending"
	// HealthOK: describe answered and the protocol is the expected one.
	HealthOK = "ok"
	// HealthMismatch: it answered but speaks another protocol than the hub expects.
	HealthMismatch = "protocol_mismatch"
	// HealthDown: it did not answer, or the circuit is open.
	HealthDown = "unreachable"
)

// Status is what the registry knows of one adapter now.
type Status struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Expected string `json:"expected"`
	Health   string `json:"health"`
	// Error is why the health is not ok.
	Error string `json:"error,omitempty"`
	// Description is the last good describe.
	Description *Description `json:"description,omitempty"`
	// Circuit is closed, open or half-open.
	Circuit   BreakerState `json:"circuit"`
	CheckedAt *time.Time   `json:"checkedAt,omitempty"`
}

type entry struct {
	spec   Spec
	client *Client
	st     Status
}

// Registry holds the registered adapters and their state. The state is fresh as of the last Check.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
	order   []string
	now     func() time.Time
}

// NewRegistry makes the clients of the specs. A wrong address or an unreadable token file stops here.
func NewRegistry(specs []Spec, base Config) (*Registry, error) {
	r := &Registry{entries: map[string]*entry{}, now: time.Now}
	for _, s := range specs {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if _, dup := r.entries[s.Name]; dup {
			return nil, fmt.Errorf("extension %q is listed twice", s.Name)
		}
		c := base
		c.BaseURL, c.TokenFile = s.URL, s.TokenFile
		if s.TimeoutSeconds > 0 {
			c.Timeout = time.Duration(s.TimeoutSeconds) * time.Second
		}
		cl, err := NewClient(c)
		if err != nil {
			return nil, fmt.Errorf("extension %q: %w", s.Name, err)
		}
		r.entries[s.Name] = &entry{spec: s, client: cl, st: Status{
			Name: s.Name, Kind: s.Kind, URL: s.URL, Expected: s.Protocol, Health: HealthPending, Circuit: StateClosed,
		}}
		r.order = append(r.order, s.Name)
	}
	sort.Strings(r.order)
	return r, nil
}

// Client is the client of an adapter by name, for the code that speaks its kind of protocol.
func (r *Registry) Client(name string) (*Client, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	return e.client, true
}

// Check asks every adapter to describe itself and records the answer. It does not return an error: an adapter that is
// down is a state, not a failure of the hub.
func (r *Registry) Check(ctx context.Context) {
	var wg sync.WaitGroup
	r.mu.Lock()
	list := make([]*entry, 0, len(r.order))
	for _, n := range r.order {
		list = append(list, r.entries[n])
	}
	r.mu.Unlock()
	for _, e := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := e.client.Describe(ctx, e.spec.Protocol)
			r.mu.Lock()
			defer r.mu.Unlock()
			at := r.now()
			e.st.CheckedAt = &at
			switch {
			case err == nil:
				e.st.Health, e.st.Error, e.st.Description = HealthOK, "", &d
			default:
				var pe *Error
				if errors.As(err, &pe) && pe.Code == "protocol_unsupported" {
					e.st.Health = HealthMismatch
				} else {
					e.st.Health = HealthDown
				}
				e.st.Error = err.Error()
			}
		}()
	}
	wg.Wait()
}

// CheckEvery is how often the hub asks the adapters to describe themselves again.
const CheckEvery = 30 * time.Second

// Run checks now and then every interval, until ctx ends.
func (r *Registry) Run(ctx context.Context, interval time.Duration) {
	r.Check(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Check(ctx)
		}
	}
}

// Statuses is the state of every adapter, sorted by name.
func (r *Registry) Statuses() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Status, 0, len(r.order))
	for _, n := range r.order {
		e := r.entries[n]
		st := e.st
		if b := e.client.Breaker(); b != nil {
			st.Circuit = b.State()
		}
		out = append(out, st)
	}
	return out
}

// Summary is one line per adapter, for `hub doctor`.
func Summary(sts []Status) string {
	var b strings.Builder
	for _, s := range sts {
		v := ""
		if s.Description != nil {
			v = " " + s.Description.Name + " " + s.Description.Version
		}
		fmt.Fprintf(&b, "%-8s %s [%s] %s%s", strings.ToUpper(s.Health), s.Name, s.Kind, s.URL, v)
		if s.Circuit != StateClosed {
			fmt.Fprintf(&b, " circuit=%s", s.Circuit)
		}
		if s.Error != "" {
			b.WriteString(": " + s.Error)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
