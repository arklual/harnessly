// Package devtools is the DevTools record of the preview slots: what a slot's page did on the network and in its
// console, and the conditions it runs under (throttling, blocked addresses, cache). The proxy and the page's bridge
// write, the panel, the CLI and the MCP server read. Rings, not logs: the newest requests and console entries per slot.
package devtools

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

const (
	NetworkLimit = 1000
	ConsoleLimit = 500
	maxText      = 4000
	maxStack     = 4000
	maxURL       = 2048
	maxShort     = 255
	// slotLimit bounds what a forged origin could add; a person has a handful of slots.
	slotLimit  = 32
	MaxBlocked = 50
	MaxPattern = 300
)

// slotOrigin is the shape of the gateway's slot hosts.
var slotOrigin = regexp.MustCompile(`^https://ai-[a-z][a-z0-9]{1,15}-[1-9]\.[a-z0-9-]+(\.[a-z0-9-]+)+$`)

// Target is what a slot shows: a local port or an external origin.
type Target struct {
	Kind   string
	Port   int
	Origin string
}

func (t Target) MarshalJSON() ([]byte, error) {
	if t.Kind == "port" {
		return marshal(struct {
			Kind string `json:"kind"`
			Port int    `json:"port"`
		}{t.Kind, t.Port}), nil
	}
	return marshal(struct {
		Kind   string `json:"kind"`
		Origin string `json:"origin"`
	}{t.Kind, t.Origin}), nil
}

// TargetOrigin is where the slot's page really lives.
func (t Target) TargetOrigin() string {
	if t.Kind == "port" {
		return "http://localhost:" + itoa(t.Port)
	}
	return t.Origin
}

type Throttle struct {
	Preset    string `json:"preset"`
	LatencyMs int64  `json:"latencyMs"`
	DownKbps  int64  `json:"downKbps"`
	UpKbps    int64  `json:"upKbps"`
}

// Presets are Chrome DevTools' own, so a number seen here means the same as there.
var Presets = map[string]Throttle{
	"none":    {"none", 0, 0, 0},
	"fast-4g": {"fast-4g", 165, 9000, 1500},
	"slow-4g": {"slow-4g", 563, 1600, 750},
	"3g":      {"3g", 2000, 400, 400},
	"offline": {"offline", 0, 0, 0},
}

type Settings struct {
	Throttle     Throttle `json:"throttle"`
	Blocked      []string `json:"blocked"`
	DisableCache bool     `json:"disableCache"`
}

func DefaultSettings() Settings { return Settings{Throttle: Presets["none"], Blocked: []string{}} }

// NetworkEntry is one request; Id grows by one per slot and is the cursor of `since`.
type NetworkEntry struct {
	At         int64   `json:"at"`
	Source     string  `json:"source"`
	Method     string  `json:"method"`
	URL        string  `json:"url"`
	Host       string  `json:"host"`
	Type       string  `json:"type"`
	Status     *int64  `json:"status"`
	Mime       *string `json:"mime"`
	Size       *int64  `json:"size"`
	DurationMs *int64  `json:"durationMs"`
	ThirdParty bool    `json:"thirdParty"`
	Blocked    bool    `json:"blocked"`
	Throttled  bool    `json:"throttled"`
	Error      *string `json:"error"`
	ID         int64   `json:"id"`
}

type ConsoleEntry struct {
	At     int64   `json:"at"`
	Level  string  `json:"level"`
	Source string  `json:"source"`
	Text   string  `json:"text"`
	Stack  *string `json:"stack"`
	URL    *string `json:"url"`
	ID     int64   `json:"id"`
	Count  int64   `json:"count"`
}

type SlotSummary struct {
	Slot     string   `json:"slot"`
	Target   *Target  `json:"target"`
	LastSeen int64    `json:"lastSeen"`
	Settings Settings `json:"settings"`
	Requests int      `json:"requests"`
	Errors   int      `json:"errors"`
}

type Snapshot struct {
	Slot     string         `json:"slot"`
	Target   *Target        `json:"target"`
	Settings Settings       `json:"settings"`
	Network  []NetworkEntry `json:"network"`
	Console  []ConsoleEntry `json:"console"`
	Cursor   int64          `json:"cursor"`
}

type slotRecord struct {
	slot     string
	target   *Target
	lastSeen int64
	settings Settings
	network  []NetworkEntry
	console  []ConsoleEntry
	seq      int64
}

// Store holds every slot's record of this process.
type Store struct {
	mu     sync.Mutex
	slots  map[string]*slotRecord
	order  []string // insertion order, as a Map iterates
	Now    func() time.Time
	quotas map[string]*quota
	links  map[string]*link
}

func NewStore() *Store {
	return &Store{slots: map[string]*slotRecord{}, quotas: map[string]*quota{}, links: map[string]*link{}, Now: time.Now}
}

func (s *Store) nowMs() int64 { return s.Now().UnixMilli() }

func clipText(v string, max int) string {
	if jscompat.Len(v) > max {
		return jscompat.Slice(v, max-1) + "…"
	}
	return v
}

func clipOptional(v *string, max int) *string {
	if v == nil {
		return nil
	}
	c := clipText(*v, max)
	return &c
}

func (s *Store) evictOldest() {
	if len(s.slots) <= slotLimit {
		return
	}
	all := s.recordsInOrder()
	sort.SliceStable(all, func(i, j int) bool { return all[i].lastSeen < all[j].lastSeen })
	for _, r := range all[:len(all)-slotLimit] {
		s.remove(r.slot)
	}
}

func (s *Store) remove(slot string) {
	delete(s.slots, slot)
	for i, o := range s.order {
		if o == slot {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *Store) recordsInOrder() []*slotRecord {
	out := make([]*slotRecord, 0, len(s.order))
	for _, o := range s.order {
		out = append(out, s.slots[o])
	}
	return out
}

func (s *Store) recordOf(slot string) *slotRecord {
	r := s.slots[slot]
	if r == nil {
		r = &slotRecord{slot: slot, lastSeen: s.nowMs(), settings: DefaultSettings()}
		s.slots[slot] = r
		s.order = append(s.order, slot)
		s.evictOldest()
	}
	return r
}

// Touch: the proxy names the slot's current target with every request it admits.
func (s *Store) Touch(slot string, target Target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.recordOf(slot)
	r.target = &target
	r.lastSeen = s.nowMs()
}

func (s *Store) Settings(slot string) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.slots[slot]; r != nil {
		return r.settings
	}
	return DefaultSettings()
}

// TargetOf is what the slot shows now, as the proxy last named it; nil before its first request.
func (s *Store) TargetOf(slot string) *Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.slots[slot]; r != nil {
		return r.target
	}
	return nil
}

// SetSettings applies a validated patch.
func (s *Store) SetSettings(slot string, p Patch) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.recordOf(slot)
	if p.Throttle != nil {
		r.settings.Throttle = *p.Throttle
	}
	if p.Blocked != nil {
		r.settings.Blocked = p.Blocked
	}
	if p.DisableCache != nil {
		r.settings.DisableCache = *p.DisableCache
	}
	return r.settings
}

// wildcardMatches matches `*` patterns piece by piece with indexOf, never as a regexp: linear in the URL.
func wildcardMatches(url, pattern string) bool {
	from := 0
	for _, piece := range strings.Split(pattern, "*") {
		if piece == "" {
			continue
		}
		at := strings.Index(url[from:], piece)
		if at < 0 {
			return false
		}
		from += at + len(piece)
	}
	return true
}

// BlockedBy is the first pattern found anywhere in the full URL, case-insensitively; "" when none.
func BlockedBy(url string, patterns []string) string {
	lower := strings.ToLower(url)
	for _, p := range patterns {
		if wildcardMatches(lower, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

func (e NetworkEntry) clipped() NetworkEntry {
	e.URL = clipText(e.URL, maxURL)
	e.Method = clipText(e.Method, 16)
	e.Host = clipText(e.Host, maxShort)
	e.Mime = clipOptional(e.Mime, maxShort)
	e.Error = clipOptional(e.Error, maxShort)
	return e
}

func pushRing[T any](ring []T, item T, limit int) []T {
	ring = append(ring, item)
	if len(ring) > limit {
		ring = append([]T(nil), ring[len(ring)-limit:]...)
	}
	return ring
}

// RecordRequest adds an entry and returns its id.
func (s *Store) RecordRequest(slot string, e NetworkEntry) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.recordOf(slot)
	r.lastSeen = s.nowMs()
	r.seq++
	e = e.clipped()
	e.ID = r.seq
	r.network = pushRing(r.network, e, NetworkLimit)
	return e.ID
}

// Finish fills in the answer of a pending request. It moves to the end of the ring with a fresh id, so `since` readers
// see it again once it is finished.
func (s *Store) Finish(slot string, id int64, patch func(*NetworkEntry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.slots[slot]
	if r == nil {
		return
	}
	for i, e := range r.network {
		if e.ID != id {
			continue
		}
		r.network = append(r.network[:i:i], r.network[i+1:]...)
		patch(&e)
		e.Mime = clipOptional(e.Mime, maxShort)
		e.Error = clipOptional(e.Error, maxShort)
		r.seq++
		e.ID = r.seq
		r.network = pushRing(r.network, e, NetworkLimit)
		return
	}
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// RecordConsole adds a console entry; one identical to the last folds into it, as in Chrome.
func (s *Store) RecordConsole(slot string, e ConsoleEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.recordOf(slot)
	r.lastSeen = s.nowMs()
	e.Text = clipText(e.Text, maxText)
	e.Stack = clipOptional(e.Stack, maxStack)
	e.URL = clipOptional(e.URL, maxURL)
	if n := len(r.console); n > 0 {
		last := r.console[n-1]
		if last.Level == e.Level && last.Source == e.Source && last.Text == e.Text && eqPtr(last.Stack, e.Stack) {
			r.console = r.console[:n-1]
			last.At, last.URL, last.Count = e.At, e.URL, last.Count+1
			r.seq++
			last.ID = r.seq
			r.console = pushRing(r.console, last, ConsoleLimit)
			return
		}
	}
	r.seq++
	e.ID, e.Count = r.seq, 1
	r.console = pushRing(r.console, e, ConsoleLimit)
}

func (s *Store) Clear(slot string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.slots[slot]; r != nil {
		r.network, r.console = nil, nil
	}
}

func (s *Store) Read(slot string, since int64) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.slots[slot]
	if r == nil {
		return Snapshot{Slot: slot, Settings: DefaultSettings(), Network: []NetworkEntry{}, Console: []ConsoleEntry{}}
	}
	out := Snapshot{Slot: slot, Target: r.target, Settings: r.settings, Network: []NetworkEntry{}, Console: []ConsoleEntry{}, Cursor: r.seq}
	for _, e := range r.network {
		if e.ID > since {
			out.Network = append(out.Network, e)
		}
	}
	for _, e := range r.console {
		if e.ID > since {
			out.Console = append(out.Console, e)
		}
	}
	return out
}

func isFailed(e NetworkEntry) bool {
	return e.Blocked || e.Error != nil || e.Status != nil && *e.Status >= 400
}

// List is every slot, newest first.
func (s *Store) List() []SlotSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list()
}

func (s *Store) list() []SlotSummary {
	all := s.recordsInOrder()
	sort.SliceStable(all, func(i, j int) bool { return all[i].lastSeen > all[j].lastSeen })
	out := make([]SlotSummary, 0, len(all))
	for _, r := range all {
		errs := 0
		for _, e := range r.network {
			if isFailed(e) {
				errs++
			}
		}
		for _, e := range r.console {
			if e.Level == "error" {
				errs++
			}
		}
		out = append(out, SlotSummary{Slot: r.slot, Target: r.target, LastSeen: r.lastSeen, Settings: r.settings, Requests: len(r.network), Errors: errs})
	}
	return out
}

// Resolve is the slot the caller named, or the one the person used last: agents rarely know the slot's name.
func (s *Store) Resolve(asked string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if asked != "" {
		n := strings.ToLower(strings.TrimRight(asked, "/"))
		if _, ok := s.slots[n]; ok || slotOrigin.MatchString(n) {
			return n
		}
		return ""
	}
	if l := s.list(); len(l) > 0 {
		return l[0].Slot
	}
	return ""
}
