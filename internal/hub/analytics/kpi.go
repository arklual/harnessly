package analytics

import (
	"math"
	"slices"
	"sort"
)

// The twelve KPI of docs/hub/analytics.md, «KPI»: each a function of the sessions' metrics.

// minSessions is the fewest sessions a value is shown for.
const minSessions = 5

// kSession is a live top-level session as the KPI see it.
type kSession struct {
	UserID     string
	Start, End int64 // epoch ms; End is the end, or the last update of a running session
	M          map[string]float64
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

func sum(ss []kSession, keys ...string) float64 {
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

var input = []string{"inputTokens", "cacheReadTokens", "cacheWriteTokens"}

var kpis = []kpiDef{
	{"money_per_mr", "usd", "down", func(ss []kSession, _ float64) *float64 {
		var list int64
		var mrs float64
		for _, s := range ss {
			if !s.NoMoney {
				list += s.ListMicros
				mrs += s.M["mrsMerged"]
			}
		}
		return ratio(float64(list)/1e6, mrs)
	}},
	{"completion", "share", "up", func(ss []kSession, _ float64) *float64 {
		var done, all float64
		for _, s := range ss {
			if s.M["turns"] >= 3 {
				all++
				if s.M["mrs"] >= 1 {
					done++
				}
			}
		}
		return ratio(done, all)
	}},
	{"time_to_mr", "ms", "down", func(ss []kSession, _ float64) *float64 {
		var vs []float64
		for _, s := range ss {
			if s.M["mrs"] >= 1 && s.M["firstMrMs"] > 0 {
				vs = append(vs, s.M["firstMrMs"])
			}
		}
		return median(vs)
	}},
	{"mrs_per_person", "count", "up", func(ss []kSession, days float64) *float64 {
		people := map[string]bool{}
		for _, s := range ss {
			people[s.UserID] = true
		}
		return ratio(sum(ss, "mrsMerged"), float64(len(people))*days/7)
	}},
	{"autonomy", "share", "up", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, "agentMs"), sum(ss, "agentMs", "waitMs"))
	}},
	{"waiting_human", "ms", "down", func(ss []kSession, _ float64) *float64 {
		var vs []float64
		for _, s := range ss {
			if s.M["waits"] >= 1 {
				vs = append(vs, s.M["waitMs"]/s.M["waits"])
			}
		}
		return median(vs)
	}},
	// The working intervals stay on the machine; the hub takes a session's span from its start to its end.
	{"parallelism", "count", "up", func(ss []kSession, _ float64) *float64 {
		agent := map[string]float64{}
		spans := map[string][][2]int64{}
		for _, s := range ss {
			agent[s.UserID] += s.M["agentMs"]
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
		return ratio(sum(ss, "toolErrors"), sum(ss, "toolCalls"))
	}},
	{"reread_context", "share", "down", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, "carryTokens"), sum(ss, input...))
	}},
	{"session_start_ctx", "tokens", "down", func(ss []kSession, _ float64) *float64 {
		var vs []float64
		for _, s := range ss {
			if s.M["firstCtx"] > 0 {
				vs = append(vs, s.M["firstCtx"])
			}
		}
		return median(vs)
	}},
	{"cache_hit", "share", "up", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, "cacheReadTokens"), sum(ss, input...))
	}},
	{"skills_in_use", "share", "up", func(ss []kSession, _ float64) *float64 {
		return ratio(sum(ss, "skillsLoaded"), sum(ss, "skillsListed"))
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
		out = append(out, v)
	}
	return out
}
