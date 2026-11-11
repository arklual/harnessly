package design

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A page written as a module.
//
// The source of a page is project/pages/<name>.tsx: an ordinary React module that imports the kit's components by the
// kit's package name, takes its data from project/pages/<name>.fixtures.ts and exports the page as its default. The
// board project/<name>.dc.html is what the library builds from it, for the board engine to draw; the file says in its
// first lines that it is built, and from what. The same .tsx is what goes into the product.
//
// What a page may import:
//   - the kit package the project is pinned to (kit.package of its design-system.json), or a subpath a component's
//     code.import names: resolved to the kit's bundle (window.<Namespace>.<Component>) the board loads;
//   - react: the engine's React;
//   - relative files of the project (other pages' fixtures, shared views, data): .ts .tsx .js .jsx .json.
//
// Everything else is a build error that says so. The build is a function of the source files, the kit version and
// the builder's version: the same inputs give the same bytes, and the bytes are kept by the inputs' versions.

const (
	pagesDirname  = "pages"
	pageSrcExt    = ".tsx"
	pageFixtureEx = ".fixtures"
	// pageBuilderVersion changes with anything in the build that changes its output.
	pageBuilderVersion = "2"
	pageGlobal         = "__tkPage"
	pageMarkPrefix     = "<!-- tk-page: "
	pageMaxSource      = 4 << 20
	pageKitNS          = "tk-kit"
	pageShimNS         = "tk-shim"
)

// A page's name is a board's name: letters and digits of any script, spaces and the punctuation boards are named
// with (a migrated board keeps its file name); never a slash, a quote or a leading dot.
var pageNameRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _.,·:()«»—–-]{0,120}$`)
var pageMarkRe = regexp.MustCompile(`^<!-- tk-page: generated from (.+?) \(in:([0-9a-f]{16})\)`)
var jsIdentRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
var noMatchingExport = regexp.MustCompile(`No matching export in "[^"]*" for import "([^"]+)"`)

// PageBuild is the outcome of building one page.
type PageBuild struct {
	Page   string   `json:"page"`
	Source string   `json:"source"`
	Board  string   `json:"board"`
	Status string   `json:"status"` // built, current, error
	Error  string   `json:"error,omitempty"`
	Inputs []string `json:"inputs,omitempty"`
}

// PageSourceProblem is a finding of the check about a page that has a module source.
type PageSourceProblem struct {
	Page    string `json:"page"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

type pageInput struct {
	path string
	ver  string
}

type pageCacheEntry struct {
	inputs []pageInput
	kit    string
	board  []byte
}

type pageBuildState struct {
	mu      sync.Mutex
	entries map[string]*pageCacheEntry
	builds  int
	timers  map[string]*time.Timer
	stop    func()
}

// PageBuildsDone is how many builds ran (not served from the cache) since the library started: a test's counter.
func (l *Library) PageBuildsDone() int {
	l.pageBuilds.mu.Lock()
	defer l.pageBuilds.mu.Unlock()
	return l.pageBuilds.builds
}

func pageErrorf(en, ru string, a ...any) error {
	return errors.New(locale.Pick(fmt.Sprintf(en, a...), fmt.Sprintf(ru, a...)))
}

// PageSources are the names of the project's pages that have a module source, sorted.
func (l *Library) PageSources(project string) []string {
	dir := l.ProjectDirOf(project)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(dir, pagesDirname))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, pageSrcExt) {
			continue
		}
		base := strings.TrimSuffix(n, pageSrcExt)
		if strings.HasSuffix(base, pageFixtureEx) || !pageNameRe.MatchString(base) {
			continue
		}
		out = append(out, base)
	}
	sort.Strings(out)
	return out
}

// kitForPages is what a page's build needs of the project's kit.
type kitForPages struct {
	ID, Version, Mount string
	Package            string
	Namespace          string
	Root               string // the provider component
	// byImport: module specifier -> export name -> component id in the namespace.
	byImport map[string]map[string]string
	exports  []string
	stamp    string
}

func (l *Library) pageKit(project string) (*kitForPages, error) {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return nil, nil
	}
	root := l.kitRootOf(pin)
	if root == "" {
		return nil, pageErrorf("the kit %s %s the project is pinned to is not installed", "кит %s %s, к которому привязан проект, не установлен", pin.ID, pin.Version)
	}
	mounts := pin.Mounts
	if len(mounts) == 0 {
		mounts = designkit.Mounts(root, pin.ID)
	}
	mount := ""
	for _, m := range mounts {
		if designkit.MountOf(root, pin.ID, m) != "" {
			mount = m
			break
		}
	}
	if mount == "" {
		return nil, pageErrorf("the kit %s has no theme the project can mount", "у кита %s нет темы, которую можно подключить к проекту", pin.ID)
	}
	manifest := filepath.Join(designkit.MountOf(root, pin.ID, mount), designkit.ManifestFile)
	raw, err := os.ReadFile(manifest)
	if err != nil {
		return nil, pageErrorf("the kit %s has no %s", "в ките %s нет %s", pin.ID, designkit.ManifestFile)
	}
	var all map[string]json.RawMessage
	if json.Unmarshal(raw, &all) != nil {
		return nil, pageErrorf("%s of the kit %s is not valid JSON", "%s кита %s — не JSON", designkit.ManifestFile, pin.ID)
	}
	k := &kitForPages{ID: pin.ID, Version: pin.Version, Mount: mount, byImport: map[string]map[string]string{}}
	if pin.Draft {
		k.Version = "draft"
	}
	var head struct {
		Namespace string `json:"namespace"`
		Groups    []struct {
			Components []struct {
				ID   string `json:"id"`
				Code *struct {
					Import string `json:"import"`
					Export string `json:"export"`
				} `json:"code"`
			} `json:"components"`
		} `json:"groups"`
	}
	_ = json.Unmarshal(raw, &head)
	k.Namespace = head.Namespace
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		var rec struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			Package string `json:"package"`
			Root    string `json:"root"`
		}
		if json.Unmarshal(all[n], &rec) == nil && rec.ID != "" && rec.Version != "" {
			k.Package, k.Root = rec.Package, rec.Root
			break
		}
	}
	if k.Package == "" {
		return nil, pageErrorf("the kit %s does not name its package (kit.package in %s): pages cannot import from it", "кит %s не называет свой пакет (kit.package в %s): страницы не могут из него импортировать", pin.ID, designkit.ManifestFile)
	}
	if !jsIdentRe.MatchString(k.Namespace) {
		return nil, pageErrorf("the kit %s has no namespace", "у кита %s нет namespace", pin.ID)
	}
	seen := map[string]bool{}
	for _, g := range head.Groups {
		for _, c := range g.Components {
			if c.Code == nil || c.Code.Import == "" || !jsIdentRe.MatchString(c.Code.Export) || c.ID == "" {
				continue
			}
			m := k.byImport[c.Code.Import]
			if m == nil {
				m = map[string]string{}
				k.byImport[c.Code.Import] = m
			}
			if _, dup := m[c.Code.Export]; !dup {
				m[c.Code.Export] = c.ID
			}
			if !seen[c.Code.Export] {
				seen[c.Code.Export] = true
				k.exports = append(k.exports, c.Code.Export)
			}
		}
	}
	sort.Strings(k.exports)
	sum := sha256.Sum256(raw)
	k.stamp = pin.ID + "@" + k.Version + "/" + mount + "/" + hex.EncodeToString(sum[:8])
	return k, nil
}

func (k *kitForPages) owns(spec string) bool {
	return k != nil && k.Package != "" && (spec == k.Package || strings.HasPrefix(spec, k.Package+"/"))
}

// insideProject is whether real is dir or below it.
func insideProject(dir, real string) bool {
	return real == dir || strings.HasPrefix(real, dir+string(filepath.Separator))
}

var pageResolveExts = []string{".tsx", ".ts", ".jsx", ".js", ".json"}

// resolvePageFile is the file a relative import names, with an extension tried or an index file; "" when none.
func resolvePageFile(base string) string {
	if st, err := os.Stat(base); err == nil && st.Mode().IsRegular() {
		return base
	}
	for _, e := range pageResolveExts {
		if st, err := os.Stat(base + e); err == nil && st.Mode().IsRegular() {
			return base + e
		}
	}
	for _, e := range pageResolveExts {
		f := filepath.Join(base, "index"+e)
		if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
			return f
		}
	}
	return ""
}

func (l *Library) pagePlugin(project, projectDir string, kit *kitForPages) api.Plugin {
	realDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		realDir = projectDir
	}
	fail := func(en, ru string, a ...any) (api.OnResolveResult, error) {
		return api.OnResolveResult{}, pageErrorf(en, ru, a...)
	}
	return api.Plugin{Name: "tk-pages", Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			spec := a.Path
			if a.Kind == api.ResolveEntryPoint {
				return api.OnResolveResult{Path: spec, Namespace: "file"}, nil
			}
			switch {
			case kit.owns(spec):
				if _, ok := kit.byImport[spec]; !ok {
					known := make([]string, 0, len(kit.byImport))
					for s := range kit.byImport {
						known = append(known, s)
					}
					sort.Strings(known)
					return fail("%q is not a path the kit %s exports components from (known: %s)",
						"«%s» — не путь, из которого кит %s отдаёт компоненты (известны: %s)", spec, kit.ID, strings.Join(known, ", "))
				}
				return api.OnResolveResult{Path: spec, Namespace: pageKitNS}, nil
			case spec == "react":
				return api.OnResolveResult{Path: spec, Namespace: pageShimNS}, nil
			case strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
				if a.Namespace != "file" {
					return fail("relative import %q from outside the project", "относительный импорт «%s» не из файла проекта", spec)
				}
				if ext := strings.ToLower(path.Ext(spec)); ext == ".css" || ext == ".scss" || ext == ".svg" || ext == ".png" || ext == ".jpg" {
					return fail("%q: only code is imported; styles and images come from the kit, the board loads them",
						"«%s»: импортируется только код; стили и картинки приходят из кита, доска подключает их сама", spec)
				}
				file := resolvePageFile(filepath.Join(a.ResolveDir, filepath.FromSlash(spec)))
				if file == "" {
					return fail("%q is not found (tried .tsx .ts .jsx .js .json and index)", "«%s» не найден (пробовал .tsx .ts .jsx .js .json и index)", spec)
				}
				real, err := filepath.EvalSymlinks(file)
				if err != nil || !insideProject(realDir, real) {
					return fail("%q leaves the project: pages import files of their own project only", "«%s» выходит из проекта: страницы импортируют только файлы своего проекта", spec)
				}
				rel, _ := filepath.Rel(realDir, real)
				if first, _, _ := strings.Cut(filepath.ToSlash(rel), "/"); first == "ds" || first == ".git" || first == "node_modules" {
					return fail("%q is a kit or tool file: import the kit by its package name", "«%s» — файл кита или инструмента: кит импортируется по имени пакета", spec)
				}
				return api.OnResolveResult{Path: real, Namespace: "file"}, nil
			}
			pkg := "none: the project has no kit"
			if kit != nil {
				pkg = kit.Package
			}
			return fail("import %q is not allowed: a page imports the kit package (%s), react and relative files of the project",
				"импорт «%s» не разрешён: страница импортирует пакет кита (%s), react и относительные файлы проекта", spec, pkg)
		})
		b.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: pageKitNS}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
			names := make([]string, 0, len(kit.byImport[a.Path]))
			for n := range kit.byImport[a.Path] {
				names = append(names, n)
			}
			sort.Strings(names)
			var sb strings.Builder
			sb.WriteString("var __ns = window." + kit.Namespace + " || {};\n")
			for _, n := range names {
				id, _ := json.Marshal(kit.byImport[a.Path][n])
				sb.WriteString("export const " + n + " = __ns[" + string(id) + "];\n")
			}
			js := sb.String()
			return api.OnLoadResult{Contents: &js, Loader: api.LoaderJS, ResolveDir: projectDir}, nil
		})
		b.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: pageShimNS}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
			js := "module.exports = window.React;\n"
			return api.OnLoadResult{Contents: &js, Loader: api.LoaderJS, ResolveDir: projectDir}, nil
		})
	}}
}

// pageMessages turns esbuild's errors into one text for a person, the kit's names added where an import fails.
func pageMessages(msgs []api.Message, projectDir string, kit *kitForPages) error {
	var lines []string
	for _, m := range msgs {
		text := m.Text
		if mm := noMatchingExport.FindStringSubmatch(text); mm != nil && kit != nil {
			text = fmt.Sprintf(locale.Pick("%q is not exported by the kit package %s (kit %s %s). Components it exports: %s",
				"«%s» не экспортируется пакетом кита %s (кит %s %s). Он отдаёт: %s"), mm[1], kit.Package, kit.ID, kit.Version, strings.Join(kit.exports, ", "))
		}
		at := ""
		if m.Location != nil {
			f := m.Location.File
			if rel, err := filepath.Rel(projectDir, f); err == nil && !strings.HasPrefix(rel, "..") {
				f = filepath.ToSlash(rel)
			}
			at = f + ":" + strconv.Itoa(m.Location.Line) + ":" + strconv.Itoa(m.Location.Column+1) + ": "
		}
		lines = append(lines, at+text)
	}
	if len(lines) == 0 {
		lines = []string{locale.Pick("the page did not build", "страница не собралась")}
	}
	return errors.New(strings.Join(lines, "\n"))
}

func inScript(code string) string {
	code = strings.ReplaceAll(code, "</script", `<\/script`)
	code = strings.ReplaceAll(code, "</SCRIPT", `<\/SCRIPT`)
	return strings.ReplaceAll(code, "<!--", `<\!--`)
}

func attr(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(s)
}

// buildPageBoard is the board's bytes for the page and the files it was built from.
func (l *Library) buildPageBoard(project, name string) (board []byte, inputs []string, err error) {
	projectDir := l.ProjectDirOf(project)
	src := filepath.Join(projectDir, pagesDirname, name+pageSrcExt)
	st, err := os.Stat(src)
	if err != nil || !st.Mode().IsRegular() {
		return nil, nil, pageErrorf("there is no pages/%s%s in the project", "в проекте нет pages/%s%s", name, pageSrcExt)
	}
	realDir, derr := filepath.EvalSymlinks(projectDir)
	realSrc, serr := filepath.EvalSymlinks(src)
	if derr != nil || serr != nil || !insideProject(realDir, realSrc) {
		return nil, nil, pageErrorf("pages/%s%s leaves the project", "pages/%s%s выходит из проекта", name, pageSrcExt)
	}
	src = realSrc
	if st.Size() > pageMaxSource {
		return nil, nil, pageErrorf("pages/%s%s is too large", "pages/%s%s слишком большой", name, pageSrcExt)
	}
	kit, err := l.pageKit(project)
	if err != nil {
		return nil, nil, err
	}
	res := api.Build(api.BuildOptions{
		EntryPoints:   []string{src},
		AbsWorkingDir: projectDir,
		Bundle:        true,
		Write:         false,
		Metafile:      true,
		Format:        api.FormatIIFE,
		GlobalName:    pageGlobal,
		Platform:      api.PlatformBrowser,
		Target:        api.ESNext,
		Charset:       api.CharsetUTF8,
		LegalComments: api.LegalCommentsNone,
		// An import used for types only is dropped by the TS rules before it is resolved: keep every import, so a
		// foreign package is named even when the page uses it for nothing.
		TsconfigRaw: `{"compilerOptions":{"importsNotUsedAsValues":"preserve","preserveValueImports":true}}`,
		JSX:         api.JSXTransform,
		JSXFactory:  "React.createElement",
		JSXFragment: "React.Fragment",
		LogLevel:    api.LogLevelSilent,
		Plugins:     []api.Plugin{l.pagePlugin(project, projectDir, kit)},
		Loader:      map[string]api.Loader{".js": api.LoaderJSX, ".json": api.LoaderJSON, ".ts": api.LoaderTS, ".tsx": api.LoaderTSX},
		Outfile:     "page.js",
	})
	if len(res.Errors) > 0 {
		return nil, nil, pageMessages(res.Errors, projectDir, kit)
	}
	if len(res.OutputFiles) != 1 {
		return nil, nil, pageErrorf("the page did not build to one script", "страница не собралась в один скрипт")
	}
	var meta struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if json.Unmarshal([]byte(res.Metafile), &meta) != nil {
		return nil, nil, pageErrorf("the build report is unreadable", "отчёт сборки не читается")
	}
	for k := range meta.Inputs {
		if strings.Contains(k, ":") && !strings.HasPrefix(k, "/") {
			continue // a virtual module (the kit, react)
		}
		p := k
		if !filepath.IsAbs(p) {
			p = filepath.Join(projectDir, filepath.FromSlash(k))
		}
		rel, rerr := filepath.Rel(projectDir, p)
		if rerr != nil || strings.HasPrefix(rel, "..") {
			if real, e := filepath.EvalSymlinks(projectDir); e == nil {
				rel, rerr = filepath.Rel(real, p)
			}
			if rerr != nil || strings.HasPrefix(rel, "..") {
				continue
			}
		}
		inputs = append(inputs, filepath.ToSlash(rel))
	}
	frame, err := readPageFrame(projectDir, name)
	if err != nil {
		return nil, nil, err
	}
	if frame != "" {
		inputs = append(inputs, pagesDirname+"/"+name+pageFrameExt)
	}
	sort.Strings(inputs)
	h := sha256.New()
	h.Write([]byte("builder " + pageBuilderVersion + "\n"))
	if kit != nil {
		h.Write([]byte("kit " + kit.stamp + "\n"))
	}
	for _, in := range inputs {
		raw, rerr := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(in)))
		if rerr != nil {
			return nil, nil, rerr
		}
		sum := sha256.Sum256(raw)
		h.Write([]byte(in + " " + hex.EncodeToString(sum[:]) + "\n"))
	}
	stamp := hex.EncodeToString(h.Sum(nil))[:16]

	mark := pageMarkPrefix + "generated from pages/" + name + pageSrcExt + " (in:" + stamp + "). Edit that file: this board is rebuilt from it and a change made here is lost. -->"
	// The engine loads React after this script: the page's module runs once React is on window.
	bundle := "<script>\n(function tkPageWhenReact() {\nif (!window.React) { setTimeout(tkPageWhenReact, 5); return; }\n" +
		inScript(string(res.OutputFiles[0].Contents)) + "\nwindow." + pageGlobal + " = " + pageGlobal + ";\n})();\n</script>\n"
	var kitHead strings.Builder
	if kit != nil {
		base := "ds/" + kit.Mount
		kitHead.WriteString("<link rel=\"stylesheet\" href=\"" + attr(base) + "/tokens.css\">\n")
		kitHead.WriteString("<link rel=\"stylesheet\" href=\"" + attr(base) + "/components/bundle.css\">\n")
		kitHead.WriteString("<script src=\"" + attr(base) + "/components/bundle.js\"></script>\n")
	}
	if frame != "" {
		board, err := framePage(frame, name, mark, kitHead.String()+bundle)
		return board, inputs, err
	}
	var sb strings.Builder
	sb.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	sb.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	sb.WriteString(mark + "\n")
	sb.WriteString("<script src=\"./support.js\"></script>\n")
	sb.WriteString(kitHead.String())
	sb.WriteString("<style>html,body{margin:0}</style>\n")
	sb.WriteString(bundle)
	sb.WriteString("</head>\n<body>\n<x-dc>\n<main class=\"board\">\n")
	sb.WriteString("<x-import component-from-global-scope=\"" + pageGlobal + ".default\"></x-import>\n")
	sb.WriteString("</main>\n</x-dc>\n</body>\n</html>\n")
	return []byte(sb.String()), inputs, nil
}

var (
	headOpenRe    = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
	supportTagRe  = regexp.MustCompile(`(?i)<script\s+src="\./support\.js"\s*></script>`)
	headCloseRe   = regexp.MustCompile(`(?i)</head\s*>`)
	frameSlotsMax = 1
)

// readPageFrame is pages/<name>.board.html, the frame a page migrated from a board is built into: the old board file
// with the page's place (one <x-import> of the page) instead of its template, "" when the page has none.
func readPageFrame(projectDir, name string) (string, error) {
	file := filepath.Join(projectDir, pagesDirname, name+pageFrameExt)
	st, err := os.Lstat(file)
	if err != nil {
		return "", nil
	}
	realDir, derr := filepath.EvalSymlinks(projectDir)
	real, rerr := filepath.EvalSymlinks(file)
	if derr != nil || rerr != nil || !insideProject(realDir, real) || !st.Mode().IsRegular() {
		return "", pageErrorf("pages/%s%s leaves the project", "pages/%s%s выходит из проекта", name, pageFrameExt)
	}
	if st.Size() > pageMaxSource {
		return "", pageErrorf("pages/%s%s is too large", "pages/%s%s слишком большой", name, pageFrameExt)
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// framePage puts a built page into its frame: the mark first in <head>, the page's script after the engine's.
func framePage(frame, name, mark, scripts string) ([]byte, error) {
	start, end, ok := dcTemplateRange(frame)
	if !ok || strings.Count(frame[start:end], pageSlot) != frameSlotsMax {
		return nil, pageErrorf("pages/%s%s must have one <x-dc> with the page's place in it: %s", "в pages/%s%s должен быть один <x-dc> и в нём место страницы: %s", name, pageFrameExt, pageSlot)
	}
	head := headOpenRe.FindStringIndex(frame)
	if head == nil || head[1] > 1024 || head[1] > start {
		return nil, pageErrorf("pages/%s%s has no <head> near its start", "в pages/%s%s нет <head> в начале", name, pageFrameExt)
	}
	at := head[1]
	out := frame[:at] + "\n" + mark + frame[at:]
	shift := len(mark) + 1
	if s := supportTagRe.FindStringIndex(out); s != nil && s[1] < start+shift {
		return []byte(out[:s[1]] + "\n" + strings.TrimSuffix(scripts, "\n") + out[s[1]:]), nil
	}
	if c := headCloseRe.FindStringIndex(out); c != nil && c[0] < start+shift {
		return []byte(out[:c[0]] + scripts + out[c[0]:]), nil
	}
	return nil, pageErrorf("pages/%s%s has no </head>", "в pages/%s%s нет </head>", name, pageFrameExt)
}

// pageStamp is the build stamp and source a board file carries, "" when it is not a built board.
func pageStamp(head []byte) (source, stamp string) {
	if len(head) > 2048 {
		head = head[:2048]
	}
	for _, ln := range strings.Split(string(head), "\n") {
		if m := pageMarkRe.FindStringSubmatch(ln); m != nil {
			return m[1], m[2]
		}
	}
	return "", ""
}

// BoardIsBuilt is whether the board file of the project is built from a page module, and from which.
func (l *Library) BoardIsBuilt(project, boardFile string) (source string, built bool) {
	dir := l.ProjectDirOf(project)
	if dir == "" || boardFile == "" || strings.Contains(boardFile, "..") {
		return "", false
	}
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(pageFileOf(boardFile))))
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, 2048)
	n, _ := f.Read(buf)
	src, _ := pageStamp(buf[:n])
	if src == "" {
		return "", false
	}
	// A board whose source is gone is an ordinary board again.
	if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(src))); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return src, true
}

// BuiltBoardReason is why a write to the board file must be refused, "" when it may go on.
func (l *Library) BuiltBoardReason(project, boardFile string) string {
	src, ok := l.BoardIsBuilt(project, boardFile)
	if !ok {
		return ""
	}
	return locale.Pick(
		"The board "+boardFile+" is built from "+src+": change that file (the board is rebuilt from it, a change made in the board is lost). To make the board an ordinary one, delete "+src+".",
		"Доска "+boardFile+" собрана из "+src+": правьте этот файл (доска пересобирается из него, правка в самой доске пропадёт). Чтобы доска стала обычной, удалите "+src+".")
}

func (l *Library) pageInputsCurrent(e *pageCacheEntry, projectDir, kitStamp string) bool {
	if e.kit != kitStamp {
		return false
	}
	for _, in := range e.inputs {
		st, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(in.path)))
		if err != nil || FileVersion(st) != in.ver {
			return false
		}
	}
	return true
}

// BuildPage builds the board of pages/<name>.tsx and writes it when its bytes changed. It does not touch a board
// file that is not built from this page.
func (l *Library) BuildPage(project, name string) PageBuild {
	out := PageBuild{Page: name, Source: pagesDirname + "/" + name + pageSrcExt, Board: name + ".dc.html"}
	if !IsLibraryID(project) || !pageNameRe.MatchString(name) {
		out.Status, out.Error = "error", locale.Pick("Not a page name", "Это не имя страницы")
		return out
	}
	mu := l.lockOf("pagebuild\x00" + project)
	mu.Lock()
	defer mu.Unlock()
	projectDir := l.ProjectDirOf(project)
	boardPath := filepath.Join(projectDir, out.Board)
	if cur, err := os.ReadFile(boardPath); err == nil {
		src, _ := pageStamp(cur)
		if src != out.Source {
			out.Status = "error"
			out.Error = locale.Pick(
				"The board "+out.Board+" exists and is not built from "+out.Source+": it is not overwritten. Rename the page source or remove the board.",
				"Доска "+out.Board+" уже есть и собрана не из "+out.Source+": я её не перезаписываю. Переименуйте исходник страницы или удалите доску.")
			return out
		}
	}
	kitStamp := ""
	if k, err := l.pageKit(project); err == nil && k != nil {
		kitStamp = k.stamp
	}
	key := l.Root() + "\x00" + project + "\x00" + name
	c := &l.pageBuilds
	c.mu.Lock()
	e := c.entries[key]
	c.mu.Unlock()
	var board []byte
	var inputs []string
	if e != nil && l.pageInputsCurrent(e, projectDir, kitStamp) {
		board = e.board
		for _, in := range e.inputs {
			inputs = append(inputs, in.path)
		}
	} else {
		var err error
		board, inputs, err = l.buildPageBoard(project, name)
		if err != nil {
			out.Status, out.Error = "error", err.Error()
			return out
		}
		ent := &pageCacheEntry{kit: kitStamp, board: board}
		for _, in := range inputs {
			if st, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(in))); err == nil {
				ent.inputs = append(ent.inputs, pageInput{in, FileVersion(st)})
			}
		}
		c.mu.Lock()
		if c.entries == nil {
			c.entries = map[string]*pageCacheEntry{}
		}
		c.entries[key] = ent
		c.builds++
		c.mu.Unlock()
	}
	out.Inputs = inputs
	cur, rerr := os.ReadFile(boardPath)
	if rerr == nil && string(cur) == string(board) {
		out.Status = "current"
		return out
	}
	if err := writeFileAtomic(boardPath, board); err != nil {
		out.Status, out.Error = "error", err.Error()
		return out
	}
	out.Status = "built"
	if rerr != nil {
		l.registerBuiltPage(project, name)
	}
	return out
}

// registerBuiltPage lists a board built for the first time as a page of the project, as `design page create --file`
// does; a project without a page model keeps its boards as they are.
func (l *Library) registerBuiltPage(project, name string) {
	m := l.ReadModel(project)
	if m == nil {
		return
	}
	if _, ok := m.Page(name + ".dc.html"); ok {
		return
	}
	_, _ = l.CreatePage(project, PageSpec{Title: name, File: name + ".dc.html"})
}

// BuildPages builds the project's pages (one when only is set), in name order.
func (l *Library) BuildPages(project, only string) []PageBuild {
	names := l.PageSources(project)
	if only != "" {
		only = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(only, pagesDirname+"/"), "./"), pageSrcExt)
		names = []string{only}
	}
	out := make([]PageBuild, 0, len(names))
	for _, n := range names {
		out = append(out, l.BuildPage(project, n))
	}
	return out
}

// PageSourceProblems are what the check says about the project's page modules: a source whose board is missing or
// not the one the source builds today, or does not build at all. Nothing is written.
func (l *Library) PageSourceProblems(project string) []PageSourceProblem {
	projectDir := l.ProjectDirOf(project)
	var out []PageSourceProblem
	for _, name := range l.PageSources(project) {
		p := PageSourceProblem{Page: name, Source: pagesDirname + "/" + name + pageSrcExt}
		cli := l.cfg.Texts.CLI
		board, _, err := l.buildPageBoard(project, name)
		if err != nil {
			p.Message = locale.Pick("does not build:\n", "не собирается:\n") + err.Error()
			out = append(out, p)
			continue
		}
		cur, rerr := os.ReadFile(filepath.Join(projectDir, name+".dc.html"))
		switch {
		case rerr != nil:
			p.Message = locale.Pick("has no board "+name+".dc.html: run `"+cli+" design page build "+project+" "+name+"`",
				"нет доски "+name+".dc.html: выполните `"+cli+" design page build "+project+" "+name+"`")
		case string(cur) != string(board):
			if src, _ := pageStamp(cur); src == "" {
				p.Message = locale.Pick("the board "+name+".dc.html is not built from this source and is not overwritten",
					"доска "+name+".dc.html собрана не из этого исходника, я её не перезаписываю")
			} else {
				p.Message = locale.Pick("the board "+name+".dc.html is out of date: run `"+cli+" design page build "+project+" "+name+"`",
					"доска "+name+".dc.html устарела: выполните `"+cli+" design page build "+project+" "+name+"`")
			}
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

// pageSourceRel is whether rel (a library path) is a file a page module is built from, and the project it is in.
func pageSourceRel(rel string) (project string, ok bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 || parts[1] != ProjectDirname || !IsLibraryID(parts[0]) {
		return "", false
	}
	switch strings.ToLower(path.Ext(parts[len(parts)-1])) {
	case ".ts", ".tsx", ".js", ".jsx", ".json":
	default:
		if !(len(parts) == 4 && parts[2] == pagesDirname && strings.HasSuffix(parts[3], pageFrameExt)) {
			return "", false
		}
	}
	switch parts[2] {
	case "ds", "node_modules", ".git":
		return "", false
	}
	return parts[0], true
}

const pageRebuildSettle = 300 * time.Millisecond

// WatchPages rebuilds a project's page boards when a file under its project/ folder changes. The builds are kept by
// the files' versions, so a change that touches no input costs a stat of each. It starts once.
func (l *Library) WatchPages() {
	c := &l.pageBuilds
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stop != nil {
		return
	}
	stop, ok := l.Subscribe(func(rel string) {
		project, hit := pageSourceRel(rel)
		if !hit {
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.timers == nil {
			c.timers = map[string]*time.Timer{}
		}
		if t := c.timers[project]; t != nil {
			t.Stop()
		}
		c.timers[project] = time.AfterFunc(pageRebuildSettle, func() {
			c.mu.Lock()
			delete(c.timers, project)
			c.mu.Unlock()
			l.BuildPages(project, "")
		})
	})
	if ok {
		c.stop = stop
	} else {
		stop()
	}
}

// StopPages ends the watch (a test's cleanup).
func (l *Library) StopPages() {
	c := &l.pageBuilds
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
	for k, t := range c.timers {
		t.Stop()
		delete(c.timers, k)
	}
}
