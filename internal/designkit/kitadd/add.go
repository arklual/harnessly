// Package kitadd makes a library kit from a package in one command (`harnessly design kit add npm:<package>` or
// `shadcn:<registry>`) and brings it to a new version of the package (`kit update`): the package's exports built into
// one browser script on the page's React, props read from its types, its CSS variables as tokens, a card per
// component. What the files say that a person wrote (what a component is for, when not to use it, samples, token
// usage) is kept on every later run; what comes from the package is rebuilt, and the run says what changed.
package kitadd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// Options say what to make the kit from and where.
type Options struct {
	Source    Source
	Folder    string // the kit folder (one theme): made when missing, updated when it holds this kit
	ID        string // the kit id; from the package name when empty
	Namespace string // the window global; from the id when empty
	Title     string
	Version   string // a shadcn kit's version (a registry has none); an npm kit takes the package's
	Root      string // an npm kit's provider every board wraps its content in
	CSS       []string
	Registry  string // npm: --registry
	From      string // a folder with node_modules already installed: nothing is installed (offline, tests)
	WorkDir   string // the build folder; the user cache when empty
	Cwd       string // where the command runs: its .npmrc is used

	Log   io.Writer
	Npm   func(dir string, args ...string) error           // runs npm; exec when nil
	Node  func(dir string, args ...string) ([]byte, error) // runs node; exec when nil
	Fetch func(loc string) ([]byte, error)                 // reads a registry address or file; http/disk when nil
}

func (o *Options) logf(format string, a ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format, a...)
	}
}

// Report is what a run did, for a person: what came in, what changed since the last run, what could not be read.
type Report struct {
	Source     string       `json:"source"`
	Package    string       `json:"package,omitempty"`
	Version    string       `json:"version"`
	Previous   string       `json:"previous,omitempty"` // the kit's version before an update
	Folder     string       `json:"folder"`
	Components int          `json:"components"`
	Parts      int          `json:"parts"`
	Providers  int          `json:"providers"`
	Tokens     int          `json:"tokens"`
	Added      []string     `json:"added,omitempty"`
	Removed    []string     `json:"removed,omitempty"`
	Changed    []PropChange `json:"changed,omitempty"`
	NotCarried []string     `json:"notCarried,omitempty"` // what could not be read automatically, with why
	ToWrite    []string     `json:"toWrite,omitempty"`    // what a person writes next
	Wrote      []string     `json:"wrote"`
}

// PropChange is how one component's props changed between the kit and the new version.
type PropChange struct {
	Component string   `json:"component"`
	Added     []string `json:"added,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	Changed   []string `json:"changed,omitempty"`
}

// SourceFile records where the kit was made from, so `kit update` can make it again from a newer version.
const SourceFile = "_harnessly/kit-source.json"

type sourceRecord struct {
	Source   string   `json:"source"`
	Root     string   `json:"root,omitempty"`
	CSS      []string `json:"css,omitempty"`
	Registry string   `json:"registry,omitempty"`
}

// ReadSource is the source a kit folder was made from (for `kit update`).
func ReadSource(folder string) (Source, *Options, error) {
	raw, err := os.ReadFile(filepath.Join(folder, filepath.FromSlash(SourceFile)))
	if err != nil {
		return Source{}, nil, fmt.Errorf("%s was not made by `kit add` (no %s)", folder, SourceFile)
	}
	var r sourceRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		return Source{}, nil, fmt.Errorf("%s: %v", SourceFile, err)
	}
	s, err := ParseSource(r.Source)
	return s, &Options{Root: r.Root, CSS: r.CSS, Registry: r.Registry}, err
}

var idCleanRe = regexp.MustCompile(`[^a-z0-9]+`)

// DefaultID is a kit id from a package name: @acme/ui → acme-ui.
func DefaultID(s Source) string {
	if s.Kind == "shadcn" {
		if s.Base == "" {
			return "shadcn-ui"
		}
		return strings.Trim(idCleanRe.ReplaceAllString(strings.ToLower(filepath.Base(strings.TrimRight(s.Base, "/"))), "-"), "-")
	}
	return strings.Trim(idCleanRe.ReplaceAllString(strings.ToLower(s.Package), "-"), "-")
}

// defaultNamespace is the global from an id: acme-ui → AcmeUi.
func defaultNamespace(id string) string {
	var b strings.Builder
	for _, w := range strings.Split(id, "-") {
		if w != "" {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	ns := b.String()
	if ns == "" || (ns[0] >= '0' && ns[0] <= '9') {
		ns = "Kit" + ns
	}
	return ns
}

// built is what the package gave before it is written as a kit.
type built struct {
	pkg, version string
	js, css      []byte
	tokensCSS    string
	comps        []genComp
	others       []string // exports that are not components
	codeImport   func(id string) string
	kitPackage   string
	root         string
}

// Add makes or updates the kit in o.Folder from o.Source.
func Add(o Options) (*Report, error) {
	if o.Folder == "" {
		return nil, errors.New("no kit folder")
	}
	old, err := readOld(o.Folder)
	if err != nil {
		return nil, err
	}
	if old != nil {
		// An update keeps the kit's identity unless the command names another one.
		if o.ID == "" {
			o.ID = old.id
		}
		if o.Namespace == "" {
			o.Namespace = old.namespace
		}
		if o.Title == "" {
			o.Title = old.title
		}
		if o.Root == "" {
			o.Root = old.root
		}
		if o.ID != old.id {
			return nil, fmt.Errorf("%s holds the kit %s, not %s", o.Folder, old.id, o.ID)
		}
	} else if entries, err := os.ReadDir(o.Folder); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty and is not a kit made by `kit add`", o.Folder)
	}
	if o.ID == "" {
		o.ID = DefaultID(o.Source)
	}
	if !designkit.ValidID(o.ID) {
		return nil, fmt.Errorf("kit id %q: lower case letters, digits and -; name it with --id", o.ID)
	}
	if o.Namespace == "" {
		o.Namespace = defaultNamespace(o.ID)
	}
	rep := &Report{Source: o.Source.String(), Folder: o.Folder, Wrote: []string{}}
	var b *built
	switch o.Source.Kind {
	case "npm":
		b, err = buildNpm(&o, rep)
	case "shadcn":
		b, err = buildShadcn(&o, rep, old)
	default:
		err = fmt.Errorf("unknown source %q", o.Source.Kind)
	}
	if err != nil {
		return nil, err
	}
	if o.Title == "" {
		o.Title = b.pkg
	}
	rep.Package, rep.Version = b.pkg, b.version
	if old != nil {
		rep.Previous = old.version
	}
	if err := writeKit(&o, b, old, rep); err != nil {
		return rep, err
	}
	return rep, nil
}

func buildNpm(o *Options, rep *Report) (*built, error) {
	src := o.Source
	dir := o.From
	if dir == "" {
		var err error
		if dir, err = workDir(o, "npm:"+src.Package); err != nil {
			return nil, err
		}
		want := src.Package
		if src.Version != "" {
			want += "@" + src.Version
		} else {
			want += "@latest"
		}
		if err := npmInstall(o, dir, append([]string{want}, buildPackages...)); err != nil {
			return nil, err
		}
	}
	version := packageVersion(dir, src.Package)
	if version == "" {
		return nil, fmt.Errorf("%s is not installed in %s", src.Package, dir)
	}
	if src.Version != "" && designkit.ValidVersion(src.Version) && version != src.Version {
		return nil, fmt.Errorf("%s: %s is installed, %s was asked for", src.Package, version, src.Version)
	}
	if !designkit.ValidVersion(version) {
		return nil, fmt.Errorf("%s's version %q is not major.minor.patch", src.Package, version)
	}
	css := o.CSS
	if len(css) == 0 {
		css = packageCSS(dir, src.Package)
	}
	b := &built{pkg: src.Package, version: version, kitPackage: src.Package, root: o.Root,
		codeImport: func(string) string { return src.Package }}
	var err error
	if b.js, b.css, err = bundleInstalling(o, dir, src.Package, o.Namespace, css, o.From == ""); err != nil {
		return nil, err
	}
	if len(css) == 0 && len(b.css) == 0 {
		rep.NotCarried = append(rep.NotCarried, "styles: the package publishes no stylesheet (no \"style\" field, no .css in \"exports\"); name one with --css <package>/<file>.css")
	}
	b.tokensCSS = string(b.css)
	entry := filepath.Join(dir, ".kit-entry.ts")
	if err := os.WriteFile(entry, []byte(fmt.Sprintf("export * from %q;\n", src.Package)), 0o644); err != nil {
		return nil, err
	}
	b.comps, b.others = components(o, dir, entry, src.Package, rep, nil)
	if o.Root != "" {
		found := false
		for i := range b.comps {
			if b.comps[i].ID == o.Root {
				b.comps[i].Part, found = designkit.PartProvider, true
			}
		}
		if !found {
			rep.NotCarried = append(rep.NotCarried, "root: the package exports no component "+o.Root+"; the kit has no root")
			b.root = ""
		}
	}
	return b, nil
}

func buildShadcn(o *Options, rep *Report, old *oldKit) (*built, error) {
	dir, err := workDir(o, o.Source.String())
	if o.From != "" {
		dir, err = o.From, nil
	}
	if err != nil {
		return nil, err
	}
	// The registry's files are written afresh: a component the registry dropped must not stay in the build.
	_ = os.RemoveAll(filepath.Join(dir, "src"))
	reg, err := fetchShadcn(o, o.Source, dir)
	if err != nil {
		return nil, err
	}
	for _, r := range reg.refused {
		rep.NotCarried = append(rep.NotCarried, fmt.Sprintf("dependency %q: not a plain npm package (a path, an address, git or an option), not installed", r))
	}
	for _, m := range reg.missing {
		rep.NotCarried = append(rep.NotCarried, "registry item "+m+": named as a dependency, the registry does not have it")
	}
	if o.From == "" {
		pkgs := append([]string{}, reg.deps...)
		pkgs = append(pkgs, reg.devDeps...)
		pkgs = append(pkgs, "tailwindcss@4", "@tailwindcss/cli@4")
		if err := npmInstall(o, dir, append(pkgs, buildPackages...)); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "Root.tsx"), []byte(rootSource), 0o644); err != nil {
		return nil, err
	}
	var index strings.Builder
	for _, f := range reg.files {
		fmt.Fprintf(&index, "export * from \"./%s\";\n", strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(f, "src/"), ".tsx"), ".ts"))
	}
	index.WriteString("export * from \"./Root\";\n")
	if err := os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte(index.String()), 0o644); err != nil {
		return nil, err
	}
	version := o.Version
	if version == "" && old != nil {
		version = old.version
	}
	if version == "" {
		version = "1.0.0"
	}
	if !designkit.ValidVersion(version) {
		return nil, fmt.Errorf("version %q: major.minor.patch", version)
	}
	b := &built{pkg: "shadcn/ui", version: version, kitPackage: "@/components/ui", root: "Root",
		codeImport: func(file string) string { return "@/components/ui/" + file }}
	if o.Source.Base != "" {
		b.pkg = o.Source.Base
	}
	if b.js, b.css, err = bundleInstalling(o, dir, "./src/index.ts", o.Namespace, nil, o.From == ""); err != nil {
		return nil, err
	}
	if len(reg.cssVars) == 0 {
		rep.NotCarried = append(rep.NotCarried, "tokens: the registry has no colours (cssVars of its style or theme)")
	}
	tw, err := tailwind(o, dir, reg)
	if err != nil {
		rep.NotCarried = append(rep.NotCarried, "styles: Tailwind did not compile the components' classes: "+firstLine(err.Error()))
	}
	b.css = append(b.css, tw...)
	b.tokensCSS = themeCSS(reg.cssVars)
	b.comps, b.others = components(o, dir, filepath.Join(dir, "src", "index.ts"), "", rep, reg)
	for i := range b.comps {
		if b.comps[i].ID == "Root" {
			b.comps[i].Part = designkit.PartProvider
		}
	}
	return b, nil
}

// tailwind compiles the classes the components use, with the registry's theme, into CSS.
func tailwind(o *Options, dir string, reg *shadcnBuild) ([]byte, error) {
	in := filepath.Join(dir, "src", "kit.css")
	if err := os.WriteFile(in, []byte(tailwindInput(reg)), 0o644); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, ".kit-tailwind.css")
	cli := filepath.Join(dir, "node_modules", "@tailwindcss", "cli", "dist", "index.mjs")
	if _, err := os.Stat(cli); err != nil && o.Node == nil {
		return nil, errors.New("@tailwindcss/cli is not installed in the build folder")
	}
	run := o.Node
	if run == nil {
		run = func(dir string, args ...string) ([]byte, error) { return runCmd(dir, nil, "node", args...) }
	}
	if _, err := run(dir, cli, "-i", in, "-o", out, "--minify"); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// exportNames is what module exports, read by esbuild: the list to fall back on when the types cannot be read.
func exportNames(dir, module string) []string {
	res := api.Build(api.BuildOptions{
		Stdin:         &api.StdinOptions{Contents: fmt.Sprintf("export * from %q;\n", module), ResolveDir: dir, Loader: api.LoaderJS},
		AbsWorkingDir: dir, Bundle: true, Write: false, Format: api.FormatESModule, Platform: api.PlatformBrowser,
		Metafile: true, LogLevel: api.LogLevelSilent, Outfile: "x.js", JSX: api.JSXAutomatic,
		External: []string{"react", "react-dom", "react/*", "react-dom/*"},
		Loader:   map[string]api.Loader{".css": api.LoaderEmpty, ".woff2": api.LoaderEmpty, ".woff": api.LoaderEmpty, ".svg": api.LoaderEmpty, ".png": api.LoaderEmpty},
	})
	var meta struct {
		Outputs map[string]struct {
			Exports []string `json:"exports"`
		} `json:"outputs"`
	}
	if json.Unmarshal([]byte(res.Metafile), &meta) != nil {
		return nil
	}
	var out []string
	for _, o := range meta.Outputs {
		out = append(out, o.Exports...)
	}
	sort.Strings(out)
	return out
}

// genComp is a component as the package gives it.
type genComp struct {
	ID          string
	Part        string
	Description string // from the doc comment; "" when the package has none
	Props       []TypeProp
	File        string // the file that declares it (shadcn: the registry file's base name)
	Pos         int    // its place in that file
	Code        string // code.import
}

// components reads the module's exports: components with their props, and the rest by name.
func components(o *Options, dir, entry, module string, rep *Report, reg *shadcnBuild) ([]genComp, []string) {
	a, err := analyze(o, dir, entry)
	if err != nil {
		rep.NotCarried = append(rep.NotCarried, "props: the types could not be read ("+firstLine(err.Error())+"): components are listed by their export names, without props")
		a = &Analysis{Exports: map[string]Export{}}
		mod := module
		if mod == "" {
			mod = "./" + filepath.ToSlash(mustRel(dir, entry))
		}
		for _, n := range exportNames(dir, mod) {
			kind := "other"
			if regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`).MatchString(n) && !strings.HasSuffix(n, "Context") {
				kind = "component"
			}
			a.Exports[n] = Export{Kind: kind}
		}
	}
	var comps []genComp
	var others []string
	for _, name := range sortedKeys(a.Exports) {
		e := a.Exports[name]
		if e.Kind != "component" {
			others = append(others, name)
			continue
		}
		c := genComp{ID: name, Description: e.Description, Props: e.Props, File: e.File, Pos: e.Pos}
		if reg != nil {
			base := strings.TrimSuffix(strings.TrimSuffix(filepathBase(e.File), ".tsx"), ".ts")
			c.File = base
			c.Code = "@/components/ui/" + base
			if c.Description == "" && reg.descs[base] != "" && isMain(name, base) {
				c.Description = reg.descs[base]
			}
			if name == "Root" {
				c.Code = ""
			}
		} else {
			c.Code = module
		}
		comps = append(comps, c)
	}
	markParts(comps)
	return comps, others
}

func mustRel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return r
}

func filepathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// isMain is whether a component is the one its file is named after (button.tsx → Button).
func isMain(id, file string) bool {
	return strings.EqualFold(strings.ReplaceAll(file, "-", ""), id)
}

// markParts: a component named <Provider> is a provider; in a file that declares several components, the ones named
// after another of the file (CardHeader after Card) are its parts. A part's parent is never a part itself.
func markParts(comps []genComp) {
	byFile := map[string][]int{}
	for i, c := range comps {
		if strings.HasSuffix(c.ID, "Provider") {
			comps[i].Part = designkit.PartProvider
			continue
		}
		if c.File != "" {
			byFile[c.File] = append(byFile[c.File], i)
		}
	}
	for _, idx := range byFile {
		var mains []string
		for _, i := range idx {
			main := true
			for _, j := range idx {
				if i != j && strings.HasPrefix(comps[i].ID, comps[j].ID) && len(comps[i].ID) > len(comps[j].ID) {
					main = false
					break
				}
			}
			if main {
				mains = append(mains, comps[i].ID)
			}
		}
		for _, i := range idx {
			best := ""
			for _, m := range mains {
				if m != comps[i].ID && strings.HasPrefix(comps[i].ID, m) && len(m) > len(best) {
					best = m
				}
			}
			if best != "" {
				comps[i].Part = best
			}
		}
	}
}
