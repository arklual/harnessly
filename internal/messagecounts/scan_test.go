package messagecounts

import (
	"encoding/json"
	"testing"
)

func TestValidAsJSONParse(t *testing.T) {
	for _, s := range []string{`{}`, `[]`, `""`, `0`, `-0`, `01`, `1.`, `.5`, `1e`, `1e+5`, `-`, `1.5E-3`, `tru`, `true`, `nul`, `null `,
		` {"a":1} `, `{"a":1,}`, `[1,]`, `{"a" 1}`, `{"a":1}x`, `"\x"`, `"\u12"`, `"ካ"`, "\"\x01\"", `"\/"`, `{"a":[1,{"b":"}"}]}`,
		`[[[[]]]]`, `{"a":{"b":{}}}`, `{1:2}`, "\t[1]\n", `"unterminated`, `[1 2]`, `{"a":1 "b":2}`, `1e400`, `-01`, `- 1`} {
		if got, want := valid([]byte(s)), json.Valid([]byte(s)); got != want {
			t.Errorf("valid(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestFieldsAndElements(t *testing.T) {
	raw := []byte(`{"type":"a","x":{"type":"no"},"type":"b","s":"q\"}","n":-1.5e2}`)
	f := fields(raw, "type", "n", "missing")
	if string(f[0]) != `"b"` || string(f[1]) != `-1.5e2` || f[2] != nil {
		t.Fatalf("%q", f)
	}
	e := elements([]byte(`[1, "a,]", {"b":[2]} ,null]`))
	if len(e) != 4 || string(e[1]) != `"a,]"` || string(e[2]) != `{"b":[2]}` {
		t.Fatalf("%q", e)
	}
}

func TestInheritedToolNames(t *testing.T) {
	c := newCounter()
	c.lines([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"constructor"},{"type":"tool_use","name":"constructor"},` +
		`{"type":"tool_use","name":"__proto__"},{"type":"tool_use","name":"10"},{"type":"tool_use","name":"2"},{"type":"tool_use","name":"Bash"}]}}`))
	got := c.counts().Breakdown.Keys()[0]
	if got != "2" {
		t.Fatalf("first key %q", got)
	}
	if v, _ := c.counts().Breakdown.Get("constructor"); v != "function Object() { [native code] }11" {
		t.Fatalf("constructor %v", v)
	}
	if c.counts().Breakdown.Has("__proto__") || c.counts().Tool != 6 {
		t.Fatal("__proto__ counted as a key")
	}
}
