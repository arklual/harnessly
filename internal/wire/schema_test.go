package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// schema is the subset of JSON Schema 2020-12 that schema/frames.json uses. A keyword outside it fails the check,
// so the schema cannot lean on something this checker would skip.
type schema struct {
	root map[string]any
	defs map[string]any
	re   map[string]*regexp.Regexp
}

var schemaKeywords = map[string]bool{
	"$schema": true, "$id": true, "title": true, "description": true, "$defs": true, "$ref": true, "type": true,
	"const": true, "enum": true, "required": true, "properties": true, "additionalProperties": true, "items": true,
	"oneOf": true, "pattern": true, "minimum": true, "maximum": true, "minLength": true, "maxLength": true, "minItems": true, "maxItems": true,
}

func loadSchema(path string) (*schema, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, err := decodeNumbers(b)
	if err != nil {
		return nil, err
	}
	m, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema: not an object")
	}
	defs, _ := m["$defs"].(map[string]any)
	return &schema{root: m, defs: defs, re: map[string]*regexp.Regexp{}}, nil
}

func decodeNumbers(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// check reports why v does not match s; nil when it does.
func (sc *schema) check(s map[string]any, v any, path string) error {
	for k := range s {
		if !schemaKeywords[k] {
			return fmt.Errorf("%s: schema keyword %q is not supported", path, k)
		}
	}
	if r, ok := s["$ref"].(string); ok {
		name, found := strings.CutPrefix(r, "#/$defs/")
		def, ok := sc.defs[name].(map[string]any)
		if !found || !ok {
			return fmt.Errorf("%s: unresolved $ref %q", path, r)
		}
		return sc.check(def, v, path)
	}
	if alts, ok := s["oneOf"].([]any); ok {
		var matched int
		var errs []string
		for _, a := range alts {
			if err := sc.check(a.(map[string]any), v, path); err != nil {
				errs = append(errs, err.Error())
			} else {
				matched++
			}
		}
		if matched != 1 {
			return fmt.Errorf("%s: matches %d of oneOf, not 1:\n  %s", path, matched, strings.Join(nearest(errs), "\n  "))
		}
	}
	if c, ok := s["const"]; ok && !jsonEqual(c, v) {
		return fmt.Errorf("%s: %v is not %v", path, v, c)
	}
	if e, ok := s["enum"].([]any); ok && !slices.ContainsFunc(e, func(x any) bool { return jsonEqual(x, v) }) {
		return fmt.Errorf("%s: %v is not in %v", path, v, e)
	}
	if t, ok := s["type"]; ok {
		if err := checkType(t, v, path); err != nil {
			return err
		}
	}
	switch x := v.(type) {
	case string:
		if p, ok := s["pattern"].(string); ok {
			re := sc.re[p]
			if re == nil {
				re = regexp.MustCompile(p)
				sc.re[p] = re
			}
			if !re.MatchString(x) {
				return fmt.Errorf("%s: %q does not match %s", path, x, p)
			}
		}
		if n, ok := s["minLength"].(json.Number); ok && int64(utf8.RuneCountInString(x)) < mustInt(n) {
			return fmt.Errorf("%s: %q is shorter than %s", path, x, n)
		}
		if n, ok := s["maxLength"].(json.Number); ok && int64(utf8.RuneCountInString(x)) > mustInt(n) {
			return fmt.Errorf("%s: %q is longer than %s", path, x, n)
		}
	case json.Number:
		if m, ok := s["minimum"].(json.Number); ok {
			a, _ := new(big.Float).SetString(x.String())
			b, _ := new(big.Float).SetString(m.String())
			if a.Cmp(b) < 0 {
				return fmt.Errorf("%s: %s is below %s", path, x, m)
			}
		}
		if m, ok := s["maximum"].(json.Number); ok {
			a, _ := new(big.Float).SetString(x.String())
			b, _ := new(big.Float).SetString(m.String())
			if a.Cmp(b) > 0 {
				return fmt.Errorf("%s: %s is above %s", path, x, m)
			}
		}
	case []any:
		if n, ok := s["minItems"].(json.Number); ok && int64(len(x)) < mustInt(n) {
			return fmt.Errorf("%s: fewer than %s items", path, n)
		}
		if n, ok := s["maxItems"].(json.Number); ok && int64(len(x)) > mustInt(n) {
			return fmt.Errorf("%s: more than %s items", path, n)
		}
		if it, ok := s["items"].(map[string]any); ok {
			for i, item := range x {
				if err := sc.check(it, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, has := x[r.(string)]; !has {
					return fmt.Errorf("%s: no %q", path, r)
				}
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p, ok := props[k].(map[string]any)
			if !ok {
				if s["additionalProperties"] == false {
					return fmt.Errorf("%s: unknown field %q", path, k)
				}
				continue
			}
			if err := sc.check(p, x[k], path+"."+k); err != nil {
				return err
			}
		}
	}
	return nil
}

// nearest keeps the oneOf errors that got furthest into the value: they name what the value most likely meant.
func nearest(errs []string) []string {
	best := 0
	for _, e := range errs {
		best = max(best, strings.Count(strings.SplitN(e, ":", 2)[0], "."))
	}
	var out []string
	for _, e := range errs {
		if strings.Count(strings.SplitN(e, ":", 2)[0], ".") == best {
			out = append(out, e)
		}
	}
	return out
}

func mustInt(n json.Number) int64 {
	i, err := n.Int64()
	if err != nil {
		panic(err)
	}
	return i
}

func checkType(t any, v any, path string) error {
	name, _ := t.(string)
	ok := false
	switch name {
	case "object":
		_, ok = v.(map[string]any)
	case "array":
		_, ok = v.([]any)
	case "string":
		_, ok = v.(string)
	case "boolean":
		_, ok = v.(bool)
	case "null":
		ok = v == nil
	case "number":
		_, ok = v.(json.Number)
	case "integer":
		if n, isNum := v.(json.Number); isNum {
			_, err := n.Int64()
			ok = err == nil
		}
	default:
		return fmt.Errorf("%s: schema type %v is not supported", path, t)
	}
	if !ok {
		return fmt.Errorf("%s: %v is not %s", path, v, name)
	}
	return nil
}

func jsonEqual(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		return ok && x.String() == y.String()
	}
	return reflect.DeepEqual(a, b)
}

// enum is the closed list a $defs entry holds.
func (sc *schema) enum(name string) []string {
	def, _ := sc.defs[name].(map[string]any)
	vals, _ := def["enum"].([]any)
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, v.(string))
	}
	return out
}

// kinds is the discriminator values of a $defs union, in its order.
func (sc *schema) kinds(name, disc string) []string {
	def, _ := sc.defs[name].(map[string]any)
	alts, _ := def["oneOf"].([]any)
	var out []string
	for _, a := range alts {
		props, _ := a.(map[string]any)["properties"].(map[string]any)
		c, _ := props[disc].(map[string]any)
		if k, ok := c["const"].(string); ok && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}
