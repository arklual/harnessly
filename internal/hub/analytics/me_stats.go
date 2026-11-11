package analytics

import (
	"cmp"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// statsDays is how far back GET /api/me/stats looks, as the runtime's own report does (usagereport.WindowDays).
const statsDays = 400

// StatsDay is one day's activity in the shape of a runtime's usage report (internal/usagereport). The hub keeps the
// sums of a session, not the moments inside it, so a session's turns, working time, tool calls and merge requests are
// counted on the day it started.
type StatsDay struct {
	Prompts     float64   `json:"prompts"`
	Sessions    float64   `json:"sessions"`
	TurnMs      float64   `json:"turnMs"`
	Tools       float64   `json:"tools"`
	ToolErrors  float64   `json:"toolErrors"`
	Interrupts  float64   `json:"interrupts"`
	Denials     float64   `json:"denials"`
	Compactions float64   `json:"compactions"`
	PRs         float64   `json:"prs"`
	Hours       []float64 `json:"hours"`
}

// StatsSession is a session in the shape of a runtime report's session. Time is in seconds.
type StatsSession struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Title        *string  `json:"title"`
	Project      *string  `json:"project"`
	Start        float64  `json:"start"`
	End          float64  `json:"end"`
	Prompts      float64  `json:"prompts"`
	TurnMs       float64  `json:"turnMs"`
	Input        float64  `json:"input"`
	Output       float64  `json:"output"`
	CacheRead    float64  `json:"cacheRead"`
	CacheWrite   float64  `json:"cacheWrite"`
	Cost         float64  `json:"cost"`
	SubagentCost float64  `json:"subagentCost"`
	Model        *string  `json:"model"`
	Tasks        []string `json:"tasks"`
	PRs          []string `json:"prs"`
}

// StatsReport is the report the «Статистика» page reads (the answer of a runtime's <platform API>/stats, shape=report),
// made of the hub's facts. Usage rows are [day, provider, model, project, input, output, cacheRead, cacheWrite, costUsd,
// apiCalls]; the cost is the list price in dollars. HubMissing names the blocks of the page the hub holds no facts for.
type StatsReport struct {
	Version     int                 `json:"version"`
	Person      string              `json:"person"`
	Machine     string              `json:"machine"`
	GeneratedAt string              `json:"generatedAt"`
	Timezone    string              `json:"timezone"`
	Days        map[string]StatsDay `json:"days"`
	Usage       [][]any             `json:"usage"`
	Sessions    []StatsSession      `json:"sessions"`
	Collecting  any                 `json:"collecting"`
	HubMissing  []string            `json:"hubMissing"`
}

// StatsRuntime and MeStatsResponse make the answer a team of one, as a runtime's own answer is.
type StatsRuntime struct {
	Person      string       `json:"person"`
	MachineID   string       `json:"machineId"`
	MachineName string       `json:"machineName"`
	Report      *StatsReport `json:"report"`
}

// MeStatsResponse is GET /api/me/stats.
type MeStatsResponse struct {
	Viewer   string         `json:"viewer"`
	Runtimes []StatsRuntime `json:"runtimes"`
}

// statsMissing are the blocks of the page the hub cannot fill: the hours of the day a prompt was sent, the project or
// folder, the task keys of the chats, and the interrupts and refusals of the tools. The page says so in these blocks.
var statsMissing = []string{"punchcard", "projects", "tasks", "interrupts"}

func (s *Service) meStats(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	_, loc, _, err := s.scopeOf(ctx, st, p, "")
	if err != nil {
		return err
	}
	now := s.now()
	end := midnight(now, loc).AddDate(0, 0, 1)
	from, to := end.AddDate(0, 0, -statsDays).UnixMilli(), end.UnixMilli()

	ss, err := st.UserSessions(ctx, store.SessionFilter{UserID: p.User.ID, From: from, To: to})
	if err != nil {
		return err
	}
	refs := make([]store.SessionRef, len(ss))
	for i, x := range ss {
		refs[i] = store.SessionRef{OrgKey: x.OrgKey, Provider: x.Provider, SessionKey: x.SessionKey}
	}
	sums, _, err := st.SessionSums(ctx, refs)
	if err != nil {
		return err
	}
	bySession := map[store.SessionRef][]store.SessionCallSum{}
	for _, x := range sums {
		bySession[x.SessionRef] = append(bySession[x.SessionRef], x)
	}
	buckets, err := st.UserCallBuckets(ctx, p.User.ID, from, to)
	if err != nil {
		return err
	}

	rep := &StatsReport{Version: 1, Person: p.User.Email, Machine: "hub", GeneratedAt: httpx.Time(now), Timezone: loc.String(),
		Days: map[string]StatsDay{}, Usage: [][]any{}, Sessions: []StatsSession{}, HubMissing: statsMissing}
	for i, x := range ss {
		met := metricsOf(x.Metrics)
		dk := time.UnixMilli(x.StartedAt).In(loc).Format(day)
		d, ok := rep.Days[dk]
		if !ok {
			d.Hours = make([]float64, 24)
		}
		d.Sessions++
		d.Prompts += met["turns"]
		d.TurnMs += met["agentMs"]
		d.Tools += met["toolCalls"]
		d.ToolErrors += met["toolErrors"]
		d.PRs += met["mrs"]
		rep.Days[dk] = d

		endMs := x.UpdatedAt
		if x.EndedAt != nil {
			endMs = *x.EndedAt
		}
		v := StatsSession{ID: x.SessionKey, Provider: x.Provider, Start: float64(x.StartedAt) / 1000, End: float64(endMs) / 1000,
			Prompts: met["turns"], TurnMs: met["agentMs"], Tasks: []string{}, PRs: []string{}}
		models := bySession[refs[i]]
		slices.SortFunc(models, func(a, b store.SessionCallSum) int {
			if c := cmp.Compare(b.ListCostMicros, a.ListCostMicros); c != 0 {
				return c
			}
			return cmp.Compare(a.Model, b.Model)
		})
		for _, m := range models {
			v.Input += float64(m.Input)
			v.Output += float64(m.Output)
			v.CacheRead += float64(m.CacheRead)
			v.CacheWrite += float64(m.CacheWrite)
			v.Cost += float64(m.ListCostMicros) / 1e6
		}
		if len(models) > 0 {
			m := models[0].Model
			v.Model = &m
		}
		rep.Sessions = append(rep.Sessions, v)
	}
	// Newest first.
	slices.SortFunc(rep.Sessions, func(a, b StatsSession) int { return cmp.Or(cmp.Compare(b.End, a.End), cmp.Compare(a.ID, b.ID)) })

	type key struct{ day, provider, model string }
	type sum struct{ in, out, cr, cw, calls, list int64 }
	acc := map[key]*sum{}
	var order []key
	for _, b := range buckets {
		k := key{time.UnixMilli(b.Hour).In(loc).Format(day), b.Provider, b.Model}
		a := acc[k]
		if a == nil {
			a = &sum{}
			acc[k] = a
			order = append(order, k)
		}
		a.in, a.out, a.cr, a.cw, a.calls, a.list = a.in+b.Input, a.out+b.Output, a.cr+b.CacheRead, a.cw+b.CacheWrite, a.calls+b.Calls, a.list+b.ListCostMicros
	}
	slices.SortFunc(order, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.day, b.day), cmp.Compare(a.provider, b.provider), cmp.Compare(a.model, b.model))
	})
	for _, k := range order {
		a := acc[k]
		rep.Usage = append(rep.Usage, []any{k.day, k.provider, k.model, "", a.in, a.out, a.cr, a.cw, float64(a.list) / 1e6, a.calls})
	}

	httpx.WriteJSON(w, http.StatusOK, MeStatsResponse{Viewer: p.User.Email,
		Runtimes: []StatsRuntime{{Person: p.User.Email, MachineID: "hub", MachineName: "hub", Report: rep}}})
	return nil
}
