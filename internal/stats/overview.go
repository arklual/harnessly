package stats

import (
	"net/http"
	"sort"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/pricing"
)

func claudeCost(model string, input, output, cacheRead, write5m, write1h float64) float64 {
	return pricing.ClaudeCostByFullID(model, z(input), z(output), z(write5m), z(write1h), z(cacheRead))
}

// overviewTokens is a model's row of modelTokens.
type overviewTokens struct {
	input, output, cacheRead, cacheCreation, cacheCreation5m, cacheCreation1h, cost float64
	codex                                                                           bool
	model                                                                           *string
	cachedInput                                                                     float64
}

func (t *overviewTokens) value() *jsvalue.Object {
	o := jsvalue.NewObject("input", t.input, "output", t.output, "cacheRead", t.cacheRead, "cacheCreation", t.cacheCreation,
		"cacheCreation5m", t.cacheCreation5m, "cacheCreation1h", t.cacheCreation1h, "cost", t.cost)
	if t.codex {
		o.Set("provider", "codex")
		if t.model != nil {
			o.Set("model", *t.model)
		} else {
			o.Set("model", nil)
		}
		o.Set("cachedInput", t.cachedInput)
	}
	return o
}

type dayTotals struct {
	date                                    string
	input, output, cacheRead, cacheCreation float64
}

func (d dayTotals) value() *jsvalue.Object {
	return jsvalue.NewObject("date", d.date, "input", d.input, "output", d.output, "cacheRead", d.cacheRead, "cacheCreation", d.cacheCreation)
}

// activityRow is a dailyActivity row; the four per-provider counts are present once the Codex days are merged in.
type activityRow struct {
	dailyActivity
	split                                  bool
	claudeMsgs, codexMsgs, claudeS, codexS float64
}

func setNum(o *jsvalue.Object, k string, v float64) {
	if !isUndef(v) {
		o.Set(k, v)
	}
}

func (a activityRow) value() *jsvalue.Object {
	o := jsvalue.NewObject("date", a.date)
	setNum(o, "messageCount", a.messageCount)
	setNum(o, "sessionCount", a.sessionCount)
	setNum(o, "toolCallCount", a.toolCallCount)
	if a.split {
		setNum(o, "claudeMessageCount", a.claudeMsgs)
		setNum(o, "codexMessageCount", a.codexMsgs)
		setNum(o, "claudeSessionCount", a.claudeS)
		setNum(o, "codexSessionCount", a.codexS)
	}
	return o
}

type overview struct {
	totalSessions, totalMessages, previousSessions, previousMessages, totalToolCalls float64
	daily                                                                            []activityRow
	models                                                                           *omap[*overviewTokens]
	dailyTokens                                                                      []dayTotals
	todayMessages, thisMonthMessages                                                 float64
	totalCost, todayCost, thisMonthCost, previousCost                                float64
	hours, dayHours                                                                  *omap[float64]
	firstSessionDate, lastComputedDate, computedAt                                   string
}

func (o *overview) value() *jsvalue.Object {
	daily := make([]any, len(o.daily))
	for i, d := range o.daily {
		daily[i] = d.value()
	}
	models := jsvalue.NewObject()
	for _, k := range o.models.ordered() {
		models.Set(k, o.models.m[k].value())
	}
	tokens := make([]any, len(o.dailyTokens))
	for i, d := range o.dailyTokens {
		tokens[i] = d.value()
	}
	return jsvalue.NewObject("totalSessions", o.totalSessions, "totalMessages", o.totalMessages, "previousSessions", o.previousSessions,
		"previousMessages", o.previousMessages, "totalToolCalls", o.totalToolCalls, "dailyActivity", daily, "modelTokens", models,
		"dailyTokens", tokens, "todayMessages", o.todayMessages, "thisMonthMessages", o.thisMonthMessages, "totalCost", o.totalCost,
		"todayCost", o.todayCost, "thisMonthCost", o.thisMonthCost, "previousCost", o.previousCost,
		"hourlyDistribution", countsValue(o.hours), "dayHourDistribution", countsValue(o.dayHours),
		"firstSessionDate", o.firstSessionDate, "lastComputedDate", o.lastComputedDate, "computedAt", o.computedAt)
}

// previousRange is the period before a 7- or 30-day one; false for the others.
func (rt *Route) previousRange(p period) (start, end float64, ok bool) {
	days := map[period]int{period7d: 7, period30d: 30}[p]
	if days == 0 {
		return 0, 0, false
	}
	now := rt.now()
	return float64(startOfDay(addDays(now, -2*days)).UnixMilli()), float64(startOfDay(addDays(now, -days)).UnixMilli()), true
}

func (rt *Route) inPrevious(ms float64, p period) bool {
	start, end, ok := rt.previousRange(p)
	return ok && ms >= start && ms < end
}

func nowISO(t time.Time) string {
	s, _ := jscompat.ISOString(float64(t.UnixMilli()))
	return s
}

// buildOverview is the Claude part of the overview from the stats cache.
func (rt *Route) buildOverview(c *statsCache, p period) *overview {
	o := &overview{models: newOmap[*overviewTokens](), lastComputedDate: c.lastComputedDate, firstSessionDate: c.firstSessionDate,
		hours: c.hours, dayHours: c.dayHours, computedAt: nowISO(rt.now())}
	var tokens []dayTokens
	for _, d := range c.daily {
		if rt.withinDate(d.date, p) {
			o.daily = append(o.daily, activityRow{dailyActivity: d})
		}
	}
	for _, d := range c.tokens {
		if rt.withinDate(d.date, p) {
			tokens = append(tokens, d)
		}
	}
	if p == periodAll {
		o.totalSessions, o.totalMessages = c.totalSessions, c.totalMessages
	} else {
		for _, d := range o.daily {
			o.totalSessions = add(o.totalSessions, d.sessionCount)
			o.totalMessages = add(o.totalMessages, d.messageCount)
		}
	}
	for _, d := range o.daily {
		o.totalToolCalls = add(o.totalToolCalls, d.toolCallCount)
	}
	for _, d := range c.daily {
		if rt.inPrevious(dayjsParse(d.date), p) {
			o.previousSessions = add(o.previousSessions, d.sessionCount)
			o.previousMessages = add(o.previousMessages, d.messageCount)
		}
	}
	if p == periodAll {
		for _, m := range c.models.ordered() {
			u := c.models.m[m]
			o.models.set(m, &overviewTokens{input: u.input, output: u.output, cacheRead: u.cacheRead, cacheCreation: u.cacheCreation,
				cacheCreation5m: u.cacheCreation5m, cacheCreation1h: u.cacheCreation1h,
				cost: claudeCost(m, u.input, u.output, u.cacheRead, u.cacheCreation5m, u.cacheCreation1h)})
		}
	} else {
		for _, d := range tokens {
			for _, m := range d.byModel.ordered() {
				b := d.byModel.m[m]
				t, ok := o.models.get(m)
				if !ok {
					t = &overviewTokens{}
					o.models.set(m, t)
				}
				t.input = add(t.input, b.input)
				t.output = add(t.output, b.output)
				t.cacheRead = add(t.cacheRead, b.cacheRead)
				t.cacheCreation = add(t.cacheCreation, b.cacheCreation)
				t.cacheCreation5m = add(t.cacheCreation5m, b.cacheCreation5m)
				t.cacheCreation1h = add(t.cacheCreation1h, b.cacheCreation1h)
			}
		}
		for _, m := range o.models.ordered() {
			t := o.models.m[m]
			t.cost = claudeCost(m, t.input, t.output, t.cacheRead, t.cacheCreation5m, t.cacheCreation1h)
		}
	}
	for _, d := range tokens {
		row := dayTotals{date: d.date}
		for _, m := range d.byModel.ordered() {
			b := d.byModel.m[m]
			row.input = add(row.input, b.input)
			row.output = add(row.output, b.output)
			row.cacheRead = add(row.cacheRead, b.cacheRead)
			row.cacheCreation = add(row.cacheCreation, b.cacheCreation)
		}
		o.dailyTokens = append(o.dailyTokens, row)
	}
	now := rt.now()
	today := localDay(now)
	y, mo, _ := now.Date()
	monthStart := localDay(time.Date(y, mo, 1, 0, 0, 0, 0, time.Local))
	for _, d := range c.daily {
		if d.date == today {
			if isNull(d.messageCount) || isUndef(d.messageCount) {
				o.todayMessages = 0
			} else {
				o.todayMessages = d.messageCount
			}
			break
		}
	}
	for _, d := range c.daily {
		if !lessJS(d.date, monthStart) {
			o.thisMonthMessages = add(o.thisMonthMessages, d.messageCount)
		}
	}
	for _, m := range o.models.ordered() {
		o.totalCost = add(o.totalCost, o.models.m[m].cost)
	}
	costOf := func(keep func(dayTokens) bool) float64 {
		cost := 0.0
		for _, d := range c.tokens {
			if !keep(d) {
				continue
			}
			for _, m := range d.byModel.ordered() {
				b := d.byModel.m[m]
				cost += claudeCost(m, b.input, b.output, b.cacheRead, b.cacheCreation5m, b.cacheCreation1h)
			}
		}
		return cost
	}
	o.todayCost = costOf(func(d dayTokens) bool { return d.date == today })
	o.thisMonthCost = costOf(func(d dayTokens) bool { return !lessJS(d.date, monthStart) })
	o.previousCost = costOf(func(d dayTokens) bool { return rt.inPrevious(dayjsParse(d.date), p) })
	return o
}

// mergeCodexOverview adds Codex's sessions, prompts and spend to the overview.
func (rt *Route) mergeCodexOverview(o *overview, p period) *overview {
	needsPrevious := p == period7d || p == period30d
	sessions := rt.codexSessionsFor(p)
	history := rt.codexHistory(p)
	stats := rt.codexJSONL(p)
	var allStats *codexProvider
	var allSessions []sessionStats
	var allHistory []codexMessage
	if needsPrevious {
		allStats, allSessions, allHistory = rt.codexJSONL(periodAll), rt.codexSessionsFor(periodAll), rt.codexHistory(periodAll)
	}

	codexDays := newOmap[*activityRow]()
	dayOf := func(date string) *activityRow {
		d, ok := codexDays.get(date)
		if !ok {
			d = &activityRow{dailyActivity: dailyActivity{date: date}, split: true}
			codexDays.set(date, d)
		}
		return d
	}
	for _, s := range sessions {
		d := dayOf(slice16(s.startedAt, 0, 10))
		d.sessionCount++
		d.codexS++
	}
	for _, m := range history {
		d := dayOf(localDate(m.timestamp))
		d.messageCount++
		d.codexMsgs++
	}

	codexModels := newOmap[*overviewTokens]()
	codexTokens := newOmap[*dayTotals]()
	for _, s := range stats.sessions {
		key := "codex:unknown"
		if s.model != nil {
			key = "codex:" + *s.model
		}
		input := jsMax0(s.inputTokens - s.cachedInput)
		t, ok := codexModels.get(key)
		if !ok {
			t = &overviewTokens{codex: true, model: s.model}
			codexModels.set(key, t)
		}
		t.input += input
		t.output += s.outputTokens
		t.cacheRead += s.cachedInput
		if s.cost != nil {
			t.cost += *s.cost
		}
		t.cachedInput += s.cachedInput
		date := localDate(s.startedAt)
		d, ok := codexTokens.get(date)
		if !ok {
			d = &dayTotals{date: date}
			codexTokens.set(date, d)
		}
		d.input += input
		d.output += s.outputTokens
		d.cacheRead += s.cachedInput
	}
	hours, dayHours := newOmap[float64](), newOmap[float64]()
	for _, m := range history {
		t := msTime(clipMs(m.timestamp))
		h := jscompat.Number(float64(t.Hour()))
		c, _ := hours.get(h)
		hours.set(h, c+1)
		k := jscompat.Number(float64(t.Weekday())) + "-" + h
		c, _ = dayHours.get(k)
		dayHours.set(k, c+1)
	}
	sumCost := func(list []*codexSession, keep func(*codexSession) bool) float64 {
		total := 0.0
		for _, s := range list {
			if keep(s) && s.cost != nil {
				total += *s.cost
			}
		}
		return total
	}
	all := func(*codexSession) bool { return true }
	now := rt.now()
	today := localDay(now)
	y, mo, _ := now.Date()
	monthStart := float64(time.Date(y, mo, 1, 0, 0, 0, 0, time.Local).UnixMilli())
	isToday := func(s *codexSession) bool { return localDate(s.startedAt) == today }
	inMonth := func(s *codexSession) bool { return s.startedAt >= monthStart }
	codexCost := sumCost(stats.sessions, all)
	var todayCost float64
	if allStats != nil {
		todayCost = sumCost(allStats.sessions, isToday)
	} else {
		todayCost = sumCost(stats.sessions, isToday)
	}
	monthStats := stats
	if allStats != nil {
		monthStats = allStats
	}
	monthCost := sumCost(monthStats.sessions, inMonth)
	todayMessages, monthMessages := 0.0, 0.0
	for _, m := range history {
		if localDate(m.timestamp) == today {
			todayMessages++
		}
	}
	monthHistory := history
	if allHistory != nil || needsPrevious {
		monthHistory = allHistory
	}
	for _, m := range monthHistory {
		if clipMs(m.timestamp) >= monthStart {
			monthMessages++
		}
	}
	var firsts []string
	for _, s := range sessions {
		firsts = append(firsts, slice16(s.startedAt, 0, 10))
	}
	sort.Slice(firsts, func(i, j int) bool { return lessJS(firsts[i], firsts[j]) })
	codexFirst := ""
	if len(firsts) > 0 {
		codexFirst = firsts[0]
	}
	var cands []string
	for _, s := range []string{o.firstSessionDate, codexFirst} {
		if s != "" {
			cands = append(cands, s)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return lessJS(cands[i], cands[j]) })
	first := ""
	if len(cands) > 0 {
		first = cands[0]
	}
	prevSessions, prevMessages, prevCost := 0.0, 0.0, 0.0
	for _, s := range allSessions {
		if rt.inPrevious(jscompat.ParseDate(s.startedAt), p) {
			prevSessions++
		}
	}
	for _, m := range allHistory {
		if rt.inPrevious(clipMs(m.timestamp), p) {
			prevMessages++
		}
	}
	if allStats != nil {
		prevCost = sumCost(allStats.sessions, func(s *codexSession) bool { return rt.inPrevious(s.startedAt, p) })
	}

	merged := *o
	merged.totalSessions = add(o.totalSessions, float64(len(sessions)))
	merged.totalMessages = add(o.totalMessages, float64(len(history)))
	merged.previousSessions = add(o.previousSessions, prevSessions)
	merged.previousMessages = add(o.previousMessages, prevMessages)
	merged.daily = mergeActivity(o.daily, codexDays)
	merged.models = newOmap[*overviewTokens]()
	for _, k := range o.models.ordered() {
		merged.models.set(k, o.models.m[k])
	}
	for _, k := range codexModels.ordered() {
		merged.models.set(k, codexModels.m[k])
	}
	merged.dailyTokens = mergeDayTotals(o.dailyTokens, codexTokens)
	merged.hours = mergeCounts(o.hours, hours)
	merged.dayHours = mergeCounts(o.dayHours, dayHours)
	merged.todayMessages = add(o.todayMessages, todayMessages)
	merged.thisMonthMessages = add(o.thisMonthMessages, monthMessages)
	merged.totalCost = add(o.totalCost, codexCost)
	merged.todayCost = add(o.todayCost, todayCost)
	merged.thisMonthCost = add(o.thisMonthCost, monthCost)
	merged.previousCost = add(o.previousCost, prevCost)
	merged.firstSessionDate = first
	return &merged
}

// coalesce is a ?? b for a cached count.
func coalesce(a, b float64) float64 {
	if isNull(a) || isUndef(a) {
		return b
	}
	return a
}

func mergeActivity(base []activityRow, additions *omap[*activityRow]) []activityRow {
	rows := newOmap[*activityRow]()
	for _, d := range base {
		c := d
		c.split = true
		c.claudeMsgs = coalesce(undefNum, d.messageCount)
		c.codexMsgs = 0
		c.claudeS = coalesce(undefNum, d.sessionCount)
		c.codexS = 0
		rows.set(d.date, &c)
	}
	for _, k := range additions.keys {
		day := additions.m[k]
		if e, ok := rows.get(day.date); ok {
			e.messageCount = add(e.messageCount, day.messageCount)
			e.sessionCount = add(e.sessionCount, day.sessionCount)
			e.toolCallCount = add(e.toolCallCount, day.toolCallCount)
			e.claudeMsgs = coalesce(e.claudeMsgs, 0)
			e.codexMsgs = add(coalesce(e.codexMsgs, 0), coalesce(day.codexMsgs, day.messageCount))
			e.claudeS = coalesce(e.claudeS, 0)
			e.codexS = add(coalesce(e.codexS, 0), coalesce(day.codexS, day.sessionCount))
		} else {
			c := *day
			rows.set(day.date, &c)
		}
	}
	out := make([]activityRow, 0, rows.len())
	for _, k := range rows.keys {
		out = append(out, *rows.m[k])
	}
	col := newCollator()
	sortStable(out, func(a, b activityRow) int { return col.CompareString(a.date, b.date) })
	return out
}

func mergeDayTotals(base []dayTotals, additions *omap[*dayTotals]) []dayTotals {
	rows := newOmap[*dayTotals]()
	for _, d := range base {
		c := d
		rows.set(d.date, &c)
	}
	for _, k := range additions.keys {
		day := additions.m[k]
		if e, ok := rows.get(day.date); ok {
			e.input = add(e.input, day.input)
			e.output = add(e.output, day.output)
			e.cacheRead = add(e.cacheRead, day.cacheRead)
			e.cacheCreation = add(e.cacheCreation, day.cacheCreation)
		} else {
			c := *day
			rows.set(day.date, &c)
		}
	}
	out := make([]dayTotals, 0, rows.len())
	for _, k := range rows.keys {
		out = append(out, *rows.m[k])
	}
	col := newCollator()
	sortStable(out, func(a, b dayTotals) int { return col.CompareString(a.date, b.date) })
	return out
}

func mergeCounts(base, additions *omap[float64]) *omap[float64] {
	out := newOmap[float64]()
	for _, k := range base.ordered() {
		out.set(k, base.m[k])
	}
	for _, k := range additions.ordered() {
		c, ok := out.get(k)
		if !ok {
			c = 0
		}
		out.set(k, add(c, additions.m[k]))
	}
	return out
}

// Overview answers GET /api/stats/overview?period=.
func (rt *Route) Overview(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(func(r *http.Request) string { return "overview:" + string(periodOf(r)) }, func(r *http.Request) (any, error) {
		p := periodOf(r)
		c, err := rt.statsCache()
		if err != nil {
			return nil, err
		}
		return rt.mergeCodexOverview(rt.buildOverview(c, p), p).value(), nil
	})(r)
}
