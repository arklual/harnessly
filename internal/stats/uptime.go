package stats

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The uptime: minutes in the last seven days when some agent was at work (a prompt or an answer, gaps up to 15 minutes
// bridged), with how many at once. Finished days are kept in <data>/stats/uptime-cache.json, the old runtime's file.

const (
	slotMs           = 60 * 1000
	activityGapMs    = 15 * 60 * 1000
	uptimeDiskVer    = 2
	uptimeKeepDays   = 30
	uptimeMemoKeyStr = "uptime:7d"
)

func (rt *Route) uptimeCachePath() string {
	return filepath.Join(rt.DataDir, "stats", "uptime-cache.json")
}

// claudeStamps are the times of every transcript's sessions on the given days (the timestamp's first ten characters).
func (rt *Route) claudeStamps(days map[string]bool) *omap[*omap[[]float64]] {
	out := newOmap[*omap[[]float64]]()
	files := listClaudeFiles(rt.claudeProjects())
	facts := rt.claude.facts(files, rt.now())
	for i, f := range facts {
		stamps := stampsFor(files[i].path, f, days)
		for _, d := range stamps.keys {
			if !days[d] {
				continue
			}
			day, ok := out.get(d)
			if !ok {
				day = newOmap[[]float64]()
				out.set(d, day)
			}
			src := stamps.m[d]
			for _, sid := range src.keys {
				list, _ := day.get(sid)
				day.set(sid, append(append([]float64(nil), list...), src.m[sid]...))
			}
		}
	}
	return out
}

func (rt *Route) codexStamps(days map[string]bool, order []string) *omap[*omap[[]float64]] {
	out := newOmap[*omap[[]float64]]()
	for _, path := range rt.codexDayFiles(order) {
		a := rt.codexActivity(path)
		sid := "codex:" + jsStr(plainKey(a.sessionID))
		if !truthy(a.sessionID) {
			sid = "codex:" + jscompat.Number(a.startedAt)
		}
		for _, m := range a.messages {
			date := localDate(m.timestamp)
			if !days[date] {
				continue
			}
			day, ok := out.get(date)
			if !ok {
				day = newOmap[[]float64]()
				out.set(date, day)
			}
			list, _ := day.get(sid)
			day.set(sid, append(list, m.timestamp))
		}
	}
	return out
}

// plainKey is a session id as a template literal prints it.
func plainKey(v any) any {
	if n, ok := isNumber(v); ok {
		return n
	}
	return v
}

type streak struct {
	startMs, endMs, minutes, maxConcurrent float64
	active                                 bool
}

func (rt *Route) uptimeResponse(sessions *omap[[]float64], start, now float64, startT jsTime) *jsvalue.Object {
	total := int(math.Ceil((now - start) / slotMs))
	if total < 0 {
		total = 0
	}
	conc := make([]int32, total)
	for _, sid := range sessions.keys {
		ts := sessions.m[sid]
		if len(ts) == 0 {
			continue
		}
		slots := map[int64]bool{}
		for i, t := range ts {
			slot := toInt32((t - start) / slotMs)
			slots[slot] = true
			if i+1 < len(ts) {
				next := ts[i+1]
				if next-t <= activityGapMs {
					end := toInt32((next - start) / slotMs)
					for s := slot; s <= end; s++ {
						slots[s] = true
					}
				}
			}
		}
		for s := range slots {
			if s >= 0 && s < int64(total) {
				conc[s]++
			}
		}
	}
	var streaks []streak
	active, maxConc := 0.0, 0.0
	combo := newOmap[float64]()
	comboLevels := map[int]float64{}
	streakStart, streakMax := -1, 0.0
	for i := 0; i <= total; i++ {
		c := 0.0
		if i < total {
			c = float64(conc[i])
		}
		if c > 0 {
			if streakStart < 0 {
				streakStart, streakMax = i, 0
			}
			active++
			streakMax = math.Max(streakMax, c)
			maxConc = math.Max(maxConc, c)
			for lvl := 2; float64(lvl) <= c; lvl++ {
				comboLevels[lvl]++
			}
		} else if streakStart >= 0 {
			startMs := start + float64(streakStart)*slotMs
			endMs := start + float64(i)*slotMs
			isActive := i >= total
			if isActive {
				endMs = now
			}
			streaks = append(streaks, streak{startMs, endMs, float64(i - streakStart), streakMax, isActive})
			streakStart = -1
		}
	}
	levels := make([]int, 0, len(comboLevels))
	for l := range comboLevels {
		levels = append(levels, l)
	}
	sort.Ints(levels)
	for _, l := range levels {
		combo.set(jscompat.Number(float64(l)), comboLevels[l])
	}
	sortStable(streaks, func(a, b streak) int { return cmpNum(b.startMs - a.startMs) })
	days := make([]any, 7)
	for d := 0; d < 7; d++ {
		dayStart := start + float64(d)*86_400_000
		dayEnd := dayStart + 86_400_000
		type segment struct{ start, minutes, maxConcurrent float64 }
		var segs []segment
		for _, s := range streaks {
			if s.endMs <= dayStart || s.startMs >= dayEnd {
				continue
			}
			segStart, segEnd := math.Max(s.startMs, dayStart), math.Min(s.endMs, dayEnd)
			segs = append(segs, segment{float64(toInt32((segStart - dayStart) / 60_000)),
				math.Max(1, float64(toInt32((segEnd-segStart)/60_000))), s.maxConcurrent})
		}
		sortStable(segs, func(a, b segment) int { return cmpNum(a.start - b.start) })
		list := make([]any, len(segs))
		for i, s := range segs {
			list[i] = jsvalue.NewObject("startMinuteOfDay", s.start, "durationMinutes", s.minutes, "maxConcurrent", s.maxConcurrent)
		}
		days[d] = jsvalue.NewObject("date", startT.addDaysDate(d), "segments", list)
	}
	longest, sum := 0.0, 0.0
	for i, s := range streaks {
		if i == 0 || s.minutes > longest {
			longest = s.minutes
		}
		sum += s.minutes
	}
	avg := 0.0
	if len(streaks) > 0 {
		avg = jsRound(sum / float64(len(streaks)))
	}
	current := jsvalue.NewObject("active", false, "minutes", 0.0, "maxConcurrent", 0.0)
	for _, s := range streaks {
		if s.active {
			current = jsvalue.NewObject("active", true, "minutes", s.minutes, "maxConcurrent", s.maxConcurrent)
			break
		}
	}
	list := make([]any, len(streaks))
	for i, s := range streaks {
		list[i] = jsvalue.NewObject("startMs", s.startMs, "endMs", s.endMs, "durationMinutes", s.minutes, "maxConcurrent", s.maxConcurrent,
			"active", s.active)
	}
	return jsvalue.NewObject("streaks", list, "days", days, "longestStreakMinutes", longest, "averageStreakMinutes", avg,
		"totalStreaks", float64(len(streaks)), "totalActiveMinutes", active, "currentStreak", current, "comboMinutes", countsValue(combo),
		"maxConcurrent", maxConc)
}

// jsTime is the period's first day, for the dates of its seven days.
type jsTime struct{ ms float64 }

func (t jsTime) addDaysDate(n int) string { return localDay(addDays(msTime(t.ms), n)) }

// Uptime answers GET /api/stats/uptime.
func (rt *Route) Uptime(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(func(*http.Request) string { return uptimeMemoKeyStr }, func(*http.Request) (any, error) {
		now := rt.now()
		today := localDay(now)
		start := startOfDay(addDays(now, -6))
		var all []string
		for d := 0; d < 7; d++ {
			all = append(all, localDay(addDays(start, d)))
		}
		var disk *jsvalue.Object
		if raw, err := os.ReadFile(rt.uptimeCachePath()); err == nil {
			if v, err := jsvalue.Parse(raw); err == nil {
				if o, ok := v.(*jsvalue.Object); ok {
					if ver, _ := o.Get("version"); ver == float64(uptimeDiskVer) {
						disk = o
					}
				}
			}
		}
		var diskDays *jsvalue.Object
		if disk != nil {
			diskDays, _ = propValueObj(disk, "days")
		}
		need := map[string]bool{today: true}
		order := []string{today}
		for _, d := range all {
			if d == today {
				continue
			}
			if v, ok := propValue(diskDays, d); !ok || !jsvalue.Truthy(v) {
				if !need[d] {
					order = append(order, d)
				}
				need[d] = true
			}
		}
		fresh := rt.claudeStamps(need)
		codex := rt.codexStamps(need, order)
		for _, date := range codex.keys {
			src := codex.m[date]
			if day, ok := fresh.get(date); ok {
				for _, sid := range src.keys {
					list, _ := day.get(sid)
					day.set(sid, append(append([]float64(nil), list...), src.m[sid]...))
				}
			} else {
				fresh.set(date, src)
			}
		}
		days := jsvalue.NewObject()
		if diskDays != nil {
			days = diskDays.Copy()
		}
		prune := localDay(addDays(now, -uptimeKeepDays))
		for _, date := range days.Keys() {
			if lessJS(date, prune) {
				days.Delete(date)
			}
		}
		for _, date := range fresh.keys {
			if date == today {
				continue
			}
			sessions := jsvalue.NewObject()
			src := fresh.m[date]
			for _, sid := range src.keys {
				list := make([]any, len(src.m[sid]))
				for i, t := range src.m[sid] {
					list[i] = t
				}
				sessions.Set(sid, list)
			}
			days.Set(date, sessions)
		}
		if os.MkdirAll(filepath.Dir(rt.uptimeCachePath()), 0o755) == nil {
			_ = os.WriteFile(rt.uptimeCachePath(), jsvalue.Stringify(jsvalue.NewObject("version", float64(uptimeDiskVer),
				"lastComputedDate", today, "days", days), ""), 0o644)
		}
		merged := newOmap[[]float64]()
		mergeInto := func(sid string, ts []float64) {
			list, _ := merged.get(sid)
			merged.set(sid, append(append([]float64(nil), list...), ts...))
		}
		for _, date := range all {
			if src, ok := fresh.get(date); ok {
				for _, sid := range src.keys {
					mergeInto(sid, src.m[sid])
				}
			} else if cached, ok := propValueObj(days, date); ok {
				for _, sid := range cached.Keys() {
					v, _ := cached.Get(sid)
					arr, _ := v.([]any)
					ts := make([]float64, 0, len(arr))
					for _, x := range arr {
						ts = append(ts, numValue(x))
					}
					mergeInto(sid, ts)
				}
			}
		}
		for _, sid := range merged.keys {
			sort.SliceStable(merged.m[sid], func(i, j int) bool { return z(merged.m[sid][i]) < z(merged.m[sid][j]) })
		}
		startMs := float64(start.UnixMilli())
		return rt.uptimeResponse(merged, startMs, float64(now.UnixMilli()), jsTime{startMs}), nil
	})(r)
}

func propValue(v *jsvalue.Object, k string) (any, bool) {
	if v == nil {
		return nil, false
	}
	return v.Get(k)
}

func propValueObj(v *jsvalue.Object, k string) (*jsvalue.Object, bool) {
	x, ok := propValue(v, k)
	o, isObj := x.(*jsvalue.Object)
	return o, ok && isObj
}
