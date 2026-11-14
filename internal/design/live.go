package design

import (
	"regexp"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// What the open previews of designs report and are told, in memory: the live log a page's script writes (errors,
// long tasks, slow loads, read by agents), what each page shows, and the commands an agent sends to the open pages.

// LiveLogEntry is one report of a page.
type LiveLogEntry struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	Page    string `json:"page"`
	Message string `json:"message"`
}

// DesignState is what a page last said it shows.
type DesignState struct {
	At    int64 `json:"at"`
	State any   `json:"state"`
}

// Control is a command for the open pages of a design.
type Control struct {
	Board string          `json:"board,omitempty"`
	View  string          `json:"view,omitempty"`
	Panel string          `json:"panel,omitempty"`
	Props *jsvalue.Object `json:"props,omitempty"`
}

type liveState struct {
	mu        sync.Mutex
	logs      map[string][]LiveLogEntry
	states    map[string]DesignState
	listeners map[string]map[int]func(Control)
	next      int
	// pending is a command for a design no page showed when it came: the page that opens it within
	// PendingControlFor gets it, once.
	pending map[string]pendingControl
}

type pendingControl struct {
	c  Control
	at time.Time
}

// PendingControlFor is how long a command waits for a page of its design; PendingControlDelay lets the page that
// came list its boards before it is told which to show.
const (
	PendingControlFor   = 10 * time.Minute
	PendingControlDelay = 1500 * time.Millisecond
)

func (s *liveState) init() {
	s.logs = map[string][]LiveLogEntry{}
	s.states = map[string]DesignState{}
	s.listeners = map[string]map[int]func(Control){}
	s.pending = map[string]pendingControl{}
}

// QueueControl keeps a command for the next page of the design that opens (the pane asked to show it a moment ago).
func (l *Library) QueueControl(ref string, c Control) {
	s := &l.live
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = map[string]pendingControl{}
	}
	s.pending[ref] = pendingControl{c: c, at: time.Now()}
}

var liveLogKinds = map[string]bool{"error": true, "rejection": true, "longtask": true, "slow": true}

var liveControls = regexp.MustCompile("[\x00-\x08\x0b-\x1f\x7f]+")

func clipLive(v *string) string {
	if v == nil {
		return ""
	}
	return jscompat.Slice(liveControls.ReplaceAllString(*v, " "), 600)
}

// nowMs is the clock of the library's records; tests set it.
var nowMs = func() int64 { return time.Now().UnixMilli() }

// RecordLiveLog keeps a page's report: the newest 200 of each design.
func (l *Library) RecordLiveLog(artifact string, kind, page, message *string) bool {
	if kind == nil || !liveLogKinds[*kind] || message == nil || *message == "" {
		return false
	}
	s := &l.live
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := append(s.logs[artifact], LiveLogEntry{At: nowMs(), Kind: *kind, Page: clipLive(page), Message: clipLive(message)})
	if len(entries) > 200 {
		entries = append([]LiveLogEntry{}, entries[len(entries)-200:]...)
	}
	s.logs[artifact] = entries
	return true
}

func (l *Library) LiveLog(artifact string) []LiveLogEntry {
	s := &l.live
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]LiveLogEntry{}, s.logs[artifact]...)
}

// RecordState keeps what a page says it shows (JSON of at most 4000 characters).
func (l *Library) RecordState(ref string, raw *string) bool {
	if raw == nil || *raw == "" || jscompat.Len(*raw) > 4000 {
		return false
	}
	v, err := jsvalue.Parse([]byte(*raw))
	if err != nil {
		return false
	}
	s := &l.live
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[ref] = DesignState{At: nowMs(), State: v}
	return true
}

// State is nil before any page of the design reported.
func (l *Library) State(ref string) *DesignState {
	s := &l.live
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[ref]
	if !ok {
		return nil
	}
	return &st
}

var controlPanels = map[string]bool{"show": true, "hide": true, "fold": true, "unfold": true}

// ParseControl keeps only the known fields of the known shapes of an agent's command; nil when nothing is left.
func ParseControl(raw any) *Control {
	in, ok := raw.(*jsvalue.Object)
	if !ok {
		return nil
	}
	c := &Control{}
	if b, ok := get(in, "board").(string); ok && b != "" {
		c.Board = jscompat.Slice(b, 300)
	}
	if v, ok := get(in, "view").(string); ok && (v == "desktop" || v == "phone") {
		c.View = v
	}
	if p, ok := get(in, "panel").(string); ok && controlPanels[p] {
		c.Panel = p
	}
	if props := in.Object("props"); props != nil {
		out := jsvalue.NewObject()
		keys := props.Keys()
		if len(keys) > 40 {
			keys = keys[:40]
		}
		for _, k := range keys {
			switch v := get(props, k).(type) {
			case string, float64, bool:
				out.Set(jscompat.Slice(k, 80), v)
			}
		}
		if out.Len() > 0 {
			c.Props = out
		}
	}
	if c.Board == "" && c.View == "" && c.Panel == "" && c.Props == nil {
		return nil
	}
	return c
}

// OnControl hears the commands for a design's pages until the returned func is called.
func (l *Library) OnControl(ref string, fn func(Control)) func() {
	s := &l.live
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := s.next
	if s.listeners[ref] == nil {
		s.listeners[ref] = map[int]func(Control){}
	}
	s.listeners[ref][id] = fn
	if p, ok := s.pending[ref]; ok {
		delete(s.pending, ref)
		if time.Since(p.at) < PendingControlFor {
			time.AfterFunc(PendingControlDelay, func() {
				s.mu.Lock()
				_, still := s.listeners[ref][id]
				s.mu.Unlock()
				if still {
					fn(p.c)
				}
			})
		}
	}
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.listeners[ref], id)
		if len(s.listeners[ref]) == 0 {
			delete(s.listeners, ref)
		}
	}
}

// SendControl sends a command to every open page of the design; the number of pages it reached.
func (l *Library) SendControl(ref string, c Control) int {
	s := &l.live
	s.mu.Lock()
	fns := make([]func(Control), 0, len(s.listeners[ref]))
	for _, fn := range s.listeners[ref] {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(c)
	}
	return len(fns)
}

// controlJSON is a command as the page reads it: the fields in the order the command was built.
func controlJSON(c Control) []byte {
	o := jsvalue.NewObject()
	if c.Board != "" {
		o.Set("board", c.Board)
	}
	if c.View != "" {
		o.Set("view", c.View)
	}
	if c.Panel != "" {
		o.Set("panel", c.Panel)
	}
	if c.Props != nil {
		o.Set("props", c.Props)
	}
	return jsvalue.Stringify(o, "")
}

// HasPages says whether a page of the design is open now (it hears the agents' commands).
func (l *Library) HasPages(ref string) bool {
	s := &l.live
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.listeners[ref]) > 0
}
