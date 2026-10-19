package machines

import (
	"math"
	"sort"
)

// How much a person's agents usually spend at a given hour. The spend is the API-equivalent cost of every model call in
// the person's own transcripts; the usual rate at a future hour comes from analogs: the same hour one to six weeks back,
// and the same hour of the last days of the same kind (a workday for a workday, a weekend day for a weekend day).

const (
	activitySlotSec = 600
	activityDays    = 43
	day             = 86400
	hour            = 3600
)

// Activity is the spend history of one provider.
type Activity struct {
	// Start is the epoch second of the first slot.
	Start float64
	// Cum[i] is the cost of the slots before slot i, USD.
	Cum []float64
	// FirstAt is the first call with a cost: before it the history knows nothing.
	FirstAt float64
	// AsOf: transcripts were read up to this moment, after it the cost is unknown, not zero.
	AsOf float64
	// TZOffset (seconds east of UTC) is the zone whose Saturday and Sunday are the weekend.
	TZOffset float64
}

// usualRateOf is USD per hour: the weighted mean and quartiles of the analogs.
type usualRateOf struct {
	mean, median, low, high float64
}

func (u usualRateOf) pick(which string) float64 {
	switch which {
	case "mean":
		return u.mean
	case "median":
		return u.median
	case "low":
		return u.low
	}
	return u.high
}

type analog struct {
	lag          float64
	weight       float64
	sameKindOnly bool // a day lag only stands for a day of the same kind; a week lag always does
}

// analogs are written out as JavaScript computed their weights (0.8**i, 0.85**n), to the last bit.
var analogs = []analog{
	{7 * day, 2, false}, {14 * day, 1.6, false}, {21 * day, 1.2800000000000002, false},
	{28 * day, 1.0240000000000002, false}, {35 * day, 0.8192000000000002, false}, {42 * day, 0.6553600000000002, false},
	{1 * day, 1, true}, {2 * day, 0.85, true}, {3 * day, 0.7224999999999999, true}, {4 * day, 0.6141249999999999, true},
	{5 * day, 0.5220062499999999, true}, {6 * day, 0.44370531249999995, true}, {8 * day, 0.32057708828125, true},
	{9 * day, 0.27249052503906246, true}, {10 * day, 0.23161694628320306, true}, {11 * day, 0.19687440434072262, true},
	{12 * day, 0.16734324368961423, true}, {13 * day, 0.14224175713617207, true},
}

func isWeekend(epochSec, tzOffset float64) bool {
	ms := math.Trunc((epochSec + tzOffset) * 1000)
	d := math.Floor(ms / 86400000)
	wd := math.Mod(d+4, 7)
	if wd < 0 {
		wd += 7
	}
	return wd == 0 || wd == 6
}

// BuildActivity is the history of events ([epoch seconds, USD], any order); nil when nothing of the last weeks cost
// anything.
func BuildActivity(events [][2]float64, asOf, tzOffset float64) *Activity {
	start := math.Floor((asOf-activityDays*day)/activitySlotSec) * activitySlotSec
	slots := int(math.Ceil((asOf-start)/activitySlotSec)) + 1
	cost := make([]float64, slots)
	firstAt := math.Inf(1)
	for _, e := range events {
		t, usd := e[0], e[1]
		if !(usd > 0) || t < start || t > asOf {
			continue
		}
		cost[int(math.Floor((t-start)/activitySlotSec))] += usd
		if t < firstAt {
			firstAt = t
		}
	}
	if math.IsInf(firstAt, 1) {
		return nil
	}
	cum := make([]float64, slots+1)
	for i := 0; i < slots; i++ {
		cum[i+1] = cum[i] + cost[i]
	}
	return &Activity{Start: start, Cum: cum, FirstAt: firstAt, AsOf: asOf, TZOffset: tzOffset}
}

func (a *Activity) cumAt(t float64) float64 {
	position := (math.Min(math.Max(t, a.Start), a.AsOf) - a.Start) / activitySlotSec
	index := int(math.Min(math.Floor(position), float64(len(a.Cum)-2)))
	return a.Cum[index] + (a.Cum[index+1]-a.Cum[index])*(position-float64(index))
}

// costBetween is the cost between two moments, USD; a slot's cost is spread evenly over it.
func (a *Activity) costBetween(from, to float64) float64 {
	if to <= from {
		return 0
	}
	return a.cumAt(to) - a.cumAt(from)
}

func (a *Activity) historyDays() float64 { return math.Max(0, (a.AsOf-a.FirstAt)/day) }

type weighted struct{ value, weight float64 }

func weightedQuantile(values []weighted, total, q float64) float64 {
	target := total * q
	acc := 0.0
	for _, it := range values {
		acc += it.weight
		if acc >= target {
			return it.value
		}
	}
	return values[len(values)-1].value
}

// usualRate is the usual spend around moment t: every analog is a two-hour span centred on the same moment back then.
// False when no analog is inside the history.
func (a *Activity) usualRate(t float64) (usualRateOf, bool) {
	weekend := isWeekend(t, a.TZOffset)
	collect := func(anyKind bool) []weighted {
		var values []weighted
		for _, an := range analogs {
			at := t - an.lag
			if at-hour < a.FirstAt || at+hour > a.AsOf {
				continue
			}
			if an.sameKindOnly && !anyKind && isWeekend(at, a.TZOffset) != weekend {
				continue
			}
			values = append(values, weighted{a.costBetween(at-hour, at+hour) / 2, an.weight})
		}
		return values
	}
	// A young history may not have seen a day of this kind yet: the same hour of any day still knows day from night.
	values := collect(false)
	if len(values) == 0 {
		values = collect(true)
	}
	if len(values) == 0 {
		return usualRateOf{}, false
	}
	total := 0.0
	for _, v := range values {
		total += v.weight
	}
	sum := 0.0
	for _, v := range values {
		sum += v.value * v.weight
	}
	mean := sum / total
	sort.SliceStable(values, func(i, j int) bool { return values[i].value < values[j].value })
	return usualRateOf{
		mean:   mean,
		median: weightedQuantile(values, total, 0.5),
		low:    weightedQuantile(values, total, 0.25),
		high:   weightedQuantile(values, total, 0.75),
	}, true
}

// averageRate is the average spend over the whole history, USD per hour: the fallback for an hour no analog covers.
func (a *Activity) averageRate() float64 {
	span := a.AsOf - a.FirstAt
	if span > 0 {
		return (a.costBetween(a.FirstAt, a.AsOf) / span) * hour
	}
	return 0
}

// exp is fdlibm's exponential as V8 computes it, to the last bit: math.Exp differs from it (an assembly version on some
// machines, and a pure one whose P1 is not fdlibm's).
func exp(x float64) float64 {
	const (
		ln2Hi     = 6.93147180369123816490e-01
		ln2Lo     = 1.90821492927058770002e-10
		log2e     = 1.44269504088896338700e+00
		overflow  = 7.09782712893383973096e+02
		underflow = -7.45133219101941108420e+02
		nearZero  = 1.0 / (1 << 28)
	)
	switch {
	case math.IsNaN(x):
		return x
	case x > overflow:
		return math.Inf(1)
	case x < underflow:
		return 0
	case -nearZero < x && x < nearZero:
		return 1 + x
	}
	var k int
	switch {
	case x < 0:
		k = int(log2e*x - 0.5)
	case x > 0:
		k = int(log2e*x + 0.5)
	}
	hi := x - float64(k)*ln2Hi
	lo := float64(k) * ln2Lo
	const (
		p1 = 1.66666666666666019037e-01
		p2 = -2.77777777770155933842e-03
		p3 = 6.61375632143793436117e-05
		p4 = -1.65339022054652515390e-06
		p5 = 4.13813679705723846039e-08
	)
	r := hi - lo
	t := r * r
	c := r - t*(p1+t*(p2+t*(p3+t*(p4+t*p5))))
	y := 1 - ((lo - (r*c)/(2-c)) - hi)
	return math.Ldexp(y, k)
}
