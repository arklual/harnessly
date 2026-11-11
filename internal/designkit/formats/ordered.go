package formats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func appendf(list []string, format string, args ...any) []string {
	return append(list, fmt.Sprintf(format, args...))
}

// ordered is a JSON object that keeps its keys in the order they were read or set: a file we rewrite keeps its shape.
type ordered struct {
	keys []string
	vals map[string]json.RawMessage
}

func newOrdered() *ordered { return &ordered{vals: map[string]json.RawMessage{}} }

// parseOrdered reads raw as an object with its key order; ok is false for anything else.
func parseOrdered(raw []byte) (*ordered, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	o := newOrdered()
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		if _, dup := o.vals[key]; !dup {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = v
	}
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	return o, true
}

func (o *ordered) has(k string) bool { _, ok := o.vals[k]; return ok }

// set stores v (anything json can write) under k; a new key goes last.
func (o *ordered) set(k string, v any) {
	raw, ok := v.(json.RawMessage)
	if !ok {
		raw = marshal(v)
	}
	if _, has := o.vals[k]; !has {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = raw
}

func (o *ordered) str(k string) string {
	var s string
	_ = json.Unmarshal(o.vals[k], &s)
	return s
}

func (o *ordered) sub(k string) (*ordered, bool) {
	raw, ok := o.vals[k]
	if !ok {
		return nil, false
	}
	return parseOrdered(raw)
}

func (o *ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		v := o.vals[k]
		if len(v) == 0 {
			v = json.RawMessage("null")
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal writes v as compact JSON without HTML escaping (a CSS selector or a `>` stays readable).
func marshal(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return json.RawMessage("null")
	}
	return bytes.TrimSpace(b.Bytes())
}

// pretty indents compact JSON with two spaces and ends it with a newline.
func pretty(v any) []byte {
	raw := marshal(v)
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return raw
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// rawMap is the unknown members of an object, written back after the known ones in key order.
type rawMap map[string]json.RawMessage

func (m rawMap) keys() []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func titleOf(id string) string {
	if id == "" {
		return ""
	}
	return strings.ToUpper(id[:1]) + id[1:]
}
