package analytics

import (
	"math"
	"net/http"
	"slices"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

// «Куда уходят деньги впустую» (docs/hub/analytics.md, «Endpoints»): the part of the organization's list spend that the
// sessions' metrics show went to waste.

// WasteKind is a way money goes to waste.
type WasteKind string

const (
	WasteRereadContext     WasteKind = "reread_context"
	WasteFailedTools       WasteKind = "failed_tools"
	WasteAbandonedSessions WasteKind = "abandoned_sessions"
)

// WasteItem is one way money goes to waste.
type WasteItem struct {
	Kind     WasteKind `json:"kind"`
	Title    string    `json:"title"`
	Amount   Money     `json:"amount"`
	Share    float64   `json:"share"`
	Sessions int       `json:"sessions"`
	Hint     string    `json:"hint"`
}

// WasteResponse is GET …/waste.
type WasteResponse struct {
	Scope    orgs.ScopeView `json:"scope"`
	Period   Period         `json:"period"`
	Currency Currency       `json:"currency"`
	Items    []WasteItem    `json:"items"`
}

var wasteTexts = map[WasteKind][2]string{
	WasteRereadContext:     {"Перечитывание контекста", "Агент заново читает то, что уже было в контексте: короче сессии и /compact раньше"},
	WasteFailedTools:       {"Упавшие вызовы инструментов", "Вызовы падают из-за окружения или прав: поправьте их, чтобы агент не тратил ходы впустую"},
	WasteAbandonedSessions: {"Брошенные сессии", "Сессии без MR: работа не дошла до результата"},
}

// frac is a part of a whole, 0 for an empty whole.
func frac(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Min(1, part/whole)
}

// waste is the list money of the organization's live top-level sessions of the scope that started in the period, split
// by what it was wasted on:
//   - reread_context: list × min(1, carryTokens ÷ (inputTokens + cacheReadTokens + cacheWriteTokens));
//   - failed_tools: list × min(1, toolErrors ÷ toolCalls);
//   - abandoned_sessions: the whole list of an ended session with 3 turns or more and no MR (the completion KPI's rule).
//
// long_waits and oversized_model are not computed yet.
func (s *Service) waste(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
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
	from, to := sp.ms()
	ss, err := st.ScopeSessions(ctx, x.filter(from, to))
	if err != nil {
		return err
	}
	var org []store.AgentSession
	for _, v := range ss {
		if v.Funding == store.FundingOrg {
			org = append(org, v)
		}
	}
	out := WasteResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}, Period: sp.period(), Currency: cur, Items: []WasteItem{}}
	if len(org) == 0 {
		httpx.WriteJSON(w, http.StatusOK, out)
		return nil
	}
	pay, err := usage.NewPayer(ctx, st, x.loc, accountIDs(org))
	if err != nil {
		return err
	}
	money, err := moneyOf(ctx, st, pay, org)
	if err != nil {
		return err
	}
	type tally struct {
		micros   float64
		sessions int
	}
	var total float64
	got := map[WasteKind]*tally{}
	add := func(k WasteKind, micros float64) {
		if micros <= 0 {
			return
		}
		t := got[k]
		if t == nil {
			t = &tally{}
			got[k] = t
		}
		t.micros += micros
		t.sessions++
	}
	for _, v := range org {
		list := float64(money[store.SessionRef{OrgKey: v.OrgKey, Provider: v.Provider, SessionKey: v.SessionKey}].list)
		total += list
		if list <= 0 {
			continue
		}
		m := metricsOf(v.Metrics)
		var in float64
		for _, k := range input {
			in += m[k]
		}
		if in > 0 {
			add(WasteRereadContext, list*frac(m["carryTokens"], in))
		}
		if m["toolCalls"] > 0 {
			add(WasteFailedTools, list*frac(m["toolErrors"], m["toolCalls"]))
		}
		if v.EndedAt != nil && m["turns"] >= 3 && m["mrs"] == 0 {
			add(WasteAbandonedSessions, list)
		}
	}
	for _, k := range []WasteKind{WasteRereadContext, WasteFailedTools, WasteAbandonedSessions} {
		t := got[k]
		if t == nil {
			continue
		}
		micros := int64(math.Round(t.micros))
		if micros <= 0 {
			continue
		}
		out.Items = append(out.Items, WasteItem{Kind: k, Title: wasteTexts[k][0], Amount: cur.Micros(micros),
			Share: round3(frac(t.micros, total)), Sessions: t.sessions, Hint: wasteTexts[k][1]})
	}
	slices.SortStableFunc(out.Items, func(a, b WasteItem) int {
		switch {
		case a.Amount.USD > b.Amount.USD:
			return -1
		case a.Amount.USD < b.Amount.USD:
			return 1
		}
		return 0
	})
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
