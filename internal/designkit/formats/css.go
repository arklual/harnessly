package formats

import (
	"fmt"
	"sort"
	"strings"
)

// KitInfo is what a conversion needs to know about the kit besides its tokens.
type KitInfo struct {
	ID     string
	Title  string
	Prefix string // the kit's tokenPrefix, "--acme-": a token without its own `var` is published as Prefix+name
}

func (k KitInfo) prefix() string {
	if k.Prefix != "" {
		return k.Prefix
	}
	if k.ID != "" {
		return "--" + k.ID + "-"
	}
	return "--"
}

// VarOf is the CSS custom property a token is published as.
func (k KitInfo) VarOf(t Token) string {
	if t.Var != "" {
		return t.Var
	}
	return k.prefix() + strings.ReplaceAll(t.Name, ".", "-")
}

func (k KitInfo) fontVar(key string) string { return k.prefix() + "font-" + key }

// modeSelector is the selector a mode's values live under.
func modeSelector(i int, id string) string {
	if i == 0 {
		return fmt.Sprintf(`:root, [data-theme="%s"]`, safeModeID(id))
	}
	return fmt.Sprintf(`[data-theme="%s"]`, safeModeID(id))
}

// cssValue is a token value as CSS: an alias {name} becomes var(--its-variable).
func cssValue(t *Tokens, info KitInfo, v string, rep *Report) string {
	if m := aliasRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
		if target, _ := t.Find(m[1]); target != nil {
			return "var(" + info.VarOf(*target) + ")"
		}
		rep.notCarried("alias {%s} points at no token of the kit: written as it is", m[1])
	}
	return v
}

// ExportCSS writes the tokens as CSS custom properties: the first mode under :root, every other under
// [data-theme="<mode>"], with only the tokens that have a value of their own in that mode.
func ExportCSS(t *Tokens, info KitInfo) ([]byte, *Report) {
	rep := &Report{}
	modes := t.Modes()
	blocks := make([][]string, len(modes))
	line := func(name, value, usage string) string {
		if unsafeCSS(value) || unsafeCSS(name) {
			rep.notCarried("%s: its value or name holds ; { } < > or a comment mark, which would break the stylesheet: skipped", name)
			return "  /* " + "skipped" + " */"
		}
		s := fmt.Sprintf("  %s: %s;", name, value)
		if usage != "" {
			s += " /* " + strings.ReplaceAll(usage, "*/", "* /") + " */"
		}
		return s
	}
	for _, f := range t.Families {
		for _, tok := range f.Tokens {
			name := info.VarOf(tok)
			if len(tok.Value.Modes) == 0 {
				blocks[0] = append(blocks[0], line(name, cssValue(t, info, tok.Value.Plain, rep), tok.Usage))
				continue
			}
			for i, m := range modes {
				if tok.Value.Has(m.ID) {
					u := ""
					if i == 0 {
						u = tok.Usage
					}
					blocks[i] = append(blocks[i], line(name, cssValue(t, info, tok.Value.For(m.ID), rep), u))
				}
			}
			for _, mv := range tok.Value.Modes {
				known := false
				for _, m := range modes {
					known = known || m.ID == mv.Mode
				}
				if !known {
					rep.notCarried("%s: a value for the mode %q, which the kit's colour themes do not list", tok.Name, mv.Mode)
				}
			}
		}
		rep.carried("%s: %d tokens", f.Key, len(f.Tokens))
	}
	if t.Type != nil {
		for _, kv := range t.Type.Families {
			blocks[0] = append(blocks[0], line(info.fontVar(kv.Key), kv.Value, ""))
		}
		rep.carried("type.families: %d font families as --…font-<key>", len(t.Type.Families))
		if len(t.Type.Groups) > 0 {
			rep.notCarried("type.groups (text styles): CSS variables hold single values; take the styles from DTCG or DESIGN.md")
		}
		if len(t.Type.Fonts) > 0 {
			rep.notCarried("type.fonts (font files): @font-face stays in fonts.css")
		}
	}
	rep.notCarried("components, cards, templates and assets: a stylesheet of variables holds tokens only")
	var b strings.Builder
	title := firstNonEmpty(info.Title, t.Name, info.ID, "Kit")
	fmt.Fprintf(&b, "/* %s tokens as CSS variables. First mode under :root, the others under [data-theme=\"<mode>\"]. */\n", title)
	for i, m := range modes {
		if len(blocks[i]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s {\n%s\n}\n", modeSelector(i, m.ID), strings.Join(blocks[i], "\n"))
	}
	return []byte(b.String()), rep
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// twName is a token's key in Tailwind's scale: the family's own word in front is dropped when that leaves a unique
// name (radius-m → m: rounded-m), kept when it would clash.
func twNames(f Family, words ...string) map[string]string {
	out := map[string]string{}
	stripped := map[string]int{}
	for _, tok := range f.Tokens {
		stripped[stripWord(tok.Name, words)]++
	}
	for _, tok := range f.Tokens {
		s := stripWord(tok.Name, words)
		if s == "" || stripped[s] > 1 {
			s = tok.Name
		}
		out[tok.Name] = s
	}
	return out
}

func stripWord(name string, words []string) string {
	for _, w := range words {
		if strings.HasPrefix(name, w+"-") {
			return strings.TrimPrefix(name, w+"-")
		}
	}
	return name
}

type twScale struct {
	family string
	v3     string
	v4     string
	words  []string
}

var twScales = []twScale{
	{"color", "colors", "color", nil},
	{"spacing", "spacing", "spacing", []string{"space", "spacing"}},
	{"radius", "borderRadius", "radius", []string{"radius", "rounded"}},
	{"shadow", "boxShadow", "shadow", []string{"shadow", "elevation"}},
}

func jsKey(s string) string { return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'" }

// ExportTailwind writes the tokens for Tailwind: v3 is a preset with theme.extend, v4 is CSS with @theme. Both point
// at the kit's CSS variables, so one class works in every mode.
func ExportTailwind(t *Tokens, info KitInfo, v4 bool) ([]byte, *Report) {
	rep := &Report{}
	modes := t.Modes()
	if v4 {
		css, r := ExportCSS(t, info)
		*rep = *r
		var b strings.Builder
		fmt.Fprintf(&b, "/* %s tokens for Tailwind CSS v4: put after @import \"tailwindcss\". */\n", firstNonEmpty(info.Title, t.Name, info.ID, "Kit"))
		for i, m := range modes {
			if i == 0 {
				continue
			}
			fmt.Fprintf(&b, "@custom-variant %s (&:where([data-theme=\"%s\"], [data-theme=\"%s\"] *));\n", safeModeID(m.ID), safeModeID(m.ID), safeModeID(m.ID))
		}
		b.Write(css)
		b.WriteString("@theme inline {\n")
		for _, sc := range twScales {
			f := t.family(sc.family)
			if f == nil {
				continue
			}
			names := twNames(*f, sc.words...)
			for _, tok := range f.Tokens {
				fmt.Fprintf(&b, "  --%s-%s: var(%s);\n", sc.v4, names[tok.Name], info.VarOf(tok))
			}
		}
		if t.Type != nil {
			for _, kv := range t.Type.Families {
				fmt.Fprintf(&b, "  --font-%s: var(%s);\n", kv.Key, info.fontVar(kv.Key))
			}
			for _, g := range t.Type.Groups {
				for _, st := range g.Styles {
					if st.FontSize == "" {
						continue
					}
					fmt.Fprintf(&b, "  --text-%s: %s;\n", st.Name, st.FontSize)
					if st.LineHeight != "" {
						fmt.Fprintf(&b, "  --text-%s--line-height: %s;\n", st.Name, st.LineHeight)
					}
					if len(st.FontWeight) > 0 {
						fmt.Fprintf(&b, "  --text-%s--font-weight: %s;\n", st.Name, scalar(st.FontWeight))
					}
					if st.LetterSpacing != "" {
						fmt.Fprintf(&b, "  --text-%s--letter-spacing: %s;\n", st.Name, st.LetterSpacing)
					}
				}
			}
		}
		b.WriteString("}\n")
		rep.carried("@theme inline: colour, spacing, radius, shadow, font and text-size keys pointing at the variables")
		return []byte(b.String()), rep
	}
	var b strings.Builder
	fmt.Fprintf(&b, "/* %s tokens for Tailwind CSS v3: presets: [require('./thisfile')]. The values are the kit's CSS variables (kit convert --to css). */\n", firstNonEmpty(info.Title, t.Name, info.ID, "Kit"))
	b.WriteString("module.exports = {\n")
	for i, m := range modes {
		if i == 1 {
			fmt.Fprintf(&b, "  darkMode: ['selector', %s],\n", jsKey(`[data-theme="`+safeModeID(m.ID)+`"]`))
		}
	}
	b.WriteString("  theme: {\n    extend: {\n")
	for _, sc := range twScales {
		f := t.family(sc.family)
		if f == nil || len(f.Tokens) == 0 {
			continue
		}
		names := twNames(*f, sc.words...)
		fmt.Fprintf(&b, "      %s: {\n", sc.v3)
		for _, tok := range f.Tokens {
			fmt.Fprintf(&b, "        %s: %s,\n", jsKey(names[tok.Name]), jsKey("var("+info.VarOf(tok)+")"))
		}
		b.WriteString("      },\n")
		rep.carried("%s → %s: %d", sc.family, sc.v3, len(f.Tokens))
	}
	if t.Type != nil {
		if len(t.Type.Families) > 0 {
			b.WriteString("      fontFamily: {\n")
			for _, kv := range t.Type.Families {
				fmt.Fprintf(&b, "        %s: [%s],\n", jsKey(kv.Key), jsKey("var("+info.fontVar(kv.Key)+")"))
			}
			b.WriteString("      },\n")
		}
		var sizes []string
		for _, g := range t.Type.Groups {
			for _, st := range g.Styles {
				if st.FontSize == "" {
					continue
				}
				opts := []string{}
				if st.LineHeight != "" {
					opts = append(opts, "lineHeight: "+jsKey(st.LineHeight))
				}
				if len(st.FontWeight) > 0 {
					opts = append(opts, "fontWeight: "+jsKey(scalar(st.FontWeight)))
				}
				if st.LetterSpacing != "" {
					opts = append(opts, "letterSpacing: "+jsKey(st.LetterSpacing))
				}
				sizes = append(sizes, fmt.Sprintf("        %s: [%s, { %s }],\n", jsKey(st.Name), jsKey(st.FontSize), strings.Join(opts, ", ")))
			}
		}
		if len(sizes) > 0 {
			b.WriteString("      fontSize: {\n" + strings.Join(sizes, "") + "      },\n")
		}
	}
	b.WriteString("    },\n  },\n};\n")
	rep.notCarried("opacity modifiers (bg-accent/50) do not work on var() colours in Tailwind v3; v4 has color-mix for that")
	rep.notCarried("components, cards, templates and assets: Tailwind takes tokens only")
	return []byte(b.String()), rep
}

// sortedKeys is a helper for stable reports.
func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
