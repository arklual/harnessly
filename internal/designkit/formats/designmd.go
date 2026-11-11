package formats

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// DESIGN.md (the format Google Stitch uses): YAML front matter with the tokens (`colors`, `typography`, `rounded`,
// `spacing`, `components`; a reference is "{colors.primary}") and prose sections in a fixed order. The export puts the
// whole kit in one file for an agent that has nothing else; the import reads the tokens from the front matter and
// their descriptions from what the prose marks (a table with a token column and a Use column, or **Name (#hex):**
// bullets). Components are never made up from prose.

// CompDoc is a component as DESIGN.md's prose describes it, read from the kit.
type CompDoc struct {
	ID, Name, Group, Description string
	Props                        []designkit.Prop
	WhenToUse, WhenNot           string
}

func yamlStr(s string) string { return string(marshal(s)) }

func yamlKey(s string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*$`).MatchString(s) {
		return s
	}
	return yamlStr(s)
}

// ExportDesignMD writes the kit as one DESIGN.md.
func ExportDesignMD(t *Tokens, info KitInfo, comps []CompDoc, readme string) ([]byte, *Report) {
	rep := &Report{}
	modes := t.Modes()
	section := map[string]string{"colors": "colors", "radius": "rounded", "spacing": "spacing"}
	alias := func(v string) string {
		if m := aliasRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
			if _, fam := t.Find(m[1]); fam != "" && section[fam] != "" {
				return "{" + section[fam] + "." + m[1] + "}"
			} else if fam == "color" {
				return "{colors." + m[1] + "}"
			}
		}
		return v
	}
	var fm strings.Builder
	fm.WriteString("---\n")
	title := firstNonEmpty(info.Title, t.Name, info.ID, "Design system")
	fmt.Fprintf(&fm, "name: %s\n", yamlStr(title))
	block := func(key string, fam *Family) {
		if fam == nil || len(fam.Tokens) == 0 {
			return
		}
		fmt.Fprintf(&fm, "%s:\n", key)
		for _, tok := range fam.Tokens {
			fmt.Fprintf(&fm, "  %s: %s\n", yamlKey(tok.Name), yamlStr(alias(tok.Value.Default())))
		}
	}
	block("colors", t.family("color"))
	if t.Type != nil && len(t.Type.Groups) > 0 {
		fm.WriteString("typography:\n")
		for _, g := range t.Type.Groups {
			fam, _ := t.Type.Family(g.Family)
			for _, st := range g.Styles {
				fmt.Fprintf(&fm, "  %s:\n", yamlKey(st.Name))
				if fam != "" {
					fmt.Fprintf(&fm, "    fontFamily: %s\n", yamlStr(fam))
				}
				if st.FontSize != "" {
					fmt.Fprintf(&fm, "    fontSize: %s\n", yamlStr(st.FontSize))
				}
				if len(st.FontWeight) > 0 {
					if _, isStr := jsonString(st.FontWeight); isStr {
						fmt.Fprintf(&fm, "    fontWeight: %s\n", string(st.FontWeight))
					} else {
						fmt.Fprintf(&fm, "    fontWeight: %s\n", scalar(st.FontWeight))
					}
				}
				if st.LineHeight != "" {
					fmt.Fprintf(&fm, "    lineHeight: %s\n", yamlStr(st.LineHeight))
				}
				if st.LetterSpacing != "" {
					fmt.Fprintf(&fm, "    letterSpacing: %s\n", yamlStr(st.LetterSpacing))
				}
			}
		}
	}
	block("rounded", t.family("radius"))
	block("spacing", t.family("spacing"))
	fm.WriteString("---\n\n")

	var b strings.Builder
	b.WriteString(fm.String())
	fmt.Fprintf(&b, "## Overview\n\n%s\n\n", overviewOf(readme, title))
	table := func(fam *Family, withModes bool) {
		b.WriteString("| Token |")
		if withModes {
			for _, m := range modes {
				fmt.Fprintf(&b, " %s |", firstNonEmpty(m.Name, titleOf(m.ID)))
			}
		} else {
			b.WriteString(" Value |")
		}
		b.WriteString(" Use |\n| --- |")
		if withModes {
			b.WriteString(strings.Repeat(" --- |", len(modes)))
		} else {
			b.WriteString(" --- |")
		}
		b.WriteString(" --- |\n")
		for _, tok := range fam.Tokens {
			fmt.Fprintf(&b, "| `%s` |", tok.Name)
			if withModes {
				for _, m := range modes {
					fmt.Fprintf(&b, " %s |", mdCell(tok.Value.For(m.ID)))
				}
			} else {
				fmt.Fprintf(&b, " %s |", mdCell(tok.Value.Default()))
			}
			fmt.Fprintf(&b, " %s |\n", mdCell(tok.Usage))
		}
		b.WriteString("\n")
	}
	if f := t.family("color"); f != nil && len(f.Tokens) > 0 {
		b.WriteString("## Colors\n\n")
		if len(modes) > 1 {
			b.WriteString("The front matter holds the first mode; every mode is in the table.\n\n")
		}
		table(f, true)
		rep.carried("colors: %d tokens, %d modes (the front matter holds the first; the others are in the table)", len(f.Tokens), len(modes))
		if len(modes) > 1 {
			rep.notCarried("colour modes other than %s: DESIGN.md's front matter has no modes; they are in the Colors table", modes[0].ID)
		}
	}
	if t.Type != nil && len(t.Type.Groups) > 0 {
		b.WriteString("## Typography\n\n| Style | Font | Size | Line | Weight |\n| --- | --- | --- | --- | --- |\n")
		n := 0
		for _, g := range t.Type.Groups {
			fam, _ := t.Type.Family(g.Family)
			for _, st := range g.Styles {
				fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", st.Name, mdCell(fam), mdCell(st.FontSize), mdCell(st.LineHeight), mdCell(scalar(st.FontWeight)))
				n++
			}
		}
		b.WriteString("\n")
		rep.carried("typography: %d text styles", n)
	}
	if f := t.family("spacing"); f != nil && len(f.Tokens) > 0 {
		b.WriteString("## Layout\n\n")
		table(f, false)
		rep.carried("spacing: %d tokens", len(f.Tokens))
	}
	if f := t.family("shadow"); f != nil && len(f.Tokens) > 0 {
		b.WriteString("## Elevation\n\n")
		table(f, false)
		rep.carried("shadow: %d tokens, in the Elevation table (DESIGN.md's front matter has no shadows)", len(f.Tokens))
		rep.notCarried("shadow tokens in the front matter: the format has no key for them; they are in the Elevation table")
	}
	if f := t.family("radius"); f != nil && len(f.Tokens) > 0 {
		b.WriteString("## Shapes\n\n")
		table(f, false)
		rep.carried("radius: %d tokens as rounded", len(f.Tokens))
	}
	for _, f := range t.Families {
		switch f.Key {
		case "color", "spacing", "radius", "shadow":
		default:
			rep.notCarried("%s: a family DESIGN.md has no section for (%d tokens)", f.Key, len(f.Tokens))
		}
	}
	if len(comps) > 0 {
		b.WriteString("## Components\n\n")
		b.WriteString("Real components of the kit, used as `<Namespace>.<Id>` on a board; the front matter's `components` key (style recipes) is not used.\n\n")
		group := ""
		for _, c := range comps {
			if c.Group != "" && c.Group != group {
				group = c.Group
				fmt.Fprintf(&b, "### %s\n\n", group)
			}
			fmt.Fprintf(&b, "#### %s\n\n", firstNonEmpty(c.Name, c.ID))
			if c.Description != "" {
				b.WriteString(c.Description + "\n\n")
			}
			if len(c.Props) > 0 {
				b.WriteString("| Prop | Type | What |\n| --- | --- | --- |\n")
				for _, p := range c.Props {
					typ := p.Type
					if len(p.Values) > 0 {
						typ = strings.Join(p.Values, " · ")
					}
					if p.Required {
						typ += " (required)"
					}
					fmt.Fprintf(&b, "| `%s` | %s | %s |\n", p.Name, mdCell(typ), mdCell(p.Description))
				}
				b.WriteString("\n")
			}
			if c.WhenToUse != "" {
				b.WriteString("Use: " + oneLine(c.WhenToUse) + "\n\n")
			}
			if c.WhenNot != "" {
				b.WriteString("Do not use: " + oneLine(c.WhenNot) + "\n\n")
			}
		}
		rep.carried("components: %d described in prose (name, purpose, props, when to use and not)", len(comps))
		rep.notCarried("component samples, variants, thumbs and the bundle: DESIGN.md describes, it does not run")
	}
	rep.notCarried("templates and assets (icons, logos): not in DESIGN.md")
	rep.notCarried("a token's CSS variable (var) and font files")
	return []byte(strings.TrimRight(b.String(), "\n") + "\n"), rep
}

func overviewOf(readme, title string) string {
	for _, para := range strings.Split(readme, "\n\n") {
		p := strings.TrimSpace(para)
		if p != "" && !strings.HasPrefix(p, "#") && !strings.HasPrefix(p, "|") && !strings.HasPrefix(p, "```") {
			return oneLine(p)
		}
	}
	return title + " — a design system kit."
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

func mdCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(oneLine(s), "|", `\|`), "\n", " ")
}

// ---------------------------------------------------------------------------------------------------------------------

// DesignMDResult is what an import of a DESIGN.md found besides the tokens.
type DesignMDResult struct {
	Tokens *Tokens
	// Notes is the prose that has no field in the kit (Overview, Layout…, Do's and Don'ts) and the front matter's
	// component recipes, as Markdown, to keep next to the kit.
	Notes string
}

var (
	fenceRe  = regexp.MustCompile(`(?s)\A\s*---\r?\n(.*?)\r?\n---\s*(?:\r?\n|\z)`)
	headRe   = regexp.MustCompile(`(?m)^##\s+(.+?)\s*$`)
	bulletRe = regexp.MustCompile(`^\s*[-*]\s+\*\*(.+?)\*\*\s*[:—–-]?\s*(.*)$`)
	hexIn    = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
)

// ImportDesignMD reads a DESIGN.md.
func ImportDesignMD(src []byte, opt DTCGOptions) (*DesignMDResult, *Report, error) {
	rep := &Report{}
	m := fenceRe.FindSubmatchIndex(src)
	if m == nil {
		return nil, rep, fmt.Errorf("no YAML front matter (--- … ---) at the top: the tokens are read from it, never from prose")
	}
	front := string(src[m[2]:m[3]])
	body := string(src[m[1]:])
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, rep, fmt.Errorf("the front matter is not a YAML mapping: %v", err)
	}
	top := doc.Content[0]
	out := &Tokens{Extra: rawMap{}}
	res := &DesignMDResult{Tokens: out}
	pairs := func(n *yaml.Node) [][2]*yaml.Node {
		var ps [][2]*yaml.Node
		if n != nil && n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				ps = append(ps, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
			}
		}
		return ps
	}
	refFix := func(s string, secName map[string]string) string {
		if mm := aliasRe.FindStringSubmatch(strings.TrimSpace(s)); mm != nil {
			parts := strings.SplitN(mm[1], ".", 2)
			if len(parts) == 2 {
				return "{" + sanitizeName(parts[1]) + "}"
			}
		}
		return s
	}
	used := map[string]string{}
	claim := func(name, fam string) string {
		if prev, ok := used[name]; ok && prev != fam {
			rep.notCarried("%s: the name is also used in %s: renamed %s-%s", name, prev, fam, name)
			name = fam + "-" + name
		}
		used[name] = fam
		return name
	}
	var recipes []string
	var familyOrder []KV
	famKey := func(css string) string {
		for _, kv := range familyOrder {
			if kv.Value == css {
				return kv.Key
			}
		}
		key := sanitizeName(strings.ToLower(strings.Split(strings.Trim(strings.TrimSpace(css), `"'`), ",")[0]))
		if key == "" {
			key = "font"
		}
		for _, kv := range familyOrder {
			if kv.Key == key {
				key += fmt.Sprint(len(familyOrder) + 1)
			}
		}
		familyOrder = append(familyOrder, KV{key, css})
		return key
	}
	for _, kv := range pairs(top) {
		key := kv[0].Value
		switch key {
		case "name":
			out.Name = kv[1].Value
		case "colors":
			for _, p := range pairs(kv[1]) {
				v := refFix(p[1].Value, nil)
				if !aliasRe.MatchString(v) && !looksLikeColor(v) {
					if c, ok := normalizeColor(v); ok {
						v = c
					} else {
						rep.notCarried("colors.%s: %q is not a colour the kit holds (hex, rgb(), hsl(), oklch())", p[0].Value, v)
						continue
					}
				}
				n := claim(sanitizeName(p[0].Value), "color")
				out.Fam("color").Tokens = append(out.Fam("color").Tokens, Token{Name: n, Value: Value{Plain: v}, Extra: rawMap{}})
			}
		case "rounded", "spacing":
			fam := map[string]string{"rounded": "radius", "spacing": "spacing"}[key]
			for _, p := range pairs(kv[1]) {
				v := refFix(p[1].Value, nil)
				if aliasRe.MatchString(v) {
					if tgt := findPlain(out, strings.Trim(v, "{}")); tgt != "" {
						v = tgt
					} else {
						rep.notCarried("%s.%s: reference %s cannot be a %s value", key, p[0].Value, v, fam)
						continue
					}
				}
				n := claim(sanitizeName(p[0].Value), fam)
				out.Fam(fam).Tokens = append(out.Fam(fam).Tokens, Token{Name: n, Value: Value{Plain: v}, Extra: rawMap{}})
			}
		case "typography":
			ty := &Type{Extra: rawMap{}}
			groups := map[string]*StyleGroup{}
			var gorder []string
			for _, p := range pairs(kv[1]) {
				st := Style{Name: sanitizeName(p[0].Value), Extra: rawMap{}}
				fam := ""
				for _, f := range pairs(p[1]) {
					v := f[1].Value
					switch f[0].Value {
					case "fontFamily":
						fam = v
					case "fontSize":
						st.FontSize = v
					case "lineHeight":
						st.LineHeight = v
					case "letterSpacing":
						st.LetterSpacing = v
					case "fontWeight":
						if _, err := strconv.ParseFloat(v, 64); err == nil {
							st.FontWeight = json.RawMessage(v)
						} else {
							st.FontWeight = marshal(v)
						}
					default:
						rep.notCarried("typography.%s.%s: no place in a text style", p[0].Value, f[0].Value)
					}
				}
				fkey := ""
				if fam != "" {
					fkey = famKey(fam)
				}
				gname := "Text"
				if fkey != "" && len(familyOrder) > 1 && fkey != familyOrder[0].Key {
					gname = "Text " + fkey
				}
				g := groups[gname]
				if g == nil {
					g = &StyleGroup{Name: gname, Family: fkey, Extra: rawMap{}}
					groups[gname] = g
					gorder = append(gorder, gname)
				}
				g.Styles = append(g.Styles, st)
			}
			ty.Families = familyOrder
			for _, n := range gorder {
				ty.Groups = append(ty.Groups, *groups[n])
			}
			out.Type = ty
		case "components":
			n := len(pairs(kv[1]))
			rep.notCarried("components: %d style recipes in the front matter (tokens for a look, no props or code): kept in the notes, not made into kit components", n)
			if raw, err := yaml.Marshal(&yaml.Node{Kind: yaml.MappingNode, Content: kv[1].Content}); err == nil {
				recipes = append(recipes, string(raw))
			}
		case "description", "version":
		default:
			rep.notCarried("front matter key %q: not part of the kit", key)
		}
	}
	for _, f := range out.Families {
		rep.carried("%s: %d tokens", f.Key, len(f.Tokens))
	}
	if out.Type != nil {
		rep.carried("typography: %d text styles, %d font families", countStyles(out.Type), len(out.Type.Families))
	}
	if out.Count() == 0 && out.Type == nil {
		return nil, rep, fmt.Errorf("the front matter has no colors, rounded, spacing or typography")
	}
	// Prose: descriptions from what the prose marks.
	sections := splitSections(body)
	described := 0
	for _, s := range sections {
		described += describeFromProse(out, s.name, s.text, opt)
	}
	if described > 0 {
		rep.carried("descriptions: %d token usages and mode values from tables and bullets of the prose", described)
	}
	// Modes found in tables.
	if f := out.family("color"); f != nil {
		for _, tok := range f.Tokens {
			if len(tok.Value.Modes) > 0 {
				f.Themes = themesFrom(f)
				break
			}
		}
	}
	noUsage := 0
	for _, f := range out.Families {
		for _, tok := range f.Tokens {
			if tok.Usage == "" {
				noUsage++
			}
		}
	}
	if noUsage > 0 {
		rep.todo("%d tokens have no usage: the prose marks no purpose for them (kit check warns: an agent chooses a token by it)", noUsage)
	}
	var notes strings.Builder
	for _, s := range sections {
		low := strings.ToLower(s.name)
		if low == "colors" || low == "typography" {
			continue
		}
		fmt.Fprintf(&notes, "## %s\n\n%s\n\n", s.name, strings.TrimSpace(s.text))
	}
	if len(recipes) > 0 {
		notes.WriteString("## Component recipes (front matter `components`)\n\nStyle recipes written in tokens; they are not kit components.\n\n```yaml\n" + strings.Join(recipes, "") + "```\n")
	}
	res.Notes = strings.TrimSpace(notes.String())
	if res.Notes != "" {
		res.Notes = "# Notes from DESIGN.md\n\nProse the kit has no field for. Read by agents; the kit screen does not show it.\n\n" + res.Notes + "\n"
		rep.carried("prose sections without a kit field are kept as notes")
	}
	rep.notCarried("components in prose: never turned into kit components (no props or code are marked there)")
	return res, rep, nil
}

func findPlain(t *Tokens, name string) string {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	if tok, _ := t.Find(sanitizeName(parts[1])); tok != nil {
		return tok.Value.Default()
	}
	return ""
}

func countStyles(t *Type) int {
	n := 0
	for _, g := range t.Groups {
		n += len(g.Styles)
	}
	return n
}

func themesFrom(f *Family) []Mode {
	if len(f.Themes) > 0 {
		return f.Themes
	}
	var out []Mode
	seen := map[string]bool{}
	for _, tok := range f.Tokens {
		for _, m := range tok.Value.Modes {
			if !seen[m.Mode] {
				seen[m.Mode] = true
				out = append(out, Mode{ID: m.Mode, Name: titleOf(m.Mode)})
			}
		}
	}
	return out
}

type mdSection struct{ name, text string }

func splitSections(body string) []mdSection {
	locs := headRe.FindAllStringSubmatchIndex(body, -1)
	var out []mdSection
	for i, l := range locs {
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out = append(out, mdSection{name: strings.TrimSpace(body[l[2]:l[3]]), text: body[l[1]:end]})
	}
	return out
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if line[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(line[i])
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

var useHead = map[string]bool{"use": true, "usage": true, "role": true, "description": true, "purpose": true, "used for": true}

// describeFromProse fills token usages (and the other modes' values of colours) from a section's tables and bullets.
func describeFromProse(t *Tokens, section, text string, opt DTCGOptions) int {
	n := 0
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(l, "|") && i+1 < len(lines) && strings.Contains(lines[i+1], "---"):
			head := splitRow(l)
			i += 2
			var rows [][]string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, splitRow(lines[i]))
			}
			i--
			if len(head) < 2 || !strings.EqualFold(strings.Trim(head[0], "` *"), "token") {
				continue
			}
			useCol := -1
			for c, h := range head {
				if useHead[strings.ToLower(h)] {
					useCol = c
				}
			}
			for _, r := range rows {
				if len(r) != len(head) {
					continue
				}
				name := strings.Trim(r[0], "` *")
				tok, fam := t.Find(sanitizeName(name))
				if tok == nil {
					continue
				}
				if useCol > 0 && r[useCol] != "" && tok.Usage == "" {
					tok.Usage = r[useCol]
					n++
				}
				if fam == "color" && strings.EqualFold(section, "colors") && useCol > 2 {
					// Mode columns after the first: the other modes' values.
					if first := strings.ToLower(head[1]); first != "value" {
						for c := 2; c < useCol; c++ {
							id := strings.ToLower(strings.TrimSpace(head[c]))
							if id == "" || r[c] == "" || tok.Value.Has(id) {
								continue
							}
							if len(tok.Value.Modes) == 0 {
								tok.Value = Value{Modes: []ModeValue{{strings.ToLower(head[1]), tok.Value.Plain}}}
							}
							tok.Value = tok.Value.With(id, r[c])
							n++
						}
					}
				}
			}
		case strings.EqualFold(section, "colors"):
			if bm := bulletRe.FindStringSubmatch(l); bm != nil {
				head := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(bm[1]), ":"))
				use := strings.TrimSpace(bm[2])
				if use == "" {
					continue
				}
				if tok := tokenForBullet(t, head); tok != nil && tok.Usage == "" {
					tok.Usage = use
					n++
				}
			}
		}
	}
	return n
}

func tokenForBullet(t *Tokens, head string) *Token {
	if hx := hexIn.FindString(head); hx != "" {
		var found *Token
		cnt := 0
		if f := t.family("color"); f != nil {
			for i := range f.Tokens {
				if strings.EqualFold(f.Tokens[i].Value.Default(), hx) {
					found = &f.Tokens[i]
					cnt++
				}
			}
		}
		if cnt == 1 {
			return found
		}
	}
	name := strings.Trim(hexIn.ReplaceAllString(head, ""), "()` :")
	if tok, fam := t.Find(sanitizeName(name)); tok != nil && fam == "color" {
		return tok
	}
	return nil
}
