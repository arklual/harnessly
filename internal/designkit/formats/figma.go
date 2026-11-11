package formats

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Figma variables: the JSON of the variables REST endpoint (`GET /v1/files/:key/variables/local`, or the same
// `meta` object saved by a plugin): `meta.variableCollections` with their modes and `meta.variables` with a value per
// mode. It is the one Figma shape that announces itself (it has `meta.variables` and `valuesByMode`). Nothing here
// calls Figma: the file is read from disk. A DTCG export from a Figma plugin goes through `--from dtcg`.

type figmaVar struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	ResolvedType      string                     `json:"resolvedType"`
	Collection        string                     `json:"variableCollectionId"`
	ValuesByMode      map[string]json.RawMessage `json:"valuesByMode"`
	Description       string                     `json:"description"`
	Remote            bool                       `json:"remote"`
	HiddenFromPublish bool                       `json:"hiddenFromPublishing"`
	Scopes            []string                   `json:"scopes"`
}

type figmaMode struct {
	ModeID string `json:"modeId"`
	Name   string `json:"name"`
}

type figmaCollection struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Modes         []figmaMode `json:"modes"`
	DefaultModeID string      `json:"defaultModeId"`
}

// IsFigmaVariables says whether raw is a Figma variables export.
func IsFigmaVariables(raw []byte) bool {
	var probe struct {
		Meta *struct {
			Variables json.RawMessage `json:"variables"`
		} `json:"meta"`
		Variables json.RawMessage `json:"variables"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return false
	}
	return (probe.Meta != nil && len(probe.Meta.Variables) > 0) || len(probe.Variables) > 0
}

// ImportFigma reads a Figma variables export.
func ImportFigma(raw []byte, opt DTCGOptions) (*Tokens, *Report, error) {
	rep := &Report{}
	var doc struct {
		Meta struct {
			Variables   map[string]figmaVar        `json:"variables"`
			Collections map[string]figmaCollection `json:"variableCollections"`
		} `json:"meta"`
		Variables   map[string]figmaVar        `json:"variables"`
		Collections map[string]figmaCollection `json:"variableCollections"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, rep, fmt.Errorf("not JSON: %w", err)
	}
	vars, cols := doc.Meta.Variables, doc.Meta.Collections
	if len(vars) == 0 {
		vars, cols = doc.Variables, doc.Collections
	}
	if len(vars) == 0 {
		return nil, rep, fmt.Errorf("no meta.variables: this is not a Figma variables export (GET /v1/files/:key/variables/local)")
	}
	// Order of variables: by collection order then name, the endpoint's maps are unordered.
	ids := make([]string, 0, len(vars))
	for id := range vars {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i := 0; i < len(ids); i++ { // by name inside, stable
		for j := i + 1; j < len(ids); j++ {
			if vars[ids[j]].Name < vars[ids[i]].Name {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	modeID := func(col figmaCollection, m figmaMode) string {
		id := kebab(m.Name)
		if id == "" {
			id = kebab(m.ModeID)
		}
		return id
	}
	defaultMode := "light"
	if len(opt.Modes) > 0 {
		defaultMode = opt.Modes[0].ID
	}
	name := map[string]string{} // variable id → token name
	family := map[string]string{}
	skipped := map[string]int{}
	for _, id := range ids {
		v := vars[id]
		if v.Remote {
			skipped["remote (from another library)"]++
			continue
		}
		fam := figmaFamily(v)
		if fam == "" {
			skipped[strings.ToLower(v.ResolvedType)+" variables the kit has no family for"]++
			continue
		}
		n := sanitizeName(strings.ReplaceAll(strings.ReplaceAll(v.Name, "/", "-"), " ", "-"))
		if n == "" {
			skipped["unnamed"]++
			continue
		}
		name[id], family[id] = n, fam
	}
	// Disambiguate clashes across families.
	taken := map[string]string{}
	for _, id := range ids {
		n, ok := name[id]
		if !ok {
			continue
		}
		if prev, clash := taken[n]; clash && prev != family[id] {
			rep.notCarried("%s: the name %q is also a %s token: renamed %s-%s", vars[id].Name, n, prev, family[id], n)
			name[id] = family[id] + "-" + n
		}
		taken[name[id]] = family[id]
	}
	out := &Tokens{Extra: rawMap{}}
	var themes []Mode
	addTheme := func(id string) {
		for _, m := range themes {
			if m.ID == id {
				return
			}
		}
		themes = append(themes, Mode{ID: id, Name: titleOf(id)})
	}
	var resolve func(id, modeKey string, depth int) (json.RawMessage, bool)
	resolve = func(id, modeKey string, depth int) (json.RawMessage, bool) {
		v, ok := vars[id]
		if !ok || depth > 16 {
			return nil, false
		}
		col := cols[v.Collection]
		mid := modeKey
		if _, has := v.ValuesByMode[mid]; !has {
			mid = col.DefaultModeID
		}
		val, has := v.ValuesByMode[mid]
		if !has {
			return nil, false
		}
		var al struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(val, &al) == nil && al.Type == "VARIABLE_ALIAS" {
			return resolve(al.ID, modeKey, depth+1)
		}
		return val, true
	}
	figmaValue := func(raw json.RawMessage, v figmaVar, fam string) (string, bool) {
		var al struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(raw, &al) == nil && al.Type == "VARIABLE_ALIAS" {
			if n, ok := name[al.ID]; ok && fam == "color" && family[al.ID] == "color" {
				return "{" + n + "}", true
			}
			lit, ok := resolve(al.ID, "", 0)
			if !ok {
				return "", false
			}
			raw = lit
		}
		switch v.ResolvedType {
		case "COLOR":
			var c struct{ R, G, B, A float64 }
			var cm map[string]float64
			if json.Unmarshal(raw, &cm) != nil {
				return "", false
			}
			c.R, c.G, c.B, c.A = cm["r"], cm["g"], cm["b"], 1
			if a, ok := cm["a"]; ok {
				c.A = a
			}
			return rgbaString(c.R, c.G, c.B, c.A), true
		case "FLOAT":
			var f float64
			if json.Unmarshal(raw, &f) != nil {
				return "", false
			}
			if f == 0 {
				return "0", true
			}
			return trimFloat(f) + "px", true
		case "STRING":
			s, ok := jsonString(raw)
			return s, ok
		}
		return "", false
	}
	fontKV := []KV{}
	for _, id := range ids {
		n, ok := name[id]
		if !ok {
			continue
		}
		v := vars[id]
		fam := family[id]
		col := cols[v.Collection]
		// The collection's modes in order; the default one first.
		modes := append([]figmaMode(nil), col.Modes...)
		for i, m := range modes {
			if m.ModeID == col.DefaultModeID && i > 0 {
				modes = append([]figmaMode{m}, append(modes[:i:i], modes[i+1:]...)...)
				break
			}
		}
		if len(modes) == 0 {
			for k := range v.ValuesByMode {
				modes = append(modes, figmaMode{ModeID: k, Name: k})
			}
		}
		var tv Value
		if len(modes) <= 1 || fam != "color" {
			m := col.DefaultModeID
			if m == "" && len(modes) > 0 {
				m = modes[0].ModeID
			}
			raw, ok := v.ValuesByMode[m]
			if !ok {
				continue
			}
			s, ok := figmaValue(raw, v, fam)
			if !ok {
				rep.notCarried("%s: value cannot be read", v.Name)
				continue
			}
			tv = Value{Plain: s}
			if fam != "color" && len(modes) > 1 {
				rep.notCarried("%s: %d modes, only %s kept (the kit has modes for colours only)", v.Name, len(modes), modes[0].Name)
			}
		} else {
			for i, m := range modes {
				raw, ok := v.ValuesByMode[m.ModeID]
				if !ok {
					continue
				}
				s, ok := figmaValue(raw, v, fam)
				if !ok {
					continue
				}
				id := modeID(col, m)
				if i == 0 && opt.Modes == nil {
					// The collection's default mode is the kit's default mode.
					if len(themes) == 0 && defaultMode == "light" {
						defaultMode = id
					}
					id = firstThemeOr(themes, id)
				}
				addTheme(id)
				tv.Modes = append(tv.Modes, ModeValue{id, s})
			}
			if len(tv.Modes) == 0 {
				continue
			}
		}
		if fam == "font" {
			fontKV = append(fontKV, KV{sanitizeName(strings.TrimPrefix(strings.TrimPrefix(n, "font-"), "font")), tv.Default()})
			continue
		}
		out.Fam(fam).Tokens = append(out.Fam(fam).Tokens, Token{Name: n, Value: tv, Usage: oneLine(v.Description), Extra: rawMap{}})
	}
	if len(themes) > 0 {
		out.Fam("color").Themes = themes
	}
	if len(fontKV) > 0 {
		out.Type = &Type{Families: fontKV, Extra: rawMap{}}
	}
	for _, f := range out.Families {
		rep.carried("%s: %d tokens", f.Key, len(f.Tokens))
	}
	if out.Type != nil {
		rep.carried("type.families: %d font families", len(out.Type.Families))
	}
	for k, n := range skipped {
		rep.notCarried("%d %s", n, k)
	}
	rep.notCarried("scopes, code syntax, hiddenFromPublishing and the collections' structure: the kit has one flat list per family")
	rep.notCarried("text styles, effects (shadows) and components: not variables, not in this file")
	if out.Count() == 0 && out.Type == nil {
		return nil, rep, fmt.Errorf("no COLOR, FLOAT (spacing, radius) or STRING (font) variable the kit can hold in the file")
	}
	return out, rep, nil
}

func firstThemeOr(themes []Mode, id string) string {
	if len(themes) > 0 {
		return themes[0].ID
	}
	return id
}

// figmaFamily maps a Figma variable to a kit family by type and name.
func figmaFamily(v figmaVar) string {
	low := strings.ToLower(v.Name)
	switch v.ResolvedType {
	case "COLOR":
		return "color"
	case "FLOAT":
		switch {
		case radiusNameRe.MatchString(low):
			return "radius"
		case spaceNameRe.MatchString(low):
			return "spacing"
		}
	case "STRING":
		if fontNameRe.MatchString(low) && !fontNotRe.MatchString(low) {
			return "font"
		}
	}
	return ""
}
