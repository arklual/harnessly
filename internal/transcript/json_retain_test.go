package transcript

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// inside says whether s points into text's bytes.
func inside(s, text string) bool {
	if len(s) == 0 || len(text) == 0 {
		return false
	}
	p, lo := uintptr(unsafe.Pointer(unsafe.StringData(s))), uintptr(unsafe.Pointer(unsafe.StringData(text)))
	return p >= lo && p < lo+uintptr(len(text))
}

// stringsOf is every key and string value of v.
func stringsOf(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, it := range x {
			stringsOf(it, out)
		}
	case *jsvalue.Object:
		for _, k := range x.Keys() {
			*out = append(*out, k)
			val, _ := x.Get(k)
			stringsOf(val, out)
		}
	}
}

// HRN-901: the strings read from a transcript line are their own, not slices of the text: a feed keeping one id of a
// read held the whole read (4 MB and more of JSONL per conversation, 3 GB on a machine) alive.
func TestParsedStringsDoNotHoldTheText(t *testing.T) {
	text := strings.Repeat(" ", 1<<10) + `{"type":"assistant","uuid":"u-1","message":{"model":"claude-opus","content":[{"type":"text","text":"plain"},{"type":"text","text":"esc\naped"}]}}`
	for name, parse := range map[string]func(string) (any, bool){
		"claude": func(s string) (any, bool) { v, err := claudeParseJSON(s); return v, err == nil },
		"codex":  parseJSONFast,
	} {
		v, ok := parse(text)
		if !ok {
			t.Fatalf("%s: does not parse", name)
		}
		var got []string
		stringsOf(v, &got)
		if len(got) < 10 {
			t.Fatalf("%s: %d strings", name, len(got))
		}
		for _, s := range got {
			if inside(s, text) {
				t.Fatalf("%s: %q is a slice of the line", name, s)
			}
		}
	}
}
