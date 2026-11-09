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
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

var (
	propNameRe  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,40}$`)
	plainPropRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,40}$`)
	compNameRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)
)

// serveKitView answers /kitv/<id>/<version>/<theme>/<path>; segments are ["<id>", "<version>", "<theme>", path...].
func (l *Library) serveKitView(w http.ResponseWriter, r *http.Request, q url.Values, segments []string, head bool) {
	if len(segments) < 4 {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	dir := l.KitsDir()
	id, version, theme, rest := segments[0], segments[1], segments[2], segments[3:]
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
		send(w, 200, "text/html; charset=utf-8", []byte(board), head, "Cache-Control", kitImmutable)
		return
	case len(rest) == 1 && strings.HasSuffix(rest[0], ".dc.html"):
		l.serveKitFile(w, r, designkit.Resolve(dir, id, version, themeSegs("_harnessly", "layouts", rest[0])), head)
		return
	case len(rest) >= 3 && rest[0] == "ds" && rest[1] == path.Base(folder):
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
	l.serveStatic(w, r, file, st, typ, ext, kitImmutable, head)
}

// declaredProps are the prop names the kit declares for component comp: its own props and those its templates' layouts
// set. Only these go into the board's attributes: the props come from the address, and the board's engine reads an
// attribute as a prop under several spellings (kebab, sc-camel-), some of which load code.
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

// propAllowed is a prop the board may write as an attribute: one the kit declares, of a plain name (letters and digits,
// no dash the engine would turn into another name), never a handler or a loader whatever the kit says.
func propAllowed(k string, declared map[string]bool) bool {
	if !declared[k] || !plainPropRe.MatchString(k) {
		return false
	}
	l := strings.ToLower(k)
	if strings.HasPrefix(l, "on") || strings.HasPrefix(l, "style") || strings.HasPrefix(l, "sc") {
		return false
	}
	switch l {
	case "from", "ref", "key", "class", "classname", "is", "slot", "src", "srcdoc", "href", "action", "formaction",
		"autofocus", "tabindex", "dangerouslysetinnerhtml":
		return false
	}
	return true
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
	ds := "ds/" + path.Base(folder) + "/"
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
	for _, css := range []string{"fonts.css", "components/bundle.css"} {
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
	b.WriteString("</head>\n<body>\n<x-dc>\n<helmet>\n<style>html,body{margin:0;background:transparent}.board{margin:0}</style>\n</helmet>\n<main class=\"board\">")
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
		switch v := props[k].(type) {
		case string:
			b.WriteString(" " + k + "=\"" + html.EscapeString(v) + "\"")
		case nil:
		default:
			vals[k] = v
			b.WriteString(" " + k + "=\"{{" + k + "}}\"")
		}
	}
	b.WriteString("></x-import></main>\n</x-dc>\n")
	// json.Marshal escapes <, > and &: the values cannot close the script.
	data, _ := json.Marshal(vals)
	b.WriteString("<script type=\"text/x-dc\" data-dc-script data-props='{}'>\nclass Component extends DCLogic {\n  renderVals() { return " + string(data) + "; }\n}\n</script>\n</body>\n</html>\n")
	return b.String(), true
}

// safePropName keeps a prop that can only be data: never an event handler (on…), a style or focus attribute, or
// the component's own address — the query string must not be able to run script on the page.
func safePropName(k string) bool {
	if !propNameRe.MatchString(k) || k == "component-from-global-scope" {
		return false
	}
	l := strings.ToLower(k)
	return !strings.HasPrefix(l, "on") && l != "style" && l != "autofocus" && l != "tabindex" && l != "is" && l != "slot"
}
