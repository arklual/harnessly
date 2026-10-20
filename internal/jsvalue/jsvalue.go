// Package jsvalue holds JSON the way JavaScript does: objects keep their key order and every key they were given, so a
// document read, changed and written back carries the fields nobody here models, in the order the previous runtime wrote
// them, and Stringify writes what JSON.stringify writes.
//
// Values are nil (null), bool, float64, string, []any and *Object. Undefined is not a value: setting it removes the key.
package jsvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Undefined given to Set removes the key, as JSON.stringify drops an undefined property.
var Undefined = undefinedValue{}

type undefinedValue struct{}

// Object is a JavaScript object with string keys.
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject is an empty object; pairs are set in order (key, value, key, value…).
func NewObject(pairs ...any) *Object {
	o := &Object{vals: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

// Set assigns a key: an existing one keeps its place, a new one goes last. Undefined removes it.
func (o *Object) Set(k string, v any) {
	if _, ok := v.(undefinedValue); ok {
		o.Delete(k)
		return
	}
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = normalize(v)
}

// normalize turns the Go numbers callers pass into float64, the only number there is.
func normalize(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	case []string:
		out := make([]any, len(n))
		for i, s := range n {
			out[i] = s
		}
		return out
	}
	return v
}

// Get is the value of a key; false when the key is absent.
func (o *Object) Get(k string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[k]
	return v, ok
}

// Has says whether the key is present.
func (o *Object) Has(k string) bool { _, ok := o.Get(k); return ok }

// Delete removes a key.
func (o *Object) Delete(k string) {
	if o == nil {
		return
	}
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

// Len is the number of keys.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys in the order JavaScript enumerates them: array-index keys ascending, then the others in insertion order.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	var idx []string
	var rest []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return append([]string(nil), o.keys...)
	}
	sort.Slice(idx, func(i, j int) bool {
		a, _ := strconv.ParseUint(idx[i], 10, 32)
		b, _ := strconv.ParseUint(idx[j], 10, 32)
		return a < b
	})
	return append(idx, rest...)
}

func isArrayIndex(k string) bool {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < math.MaxUint32
}

// Clear removes every key.
func (o *Object) Clear() {
	o.keys = nil
	o.vals = map[string]any{}
}

// Assign copies every key of src onto o, as Object.assign.
func (o *Object) Assign(src *Object) {
	for _, k := range src.Keys() {
		v, _ := src.Get(k)
		o.Set(k, v)
	}
}

// Replace makes o hold exactly what src holds, in src's order: the object a caller holds becomes the other one.
func (o *Object) Replace(src *Object) {
	o.Clear()
	o.Assign(src)
}

// Copy is a shallow copy ({...o}).
func (o *Object) Copy() *Object {
	c := NewObject()
	c.Assign(o)
	return c
}

// String is the value of a key when it is a string.
func (o *Object) String(k string) string {
	v, _ := o.Get(k)
	s, _ := v.(string)
	return s
}

// Object is the value of a key when it is an object.
func (o *Object) Object(k string) *Object {
	v, _ := o.Get(k)
	c, _ := v.(*Object)
	return c
}

// Array is the value of a key when it is an array.
func (o *Object) Array(k string) []any {
	v, _ := o.Get(k)
	a, _ := v.([]any)
	return a
}

// Number is the value of a key when it is a number.
func (o *Object) Number(k string) (float64, bool) {
	v, _ := o.Get(k)
	n, ok := v.(float64)
	return n, ok
}

// Clone is a deep copy (structuredClone of JSON data).
func Clone(v any) any {
	switch x := v.(type) {
	case *Object:
		c := NewObject()
		for _, k := range x.keys {
			c.keys = append(c.keys, k)
			c.vals[k] = Clone(x.vals[k])
		}
		return c
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Clone(e)
		}
		return out
	}
	return v
}

// Equal says whether two values stringify alike (key order counts, as it does for JSON.stringify comparisons).
func Equal(a, b any) bool { return bytes.Equal(Stringify(a, ""), Stringify(b, "")) }

// SameValue compares as a deep equality that ignores key order (objects with the same keys and equal values).
func SameValue(a, b any) bool {
	switch x := a.(type) {
	case *Object:
		y, ok := b.(*Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.keys {
			yv, ok := y.Get(k)
			if !ok || !SameValue(x.vals[k], yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !SameValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	}
	return a == b
}

// ErrSyntax is any text JSON.parse refuses.
var ErrSyntax = errors.New("jsvalue: invalid JSON")

// Parse reads JSON as JSON.parse does: a duplicate key keeps its first place and its last value, numbers are float64.
func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, ErrSyntax
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, ErrSyntax
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return fromToken(dec, tok)
}

func fromToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, ErrSyntax
				}
				v, err := parseValue(dec)
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
				v, err := parseValue(dec)
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
		return nil, ErrSyntax
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, err
		}
		return f, nil
	case string, bool, nil:
		return t, nil
	}
	return nil, ErrSyntax
}

// Stringify writes v as JSON.stringify(v, null, indent) does; indent "" is the compact form.
func Stringify(v any, indent string) []byte {
	var b bytes.Buffer
	write(&b, v, indent, "")
	return b.Bytes()
}

func write(b *bytes.Buffer, v any, indent, cur string) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(jscompat.Number(x))
		}
	case int:
		b.WriteString(jscompat.Number(float64(x)))
	case int64:
		b.WriteString(jscompat.Number(float64(x)))
	case []string:
		write(b, StringArray(x), indent, cur)
	case string:
		quote(b, x)
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		inner := cur + indent
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteString("\n" + inner)
			}
			write(b, e, indent, inner)
		}
		if indent != "" {
			b.WriteString("\n" + cur)
		}
		b.WriteByte(']')
	case *Object:
		keys := x.Keys()
		if len(keys) == 0 {
			b.WriteString("{}")
			return
		}
		inner := cur + indent
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteString("\n" + inner)
			}
			quote(b, k)
			b.WriteByte(':')
			if indent != "" {
				b.WriteByte(' ')
			}
			write(b, x.vals[k], indent, inner)
		}
		if indent != "" {
			b.WriteString("\n" + cur)
		}
		b.WriteByte('}')
	default:
		panic(fmt.Sprintf("jsvalue: %T is not a JSON value", v))
	}
}

func quote(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// Strings is an array's string elements, in order.
func Strings(a []any) []string {
	var out []string
	for _, e := range a {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// StringArray turns strings into an array value.
func StringArray(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Truthy is JavaScript truthiness of a JSON value.
func Truthy(v any) bool {
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

// TrimJS trims as String.prototype.trim.
func TrimJS(s string) string { return strings.TrimFunc(s, jscompat.IsSpace) }

// MarshalJSON lets encoding/json write an object in its own key order.
func (o *Object) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	return Stringify(o, ""), nil
}
