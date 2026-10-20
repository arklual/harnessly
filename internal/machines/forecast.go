package machines

import (
	"math"
	"slices"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// When a subscription window runs out. With the person's spend history the forecast follows their usual hours: the
// current pace fades within about an hour into what they usually spend at those hours; percent per dollar is calibrated
// on the window itself and the range is the quieter and the busier quartile of the same hours. Without that history it
// extends the pace of the last samples: the median of pairwise slopes (Theil–Sen), so one-point steps of whole percents
// do not throw it off. What the samples cannot support is "insufficient_data", never a guess.

// Num is a number written as JSON.stringify writes it: NaN and the infinities as null, -0 as 0.
type Num float64

func (n Num) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return []byte("null"), nil
	}
	return []byte(jscompat.Number(f)), nil
}

func num(f float64) *Num { n := Num(f); return &n }

// Forecast is one window's verdict; the keys are in the order the previous runtime built the object.
type Forecast struct {
	RatePctPerHour *Num     `json:"ratePctPerHour"`
	ExhaustAt      *Num     `json:"exhaustAt"`
	ExhaustRange   []*Num   `json:"exhaustRange"`
	UsedAtReset    *Num     `json:"usedAtReset"`
	Confidence     *string  `json:"confidence"`
	Samples        int      `json:"samples"`
	SpanSec        Num      `json:"spanSec"`
	Reason         *string  `json:"reason"`
	LastAt         *Num     `json:"lastAt"`
	ResetsAt       *Num     `json:"resetsAt"`
	Method         string   `json:"method,omitempty"`
	HistoryDays    *Num     `json:"historyDays,omitempty"`
	Path           [][2]Num `json:"path,omitempty"`
	Status         string   `json:"status"`
}

const (
	statusExhausts     = "exhausts_before_reset"
	statusLasts        = "lasts_until_reset"
	statusInsufficient = "insufficient_data"
	statusStale        = "stale"
	statusExhausted    = "exhausted"

	confidenceHigh   = "high"
	confidenceMedium = "medium"
	confidenceLow    = "low"
)

// WindowSpec tunes the forecast of one window length.
type WindowSpec struct {
	Period   float64 // window length, seconds
	Lookback float64 // how far back the pace is measured
	MinGap   float64 // shortest time between two samples of one slope
	MinSpan  float64 // shortest span of samples to forecast from
	Fresh    float64 // a last sample older than this makes the forecast stale
	Bucket   float64 // samples are thinned to the last one per bucket before pairing
}

var (
	FiveHour = WindowSpec{Period: 5 * 3600, Lookback: 90 * 60, MinGap: 5 * 60, MinSpan: 20 * 60, Fresh: 10 * 60, Bucket: 60}
	SevenDay = WindowSpec{Period: 7 * 86400, Lookback: 24 * 3600, MinGap: 60 * 60, MinSpan: 6 * 3600, Fresh: 2 * 3600, Bucket: 10 * 60}
)

const (
	sameResetSec       = 120 // samples whose reset moved by at most this much belong to the same window
	dropPct            = 1   // a larger drop is a reset or a glitch: the pace is measured after it
	idleRatePctPerHour = 0.1
	momentumTauSec     = hour
	momentumFreshSec   = 15 * 60
	priorPct           = 10
	spreadFloorPct     = 5
	minCalibrationPct  = 3
)

func str(s string) *string { return &s }

// The forecast's reasons, picked by RUNTIME_LOCALE when the forecast is made; the Russian ones are the previous
// runtime's texts, byte for byte.
var (
	reasonNoSamples   = locale.T("no samples", "нет замеров")
	reasonWindowReset = locale.T("the window reset, waiting for new samples", "окно сбросилось, ждём новых замеров")
	reasonNoFresh     = locale.T("no fresh samples", "нет свежих замеров")
	reasonFlat        = locale.T("usage is below the measurement step (1%)", "расход меньше шага измерения (1%)")
)

// formatSpan: "20 min", "6 h", "1 h 30 min" (Russian uses its own abbreviations for minutes and hours).
func formatSpan(seconds float64) string {
	mins, hrs := locale.Pick(" min", " мин"), locale.Pick(" h", " ч")
	minutes := jsRound(seconds / 60)
	hours := math.Floor(minutes / 60)
	rest := math.Mod(minutes, 60)
	m, h, r := jscompat.Number(minutes), jscompat.Number(hours), jscompat.Number(rest)
	if hours == 0 {
		return m + mins
	}
	if rest == 0 {
		return h + hrs
	}
	return h + hrs + " " + r + mins
}

// jsRound is Math.round: halves go up.
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	r := math.Floor(x)
	if x-r >= 0.5 {
		r++
	}
	return r
}

func quantile(sorted []float64, q float64) float64 {
	position := float64(len(sorted)-1) * q
	low := math.Floor(position)
	high := math.Ceil(position)
	return sorted[int(low)] + (sorted[int(high)]-sorted[int(low)])*(position-low)
}

func lastPerBucket(samples []limits.Sample, bucket float64) []limits.Sample {
	var out []limits.Sample
	for _, s := range samples {
		if n := len(out); n > 0 && math.Floor(out[n-1].T/bucket) == math.Floor(s.T/bucket) {
			out[n-1] = s
		} else {
			out = append(out, s)
		}
	}
	return out
}

func slopesPerHour(samples []limits.Sample, minGap float64) []float64 {
	var slopes []float64
	for i := range samples {
		for j := i + 1; j < len(samples); j++ {
			dt := samples[j].T - samples[i].T
			if dt >= minGap {
				slopes = append(slopes, ((samples[j].U-samples[i].U)/dt)*3600)
			}
		}
	}
	slices.Sort(slopes)
	return slopes
}

func confidenceOf(count int, span, spread, age float64, spec WindowSpec) string {
	if count >= 12 && span >= 2*spec.MinSpan && spread <= 0.35 && age <= spec.Fresh/3 {
		return confidenceHigh
	}
	if spread <= 0.8 {
		return confidenceMedium
	}
	return confidenceLow
}

type paceRates struct{ r25, r50, r75 float64 }

type pace struct {
	series []limits.Sample
	span   float64
	rates  *paceRates
	flat   bool
}

func measurePace(all []limits.Sample, last limits.Sample, now float64, spec WindowSpec) pace {
	reset := last.R
	windowStart := reset - spec.Period
	from := math.Max(windowStart, now-spec.Lookback)
	var series []limits.Sample
	for _, x := range all {
		if math.Abs(x.R-reset) <= sameResetSec && x.T >= from {
			series = append(series, x)
		}
	}
	dropAt := -1
	for i := 1; i < len(series); i++ {
		if series[i].U < series[i-1].U-dropPct {
			dropAt = i
		}
	}
	if dropAt > 0 {
		series = series[dropAt:]
	}
	// The window really starts at zero: a window opened within the lookback gets that point for free.
	if windowStart >= now-spec.Lookback && len(series) > 0 && series[0].T > windowStart {
		series = append([]limits.Sample{{T: windowStart, U: 0, R: reset}}, series...)
	}
	series = lastPerBucket(series, spec.Bucket)
	span := 0.0
	if len(series) > 0 {
		span = series[len(series)-1].T - series[0].T
	}
	flat := len(series) == 0 || last.U-series[0].U <= dropPct
	p := pace{series: series, span: span, flat: flat}
	if len(series) < 4 || span < spec.MinSpan || flat {
		return p
	}
	slopes := slopesPerHour(series, spec.MinGap)
	if len(slopes) == 0 {
		return p
	}
	p.rates = &paceRates{quantile(slopes, 0.25), quantile(slopes, 0.5), quantile(slopes, 0.75)}
	return p
}

type calibration struct {
	pctPerUsd float64
	strong    bool // backed by past windows or by a good part of the current one
}

type measuredSpend struct{ used, cost float64 }

// calibrate is percent of the window per dollar of spend. A window starts at zero, so its use at any moment is what was
// spent since its start; past windows steady the ratio while the current one is still young. A window that began
// before the history is measured by its rise since the history began.
func calibrate(all []limits.Sample, last limits.Sample, spec WindowSpec, a *Activity) *calibration {
	// all is sorted by time: a window's samples are a run of it, and those measured by asOf are a prefix of the run.
	measuredOf := func(group []limits.Sample) *measuredSpend {
		known := 0
		for known < len(group) && group[known].T <= a.AsOf {
			known++
		}
		if known == 0 {
			return nil
		}
		at := group[known-1]
		start := group[0].R - spec.Period
		if start >= a.FirstAt {
			return &measuredSpend{at.U, a.costBetween(start, at.T)}
		}
		fromIdx := -1
		for i := 0; i < known; i++ {
			if group[i].T >= a.FirstAt {
				fromIdx = i
				break
			}
		}
		if fromIdx < 0 || fromIdx == known-1 || at.U < group[fromIdx].U {
			return nil
		}
		from := group[fromIdx]
		return &measuredSpend{at.U - from.U, a.costBetween(from.T, at.T)}
	}
	pastUsed, pastCost := 0.0, 0.0
	var current *measuredSpend
	for begin := 0; begin < len(all); {
		end := begin + 1
		for end < len(all) && math.Abs(all[begin].R-all[end].R) <= sameResetSec {
			end++
		}
		g := all[begin:end]
		begin = end
		m := measuredOf(g)
		if m == nil {
			continue
		}
		if math.Abs(g[0].R-last.R) <= sameResetSec {
			current = m
		} else if m.used >= minCalibrationPct && m.cost > 0 {
			pastUsed += m.used
			pastCost += m.cost
		}
	}
	if pastCost > 0 && pastUsed >= 2*minCalibrationPct {
		past := pastUsed / pastCost
		pctPerUsd := past
		if current != nil {
			pctPerUsd = (current.used + priorPct) / (current.cost + priorPct/past)
		}
		if !math.IsNaN(pctPerUsd) && !math.IsInf(pctPerUsd, 0) && pctPerUsd > 0 {
			return &calibration{pctPerUsd, true}
		}
		return nil
	}
	if current == nil || current.used < minCalibrationPct || current.cost <= 0 {
		return nil
	}
	return &calibration{current.used / current.cost, current.used >= priorPct}
}

type run struct {
	exhaustAt   *float64
	usedAtReset float64 // without the 100% cap, so the spread of the runs stays visible
	firstRate   float64
	path        [][2]Num
}

func forecastByProfile(all []limits.Sample, last limits.Sample, now float64, spec WindowSpec, a *Activity, p pace, measured Forecast) *Forecast {
	days := a.historyDays()
	if days < 1 {
		return nil
	}
	cal := calibrate(all, last, spec, a)
	if cal == nil {
		return nil
	}
	k := cal.pctPerUsd
	fallback := a.averageRate()
	usualAt := func(t float64) usualRateOf {
		if u, ok := a.usualRate(t); ok {
			return u
		}
		return usualRateOf{fallback, fallback, fallback, fallback}
	}
	central := "mean"
	if spec.Period <= day {
		central = "median"
	}
	var momentum *float64
	if a.AsOf >= now-momentumFreshSec {
		m := k * a.costBetween(a.AsOf-hour, a.AsOf)
		momentum = &m
	} else if p.rates != nil {
		m := p.rates.r50
		momentum = &m
	}
	reset := last.R
	step := float64(hour)
	if spec.Period <= day {
		step = 10 * 60
	}
	doRun := func(pick string) run {
		used := last.U
		var exhaustAt *float64
		firstRate := 0.0
		path := [][2]Num{{Num(last.T), Num(last.U)}}
		for from := last.T; from < reset; from += step {
			to := math.Min(reset, from+step)
			mid := (from + to) / 2
			carry, mom := 0.0, 0.0
			if momentum != nil {
				carry = exp(-(mid - last.T) / momentumTauSec)
				mom = *momentum
			}
			rate := carry*mom + (1-carry)*k*usualAt(mid).pick(pick)
			if from == last.T {
				firstRate = rate
			}
			next := used + (rate*(to-from))/hour
			if exhaustAt == nil && next >= 100 {
				e := from + ((100-used)/rate)*hour
				exhaustAt = &e
			}
			used = next
			if exhaustAt == nil {
				path = append(path, [2]Num{Num(to), Num(jsRound(used*10) / 10)})
			}
		}
		if exhaustAt != nil {
			path = append(path, [2]Num{Num(*exhaustAt), 100})
		}
		return run{exhaustAt, used, firstRate, path}
	}
	expected := doRun(central)
	busy := doRun("high")
	quiet := doRun("low")
	spent := func(x run) float64 { return x.usedAtReset - last.U }
	spread := (spent(busy) - spent(quiet)) / math.Max(spent(expected), spreadFloorPct)
	confidence := confidenceLow
	if days >= 14 && cal.strong && spread <= 0.8 {
		confidence = confidenceHigh
	} else if days >= 3 && spread <= 1.6 {
		confidence = confidenceMedium
	}
	f := measured
	f.RatePctPerHour = num(jsRound(expected.firstRate*100) / 100)
	f.Confidence = str(confidence)
	f.Method = "profile"
	f.HistoryDays = num(math.Floor(days))
	f.Path = expected.path
	if expected.exhaustAt == nil {
		f.Status = statusLasts
		f.UsedAtReset = num(math.Min(100, expected.usedAtReset))
		return &f
	}
	e := *expected.exhaustAt
	// A skewed hour can put the average above the busy quartile, so the range is widened to hold the forecast itself.
	early := e
	if busy.exhaustAt != nil {
		early = *busy.exhaustAt
	}
	var late *Num
	if quiet.exhaustAt != nil {
		late = num(math.Max(*quiet.exhaustAt, e))
	}
	f.Status = statusExhausts
	f.ExhaustAt = num(e)
	f.ExhaustRange = []*Num{num(math.Min(early, e)), late}
	return &f
}

// ForecastWindow: all sorted by time, now in epoch seconds; without an activity (or one that cannot be calibrated)
// the forecast extends the pace of the last samples.
func ForecastWindow(all []limits.Sample, now float64, spec WindowSpec, a *Activity) Forecast {
	if len(all) == 0 {
		return Forecast{Status: statusInsufficient, Reason: str(reasonNoSamples.String())}
	}
	last := all[len(all)-1]
	base := Forecast{LastAt: num(last.T), ResetsAt: num(last.R)}
	if last.R <= now {
		base.Status, base.Reason = statusInsufficient, str(reasonWindowReset.String())
		return base
	}
	if last.U >= 100 {
		base.Status, base.UsedAtReset = statusExhausted, num(100)
		return base
	}
	if now-last.T > spec.Fresh {
		base.Status, base.Reason = statusStale, str(reasonNoFresh.String())
		return base
	}
	p := measurePace(all, last, now, spec)
	measured := base
	measured.Samples, measured.SpanSec = len(p.series), Num(p.span)
	if a != nil {
		if f := forecastByProfile(all, last, now, spec, a, p, measured); f != nil {
			return *f
		}
	}
	reset := last.R
	byPace := measured
	byPace.Method = "pace"
	insufficient := func(reason string) Forecast {
		f := byPace
		f.Status, f.Reason = statusInsufficient, str(reason)
		return f
	}
	minSpan := formatSpan(spec.MinSpan)
	need := locale.Pick("needs at least "+minSpan+" of samples", "нужно ≥ "+minSpan+" замеров")
	if len(p.series) < 4 || p.span < spec.MinSpan {
		return insufficient(need)
	}
	lasts := func(rate float64, confidence string) Forecast {
		f := byPace
		f.Status = statusLasts
		f.RatePctPerHour = num(rate)
		f.UsedAtReset = num(math.Min(100, last.U+(math.Max(0, rate)*(reset-last.T))/3600))
		f.Confidence = str(confidence)
		return f
	}
	if p.flat {
		if p.span >= 2*spec.MinSpan {
			return lasts(0, confidenceMedium)
		}
		return insufficient(reasonFlat.String())
	}
	if p.rates == nil {
		return insufficient(need)
	}
	r25, r50, r75 := p.rates.r25, p.rates.r50, p.rates.r75
	if r50 <= idleRatePctPerHour {
		return lasts(r50, confidenceMedium)
	}
	hoursLeft := func(rate float64) float64 { return ((100 - last.U) / rate) * 3600 }
	exhaustAt := last.T + hoursLeft(r50)
	spread := (r75 - r25) / r50
	confidence := confidenceOf(len(p.series), p.span, spread, now-last.T, spec)
	if exhaustAt >= reset {
		return lasts(r50, confidence)
	}
	f := byPace
	f.Status = statusExhausts
	f.RatePctPerHour = num(r50)
	f.ExhaustAt = num(exhaustAt)
	var late *Num
	if r25 > 0 {
		late = num(last.T + hoursLeft(r25))
	}
	f.ExhaustRange = []*Num{num(last.T + hoursLeft(r75)), late}
	f.Confidence = str(confidence)
	return f
}
