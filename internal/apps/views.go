package apps

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/journal"
)

// An installed app's views are served by a loopback server of the runtime that the right pane's preview opens like any
// dev server: through a slot, so a view runs on an origin of its own (never the page's) and the preview's DevTools see
// it. The server answers /<app id>/<path inside the package> and nothing else of the machine.

//go:embed sdk/app-sdk.js
var sdkJS []byte

//go:embed sdk/app-sdk.d.ts
var sdkDTS []byte

//go:embed sdk/bridge-shim.js
var shimJS []byte

//go:embed sdk/widget.css
var widgetCSS []byte

//go:embed sdk/hl-ui.css
var hlUICSS []byte

// echartsJS is Apache ECharts (Apache-2.0), the charts library the host gives every view as a classic script
// (window.echarts), so an agent's widget draws a chart without the network.
//
//go:embed sdk/lib/echarts.min.js
var echartsJS []byte

// SDKPath is where every view finds @harnessly/app-sdk as a classic script (window.harnessly); SDKModulePath is the
// same code as an ES module. A view runs on an opaque origin (see ViewCSP), where a module script cannot carry the
// slot's cookie, so the classic one is what a view loads.
const (
	SDKPath       = "/__hl/app-sdk.js"
	SDKModulePath = "/__hl/app-sdk.mjs"
	// ShimPath is the script the server puts first in every page it serves: it makes the handshake with the page's nonce
	// and gives the view a window.parent that speaks on the MessageChannel port the host hands over.
	ShimPath = "/__hl/bridge-shim.js"
	// WidgetBaseCSSPath is the host's base style of an agent's widget; any view may load it.
	WidgetBaseCSSPath = "/__hl/widget.css"
	// UICSSPath is hl-ui.css, the host's primitives (.hl-btn, .hl-card, .hl-stat, …) on the platform's tokens; any view
	// may load it (HRN-695).
	UICSSPath = "/__hl/hl-ui.css"
	// EChartsPath is the charts library every view may load (window.echarts).
	EChartsPath = "/__hl/lib/echarts.js"
)

// classicSDK is the SDK as a classic script: the module without its exports, publishing window.harnessly.
func classicSDK() []byte {
	src := strings.NewReplacer("export async function", "async function", "export class", "class").Replace(string(sdkJS))
	return []byte("(function () {\n" + src + "\nglobalThis.harnessly = { connect: connect, HarnesslyError: HarnesslyError };\n})();\n")
}

// maxViewBytes is the most one file of a view may weigh (a view is at most 10 MB).
const maxViewBytes = 10 << 20

// ViewFile resolves a request path of a view (the part after the app id) to a file of the package. Only a view's own
// files are served: its entry, and the files under ui/ or under the entry's folder. Anything else of the package
// (the manifest, instructions, .mcp.json with its headers) is not for a view to read.
func (p *Package) ViewFile(rel string) (string, error) {
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.ContainsRune(rel, 0) || strings.Contains(rel, "\\") {
		return "", fs.ErrNotExist
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == ".." || strings.HasPrefix(seg, ".") {
			return "", fs.ErrNotExist
		}
	}
	if path.Clean(rel) != rel || !p.viewMayRead(rel) {
		return "", fs.ErrNotExist
	}
	full, err := p.inside(rel)
	if err != nil {
		// A link that leads out is the same as no file: the answer does not say what is beyond.
		return "", fs.ErrNotExist
	}
	return full, nil
}

func (p *Package) viewMayRead(rel string) bool {
	if strings.HasPrefix(rel, "ui/") {
		return true
	}
	for _, v := range p.Manifest.Views {
		if v.Entry == "" || strings.HasPrefix(v.Entry, "https://") {
			continue
		}
		entry := path.Clean(v.Entry)
		if rel == entry {
			return true
		}
		if dir := path.Dir(entry); dir != "." && strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// ViewCSP is the Content-Security-Policy of an app's views, built from its network: nothing leaves the view unless the
// manifest names the address. Scripts are files of the view (no inline ones, no eval).
func (m *Manifest) ViewCSP() string {
	var connect, resources, frames []string
	if n := m.Network; n != nil {
		connect, resources, frames = cspSources(n.Connect), cspSources(n.Resources), cspSources(n.Frames)
	}
	list := func(base string, extra []string) string {
		if len(extra) == 0 {
			return base
		}
		return base + " " + strings.Join(extra, " ")
	}
	none := func(extra []string) string {
		if len(extra) == 0 {
			return "'none'"
		}
		return strings.Join(extra, " ")
	}
	return strings.Join([]string{
		// No allow-same-origin: whatever the frame around it says, the view's origin is opaque, so it cannot read
		// another app's view, the slot's storage or its cookies. Its state goes through the bridge.
		"sandbox allow-scripts allow-forms allow-popups",
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		list("img-src 'self' data: blob:", resources),
		list("font-src 'self' data:", resources),
		list("media-src 'self' data: blob:", resources),
		"connect-src " + none(connect),
		"frame-src " + none(frames),
		"base-uri 'none'",
		"form-action 'self'",
	}, "; ")
}

// cspSources keeps the entries that are one source expression; an entry with a space, a ';' or a ',' could add a
// directive of its own.
func cspSources(list []string) []string {
	var out []string
	for _, s := range list {
		if s == "" || strings.ContainsAny(s, " ;,'\"\t\r\n") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ViewServer serves the views of the installed apps on a loopback port, started on first use. Every request but the
// SDK's carries the token of a view session (/<token>/<app>/<file>) and the session's app must be the path's.
type ViewServer struct {
	reg *Registry
	// Widgets are the agents' own widgets (WidgetsApp), served under a session for their chat; nil serves none.
	Widgets *Widgets
	// Sessions are the mounted views; the page issues one per view it shows.
	Sessions *ViewSessions

	mu   sync.Mutex
	ln   net.Listener
	port int
	srv  *http.Server
}

// NewViewServer is the view server of a registry.
func NewViewServer(reg *Registry) *ViewServer {
	return &ViewServer{reg: reg, Sessions: NewViewSessions()}
}

// hostOK refuses a request whose Host is not this server's own loopback address: a page of another name that DNS points
// at 127.0.0.1 (rebinding) must not read a view.
func (s *ViewServer) hostOK(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	switch name {
	case "127.0.0.1", "localhost", "::1":
	default:
		return false
	}
	want := s.Port()
	// The handler used on its own (a test) has no port yet.
	return want == 0 || port == strconv.Itoa(want)
}

// EnsureServer starts the server on first use and returns its port.
func (s *ViewServer) EnsureServer() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.port, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	s.ln, s.port = ln, ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	s.srv = srv
	// The goroutine holds its own server: Close may clear s.srv before it starts.
	go func() { _ = srv.Serve(ln) }()
	return s.port, nil
}

// Port is the port once the server is up, 0 before.
func (s *ViewServer) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Close stops the server.
func (s *ViewServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		_ = s.srv.Close()
	}
	s.srv, s.ln, s.port = nil, nil, 0
}

// Handler answers the views' requests.
func (s *ViewServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A file the server refuses is also a line of the app's log (HRN-941), so `harnessly app logs` names it.
		refuse := func(v viewRefusal) {
			if v.App != "" {
				// The journal's line names the session's chat and trace, so it reads with the page's (HRN-976).
				token, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
				sess, _ := s.Sessions.Peek(token)
				s.reg.LogEventOnceJ(v.App, v.View, ViewLogServe, "GET "+clip(v.Path, 200)+" — "+strconv.Itoa(v.Status)+" "+v.Code,
					journal.Entry{Level: journal.Error, Event: "view.file", Method: r.Method, URL: "/" + v.Path, Status: v.Status,
						Text: v.Code + ": " + v.Reason, Chat: sess.Conversation, Trace: sess.Trace})
			}
			refuseView(w, r, v)
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.hostOK(r.Host) {
			refuse(viewRefusal{Status: http.StatusForbidden, Code: "host", Reason: "Адрес не принадлежит серверу видов."})
			return
		}
		switch r.URL.Path {
		case SDKPath:
			serveBytes(w, r, "text/javascript; charset=utf-8", classicSDK())
			return
		case SDKModulePath:
			serveBytes(w, r, "text/javascript; charset=utf-8", sdkJS)
			return
		case ShimPath:
			serveBytes(w, r, "text/javascript; charset=utf-8", shimJS)
			return
		case "/__hl/app-sdk.d.ts":
			serveBytes(w, r, "text/plain; charset=utf-8", sdkDTS)
			return
		case WidgetBaseCSSPath:
			serveBytes(w, r, "text/css; charset=utf-8", widgetCSS)
			return
		case UICSSPath:
			serveBytes(w, r, "text/css; charset=utf-8", hlUICSS)
			return
		case EChartsPath:
			serveBytes(w, r, "text/javascript; charset=utf-8", echartsJS)
			return
		}
		token, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		sess, live := s.Sessions.Get(token)
		// The SDK's files under the session's own path too: a view's page reaches them by a relative path (relativeSDK),
		// so a gateway that carries the slot's session in the address (an opaque frame sends no cookie) keeps it on them.
		if name, isSDK := strings.CutPrefix(rest, "__hl/"); isSDK && live {
			if body, typ, found := sdkFile(name); found {
				journalServed(sess, "view.sdk", "/__hl/"+name)
				serveBytes(w, r, typ, body)
				return
			}
			refuse(viewRefusal{Status: http.StatusNotFound, Code: "sdk-file", App: sess.App, View: sess.View, Path: "__hl/" + name,
				Reason: "Нет такого файла SDK: " + name + "."})
			return
		}
		id, rel, ok := strings.Cut(rest, "/")
		if !live {
			// The token is the session's secret: it is never written to the log, only that there was none.
			refuse(viewRefusal{Status: http.StatusNotFound, Code: "no-session", Path: rest,
				Reason: "Сессия вида закончилась или не выдавалась: откройте вид заново."})
			return
		}
		if !ok || id != sess.App || !idRe.MatchString(id) {
			refuse(viewRefusal{Status: http.StatusNotFound, Code: "other-app", App: sess.App, View: sess.View, Path: rest,
				Reason: "Эта сессия не открывает файлы другого приложения."})
			return
		}
		missing := func(why string) {
			refuse(viewRefusal{Status: http.StatusNotFound, Code: "no-file", App: id, View: sess.View, Path: rel,
				Reason: "Не загрузился файл " + rel + ": " + why})
		}
		if id == WidgetsApp {
			s.serveWidget(w, r, sess, rel)
			return
		}
		app, err := s.reg.Get(id)
		if err != nil || app.Manifest == nil {
			refuse(viewRefusal{Status: http.StatusNotFound, Code: "no-app", App: id, View: sess.View, Path: rel,
				Reason: "Приложение не установлено или выключено."})
			return
		}
		pkg := &Package{Dir: app.Dir, Manifest: app.Manifest}
		full, err := pkg.ViewFile(rel)
		if err != nil {
			missing("такого файла нет среди файлов видов приложения.")
			return
		}
		f, err := os.Open(full)
		if err != nil {
			missing("файл не открывается.")
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() || info.Size() > maxViewBytes {
			missing("это не файл или он слишком большой.")
			return
		}
		size := info.Size()
		var page []byte
		ct := contentType(full)
		if isHTML, isJS := strings.HasPrefix(ct, "text/html"), strings.HasPrefix(ct, "text/javascript"); isHTML || isJS {
			raw, err := io.ReadAll(io.LimitReader(f, maxViewBytes+1))
			if err != nil || len(raw) > maxViewBytes {
				missing("файл не читается или слишком большой.")
				return
			}
			// The SDK by a path relative to the file, and a page with the session's nonce for the bridge to check.
			page = relativeSDK(raw, rel, isJS)
			if isHTML {
				page = injectNonceShim(page, sess.Nonce, strings.Repeat("../", strings.Count(rel, "/")+1)+"__hl/bridge-shim.js")
				journalServed(sess, "view.page", "/"+rel)
			}
			size = int64(len(page))
		}
		h := w.Header()
		// Not for the page itself: its nonce is the bridge's check.
		if !strings.HasPrefix(ct, "text/html") {
			openCORS(h)
		}
		h.Set("Content-Type", contentType(full))
		h.Set("Content-Security-Policy", app.Manifest.ViewCSP())
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// An author edits a dev install under the view: every load asks again.
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Length", strconv.FormatInt(size, 10))
		if r.Method == http.MethodHead {
			return
		}
		if page != nil {
			_, _ = w.Write(page)
			return
		}
		_, _ = io.Copy(w, f)
	})
}

// serveWidget answers a file of an agent's own widget: the session names the chat and the widget (its view), and only
// that widget's files are served, with the CSP of a view that has no network.
func (s *ViewServer) serveWidget(w http.ResponseWriter, r *http.Request, sess Session, rel string) {
	widget, name, ok := strings.Cut(rel, "/")
	if s.Widgets == nil || !ok || widget != sess.View || sess.Conversation == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	body, typ, ok := s.Widgets.WidgetFile(sess.Conversation, widget, name, sess.Nonce)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Security-Policy", WidgetCSP())
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// NonceMeta is the name of the meta tag that carries a view session's nonce in the view's pages; the SDK reads it.
const NonceMeta = "hl-nonce"

var (
	headOpen = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
	htmlOpen = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)
	doctype  = regexp.MustCompile(`(?i)^\s*<!doctype[^>]*>`)
)

// injectNonce writes <meta name="hl-nonce"> and, right after it, the bridge shim's script tag at the opening <head> of a
// page, so the shim runs before any script of the view (the shim is a file, so the CSP stays script-src 'self'). A page
// with no <head> gets one after <html>, or after the doctype, or at its start.
func injectNonce(page []byte, nonce string) []byte { return injectNonceShim(page, nonce, ShimPath) }

// injectNonceShim is injectNonce with the shim's script at shim (a guest's page loads it by a relative path).
func injectNonceShim(page []byte, nonce, shim string) []byte {
	meta := `<meta name="` + NonceMeta + `" content="` + nonce + `"><script src="` + shim + `"></script>`
	if loc := headOpen.FindIndex(page); loc != nil {
		return splice(page, loc[1], meta)
	}
	if loc := htmlOpen.FindIndex(page); loc != nil {
		return splice(page, loc[1], "<head>"+meta+"</head>")
	}
	if loc := doctype.FindIndex(page); loc != nil {
		return splice(page, loc[1], meta)
	}
	return splice(page, 0, meta)
}

func splice(page []byte, at int, insert string) []byte {
	out := make([]byte, 0, len(page)+len(insert))
	out = append(out, page[:at]...)
	out = append(out, insert...)
	return append(out, page[at:]...)
}

// sdkFile is one of the SDK's own files by its name under /__hl/.
func sdkFile(name string) ([]byte, string, bool) {
	const js, css = "text/javascript; charset=utf-8", "text/css; charset=utf-8"
	switch "/__hl/" + name {
	case SDKPath:
		return classicSDK(), js, true
	case SDKModulePath:
		return sdkJS, js, true
	case ShimPath:
		return shimJS, js, true
	case WidgetBaseCSSPath:
		return widgetCSS, css, true
	case UICSSPath:
		return hlUICSS, css, true
	case EChartsPath:
		return echartsJS, js, true
	}
	return nil, "", false
}

// openCORS lets the opaque origin of a view's frame read what it loads in CORS mode: module scripts (a bundler's
// output), fetch of its own files, fonts. No credentials: what guards a file is the view session's token in its path.
func openCORS(h http.Header) {
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
}

func serveBytes(w http.ResponseWriter, r *http.Request, typ string, body []byte) {
	h := w.Header()
	openCORS(h)
	h.Set("Content-Type", typ)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

var viewTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json", ".svg": "image/svg+xml", ".wasm": "application/wasm",
	".woff2": "font/woff2", ".woff": "font/woff", ".map": "application/json", ".txt": "text/plain; charset=utf-8",
}

func contentType(file string) string {
	ext := strings.ToLower(path.Ext(file))
	if t, ok := viewTypes[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	// Unknown bytes are never taken for a page.
	return "application/octet-stream"
}

// ErrViewNotFound: the app or the view is not there, or the view is not for this target.
var ErrViewNotFound = errors.New("apps: no such view")

// ViewPath is the address of a view's entry on the view server without its session: /<app id>/<entry>. An external
// entry has none. The page puts the session's token in front.
// RemoteView is the address of a view the app's own server hosts (delivery mode remote, HRN-808): an https entry whose
// host network.frames lists. A package view, or an address the manifest does not let be framed, is not one.
func (m *Manifest) RemoteView(viewID string) (string, bool) {
	v := m.view(viewID)
	if v == nil || !strings.HasPrefix(v.Entry, "https://") {
		return "", false
	}
	return m.FrameURL(v.Entry)
}

func (m *Manifest) ViewPath(viewID string) (string, error) {
	for _, v := range m.Views {
		if v.ID != viewID {
			continue
		}
		if v.Entry == "" || strings.HasPrefix(v.Entry, "https://") {
			return "", fmt.Errorf("%w: view %s is at an address of its own", ErrViewNotFound, viewID)
		}
		return "/" + m.ID + "/" + path.Clean(v.Entry), nil
	}
	return "", fmt.Errorf("%w: %s", ErrViewNotFound, viewID)
}

// SDKFiles are the files a view the app's own server hosts serves itself (delivery mode remote): the shim first in its
// page, then the SDK, by name.
func SDKFiles() map[string][]byte {
	return map[string][]byte{
		"bridge-shim.js": shimJS, "app-sdk.js": sdkJS, "app-sdk.d.ts": sdkDTS, "hl-ui.css": hlUICSS,
	}
}

// journalServed writes down a view's page or an SDK file the view server gave (HRN-976): with them the journal tells a
// view whose page came but whose SDK never reached the machine (stopped on the way) from one whose SDK did not start.
func journalServed(sess Session, event, path string) {
	journal.Add(journal.Entry{Level: journal.Info, Source: JournalSource(sess.App), Event: event, URL: path, Status: http.StatusOK,
		App: sess.App, View: sess.View, Chat: sess.Conversation, Trace: sess.Trace})
}
