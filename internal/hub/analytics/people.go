package analytics

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// «Расход по людям» of the lead's overview, the member's week and a person's own KPI chart (docs/hub/analytics.md,
// «People and the week»).

const (
	// hotPct is the share of a personal limit from which a person is marked hot.
	hotPct = 0.8
	// warnTone and riskTone are the shares of the scope's limit from which its total is warn and risk.
	warnTone = 0.75
	riskTone = 0.9
	// weekDays is the length of «за неделю».
	weekDays = 7
)

func (s *Service) mountPeople(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/people", s.gate(s.people))
	mux.Handle("GET /api/me/week", s.gate(s.meWeek))
	mux.Handle("GET /api/me/kpis/{kpi}/series", s.gate(s.meSeries))
}

// Chats are the chats of a week and the ones of them still running.
type Chats struct {
	N       int `json:"n"`
	Running int `json:"running"`
}

// MRs are the merge requests of a week and the ones of them merged.
type MRs struct {
	N      int `json:"n"`
	Merged int `json:"merged"`
}

// PersonSpend is one person of «Расход по людям».
type PersonSpend struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	// Role is the person's role in the tenant of the scope ("" when they have none there).
	Role string `json:"role"`
	List Money  `json:"list"`
	Paid Money  `json:"paid"`
	// Share is Paid ÷ the scope's own limit; null when the scope has none.
	Share *float64 `json:"share"`
	// Limit is the person's own monthly limit with its use; null when they have none.
	Limit *LimitBar `json:"limit"`
	// Hot is whether the personal limit is used to hotPct or more.
	Hot bool `json:"hot"`
	// Chats and MRs are of the last week, as the member sees them.
	Chats Chats `json:"chats"`
	MRs   MRs   `json:"mrs"`
}

// PeopleTotal is the scope's spend with its own limit.
type PeopleTotal struct {
	List  Money     `json:"list"`
	Paid  Money     `json:"paid"`
	Limit *LimitBar `json:"limit"`
	// Tone is ok, warn (the limit used to 75%) or risk (90%); null without a limit.
	Tone *string `json:"tone"`
}

// PeopleResponse is GET …/people.
type PeopleResponse struct {
	Scope    orgs.ScopeView `json:"scope"`
	Period   Period         `json:"period"`
	Currency Currency       `json:"currency"`
	Total    PeopleTotal    `json:"total"`
	Week     Period         `json:"week"`
	People   []PersonSpend  `json:"people"`
}

// toneOf is the colour of a share of a limit.
func toneOf(pct float64) string {
	switch {
	case pct >= riskTone:
		return "risk"
	case pct >= warnTone:
		return "warn"
	}
	return "ok"
}

// weekSpan is the last weekDays days of the zone, today included.
func weekSpan(now time.Time, loc *time.Location) span {
	end := midnight(now, loc).AddDate(0, 0, 1)
	return newSpan(end.AddDate(0, 0, -weekDays), end, loc)
}

// weekOf counts the chats and merge requests of sessions, keyed by the person.
func weekOf(ss []store.AgentSession, now time.Time) (map[string]*Chats, map[string]*MRs) {
	chats, mrs := map[string]*Chats{}, map[string]*MRs{}
	since := now.Add(-activeWindow).UnixMilli()
	for _, v := range ss {
		met := metricsOf(v.Metrics)
		k := v.UserID
		if chats[k] == nil {
			chats[k], mrs[k] = &Chats{}, &MRs{}
		}
		chats[k].N++
		if v.EndedAt == nil && v.UpdatedAt >= since {
			chats[k].Running++
		}
		mrs[k].N += int(met["mrs"])
		mrs[k].Merged += int(met["mrsMerged"])
	}
	return chats, mrs
}

// roles are the roles of people in the tenants of the scope.
func roles(ctx context.Context, st store.Store, x *orgScope) (map[string]string, error) {
	out := map[string]string{}
	for id := range x.tenants {
		ms, err := st.Members(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if _, ok := out[m.UserID]; !ok {
				out[m.UserID] = m.Role
			}
		}
	}
	return out, nil
}

func (s *Service) people(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.Money)
	if err != nil {
		return err
	}
	sp, err := parsePeriod(r.URL.Query(), x.loc, x.now)
	if err != nil {
		return err
	}
	m, err := s.money(ctx, st, x, sp, "person")
	if err != nil {
		return err
	}
	rows, err := m.breakdown(ctx, st, "person")
	if err != nil {
		return err
	}
	wk := weekSpan(x.now, x.loc)
	wf, wt := wk.ms()
	ss, err := st.ScopeSessions(ctx, x.filter(wf, wt))
	if err != nil {
		return err
	}
	chats, mrs := weekOf(ss, x.now)
	role, err := roles(ctx, st, x)
	if err != nil {
		return err
	}
	members, err := peopleOf(ctx, st, x)
	if err != nil {
		return err
	}

	out := PeopleResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}, Period: sp.period(), Currency: m.cur,
		Week: wk.period(), People: []PersonSpend{}}
	out.Total.List, out.Total.Paid = m.cur.Micros(m.sum.list), m.cur.Micros(m.sum.paid)
	if out.Total.Limit, err = m.bar(ctx, st, own(x.sc.Kind, x.sc.ID)); err != nil {
		return err
	}
	if l := out.Total.Limit; l != nil {
		t := toneOf(l.Pct)
		out.Total.Tone = &t
	}

	byUser := map[string]SpendRow{}
	for _, row := range rows {
		byUser[row.Key] = row
	}
	for _, id := range members {
		if _, ok := byUser[id]; !ok {
			byUser[id] = SpendRow{Key: id, List: m.cur.Micros(0), Paid: m.cur.Micros(0)}
		}
	}
	for id, row := range byUser {
		if id == "" {
			continue
		}
		who, err := m.person(ctx, st, id)
		if err != nil {
			return err
		}
		if who == nil {
			continue
		}
		v := PersonSpend{UserID: id, Name: who.Name, Role: role[id], List: row.List, Paid: row.Paid, Limit: row.Limit,
			Chats: Chats{}, MRs: MRs{}}
		if c := chats[id]; c != nil {
			v.Chats, v.MRs = *c, *mrs[id]
		}
		if l := out.Total.Limit; l != nil && l.Amount.USD > 0 {
			sh := round3(row.Paid.USD / l.Amount.USD)
			v.Share = &sh
		}
		v.Hot = row.Limit != nil && row.Limit.Pct >= hotPct
		out.People = append(out.People, v)
	}
	slices.SortFunc(out.People, func(a, b PersonSpend) int {
		return cmp.Or(cmp.Compare(b.Paid.USD, a.Paid.USD), cmp.Compare(a.Name, b.Name), cmp.Compare(a.UserID, b.UserID))
	})
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// MeWeek is GET /api/me/week: «Чатов за неделю» and «MR за неделю» of the member's overview.
type MeWeek struct {
	Week  Period `json:"week"`
	Chats Chats  `json:"chats"`
	MRs   MRs    `json:"mrs"`
}

func (s *Service) meWeek(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	_, loc, _, err := s.scopeOf(ctx, st, p, r.URL.Query().Get("org"))
	if err != nil {
		return err
	}
	now := s.now()
	wk := weekSpan(now, loc)
	from, to := wk.ms()
	ss, err := st.UserSessions(ctx, store.SessionFilter{UserID: p.User.ID, From: from, To: to, LiveOnly: true})
	if err != nil {
		return err
	}
	chats, mrs := weekOf(ss, now)
	out := MeWeek{Week: wk.period()}
	if c := chats[p.User.ID]; c != nil {
		out.Chats, out.MRs = *c, *mrs[p.User.ID]
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// meSeries is the person's own KPI by day or week, with the changes of the organization named by ?org= (and of the
// tenants and teams their sessions in the period belong to) as markers. Without ?org= there are no markers: a change
// belongs to an organization. The sessions are all the person's own, so the chart agrees with GET /api/me/kpis.
func (s *Service) meSeries(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	q := r.URL.Query()
	i := slices.IndexFunc(kpis, func(d kpiDef) bool { return d.id == r.PathValue("kpi") })
	if i < 0 {
		return httpx.ErrNotFound
	}
	d := kpis[i]
	by := cmp.Or(q.Get("by"), "day")
	if by != "day" && by != "week" {
		return httpx.Invalid("by", "by — day или week")
	}
	orgID, loc, _, err := s.scopeOf(ctx, st, p, q.Get("org"))
	if err != nil {
		return err
	}
	sp, err := parsePeriod(q, loc, s.now())
	if err != nil {
		return err
	}
	pf, pt := sp.ms()
	var cs []store.Change
	var scopes []store.ChangeScope
	var mine []store.AgentSession
	if orgID != "" {
		if mine, err = st.UserSessions(ctx, store.SessionFilter{UserID: p.User.ID, OrgKey: orgID, From: pf, To: pt, LiveOnly: true}); err != nil {
			return err
		}
		scopes = []store.ChangeScope{{Kind: store.ScopeOrg, ID: orgID}}
		seen := map[store.ChangeScope]bool{scopes[0]: true}
		for _, v := range mine {
			for _, c := range []store.ChangeScope{{Kind: store.ScopeTenant, ID: v.SpaceID}, {Kind: store.ScopeTeam, ID: v.TeamID}} {
				if c.ID != "" && !seen[c] {
					seen[c] = true
					scopes = append(scopes, c)
				}
			}
		}
		if cs, err = st.ChangesOn(ctx, orgID, scopes, pf, pt); err != nil {
			return err
		}
	}
	from, to := pf, pt
	for _, c := range cs {
		before, after := effectSpans(c.At, loc)
		a, b := kpiRange(after, before)
		from, to = min(from, a), max(to, b)
	}
	ss, err := st.UserSessions(ctx, store.SessionFilter{UserID: p.User.ID, From: from, To: to, LiveOnly: true})
	if err != nil {
		return err
	}
	ks, err := kSessionsOf(ctx, st, loc, ss, false)
	if err != nil {
		return err
	}
	out := KpiSeriesResponse{Kpi: d.id, Unit: d.unit, By: by, Points: seriesOf(d, ks, buckets(sp, by)), Markers: []ChangeMarker{}}
	if out.Markers, err = markersOf(ctx, st, loc, orgID, scopes, cs, d, ks); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
