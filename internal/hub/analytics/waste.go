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

// WasteKind is a way money goes to waste. There are four.
type WasteKind string

const (
	WasteAbandonedSessions WasteKind = "abandoned_sessions"
	WasteRetriesAfterError WasteKind = "retries_after_errors"
	WasteLongContext       WasteKind = "long_context"
	WasteExtraSkills       WasteKind = "extra_skills"
)

// wasteKinds are the kinds in the order they are told apart when the amounts are equal.
var wasteKinds = []WasteKind{WasteAbandonedSessions, WasteRetriesAfterError, WasteLongContext, WasteExtraSkills}

const (
	// abandonedTurns is how many turns a session needs to count as work that was not brought to a result.
	abandonedTurns = 3
	// longContextTokens is the context (input, cache read and cache writes) from which a call is long: it is paid for at
	// the whole context every turn.
	longContextTokens = 250_000
)

// WasteItem is one way money goes to waste. Title, Amount and Hint are the first version's fields and stay; Money is
// Amount under the name the screens use, and Explain says how the number was counted.
type WasteItem struct {
	Kind     WasteKind `json:"kind"`
	Title    string    `json:"title"`
	Amount   Money     `json:"amount"`
	Money    Money     `json:"money"`
	Share    float64   `json:"share"`
	Sessions int       `json:"sessions"`
	Hint     string    `json:"hint"`
	Explain  string    `json:"explain"`
}

// WasteResponse is GET …/waste.
type WasteResponse struct {
	Scope    orgs.ScopeView `json:"scope"`
	Period   Period         `json:"period"`
	Currency Currency       `json:"currency"`
	Items    []WasteItem    `json:"items"`
}

// wasteTexts are, per kind, the title, the advice and how it is counted.
var wasteTexts = map[WasteKind][3]string{
	WasteAbandonedSessions: {"Брошенные сессии", "Сессии без MR: работа не дошла до результата",
		"Полная стоимость завершённых сессий, где было три хода и больше и не появилось ни одного MR."},
	WasteRetriesAfterError: {"Повторы после ошибок", "Вызовы падают из-за окружения или прав: поправьте их, чтобы агент не тратил ходы впустую",
		"Оценка: стоимость сессии, умноженная на долю упавших вызовов инструментов. Сколько стоил каждый повтор, машины не измеряют."},
	WasteLongContext: {"Длинные сессии после 250 тыс. контекста", "Длинную сессию лучше сжать (/compact) или начать заново: каждый ход платит за весь контекст",
		"Стоимость вызовов, у которых контекст (вход, чтение и запись кэша) был не меньше 250 тыс. токенов."},
	WasteExtraSkills: {"Лишние скиллы на старте", "Отключите скиллы, которыми не пользуются: их описания едут в каждой сессии",
		"Оценка сверху: стоимость сессии, умноженная на долю входа, которую занимает стартовый контекст невостребованных скиллов (стартовый контекст × доля невостребованных × ходы)."},
}

// frac is a part of a whole, 0 for an empty whole.
func frac(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Min(1, part/whole)
}

// waste is the list money of the organization's live top-level sessions of the scope that started in the period, split
// by what it was wasted on (a session can be in several kinds, so the amounts are not added):
//   - abandoned_sessions: the whole list of an ended session with 3 turns or more and no MR (the completion KPI's rule);
//   - retries_after_errors: list × min(1, toolErrors ÷ toolCalls), a proxy: the price of one retry is not measured;
//   - long_context: the list cost of the calls (subagents' included) whose context is 250 000 tokens or more;
//   - extra_skills: an estimate, list × min(1, firstCtx × (skillsListed − skillsLoaded) ÷ skillsListed × turns ÷ input
//     tokens), where input tokens are inputTokens + cacheReadTokens + cacheWriteTokens.
//
// The old reread_context and failed_tools items are gone: the first lives on as the reread_context KPI, the second is
// retries_after_errors.
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
	refs := make([]store.SessionRef, len(org))
	for i, v := range org {
		refs[i] = store.SessionRef{OrgKey: v.OrgKey, Provider: v.Provider, SessionKey: v.SessionKey}
	}
	longSums, err := st.SessionSumsOver(ctx, refs, longContextTokens)
	if err != nil {
		return err
	}
	long := map[store.SessionRef]float64{}
	for _, c := range longSums {
		long[c.SessionRef] += float64(c.ListCostMicros)
	}
	for i, v := range org {
		list := float64(money[refs[i]].list)
		total += list
		if list <= 0 {
			continue
		}
		m := metricsOf(v.Metrics)
		var in float64
		for _, k := range input {
			in += m[k]
		}
		if m["toolCalls"] > 0 {
			add(WasteRetriesAfterError, list*frac(m["toolErrors"], m["toolCalls"]))
		}
		if v.EndedAt != nil && m["turns"] >= abandonedTurns && m["mrs"] == 0 {
			add(WasteAbandonedSessions, list)
		}
		add(WasteLongContext, math.Min(list, long[refs[i]]))
		if m["skillsListed"] > 0 && m["skillsLoaded"] < m["skillsListed"] && in > 0 {
			unused := (m["skillsListed"] - m["skillsLoaded"]) / m["skillsListed"]
			add(WasteExtraSkills, list*frac(m["firstCtx"]*unused*m["turns"], in))
		}
	}
	for _, k := range wasteKinds {
		t := got[k]
		if t == nil {
			continue
		}
		micros := int64(math.Round(t.micros))
		if micros <= 0 {
			continue
		}
		amount := cur.Micros(micros)
		out.Items = append(out.Items, WasteItem{Kind: k, Title: wasteTexts[k][0], Amount: amount, Money: amount,
			Share: round3(frac(t.micros, total)), Sessions: t.sessions, Hint: wasteTexts[k][1], Explain: wasteTexts[k][2]})
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
