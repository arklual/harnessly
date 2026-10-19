package devtools

import (
	"math"
	"regexp"
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/preview/weburl"
)

// What the page's bridge reports: requests to other sites and its console. The page's own code can post anything here,
// so every field is checked and bounded, and what cannot be checked (the host, the source) is set here. A bad event is
// dropped, not the batch.

const (
	MaxReportEvents    = 100
	MaxReportBytes     = 256 * 1024
	reportMaxURL       = 2048
	reportMaxText      = 8000
	reportMaxError     = 200
	maxSkewMs          = 5 * 60_000
	windowMs           = 10_000
	MaxEventsPerWindow = 500
)

var (
	resourceTypes  = []string{"document", "script", "style", "image", "font", "media", "fetch", "xhr", "beacon", "websocket", "manifest", "other"}
	consoleLevels  = []string{"error", "warn", "info", "log"}
	consoleSources = map[string]bool{"console": true, "exception": true, "rejection": true, "network": true}
	methodRe       = regexp.MustCompile(`^[A-Z]{1,16}$`)
	mimeRe         = regexp.MustCompile(`(?i)^[a-z0-9!#$&^_.+-]{1,64}/[a-z0-9!#$&^_.+-]{1,128}$`)
)

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Report is a report's valid events.
type Report struct {
	Network []NetworkEntry
	Console []ConsoleEntry
}

type reportContext struct {
	slot   string
	target *Target
	now    int64
}

func reportURL(v any) *weburl.URL {
	s, ok := v.(string)
	if !ok || jscompat.Len(s) > reportMaxURL {
		return nil
	}
	u, ok := weburl.Parse(s)
	if !ok {
		return nil
	}
	switch u.Scheme {
	case "http", "https", "ws", "wss":
		return u
	}
	return nil
}

func countOf(v any, max float64) *int64 {
	f, ok := v.(float64)
	if !ok || math.IsInf(f, 0) || math.IsNaN(f) || f < 0 || f > max {
		return nil
	}
	n := int64(jsRound(f))
	return &n
}

func atOf(v any, now int64) int64 {
	f, ok := v.(float64)
	if ok && !math.IsInf(f, 0) && !math.IsNaN(f) && math.Abs(f-float64(now)) <= maxSkewMs {
		return int64(jsRound(f))
	}
	return now
}

func textOf(v any, max int) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if jscompat.Len(s) > max {
		s = jscompat.Slice(s, max)
	}
	return &s
}

func networkOf(raw *jsvalue.Object, c reportContext) (NetworkEntry, bool) {
	u := reportURL(field(raw, "url"))
	method, ok := field(raw, "method").(string)
	typ, ok2 := field(raw, "type").(string)
	if u == nil || !ok || !methodRe.MatchString(method) || !ok2 || !contains(resourceTypes, typ) {
		return NetworkEntry{}, false
	}
	real := ToTargetText(u.Href(), c.slot, c.target)
	host := ""
	if ru, ok := weburl.Parse(real); ok {
		host = ru.Host()
	}
	e := NetworkEntry{At: atOf(field(raw, "at"), c.now), Source: "page", Method: method, URL: real, Host: host, Type: typ}
	if st := countOf(field(raw, "status"), 999); st != nil && *st != 0 {
		e.Status = st
	}
	if m, ok := field(raw, "mime").(string); ok {
		m = strings.ToLower(jscompat.Trim(strings.SplitN(m, ";", 2)[0]))
		if mimeRe.MatchString(m) {
			e.Mime = &m
		}
	}
	e.Size = countOf(field(raw, "size"), 1e12)
	e.DurationMs = countOf(field(raw, "durationMs"), 3_600_000)
	e.ThirdParty = field(raw, "thirdParty") == true
	e.Blocked = field(raw, "blocked") == true
	e.Throttled = field(raw, "throttled") == true
	e.Error = textOf(field(raw, "error"), reportMaxError)
	return e, true
}

func consoleOf(raw *jsvalue.Object, c reportContext) (ConsoleEntry, bool) {
	level, ok := field(raw, "level").(string)
	if !ok || !contains(consoleLevels, level) {
		return ConsoleEntry{}, false
	}
	source, ok := field(raw, "source").(string)
	if !ok || !consoleSources[source] {
		return ConsoleEntry{}, false
	}
	text := textOf(field(raw, "text"), reportMaxText)
	if text == nil {
		return ConsoleEntry{}, false
	}
	e := ConsoleEntry{At: atOf(field(raw, "at"), c.now), Level: level, Source: source, Text: ToTargetText(*text, c.slot, c.target)}
	if st := textOf(field(raw, "stack"), reportMaxText); st != nil {
		v := ToTargetText(*st, c.slot, c.target)
		e.Stack = &v
	}
	if u := reportURL(field(raw, "url")); u != nil {
		v := ToTargetText(u.Href(), c.slot, c.target)
		e.URL = &v
	}
	return e, true
}

// ParseReport is nil when the body is not a report at all; otherwise its valid events, the first MaxReportEvents.
func ParseReport(v any, slot string, target *Target, now int64) *Report {
	obj, ok := v.(*jsvalue.Object)
	if !ok {
		return nil
	}
	events, ok := field(obj, "events").([]any)
	if !ok {
		return nil
	}
	c := reportContext{slot, target, now}
	r := &Report{}
	if len(events) > MaxReportEvents {
		events = events[:MaxReportEvents]
	}
	for _, ev := range events {
		raw, ok := ev.(*jsvalue.Object)
		if !ok {
			continue
		}
		switch field(raw, "kind") {
		case "network":
			if e, ok := networkOf(raw, c); ok {
				r.Network = append(r.Network, e)
			}
		case "console":
			if e, ok := consoleOf(raw, c); ok {
				r.Console = append(r.Console, e)
			}
		}
	}
	return r
}

type quota struct {
	start int64
	used  int
}

// TakeQuota is how many of wanted events the slot may still record in this window: a page logging in a loop fills the
// rings with its last words, not the runtime's memory.
func (s *Store) TakeQuota(slot string, wanted int, now int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.quotas[slot]
	if q == nil || now-q.start >= windowMs {
		q = &quota{start: now}
		s.quotas[slot] = q
	}
	granted := max(0, min(wanted, MaxEventsPerWindow-q.used))
	q.used += granted
	return granted
}
