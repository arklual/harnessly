package apps

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testWidgets(t *testing.T) *Widgets {
	t.Helper()
	at := time.UnixMilli(1_000)
	return &Widgets{Root: t.TempDir(), Now: func() time.Time { at = at.Add(time.Second); return at }}
}

func TestWidgetsShowAndChangeInPlace(t *testing.T) {
	w := testWidgets(t)
	a, err := w.Show("tab-1", WidgetInput{Kind: WidgetView, App: "acme.cash", View: "card", Data: json.RawMessage(`{"n":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Rev != 1 {
		t.Fatalf("new widget: %+v", a)
	}
	b, err := w.Show("tab-1", WidgetInput{ID: a.ID, Kind: WidgetView, App: "acme.cash", View: "card", Data: json.RawMessage(`{"n":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID || b.Rev != 2 || b.CreatedAt != a.CreatedAt || string(b.Data) != `{"n":2}` {
		t.Fatalf("changed widget: %+v", b)
	}
	list, err := w.List("tab-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %+v", err, list)
	}
	// Another chat has its own widgets.
	if other, _ := w.List("tab-2"); len(other) != 0 {
		t.Fatalf("other chat: %+v", other)
	}
	// The same id is not turned into a widget of another app.
	if _, err := w.Show("tab-1", WidgetInput{ID: a.ID, Kind: WidgetView, App: "acme.other", View: "card"}); !errors.Is(err, ErrWidgetKind) {
		t.Fatalf("another app's id: %v", err)
	}
}

func TestWidgetsLimits(t *testing.T) {
	w := testWidgets(t)
	bad := []WidgetInput{
		{Kind: WidgetView, App: "acme.cash", View: ""},
		{Kind: WidgetView, App: "nope", View: "card"},
		{ID: "Bad ID", Kind: WidgetView, App: "acme.cash", View: "card"},
		{Kind: WidgetView, App: "acme.cash", View: "card", Data: json.RawMessage(`{`)},
		{Kind: WidgetView, App: "acme.cash", View: "card", Data: json.RawMessage(`"` + strings.Repeat("x", MaxWidgetData) + `"`)},
		{Kind: WidgetHTML, App: WidgetsApp},
		{Kind: WidgetHTML, App: "acme.cash", HTML: "<p>x</p>"},
		{Kind: WidgetHTML, App: WidgetsApp, HTML: strings.Repeat("x", MaxWidgetHTML+1)},
		{Kind: "other", App: WidgetsApp, HTML: "<p>x</p>"},
	}
	for i, in := range bad {
		if _, err := w.Show("tab-1", in); !errors.Is(err, ErrWidgetArgs) {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, err := w.Show("../x", WidgetInput{Kind: WidgetHTML, App: WidgetsApp, HTML: "<p>x</p>"}); !errors.Is(err, ErrWidgetArgs) {
		t.Fatalf("a chat id that leaves the folder: %v", err)
	}
}

func TestWidgetHTMLFiles(t *testing.T) {
	w := testWidgets(t)
	x, err := w.Show("tab-1", WidgetInput{Kind: WidgetHTML, App: WidgetsApp, Title: "Продажи <b>", HTML: `<div id="c" class="chart"></div>`,
		Style: "#c{height:200px}", Script: "echarts.init(c)", Data: json.RawMessage(`[1,2,3]`)})
	if err != nil {
		t.Fatal(err)
	}
	page, typ, ok := w.WidgetFile("tab-1", x.ID, "index.html", "NONCE")
	if !ok || !strings.HasPrefix(typ, "text/html") {
		t.Fatal("no page")
	}
	p := string(page)
	for _, want := range []string{`<div id="c" class="chart"></div>`, SDKPath, WidgetBaseCSSPath, "widget.js?v=1", "data.js?v=1", "NONCE", "Продажи &lt;b&gt;"} {
		if !strings.Contains(p, want) {
			t.Fatalf("page has no %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "<script>") {
		t.Fatal("the page carries an inline script")
	}
	data, _, ok := w.WidgetFile("tab-1", x.ID, "data.js", "")
	if !ok || string(data) != "window.widgetData = [1,2,3];\n" {
		t.Fatalf("data.js: %q", data)
	}
	if js, _, ok := w.WidgetFile("tab-1", x.ID, "widget.js", ""); !ok || string(js) != "echarts.init(c)" {
		t.Fatalf("widget.js: %q", js)
	}
	if _, _, ok := w.WidgetFile("tab-1", x.ID, "../widgets.json", ""); ok {
		t.Fatal("served a file that is not the widget's")
	}
	if _, _, ok := w.WidgetFile("tab-2", x.ID, "index.html", ""); ok {
		t.Fatal("served another chat's widget")
	}
	if err := w.Remove("tab-1", x.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := w.WidgetFile("tab-1", x.ID, "index.html", ""); ok {
		t.Fatal("served a removed widget")
	}
}

func TestWidgetCSPHasNoNetwork(t *testing.T) {
	csp := WidgetCSP()
	for _, want := range []string{"connect-src 'none'", "frame-src 'none'", "script-src 'self'", "sandbox allow-scripts"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("csp has no %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") || strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("csp: %s", csp)
	}
}

func TestWidgetsAppIDIsReserved(t *testing.T) {
	m := &Manifest{ID: WidgetsApp, Name: "x", Description: "x", Version: "1.0.0"}
	for _, p := range m.Check() {
		if p.Path == "id" && !p.Warning {
			return
		}
	}
	t.Fatal("a package may take the widgets' id")
}
