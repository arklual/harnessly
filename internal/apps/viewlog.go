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
	return level == ViewLogError || level == ViewLogWarn || level == ViewLogBridge
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
