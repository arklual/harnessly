// Package analytics is the money and KPI screens of the hub (docs/hub/analytics.md). This part is the person's own view,
// /api/me/*: their money, their sessions and their KPI, in any organization or none.
package analytics

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

const (
	defaultSessions = 50
	maxSessions     = 200
	// sessionsBack is how far /api/me/sessions looks when no from is given.
	sessionsBack = 90
	// noiseWeeks are the weeks before a period the noise of a KPI is measured on.
	noiseWeeks = 8
	// usageStream is the outbox stream the ingest reads (internal/hub/ingest).
	usageStream = "usage"
)

// Deps are what the service is built from.
type Deps struct {
	DB *store.DB
	// Gate wraps an endpoint in the session check and HUB_ORGS (orgs.Service.Gate).
	Gate func(orgs.Handler) httpx.Handler
	Log  *slog.Logger
	Now  func() time.Time
	// Rates is the exchange rate money is shown in; nil shows dollars.
	Rates *rates.Book
}

// Service serves the analytics endpoints.
type Service struct {
	db    *store.DB
	gate  func(orgs.Handler) httpx.Handler
	log   *slog.Logger
	now   func() time.Time
	rates *rates.Book
	// machines build the rows of the organization's machine list (UseMachines).
	machines *machines.Service
	// bus gets the live events (UseEvents); flags switch the signal checker on per organization (UseFeatures).
	bus   events.Publisher
	flags features.Gate
	// trig carries Trigger calls to RunLive.
	trigMu sync.Mutex
	trig   chan string
}

// New makes the service.
func New(d Deps) *Service {
	s := &Service{db: d.DB, gate: d.Gate, log: d.Log, now: d.Now, rates: d.Rates}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s
}

// Mount registers the endpoints.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/me/usage", s.gate(s.meUsage))
	mux.Handle("GET /api/me/sessions", s.gate(s.meSessions))
	mux.Handle("GET /api/me/kpis", s.gate(s.meKPIs))
	s.mountOrg(mux)
}

// scopeOf is the organization of ?org= (orgKey "" for all) and the time zone the days are cut in: the organization's,
// the only organization's of a person in one, or the default.
func (s *Service) scopeOf(ctx context.Context, st store.Store, p *auth.Principal, ref string) (string, *time.Location, string, error) {
	tz := defaultTZ
	orgKey, code := "", "RUB"
	if ref != "" {
		o, err := orgs.Resolve(ctx, st, p, ref)
		if err != nil {
			return "", nil, "", err
		}
		orgKey, tz, code = o.ID, o.Timezone, o.Currency
	} else {
		mine, err := st.OrgsForUser(ctx, p.User.ID)
		if err != nil {
			return "", nil, "", err
		}
		if len(mine) == 1 {
			tz, code = mine[0].Timezone, mine[0].Currency
		}
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.UTC
	}
	return orgKey, loc, code, nil
}

// MeUsageResponse is GET /api/me/usage.
type MeUsageResponse struct {
	Period         Period   `json:"period"`
	Currency       Currency `json:"currency"`
	AccountedUntil *string  `json:"accountedUntil"`
	Total          struct {
		List Money `json:"list"`
		Paid Money `json:"paid"`
	} `json:"total"`
	ByDay     []DayMoney     `json:"byDay"`
	ByModel   []ModelMoney   `json:"byModel"`
	ByAccount []AccountMoney `json:"byAccount"`
}

type DayMoney struct {
	Day  string `json:"day"`
	List Money  `json:"list"`
	Paid Money  `json:"paid"`
}

type ModelMoney struct {
	Model string `json:"model"`
	List  Money  `json:"list"`
	Calls int64  `json:"calls"`
}

type AccountMoney struct {
	AccountID string `json:"accountId"`
	Label     string `json:"label"`
	Funding   string `json:"funding"`
	List      Money  `json:"list"`
	Paid      Money  `json:"paid"`
}

func (s *Service) meUsage(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	orgKey, loc, code, err := s.scopeOf(ctx, st, p, r.URL.Query().Get("org"))
	if err != nil {
		return err
	}
	sp, err := parsePeriod(r.URL.Query(), loc, s.now())
	if err != nil {
		return err
	}
	from, to := sp.ms()
	rows, err := st.UserRollup(ctx, p.User.ID, orgKey, from, to)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(rows))
	for _, x := range rows {
		ids = append(ids, x.AccountID)
	}
	pay, err := usage.NewPayer(ctx, st, loc, ids)
	if err != nil {
		return err
	}
	cur := s.currency(ctx, code, sp)
	type pair struct{ list, paid int64 }
	var total pair
	byDay := map[string]*pair{}
	type mm struct {
		list, calls int64
	}
	byModel := map[string]*mm{}
	byAccount := map[string]*pair{}
	accFunding := map[string]string{}
	for _, x := range rows {
		paid, err := pay.Paid(ctx, x.AccountID, x.Hour, x.ListCostMicros)
		if err != nil {
			return err
		}
		total.list += x.ListCostMicros
		total.paid += paid
		d := time.UnixMilli(x.Hour).In(loc).Format(day)
		if byDay[d] == nil {
			byDay[d] = &pair{}
		}
		byDay[d].list += x.ListCostMicros
		byDay[d].paid += paid
		if byModel[x.Model] == nil {
			byModel[x.Model] = &mm{}
		}
		byModel[x.Model].list += x.ListCostMicros
		byModel[x.Model].calls += x.Calls
		if byAccount[x.AccountID] == nil {
			byAccount[x.AccountID] = &pair{}
		}
		byAccount[x.AccountID].list += x.ListCostMicros
		byAccount[x.AccountID].paid += paid
		accFunding[x.AccountID] = fundingOf(x.Funding)
	}
	out := MeUsageResponse{Period: sp.period(), Currency: cur, ByDay: []DayMoney{}, ByModel: []ModelMoney{}, ByAccount: []AccountMoney{}}
	out.Total.List, out.Total.Paid = cur.Micros(total.list), cur.Micros(total.paid)
	for d := sp.from; d.Before(sp.to); d = d.AddDate(0, 0, 1) {
		if v := byDay[d.Format(day)]; v != nil {
			out.ByDay = append(out.ByDay, DayMoney{Day: d.Format(day), List: cur.Micros(v.list), Paid: cur.Micros(v.paid)})
		}
	}
	for m, v := range byModel {
		out.ByModel = append(out.ByModel, ModelMoney{Model: m, List: cur.Micros(v.list), Calls: v.calls})
	}
	slices.SortFunc(out.ByModel, func(a, b ModelMoney) int { return cmpDesc(a.List.USD, b.List.USD, a.Model, b.Model) })
	for id, v := range byAccount {
		a, ok := pay.Account(id)
		funding := accFunding[id]
		if ok {
			funding = fundingOf(a.Funding)
		}
		out.ByAccount = append(out.ByAccount, AccountMoney{AccountID: id, Label: accountLabel(a, ok), Funding: funding,
			List: cur.Micros(v.list), Paid: cur.Micros(v.paid)})
	}
	slices.SortFunc(out.ByAccount, func(a, b AccountMoney) int { return cmpDesc(a.List.USD, b.List.USD, a.AccountID, b.AccountID) })
	until, err := st.UserAccountedUntil(ctx, p.User.ID, orgKey, usageStream)
	if err != nil {
		return err
	}
	if until != nil {
		t := httpx.Time(time.UnixMilli(*until))
		out.AccountedUntil = &t
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func cmpDesc(a, b float64, ka, kb string) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	case ka < kb:
		return -1
	case ka > kb:
		return 1
	}
	return 0
}

// MeSession is a session of /api/me/sessions: its price and result, no text.
type MeSession struct {
	SessionKey string      `json:"sessionKey"`
	Provider   string      `json:"provider"`
	StartedAt  string      `json:"startedAt"`
	EndedAt    *string     `json:"endedAt"`
	Machine    *MachineRef `json:"machine"`
	Models     []string    `json:"models"`
	List       Money       `json:"list"`
	Paid       Money       `json:"paid"`
	Funding    string      `json:"funding"`
	Origin     string      `json:"origin"`
	Turns      int64       `json:"turns"`
	MRs        int64       `json:"mrs"`
	Subagents  int         `json:"subagents"`
}

type MachineRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MeSessionsResponse is GET /api/me/sessions.
type MeSessionsResponse struct {
	Currency Currency    `json:"currency"`
	Sessions []MeSession `json:"sessions"`
	Next     *string     `json:"next"`
}

func metricsOf(raw string) map[string]float64 {
	m := map[string]float64{}
	var anyv map[string]any
	if json.Unmarshal([]byte(raw), &anyv) != nil {
		return m
	}
	for k, v := range anyv {
		if f, ok := v.(float64); ok {
			m[k] = f
		}
	}
	return m
}

// sessionMoney is the list and paid of top-level sessions, their subagents' calls included.
type sessionMoney struct {
	list, paid int64
	models     []string
	subagents  int
}

func moneyOf(ctx context.Context, st store.Store, pay *usage.Payer, ss []store.AgentSession) (map[store.SessionRef]*sessionMoney, error) {
	refs := make([]store.SessionRef, len(ss))
	byRef := map[store.SessionRef]store.AgentSession{}
	for i, x := range ss {
		refs[i] = store.SessionRef{OrgKey: x.OrgKey, Provider: x.Provider, SessionKey: x.SessionKey}
		byRef[refs[i]] = x
	}
	sums, subs, err := st.SessionSums(ctx, refs)
	if err != nil {
		return nil, err
	}
	out := map[store.SessionRef]*sessionMoney{}
	for _, r := range refs {
		out[r] = &sessionMoney{subagents: subs[r]}
	}
	type modelList struct {
		model string
		list  int64
	}
	models := map[store.SessionRef][]modelList{}
	for _, x := range sums {
		m := out[x.SessionRef]
		if m == nil {
			continue
		}
		sess := byRef[x.SessionRef]
		paid, err := pay.Paid(ctx, sess.AccountID, sess.StartedAt, x.ListCostMicros)
		if err != nil {
			return nil, err
		}
		m.list += x.ListCostMicros
		m.paid += paid
		models[x.SessionRef] = append(models[x.SessionRef], modelList{x.Model, x.ListCostMicros})
	}
	for r, ml := range models {
		slices.SortFunc(ml, func(a, b modelList) int { return cmpDesc(float64(a.list), float64(b.list), a.model, b.model) })
		for _, x := range ml {
			out[r].models = append(out[r].models, x.model)
		}
	}
	return out, nil
}

func accountIDs(ss []store.AgentSession) []string {
	ids := make([]string, 0, len(ss))
	for _, x := range ss {
		ids = append(ids, x.AccountID)
	}
	return ids
}

func (s *Service) meSessions(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	q := r.URL.Query()
	_, loc, code, err := s.scopeOf(ctx, st, p, "")
	if err != nil {
		return err
	}
	now := s.now()
	sp, ok, err := parseRange(q, "from", "to", loc)
	if err != nil {
		return err
	}
	if !ok {
		end := midnight(now, loc).AddDate(0, 0, 1)
		sp = newSpan(end.AddDate(0, 0, -sessionsBack), end, loc)
	}
	from, to := sp.ms()
	if b := q.Get("before"); b != "" {
		t, err := time.Parse(time.RFC3339Nano, b)
		if err != nil {
			return httpx.Invalid("before", "an RFC 3339 time, the next of the previous page")
		}
		to = min(to, t.UnixMilli())
	}
	limit := defaultSessions
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > maxSessions {
			return httpx.Invalid("limit", "1 to 200")
		}
		limit = n
	}
	ss, err := st.UserSessions(ctx, store.SessionFilter{UserID: p.User.ID, From: from, To: to, Limit: limit + 1})
	if err != nil {
		return err
	}
	cur := s.currency(ctx, code, newSpan(midnight(now, loc), midnight(now, loc), loc))
	out := MeSessionsResponse{Currency: cur, Sessions: []MeSession{}}
	if len(ss) > limit {
		ss = ss[:limit]
		next := time.UnixMilli(ss[len(ss)-1].StartedAt).UTC().Format(time.RFC3339Nano)
		out.Next = &next
	}
	pay, err := usage.NewPayer(ctx, st, loc, accountIDs(ss))
	if err != nil {
		return err
	}
	money, err := moneyOf(ctx, st, pay, ss)
	if err != nil {
		return err
	}
	var machineIDs []string
	for _, x := range ss {
		if x.MachineID != "" {
			machineIDs = append(machineIDs, x.MachineID)
		}
	}
	names, err := st.MachineNames(ctx, machineIDs)
	if err != nil {
		return err
	}
	for _, x := range ss {
		m := money[store.SessionRef{OrgKey: x.OrgKey, Provider: x.Provider, SessionKey: x.SessionKey}]
		met := metricsOf(x.Metrics)
		v := MeSession{
			SessionKey: x.SessionKey, Provider: x.Provider, StartedAt: httpx.Time(time.UnixMilli(x.StartedAt)),
			Models: []string{}, List: cur.Micros(m.list), Paid: cur.Micros(m.paid), Funding: fundingOf(x.Funding), Origin: x.Origin,
			Turns: int64(met["turns"]), MRs: int64(met["mrs"]), Subagents: m.subagents,
		}
		if m.models != nil {
			v.Models = m.models
		}
		if x.EndedAt != nil {
			e := httpx.Time(time.UnixMilli(*x.EndedAt))
			v.EndedAt = &e
		}
		if name, ok := names[x.MachineID]; ok {
			v.Machine = &MachineRef{ID: x.MachineID, Name: name}
		}
		out.Sessions = append(out.Sessions, v)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// KpisResponse is GET /api/me/kpis (scope 'me') and the organization's KPI.
type KpisResponse struct {
	Scope     any         `json:"scope"` // "me" or the organization's scope
	Period    Period      `json:"period"`
	CmpPeriod Period      `json:"cmpPeriod"`
	Kpis      []KpiValue  `json:"kpis"`
	Series    []KpiSeries `json:"series"`
}

type KpiSeries struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Kpis  []KpiValue `json:"kpis"`
}

func (s *Service) meKPIs(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	_, loc, _, err := s.scopeOf(ctx, st, p, "")
	if err != nil {
		return err
	}
	sp, ok, err := parseRange(r.URL.Query(), "from", "to", loc)
	if err != nil {
		return err
	}
	if !ok {
		m := monthStart(s.now(), loc)
		sp = newSpan(m, m.AddDate(0, 1, 0), loc)
	}
	cmp, err := comparison(r.URL.Query(), sp)
	if err != nil {
		return err
	}
	from, to := kpiRange(sp, cmp)
	ss, err := st.UserSessions(ctx, store.SessionFilter{UserID: p.User.ID, From: from, To: to, LiveOnly: true})
	if err != nil {
		return err
	}
	kv, err := kpisOf(ctx, st, loc, ss, sp, cmp, false)
	if err != nil {
		return err
	}
	out := KpisResponse{Scope: "me", Period: sp.period(), CmpPeriod: cmp.period(), Kpis: kv}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// kpiRange is what sessions the KPI of a period need: the period, its comparison and the noise's weeks before it.
func kpiRange(sp, cmp span) (int64, int64) {
	noiseFrom := sp.from.AddDate(0, 0, -7*noiseWeeks)
	return min(cmp.from.UnixMilli(), noiseFrom.UnixMilli()), max(sp.to.UnixMilli(), cmp.to.UnixMilli())
}

// kpisOf are the twelve KPI of the live top-level sessions ss (those of kpiRange). orgMoney counts only the
// organization's money in money_per_mr: a personal account's sessions are left out of it.
func kpisOf(ctx context.Context, st store.Store, loc *time.Location, ss []store.AgentSession, sp, cmp span, orgMoney bool) ([]KpiValue, error) {
	ks, err := kSessionsOf(ctx, st, loc, ss, orgMoney)
	if err != nil {
		return nil, err
	}
	return kpisFrom(ks, sp, cmp), nil
}

// kSessionsOf turns sessions into what the KPI see, with their money.
func kSessionsOf(ctx context.Context, st store.Store, loc *time.Location, ss []store.AgentSession, orgMoney bool) ([]kSession, error) {
	pay, err := usage.NewPayer(ctx, st, loc, accountIDs(ss))
	if err != nil {
		return nil, err
	}
	money, err := moneyOf(ctx, st, pay, ss)
	if err != nil {
		return nil, err
	}
	ks := make([]kSession, len(ss))
	for i, x := range ss {
		end := x.UpdatedAt
		if x.EndedAt != nil {
			end = *x.EndedAt
		}
		m := money[store.SessionRef{OrgKey: x.OrgKey, Provider: x.Provider, SessionKey: x.SessionKey}]
		ks[i] = kSession{UserID: x.UserID, Start: x.StartedAt, End: end, M: metricsOf(x.Metrics), PaidMicros: m.paid,
			NoMoney: orgMoney && x.Funding != store.FundingOrg}
	}
	return ks, nil
}

// startedIn is the sessions started in [a, b).
func startedIn(ks []kSession, a, b time.Time) []kSession {
	var out []kSession
	for _, k := range ks {
		if k.Start >= a.UnixMilli() && k.Start < b.UnixMilli() {
			out = append(out, k)
		}
	}
	return out
}

// kpisFrom is the KPI of sp against cmp, the noise from the noiseWeeks weeks before sp.
func kpisFrom(ks []kSession, sp, cmp span) []KpiValue {
	weeks := make([][]kSession, noiseWeeks)
	for i := range weeks {
		end := sp.from.AddDate(0, 0, -7*i)
		weeks[i] = startedIn(ks, end.AddDate(0, 0, -7), end)
	}
	return computeKPIs(startedIn(ks, sp.from, sp.to), sp.days(), startedIn(ks, cmp.from, cmp.to), cmp.days(), weeks)
}
