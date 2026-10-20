package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// Obj is a JSON object that remembers the order its keys came in: a transcript line's tool input is summarised by
// its first key, and counts keyed by tool name are written in the order the tools were met, as JavaScript does.
type Obj struct {
	keys []string
	m    map[string]any
}

// NewObj is an empty object.
func NewObj() *Obj { return &Obj{m: map[string]any{}} }

// Get is the value under k and whether k is there at all.
func (o *Obj) Get(k string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.m[k]
	return v, ok
}

// Val is the value under k, nil when absent (JavaScript's undefined and null both read as nil).
func (o *Obj) Val(k string) any {
	v, _ := o.Get(k)
	return v
}

// Str is the value under k when it is a string.
func (o *Obj) Str(k string) (string, bool) {
	s, ok := o.Val(k).(string)
	return s, ok
}

// Set stores v under k, keeping k's first position.
func (o *Obj) Set(k string, v any) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

// Len is the number of keys.
func (o *Obj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// arrayIndex tells a key JavaScript orders as an array index: a canonical number below 2^32-1.
func arrayIndex(k string) (uint64, bool) {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil || n >= 1<<32-1 {
		return 0, false
	}
	return n, true
}

// Keys are the keys in JavaScript's order: array indices ascending, then the rest as they came.
func (o *Obj) Keys() []string {
	if o == nil {
		return nil
	}
	var idx, rest []string
	for _, k := range o.keys {
		if _, ok := arrayIndex(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return rest
	}
	slices.SortFunc(idx, func(a, b string) int {
		x, _ := arrayIndex(a)
		y, _ := arrayIndex(b)
		return int(int64(x) - int64(y))
	})
	return append(idx, rest...)
}

// MarshalJSON writes the object in its key order.
func (o *Obj) MarshalJSON() ([]byte, error) { return []byte(Stringify(o)), nil }

var errTrailing = errors.New("trailing data after JSON value")

// Decode parses one JSON text as JSON.parse does (whitespace around it allowed, nothing else), objects as *Obj, numbers
// as float64.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errTrailing
	}
	return v, nil
}

// DecodeObj parses a line that must hold an object; nil for anything else.
func DecodeObj(data []byte) *Obj {
	v, err := Decode(data)
	if err != nil {
		return nil
	}
	o, _ := v.(*Obj)
	return o
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObj()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, errors.New("unexpected delimiter")
	default:
		return t, nil
	}
}

// AsObj is v when it is an object.
func AsObj(v any) *Obj {
	o, _ := v.(*Obj)
	return o
}

// AsStr is v when it is a string, else "" (JavaScript's `typeof v === 'string' ? v : ”`).
func AsStr(v any) string {
	s, _ := v.(string)
	return s
}

// Truthy is JavaScript truthiness of a decoded value.
func Truthy(v any) bool {
	if o, ok := v.(*Obj); ok {
		return o != nil
	}
	return jscompat.Truthy(v)
}

// JSString is String(v) of a decoded value.
func JSString(v any) string {
	switch x := v.(type) {
	case *Obj:
		return "[object Object]"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = JSString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return jscompat.String(v)
}

// Stringify is JSON.stringify of a decoded value, keys in their order.
func Stringify(v any) string {
	var b strings.Builder
	writeJSON(&b, v)
	return b.String()
}

func writeJSON(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case float64:
		s := jscompat.Number(x)
		if s == "NaN" || strings.HasSuffix(s, "Infinity") {
			s = "null"
		}
		b.WriteString(s)
	case string:
		b.Write(nodejson.Marshal(x))
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e)
		}
		b.WriteByte(']')
	case *Obj:
		b.WriteByte('{')
		for i, k := range x.Keys() {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(nodejson.Marshal(k))
			b.WriteByte(':')
			writeJSON(b, x.m[k])
		}
		b.WriteByte('}')
	default:
		b.Write(nodejson.Marshal(x))
	}
}
