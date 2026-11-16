package kitadd

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

//go:embed analyze.mjs
var analyzeScript []byte

// Analysis is what the types say about the module's exports.
type Analysis struct {
	Exports map[string]Export `json:"exports"`
}

// Export is one export: a component with its props, or something else (a hook, a function, a constant).
type Export struct {
	Kind        string     `json:"kind"`           // component or other
	File        string     `json:"file,omitempty"` // the file that declares it, relative to the entry
	Pos         int        `json:"pos,omitempty"`  // where in that file: parts are drawn in the order they are written
	Description string     `json:"description,omitempty"`
	Props       []TypeProp `json:"props,omitempty"`
}

// TypeProp is a prop as the types declare it.
type TypeProp struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Values      []string `json:"values,omitempty"`
	Required    bool     `json:"required"`
	Description string   `json:"description,omitempty"`
}

// analyze runs analyze.mjs with node in dir on entry; without typescript in dir the kit gets no props, and says so.
func analyze(o *Options, dir, entry string) (*Analysis, error) {
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "typescript", "package.json")); err != nil && o.Node == nil {
		return nil, errors.New("typescript is not installed in the build folder")
	}
	script := filepath.Join(dir, ".kit-analyze.mjs")
	if err := os.WriteFile(script, analyzeScript, 0o644); err != nil {
		return nil, err
	}
	run := o.Node
	if run == nil {
		run = func(dir string, args ...string) ([]byte, error) { return o.runCmd(dir, "node", args...) }
	}
	out, err := run(dir, script, entry)
	if err != nil {
		return nil, err
	}
	var a Analysis
	if err := json.Unmarshal(out, &a); err != nil {
		return nil, fmt.Errorf("the types' reader answered something unreadable: %v", err)
	}
	return &a, nil
}

// The page's React, for every way a package may import it. jsx() builds elements with React.createElement, which
// React 18 understands whatever React the package was written for; the compiler runtime is React 19's memo cache.
var reactShims = map[string]string{
	"react":     "module.exports = window.React;",
	"react-dom": "module.exports = window.ReactDOM;",
	"react/jsx-runtime": `var R = window.React;
function jsx(t, p, k) { if (k !== undefined) { p = Object.assign({}, p, { key: k }); } return R.createElement(t, p); }
module.exports = { jsx: jsx, jsxs: jsx, jsxDEV: jsx, Fragment: R.Fragment };`,
	"react/compiler-runtime": `var R = window.React;
module.exports = { c: function (n) { return R.useMemo(function () { var a = new Array(n); for (var i = 0; i < n; i++) a[i] = Symbol.for("react.memo_cache_sentinel"); return a; }, []); } };`,
}

func reactShim(spec string) string {
	if s, ok := reactShims[spec]; ok {
		return s
	}
	switch {
	case spec == "react/jsx-dev-runtime":
		return reactShims["react/jsx-runtime"]
	case strings.HasPrefix(spec, "react-dom/"):
		return reactShims["react-dom"]
	}
	return reactShims["react"]
}

var unresolvedRe = regexp.MustCompile(`^Could not resolve "([^"]+)"`)

// missingError: the bundle imports packages the build folder does not have (a peer dependency npm left out).
type missingError struct{ pkgs []string }

func (e *missingError) Error() string {
	return "the bundle imports packages that are not installed: " + strings.Join(e.pkgs, ", ")
}

// packageOf is the package a bare import names: @scope/name/sub → @scope/name, name/sub → name.
func packageOf(spec string) string {
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// bundleInstalling is bundle that installs a missing peer dependency and builds again (three rounds at most); with
// no installer (a prepared folder) a missing package is an error.
func bundleInstalling(o *Options, dir, module, namespace string, css []string, extra string, install bool) ([]byte, []byte, error) {
	for round := 0; ; round++ {
		js, styles, err := bundle(dir, module, namespace, css, extra)
		var miss *missingError
		if !errors.As(err, &miss) || !install || round == 3 {
			return js, styles, err
		}
		if err := npmInstall(o, dir, sortedSet(setOf(miss.pkgs))); err != nil {
			return nil, nil, err
		}
	}
}

func setOf(list []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range list {
		m[s] = true
	}
	return m
}

var reactSpecRe = regexp.MustCompile(`^(react|react-dom)(/.*)?$`)

// bundle builds module (an npm name or a file of dir) into one classic script that adds every export to
// window.<namespace>, and the CSS it imports (plus the package's stylesheets in css) into one stylesheet.
func bundle(dir, module, namespace string, css []string, extra string) (js, styles []byte, err error) {
	var entry strings.Builder
	fmt.Fprintf(&entry, "import * as K from %q;\n", module)
	for _, c := range css {
		fmt.Fprintf(&entry, "import %q;\n", c)
	}
	ns, _ := json.Marshal(namespace)
	fmt.Fprintf(&entry, "var ns = window[%s] || (window[%s] = {});\nfor (var k in K) if (k !== \"default\") ns[k] = K[k];\n", ns, ns)
	entry.WriteString(extra)
	res := api.Build(api.BuildOptions{
		Stdin:         &api.StdinOptions{Contents: entry.String(), ResolveDir: dir, Sourcefile: "kit-entry.js", Loader: api.LoaderJS},
		AbsWorkingDir: dir,
		Bundle:        true,
		Write:         false,
		Format:        api.FormatIIFE,
		Platform:      api.PlatformBrowser,
		Target:        api.ES2020,
		Charset:       api.CharsetUTF8,
		LegalComments: api.LegalCommentsNone,
		MinifySyntax:  true, MinifyWhitespace: true, MinifyIdentifiers: true,
		JSX:      api.JSXAutomatic,
		Define:   map[string]string{"process.env.NODE_ENV": `"production"`},
		LogLevel: api.LogLevelSilent,
		Outfile:  "bundle.js",
		Loader: map[string]api.Loader{".woff": api.LoaderDataURL, ".woff2": api.LoaderDataURL, ".ttf": api.LoaderDataURL,
			".otf": api.LoaderDataURL, ".eot": api.LoaderDataURL, ".png": api.LoaderDataURL, ".jpg": api.LoaderDataURL,
			".jpeg": api.LoaderDataURL, ".gif": api.LoaderDataURL, ".webp": api.LoaderDataURL, ".svg": api.LoaderDataURL},
		Plugins: []api.Plugin{{Name: "kit-page-react", Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `^react(-dom)?(/.*)?$`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
				if !reactSpecRe.MatchString(a.Path) {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: a.Path, Namespace: "kit-react"}, nil
			})
			b.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "kit-react"}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
				s := reactShim(a.Path)
				return api.OnLoadResult{Contents: &s, Loader: api.LoaderJS}, nil
			})
		}}},
	})
	if len(res.Errors) > 0 {
		var missing []string
		for _, m := range res.Errors {
			if mm := unresolvedRe.FindStringSubmatch(m.Text); mm != nil && !strings.HasPrefix(mm[1], ".") && !strings.HasPrefix(mm[1], "/") {
				missing = append(missing, packageOf(mm[1]))
			}
		}
		if len(missing) == len(res.Errors) {
			return nil, nil, &missingError{pkgs: missing}
		}
		var msgs []string
		for i, m := range res.Errors {
			if i == 8 {
				msgs = append(msgs, fmt.Sprintf("… and %d more", len(res.Errors)-i))
				break
			}
			at := ""
			if m.Location != nil {
				at = fmt.Sprintf("%s:%d: ", m.Location.File, m.Location.Line)
			}
			msgs = append(msgs, at+m.Text)
		}
		return nil, nil, fmt.Errorf("the bundle does not build:\n  %s", strings.Join(msgs, "\n  "))
	}
	for _, f := range res.OutputFiles {
		switch filepath.Ext(f.Path) {
		case ".js":
			js = f.Contents
		case ".css":
			styles = f.Contents
		}
	}
	// A literal </script would end the script when a board inlines it; <\/script means the same in strings and regexps.
	js = regexp.MustCompile(`(?i)</script`).ReplaceAll(js, []byte(`<\/script`))
	// A line of a template string that starts with "export " (a code sample in a package's warning) would read as a module
	// statement to the kit check; \u0065 is the same letter in a string, and the bundle has no real module statements.
	js = regexp.MustCompile(`(?m)^(\s*)export(\s)`).ReplaceAll(js, []byte(`${1}\u0065xport${2}`))
	js = regexp.MustCompile(`(?m)^(\s*)import(\s)`).ReplaceAll(js, []byte(`${1}\u0069mport${2}`))
	return js, styles, nil
}

// packageCSS is the stylesheets a package publishes: the "style" field and the .css paths of "exports".
func packageCSS(dir, pkg string) []string {
	raw, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return nil
	}
	var pj struct {
		Style   string            `json:"style"`
		Exports json.RawMessage   `json:"exports"`
		Deps    map[string]string `json:"dependencies"`
	}
	if json.Unmarshal(raw, &pj) != nil {
		return nil
	}
	set := map[string]bool{}
	if pj.Style != "" {
		set[pkg+"/"+strings.TrimPrefix(pj.Style, "./")] = true
	}
	var ex map[string]json.RawMessage
	if json.Unmarshal(pj.Exports, &ex) == nil {
		for k := range ex {
			if strings.HasSuffix(k, ".css") && !strings.Contains(k, "*") {
				set[pkg+"/"+strings.TrimPrefix(k, "./")] = true
			}
		}
	}
	if len(set) == 0 && !strings.HasSuffix(pkg, "/styles") {
		// The styles of a package live in a sibling it depends on (@heroui/react → @heroui/styles): its "style" field.
		for dep := range pj.Deps {
			if dep == packageScope(pkg)+"styles" || dep == pkg+"-styles" || dep == pkg+"-theme" {
				if spec := styleSpecifier(dir, dep); spec != "" {
					set[spec] = true
				}
			}
		}
	}
	out := sortedSet(set)
	// Two names for one file (the style field and an export) would load it twice: keep the export's.
	return out
}

// packageVersion is the installed version of pkg.
func packageVersion(dir, pkg string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return ""
	}
	var pj struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &pj)
	return pj.Version
}

// layerCSS puts a stylesheet into @layer <layer>, so its rules on html, body and * restyle nothing but what loads the
// kit, and lose to a board's own styles. @charset goes, @import and @property stay outside: neither may sit in a layer.
func layerCSS(css, layer string) string {
	var outside, inside strings.Builder
	for _, r := range topLevel(css) {
		t := strings.TrimSpace(r)
		low := strings.ToLower(t)
		switch {
		case t == "":
		case strings.HasPrefix(low, "@charset"):
		case strings.HasPrefix(low, "@import"), strings.HasPrefix(low, "@property"):
			outside.WriteString(t + "\n")
		default:
			inside.WriteString(t + "\n")
		}
	}
	if inside.Len() == 0 {
		return outside.String()
	}
	return outside.String() + "@layer " + layer + " {\n" + inside.String() + "}\n"
}

// topLevel splits CSS into its top-level rules and statements, comments dropped, strings kept whole.
func topLevel(css string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for i := 0; i < len(css); i++ {
		c := css[i]
		switch {
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				i = len(css)
			} else {
				i += end + 3
			}
			continue
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(css) && css[j] != c {
				if css[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(css) {
				j = len(css) - 1
			}
			cur.WriteString(css[i : j+1])
			i = j
			continue
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				cur.WriteByte(c)
				out = append(out, cur.String())
				cur.Reset()
				continue
			}
		case c == ';' && depth == 0:
			cur.WriteByte(c)
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

// themeCSS is the registry's colours as CSS variables: light under :root, dark under .dark, the way shadcn writes
// them into a project. It is what the kit's tokens are read from.
func themeCSS(vars map[string]map[string]string) string {
	var b strings.Builder
	for _, mode := range []struct{ key, sel string }{{"light", ":root"}, {"dark", ".dark"}} {
		vs := vars[mode.key]
		if len(vs) == 0 {
			continue
		}
		b.WriteString(mode.sel + " {\n")
		for _, k := range sortedKeys(vs) {
			fmt.Fprintf(&b, "  --%s: %s;\n", k, vs[k])
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// tailwindInput is the stylesheet Tailwind compiles for a shadcn kit: Tailwind, the registry's style CSS, the dark
// variant on the kit's root ([data-theme="dark"]) as well as on .dark, and the theme mapping every colour variable
// to Tailwind's names (bg-primary → var(--primary)). The variables' values are not in it: tokens.css carries them.
func tailwindInput(b *shadcnBuild) string {
	var s strings.Builder
	s.WriteString("@import \"tailwindcss\" source(none);\n@source \"./\";\n")
	var rest strings.Builder
	cssJSON(&rest, b.styleCSS, 0, &s)
	s.WriteString("@custom-variant dark (&:is(.dark *, [data-theme=\"dark\"] *));\n")
	s.WriteString("@theme inline {\n")
	for _, k := range sortedKeys(b.cssVars["theme"]) {
		fmt.Fprintf(&s, "  --%s: %s;\n", k, b.cssVars["theme"][k])
	}
	light := b.cssVars["light"]
	for _, k := range sortedKeys(light) {
		if k == "radius" {
			s.WriteString("  --radius-sm: calc(var(--radius) - 4px);\n  --radius-md: calc(var(--radius) - 2px);\n  --radius-lg: var(--radius);\n  --radius-xl: calc(var(--radius) + 4px);\n")
			continue
		}
		if colorish(light[k]) {
			fmt.Fprintf(&s, "  --color-%s: var(--%s);\n", k, k)
		}
	}
	s.WriteString("}\n")
	s.WriteString(rest.String())
	return s.String()
}

func colorish(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return strings.HasPrefix(v, "#") || strings.HasPrefix(v, "oklch(") || strings.HasPrefix(v, "hsl(") ||
		strings.HasPrefix(v, "rgb(") || strings.HasPrefix(v, "lab(") || strings.HasPrefix(v, "lch(")
}

// cssJSON writes shadcn's CSS-as-JSON ({"@layer base": {"body": {"@apply …": {}}}}): a key with an empty object is a
// statement, other objects are blocks, strings are declarations. Top-level @imports go to imports, the rest to b.
func cssJSON(b *strings.Builder, raw json.RawMessage, depth int, imports *strings.Builder) {
	if len(raw) == 0 {
		return
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	pad := strings.Repeat("  ", depth)
	for _, k := range orderedKeys(raw) {
		v := m[k]
		var s string
		if json.Unmarshal(v, &s) == nil {
			fmt.Fprintf(b, "%s%s: %s;\n", pad, k, s)
			continue
		}
		var sub map[string]json.RawMessage
		if json.Unmarshal(v, &sub) == nil && len(sub) == 0 {
			if depth == 0 && strings.HasPrefix(k, "@import") {
				imports.WriteString(k + ";\n")
			} else {
				fmt.Fprintf(b, "%s%s;\n", pad, k)
			}
			continue
		}
		fmt.Fprintf(b, "%s%s {\n", pad, k)
		cssJSON(b, v, depth+1, imports)
		fmt.Fprintf(b, "%s}\n", pad)
	}
}

// orderedKeys is an object's keys in the order they are written (CSS depends on it).
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		k, _ := t.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// rootSource is the provider a shadcn kit gets: shadcn has none, the kit's pages need one to switch light and dark.
const rootSource = `import * as React from "react"

/** The kit's root: light or dark for everything inside it, with the page's background and text colour. Wrap a board's content in it once. */
export function Root({ theme = "light", children }: {
  /** light or dark: the tokens' mode and Tailwind's dark: variant */
  theme?: "light" | "dark"
  children?: React.ReactNode
}) {
  return (
    <div data-theme={theme} className={(theme === "dark" ? "dark " : "") + "bg-background text-foreground"}>
      {children}
    </div>
  )
}
`

// packageScope is "@scope/" of a scoped package, "" of an unscoped one.
func packageScope(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		if i := strings.Index(pkg, "/"); i > 0 {
			return pkg[:i+1]
		}
	}
	return ""
}

// styleSpecifier is the import that gives a package's main stylesheet: the "style" field when "exports" lets it be
// imported, else the export that points at the same file.
func styleSpecifier(dir, pkg string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return ""
	}
	var pj struct {
		Style   string                     `json:"style"`
		Exports map[string]json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(raw, &pj) != nil || pj.Style == "" {
		return ""
	}
	if len(pj.Exports) == 0 {
		return pkg + "/" + strings.TrimPrefix(pj.Style, "./")
	}
	for _, k := range sortedKeysRaw(pj.Exports) {
		if k != "." && !strings.Contains(k, "*") && strings.Contains(string(pj.Exports[k]), pj.Style) {
			return pkg + "/" + strings.TrimPrefix(k, "./")
		}
	}
	return ""
}

func sortedKeysRaw(m map[string]json.RawMessage) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

var tailwindSourceRe = regexp.MustCompile(`@import\s+["']tailwindcss["']|@tailwind\s`)

// specPackage is the package a specifier like "@heroui/styles/css" names.
func specPackage(spec string) string {
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// isTailwindSource is true when a package's stylesheet is Tailwind input (it imports "tailwindcss"): it is not CSS yet,
// Tailwind has to compile it with the classes the components use.
func isTailwindSource(dir, spec string) bool {
	pkg := specPackage(spec)
	raw, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), "package.json"))
	if err != nil {
		return false
	}
	var pj struct {
		Style string `json:"style"`
	}
	if json.Unmarshal(raw, &pj) != nil || pj.Style == "" {
		return false
	}
	css, err := os.ReadFile(filepath.Join(dir, "node_modules", filepath.FromSlash(pkg), filepath.FromSlash(pj.Style)))
	return err == nil && tailwindSourceRe.Match(css)
}

// compileTailwindSources replaces the stylesheets that are Tailwind input by what Tailwind makes of them, with the
// classes the package's components use (@source its folder): a package such as HeroUI styles its components only that way.
func compileTailwindSources(o *Options, dir, pkg string, css []string, rep *Report) []string {
	var out []string
	for i, spec := range css {
		if !isTailwindSource(dir, spec) {
			out = append(out, spec)
			continue
		}
		file, err := tailwindCompile(o, dir, pkg, spec, i)
		if err != nil {
			rep.NotCarried = append(rep.NotCarried, "styles: "+spec+" is Tailwind input and Tailwind did not compile it: "+firstLine(err.Error()))
			continue
		}
		out = append(out, file)
	}
	return out
}

var tailwindCodeRe = regexp.MustCompile(`@plugin\b|@config\b`)

// tailwindLoadsCode: a stylesheet of the package asks Tailwind to load a script (@plugin, @config), which would run the
// package's code inside the compiler; such a package is not compiled.
func tailwindLoadsCode(dir, spec string) bool {
	root := filepath.Join(dir, "node_modules", filepath.FromSlash(specPackage(spec)))
	found := false
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() && d.Name() == "node_modules" && p != root {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".css") {
			if raw, rerr := os.ReadFile(p); rerr == nil && tailwindCodeRe.Match(raw) {
				found = true
			}
		}
		return nil
	})
	return found
}

func tailwindCompile(o *Options, dir, pkg, spec string, n int) (string, error) {
	if tailwindLoadsCode(dir, spec) {
		return "", fmt.Errorf("its stylesheets load scripts through @plugin or @config, which the kit does not run")
	}
	cli := filepath.Join(dir, "node_modules", "@tailwindcss", "cli", "dist", "index.mjs")
	if _, err := os.Stat(cli); err != nil && o.From == "" {
		need := []string{"tailwindcss@4", "@tailwindcss/cli@4"}
		if err := npmInstall(o, dir, need); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "tw-animate-css")); err != nil && o.From == "" {
		_ = npmInstall(o, dir, []string{"tw-animate-css"}) // imported by some themes; one that does not need it ignores it
	}
	in := filepath.Join(dir, fmt.Sprintf(".kit-pkg-%d.src.css", n))
	src := fmt.Sprintf("@import %q;\n@source %q;\n", spec, "./node_modules/"+pkg)
	if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(dir, fmt.Sprintf(".kit-pkg-%d.css", n))
	run := o.Node
	if run == nil {
		run = func(dir string, args ...string) ([]byte, error) { return o.runCmd(dir, "node", args...) }
	}
	if _, err := run(dir, cli, "-i", in, "-o", out, "--minify"); err != nil {
		return "", err
	}
	return out, nil
}
