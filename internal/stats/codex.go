package stats

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"harnessly/internal/jscompat"
	"harnessly/internal/pricing"
)

// Codex sessions (sessions/YYYY/MM/DD/*.jsonl): a session's totals come from its first line and the last megabyte, its
// activity (prompts, last time) from the whole file. Both are kept per version of a file.

const codexTailBytes = 1024 * 1024

type rateBucket struct {
	usedPercent   float64
	windowMinutes any // as the line had it; absent leaves the key out
	hasWindow     bool
	resetsIn      float64
	hasResetsIn   bool
}

type codexExtras struct {
	rateLimits          bool // false: null
	primary, secondary  *rateBucket
	modelContextWindow  *float64
	cachedInputTokens   *float64
	reasoningOutputToks *float64
	capturedAt          float64
}

type codexSession struct {
	sessionID      any
	jsonlPath      string
	startedAt      float64
	cwd            any
	model          *string
	effort         *string
	inputTokens    float64
	cachedInput    float64
	outputTokens   float64
	reasoning      float64
	totalTokens    float64
	currentContext float64
	contextWindow  float64
	cost           *float64
	extras         *codexExtras
}

type codexMessage struct {
	timestamp float64
	text      string
}

type codexActivity struct {
	sessionID      any
	cwd            any
	startedAt      float64
	lastActivityAt float64
	messageCount   float64
	messages       []codexMessage
}

type codexLineJSON struct {
	Timestamp any             `json:"timestamp"`
	Type      any             `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	ID         any `json:"id"`
	Timestamp  any `json:"timestamp"`
	Cwd        any `json:"cwd"`
	Type       any `json:"type"`
	Message    any `json:"message"`
	Model      any `json:"model"`
	Effort     any `json:"effort"`
	Info       any `json:"info"`
	RateLimits any `json:"rate_limits"`
}

// payloadOf is a line's payload when it is truthy; obj says whether it is an object (whose fields are read).
func payloadOf(raw json.RawMessage) (p codexPayload, truthyP, obj bool) {
	if !rawTruthy(raw) {
		return p, false, false
	}
	raw = bytes.TrimSpace(raw)
	if raw[0] != '{' {
		return p, true, false
	}
	return p, true, scanPayload(raw, &p)
}

// dateParse is Date.parse(v) for a value of a line (a string as such, anything else as its text).
func dateParse(v any) float64 {
	switch x := v.(type) {
	case string:
		return jscompat.ParseDate(x)
	case nil:
		return math.NaN()
	}
	return jscompat.ParseDate(jsStr(v))
}

func readCodexMeta(path string) (sessionID any, startedAt float64, cwd any, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, err
	}
	defer file.Close()
	lr := newLineReader(file)
	for {
		line, ok := lr.next()
		if !ok {
			if lr.readErr != nil {
				return nil, 0, nil, lr.readErr
			}
			return nil, 0, nil, nil
		}
		trimmed := bytes.TrimFunc(line, jscompat.IsSpace)
		if len(trimmed) == 0 {
			continue
		}
		var l codexLineJSON
		if !scanCodexLine(trimmed, &l) {
			return nil, 0, nil, nil
		}
		p, ok, obj := payloadOf(l.Payload)
		if l.Type != "session_meta" || !ok {
			return nil, 0, nil, nil
		}
		if !obj {
			p = codexPayload{}
		}
		switch {
		case truthy(p.Timestamp):
			startedAt = dateParse(p.Timestamp)
		case truthy(l.Timestamp):
			startedAt = dateParse(l.Timestamp)
		}
		return p.ID, startedAt, p.Cwd, nil
	}
}

func readTail(path string, size int64) ([][]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	start := max(0, size-codexTailBytes)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	lines := splitLines(data)
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	return lines, nil
}

type tokenRecord struct {
	info       any
	rateLimits any
}

func (rt *Route) parseCodexFile(path string, st os.FileInfo) (*codexSession, error) {
	sessionID, startedAt, cwd, err := readCodexMeta(path)
	if err != nil {
		return nil, err
	}
	if !truthy(sessionID) || startedAt == 0 || math.IsNaN(startedAt) {
		return nil, nil
	}
	lines, err := readTail(path, st.Size())
	if err != nil {
		return nil, err
	}
	var rec *tokenRecord
	var model, effort *string
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) == 0 {
			continue
		}
		var l codexLineJSON
		if !scanCodexLine(decodeText(lines[i]), &l) {
			continue
		}
		p, ok, obj := payloadOf(l.Payload)
		if !ok {
			continue
		}
		if !obj {
			p = codexPayload{}
		}
		if model == nil && l.Type == "turn_context" {
			if m, isStr := p.Model.(string); isStr {
				model = &m
				if e, isStr := p.Effort.(string); isStr {
					effort = &e
				} else {
					effort = nil
				}
			}
		}
		if rec == nil && l.Type == "event_msg" && p.Type == "token_count" && truthy(p.Info) {
			rec = &tokenRecord{info: p.Info, rateLimits: p.RateLimits}
		}
		if rec != nil && model != nil {
			break
		}
	}
	s := &codexSession{sessionID: sessionID, jsonlPath: path, startedAt: startedAt, cwd: cwd, model: model, effort: effort}
	var info any
	if rec != nil {
		info = rec.info
	}
	usage := prop(info, "total_token_usage")
	if usage == nil {
		usage = prop(info, "last_token_usage")
	}
	s.inputTokens = numOr(prop(usage, "input_tokens"))
	s.cachedInput = numOr(prop(usage, "cached_input_tokens"))
	s.outputTokens = numOr(prop(usage, "output_tokens"))
	s.reasoning = numOr(prop(usage, "reasoning_output_tokens"))
	rawTotal := prop(prop(info, "total_token_usage"), "total_tokens")
	if rawTotal == nil {
		rawTotal = prop(prop(info, "last_token_usage"), "total_tokens")
	}
	s.totalTokens = jsMax0(s.inputTokens-s.cachedInput) + s.outputTokens
	cur := prop(prop(info, "last_token_usage"), "total_tokens")
	if cur == nil {
		cur = rawTotal
	}
	s.currentContext = numOr(cur)
	s.contextWindow = numOr(prop(info, "model_context_window"))
	mtimeMs := jscompat.TimeMs(st.ModTime())
	if rec != nil {
		s.extras = buildExtras(rec, mtimeMs)
	}
	if model != nil && *model != "" {
		if c, ok := pricing.OpenAICostInWindow(*model, s.inputTokens, s.cachedInput, s.outputTokens, s.contextWindow); ok {
			s.cost = &c
		}
	}
	return s, nil
}

func finite(v any) *float64 {
	f, ok := v.(float64)
	if n, isNum := v.(json.Number); isNum {
		f, ok = numberText(string(n)), true
	}
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

func isNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		return numberText(string(x)), true
	}
	return 0, false
}

func buildExtras(rec *tokenRecord, capturedAt float64) *codexExtras {
	e := &codexExtras{capturedAt: capturedAt}
	if truthy(rec.rateLimits) {
		e.rateLimits = true
		e.primary = rateBucketOf(prop(rec.rateLimits, "primary"), capturedAt)
		e.secondary = rateBucketOf(prop(rec.rateLimits, "secondary"), capturedAt)
	}
	e.modelContextWindow = finite(prop(rec.info, "model_context_window"))
	e.cachedInputTokens = finite(prop(prop(rec.info, "total_token_usage"), "cached_input_tokens"))
	e.reasoningOutputToks = finite(prop(prop(rec.info, "total_token_usage"), "reasoning_output_tokens"))
	return e
}

func rateBucketOf(w any, capturedAt float64) *rateBucket {
	if !truthy(w) {
		return nil
	}
	used, ok := isNumber(prop(w, "used_percent"))
	if !ok {
		return nil
	}
	b := &rateBucket{usedPercent: used}
	if v := prop(w, "window_minutes"); v != nil || hasKey(w, "window_minutes") {
		b.windowMinutes, b.hasWindow = v, true
	}
	if n, ok := isNumber(prop(w, "resets_in_seconds")); ok {
		b.resetsIn, b.hasResetsIn = n, true
	} else if n, ok := isNumber(prop(w, "resets_at")); ok {
		b.resetsIn, b.hasResetsIn = jsMax0(jsRound(n-capturedAt/1000)), true
	}
	return b
}

func hasKey(v any, k string) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, has := m[k]
	return has
}

func readCodexActivity(path string) *codexActivity {
	a := &codexActivity{}
	file, err := os.Open(path)
	if err == nil {
		lr := newLineReader(file)
		for {
			line, ok := lr.next()
			if !ok {
				break
			}
			trimmed := bytes.TrimFunc(line, jscompat.IsSpace)
			if len(trimmed) == 0 {
				continue
			}
			var l codexLineJSON
			if !scanCodexLine(trimmed, &l) {
				continue
			}
			tsMs := 0.0
			if truthy(l.Timestamp) {
				tsMs = dateParse(l.Timestamp)
			}
			if tsMs != 0 && !math.IsNaN(tsMs) && tsMs > a.lastActivityAt {
				a.lastActivityAt = tsMs
			}
			p, ok, obj := payloadOf(l.Payload)
			if !obj {
				p = codexPayload{}
			}
			if l.Type == "session_meta" && ok {
				if p.ID != nil {
					a.sessionID = p.ID
				}
				if p.Cwd != nil {
					a.cwd = p.Cwd
				}
				startedAt := tsMs
				if truthy(p.Timestamp) {
					startedAt = dateParse(p.Timestamp)
				}
				if startedAt != 0 && !math.IsNaN(startedAt) {
					a.startedAt = startedAt
				}
				continue
			}
			if l.Type == "event_msg" && p.Type == "user_message" {
				text, _ := p.Message.(string)
				ts := tsMs
				if ts == 0 || math.IsNaN(ts) {
					ts = a.startedAt
				}
				if text != "" && ts != 0 && !math.IsNaN(ts) {
					a.messageCount++
					a.messages = append(a.messages, codexMessage{ts, text})
				}
			}
		}
		file.Close()
	}
	if a.startedAt == 0 && len(a.messages) > 0 {
		a.startedAt = a.messages[0].timestamp
	}
	if a.lastActivityAt == 0 {
		if n := len(a.messages); n > 0 {
			a.lastActivityAt = a.messages[n-1].timestamp
		} else {
			a.lastActivityAt = a.startedAt
		}
	}
	return a
}

type codexCached struct {
	version  fileVersion
	session  *codexSession
	activity *codexActivity
}

// codexStore keeps each session file's totals and activity while the file stays as it was read.
type codexStore struct {
	mu    sync.Mutex
	stats map[string]*codexCached
	acts  map[string]*codexCached
}

func (rt *Route) codexStats(path string) *codexSession {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	v := versionOf(st)
	s := &rt.codex
	s.mu.Lock()
	if c := s.stats[path]; c != nil && c.version == v {
		s.mu.Unlock()
		return c.session
	}
	s.mu.Unlock()
	session, err := rt.parseCodexFile(path, st)
	if err != nil {
		session = nil
	}
	s.mu.Lock()
	if s.stats == nil {
		s.stats = map[string]*codexCached{}
	}
	s.stats[path] = &codexCached{version: v, session: session}
	s.mu.Unlock()
	return session
}

func (rt *Route) codexActivity(path string) *codexActivity {
	st, err := os.Stat(path)
	if err != nil {
		return readCodexActivity(path)
	}
	v := versionOf(st)
	s := &rt.codex
	s.mu.Lock()
	if c := s.acts[path]; c != nil && c.version == v {
		s.mu.Unlock()
		return c.activity
	}
	s.mu.Unlock()
	a := readCodexActivity(path)
	s.mu.Lock()
	if s.acts == nil {
		s.acts = map[string]*codexCached{}
	}
	s.acts[path] = &codexCached{version: v, activity: a}
	s.mu.Unlock()
	return a
}

// allCodexFiles walks the sessions folder as the old runtime did: names in order, links followed, every .jsonl file.
func allCodexFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			out = append(out, allCodexFiles(p)...)
		} else if st.Mode().IsRegular() && strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, p)
		}
	}
	return out
}

func (rt *Route) codexDayFiles(dates []string) []string {
	var out []string
	for _, date := range dates {
		if len(date) != 10 || date[4] != '-' || date[7] != '-' {
			continue
		}
		y, m, d := atoiDigits(date[:4]), atoiDigits(date[5:7]), atoiDigits(date[8:])
		if y <= 0 || m <= 0 || d <= 0 {
			continue
		}
		t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Local)
		yy, mm, dd := t.Date()
		dir := filepath.Join(rt.codexSessions(), jscompat.Number(float64(yy)), pad2(int(mm)), pad2(dd))
		names, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, n := range names {
			if strings.HasSuffix(n.Name(), ".jsonl") {
				out = append(out, filepath.Join(dir, n.Name()))
			}
		}
	}
	return out
}

func atoiDigits(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

func (rt *Route) codexFilesFor(p period) []string {
	if p == periodAll {
		return allCodexFiles(rt.codexSessions())
	}
	back := map[period]int{periodToday: 1, period7d: 7, period30d: 30}[p]
	now := rt.now()
	dates := make([]string, 0, back)
	for i := 0; i < back; i++ {
		dates = append(dates, localDay(addDays(now, -i)))
	}
	return rt.codexDayFiles(dates)
}

func localDate(ms float64) string { return localDay(msTime(ms)) }

type codexProvider struct {
	daily    []codexDay
	tokens   float64
	withCach float64
	cached   float64
	extras   *codexExtras
	sessions []*codexSession
}

type codexDay struct {
	date             string
	tokens, sessions float64
}

func (rt *Route) codexJSONL(p period) *codexProvider {
	out := &codexProvider{}
	paths := rt.codexFilesFor(p)
	byDay := newOmap[*codexDay]()
	for _, path := range paths {
		s := rt.codexStats(path)
		if s == nil || !rt.within(s.startedAt, p) {
			continue
		}
		out.sessions = append(out.sessions, s)
	}
	for _, s := range out.sessions {
		date := localDate(s.startedAt)
		d, ok := byDay.get(date)
		if !ok {
			d = &codexDay{date: date}
			byDay.set(date, d)
		}
		d.tokens += s.totalTokens
		d.sessions++
		out.tokens += s.totalTokens
		out.cached += s.cachedInput
		out.withCach += s.totalTokens + s.cachedInput
	}
	for _, k := range byDay.ordered() {
		out.daily = append(out.daily, *byDay.m[k])
	}
	col := newCollator()
	sortStable(out.daily, func(a, b codexDay) int { return col.CompareString(a.date, b.date) })
	for _, s := range out.sessions {
		if s.extras != nil && (out.extras == nil || s.extras.capturedAt > out.extras.capturedAt) {
			out.extras = s.extras
		}
	}
	return out
}

// sessionStats is the shape both providers' sessions take in the sessions and projects lists.
type sessionStats struct {
	sessionID      any
	project        string
	startedAt      string
	lastActivityAt string
	messageCount   float64
	totalTokens    float64
	withCached     float64
	model          string
}

func (rt *Route) codexSessionsFor(p period) []sessionStats {
	var out []sessionStats
	for _, path := range rt.codexFilesFor(p) {
		s := rt.codexStats(path)
		a := rt.codexActivity(path)
		if s == nil || !rt.within(s.startedAt, p) {
			continue
		}
		startedAt := s.startedAt
		if startedAt == 0 || math.IsNaN(startedAt) {
			startedAt = a.startedAt
		}
		last := math.Max(a.lastActivityAt, startedAt)
		if math.IsNaN(a.lastActivityAt) || math.IsNaN(startedAt) {
			last = math.NaN()
		}
		project := ""
		if truthy(s.cwd) {
			project = rt.shortenCwd(jsStr(s.cwd))
		} else if truthy(a.cwd) {
			project = rt.shortenCwd(jsStr(a.cwd))
		}
		started, _ := jscompat.ISOString(startedAt)
		lastISO, _ := jscompat.ISOString(last)
		model := ""
		if s.model != nil {
			model = *s.model
		}
		out = append(out, sessionStats{s.sessionID, project, started, lastISO, a.messageCount, s.totalTokens, s.totalTokens + s.cachedInput, model})
	}
	col := newCollator()
	sortStable(out, func(a, b sessionStats) int { return col.CompareString(b.startedAt, a.startedAt) })
	return out
}

func (rt *Route) codexHistory(p period) []codexMessage {
	var out []codexMessage
	for _, path := range rt.codexFilesFor(p) {
		for _, m := range rt.codexActivity(path).messages {
			if rt.within(m.timestamp, p) {
				out = append(out, m)
			}
		}
	}
	sortStable(out, func(a, b codexMessage) int { return cmpNum(b.timestamp - a.timestamp) })
	return out
}

func cmpNum(d float64) int {
	switch {
	case d < 0:
		return -1
	case d > 0:
		return 1
	}
	return 0
}
