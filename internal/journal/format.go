package journal

import (
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// FormatLine is one line in a few words: time, source, event, what and how it ended.
func FormatLine(e Entry) string {
	var b strings.Builder
	b.WriteString(time.UnixMilli(e.At).Local().Format("15:04:05.000"))
	b.WriteString(" ")
	switch e.Level {
	case Error:
		b.WriteString("✗ ")
	case Warn:
		b.WriteString("! ")
	default:
		b.WriteString("  ")
	}
	b.WriteString(pad(e.Source, 7))
	b.WriteString(" ")
	b.WriteString(e.Event)
	// A request names its method and address first, then what it called; anything else its tool first.
	if e.Tool != "" && e.Method == "" {
		b.WriteString(" " + e.Tool)
	}
	if e.Method != "" {
		b.WriteString(" " + e.Method)
	}
	if e.URL != "" {
		b.WriteString(" " + e.URL)
	}
	if e.Status != 0 {
		b.WriteString(" → " + strconv.Itoa(e.Status))
	}
	if e.Dur > 0 || timed[e.Event] {
		b.WriteString(" " + formatDur(e.Dur))
	}
	if e.Tool != "" && e.Method != "" {
		b.WriteString(" · " + e.Tool)
	}
	if e.Text != "" {
		b.WriteString(" — " + e.Text)
	}
	var tags []string
	if e.App != "" {
		app := "app=" + e.App
		if e.View != "" {
			app += "/" + e.View
		}
		tags = append(tags, app)
	}
	if e.Chat != "" {
		tags = append(tags, "chat="+shortID(e.Chat))
	}
	if e.Trace != "" {
		tags = append(tags, "trace="+e.Trace)
	}
	if len(tags) > 0 {
		b.WriteString("  [" + strings.Join(tags, " ") + "]")
	}
	return b.String()
}

// timed are the events whose time is always shown, a quick one as 0ms.
var timed = map[string]bool{"http": true, "fetch": true, "mcp.call": true, "ui.initialize": true}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func shortID(s string) string {
	if len(s) > 8 && strings.Count(s, "-") == 4 {
		return s[:8]
	}
	return s
}

func formatDur(ms int64) string {
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + "s"
}

// FormatChains is the chains as text: each error, then the lines that led to it, then the conclusions.
func FormatChains(chains []Chain) string {
	if len(chains) == 0 {
		return locale.Pick("No errors in this time.", "Ошибок нет за это время.")
	}
	var b strings.Builder
	for i, c := range chains {
		if i > 0 {
			b.WriteString("\n")
		}
		first := c.Errors[0]
		head := "■ " + Describe(first)
		if first.App != "" {
			head += locale.Pick(" · app ", " · приложение ") + first.App
		}
		if first.Chat != "" {
			head += locale.Pick(" · chat ", " · чат ") + shortID(first.Chat)
		}
		if len(c.Errors) > 1 {
			head += locale.Pick(" · errors in the chain: ", " · ошибок в цепочке: ") + strconv.Itoa(len(c.Errors))
		}
		b.WriteString(head + "\n")
		for _, e := range c.Lines {
			b.WriteString("  " + FormatLine(e) + "\n")
		}
		for _, h := range c.Hints {
			b.WriteString("  ⇒ " + h + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatLines is lines as text, one a line.
func FormatLines(lines []Entry) string {
	if len(lines) == 0 {
		return locale.Pick("No lines in this time.", "Записей нет за это время.")
	}
	out := make([]string, len(lines))
	for i, e := range lines {
		out[i] = FormatLine(e)
	}
	return strings.Join(out, "\n")
}

// ParseSince reads "10m", "2h", "1d", a time "15:04" today, or an RFC 3339 time; "" is the default given.
func ParseSince(s string, now time.Time, def time.Duration) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return now.Add(-def), true
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n >= 0 {
			return now.Add(-time.Duration(n) * 24 * time.Hour), true
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), true
	}
	if t, err := time.ParseInLocation("15:04", s, now.Location()); err == nil {
		y, m, d := now.Date()
		return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, now.Location()), true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil && ms > 1e12 {
		return time.UnixMilli(ms), true
	}
	return time.Time{}, false
}
