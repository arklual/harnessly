package stats

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

type historyEntry struct {
	timestamp float64
	display   string
}

type historyFile struct {
	version fileVersion
	entries []historyEntry
}

type historyLine struct {
	Display   any `json:"display"`
	Timestamp any `json:"timestamp"`
}

// claudeHistory is ~/.claude/history.jsonl's prompts with a time, kept while the file stays as it was read.
func (rt *Route) claudeHistory() []historyEntry {
	path := filepath.Join(rt.claudeDir(), "history.jsonl")
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	v := versionOf(st)
	rt.historyMu.Lock()
	defer rt.historyMu.Unlock()
	if rt.historyCache != nil && rt.historyCache.version == v {
		return rt.historyCache.entries
	}
	var entries []historyEntry
	if file, err := os.Open(path); err == nil {
		lr := newLineReader(file)
		for {
			line, ok := lr.next()
			if !ok {
				break
			}
			if len(strings.TrimFunc(string(line), jscompat.IsSpace)) == 0 {
				continue
			}
			var h historyLine
			if !decodeLine(line, &h) {
				continue
			}
			ts := toNum(h.Timestamp)
			if ts == 0 || math.IsNaN(ts) {
				continue
			}
			entries = append(entries, historyEntry{ts, jsStr(h.Display)})
		}
		file.Close()
	}
	rt.historyCache = &historyFile{version: v, entries: entries}
	return entries
}

var lengthBuckets = []struct {
	label string
	max   float64
}{{"≤50", 50}, {"51–200", 200}, {"201–500", 500}, {"501–1000", 1000}, {"1000+", math.Inf(1)}}

var spaceRun = regexp.MustCompile(jscompat.SpaceClass)

// History answers GET /api/stats/history?period=&limit=.
func (rt *Route) History(r *http.Request) (int, any, http.Header) {
	limitOf := func(r *http.Request) float64 {
		v := jscompat.Query(r.URL.RawQuery)["limit"]
		n := math.NaN()
		if len(v) > 0 {
			n = jscompat.ParseInt(strings.Join(v, ","), 10)
		}
		if n == 0 || math.IsNaN(n) {
			n = 10
		}
		return math.Max(1, n)
	}
	return rt.getRoute(func(r *http.Request) string {
		return "history:" + string(periodOf(r)) + ":" + jscompat.Number(limitOf(r))
	}, func(r *http.Request) (any, error) {
		p := periodOf(r)
		limit := limitOf(r)
		commands := newOmap[float64]()
		var commandOrder []string
		lengths := make([]float64, len(lengthBuckets))
		hours := newOmap[float64]()
		total := 0.0
		addEntry := func(display string, ts float64) {
			total++
			if t := jscompat.Trim(display); strings.HasPrefix(t, "/") {
				cmd := t
				if loc := spaceRun.FindStringIndex(t); loc != nil {
					cmd = t[:loc[0]]
				}
				c, ok := commands.get(cmd)
				if !ok {
					commandOrder = append(commandOrder, cmd)
				}
				commands.set(cmd, c+1)
			}
			n := float64(jscompat.Len(display))
			for i, b := range lengthBuckets {
				if n <= b.max {
					lengths[i]++
					break
				}
			}
			hour := "NaN"
			if ms := clipMs(ts); !math.IsNaN(ms) {
				hour = jscompat.Number(float64(msTime(ms).Hour()))
			}
			c, _ := hours.get(hour)
			hours.set(hour, c+1)
		}
		for _, e := range rt.claudeHistory() {
			if rt.within(e.timestamp, p) {
				addEntry(e.display, e.timestamp)
			}
		}
		for _, e := range rt.codexHistory(p) {
			addEntry(e.text, e.timestamp)
		}
		type cmdCount struct {
			command string
			count   float64
		}
		list := make([]cmdCount, len(commandOrder))
		for i, c := range commandOrder {
			list[i] = cmdCount{c, commands.m[c]}
		}
		sortStable(list, func(a, b cmdCount) int { return cmpNum(b.count - a.count) })
		if float64(len(list)) > limit {
			list = list[:int(limit)]
		}
		top := make([]any, len(list))
		for i, c := range list {
			top[i] = jsvalue.NewObject("command", c.command, "count", c.count)
		}
		dist := make([]any, len(lengthBuckets))
		for i, b := range lengthBuckets {
			dist[i] = jsvalue.NewObject("bucket", b.label, "count", lengths[i])
		}
		return jsvalue.NewObject("topCommands", top, "inputLengthDistribution", dist, "hourlyPattern", countsValue(hours),
			"totalEntries", total), nil
	})(r)
}

// Facets answers GET /api/stats/facets?period=.
func (rt *Route) Facets(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(func(r *http.Request) string { return "facets:" + string(periodOf(r)) }, func(r *http.Request) (any, error) {
		p := periodOf(r)
		dir := filepath.Join(rt.claudeDir(), "usage-data", "facets")
		facets := []any{}
		categories, outcomes := jsvalue.NewObject(), jsvalue.NewObject()
		names, _ := os.ReadDir(dir)
		for _, n := range names {
			name := n.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			path := filepath.Join(dir, name)
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			st, err := os.Stat(path)
			if err != nil {
				continue
			}
			if !rt.within(jscompat.DateMs(jscompat.TimeMs(st.ModTime())), p) {
				continue
			}
			v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
			if err != nil || v == nil {
				continue
			}
			o, _ := v.(*jsvalue.Object)
			field := func(k string) any {
				if o == nil {
					return nil
				}
				x, _ := o.Get(k)
				return x
			}
			sessionID := jsValueStr(field("session_id"))
			if field("session_id") == nil {
				sessionID = strings.Replace(name, ".json", "", 1)
			}
			goals := numbersOf(field("goal_categories"))
			facet := jsvalue.NewObject("sessionId", sessionID, "outcome", jsValueStr(field("outcome")), "sessionType",
				jsValueStr(field("session_type")), "goalCategories", goals, "satisfaction", numbersOf(field("user_satisfaction_counts")),
				"helpfulness", jsValueStr(field("claude_helpfulness")), "summary", jsValueStr(field("brief_summary")))
			facets = append(facets, facet)
			for _, k := range goals.Keys() {
				c, _ := goals.Get(k)
				prev, ok := categories.Get(k)
				if !ok {
					prev = 0.0
				}
				categories.Set(k, add(prev.(float64), c.(float64)))
			}
			if outcome := jsValueStr(field("outcome")); outcome != "" {
				prev, ok := outcomes.Get(outcome)
				if !ok {
					prev = 0.0
				}
				outcomes.Set(outcome, prev.(float64)+1)
			}
		}
		return jsvalue.NewObject("facets", facets, "categoryDistribution", categories, "outcomeDistribution", outcomes,
			"totalFacets", float64(len(facets))), nil
	})(r)
}

// jsValueStr is String(v ?? ”) for a jsvalue value.
func jsValueStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case *jsvalue.Object:
		return "[object Object]"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = jsValueStr(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return jscompat.String(v)
}

// jsValueNum is Number(v ?? 0) for a jsvalue value.
func jsValueNum(v any) float64 {
	switch x := v.(type) {
	case *jsvalue.Object:
		return math.NaN()
	case []any:
		return jsNumber(jsValueStr(x))
	}
	return toNum(v)
}

// numbersOf is an object of the old runtime's `Object.fromEntries(Object.entries(v).map(([k, v]) => [k, Number(v ?? 0)]))`
// for an object value, {} otherwise.
func numbersOf(v any) *jsvalue.Object {
	out := jsvalue.NewObject()
	switch x := v.(type) {
	case *jsvalue.Object:
		for _, k := range x.Keys() {
			e, _ := x.Get(k)
			out.Set(k, jsValueNum(e))
		}
	case []any:
		for i, e := range x {
			out.Set(jscompat.Number(float64(i)), jsValueNum(e))
		}
	}
	return out
}
