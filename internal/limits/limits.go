// Package limits keeps the machine's subscription limits: rate-limits.json, one entry per provider merged field by
// field from every source that measures them (the agent's statusline, codexbar, Codex's usage endpoint, the Codex
// transcript), its watcher, and limits-history.jsonl for the burn-rate forecast.
package limits

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// Data is one provider's entry as it is stored: a JSON object whose fields beyond the windows are optional, so writers
// merge field by field and fields a writer does not know survive.
type Data = map[string]any

// Providers are the entries rate-limits.json holds.
var Providers = []string{"claude", "codex"}

// Sources.
const (
	SourceCodexbar   = "codexbar"
	SourceCodexUsage = "codex-usage"
)

// PrecedenceSec is how long a fresh value of a usage endpoint outranks the statusline and the Codex transcript: those
// echo what the CLI saw on its last request and would only make the percentage jump back and forth.
const PrecedenceSec = 180

var windowKeys = []string{"five_hour", "seven_day"}

func polled(source any) bool   { return source == SourceCodexbar || source == SourceCodexUsage }
func complete(source any) bool { return source == SourceCodexUsage } // lists every window the plan has

// num is a field as a number; NaN when it is not one (JavaScript arithmetic on undefined gives NaN too).
func num(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return math.NaN()
}

// orZero is `v ?? 0` for a numeric field.
func orZero(v any) float64 {
	if v == nil {
		return 0
	}
	return num(v)
}

// jsMax is Math.max: NaN as soon as one argument is.
func jsMax(xs ...float64) float64 {
	m := math.Inf(-1)
	for _, x := range xs {
		if math.IsNaN(x) {
			return x
		}
		if x > m {
			m = x
		}
	}
	return m
}

// numValue stores a computed number: NaN is written as null by JSON.stringify.
func numValue(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}

func object(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func copyObject(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// windowAt is when a window was measured: its own `at`, else the provider's `ts`.
func windowAt(w map[string]any, providerTs float64) float64 {
	if w["at"] == nil {
		return providerTs
	}
	return num(w["at"])
}

func newerWindow(prev any, prevTs float64, next map[string]any, nextTs float64) bool {
	p, ok := object(prev)
	if !ok || !truthy(prev) {
		return true
	}
	prevAt, nextAt := windowAt(p, prevTs), windowAt(next, nextTs)
	if nextAt < prevAt {
		return false
	}
	outranked := polled(p["source"]) && !polled(next["source"]) && nextAt-prevAt < PrecedenceSec
	return !outranked
}

// stamped is a window as stored: with its measurement time and, when it has none, the source of its provider. The time
// is `window.at ?? provider.ts` as written: a provider without ts leaves the window without at, a null ts writes null.
func stamped(w map[string]any, provider Data, source any) map[string]any {
	out := copyObject(w)
	if w["at"] == nil {
		if ts, has := provider["ts"]; has {
			out["at"] = ts
		} else {
			delete(out, "at")
		}
	}
	if truthy(source) && !truthy(w["source"]) {
		out["source"] = source
	}
	return out
}

// Merge is one provider's merge: a window or a codexbar field is replaced only by a value measured later, so the
// statusline never wipes codexbar's plan, pace or extra windows, and a stale copy never overwrites a fresh one.
func Merge(prev, next Data) Data {
	nextTs := num(next["ts"])
	if prev == nil {
		out := copyObject(next)
		for _, k := range windowKeys {
			v, has := next[k]
			if !has {
				delete(out, k)
				continue
			}
			if w, ok := object(v); ok {
				out[k] = stamped(w, next, next["source"])
			}
		}
		return out
	}
	prevTs := num(prev["ts"])
	merged := copyObject(prev)
	merged["ts"] = numValue(jsMax(prevTs, nextTs))
	for _, k := range windowKeys {
		incoming, ok := object(next[k])
		if ok && newerWindow(prev[k], prevTs, incoming, nextTs) {
			merged[k] = stamped(incoming, next, next["source"])
			if truthy(next["source"]) {
				merged["source"] = next["source"]
			}
		} else if !ok && complete(next["source"]) {
			merged[k] = nil
		}
	}
	for _, k := range []string{"plan", "pace"} {
		if n, ok := object(next[k]); ok {
			var prevAt float64
			if p, ok := object(prev[k]); ok {
				prevAt = orZero(p["at"])
			}
			if num(n["at"]) >= prevAt {
				merged[k] = n
			}
		}
	}
	if nextExtra, ok := next["extra"].([]any); ok {
		prevExtra, _ := prev["extra"].([]any)
		if extraAt(nextExtra, nextTs) >= extraAt(prevExtra, prevTs) {
			merged["extra"] = nextExtra
		}
	}
	if e, has := next["codexbarError"]; has {
		nextAt := nextTs
		if m, ok := object(e); ok && m["at"] != nil {
			nextAt = num(m["at"])
		}
		var prevAt float64
		if p, ok := object(prev["codexbarError"]); ok {
			prevAt = orZero(p["at"])
		}
		if nextAt >= prevAt {
			merged["codexbarError"] = e
		}
	}
	return merged
}

func extraAt(items []any, providerTs float64) float64 {
	ats := []float64{0}
	for _, item := range items {
		m, _ := object(item)
		w, ok := object(m["window"])
		if !ok {
			return math.NaN()
		}
		ats = append(ats, windowAt(w, providerTs))
	}
	return jsMax(ats...)
}

// Cache is rate-limits.json. Every read-merge-write goes through one lock; the file is replaced, never rewritten in
// place, because other writers (a root timer copying limits between accounts) and the watcher read it at any moment.
type Cache struct {
	File string
	mu   sync.Mutex
}

// Read is the stored providers; a missing or broken file is empty.
func (c *Cache) Read() map[string]Data {
	out := map[string]Data{}
	raw, err := os.ReadFile(c.File)
	if err != nil {
		return out
	}
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) != nil {
		return out
	}
	for _, p := range Providers {
		if m, ok := object(parsed[p]); ok {
			out[p] = m
		}
	}
	return out
}

// Write merges data into the provider's entry and returns the whole new content.
func (c *Cache) Write(provider string, data Data) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cache := c.Read()
	next := map[string]any{}
	for p, d := range cache {
		next[p] = d
	}
	next["ts"] = numValue(jsMax(num(data["ts"]), tsOf(cache["claude"]), tsOf(cache["codex"])))
	next[provider] = Merge(cache[provider], data)
	if err := os.MkdirAll(filepath.Dir(c.File), 0o755); err != nil {
		return nil, err
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	tmp := c.File + "." + strconv.Itoa(os.Getpid()) + "." + hex.EncodeToString(id[:]) + ".tmp"
	if err := os.WriteFile(tmp, nodejson.Marshal(next), 0o644); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, c.File); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return next, nil
}

func tsOf(d Data) float64 {
	if d == nil {
		return 0
	}
	return orZero(d["ts"])
}

// ExhaustedReset is when the windows of an entry that ran out reset (unix seconds), the latest of them: what the limits
// say a chat held on its usage limit waits for. 0 when no window is at its limit, or none resets after now.
func ExhaustedReset(d Data, now float64) float64 {
	var reset float64
	for _, k := range []string{"five_hour", "seven_day"} {
		w, ok := object(d[k])
		if !ok || !(num(w["used_percentage"]) >= 100) {
			continue
		}
		if r := num(w["resets_at"]); r > now && r > reset {
			reset = r
		}
	}
	return reset
}
