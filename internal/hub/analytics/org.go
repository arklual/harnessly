package analytics

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/limits"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

// The organization's screens (docs/hub/analytics.md, «Endpoints»): the overview and the spend of a scope. Money counts
// only funding 'org'; activity counts every live session of the scope.

const (
	// paceDays is the window the forecast takes its pace from.
	paceDays = 7
	// maxAttention is the most signals the overview shows.
	maxAttention = 6
)

func (s *Service) mountOrg(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/overview", s.gate(s.overview))
	mux.Handle("GET /api/orgs/{org}/spend", s.gate(s.spend))
	mux.Handle("GET /api/orgs/{org}/kpis", s.gate(s.orgKPIs))
	mux.Handle("GET /api/orgs/{org}/kpis/{kpi}/series", s.gate(s.kpiSeries))
	s.mountChanges(mux)
	s.mountPeople(mux)
	s.mountSignals(mux)
	mux.Handle("GET /api/orgs/{org}/waste", s.gate(s.waste))
	s.mountSubscriptions(mux)
	s.mountLimitWindows(mux)
	s.mountAccounts(mux)
	s.mountMachines(mux)
	s.mountTools(mux)
}

// LimitBar is the limit a row is held to.
type LimitBar struct {
	RuleID string          `json:"ruleId"`
	Amount Money           `json:"amount"`
	Pct    float64         `json:"pct"`
	Status string          `json:"status"`
	SetBy  *orgs.PersonRef `json:"setBy"`
}

// Tokens are a row's tokens.
type Tokens struct {
	// Total is the four kinds added up, for the «Токены» unit of the breakdowns.
	Total      int64 `json:"total"`
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

// SpendRow is one row of a spend breakdown.
type SpendRow struct {
	Key           string    `json:"key"`
	Label         string    `json:"label"`
	List          Money     `json:"list"`
	Paid          Money     `json:"paid"`
	Calls         int64     `json:"calls"`
	UnpricedCalls int64     `json:"unpricedCalls"`
	Tokens        Tokens    `json:"tokens"`
	Limit         *LimitBar `json:"limit"`
	Forecast      *Money    `json:"forecast"`
	ForecastList  *Money    `json:"forecastList"`
	GuessedShare  float64   `json:"guessedShare"`
}

// SpendSum is the money and tokens of all rows of a breakdown: the base its shares are taken of, in any of the units.
type SpendSum struct {
	List   Money `json:"list"`
	Paid   Money `json:"paid"`
	Tokens int64 `json:"tokens"`
}

// SpendTotal is the sum of the rows with the scope's own limit.
type SpendTotal struct {
	List         Money     `json:"list"`
	Paid         Money     `json:"paid"`
	Limit        *LimitBar `json:"limit"`
	Forecast     *Money    `json:"forecast"`
	ForecastList *Money    `json:"forecastList"` // the same pace at API prices: next to the list spend the screens show
}

// SpendResponse is GET …/spend.
type SpendResponse struct {
	Scope          orgs.ScopeView `json:"scope"`
	By             string         `json:"by"`
	Period         Period         `json:"period"`
	Currency       Currency       `json:"currency"`
	Total          SpendTotal     `json:"total"`
	Rows           []SpendRow     `json:"rows"`
	AccountedUntil *string        `json:"accountedUntil"`
}

// Tile is one number of the overview with its comparison.
type Tile struct {
	Value    *float64 `json:"value"`
	CmpValue *float64 `json:"cmpValue"`
	Unit     string   `json:"unit"`
}

// SignalSubject is what a signal is about.
type SignalSubject struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// SignalAction is the one button of a signal. Which fields it carries depends on Kind (the variants of SignalAction in
// web/app/hub-api/analytics.ts); Target is the id of the object the button leads to, for every kind.
type SignalAction struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Target string `json:"target,omitempty"`
	// raise_limit, open_kpi
	Scope orgs.ScopeView `json:"scope"`
	// raise_limit
	UserID *string `json:"userId"`
	// move_account
	FromAccountID string `json:"fromAccountId,omitempty"`
	ToAccountID   string `json:"toAccountId,omitempty"`
	// open_machine, open_chat
	MachineID string `json:"machineId,omitempty"`
	// resend_invite
	InviteID string `json:"inviteId,omitempty"`
	TenantID string `json:"tenantId,omitempty"`
	// register_account
	AccountID string `json:"accountId,omitempty"`
	// open_kpi
	KPI string `json:"kpi,omitempty"`
	// open_chat
	SessionKey string `json:"sessionKey,omitempty"`
	// open_prices
	Model string `json:"model,omitempty"`
}

// MarshalJSON writes the fields of the kind's variant only.
func (a SignalAction) MarshalJSON() ([]byte, error) {
	m := map[string]any{"kind": a.Kind, "label": a.Label, "target": a.Target}
	switch a.Kind {
	case "raise_limit":
		m["scope"], m["userId"] = a.Scope, a.UserID
		if a.Target == "" {
			m["target"] = a.Scope.Kind + ":" + a.Scope.ID
			if a.UserID != nil {
				m["target"] = *a.UserID
			}
		}
	case "move_account":
		m["fromAccountId"], m["toAccountId"] = a.FromAccountID, a.ToAccountID
	case "open_machine":
		m["machineId"] = a.MachineID
	case "resend_invite":
		m["inviteId"], m["tenantId"] = a.InviteID, a.TenantID
	case "register_account":
		m["accountId"] = a.AccountID
	case "open_kpi":
		m["kpi"], m["scope"] = a.KPI, a.Scope
	case "open_chat":
		m["machineId"], m["sessionKey"] = a.MachineID, a.SessionKey
	case "open_prices":
		m["model"] = a.Model
	}
	return json.Marshal(m)
}

// Signal is one line of «Требует внимания».
type Signal struct {
	ID           string         `json:"id"`
	Rule         string         `json:"rule"`
	Severity     string         `json:"severity"`
	Scope        orgs.ScopeView `json:"scope"`
	Subject      SignalSubject  `json:"subject"`
	Title        string         `json:"title"`
	Detail       string         `json:"detail"`
	FirstAt      string         `json:"firstAt"`
	LastAt       string         `json:"lastAt"`
	SnoozedUntil *string        `json:"snoozedUntil"`
	Escalated    bool           `json:"escalated"`
	Action       *SignalAction  `json:"action"`
}

// OverviewResponse is GET …/overview.
type OverviewResponse struct {
	Scope    orgs.ScopeView `json:"scope"`
	Period   Period         `json:"period"`
	Currency Currency       `json:"currency"`
	Spend    SpendTotal     `json:"spend"`
	Tiles    struct {
		ActivePeople Tile `json:"activePeople"`
		Sessions     Tile `json:"sessions"`
		MRs          Tile `json:"mrs"`
		MoneyPerMR   Tile `json:"moneyPerMr"`
	} `json:"tiles"`
	Attention []Signal `json:"attention"`
	// Today is the day's numbers with their 14-day strips, Efficiency the closed tasks per person and the chats that
	// reached a result.
	Today      Today      `json:"today"`
	Efficiency Efficiency `json:"efficiency"`
	SpendBy    struct {
		By     string     `json:"by"`
		Rows   []SpendRow `json:"rows"`
		Totals SpendSum   `json:"totals"`
	} `json:"spendBy"`
	Subscriptions  []Subscription `json:"subscriptions"`
	AccountedUntil *string        `json:"accountedUntil"`
}

// orgScope is a scope of an organization with what its screens are cut in.
type orgScope struct {
	org   store.Org
	sc    authz.Scope
	loc   *time.Location
	now   time.Time
	space string // the tenant of a tenant scope
	// full is set by the background checker, which has looked at the whole organization and so may resolve what it no
	// longer finds (the rules of orgRules).
	full  bool
	teams map[string]store.Team
	// tenants are the tenants under the scope (a team's own tenant for a team).
	tenants map[string]store.Space
}

func (s *Service) orgScope(ctx context.Context, st store.Store, p *auth.Principal, r *http.Request, action authz.Action) (*orgScope, error) {
	o, sc, err := orgs.ScopeIn(ctx, st, p, r.PathValue("org"), r.URL.Query().Get("scope"), action)
	if err != nil {
		return nil, err
	}
	return s.loadScope(ctx, st, o, sc)
}

// memberScope is orgScope for a person who only belongs (orgs.MemberScopeIn): the scope cut to the tenants they are in.
func (s *Service) memberScope(ctx context.Context, st store.Store, p *auth.Principal, r *http.Request) (*orgScope, error) {
	o, sc, in, err := orgs.MemberScopeIn(ctx, st, p, r.PathValue("org"), r.URL.Query().Get("scope"))
	if err != nil {
		return nil, err
	}
	x, err := s.loadScope(ctx, st, o, sc)
	if err != nil {
		return nil, err
	}
	for id := range x.tenants {
		if !in[id] {
			delete(x.tenants, id)
		}
	}
	for id, t := range x.teams {
		if !in[t.SpaceID] {
			delete(x.teams, id)
		}
	}
	return x, nil
}

// isForbiddenScope is the refusal of a role the person does not have (authz.ErrForbiddenScope).
func isForbiddenScope(err error) bool {
	var he *httpx.Error
	return errors.As(err, &he) && he.Code == authz.ErrForbiddenScope.Code
}

// loadScope is a scope of the organization with what its screens are cut in, the person's right to it checked already.
func (s *Service) loadScope(ctx context.Context, st store.Store, o store.Org, sc authz.Scope) (*orgScope, error) {
	loc, err := limits.Location(ctx, st, o.ID)
	if err != nil {
		return nil, err
	}
	x := &orgScope{org: o, sc: sc, loc: loc, now: s.now(), teams: map[string]store.Team{}, tenants: map[string]store.Space{}}
	var sps []store.Space
	switch sc.Kind {
	case store.ScopeOrg:
		if sps, err = st.SpacesByOrg(ctx, o.ID); err != nil {
			return nil, err
		}
	case store.ScopeTenant:
		sp, err := st.SpaceByID(ctx, sc.ID)
		if err != nil {
			return nil, err
		}
		x.space, sps = sp.ID, []store.Space{sp}
	case store.ScopeTeam:
		t, err := st.TeamByID(ctx, sc.ID)
		if err != nil {
			return nil, err
		}
		sp, err := st.SpaceByID(ctx, t.SpaceID)
		if err != nil {
			return nil, err
		}
		x.teams[t.ID], x.tenants[sp.ID] = t, sp
		return x, nil
	}
	for _, sp := range sps {
		x.tenants[sp.ID] = sp
		ts, err := st.TeamsBySpace(ctx, sp.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			x.teams[t.ID] = t
		}
	}
	return x, nil
}

// in is whether an hourly row is the scope's money.
func (x *orgScope) in(r store.RollupRow) bool {
	if r.Funding != store.FundingOrg {
		return false
	}
	switch x.sc.Kind {
	case store.ScopeTenant:
		return r.SpaceID == x.sc.ID
	case store.ScopeTeam:
		return r.TeamID == x.sc.ID
	}
	return true
}

func (x *orgScope) filter(from, to int64) store.ScopeFilter {
	f := store.ScopeFilter{OrgKey: x.org.ID, From: from, To: to, LiveOnly: true}
	switch x.sc.Kind {
	case store.ScopeTenant:
		f.SpaceID = x.sc.ID
	case store.ScopeTeam:
		f.TeamID = x.sc.ID
	}
	return f
}

// owns is whether a limit rule sits on the scope or under it.
func (x *orgScope) owns(r store.LimitRule) bool {
	switch r.ScopeKind {
	case store.ScopeOrg:
		return x.sc.Kind == store.ScopeOrg && r.ScopeID == x.sc.ID
	case store.ScopeTenant:
		_, ok := x.tenants[r.ScopeID]
		return ok && x.sc.Kind != store.ScopeTeam
	case store.ScopeTeam:
		_, ok := x.teams[r.ScopeID]
		return ok
	}
	return false
}

// agg is the money and tokens of one row of a breakdown.
type agg struct {
	list, paid, calls, unpriced, in, out, cr, cw int64
	pace                                         int64 // paid in the last paceDays
	paceList                                     int64 // the same at API prices
}

// money is the spend of a scope in a period, by one key.
type money struct {
	x     *orgScope
	sp    span
	cur   Currency
	sum   agg
	rows  map[string]*agg
	pay   *usage.Payer
	book  *limits.Book
	names map[string]*orgs.PersonRef
	open  bool
}

func keyOf(by string, r store.RollupRow) string {
	switch by {
	case "team":
		return r.TeamID
	case "person":
		return r.UserID
	case "model":
		return r.Model
	case "account":
		return r.AccountID
	case "tenant":
		return r.SpaceID
	}
	return ""
}

func (s *Service) money(ctx context.Context, st store.Store, x *orgScope, sp span, by string) (*money, error) {
	m := &money{x: x, sp: sp, cur: s.currency(ctx, x.org.Currency, sp), rows: map[string]*agg{}, names: map[string]*orgs.PersonRef{}}
	m.open = !x.now.Before(sp.from) && x.now.Before(sp.to)
	from, to := sp.ms()
	paceFrom := x.now.Add(-paceDays * 24 * time.Hour).UnixMilli()
	if m.open {
		from = min(from, store.HourMs(paceFrom))
	}
	rows, err := st.RollupRows(ctx, x.org.ID, from, to)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range rows {
		if x.in(r) {
			ids = append(ids, r.AccountID)
		}
	}
	if m.pay, err = usage.NewPayer(ctx, st, x.loc, ids); err != nil {
		return nil, err
	}
	pf, pt := sp.ms()
	for _, r := range rows {
		if !x.in(r) {
			continue
		}
		paid, err := m.pay.Paid(ctx, r.AccountID, r.Hour, r.ListCostMicros)
		if err != nil {
			return nil, err
		}
		k := keyOf(by, r)
		a := m.rows[k]
		if a == nil {
			a = &agg{}
			m.rows[k] = a
		}
		if r.Hour >= pf && r.Hour < pt {
			for _, t := range []*agg{a, &m.sum} {
				t.list += r.ListCostMicros
				t.paid += paid
				t.calls += r.Calls
				t.unpriced += r.Unpriced
				t.in += r.Input
				t.out += r.Output
				t.cr += r.CacheRead
				t.cw += r.CacheWrite
			}
		}
		if m.open && r.Hour >= store.HourMs(paceFrom) {
			a.pace += paid
			m.sum.pace += paid
			a.paceList += r.ListCostMicros
			m.sum.paceList += r.ListCostMicros
		}
	}
	for k, a := range m.rows {
		if a.calls == 0 && a.pace == 0 && a.paceList == 0 {
			delete(m.rows, k)
		}
	}
	if m.book, err = limits.Evaluate(ctx, st, x.org.ID, x.loc, x.now); err != nil {
		return nil, err
	}
	return m, nil
}

// forecast is the period's paid at the pace of the last 7 days (what a limit is checked against); nil for a closed
// period.
func (m *money) forecast(a agg) *Money { return m.ahead(a.paid, a.pace) }

// forecastList is the same at API prices: the screens show the spend at list price and the forecast next to it.
func (m *money) forecastList(a agg) *Money { return m.ahead(a.list, a.paceList) }

func (m *money) ahead(sofar, pace int64) *Money {
	if !m.open {
		return nil
	}
	left := m.sp.to.Sub(m.x.now).Hours() / 24
	v := m.cur.Micros(sofar + int64(math.Round(float64(pace)/paceDays*left)))
	return &v
}

func (m *money) person(ctx context.Context, st store.Store, userID string) (*orgs.PersonRef, error) {
	if userID == "" {
		return nil, nil
	}
	if p, ok := m.names[userID]; ok {
		return p, nil
	}
	u, err := st.UserByID(ctx, userID)
	if store.IsNotFound(err) {
		m.names[userID] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &orgs.PersonRef{UserID: u.ID, Name: u.Name}
	if p.Name == "" {
		p.Name = u.Email
	}
	m.names[userID] = p
	return p, nil
}

// bar is the monthly money limit that holds a row: the one of the rules picked that is used the most.
func (m *money) bar(ctx context.Context, st store.Store, pick func(r store.LimitRule) bool) (*LimitBar, error) {
	var best *limits.State
	for i := range m.book.States {
		x := &m.book.States[i]
		r := x.Rule
		if r.Forbid || r.Unit != store.UnitMoney || r.Period != store.PeriodMonth || !pick(r) {
			continue
		}
		if best == nil || x.Pct > best.Pct {
			best = x
		}
	}
	if best == nil {
		return nil, nil
	}
	by, err := m.person(ctx, st, best.Rule.SetBy)
	if err != nil {
		return nil, err
	}
	return &LimitBar{RuleID: best.Rule.ID, Amount: m.cur.USD(best.Rule.Amount), Pct: round3(best.Pct), Status: best.Status, SetBy: by}, nil
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// own is a rule of the scope itself: no person, account or model.
func own(kind, id string) func(r store.LimitRule) bool {
	return func(r store.LimitRule) bool {
		return r.ScopeKind == kind && r.ScopeID == id && r.UserID == "" && r.AccountID == "" && r.ModelPattern == ""
	}
}

func (m *money) total(ctx context.Context, st store.Store) (SpendTotal, error) {
	t := SpendTotal{List: m.cur.Micros(m.sum.list), Paid: m.cur.Micros(m.sum.paid), Forecast: m.forecast(m.sum), ForecastList: m.forecastList(m.sum)}
	var err error
	t.Limit, err = m.bar(ctx, st, own(m.x.sc.Kind, m.x.sc.ID))
	return t, err
}

func (m *money) breakdown(ctx context.Context, st store.Store, by string) ([]SpendRow, error) {
	out := make([]SpendRow, 0, len(m.rows))
	for k, a := range m.rows {
		row := SpendRow{Key: k, List: m.cur.Micros(a.list), Paid: m.cur.Micros(a.paid), Calls: a.calls, UnpricedCalls: a.unpriced,
			Tokens: Tokens{Total: a.in + a.out + a.cr + a.cw, Input: a.in, Output: a.out, CacheRead: a.cr, CacheWrite: a.cw}, Forecast: m.forecast(*a), ForecastList: m.forecastList(*a)}
		var pick func(r store.LimitRule) bool
		switch by {
		case "team":
			row.Label = "Без команды"
			if t, ok := m.x.teams[k]; ok {
				row.Label = t.Name
			} else if t, err := st.TeamByID(ctx, k); err == nil {
				row.Label = t.Name
			}
			if k != "" {
				pick = own(store.ScopeTeam, k)
			}
		case "tenant":
			row.Label = "Без тенанта"
			if sp, ok := m.x.tenants[k]; ok {
				row.Label = sp.Name
			} else if sp, err := st.SpaceByID(ctx, k); err == nil {
				row.Label = sp.Name
			}
			if k != "" {
				pick = own(store.ScopeTenant, k)
			}
		case "person":
			row.Label = "Неизвестно"
			p, err := m.person(ctx, st, k)
			if err != nil {
				return nil, err
			}
			if p != nil {
				row.Label = p.Name
				pick = func(r store.LimitRule) bool {
					return r.UserID == k && r.AccountID == "" && r.ModelPattern == "" && m.x.owns(r)
				}
			}
		case "account":
			a, ok := m.pay.Account(k)
			row.Label = accountLabel(a, ok)
			if k != "" {
				pick = func(r store.LimitRule) bool {
					return r.AccountID == k && r.UserID == "" && r.ModelPattern == "" && m.x.owns(r)
				}
			}
		case "model":
			row.Label = k
		}
		if pick != nil {
			bar, err := m.bar(ctx, st, pick)
			if err != nil {
				return nil, err
			}
			row.Limit = bar
		}
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b SpendRow) int {
		return cmpDesc(a.Paid.USD+a.List.USD/1e6, b.Paid.USD+b.List.USD/1e6, a.Label, b.Label)
	})
	return out, nil
}

func (s *Service) accountedUntil(ctx context.Context, st store.Store, x *orgScope) (*string, error) {
	until, err := st.ScopeAccountedUntil(ctx, x.filter(0, 0), usageStream)
	if err != nil || until == nil {
		return nil, err
	}
	t := httpx.Time(time.UnixMilli(*until))
	return &t, nil
}

var spendBys = []string{"team", "person", "model", "account", "tenant"}

func (s *Service) spend(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.Money)
	if err != nil {
		return err
	}
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "team"
		if x.sc.Kind == store.ScopeTeam {
			by = "person"
		}
	}
	if !slices.Contains(spendBys, by) {
		return httpx.Invalid("by", "team, person, model, account or tenant")
	}
	sp, err := parsePeriod(r.URL.Query(), x.loc, x.now)
	if err != nil {
		return err
	}
	m, err := s.money(ctx, st, x, sp, by)
	if err != nil {
		return err
	}
	out := SpendResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}, By: by, Period: sp.period(), Currency: m.cur}
	if out.Total, err = m.total(ctx, st); err != nil {
		return err
	}
	if out.Rows, err = m.breakdown(ctx, st, by); err != nil {
		return err
	}
	if out.AccountedUntil, err = s.accountedUntil(ctx, st, x); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// tiles are the four numbers of a period: active people, live sessions, merged MRs and the money of an MR (the
// organization's sessions at API prices ÷ their merged MRs, as the money_per_mr KPI).
type tiles struct {
	people, sessions, mrs float64
	perMR                 *float64
}

func (s *Service) tilesOf(ctx context.Context, st store.Store, x *orgScope, sp span) (tiles, error) {
	var t tiles
	from, to := sp.ms()
	ss, err := st.ScopeSessions(ctx, x.filter(from, to))
	if err != nil {
		return t, err
	}
	people := map[string]bool{}
	var org []store.AgentSession
	for _, v := range ss {
		if v.UserID != "" {
			people[v.UserID] = true
		}
		t.mrs += metricsOf(v.Metrics)["mrsMerged"]
		if v.Funding == store.FundingOrg {
			org = append(org, v)
		}
	}
	t.people, t.sessions = float64(len(people)), float64(len(ss))
	if len(org) == 0 {
		return t, nil
	}
	pay, err := usage.NewPayer(ctx, st, x.loc, accountIDs(org))
	if err != nil {
		return t, err
	}
	money, err := moneyOf(ctx, st, pay, org)
	if err != nil {
		return t, err
	}
	var list int64
	var mrs float64
	for _, v := range org {
		list += money[store.SessionRef{OrgKey: v.OrgKey, Provider: v.Provider, SessionKey: v.SessionKey}].list
		mrs += metricsOf(v.Metrics)["mrsMerged"]
	}
	if mrs > 0 {
		v := math.Round(float64(list)/1e6/mrs*100) / 100
		t.perMR = &v
	}
	return t, nil
}

func num(v float64) *float64 { return &v }

func (s *Service) overview(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.View)
	if err != nil {
		return err
	}
	sp, err := parsePeriod(r.URL.Query(), x.loc, x.now)
	if err != nil {
		return err
	}
	cmpSp, err := comparison(r.URL.Query(), sp)
	if err != nil {
		return err
	}
	by := "team"
	if x.sc.Kind == store.ScopeTeam {
		by = "person"
	}
	m, err := s.money(ctx, st, x, sp, by)
	if err != nil {
		return err
	}
	out := OverviewResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}, Period: sp.period(), Currency: m.cur}
	if out.Spend, err = m.total(ctx, st); err != nil {
		return err
	}
	out.SpendBy.By = by
	if out.SpendBy.Rows, err = m.breakdown(ctx, st, by); err != nil {
		return err
	}
	out.SpendBy.Totals = SpendSum{List: m.cur.Micros(m.sum.list), Paid: m.cur.Micros(m.sum.paid), Tokens: m.sum.in + m.sum.out + m.sum.cr + m.sum.cw}
	if out.Today, err = s.today(ctx, st, x, m.cur); err != nil {
		return err
	}
	if out.Efficiency, err = s.efficiency(ctx, st, x, sp); err != nil {
		return err
	}
	cur, err := s.tilesOf(ctx, st, x, sp)
	if err != nil {
		return err
	}
	prev, err := s.tilesOf(ctx, st, x, cmpSp)
	if err != nil {
		return err
	}
	out.Tiles.ActivePeople = Tile{Value: num(cur.people), CmpValue: num(prev.people), Unit: "people"}
	out.Tiles.Sessions = Tile{Value: num(cur.sessions), CmpValue: num(prev.sessions), Unit: "count"}
	out.Tiles.MRs = Tile{Value: num(cur.mrs), CmpValue: num(prev.mrs), Unit: "count"}
	out.Tiles.MoneyPerMR = Tile{Value: cur.perMR, CmpValue: prev.perMR, Unit: "usd"}
	if out.Subscriptions, err = s.subscriptionsOf(ctx, st, x, sp, m.cur); err != nil {
		return err
	}
	if out.Attention, err = s.attention(ctx, st, p, x, m, out.Spend); err != nil {
		return err
	}
	if out.AccountedUntil, err = s.accountedUntil(ctx, st, x); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (m *money) scopeLabel(kind, id string) string {
	switch kind {
	case store.ScopeTeam:
		if t, ok := m.x.teams[id]; ok {
			return "Команда «" + t.Name + "»"
		}
		return "Команда"
	case store.ScopeTenant:
		if sp, ok := m.x.tenants[id]; ok {
			return "Тенант «" + sp.Name + "»"
		}
		return "Тенант"
	}
	return "Организация «" + m.x.org.Name + "»"
}

// orgKPIs are the twelve KPI of the scope's live sessions; money_per_mr counts the organization's money only.
// ?series=team adds them per team of the scope, by name.
func (s *Service) orgKPIs(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.View)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	if v := q.Get("series"); v != "" && v != "team" {
		return httpx.Invalid("series", "series — только team")
	}
	sp, err := parsePeriod(q, x.loc, x.now)
	if err != nil {
		return err
	}
	cmpSp, err := comparison(q, sp)
	if err != nil {
		return err
	}
	from, to := kpiRange(sp, cmpSp)
	ss, err := st.ScopeSessions(ctx, x.filter(from, to))
	if err != nil {
		return err
	}
	out := KpisResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}, Period: sp.period(), CmpPeriod: cmpSp.period()}
	if out.Kpis, err = kpisOf(ctx, st, x.loc, ss, sp, cmpSp, true); err != nil {
		return err
	}
	if q.Get("series") == "team" {
		out.Series = []KpiSeries{}
		teams := slices.SortedFunc(maps.Values(x.teams), func(a, b store.Team) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
		})
		for _, t := range teams {
			var mine []store.AgentSession
			for _, v := range ss {
				if v.TeamID == t.ID {
					mine = append(mine, v)
				}
			}
			kv, err := kpisOf(ctx, st, x.loc, mine, sp, cmpSp, true)
			if err != nil {
				return err
			}
			out.Series = append(out.Series, KpiSeries{Key: t.ID, Label: t.Name, Kpis: kv})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
