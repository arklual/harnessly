package analytics

import (
	"cmp"
	"context"
	"math"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

// «Сегодня» of the overview (docs/hub/analytics.md, «Today and efficiency»): the tiles of the day with their 14-day
// strips, and the efficiency block. Everything is counted from the same sessions and hourly rows as the rest of the
// screen, so the numbers agree with the usage.tick events that move them.

// sparkDays is how many days the tiles' strips cover, today last.
const sparkDays = 14

// DayValue is one point of a tile's strip.
type DayValue struct {
	Day   string  `json:"day"`
	Value float64 `json:"value"`
}

// Today is the day of the scope: the money spent, the people at work and the tasks.
type Today struct {
	Day   string `json:"day"`
	Spend struct {
		// Paid and List are the organization's money (funding 'org') since midnight of its zone; Tokens are the tokens
		// of all funding of the scope, as usage.tick counts them.
		Paid   Money `json:"paid"`
		List   Money `json:"list"`
		Tokens int64 `json:"tokens"`
	} `json:"spend"`
	Active struct {
		// People had a live session going on today; Of is the people of the scope.
		People int `json:"people"`
		Of     int `json:"of"`
		// Who are all the people of the scope with their day, the working first, then by the last time they worked.
		Who []TodayPerson `json:"who"`
	} `json:"active"`
	Tasks struct {
		// Closed are the sessions that ended today with a merge request; InWork the chats going on now.
		Closed int `json:"closed"`
		InWork int `json:"inWork"`
	} `json:"tasks"`
	// Sparks are the last sparkDays days, oldest first, today included. spend is the day's dollars at API prices.
	Sparks struct {
		Spend  []DayValue `json:"spend"`
		Active []DayValue `json:"active"`
		Tasks  []DayValue `json:"tasks"`
	} `json:"sparks"`
}

// Efficiency is «Эффективность»: the tasks closed per person by team, and how many chats reach a result.
type Efficiency struct {
	// Teams are the teams of the scope (and «Без команды» when it has sessions), the most tasks per person first.
	Teams []EfficiencyRow `json:"teams"`
	// Reached is the share of the scope's chats of 3 turns or more that made a merge request (the completion KPI).
	Reached Reached `json:"reached"`
}

// Reached is a share of chats with the chats behind it; the value is null below minSessions of them.
type Reached struct {
	Value *float64 `json:"value"`
	N     int      `json:"n"`
}

// EfficiencyRow is one team.
type EfficiencyRow struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	People int    `json:"people"`
	// Closed are the sessions of the team that ended in the period with a merge request.
	Closed int `json:"closed"`
	// PerPersonMonth is Closed ÷ People, per calendar month for a period of whole months and scaled to 30 days for
	// any other; null without people.
	PerPersonMonth *float64 `json:"perPersonMonth"`
	Reached        Reached  `json:"reached"`
}

// TodayPerson is one person's day: the «Активны сегодня» tile opens the list of them.
type TodayPerson struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	// Working: a chat of theirs was heard from in the last minutes; Chats are their chats going on at some moment of today.
	Working bool `json:"working"`
	Chats   int  `json:"chats"`
	// Closed are their chats that ended today with a merge request.
	Closed int `json:"closed"`
	// LastAt is when they last worked through a machine (any day; RFC 3339), null when never.
	LastAt *string `json:"lastAt"`
	// Paid and List are today's money of the organization's accounts by them; Tokens of all their funding.
	Paid   Money `json:"paid"`
	List   Money `json:"list"`
	Tokens int64 `json:"tokens"`
}

// dayAgg is the money and tokens of one day, and the same by person.
type dayAgg struct {
	paid, list, tokens int64
	people             map[string]*dayAgg
}

func (a *dayAgg) person(id string) *dayAgg {
	if a.people == nil {
		a.people = map[string]*dayAgg{}
	}
	p := a.people[id]
	if p == nil {
		p = &dayAgg{}
		a.people[id] = p
	}
	return p
}

// peopleOf are the people of the scope: the members of the team, or of the tenants under the scope.
func peopleOf(ctx context.Context, st store.Store, x *orgScope) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if x.sc.Kind == store.ScopeTeam {
		ms, err := st.TeamMembers(ctx, x.sc.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			add(m.UserID)
		}
		return out, nil
	}
	for id := range x.tenants {
		ms, err := st.Members(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			add(m.UserID)
		}
	}
	slices.Sort(out)
	return out, nil
}

// spendByDay is the money of the organization's accounts (paid and at list) and the tokens of all funding of the scope
// per day of [from, to), keyed by 'YYYY-MM-DD' in the organization's zone.
func (s *Service) spendByDay(ctx context.Context, st store.Store, x *orgScope, from, to time.Time) (map[string]*dayAgg, error) {
	rows, err := st.RollupRows(ctx, x.org.ID, store.HourMs(from.UnixMilli()), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	var in []store.RollupRow
	var ids []string
	for _, r := range rows {
		if x.scopeRow(r) {
			in = append(in, r)
			ids = append(ids, r.AccountID)
		}
	}
	pay, err := usage.NewPayer(ctx, st, x.loc, ids)
	if err != nil {
		return nil, err
	}
	out := map[string]*dayAgg{}
	for _, r := range in {
		d := time.UnixMilli(r.Hour).In(x.loc).Format(day)
		a := out[d]
		if a == nil {
			a = &dayAgg{}
			out[d] = a
		}
		tokens := r.Input + r.Output + r.CacheRead + r.CacheWrite
		u := a.person(r.UserID)
		a.tokens += tokens
		u.tokens += tokens
		if r.Funding != store.FundingOrg {
			continue
		}
		p, err := pay.Paid(ctx, r.AccountID, r.Hour, r.ListCostMicros)
		if err != nil {
			return nil, err
		}
		a.paid += p
		a.list += r.ListCostMicros
		u.paid += p
		u.list += r.ListCostMicros
	}
	return out, nil
}

// endOf is when a session ended, or was last heard of while it runs.
func endOf(v store.AgentSession) int64 {
	if v.EndedAt != nil {
		return *v.EndedAt
	}
	return v.UpdatedAt
}

// closedTask is whether a session is a closed task: it ended and made a merge request.
func closedTask(v store.AgentSession) bool {
	return v.EndedAt != nil && metricsOf(v.Metrics)["mrs"] >= 1
}

// today is the day of the scope for the overview.
func (s *Service) today(ctx context.Context, st store.Store, x *orgScope, cur Currency) (Today, error) {
	var t Today
	today := midnight(x.now, x.loc)
	first := today.AddDate(0, 0, 1-sparkDays)
	t.Day = today.Format(day)

	spend, err := s.spendByDay(ctx, st, x, first, today.AddDate(0, 0, 1))
	if err != nil {
		return t, err
	}
	ss, err := st.ScopeSessionsOverlapping(ctx, x.filter(0, 0), first.UnixMilli(), today.AddDate(0, 0, 1).UnixMilli())
	if err != nil {
		return t, err
	}
	members, err := peopleOf(ctx, st, x)
	if err != nil {
		return t, err
	}
	of := map[string]bool{}
	for _, id := range members {
		of[id] = true
	}
	t.Active.Of = len(members)

	t.Sparks.Spend, t.Sparks.Active, t.Sparks.Tasks = []DayValue{}, []DayValue{}, []DayValue{}
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		a, b := d.UnixMilli(), d.AddDate(0, 0, 1).UnixMilli()
		people := map[string]bool{}
		closed := 0
		for _, v := range ss {
			if v.StartedAt < b && endOf(v) >= a && of[v.UserID] {
				people[v.UserID] = true
			}
			if e := endOf(v); closedTask(v) && e >= a && e < b {
				closed++
			}
		}
		key := d.Format(day)
		var list int64
		if g := spend[key]; g != nil {
			list = g.list
		}
		t.Sparks.Spend = append(t.Sparks.Spend, DayValue{Day: key, Value: math.Round(float64(list)/1e6*10000) / 10000})
		t.Sparks.Active = append(t.Sparks.Active, DayValue{Day: key, Value: float64(len(people))})
		t.Sparks.Tasks = append(t.Sparks.Tasks, DayValue{Day: key, Value: float64(closed)})
		if d.Equal(today) {
			t.Active.People, t.Tasks.Closed = len(people), closed
		}
	}
	g := spend[t.Day]
	if g == nil {
		g = &dayAgg{}
	}
	t.Spend.Paid, t.Spend.List, t.Spend.Tokens = cur.Micros(g.paid), cur.Micros(g.list), g.tokens
	since := x.now.Add(-activeWindow).UnixMilli()
	for _, v := range ss {
		if v.EndedAt == nil && v.UpdatedAt >= since {
			t.Tasks.InWork++
		}
	}
	if t.Active.Who, err = whoToday(ctx, st, x, members, ss, g, today, cur); err != nil {
		return t, err
	}
	return t, nil
}

// whoToday is the day of each person of the scope: the sessions are those of the strips, g today's money.
func whoToday(ctx context.Context, st store.Store, x *orgScope, members []string, ss []store.AgentSession, g *dayAgg,
	today time.Time, cur Currency) ([]TodayPerson, error) {
	a, b := today.UnixMilli(), today.AddDate(0, 0, 1).UnixMilli()
	since := x.now.Add(-activeWindow).UnixMilli()
	worked, err := st.LastWorked(ctx, []string{x.org.ID})
	if err != nil {
		return nil, err
	}
	out := make([]TodayPerson, 0, len(members))
	at := map[string]int64{}
	for _, id := range members {
		p := TodayPerson{UserID: id, Name: id}
		if u, err := st.UserByID(ctx, id); err == nil {
			p.Name, p.Email = cmp.Or(u.Name, u.Email), u.Email
		} else if !store.IsNotFound(err) {
			return nil, err
		}
		for _, v := range ss {
			if v.UserID != id {
				continue
			}
			e := endOf(v)
			if v.StartedAt < b && e >= a {
				p.Chats++
				if v.EndedAt == nil && v.UpdatedAt >= since {
					p.Working = true
				}
			}
			if closedTask(v) && e >= a && e < b {
				p.Closed++
			}
		}
		if w, ok := worked[id]; ok {
			at[id] = w
			s := time.UnixMilli(w).UTC().Format(time.RFC3339)
			p.LastAt = &s
		}
		if d := g.people[id]; d != nil {
			p.Paid, p.List, p.Tokens = cur.Micros(d.paid), cur.Micros(d.list), d.tokens
		}
		out = append(out, p)
	}
	slices.SortStableFunc(out, func(l, r TodayPerson) int {
		if l.Working != r.Working {
			if l.Working {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(at[r.UserID], at[l.UserID]); c != 0 {
			return c
		}
		return cmp.Compare(l.Name, r.Name)
	})
	return out, nil
}

// plainK is a session as the KPI that need no money see it.
func plainK(v store.AgentSession) kSession {
	return kSession{UserID: v.UserID, Start: v.StartedAt, End: endOf(v), M: metricsOf(v.Metrics)}
}

// reachedOf is the completion KPI over sessions.
func reachedOf(ks []kSession) Reached {
	r := Reached{N: len(ks)}
	for _, d := range kpis {
		if d.id == "completion" {
			r.Value = value(d, ks, 0)
		}
	}
	if r.Value != nil {
		r.Value = num(round3(*r.Value))
	}
	return r
}

// efficiency is the tasks closed per person by team in the period, and the share of chats that reached a result.
func (s *Service) efficiency(ctx context.Context, st store.Store, x *orgScope, sp span) (Efficiency, error) {
	out := Efficiency{Teams: []EfficiencyRow{}}
	from, to := sp.ms()
	started, err := st.ScopeSessions(ctx, x.filter(from, to))
	if err != nil {
		return out, err
	}
	ended, err := st.ScopeSessionsOverlapping(ctx, x.filter(0, 0), from, to)
	if err != nil {
		return out, err
	}
	ks := make([]kSession, len(started))
	byTeam := map[string][]kSession{}
	for i, v := range started {
		ks[i] = plainK(v)
		byTeam[v.TeamID] = append(byTeam[v.TeamID], ks[i])
	}
	out.Reached = reachedOf(ks)

	closed := map[string]int{}
	closers := map[string]map[string]bool{}
	for _, v := range ended {
		if e := endOf(v); closedTask(v) && e >= from && e < to {
			closed[v.TeamID]++
			if closers[v.TeamID] == nil {
				closers[v.TeamID] = map[string]bool{}
			}
			closers[v.TeamID][v.UserID] = true
		}
	}
	months := float64(sp.months)
	if months == 0 {
		months = sp.days() / 30
	}
	row := func(key, label string, people int) {
		r := EfficiencyRow{Key: key, Label: label, People: people, Closed: closed[key], Reached: reachedOf(byTeam[key])}
		if people > 0 && months > 0 {
			r.PerPersonMonth = num(math.Round(float64(r.Closed)/float64(people)/months*10) / 10)
		}
		out.Teams = append(out.Teams, r)
	}
	for id, t := range x.teams {
		ms, err := st.TeamMembers(ctx, id)
		if err != nil {
			return out, err
		}
		row(id, t.Name, len(ms))
	}
	// Sessions of nobody's team: the people are those who had them.
	if len(byTeam[""]) > 0 || closed[""] > 0 {
		people := map[string]bool{}
		for _, k := range byTeam[""] {
			people[k.UserID] = true
		}
		for u := range closers[""] {
			people[u] = true
		}
		row("", "Без команды", len(people))
	}
	slices.SortFunc(out.Teams, func(a, b EfficiencyRow) int {
		switch {
		case a.PerPersonMonth == nil && b.PerPersonMonth != nil:
			return 1
		case a.PerPersonMonth != nil && b.PerPersonMonth == nil:
			return -1
		case a.PerPersonMonth != nil:
			if c := cmp.Compare(*b.PerPersonMonth, *a.PerPersonMonth); c != 0 {
				return c
			}
		}
		return cmp.Or(cmp.Compare(a.Label, b.Label), cmp.Compare(a.Key, b.Key))
	})
	return out, nil
}
