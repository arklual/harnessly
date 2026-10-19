// Package stats answers the usage statistics pages (/api/stats/*) from this machine's Claude Code and Codex files, as
// the previous runtime's handlers did: the same answers, the same cache files under the data folder, each transcript
// read again only once it changes.
package stats

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/nodejson"
)

// Route answers the routes under /api/stats/.
type Route struct {
	// Home is the person's home: the old handlers read ~/.claude and ~/.codex there whatever CLAUDE_CONFIG_DIR or
	// CODEX_HOME said, and shortened project folders under it.
	Home string
	// ClaudeDir and CodexDir default to Home/.claude and Home/.codex.
	ClaudeDir, CodexDir string
	// DataDir holds stats/cache.json, stats/uptime-cache.json and stats/daily-reports.
	DataDir string
	// ConfigFile is the runtime's config.json (noteSummaryProvider picks the daily report's CLI).
	ConfigFile string
	// Authorized is the old dispatcher's check: a page session or the cli-token.
	Authorized func(*http.Request) bool
	// CwdRewrites shorten a project folder (relative to Home) after the home is cut: each prefix in order, applied to
	// what the previous left. The old runtime's own are DefaultCwdRewrites.
	CwdRewrites []Rewrite
	// ShellPath is the PATH the daily report's CLI runs with (the person's login shell's); nil is this process's.
	ShellPath func() string
	// CLITimeout bounds the daily report's CLI run (120 s when 0).
	CLITimeout time.Duration
	// Now is the clock (tests freeze it); nil is the real one.
	Now func() time.Time

	claude claudeStore
	codex  codexStore

	statsMu        sync.Mutex
	statsMemo      *statsCache
	statsMemoUntil time.Time

	memoMu sync.Mutex
	memo   map[string]memoEntry

	historyMu    sync.Mutex
	historyCache *historyFile
}

// Rewrite replaces a leading Prefix of a project folder with To.
type Rewrite struct{ Prefix, To string }

// DefaultCwdRewrites are the old runtime's: GitHub checkouts under Workspace/ lose the host, Documents/ becomes docs/.
var DefaultCwdRewrites = []Rewrite{{"Workspace/github.com/", ""}, {"Documents/", "docs/"}}

func (rt *Route) now() time.Time {
	if rt.Now != nil {
		return rt.Now()
	}
	return time.Now()
}

func (rt *Route) claudeDir() string {
	if rt.ClaudeDir != "" {
		return rt.ClaudeDir
	}
	return filepath.Join(rt.Home, ".claude")
}

func (rt *Route) codexDir() string {
	if rt.CodexDir != "" {
		return rt.CodexDir
	}
	return filepath.Join(rt.Home, ".codex")
}

func (rt *Route) claudeProjects() string { return filepath.Join(rt.claudeDir(), "projects") }
func (rt *Route) codexSessions() string  { return filepath.Join(rt.codexDir(), "sessions") }

// shortenCwd is a project's name from its folder.
func (rt *Route) shortenCwd(cwd string) string {
	rel := cwd
	if strings.HasPrefix(cwd, rt.Home) {
		rel = slice16(cwd, jscompat.Len(rt.Home)+1, math.MaxInt32)
	}
	for _, r := range rt.CwdRewrites {
		if strings.HasPrefix(rel, r.Prefix) {
			rel = r.To + rel[len(r.Prefix):]
		}
	}
	return rel
}

type period string

const (
	periodToday period = "today"
	period7d    period = "7d"
	period30d   period = "30d"
	periodAll   period = "all"
)

// periodOf is parsePeriod(req.query.period).
func periodOf(r *http.Request) period {
	v := jscompat.Query(r.URL.RawQuery)["period"]
	if len(v) == 1 {
		switch p := period(v[0]); p {
		case periodToday, period7d, period30d, periodAll:
			return p
		}
	}
	return periodAll
}

// periodStart is getPeriodRange(period).start.
func (rt *Route) periodStart(p period) time.Time {
	now := rt.now()
	switch p {
	case periodToday:
		return startOfDay(now)
	case period7d:
		return startOfDay(addDays(now, -7))
	case period30d:
		return startOfDay(addDays(now, -30))
	}
	return time.UnixMilli(0)
}

// clipMs is the time of new Date(ms).
func clipMs(ms float64) float64 {
	if math.IsNaN(ms) || math.Abs(ms) > 8.64e15 {
		return math.NaN()
	}
	return math.Trunc(ms)
}

// within is isWithinPeriod for a time in milliseconds.
func (rt *Route) within(ms float64, p period) bool {
	if p == periodAll {
		return true
	}
	ms = clipMs(ms)
	return ms >= float64(rt.periodStart(p).UnixMilli()) && ms <= float64(rt.now().UnixMilli())
}

// withinDate is isDateStringWithinPeriod.
func (rt *Route) withinDate(date string, p period) bool {
	if p == periodAll {
		return true
	}
	return rt.within(dayjsParse(date), p)
}

const memoTTL = 60 * time.Second

type memoEntry struct {
	body    []byte
	expires time.Time
}

func (rt *Route) memoGet(key string) ([]byte, bool) {
	rt.memoMu.Lock()
	defer rt.memoMu.Unlock()
	e, ok := rt.memo[key]
	if !ok {
		return nil, false
	}
	if rt.now().After(e.expires) {
		delete(rt.memo, key)
		return nil, false
	}
	return e.body, true
}

func (rt *Route) memoSet(key string, body []byte) {
	rt.memoMu.Lock()
	defer rt.memoMu.Unlock()
	if rt.memo == nil {
		rt.memo = map[string]memoEntry{}
	}
	rt.memo[key] = memoEntry{body, rt.now().Add(memoTTL)}
}

var (
	unauthorized = gate.Raw{ContentType: "application/json", Body: []byte(`{"error":"Unauthorized"}`)}
	serverError  = gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}
)

func jsonAnswer(r *http.Request, status int, body []byte) (int, any, http.Header) {
	return nodejson.Answer(r, status, json.RawMessage(body), nil)
}

func errorAnswer(r *http.Request, status int, code string) (int, any, http.Header) {
	return jsonAnswer(r, status, jsvalue.Stringify(jsvalue.NewObject("error", code), ""))
}

// getRoute wraps a cached GET answer: the dispatcher's 401, the handler's 405, then the answer for 60 s per key.
func (rt *Route) getRoute(key func(*http.Request) string, compute func(*http.Request) (any, error)) func(*http.Request) (int, any, http.Header) {
	return func(r *http.Request) (int, any, http.Header) {
		if !rt.Authorized(r) {
			return http.StatusUnauthorized, unauthorized, nil
		}
		if r.Method != http.MethodGet {
			status, v, h := errorAnswer(r, http.StatusMethodNotAllowed, "method-not-allowed")
			h.Set("Allow", "GET")
			return status, v, h
		}
		k := ""
		if key != nil {
			k = key(r)
			if body, ok := rt.memoGet(k); ok {
				return jsonAnswer(r, http.StatusOK, body)
			}
		}
		v, err := compute(r)
		if err != nil {
			return http.StatusInternalServerError, serverError, nil
		}
		body := jsvalue.Stringify(v, "")
		if key != nil {
			rt.memoSet(k, body)
		}
		return jsonAnswer(r, http.StatusOK, body)
	}
}

// Routes are the answers by the path after /api/stats/. All are idempotent GETs except "daily-report/generate", a POST
// that runs a CLI and writes the report. daily-report/cache is not here: nothing calls it.
func (rt *Route) Routes() map[string]func(*http.Request) (int, any, http.Header) {
	return map[string]func(*http.Request) (int, any, http.Header){
		"aggregated":            rt.Aggregated,
		"cache-status":          rt.CacheStatus,
		"facets":                rt.Facets,
		"history":               rt.History,
		"overview":              rt.Overview,
		"projects":              rt.Projects,
		"sessions":              rt.Sessions,
		"uptime":                rt.Uptime,
		"daily-report/list":     rt.DailyReportList,
		"daily-report/generate": rt.DailyReportGenerate,
	}
}

// Shadowable are the routes of Routes that only read (a shadow may run them next to the old runtime).
var Shadowable = []string{"aggregated", "cache-status", "facets", "history", "overview", "projects", "sessions", "uptime", "daily-report/list"}

func sortStable[T any](s []T, cmp func(a, b T) int) {
	sort.SliceStable(s, func(i, j int) bool { return cmp(s[i], s[j]) < 0 })
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ParseCwdRewrites reads extra rewrites as "prefix=to" pairs separated by commas (an environment value), after the
// defaults.
func ParseCwdRewrites(s string) []Rewrite {
	out := append([]Rewrite(nil), DefaultCwdRewrites...)
	for _, pair := range strings.Split(s, ",") {
		if prefix, to, ok := strings.Cut(pair, "="); ok && prefix != "" {
			out = append(out, Rewrite{prefix, to})
		}
	}
	return out
}
