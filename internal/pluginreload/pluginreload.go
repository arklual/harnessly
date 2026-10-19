// Package pluginreload carries a change of the person's Claude Code plugins and skills into the Claude tabs that run.
// The plugin reconciler leaves a mark file ({"plugins": <ms>, "skills": <ms>}) when it installed or updated some; a
// running Claude keeps what it loaded at start until /reload-plugins or /reload-skills, so each Claude tab gets the
// command typed once per change: only an agent that stopped, with an empty prompt box and no message of the person
// waiting. What each tab has applied is kept on disk, so a restart does not reload every tab again.
package pluginreload

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"harnessly/internal/jscompat"
)

// Kinds, plugins first: their reload also brings the skills plugins carry.
var Kinds = []string{"plugins", "skills"}

// Commands are what each kind's reload types.
var Commands = map[string]string{"plugins": "/reload-plugins", "skills": "/reload-skills"}

// Mark is the time of the last change of each kind (epoch ms); a kind that is not there never changed.
type Mark map[string]float64

// ParseMark reads a mark file; false when it is not a JSON object. Only positive finite numbers count.
func ParseMark(raw []byte) (Mark, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil || dec.More() {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	m := Mark{}
	for _, k := range Kinds {
		n, ok := o[k].(json.Number)
		if !ok {
			continue
		}
		// JSON.parse reads a number too large for a double as Infinity, which is not a time.
		if at, err := strconv.ParseFloat(string(n), 64); err == nil && at > 0 && !math.IsInf(at, 0) {
			m[k] = at
		}
	}
	return m, true
}

func (m Mark) copy() Mark {
	out := Mark{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// MarshalJSON writes the kinds in their order.
func (m Mark) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, k := range Kinds {
		v, ok := m[k]
		if !ok {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(`"` + k + `":` + strconv.FormatFloat(v, 'f', -1, 64))
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Pending are the kinds changed since what a tab applied.
func Pending(mark, applied Mark) []string {
	var out []string
	for _, k := range Kinds {
		if mark[k] > applied[k] {
			out = append(out, k)
		}
	}
	return out
}

// Tab is what the decision reads of a tab.
type Tab struct {
	PanelType, Provider string // "" for none
	CliState            string
	// Queued: a message of the person waits in the platform's queue; it goes first.
	Queued bool
}

// Decision is insert (with the kind to reload), wait or skip.
type Decision struct {
	Action, Kind string
}

var working = map[string]bool{"busy": true, "unknown": true, "cancelled": true}

// Decide says whether to type a reload now. The screen is read once more right before typing, so an agent shown idle
// by a stale state is not interrupted.
func Decide(t Tab, mark, applied Mark) Decision {
	if t.PanelType != "claude-code" || (t.Provider != "" && t.Provider != "claude") {
		return Decision{Action: "skip"}
	}
	pending := Pending(mark, applied)
	if len(pending) == 0 {
		return Decision{Action: "skip"}
	}
	if working[t.CliState] || t.Queued {
		return Decision{Action: "wait"}
	}
	return Decision{Action: "insert", Kind: pending[0]}
}

// Screen is the screen's say: a working agent, an open menu, the text in Claude's prompt box (nil without one).
type Screen struct {
	Busy, Menu bool
	Input      *string
}

// ScreenDecision is insert, wait, or probe (no prompt box on screen: whether a Claude runs in the pane decides).
func ScreenDecision(s *Screen) string {
	if s == nil || s.Busy || s.Menu {
		return "wait"
	}
	if s.Input == nil {
		return "probe"
	}
	if jscompat.Trim(*s.Input) == "" {
		return "insert"
	}
	return "wait"
}

// Outcome is what a typed reload came to: applied, settled (no Claude runs in the tab; the next one reads the plugins
// as they are), gone (the tab has no session) or wait.
func Outcome(status int, errorCode string) string {
	switch {
	case status == 200:
		return "applied"
	case errorCode == "not-running":
		return "settled"
	case errorCode == "not-found":
		return "gone"
	}
	return "wait"
}

// Apply is what a tab has applied after an outcome; the same map when nothing changed.
func Apply(applied, mark Mark, kind, outcome string) Mark {
	switch outcome {
	case "applied":
		next := applied.copy()
		if v, ok := mark[kind]; ok {
			next[kind] = v
		} else {
			delete(next, kind)
		}
		return next
	case "settled":
		return mark.copy()
	}
	return applied
}

// Applied is what each tab's Claude has, kept in a file.
type Applied struct {
	File string

	mu     sync.Mutex
	loaded bool
	tabs   map[string]Mark
	order  []string
}

func (a *Applied) load() {
	if a.loaded {
		return
	}
	a.loaded = true
	a.tabs = map[string]Mark{}
	raw, err := os.ReadFile(a.File)
	if err != nil {
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return
		}
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return
		}
		if m, ok := ParseMark(v); ok {
			a.set(t.(string), m)
		}
	}
}

func (a *Applied) set(tab string, m Mark) {
	if _, ok := a.tabs[tab]; !ok {
		a.order = append(a.order, tab)
	}
	a.tabs[tab] = m
}

// Get is a tab's applied mark (nil when none).
func (a *Applied) Get(tab string) Mark {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	return a.tabs[tab]
}

// Tabs are the tabs with a mark.
func (a *Applied) Tabs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	return append([]string(nil), a.order...)
}

// Set records a tab's mark.
func (a *Applied) Set(tab string, m Mark) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	a.set(tab, m)
}

// Delete forgets a tab.
func (a *Applied) Delete(tab string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load()
	if _, ok := a.tabs[tab]; !ok {
		return
	}
	delete(a.tabs, tab)
	for i, t := range a.order {
		if t == tab {
			a.order = append(a.order[:i], a.order[i+1:]...)
			break
		}
	}
}

// Save writes the file whole, through a temporary one.
func (a *Applied) Save() error {
	a.mu.Lock()
	var b bytes.Buffer
	b.WriteByte('{')
	for i, tab := range a.order {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(tab)
		v, _ := a.tabs[tab].MarshalJSON()
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteString("}\n")
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.File), 0o700); err != nil {
		return err
	}
	tmp := a.File + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.File)
}
