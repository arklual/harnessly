package wire

import (
	"encoding/json"
	"reflect"
	"sync/atomic"
)

// Marshal is json.Marshal for everything that goes on the wire: a nil slice leaves as [], never as null. The page's
// decoders read an array field as an array — one `null` in queue.items once brought a live page down — and Go leaves
// a slice nil whenever nothing was appended to it, so the guarantee is made here, once, and not by every producer.
//
// The value is copied on the way (the producers' own slices are not touched) and the copy is what is encoded.
//
// What it encodes passes the redactor, if one is set (SetRedactor): a secret an agent printed does not leave the machine.
func Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(emptySlices(v))
	if err != nil {
		return b, err
	}
	if r := redactor.Load(); r != nil {
		b = (*r)(b)
	}
	return b, nil
}

var redactor atomic.Pointer[func([]byte) []byte]

// SetRedactor sets what every frame and page passes on its way out; it must return its argument when there is nothing
// to change. nil sets none. The runtime sets the mask of its profiles' keys.
func SetRedactor(f func([]byte) []byte) {
	if f == nil {
		redactor.Store(nil)
		return
	}
	redactor.Store(&f)
}

func emptySlices(v any) any {
	if v == nil {
		return nil
	}
	src := reflect.ValueOf(v)
	if !holdsSlices(src.Type(), map[reflect.Type]bool{}) {
		return v
	}
	dst := reflect.New(src.Type()).Elem()
	cleanInto(dst, src)
	return dst.Interface()
}

// holdsSlices: a value of t can contain a slice (or something that may hold one behind an interface), so it needs the
// copy. Plain values do not.
func holdsSlices(t reflect.Type, seen map[reflect.Type]bool) bool {
	switch t.Kind() {
	case reflect.Slice:
		return t.Elem().Kind() != reflect.Uint8
	case reflect.Interface:
		return true
	case reflect.Pointer, reflect.Array:
		return holdsSlices(t.Elem(), seen)
	case reflect.Map:
		return holdsSlices(t.Elem(), seen)
	case reflect.Struct:
		if seen[t] {
			return false
		}
		seen[t] = true
		for i := range t.NumField() {
			if t.Field(i).IsExported() && holdsSlices(t.Field(i).Type, seen) {
				return true
			}
		}
	}
	return false
}

// cleanInto sets dst, a settable value of src's type, to a copy of src with every nil slice empty. Byte slices
// (json.RawMessage, []byte) are left as they are: they are not arrays on the wire.
func cleanInto(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.New(src.Type().Elem()))
		cleanInto(dst.Elem(), src.Elem())
	case reflect.Interface:
		if src.IsNil() {
			return
		}
		inner := src.Elem()
		c := reflect.New(inner.Type()).Elem()
		cleanInto(c, inner)
		dst.Set(c)
	case reflect.Struct:
		dst.Set(src) // the fields with nothing to clean, and the unexported ones, are as they were
		for i := range src.NumField() {
			if f := dst.Field(i); f.CanSet() {
				cleanInto(f, src.Field(i))
			}
		}
	case reflect.Slice:
		if src.Type().Elem().Kind() == reflect.Uint8 {
			dst.Set(src)
			return
		}
		n := src.Len()
		out := reflect.MakeSlice(src.Type(), n, n)
		for i := range n {
			cleanInto(out.Index(i), src.Index(i))
		}
		dst.Set(out)
	case reflect.Array:
		for i := range src.Len() {
			cleanInto(dst.Index(i), src.Index(i))
		}
	case reflect.Map:
		if src.IsNil() {
			return
		}
		out := reflect.MakeMapWithSize(src.Type(), src.Len())
		for it := src.MapRange(); it.Next(); {
			e := reflect.New(src.Type().Elem()).Elem()
			cleanInto(e, it.Value())
			out.SetMapIndex(it.Key(), e)
		}
		dst.Set(out)
	default:
		dst.Set(src)
	}
}
