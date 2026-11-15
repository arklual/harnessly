package agenteval

import "math"

// Wilson is the 95% Wilson score interval of k passes in n runs.
func Wilson(k, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 1
	}
	const z = 1.959963984540054
	p := float64(k) / float64(n)
	nn := float64(n)
	den := 1 + z*z/nn
	mid := (p + z*z/(2*nn)) / den
	half := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / den
	return math.Max(0, mid-half), math.Min(1, mid+half)
}

// MinEffect is the smallest change of a pass rate that is reported as a change, whatever the intervals say.
const MinEffect = 0.10

// Change labels a cell against the same cell of an earlier run: "better", "worse", "same" (within noise) or "new".
// A change counts when the two Wilson intervals do not overlap and the rates differ by at least MinEffect and by the
// noise threshold measured for the setup (0 when none was measured).
func Change(k0, n0, k1, n1 int, noise float64) string {
	if n0 == 0 {
		return "new"
	}
	if n1 == 0 {
		return "same"
	}
	p0, p1 := float64(k0)/float64(n0), float64(k1)/float64(n1)
	lo0, hi0 := Wilson(k0, n0)
	lo1, hi1 := Wilson(k1, n1)
	d := p1 - p0
	if math.Abs(d) < math.Max(MinEffect, noise) {
		return "same"
	}
	switch {
	case d > 0 && lo1 > hi0:
		return "better"
	case d < 0 && hi1 < lo0:
		return "worse"
	}
	return "same"
}

// PassHatK is the unbiased estimate of pass^k (all of k fresh runs pass) from c passes in n runs: C(c,k)/C(n,k).
func PassHatK(c, n, k int) float64 {
	if k <= 0 || n < k {
		return math.NaN()
	}
	r := 1.0
	for i := 0; i < k; i++ {
		r *= float64(c-i) / float64(n-i)
	}
	if r < 0 {
		return 0
	}
	return r
}
