package jscompat

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestTrimAndLength(t *testing.T) {
	if got := Trim("\ufeff\u00a0 hi \u3000\n"); got != "hi" {
		t.Fatalf("trim %q", got)
	}
	if got := Trim("\u0085x\u0085"); got != "\u0085x\u0085" {
		t.Fatalf("NEL is not JavaScript space: %q", got)
	}
	s := strings.Repeat("x", 199) + "😀abc"
	if Len(s) != 204 {
		t.Fatalf("len %d", Len(s))
	}
	if got := Truncate(s, 200); got != strings.Repeat("x", 199)+"\ufffd…" {
		t.Fatalf("cut surrogate: %q", got)
	}
	if got := Truncate("привет", 200); got != "привет" {
		t.Fatal(got)
	}
	if got := Truncate(strings.Repeat("я", 201), 200); got != strings.Repeat("я", 200)+"…" {
		t.Fatal(got)
	}
}

func TestParseInt(t *testing.T) {
	for _, c := range []struct {
		in    string
		radix int
		want  float64
	}{
		{"0x1f", 0, 31}, {"  -12abc", 0, -12}, {"1e3", 0, 1}, {"0x1f", 10, 0}, {"+20", 10, 20}, {"3.9", 10, 3},
		{"99999999999999999999", 10, 1e20}, {"2,9", 10, 2},
	} {
		if got := ParseInt(c.in, c.radix); got != c.want {
			t.Errorf("%q/%d: %v, want %v", c.in, c.radix, got, c.want)
		}
	}
	for _, in := range []string{"", "0x", "abc", "５", "-"} {
		if got := ParseInt(in, 0); !math.IsNaN(got) {
			t.Errorf("%q: %v, want NaN", in, got)
		}
	}
}

func TestDates(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	defer func() { time.Local = saved }()
	for in, want := range map[string]float64{
		"2025-10-16":                  1760572800000,
		"2025-10-16T10:00":            1760608800000,
		"2025-10-16T10:00:00.123456Z": 1760608800123,
		"2025-10-16 10:00:00":         1760608800000,
		"2025-10-16t10:00:00z":        1760608800000,
		"+002025-10-16T10:00:00Z":     1760608800000,
		"2025-10-16T24:00:00Z":        1760659200000,
		"2025-02-30T00:00:00Z":        1740873600000,
		"2025-10-16T10:00:00+0300":    1760598000000,
		"2025-10-16T10:00:00.5-01:30": 1760614200500,
	} {
		if got := ParseDate(in); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"1760000000000", "2025-10-16T10Z", "yesterday", "2025-13-01", ""} {
		if got := ParseDate(in); !math.IsNaN(got) {
			t.Errorf("%q: %v, want NaN", in, got)
		}
	}
	if DateValue(1.5e12) != 1.5e12 || DateValue(true) != 1 || !math.IsNaN(DateValue(map[string]any{})) {
		t.Fatal("DateValue")
	}
	for ms, want := range map[float64]string{
		1e15: "+033658-09-27T01:46:40.000Z", -1: "1969-12-31T23:59:59.999Z", 253402300800000: "+010000-01-01T00:00:00.000Z",
		-62198755200000: "-000001-01-01T00:00:00.000Z", 1760608800123: "2025-10-16T10:00:00.123Z",
	} {
		if got, ok := ISOString(ms); !ok || got != want {
			t.Errorf("%v: %q, want %q", ms, got, want)
		}
	}
	if _, ok := ISOString(math.NaN()); ok {
		t.Fatal("NaN must not print")
	}
	if DateMs(1790518992418.97) != 1790518992419 || DateMs(10.4) != 10 {
		t.Fatal("DateMs rounds")
	}
}

func TestQueryAndBasename(t *testing.T) {
	q := Query("a=1&a=2&b=%zz&c=x+y&d=%41;e&f&&g=%D0%BF")
	if strings.Join(q["a"], "|") != "1|2" || q.Get("b") != "%zz" || q.Get("c") != "x y" || q.Get("d") != "A;e" || q.Get("g") != "п" {
		t.Fatalf("%v", q)
	}
	if v, ok := q["f"]; !ok || v[0] != "" {
		t.Fatalf("bare key: %v", q)
	}
	p := Param{Values: q["a"]}
	if !p.Array() || p.String() != "1,2" || !p.Truthy() {
		t.Fatal("array param")
	}
	if (Param{Values: []string{""}}).Truthy() || (Param{}).Present() {
		t.Fatal("empty param")
	}
	for in, want := range map[[2]string]string{
		{"/a/.jsonl", ".jsonl"}: ".jsonl", {"/a/x.jsonl/", ".jsonl"}: "x", {"/", ""}: "", {"/a/b//", ""}: "b", {"rel", ""}: "rel",
	} {
		if got := Basename(in[0], in[1]); got != want {
			t.Errorf("%v: %q, want %q", in, got, want)
		}
	}
}

func TestNumberAndString(t *testing.T) {
	for f, want := range map[float64]string{1e21: "1e+21", 123.5: "123.5", 1e-7: "1e-7", 0: "0", 42: "42"} {
		if got := Number(f); got != want {
			t.Errorf("%v: %q, want %q", f, got, want)
		}
	}
	if String([]any{"a", nil, 1.0}) != "a,,1" || String(map[string]any{}) != "[object Object]" || String(true) != "true" {
		t.Fatal("String")
	}
	if Truthy("") || Truthy(0.0) || Truthy(nil) || !Truthy([]any{}) || !Truthy(map[string]any{}) {
		t.Fatal("Truthy")
	}
}
