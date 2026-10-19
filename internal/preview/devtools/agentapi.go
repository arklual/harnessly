package devtools

import (
	"math"
	"strconv"
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// What an agent can do with a slot's DevTools record: read the network and console logs and the dev server's terminal,
// and change the throttling, blocked addresses and cache. The CLI routes and the MCP server both call these, so the
// filters, limits and messages are the same on both.

const (
	DefaultLimit   = 100
	MaxLimit       = 500
	MaxServerLines = 1000
)

// Outcome is a value, or a refusal with its status and words.
type Outcome struct {
	Value  any
	Status int
	Error  string
}

func (o Outcome) OK() bool { return o.Error == "" && o.Status == 0 }

func fail(status int, msg string) Outcome { return Outcome{Status: status, Error: msg} }

// API is the agents' view of a store.
type API struct {
	Store *Store
	// CLI is the command people and agents type ("acme"), named in the messages.
	CLI string
	// ServerLog reads the dev server's terminal of a slot.
	ServerLog func(slot string, all bool, lines int) (ServerLog, error)
}

func (a *API) noSlotMessage() string {
	return "Превью ещё не открывали — откройте страницу в браузере панели: " + a.CLI + " panel open browser <port>"
}

func (a *API) slotOf(asked string) (string, Outcome) {
	if slot := a.Store.Resolve(asked); slot != "" {
		return slot, Outcome{}
	}
	if asked != "" {
		return "", fail(404, "Слот не найден: "+asked+". Список слотов: "+a.CLI+" preview slots")
	}
	return "", fail(404, a.noSlotMessage())
}

type SlotsView struct {
	Slots []SlotSummary `json:"slots"`
}

func (a *API) Slots() SlotsView { return SlotsView{Slots: a.Store.List()} }

type NetworkQuery struct {
	Since      int64
	Errors     bool
	ThirdParty bool
	URL        string
	Types      []string
	Limit      int
}

type NetworkView struct {
	Slot     string   `json:"slot"`
	Target   *Target  `json:"target"`
	Settings Settings `json:"settings"`
	Cursor   int64    `json:"cursor"`
	Matched  int      `json:"matched"`
	Entries  any      `json:"entries"`
}

func newest[T any](items []T, limit int) []T {
	if limit == 0 {
		limit = DefaultLimit
	}
	if len(items) > limit {
		return items[len(items)-limit:]
	}
	return items
}

func filterNetwork(entries []NetworkEntry, q NetworkQuery) []NetworkEntry {
	needle := strings.ToLower(q.URL)
	out := []NetworkEntry{}
	for _, e := range entries {
		if q.Errors && !isFailed(e) || q.ThirdParty && !e.ThirdParty || needle != "" && !strings.Contains(strings.ToLower(e.URL), needle) ||
			len(q.Types) > 0 && !contains(q.Types, e.Type) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (a *API) Network(asked string, q NetworkQuery) Outcome {
	slot, refused := a.slotOf(asked)
	if slot == "" {
		return refused
	}
	snap := a.Store.Read(slot, q.Since)
	matched := filterNetwork(snap.Network, q)
	return Outcome{Value: NetworkView{Slot: slot, Target: snap.Target, Settings: snap.Settings, Cursor: snap.Cursor, Matched: len(matched), Entries: newest(matched, q.Limit)}}
}

type ConsoleQuery struct {
	Since int64
	Level string
	Limit int
}

type ConsoleView struct {
	Slot    string  `json:"slot"`
	Target  *Target `json:"target"`
	Cursor  int64   `json:"cursor"`
	Matched int     `json:"matched"`
	Entries any     `json:"entries"`
}

var levelRank = map[string]int{"log": 0, "info": 1, "warn": 2, "error": 3}

func (a *API) Console(asked string, q ConsoleQuery) Outcome {
	slot, refused := a.slotOf(asked)
	if slot == "" {
		return refused
	}
	snap := a.Store.Read(slot, q.Since)
	floor := levelRank[q.Level]
	matched := []ConsoleEntry{}
	for _, e := range snap.Console {
		if levelRank[e.Level] >= floor {
			matched = append(matched, e)
		}
	}
	return Outcome{Value: ConsoleView{Slot: slot, Target: snap.Target, Cursor: snap.Cursor, Matched: len(matched), Entries: newest(matched, q.Limit)}}
}

type ServerLogView struct {
	Slot   string  `json:"slot"`
	Target *Target `json:"target"`
	ServerLog
}

func (a *API) ServerLogOf(asked string, all bool, lines int) Outcome {
	slot, refused := a.slotOf(asked)
	if slot == "" {
		return refused
	}
	log, err := a.ServerLog(slot, all, lines)
	if err != nil {
		return fail(500, "Не удалось прочитать терминал dev-сервера")
	}
	return Outcome{Value: ServerLogView{Slot: slot, Target: a.Store.TargetOf(slot), ServerLog: log}}
}

type SettingsView struct {
	Slot     string   `json:"slot"`
	Target   *Target  `json:"target"`
	Settings Settings `json:"settings"`
}

func (a *API) SettingsOf(asked string) Outcome {
	slot, refused := a.slotOf(asked)
	if slot == "" {
		return refused
	}
	snap := a.Store.Read(slot, 0)
	return Outcome{Value: SettingsView{Slot: slot, Target: snap.Target, Settings: snap.Settings}}
}

type BlockedChanges struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Missing []string `json:"missing"`
}

type SettingsChangeView struct {
	Slot     string         `json:"slot"`
	Target   *Target        `json:"target"`
	Settings Settings       `json:"settings"`
	Changes  BlockedChanges `json:"changes"`
}

func patternList(v any, present bool, field string) ([]string, string) {
	if !present {
		return []string{}, ""
	}
	list, ok := v.([]any)
	if ok {
		for _, item := range list {
			s, isString := item.(string)
			if !isString || jscompat.Trim(s) == "" || jscompat.Len(jscompat.Trim(s)) > MaxPattern {
				ok = false
				break
			}
		}
	}
	if !ok {
		return nil, field + ": список шаблонов адреса, каждый до " + itoa(MaxPattern) + " символов"
	}
	out := []string{}
	seen := map[string]bool{}
	for _, item := range list {
		s := jscompat.Trim(item.(string))
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, ""
}

// ChangeSettings applies what the panel sends (throttle, blocked, disableCache) plus blockedAdd and blockedRemove,
// which edit the current list in one step instead of replacing it.
func (a *API) ChangeSettings(asked string, body any) Outcome {
	slot, refused := a.slotOf(asked)
	if slot == "" {
		return refused
	}
	raw, ok := body.(*jsvalue.Object)
	if !ok {
		return fail(400, "Нужен объект настроек")
	}
	patch, msg := ParseSettingsPatch(raw)
	if msg != "" {
		return fail(400, msg)
	}
	addRaw, addPresent := raw.Get("blockedAdd")
	add, msg := patternList(addRaw, addPresent, "blockedAdd")
	if msg != "" {
		return fail(400, msg)
	}
	removeRaw, removePresent := raw.Get("blockedRemove")
	remove, msg := patternList(removeRaw, removePresent, "blockedRemove")
	if msg != "" {
		return fail(400, msg)
	}
	base := patch.Blocked
	if base == nil {
		base = a.Store.Read(slot, 0).Settings.Blocked
	}
	if len(add) > 0 || len(remove) > 0 {
		kept := []string{}
		for _, p := range base {
			if !contains(remove, p) {
				kept = append(kept, p)
			}
		}
		next := append([]string{}, kept...)
		for _, p := range add {
			if !contains(kept, p) {
				next = append(next, p)
			}
		}
		if len(next) > MaxBlocked {
			return fail(400, "Не больше "+itoa(MaxBlocked)+" заблокированных шаблонов")
		}
		patch.Blocked = next
	}
	if patch.empty() {
		return fail(400, "Нечего менять: нужны throttle, blocked, blockedAdd, blockedRemove или disableCache")
	}
	settings := a.Store.SetSettings(slot, patch)
	ch := BlockedChanges{Added: []string{}, Removed: []string{}, Missing: []string{}}
	for _, p := range add {
		if !contains(base, p) {
			ch.Added = append(ch.Added, p)
		}
	}
	for _, p := range remove {
		if contains(base, p) {
			ch.Removed = append(ch.Removed, p)
		} else {
			ch.Missing = append(ch.Missing, p)
		}
	}
	return Outcome{Value: SettingsChangeView{Slot: slot, Target: a.Store.TargetOf(slot), Settings: settings, Changes: ch}}
}

type ClearedView struct {
	Slot    string `json:"slot"`
	Cleared bool   `json:"cleared"`
}

func (a *API) ClearLogs(asked string) Outcome {
	slot, refused := a.slotOf(asked)
	if slot == "" {
		return refused
	}
	a.Store.Clear(slot)
	return Outcome{Value: ClearedView{Slot: slot, Cleared: true}}
}

// Query is a handler's req.query: every value of a key, in order.
type Query map[string][]string

// single is the first value of a key; "" counts as absent.
func (q Query) single(key string) (string, bool) {
	v := q[key]
	if len(v) == 0 || v[0] == "" {
		return "", false
	}
	return v[0], true
}

func (q Query) flag(key string) bool {
	v, _ := q.single(key)
	return v == "1" || v == "true"
}

// Slot is the ?slot= an agent route names.
func (q Query) Slot() string {
	v, _ := q.single("slot")
	return v
}

// JSNumber is Number(text) for a query value.
func JSNumber(text string) float64 {
	t := jscompat.Trim(text)
	if t == "" {
		return 0
	}
	lower := strings.ToLower(t)
	if len(t) > 2 && t[0] == '0' && (lower[1] == 'x' || lower[1] == 'o' || lower[1] == 'b') {
		base := map[byte]int{'x': 16, 'o': 8, 'b': 2}[lower[1]]
		if n, err := strconv.ParseUint(t[2:], base, 64); err == nil {
			return float64(n)
		}
		return math.NaN()
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	for _, c := range lower {
		if !(c >= '0' && c <= '9' || c == '.' || c == 'e' || c == '+' || c == '-') {
			return math.NaN()
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	return f
}

func isInteger(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) }

// whole: absent (ok, set false), or given and a whole number in range (ok), or not (bad).
func whole(text string, given bool, min, max float64) (v float64, set bool, bad bool) {
	if !given {
		return 0, false, false
	}
	f := JSNumber(text)
	if isInteger(f) && f >= min && f <= max {
		return f, true, false
	}
	return 0, false, true
}

const (
	sinceError = "since: целое число от 0, курсор прошлого ответа"
	maxSafe    = 9007199254740991
)

func limitError() string { return "limit: целое число от 1 до " + itoa(MaxLimit) }

// ParseNetworkQuery is the HTTP form: since, errors=1, thirdParty=1, url, type (comma-separated), limit.
func ParseNetworkQuery(q Query) (NetworkQuery, string) {
	var out NetworkQuery
	t, g := q.single("since")
	since, _, bad := whole(t, g, 0, maxSafe)
	if bad {
		return out, sinceError
	}
	t, g = q.single("limit")
	limit, _, bad := whole(t, g, 1, MaxLimit)
	if bad {
		return out, limitError()
	}
	if raw, ok := q.single("type"); ok {
		for _, part := range strings.Split(raw, ",") {
			part = strings.ToLower(jscompat.Trim(part))
			if part == "" {
				continue
			}
			if !contains(resourceTypes, part) {
				return out, "type: " + strings.Join(resourceTypes, ", ") + "; несколько через запятую"
			}
			out.Types = append(out.Types, part)
		}
	}
	out.Since, out.Limit = int64(since), int(limit)
	out.Errors, out.ThirdParty = q.flag("errors"), q.flag("thirdParty")
	out.URL, _ = q.single("url")
	return out, ""
}

func ParseConsoleQuery(q Query) (ConsoleQuery, string) {
	var out ConsoleQuery
	t, g := q.single("since")
	since, _, bad := whole(t, g, 0, maxSafe)
	if bad {
		return out, sinceError
	}
	t, g = q.single("limit")
	limit, _, bad := whole(t, g, 1, MaxLimit)
	if bad {
		return out, limitError()
	}
	if level, ok := q.single("level"); ok {
		level = strings.ToLower(level)
		if !contains(consoleLevels, level) {
			return out, "level: " + strings.Join(consoleLevels, ", ")
		}
		out.Level = level
	}
	out.Since, out.Limit = int64(since), int(limit)
	return out, ""
}

func ParseServerLogQuery(q Query) (bool, int, string) {
	t, g := q.single("lines")
	lines, _, bad := whole(t, g, 1, MaxServerLines)
	if bad {
		return false, 0, "lines: целое число от 1 до " + itoa(MaxServerLines)
	}
	return q.flag("all"), int(lines), ""
}

const (
	mcpURLMax   = 500
	mcpTextMax  = 1000
	mcpStackMax = 1500
)

func isoOf(ms int64) string {
	s, _ := jscompat.ISOString(float64(ms))
	return s
}

// compactNetwork is what the MCP tools return for a request: no nulls, no false flags, the time in words.
func compactNetwork(e NetworkEntry) *jsvalue.Object {
	o := jsvalue.NewObject("id", float64(e.ID), "at", isoOf(e.At), "method", e.Method, "url", clipText(e.URL, mcpURLMax), "type", e.Type)
	if e.Status != nil {
		o.Set("status", float64(*e.Status))
	}
	if e.Mime != nil && *e.Mime != "" {
		o.Set("mime", *e.Mime)
	}
	if e.Size != nil {
		o.Set("bytes", float64(*e.Size))
	}
	if e.DurationMs != nil {
		o.Set("ms", float64(*e.DurationMs))
	}
	if e.ThirdParty {
		o.Set("thirdParty", true)
	}
	if e.Blocked {
		o.Set("blocked", true)
	}
	if e.Throttled {
		o.Set("throttled", true)
	}
	if e.Error != nil && *e.Error != "" {
		o.Set("error", *e.Error)
	}
	return o
}

func compactConsole(e ConsoleEntry) *jsvalue.Object {
	o := jsvalue.NewObject("id", float64(e.ID), "at", isoOf(e.At), "level", e.Level, "source", e.Source, "text", clipText(e.Text, mcpTextMax))
	if e.Count > 1 {
		o.Set("count", float64(e.Count))
	}
	if e.Stack != nil && *e.Stack != "" {
		o.Set("stack", clipText(*e.Stack, mcpStackMax))
	}
	if e.URL != nil && *e.URL != "" {
		o.Set("page", clipText(*e.URL, mcpURLMax))
	}
	return o
}
