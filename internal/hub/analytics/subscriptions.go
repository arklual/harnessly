package analytics

import (
	"cmp"
	"context"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

// The organization's subscriptions (docs/hub/analytics.md, «As built»): each subscription account of the
// organization with its fee split between the people of the scope and the rate-limit windows the hub has measured.

// WindowView is one measured rate-limit window of an account.
type WindowView struct {
	Name           string   `json:"name"`
	Scope          string   `json:"scope"`
	LengthSec      int64    `json:"lengthSec"`
	UsedPct        *float64 `json:"usedPct"`
	ResetsAt       *string  `json:"resetsAt"`
	MeasuredAt     string   `json:"measuredAt"`
	Confidence     string   `json:"confidence"`
	Waiting        bool     `json:"waiting"`
	ForecastFullAt *string  `json:"forecastFullAt"`
}

// SubscriptionShare is one person's part of a subscription's fee.
type SubscriptionShare struct {
	Person    orgs.PersonRef `json:"person"`
	Share     float64        `json:"share"`
	Amount    Money          `json:"amount"`
	WindowPct *float64       `json:"windowPct"`
}

// Subscription is one subscription account.
type Subscription struct {
	AccountID   string              `json:"accountId"`
	Label       string              `json:"label"`
	Provider    string              `json:"provider"`
	Plan        string              `json:"plan"`
	MonthlyFee  Money               `json:"monthlyFee"`
	Preliminary bool                `json:"preliminary"`
	Shares      []SubscriptionShare `json:"shares"`
	Windows     []WindowView        `json:"windows"`
}

// SubscriptionsResponse is GET …/subscriptions.
type SubscriptionsResponse struct {
	Scope         orgs.ScopeView `json:"scope"`
	Period        Period         `json:"period"`
	Currency      Currency       `json:"currency"`
	Subscriptions []Subscription `json:"subscriptions"`
}

func (s *Service) mountSubscriptions(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/subscriptions", s.gate(s.subscriptions))
}

func (s *Service) subscriptions(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
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
	cur := s.currency(ctx, x.org.Currency, sp)
	subs, err := s.subscriptionsOf(ctx, st, x, sp, cur)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, SubscriptionsResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID},
		Period: sp.period(), Currency: cur, Subscriptions: subs})
	return nil
}

// reaches says whether an account is the scope's to look at: it belongs or is granted to the scope, above it or in it.
func (x *orgScope) reaches(kind, id string) bool {
	switch x.sc.Kind {
	case store.ScopeOrg:
		return true
	case store.ScopeTenant:
		if kind == store.ScopeTeam {
			_, ok := x.teams[id]
			return ok
		}
		return kind == store.ScopeOrg && id == x.org.ID || kind == store.ScopeTenant && id == x.sc.ID
	}
	if kind == store.ScopeTenant {
		_, ok := x.tenants[id]
		return ok
	}
	return kind == store.ScopeOrg && id == x.org.ID || kind == store.ScopeTeam && id == x.sc.ID
}

// reachesAccount says whether an account belongs or is granted to the scope, above it or in it.
func (x *orgScope) reachesAccount(ctx context.Context, st store.Store, a store.Account) (bool, error) {
	if x.reaches(a.ScopeKind, a.ScopeID) {
		return true, nil
	}
	gs, err := st.AccountGrants(ctx, a.ID)
	if err != nil {
		return false, err
	}
	for _, g := range gs {
		if x.reaches(g.ScopeKind, g.ScopeID) {
			return true, nil
		}
	}
	return false, nil
}

// subscriptionsOf is the organization's subscription accounts as the scope sees them in the period. An account that
// reaches the scope, or that the scope has used in the period, is listed, with no shares when nobody used it.
func (s *Service) subscriptionsOf(ctx context.Context, st store.Store, x *orgScope, sp span, cur Currency) ([]Subscription, error) {
	all, err := st.AccountsByOrg(ctx, x.org.ID)
	if err != nil {
		return nil, err
	}
	var subs []store.Account
	for _, a := range all {
		if a.Funding == store.FundingOrg && a.Kind == "subscription" {
			subs = append(subs, a)
		}
	}
	out := []Subscription{}
	if len(subs) == 0 {
		return out, nil
	}
	ids := make([]string, len(subs))
	for i, a := range subs {
		ids[i] = a.ID
	}
	from, to := sp.ms()
	rows, err := st.RollupRows(ctx, x.org.ID, from, to)
	if err != nil {
		return nil, err
	}
	pay, err := usage.NewPayer(ctx, st, x.loc, ids)
	if err != nil {
		return nil, err
	}
	totals, err := st.AccountListTotals(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	type mine struct{ list, paid, calls int64 }
	byAccount := map[string]map[string]*mine{}
	for _, r := range rows {
		if !x.in(r) {
			continue
		}
		if _, ok := pay.Account(r.AccountID); !ok {
			continue
		}
		paid, err := pay.Paid(ctx, r.AccountID, r.Hour, r.ListCostMicros)
		if err != nil {
			return nil, err
		}
		people := byAccount[r.AccountID]
		if people == nil {
			people = map[string]*mine{}
			byAccount[r.AccountID] = people
		}
		m := people[r.UserID]
		if m == nil {
			m = &mine{}
			people[r.UserID] = m
		}
		m.list += r.ListCostMicros
		m.paid += paid
		m.calls += r.Calls
	}

	windows := map[string][]store.AccountWindow{}
	primary := map[string]*store.AccountWindow{}
	nowMs := x.now.UnixMilli()
	winFrom := int64(math.MaxInt64)
	for _, a := range subs {
		ws, err := st.Windows(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		windows[a.ID] = ws
		if w := primaryWindow(ws, nowMs); w != nil {
			primary[a.ID] = w
			winFrom = min(winFrom, windowStart(*w, nowMs))
		}
	}
	// Everyone's list since the start of each primary window: a person's part of the window.
	winRows := map[string][]store.RollupRow{}
	if len(primary) > 0 {
		recent, err := st.RollupRows(ctx, x.org.ID, winFrom, store.HourMs(nowMs)+3600000)
		if err != nil {
			return nil, err
		}
		for _, r := range recent {
			if primary[r.AccountID] != nil {
				winRows[r.AccountID] = append(winRows[r.AccountID], r)
			}
		}
	}

	names := map[string]orgs.PersonRef{}
	person := func(id string) (orgs.PersonRef, error) {
		if p, ok := names[id]; ok {
			return p, nil
		}
		p := orgs.PersonRef{UserID: id, Name: "Неизвестно"}
		if id != "" {
			u, err := st.UserByID(ctx, id)
			switch {
			case store.IsNotFound(err):
			case err != nil:
				return p, err
			default:
				p.Name = cmp.Or(u.Name, u.Email)
			}
		}
		names[id] = p
		return p, nil
	}

	last := sp.to.AddDate(0, 0, -1)
	preliminary := x.now.Before(monthStart(last, x.loc).AddDate(0, 1, 0))
	for _, a := range subs {
		people := byAccount[a.ID]
		if len(people) == 0 {
			ok, err := x.reachesAccount(ctx, st, a)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		sub := Subscription{AccountID: a.ID, Label: accountLabel(a, true), Provider: a.Provider, Plan: a.Plan,
			MonthlyFee: cur.Micros(a.MonthlyFeeMicros), Preliminary: preliminary, Shares: []SubscriptionShare{}, Windows: []WindowView{}}
		for uid, m := range people {
			if m.calls == 0 && m.list == 0 && m.paid == 0 {
				continue
			}
			ref, err := person(uid)
			if err != nil {
				return nil, err
			}
			sh := SubscriptionShare{Person: ref, Amount: cur.Micros(m.paid)}
			if t := totals[a.ID]; t > 0 {
				sh.Share = math.Round(float64(m.list)/float64(t)*1e4) / 1e4
			}
			if w := primary[a.ID]; w != nil {
				sh.WindowPct = personWindowPct(*w, winRows[a.ID], uid, nowMs)
			}
			sub.Shares = append(sub.Shares, sh)
		}
		slices.SortFunc(sub.Shares, func(l, r SubscriptionShare) int {
			return cmp.Or(cmp.Compare(r.Amount.USD, l.Amount.USD), cmp.Compare(l.Person.Name, r.Person.Name), cmp.Compare(l.Person.UserID, r.Person.UserID))
		})
		for _, w := range windows[a.ID] {
			sub.Windows = append(sub.Windows, windowView(w, nowMs))
		}
		out = append(out, sub)
	}
	slices.SortFunc(out, func(l, r Subscription) int {
		return cmp.Or(cmp.Compare(r.MonthlyFee.USD, l.MonthlyFee.USD), cmp.Compare(l.Label, r.Label), cmp.Compare(l.AccountID, r.AccountID))
	})
	return out, nil
}

// usedPct is a window's use in percent: as measured, or from used and limit when the provider counts in units.
func usedPct(w store.AccountWindow) *float64 {
	if w.UsedPct != nil {
		return w.UsedPct
	}
	if w.Used != nil && w.Limit != nil && *w.Limit > 0 {
		v := *w.Used / *w.Limit * 100
		return &v
	}
	return nil
}

func reset(w store.AccountWindow, now int64) bool { return w.ResetsAt != nil && *w.ResetsAt <= now }

// primaryWindow is the window a person's part is told by: of all models, measured, not reset since, the shortest.
func primaryWindow(ws []store.AccountWindow, now int64) *store.AccountWindow {
	var best *store.AccountWindow
	for i := range ws {
		w := &ws[i]
		if (w.Scope != "" && w.Scope != "all") || usedPct(*w) == nil || reset(*w, now) {
			continue
		}
		if best == nil || w.LengthSec < best.LengthSec {
			best = w
		}
	}
	return best
}

// windowStart is the hour the window began in: its reset less its length, or the length before now.
func windowStart(w store.AccountWindow, now int64) int64 {
	end := now
	if w.ResetsAt != nil {
		end = *w.ResetsAt
	}
	return store.HourMs(end - w.LengthSec*1000)
}

// personWindowPct is the window's use × the person's part of the account's list over the window's hours, the way the
// limits take a person's window_pct (docs/hub/limits.md); nil when the account had no use in those hours.
func personWindowPct(w store.AccountWindow, rows []store.RollupRow, userID string, now int64) *float64 {
	pct := usedPct(w)
	if pct == nil {
		return nil
	}
	from := windowStart(w, now)
	var mine, all int64
	for _, r := range rows {
		if r.Hour < from {
			continue
		}
		all += r.ListCostMicros
		if r.UserID == userID {
			mine += r.ListCostMicros
		}
	}
	if all <= 0 {
		return nil
	}
	v := math.Round(*pct*float64(mine)/float64(all)*10) / 10
	return &v
}

func windowView(w store.AccountWindow, now int64) WindowView {
	v := WindowView{Name: w.Name, Scope: w.Scope, LengthSec: w.LengthSec, UsedPct: usedPct(w), MeasuredAt: httpx.Time(time.UnixMilli(w.MeasuredAt)),
		Confidence: w.Confidence, Waiting: reset(w, now)}
	if w.ResetsAt != nil {
		t := time.UnixMilli(*w.ResetsAt)
		v.ResetsAt = httpx.TimePtr(&t)
	}
	// Full at the pace the window has had since it began, if that is before it resets.
	if v.UsedPct != nil && *v.UsedPct > 0 && w.ResetsAt != nil && !v.Waiting {
		begin := *w.ResetsAt - w.LengthSec*1000
		if el := w.MeasuredAt - begin; el > 0 {
			full := begin + int64(float64(el)*100 / *v.UsedPct)
			if full < *w.ResetsAt && full >= w.MeasuredAt {
				t := time.UnixMilli(full)
				v.ForecastFullAt = httpx.TimePtr(&t)
			}
		}
	}
	return v
}
