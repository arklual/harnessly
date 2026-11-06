package env

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	callTimeout     = 15 * time.Second
	defaultRunLimit = 50
	maxRunLimit     = 200
)

// NameRef is a machine or a person as the run rows name them.
type NameRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EnvStepView is a step of a run; Log only on a single run.
type EnvStepView struct {
	ItemID  string  `json:"itemId"`
	Kind    string  `json:"kind"`
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	Message *string `json:"message"`
	Exit    *int    `json:"exit"`
	Ms      int64   `json:"ms"`
	Log     string  `json:"log,omitempty"`
}

// EnvRunRow is a run as the screens show it.
type EnvRunRow struct {
	ID         string        `json:"id"`
	Machine    NameRef       `json:"machine"`
	Person     NameRef       `json:"person"`
	Mode       string        `json:"mode"`
	By         *string       `json:"by"`
	Status     string        `json:"status"`
	StartedAt  string        `json:"startedAt"`
	FinishedAt *string       `json:"finishedAt"`
	Steps      []EnvStepView `json:"steps"`
}

// runNames remembers the names of machines and people for the rows of one answer.
type runNames struct {
	ctx      context.Context
	st       store.Store
	machines map[string]string
	people   map[string]string
}

func newRunNames(ctx context.Context, st store.Store) *runNames {
	return &runNames{ctx: ctx, st: st, machines: map[string]string{}, people: map[string]string{}}
}

func (n *runNames) machine(id string) (string, error) {
	if v, ok := n.machines[id]; ok {
		return v, nil
	}
	m, err := n.st.MachineByID(n.ctx, id)
	if err != nil && !store.IsNotFound(err) {
		return "", err
	}
	n.machines[id] = m.Name // a machine that is gone has no name
	return m.Name, nil
}

func (n *runNames) person(id string) (string, error) {
	if v, ok := n.people[id]; ok {
		return v, nil
	}
	name := ""
	if id != "" {
		u, err := n.st.UserByID(n.ctx, id)
		if err != nil && !store.IsNotFound(err) {
			return "", err
		}
		name = cmp.Or(u.Name, u.Email)
	}
	n.people[id] = name
	return name, nil
}

func (n *runNames) row(r store.EnvRun, logs bool) (EnvRunRow, error) {
	mn, err := n.machine(r.MachineID)
	if err != nil {
		return EnvRunRow{}, err
	}
	pn, err := n.person(r.UserID)
	if err != nil {
		return EnvRunRow{}, err
	}
	out := EnvRunRow{ID: r.ID, Machine: NameRef{r.MachineID, mn}, Person: NameRef{r.UserID, pn}, Mode: r.Mode, Status: r.Status,
		StartedAt: httpx.Time(r.StartedAt), Steps: []EnvStepView{}}
	if r.ByName != "" {
		out.By = strp(r.ByName)
	}
	if !r.FinishedAt.IsZero() {
		out.FinishedAt = strp(httpx.Time(r.FinishedAt))
	}
	var steps []hubproto.EnvStep
	if err := json.Unmarshal([]byte(r.Steps), &steps); err != nil {
		return EnvRunRow{}, err
	}
	for _, s := range steps {
		v := EnvStepView{ItemID: s.ItemID, Kind: s.Kind, Name: s.Name, Status: s.Status, Exit: s.Exit, Ms: s.Ms}
		if s.Message != "" {
			v.Message = strp(s.Message)
		}
		if logs {
			v.Log = s.Log
		}
		out.Steps = append(out.Steps, v)
	}
	return out, nil
}

func runLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultRunLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, httpx.Invalid("limit", "Число от 1 до "+strconv.Itoa(maxRunLimit))
	}
	return min(n, maxRunLimit), nil
}

func (s *Service) runsSince() time.Time { return s.now().AddDate(0, 0, -hubproto.EnvRunKeepDays) }

// listRuns lists the runs of the machines a layer reaches, newest first.
func (s *Service) listRuns(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	q, err := s.open(r, s.db.S(), p, FeatureEnv)
	if err != nil {
		return err
	}
	qs := r.URL.Query()
	layer, scopeID := qs.Get("layer"), qs.Get("scopeId")
	if layer != store.EnvLayerOrg && layer != store.EnvLayerTenant && layer != store.EnvLayerTeam {
		return httpx.Invalid("layer", "org, tenant или team")
	}
	if !q.w.layerExists(layer, scopeID) {
		return httpx.ErrNotFound
	}
	if err := q.rt.require(scopeOf(layer, scopeID)); err != nil {
		return err
	}
	limit, err := runLimit(r)
	if err != nil {
		return err
	}
	rq := store.EnvRunQuery{OrgID: q.w.org.ID, Since: s.runsSince(), ErrorsOnly: qs.Get("errors") == "1", Limit: limit}
	switch layer {
	case store.EnvLayerTenant:
		rq.SpaceID = scopeID
	case store.EnvLayerTeam:
		rq.SpaceID, rq.TeamID = q.w.tenantOf(layer, scopeID), scopeID
	}
	rows, err := q.st.EnvRunsOf(q.ctx, rq)
	if err != nil {
		return err
	}
	return writeRuns(w, newRunNames(q.ctx, q.st), rows)
}

func writeRuns(w http.ResponseWriter, names *runNames, rows []store.EnvRun) error {
	out := struct {
		Runs []EnvRunRow `json:"runs"`
	}{Runs: []EnvRunRow{}}
	for _, r := range rows {
		v, err := names.row(r, false)
		if err != nil {
			return err
		}
		out.Runs = append(out.Runs, v)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// getRun is one run with the logs of its steps: for the machine's owner and for the managers of the machine's layers.
func (s *Service) getRun(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	q, err := s.open(r, s.db.S(), p, FeatureEnv)
	if err != nil {
		return err
	}
	run, err := q.st.EnvRunByID(q.ctx, q.w.org.ID, r.PathValue("runId"))
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if run.UserID != p.User.ID {
		scopes := []authz.Scope{{Kind: store.ScopeOrg, ID: q.w.org.ID}, {Kind: store.ScopeTenant, ID: run.SpaceID}}
		if run.TeamID != "" {
			scopes = append(scopes, authz.Scope{Kind: store.ScopeTeam, ID: run.TeamID})
		}
		seen := false
		for _, sc := range scopes {
			if ok, err := q.rt.manages(sc); err != nil {
				return err
			} else if ok {
				seen = true
				break
			}
		}
		if !seen {
			return q.rt.require(scopes[1])
		}
	}
	v, err := newRunNames(q.ctx, q.st).row(run, true)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": v})
	return nil
}

func newRunID() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// startRun asks a machine for a run. The row is kept first, as «running», so that the screen can look at it at once and
// the machine's events have a run of this machine to replace; if the machine does not accept, the row says so.
func (s *Service) startRun(ctx context.Context, orgID string, m store.Machine, mode string, items []hubproto.EnvItem, by string) (string, error) {
	if s.reg == nil {
		return "", httpx.ErrMachineOffline
	}
	id, err := newRunID()
	if err != nil {
		return "", err
	}
	now := s.now()
	row := store.EnvRun{ID: id, OrgID: orgID, MachineID: m.ID, UserID: m.CreatedBy, SpaceID: m.SpaceID, TeamID: m.TeamID, Mode: mode, Status: hubproto.EnvRunning,
		ByName: by, StartedAt: now, Steps: "[]", UpdatedAt: now}
	st := s.db.S()
	if _, err := st.UpsertEnvRun(ctx, row); err != nil {
		return "", err
	}
	var ans struct {
		RunID    string `json:"runId"`
		Accepted bool   `json:"accepted"`
	}
	req := hubproto.EnvRunRequest{RunID: id, Mode: mode, Items: items, By: by, At: now.UnixMilli()}
	err = s.reg.DoInto(ctx, m.ID, hubproto.OpEnvRun, req, &ans, callTimeout)
	if err == nil && !ans.Accepted {
		err = httpx.New(http.StatusBadGateway, "machine_error", "Машина не приняла запуск")
	}
	if err != nil {
		row.Status, row.FinishedAt, row.UpdatedAt = hubproto.EnvError, s.now(), s.now()
		if _, uerr := st.UpsertEnvRun(ctx, row); uerr != nil {
			s.log.Warn("environment run not marked", "run", id, "error", uerr)
		}
		return "", err
	}
	if s.pub != nil {
		s.pub.Publish(orgID, "env.run.changed", map[string]any{"runId": id, "status": hubproto.EnvRunning, "machineId": m.ID})
	}
	return id, nil
}

// ownSetupMachines are the person's machines in a tenant that are online and can do a setup run, most recently seen first.
func (s *Service) ownSetupMachines(ctx context.Context, st store.Store, userID, spaceID string) ([]store.Machine, error) {
	if s.reg == nil || s.push == nil {
		return nil, nil
	}
	ms, err := st.MachinesByUser(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	online := s.reg.Online()
	var out []store.Machine
	for _, m := range ms {
		if m.RevokedAt != nil || m.DisabledAt != nil || !slices.Contains(online, m.ID) {
			continue
		}
		if setup, _ := s.push.SetupOf(m.ID); setup {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b store.Machine) int {
		return cmp.Or(cmp.Compare(seenMs(b), seenMs(a)), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

func seenMs(m store.Machine) int64 {
	if m.LastSeenAt == nil {
		return 0
	}
	return m.LastSeenAt.UnixMilli()
}

var errNoMachine = httpx.Conflict("no_machine", "Подключите свою машину к хабу — проверка идёт на ней")

// draftIn is the item being edited: an entry's item replaced by it, or a new one added to the layer.
type draftIn struct {
	EntryID string          `json:"entryId"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	Source  json.RawMessage `json:"source"`
}

type tryIn struct {
	Layer     string   `json:"layer"`
	ScopeID   string   `json:"scopeId"`
	Draft     *draftIn `json:"draft"`
	MachineID string   `json:"machineId"`
}

// withDraft is the world with the draft in the layer: in place of the item of entryId, or added at the end.
func (q *req) withDraft(layer, scopeID string, d *draftIn, src json.RawMessage) (*world, error) {
	w := *q.w
	w.items = make(map[string]store.CatalogItem, len(q.w.items)+1)
	for k, v := range q.w.items {
		w.items[k] = v
	}
	w.entries = slices.Clone(q.w.entries)
	if d.EntryID != "" {
		i := slices.IndexFunc(w.entries, func(e store.EnvEntry) bool {
			return e.ID == d.EntryID && e.Layer == layer && e.ScopeID == scopeID
		})
		if i < 0 {
			return nil, httpx.Invalid("draft.entryId", "Такой записи в слое нет")
		}
		it := w.items[w.entries[i].ItemID]
		it.Kind, it.Name, it.Source = d.Kind, d.Name, string(src)
		w.items[it.ID] = it
		return &w, nil
	}
	id := "draft-" + store.NewID()
	w.items[id] = store.CatalogItem{ID: id, OrgID: q.w.org.ID, Kind: d.Kind, Name: d.Name, Source: string(src)}
	e := store.EnvEntry{ID: id, OrgID: q.w.org.ID, Layer: layer, ScopeID: scopeID, ItemID: id, Enabled: true, Audience: AudienceJSON(Audience{Mode: AudienceTenant})}
	if d.Kind == store.CatalogScript {
		e.Position = w.nextPosition(layer, scopeID)
	}
	w.entries = append(w.entries, e)
	return &w, nil
}

// tryDraft runs the layer's list, with the editor's draft, as a sandbox run on one of the asker's own machines.
func (s *Service) tryDraft(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in tryIn
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	q, err := s.open(r, s.db.S(), p, FeatureEnv)
	if err != nil {
		return err
	}
	if in.Layer != store.EnvLayerOrg && in.Layer != store.EnvLayerTenant && in.Layer != store.EnvLayerTeam {
		return httpx.Invalid("layer", "org, tenant или team")
	}
	if !q.w.layerExists(in.Layer, in.ScopeID) {
		return httpx.ErrNotFound
	}
	if err := q.rt.require(scopeOf(in.Layer, in.ScopeID)); err != nil {
		return err
	}
	world := q.w
	if in.Draft != nil {
		d := in.Draft
		if !ValidKind(d.Kind) {
			return httpx.Invalid("draft.kind", kindsRu)
		}
		if d.Name, err = CleanText("draft.name", d.Name, maxNameRunes, true); err != nil {
			return err
		}
		src, err := CleanSource(d.Kind, d.Source)
		if err != nil {
			return err
		}
		if err := q.checkSource(d.Kind, src); err != nil {
			return err
		}
		// Secrets of the safe and accesses are an organization admin's: a draft of anybody else, or of an item a layer
		// owns, may have neither.
		orgAdmin, err := q.rt.manages(authz.Scope{Kind: store.ScopeOrg, ID: q.w.org.ID})
		if err != nil {
			return err
		}
		owned := false
		if d.EntryID != "" {
			for _, e := range q.w.entries {
				if e.ID == d.EntryID {
					owned = q.w.items[e.ItemID].OwnerLayer != ""
				}
			}
		}
		if !orgAdmin || owned {
			if err := CheckConsent(d.Kind, src); err != nil {
				return err
			}
		}
		if world, err = q.withDraft(in.Layer, in.ScopeID, d, src); err != nil {
			return err
		}
	}
	cands, err := s.eligibleMachines(q, p.User.ID)
	if err != nil {
		return err
	}
	if in.MachineID != "" {
		cands = slices.DeleteFunc(cands, func(m store.Machine) bool { return m.ID != in.MachineID })
	}
	if len(cands) == 0 {
		return errNoMachine
	}
	m := cands[0]
	spaceID, teamID := "", ""
	switch in.Layer {
	case store.EnvLayerTenant:
		spaceID = in.ScopeID
	case store.EnvLayerTeam:
		spaceID, teamID = q.w.tenantOf(in.Layer, in.ScopeID), in.ScopeID
	}
	// The list is the one a machine of the layer holds, with the setup kinds: the same function the pusher uses.
	items := MachineItems(world, spaceID, teamID, p.User.ID, true, true) // the asker's choices: they try what they would get
	by := cmp.Or(p.User.Name, p.User.Email)
	runID, err := s.startRun(r.Context(), q.w.org.ID, m, hubproto.RunSandbox, items, by)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"runId": runID, "machine": NameRef{m.ID, m.Name}})
	return nil
}

// eligibleMachines are the person's online machines in the organization that can do a setup run.
func (s *Service) eligibleMachines(q *req, userID string) ([]store.Machine, error) {
	var all []store.Machine
	for _, sp := range q.w.tenants {
		ms, err := s.ownSetupMachines(q.ctx, q.st, userID, sp.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, ms...)
	}
	slices.SortFunc(all, func(a, b store.Machine) int {
		return cmp.Or(cmp.Compare(seenMs(b), seenMs(a)), cmp.Compare(a.ID, b.ID))
	})
	return all, nil
}
