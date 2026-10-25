package feed

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Limit is a usage limit of the agent's plan that a notice of its transcript says it hit: which window and when it
// resets (0 when the notice names no time).
type Limit struct {
	Window   wire.LimitWindow
	ResetsAt wire.Ms
}

// The agents' own words, as their CLIs print them:
//
//	Claude Code  You've hit your session limit · resets 11:50pm (UTC)
//	             You've hit your weekly limit · resets Oct 7, 5am (UTC)
//	             5-hour limit reached ∙ resets 6pm  ·  Claude AI usage limit reached|1760000000 (older)
//	Codex        You've hit your usage limit. Upgrade to Pro (…) or try again at 6:05 PM.
//	             You've hit your usage limit. Try again at Oct 26th, 2026 6:05 PM.
//	             You've hit your usage limit. Try again in 2 days 3 hours 5 minutes. (older)
//
// Claude's line also carries quotaLimits with the reset as a number, which the transcript reads first (limitResetsAt);
// the words are for a line without it. A plain 429 («would exceed your account's rate limit») is no usage limit: the
// agent retries it by itself.
var (
	claudeLimitRE  = regexp.MustCompile(`(?i)(?:hit your\s+(\S+(?:\s+\S+)?)\s+limit|(\S+(?:\s+\S+)?)\s+limit reached)\s*[·∙•|-]?\s*(?:resets\s+(.+))?$`)
	claudeEpochRE  = regexp.MustCompile(`(?i)usage limit reached\|(\d{9,11})`)
	codexLimitRE   = regexp.MustCompile(`(?i)you'?ve hit your usage limit`)
	codexAtRE      = regexp.MustCompile(`(?i)try again at\s+(.+?)\.?\s*$`)
	codexInRE      = regexp.MustCompile(`(?i)try again in\s+(.+?)\.?\s*$`)
	resetTZRE      = regexp.MustCompile(`\s*\(([^)]+)\)\s*$`)
	resetDateRE    = regexp.MustCompile(`^([A-Za-z]{3,9})\.?\s+(\d{1,2})(?:st|nd|rd|th)?(?:,?\s+(\d{4}))?(?:,|\s+at)?\s+`)
	resetClockRE   = regexp.MustCompile(`(?i)^(\d{1,2})(?::(\d{2}))?\s*([ap])\.?m\.?`)
	resetSpanRE    = regexp.MustCompile(`(?i)(\d+)\s*(day|hour|hr|minute|min|second|sec)s?`)
	codexLimitCode = "usage_limit_exceeded"
)

// limitOf reads a usage limit off an error-notice of the transcript, written at at; false when it is no such notice.
func limitOf(o *jsvalue.Object, provider wire.Provider, at wire.Ms) (Limit, bool) {
	message := strings.TrimSpace(str(o, "message"))
	when := time.UnixMilli(at)
	if provider == wire.ProviderCodex {
		if str(o, "errorCode") != codexLimitCode && !codexLimitRE.MatchString(message) {
			return Limit{}, false
		}
		return Limit{Window: wire.LimitOther, ResetsAt: codexReset(message, when, time.Local)}, true
	}
	if n, ok := num(o, "limitResetsAt"); ok && n > 0 {
		return Limit{Window: claudeWindow(str(o, "limitWindow")), ResetsAt: wire.Ms(n)}, true
	}
	if str(o, "errorCode") != "rate_limit" {
		return Limit{}, false
	}
	return ClaudeLimitText(message, when)
}

// ClaudeLimitText reads Claude Code's words of a usage limit hit at when: which window, and the reset it names.
func ClaudeLimitText(message string, when time.Time) (Limit, bool) {
	if m := claudeEpochRE.FindStringSubmatch(message); m != nil {
		sec, _ := strconv.ParseInt(m[1], 10, 64)
		return Limit{Window: wire.LimitOther, ResetsAt: sec * 1000}, true
	}
	m := claudeLimitRE.FindStringSubmatch(strings.TrimSpace(message))
	if m == nil {
		return Limit{}, false
	}
	name := strings.ToLower(m[1] + m[2])
	l := Limit{Window: wire.LimitOther}
	switch {
	case strings.Contains(name, "session"), strings.Contains(name, "5-hour"), strings.Contains(name, "five"):
		l.Window = wire.LimitSession
	case strings.Contains(name, "week"):
		l.Window = wire.LimitWeekly
	}
	if m[3] != "" {
		if t, ok := resetTime(m[3], when, time.Local); ok {
			l.ResetsAt = t.UnixMilli()
		}
	}
	return l, true
}

// CodexLimitText is the limit Codex's words of it name, seen at when (Codex prints the machine's time).
func CodexLimitText(message string, when time.Time) Limit {
	return Limit{Window: wire.LimitOther, ResetsAt: codexReset(message, when, time.Local)}
}

// claudeWindow is the window of Claude's quotaLimits.rateLimitType.
func claudeWindow(t string) wire.LimitWindow {
	switch {
	case t == "five_hour":
		return wire.LimitSession
	case strings.HasPrefix(t, "seven_day"):
		return wire.LimitWeekly
	}
	return wire.LimitOther
}

// codexReset is the reset Codex's words name, in the machine's time (Codex prints local time); 0 when none.
func codexReset(message string, when time.Time, loc *time.Location) wire.Ms {
	if m := codexAtRE.FindStringSubmatch(message); m != nil {
		if t, ok := resetTime(m[1], when, loc); ok {
			return t.UnixMilli()
		}
	}
	if m := codexInRE.FindStringSubmatch(message); m != nil {
		var d time.Duration
		for _, p := range resetSpanRE.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.Atoi(p[1])
			switch strings.ToLower(p[2]) {
			case "day":
				d += time.Duration(n) * 24 * time.Hour
			case "hour", "hr":
				d += time.Duration(n) * time.Hour
			case "minute", "min":
				d += time.Duration(n) * time.Minute
			default:
				d += time.Duration(n) * time.Second
			}
		}
		if d > 0 {
			return when.Add(d).UnixMilli()
		}
	}
	return 0
}

// resetTime reads «6pm», «11:50pm (UTC)», «Oct 7, 5am (Europe/Moscow)», «Oct 26th, 2026 6:05 PM» as the first such
// moment at or after when (a minute of slack: the words are rounded). A zone in brackets is the zone of the words;
// loc otherwise.
func resetTime(s string, when time.Time, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if m := resetTZRE.FindStringSubmatch(s); m != nil {
		if z, err := time.LoadLocation(strings.TrimSpace(m[1])); err == nil {
			loc = z
		}
		s = strings.TrimSpace(s[:len(s)-len(m[0])])
	}
	now := when.In(loc)
	year, month, day := now.Date()
	dated := false
	if m := resetDateRE.FindStringSubmatch(s); m != nil {
		mo, ok := monthOf(m[1])
		if !ok {
			return time.Time{}, false
		}
		month, dated = mo, true
		day, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			year, _ = strconv.Atoi(m[3])
		}
		s = s[len(m[0]):]
	}
	m := resetClockRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return time.Time{}, false
	}
	hour, _ := strconv.Atoi(m[1])
	minute := 0
	if m[2] != "" {
		minute, _ = strconv.Atoi(m[2])
	}
	if hour < 1 || hour > 12 || minute > 59 {
		return time.Time{}, false
	}
	hour %= 12
	if strings.EqualFold(m[3], "p") {
		hour += 12
	}
	t := time.Date(year, month, day, hour, minute, 0, 0, loc)
	slack := now.Add(-time.Minute)
	switch {
	case !t.Before(slack):
	case dated:
		t = t.AddDate(1, 0, 0) // «Jan 3» read in late December is next year's
	default:
		t = t.AddDate(0, 0, 1) // a clock time already past today is tomorrow's
	}
	return t, true
}

func monthOf(s string) (time.Month, bool) {
	s = strings.ToLower(s)
	if len(s) < 3 {
		return 0, false
	}
	for m := time.January; m <= time.December; m++ {
		if strings.HasPrefix(strings.ToLower(m.String()), s[:3]) {
			return m, true
		}
	}
	return 0, false
}

// LimitEnd is the usage limit a turn ends on ("" the open one): its last entry is the agent's notice of the limit.
// The notice's words come with it.
func (f *Feed) LimitEnd(id string) (Limit, string, bool) {
	if id == "" {
		id = f.b.openID()
	}
	i, ok := f.b.byTurn[id]
	if !ok {
		return Limit{}, "", false
	}
	t := f.b.turns[i]
	if len(t.entries) == 0 {
		return Limit{}, "", false
	}
	last := t.entries[len(t.entries)-1]
	l, ok := f.b.limits[last]
	if !ok {
		return Limit{}, "", false
	}
	e, _ := f.Entry(last)
	detail := ""
	if e.Detail != nil {
		detail = *e.Detail
	}
	return l, detail, true
}
