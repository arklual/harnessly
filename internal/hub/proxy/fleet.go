package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// FeatureGate says whether an organization has a feature on (features.Gate).
type FeatureGate interface {
	On(ctx context.Context, orgID, name string) bool
}

// The fleet is one runtime for all of a person's machines (flag runtime.fleet): the runtime asks the hub for the list
// of machines (GET /api/v2/machines, in the runtime's own Machine format; GET /api/me/machines, with the organizations)
// and sends everything else under /api/v2 with ?machine=<id> to that machine through its agent tunnel. Every way in
// passes admit, the rule of /m/<id>/…: a machine the person has no access to is not listed and is refused.
var errFleetOff = httpx.New(http.StatusNotFound, "not_enabled", "Один рантайм для всех машин не включён")

func (s *Service) mountFleet(mux *http.ServeMux) {
	mux.Handle("GET /api/me/machines", s.auth.Require(s.meMachines))
	mux.Handle("/api/v2/", http.HandlerFunc(s.serveFleet))
}

// FleetMachine is a machine of the person in any of their organizations.
type FleetMachine struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Online     bool   `json:"online"`
	Status     string `json:"status"`
	LastSeenAt *int64 `json:"lastSeenAt"`
	OrgID      string `json:"orgId"`
	OrgName    string `json:"orgName"`
	TenantID   string `json:"tenantId"`
	Tenant     string `json:"tenant"`
	// Mine: the machine is the person's own (they added it, or it was assigned to them). The others are there because
	// the person administers their space or organization: they open them on purpose, their chats never join the
	// person's own list (docs/hub/machines.md, «Personal and administered machines»).
	Mine bool `json:"mine"`
}

// fleetOn says whether the flag is on for the space's organization.
func (s *Service) fleetOn(ctx context.Context, sp store.Space) bool {
	return s.gate != nil && s.gate.On(ctx, store.OrgKey(sp), features.RuntimeFleet)
}

// fleetSpaces is every space the person may see machines of: the ones they are a member of and, for a person who
// manages an organization from above, its tenants they reach without being a member. With them, the organizations'
// names by id.
func (s *Service) fleetSpaces(ctx context.Context, userID string) (map[string]store.Space, map[string]string, error) {
	st := s.db.S()
	byID := map[string]store.Space{}
	mine, err := st.SpacesByUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	for _, x := range mine {
		byID[x.ID] = x.Space
	}
	orgs, err := st.OrgsForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	orgName := map[string]string{}
	for _, o := range orgs {
		orgName[o.ID] = o.Name
		sps, err := st.SpacesByOrg(ctx, o.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, sp := range sps {
			if _, seen := byID[sp.ID]; seen {
				continue
			}
			if _, err := s.spaces.Access(ctx, userID, sp.ID); err == nil {
				byID[sp.ID] = sp
			}
		}
	}
	return byID, orgName, nil
}

// fleetEvents is the workspace page's live channel (GET /api/v2/machines/events, events.FleetBus): it says when the
// answer of GET /api/v2/machines may have changed, and nothing else. Without the flag for any of the person's spaces
// there is no fleet and no stream, as with the list.
func (s *Service) fleetEvents(w http.ResponseWriter, r *http.Request, u store.User) {
	watched := func(ctx context.Context) ([]string, error) {
		byID, _, err := s.fleetSpaces(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		return ids, nil
	}
	if s.fleetBus == nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	byID, _, err := s.fleetSpaces(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	on := false
	ids := make([]string, 0, len(byID))
	for id, sp := range byID {
		ids = append(ids, id)
		on = on || s.fleetOn(r.Context(), sp)
	}
	if !on {
		httpx.WriteError(w, r, errFleetOff)
		return
	}
	sub := s.fleetBus.Subscribe(u.ID, ids)
	if sub == nil {
		httpx.WriteError(w, r, httpx.New(http.StatusServiceUnavailable, "unavailable", "Сервер перезапускается"))
		return
	}
	s.fleetBus.Serve(w, r, sub, watched)
}

// denied is a refusal (no access, an untrusted machine), as against a failure to find out (the database did not
// answer): a machine is left out of the list only for a refusal. Leaving it out for a failure made it vanish from the
// person's list until the next ask, and its chats and favorites with it.
func denied(err error) bool {
	var he *httpx.Error
	return errors.As(err, &he)
}

// fleet is every machine the person may open now, across the organizations, oldest first within a tenant. onHub keeps
// only the machines that may answer on the hub's own origin (the workspace, workspace.go), in either mode.
func (s *Service) fleet(ctx context.Context, userID string, onHub bool) ([]FleetMachine, error) {
	st := s.db.S()
	byID, orgName, err := s.fleetSpaces(ctx, userID)
	if err != nil {
		return nil, err
	}
	spaces := make([]store.Space, 0, len(byID))
	for _, sp := range byID {
		spaces = append(spaces, sp)
	}
	sort.Slice(spaces, func(i, j int) bool {
		if !spaces[i].CreatedAt.Equal(spaces[j].CreatedAt) {
			return spaces[i].CreatedAt.Before(spaces[j].CreatedAt)
		}
		return spaces[i].ID < spaces[j].ID
	})
	out := []FleetMachine{}
	on := false
	for _, sp := range spaces {
		if !s.fleetOn(ctx, sp) {
			continue
		}
		on = true
		ms, err := st.MachinesBySpace(ctx, sp.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if m.DisabledAt != nil {
				continue
			}
			if _, err := s.admit(ctx, userID, m.ID); err != nil {
				if denied(err) {
					continue // no access, an untrusted machine: it is not there for this person
				}
				return nil, err
			}
			if onHub {
				if err := s.trustedOnHub(ctx, m); err != nil {
					if denied(err) {
						continue
					}
					return nil, err
				}
			}
			f := FleetMachine{
				ID: m.ID, Name: m.Name, Host: m.Hostname, OrgID: sp.OrgID, OrgName: orgName[sp.OrgID],
				TenantID: sp.ID, Tenant: sp.Name, Mine: m.CreatedBy != "" && m.CreatedBy == userID,
			}
			if f.OrgName == "" {
				f.OrgName = sp.Name
			}
			switch {
			case s.reg.Get(m.ID) != nil:
				f.Online, f.Status = true, "online"
			case m.LastSeenAt == nil:
				f.Status = "pending"
			default:
				f.Status = "offline"
			}
			if m.LastSeenAt != nil {
				ms := m.LastSeenAt.UnixMilli()
				f.LastSeenAt = &ms
			}
			out = append(out, f)
		}
	}
	if !on {
		return nil, errFleetOff
	}
	return out, nil
}

func (s *Service) meMachines(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	list, err := s.fleet(r.Context(), p.User.ID, false)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machines": list})
	return nil
}

// v2Machine is the runtime's Machine (web/app/next/protocol/v2.ts) plus where the machine belongs. HRN-1036: its agents
// are what the machine last said of them (machine_cards), whatever it does now; features and usage too, and hangs says
// that it is on but its runtime did not answer a moment ago. The runtime takes the machine's own welcome over them.
type v2Machine struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Host       string          `json:"host"`
	Reach      string          `json:"reach"`
	LastSeenAt *int64          `json:"lastSeenAt"`
	Runtime    any             `json:"runtime"`
	Agents     json.RawMessage `json:"agents"`
	Features   []wire.Feature  `json:"features,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	Hangs      bool            `json:"hangs,omitempty"`
	OrgID      string          `json:"orgId"`
	OrgName    string          `json:"orgName"`
	Tenant     string          `json:"tenant"`
	Mine       bool            `json:"mine"`
}

func (s *Service) v2List(w http.ResponseWriter, r *http.Request, u store.User, onHub bool) {
	list, err := s.fleet(r.Context(), u.ID, onHub)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids := make([]string, len(list))
	for i, f := range list {
		ids[i] = f.ID
	}
	cards, err := s.cards(r.Context(), ids)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]v2Machine, 0, len(list))
	for _, f := range list {
		reach := "unknown"
		switch f.Status {
		case "online":
			reach = "online"
		case "offline":
			reach = "unreachable"
		}
		c := cards[f.ID]
		agents := c.agents
		if agents == nil {
			agents = json.RawMessage("[]")
		}
		out = append(out, v2Machine{ID: f.ID, Name: f.Name, Host: f.Host, Reach: reach, LastSeenAt: f.LastSeenAt, Agents: agents,
			Features: c.features(), Usage: c.usage, Hangs: f.Online && s.hangs(f.ID),
			OrgID: f.OrgID, OrgName: f.OrgName, Tenant: f.Tenant, Mine: f.Mine})
	}
	s.refreshCards(r.Host, u, list)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

// clusterPath: the paths of /api/v2 that belong to the person and not to a machine (web: clusterApi).
func clusterPath(p string) bool {
	for _, c := range []string{"/api/v2/me", "/api/v2/search", "/api/v2/vault", "/api/v2/push"} {
		if p == c || strings.HasPrefix(p, c+"/") {
			return true
		}
	}
	return false
}

// home is the machine the cluster's paths go to: the first of the person's own machines that is on, else the first of
// them. Only a person with no machine of their own falls back on the machines they administer.
func (s *Service) home(ctx context.Context, userID string, onHub bool) string {
	f, ok := s.homeOf(ctx, userID, onHub)
	if !ok {
		return ""
	}
	return f.ID
}

func (s *Service) homeOf(ctx context.Context, userID string, onHub bool) (FleetMachine, bool) {
	list, err := s.fleet(ctx, userID, onHub)
	if err != nil || len(list) == 0 {
		return FleetMachine{}, false
	}
	if own := personal(list); len(own) > 0 {
		list = own
	}
	// a machine that is on but did not give its page a moment ago (its runtime hangs while its agent keeps the
	// link) answers for the person only when no other machine of theirs is on.
	first := -1
	for i, f := range list {
		if !f.Online {
			continue
		}
		if !s.hangs(f.ID) {
			return f, true
		}
		if first < 0 {
			first = i
		}
	}
	if first >= 0 {
		return list[first], true
	}
	return list[0], true
}

// personal keeps the person's own machines of a fleet list.
func personal(list []FleetMachine) []FleetMachine {
	var out []FleetMachine
	for _, f := range list {
		if f.Mine {
			out = append(out, f)
		}
	}
	return out
}

// hangs: the machine did not give its page settings when last asked, a moment ago (machineSettings), or its card when
// last asked (askCard).
func (s *Service) hangs(id string) bool {
	s.mu.Lock()
	c, ok := s.pages[id]
	settings := ok && c.missed && s.now().Before(c.until)
	s.mu.Unlock()
	return settings || s.cardHangs(id)
}

// serveFleet answers /api/v2/…: the list of machines itself, anything else for the machine named by ?machine=.
func (s *Service) serveFleet(w http.ResponseWriter, r *http.Request) {
	if s.gate == nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if refuseCrossSite(w, r) {
		return
	}
	p, err := s.auth.Authenticate(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p == nil {
		httpx.WriteError(w, r, httpx.ErrUnauthenticated)
		return
	}
	s.fleetCall(w, r, p.User, false)
}

// fleetCall answers /api/v2/… for a signed-in person; onHub is the workspace's way in (workspace.go), where only the
// machines that may answer on the hub's own origin are there.
func (s *Service) fleetCall(w http.ResponseWriter, r *http.Request, u store.User, onHub bool) {
	if r.URL.Path == "/api/v2/chat-move" || strings.HasPrefix(r.URL.Path, "/api/v2/chat-move/") {
		s.serveMove(w, r, u)
		return
	}
	if me, settings := mePath(r.URL.Path); me {
		// The person's settings are the hub user's, whatever machine is named: they never go to a runtime.
		if _, err := s.fleet(r.Context(), u.ID, onHub); err != nil {
			s.fail(w, r, err)
			return
		}
		s.serveMe(w, r, u, settings)
		return
	}
	q := r.URL.Query()
	id := q.Get("machine")
	if r.URL.Path == "/api/v2/machines/events" && id == "" {
		if r.Method != http.MethodGet {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		s.fleetEvents(w, r, u)
		return
	}
	if r.URL.Path == "/api/v2/machines" && id == "" {
		if r.Method != http.MethodGet {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		s.v2List(w, r, u, onHub)
		return
	}
	if id == "" && clusterPath(r.URL.Path) {
		// The cluster's own paths (the person, search, the vault) have no machine in the address: the runtime of the
		// person's first machine that is on answers them, the one a standalone runtime would.
		f, ok := s.homeOf(r.Context(), u.ID, onHub)
		// A search goes over every chat of the machine that answers it: only a machine of the person's own may.
		if !ok || (!f.Mine && (r.URL.Path == "/api/v2/search" || strings.HasPrefix(r.URL.Path, "/api/v2/search/"))) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		id = f.ID
	}
	if !machineID.MatchString(id) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	m, err := s.admit(r.Context(), u.ID, id)
	if err == nil && onHub {
		err = s.trustedOnHub(r.Context(), m)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sp, err := s.db.S().SpaceByID(r.Context(), m.SpaceID)
	if err != nil || !s.fleetOn(r.Context(), sp) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	// The machine's runtime knows nothing of which machine it is asked for: the address is its own.
	clone := r.Clone(r.Context())
	target := url.URL{Path: strings.TrimSuffix(r.URL.Path, "/"), RawPath: ""}
	q.Del("machine")
	target.RawQuery = q.Encode()
	clone.URL = &target
	clone.RequestURI = ""
	s.forward(w, clone, u, m)
}
