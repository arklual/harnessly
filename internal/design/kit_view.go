package design

// A kit's boards shown live on the kit screen without a project: /kitv/<id>/<version>/<theme>/… serves a theme the way
// a project that took it sees it. ./support.js is the engine new projects get, ds/<folder>/… is the theme's own folder,
// <layout>.dc.html is a layout's board the theme ships (_harnessly/layouts/), and _play.dc.html draws one component
// with the props asked for (?c=<component>&p=<json>&s=<script>…), so the panel's parameters show live.

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

var (
	propNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,40}$`)
	compNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)
)

// withResources puts the runtime's React, ReactDOM and Babel map (vendor.go) before a kit page's engine, the way a
// project's board gets it: the kit screen then draws without unpkg.com, on a machine that cannot reach it too.
func withResources(source string) string {
	if m := headTag.FindString(source); m != "" {
		return strings.Replace(source, m, m+resourcesScript(), 1)
	}
	return resourcesScript() + source
}

// kitThemeFolder is the folder name a theme has on a board, ds/<name>/: its own folder's name, or the kit's id for a
// kit that is one theme at the root of its version (path.Base("") would give "." and ds/./ addresses no file).
func kitThemeFolder(folder, kitID string) string {
	if folder == "" {
		return kitID
	}
	return path.Base(folder)
}

// serveKitView answers /kitv/<id>/<version>/<theme>/<path>; segments are ["<id>", "<version>", "<theme>", path...].
func (l *Library) serveKitView(w http.ResponseWriter, r *http.Request, q url.Values, segments []string, head bool) {
	if len(segments) < 4 {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	dir := l.KitsDir()
	id, version, theme, rest := segments[0], segments[1], segments[2], segments[3:]
	if l.refuseKitDraft(w, id, version, head) {
		return
	}
	var folder string
	found := false
	for _, t := range designkit.Themes(dir, id, version) {
		if t.ID == theme {
			folder, found = t.Folder, true
		}
	}
	if !found {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	themeSegs := func(more ...string) []string {
		if folder == "" {
			return more
		}
		return append(strings.Split(folder, "/"), more...)
	}
	switch {
	case len(rest) == 1 && rest[0] == EngineFile:
		send(w, 200, "text/javascript; charset=utf-8", l.engineBytes(), head, "Cache-Control", "public, max-age=300")
		return
	case len(rest) == 1 && rest[0] == "_play.dc.html":
		c, err := designkit.ReadCanvas(dir, id, version, theme)
		if err != nil {
			sendHTML(w, 404, l.notFoundPage(), head)
			return
		}
		board, ok := playBoard(c, folder, dir, q)
		if !ok {
			sendHTML(w, 400, l.notFoundPage(), head)
			return
		}
		cache := kitImmutable
		if version == designkit.DraftVersion {
			cache = "no-cache"
		}
		send(w, 200, "text/html; charset=utf-8", []byte(withResources(board)), head, "Cache-Control", cache)
		return
	case len(rest) == 1 && strings.HasSuffix(rest[0], ".dc.html"):
		file := designkit.Resolve(dir, id, version, themeSegs("_harnessly", "layouts", rest[0]))
		src, err := readFileLimited(file, 16<<20)
		if file == "" || err != nil {
			sendHTML(w, 404, l.notFoundPage(), head)
			return
		}
		send(w, 200, "text/html; charset=utf-8", []byte(withResources(string(src))), head, "Cache-Control", kitImmutable)
		return
	case len(rest) >= 3 && rest[0] == "ds" && rest[1] == kitThemeFolder(folder, id):
		l.serveKitFile(w, r, designkit.Resolve(dir, id, version, themeSegs(rest[2:]...)), head)
		return
	}
	sendHTML(w, 404, l.notFoundPage(), head)
}

func (l *Library) serveKitFile(w http.ResponseWriter, r *http.Request, file string, head bool) {
	ext := strings.ToLower(extname(file))
	typ := ""
	if file != "" {
		typ = kitContentType(ext)
	}
	st, err := os.Stat(file)
	if typ == "" || file == "" || err != nil {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	l.serveStatic(w, r, file, st, typ, ext, l.kitCache(file), head)
}

// declaredProps are the prop names the kit declares for component comp: its own props and those its templates' layouts
// set. Only these go into the board's attributes: the props come from the address, and the board's engine reads an
// attribute as a prop under several spellings (kebab, sc-camel-), some of which load code (HRN-897).
func declaredProps(c *designkit.Canvas, comp string) map[string]bool {
	out := map[string]bool{}
	for _, g := range c.Groups {
		for _, k := range g.Components {
			if k.ID == comp || k.Name == comp {
				for _, p := range k.Props {
					out[p.Name] = true
				}
				for _, v := range k.Variants {
					for name := range v.Props {
						out[name] = true
					}
				}
			}
		}
	}
	for _, t := range c.Templates {
		if t.Component != comp {
			continue
		}
		for _, sec := range t.Sections {
			for _, l := range sec.Layouts {
				for name := range l.Props {
					out[name] = true
				}
			}
		}
	}
	return out
}

// propAllowed is a prop the board may write as an attribute: one the kit declares and designkit.PreviewProp allows
// (a plain name, never a handler or a loader whatever the kit says).
func propAllowed(k string, declared map[string]bool) bool {
	return declared[k] && designkit.PreviewProp(k)
}

// playBoard is a board that draws component c of the canvas with props p (JSON): plain values as attributes, the rest
// through the board's values. Scripts s (paths of the theme) load before it, the way a layout's board loads its own.
func playBoard(c *designkit.Canvas, folder, dir string, q url.Values) (string, bool) {
	comp := q.Get("c")
	if !compNameRe.MatchString(comp) || !compNameRe.MatchString(c.Namespace) {
		return "", false
	}
	props := map[string]any{}
	if raw := q.Get("p"); raw != "" {
		if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &props) != nil {
			return "", false
		}
	}
	ds := "ds/" + kitThemeFolder(folder, c.Kit) + "/"
	exists := func(rel string) bool {
		segs := strings.Split(rel, "/")
		if folder != "" {
			segs = append(strings.Split(folder, "/"), segs...)
		}
		return designkit.Resolve(dir, c.Kit, c.Version, segs) != ""
	}
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"ru\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<script src=\"./support.js\"></script>\n")
	// The theme's tokens first (components and the board's own markup read their variables), then its fonts and the
	// components' styles: the same files, in the same order, a board on the kit loads.
	for _, css := range []string{"tokens.css", "fonts.css", "components/bundle.css"} {
		if exists(css) {
			b.WriteString("<link rel=\"stylesheet\" href=\"" + html.EscapeString(ds+css) + "\">\n")
		}
	}
	if exists("components/bundle.js") {
		b.WriteString("<script src=\"" + html.EscapeString(ds+"components/bundle.js") + "\"></script>\n")
	}
	for _, s := range q["s"] {
		if rel := path.Clean(strings.TrimPrefix(s, "/")); rel != "." && !strings.HasPrefix(rel, "..") && strings.HasSuffix(rel, ".js") && exists(rel) {
			b.WriteString("<script src=\"" + html.EscapeString(ds+rel) + "\"></script>\n")
		}
	}
	b.WriteString("</head>\n<body>\n<x-dc>\n<helmet>\n<style>html,body{margin:0;background:transparent}.board{margin:0}.kit-subject{display:contents}</style>\n</helmet>\n<main class=\"board\">")
	// A kit whose components need its provider (kit.root: theme, tokens, portals) gets the component inside it, the way
	// every board on the kit wraps its content.
	root := c.Root != "" && c.Root != comp && compNameRe.MatchString(c.Root)
	if root {
		b.WriteString("<x-import component-from-global-scope=\"" + c.Namespace + "." + c.Root + "\">")
	}
	// The component sits in a marker of no box of its own: `kit render` measures what is drawn without the root's
	// wrappers around it (their background spans the page), portals included.
	b.WriteString("<div class=\"kit-subject\" data-kit-subject=\"\">")
	b.WriteString("<x-import component-from-global-scope=\"" + c.Namespace + "." + comp + "\"")
	vals := map[string]any{}
	declared := declaredProps(c, comp)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !propAllowed(k, declared) || !safePropName(k) {
			continue
		}
		switch v := c.CleanValue(props[k]).(type) {
		case string:
			b.WriteString(" " + k + "=\"" + html.EscapeString(v) + "\"")
		case nil:
		default:
			vals[k] = v
			b.WriteString(" " + k + "=\"{{" + k + "}}\"")
		}
	}
	b.WriteString("></x-import></div>")
	if root {
		b.WriteString("</x-import>")
	}
	b.WriteString("</main>\n</x-dc>\n")
	// json.Marshal escapes <, > and &: the values cannot close the script.
	data, _ := json.Marshal(vals)
	b.WriteString("<script type=\"text/x-dc\" data-dc-script data-props='{}'>\nclass Component extends DCLogic {\n  renderVals() {\n    const ns = window[" + strconv.Quote(c.Namespace) + "];\n" + elementReviver + "    return h(" + string(data) + ");\n  }\n}\n</script>\n</body>\n</html>\n")
	return b.String(), true
}

// elementReviver is the play page's h(v): designkit.ElementKey values become React elements of the namespace's
// components (CleanValue let through only the kit's own), each in a list keyed by its place so React stays quiet.
const elementReviver = `    const h = (v, key) => {
      if (Array.isArray(v)) return v.map((x, i) => h(x, i));
      if (!v || typeof v !== "object") return v;
      if (typeof v.$fn === "string") return v.$fn ? (x) => (x == null ? undefined : x[v.$fn]) : () => undefined;
      if (typeof v.$c !== "string") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, h(x)]));
      const C = v.$c.split(".").reduce((o, k) => (o ? o[k] : undefined), ns);
      if (!C) return null;
      const props = Object.assign({}, h(v.props || {}), key === undefined ? {} : { key });
      const kids = v.children === undefined ? [] : Array.isArray(v.children) ? v.children.map((x) => h(x)) : [h(v.children)];
      return React.createElement(C, props, ...kids);
    };
`

// safePropName keeps a prop that can only be data: never an event handler (on…), a style or focus attribute, or
// the component's own address — the query string must not be able to run script on the page.
func safePropName(k string) bool {
	if !propNameRe.MatchString(k) || k == "component-from-global-scope" {
		return false
	}
	l := strings.ToLower(k)
	return !strings.HasPrefix(l, "on") && l != "style" && l != "autofocus" && l != "tabindex" && l != "is" && l != "slot"
}
