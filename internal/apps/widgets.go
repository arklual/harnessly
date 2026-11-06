package apps

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Widgets are what an agent shows in the feed of its chat: a view of an app with the chat target and the data to draw it
// with (kind "view"), or a page the agent wrote for the moment (kind "html": its words, style and script, drawn in the
// same sandbox as any view, with no network). They are kept on the machine beside the chat, under
// <data>/apps/widgets/<conversation>/, so a widget is there after a reload and in another browser, and the same id shown
// again changes the widget where it stands instead of adding one.

// WidgetsApp is the app id under which the agent's own widgets (kind "html") are served and bridged. No package has it:
// an app id names its publisher first, and harnessly is the host's.
const WidgetsApp = "harnessly.widgets"

// The kinds of widget.
const (
	WidgetView = "view"
	WidgetHTML = "html"
)

const (
	MaxWidgetData    = 64 << 10
	MaxWidgetHTML    = 128 << 10
	MaxWidgetScript  = 256 << 10
	MaxWidgetStyle   = 64 << 10
	MaxWidgetTitle   = 120
	MaxWidgetsInChat = 200
)

var (
	// ErrWidgetArgs: an id, a title, the data or a file past its limit, or a view that is not one.
	ErrWidgetArgs = errors.New("apps: bad widget")
	// ErrWidgetsFull: the chat has as many widgets as it may keep.
	ErrWidgetsFull = errors.New("apps: the chat has too many widgets")
	// ErrWidgetGone: no such widget in the chat.
	ErrWidgetGone = errors.New("apps: no such widget")
	// ErrWidgetKind: the id is a widget of another app or kind; it is not changed into one.
	ErrWidgetKind = errors.New("apps: the id is another widget's")

	widgetIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
)

// Widget is a widget as the page lists it.
type Widget struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	App   string `json:"app"`
	View  string `json:"view,omitempty"`
	Title string `json:"title,omitempty"`
	// Data is what the view reads as hl.context.data; null when none was given.
	Data      json.RawMessage `json:"data"`
	Rev       int             `json:"rev"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

// WidgetInput is a widget to show or change. An empty ID makes a new one.
type WidgetInput struct {
	ID    string
	Kind  string
	App   string
	View  string
	Title string
	Data  json.RawMessage
	// The files of a kind "html" widget: the body's markup, its stylesheet and its classic script.
	HTML, Style, Script string
}

// Widgets is the widgets of the machine's chats.
type Widgets struct {
	Root string
	Now  func() time.Time

	mu sync.Mutex
}

var (
	widgetsMu sync.Mutex
	widgets   = map[string]*Widgets{}
)

// OpenWidgets is the process's one Widgets of a data folder.
func OpenWidgets(dataDir string) *Widgets {
	root := filepath.Join(dataDir, "apps", "widgets")
	widgetsMu.Lock()
	defer widgetsMu.Unlock()
	if w := widgets[root]; w != nil {
		return w
	}
	w := &Widgets{Root: root}
	widgets[root] = w
	return w
}

func (w *Widgets) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Widgets) dir(conv string) (string, error) {
	if !ValidOwner(conv) {
		return "", ErrWidgetArgs
	}
	return filepath.Join(w.Root, conv), nil
}

func (w *Widgets) read(conv string) ([]Widget, error) {
	dir, err := w.dir(conv)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "widgets.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []Widget{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Widget
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (w *Widgets) write(conv string, list []Widget) error {
	dir, err := w.dir(conv)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "widgets.json"), raw)
}

// List is the widgets of a chat, oldest first.
func (w *Widgets) List(conv string) ([]Widget, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	list, err := w.read(conv)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].CreatedAt < list[j].CreatedAt })
	return list, nil
}

// Get is one widget of a chat.
func (w *Widgets) Get(conv, id string) (Widget, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	list, err := w.read(conv)
	if err != nil {
		return Widget{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return Widget{}, ErrWidgetGone
}

func newWidgetID() string {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	return "w" + hex.EncodeToString(raw[:])
}

func (in WidgetInput) check() error {
	if in.ID != "" && !widgetIDRe.MatchString(in.ID) {
		return ErrWidgetArgs
	}
	if len(in.Title) > MaxWidgetTitle || strings.ContainsAny(in.Title, "\n\r") {
		return ErrWidgetArgs
	}
	if len(in.Data) > MaxWidgetData || (len(in.Data) > 0 && !json.Valid(in.Data)) {
		return ErrWidgetArgs
	}
	switch in.Kind {
	case WidgetView:
		if !idRe.MatchString(in.App) || in.View == "" || in.HTML != "" || in.Style != "" || in.Script != "" {
			return ErrWidgetArgs
		}
	case WidgetHTML:
		if in.App != WidgetsApp || in.View != "" || in.HTML == "" || len(in.HTML) > MaxWidgetHTML ||
			len(in.Style) > MaxWidgetStyle || len(in.Script) > MaxWidgetScript {
			return ErrWidgetArgs
		}
	default:
		return ErrWidgetArgs
	}
	return nil
}

// Show puts a widget into the chat, or changes the one with the same id where it stands: its data, title and (for
// kind "html") files are replaced and its revision goes up. A widget keeps its app, view and kind; the same id given to
// another is refused.
func (w *Widgets) Show(conv string, in WidgetInput) (Widget, error) {
	if err := in.check(); err != nil {
		return Widget{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	list, err := w.read(conv)
	if err != nil {
		return Widget{}, err
	}
	now := w.now().UnixMilli()
	at := -1
	if in.ID != "" {
		for i, x := range list {
			if x.ID == in.ID {
				at = i
			}
		}
	}
	var out Widget
	if at >= 0 {
		out = list[at]
		if out.Kind != in.Kind || out.App != in.App || out.View != in.View {
			return Widget{}, ErrWidgetKind
		}
		out.Rev++
		out.UpdatedAt = now
	} else {
		if len(list) >= MaxWidgetsInChat {
			return Widget{}, ErrWidgetsFull
		}
		id := in.ID
		if id == "" {
			id = newWidgetID()
		}
		out = Widget{ID: id, Kind: in.Kind, App: in.App, View: in.View, Rev: 1, CreatedAt: now, UpdatedAt: now}
	}
	if in.Title != "" {
		out.Title = in.Title
	}
	out.Data = json.RawMessage("null")
	if len(in.Data) > 0 {
		out.Data = append(json.RawMessage(nil), in.Data...)
	}
	if in.Kind == WidgetHTML {
		if err := w.writeFiles(conv, out.ID, in); err != nil {
			return Widget{}, err
		}
	}
	if at >= 0 {
		list[at] = out
	} else {
		list = append(list, out)
	}
	if err := w.write(conv, list); err != nil {
		return Widget{}, err
	}
	return out, nil
}

// The files of a kind "html" widget, under its id.
const (
	widgetBody   = "body.html"
	widgetStyle  = "style.css"
	widgetScript = "widget.js"
)

func (w *Widgets) writeFiles(conv, id string, in WidgetInput) error {
	dir, err := w.dir(conv)
	if err != nil {
		return err
	}
	at := filepath.Join(dir, id)
	for name, text := range map[string]string{widgetBody: in.HTML, widgetStyle: in.Style, widgetScript: in.Script} {
		if err := writeFileAtomic(filepath.Join(at, name), []byte(text)); err != nil {
			return err
		}
	}
	return nil
}

// Remove takes a widget out of the chat.
func (w *Widgets) Remove(conv, id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	list, err := w.read(conv)
	if err != nil {
		return err
	}
	out := list[:0]
	found := false
	for _, x := range list {
		if x.ID == id {
			found = true
			continue
		}
		out = append(out, x)
	}
	if !found {
		return ErrWidgetGone
	}
	if dir, err := w.dir(conv); err == nil && widgetIDRe.MatchString(id) {
		_ = os.RemoveAll(filepath.Join(dir, id))
	}
	return w.write(conv, out)
}

// WidgetFile is a file the view server serves for a kind "html" widget: the page it builds around the widget's markup,
// its stylesheet, its script and its data as a script that sets window.widgetData. ok is false for any other name.
func (w *Widgets) WidgetFile(conv, id, name, nonce string) (body []byte, contentType string, ok bool) {
	x, err := w.Get(conv, id)
	if err != nil || x.Kind != WidgetHTML {
		return nil, "", false
	}
	dir, err := w.dir(conv)
	if err != nil {
		return nil, "", false
	}
	read := func(file string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, id, file))
		if err != nil {
			return nil
		}
		return raw
	}
	switch name {
	case "index.html":
		page := widgetPage(x.Title, read(widgetBody), x.Rev)
		return injectNonce(page, nonce), "text/html; charset=utf-8", true
	case widgetStyle:
		return read(widgetStyle), "text/css; charset=utf-8", true
	case widgetScript:
		return read(widgetScript), "text/javascript; charset=utf-8", true
	case "data.js":
		data := x.Data
		if len(data) == 0 {
			data = json.RawMessage("null")
		}
		return append(append([]byte("window.widgetData = "), data...), ";\n"...), "text/javascript; charset=utf-8", true
	}
	return nil, "", false
}

// widgetPage is the page of a kind "html" widget: the SDK, the host's base style and the libraries come from the host
// (/__hl/), the rest is the widget's. Its scripts are classic files: the view's CSP takes no inline script.
func widgetPage(title string, body []byte, rev int) []byte {
	v := "?v=" + itoa(rev)
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	b.WriteString("<title>" + htmlEscape(title) + "</title>")
	b.WriteString("<link rel=\"stylesheet\" href=\"" + WidgetBaseCSSPath + "\">")
	b.WriteString("<link rel=\"stylesheet\" href=\"" + widgetStyle + v + "\">")
	b.WriteString("<script src=\"" + SDKPath + "\"></script>")
	b.WriteString("<script src=\"data.js" + v + "\"></script>")
	b.WriteString("</head><body>\n")
	b.Write(body)
	b.WriteString("\n<script src=\"" + widgetScript + v + "\"></script>\n</body></html>\n")
	return []byte(b.String())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// WidgetCSP is the Content-Security-Policy of an agent's own widget: the view's sandbox with no network at all, and
// no frames or forms that leave it.
func WidgetCSP() string { return (&Manifest{}).ViewCSP() }
