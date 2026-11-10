package apps

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/journal"
)

// The errors of an app's views: what the SDK catches in a view (console.error and console.warn, uncaught
// exceptions, rejected promises, refusals of the content policy) and the bridge calls that failed. The page sends them
// here while a view runs, the runtime keeps the last ones of each app in <registry>/viewlog/<id>.jsonl, and
// `harnessly app logs <id>` and `harnessly app explain <id>` read them. A hint for the author, bounded in size: never a
// view's data, only the line it wrote.

// ViewLogMax is how many lines an app's log keeps; ViewLogTextMax the longest line, in characters; ViewLogBatchMax how
// many lines one report may carry.
const (
	ViewLogMax      = 200
	ViewLogTextMax  = 1000
	ViewLogBatchMax = 50
)

// The levels of a line: the view's console, an exception, a failed bridge call.
const (
	ViewLogError  = "error"
	ViewLogWarn   = "warn"
	ViewLogBridge = "bridge"
	// The host's own lines: a file of a view the server refused, a call of the app's MCP backend that failed,
	// and the steps of a load (a view opened, its handshake answered, no handshake in time).
	ViewLogServe = "serve"
	ViewLogMCP   = "mcp"
	ViewLogLoad  = "load"
)

// ViewLogLine is one line of an app's log.
type ViewLogLine struct {
	At    int64  `json:"at"`
	Level string `json:"level"`
	View  string `json:"view,omitempty"`
	Text  string `json:"text"`
}

var viewLogMu sync.Mutex

// ValidViewLevel says whether a level is one a line may carry.
func ValidViewLevel(level string) bool {
	switch level {
	case ViewLogError, ViewLogWarn, ViewLogBridge, ViewLogServe, ViewLogMCP, ViewLogLoad:
		return true
	}
	return false
}

// ViewLogFailure says whether a line is a failure: everything but a load step and a warning.
func ViewLogFailure(l ViewLogLine) bool { return l.Level != ViewLogLoad && l.Level != ViewLogWarn }

// LogEvent adds one line of the host's own to an app's log; a failure to write it is not the caller's concern. The
// line goes to the machine's journal too.
func (r *Registry) LogEvent(id, view, level, text string) {
	r.logEvent(id, view, level, text, journal.Entry{})
}

func (r *Registry) logEvent(id, view, level, text string, je journal.Entry) {
	if r == nil {
		return
	}
	_, _ = r.AppendViewLog(id, []ViewLogLine{{Level: level, View: view, Text: text}}, time.Now())
	if je.Event == "" {
		je.Event = "app." + level
		je.Text = text
	}
	je.Source, je.App, je.View = JournalSource(id), id, view
	if je.Level == "" {
		je.Level = journalLevel(level)
	}
	journal.Add(je)
}

// JournalSource is the journal's source of an app's lines: Sinopia's own, or an app's.
func JournalSource(id string) string {
	if id == SinopiaApp {
		return journal.SourceSinopia
	}
	return journal.SourceApp
}

// journalLevel is the journal's level of an app log's level: a load step is info, a warning a warning, the rest errors.
func journalLevel(level string) string {
	switch level {
	case ViewLogLoad:
		return journal.Info
	case ViewLogWarn:
		return journal.Warn
	}
	return journal.Error
}

// ViewLogSince is the lines of an app's log written at or after since (milliseconds), oldest first, at most limit of the
// last ones (0: all kept).
func (r *Registry) ViewLogSince(id string, since int64, limit int) ([]ViewLogLine, error) {
	all, err := r.ViewLog(id, 0)
	if err != nil {
		return nil, err
	}
	out := all[:0:0]
	for _, l := range all {
		if l.At >= since {
			out = append(out, l)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (r *Registry) viewLogPath(id string) (string, bool) {
	if !idRe.MatchString(id) {
		return "", false
	}
	return filepath.Join(r.Root, "viewlog", id+".jsonl"), true
}

// AppendViewLog adds lines to an app's log and trims it to ViewLogMax; lines of an unknown level or with no text are
// dropped, a longer text is cut, and at most ViewLogBatchMax lines are taken. It returns the lines kept.
func (r *Registry) AppendViewLog(id string, lines []ViewLogLine, now time.Time) ([]ViewLogLine, error) {
	path, ok := r.viewLogPath(id)
	if !ok {
		return nil, os.ErrInvalid
	}
	kept := []ViewLogLine{}
	var raw [][]byte
	for _, l := range lines {
		if len(kept) == ViewLogBatchMax {
			break
		}
		if !ValidViewLevel(l.Level) || l.Text == "" {
			continue
		}
		// The text is the app's: control characters (newlines, escapes) go, so a line stays one line wherever it is read.
		l.Text, l.View = clip(plainLine(l.Text), ViewLogTextMax), clip(plainLine(l.View), 100)
		if l.Text == "" {
			continue
		}
		if l.At <= 0 || l.At > now.UnixMilli() {
			l.At = now.UnixMilli()
		}
		b, err := json.Marshal(l)
		if err != nil {
			continue
		}
		kept = append(kept, l)
		raw = append(raw, b)
	}
	if len(kept) == 0 {
		return kept, nil
	}
	viewLogMu.Lock()
	defer viewLogMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	have, _ := os.ReadFile(path)
	var all [][]byte
	if len(have) > 0 {
		all = bytes.Split(bytes.TrimRight(have, "\n"), []byte("\n"))
	}
	all = append(all, raw...)
	if len(all) > ViewLogMax {
		all = all[len(all)-ViewLogMax:]
	}
	return kept, writeFileAtomic(path, append(bytes.Join(all, []byte("\n")), '\n'))
}

// ViewLog is the last limit lines of an app's log, oldest first; none kept is an empty list.
func (r *Registry) ViewLog(id string, limit int) ([]ViewLogLine, error) {
	path, ok := r.viewLogPath(id)
	if !ok {
		return nil, os.ErrInvalid
	}
	viewLogMu.Lock()
	have, err := os.ReadFile(path)
	viewLogMu.Unlock()
	out := []ViewLogLine{}
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(have))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var l ViewLogLine
		if json.Unmarshal(sc.Bytes(), &l) == nil {
			out = append(out, l)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, sc.Err()
}

// plainLine is s with every control character a space, trimmed: no newline, carriage return or terminal escape.
func plainLine(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s))
}

// ClearViewLog empties an app's log.
func (r *Registry) ClearViewLog(id string) error {
	path, ok := r.viewLogPath(id)
	if !ok {
		return os.ErrInvalid
	}
	viewLogMu.Lock()
	defer viewLogMu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var (
	onceMu   sync.Mutex
	onceSeen = map[string]time.Time{}
)

// LogEventOnce is LogEvent, but the same line of the same app is written once in ten seconds: a view that asks for a
// missing file on every load fills its log with one line, not a hundred.
func (r *Registry) LogEventOnce(id, view, level, text string) {
	r.LogEventOnceJ(id, view, level, text, journal.Entry{})
}

// LogEventOnceJ is LogEventOnce with the journal's line given whole (its chat, trace, address and status).
func (r *Registry) LogEventOnceJ(id, view, level, text string, je journal.Entry) {
	key := id + "|" + view + "|" + level + "|" + text
	now := time.Now()
	onceMu.Lock()
	if at, ok := onceSeen[key]; ok && now.Sub(at) < 10*time.Second {
		onceMu.Unlock()
		return
	}
	if len(onceSeen) > 500 {
		onceSeen = map[string]time.Time{}
	}
	onceSeen[key] = now
	onceMu.Unlock()
	r.logEvent(id, view, level, text, je)
}
