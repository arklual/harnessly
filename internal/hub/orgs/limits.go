package orgs

import (
	"context"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/limits"
	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Limits: the rules of a scope, the effective limit of its people and of oneself (docs/hub/limits.md, «Endpoints»).

func (s *Service) mountLimits(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/limits", s.Gate(s.getLimits))
	mux.Handle("PUT /api/orgs/{org}/limits", s.Gate(s.putLimit))
	mux.Handle("DELETE /api/orgs/{org}/limits/{ruleId}", s.Gate(s.deleteLimit))
	mux.Handle("GET /api/me/limits", s.Gate(s.meLimits))
}

// PersonRef is a person as a limit names them.
type PersonRef struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
}

// LimitRuleView is LimitRule of web/app/hub-api/limits.ts.
type LimitRuleView struct {
	ID           string     `json:"id"`
	Scope        ScopeView  `json:"scope"`
	UserID       *string    `json:"userId"`
	AccountID    *string    `json:"accountId"`
	ModelPattern *string    `json:"modelPattern"`
	Forbid       bool       `json:"forbid"`
	Period       string     `json:"period"`
	Unit         string     `json:"unit"`
	Amount       float64    `json:"amount"`
	WarnPct      float64    `json:"warnPct"`
	Used         float64    `json:"used"`
	Pct          float64    `json:"pct"`
	Status       string     `json:"status"`
	ResetsAt     *string    `json:"resetsAt"`
	Estimated    bool       `json:"estimated"`
	SetBy        *PersonRef `json:"setBy"`
	SetAt        string     `json:"setAt"`
}

// AccountRef is an account as a limit names it.
type AccountRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// EffectiveView is EffectiveLimit.
type EffectiveView struct {
	Account       *AccountRef `json:"account"`
	ModelPattern  string      `json:"modelPattern"`
	Status        string      `json:"status"`
	Pct           float64     `json:"pct"`
	BindingRuleID string      `json:"bindingRuleId"`
	RuleIDs       []string    `json:"ruleIds"`
	Reason        string      `json:"reason"`
	ResetsAt      *string     `json:"resetsAt"`
}

// PersonLimits is one person's effective limits.
type PersonLimits struct {
	Person    PersonRef       `json:"person"`
	Effective []EffectiveView `json:"effective"`
}

// LimitCurrency is the rate of the shown amounts.
type LimitCurrency = rates.Currency

// LimitMoney is an amount in USD and in the shown currency.
type LimitMoney = rates.Money

// LimitsResponse is GET /api/orgs/{org}/limits.
type LimitsResponse struct {
	Scope        ScopeView       `json:"scope"`
	Currency     LimitCurrency   `json:"currency"`
	Mode         string          `json:"mode"`
	Rules        []LimitRuleView `json:"rules"`
	People       []PersonLimits  `json:"people"`
	HandedOutPct *float64        `json:"handedOutPct"`
	Budget       *LimitMoney     `json:"budget"`
}

// MachineBudgetView is what one of the person's machines holds.
type MachineBudgetView struct {
	MachineID string  `json:"machineId"`
	Name      string  `json:"name"`
	Version   int64   `json:"version"`
	SyncedAt  *string `json:"syncedAt"`
}

// MeLimitsResponse is GET /api/me/limits.
type MeLimitsResponse struct {
	Mode      string              `json:"mode"`
	Currency  LimitCurrency       `json:"currency"`
	Rules     []LimitRuleView     `json:"rules"`
	Effective []EffectiveView     `json:"effective"`
	Machines  []MachineBudgetView `json:"machines"`
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func timeStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := httpx.Time(*t)
	return &v
}

func strPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// currencyAt is the rate of the organization's currency for the month of now.
func (s *Service) currencyAt(ctx context.Context, orgKey string, now time.Time, loc *time.Location) LimitCurrency {
	code := "RUB"
	if orgKey != "" && !strings.HasPrefix(orgKey, "space:") {
		if o, err := s.db.S().OrgByID(ctx, orgKey); err == nil && o.Currency != "" {
			code = o.Currency
		}
	}
	return s.rates.For(ctx, code, usage.MonthStart(now, loc))
}

func (s *Service) mode() string {
	if s.cfg.LimitsEnforce == "" {
		return hubproto.EnforceOff
	}
	return s.cfg.LimitsEnforce
}

// views is the rules of the book with their state, setBy named.
type viewer struct {
	ctx   context.Context
	st    store.Store
	names map[string]*PersonRef
}

func (v *viewer) person(userID string) (*PersonRef, error) {
	if userID == "" {
		return nil, nil
	}
	if p, ok := v.names[userID]; ok {
		return p, nil
	}
	u, err := v.st.UserByID(v.ctx, userID)
	if store.IsNotFound(err) {
		v.names[userID] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &PersonRef{UserID: u.ID, Name: u.Name}
	if p.Name == "" {
		p.Name = u.Email
	}
	v.names[userID] = p
	return p, nil
}

func (v *viewer) rule(st limits.State) (LimitRuleView, error) {
	r := st.Rule
	by, err := v.person(r.SetBy)
	if err != nil {
		return LimitRuleView{}, err
	}
	warn := r.WarnPct
	if warn <= 0 {
		warn = limits.DefaultWarnPct
	}
	out := LimitRuleView{ID: r.ID, Scope: ScopeView{r.ScopeKind, r.ScopeID}, UserID: strPtr(r.UserID), AccountID: strPtr(r.AccountID),
		ModelPattern: strPtr(r.ModelPattern), Forbid: r.Forbid, Period: r.Period, Unit: r.Unit, Amount: r.Amount, WarnPct: warn,
		Used: round3(st.Used), Pct: round3(st.Pct), Status: st.Status, ResetsAt: timeStr(st.ResetsAt), Estimated: st.Estimated,
		SetBy: by, SetAt: httpx.Time(r.SetAt)}
	if r.Unit == store.UnitMoney {
		out.Used = math.Round(st.Used*100) / 100
	}
	return out, nil
}

func effectiveViews(b *limits.Book, es []limits.Effective) []EffectiveView {
	out := make([]EffectiveView, 0, len(es))
	for _, e := range es {
		v := EffectiveView{ModelPattern: e.ModelPattern, Status: e.Status, Pct: round3(e.Pct), BindingRuleID: e.Binding,
			RuleIDs: e.RuleIDs, Reason: e.Reason, ResetsAt: timeStr(e.ResetsAt)}
		if e.AccountID != "" {
			v.Account = &AccountRef{ID: e.AccountID, Label: b.AccountLabel(e.AccountID)}
		}
		out = append(out, v)
	}
	return out
}

// scopeParam is ?scope=kind:id, the organization when absent.
func scopeParam(o store.Org, v string) (authz.Scope, error) {
	if v == "" {
		return authz.Scope{Kind: store.ScopeOrg, ID: o.ID}, nil
	}
	kind, id, ok := strings.Cut(v, ":")
	if !ok || id == "" || (kind != store.ScopeOrg && kind != store.ScopeTenant && kind != store.ScopeTeam) {
		return authz.Scope{}, httpx.Invalid("scope", "Неверная область")
	}
	return authz.Scope{Kind: kind, ID: id}, nil
}

func (s *Service) getLimits(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	o, err := Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	sc, err := scopeParam(o, r.URL.Query().Get("scope"))
	if err != nil {
		return err
	}
	chain, err := chainIn(ctx, st, o, sc)
	if err != nil {
		return err
	}
	if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: authz.View, Scope: sc}); err != nil {
		return err
	}
	people, err := members(ctx, st, sc)
	if err != nil {
		return err
	}
	if only := r.URL.Query().Get("userId"); only != "" {
		people = slices.DeleteFunc(people, func(m MemberView) bool { return m.UserID != only })
		if len(people) == 0 {
			return httpx.ErrNotFound
		}
	}
	loc, err := limits.Location(ctx, st, o.ID)
	if err != nil {
		return err
	}
	now := s.now()
	book, err := limits.Evaluate(ctx, st, o.ID, loc, now)
	if err != nil {
		return err
	}
	onChain := map[string]bool{}
	for _, c := range chain {
		onChain[c.Key()] = true
	}
	ids := map[string]bool{}
	resp := LimitsResponse{Scope: ScopeView{sc.Kind, sc.ID}, Currency: s.currencyAt(ctx, o.ID, now, loc), Mode: s.mode(), Rules: []LimitRuleView{}, People: []PersonLimits{}}
	for _, m := range people {
		ids[m.UserID] = true
		person, err := book.PersonOf(ctx, st, m.UserID)
		if err != nil {
			return err
		}
		name := m.Name
		if name == "" {
			name = m.Email
		}
		resp.People = append(resp.People, PersonLimits{Person: PersonRef{UserID: m.UserID, Name: name}, Effective: effectiveViews(book, book.Effective(person))})
	}
	v := &viewer{ctx: ctx, st: st, names: map[string]*PersonRef{}}
	var budget, handed float64
	for _, x := range book.States {
		rl := x.Rule
		if !onChain[rl.ScopeKind+":"+rl.ScopeID] || (rl.UserID != "" && !ids[rl.UserID]) {
			continue
		}
		rv, err := v.rule(x)
		if err != nil {
			return err
		}
		resp.Rules = append(resp.Rules, rv)
		if rl.ScopeKind == sc.Kind && rl.ScopeID == sc.ID && rl.AccountID == "" && rl.ModelPattern == "" && !rl.Forbid &&
			rl.Period == store.PeriodMonth && rl.Unit == store.UnitMoney {
			if rl.UserID == "" {
				budget = rl.Amount
			} else {
				handed += rl.Amount
			}
		}
	}
	if budget > 0 {
		b := resp.Currency.USD(budget)
		resp.Budget = &b
		if sc.Kind != store.ScopeOrg {
			h := round3(handed / budget)
			resp.HandedOutPct = &h
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

// limitCheck is the right a rule needs: a personal limit is set from above the person, an account rule by whoever
// manages the scope, a scope's own budget from above the scope.
func limitCheck(r store.LimitRule) authz.Check {
	sc := authz.Scope{Kind: r.ScopeKind, ID: r.ScopeID}
	switch {
	case r.UserID != "":
		return authz.Check{Action: authz.SetPersonalLimit, Scope: sc, Target: r.UserID}
	case r.AccountID != "":
		return authz.Check{Action: authz.SetAccountLimit, Scope: sc}
	}
	return authz.Check{Action: authz.SetLimit, Scope: sc}
}

// ruleJournal is a rule as the journal keeps it.
func ruleJournal(r *store.LimitRule) any {
	if r == nil {
		return nil
	}
	return map[string]any{"scope": r.ScopeKind + ":" + r.ScopeID, "userId": strPtr(r.UserID), "accountId": strPtr(r.AccountID),
		"modelPattern": strPtr(r.ModelPattern), "forbid": r.Forbid, "period": r.Period, "unit": r.Unit, "amount": r.Amount, "warnPct": r.WarnPct}
}

func (s *Service) putLimit(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		Scope        ScopeView
		UserID       *string
		AccountID    *string
		ModelPattern *string
		Forbid       bool
		Period       string
		Unit         string
		Amount       float64
		WarnPct      *float64
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	deref := func(v *string) string {
		if v == nil {
			return ""
		}
		return strings.TrimSpace(*v)
	}
	rule := store.LimitRule{ScopeKind: req.Scope.Kind, ScopeID: req.Scope.ID, UserID: deref(req.UserID), AccountID: deref(req.AccountID),
		ModelPattern: strings.ToLower(deref(req.ModelPattern)), Forbid: req.Forbid, Period: req.Period, Unit: req.Unit, Amount: req.Amount,
		WarnPct: limits.DefaultWarnPct, SetBy: p.User.ID, SetAt: s.now().UTC()}
	if rule.Period == "" {
		rule.Period = store.PeriodMonth
	}
	if rule.Unit == "" {
		rule.Unit = store.UnitMoney
	}
	if req.WarnPct != nil {
		rule.WarnPct = *req.WarnPct
	}
	switch {
	case rule.ScopeKind != store.ScopeOrg && rule.ScopeKind != store.ScopeTenant && rule.ScopeKind != store.ScopeTeam || rule.ScopeID == "":
		return httpx.Invalid("scope", "Выберите, к чему лимит")
	case rule.Period != store.PeriodMonth && rule.Period != store.PeriodFiveHour && rule.Period != store.PeriodSevenDay:
		return httpx.Invalid("period", "Период — месяц, 5 часов или 7 дней")
	case rule.Unit != store.UnitMoney && rule.Unit != store.UnitTokens && rule.Unit != store.UnitWindowPct:
		return httpx.Invalid("unit", "Лимит — в деньгах, токенах или процентах окна")
	case rule.Forbid && rule.ModelPattern == "":
		return httpx.Invalid("modelPattern", "Укажите, какую модель запретить")
	case !rule.Forbid && (rule.Amount <= 0 || math.IsInf(rule.Amount, 0) || math.IsNaN(rule.Amount)):
		return httpx.Invalid("amount", "Лимит должен быть больше нуля")
	case rule.Unit == store.UnitWindowPct && (rule.AccountID == "" || rule.Period == store.PeriodMonth):
		return httpx.Invalid("unit", "Процент окна — только для аккаунта на 5 часов или 7 дней")
	case rule.Unit == store.UnitWindowPct && rule.Amount > 100:
		return httpx.Invalid("amount", "Процент окна — не больше 100")
	case rule.WarnPct <= 0 || rule.WarnPct > 1:
		return httpx.Invalid("warnPct", "Порог предупреждения — от 0 до 1")
	case len(rule.ModelPattern) > 100:
		return httpx.Invalid("modelPattern", "Слишком длинное имя модели")
	}
	if rule.Forbid {
		rule.Amount = 0
	}
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	rule.OrgKey = o.ID
	var saved store.LimitRule
	err = s.db.Tx(ctx, func(st store.Store) error {
		sc := authz.Scope{Kind: rule.ScopeKind, ID: rule.ScopeID}
		chain, err := chainIn(ctx, st, o, sc)
		if err != nil {
			return err
		}
		dec, err := authz.New(st).Can(ctx, Principal(p), limitCheck(rule))
		if err != nil {
			return err
		}
		if rule.UserID != "" {
			ms, err := members(ctx, st, sc)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(ms, func(m MemberView) bool { return m.UserID == rule.UserID }) {
				return httpx.Invalid("userId", "Этого человека нет в области")
			}
		}
		if rule.AccountID != "" {
			a, err := st.AccountByID(ctx, rule.AccountID)
			if err != nil || a.OrgKey != o.ID {
				if err != nil && !store.IsNotFound(err) {
					return err
				}
				return httpx.Invalid("accountId", "Такого аккаунта в организации нет")
			}
		}
		var before *store.LimitRule
		if old, err := st.LimitRuleByKey(ctx, rule); err == nil {
			before = &old
		} else if !store.IsNotFound(err) {
			return err
		}
		if saved, err = st.PutLimitRule(ctx, rule); err != nil {
			return err
		}
		e := event(chain, p.User.ID, "limit.set", "limit", saved.ID, ruleJournal(before), ruleJournal(&saved), rule.SetAt)
		e.Self = dec.Self
		return st.AddAudit(ctx, e)
	})
	if err != nil {
		return err
	}
	s.limitsChanged()
	view, err := s.ruleView(ctx, o, saved.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rule": view})
	return nil
}

// ruleView is one rule of the organization with its state now.
func (s *Service) ruleView(ctx context.Context, o store.Org, id string) (LimitRuleView, error) {
	st := s.db.S()
	loc, err := limits.Location(ctx, st, o.ID)
	if err != nil {
		return LimitRuleView{}, err
	}
	book, err := limits.Evaluate(ctx, st, o.ID, loc, s.now())
	if err != nil {
		return LimitRuleView{}, err
	}
	v := &viewer{ctx: ctx, st: st, names: map[string]*PersonRef{}}
	for _, x := range book.States {
		if x.Rule.ID == id {
			return v.rule(x)
		}
	}
	return LimitRuleView{}, httpx.ErrNotFound
}

func (s *Service) deleteLimit(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	o, err := Resolve(ctx, s.db.S(), p, r.PathValue("org"))
	if err != nil {
		return err
	}
	err = s.db.Tx(ctx, func(st store.Store) error {
		rule, err := st.LimitRuleByID(ctx, r.PathValue("ruleId"))
		if err != nil {
			return notFound(err)
		}
		if rule.OrgKey != o.ID {
			return httpx.ErrNotFound
		}
		chain, err := chainIn(ctx, st, o, authz.Scope{Kind: rule.ScopeKind, ID: rule.ScopeID})
		if err != nil {
			return err
		}
		dec, err := authz.New(st).Can(ctx, Principal(p), limitCheck(rule))
		if err != nil {
			return err
		}
		if _, err := st.DeleteLimitRule(ctx, rule.ID); err != nil {
			return err
		}
		e := event(chain, p.User.ID, "limit.remove", "limit", rule.ID, ruleJournal(&rule), nil, s.now().UTC())
		e.Self = dec.Self
		return st.AddAudit(ctx, e)
	})
	if err != nil {
		return err
	}
	s.limitsChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) limitsChanged() {
	if s.onLimits != nil {
		s.onLimits()
	}
}

// meLimits is the person's own limits in every organization they are in (or in ?org= only), and what their machines
// hold.
func (s *Service) meLimits(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	only := ""
	if ref := r.URL.Query().Get("org"); ref != "" {
		o, err := Resolve(ctx, st, p, ref)
		if err != nil {
			return err
		}
		only = o.ID
	}
	spaces, err := st.SpacesByUser(ctx, p.User.ID)
	if err != nil {
		return err
	}
	var keys []string
	var mine []store.Space
	for _, sp := range spaces {
		k := store.OrgKey(sp.Space)
		if only != "" && k != only {
			continue
		}
		mine = append(mine, sp.Space)
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	if only != "" && !slices.Contains(keys, only) {
		keys = append(keys, only) // an admin of the organization who is in none of its tenants
	}
	now := s.now()
	loc := time.UTC
	resp := MeLimitsResponse{Mode: s.mode(), Rules: []LimitRuleView{}, Effective: []EffectiveView{}, Machines: []MachineBudgetView{}}
	v := &viewer{ctx: ctx, st: st, names: map[string]*PersonRef{}}
	for i, k := range keys {
		l, err := limits.Location(ctx, st, k)
		if err != nil {
			return err
		}
		if i == 0 {
			loc = l
		}
		book, err := limits.Evaluate(ctx, st, k, l, now)
		if err != nil {
			return err
		}
		person, err := book.PersonOf(ctx, st, p.User.ID)
		if err != nil {
			return err
		}
		for _, x := range book.Rules(person) {
			rv, err := v.rule(x)
			if err != nil {
				return err
			}
			resp.Rules = append(resp.Rules, rv)
		}
		resp.Effective = append(resp.Effective, effectiveViews(book, book.Effective(person))...)
	}
	first := ""
	if len(keys) > 0 {
		first = keys[0]
	}
	resp.Currency = s.currencyAt(ctx, first, now, loc)
	for _, sp := range mine {
		ms, err := st.MachinesBySpace(ctx, sp.ID)
		if err != nil {
			return err
		}
		for _, m := range ms {
			mv := MachineBudgetView{MachineID: m.ID, Name: m.Name}
			b, err := st.MachineBudgetOf(ctx, m.ID)
			if err != nil && !store.IsNotFound(err) {
				return err
			}
			if err == nil {
				mv.Version = b.Version
				mv.SyncedAt = timeStr(&b.SyncedAt)
			}
			resp.Machines = append(resp.Machines, mv)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

// ScopeIn is the organization of ref and its scope ?scope= (the organization when ""), once the person may take action
// on it (authz.View or authz.Money): 404 for an organization they cannot see, 403 forbidden_scope otherwise.
func ScopeIn(ctx context.Context, st store.Store, p *auth.Principal, ref, scope string, action authz.Action) (store.Org, authz.Scope, error) {
	o, err := Resolve(ctx, st, p, ref)
	if err != nil {
		return o, authz.Scope{}, err
	}
	sc, err := scopeParam(o, scope)
	if err != nil {
		return o, sc, err
	}
	if _, err := chainIn(ctx, st, o, sc); err != nil {
		return o, sc, err
	}
	if _, err := authz.New(st).Can(ctx, Principal(p), authz.Check{Action: action, Scope: sc}); err != nil {
		return o, sc, err
	}
	return o, sc, nil
}

// MemberScopeIn is the scope for a person who only belongs to it: no admin or lead role, a member of its tenants. It is
// asked after ScopeIn refused, by the screens a member reads too (the machines they may open). It answers the tenants of
// the scope the person is in; none is the refusal ScopeIn gave.
func MemberScopeIn(ctx context.Context, st store.Store, p *auth.Principal, ref, scope string) (store.Org, authz.Scope, map[string]bool, error) {
	o, err := Resolve(ctx, st, p, ref)
	if err != nil {
		return o, authz.Scope{}, nil, err
	}
	sc, err := scopeParam(o, scope)
	if err != nil {
		return o, sc, nil, err
	}
	chain, err := chainIn(ctx, st, o, sc)
	if err != nil {
		return o, sc, nil, err
	}
	var candidates []string
	switch sc.Kind {
	case store.ScopeOrg:
		sps, err := st.SpacesByOrg(ctx, o.ID)
		if err != nil {
			return o, sc, nil, err
		}
		for _, sp := range sps {
			candidates = append(candidates, sp.ID)
		}
	default:
		for _, c := range chain {
			if c.Kind == store.ScopeTenant {
				candidates = append(candidates, c.ID)
			}
		}
	}
	in := map[string]bool{}
	for _, id := range candidates {
		role, err := st.Role(ctx, id, p.User.ID)
		if err != nil {
			return o, sc, nil, err
		}
		if role != "" {
			in[id] = true
		}
	}
	if len(in) == 0 {
		return o, sc, nil, authz.ErrForbiddenScope.With("scope", sc.Key()).With("needs", authz.NeedsTenantAdmin)
	}
	return o, sc, in, nil
}

// Chain is the scope with its ancestors, the organization first; not found when the scope is not the organization's.
func Chain(ctx context.Context, st store.Store, o store.Org, sc authz.Scope) ([]authz.Scope, error) {
	return chainIn(ctx, st, o, sc)
}

// Journal is an audit event on the last scope of chain, for another package to add.
func Journal(chain []authz.Scope, actor, action, targetKind, targetID string, before, after any, at time.Time) *store.AuditEvent {
	return event(chain, actor, action, targetKind, targetID, before, after, at)
}

// Members is the people of a scope (members).
func Members(ctx context.Context, st store.Store, sc authz.Scope) ([]MemberView, error) {
	return members(ctx, st, sc)
}
