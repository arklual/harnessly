package analytics

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sort"
)

// The twelve KPI of docs/hub/analytics.md, «KPI»: each a function of the sessions' metrics.

// minSessions is the fewest sessions a value is shown for.
const minSessions = 5

// kMetric is a metric of a session the KPI read: an index of kMetrics. The KPI read them a few million times an answer,
// a map by name made that most of the screens' time.
type kMetric int

const (
	mMrsMerged kMetric = iota
	mTurns
	mMrs
	mFirstMrMs
	mAgentMs
	mWaitMs
	mWaits
	mToolErrors
	mToolCalls
	mCarryTokens
	mInputTokens
	mCacheReadTokens
	mCacheWriteTokens
	mFirstCtx
	mCacheWrite1hTokens
	mCacheWrite5mTokens
	mMrsSurvived
	mMrsMature
	mWaitP90Ms
	mSkillsLoaded
	mSkillsListed
	nKMetrics
)

// kMetrics is a session's metrics the KPI read, by kMetric; one it lacks is 0.
type kMetrics [nKMetrics]float64

// kMetricsJSON reads the metrics of kMetrics out of a session's metrics.
type kMetricsJSON struct {
	MrsMerged          float64 `json:"mrsMerged"`
	Turns              float64 `json:"turns"`
	Mrs                float64 `json:"mrs"`
	FirstMrMs          float64 `json:"firstMrMs"`
	AgentMs            float64 `json:"agentMs"`
	WaitMs             float64 `json:"waitMs"`
	Waits              float64 `json:"waits"`
	ToolErrors         float64 `json:"toolErrors"`
	ToolCalls          float64 `json:"toolCalls"`
	CarryTokens        float64 `json:"carryTokens"`
	InputTokens        float64 `json:"inputTokens"`
	CacheReadTokens    float64 `json:"cacheReadTokens"`
	CacheWriteTokens   float64 `json:"cacheWriteTokens"`
	FirstCtx           float64 `json:"firstCtx"`
	CacheWrite1hTokens float64 `json:"cacheWrite1hTokens"`
	CacheWrite5mTokens float64 `json:"cacheWrite5mTokens"`
	MrsSurvived        float64 `json:"mrsSurvived"`
	MrsMature          float64 `json:"mrsMature"`
	WaitP90Ms          float64 `json:"waitP90Ms"`
	SkillsLoaded       float64 `json:"skillsLoaded"`
	SkillsListed       float64 `json:"skillsListed"`
}

// kMetricsOf is the metrics of kMetrics in raw, as metricsOf reads them: one that is not a number is 0, and so are all of
// a raw that is not JSON.
func kMetricsOf(raw string) kMetrics {
	var j kMetricsJSON
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return kMetrics{}
		}
		// A field of another type stays 0, the rest are read.
	}
	return kMetrics{j.MrsMerged, j.Turns, j.Mrs, j.FirstMrMs, j.AgentMs, j.WaitMs, j.Waits, j.ToolErrors, j.ToolCalls, j.CarryTokens, j.InputTokens, j.CacheReadTokens, j.CacheWriteTokens, j.FirstCtx, j.CacheWrite1hTokens, j.CacheWrite5mTokens, j.MrsSurvived, j.MrsMature, j.WaitP90Ms, j.SkillsLoaded, j.SkillsListed}
}

// kMetricsFrom is the metrics of kMetrics in m.
func kMetricsFrom(m map[string]float64) kMetrics {
	var out kMetrics
	for i, k := range kMetricNames {
		out[i] = m[k]
	}
	return out
}

var kMetricNames = [nKMetrics]string{"mrsMerged", "turns", "mrs", "firstMrMs", "agentMs", "waitMs", "waits", "toolErrors", "toolCalls", "carryTokens", "inputTokens", "cacheReadTokens", "cacheWriteTokens", "firstCtx", "cacheWrite1hTokens", "cacheWrite5mTokens", "mrsSurvived", "mrsMature", "waitP90Ms", "skillsLoaded", "skillsListed"}

// kSession is a live top-level session as the KPI see it.
type kSession struct {
	UserID     string
	TeamID     string
	Start, End int64 // epoch ms; End is the end, or the last update of a running session
	M          kMetrics
	PaidMicros int64 // its money and its subagents'
	ListMicros int64 // the same work at API prices: what «Деньги на MR» counts
	NoMoney    bool  // its money is not the scope's (a personal account in an organization's KPI): not in money_per_mr
}

// KpiValue is one KPI with its comparison.
type KpiValue struct {
	Kpi        string   `json:"kpi"`
	Unit       string   `json:"unit"`
	BetterWhen string   `json:"betterWhen"`
	Value      *float64 `json:"value"`
	N          int      `json:"n"`
	CmpValue   *float64 `json:"cmpValue"`
	CmpN       int      `json:"cmpN"`
	Delta      *float64 `json:"delta"`
	Noise      *float64 `json:"noise"`
	Verdict    string   `json:"verdict"`
	// Spark is the value by day of the period (by week past 45 days), CmpSpark the same of the comparison; a bucket with
	// too few sessions is null. They draw the small charts of the KPI cards.
	Spark    []*float64 `json:"spark,omitempty"`
	CmpSpark []*float64 `json:"cmpSpark,omitempty"`
}

type kpiDef struct {
	id, unit, better string
	f                func(ss []kSession, days float64) *float64
}

func ratio(num, den float64) *float64 {
	if den <= 0 {
		return nil
	}
	v := num / den
	return &v
}

func sum(ss []kSession, keys ...kMetric) float64 {
	var t float64
	for _, s := range ss {
		for _, k := range keys {
			t += s.M[k]
		}
	}
	return t
}

func median(vs []float64) *float64 {
	if len(vs) == 0 {
		return nil
	}
	slices.Sort(vs)
	n := len(vs)
	v := vs[n/2]
	if n%2 == 0 {
		v = (vs[n/2-1] + vs[n/2]) / 2
	}
	return &v
}

// union is the length of the union of intervals.
func union(iv [][2]int64) int64 {
	sort.Slice(iv, func(i, j int) bool { return iv[i][0] < iv[j][0] })
	var total, curS, curE int64
	open := false
	for _, x := range iv {
		if x[1] <= x[0] {
			continue
		}
		if !open || x[0] > curE {
			if open {
				total += curE - curS
			}
			curS, curE, open = x[0], x[1], true
			continue
		}
		curE = max(curE, x[1])
	}
	if open {
		total += curE - curS
	}
	return total
}

var input = []kMetric{mInputTokens, mCacheReadTokens, mCacheWriteTokens}

var kpis = []kpiDef{
	{"money_per_mr", "usd", "down", func(ss []kSession, _ float64) *float64 {
		var list int64
		var mrs float64
		for _, s := range ss {
			if !s.NoMoney {
				list += s.ListMicros
				mrs += s.M[mMrsMerged]
			}
		}
		return ratio(float64(list)/1e6, mrs)
	}},
	{"completion", "share", "up", func(ss []kSession, _ float64) *float64 {
		var done, all float64
		for _, s := range ss {
			if s.M[mTurns] >= 3 {
				all++
				if s.M[mMrs] >= 1 {
					done++
				}
			}
		}
		return ratio(done, all)
	}},
	{"time_to_mr", "ms", "down", func(ss []kSession, _ float64) *float64 {
		var vs []float64
		for _, s := range ss {
			if s.M[mMrs] >= 1 && s.M[mFirstMrMs] > 0 {
				vs = append(vs, s.M[mFirstMrMs])
			}
		}
		return median(vs)
	}},
	{"mrs_per_person", "count", "up", func(ss []kSession, days float64) *float64 {
		people := map[string]bool{}
		for _, s := range ss {
			people[s.UserID] = true
		}
		return ratio(sum(ss, mMrsMerged), float64(len(people))*days/7)
	}},
	{"autonomy", "share", "up", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, mAgentMs), sum(ss, mAgentMs, mWaitMs))
	}},
	{"waiting_human", "ms", "down", func(ss []kSession, _ float64) *float64 {
		var vs []float64
		for _, s := range ss {
			if s.M[mWaits] >= 1 {
				vs = append(vs, s.M[mWaitMs]/s.M[mWaits])
			}
		}
		return median(vs)
	}},
	// The working intervals stay on the machine; the hub takes a session's span from its start to its end.
	{"parallelism", "count", "up", func(ss []kSession, _ float64) *float64 {
		agent := map[string]float64{}
		spans := map[string][][2]int64{}
		for _, s := range ss {
			agent[s.UserID] += s.M[mAgentMs]
			spans[s.UserID] = append(spans[s.UserID], [2]int64{s.Start, s.End})
		}
		var t float64
		var n int
		for u, iv := range spans {
			if r := ratio(agent[u], float64(union(iv))); r != nil {
				t += *r
				n++
			}
		}
		return ratio(t, float64(n))
	}},
	{"tool_errors", "share", "down", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, mToolErrors), sum(ss, mToolCalls))
	}},
	{"reread_context", "share", "down", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, mCarryTokens), sum(ss, input...))
	}},
	{"session_start_ctx", "tokens", "down", func(ss []kSession, _ float64) *float64 {
		var vs []float64
		for _, s := range ss {
			if s.M[mFirstCtx] > 0 {
				vs = append(vs, s.M[mFirstCtx])
			}
		}
		return median(vs)
	}},
	{"cache_hit", "share", "up", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, mCacheReadTokens), sum(ss, input...))
	}},
	// The share of cache writes made for an hour (twice the price of a five-minute one); sessions of machines that do not
	// split the writes add nothing to either side.
	{"cache_write_1h", "share", "down", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, mCacheWrite1hTokens), sum(ss, mCacheWrite5mTokens, mCacheWrite1hTokens))
	}},
	// Merged requests that stayed 14 days without a revert, of those old enough to tell (mrsMature is set by the hub).
	{"mr_survival", "share", "up", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, mMrsSurvived), sum(ss, mMrsMature))
	}},
	// How long the person makes the agent wait: the 90th percentile of the waits of a session, averaged over sessions by
	// their number of waits (the machine keeps the waits, the hub only gets each session's p90).
	{"waiting_p90", "ms", "down", func(ss []kSession, _ float64) *float64 {
		var t, n float64
		for _, s := range ss {
			if p, w := s.M[mWaitP90Ms], s.M[mWaits]; p > 0 && w >= 1 {
				t += p * w
				n += w
			}
		}
		return ratio(t, n)
	}},
	{"skills_in_use", "share", "up", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, mSkillsLoaded), sum(ss, mSkillsListed))
	}},
}

func value(d kpiDef, ss []kSession, days float64) *float64 {
	if len(ss) < minSessions {
		return nil
	}
	return d.f(ss, days)
}

// noise is ±2σ of the weekly values; nil below 4 weeks with a value.
func noise(vs []float64) *float64 {
	if len(vs) < 4 {
		return nil
	}
	var mean float64
	for _, v := range vs {
		mean += v
	}
	mean /= float64(len(vs))
	var sq float64
	for _, v := range vs {
		sq += (v - mean) * (v - mean)
	}
	n := 2 * math.Sqrt(sq/float64(len(vs)-1))
	return &n
}

// computeKPIs gives the twelve for the period against the comparison; weeks are the 8 weeks before the period, each a
// week's sessions, for the noise.
func computeKPIs(cur []kSession, curDays float64, cmp []kSession, cmpDays float64, weeks [][]kSession) []KpiValue {
	out := make([]KpiValue, 0, len(kpis))
	for _, d := range kpis {
		out = append(out, computeKPI(d, cur, curDays, cmp, cmpDays, weeks))
	}
	return out
}

// computeKPI is the KPI d of computeKPIs.
func computeKPI(d kpiDef, cur []kSession, curDays float64, cmp []kSession, cmpDays float64, weeks [][]kSession) KpiValue {
	v := KpiValue{Kpi: d.id, Unit: d.unit, BetterWhen: d.better, N: len(cur), CmpN: len(cmp)}
	v.Value = value(d, cur, curDays)
	v.CmpValue = value(d, cmp, cmpDays)
	var weekly []float64
	for _, w := range weeks {
		if x := value(d, w, 7); x != nil {
			weekly = append(weekly, *x)
		}
	}
	v.Noise = noise(weekly)
	v.Verdict = "too_few"
	if v.Value != nil && v.CmpValue != nil {
		delta := *v.Value - *v.CmpValue
		v.Delta = &delta
		switch {
		case delta == 0 || (v.Noise != nil && math.Abs(delta) <= *v.Noise):
			v.Verdict = "noise"
		case (delta < 0) == (d.better == "down"):
			v.Verdict = "better"
		default:
			v.Verdict = "worse"
		}
	}
	return v
}
