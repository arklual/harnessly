package kitadd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit/formats"
)

// ProbeVar is a CSS custom property a browser read from the kit drawn: see design.ProbeKit.
type ProbeVar struct {
	Name  string // with the leading --
	Value string // resolved: calc(), color-mix() and var() chains are values
	Dark  string
}

var declRe = regexp.MustCompile(`--([A-Za-z0-9_-]+)\s*:`)

// ApplyProbe adds to the kit's tokens what its stylesheets could not give: the custom properties the browser read
// (theme objects in JavaScript, values made of others) that tokens.json has not got yet. A token that is there is
// left as it is: its alias and its usage are better than a resolved copy. It returns how many tokens were added.
func ApplyProbe(folder, title string, vars []ProbeVar) (int, error) {
	toks, _, err := formats.LoadTokens(folder)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	oldCSS, _ := formats.ExportCSS(toks, formats.KitInfo{ID: "kit", Title: title})
	for _, m := range declRe.FindAllStringSubmatch(string(oldCSS), -1) {
		have[m[1]] = true
	}
	var light, dark strings.Builder
	n := 0
	sort.Slice(vars, func(i, j int) bool { return vars[i].Name < vars[j].Name })
	for _, v := range vars {
		name := strings.TrimPrefix(v.Name, "--")
		if have[name] || strings.ContainsAny(name, ".:/ ()%\\") || strings.HasPrefix(name, "tw-") || v.Value == "" || strings.ContainsAny(v.Value, ";{}<>") {
			continue
		}
		fmt.Fprintf(&light, "  --%s: %s;\n", name, v.Value)
		if v.Dark != "" && !strings.ContainsAny(v.Dark, ";{}<>") {
			fmt.Fprintf(&dark, "  --%s: %s;\n", name, v.Dark)
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	css := string(oldCSS) + "\n:root {\n" + light.String() + "}\n"
	if dark.Len() > 0 {
		css += "[data-theme=\"dark\"] {\n" + dark.String() + "}\n"
	}
	merged, _, err := formats.ImportCSS(css, formats.CSSOptions{Modes: []formats.Mode{{ID: "light", Name: "Light"}, {ID: "dark", Name: "Dark"}}})
	if err != nil {
		return 0, err
	}
	for fi := range merged.Families {
		f := &merged.Families[fi]
		for ti := range f.Tokens {
			if ot, _ := toks.Find(f.Tokens[ti].Name); ot != nil && ot.Usage != "" && f.Tokens[ti].Usage == "" {
				f.Tokens[ti].Usage = ot.Usage
			}
		}
	}
	merged.Name = toks.Name
	added := merged.Count() - toks.Count()
	if added <= 0 {
		return 0, nil
	}
	if err := os.WriteFile(filepath.Join(folder, "tokens.json"), merged.Marshal(), 0o644); err != nil {
		return 0, err
	}
	tcss, _ := formats.ExportCSS(merged, formats.KitInfo{ID: kitIDOf(folder), Title: title})
	return added, os.WriteFile(filepath.Join(folder, "tokens.css"), tcss, 0o644)
}

func kitIDOf(folder string) string {
	return strings.TrimPrefix(filepath.Base(folder), ".")
}
