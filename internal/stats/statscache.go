package stats

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// The stats cache: Claude Code days summed from the transcripts, finished days kept in <data>/stats/cache.json (the
// old runtime's file and format, which both runtimes read and write), today recomputed, the result held five minutes.

const statsCacheVersion = 3

// undefNum is a field a cached day lacks: arithmetic makes it NaN, and an object it is copied into leaves the key out.
var undefNum = math.Float64frombits(0x7ff8000000000dad)

func isUndef(f float64) bool { return math.Float64bits(f) == 0x7ff8000000000dad }

// errMalformed is a cache file whose days the old runtime could not walk (it answered 500).
var errMalformed = errors.New("malformed stats cache")

type dailyActivity struct {
	date                                      string
	messageCount, sessionCount, toolCallCount float64
}

type breakdown struct {
	input, output, cacheRead, cacheCreation, cacheCreation5m, cacheCreation1h float64
}

type dayTokens struct {
	date    string
	byModel *omap[*breakdown]
}

type modelUsage struct {
	input, output, cacheRead, cacheCreation, cacheCreation5m, cacheCreation1h float64
}

type longestSession struct {
	sessionID    any
	duration     float64
	messageCount any
	timestamp    any
}

type statsCache struct {
	lastComputedDate string
	daily            []dailyActivity
	tokens           []dayTokens
	models           *omap[*modelUsage]
	totalSessions    float64
	totalMessages    float64
	longest          longestSession
	firstSessionDate string
	hours            *omap[float64]
	dayHours         *omap[float64]
}

// cachedNum is a number field of the cache file as the old code met it.
func cachedNum(o *jsvalue.Object, k string) float64 {
	v, ok := o.Get(k)
	if !ok {
		return undefNum
	}
	switch x := v.(type) {
	case nil:
		return nullNum
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return math.NaN()
}

// entriesOf is Object.entries of a cached value; false where it threw.
func entriesOf(v any) ([]string, func(string) any, bool) {
	switch x := v.(type) {
	case *jsvalue.Object:
		return x.Keys(), func(k string) any { r, _ := x.Get(k); return r }, true
	case []any:
		keys := make([]string, len(x))
		for i := range x {
			keys[i] = jscompat.Number(float64(i))
		}
		return keys, func(k string) any {
			for i := range x {
				if keys[i] == k {
					return x[i]
				}
			}
			return nil
		}, true
	}
	return nil, nil, false
}

// dayValue is a computed day in the cache file's form.
func dayValue(d *dayData) *jsvalue.Object {
	models := jsvalue.NewObject()
	for _, k := range d.models.keys {
		t := d.models.m[k]
		models.Set(k, jsvalue.NewObject("input", t[0], "output", t[1], "cacheRead", t[2], "cacheCreation", t[3], "cacheCreation5m", t[4],
			"cacheCreation1h", t[5]))
	}
	sessions := make([]any, 0, d.sessions.len())
	for _, k := range d.sessions.keys {
		s := d.sessions.m[k]
		sessions = append(sessions, jsvalue.NewObject("id", k, "start", s.start, "end", s.end, "messages", s.messages))
	}
	return jsvalue.NewObject("messageCount", d.messageCount, "sessionCount", float64(d.sessions.len()), "toolCallCount", d.toolCallCount,
		"hourCounts", countsValue(d.hours), "modelTokens", models, "sessions", sessions)
}

func (rt *Route) statsCachePath() string { return filepath.Join(rt.DataDir, "stats", "cache.json") }

func (rt *Route) readStatsCacheFile() *jsvalue.Object {
	raw, err := os.ReadFile(rt.statsCachePath())
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil
	}
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return nil
	}
	if ver, _ := o.Get("version"); ver != float64(statsCacheVersion) {
		return nil
	}
	return o
}

func (rt *Route) writeStatsCacheFile(o *jsvalue.Object) {
	dir := filepath.Dir(rt.statsCachePath())
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	_ = os.WriteFile(rt.statsCachePath(), jsvalue.Stringify(o, ""), 0o644)
}

// mergeDay adds a file's day into the day summed so far.
func mergeDay(target, source *dayData) {
	target.messageCount += source.messageCount
	target.toolCallCount += source.toolCallCount
	for _, h := range source.hours.ordered() {
		c, _ := target.hours.get(h)
		target.hours.set(h, c+source.hours.m[h])
	}
	for _, m := range source.models.keys {
		t, ok := target.models.get(m)
		if !ok {
			t = &tokens{}
			target.models.set(m, t)
		}
		for i, v := range source.models.m[m] {
			t[i] += v
		}
	}
	for _, id := range source.sessions.keys {
		s := source.sessions.m[id]
		if e, ok := target.sessions.get(id); ok {
			e.messages += s.messages
			if lessJS(s.start, e.start) {
				e.start = s.start
			}
			if lessJS(e.end, s.end) {
				e.end = s.end
			}
		} else {
			c := *s
			target.sessions.set(id, &c)
		}
	}
}

// missingDays sums the given days over every transcript.
func (rt *Route) missingDays(days map[string]bool) *omap[*dayData] {
	merged := newOmap[*dayData]()
	if len(days) == 0 {
		return merged
	}
	files := listClaudeFiles(rt.claudeProjects())
	facts := rt.claude.facts(files, rt.now())
	for i, f := range facts {
		if f.daysCrossDup {
			f = readClaudeFacts(files[i].path, "", &factsFilter{days: days})
		}
		for _, date := range f.days.keys {
			if !days[date] {
				continue
			}
			d := f.days.m[date]
			if e, ok := merged.get(date); ok {
				mergeDay(e, d)
			} else {
				merged.set(date, d.clone())
			}
		}
	}
	return merged
}

func (rt *Route) firstDate() (string, bool) {
	files := listClaudeFiles(rt.claudeProjects())
	if len(files) == 0 {
		return "", false
	}
	var dates []string
	for _, f := range rt.claude.facts(files, rt.now()) {
		if f.hasFirst {
			dates = append(dates, f.firstDate)
		}
	}
	if len(dates) == 0 {
		return "", false
	}
	sort.Slice(dates, func(i, j int) bool { return lessJS(dates[i], dates[j]) })
	return dates[0], true
}

const statsMemoryTTL = 300 * time.Second

// statsCache is getStatsCache: the cached result for five minutes, else the file brought up to yesterday and today
// added.
func (rt *Route) statsCache() (*statsCache, error) {
	now := rt.now()
	rt.statsMu.Lock()
	defer rt.statsMu.Unlock()
	if rt.statsMemo != nil && now.Before(rt.statsMemoUntil) {
		return rt.statsMemo, nil
	}
	yesterday := localDay(addDays(now, -1))
	today := localDay(now)
	cache := rt.readStatsCacheFile()
	missing := map[string]bool{today: true}
	if cache == nil {
		cache = jsvalue.NewObject("version", float64(statsCacheVersion), "lastComputedDate", "", "days", jsvalue.NewObject())
		first, ok := rt.firstDate()
		if !ok {
			return buildStatsCache(cache, today, nil)
		}
		for d, end := dayjsParse(first), dayjsParse(yesterday); d <= end; {
			t := msTime(d)
			missing[localDay(t)] = true
			d = float64(addDays(t, 1).UnixMilli())
		}
	}
	days, _ := cache.Get("days")
	daysObj, ok := days.(*jsvalue.Object)
	if !ok {
		return nil, errMalformed
	}
	if last, _ := cache.Get("lastComputedDate"); isString(last) && lessJS(last.(string), yesterday) {
		for d, end := dayjsParse(last.(string)), dayjsParse(yesterday); ; {
			if math.IsNaN(d) {
				break
			}
			d = float64(addDays(msTime(d), 1).UnixMilli())
			if !(d <= end) {
				break
			}
			date := localDay(msTime(d))
			if v, ok := daysObj.Get(date); !ok || !jsvalue.Truthy(v) {
				missing[date] = true
			}
		}
	}
	fresh := rt.missingDays(missing)
	for _, date := range fresh.keys {
		if date != today {
			daysObj.Set(date, dayValue(fresh.m[date]))
		}
	}
	cache.Set("lastComputedDate", yesterday)
	rt.writeStatsCacheFile(cache)
	var todayData *dayData
	if d, ok := fresh.get(today); ok {
		todayData = d
	}
	result, err := buildStatsCache(cache, today, todayData)
	if err != nil {
		return nil, err
	}
	rt.statsMemo, rt.statsMemoUntil = result, now.Add(statsMemoryTTL)
	return result, nil
}

func isString(v any) bool { _, ok := v.(string); return ok }

type cachedDay struct {
	date string
	day  *jsvalue.Object
}

// buildStatsCache turns the days into the summary every route reads.
func buildStatsCache(cache *jsvalue.Object, today string, todayData *dayData) (*statsCache, error) {
	out := &statsCache{lastComputedDate: today, models: newOmap[*modelUsage](), hours: newOmap[float64](), dayHours: newOmap[float64](),
		longest: longestSession{sessionID: "", duration: 0, messageCount: 0.0, timestamp: ""}}
	var all []cachedDay
	days, _ := cache.Get("days")
	keys, get, ok := entriesOf(days)
	if !ok {
		return nil, errMalformed
	}
	for _, k := range keys {
		d, isObj := get(k).(*jsvalue.Object)
		if !isObj {
			return nil, errMalformed
		}
		all = append(all, cachedDay{k, d})
	}
	if todayData != nil {
		all = append(all, cachedDay{today, dayValue(todayData)})
	}
	col := newCollator()
	sort.SliceStable(all, func(i, j int) bool { return col.CompareString(all[i].date, all[j].date) < 0 })
	for _, cd := range all {
		day := cd.day
		messages := cachedNum(day, "messageCount")
		if out.firstSessionDate == "" && z(messages) > 0 {
			out.firstSessionDate = cd.date
		}
		sessions := cachedNum(day, "sessionCount")
		out.daily = append(out.daily, dailyActivity{cd.date, messages, sessions, cachedNum(day, "toolCallCount")})
		mt, _ := day.Get("modelTokens")
		mkeys, mget, ok := entriesOf(mt)
		if !ok {
			return nil, errMalformed
		}
		byModel := newOmap[*breakdown]()
		for _, model := range mkeys {
			t, _ := mget(model).(*jsvalue.Object)
			b := &breakdown{}
			if t != nil {
				b = &breakdown{cachedNum(t, "input"), cachedNum(t, "output"), cachedNum(t, "cacheRead"), cachedNum(t, "cacheCreation"),
					cachedNum(t, "cacheCreation5m"), cachedNum(t, "cacheCreation1h")}
			} else {
				b = &breakdown{undefNum, undefNum, undefNum, undefNum, undefNum, undefNum}
			}
			byModel.set(model, b)
			mu, ok := out.models.get(model)
			if !ok {
				mu = &modelUsage{}
				out.models.set(model, mu)
			}
			mu.input = add(mu.input, b.input)
			mu.output = add(mu.output, b.output)
			mu.cacheRead = add(mu.cacheRead, b.cacheRead)
			mu.cacheCreation = add(mu.cacheCreation, b.cacheCreation)
			mu.cacheCreation5m = add(mu.cacheCreation5m, b.cacheCreation5m)
			mu.cacheCreation1h = add(mu.cacheCreation1h, b.cacheCreation1h)
		}
		out.tokens = append(out.tokens, dayTokens{cd.date, byModel})
		dowMs := jscompat.ParseDate(cd.date)
		dow := "NaN"
		if !math.IsNaN(dowMs) {
			dow = jscompat.Number(float64(msTime(dowMs).Weekday()))
		}
		hc, _ := day.Get("hourCounts")
		hkeys, hget, ok := entriesOf(hc)
		if !ok {
			return nil, errMalformed
		}
		for _, h := range hkeys {
			count := numValue(hget(h))
			c, _ := out.hours.get(h)
			out.hours.set(h, add(c, count))
			key := dow + "-" + h
			c, _ = out.dayHours.get(key)
			out.dayHours.set(key, add(c, count))
		}
		out.totalSessions = add(out.totalSessions, sessions)
		out.totalMessages = add(out.totalMessages, messages)
		ss, _ := day.Get("sessions")
		list, ok := ss.([]any)
		if !ok {
			return nil, errMalformed
		}
		for _, s := range list {
			so, ok := s.(*jsvalue.Object)
			if !ok {
				return nil, errMalformed
			}
			end, _ := so.Get("end")
			start, _ := so.Get("start")
			duration := jscompat.DateValue(end) - jscompat.DateValue(start)
			if duration > out.longest.duration {
				id, _ := so.Get("id")
				msgs, _ := so.Get("messages")
				out.longest = longestSession{sessionID: id, duration: duration, messageCount: msgs, timestamp: start}
			}
		}
	}
	return out, nil
}

// numValue is a cached count as an operand: null is 0, anything not a number NaN.
func numValue(v any) float64 {
	switch x := v.(type) {
	case nil:
		return nullNum
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return math.NaN()
}
