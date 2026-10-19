// Package economics is the tokens, time and list-price cost of one agent conversation, read from its transcripts on
// demand. Each transcript keeps a cursor (device, inode, offset, unfinished tail) and the counters so far, in memory and
// in <data>/economics/<sha1(path)>.json, so a request reads only what was appended since the last one; the file is the
// one the previous runtime wrote and read, so both runtimes go on from each other's cursors.
package economics

import (
	"math"
	"strconv"
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// Usage is the raw counters of one model; cache writes are split by lifetime because they are priced apart.
type Usage struct {
	Input         float64 `json:"input"`
	Output        float64 `json:"output"`
	CacheRead     float64 `json:"cacheRead"`
	CacheWrite5m  float64 `json:"cacheWrite5m"`
	CacheWrite1h  float64 `json:"cacheWrite1h"`
	Reasoning     float64 `json:"reasoning"`
	ReasoningSeen bool    `json:"reasoningSeen"`
}

// Models are the counters by model name, in the order JavaScript enumerates the keys of an object.
type Models struct {
	keys []string
	m    map[string]*Usage
}

func (ms *Models) get(name string) *Usage {
	if ms.m == nil {
		ms.m = map[string]*Usage{}
	}
	u, ok := ms.m[name]
	if !ok {
		u = &Usage{}
		ms.m[name] = u
		ms.keys = append(ms.keys, name)
	}
	return u
}

// Keys in JavaScript's order: array-index names ascending, then the rest as they came.
func (ms *Models) Keys() []string {
	o := jsvalue.NewObject()
	for _, k := range ms.keys {
		o.Set(k, true)
	}
	return o.Keys()
}

// Each calls fn for every model in key order.
func (ms *Models) Each(fn func(name string, u *Usage)) {
	for _, k := range ms.Keys() {
		fn(k, ms.m[k])
	}
}

// Len is the number of models.
func (ms *Models) Len() int { return len(ms.keys) }

func (ms *Models) clone() Models {
	c := Models{keys: append([]string(nil), ms.keys...), m: make(map[string]*Usage, len(ms.m))}
	for k, u := range ms.m {
		cp := *u
		c.m[k] = &cp
	}
	return c
}

// MarshalJSON writes the models as one object.
func (ms Models) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range ms.Keys() {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(jsvalue.Stringify(k, ""))
		b.WriteByte(':')
		b.Write(usageJSON(ms.m[k]))
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func usageJSON(u *Usage) []byte {
	return jsvalue.Stringify(jsvalue.NewObject("input", u.Input, "output", u.Output, "cacheRead", u.CacheRead, "cacheWrite5m", u.CacheWrite5m,
		"cacheWrite1h", u.CacheWrite1h, "reasoning", u.Reasoning, "reasoningSeen", u.ReasoningSeen), "")
}

// CodexTotals are the cumulative counters of Codex's last token_count.
type CodexTotals struct {
	Input     float64 `json:"input"`
	Cached    float64 `json:"cached"`
	Output    float64 `json:"output"`
	Reasoning float64 `json:"reasoning"`
}

// State is one transcript's cursor and counters, as the cursor file holds it.
type State struct {
	Schema    int      `json:"schema"`
	Path      string   `json:"path"`
	Provider  string   `json:"provider"`
	Dev       float64  `json:"dev"`
	Ino       float64  `json:"ino"`
	Offset    int64    `json:"offset"`
	Carry     string   `json:"carry"`
	RecentIDs []any    `json:"recentIds"`
	Models    Models   `json:"models"`
	Turns     float64  `json:"turns"`
	ActiveMs  float64  `json:"activeMs"`
	FirstAt   *float64 `json:"firstAt"`
	LastAt    *float64 `json:"lastAt"`
	// ClaudeCodeCostUsd is Claude Code's own running total (cost-state lines).
	ClaudeCodeCostUsd *float64 `json:"claudeCodeCostUsd"`
	// CodexLast and CodexModel: Codex's cumulative counters at the last token_count and the model they go to.
	CodexLast  *CodexTotals `json:"codexLast"`
	CodexModel *string      `json:"codexModel"`
}

// NewState is a transcript read from its start.
func NewState(path, provider string) *State {
	return &State{Schema: 1, Path: path, Provider: provider, RecentIDs: []any{}}
}

func (s *State) clone() *State {
	c := *s
	c.RecentIDs = append([]any{}, s.RecentIDs...)
	c.Models = s.Models.clone()
	if s.CodexLast != nil {
		cl := *s.CodexLast
		c.CodexLast = &cl
	}
	return &c
}

// stateFromJSON reads a cursor file: one for this path and provider, else nil.
func stateFromJSON(raw []byte, path, provider string) *State {
	v, err := jsvalue.Parse(raw)
	o, _ := v.(*jsvalue.Object)
	if err != nil || o == nil {
		return nil
	}
	schema, _ := o.Number("schema")
	offset, isNum := o.Number("offset")
	if schema != 1 || o.String("path") != path || o.String("provider") != provider || !isNum {
		return nil
	}
	if _, isStr := mustGet(o, "path").(string); !isStr {
		return nil
	}
	num := func(k string) float64 { n, _ := o.Number(k); return n }
	nullable := func(k string) *float64 {
		if n, ok := o.Number(k); ok {
			return &n
		}
		return nil
	}
	s := &State{Schema: 1, Path: path, Provider: provider, Dev: num("dev"), Ino: num("ino"), Offset: int64(offset), Carry: o.String("carry"),
		RecentIDs: append([]any{}, o.Array("recentIds")...), Turns: num("turns"), ActiveMs: num("activeMs"), FirstAt: nullable("firstAt"),
		LastAt: nullable("lastAt"), ClaudeCodeCostUsd: nullable("claudeCodeCostUsd")}
	if models := o.Object("models"); models != nil {
		for _, k := range models.Keys() {
			m := models.Object(k)
			u := s.Models.get(k)
			if m == nil {
				continue
			}
			u.Input, _ = m.Number("input")
			u.Output, _ = m.Number("output")
			u.CacheRead, _ = m.Number("cacheRead")
			u.CacheWrite5m, _ = m.Number("cacheWrite5m")
			u.CacheWrite1h, _ = m.Number("cacheWrite1h")
			u.Reasoning, _ = m.Number("reasoning")
			seen, _ := mustGet(m, "reasoningSeen").(bool)
			u.ReasoningSeen = seen
		}
	}
	if last := o.Object("codexLast"); last != nil {
		t := CodexTotals{}
		t.Input, _ = last.Number("input")
		t.Cached, _ = last.Number("cached")
		t.Output, _ = last.Number("output")
		t.Reasoning, _ = last.Number("reasoning")
		s.CodexLast = &t
	}
	if m, ok := mustGet(o, "codexModel").(string); ok {
		s.CodexModel = &m
	}
	return s
}

func mustGet(o *jsvalue.Object, k string) any {
	v, _ := o.Get(k)
	return v
}

// get is v[k] for a value read from JSON: undefined (nil) for anything but an object's key.
func get(v any, k string) any {
	if o, ok := v.(*jsvalue.Object); ok {
		return mustGet(o, k)
	}
	return nil
}

// num is a counter: a positive finite number, else 0.
func num(v any) float64 {
	if f, ok := v.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) && f > 0 {
		return f
	}
	return 0
}

func (s *State) stamp(timestamp any) {
	str, ok := timestamp.(string)
	if !ok {
		return
	}
	at := jscompat.ParseDate(str)
	if math.IsNaN(at) {
		return
	}
	if s.FirstAt == nil || at < *s.FirstAt {
		s.FirstAt = &at
	}
	if s.LastAt == nil || at > *s.LastAt {
		s.LastAt = &at
	}
}

// sameID is Array#includes of a message id: objects never equal one another.
func sameID(a, b any) bool {
	switch a.(type) {
	case string, float64, bool:
		return a == b
	}
	return false
}

const recentIDs = 256

func (s *State) applyClaude(line *jsvalue.Object) {
	typ := mustGet(line, "type")
	if typ == "system" && mustGet(line, "subtype") == "turn_duration" {
		s.ActiveMs += num(mustGet(line, "durationMs"))
		s.Turns++
		s.stamp(mustGet(line, "timestamp"))
		return
	}
	if typ == "cost-state" {
		if c, ok := mustGet(line, "totalCostUSD").(float64); ok && !math.IsNaN(c) && !math.IsInf(c, 0) {
			s.ClaudeCodeCostUsd = &c
		}
		return
	}
	message := mustGet(line, "message")
	usage := get(message, "usage")
	if typ != "assistant" || !jsvalue.Truthy(usage) {
		return
	}
	// Claude Code writes one line per content block, each with the whole message's usage.
	if id := get(message, "id"); jsvalue.Truthy(id) {
		for _, seen := range s.RecentIDs {
			if sameID(seen, id) {
				return
			}
		}
		s.RecentIDs = append(s.RecentIDs, id)
		if len(s.RecentIDs) > recentIDs {
			s.RecentIDs = s.RecentIDs[1:]
		}
	}
	s.stamp(mustGet(line, "timestamp"))
	cacheWrite := num(get(usage, "cache_creation_input_tokens"))
	split := get(usage, "cache_creation")
	write1h, write5m := 0.0, cacheWrite // without the split every cache write is the default 5-minute one
	if jsvalue.Truthy(split) {
		write1h, write5m = num(get(split, "ephemeral_1h_input_tokens")), num(get(split, "ephemeral_5m_input_tokens"))
	}
	input, output, cacheRead := num(get(usage, "input_tokens")), num(get(usage, "output_tokens")), num(get(usage, "cache_read_input_tokens"))
	if input == 0 && output == 0 && cacheRead == 0 && cacheWrite == 0 {
		return
	}
	name := "unknown"
	if model := get(message, "model"); jsvalue.Truthy(model) {
		name = jscompat.String(model)
	}
	u := s.Models.get(name)
	u.Input += input
	u.Output += output
	u.CacheRead += cacheRead
	u.CacheWrite5m += write5m
	u.CacheWrite1h += write1h
	if thinking, ok := get(get(usage, "output_tokens_details"), "thinking_tokens").(float64); ok {
		u.Reasoning += num(thinking)
		u.ReasoningSeen = true
	}
}

func (s *State) applyCodex(line *jsvalue.Object) {
	payload := mustGet(line, "payload")
	if !jsvalue.Truthy(payload) {
		return
	}
	typ := mustGet(line, "type")
	if model, ok := get(payload, "model").(string); typ == "turn_context" && ok {
		s.CodexModel = &model
		return
	}
	if typ != "event_msg" {
		return
	}
	ptype := get(payload, "type")
	if ptype == "task_complete" {
		s.ActiveMs += num(get(payload, "duration_ms"))
		s.Turns++
		s.stamp(mustGet(line, "timestamp"))
		return
	}
	if ptype != "token_count" {
		return
	}
	total := get(get(payload, "info"), "total_token_usage")
	if !jsvalue.Truthy(total) {
		return
	}
	s.stamp(mustGet(line, "timestamp"))
	// Counters are cumulative for the session: the step since the previous token_count goes to the current model.
	next := CodexTotals{Input: num(get(total, "input_tokens")), Cached: num(get(total, "cached_input_tokens")),
		Output: num(get(total, "output_tokens")), Reasoning: num(get(total, "reasoning_output_tokens"))}
	prev := CodexTotals{}
	if s.CodexLast != nil {
		prev = *s.CodexLast
	}
	base := prev
	if next.Input < prev.Input || next.Output < prev.Output {
		base = CodexTotals{}
	}
	name := "unknown"
	if s.CodexModel != nil {
		name = *s.CodexModel
	}
	u := s.Models.get(name)
	cachedStep := math.Max(0, next.Cached-base.Cached)
	u.Input += math.Max(0, next.Input-base.Input-cachedStep)
	u.CacheRead += cachedStep
	u.Output += math.Max(0, next.Output-base.Output)
	u.Reasoning += math.Max(0, next.Reasoning-base.Reasoning)
	u.ReasoningSeen = true
	s.CodexLast = &next
}

// Cheap filters before parsing: huge tool results never hold these in a line worth reading.
var (
	claudeMarkers = []string{`"assistant"`, `"turn_duration"`, `"cost-state"`}
	codexMarkers  = []string{`"token_count"`, `"task_complete"`, `"turn_context"`}
)

// consume feeds text appended to the transcript; an unfinished last line waits in Carry for the next read.
func (s *State) consume(text string) {
	lines := strings.Split(s.Carry+text, "\n")
	s.Carry = lines[len(lines)-1]
	markers := codexMarkers
	if s.Provider == "claude" {
		markers = claudeMarkers
	}
	for _, raw := range lines[:len(lines)-1] {
		hit := false
		for _, m := range markers {
			if strings.Contains(raw, m) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		v, err := jsvalue.Parse([]byte(raw))
		line, ok := v.(*jsvalue.Object)
		if err != nil || !ok {
			continue
		}
		if s.Provider == "claude" {
			s.applyClaude(line)
		} else {
			s.applyCodex(line)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
