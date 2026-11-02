// Package limits computes the state of the limit rules of an organization and the effective limit of each person
// (docs/hub/limits.md): every rule that applies to a call counts it, the tightest one wins. The hub shows the result and,
// unless HUB_LIMITS_ENFORCE=off, sends it to the machines (push.go).
package limits

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	OK   = hubproto.BudgetOK
	Warn = hubproto.BudgetWarn
	Stop = hubproto.BudgetStop

	// DefaultWarnPct is the share at which a rule warns unless set.
	DefaultWarnPct = 0.8

	hourMs = int64(3600000)
	// seenDays is how far back spending through an account makes it the person's account.
	seenDays = 31
)

// Length is a rolling period's length.
func Length(period string) time.Duration {
	switch period {
	case store.PeriodFiveHour:
		return 5 * time.Hour
	case store.PeriodSevenDay:
		return 7 * 24 * time.Hour
	}
	return 0
}

// State is a rule with what it counts now.
type State struct {
	Rule      store.LimitRule
	Used      float64 // in the rule's unit: dollars, tokens or percent of the window
	Pct       float64 // Used ÷ Amount; 1 for a forbid rule
	Status    string
	ResetsAt  *time.Time
	Estimated bool
	Reason    string
}

// Effective is the limit of a person for one account ("" any) and one model ("" any): the worst rule that applies.
type Effective struct {
	AccountID    string
	ModelPattern string
	Status       string
	Pct          float64
	Binding      string
	RuleIDs      []string // the binding one first
	Reason       string
	ResetsAt     *time.Time
}

// Person is where a person is in the organization: the tenants and the teams whose rules bind them.
type Person struct {
	UserID  string
	Tenants map[string]bool
	Teams   map[string]bool
}

// Book is the rules of one organization evaluated at one instant.
type Book struct {
	OrgKey   string
	Loc      *time.Location
	Now      time.Time
	States   []State
	Accounts map[string]store.Account

	grants map[string][]store.AccountGrant
	seen   map[string]map[string]bool // user → accounts they spent through lately
	names  map[string]string          // scope key → name
}

// Evaluate computes every rule of the organization (orgKey: store.OrgKey) at now, months cut in loc.
func Evaluate(ctx context.Context, st store.Store, orgKey string, loc *time.Location, now time.Time) (*Book, error) {
	b := &Book{OrgKey: orgKey, Loc: loc, Now: now, Accounts: map[string]store.Account{}, grants: map[string][]store.AccountGrant{},
		seen: map[string]map[string]bool{}, names: map[string]string{}}
	rules, err := st.LimitRulesByOrg(ctx, orgKey)
	if err != nil || len(rules) == 0 {
		return b, err
	}
	from := min(usage.MonthStart(now, loc).UnixMilli(), store.HourMs(now.Add(-seenDays*24*time.Hour).UnixMilli()))
	rows, err := st.RollupRows(ctx, orgKey, from, store.HourMs(now.UnixMilli())+hourMs)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range rules {
		ids = append(ids, r.AccountID)
	}
	for _, x := range rows {
		ids = append(ids, x.AccountID)
		if x.UserID != "" && x.AccountID != "" {
			if b.seen[x.UserID] == nil {
				b.seen[x.UserID] = map[string]bool{}
			}
			b.seen[x.UserID][x.AccountID] = true
		}
	}
	pay, err := usage.NewPayer(ctx, st, loc, ids)
	if err != nil {
		return nil, err
	}
	windows := map[string][]store.AccountWindow{}
	for _, id := range ids {
		if a, ok := pay.Account(id); ok {
			b.Accounts[id] = a
		}
	}
	for _, r := range rules {
		if r.AccountID == "" {
			continue
		}
		if _, ok := windows[r.AccountID]; !ok {
			if windows[r.AccountID], err = st.Windows(ctx, r.AccountID); err != nil {
				return nil, err
			}
			if b.grants[r.AccountID], err = st.AccountGrants(ctx, r.AccountID); err != nil {
				return nil, err
			}
		}
	}
	if err := b.loadNames(ctx, st, rules); err != nil {
		return nil, err
	}
	paid := make([]*int64, len(rows))
	for _, r := range rules {
		s, err := b.eval(ctx, r, rows, paid, pay, windows[r.AccountID])
		if err != nil {
			return nil, err
		}
		s.Reason = b.reason(s)
		b.States = append(b.States, s)
	}
	return b, nil
}

func (b *Book) loadNames(ctx context.Context, st store.Store, rules []store.LimitRule) error {
	for _, r := range rules {
		key := r.ScopeKind + ":" + r.ScopeID
		if _, ok := b.names[key]; ok {
			continue
		}
		switch r.ScopeKind {
		case store.ScopeTeam:
			t, err := st.TeamByID(ctx, r.ScopeID)
			if err != nil && !store.IsNotFound(err) {
				return err
			}
			b.names[key] = t.Name
		case store.ScopeTenant:
			sp, err := st.SpaceByID(ctx, r.ScopeID)
			if err != nil && !store.IsNotFound(err) {
				return err
			}
			b.names[key] = sp.Name
		}
	}
	return nil
}

// window is the account's measurement for a rule's period: the window of that name, of the rule's model when it has
// one, else of all models; else one of the period's length.
func window(ws []store.AccountWindow, r store.LimitRule) (store.AccountWindow, bool) {
	length := int64(Length(r.Period) / time.Second)
	best, rank := store.AccountWindow{}, 0
	for _, w := range ws {
		k := 0
		switch {
		case r.ModelPattern != "" && (w.Scope == "model:"+r.ModelPattern || w.Name == r.Period+"_"+r.ModelPattern):
			k = 4
		case w.Name == r.Period && (w.Scope == "" || w.Scope == "all"):
			k = 3
		case w.LengthSec == length && (w.Scope == "" || w.Scope == "all"):
			k = 2
		case w.LengthSec == length && r.ModelPattern == "":
			k = 1
		}
		if k > rank {
			best, rank = w, k
		}
	}
	return best, rank > 0
}

// span is [from, resetsAt) of a money or tokens rule: the calendar month, or the account's window when the rule has an
// account with a measured window of that length, or the last hours of the period.
func (b *Book) span(r store.LimitRule, ws []store.AccountWindow) (int64, *time.Time) {
	if r.Period == store.PeriodMonth || Length(r.Period) == 0 {
		m := usage.MonthStart(b.Now, b.Loc)
		next := m.AddDate(0, 1, 0)
		return m.UnixMilli(), &next
	}
	length := Length(r.Period)
	if r.AccountID != "" {
		if w, ok := window(ws, r); ok && w.ResetsAt != nil && *w.ResetsAt > b.Now.UnixMilli() {
			t := time.UnixMilli(*w.ResetsAt)
			return store.HourMs(t.Add(-length).UnixMilli()), &t
		}
	}
	return store.HourMs(b.Now.Add(-length).UnixMilli()), nil
}

// ModelMatches says whether a rule's model pattern covers a call's model: the same name, a family key of the price
// list, or a part of the name («opus-4-1» covers claude-opus-4-1-20250805).
func ModelMatches(pattern, model string) bool {
	if pattern == "" {
		return true
	}
	p, m := strings.ToLower(pattern), strings.ToLower(model)
	return m == p || strings.Contains(m, p) || usage.PriceKey(usage.ProviderAnthropic, m) == p || usage.PriceKey(usage.ProviderOpenAI, m) == p
}

// counts says whether a row of spend counts against the rule (only the organization's money counts).
func counts(r store.LimitRule, x store.RollupRow) bool {
	if x.Funding != store.FundingOrg {
		return false
	}
	switch r.ScopeKind {
	case store.ScopeTenant:
		if x.SpaceID != r.ScopeID {
			return false
		}
	case store.ScopeTeam:
		if x.TeamID != r.ScopeID {
			return false
		}
	}
	return (r.UserID == "" || x.UserID == r.UserID) && (r.AccountID == "" || x.AccountID == r.AccountID) && ModelMatches(r.ModelPattern, x.Model)
}

func (b *Book) eval(ctx context.Context, r store.LimitRule, rows []store.RollupRow, paid []*int64, pay *usage.Payer, ws []store.AccountWindow) (State, error) {
	s := State{Rule: r, Status: OK}
	if r.Forbid {
		s.Pct, s.Status = 1, Stop
		return s, nil
	}
	now := b.Now.UnixMilli()
	switch r.Unit {
	case store.UnitWindowPct:
		s.Estimated = true
		w, ok := window(ws, r)
		if !ok || w.UsedPct == nil || (w.ResetsAt != nil && *w.ResetsAt <= now) {
			break // not measured yet, or the window has reset since
		}
		s.Used = *w.UsedPct
		if w.ResetsAt != nil {
			t := time.UnixMilli(*w.ResetsAt)
			s.ResetsAt = &t
		}
		if r.UserID != "" {
			// The person's share of the window: their list on the account over the window's hours of everyone's.
			from := store.HourMs(now - int64(Length(r.Period)/time.Millisecond))
			if w.ResetsAt != nil {
				from = store.HourMs(*w.ResetsAt - int64(Length(r.Period)/time.Millisecond))
			}
			var mine, all int64
			for _, x := range rows {
				if x.Hour < from || x.AccountID != r.AccountID {
					continue
				}
				all += x.ListCostMicros
				if x.UserID == r.UserID {
					mine += x.ListCostMicros
				}
			}
			if all > 0 {
				s.Used *= float64(mine) / float64(all)
			} else {
				s.Used = 0
			}
		}
	default:
		from, resets := b.span(r, ws)
		s.ResetsAt = resets
		var sum int64
		for i, x := range rows {
			if x.Hour < from || !counts(r, x) {
				continue
			}
			if r.Unit == store.UnitTokens {
				sum += x.Input + x.Output + x.CacheRead + x.CacheWrite
				continue
			}
			if paid[i] == nil {
				v, err := pay.Paid(ctx, x.AccountID, x.Hour, x.ListCostMicros)
				if err != nil {
					return s, err
				}
				paid[i] = &v
			}
			sum += *paid[i]
		}
		if r.Unit == store.UnitTokens {
			s.Used = float64(sum)
		} else {
			s.Used = float64(sum) / 1e6
		}
	}
	switch {
	case r.Amount > 0:
		s.Pct = s.Used / r.Amount
	case s.Used > 0:
		s.Pct = 1
	}
	warn := r.WarnPct
	if warn <= 0 {
		warn = DefaultWarnPct
	}
	switch {
	case s.Pct >= 1:
		s.Status = Stop
	case s.Pct >= warn:
		s.Status = Warn
	}
	return s, nil
}

func rank(status string) int {
	switch status {
	case Stop:
		return 2
	case Warn:
		return 1
	}
	return 0
}

// Worse is the worse of two statuses.
func Worse(a, b string) string {
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// PersonOf is where the person is in the book's organization.
func (b *Book) PersonOf(ctx context.Context, st store.Store, userID string) (Person, error) {
	p := Person{UserID: userID, Tenants: map[string]bool{}, Teams: map[string]bool{}}
	sps, err := st.SpacesByUser(ctx, userID)
	if err != nil {
		return p, err
	}
	for _, sp := range sps {
		if store.OrgKey(sp.Space) == b.OrgKey {
			p.Tenants[sp.ID] = true
		}
	}
	teams, err := st.TeamsOfUser(ctx, userID)
	if err != nil {
		return p, err
	}
	for _, t := range teams {
		p.Teams[t.TeamID] = true
	}
	return p, nil
}

// in says whether the person is in a scope of the organization (or is that person).
func (p Person) in(kind, id string) bool {
	switch kind {
	case store.ScopeOrg:
		return true
	case store.ScopeTenant:
		return p.Tenants[id]
	case store.ScopeTeam:
		return p.Teams[id]
	case "user":
		return id == p.UserID
	}
	return false
}

// canUse says whether an account is the person's to spend through: they did lately, they own it, or it belongs or is
// granted to a scope they are in.
func (b *Book) canUse(p Person, accountID string) bool {
	if b.seen[p.UserID][accountID] {
		return true
	}
	a, ok := b.Accounts[accountID]
	if !ok {
		return false
	}
	if a.OwnerUserID == p.UserID || p.in(a.ScopeKind, a.ScopeID) {
		return true
	}
	for _, g := range b.grants[accountID] {
		if p.in(g.ScopeKind, g.ScopeID) {
			return true
		}
	}
	return false
}

// Applies says whether a rule binds the person.
func (b *Book) Applies(p Person, r store.LimitRule) bool {
	if r.UserID != "" && r.UserID != p.UserID {
		return false
	}
	if !p.in(r.ScopeKind, r.ScopeID) {
		return false
	}
	return r.AccountID == "" || b.canUse(p, r.AccountID)
}

// Rules is the states of the rules that bind the person.
func (b *Book) Rules(p Person) []State {
	var out []State
	for _, s := range b.States {
		if b.Applies(p, s.Rule) {
			out = append(out, s)
		}
	}
	return out
}

// Effective is the person's effective limits. One entry stands for every account and model (when any rule binds the
// person), and one more for each account or model whose own rule is the binding one there: what the machine needs to
// tell a stop through «Claude Max №2» from a warn anywhere else.
func (b *Book) Effective(p Person) []Effective {
	rules := b.Rules(p)
	if len(rules) == 0 {
		return nil
	}
	type combo struct{ account, model string }
	combos := []combo{{}}
	seen := map[combo]bool{{}: true}
	for _, s := range rules {
		c := combo{s.Rule.AccountID, s.Rule.ModelPattern}
		if !seen[c] {
			seen[c] = true
			combos = append(combos, c)
		}
	}
	var out []Effective
	for _, c := range combos {
		var hit, forbid []State
		for _, s := range rules {
			r := s.Rule
			if (r.AccountID != "" && r.AccountID != c.account) || (r.ModelPattern != "" && r.ModelPattern != c.model) {
				continue
			}
			hit = append(hit, s)
			if r.Forbid {
				forbid = append(forbid, s)
			}
		}
		if len(hit) == 0 {
			continue
		}
		if len(forbid) > 0 {
			hit = forbid
		}
		slices.SortStableFunc(hit, func(x, y State) int {
			if x.Rule.Forbid != y.Rule.Forbid {
				if x.Rule.Forbid {
					return -1
				}
				return 1
			}
			return cmp.Compare(y.Pct, x.Pct)
		})
		bind := hit[0]
		if (c != combo{}) && (bind.Rule.AccountID != c.account || bind.Rule.ModelPattern != c.model) {
			continue // a rule of every account or model binds here too: the wider entry already says it
		}
		e := Effective{AccountID: c.account, ModelPattern: c.model, Status: OK, Pct: bind.Pct, Binding: bind.Rule.ID,
			Reason: bind.Reason, ResetsAt: bind.ResetsAt}
		for _, s := range hit {
			e.Status = Worse(e.Status, s.Status)
			e.RuleIDs = append(e.RuleIDs, s.Rule.ID)
		}
		out = append(out, e)
	}
	order := func(e Effective) int {
		switch {
		case e.AccountID != "" && e.ModelPattern == "":
			return 0
		case e.AccountID != "":
			return 1
		case e.ModelPattern == "":
			return 2
		}
		return 3
	}
	slices.SortStableFunc(out, func(x, y Effective) int { return cmp.Compare(order(x), order(y)) })
	return out
}

// AccountLabel is the name of an account of the book.
func (b *Book) AccountLabel(id string) string {
	a, ok := b.Accounts[id]
	return usage.AccountLabel(a, ok)
}

var (
	monthNom = [...]string{"", "январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}
	monthGen = [...]string{"", "января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
)

// when is a reset time as the person reads it: «в 20:00» today, «1 ноября» at a midnight, else «3 ноября в 14:00».
func (b *Book) when(t time.Time) string {
	t, now := t.In(b.Loc), b.Now.In(b.Loc)
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return "в " + t.Format("15:04")
	}
	d := fmt.Sprintf("%d %s", t.Day(), monthGen[t.Month()])
	if t.Hour() == 0 && t.Minute() == 0 {
		return d
	}
	return d + " в " + t.Format("15:04")
}

func (b *Book) scopeName(r store.LimitRule) string {
	if n := b.names[r.ScopeKind+":"+r.ScopeID]; n != "" {
		return "«" + n + "»"
	}
	return ""
}

// subject is what the rule is, as a reason starts: «Личный лимит», «Бюджет команды «Платформа»», …
func (b *Book) subject(r store.LimitRule) string {
	var s string
	switch {
	case r.UserID != "" && r.AccountID != "":
		s = "Личный лимит на " + b.AccountLabel(r.AccountID)
	case r.AccountID != "":
		s = "Лимит на " + b.AccountLabel(r.AccountID)
		if r.ScopeKind != store.ScopeOrg {
			s += strings.TrimRight(" в "+map[string]string{store.ScopeTenant: "тенанте ", store.ScopeTeam: "команде "}[r.ScopeKind]+b.scopeName(r), " ")
		}
	case r.UserID != "":
		s = map[string]string{store.ScopeTeam: "Личный лимит", store.ScopeTenant: "Личный потолок", store.ScopeOrg: "Личный лимит в организации"}[r.ScopeKind]
	default:
		s = strings.TrimRight(map[string]string{store.ScopeTeam: "Бюджет команды ", store.ScopeTenant: "Бюджет тенанта ", store.ScopeOrg: "Бюджет организации"}[r.ScopeKind]+b.scopeName(r), " ")
	}
	if r.ModelPattern != "" {
		s += " на модель " + r.ModelPattern
	}
	if r.Unit == store.UnitTokens {
		s += " в токенах"
	}
	return s
}

func (b *Book) periodWords(r store.LimitRule) string {
	switch r.Period {
	case store.PeriodFiveHour:
		return "за 5 часов"
	case store.PeriodSevenDay:
		return "за 7 дней"
	}
	return "за " + monthNom[b.Now.In(b.Loc).Month()]
}

// reason is the rule's state in Russian, the text the person and the machine see.
func (b *Book) reason(s State) string {
	r := s.Rule
	if r.Forbid {
		where := "в организации"
		switch r.ScopeKind {
		case store.ScopeTenant:
			where = strings.TrimSpace("в тенанте " + b.scopeName(r))
		case store.ScopeTeam:
			where = strings.TrimSpace("в команде " + b.scopeName(r))
		}
		if r.AccountID != "" {
			where = "на " + b.AccountLabel(r.AccountID) + " " + where
		}
		return fmt.Sprintf("Модель %s %s запрещена", r.ModelPattern, where)
	}
	var txt string
	switch {
	case r.Unit == store.UnitWindowPct && r.UserID != "":
		txt = fmt.Sprintf("Ваша доля окна %s — примерно %d%%", b.AccountLabel(r.AccountID), int(math.Round(s.Used)))
	case r.Unit == store.UnitWindowPct:
		txt = fmt.Sprintf("Окно %s — %d%%", b.AccountLabel(r.AccountID), int(math.Round(s.Used)))
	case s.Status == Stop:
		txt = b.subject(r) + " " + b.periodWords(r) + " исчерпан"
	default:
		return fmt.Sprintf("%s — %d%% %s", b.subject(r), int(math.Round(s.Pct*100)), b.periodWords(r))
	}
	if s.ResetsAt != nil {
		txt += ", сброс " + b.when(*s.ResetsAt)
	}
	return txt
}
