package analytics

import (
	"math"
	"testing"
)

func TestUnion(t *testing.T) {
	for _, c := range []struct {
		iv   [][2]int64
		want int64
	}{
		{nil, 0},
		{[][2]int64{{0, 10}}, 10},
		{[][2]int64{{0, 10}, {5, 15}}, 15},
		{[][2]int64{{20, 30}, {0, 10}}, 20},
		{[][2]int64{{0, 10}, {2, 3}, {10, 12}}, 12},
		{[][2]int64{{5, 5}, {7, 3}}, 0},
	} {
		if got := union(c.iv); got != c.want {
			t.Errorf("union(%v) = %d, want %d", c.iv, got, c.want)
		}
	}
}

func TestMedian(t *testing.T) {
	if median(nil) != nil {
		t.Error("median of nothing")
	}
	if v := median([]float64{3, 1, 2}); *v != 2 {
		t.Errorf("odd = %v", *v)
	}
	if v := median([]float64{4, 1, 3, 2}); *v != 2.5 {
		t.Errorf("even = %v", *v)
	}
}

func TestNoise(t *testing.T) {
	if noise([]float64{1, 2, 3}) != nil {
		t.Error("noise of three weeks")
	}
	// Sample σ of 1,2,3,4 is √(5/3).
	if v := noise([]float64{1, 2, 3, 4}); math.Abs(*v-2*math.Sqrt(5.0/3)) > 1e-12 {
		t.Errorf("noise = %v", *v)
	}
}

func many(n int, user string, m map[string]float64) []kSession {
	out := make([]kSession, n)
	for i := range out {
		out[i] = kSession{UserID: user, Start: int64(i) * 100, End: int64(i)*100 + 50, M: m, PaidMicros: 1_000_000, ListMicros: 1_000_000}
	}
	return out
}

func find(vs []KpiValue, id string) KpiValue {
	for _, v := range vs {
		if v.Kpi == id {
			return v
		}
	}
	return KpiValue{}
}

func TestComputeKPIs(t *testing.T) {
	good := many(6, "a", map[string]float64{"turns": 5, "mrs": 1, "mrsMerged": 1, "toolCalls": 10, "toolErrors": 1})
	bad := many(6, "a", map[string]float64{"turns": 5, "mrs": 0, "toolCalls": 10, "toolErrors": 5})
	out := computeKPIs(good, 7, bad, 7, nil)
	if len(out) != 15 {
		t.Fatalf("%d KPI", len(out))
	}
	c := find(out, "completion")
	if *c.Value != 1 || *c.CmpValue != 0 || *c.Delta != 1 || c.Noise != nil || c.Verdict != "better" {
		t.Errorf("completion = %+v", c)
	}
	if e := find(out, "tool_errors"); e.Verdict != "better" || *e.Value != 0.1 || *e.CmpValue != 0.5 {
		t.Errorf("tool_errors = %+v", e)
	}
	// Six people-weeks of one MR each: six a week.
	if m := find(out, "mrs_per_person"); *m.Value != 6 || *m.CmpValue != 0 || m.Verdict != "better" {
		t.Errorf("mrs_per_person = %+v", m)
	}
	if m := find(out, "money_per_mr"); *m.Value != 1 {
		t.Errorf("money_per_mr = %+v", m)
	}

	// Within the weekly spread it is noise.
	weeks := [][]kSession{}
	for _, share := range []float64{0, 1, 0, 1} {
		weeks = append(weeks, many(5, "a", map[string]float64{"turns": 5, "mrs": share}))
	}
	c = find(computeKPIs(good, 7, bad, 7, weeks), "completion")
	if c.Noise == nil || c.Verdict != "noise" {
		t.Errorf("completion with noisy weeks = %+v", c)
	}

	// Under five sessions nothing is shown.
	c = find(computeKPIs(good[:4], 7, good, 7, nil), "completion")
	if c.Value != nil || c.N != 4 || c.Verdict != "too_few" {
		t.Errorf("four sessions = %+v", c)
	}
	// The same value is noise without a measured spread.
	if c := find(computeKPIs(good, 7, good, 7, nil), "completion"); c.Verdict != "noise" {
		t.Errorf("equal = %+v", c)
	}
}

func TestParallelism(t *testing.T) {
	// Two people; a runs two agents side by side over 100 ms, b one agent half of its 100 ms.
	ss := []kSession{
		{UserID: "a", Start: 0, End: 100, M: map[string]float64{"agentMs": 100}},
		{UserID: "a", Start: 0, End: 100, M: map[string]float64{"agentMs": 100}},
		{UserID: "b", Start: 0, End: 60, M: map[string]float64{"agentMs": 30}},
		{UserID: "b", Start: 40, End: 100, M: map[string]float64{"agentMs": 20}},
		{UserID: "b", Start: 200, End: 200, M: map[string]float64{}},
	}
	v := find(computeKPIs(ss, 7, nil, 7, nil), "parallelism")
	if v.Value == nil || math.Abs(*v.Value-1.25) > 1e-12 {
		t.Errorf("parallelism = %+v", v)
	}
}

// The KPI that older sessions cannot answer (the cache write split, the p90 of the waits, the survival of merged
// requests) count only the sessions that carry their numbers.
func TestComputeKPIsGapMetrics(t *testing.T) {
	old := many(3, "a", map[string]float64{"turns": 5, "waits": 4})
	fresh := many(3, "a", map[string]float64{
		"turns": 5, "waits": 2, "waitP90Ms": 600_000, "cacheWrite5mTokens": 300, "cacheWrite1hTokens": 100,
		"mrsMature": 4, "mrsSurvived": 3,
	})
	out := computeKPIs(append(old, fresh...), 7, nil, 7, nil)
	want := map[string]float64{"cache_write_1h": 0.25, "mr_survival": 0.75, "waiting_p90": 600_000}
	for k, w := range want {
		v := find(out, k)
		if v.Value == nil || *v.Value != w {
			t.Errorf("%s = %v, want %v", k, v.Value, w)
		}
	}
	// Sessions without the numbers give no value, not zero.
	none := computeKPIs(old, 7, nil, 7, nil)
	for k := range want {
		if v := find(none, k); v.Value != nil {
			t.Errorf("%s of old sessions = %v", k, *v.Value)
		}
	}
}
