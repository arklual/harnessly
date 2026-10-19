package stats

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
)

// The scanner accepts exactly the objects JSON.parse (RFC 8259, as encoding/json checks it) accepts, and reads the
// members encoding/json reads, a repeated key's last value included.
func TestScannerAgreesWithJSON(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	pieces := []string{`{`, `}`, `[`, `]`, `,`, `:`, ` `, "\n", `"a"`, `"type"`, `"type"`, `"x\"y"`, `"\q"`, `1`, `-0.5e+3`, `01`, `1.`, `-`,
		`true`, `nul`, `null`, `"tool_use"`, "\"\x01\"", `"\ud83d"`, `{"type":"tool_use"}`, `[]`, `{}`}
	for n := 0; n < 200000; n++ {
		s := "{"
		for k := rng.Intn(8); k > 0; k-- {
			s += pieces[rng.Intn(len(pieces))]
		}
		if rng.Intn(2) == 0 {
			s += "}"
		}
		b := []byte(s)
		_, ok := wholeObject(b)
		if line := lineMembers(b, func([]byte, bool, []byte) {}); line != ok {
			t.Fatalf("%q: line scanner %v, object scanner %v", s, line, ok)
		}
		if ok != json.Valid(b) {
			t.Fatalf("%q: scanner %v, encoding/json %v", s, ok, json.Valid(b))
		}
		if !ok {
			continue
		}
		var want map[string]any
		if json.Unmarshal(b, &want) != nil {
			continue
		}
		got := map[string]any{}
		members(b, func(key []byte, esc bool, raw []byte) {
			var k string
			_ = json.Unmarshal(append(append([]byte{'"'}, key...), '"'), &k)
			got[k] = value(raw)
		})
		if !reflect.DeepEqual(normalizeNums(got), normalizeNums(want)) {
			t.Fatalf("%q: members %v, want %v", s, got, want)
		}
	}
}

func normalizeNums(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalizeNums(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalizeNums(e)
		}
		return out
	case json.Number:
		return toNum(x)
	}
	return v
}

func TestCountToolUses(t *testing.T) {
	if n := countToolUses([]byte(`[{"type":"tool_use"},{"type":"text"}, "tool_use", {"type":"tool_use","x":[{"type":"tool_use"}]}]`)); n != 2 {
		t.Fatalf("%d", n)
	}
}
