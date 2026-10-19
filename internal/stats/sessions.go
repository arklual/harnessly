package stats

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// claudeAgg is one Claude session of a period, as parseJsonlStream summed it.
type claudeAgg struct {
	sessionID, project, startedAt, lastActivityAt string
	messages, input, output, read, write          float64
	model                                         string
}

// fileSessions are a transcript's sessions in a period: the kept facts summed over the days the period covers, or the
// file read again when those could differ from a read of the period alone.
func (rt *Route) fileSessions(file claudeFile, f *claudeFacts, p period) []claudeAgg {
	now := float64(rt.now().UnixMilli())
	startDay := dayNumber(float64(rt.periodStart(p).UnixMilli()))
	var keys []int
	exact := !f.bucketCrossDp
	for k, b := range f.buckets {
		if p != periodAll {
			if k == invalidDay || k < startDay {
				continue
			}
			if b.maxMs > now {
				exact = false
			}
		}
		keys = append(keys, k)
	}
	if !exact {
		start := float64(rt.periodStart(p).UnixMilli())
		f = readClaudeFacts(file.path, "", &factsFilter{period: func(ts string, ms float64) bool {
			if p == periodAll {
				return true
			}
			ms = clipMs(ms)
			return ms >= start && ms <= now
		}})
		keys = keys[:0]
		for k := range f.buckets {
			keys = append(keys, k)
		}
	}
	sessions := map[string]*periodSession{}
	for _, k := range keys {
		b := f.buckets[k]
		for _, id := range b.sessions.keys {
			s := b.sessions.m[id]
			e, ok := sessions[id]
			if !ok {
				c := *s
				sessions[id] = &c
				continue
			}
			if s.firstLine < e.firstLine {
				e.firstLine = s.firstLine
			}
			if lessJS(s.start, e.start) {
				e.start = s.start
			}
			if lessJS(e.end, s.end) {
				e.end = s.end
			}
			e.messages += s.messages
			e.input += s.input
			e.output += s.output
			e.read += s.read
			e.write += s.write
			if s.modelLine > e.modelLine {
				e.modelLine, e.model = s.modelLine, s.model
			}
		}
	}
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return sessions[ids[i]].firstLine < sessions[ids[j]].firstLine })
	project := file.project
	if f.cwd != "" {
		project = rt.shortenCwd(f.cwd)
	}
	out := make([]claudeAgg, len(ids))
	for i, id := range ids {
		s := sessions[id]
		out[i] = claudeAgg{id, project, s.start, s.end, s.messages, s.input, s.output, s.read, s.write, s.model}
	}
	return out
}

func (rt *Route) claudeSessions(p period) [][]claudeAgg {
	files := listClaudeFiles(rt.claudeProjects())
	facts := rt.claude.facts(files, rt.now())
	out := make([][]claudeAgg, len(files))
	for i := range files {
		out[i] = rt.fileSessions(files[i], facts[i], p)
	}
	return out
}

func (rt *Route) claudeSessionStats(p period) []sessionStats {
	var out []sessionStats
	for _, list := range rt.claudeSessions(p) {
		for _, s := range list {
			out = append(out, sessionStats{s.sessionID, s.project, s.startedAt, s.lastActivityAt, s.messages, s.input + s.output,
				s.input + s.output + s.read + s.write, s.model})
		}
	}
	col := newCollator()
	sortStable(out, func(a, b sessionStats) int { return col.CompareString(b.startedAt, a.startedAt) })
	return out
}

func (s sessionStats) value() *jsvalue.Object {
	return jsvalue.NewObject("sessionId", plainValue(s.sessionID), "project", s.project, "startedAt", s.startedAt,
		"lastActivityAt", s.lastActivityAt, "messageCount", s.messageCount, "totalTokens", s.totalTokens,
		"totalTokensWithCached", s.withCached, "model", s.model)
}

type projectStats struct {
	project                   string
	sessions, messages, total float64
}

func (p projectStats) value() *jsvalue.Object {
	return jsvalue.NewObject("project", p.project, "sessionCount", p.sessions, "messageCount", p.messages, "totalTokens", p.total)
}

func sortProjects(list []projectStats) {
	sortStable(list, func(a, b projectStats) int { return cmpNum(b.total - a.total) })
}

func (rt *Route) claudeProjectsFor(p period) []projectStats {
	byName := newOmap[*projectStats]()
	for _, list := range rt.claudeSessions(p) {
		for _, s := range list {
			if e, ok := byName.get(s.project); ok {
				e.sessions++
				e.messages += s.messages
				e.total += s.input + s.output
			} else {
				byName.set(s.project, &projectStats{s.project, 1, s.messages, s.input + s.output})
			}
		}
	}
	return projectList(byName)
}

func projectList(m *omap[*projectStats]) []projectStats {
	out := make([]projectStats, 0, m.len())
	for _, k := range m.keys {
		out = append(out, *m.m[k])
	}
	sortProjects(out)
	return out
}

func (rt *Route) codexProjectsFor(p period) []projectStats {
	byName := newOmap[*projectStats]()
	for _, s := range rt.codexSessionsFor(p) {
		name := s.project
		if name == "" {
			name = "(unknown)"
		}
		if e, ok := byName.get(name); ok {
			e.sessions++
			e.messages += s.messageCount
			e.total += s.totalTokens
		} else {
			byName.set(name, &projectStats{name, 1, s.messageCount, s.totalTokens})
		}
	}
	return projectList(byName)
}

// Projects answers GET /api/stats/projects?period=.
func (rt *Route) Projects(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(func(r *http.Request) string { return "projects:" + string(periodOf(r)) }, func(r *http.Request) (any, error) {
		p := periodOf(r)
		byName := newOmap[*projectStats]()
		for _, pr := range append(rt.claudeProjectsFor(p), rt.codexProjectsFor(p)...) {
			if e, ok := byName.get(pr.project); ok {
				e.sessions += pr.sessions
				e.messages += pr.messages
				e.total += pr.total
			} else {
				c := pr
				byName.set(pr.project, &c)
			}
		}
		list := projectList(byName)
		items := make([]any, len(list))
		for i, pr := range list {
			items[i] = pr.value()
		}
		return jsvalue.NewObject("projects", items, "totalProjects", float64(len(list))), nil
	})(r)
}

// Sessions answers GET /api/stats/sessions?period=.
func (rt *Route) Sessions(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(func(r *http.Request) string { return "sessions:" + string(periodOf(r)) }, func(r *http.Request) (any, error) {
		p := periodOf(r)
		list := append(rt.claudeSessionStats(p), rt.codexSessionsFor(p)...)
		col := newCollator()
		sortStable(list, func(a, b sessionStats) int { return col.CompareString(b.startedAt, a.startedAt) })
		duration := func(s sessionStats) float64 {
			return jscompat.ParseDate(s.lastActivityAt) - jscompat.ParseDate(s.startedAt)
		}
		var durations []float64
		for _, s := range list {
			if d := duration(s); d > 0 {
				durations = append(durations, d)
			}
		}
		avg := 0.0
		if len(durations) > 0 {
			sum := 0.0
			for _, d := range durations {
				sum += d
			}
			avg = jsRound(sum / float64(len(durations)))
		}
		var longest any
		best := math.Inf(-1)
		for _, s := range list {
			d := duration(s)
			if d <= 0 {
				continue
			}
			if longest == nil || d > best {
				best = d
				longest = jsvalue.NewObject("sessionId", plainValue(s.sessionID), "duration", d, "messageCount", s.messageCount,
					"timestamp", s.startedAt)
			}
		}
		items := make([]any, len(list))
		for i, s := range list {
			items[i] = s.value()
		}
		return jsvalue.NewObject("sessions", items, "averageDurationMs", avg, "longestSession", longest,
			"totalSessions", float64(len(list))), nil
	})(r)
}

// Aggregated answers GET /api/stats/aggregated?period=.
func (rt *Route) Aggregated(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(func(r *http.Request) string { return "aggregated:" + string(periodOf(r)) }, func(r *http.Request) (any, error) {
		p := periodOf(r)
		claude := rt.claudeSessionStats(p)
		codex := rt.codexJSONL(p)
		type dayRow struct{ tokens, sessions float64 }
		claudeDays := newOmap[*dayRow]()
		totalTokens, withCached := 0.0, 0.0
		for _, s := range claude {
			ms := jscompat.ParseDate(s.startedAt)
			if math.IsNaN(ms) || math.IsInf(ms, 0) {
				continue
			}
			date := localDate(ms)
			d, ok := claudeDays.get(date)
			if !ok {
				d = &dayRow{}
				claudeDays.set(date, d)
			}
			d.tokens += s.totalTokens
			d.sessions++
			totalTokens += s.totalTokens
			withCached += s.withCached
		}
		codexDays := newOmap[*dayRow]()
		for _, d := range codex.daily {
			codexDays.set(d.date, &dayRow{d.tokens, d.sessions})
		}
		dates := map[string]bool{}
		var all []string
		for _, k := range append(append([]string{}, claudeDays.keys...), codexDays.keys...) {
			if !dates[k] {
				dates[k] = true
				all = append(all, k)
			}
		}
		sort.Slice(all, func(i, j int) bool { return lessJS(all[i], all[j]) })
		daily := make([]any, len(all))
		for i, date := range all {
			c, x := &dayRow{}, &dayRow{}
			if v, ok := claudeDays.get(date); ok {
				c = v
			}
			if v, ok := codexDays.get(date); ok {
				x = v
			}
			daily[i] = jsvalue.NewObject("date", date, "claudeTokens", c.tokens, "codexTokens", x.tokens, "claudeSessions", c.sessions,
				"codexSessions", x.sessions)
		}
		type modelRow struct {
			provider string
			model    *string
			tokens   float64
			sessions float64
		}
		rows := newOmap[*modelRow]()
		addRow := func(provider string, model *string, tokens float64) {
			key := provider + ":"
			if model != nil {
				key += *model
			}
			if e, ok := rows.get(key); ok {
				e.tokens += tokens
				e.sessions++
				return
			}
			rows.set(key, &modelRow{provider, model, tokens, 1})
		}
		for _, s := range claude {
			var model *string
			if s.model != "" {
				m := s.model
				model = &m
			}
			addRow("claude", model, s.totalTokens)
		}
		for _, s := range codex.sessions {
			addRow("codex", s.model, s.totalTokens)
		}
		var breakdown []*modelRow
		for _, k := range rows.keys {
			if rows.m[k].tokens > 0 {
				breakdown = append(breakdown, rows.m[k])
			}
		}
		sortStable(breakdown, func(a, b *modelRow) int { return cmpNum(b.tokens - a.tokens) })
		models := make([]any, len(breakdown))
		for i, b := range breakdown {
			var model any
			if b.model != nil {
				model = *b.model
			}
			models[i] = jsvalue.NewObject("provider", b.provider, "model", model, "tokens", b.tokens, "sessions", b.sessions)
		}
		var extras any
		if codex.extras != nil {
			extras = codex.extras.value()
		}
		return jsvalue.NewObject("period", string(p), "daily", daily,
			"totals", jsvalue.NewObject(
				"claude", jsvalue.NewObject("tokens", totalTokens, "tokensWithCached", withCached, "sessions", float64(len(claude))),
				"codex", jsvalue.NewObject("tokens", codex.tokens, "tokensWithCached", codex.withCach, "sessions", float64(len(codex.sessions)),
					"cachedInputTokens", codex.cached)),
			"modelBreakdown", models, "codexExtras", extras, "errors", []any{}, "computedAt", nowISO(rt.now())), nil
	})(r)
}

func optNum(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (e *codexExtras) value() *jsvalue.Object {
	var limits any
	if e.rateLimits {
		o := jsvalue.NewObject()
		if e.primary != nil {
			o.Set("primary", e.primary.value())
		}
		if e.secondary != nil {
			o.Set("secondary", e.secondary.value())
		}
		limits = o
	}
	return jsvalue.NewObject("rateLimits", limits, "modelContextWindow", optNum(e.modelContextWindow), "cachedInputTokens",
		optNum(e.cachedInputTokens), "reasoningOutputTokens", optNum(e.reasoningOutputToks), "capturedAt", e.capturedAt)
}

func (b *rateBucket) value() *jsvalue.Object {
	o := jsvalue.NewObject("usedPercent", b.usedPercent)
	if b.hasWindow {
		o.Set("windowMinutes", plainValue(b.windowMinutes))
	}
	if b.hasResetsIn {
		o.Set("resetsInSeconds", b.resetsIn)
	}
	return o
}

// plainValue turns a value decoded from a line into one jsvalue can write (an object's keys in name order: the line's
// own order is not kept).
func plainValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		o := jsvalue.NewObject()
		for _, k := range keys {
			o.Set(k, plainValue(x[k]))
		}
		return o
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plainValue(e)
		}
		return out
	}
	if n, ok := isNumber(v); ok {
		return n
	}
	return v
}

// CacheStatus answers GET /api/stats/cache-status.
func (rt *Route) CacheStatus(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(nil, func(r *http.Request) (any, error) {
		count := 0
		if dirs, err := os.ReadDir(rt.claudeProjects()); err == nil {
			for _, d := range dirs {
				dir := filepath.Join(rt.claudeProjects(), d.Name())
				if st, err := os.Stat(dir); err != nil || !st.IsDir() {
					continue
				}
				names, _ := os.ReadDir(dir)
				for _, n := range names {
					if strings.HasSuffix(n.Name(), ".jsonl") && !strings.HasPrefix(n.Name(), "agent-") {
						count++
					}
				}
			}
		}
		count += len(allCodexFiles(rt.codexSessions()))
		return jsvalue.NewObject("exists", fileExists(rt.statsCachePath()), "fileCount", float64(count)), nil
	})(r)
}
