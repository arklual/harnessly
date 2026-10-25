// Package paneintents keeps what an agent asked its right pane to show, with the conversation rather than sent to a page
// that may not be there: the page that shows the conversation applies it when it does, now or hours later, on any
// device. The intents live in a file, so a restart or a closed browser loses nothing.
package paneintents

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/panel"
)

const (
	// TTL: an intent nobody came to see for a day is history, not something to throw at the person.
	TTL    = 24 * time.Hour
	PerTab = 20

	saveDelay = 200 * time.Millisecond
)

// An intent is `{id, command, createdAt, appliedAt, appliedOn}`: command is `{kind:'open', tab, target?, auto?, reload?}` or
// `{kind:'show'}`, times are epoch milliseconds, appliedAt and appliedOn null until a page first applied it.

func isRecord(v any) (*jsvalue.Object, bool) { o, ok := v.(*jsvalue.Object); return o, ok }

func isCommand(v any) bool {
	o, ok := isRecord(v)
	if !ok {
		return false
	}
	kind, _ := o.Get("kind")
	if kind == "show" {
		return true
	}
	tab, _ := o.Get("tab")
	target, has := o.Get("target")
	_, isStr := target.(string)
	return kind == "open" && panel.IsTabKind(tab) && (!has || isStr) && optionalBool(o, "auto") && optionalBool(o, "reload")
}

// optionalBool: the flag is absent or a boolean.
func optionalBool(o *jsvalue.Object, key string) bool {
	v, has := o.Get(key)
	_, isBool := v.(bool)
	return !has || isBool
}

// IsIntent says whether a stored value is a well-formed intent.
func IsIntent(v any) bool {
	o, ok := isRecord(v)
	if !ok {
		return false
	}
	id, _ := o.Get("id")
	_, idOK := id.(string)
	_, createdOK := o.Number("createdAt")
	cmd, _ := o.Get("command")
	at, _ := o.Get("appliedAt")
	_, atNum := at.(float64)
	on, _ := o.Get("appliedOn")
	return idOK && createdOK && isCommand(cmd) && (at == nil || atNum) && (on == nil || on == "desktop" || on == "phone")
}

func createdAt(intent *jsvalue.Object) float64 { n, _ := intent.Number("createdAt"); return n }

// Prune keeps the fresh intents, newest at the end, at most a tab's worth.
func Prune(intents []*jsvalue.Object, now time.Time) []*jsvalue.Object {
	cutoff := float64(now.Add(-TTL).UnixMilli())
	var kept []*jsvalue.Object
	for _, it := range intents {
		if createdAt(it) >= cutoff {
			kept = append(kept, it)
		}
	}
	if len(kept) > PerTab {
		kept = kept[len(kept)-PerTab:]
	}
	return kept
}

// Listener hears that a conversation's intents changed.
type Listener func(tabID string, intents []*jsvalue.Object)

// Store is the file of every conversation's intents.
type Store struct {
	file    string
	now     func() time.Time
	persist bool

	mu        sync.Mutex
	loaded    bool
	byTab     *jsvalue.Object // tab id → array of intents, in the order tabs came
	listeners map[int]Listener
	nextID    int
	saveTimer *time.Timer
}

// New keeps intents in file; now is the clock; persist false keeps them in memory only.
func New(file string, now func() time.Time, persist bool) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{file: file, now: now, persist: persist, byTab: jsvalue.NewObject(), listeners: map[int]Listener{}}
}

func toObjects(a []any) []*jsvalue.Object {
	var out []*jsvalue.Object
	for _, v := range a {
		if o, ok := v.(*jsvalue.Object); ok {
			out = append(out, o)
		}
	}
	return out
}

func toArray(list []*jsvalue.Object) []any {
	out := make([]any, len(list))
	for i, o := range list {
		out[i] = o
	}
	return out
}

func (s *Store) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	raw, err := os.ReadFile(s.file)
	if err != nil {
		return
	}
	v, err := jsvalue.Parse(raw)
	data, ok := v.(*jsvalue.Object)
	if err != nil || !ok {
		return
	}
	for _, tabID := range data.Keys() {
		items, isArr := func() ([]any, bool) { x, _ := data.Get(tabID); a, ok := x.([]any); return a, ok }()
		if !isArr || s.byTab.Has(tabID) {
			continue
		}
		var valid []*jsvalue.Object
		for _, it := range items {
			if IsIntent(it) {
				valid = append(valid, it.(*jsvalue.Object))
			}
		}
		if valid = Prune(valid, s.now()); len(valid) > 0 {
			s.byTab.Set(tabID, toArray(valid))
		}
	}
}

func (s *Store) current(tabID string) []*jsvalue.Object {
	kept := Prune(toObjects(s.byTab.Array(tabID)), s.now())
	if len(kept) == 0 {
		s.byTab.Delete(tabID)
	} else {
		s.byTab.Set(tabID, toArray(kept))
	}
	return kept
}

// save writes the file 200 ms after the last change, through a temp file, readable by the account only.
func (s *Store) save() {
	if !s.persist {
		return
	}
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(saveDelay, func() {
		s.mu.Lock()
		s.saveTimer = nil
		data := jsvalue.Stringify(s.byTab, "  ")
		s.mu.Unlock()
		tmp := fmt.Sprintf("%s.%d.tmp", s.file, os.Getpid())
		if err := os.MkdirAll(filepath.Dir(s.file), 0o777); err != nil {
			return
		}
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return
		}
		_ = os.Chmod(tmp, 0o600)
		_ = os.Rename(tmp, s.file)
	})
}

// changed saves, under the lock, and returns what tells the listeners, to run once the lock is released.
func (s *Store) changed(tabID string) func() {
	s.save()
	intents := s.current(tabID)
	listeners := make([]Listener, 0, len(s.listeners))
	for _, l := range s.listeners {
		listeners = append(listeners, l)
	}
	return func() {
		for _, l := range listeners {
			l(tabID, intents)
		}
	}
}

// Add keeps a command for the conversation and returns its intent.
func (s *Store) Add(tabID string, command *jsvalue.Object) *jsvalue.Object {
	s.mu.Lock()
	s.load()
	intent := jsvalue.NewObject("id", panel.UUID(), "command", command, "createdAt", s.now().UnixMilli(), "appliedAt", nil, "appliedOn", nil)
	s.byTab.Set(tabID, toArray(Prune(append(s.current(tabID), intent), s.now())))
	notify := s.changed(tabID)
	s.mu.Unlock()
	notify()
	return intent
}

// List is the conversation's live intents.
func (s *Store) List(tabID string) []*jsvalue.Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.current(tabID)
}

// MarkApplied marks the first application of each id; later ones and unknown ids change nothing.
func (s *Store) MarkApplied(tabID string, ids []string, device string) []*jsvalue.Object {
	s.mu.Lock()
	s.load()
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	touched := false
	cur := s.current(tabID)
	next := make([]*jsvalue.Object, len(cur))
	for i, it := range cur {
		at, _ := it.Get("appliedAt")
		if !wanted[it.String("id")] || at != nil {
			next[i] = it
			continue
		}
		touched = true
		c := it.Copy()
		c.Set("appliedAt", s.now().UnixMilli())
		c.Set("appliedOn", device)
		next[i] = c
	}
	if !touched {
		s.mu.Unlock()
		return next
	}
	s.byTab.Set(tabID, toArray(next))
	notify := s.changed(tabID)
	s.mu.Unlock()
	notify()
	return next
}

// Subscribe adds a listener; the function returned removes it.
func (s *Store) Subscribe(l Listener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	s.listeners[id] = l
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// Snapshot is every conversation's live intents.
func (s *Store) Snapshot() *jsvalue.Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	all := jsvalue.NewObject()
	for _, tabID := range s.byTab.Keys() {
		if intents := s.current(tabID); len(intents) > 0 {
			all.Set(tabID, toArray(intents))
		}
	}
	return all
}

// WaitForApplied is true once a page applied the intent, false when none did within d; the intent stays either way.
func (s *Store) WaitForApplied(tabID, id string, d time.Duration) bool {
	done := make(chan struct{}, 1)
	unsubscribe := s.Subscribe(func(changed string, intents []*jsvalue.Object) {
		if changed != tabID {
			return
		}
		for _, it := range intents {
			if at, _ := it.Get("appliedAt"); it.String("id") == id && at != nil {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}
	})
	defer unsubscribe()
	// A page may apply the intent between its broadcast and the subscription above.
	for _, it := range s.List(tabID) {
		if at, _ := it.Get("appliedAt"); it.String("id") == id && at != nil {
			return true
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}

var moscow = time.FixedZone("MSK", 3*3600)

func clock(ms float64) string { return time.UnixMilli(int64(ms)).In(moscow).Format("15:04") }

func describeCommand(cmd *jsvalue.Object) string {
	if cmd.String("kind") == "show" {
		return locale.Pick("show the pane", "показать панель")
	}
	s := locale.Pick("open \""+cmd.String("tab")+"\"", "открыть «"+cmd.String("tab")+"»")
	if reload, _ := cmd.Get("reload"); reload == true {
		s = locale.Pick("reload \""+cmd.String("tab")+"\"", "обновить «"+cmd.String("tab")+"»")
	}
	if t := cmd.String("target"); t != "" {
		s += " (" + t + ")"
	}
	return s
}

// Describe is one line per intent for the agent's status.
func Describe(intents []*jsvalue.Object) []string {
	var out []string
	for _, it := range intents {
		what := describeCommand(it.Object("command"))
		when := clock(createdAt(it))
		at, _ := it.Number("appliedAt")
		if v, _ := it.Get("appliedAt"); v == nil {
			out = append(out, when+" "+what+locale.Pick(" — waiting, it will open when the person comes to the conversation", " — ждёт, откроется, когда человек придёт в разговор"))
			continue
		}
		line := when + " " + what + locale.Pick(" — opened ", " — открыто ") + clock(at)
		if on, _ := it.Get("appliedOn"); on == "phone" {
			line += locale.Pick(" on the phone", " на телефоне")
		}
		out = append(out, line)
	}
	return out
}
