package formats

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// The kit folder as the conversions see it: design-system.json (kept with its key order), tokens.json, the cards.

// Kit is a theme folder opened for conversion.
type Kit struct {
	Dir       string
	Info      KitInfo
	Namespace string
	manifest  *ordered
}

// OpenKit reads the identity of the kit in dir (a theme folder: the one with design-system.json).
func OpenKit(dir string) (*Kit, error) {
	raw, err := os.ReadFile(filepath.Join(dir, designkit.ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("%s is not a kit theme folder (no %s): make one with `harnessly design kit init`", dir, designkit.ManifestFile)
	}
	m, ok := parseOrdered(raw)
	if !ok {
		return nil, fmt.Errorf("%s is not valid JSON", designkit.ManifestFile)
	}
	k := &Kit{Dir: dir, manifest: m, Namespace: m.str("namespace")}
	k.Info.Title = m.str("title")
	for _, key := range m.keys {
		if rec, ok := m.sub(key); ok && rec.has("id") && rec.has("version") {
			k.Info.ID = rec.str("id")
			k.Info.Prefix = rec.str("tokenPrefix")
			break
		}
	}
	return k, nil
}

// Tokens reads the kit's tokens.json (empty when it has none).
func (k *Kit) Tokens() (*Tokens, bool, error) { return LoadTokens(k.Dir) }

// Components reads the kit's components as DESIGN.md's prose needs them, with the sections of their cards.
func (k *Kit) Components() []CompDoc {
	groups, ok := k.manifest.vals["groups"]
	if !ok {
		return nil
	}
	var gs []struct {
		ID, Title  string
		Components []struct {
			ID, Name, Description string
			Props                 []designkit.Prop
			Part                  json.RawMessage
		}
	}
	if json.Unmarshal(groups, &gs) != nil {
		return nil
	}
	var out []CompDoc
	for _, g := range gs {
		for _, c := range g.Components {
			if string(c.Part) == `"provider"` {
				continue
			}
			d := CompDoc{ID: c.ID, Name: firstNonEmpty(c.Name, c.ID), Group: g.Title, Description: c.Description, Props: c.Props}
			card, err := os.ReadFile(filepath.Join(k.Dir, "components", strings.ReplaceAll(c.ID, ".", "/"), "README.md"))
			if err == nil {
				d.WhenToUse = cardSection(string(card), "when to use")
				d.WhenNot = cardSection(string(card), "when not to use")
			}
			out = append(out, d)
		}
	}
	return out
}

func cardSection(md, head string) string {
	lines := strings.Split(md, "\n")
	var out []string
	in := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			in = strings.EqualFold(strings.TrimSpace(strings.TrimLeft(t, "#")), head)
			continue
		}
		if in && t != "" {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(t, "-")))
		}
	}
	return strings.Join(out, " ")
}

func (k *Kit) readme() string {
	raw, _ := os.ReadFile(filepath.Join(k.Dir, "README.md"))
	return string(raw)
}

// Export converts the kit to a format: dtcg, css, tailwind, tailwind4, design-md.
func (k *Kit) Export(format string) ([]byte, *Report, error) {
	t, found, err := k.Tokens()
	if err != nil {
		return nil, nil, fmt.Errorf("tokens.json: %w", err)
	}
	if !found {
		return nil, nil, errors.New("the kit has no tokens.json")
	}
	switch format {
	case "dtcg":
		out, rep := ExportDTCG(t)
		return out, rep, nil
	case "css":
		out, rep := ExportCSS(t, k.Info)
		return out, rep, nil
	case "tailwind", "tailwind3":
		out, rep := ExportTailwind(t, k.Info, false)
		return out, rep, nil
	case "tailwind4":
		out, rep := ExportTailwind(t, k.Info, true)
		return out, rep, nil
	case "design-md":
		out, rep := ExportDesignMD(t, k.Info, k.Components(), k.readme())
		return out, rep, nil
	}
	return nil, nil, fmt.Errorf("unknown format %q: dtcg, css, tailwind, tailwind4, design-md", format)
}

// DefaultFile is where an export goes by default, next to the kit.
func DefaultFile(format string) string {
	switch format {
	case "dtcg":
		return "tokens.dtcg.json"
	case "css":
		return "tokens.export.css"
	case "tailwind", "tailwind3":
		return "tailwind.preset.js"
	case "tailwind4":
		return "tailwind.theme.css"
	}
	return "DESIGN.md"
}

// ImportOptions steer an import into a kit folder.
type ImportOptions struct {
	Mode   string // --mode: the file holds this colour mode's values
	DryRun bool
	CSS    bool // regenerate tokens.css from the merged tokens
}

// Import converts a file from a format into the kit folder: tokens are merged into tokens.json by name, components
// are added to design-system.json, nothing existing is deleted.
func (k *Kit) Import(format, source string, opt ImportOptions) (*Report, error) {
	t, _, err := k.Tokens()
	if err != nil {
		return nil, fmt.Errorf("tokens.json: %w", err)
	}
	dopt := DTCGOptions{Mode: opt.Mode, Modes: t.Modes()}
	if f := t.family("color"); f == nil || len(f.Themes) == 0 {
		dopt.Modes = nil
	}
	var in *Tokens
	var rep *Report
	var notes string
	var drafts *StorybookResult
	switch format {
	case "dtcg", "figma", "css", "design-md":
		raw, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		switch format {
		case "dtcg":
			in, rep, err = ImportDTCG(raw, dopt)
		case "figma":
			in, rep, err = ImportFigma(raw, dopt)
		case "css":
			in, rep, err = ImportCSS(string(raw), CSSOptions{Prefix: k.Info.Prefix, Modes: dopt.Modes})
		case "design-md":
			var r *DesignMDResult
			r, rep, err = ImportDesignMD(raw, dopt)
			if r != nil {
				in, notes = r.Tokens, r.Notes
			}
		}
		if err != nil {
			if rep != nil && (len(rep.NotCarried) > 0) {
				return nil, fmt.Errorf("%w\n%s", err, rep.Lines())
			}
			return nil, err
		}
	case "storybook":
		drafts, rep, err = ImportStorybook(source, k.Namespace)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown format %q to import from: dtcg, css, design-md, storybook, figma", format)
	}
	if in != nil {
		k.mergeTokens(t, in, opt, rep)
		if !opt.DryRun {
			if err := os.WriteFile(filepath.Join(k.Dir, "tokens.json"), t.Marshal(), 0o644); err != nil {
				return rep, err
			}
			rep.Wrote = append(rep.Wrote, "tokens.json")
			if opt.CSS {
				css, _ := ExportCSS(t, k.Info)
				if err := os.WriteFile(filepath.Join(k.Dir, "tokens.css"), css, 0o644); err != nil {
					return rep, err
				}
				rep.Wrote = append(rep.Wrote, "tokens.css")
			}
		}
		if !opt.CSS {
			rep.todo("tokens.css is not rewritten: components read it. Regenerate it with `kit convert <folder> --to css --out <folder>/tokens.css`, or pass --css")
		}
		if notes != "" && !opt.DryRun {
			p := filepath.Join(k.Dir, "_harnessly", "design-notes.md")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return rep, err
			}
			if err := os.WriteFile(p, []byte(notes), 0o644); err != nil {
				return rep, err
			}
			rep.Wrote = append(rep.Wrote, "_harnessly/design-notes.md")
		}
	}
	if drafts != nil {
		if err := k.addComponents(drafts, opt, rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// mergeTokens puts the imported tokens into the kit's: same name replaces, a new one is added, modes are unioned.
func (k *Kit) mergeTokens(dst, src *Tokens, opt ImportOptions, rep *Report) {
	added, replaced, modeSet := 0, 0, 0
	for _, sf := range src.Families {
		df := dst.Fam(sf.Key)
		if len(sf.Themes) > 0 {
			df.Themes = mergeThemes(df.Themes, sf.Themes)
		}
		for k2, v := range sf.Extra {
			if df.Extra == nil {
				df.Extra = rawMap{}
			}
			if _, has := df.Extra[k2]; !has {
				df.Extra[k2] = v
			}
		}
		for _, st := range sf.Tokens {
			if existing, fam := dst.Find(st.Name); existing != nil {
				if fam != sf.Key {
					rep.notCarried("%s: the kit has it in %s, the file in %s: skipped", st.Name, fam, sf.Key)
					continue
				}
				if opt.Mode != "" && len(st.Value.Modes) == 1 {
					base := *existing
					if len(base.Value.Modes) == 0 {
						first := df.Themes
						id := "light"
						if len(first) > 0 {
							id = first[0].ID
						}
						base.Value = Value{Modes: []ModeValue{{id, base.Value.Plain}}}
					}
					base.Value = base.Value.With(st.Value.Modes[0].Mode, st.Value.Modes[0].Value)
					existing.Value = base.Value
					modeSet++
					continue
				}
				if st.Usage == "" {
					st.Usage = existing.Usage
				}
				if st.Var == "" {
					st.Var = existing.Var
				}
				for k2, v := range existing.Extra {
					if _, has := st.Extra[k2]; !has {
						if st.Extra == nil {
							st.Extra = rawMap{}
						}
						st.Extra[k2] = v
					}
				}
				*existing = st
				replaced++
				continue
			}
			df.Tokens = append(df.Tokens, st)
			added++
		}
	}
	if src.Type != nil {
		if dst.Type == nil {
			dst.Type = &Type{Extra: rawMap{}}
		}
		for _, kv := range src.Type.Families {
			found := false
			for i, e := range dst.Type.Families {
				if e.Key == kv.Key {
					dst.Type.Families[i].Value = kv.Value
					found = true
				}
			}
			if !found {
				dst.Type.Families = append(dst.Type.Families, kv)
			}
		}
		for _, g := range src.Type.Groups {
			found := false
			for i, e := range dst.Type.Groups {
				if e.Name == g.Name {
					for _, st := range g.Styles {
						replacedStyle := false
						for j, es := range e.Styles {
							if es.Name == st.Name {
								dst.Type.Groups[i].Styles[j] = st
								replacedStyle = true
							}
						}
						if !replacedStyle {
							dst.Type.Groups[i].Styles = append(dst.Type.Groups[i].Styles, st)
						}
					}
					found = true
				}
			}
			if !found {
				dst.Type.Groups = append(dst.Type.Groups, g)
			}
		}
		if len(dst.Type.Fonts) == 0 {
			dst.Type.Fonts = src.Type.Fonts
		}
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	rep.carried("into tokens.json: %d added, %d replaced by name, %d mode values set", added, replaced, modeSet)
	if opt.DryRun {
		rep.carried("dry run: nothing written")
	}
}

func mergeThemes(have, add []Mode) []Mode {
	for _, m := range add {
		found := false
		for _, h := range have {
			found = found || h.ID == m.ID
		}
		if !found {
			have = append(have, m)
		}
	}
	return have
}

// addComponents writes the drafts into design-system.json (groups) and their cards.
func (k *Kit) addComponents(res *StorybookResult, opt ImportOptions, rep *Report) error {
	var groups []*ordered
	if raw, ok := k.manifest.vals["groups"]; ok {
		var rawGroups []json.RawMessage
		if err := json.Unmarshal(raw, &rawGroups); err != nil && string(raw) != "null" {
			return fmt.Errorf("design-system.json groups: %w", err)
		}
		for _, rg := range rawGroups {
			g, ok := parseOrdered(rg)
			if !ok {
				return errors.New("design-system.json groups: a group is not an object")
			}
			groups = append(groups, g)
		}
	}
	existing := map[string]bool{}
	for _, g := range groups {
		var comps []json.RawMessage
		_ = json.Unmarshal(g.vals["components"], &comps)
		for _, c := range comps {
			if o, ok := parseOrdered(c); ok {
				existing[o.str("id")] = true
			}
		}
	}
	added := 0
	var todoAll []string
	for _, d := range res.Components {
		if existing[d.ID] {
			rep.notCarried("%s: the kit already has a component with this id: left as it is", d.ID)
			continue
		}
		var g *ordered
		for _, cand := range groups {
			if cand.str("id") == d.Group {
				g = cand
			}
		}
		if g == nil {
			g = newOrdered()
			g.set("id", d.Group)
			g.set("title", d.GroupTitle)
			g.set("components", []json.RawMessage{})
			groups = append(groups, g)
		}
		var comps []json.RawMessage
		_ = json.Unmarshal(g.vals["components"], &comps)
		comps = append(comps, marshal(d.Record))
		g.set("components", comps)
		added++
		cardPath := filepath.Join(k.Dir, "components", strings.ReplaceAll(d.ID, ".", "/"), "README.md")
		if _, err := os.Stat(cardPath); errors.Is(err, fs.ErrNotExist) {
			if !opt.DryRun {
				if err := os.MkdirAll(filepath.Dir(cardPath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(cardPath, []byte(d.Card), 0o644); err != nil {
					return err
				}
			}
		} else if err == nil {
			rep.notCarried("%s: components/%s/README.md exists: the card was not rewritten", d.ID, d.ID)
		}
		for _, t := range d.Todo {
			todoAll = append(todoAll, d.ID+" — "+t)
		}
	}
	rep.carried("into design-system.json: %d components as drafts (each lists what it lacks in its `todo`)", added)
	// Group the todos by kind so the report stays short.
	counts := map[string]int{}
	for _, t := range todoAll {
		kind, _, _ := strings.Cut(strings.SplitN(t, " — ", 2)[1], ":")
		counts[kind]++
	}
	for _, kind := range sortedKeys(counts) {
		rep.todo("%d components: %s (see each component's `todo` in design-system.json, `kit check` warns while it stays)", counts[kind], kind)
	}
	if !opt.DryRun && added > 0 {
		var raws []json.RawMessage
		for _, g := range groups {
			raws = append(raws, marshal(g))
		}
		k.manifest.set("groups", raws)
		var buf bytes.Buffer
		buf.Write(pretty(k.manifest))
		if err := os.WriteFile(filepath.Join(k.Dir, designkit.ManifestFile), buf.Bytes(), 0o644); err != nil {
			return err
		}
		rep.Wrote = append(rep.Wrote, designkit.ManifestFile)
	}
	return nil
}
