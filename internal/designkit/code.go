package designkit

// The turn of a mockup's module into the product's code (`harnessly design code`): rules over text, without
// a model, so the same module and kit always give the same bytes.
//
//	imports   a kit import is written from the components' code {import, export}, one statement per module
//	presets   a preset (`PrimaryButton`) becomes its target with the preset's props (`<Button variant="primary">`)
//	tokens    a token's semantic variable (`var(--acme-accent)`) becomes the library's (`var(--lib-color-accent)`)
//	fixtures  the names a page imports from its fixtures become the view's props, typed by the fixtures
//
// Everything else of the module stays as it is.

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CodeModule is a module turned into the product's code.
type CodeModule struct {
	Source string
	// Component is the name of the page's default export, Props what it takes from its fixtures (in import order).
	Component string
	Props     []string
}

type codeEdit struct {
	off, end int
	text     string
}

var defaultFnRe = regexp.MustCompile(`export\s+default\s+function\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(\s*\)`)
var defaultNameRe = regexp.MustCompile(`export\s+default\s+function\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

// TransformModule turns a module of the mockup into the product's. fixtures is the module specifier of the page's own
// fixtures ("./home.fixtures"), "" for a module that is not a page.
func (k *StrictKit) TransformModule(src, fixtures string) (CodeModule, error) {
	var out CodeModule
	var edits []codeEdit
	type spec struct{ module, export, local string }
	var kitSpecs []spec
	firstKit := -1
	presets := map[string]string{} // local name of a preset -> "<Target attrs"
	presetName := map[string]string{}
	for _, imp := range ScanImports(src) {
		switch {
		case k.owns(imp.From):
			if firstKit < 0 {
				firstKit = imp.Off
				edits = append(edits, codeEdit{off: imp.Off, end: imp.End}) // filled below
			} else {
				end := imp.End
				if end < len(src) && src[end] == '\n' {
					end++
				}
				edits = append(edits, codeEdit{off: imp.Off, end: end})
			}
			locals := make([]string, 0, len(imp.Names))
			for l := range imp.Names {
				locals = append(locals, l)
			}
			sort.Strings(locals)
			for _, local := range locals {
				imported := imp.Names[local]
				id := k.byImport[imp.From][imported]
				comp := k.comps[id]
				if comp == nil {
					return out, fmt.Errorf("%s is not a component the kit %s exports from %s", imported, k.c.Kit, imp.From)
				}
				if comp.Preset != nil {
					t := k.comps[comp.Preset.Of]
					if t == nil || t.Code == nil {
						return out, fmt.Errorf("the preset %s points at %s, which has no code {import, export}", comp.ID, comp.Preset.Of)
					}
					presets[local] = t.Code.Export
					if a := presetAttrs(comp.Preset.Props); a != "" {
						presetName[local] = t.Code.Export + " " + a
					} else {
						presetName[local] = t.Code.Export
					}
					kitSpecs = append(kitSpecs, spec{t.Code.Import, t.Code.Export, t.Code.Export})
					continue
				}
				if comp.Code == nil {
					return out, fmt.Errorf("%s has no code {import, export} in the kit %s", comp.ID, k.c.Kit)
				}
				kitSpecs = append(kitSpecs, spec{comp.Code.Import, comp.Code.Export, local})
			}
		case fixtures != "" && sameModule(imp.From, fixtures):
			for local, imported := range imp.Names {
				if imported == "*" || imported == "default" {
					return out, errors.New("import the fixtures by name (import { rows } from \"" + fixtures + "\"): each name becomes a prop of the view")
				}
				if local != imported {
					return out, fmt.Errorf("import %s from the fixtures without renaming it: the name is the view's prop", imported)
				}
			}
			names := make([]string, 0, len(imp.Names))
			for n := range imp.Names {
				names = append(names, n)
			}
			// The props in the order the import names them.
			sort.Slice(names, func(i, j int) bool {
				return strings.Index(src[imp.Off:imp.End], names[i]) < strings.Index(src[imp.Off:imp.End], names[j])
			})
			out.Props = append(out.Props, names...)
			edits = append(edits, codeEdit{off: imp.Off, end: imp.End, text: "import type * as fixtureData from \"" + imp.From + "\";"})
		}
	}
	if firstKit >= 0 {
		byModule := map[string][]string{}
		seen := map[string]bool{}
		for _, s := range kitSpecs {
			item := s.export
			if s.local != s.export {
				item = s.export + " as " + s.local
			}
			if seen[s.module+"\x00"+item] {
				continue
			}
			seen[s.module+"\x00"+item] = true
			byModule[s.module] = append(byModule[s.module], item)
		}
		modules := make([]string, 0, len(byModule))
		for m := range byModule {
			modules = append(modules, m)
		}
		sort.Strings(modules)
		var b strings.Builder
		for i, m := range modules {
			items := byModule[m]
			sort.Strings(items)
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "import { %s } from %q;", strings.Join(items, ", "), m)
		}
		for i := range edits {
			if edits[i].off == firstKit {
				edits[i].text = b.String()
			}
		}
	}
	if len(presets) > 0 {
		for _, el := range ScanJSX(src) {
			head, rest, dotted := strings.Cut(el.Name, ".")
			target, ok := presets[head]
			if !ok {
				continue
			}
			if dotted {
				edits = append(edits, codeEdit{off: el.Off, end: el.Off + len(head), text: target})
				if el.CloseOff >= 0 {
					edits = append(edits, codeEdit{off: el.CloseOff, end: el.CloseOff + len(head), text: target})
				}
				_ = rest
				continue
			}
			edits = append(edits, codeEdit{off: el.Off, end: el.Off + len(el.Name), text: presetName[head]})
			if el.CloseOff >= 0 {
				edits = append(edits, codeEdit{off: el.CloseOff, end: el.CloseOff + len(el.Name), text: target})
			}
		}
	}
	if m := defaultNameRe.FindStringSubmatch(src); m != nil {
		out.Component = m[1]
	}
	if len(out.Props) > 0 {
		loc := defaultFnRe.FindStringSubmatchIndex(src)
		if loc == nil {
			return out, errors.New("a page whose data comes from its fixtures is exported when its default export is `export default function Name()`, with no parameters")
		}
		name := src[loc[2]:loc[3]]
		var typ strings.Builder
		fmt.Fprintf(&typ, "export type %sData = {\n", name)
		for _, p := range out.Props {
			fmt.Fprintf(&typ, "  %s: typeof fixtureData.%s;\n", p, p)
		}
		typ.WriteString("};\n\n")
		edits = append(edits, codeEdit{off: loc[0], end: loc[0], text: typ.String()})
		open := strings.LastIndex(src[loc[0]:loc[1]], "(") + loc[0]
		edits = append(edits, codeEdit{off: open, end: loc[1], text: "({ " + strings.Join(out.Props, ", ") + " }: " + name + "Data)"})
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].off > edits[j].off })
	text := src
	last := len(src) + 1
	for _, e := range edits {
		if e.end > last {
			return out, errors.New("the module's imports and elements overlap: it cannot be turned by the rules")
		}
		text = text[:e.off] + e.text + text[e.end:]
		last = e.off
	}
	if len(k.libVar) > 0 {
		text = varRe.ReplaceAllStringFunc(text, func(m string) string {
			sub := varRe.FindStringSubmatch(m)
			if lib, ok := k.libVar[sub[1]]; ok {
				return strings.Replace(m, sub[1], lib, 1)
			}
			return m
		})
	}
	out.Source = text
	return out, nil
}

// sameModule is whether an import names the given module, with or without a code extension.
func sameModule(spec, want string) bool {
	for _, ext := range []string{"", ".ts", ".tsx", ".js", ".jsx"} {
		if spec == want+ext {
			return true
		}
	}
	return false
}

// CodeImportOf is how the product imports a component of the kit (its preset target for a preset): module and name.
func (k *StrictKit) CodeImportOf(id string) (module, export string, ok bool) {
	comp := k.comps[id]
	if comp != nil && comp.Preset != nil {
		comp = k.comps[comp.Preset.Of]
	}
	if comp == nil || comp.Code == nil {
		return "", "", false
	}
	return comp.Code.Import, comp.Code.Export, true
}
