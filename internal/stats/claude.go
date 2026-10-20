package stats

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// The Claude Code transcripts (projects/<folder>/<session>.jsonl) are read once per version of a file (size and
// modification time): one pass gathers what every route asks of it. A route then sums the files' facts instead of
// reading gigabytes again. Where a fact kept for the whole file could differ from what the previous runtime computed
// over a part of it (a message id repeated across days), that file is read again for the request, as it was.

type claudeEntry struct {
	Timestamp any             `json:"timestamp"`
	Type      any             `json:"type"`
	SessionID any             `json:"sessionId"`
	Cwd       any             `json:"cwd"`
	UUID      any             `json:"uuid"`
	Message   json.RawMessage `json:"message"`
}

type claudeMessage struct {
	ID      any             `json:"id"`
	Model   any             `json:"model"`
	Usage   any             `json:"usage"`
	Content json.RawMessage `json:"content"`
}

// prop is v[key] for a decoded value: undefined (nil) unless v is an object.
func prop(v any, key string) any {
	if m, ok := v.(map[string]any); ok {
		return m[key]
	}
	return nil
}

// tokens is a day's usage of one model: input, output, cache read, cache creation (all, 5 minutes, 1 hour).
type tokens [6]float64

type daySession struct {
	start, end string
	messages   float64
}

// dayData is one calendar day (the timestamp's first ten characters) of the stats cache, as the old runtime kept it.
type dayData struct {
	messageCount, toolCallCount float64
	hours                       *omap[float64]
	models                      *omap[*tokens]
	sessions                    *omap[*daySession]
}

func newDay() *dayData {
	return &dayData{hours: newOmap[float64](), models: newOmap[*tokens](), sessions: newOmap[*daySession]()}
}

func (d *dayData) clone() *dayData {
	c := &dayData{messageCount: d.messageCount, toolCallCount: d.toolCallCount, hours: newOmap[float64](), models: newOmap[*tokens](),
		sessions: newOmap[*daySession]()}
	for _, k := range d.hours.keys {
		c.hours.set(k, d.hours.m[k])
	}
	for _, k := range d.models.keys {
		t := *d.models.m[k]
		c.models.set(k, &t)
	}
	for _, k := range d.sessions.keys {
		s := *d.sessions.m[k]
		c.sessions.set(k, &s)
	}
	return c
}

// periodSession is one session's part of a calendar day (local time) for the sessions and projects lists.
type periodSession struct {
	firstLine                  int
	start, end                 string
	messages                   float64
	input, output, read, write float64
	modelLine                  int
	model                      string
}

type bucket struct {
	maxMs    float64
	sessions *omap[*periodSession]
}

const invalidDay = math.MinInt32

// claudeFacts is what one pass over a transcript keeps.
type claudeFacts struct {
	firstDate string
	hasFirst  bool
	cwd       string

	days          *omap[*dayData]
	daysCrossDup  bool
	buckets       map[int]*bucket
	bucketCrossDp bool

	// stamps: user and assistant times per day (first ten characters) and session, for days from stampsFrom on.
	stamps     *omap[*omap[[]float64]]
	stampsFrom string
}

// factsFilter narrows a pass to what one request of the old runtime read: only these days (stats), or only the entries
// of a period (sessions). A filtered pass is not kept.
type factsFilter struct {
	days   map[string]bool
	period func(ts string, ms float64) bool
}

func dayNumber(ms float64) int {
	if math.IsNaN(ms) {
		return invalidDay
	}
	y, m, d := msTime(ms).Date()
	return y*10000 + int(m)*100 + d
}

func readClaudeFacts(path string, stampsFrom string, filter *factsFilter) *claudeFacts {
	f := &claudeFacts{days: newOmap[*dayData](), buckets: map[int]*bucket{}, stamps: newOmap[*omap[[]float64]](), stampsFrom: stampsFrom}
	file, err := os.Open(path)
	if err != nil {
		return f
	}
	defer file.Close()
	dayUsage := map[string]string{}
	bucketUsage := map[string]int{}
	lr := newLineReader(file)
	for line := 0; ; line++ {
		raw, ok := lr.next()
		if !ok {
			break
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var e claudeEntry
		if !scanClaudeEntry(raw, &e) {
			continue
		}
		f.entry(&e, line, filter, dayUsage, bucketUsage)
	}
	return f
}

func (f *claudeFacts) entry(e *claudeEntry, line int, filter *factsFilter, dayUsage map[string]string, bucketUsage map[string]int) {
	ts := jsStr(e.Timestamp)
	if !f.hasFirst && ts != "" {
		f.firstDate, f.hasFirst = slice16(ts, 0, 10), true
	}
	if f.cwd == "" {
		if s, ok := e.Cwd.(string); ok {
			f.cwd = s
		}
	}
	if ts == "" {
		return
	}
	typ := jsStr(e.Type)
	isUser, isAssistant := typ == "user", typ == "assistant"
	if !isUser && !isAssistant {
		return
	}
	sid := jsStr(e.SessionID)
	date := slice16(ts, 0, 10)
	ms := jscompat.ParseDate(ts)
	msgTruthy := rawTruthy(e.Message)
	var msg claudeMessage
	if isAssistant && msgTruthy {
		msg, _ = scanMessage(e.Message)
	}
	usageID := ""
	if isAssistant {
		if id, ok := msg.ID.(string); ok {
			usageID = id
		}
		if usageID == "" {
			usageID = jsStr(e.UUID)
		}
	}

	if filter == nil || filter.days != nil {
		if filter == nil || filter.days[date] {
			f.statsEntry(ts, date, ms, sid, isUser, msgTruthy, msg, usageID, dayUsage)
		}
	}
	if sid == "" {
		return
	}
	if filter == nil || filter.period != nil {
		if filter == nil || filter.period(ts, ms) {
			f.periodEntry(ts, ms, sid, line, isUser, msgTruthy, msg, usageID, bucketUsage)
		}
	}
	if filter == nil && !math.IsNaN(ms) && date >= f.stampsFrom {
		s, ok := f.stamps.get(date)
		if !ok {
			s = newOmap[[]float64]()
			f.stamps.set(date, s)
		}
		list, _ := s.get(sid)
		s.set(sid, append(list, ms))
	}
}

func (f *claudeFacts) statsEntry(ts, date string, ms float64, sid string, isUser, msgTruthy bool, msg claudeMessage, usageID string,
	seen map[string]string) {
	day, ok := f.days.get(date)
	if !ok {
		day = newDay()
		f.days.set(date, day)
	}
	hour := jscompat.Number(float64(msTime(ms).Hour()))
	if math.IsNaN(ms) {
		hour = "NaN"
	}
	if isUser {
		day.messageCount++
		h, _ := day.hours.get(hour)
		day.hours.set(hour, h+1)
		if sid != "" {
			if s, ok := day.sessions.get(sid); ok {
				s.messages++
				if lessJS(s.end, ts) {
					s.end = ts
				}
				if lessJS(ts, s.start) {
					s.start = ts
				}
			} else {
				day.sessions.set(sid, &daySession{start: ts, end: ts, messages: 1})
			}
		}
		return
	}
	if !msgTruthy {
		return
	}
	day.toolCallCount += float64(countToolUses(msg.Content))
	model := jsStr(msg.Model)
	if model != "" && !strings.HasPrefix(model, "<") && truthy(msg.Usage) {
		first, counted := seen[usageID]
		if usageID != "" && counted && first != date {
			f.daysCrossDup = true
		}
		if usageID == "" || !counted {
			if usageID != "" {
				seen[usageID] = date
			}
			t, ok := day.models.get(model)
			if !ok {
				t = &tokens{}
				day.models.set(model, t)
			}
			usage := msg.Usage
			cc := toNum(prop(usage, "cache_creation_input_tokens"))
			creation := prop(usage, "cache_creation")
			cc1h := toNum(prop(creation, "ephemeral_1h_input_tokens"))
			var cc5m float64
			if v := prop(creation, "ephemeral_5m_input_tokens"); v != nil {
				cc5m = toNum(v)
			} else {
				cc5m = jsMax0(cc - cc1h)
			}
			t[0] += toNum(prop(usage, "input_tokens"))
			t[1] += toNum(prop(usage, "output_tokens"))
			t[2] += toNum(prop(usage, "cache_read_input_tokens"))
			t[3] += cc
			t[4] += cc5m
			t[5] += cc1h
		}
	}
	if sid != "" {
		if s, ok := day.sessions.get(sid); ok {
			if lessJS(s.end, ts) {
				s.end = ts
			}
			if lessJS(ts, s.start) {
				s.start = ts
			}
		} else {
			day.sessions.set(sid, &daySession{start: ts, end: ts})
		}
	}
}

// jsMax0 is Math.max(0, x).
func jsMax0(x float64) float64 {
	if math.IsNaN(x) {
		return x
	}
	return math.Max(0, x)
}

func (f *claudeFacts) periodEntry(ts string, ms float64, sid string, line int, isUser, msgTruthy bool, msg claudeMessage, usageID string,
	seen map[string]int) {
	key := dayNumber(ms)
	b := f.buckets[key]
	if b == nil {
		b = &bucket{maxMs: math.Inf(-1), sessions: newOmap[*periodSession]()}
		f.buckets[key] = b
	}
	if ms > b.maxMs {
		b.maxMs = ms
	}
	s, ok := b.sessions.get(sid)
	if !ok {
		s = &periodSession{firstLine: line, start: ts, end: ts, modelLine: -1}
		b.sessions.set(sid, s)
	}
	if lessJS(ts, s.start) {
		s.start = ts
	}
	if lessJS(s.end, ts) {
		s.end = ts
	}
	if isUser {
		s.messages++
		return
	}
	if !msgTruthy {
		return
	}
	if model := jsStr(msg.Model); model != "" {
		s.model, s.modelLine = model, line
	}
	if !truthy(msg.Usage) {
		return
	}
	first, counted := seen[usageID]
	if usageID != "" && counted && first != key {
		f.bucketCrossDp = true
	}
	if usageID != "" && counted {
		return
	}
	if usageID != "" {
		seen[usageID] = key
	}
	s.input += toNum(prop(msg.Usage, "input_tokens"))
	s.output += toNum(prop(msg.Usage, "output_tokens"))
	s.read += toNum(prop(msg.Usage, "cache_read_input_tokens"))
	s.write += toNum(prop(msg.Usage, "cache_creation_input_tokens"))
}

// claudeFile is a transcript in the list the old runtime read: projects/<folder>/<name>.jsonl, agent files left out.
type claudeFile struct {
	path, project string
}

func listClaudeFiles(projects string) []claudeFile {
	var out []claudeFile
	dirs, err := os.ReadDir(projects)
	if err != nil {
		return nil
	}
	for _, d := range dirs {
		dir := filepath.Join(projects, d.Name())
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			continue
		}
		names, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, n := range names {
			name := n.Name()
			if strings.HasSuffix(name, ".jsonl") && !strings.HasPrefix(name, "agent-") {
				out = append(out, claudeFile{path: filepath.Join(dir, name), project: d.Name()})
			}
		}
	}
	return out
}

type fileVersion struct {
	size  int64
	mtime int64
}

func versionOf(st os.FileInfo) fileVersion { return fileVersion{st.Size(), st.ModTime().UnixNano()} }

type cachedFacts struct {
	version fileVersion
	facts   *claudeFacts
}

// claudeStore keeps each transcript's facts while the file stays as it was read.
type claudeStore struct {
	mu    sync.Mutex
	files map[string]*cachedFacts
}

// stampsDays is how far back the per-day times are kept: the uptime asks for the last seven days only.
const stampsDays = 10

// facts are the facts of each listed file, in the list's order; files no longer listed are forgotten.
func (s *claudeStore) facts(files []claudeFile, now time.Time) []*claudeFacts {
	from := now.UTC().AddDate(0, 0, -stampsDays).Format("2006-01-02")
	out := make([]*claudeFacts, len(files))
	type job struct {
		i       int
		version fileVersion
	}
	var todo []job
	s.mu.Lock()
	if s.files == nil {
		s.files = map[string]*cachedFacts{}
	}
	present := make(map[string]bool, len(files))
	for i, f := range files {
		present[f.path] = true
		st, err := os.Stat(f.path)
		if err != nil {
			out[i] = &claudeFacts{days: newOmap[*dayData](), buckets: map[int]*bucket{}, stamps: newOmap[*omap[[]float64]](), stampsFrom: from}
			continue
		}
		v := versionOf(st)
		if c := s.files[f.path]; c != nil && c.version == v {
			out[i] = c.facts
			continue
		}
		todo = append(todo, job{i, v})
	}
	for p := range s.files {
		if !present[p] {
			delete(s.files, p)
		}
	}
	s.mu.Unlock()
	if len(todo) > 0 {
		var wg sync.WaitGroup
		next := make(chan job)
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range next {
					out[j.i] = readClaudeFacts(files[j.i].path, from, nil)
				}
			}()
		}
		for _, j := range todo {
			next <- j
		}
		close(next)
		wg.Wait()
		s.mu.Lock()
		for _, j := range todo {
			s.files[files[j.i].path] = &cachedFacts{version: j.version, facts: out[j.i]}
		}
		s.mu.Unlock()
	}
	return out
}

// stampsFor are a file's times on the given days, read again when the kept ones do not reach back that far.
func stampsFor(path string, f *claudeFacts, days map[string]bool) *omap[*omap[[]float64]] {
	for d := range days {
		if d < f.stampsFrom {
			return readClaudeFacts(path, "", nil).stamps
		}
	}
	return f.stamps
}
