package analytics

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The rules the background checker finds (docs/hub/analytics.md, «Signals»). Unlike over_80 and budget_runs_out, which
// are cut by the scope a screen asks for, these are about the organization as a whole and are looked for by checkOrg
// only: a screen reads what the checker kept. Every threshold is a constant here and a row of the rules' table there.
const (
	ruleMachineOffline = "machine_offline"
	ruleMachineIdle    = "machine_idle"
	ruleInvitePending  = "invite_pending"
	ruleWindow90       = "window_90"
	ruleToolErrorsUp   = "tool_errors_up"
	ruleUnknownAccount = "unknown_account"
	ruleModelNoPrice   = "model_no_price"
	ruleInvoiceMiss    = "invoice_mismatch"
	ruleChatWaiting    = "chat_waiting"
)

// orgRules are the rules only the checker finds and resolves.
var orgRules = []string{ruleMachineOffline, ruleMachineIdle, ruleInvitePending, ruleWindow90, ruleToolErrorsUp, ruleUnknownAccount,
	ruleModelNoPrice, ruleInvoiceMiss, ruleChatWaiting}

const (
	// machineOfflineAfter is how long a machine has not been heard from before machine_offline; machineOfflineCritical
	// is when it becomes critical.
	machineOfflineAfter    = 15 * time.Minute
	machineOfflineCritical = 24 * time.Hour
	// invitePendingAfter is how long an invitation by email waits before invite_pending; inviteWarnAfter raises it to warn.
	invitePendingAfter = 3 * 24 * time.Hour
	inviteWarnAfter    = 7 * 24 * time.Hour
	// windowPct is the use of a shared account's window from which window_90 is found; windowCriticalPct makes it critical.
	windowPct         = 90.0
	windowCriticalPct = 98.0
	// windowFresh is how old the measurement of a window may be to count: a window nobody has measured for a day says
	// nothing about now. windowSpare is the use below which another account takes the load.
	windowFresh = 24 * time.Hour
	windowSpare = 70.0
	// toolErrorsWeek is the length of the weeks tool_errors_up compares.
	toolErrorsWeek = 7
	// unpricedWindow is how far back calls without a price are looked for.
	unpricedWindow = 7 * 24 * time.Hour
	// chatWaitingAfter is how long a chat waits for its person before chat_waiting; chatWaitingWarn raises it to warn.
	// chatWaitingLook is how far back the sessions that may be waiting are looked for.
	chatWaitingAfter = time.Hour
	chatWaitingWarn  = 4 * time.Hour
	chatWaitingLook  = 7 * 24 * time.Hour
	// metricWaitingSince is the session metric (epoch ms) a machine's scanner sets while the agent waits for its person.
	metricWaitingSince = "waitingSince"
)

// findOrgSignals runs the checker's rules. A rule whose data is not there (no machines, no windows, no invoices) finds
// nothing; an error stops the whole check, so that nothing is resolved on a half look.
func (s *Service) findOrgSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	var all []found
	for _, rule := range []func(context.Context, store.Store, *orgScope) ([]found, error){
		s.machineSignals, s.inviteSignals, s.windowSignals, s.toolErrorSignals, s.accountSignals, s.priceSignals, s.invoiceSignals,
		s.chatSignals,
	} {
		fs, err := rule(ctx, st, x)
		if err != nil {
			return nil, err
		}
		all = append(all, fs...)
	}
	return all, nil
}

func tenantScope(id string) orgs.ScopeView { return orgs.ScopeView{Kind: store.ScopeTenant, ID: id} }

func since(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d >= 48*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + " дн."
	case d >= time.Hour:
		return strconv.Itoa(int(d.Hours())) + " ч"
	}
	return strconv.Itoa(max(1, int(d.Minutes()))) + " мин"
}

// machineSignals: machine_offline (not heard from for machineOfflineAfter) and machine_idle (no chat for a week).
func (s *Service) machineSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	if s.machines == nil {
		return nil, nil
	}
	var ms []store.Machine
	for id := range x.tenants {
		list, err := st.MachinesBySpace(ctx, id)
		if err != nil {
			return nil, err
		}
		ms = append(ms, list...)
	}
	hs, err := s.machines.HubMachines(ctx, st, ms)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.Machine{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	var out []found
	for _, h := range hs {
		m := byID[h.ID]
		subject := SignalSubject{Kind: "machine", ID: h.ID, Label: h.Name}
		scope := tenantScope(h.TenantID)
		act := SignalAction{Kind: "open_machine", Label: "Открыть машину", Target: h.ID, MachineID: h.ID}
		if slices.Contains(h.Problems, machines.ProblemOffline) && m.LastSeenAt != nil && x.now.Sub(*m.LastSeenAt) >= machineOfflineAfter {
			gone := x.now.Sub(*m.LastSeenAt)
			sev := "warn"
			if gone >= machineOfflineCritical {
				sev = "critical"
			}
			out = append(out, found{rule: ruleMachineOffline, key: h.ID, severity: sev, weight: gone.Hours(), scope: scope, subject: subject,
				title:  fmt.Sprintf("Машина «%s» не выходит на связь", h.Name),
				detail: "Последний раз на связи " + since(x.now, *m.LastSeenAt) + " назад", action: act})
		}
		if slices.Contains(h.Problems, machines.ProblemIdleWeek) {
			out = append(out, found{rule: ruleMachineIdle, key: h.ID, severity: "info", scope: scope, subject: subject,
				title:  fmt.Sprintf("Машина «%s» не нужна уже неделю", h.Name),
				detail: "За неделю на ней не было ни одного чата: выключите её или отдайте команде", action: act})
		}
	}
	return out, nil
}

// inviteSignals: an invitation by email that waited invitePendingAfter and is still good.
func (s *Service) inviteSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	var out []found
	for id, sp := range x.tenants {
		ivs, err := st.ActiveInvites(ctx, id, x.now)
		if err != nil {
			return nil, err
		}
		for _, iv := range ivs {
			waited := x.now.Sub(iv.CreatedAt)
			if iv.Kind != "email" || waited < invitePendingAfter {
				continue
			}
			sev := "info"
			if waited >= inviteWarnAfter {
				sev = "warn"
			}
			out = append(out, found{rule: ruleInvitePending, key: iv.ID, severity: sev, weight: waited.Hours(), scope: tenantScope(id),
				subject: SignalSubject{Kind: "invite", ID: iv.ID, Label: iv.Email},
				title:   fmt.Sprintf("%s не принял(а) приглашение в «%s»", iv.Email, sp.Name),
				detail:  "Приглашение ждёт " + since(x.now, iv.CreatedAt),
				action:  SignalAction{Kind: "resend_invite", Label: "Отправить ещё раз", Target: iv.ID, InviteID: iv.ID, TenantID: id}})
		}
	}
	return out, nil
}

// windowSignals: a window of an organization's shared account at windowPct or more before it resets. The action moves
// the load to the shared account of the same provider and kind with the most room, when there is one.
func (s *Service) windowSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	accs, err := st.AccountsByOrg(ctx, x.org.ID)
	if err != nil {
		return nil, err
	}
	type use struct {
		acc  store.Account
		pct  float64
		name string
		at   time.Time
	}
	var uses []use
	for _, a := range accs {
		if a.Funding != store.FundingOrg {
			continue
		}
		ws, err := st.Windows(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		var top *use
		for _, w := range ws {
			at := time.UnixMilli(w.MeasuredAt)
			if w.UsedPct == nil || x.now.Sub(at) > windowFresh || (w.ResetsAt != nil && !time.UnixMilli(*w.ResetsAt).After(x.now)) {
				continue
			}
			if top == nil || *w.UsedPct > top.pct {
				u := use{acc: a, pct: *w.UsedPct, name: w.Name, at: at}
				top = &u
			}
		}
		if top != nil {
			uses = append(uses, *top)
		}
	}
	var out []found
	for _, u := range uses {
		if u.pct < windowPct {
			continue
		}
		var to *use
		for i := range uses {
			c := &uses[i]
			if c.acc.ID == u.acc.ID || c.acc.Provider != u.acc.Provider || c.acc.Kind != u.acc.Kind || c.pct >= windowSpare {
				continue
			}
			if to == nil || c.pct < to.pct {
				to = c
			}
		}
		sev := "warn"
		if u.pct >= windowCriticalPct {
			sev = "critical"
		}
		scope := orgs.ScopeView{Kind: store.ScopeOrg, ID: x.org.ID}
		if u.acc.ScopeKind == store.ScopeTenant || u.acc.ScopeKind == store.ScopeTeam {
			scope = orgs.ScopeView{Kind: u.acc.ScopeKind, ID: u.acc.ScopeID}
		}
		label := accountLabel(u.acc, true)
		f := found{rule: ruleWindow90, key: u.acc.ID, severity: sev, weight: u.pct, scope: scope,
			subject: SignalSubject{Kind: "account", ID: u.acc.ID, Label: label},
			title:   fmt.Sprintf("%s: окно «%s» занято на %.0f%%", label, u.name, u.pct),
			detail:  "Ответы агентов на этом аккаунте замедлятся или остановятся до сброса окна"}
		if to != nil {
			toLabel := accountLabel(to.acc, true)
			f.action = SignalAction{Kind: "move_account", Label: "Перекинуть на " + toLabel, Target: to.acc.ID, FromAccountID: u.acc.ID, ToAccountID: to.acc.ID}
			f.detail += ". Свободнее всего «" + toLabel + "»: занято " + strconv.Itoa(int(to.pct)) + "%"
		}
		out = append(out, f)
	}
	return out, nil
}

// toolErrorSignals: the tool_errors KPI of the organization's last whole 7 days worse than the 7 before them by more than
// its noise (the KPI's own verdict). Per tool, tool_usage_day could say which tool; the KPI needs only the sessions.
func (s *Service) toolErrorSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	today := midnight(x.now, x.loc)
	sp := newSpan(today.AddDate(0, 0, -toolErrorsWeek), today, x.loc)
	cmp := newSpan(sp.from.AddDate(0, 0, -toolErrorsWeek), sp.from, x.loc)
	from, to := kpiRange(sp, cmp)
	ss, err := st.ScopeSessions(ctx, x.filter(from, to))
	if err != nil {
		return nil, err
	}
	ks, err := kSessionsOf(ctx, st, x.loc, ss, true)
	if err != nil {
		return nil, err
	}
	for _, v := range kpisFrom(ks, sp, cmp) {
		if v.Kpi != "tool_errors" || v.Verdict != "worse" || v.Value == nil || v.CmpValue == nil {
			continue
		}
		scope := orgs.ScopeView{Kind: store.ScopeOrg, ID: x.org.ID}
		return []found{{rule: ruleToolErrorsUp, key: x.org.ID, severity: "warn", weight: *v.Value - *v.CmpValue, scope: scope,
			subject: SignalSubject{Kind: "org", ID: x.org.ID, Label: x.org.Name},
			title:   "Инструменты агентов падают чаще",
			detail:  fmt.Sprintf("Доля ошибок %.1f%% против %.1f%% неделей раньше", *v.Value*100, *v.CmpValue*100),
			action:  SignalAction{Kind: "open_kpi", Label: "Открыть KPI", Target: "tool_errors", KPI: "tool_errors", Scope: scope}}}, nil
	}
	return nil, nil
}

// accountSignals: an account a machine of the organization used that nobody registered (the hub made it «discovered»
// and personal), with what it cost this month at list prices.
func (s *Service) accountSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	accs, err := st.AccountsByOrg(ctx, x.org.ID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, a := range accs {
		if a.Discovered {
			ids = append(ids, a.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	start := monthStart(x.now, x.loc)
	totals, err := st.AccountListTotals(ctx, ids, start.UnixMilli(), start.AddDate(0, 1, 0).UnixMilli())
	if err != nil {
		return nil, err
	}
	cur := s.currency(ctx, x.org.Currency, newSpan(start, start.AddDate(0, 1, 0), x.loc))
	var out []found
	for _, a := range accs {
		if !a.Discovered {
			continue
		}
		label := accountLabel(a, true)
		detail := "Машина использовала аккаунт, которого нет в списке: зарегистрируйте его или уберите ключ"
		if v := totals[a.ID]; v > 0 {
			detail += fmt.Sprintf(". В этом месяце по прайсу: %.0f $", cur.Micros(v).USD)
		}
		out = append(out, found{rule: ruleUnknownAccount, key: a.ID, severity: "warn", weight: float64(totals[a.ID]),
			scope:   orgs.ScopeView{Kind: store.ScopeOrg, ID: x.org.ID},
			subject: SignalSubject{Kind: "account", ID: a.ID, Label: label},
			title:   fmt.Sprintf("Незнакомый аккаунт: %s", label), detail: detail,
			action: SignalAction{Kind: "register_account", Label: "Зарегистрировать", Target: a.ID, AccountID: a.ID}})
	}
	return out, nil
}

// priceSignals: a model with calls the hub could not price in the last unpricedWindow. The money of those calls is not
// known, so none is guessed.
func (s *Service) priceSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	rows, err := st.RollupRows(ctx, x.org.ID, store.HourMs(x.now.Add(-unpricedWindow).UnixMilli()), x.now.UnixMilli()+1)
	if err != nil {
		return nil, err
	}
	calls := map[string]int64{}
	for _, r := range rows {
		if r.Unpriced > 0 && r.Model != "" {
			calls[r.Model] += r.Unpriced
		}
	}
	models := make([]string, 0, len(calls))
	for m := range calls {
		models = append(models, m)
	}
	slices.Sort(models)
	var out []found
	for _, m := range models {
		out = append(out, found{rule: ruleModelNoPrice, key: strings.ToLower(m), severity: "warn", weight: float64(calls[m]),
			scope:   orgs.ScopeView{Kind: store.ScopeOrg, ID: x.org.ID},
			subject: SignalSubject{Kind: "model", ID: m, Label: m},
			title:   fmt.Sprintf("У модели %s нет цены", m),
			detail:  fmt.Sprintf("%d вызовов за неделю не посчитаны в деньгах", calls[m]),
			action:  SignalAction{Kind: "open_prices", Label: "Задать цену", Target: m, Model: m}})
	}
	return out, nil
}

// invoiceSignals: invoice_mismatch is the estimate differing from the provider's bill by more than 5%. The hub has no
// provider bills (no table, no ingest), so there is nothing to compare and the rule finds nothing until they exist; it
// is in the registry so that the screens and the table of rules are complete.
func (s *Service) invoiceSignals(context.Context, store.Store, *orgScope) ([]found, error) {
	return nil, nil
}

// chatSignals: a chat whose machine reports that the agent has waited for its person (metric waitingSince) longer than
// chatWaitingAfter. The person it waits for gets the signal.
func (s *Service) chatSignals(ctx context.Context, st store.Store, x *orgScope) ([]found, error) {
	f := x.filter(x.now.Add(-chatWaitingLook).UnixMilli(), x.now.UnixMilli()+1)
	ss, err := st.ScopeSessions(ctx, f)
	if err != nil {
		return nil, err
	}
	var out []found
	for _, v := range ss {
		if v.EndedAt != nil || v.MachineID == "" {
			continue
		}
		at := int64(metricsOf(v.Metrics)[metricWaitingSince])
		if at <= 0 {
			continue
		}
		waited := x.now.Sub(time.UnixMilli(at))
		if waited < chatWaitingAfter {
			continue
		}
		sev := "info"
		if waited >= chatWaitingWarn {
			sev = "warn"
		}
		name := "Неизвестно"
		var scope orgs.ScopeView = tenantScope(v.SpaceID)
		if v.SpaceID == "" {
			scope = orgs.ScopeView{Kind: store.ScopeOrg, ID: x.org.ID}
		}
		if v.UserID != "" {
			if u, err := st.UserByID(ctx, v.UserID); err == nil {
				name = personName(u)
			} else if !store.IsNotFound(err) {
				return nil, err
			}
		}
		out = append(out, found{rule: ruleChatWaiting, key: v.Provider + "|" + v.SessionKey, severity: sev, weight: waited.Hours(),
			addressee: v.UserID, scope: scope, subject: SignalSubject{Kind: "user", ID: v.UserID, Label: name},
			title:  "Чат ждёт ответа " + since(x.now, time.UnixMilli(at)),
			detail: name + ": агент остановился и ждёт человека",
			action: SignalAction{Kind: "open_chat", Label: "Открыть чат", Target: v.SessionKey, MachineID: v.MachineID, SessionKey: v.SessionKey}})
	}
	return out, nil
}
