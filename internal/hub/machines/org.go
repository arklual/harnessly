package machines

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The machines as the organization's screens see them (docs/hub/analytics.md, «Machines»): the list is served by the
// analytics package (it builds on HubMachines), the changes — team, access, switching off, restart — are here, next to
// the machine's own endpoints.

const (
	// runningWindow is how recently a chat must have moved to count as running.
	runningWindow = 15 * time.Minute
	// peopleDays is how far back «who used the machine» looks.
	peopleDays = 30
	// idleDays without a chat make a machine idle_week.
	idleDays = 7
	// diskLowShare and diskLowBytes: free disk below a tenth of the volume or below 5 GiB is disk_low.
	diskLowShare = 0.1
	diskLowBytes = 5 << 30
	// maxAccess is the most entries one machine's access list takes.
	maxAccess = 200
)

// Machine problems of the list (MachineProblem of web/app/hub-api/analytics.ts).
const (
	ProblemOffline      = "offline"
	ProblemIdleWeek     = "idle_week"
	ProblemAgentOld     = "agent_old"
	ProblemDiskLow      = "disk_low"
	ProblemLoginExpired = "login_expired"
	ProblemDisabled     = "disabled"
)

var (
	// errDisabledMachine is what a switched-off machine answers to anything that needs it.
	errDisabledMachine = httpx.New(http.StatusLocked, "machine_disabled", "Машина выключена администратором. Включите её, чтобы открыть или перезапустить")
	errObserveOnly     = httpx.New(http.StatusConflict, "observe_only", "Хаб только наблюдает за этой машиной: её чаты открываются там, где она работает")
	errRestartVM       = httpx.New(http.StatusNotImplemented, "not_implemented", "Перезапуск ВМ пока недоступен")
	errRestartOld      = httpx.New(http.StatusUnprocessableEntity, "restart_unsupported", "Эта версия агента не умеет перезапускаться: обновите агента")
)

// AccessView is one entry of a machine's access list.
type AccessView struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// HubMachine is HubMachine of web/app/hub-api/analytics.ts: a machine of an organization.
type HubMachine struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Hostname     string           `json:"hostname"`
	TenantID     string           `json:"tenantId"`
	TeamID       *string          `json:"teamId"`
	Status       string           `json:"status"`
	DisabledAt   *string          `json:"disabledAt"`
	AgentVersion string           `json:"agentVersion"`
	LastSeenAt   *string          `json:"lastSeenAt"`
	LastActiveAt *string          `json:"lastActiveAt"`
	ChatsRunning int              `json:"chatsRunning"`
	People       []orgs.PersonRef `json:"people"`
	Access       []AccessView     `json:"access"`
	Problems     []string         `json:"problems"`
	// OpensRuntime is false for a machine whose own policy keeps the hub out of its runtime (an observed machine run by
	// a cluster of its own): the hub lists it, but «Открыть» has nowhere to lead.
	OpensRuntime bool `json:"opensRuntime"`
	// Cloud is what the machine runs on, as its provider says; null when the hub knows no server of it.
	Cloud *CloudView `json:"cloud"`
	// Hardware is what the machine said it is made of; null when it said nothing.
	Hardware *HardwareView `json:"hardware"`
	// Load is the machine's load now; null when it is not connected or sent no reading.
	Load *LoadView `json:"load"`
	// Week is the machine's calls over the last seven days; null when it made none.
	Week *WeekView `json:"week"`
	// Managed says which parts of the machine the hub controls; null while the resources feature is off.
	Managed *ManagedView `json:"managed"`
	// ActiveOps are the operations running on the machine.
	ActiveOps []OpView `json:"activeOps"`
}

// orgsOn says whether the person gets the organizations' API (HUB_ORGS: off is nobody).
func (s *Service) orgsOn(_ *auth.Principal) bool {
	return s.cfg.Orgs == config.OrgsOn
}

// orgHandler wraps an endpoint of the organizations' screens in the session check and HUB_ORGS.
func (s *Service) orgHandler(h orgs.Handler) httpx.Handler {
	return s.auth.Require(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		if !s.orgsOn(p) {
			return httpx.ErrNotFound
		}
		return h(w, r, p)
	})
}

// mountOrg registers the endpoints that are the organization's alone.
func (s *Service) mountOrg(mux *http.ServeMux) {
	mux.Handle("POST /api/machines/{machineId}/disable", s.orgHandler(s.disable))
	mux.Handle("POST /api/machines/{machineId}/enable", s.orgHandler(s.enable))
	mux.Handle("POST /api/machines/{machineId}/restart", s.orgHandler(s.restart))
	mux.Handle("POST /api/machines/{machineId}/move", s.orgHandler(s.move))
	s.mountResources(mux)
}

// machineOf finds a live machine that sits in a tenant. An id that is no machine and a machine of no tenant are the same
// forbidden, as everywhere else.
func machineOf(ctx context.Context, st store.Store, id string) (store.Machine, error) {
	m, err := st.MachineByID(ctx, id)
	if store.IsNotFound(err) {
		return store.Machine{}, httpx.ErrForbidden
	}
	if err != nil {
		return store.Machine{}, err
	}
	if m.SpaceID == "" {
		return store.Machine{}, httpx.ErrForbidden
	}
	if m.RevokedAt != nil {
		return store.Machine{}, httpx.ErrNotFound
	}
	return m, nil
}

func tenantOf(m store.Machine) authz.Scope {
	return authz.Scope{Kind: store.ScopeTenant, ID: m.SpaceID}
}

// manage checks the person manages the machine's tenant.
func manage(ctx context.Context, st store.Store, p *auth.Principal, m store.Machine) error {
	_, err := authz.New(st).Can(ctx, orgs.Principal(p), authz.Check{Action: authz.Manage, Scope: tenantOf(m)})
	return err
}

// journal writes the machine's change into the journal: at its team when it has one, else at its tenant.
func (s *Service) journal(ctx context.Context, st store.Store, p *auth.Principal, m store.Machine, action string, before, after any) error {
	return s.journalBy(ctx, st, p.User.ID, m, action, before, after)
}

// journalBy is journal for a change made by the person with the id.
func (s *Service) journalBy(ctx context.Context, st store.Store, userID string, m store.Machine, action string, before, after any) error {
	sc := tenantOf(m)
	if m.TeamID != "" {
		sc = authz.Scope{Kind: store.ScopeTeam, ID: m.TeamID}
	}
	chain, err := authz.New(st).Chain(ctx, sc)
	if err != nil {
		return err
	}
	return st.AddAudit(ctx, orgs.Journal(chain, userID, action, "machine", m.ID, before, after, s.now().UTC()))
}

// HubMachines builds the organization's form of the machines, in the order given.
func (s *Service) HubMachines(ctx context.Context, st store.Store, ms []store.Machine) ([]HubMachine, error) {
	now := s.now()
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	uses, err := st.MachineUses(ctx, ids, now.Add(-runningWindow).UnixMilli())
	if err != nil {
		return nil, err
	}
	acc, err := st.MachineAccess(ctx, ids)
	if err != nil {
		return nil, err
	}
	useBy := map[string][]store.MachineUse{}
	for _, u := range uses {
		useBy[u.MachineID] = append(useBy[u.MachineID], u)
	}
	accBy := map[string][]store.MachineAccessEntry{}
	for _, e := range acc {
		accBy[e.MachineID] = append(accBy[e.MachineID], e)
	}
	lb := newLabels(ctx, st)
	since := now.AddDate(0, 0, -peopleDays).UnixMilli()
	out := make([]HubMachine, 0, len(ms))
	for _, m := range ms {
		h := HubMachine{ID: m.ID, Name: m.Name, Hostname: m.Hostname, TenantID: m.SpaceID, Status: s.status(m), AgentVersion: m.AgentVersion,
			LastSeenAt: httpx.TimePtr(m.LastSeenAt), DisabledAt: httpx.TimePtr(m.DisabledAt), People: []orgs.PersonRef{}, Access: []AccessView{}, Problems: []string{}}
		if m.TeamID != "" {
			id := m.TeamID
			h.TeamID = &id
		}
		var last int64
		seen := map[string]bool{}
		for _, u := range useBy[m.ID] {
			if u.Origin != "live" {
				continue
			}
			h.ChatsRunning += u.Running
			last = max(last, u.Last)
			if u.UserID != "" && u.Last >= since && !seen[u.UserID] {
				seen[u.UserID] = true
				if name, ok := lb.user(u.UserID); ok {
					h.People = append(h.People, orgs.PersonRef{UserID: u.UserID, Name: name})
				}
			}
		}
		slices.SortFunc(h.People, func(a, b orgs.PersonRef) int {
			return strings.Compare(strings.ToLower(a.Name)+a.UserID, strings.ToLower(b.Name)+b.UserID)
		})
		if last > 0 {
			t := httpx.Time(time.UnixMilli(last))
			h.LastActiveAt = &t
		}
		for _, e := range accBy[m.ID] {
			label, ok := lb.access(e.Kind, e.RefID)
			if !ok {
				continue
			}
			h.Access = append(h.Access, AccessView{Kind: e.Kind, ID: e.RefID, Label: label})
		}
		h.Problems = s.problems(m, h.Status, last, now)
		h.OpensRuntime = opensRuntime(m)
		h.ActiveOps = []OpView{}
		out = append(out, h)
	}
	if err := s.enrich(ctx, st, ms, out, now); err != nil {
		return nil, err
	}
	return out, nil
}

// problems lists what is wrong with a machine. A switched-off machine has the one problem it was given on purpose.
func (s *Service) problems(m store.Machine, status string, lastActive int64, now time.Time) []string {
	out := []string{}
	if m.DisabledAt != nil {
		return append(out, ProblemDisabled)
	}
	if status == "offline" {
		out = append(out, ProblemOffline)
	}
	if status != "pending" {
		idleSince := now.AddDate(0, 0, -idleDays)
		if m.CreatedAt.Before(idleSince) && (lastActive == 0 || time.UnixMilli(lastActive).Before(idleSince)) {
			out = append(out, ProblemIdleWeek)
		}
	}
	if oldest := s.cfg.MinAgentVersion; oldest != "" && m.AgentVersion != "" && link.OlderThan(m.AgentVersion, oldest) {
		out = append(out, ProblemAgentOld)
	}
	var in struct {
		Preflight *hubproto.PreflightReport `json:"preflight"`
		Agents    []hubproto.AgentStatus    `json:"agents"`
	}
	if json.Unmarshal([]byte(m.Info), &in) == nil {
		if d := in.Preflight; d != nil && d.Disk.Total > 0 && (d.Disk.Free < diskLowBytes || float64(d.Disk.Free) < float64(d.Disk.Total)*diskLowShare) {
			out = append(out, ProblemDiskLow)
		}
		for _, a := range in.Agents {
			if a.Installed && a.Login == hubproto.LoggedOut {
				out = append(out, ProblemLoginExpired)
				break
			}
		}
	}
	return out
}

// labels names people and teams once.
type labels struct {
	ctx   context.Context
	st    store.Store
	users map[string]string
	teams map[string]string
}

func newLabels(ctx context.Context, st store.Store) *labels {
	return &labels{ctx: ctx, st: st, users: map[string]string{}, teams: map[string]string{}}
}

// user is a person's name (their e-mail when they have no name); false when they are gone.
func (l *labels) user(id string) (string, bool) {
	if v, ok := l.users[id]; ok {
		return v, v != ""
	}
	name := ""
	if u, err := l.st.UserByID(l.ctx, id); err == nil {
		if name = u.Name; name == "" {
			name = u.Email
		}
	}
	l.users[id] = name
	return name, name != ""
}

func (l *labels) team(id string) (string, bool) {
	if v, ok := l.teams[id]; ok {
		return v, v != ""
	}
	name := ""
	if t, err := l.st.TeamByID(l.ctx, id); err == nil {
		name = t.Name
	}
	l.teams[id] = name
	return name, name != ""
}

func (l *labels) access(kind, id string) (string, bool) {
	if kind == store.AccessTeam {
		return l.team(id)
	}
	return l.user(id)
}

// view builds the one machine's HubMachine.
func (s *Service) hubMachine(ctx context.Context, st store.Store, id string) (HubMachine, error) {
	m, err := st.MachineByID(ctx, id)
	if err != nil {
		return HubMachine{}, err
	}
	hs, err := s.HubMachines(ctx, st, []store.Machine{m})
	if err != nil {
		return HubMachine{}, err
	}
	return hs[0], nil
}

// cleanMachineName is a machine's name: 1 to 60 characters, one line.
func cleanMachineName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if n := len([]rune(name)); n == 0 || n > 60 {
		return "", httpx.Invalid("name", "Название — от 1 до 60 символов")
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return "", httpx.Invalid("name", "В названии есть недопустимые символы")
		}
	}
	return name, nil
}

// accessRef is one entry of the access list of a PATCH.
type accessRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// patchRequest is the body of PATCH /api/machines/{id}. TeamID keeps the difference between absent (nil) and null.
type patchRequest struct {
	Name   *string         `json:"name"`
	TeamID json.RawMessage `json:"teamId"`
	Access *[]accessRef    `json:"access"`
}

// patchOrg is the PATCH that changes more than the name: the team and who may use the machine. Whoever manages the
// machine's tenant may change all of it; a lead of the machine's team may change the access alone.
func (s *Service) patchOrg(w http.ResponseWriter, r *http.Request, p *auth.Principal, req patchRequest) error {
	if !s.orgsOn(p) {
		return httpx.ErrNotFound
	}
	ctx := r.Context()
	var name string
	if req.Name != nil {
		var err error
		if name, err = cleanMachineName(*req.Name); err != nil {
			return err
		}
	}
	var teamID string
	if req.TeamID != nil && string(req.TeamID) != "null" {
		if err := json.Unmarshal(req.TeamID, &teamID); err != nil || teamID == "" {
			return httpx.Invalid("teamId", "teamId — идентификатор команды или null")
		}
	}
	var want []store.MachineAccessEntry
	if req.Access != nil {
		if len(*req.Access) > maxAccess {
			return httpx.Invalid("access", "Слишком длинный список доступа")
		}
		seen := map[string]bool{}
		for _, a := range *req.Access {
			if (a.Kind != store.AccessUser && a.Kind != store.AccessTeam) || a.ID == "" {
				return httpx.Invalid("access", "Запись доступа — {kind: user|team, id}")
			}
			if k := a.Kind + ":" + a.ID; !seen[k] {
				seen[k] = true
				want = append(want, store.MachineAccessEntry{Kind: a.Kind, RefID: a.ID})
			}
		}
	}
	id := r.PathValue("machineId")
	err := s.db.Tx(ctx, func(st store.Store) error {
		m, err := machineOf(ctx, st, id)
		if err != nil {
			return err
		}
		if err := manage(ctx, st, p, m); err != nil {
			// The lead of the machine's team changes who may use it, and nothing else.
			var he *httpx.Error
			if !errors.As(err, &he) || he.Status != http.StatusForbidden || req.Name != nil || req.TeamID != nil || m.TeamID == "" {
				return err
			}
			if _, terr := authz.New(st).Can(ctx, orgs.Principal(p), authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeTeam, ID: m.TeamID}}); terr != nil {
				return err
			}
		}
		if req.Name != nil && name != m.Name {
			if err := st.RenameMachine(ctx, m.ID, name); err != nil {
				return err
			}
		}
		if req.TeamID != nil && teamID != m.TeamID {
			if teamID != "" {
				t, err := st.TeamByID(ctx, teamID)
				if store.IsNotFound(err) || (err == nil && t.SpaceID != m.SpaceID) {
					return httpx.Invalid("teamId", "Команда должна быть из тенанта машины")
				}
				if err != nil {
					return err
				}
			}
			if err := st.SetMachineTeam(ctx, m.ID, teamID); err != nil {
				return err
			}
			was := m.TeamID
			m.TeamID = teamID
			if err := s.journal(ctx, st, p, m, "machine.team", map[string]any{"teamId": nilIfEmpty(was)}, map[string]any{"teamId": nilIfEmpty(teamID)}); err != nil {
				return err
			}
		}
		if req.Access != nil {
			if err := s.checkAccess(ctx, st, m, want); err != nil {
				return err
			}
			cur, err := st.MachineAccess(ctx, []string{m.ID})
			if err != nil {
				return err
			}
			if !sameAccess(cur, want) {
				if err := st.SetMachineAccess(ctx, m.ID, want, s.now().UTC()); err != nil {
					return err
				}
				if err := s.journal(ctx, st, p, m, "machine.access", map[string]any{"access": accessJSON(cur)}, map[string]any{"access": accessJSON(want)}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	st := s.db.S()
	h, err := s.hubMachine(ctx, st, id)
	if err != nil {
		return err
	}
	if m, err := st.MachineByID(ctx, id); err == nil {
		s.Publish(ctx, m)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machine": h})
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// checkAccess says every entry is a person of the machine's tenant or a team of it.
func (s *Service) checkAccess(ctx context.Context, st store.Store, m store.Machine, entries []store.MachineAccessEntry) error {
	for _, e := range entries {
		switch e.Kind {
		case store.AccessTeam:
			t, err := st.TeamByID(ctx, e.RefID)
			if store.IsNotFound(err) || (err == nil && t.SpaceID != m.SpaceID) {
				return httpx.Invalid("access", "Команда должна быть из тенанта машины")
			}
			if err != nil {
				return err
			}
		case store.AccessUser:
			role, err := st.Role(ctx, m.SpaceID, e.RefID)
			if err != nil {
				return err
			}
			if role == "" {
				return httpx.Invalid("access", "Человек должен быть из тенанта машины")
			}
		}
	}
	return nil
}

func sameAccess(cur, want []store.MachineAccessEntry) bool {
	if len(cur) != len(want) {
		return false
	}
	have := map[string]bool{}
	for _, e := range cur {
		have[e.Kind+":"+e.RefID] = true
	}
	for _, e := range want {
		if !have[e.Kind+":"+e.RefID] {
			return false
		}
	}
	return true
}

func accessJSON(es []store.MachineAccessEntry) []accessRef {
	out := make([]accessRef, 0, len(es))
	for _, e := range es {
		out = append(out, accessRef{Kind: e.Kind, ID: e.RefID})
	}
	return out
}

func (s *Service) disable(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	return s.setDisabled(w, r, p, true)
}

func (s *Service) enable(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	return s.setDisabled(w, r, p, false)
}

// setDisabled switches a machine off or on. Off: its link closes with the code that makes the agent wait and dial again,
// the dial is refused (423) while it is off, and its key is kept. On: the agent's next dial is admitted with the same
// files.
func (s *Service) setDisabled(w http.ResponseWriter, r *http.Request, p *auth.Principal, off bool) error {
	ctx := r.Context()
	id := r.PathValue("machineId")
	changed := false
	err := s.db.Tx(ctx, func(st store.Store) error {
		m, err := machineOf(ctx, st, id)
		if err != nil {
			return err
		}
		if err := manage(ctx, st, p, m); err != nil {
			return err
		}
		if (m.DisabledAt != nil) == off {
			return nil
		}
		changed = true
		action := "machine.enable"
		var at *time.Time
		if off {
			now := s.now().UTC()
			at, action = &now, "machine.disable"
		}
		if err := st.SetMachineDisabled(ctx, m.ID, at); err != nil {
			return err
		}
		return s.journal(ctx, st, p, m, action, nil, nil)
	})
	if err != nil {
		return err
	}
	if off {
		s.reg.Disable(id)
	}
	st := s.db.S()
	m, err := st.MachineByID(ctx, id)
	if err != nil {
		return err
	}
	// With the resources feature on and the server managed by the hub, switching the machine off or on also switches its
	// server (the provider's power operations).
	var power *store.MachineOp
	if changed {
		power = s.powerOp(ctx, m, p.User.ID, off)
	}
	h, err := s.hubMachine(ctx, st, id)
	if err != nil {
		return err
	}
	s.Publish(ctx, m)
	out := map[string]any{"machine": h}
	if power != nil {
		out["op"] = s.opView(*power)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// restart restarts a machine. For an organization without the resources feature that is the runner (the agent's
// runtime.restart) now or when its chats are done; a restart of the server needs the provider, which that organization
// does not have. With the feature on the restart is an operation (restartOn): the runner or the whole machine, now or
// after a drain.
func (s *Service) restart(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req restartRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	m, err := machineOf(ctx, s.db.S(), r.PathValue("machineId"))
	if err != nil {
		return err
	}
	if err := manage(ctx, s.db.S(), p, m); err != nil {
		return err
	}
	if s.on(ctx, m.SpaceID) {
		return s.restartOn(w, r, p, m, req)
	}
	if req.What != "runner" && req.What != "vm" {
		return httpx.Invalid("what", "what — runner или vm")
	}
	if req.When == "" {
		req.When = "now"
	}
	if req.When != "now" && req.When != "after_chats" {
		return httpx.Invalid("when", "when — now или after_chats")
	}
	if req.What == "vm" {
		return errRestartVM
	}
	if m.DisabledAt != nil {
		return errDisabledMachine
	}
	jobID := store.RandomHex(16)
	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpRuntimeRestart, map[string]any{"jobId": jobID, "when": req.When}, callTimeout); err != nil {
		var he *httpx.Error
		if errors.As(err, &he) && he.Code == "provider_off" {
			return errRestartOld
		}
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_restart", r, m.ID+" "+req.What+" "+req.When)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
	return nil
}
