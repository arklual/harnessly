package apps

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// The journal of the guard's decisions (policy.go): one JSON line per call an app of a chat refused or asked the person
// about, kept by the runtime in <registry>/decisions/<conversation>.jsonl. The page shows the last ones of an app in its
// «О приложении» section. The journal holds the tool's name and the rule, never the call's input.

// DecisionsMax is how many lines a conversation's journal keeps; past it the oldest go.
const DecisionsMax = 500

// DecisionReasonMax is the longest reason (in characters) a line keeps.
const DecisionReasonMax = 300

// DecisionEntry is one line of the journal.
type DecisionEntry struct {
	// At is the time of the decision, in milliseconds.
	At int64 `json:"at"`
	// Session is the agent's own session the call was made in.
	Session  string `json:"session,omitempty"`
	App      string `json:"app"`
	Tool     string `json:"tool"`
	Rule     string `json:"rule"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

var decisionsMu sync.Mutex

var safeConv = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func decisionsPath(dir, conv string) (string, bool) {
	if !safeConv.MatchString(conv) {
		return "", false
	}
	return filepath.Join(dir, conv+".jsonl"), true
}

func clip(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// AppendDecision adds a line to the conversation's journal in dir and trims it to DecisionsMax lines.
func AppendDecision(dir, conv string, e DecisionEntry) error {
	path, ok := decisionsPath(dir, conv)
	if !ok {
		return os.ErrInvalid
	}
	e.Reason = clip(e.Reason, DecisionReasonMax)
	e.Tool, e.Rule, e.App, e.Session = clip(e.Tool, 200), clip(e.Rule, 200), clip(e.App, 200), clip(e.Session, 100)
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	decisionsMu.Lock()
	defer decisionsMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	have, _ := os.ReadFile(path)
	var lines [][]byte
	if len(have) > 0 {
		lines = bytes.Split(bytes.TrimRight(have, "\n"), []byte("\n"))
	}
	lines = append(lines, line)
	if len(lines) > DecisionsMax {
		lines = lines[len(lines)-DecisionsMax:]
	}
	return writeFileAtomic(path, append(bytes.Join(lines, []byte("\n")), '\n'))
}

// ReadDecisions is the last limit lines of the conversation's journal for an app, newest first. A journal that is not
// there is an empty list; a line that cannot be read is skipped.
func ReadDecisions(dir, conv, app string, limit int) ([]DecisionEntry, error) {
	path, ok := decisionsPath(dir, conv)
	if !ok {
		return nil, os.ErrInvalid
	}
	out := []DecisionEntry{}
	decisionsMu.Lock()
	have, err := os.ReadFile(path)
	decisionsMu.Unlock()
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var all []DecisionEntry
	sc := bufio.NewScanner(bytes.NewReader(have))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e DecisionEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.App == app {
			all = append(all, e)
		}
	}
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, all[i])
	}
	return out, sc.Err()
}
