package devtools

import (
	"math"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

func marshal(v any) []byte { return nodejson.Marshal(v) }

func itoa(n int) string { return strconv.Itoa(n) }

// Patch is a validated partial change of a slot's settings; nil fields stay.
type Patch struct {
	Throttle     *Throttle
	Blocked      []string // nil: unchanged
	DisableCache *bool
}

func (p Patch) empty() bool { return p.Throttle == nil && p.Blocked == nil && p.DisableCache == nil }

func finite(v any, max float64) (int64, bool) {
	f, ok := v.(float64)
	if !ok || math.IsInf(f, 0) || math.IsNaN(f) || f < 0 {
		return 0, false
	}
	return int64(math.Min(jsRound(f), max)), true
}

// jsRound is Math.round.
func jsRound(f float64) float64 { return math.Floor(f + 0.5) }

// ParseThrottle: a preset fills in its numbers, custom keeps the given ones; false for anything else.
func ParseThrottle(v any) (Throttle, bool) {
	raw, ok := v.(*jsvalue.Object)
	if !ok {
		return Throttle{}, false
	}
	preset, isString := field(raw, "preset").(string)
	if isString && preset != "custom" {
		if t, known := Presets[preset]; known {
			return t, true
		}
	}
	if preset != "custom" {
		return Throttle{}, false
	}
	lat, ok1 := finite(field(raw, "latencyMs"), 60_000)
	down, ok2 := finite(field(raw, "downKbps"), 10_000_000)
	up, ok3 := finite(field(raw, "upKbps"), 10_000_000)
	if !ok1 || !ok2 || !ok3 {
		return Throttle{}, false
	}
	return Throttle{"custom", lat, down, up}, true
}

// ParseBlocked keeps the strings, trimmed, non-empty and not too long, once each, at most MaxBlocked; false for a
// value that is not a list.
func ParseBlocked(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := []string{}
	seen := map[string]bool{}
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			continue
		}
		s = jscompat.Trim(s)
		if s == "" || jscompat.Len(s) > MaxPattern || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) > MaxBlocked {
		out = out[:MaxBlocked]
	}
	return out, true
}

// ParseSettingsPatch validates a partial change; the error names the field in words.
func ParseSettingsPatch(v any) (Patch, string) {
	raw, ok := v.(*jsvalue.Object)
	if !ok {
		if _, isList := v.([]any); !isList {
			return Patch{}, "Нужен объект настроек"
		}
		raw = jsvalue.NewObject()
	}
	var p Patch
	if t, present := raw.Get("throttle"); present {
		th, ok := ParseThrottle(t)
		if !ok {
			return Patch{}, "throttle: preset — none, fast-4g, slow-4g, 3g, offline или custom с latencyMs, downKbps, upKbps"
		}
		p.Throttle = &th
	}
	if b, present := raw.Get("blocked"); present {
		list, ok := ParseBlocked(b)
		if !ok {
			return Patch{}, "blocked: список шаблонов адреса, не больше " + itoa(MaxBlocked)
		}
		p.Blocked = list
	}
	if d, present := raw.Get("disableCache"); present {
		b, ok := d.(bool)
		if !ok {
			return Patch{}, "disableCache: true или false"
		}
		p.DisableCache = &b
	}
	return p, ""
}

// field is a key's value, nil when absent.
func field(o *jsvalue.Object, k string) any {
	v, _ := o.Get(k)
	return v
}
