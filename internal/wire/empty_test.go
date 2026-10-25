package wire

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// notNullArrays walks a Go value and the JSON it went out as together, and reports every slice field that went out as
// null: the page's decoders read an array field as an array.
func notNullArrays(v reflect.Value, j any, path string, bad *[]string) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			notNullArrays(v.Elem(), j, path, bad)
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		arr, ok := j.([]any)
		if !ok {
			*bad = append(*bad, path+": "+jsonText(j))
			return
		}
		for i := range min(v.Len(), len(arr)) {
			notNullArrays(v.Index(i), arr[i], path+"["+string(rune('0'+i%10))+"]", bad)
		}
	case reflect.Struct:
		obj, ok := j.(map[string]any)
		if !ok {
			return
		}
		switch f := v.Interface().(type) {
		case SnapshotFrame:
			for _, data := range []any{f.Machine, f.Convs, f.Conv} {
				if dv := reflect.ValueOf(data); !dv.IsNil() {
					notNullArrays(dv, obj["data"], path+".data", bad)
				}
			}
			return
		}
		for i := range v.NumField() {
			sf := v.Type().Field(i)
			name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
			if !sf.IsExported() || name == "-" || name == "" {
				continue
			}
			if jv, has := obj[name]; has {
				notNullArrays(v.Field(i), jv, path+"."+name, bad)
			}
		}
	}
}

func jsonText(j any) string {
	b, _ := json.Marshal(j)
	return string(b)
}

func checkOut(t *testing.T, what string, v any) {
	t.Helper()
	b, err := Marshal(v)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	var j any
	if err := json.Unmarshal(b, &j); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	var bad []string
	notNullArrays(reflect.ValueOf(v), j, what, &bad)
	if len(bad) > 0 {
		t.Errorf("an array went out as null:\n  %s\nin %s", strings.Join(bad, "\n  "), b)
	}
}

// A slice nothing was appended to is nil in Go and null in JSON; the wire never lets it out as null.
func TestAnEmptySnapshotNeverCarriesANullArray(t *testing.T) {
	checkOut(t, "ConvSnapshot", ConvSnapshot{})
	checkOut(t, "ConvsSnapshot", ConvsSnapshot{})
	checkOut(t, "Machine", Machine{})
	checkOut(t, "MachineSnapshot", MachineSnapshot{})
	checkOut(t, "WelcomeFrame", WelcomeFrame{})
	checkOut(t, "Hello", Hello{})
	checkOut(t, "TurnsPage", TurnsPage{})
	checkOut(t, "EntriesPage", EntriesPage{})
	checkOut(t, "Queue", Queue{})
	checkOut(t, "convs frame", SnapshotFrame{Ch: ChannelConvs, Convs: &ConvsSnapshot{}})
	checkOut(t, "conv frame", SnapshotFrame{Ch: ConvChannel("tab-x"), Conv: &ConvSnapshot{}})
	checkOut(t, "machine frame", SnapshotFrame{Ch: ChannelMachine, Machine: &MachineSnapshot{}})

	b, err := Marshal(ConvSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"turns", "entries", "messages", "questions", "permissions", "background"} {
		if !strings.Contains(string(b), `"`+field+`":[]`) {
			t.Errorf("%s is not [] in %s", field, b)
		}
	}
	if !strings.Contains(string(b), `"items":[]`) {
		t.Errorf("queue.items is not [] in %s", b)
	}
	if m := new(Machine); !strings.Contains(mustMarshal(t, m), `"agents":[]`) {
		t.Errorf("agents is not []")
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// eachSlice calls visit on every slice field below v, outermost first, until it answers true. An element of a slice or
// a value in an interface is copied to be addressable, and put back after, so what visit does to a slice stays.
func eachSlice(v reflect.Value, visit func(reflect.Value) bool) bool {
	switch v.Kind() {
	case reflect.Pointer:
		return !v.IsNil() && eachSlice(v.Elem(), visit)
	case reflect.Interface:
		if v.IsNil() {
			return false
		}
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		done := eachSlice(inner, visit)
		v.Set(inner)
		return done
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() && eachSlice(f, visit) {
				return true
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return false
		}
		if visit(v) {
			return true
		}
		for i := range v.Len() {
			if eachSlice(v.Index(i), visit) {
				return true
			}
		}
	}
	return false
}

// Every slice of every server frame in the fixtures, left nil on its own, still goes out as an array.
func TestEverySliceOfTheFixturesLeftNilGoesOutAsAnArray(t *testing.T) {
	slicesTried := 0
	for name, raw := range fixtures(t) {
		first, err := DecodeServerFrame(raw)
		if err != nil {
			continue // not a server frame
		}
		n := 0
		eachSlice(reflect.ValueOf(&first).Elem(), func(reflect.Value) bool { n++; return false })
		for k := range n {
			f, _ := DecodeServerFrame(raw)
			at := 0
			eachSlice(reflect.ValueOf(&f).Elem(), func(s reflect.Value) bool {
				if at++; at-1 == k {
					s.SetZero()
					return true
				}
				return false
			})
			slicesTried++
			checkOut(t, name+"#"+strings.Repeat("+", k%5), f)
		}
	}
	if slicesTried < 100 {
		t.Fatalf("only %d slices tried: the fixtures changed shape", slicesTried)
	}
}
