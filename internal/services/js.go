package services

import (
	"errors"
	"strconv"
	"unicode/utf16"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The answers are built from what external services send, read the way the previous runtime's handlers read it:
// properties of any JSON value, undefined for a missing one, a TypeError where JavaScript would throw one. A value is
// nil (null), jsvalue.Undefined, bool, float64, string, []any or *jsvalue.Object.

// errType is a TypeError: the previous handler threw, and its route answered 500.
var errType = errors.New("type error")

var undefined any = jsvalue.Undefined

func isUndefined(v any) bool { return v == undefined }

func nullish(v any) bool { return v == nil || isUndefined(v) }

// opt is v?.[k].
func opt(v any, k string) any {
	switch x := v.(type) {
	case *jsvalue.Object:
		if val, ok := x.Get(k); ok {
			return val
		}
	case []any:
		if k == "length" {
			return float64(len(x))
		}
		if i, err := strconv.Atoi(k); err == nil && strconv.Itoa(i) == k && i >= 0 && i < len(x) {
			return x[i]
		}
	case string:
		units := utf16.Encode([]rune(x))
		if k == "length" {
			return float64(len(units))
		}
		if i, err := strconv.Atoi(k); err == nil && strconv.Itoa(i) == k && i >= 0 && i < len(units) {
			return string(utf16.Decode(units[i : i+1]))
		}
	}
	return undefined
}

// prop is v[k]: reading a property of null or undefined throws.
func prop(v any, k string) (any, error) {
	if nullish(v) {
		return nil, errType
	}
	return opt(v, k), nil
}

// orNull is v ?? null.
func orNull(v any) any {
	if nullish(v) {
		return nil
	}
	return v
}

func truthy(v any) bool { return !isUndefined(v) && jscompat.Truthy(v) }

// str is String(v), as a template literal prints it.
func str(v any) string {
	if isUndefined(v) {
		return "undefined"
	}
	return jscompat.String(v)
}

// find is arr.find(e => e.name === name): a non-array throws, so does an element that is null or undefined.
func findByName(arr any, name string) (any, error) {
	list, ok := arr.([]any)
	if !ok {
		return nil, errType
	}
	for _, e := range list {
		n, err := prop(e, "name")
		if err != nil {
			return nil, err
		}
		if s, ok := n.(string); ok && s == name {
			return e, nil
		}
	}
	return undefined, nil
}

func obj(pairs ...any) *jsvalue.Object { return jsvalue.NewObject(pairs...) }
