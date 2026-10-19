package usagereport

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"unicode/utf8"
)

// The scanner accepts exactly what a JSON parser does, and reads the same values out of what it accepts.
func TestScannerAgreesWithJSON(t *testing.T) {
	seeds := []string{
		`{"type":"user","uuid":"u1","message":{"content":[{"type":"text","text":"a \"q\" \\ é 😀"}]},"n":-1.5e-3}`,
		`{"a":[1,2.0,-0,true,false,null,{"b":"c"}],"d":{}, "e" : [ ] }`, `[1,2]`, `"s"`, `5`, `null`, `true`, ` {} `,
		`{"a":01}`, `{"a":1,}`, `[1,]`, `{"a":"\x"}`, `{"a":"` + "\x01" + `"}`, `{"a":1.}`, `{"a":-}`, `{"a":+1}`, `{"a":.5}`,
		`{"a":NaN}`, `{"a":"\u12g4"}`, `{"a" 1}`, `{a:1}`, `{"a":1}}`, `{"a":1} x`, `nul`, `tru`, `{"a":[}`, `{"a":"\ud800"}`,
	}
	rng := rand.New(rand.NewSource(5))
	alphabet := []byte(`{}[]":,.-+0123456789eEtrufalsn \\/u` + "\n\x01é")
	check := func(s string) {
		b := []byte(s)
		if !utf8.Valid(b) {
			return // lines are made valid UTF-8 before they are read
		}
		if got, want := valid(b), json.Valid(b); got != want {
			t.Fatalf("valid(%q) = %v, json says %v", s, got, want)
		}
		if !json.Valid(b) {
			return
		}
		var want map[string]any
		if json.Unmarshal(b, &want) != nil || want == nil {
			return
		}
		o := objectOf(b[skipWS(b, 0):])
		got := map[string]any{}
		for _, m := range o {
			var k string
			_ = json.Unmarshal(append(append([]byte{'"'}, m.name...), '"'), &k)
			v, _ := o.value(k)
			got[k] = v
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: read %v, json reads %v", s, got, want)
		}
	}
	for _, s := range seeds {
		check(s)
		for i := 0; i < 3000; i++ {
			b := []byte(s)
			for n := rng.Intn(3) + 1; n > 0 && len(b) > 0; n-- {
				at := rng.Intn(len(b))
				switch rng.Intn(3) {
				case 0:
					b[at] = alphabet[rng.Intn(len(alphabet))]
				case 1:
					b = append(b[:at], b[at+1:]...)
				default:
					b = append(b[:at], append([]byte{alphabet[rng.Intn(len(alphabet))]}, b[at:]...)...)
				}
			}
			if json.Valid(b) || rng.Intn(4) == 0 {
				check(string(b))
			}
		}
	}
}
