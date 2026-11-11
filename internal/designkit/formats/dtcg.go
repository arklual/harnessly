package formats

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// W3C Design Tokens (DTCG): a tree of groups; a token is an object with `$value` and `$type`; `{a.b}` is an alias.
//
// Out: one group per family (`color`, `spacing`, `radius`, `shadow`, your own), `type` for families and text styles.
// A colour mode other than the first goes to `$extensions.mode.<mode>` (what Terrazzo and Style Dictionary read);
// what DTCG has no place for (usage's token variable, font files) goes to `$extensions.harnessly`, which other tools
// ignore and our import reads, so "ours → DTCG → ours" loses nothing.

const extNS = "harnessly"

var dtcgTypeOf = map[string]string{"color": "color", "spacing": "dimension", "radius": "dimension", "shadow": "shadow"}

func dtcgKey(name string) string { return strings.ReplaceAll(name, ".", "_") }

// ExportDTCG writes the tokens as a DTCG document.
func ExportDTCG(t *Tokens) ([]byte, *Report) {
	rep := &Report{}
	paths := map[string]string{} // token name → "family.key"
	for _, f := range t.Families {
		for _, tok := range f.Tokens {
			paths[tok.Name] = f.Key + "." + dtcgKey(tok.Name)
		}
	}
	alias := func(v string) string {
		if m := aliasRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
			if p, ok := paths[m[1]]; ok {
				return "{" + p + "}"
			}
			rep.notCarried("alias {%s} points at no token of the kit: written as it is", m[1])
		}
		return v
	}
	root := newOrdered()
	rootExt := newOrdered()
	if t.Name != "" {
		rootExt.set("name", t.Name)
	}
	if len(t.Version) > 0 {
		rootExt.set("version", t.Version)
	}
	if len(t.Extra) > 0 {
		rootExt.set("extra", t.Extra)
	}
	if len(rootExt.keys) > 0 {
		root.set("$extensions", map[string]any{extNS: rootExt})
	}
	for _, f := range t.Families {
		g := newOrdered()
		typ := dtcgTypeOf[f.Key]
		if typ != "" {
			g.set("$type", typ)
		}
		ext := newOrdered()
		if len(f.Themes) > 0 {
			ext.set("themes", f.Themes)
		}
		if _, known := dtcgTypeOf[f.Key]; !known {
			ext.set("family", f.Key)
		}
		if len(f.Extra) > 0 {
			ext.set("extra", f.Extra)
		}
		if len(ext.keys) > 0 {
			g.set("$extensions", map[string]any{extNS: ext})
		}
		for _, tok := range f.Tokens {
			n := newOrdered()
			dv := alias(tok.Value.Default())
			switch {
			case f.Key == "shadow" && !aliasRe.MatchString(dv):
				if layers, ok := shadowLayers(dv); ok {
					if len(layers) == 1 {
						n.set("$value", layers[0])
					} else {
						n.set("$value", layers)
					}
				} else {
					n.set("$value", dv)
					rep.notCarried("shadow %s is not [inset] x y blur spread colour: written as its CSS text, tools that expect a shadow object will not read it", tok.Name)
				}
			default:
				n.set("$value", dv)
			}
			if tok.Usage != "" {
				n.set("$description", tok.Usage)
			}
			ext := newOrdered()
			if dtcgKey(tok.Name) != tok.Name {
				ext.set("name", tok.Name)
			}
			if tok.Var != "" {
				ext.set("var", tok.Var)
			}
			if f.Key == "shadow" {
				ext.set("css", tok.Value.Default())
			}
			if len(tok.Value.Modes) > 0 {
				ext.set("defaultMode", tok.Value.Modes[0].Mode)
			}
			if len(tok.Extra) > 0 {
				ext.set("extra", tok.Extra)
			}
			node := newOrdered()
			if len(tok.Value.Modes) > 1 {
				modes := newOrdered()
				for _, m := range tok.Value.Modes[1:] {
					modes.set(m.Mode, alias(m.Value))
				}
				node.set("mode", modes)
			}
			if len(ext.keys) > 0 {
				node.set(extNS, ext)
			}
			if len(node.keys) > 0 {
				n.set("$extensions", node)
			}
			g.set(dtcgKey(tok.Name), n)
		}
		root.set(f.Key, g)
		rep.carried("%s: %d tokens", f.Key, len(f.Tokens))
	}
	if t.Type != nil {
		root.set("type", exportDTCGType(t.Type, rep, paths))
	}
	rep.notCarried("components, cards, templates and assets: DTCG describes tokens only")
	rep.notCarried("a token's CSS variable (var), font files and members the kit adds are kept in $extensions.%s: other tools ignore them", extNS)
	rep.notCarried("dimensions are written as strings (\"8px\"), not {value, unit}: the form Style Dictionary and Tokens Studio read")
	return pretty(root), rep
}

func exportDTCGType(ty *Type, rep *Report, paths map[string]string) *ordered {
	g := newOrdered()
	ext := newOrdered()
	if len(ty.Fonts) > 0 {
		ext.set("fonts", ty.Fonts)
	}
	if len(ty.Extra) > 0 {
		ext.set("extra", ty.Extra)
	}
	if len(ext.keys) > 0 {
		g.set("$extensions", map[string]any{extNS: ext})
	}
	famPath := map[string]string{}
	if len(ty.Families) > 0 {
		fams := newOrdered()
		for _, kv := range ty.Families {
			n := newOrdered()
			n.set("$type", "fontFamily")
			names := []string{}
			for _, p := range splitTop(kv.Value, ',') {
				names = append(names, strings.Trim(p, `"' `))
			}
			n.set("$value", names)
			n.set("$extensions", map[string]any{extNS: map[string]any{"css": kv.Value, "key": kv.Key}})
			fams.set(dtcgKey(kv.Key), n)
			famPath[kv.Key] = "type.families." + dtcgKey(kv.Key)
		}
		g.set("families", fams)
		rep.carried("type.families: %d", len(ty.Families))
	}
	if len(ty.Groups) > 0 {
		styles := newOrdered()
		count := 0
		for _, grp := range ty.Groups {
			gn := newOrdered()
			gext := newOrdered()
			if dtcgKey(grp.Name) != grp.Name {
				gext.set("name", grp.Name)
			}
			if grp.Family != "" {
				gext.set("family", grp.Family)
			}
			if len(grp.Extra) > 0 {
				gext.set("extra", grp.Extra)
			}
			if len(gext.keys) > 0 {
				gn.set("$extensions", map[string]any{extNS: gext})
			}
			for _, st := range grp.Styles {
				val := newOrdered()
				if p, ok := famPath[grp.Family]; ok {
					val.set("fontFamily", "{"+p+"}")
				}
				if st.FontSize != "" {
					val.set("fontSize", st.FontSize)
				}
				if st.FontWeight != nil {
					val.set("fontWeight", st.FontWeight)
				}
				if st.LineHeight != "" {
					val.set("lineHeight", numberOrString(st.LineHeight))
				}
				if st.LetterSpacing != "" {
					val.set("letterSpacing", st.LetterSpacing)
				}
				n := newOrdered()
				n.set("$type", "typography")
				n.set("$value", val)
				sext := newOrdered()
				if dtcgKey(st.Name) != st.Name {
					sext.set("name", st.Name)
				}
				if len(st.Extra) > 0 {
					sext.set("extra", st.Extra)
				}
				if len(sext.keys) > 0 {
					n.set("$extensions", map[string]any{extNS: sext})
				}
				gn.set(dtcgKey(st.Name), n)
				count++
			}
			styles.set(dtcgKey(grp.Name), gn)
		}
		g.set("styles", styles)
		rep.carried("type.styles: %d text styles", count)
	}
	return g
}

var plainNumberRe = regexp.MustCompile(`^-?(\d+\.?\d*|\.\d+)$`)

func numberOrString(s string) any {
	if plainNumberRe.MatchString(s) {
		var f float64
		if _, err := fmt.Sscan(s, &f); err == nil {
			return f
		}
	}
	return s
}

// ---------------------------------------------------------------------------------------------------------------------

// DTCGOptions steer an import.
type DTCGOptions struct {
	// Mode, when set, says the whole file holds the values of that colour mode (a light file and a dark file of the
	// same tokens): they are written into the mode, not as the default.
	Mode string
	// Modes are the kit's modes, the first is the default; without them the file's own say, else light.
	Modes []Mode
}

type dtok struct {
	path  []string
	typ   string
	value json.RawMessage
	desc  string
	ext   *ordered // $extensions
	h     *ordered // $extensions.harnessly
	fam   string   // family key a group says its tokens belong to
	// result
	name   string
	family string
	skip   bool
}

func (d *dtok) dotted() string { return strings.Join(d.path, ".") }

var stripTops = map[string]bool{"color": true, "colors": true, "colour": true, "colours": true, "spacing": true, "space": true, "radius": true,
	"radii": true, "borderradius": true, "rounded": true, "shadow": true, "shadows": true, "elevation": true, "dimension": true}

// ImportDTCG reads a DTCG document into tokens.
func ImportDTCG(raw []byte, opt DTCGOptions) (*Tokens, *Report, error) {
	root, ok := parseOrdered(raw)
	if !ok {
		return nil, nil, fmt.Errorf("not a JSON object: a DTCG file is a tree of groups")
	}
	rep := &Report{}
	var toks []*dtok
	var walk func(path []string, o *ordered, typ, fam string)
	walk = func(path []string, o *ordered, typ, fam string) {
		if o.has("$type") {
			typ = o.str("$type")
		}
		var h *ordered
		if ext, ok := o.sub("$extensions"); ok {
			if hh, ok := ext.sub(extNS); ok {
				h = hh
				if f := hh.str("family"); f != "" {
					fam = f
				}
			}
		}
		if o.has("$value") {
			d := &dtok{path: path, typ: typ, value: o.vals["$value"], desc: o.str("$description"), fam: fam}
			d.ext, _ = o.sub("$extensions")
			d.h = h
			toks = append(toks, d)
			return
		}
		for _, k := range o.keys {
			if strings.HasPrefix(k, "$") {
				continue
			}
			child, ok := o.sub(k)
			if !ok {
				rep.notCarried("%s: not a group or a token, skipped", strings.Join(append(append([]string{}, path...), k), "."))
				continue
			}
			walk(append(append([]string{}, path...), k), child, typ, fam)
		}
	}
	// Root-level extensions (kit name, version) and the groups.
	out := &Tokens{Extra: rawMap{}}
	if ext, ok := root.sub("$extensions"); ok {
		if h, ok := ext.sub(extNS); ok {
			out.Name = h.str("name")
			if h.has("version") {
				out.Version = h.vals["version"]
			}
			if e, ok := h.sub("extra"); ok {
				for _, k := range e.keys {
					out.Extra[k] = e.vals[k]
				}
			}
		}
	}
	groupExt := map[string]*ordered{} // top-level group → its harnessly extension
	for _, k := range root.keys {
		if strings.HasPrefix(k, "$") {
			continue
		}
		child, ok := root.sub(k)
		if !ok {
			continue
		}
		if ext, ok := child.sub("$extensions"); ok {
			if h, ok := ext.sub(extNS); ok {
				groupExt[k] = h
			}
		}
		walk([]string{k}, child, "", "")
	}

	byPath := map[string]*dtok{}
	for _, d := range toks {
		byPath[d.dotted()] = d
	}
	// Which family and name each token gets.
	counts := map[string]int{}
	for _, d := range toks {
		classify(d, rep, counts)
	}
	used := map[string]string{} // name → family
	taken := func(d *dtok) {
		if d.skip || d.family == "type.family" || d.family == "type.style" {
			return
		}
		name := d.name
		if prev, clash := used[name]; clash && prev != d.family {
			rep.notCarried("%s: the name %q is also a %s token: renamed %s-%s", d.dotted(), name, prev, d.family, name)
			name = d.family + "-" + name
		}
		d.name = name
		used[name] = d.family
	}
	for _, d := range toks {
		taken(d)
	}
	nameOf := func(p string) (string, bool) {
		d, ok := byPath[p]
		if !ok || d.skip || d.family == "type.family" || d.family == "type.style" {
			return "", false
		}
		return d.name, true
	}
	// A literal value after following aliases (a token the kit does not carry may stand behind an alias).
	var literal func(d *dtok, depth int) (json.RawMessage, bool)
	literal = func(d *dtok, depth int) (json.RawMessage, bool) {
		if s, ok := jsonString(d.value); ok {
			if m := aliasRe.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
				t, ok := byPath[m[1]]
				if !ok || depth > 16 {
					return nil, false
				}
				return literal(t, depth+1)
			}
		}
		return d.value, true
	}
	valueOf := func(raw json.RawMessage, d *dtok) (string, bool) {
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return "", false
		}
		if s, ok := v.(string); ok {
			if m := aliasRe.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
				if n, ok := nameOf(m[1]); ok {
					return "{" + n + "}", true
				}
				t, ok := byPath[m[1]]
				if !ok {
					rep.notCarried("%s: alias {%s} points at nothing in the file", d.dotted(), m[1])
					return "", false
				}
				lit, ok := literal(t, 0)
				if !ok {
					return "", false
				}
				rep.carried("%s: alias {%s} stands for a token the kit does not keep: its value was written instead", d.dotted(), m[1])
				return valueOf2(lit, d.family)
			}
		}
		return valueOf2(raw, d.family)
	}

	// Modes.
	defaultMode := ""
	var themes []Mode
	for _, g := range groupExt {
		var th []Mode
		if raw, ok := g.vals["themes"]; ok && json.Unmarshal(raw, &th) == nil && len(th) > 0 && len(themes) == 0 {
			themes = th
		}
	}
	if len(opt.Modes) > 0 {
		themes = opt.Modes
	}
	if len(themes) > 0 {
		defaultMode = themes[0].ID
	}
	if defaultMode == "" {
		defaultMode = "light"
	}
	seenModes := []string{}
	addMode := func(id string) {
		for _, m := range seenModes {
			if m == id {
				return
			}
		}
		seenModes = append(seenModes, id)
	}

	for _, d := range toks {
		if d.skip || d.family == "type.family" || d.family == "type.style" {
			continue
		}
		val, ok := valueOf(d.value, d)
		if !ok {
			rep.notCarried("%s: value %s cannot be kept (%s)", d.dotted(), truncate(string(d.value), 40), d.typ)
			continue
		}
		var v Value
		first := defaultMode
		if d.h != nil && d.h.has("defaultMode") {
			first = d.h.str("defaultMode")
		}
		mv := map[string]string{}
		var order []string
		if d.ext != nil {
			if mo, ok := d.ext.sub("mode"); ok {
				for _, k := range mo.keys {
					raw := mo.vals[k]
					if s, ok := valueOf(raw, d); ok {
						mv[k] = s
						order = append(order, k)
					}
				}
			}
			if mo, ok := d.ext.sub("modes"); ok && len(order) == 0 {
				for _, k := range mo.keys {
					if s, ok := valueOf(mo.vals[k], d); ok {
						mv[k] = s
						order = append(order, k)
					}
				}
			}
		}
		switch {
		case opt.Mode != "":
			v = Value{Modes: []ModeValue{{opt.Mode, val}}}
			addMode(opt.Mode)
		case len(order) > 0 || (d.h != nil && d.h.has("defaultMode")):
			v = Value{Modes: []ModeValue{{first, val}}}
			addMode(first)
			for _, k := range order {
				v.Modes = append(v.Modes, ModeValue{k, mv[k]})
				addMode(k)
			}
		default:
			v = Value{Plain: val}
		}
		tok := Token{Name: d.name, Value: v, Usage: d.desc, Extra: rawMap{}}
		if d.h != nil {
			if n := d.h.str("name"); n != "" {
				tok.Name = n
			}
			tok.Var = d.h.str("var")
			if css := d.h.str("css"); css != "" && d.family == "shadow" {
				if v.Plain != "" {
					tok.Value.Plain = css
				} else if len(v.Modes) > 0 {
					tok.Value.Modes[0].Value = css
				}
			}
			if e, ok := d.h.sub("extra"); ok {
				for _, k := range e.keys {
					tok.Extra[k] = e.vals[k]
				}
			}
		}
		f := out.Fam(d.family)
		f.Tokens = append(f.Tokens, tok)
	}
	if len(seenModes) > 0 || len(themes) > 0 {
		var th []Mode
		if len(themes) > 0 {
			th = append(th, themes...)
		}
		if opt.Mode == "" || len(themes) == 0 {
			if len(th) == 0 {
				th = append(th, Mode{ID: defaultMode, Name: titleOf(defaultMode)})
			}
		}
		for _, id := range seenModes {
			found := false
			for _, m := range th {
				if m.ID == id {
					found = true
				}
			}
			if !found {
				th = append(th, Mode{ID: id, Name: titleOf(id)})
			}
		}
		out.Fam("color").Themes = th
	}
	// Family-level extras and themes written by our export.
	for top, g := range groupExt {
		fam := top
		if f := g.str("family"); f != "" {
			fam = f
		}
		if e, ok := g.sub("extra"); ok {
			if f := out.family(fam); f != nil {
				if f.Extra == nil {
					f.Extra = rawMap{}
				}
				for _, k := range e.keys {
					f.Extra[k] = e.vals[k]
				}
			}
		}
	}
	importDTCGType(root, toks, byPath, out, rep, literal)
	// Families in a stable order when the file says nothing: as met.
	for _, f := range out.Families {
		rep.carried("%s: %d tokens", f.Key, len(f.Tokens))
	}
	for typ, n := range counts {
		rep.notCarried("%d tokens of type %q: the kit has no place for them", n, typ)
	}
	sort.Strings(rep.NotCarried)
	if out.Count() == 0 && out.Type == nil {
		return nil, rep, fmt.Errorf("no token the kit can hold in the file (colour, dimension, shadow, fontFamily, typography)")
	}
	return out, rep, nil
}

func valueOf2(raw json.RawMessage, family string) (string, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	switch family {
	case "color":
		return dtcgColorAny(v)
	case "shadow":
		return shadowCSS(raw)
	case "spacing", "radius":
		return dimensionAny(v)
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}

// classify decides a token's family and name.
func classify(d *dtok, rep *Report, counts map[string]int) {
	typ := d.typ
	top := d.path[0]
	switch {
	case d.fam != "" && dtcgTypeOf[d.fam] == "" && typ != "typography" && typ != "fontFamily":
		d.family = d.fam
	case typ == "color":
		d.family = "color"
	case typ == "shadow":
		d.family = "shadow"
	case typ == "dimension":
		d.family = "spacing"
		for _, seg := range d.path {
			switch strings.ToLower(strings.ReplaceAll(seg, "-", "")) {
			case "radius", "radii", "rounded", "borderradius", "corner", "corners":
				d.family = "radius"
			}
		}
	case typ == "fontFamily":
		d.family = "type.family"
	case typ == "typography":
		d.family = "type.style"
	case typ == "":
		if s, ok := jsonString(d.value); ok && looksLikeColor(s) && d.fam == "" {
			d.family = "color"
			break
		}
		if d.fam != "" {
			d.family = d.fam
			break
		}
		counts["(no $type)"]++
		d.skip = true
	default:
		counts[typ]++
		d.skip = true
	}
	if d.skip {
		return
	}
	rest := d.path
	if len(rest) > 1 && (stripTops[strings.ToLower(top)] || d.fam == top || d.family == top) {
		rest = rest[1:]
	}
	d.name = sanitizeName(strings.Join(rest, "-"))
	if d.name == "" {
		d.skip = true
		counts["(unnamed)"]++
	} else if d.name != strings.Join(rest, "-") {
		rep.notCarried("%s: name written as %q (letters, digits, _ . - only)", d.dotted(), d.name)
	}
	if d.h != nil && d.h.str("name") != "" {
		d.name = d.h.str("name") // the kit's own name, written by our export when the key could not hold it
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// importDTCGType collects fontFamily and typography tokens into tokens.json's `type`.
func importDTCGType(root *ordered, toks []*dtok, byPath map[string]*dtok, out *Tokens, rep *Report,
	literal func(*dtok, int) (json.RawMessage, bool)) {
	ty := &Type{Extra: rawMap{}}
	famKeyByPath := map[string]string{}
	famByCSS := map[string]string{}
	if tg, ok := root.sub("type"); ok {
		if ext, ok := tg.sub("$extensions"); ok {
			if h, ok := ext.sub(extNS); ok {
				if raw, ok := h.vals["fonts"]; ok {
					_ = json.Unmarshal(raw, &ty.Fonts)
				}
				if e, ok := h.sub("extra"); ok {
					for _, k := range e.keys {
						ty.Extra[k] = e.vals[k]
					}
				}
			}
		}
	}
	addFamily := func(key, css string) string {
		if k, ok := famByCSS[css]; ok {
			return k
		}
		for _, kv := range ty.Families {
			if kv.Key == key && kv.Value != css {
				key = key + "-" + fmt.Sprint(len(ty.Families)+1)
			}
		}
		ty.Families = append(ty.Families, KV{key, css})
		famByCSS[css] = key
		return key
	}
	for _, d := range toks {
		if d.family != "type.family" {
			continue
		}
		css := ""
		if d.h != nil && d.h.str("css") != "" {
			css = d.h.str("css")
		} else {
			lit, ok := literal(d, 0)
			if !ok {
				continue
			}
			var arr []string
			if json.Unmarshal(lit, &arr) == nil {
				var q []string
				for _, n := range arr {
					if strings.ContainsAny(n, " ") && !strings.Contains(n, `"`) {
						n = `"` + n + `"`
					}
					q = append(q, n)
				}
				css = strings.Join(q, ", ")
			} else if s, ok := jsonString(lit); ok {
				css = s
			}
		}
		if css == "" {
			continue
		}
		key := sanitizeName(d.path[len(d.path)-1])
		if d.h != nil && d.h.str("key") != "" {
			key = d.h.str("key")
		}
		famKeyByPath[d.dotted()] = addFamily(key, css)
	}
	groups := map[string]*StyleGroup{}
	var gorder []string
	for _, d := range toks {
		if d.family != "type.style" {
			continue
		}
		o, ok := parseOrdered(d.value)
		if !ok {
			rep.notCarried("%s: a typography token that is not an object", d.dotted())
			continue
		}
		gname := "Text"
		if len(d.path) >= 3 {
			gname = d.path[len(d.path)-2]
			switch strings.ToLower(gname) {
			case "styles", "typography", "type", "text-styles":
				gname = "Text"
			}
		}
		var gh *ordered
		if len(d.path) >= 3 {
			if gnode, ok := walkTo(root, d.path[:len(d.path)-1]); ok {
				if ext, ok := gnode.sub("$extensions"); ok {
					gh, _ = ext.sub(extNS)
				}
			}
		}
		if gh != nil && gh.str("name") != "" {
			gname = gh.str("name")
		}
		g := groups[gname]
		if g == nil {
			g = &StyleGroup{Name: gname, Extra: rawMap{}}
			groups[gname] = g
			gorder = append(gorder, gname)
			if gh != nil {
				g.Family = gh.str("family")
				if e, ok := gh.sub("extra"); ok {
					for _, k := range e.keys {
						g.Extra[k] = e.vals[k]
					}
				}
			}
		}
		st := Style{Name: sanitizeName(d.path[len(d.path)-1]), Extra: rawMap{}}
		if d.h != nil {
			if n := d.h.str("name"); n != "" {
				st.Name = n
			}
			if e, ok := d.h.sub("extra"); ok {
				for _, k := range e.keys {
					st.Extra[k] = e.vals[k]
				}
			}
		}
		for _, k := range o.keys {
			rawv := o.vals[k]
			switch k {
			case "fontFamily":
				ref, isRef := jsonString(rawv)
				if m := aliasRe.FindStringSubmatch(ref); isRef && m != nil {
					if key, ok := famKeyByPath[m[1]]; ok && g.Family == "" {
						g.Family = key
					}
				} else if isRef && g.Family == "" {
					g.Family = addFamily(sanitizeName(strings.Split(strings.Trim(ref, `"' `), ",")[0]), ref)
				} else {
					var arr []string
					if json.Unmarshal(rawv, &arr) == nil && len(arr) > 0 && g.Family == "" {
						g.Family = addFamily(sanitizeName(arr[0]), strings.Join(arr, ", "))
					}
				}
			case "fontSize":
				st.FontSize = dimOrScalar(rawv)
			case "lineHeight":
				st.LineHeight = dimOrScalar(rawv)
			case "letterSpacing":
				st.LetterSpacing = dimOrScalar(rawv)
			case "fontWeight":
				st.FontWeight = rawv
			default:
				rep.notCarried("%s: typography member %s has no place in a text style", d.dotted(), k)
			}
		}
		g.Styles = append(g.Styles, st)
	}
	for _, n := range gorder {
		ty.Groups = append(ty.Groups, *groups[n])
	}
	if len(ty.Families) > 0 || len(ty.Groups) > 0 || len(ty.Fonts) > 0 {
		out.Type = ty
	}
}

func dimOrScalar(raw json.RawMessage) string {
	if s, ok := dimensionRaw(raw); ok {
		if _, isStr := jsonString(raw); isStr {
			return s
		}
		var n float64
		if json.Unmarshal(raw, &n) == nil {
			return trimFloat(n) // a bare number is a multiplier (line height) or px: leave the number
		}
		return s
	}
	return scalar(raw)
}

func walkTo(root *ordered, path []string) (*ordered, bool) {
	cur := root
	for _, p := range path {
		next, ok := cur.sub(p)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}
