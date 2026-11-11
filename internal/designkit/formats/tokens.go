package formats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The kit's tokens.json as a typed model that writes back what it read: a family is a list of tokens, a colour has a
// value per mode, `type` holds the font families and the text styles. Members this model does not know are kept
// (Extra) and written back, so a conversion never drops what it does not understand.

// Mode is a colour mode of the kit: light, dark.
type Mode struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ModeValue is one mode's value of a token.
type ModeValue struct{ Mode, Value string }

// Value is a token's value: one string for every mode, or a value per mode (the first is the default).
type Value struct {
	Plain string
	Modes []ModeValue
}

// Default is the value of the first mode.
func (v Value) Default() string {
	if len(v.Modes) > 0 {
		return v.Modes[0].Value
	}
	return v.Plain
}

// For is the value in a mode; a mode it has no value for takes the default.
func (v Value) For(mode string) string {
	for _, m := range v.Modes {
		if m.Mode == mode {
			return m.Value
		}
	}
	return v.Default()
}

// Has is whether the value is written for the mode itself.
func (v Value) Has(mode string) bool {
	for _, m := range v.Modes {
		if m.Mode == mode {
			return true
		}
	}
	return false
}

// With returns the value with the mode set.
func (v Value) With(mode, val string) Value {
	out := Value{Plain: v.Plain, Modes: append([]ModeValue(nil), v.Modes...)}
	for i := range out.Modes {
		if out.Modes[i].Mode == mode {
			out.Modes[i].Value = val
			return out
		}
	}
	out.Modes = append(out.Modes, ModeValue{mode, val})
	return out
}

func (v *Value) UnmarshalJSON(raw []byte) error {
	if s, ok := jsonString(raw); ok {
		*v = Value{Plain: s}
		return nil
	}
	o, ok := parseOrdered(raw)
	if !ok {
		return fmt.Errorf("a token value is a string or {mode: value}, got %s", string(raw))
	}
	*v = Value{}
	for _, k := range o.keys {
		s, ok := jsonString(o.vals[k])
		if !ok {
			return fmt.Errorf("a token value in mode %s is not a string", k)
		}
		v.Modes = append(v.Modes, ModeValue{k, s})
	}
	return nil
}

func (v Value) MarshalJSON() ([]byte, error) {
	if len(v.Modes) == 0 {
		return marshal(v.Plain), nil
	}
	o := newOrdered()
	for _, m := range v.Modes {
		o.set(m.Mode, m.Value)
	}
	return o.MarshalJSON()
}

// Token is one entry of a family.
type Token struct {
	Name  string
	Value Value
	Usage string
	Var   string
	Extra rawMap
}

func (t *Token) UnmarshalJSON(raw []byte) error {
	o, ok := parseOrdered(raw)
	if !ok {
		return errors.New("a token is an object")
	}
	*t = Token{Extra: rawMap{}}
	for _, k := range o.keys {
		switch k {
		case "name":
			t.Name = o.str(k)
		case "usage":
			t.Usage = o.str(k)
		case "var":
			t.Var = o.str(k)
		case "value":
			if err := json.Unmarshal(o.vals[k], &t.Value); err != nil {
				return fmt.Errorf("token %s: %w", t.Name, err)
			}
		default:
			t.Extra[k] = o.vals[k]
		}
	}
	return nil
}

func (t Token) MarshalJSON() ([]byte, error) {
	o := newOrdered()
	o.set("name", t.Name)
	o.set("value", t.Value)
	if t.Usage != "" {
		o.set("usage", t.Usage)
	}
	if t.Var != "" {
		o.set("var", t.Var)
	}
	for _, k := range t.Extra.keys() {
		o.set(k, t.Extra[k])
	}
	return o.MarshalJSON()
}

// Family is a list of tokens under one key: color, spacing, radius, shadow or a family of the kit's own.
type Family struct {
	Key    string
	Themes []Mode
	Tokens []Token
	Extra  rawMap
}

func (f *Family) UnmarshalJSON(raw []byte) error {
	o, ok := parseOrdered(raw)
	if !ok {
		return errors.New("a family is an object")
	}
	*f = Family{Extra: rawMap{}}
	for _, k := range o.keys {
		switch k {
		case "themes":
			if err := json.Unmarshal(o.vals[k], &f.Themes); err != nil {
				return err
			}
		case "tokens":
			if err := json.Unmarshal(o.vals[k], &f.Tokens); err != nil {
				return err
			}
		default:
			f.Extra[k] = o.vals[k]
		}
	}
	return nil
}

func (f Family) MarshalJSON() ([]byte, error) {
	o := newOrdered()
	if len(f.Themes) > 0 {
		o.set("themes", f.Themes)
	}
	toks := f.Tokens
	if toks == nil {
		toks = []Token{}
	}
	o.set("tokens", toks)
	for _, k := range f.Extra.keys() {
		o.set(k, f.Extra[k])
	}
	return o.MarshalJSON()
}

// KV is a font family: its key in `type.families` and the CSS value.
type KV struct{ Key, Value string }

// Style is a text style of a group.
type Style struct {
	Name          string
	FontSize      string
	LineHeight    string
	LetterSpacing string
	FontWeight    json.RawMessage
	Extra         rawMap
}

// StyleGroup is a named group of text styles that share a font family.
type StyleGroup struct {
	Name   string
	Family string
	Styles []Style
	Extra  rawMap
}

// Type is tokens.json's `type`: font files, families, text styles.
type Type struct {
	Fonts    []json.RawMessage
	Families []KV
	Groups   []StyleGroup
	Extra    rawMap
}

func (s *Style) UnmarshalJSON(raw []byte) error {
	o, ok := parseOrdered(raw)
	if !ok {
		return errors.New("a text style is an object")
	}
	*s = Style{Extra: rawMap{}}
	for _, k := range o.keys {
		switch k {
		case "name":
			s.Name = o.str(k)
		case "fontSize":
			s.FontSize = scalar(o.vals[k])
		case "lineHeight":
			s.LineHeight = scalar(o.vals[k])
		case "letterSpacing":
			s.LetterSpacing = scalar(o.vals[k])
		case "fontWeight":
			s.FontWeight = o.vals[k]
		default:
			s.Extra[k] = o.vals[k]
		}
	}
	return nil
}

// scalar reads a JSON string or number as text ("15px", 1.5).
func scalar(raw json.RawMessage) string {
	if s, ok := jsonString(raw); ok {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func (s Style) MarshalJSON() ([]byte, error) {
	o := newOrdered()
	o.set("name", s.Name)
	if s.FontSize != "" {
		o.set("fontSize", s.FontSize)
	}
	if s.LineHeight != "" {
		o.set("lineHeight", s.LineHeight)
	}
	if s.LetterSpacing != "" {
		o.set("letterSpacing", s.LetterSpacing)
	}
	if len(s.FontWeight) > 0 {
		o.set("fontWeight", s.FontWeight)
	}
	for _, k := range s.Extra.keys() {
		o.set(k, s.Extra[k])
	}
	return o.MarshalJSON()
}

func (g *StyleGroup) UnmarshalJSON(raw []byte) error {
	o, ok := parseOrdered(raw)
	if !ok {
		return errors.New("a style group is an object")
	}
	*g = StyleGroup{Extra: rawMap{}}
	for _, k := range o.keys {
		switch k {
		case "name":
			g.Name = o.str(k)
		case "family":
			g.Family = o.str(k)
		case "styles":
			if err := json.Unmarshal(o.vals[k], &g.Styles); err != nil {
				return err
			}
		default:
			g.Extra[k] = o.vals[k]
		}
	}
	return nil
}

func (g StyleGroup) MarshalJSON() ([]byte, error) {
	o := newOrdered()
	o.set("name", g.Name)
	if g.Family != "" {
		o.set("family", g.Family)
	}
	styles := g.Styles
	if styles == nil {
		styles = []Style{}
	}
	o.set("styles", styles)
	for _, k := range g.Extra.keys() {
		o.set(k, g.Extra[k])
	}
	return o.MarshalJSON()
}

func (t *Type) UnmarshalJSON(raw []byte) error {
	o, ok := parseOrdered(raw)
	if !ok {
		return errors.New("type is an object")
	}
	*t = Type{Extra: rawMap{}}
	for _, k := range o.keys {
		switch k {
		case "fonts":
			if err := json.Unmarshal(o.vals[k], &t.Fonts); err != nil {
				return err
			}
		case "families":
			fam, ok := parseOrdered(o.vals[k])
			if !ok {
				return errors.New("type.families is an object")
			}
			for _, fk := range fam.keys {
				t.Families = append(t.Families, KV{fk, fam.str(fk)})
			}
		case "groups":
			if err := json.Unmarshal(o.vals[k], &t.Groups); err != nil {
				return err
			}
		default:
			t.Extra[k] = o.vals[k]
		}
	}
	return nil
}

func (t Type) MarshalJSON() ([]byte, error) {
	o := newOrdered()
	if len(t.Fonts) > 0 {
		o.set("fonts", t.Fonts)
	}
	if len(t.Families) > 0 {
		fam := newOrdered()
		for _, kv := range t.Families {
			fam.set(kv.Key, kv.Value)
		}
		o.set("families", fam)
	}
	if len(t.Groups) > 0 {
		o.set("groups", t.Groups)
	}
	for _, k := range t.Extra.keys() {
		o.set(k, t.Extra[k])
	}
	return o.MarshalJSON()
}

// Family returns the CSS value of a font family key.
func (t *Type) Family(key string) (string, bool) {
	if t == nil {
		return "", false
	}
	for _, kv := range t.Families {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return "", false
}

// Tokens is a whole tokens.json.
type Tokens struct {
	Name     string
	Version  json.RawMessage
	Families []Family
	Type     *Type
	Extra    rawMap // members that are neither a family nor `type`, kept as they were
	order    []string
}

// ParseTokens reads tokens.json.
func ParseTokens(raw []byte) (*Tokens, error) {
	o, ok := parseOrdered(raw)
	if !ok {
		return nil, errors.New("tokens.json is not a JSON object")
	}
	t := &Tokens{Extra: rawMap{}}
	for _, k := range o.keys {
		t.order = append(t.order, k)
		switch k {
		case "name":
			t.Name = o.str(k)
		case "version":
			t.Version = o.vals[k]
		case "type":
			t.Type = &Type{}
			if err := json.Unmarshal(o.vals[k], t.Type); err != nil {
				return nil, fmt.Errorf("type: %w", err)
			}
		case "meta":
			t.Extra[k] = o.vals[k]
		default:
			var probe struct {
				Tokens *json.RawMessage `json:"tokens"`
			}
			if json.Unmarshal(o.vals[k], &probe) != nil || probe.Tokens == nil {
				t.Extra[k] = o.vals[k]
				continue
			}
			f := Family{Key: k}
			if err := json.Unmarshal(o.vals[k], &f); err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			f.Key = k
			t.Families = append(t.Families, f)
		}
	}
	return t, nil
}

// Marshal writes tokens.json with the members in the order they were read; new ones go last.
func (t *Tokens) Marshal() []byte {
	o := newOrdered()
	written := map[string]bool{}
	put := func(k string) {
		if written[k] {
			return
		}
		switch {
		case k == "name":
			if t.Name != "" {
				o.set("name", t.Name)
			}
		case k == "version":
			if len(t.Version) > 0 {
				o.set("version", t.Version)
			}
		case k == "type":
			if t.Type != nil {
				o.set("type", t.Type)
			}
		default:
			if f := t.family(k); f != nil {
				o.set(k, f)
			} else if raw, ok := t.Extra[k]; ok {
				o.set(k, raw)
			}
		}
		written[k] = true
	}
	for _, k := range t.order {
		put(k)
	}
	put("name")
	put("version")
	for _, f := range t.Families {
		put(f.Key)
	}
	put("type")
	for _, k := range t.Extra.keys() {
		put(k)
	}
	return pretty(o)
}

func (t *Tokens) family(key string) *Family {
	for i := range t.Families {
		if t.Families[i].Key == key {
			return &t.Families[i]
		}
	}
	return nil
}

// Fam is the family under key, created (last) when the kit has none yet.
func (t *Tokens) Fam(key string) *Family {
	if f := t.family(key); f != nil {
		return f
	}
	t.Families = append(t.Families, Family{Key: key})
	return &t.Families[len(t.Families)-1]
}

// Modes are the colour modes of the kit; a kit without any has light.
func (t *Tokens) Modes() []Mode {
	if f := t.family("color"); f != nil && len(f.Themes) > 0 {
		return f.Themes
	}
	return []Mode{{ID: "light", Name: "Light"}}
}

// Find is the token called name and the family it is in.
func (t *Tokens) Find(name string) (*Token, string) {
	for i := range t.Families {
		for j := range t.Families[i].Tokens {
			if t.Families[i].Tokens[j].Name == name {
				return &t.Families[i].Tokens[j], t.Families[i].Key
			}
		}
	}
	return nil, ""
}

// Count is the number of tokens in all families.
func (t *Tokens) Count() int {
	n := 0
	for _, f := range t.Families {
		n += len(f.Tokens)
	}
	return n
}

// LoadTokens reads <folder>/tokens.json; a missing file is an empty set of tokens (found false).
func LoadTokens(folder string) (t *Tokens, found bool, err error) {
	raw, err := os.ReadFile(filepath.Join(folder, "tokens.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return &Tokens{Extra: rawMap{}}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	t, err = ParseTokens(raw)
	return t, true, err
}
