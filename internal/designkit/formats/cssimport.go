package formats

import (
	"fmt"
	"regexp"
	"strings"
)

// Tokens from a stylesheet of CSS custom properties: `:root { --x: … }` is the default mode, a dark selector
// ([data-theme="dark"], .dark, @media (prefers-color-scheme: dark)) its own mode. The token's `var` is the property.

type cssDecl struct {
	Name, Value, Comment string
}

type cssRule struct {
	Selectors []string
	Context   []string // enclosing at-rule preludes: "@media (prefers-color-scheme: dark)", "@layer base", "@theme"
	Decls     []cssDecl
}

// parseCSS reads rules with their declarations; it keeps the comment next to a custom property (the line above it or
// after it on its line) as that property's usage.
func parseCSS(src string) []cssRule {
	var rules []cssRule
	var parse func(s string, ctx []string)
	parse = func(s string, ctx []string) {
		i := 0
		for i < len(s) {
			// skip blanks and comments between rules
			for i < len(s) {
				if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
					i++
				} else if strings.HasPrefix(s[i:], "/*") {
					end := strings.Index(s[i+2:], "*/")
					if end < 0 {
						return
					}
					i += end + 4
				} else {
					break
				}
			}
			if i >= len(s) {
				return
			}
			start := i
			// prelude until { or ; at depth 0
			depth := 0
			var quote byte
			for i < len(s) {
				c := s[i]
				if quote != 0 {
					if c == quote && s[i-1] != '\\' {
						quote = 0
					}
				} else if c == '"' || c == '\'' {
					quote = c
				} else if c == '(' {
					depth++
				} else if c == ')' && depth > 0 {
					depth--
				} else if depth == 0 && (c == '{' || c == ';') {
					break
				} else if strings.HasPrefix(s[i:], "/*") {
					end := strings.Index(s[i+2:], "*/")
					if end < 0 {
						return
					}
					i += end + 3
				}
				i++
			}
			prelude := strings.TrimSpace(stripComments(s[start:min(i, len(s))]))
			if i >= len(s) || s[i] == ';' {
				i++
				continue
			}
			// block: find the matching }
			bodyStart := i + 1
			d := 1
			j := bodyStart
			quote = 0
			for j < len(s) && d > 0 {
				c := s[j]
				switch {
				case quote != 0:
					if c == quote && s[j-1] != '\\' {
						quote = 0
					}
				case c == '"' || c == '\'':
					quote = c
				case strings.HasPrefix(s[j:], "/*"):
					end := strings.Index(s[j+2:], "*/")
					if end < 0 {
						j = len(s)
						continue
					}
					j += end + 3
				case c == '{':
					d++
				case c == '}':
					d--
				}
				j++
			}
			body := s[bodyStart:min(j-1, len(s))]
			i = j
			if strings.HasPrefix(prelude, "@") {
				name := strings.ToLower(strings.Fields(prelude + " ")[0])
				switch name {
				case "@media", "@supports", "@layer", "@container", "@scope":
					parse(body, append(append([]string{}, ctx...), prelude))
				case "@theme":
					rules = append(rules, cssRule{Selectors: []string{"@theme"}, Context: ctx, Decls: parseDecls(body)})
				default: // @font-face, @keyframes, @property…
				}
				continue
			}
			var sels []string
			for _, p := range splitTop(prelude, ',') {
				if p != "" {
					sels = append(sels, p)
				}
			}
			rules = append(rules, cssRule{Selectors: sels, Context: append([]string{}, ctx...), Decls: parseDecls(body)})
		}
	}
	parse(src, nil)
	return rules
}

func stripComments(s string) string {
	for {
		a := strings.Index(s, "/*")
		if a < 0 {
			return s
		}
		b := strings.Index(s[a+2:], "*/")
		if b < 0 {
			return s[:a]
		}
		s = s[:a] + s[a+b+4:]
	}
}

// parseDecls reads the `--name: value` declarations of a block body; other properties and nested rules are skipped.
func parseDecls(body string) []cssDecl {
	var out []cssDecl
	pending := ""
	i := 0
	for i < len(body) {
		c := body[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ';':
			i++
		case strings.HasPrefix(body[i:], "/*"):
			end := strings.Index(body[i+2:], "*/")
			if end < 0 {
				return out
			}
			pending = strings.TrimSpace(body[i+2 : i+2+end])
			i += end + 4
		default:
			// a declaration (or a nested rule) up to ; or a balanced {…}
			start := i
			depth, brace := 0, 0
			var quote byte
			for i < len(body) {
				ch := body[i]
				if quote != 0 {
					if ch == quote && body[i-1] != '\\' {
						quote = 0
					}
				} else if ch == '"' || ch == '\'' {
					quote = ch
				} else if ch == '(' {
					depth++
				} else if ch == ')' && depth > 0 {
					depth--
				} else if ch == '{' {
					brace++
				} else if ch == '}' && brace > 0 {
					brace--
					if brace == 0 {
						i++
						break
					}
				} else if ch == ';' && depth == 0 && brace == 0 {
					break
				}
				i++
			}
			text := strings.TrimSpace(stripComments(body[start:min(i, len(body))]))
			name, value, ok := strings.Cut(text, ":")
			name = strings.TrimSpace(name)
			if !ok || !strings.HasPrefix(name, "--") || strings.Contains(value, "{") {
				pending = ""
				continue
			}
			d := cssDecl{Name: name, Value: strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important")), Comment: pending}
			pending = ""
			// a comment after the ; on the same line is this declaration's
			j := i
			if j < len(body) && body[j] == ';' {
				j++
			}
			k := j
			for k < len(body) && (body[k] == ' ' || body[k] == '\t') {
				k++
			}
			if strings.HasPrefix(body[k:], "/*") {
				if end := strings.Index(body[k+2:], "*/"); end >= 0 && !strings.Contains(body[j:k+2], "\n") {
					d.Comment = strings.TrimSpace(body[k+2 : k+2+end])
					i = k + end + 4
				}
			}
			out = append(out, d)
		}
	}
	return out
}

var (
	dataModeRe = regexp.MustCompile(`\[data-(?:theme|mode|color-scheme|bs-theme)\s*=\s*["']?([A-Za-z0-9_-]+)["']?\]`)
	// A class that names a mode: .dark, .light, .theme-x, .x-theme, .x-mode, alone or on :root, html, body.
	classModeRe = regexp.MustCompile(`^(?::root|html|body)?\.(theme-[a-z0-9_-]+|[a-z0-9_-]+-(?:theme|mode)|dark|light)$`)
	notRe       = regexp.MustCompile(`:not\([^)]*\)`)
	varRefRe    = regexp.MustCompile(`^var\(\s*(--[A-Za-z0-9_-]+)\s*(?:,\s*(.+))?\)$`)
)

// selectorMode says which mode a selector writes: "" is the default (:root), "-" is a selector this import does not
// read, anything else a mode id.
func selectorMode(sel string, ctx []string) string {
	media := ""
	for _, c := range ctx {
		low := strings.ToLower(c)
		switch {
		case strings.HasPrefix(low, "@media"):
			if strings.Contains(low, "prefers-color-scheme") {
				if strings.Contains(low, "dark") {
					media = "dark"
				} else {
					media = "light"
				}
			} else {
				return "-"
			}
		case strings.HasPrefix(low, "@layer"), strings.HasPrefix(low, "@supports"):
		default:
			return "-"
		}
	}
	sel = strings.TrimSpace(notRe.ReplaceAllString(sel, ""))
	low := strings.ToLower(sel)
	var id string
	if m := dataModeRe.FindStringSubmatch(sel); m != nil {
		id = strings.ToLower(m[1])
	} else if m := classModeRe.FindStringSubmatch(low); m != nil {
		id = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSuffix(m[1], "-mode"), "theme-"), "-theme")
	}
	switch low {
	case ":root", "html", ":host", "@theme", "body":
		if media != "" {
			return media
		}
		return ""
	}
	if id != "" {
		return id
	}
	if media != "" && (low == ":root" || low == "html") {
		return media
	}
	return "-"
}

// CSSOptions steer an import of CSS variables.
type CSSOptions struct {
	Prefix string // the kit's tokenPrefix: stripped from the property to make the token's name
	Modes  []Mode // the kit's modes; the first is what :root means
}

type cssVar struct {
	prop   string
	name   string
	values map[string]string // mode ("" = default) → raw value
	order  []string
	usage  string
}

// ImportCSS reads CSS custom properties into tokens.
func ImportCSS(src string, opt CSSOptions) (*Tokens, *Report, error) {
	rep := &Report{}
	rules := parseCSS(src)
	defaultMode := "light"
	if len(opt.Modes) > 0 {
		defaultMode = opt.Modes[0].ID
	}
	vars := map[string]*cssVar{}
	var order []string
	skippedSel := map[string]bool{}
	for _, r := range rules {
		for _, sel := range r.Selectors {
			mode := selectorMode(sel, r.Context)
			if mode == "-" {
				if len(r.Decls) > 0 {
					skippedSel[sel] = true
				}
				continue
			}
			if mode == "" {
				mode = defaultMode
			}
			for _, d := range r.Decls {
				v := vars[d.Name]
				if v == nil {
					v = &cssVar{prop: d.Name, values: map[string]string{}}
					vars[d.Name] = v
					order = append(order, d.Name)
				}
				if _, seen := v.values[mode]; !seen {
					v.order = append(v.order, mode)
				}
				v.values[mode] = d.Value
				if d.Comment != "" && v.usage == "" {
					v.usage = d.Comment
				}
			}
		}
	}
	if len(vars) == 0 {
		return nil, rep, fmt.Errorf("no CSS custom properties (--name: value) under :root, a [data-theme] selector or @theme")
	}
	for sel := range skippedSel {
		rep.notCarried("selector %s: variables under it are component- or page-specific, not tokens", truncate(sel, 60))
	}
	prefix := strings.TrimPrefix(opt.Prefix, "--")
	nameOf := func(prop string) string {
		n := strings.TrimPrefix(prop, "--")
		if prefix != "" && strings.HasPrefix(n, prefix) && len(n) > len(prefix) {
			n = n[len(prefix):]
		}
		return n
	}
	for _, v := range vars {
		v.name = nameOf(v.prop)
	}
	// Follow var(--x) to a literal: for classification and for families that cannot hold an alias.
	var literal func(prop, mode string, depth int) (string, bool)
	literal = func(prop, mode string, depth int) (string, bool) {
		v := vars[prop]
		if v == nil || depth > 16 {
			return "", false
		}
		raw, ok := v.values[mode]
		if !ok {
			raw, ok = v.values[defaultMode]
		}
		if !ok {
			return "", false
		}
		if m := varRefRe.FindStringSubmatch(raw); m != nil {
			if s, ok := literal(m[1], mode, depth+1); ok {
				return s, true
			}
			if m[2] != "" {
				return strings.TrimSpace(m[2]), true
			}
			return "", false
		}
		return raw, true
	}

	out := &Tokens{Extra: rawMap{}}
	modesSeen := []string{}
	seenMode := func(m string) {
		for _, x := range modesSeen {
			if x == m {
				return
			}
		}
		modesSeen = append(modesSeen, m)
	}
	var fontFamilies []KV
	for _, prop := range order {
		v := vars[prop]
		def, ok := literal(prop, defaultMode, 0)
		if !ok {
			def, ok = literal(prop, v.order[0], 0)
		}
		if !ok {
			rep.notCarried("%s: its value %s refers to a property this file does not define", prop, truncate(v.values[v.order[0]], 40))
			continue
		}
		fam := cssFamily(v.name, def)
		switch fam {
		case "":
			rep.notCarried("%s: %s is not a colour, a length, a shadow or a font family (z-index, duration, calc, color-mix…)", prop, truncate(def, 40))
			continue
		case "font":
			key := strings.TrimPrefix(strings.TrimPrefix(v.name, "font-"), "font")
			key = sanitizeName(strings.Trim(key, "-"))
			if key == "" {
				key = sanitizeName(v.name)
			}
			fontFamilies = append(fontFamilies, KV{key, def})
			continue
		}
		val := func(mode string) (string, bool) {
			raw, ok := v.values[mode]
			if !ok {
				return "", false
			}
			if m := varRefRe.FindStringSubmatch(raw); m != nil && fam == "color" {
				if t := vars[m[1]]; t != nil && cssFamily(t.name, mustLiteral(literal, m[1], mode)) == "color" {
					return "{" + t.name + "}", true
				}
			}
			lit, ok := literal(prop, mode, 0)
			if !ok {
				return "", false
			}
			if fam == "color" {
				c, ok := normalizeColor(lit)
				if !ok {
					rep.notCarried("%s (%s): %s cannot be a kit colour — resolve it to hex, rgb(), hsl() or oklch()", prop, mode, truncate(lit, 40))
					return "", false
				}
				return c, true
			}
			return lit, true
		}
		var value Value
		if len(v.order) == 1 && v.order[0] == defaultMode {
			s, ok := val(defaultMode)
			if !ok {
				continue
			}
			value = Value{Plain: s}
		} else {
			for _, m := range v.order {
				s, ok := val(m)
				if !ok {
					continue
				}
				value.Modes = append(value.Modes, ModeValue{m, s})
				seenMode(m)
			}
			if len(value.Modes) == 0 {
				continue
			}
			// the default mode always first
			for i, mv := range value.Modes {
				if mv.Mode == defaultMode && i > 0 {
					value.Modes = append([]ModeValue{mv}, append(value.Modes[:i:i], value.Modes[i+1:]...)...)
					break
				}
			}
			if len(value.Modes) == 1 && value.Modes[0].Mode == defaultMode {
				value = Value{Plain: value.Modes[0].Value}
			}
		}
		out.Fam(fam).Tokens = append(out.Fam(fam).Tokens, Token{Name: sanitizeName(v.name), Value: value, Usage: v.usage, Var: prop, Extra: rawMap{}})
	}
	if len(fontFamilies) > 0 {
		out.Type = &Type{Families: fontFamilies, Extra: rawMap{}}
	}
	if len(modesSeen) > 0 {
		th := append([]Mode(nil), opt.Modes...)
		if len(th) == 0 {
			th = []Mode{{ID: defaultMode, Name: titleOf(defaultMode)}}
		}
		for _, id := range modesSeen {
			found := false
			for _, m := range th {
				found = found || m.ID == id
			}
			if !found {
				th = append(th, Mode{ID: id, Name: titleOf(id)})
			}
		}
		out.Fam("color").Themes = th
	}
	for _, f := range out.Families {
		rep.carried("%s: %d tokens (each keeps its property as var)", f.Key, len(f.Tokens))
	}
	if out.Type != nil {
		rep.carried("type.families: %d font families", len(out.Type.Families))
	}
	rep.notCarried("text styles (type.groups), font files and components: a stylesheet of variables has none")
	if out.Count() == 0 && out.Type == nil {
		return nil, rep, fmt.Errorf("none of the %d custom properties is a colour, a length, a shadow or a font family", len(vars))
	}
	return out, rep, nil
}

func mustLiteral(f func(string, string, int) (string, bool), prop, mode string) string {
	s, _ := f(prop, mode, 0)
	return s
}

var (
	radiusNameRe = regexp.MustCompile(`(?i)radius|rounded|corner`)
	shadowNameRe = regexp.MustCompile(`(?i)shadow|elevation`)
	spaceNameRe  = regexp.MustCompile(`(?i)space|spacing|gap|gutter|padding|margin|size|width|height|inset|indent|container`)
	fontNameRe   = regexp.MustCompile(`(?i)font`)
	fontNotRe    = regexp.MustCompile(`(?i)size|weight|height|style|stretch|feature|variant|leading|tracking`)
)

// cssFamily is the family a custom property belongs to: color, spacing, radius, shadow, font; "" when it is none.
func cssFamily(name, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, ok := normalizeColor(value); ok && !strings.Contains(strings.ToLower(name), "shadow") {
		return "color"
	}
	isLen := lenRe.MatchString(value) || value == "0"
	switch {
	case shadowNameRe.MatchString(name) && !isLen && !colorFnRe.MatchString(value):
		return "shadow"
	case radiusNameRe.MatchString(name) && (isLen || strings.Contains(value, " ")):
		return "radius"
	case fontNameRe.MatchString(name) && !fontNotRe.MatchString(name) && !isLen && !plainNumberRe.MatchString(value):
		return "font"
	case spaceNameRe.MatchString(name) && isLen:
		return "spacing"
	}
	return ""
}

var namedColors = map[string]string{"white": "#ffffff", "black": "#000000", "transparent": "rgb(0 0 0 / 0)"}

// normalizeColor accepts what a kit holds as a colour; the few CSS names the readers drop become values.
func normalizeColor(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if looksLikeColor(v) && !strings.Contains(strings.ToLower(v), "var(") && !strings.Contains(strings.ToLower(v), "color-mix(") {
		return v, true
	}
	if c, ok := namedColors[strings.ToLower(v)]; ok {
		return c, true
	}
	return "", false
}
