package analytics

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/limits"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Signals, «Требует внимания» (docs/hub/analytics.md, «Signals»): what the hub finds from the limits as they stand, kept
// so that a signal has a first moment, can be snoozed and can be raised when nobody acts. The overview and
// GET …/signals read the same rows, after the same sync.

const (
	// overPct is the share of a limit from which a person or a scope gets an over_80 signal.
	overPct = 0.8
	// escalateAfter is how long a signal waits for somebody before it is raised one level.
	escalateAfter = 48 * time.Hour
	// maxSnooze is the longest a signal may be snoozed for.
	maxSnooze = 30 * 24 * time.Hour
	// maxReason is the longest reason of a limit request, in characters.
	maxReason = 500
)

// The rules computed here.
const (
	ruleOver80       = "over_80"
	ruleBudget       = "budget_runs_out"
	ruleLimitRequest = "limit_request"
)

var severityRank = map[string]int{"critical": 0, "warn": 1, "info": 2}

func (s *Service) mountSignals(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/signals", s.gate(s.listSignals))
	mux.Handle("POST /api/orgs/{org}/signals/{id}/snooze", s.gate(s.snoozeSignal))
	mux.Handle("POST /api/me/limit-requests", s.gate(s.limitRequest))
}

func signalID(rule, key string) string {
	h := sha256.Sum256([]byte(rule + "|" + key))
	return "sig" + hex.EncodeToString(h[:])[:17]
}

// found is a signal the rules find now.
type found struct {
	rule, key string
	severity  string
	weight    float64 // how bad inside a severity
	addressee string  // the person it is addressed to by name, "" for the managers of its scope
	scope     orgs.ScopeView
	subject   SignalSubject
	title     string
	detail    string
	action    SignalAction
}

// row is f as a new row.
func (f found) row(orgKey string, now time.Time) store.Signal {
	g := store.Signal{ID: signalID(f.rule, f.key), OrgKey: orgKey, Rule: f.rule, DedupeKey: f.key, FirstAt: now, CreatedAt: now}
	return f.fill(g, now)
}

// fill writes what the rules say now into g, keeping its first moment and its snooze, unless it was resolved (then it is
// a new signal) or got worse (then the snooze is lifted).
func (f found) fill(g store.Signal, now time.Time) store.Signal {
	if g.ResolvedAt != nil {
		g.FirstAt, g.ActedAt, g.SnoozedUntil, g.ResolvedAt = now, nil, nil, nil
	}
	if severityRank[f.severity] < severityRank[g.Severity] && g.Severity != "" {
		g.SnoozedUntil = nil
	}
	act, _ := json.Marshal(f.action)
	g.SubjectKind, g.SubjectID, g.SubjectLabel = f.subject.Kind, f.subject.ID, f.subject.Label
	g.ScopeKind, g.ScopeID, g.AddresseeID = f.scope.Kind, f.scope.ID, f.addressee
	g.Severity, g.Weight, g.Title, g.Detail, g.Action, g.LastAt = f.severity, f.weight, f.title, f.detail, string(act), now
	return g
}

// findSignals are the signals the limits make now: a person or a scope past 80% of a limit (over_80), and a budget that
// runs out before the period ends at the pace of the last 7 days (budget_runs_out, of the scope asked for only).
func (s *Service) findSignals(ctx context.Context, st store.Store, m *money, total SpendTotal) ([]found, error) {
	var all []found
	for _, x := range m.book.States {
		r := x.Rule
		if r.Forbid || r.Unit == store.UnitWindowPct || x.Pct < overPct || !m.x.owns(r) {
			continue
		}
		sev := "warn"
		if x.Status == limits.Stop || x.Pct >= 1 {
			sev = "critical"
		}
		scope := orgs.ScopeView{Kind: r.ScopeKind, ID: r.ScopeID}
		f := found{rule: ruleOver80, key: r.ID, severity: sev, weight: x.Pct, scope: scope, detail: x.Reason}
		pct := int(math.Round(x.Pct * 100))
		if r.UserID != "" {
			p, err := m.person(ctx, st, r.UserID)
			if err != nil {
				return nil, err
			}
			name := "Неизвестно"
			if p != nil {
				name = p.Name
			}
			f.subject = SignalSubject{Kind: "user", ID: r.UserID, Label: name}
			f.addressee = r.UserID
			uid := r.UserID
			f.action = SignalAction{Kind: "raise_limit", Label: "Поднять лимит", Scope: scope, UserID: &uid}
			f.title = fmt.Sprintf("%s потратил(а) %d%% личного лимита", name, pct)
		} else {
			f.subject = SignalSubject{Kind: r.ScopeKind, ID: r.ScopeID, Label: m.scopeLabel(r.ScopeKind, r.ScopeID)}
			f.action = SignalAction{Kind: "raise_limit", Label: "Поднять лимит", Scope: scope}
			f.title = fmt.Sprintf("%s: потрачено %d%% лимита", f.subject.Label, pct)
		}
		all = append(all, f)
	}
	if l := total.Limit; l != nil && total.Forecast != nil && total.Forecast.USD > l.Amount.USD && l.Pct < 1 {
		scope := orgs.ScopeView{Kind: m.x.sc.Kind, ID: m.x.sc.ID}
		label := m.scopeLabel(m.x.sc.Kind, m.x.sc.ID)
		all = append(all, found{rule: ruleBudget, key: m.x.sc.Kind + ":" + m.x.sc.ID, severity: "warn", weight: total.Forecast.USD / l.Amount.USD,
			scope: scope, subject: SignalSubject{Kind: m.x.sc.Kind, ID: m.x.sc.ID, Label: label},
			title:  fmt.Sprintf("%s: бюджета не хватит до конца периода", label),
			detail: fmt.Sprintf("Прогноз %.0f $ при лимите %.0f $", total.Forecast.USD, l.Amount.USD),
			action: SignalAction{Kind: "raise_limit", Label: "Поднять лимит", Scope: scope}})
	}
	return all, nil
}

// limitChanged is whether somebody set a money limit for the person of a limit request in its scope (their own, or the
// scope's) after the request was last made.
func limitChanged(m *money, g store.Signal) bool {
	for _, x := range m.book.States {
		r := x.Rule
		if r.ScopeKind == g.ScopeKind && r.ScopeID == g.ScopeID && (r.UserID == g.SubjectID || r.UserID == "") && !r.Forbid &&
			r.Unit == store.UnitMoney && r.SetAt.After(g.LastAt) {
			return true
		}
	}
	return false
}

// syncSignals keeps what the rules find: a new signal is made, a known one keeps its first moment and snooze and gets
// a new last moment, and one whose condition no longer holds is resolved. Only what the scope asked for was looked at is
// resolved: over_80 under it, budget_runs_out of it. A limit_request is resolved once a limit of its scope is set for the
// person after it was made (limitChanged); otherwise it stays until the person asks again or it is resolved so.
func (s *Service) syncSignals(ctx context.Context, x *orgScope, m *money, fs []found) error {
	now := x.now
	var fresh []store.Signal
	err := s.db.Tx(ctx, func(st store.Store) error {
		fresh = fresh[:0]
		seen := map[string]bool{}
		for _, f := range fs {
			seen[f.rule+"|"+f.key] = true
			g, err := st.SignalByKey(ctx, x.org.ID, f.rule, f.key)
			switch {
			case store.IsNotFound(err):
				row := f.row(x.org.ID, now)
				ok, err := st.AddSignal(ctx, row)
				if err != nil {
					return err
				}
				if ok {
					fresh = append(fresh, row)
				}
			case err != nil:
				return err
			default:
				reopened := g.ResolvedAt != nil
				g = f.fill(g, now)
				if err = st.UpdateSignal(ctx, g); err != nil {
					return err
				}
				if reopened {
					fresh = append(fresh, g)
				}
			}
		}
		open, err := st.OpenSignals(ctx, x.org.ID)
		if err != nil {
			return err
		}
		for _, g := range open {
			if seen[g.Rule+"|"+g.DedupeKey] {
				continue
			}
			var gone bool
			switch g.Rule {
			case ruleOver80:
				gone = x.owns(store.LimitRule{ScopeKind: g.ScopeKind, ScopeID: g.ScopeID})
			case ruleBudget:
				gone = g.ScopeKind == x.sc.Kind && g.ScopeID == x.sc.ID
			case ruleLimitRequest:
				gone = limitChanged(m, g)
			default:
				// The checker's rules are found only by the checker, which has looked at the whole organization.
				gone = x.full && slices.Contains(orgRules, g.Rule)
			}
			if gone {
				g.ResolvedAt = &now
				if err := st.UpdateSignal(ctx, g); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, g := range fresh {
		s.announce(ctx, x.org.ID, x.now, g)
	}
	return nil
}

func latest(a time.Time, b *time.Time) time.Time {
	if b != nil && b.After(a) {
		return *b
	}
	return a
}

func snoozed(g store.Signal, now time.Time) bool {
	return g.SnoozedUntil != nil && g.SnoozedUntil.After(now)
}

// signalView is a kept signal as the API shows it. A signal nobody acted on (snoozed) for 48 hours is escalated.
func signalView(g store.Signal, now time.Time) Signal {
	v := Signal{ID: g.ID, Rule: g.Rule, Severity: g.Severity, Scope: orgs.ScopeView{Kind: g.ScopeKind, ID: g.ScopeID},
		Subject: SignalSubject{Kind: g.SubjectKind, ID: g.SubjectID, Label: g.SubjectLabel}, Title: g.Title, Detail: g.Detail,
		FirstAt: httpx.Time(g.FirstAt), LastAt: httpx.Time(g.LastAt), SnoozedUntil: httpx.TimePtr(g.SnoozedUntil)}
	v.Escalated = !snoozed(g, now) && now.Sub(latest(g.FirstAt, g.ActedAt)) >= escalateAfter
	if g.Action != "" {
		var a SignalAction
		if json.Unmarshal([]byte(g.Action), &a) == nil && a.Kind != "" {
			v.Action = &a
		}
	}
	return v
}

// visibleSignals are the open signals of the scope and under it the person may see: a manager's all of them, a
// member's only their own (about them, or addressed to them). budget_runs_out is only known for the scope looked at.
// Snoozed ones come with all; the order is critical first, escalated first, the worst first.
func (s *Service) visibleSignals(ctx context.Context, st store.Store, x *orgScope, userID string, manager, all bool) ([]Signal, error) {
	open, err := st.OpenSignals(ctx, x.org.ID)
	if err != nil {
		return nil, err
	}
	type ranked struct {
		Signal
		weight float64
	}
	var rs []ranked
	for _, g := range open {
		switch {
		case !x.owns(store.LimitRule{ScopeKind: g.ScopeKind, ScopeID: g.ScopeID}):
			continue
		case g.Rule == ruleBudget && (g.ScopeKind != x.sc.Kind || g.ScopeID != x.sc.ID):
			continue
		case !manager && g.AddresseeID != userID && !(g.SubjectKind == "user" && g.SubjectID == userID):
			continue
		case snoozed(g, x.now) && !all:
			continue
		}
		rs = append(rs, ranked{signalView(g, x.now), g.Weight})
	}
	slices.SortFunc(rs, func(a, b ranked) int {
		if c := cmp.Compare(severityRank[a.Severity], severityRank[b.Severity]); c != 0 {
			return c
		}
		if a.Escalated != b.Escalated {
			if a.Escalated {
				return -1
			}
			return 1
		}
		return cmpDesc(a.weight, b.weight, a.ID, b.ID)
	})
	out := make([]Signal, len(rs))
	for i, r := range rs {
		out[i] = r.Signal
	}
	return out, nil
}

// signals finds, keeps and reads the signals of the scope; m is the money of the current month.
func (s *Service) signals(ctx context.Context, st store.Store, x *orgScope, userID string, manager, all bool, m *money, total SpendTotal) ([]Signal, error) {
	fs, err := s.findSignals(ctx, st, m, total)
	if err != nil {
		return nil, err
	}
	if err := s.syncSignals(ctx, x, m, fs); err != nil {
		return nil, err
	}
	return s.visibleSignals(ctx, st, x, userID, manager, all)
}

// currentMoney is the money of the month now is in, whatever period a screen shows: signals are about now.
func (s *Service) currentMoney(ctx context.Context, st store.Store, x *orgScope) (*money, SpendTotal, error) {
	start := monthStart(x.now, x.loc)
	m, err := s.money(ctx, st, x, newSpan(start, start.AddDate(0, 1, 0), x.loc), "team")
	if err != nil {
		return nil, SpendTotal{}, err
	}
	t, err := m.total(ctx, st)
	return m, t, err
}

// attention is «Требует внимания» of the overview: the open, not snoozed signals of the scope, at most maxAttention. m
// is the overview's money; another period than the current month is not what signals are about, so they are made again
// from this month.
func (s *Service) attention(ctx context.Context, st store.Store, p *auth.Principal, x *orgScope, m *money, total SpendTotal) ([]Signal, error) {
	if !m.open || m.sp.months != 1 {
		var err error
		if m, total, err = s.currentMoney(ctx, st, x); err != nil {
			return nil, err
		}
	}
	out, err := s.signals(ctx, st, x, p.User.ID, true, false, m, total)
	if err != nil {
		return nil, err
	}
	return out[:min(len(out), maxAttention)], nil
}

// SignalsResponse is GET …/signals.
type SignalsResponse struct {
	Signals []Signal `json:"signals"`
}

func (s *Service) listSignals(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	o, sc, err := orgs.ScopeIn(ctx, st, p, r.PathValue("org"), r.URL.Query().Get("scope"), authz.View)
	manager := err == nil
	if err != nil {
		// A member of the scope sees their own signals there; ScopeIn hands back the scope with the refusal.
		var he *httpx.Error
		if !errors.As(err, &he) || he.Code != authz.ErrForbiddenScope.Code {
			return err
		}
		in, ierr := orgs.Belongs(ctx, st, o, sc, p.User.ID)
		if ierr != nil {
			return ierr
		}
		if !in {
			return err
		}
	}
	x, err := s.loadScope(ctx, st, o, sc)
	if err != nil {
		return err
	}
	m, total, err := s.currentMoney(ctx, st, x)
	if err != nil {
		return err
	}
	all := r.URL.Query().Get("all") == "1"
	out, err := s.signals(ctx, st, x, p.User.ID, manager, all, m, total)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, SignalsResponse{Signals: out})
	return nil
}

func (s *Service) snoozeSignal(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	var req struct {
		Until string `json:"until"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	st := s.db.S()
	o, err := orgs.Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	g, err := st.SignalByID(ctx, r.PathValue("id"))
	if store.IsNotFound(err) || (err == nil && (g.OrgKey != o.ID || g.ResolvedAt != nil)) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	sc := authz.Scope{Kind: g.ScopeKind, ID: g.ScopeID}
	if g.AddresseeID != p.User.ID {
		if _, err := authz.New(st).Can(ctx, orgs.Principal(p), authz.Check{Action: authz.Manage, Scope: sc}); err != nil {
			return err
		}
	}
	loc, err := limits.Location(ctx, st, o.ID)
	if err != nil {
		return err
	}
	now := s.now()
	until, ok := changeAt(strings.TrimSpace(req.Until), loc)
	switch {
	case !ok:
		return httpx.Invalid("until", "Срок — YYYY-MM-DD или RFC 3339")
	case !until.After(now):
		return httpx.Invalid("until", "Отложить можно только до будущего времени")
	case until.After(now.Add(maxSnooze)):
		return httpx.Invalid("until", "Отложить можно не больше чем на 30 дней")
	}
	until = until.UTC()
	err = s.db.Tx(ctx, func(st store.Store) error {
		chain, err := orgs.Chain(ctx, st, o, sc)
		if err != nil {
			return err
		}
		if g, err = st.SignalByID(ctx, g.ID); err != nil {
			return err
		}
		before := httpx.TimePtr(g.SnoozedUntil)
		g.SnoozedUntil, g.ActedAt = &until, &now
		if err := st.UpdateSignal(ctx, g); err != nil {
			return err
		}
		return st.AddAudit(ctx, orgs.Journal(chain, p.User.ID, "signal.snooze", "signal", g.ID,
			map[string]any{"snoozedUntil": before},
			map[string]any{"rule": g.Rule, "title": g.Title, "snoozedUntil": httpx.Time(until)}, now))
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"signal": signalView(g, now)})
	return nil
}

// LimitRequestResponse is the answer of POST /api/me/limit-requests.
type LimitRequestResponse struct {
	Request struct {
		ID     string           `json:"id"`
		Status string           `json:"status"`
		To     []orgs.PersonRef `json:"to"`
	} `json:"request"`
}

func personName(u store.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

// limitRequest is «Попросить больше»: a limit_request signal for those who can raise the person's limit in the scope.
func (s *Service) limitRequest(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	var req struct {
		AmountUsd float64        `json:"amountUsd"`
		Reason    string         `json:"reason"`
		Scope     orgs.ScopeView `json:"scope"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	switch {
	case !(req.AmountUsd > 0) || math.IsInf(req.AmountUsd, 0) || req.AmountUsd > 1e9:
		return httpx.Invalid("amountUsd", "Сумма — число больше нуля")
	case reason == "" || utf8.RuneCountInString(reason) > maxReason:
		return httpx.Invalid("reason", "Причина — от 1 до 500 символов")
	case req.Scope.Kind == "" || req.Scope.ID == "":
		return httpx.Invalid("scope", "Укажите область")
	}
	st := s.db.S()
	sc := authz.Scope{Kind: req.Scope.Kind, ID: req.Scope.ID}
	top, err := authz.New(st).Chain(ctx, sc)
	if err != nil {
		return err
	}
	if len(top) == 0 || top[0].Kind != store.ScopeOrg {
		return httpx.ErrNotFound
	}
	o, err := orgs.Resolve(ctx, st, p, top[0].ID)
	if err != nil {
		return err
	}
	chain, err := orgs.Chain(ctx, st, o, sc)
	if err != nil {
		return err
	}
	in, err := orgs.Belongs(ctx, st, o, sc, p.User.ID)
	if err != nil {
		return err
	}
	if !in {
		return authz.ErrForbiddenScope.With("scope", sc.Key())
	}
	ids, err := orgs.LimitRaisers(ctx, st, chain, p.User.ID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return httpx.Conflict("no_recipient", "Некому отправить запрос: выше вас в этой области никого нет")
	}
	var resp LimitRequestResponse
	resp.Request.Status, resp.Request.To = "sent", []orgs.PersonRef{}
	for _, id := range ids {
		u, err := st.UserByID(ctx, id)
		if store.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		resp.Request.To = append(resp.Request.To, orgs.PersonRef{UserID: u.ID, Name: personName(u)})
	}
	now := s.now()
	name := personName(p.User)
	scope := orgs.ScopeView{Kind: sc.Kind, ID: sc.ID}
	uid := p.User.ID
	f := found{rule: ruleLimitRequest, key: uid + "|" + sc.Kind + "|" + sc.ID, severity: "warn", scope: scope,
		subject: SignalSubject{Kind: "user", ID: uid, Label: name}, title: name + " просит увеличить лимит",
		detail: "Просит " + strconv.FormatFloat(req.AmountUsd, 'f', -1, 64) + " $: " + reason,
		action: SignalAction{Kind: "raise_limit", Label: "Поднять лимит", Scope: scope, UserID: &uid}}
	err = s.db.Tx(ctx, func(st store.Store) error {
		g, err := st.SignalByKey(ctx, o.ID, f.rule, f.key)
		switch {
		case store.IsNotFound(err):
			ok, err := st.AddSignal(ctx, f.row(o.ID, now))
			if err == nil && !ok { // made in the same moment by the same person: one request
				g, err = st.SignalByKey(ctx, o.ID, f.rule, f.key)
			}
			return err
		case err != nil:
			return err
		}
		// Asking again while the first waits: the new sum and reason, the first moment kept, the snooze lifted.
		g.SnoozedUntil, g.ActedAt = nil, nil
		return st.UpdateSignal(ctx, f.fill(g, now))
	})
	if err != nil {
		return err
	}
	resp.Request.ID = signalID(f.rule, f.key)
	if g, err := s.db.S().SignalByKey(ctx, o.ID, f.rule, f.key); err == nil {
		s.publishLimitRequest(ctx, o.ID, g)
		s.announce(ctx, o.ID, now, g)
	}
	httpx.WriteJSON(w, http.StatusCreated, resp)
	return nil
}
