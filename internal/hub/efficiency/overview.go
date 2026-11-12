package efficiency

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

// The overview and the personal tab of the screen (IEffOverview and IEffMe in web/app/types/efficiency.ts). Every number comes
// from what machines already send: the hourly rollup of calls and the metrics of agent_sessions. The hub has no transcript
// scanner, so a figure that needs one is null (never 0) and the screen shows «no data» for it.

const (
	defaultDays = 30
	over250k    = 250_000
	meSessions  = 100
)

// OverviewDay is IEffDay.
type OverviewDay struct {
	Day                 string   `json:"day"`
	CostUSD             float64  `json:"costUsd"`
	Sessions            int      `json:"sessions"`
	ModelCalls          int64    `json:"modelCalls"`
	MRs                 int      `json:"mrs"`
	AvgCtx              *float64 `json:"avgCtx"`
	FirstCtxP50         *float64 `json:"firstCtxP50"`
	CarryShare          *float64 `json:"carryShare"`
	ToolErrorRate       *float64 `json:"toolErrorRate"`
	GuardStopRate       *float64 `json:"guardStopRate"`
	RedGateRate         *float64 `json:"redGateRate"`
	SubagentSonnetShare *float64 `json:"subagentSonnetShare"`
	GateVisibleShare    *float64 `json:"gateVisibleShare"`
	HumanReplyP90Min    *float64 `json:"humanReplyP90Min"`
	SleepHours          float64  `json:"sleepHours"`
	CIPollsPerMR        *float64 `json:"ciPollsPerMr"`
}

// OverviewChannel is IEffChannel.
type OverviewChannel struct {
	Channel  string  `json:"channel"`
	Sessions int     `json:"sessions"`
	CostUSD  float64 `json:"costUsd"`
	MRs      int     `json:"mrs"`
}

// OverviewTotals is the totals of IEffOverview.
type OverviewTotals struct {
	CostUSD      float64  `json:"costUsd"`
	Sessions     int      `json:"sessions"`
	MRs          int      `json:"mrs"`
	CarryTokens  int64    `json:"carryTokens"`
	ResultTokens int64    `json:"resultTokens"`
	ReadingShare *float64 `json:"readingShare"`
}

// Overview is the answer of the overview endpoint.
type Overview struct {
	Scope     string            `json:"scope"`
	From      string            `json:"from"`
	To        string            `json:"to"`
	Days      []OverviewDay     `json:"days"`
	Markers   []Marker          `json:"markers"`
	Channels  []OverviewChannel `json:"channels"`
	Totals    OverviewTotals    `json:"totals"`
	ScannedAt *string           `json:"scannedAt"`
}

// Me is IEffMe.
type Me struct {
	Person    string      `json:"person"`
	From      string      `json:"from"`
	To        string      `json:"to"`
	Attention MeAttention `json:"attention"`
	Limit     MeLimit     `json:"limit"`
	Sessions  []MeSession `json:"sessions"`
}

// MeAttention is how much the person's attention costs the agents.
type MeAttention struct {
	ReplyP50Min    *float64 `json:"replyP50Min"`
	ReplyP90Min    *float64 `json:"replyP90Min"`
	AutonomyP90Min *float64 `json:"autonomyP90Min"`
	AskWaitP90Min  *float64 `json:"askWaitP90Min"`
	ParallelP90    *float64 `json:"parallelP90"`
	Questions      int      `json:"questions"`
	Interrupts     int      `json:"interrupts"`
}

// MeLimit is the subscription limit: the fullest seven-day window of Claude among the person's machines that reported
// lately (machine_provider_limits); empty when none did.
type MeLimit struct {
	WeekUsedPct *float64 `json:"weekUsedPct"`
	EvalWeekPct float64  `json:"evalWeekPct"`
	ResetsAt    *string  `json:"resetsAt"`
}

// MeSession is IEffSession.
type MeSession struct {
	FileID        string    `json:"fileId"`
	SessionUUID   *string   `json:"sessionUuid"`
	Title         *string   `json:"title"`
	Channel       string    `json:"channel"`
	StartedAt     *string   `json:"startedAt"`
	EndedAt       *string   `json:"endedAt"`
	CostUSD       float64   `json:"costUsd"`
	MRs           int       `json:"mrs"`
	ModelCalls    int64     `json:"modelCalls"`
	PeakCtx       *float64  `json:"peakCtx"`
	AvgCtx        *float64  `json:"avgCtx"`
	Cardio        []float64 `json:"cardio"`
	Over250kShare *float64  `json:"over250kShare"`
}

func (s *Service) mountOverview(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/efficiency/overview", s.OrgGate(s.overview))
	mux.Handle("GET /api/orgs/{org}/efficiency/me", s.OrgGate(s.me))
}

// period reads ?days=N (default 30) and answers [from, to) in the organization's time zone.
func (s *Service) period(r *http.Request, o store.Org) (from, to time.Time, loc *time.Location, err error) {
	loc = time.UTC
	if l, e := time.LoadLocation(o.Timezone); e == nil && o.Timezone != "" {
		loc = l
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	n := defaultDays
	if v := r.URL.Query().Get("days"); v != "" {
		x, e := parseDays(v)
		if e != nil {
			return from, to, loc, httpx.Invalid("days", "Число дней от 1 до 92")
		}
		n = x
	}
	// Midnight of the organization's day, not of UTC: Truncate would cut at UTC.
	y, m, d := now.In(loc).Date()
	to = time.Date(y, m, d, 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	from = to.AddDate(0, 0, -n)
	return from, to, loc, nil
}

func parseDays(v string) (int, error) {
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' || n > 1000 {
			return 0, httpx.Invalid("days", "")
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > int(maxSpan/(24*time.Hour)) {
		return 0, httpx.Invalid("days", "")
	}
	return n, nil
}

// SwitchOverview is the rollout switch (docs/hub/rollout-switches.md) that gives a person the overview and the personal tab
// from the hub. Without a row the endpoints answer 404 and the screen keeps asking the gateway.
const SwitchOverview = "hub-efficiency"

func (s *Service) gate(r *http.Request, t *tenant.Ctx) error {
	if s.Features == nil || !s.Features.On(r.Context(), t.Org.ID, features.Efficiency) {
		return httpx.ErrNotFound
	}
	if s.Switches == nil || !s.Switches.On(r.Context(), SwitchOverview, t.Person.User.Email, "") {
		return httpx.ErrNotFound
	}
	return nil
}

// scopeOf reads ?scope=me|team (team by default).
func scopeOf(r *http.Request) (string, error) {
	switch v := r.URL.Query().Get("scope"); v {
	case "", "team":
		return "team", nil
	case "me":
		return "me", nil
	default:
		return "", httpx.Invalid("scope", "me или team")
	}
}

func (s *Service) overview(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	if err := s.gate(r, t); err != nil {
		return err
	}
	scope, err := scopeOf(r)
	if err != nil {
		return err
	}
	from, to, loc, err := s.period(r, *t.Org)
	if err != nil {
		return err
	}
	out, err := ComputeOverview(r.Context(), s.DB.S(), t.Org.ID, personOf(scope, t), from, to, loc)
	if err != nil {
		return err
	}
	out.Scope = scope
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (s *Service) me(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	if err := s.gate(r, t); err != nil {
		return err
	}
	from, to, loc, err := s.period(r, *t.Org)
	if err != nil {
		return err
	}
	out, err := ComputeMe(r.Context(), s.DB.S(), t.Org.ID, t.Person.User.ID, t.Person.User.Name, from, to, loc)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// personOf is the user whose figures the scope shows: the caller for «me», nobody (the whole organization) for «team».
func personOf(scope string, t *tenant.Ctx) string {
	if scope == "me" {
		return t.Person.User.ID
	}
	return ""
}

func dayKey(ms int64, loc *time.Location) string {
	return time.UnixMilli(ms).In(loc).Format("2006-01-02")
}

type dayAcc struct {
	sessions            int
	mrs                 int
	calls               int64
	ctxTokens           int64
	costMicros          int64
	firstCtx            []float64
	carry, ctxIn        float64
	toolCalls, toolErrs float64
	waitP90W, waits     float64
}

// sessionsOf reads the top-level sessions of the scope that started in [from, to).
func sessionsOf(ctx context.Context, st store.Store, org, user string, from, to time.Time, liveOnly bool) ([]store.AgentSession, error) {
	if user != "" {
		return st.UserSessions(ctx, store.SessionFilter{UserID: user, OrgKey: org, From: from.UnixMilli(), To: to.UnixMilli(), LiveOnly: liveOnly})
	}
	return st.ScopeSessions(ctx, store.ScopeFilter{OrgKey: org, From: from.UnixMilli(), To: to.UnixMilli(), LiveOnly: liveOnly})
}

func metricsMap(raw string) map[string]float64 {
	m := map[string]float64{}
	var v map[string]any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return m
	}
	for k, x := range v {
		if f, ok := x.(float64); ok {
			m[k] = f
		}
	}
	return m
}

func ptr(v float64) *float64 { return &v }

func median(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return ptr(s[len(s)/2])
	}
	return ptr((s[len(s)/2-1] + s[len(s)/2]) / 2)
}

// nearest is the nearest-rank percentile (p in 0..1).
func nearest(xs []float64, p float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	return ptr(s[max(0, min(i, len(s)-1))])
}

func share(num, den float64) *float64 {
	if den <= 0 {
		return nil
	}
	return ptr(num / den)
}

// ComputeOverview is the overview of [from, to): per day the spend and calls of the rollup, the sessions and their metrics, and
// the requests merged (the organization's, or the person's sessions' own when user is set).
func ComputeOverview(ctx context.Context, st store.Store, org, user string, from, to time.Time, loc *time.Location) (Overview, error) {
	rows, err := st.RollupRows(ctx, org, store.HourMs(from.UnixMilli()), to.UnixMilli())
	if err != nil {
		return Overview{}, err
	}
	days := map[string]*dayAcc{}
	var order []string
	for d := from.In(loc); d.Before(to); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		days[k] = &dayAcc{}
		order = append(order, k)
	}
	for _, r := range rows {
		if r.Calls == 0 || (user != "" && r.UserID != user) {
			continue
		}
		if a := days[dayKey(r.Hour, loc)]; a != nil {
			a.calls += r.Calls
			a.ctxTokens += r.Input + r.CacheRead + r.CacheWrite
			a.costMicros += r.ListCostMicros
		}
	}
	ss, err := sessionsOf(ctx, st, org, user, from, to, false)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{From: from.In(loc).Format("2006-01-02"), To: to.In(loc).AddDate(0, 0, -1).Format("2006-01-02"),
		Days: make([]OverviewDay, 0, len(order)), Markers: []Marker{}, Channels: []OverviewChannel{}}
	if out.Markers, err = markersBetween(ctx, st, from, to); err != nil {
		return Overview{}, err
	}
	channels := map[string]*OverviewChannel{}
	refs := make([]store.SessionRef, 0, len(ss))
	chanOf := map[store.SessionRef]string{}
	var scanned int64
	var carryAll, ctxAll float64
	for _, x := range ss {
		met := metricsMap(x.Metrics)
		a := days[dayKey(x.StartedAt, loc)]
		if a == nil {
			continue
		}
		scanned = max(scanned, x.UpdatedAt)
		ch := channelOf(x.Origin)
		c := channels[ch]
		if c == nil {
			c = &OverviewChannel{Channel: ch}
			channels[ch] = c
		}
		c.Sessions++
		c.MRs += int(met["mrs"])
		ref := store.SessionRef{OrgKey: x.OrgKey, Provider: x.Provider, SessionKey: x.SessionKey}
		refs = append(refs, ref)
		chanOf[ref] = ch
		a.sessions++
		if user != "" {
			a.mrs += int(met["mrs"])
		}
		if v := met["firstCtx"]; v > 0 {
			a.firstCtx = append(a.firstCtx, v)
		}
		in := met["inputTokens"] + met["cacheReadTokens"] + met["cacheWriteTokens"]
		a.carry += met["carryTokens"]
		a.ctxIn += in
		carryAll += met["carryTokens"]
		ctxAll += in
		a.toolCalls += met["toolCalls"]
		a.toolErrs += met["toolErrors"]
		if met["waits"] > 0 && met["waitP90Ms"] > 0 {
			a.waitP90W += met["waitP90Ms"] * met["waits"]
			a.waits += met["waits"]
		}
	}
	if user == "" {
		// The requests the code host reports merged, whoever created them (the same source as the summary).
		sum, err := Compute(ctx, st, from, to, loc, nil)
		if err != nil {
			return Overview{}, err
		}
		for _, d := range sum.Days {
			if a := days[d.Day]; a != nil {
				a.mrs = d.MRs
			}
		}
	}
	for _, k := range order {
		a := days[k]
		d := OverviewDay{Day: k, CostUSD: float64(a.costMicros) / 1e6, Sessions: a.sessions, ModelCalls: a.calls, MRs: a.mrs}
		if a.calls > 0 {
			d.AvgCtx = ptr(float64(a.ctxTokens) / float64(a.calls))
		}
		d.FirstCtxP50 = median(a.firstCtx)
		d.CarryShare = share(a.carry, a.ctxIn)
		d.ToolErrorRate = share(a.toolErrs, a.toolCalls)
		if a.waits > 0 {
			d.HumanReplyP90Min = ptr(a.waitP90W / a.waits / 60000)
		}
		out.Totals.CostUSD += d.CostUSD
		out.Totals.MRs += d.MRs
		out.Days = append(out.Days, d)
	}
	out.Totals.Sessions = len(refs)
	out.Totals.CarryTokens = int64(carryAll)
	out.Totals.ReadingShare = share(carryAll, ctxAll)
	if len(refs) > 0 {
		sums, _, err := st.SessionSums(ctx, refs)
		if err != nil {
			return Overview{}, err
		}
		for _, x := range sums {
			if c := channels[chanOf[x.SessionRef]]; c != nil {
				c.CostUSD += float64(x.ListCostMicros) / 1e6
			}
		}
	}
	for _, name := range []string{"tab", "automation", "eval"} {
		if c := channels[name]; c != nil {
			out.Channels = append(out.Channels, *c)
		}
	}
	if scanned > 0 {
		v := time.UnixMilli(scanned).UTC().Format(time.RFC3339)
		out.ScannedAt = &v
	}
	return out, nil
}

// channelOf names the screen's channel of a session origin. A session that came from a machine's tab is «tab»; the hub does
// not tell remote-control and sdk sessions apart.
func channelOf(origin string) string {
	switch origin {
	case "eval":
		return "eval"
	case "automation":
		return "automation"
	default:
		return "tab"
	}
}

// ComputeMe is the personal tab: how the person's attention is spent and their newest sessions.
func ComputeMe(ctx context.Context, st store.Store, org, user, name string, from, to time.Time, loc *time.Location) (Me, error) {
	ss, err := sessionsOf(ctx, st, org, user, from, to, false)
	if err != nil {
		return Me{}, err
	}
	out := Me{Person: name, From: from.In(loc).Format("2006-01-02"), To: to.In(loc).AddDate(0, 0, -1).Format("2006-01-02"),
		Sessions: []MeSession{}}
	// Newest first, the newest meSessions of them.
	slices.Reverse(ss)
	var replies []float64
	for _, x := range ss {
		met := metricsMap(x.Metrics)
		if met["waits"] > 0 && met["waitMs"] > 0 {
			replies = append(replies, met["waitMs"]/met["waits"]/60000)
		}
	}
	out.Attention.ReplyP50Min = median(replies)
	out.Attention.ReplyP90Min = nearest(replies, 0.9)
	out.Limit = meLimit(ctx, st, org, user, time.Now())
	if len(ss) > meSessions {
		ss = ss[:meSessions]
	}
	if len(ss) == 0 {
		return out, nil
	}
	refs := make([]store.SessionRef, len(ss))
	for i, x := range ss {
		refs[i] = store.SessionRef{OrgKey: x.OrgKey, Provider: x.Provider, SessionKey: x.SessionKey}
	}
	all, _, err := st.SessionSums(ctx, refs)
	if err != nil {
		return Me{}, err
	}
	big, err := st.SessionSumsOver(ctx, refs, over250k)
	if err != nil {
		return Me{}, err
	}
	type tot struct {
		cost, calls, ctx, cacheRead int64
	}
	sum := map[store.SessionRef]*tot{}
	for _, x := range all {
		t := sum[x.SessionRef]
		if t == nil {
			t = &tot{}
			sum[x.SessionRef] = t
		}
		t.cost += x.ListCostMicros
		t.calls += x.Calls
		t.ctx += x.Input + x.CacheRead + x.CacheWrite
		t.cacheRead += x.CacheRead
	}
	bigRead := map[store.SessionRef]int64{}
	for _, x := range big {
		bigRead[x.SessionRef] += x.CacheRead
	}
	for i, x := range ss {
		met := metricsMap(x.Metrics)
		v := MeSession{FileID: x.SessionKey, Channel: channelOf(x.Origin), MRs: int(met["mrs"]), Cardio: []float64{}}
		st0 := time.UnixMilli(x.StartedAt).UTC().Format(time.RFC3339)
		v.StartedAt = &st0
		end := x.UpdatedAt
		if x.EndedAt != nil {
			end = *x.EndedAt
		}
		e := time.UnixMilli(end).UTC().Format(time.RFC3339)
		v.EndedAt = &e
		if t := sum[refs[i]]; t != nil {
			v.CostUSD = float64(t.cost) / 1e6
			v.ModelCalls = t.calls
			if t.calls > 0 {
				v.AvgCtx = ptr(float64(t.ctx) / float64(t.calls))
			}
			v.Over250kShare = share(float64(bigRead[refs[i]]), float64(t.cacheRead))
		}
		out.Sessions = append(out.Sessions, v)
	}
	return out, nil
}

// limitFresh is how long a machine's reading of a window counts as the person's limit; the agent sends it every minute.
const limitFresh = 2 * time.Hour

// meLimit is the person's subscription limit now; the hub knows only what machines told it, so no reading is no limit.
func meLimit(ctx context.Context, st store.Store, org, user string, now time.Time) MeLimit {
	w, err := st.FullestProviderWindow(ctx, org, user, "claude", "seven_day", now.Add(-limitFresh).UnixMilli())
	if err != nil {
		return MeLimit{}
	}
	out := MeLimit{WeekUsedPct: &w.UsedPct}
	if w.ResetsAt > 0 {
		at := time.Unix(w.ResetsAt, 0).UTC().Format("2006-01-02T15:04:05.000Z")
		out.ResetsAt = &at
	}
	return out
}
