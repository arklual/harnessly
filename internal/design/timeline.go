package design

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"unicode/utf8"
)

// The timeline of a design: what its open page did while the owner worked with it (navigation, clicks, scrolling,
// tweaks, the console, slow or failed requests, long tasks, janky frames, web vitals), with the time of each event.
// Agents read it on demand with `design timeline` and `design moment`; nothing is pushed into their context.
//
// An event is a flat object: `t` (ms since the epoch), `k` (the kind) and the fields of that kind, each a short
// string, number or boolean. A new kind or field needs no change here but the entry in timelineKinds, so later stages
// (DOM snapshots, screenshots) can add their own.

// The kinds of events a page may record.
var timelineKinds = map[string]bool{
	"nav": true, "vp": true, "click": true, "scroll": true, "tweak": true, "console": true, "err": true,
	"req": true, "lt": true, "jank": true, "fps": true, "vital": true, "inp": true, "shift": true,
}

// Limits of what is kept per design. The file is rewritten without its oldest part when it outgrows the byte limit;
// events older than the age limit are dropped at the same time.
const (
	TimelineMaxAgeMs     = 6 * 60 * 60 * 1000
	TimelineMaxEvents    = 20000
	TimelineMaxFileBytes = 4 << 20
	timelineMaxBatch     = 60
	timelineMaxFields    = 14
	timelineMaxText      = 240
	timelineMaxSkewMs    = 10 * 60 * 1000
	timelineCheckEvery   = 200
	timelineDirName      = "design-timeline"
)

var timelineKeyRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,11}$`)

// TimelineEvent is one thing that happened in the design.
type TimelineEvent struct {
	T      int64
	K      string
	Page   string
	Fields map[string]any
}

func (e TimelineEvent) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, len(e.Fields)+3)
	for k, v := range e.Fields {
		m[k] = v
	}
	m["t"], m["k"] = e.T, e.K
	if e.Page != "" {
		m["p"] = e.Page
	}
	return json.Marshal(m)
}

func (e *TimelineEvent) UnmarshalJSON(b []byte) error {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return err
	}
	*e = TimelineEvent{Fields: map[string]any{}}
	for k, v := range m {
		switch k {
		case "t":
			if n, ok := v.(json.Number); ok {
				f, _ := n.Float64()
				e.T = int64(f)
			}
		case "k":
			e.K, _ = v.(string)
		case "p":
			e.Page, _ = v.(string)
		default:
			e.Fields[k] = v
		}
	}
	return nil
}

// Str, Num and Bool read a field by name; a missing or differently typed field reads as the zero value.
func (e TimelineEvent) Str(name string) string {
	s, _ := e.Fields[name].(string)
	return s
}

func (e TimelineEvent) Num(name string) float64 {
	switch v := e.Fields[name].(type) {
	case json.Number:
		f, _ := v.Float64()
		return f
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func (e TimelineEvent) Has(name string) bool { _, ok := e.Fields[name]; return ok }

type timelineLog struct {
	events    []TimelineEvent
	loaded    bool
	bytes     int64
	sinceScan int
}

type timelineState struct {
	mu   sync.Mutex
	logs map[string]*timelineLog
}

// cleanTimelineEvents keeps the well-formed events of a batch a page sent: a known kind, a plain time, flat short
// fields. now is the runtime's clock: a time far from it (a wrong clock in the page) is replaced by it.
func cleanTimelineEvents(raw []byte, page string, now int64) []TimelineEvent {
	var items []map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&items) != nil {
		return nil
	}
	if len(items) > timelineMaxBatch {
		items = items[:timelineMaxBatch]
	}
	out := make([]TimelineEvent, 0, len(items))
	for _, item := range items {
		kind, _ := item["k"].(string)
		if !timelineKinds[kind] {
			continue
		}
		ev := TimelineEvent{T: now, K: kind, Page: clipLive(&page), Fields: map[string]any{}}
		if n, ok := item["t"].(json.Number); ok {
			if f, err := n.Float64(); err == nil {
				t := int64(f)
				if t > now-timelineMaxSkewMs && t < now+timelineMaxSkewMs {
					ev.T = t
				}
			}
		}
		for k, v := range item {
			if k == "t" || k == "k" || k == "p" || !timelineKeyRe.MatchString(k) || len(ev.Fields) >= timelineMaxFields {
				continue
			}
			switch x := v.(type) {
			case string:
				ev.Fields[k] = clipTimelineText(x)
			case json.Number:
				if f, err := x.Float64(); err == nil {
					ev.Fields[k] = roundTimelineNumber(f)
				}
			case bool:
				ev.Fields[k] = x
			}
		}
		out = append(out, ev)
	}
	return out
}

func roundTimelineNumber(f float64) float64 {
	return float64(int64(f*100+0.5*sign(f))) / 100
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

func clipTimelineText(s string) string {
	s = liveControls.ReplaceAllString(s, " ")
	if len(s) <= timelineMaxText {
		return s
	}
	cut := timelineMaxText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func (l *Library) timelineFile(artifact string) string {
	if l.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(l.cfg.DataDir, timelineDirName, artifact+".jsonl")
}

// timelineLogOf is the design's log, read from its file the first time. The caller holds the state's lock.
func (l *Library) timelineLogOf(artifact string) *timelineLog {
	s := &l.timeline
	if s.logs == nil {
		s.logs = map[string]*timelineLog{}
	}
	lg := s.logs[artifact]
	if lg == nil {
		lg = &timelineLog{}
		s.logs[artifact] = lg
	}
	if lg.loaded {
		return lg
	}
	lg.loaded = true
	path := l.timelineFile(artifact)
	if path == "" {
		return lg
	}
	f, err := os.Open(path)
	if err != nil {
		return lg
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var ev TimelineEvent
		if json.Unmarshal(sc.Bytes(), &ev) == nil && timelineKinds[ev.K] {
			lg.events = append(lg.events, ev)
			lg.bytes += int64(len(sc.Bytes()) + 1)
		}
	}
	return lg
}

// RecordTimeline keeps the events a page sent and returns how many it took. page is the page's path, as in the log.
func (l *Library) RecordTimeline(artifact string, page, raw *string) int {
	if raw == nil || *raw == "" {
		return 0
	}
	p := ""
	if page != nil {
		p = *page
	}
	now := nowMs()
	events := cleanTimelineEvents([]byte(*raw), p, now)
	if len(events) == 0 {
		return 0
	}
	s := &l.timeline
	s.mu.Lock()
	defer s.mu.Unlock()
	lg := l.timelineLogOf(artifact)
	var added bytes.Buffer
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		added.Write(b)
		added.WriteByte('\n')
		lg.events = append(lg.events, ev)
	}
	lg.bytes += int64(added.Len())
	lg.sinceScan += len(events)
	path := l.timelineFile(artifact)
	if lg.bytes > TimelineMaxFileBytes || len(lg.events) > TimelineMaxEvents || lg.sinceScan >= timelineCheckEvery {
		lg.sinceScan = 0
		if l.trimTimeline(lg, now) {
			l.rewriteTimeline(path, lg)
			return len(events)
		}
	}
	if path != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.Write(added.Bytes())
			_ = f.Close()
		}
	}
	return len(events)
}

// trimTimeline drops what is too old, then the oldest events while the log is over its count or byte limit (down to
// three quarters of it, so the file is not rewritten on every batch). It tells whether anything went.
func (l *Library) trimTimeline(lg *timelineLog, now int64) bool {
	sort.SliceStable(lg.events, func(i, j int) bool { return lg.events[i].T < lg.events[j].T })
	cut := 0
	for cut < len(lg.events) && lg.events[cut].T < now-TimelineMaxAgeMs {
		cut++
	}
	kept := lg.events[cut:]
	over := func(n int, b int64) bool { return n > TimelineMaxEvents || b > TimelineMaxFileBytes }
	if over(len(kept), lg.bytes) {
		target := len(kept) * 3 / 4
		if target > TimelineMaxEvents*3/4 {
			target = TimelineMaxEvents * 3 / 4
		}
		if lg.bytes > TimelineMaxFileBytes {
			if byBytes := int(int64(len(kept)) * (TimelineMaxFileBytes * 3 / 4) / lg.bytes); byBytes < target {
				target = byBytes
			}
		}
		cut += len(kept) - target
		kept = lg.events[cut:]
	}
	if cut == 0 {
		return false
	}
	lg.events = append([]TimelineEvent{}, kept...)
	lg.bytes = 0
	for _, ev := range lg.events {
		b, _ := json.Marshal(ev)
		lg.bytes += int64(len(b) + 1)
	}
	return true
}

func (l *Library) rewriteTimeline(path string, lg *timelineLog) {
	if path == "" {
		return
	}
	var buf bytes.Buffer
	for _, ev := range lg.events {
		b, _ := json.Marshal(ev)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, buf.Bytes(), 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// Timeline is the events of a design between two times (ms, both ends included; to 0 means no end), oldest first and
// no older than the age limit.
func (l *Library) Timeline(artifact string, from, to int64) []TimelineEvent {
	s := &l.timeline
	s.mu.Lock()
	defer s.mu.Unlock()
	lg := l.timelineLogOf(artifact)
	oldest := nowMs() - TimelineMaxAgeMs
	if from < oldest {
		from = oldest
	}
	out := []TimelineEvent{}
	for _, ev := range lg.events {
		if ev.T >= from && (to == 0 || ev.T <= to) {
			out = append(out, ev)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

// Moment is what the timeline holds around one time: the events of the window, the last of each kind of state before
// it (the page, the viewport, every tweak, the vitals) and the last actions of the person.
type Moment struct {
	At      int64           `json:"at"`
	Window  int64           `json:"window"`
	Events  []TimelineEvent `json:"events"`
	State   []TimelineEvent `json:"state"`
	Actions []TimelineEvent `json:"actions"`
}

const momentActions = 8
const momentActionsLookbackMs = 2 * 60 * 1000

var timelineActionKinds = map[string]bool{"nav": true, "click": true, "tweak": true}

// MomentAt gathers the window [at-window, at+window] of a design.
func (l *Library) MomentAt(artifact string, at, window int64) Moment {
	all := l.Timeline(artifact, 0, at+window)
	m := Moment{At: at, Window: window, Events: []TimelineEvent{}, State: []TimelineEvent{}, Actions: []TimelineEvent{}}
	last := map[string]TimelineEvent{}
	var order []string
	for _, ev := range all {
		if ev.T >= at-window {
			m.Events = append(m.Events, ev)
		}
		if ev.T > at {
			continue
		}
		key := ""
		switch ev.K {
		case "nav", "vp":
			key = ev.K
		case "tweak":
			key = "tweak:" + ev.Str("n")
		case "vital":
			key = "vital:" + ev.Str("n")
		}
		if key != "" {
			if _, seen := last[key]; !seen {
				order = append(order, key)
			}
			last[key] = ev
		}
		if timelineActionKinds[ev.K] && ev.T >= at-momentActionsLookbackMs {
			m.Actions = append(m.Actions, ev)
		}
	}
	for _, k := range order {
		m.State = append(m.State, last[k])
	}
	if len(m.Actions) > momentActions {
		m.Actions = m.Actions[len(m.Actions)-momentActions:]
	}
	return m
}
