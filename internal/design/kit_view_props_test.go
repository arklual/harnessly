package design

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// A link to the kit's view carries the props in its address: none of them may become a script's way in.
func TestPlayBoardWritesNoHandlerFromTheAddress(t *testing.T) {
	c := &designkit.Canvas{Namespace: "Kit", Groups: []designkit.ComponentGroup{{ID: "g", Components: []designkit.Component{{
		ID: "Button", Name: "Button",
		Props: []designkit.Prop{{Name: "label"}, {Name: "size"}, {Name: "onClick"}, {Name: "from"}, {Name: "sc-camel-onclick"}},
	}}}}}
	q := url.Values{}
	q.Set("c", "Button")
	q.Set("p", `{"label":"ok","onfocus":"alert(1)","OnClick":"alert(2)","autofocus":"","tabindex":"0","style":"x","href":"javascript:alert(3)","size":2,"from":"https://evil/x.js","sc-camel-onclick":"alert(4)","undeclared":"x"}`)
	page, ok := playBoard(c, "", t.TempDir(), q)
	if !ok {
		t.Fatal("no board")
	}
	low := strings.ToLower(page)
	for _, bad := range []string{"onfocus", "onclick", "autofocus", "tabindex", ` style="x"`, "javascript:", "evil", "sc-camel", "undeclared"} {
		if strings.Contains(low, bad) {
			t.Fatalf("the board writes %q:\n%s", bad, page)
		}
	}
	if !strings.Contains(page, ` label="ok"`) || !strings.Contains(page, ` size="{{size}}"`) {
		t.Fatalf("the board lost its props:\n%s", page)
	}
}

// The live preview loads what a board on the kit loads (tokens first) and wraps the component in the kit's root.
func TestPlayBoardLoadsTokensAndWrapsInTheRoot(t *testing.T) {
	dir := t.TempDir()
	src := "../designkit/testdata/check/ok"
	ver := filepath.Join(dir, "acme-ui", "1.0.0")
	if err := os.MkdirAll(filepath.Dir(ver), 0o755); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(src)
	if err := os.Symlink(abs, ver); err != nil {
		t.Fatal(err)
	}
	c, err := designkit.ReadCanvas(dir, "acme-ui", "1.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Root = "AcmeRoot"
	q := url.Values{}
	q.Set("c", "Button")
	page, ok := playBoard(c, "", dir, q)
	if !ok {
		t.Fatal("no board")
	}
	for _, want := range []string{`href="ds/acme-ui/components/bundle.css"`, `src="ds/acme-ui/components/bundle.js"`,
		`<x-import component-from-global-scope="Acme.AcmeRoot"><div class="kit-subject" data-kit-subject=""><x-import component-from-global-scope="Acme.Button"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("no %s in:\n%s", want, page)
		}
	}
	if strings.Contains(page, "ds/./") {
		t.Fatalf("a root theme addressed as ds/./:\n%s", page)
	}
}

// A kit that is one theme at the root of its version serves its files under ds/<kit id>/ (path.Base("") was ".").
func TestKitViewServesARootThemeUnderTheKitID(t *testing.T) {
	dir := t.TempDir()
	ver := filepath.Join(dir, "acme-ui", "1.0.0")
	if err := os.MkdirAll(filepath.Dir(ver), 0o755); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("../designkit/testdata/check/ok")
	if err := os.Symlink(abs, ver); err != nil {
		t.Fatal(err)
	}
	l := New(Config{LibraryDir: t.TempDir(), KitsDir: dir, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	for path, code := range map[string]int{"ds/acme-ui/components/bundle.js": 200, "ds/acme-ui/tokens.json": 200, "ds/./components/bundle.js": 404} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/kitv/acme-ui/1.0.0/default/"+path, nil)
		segs := append([]string{"acme-ui", "1.0.0", "default"}, strings.Split(path, "/")...)
		l.serveKitView(rec, req, url.Values{}, segs, false)
		if rec.Code != code {
			t.Errorf("%s: %d, want %d", path, rec.Code, code)
		}
	}
}

// An element value ({"$c": …}) reaches the page as data the reviver turns into the kit's component; what the kit does
// not have, and props its component does not declare or the preview never passes, do not reach it at all.
func TestPlayBoardBuildsElementsOfTheKitOnly(t *testing.T) {
	c := &designkit.Canvas{Namespace: "Kit", Groups: []designkit.ComponentGroup{{ID: "g", Components: []designkit.Component{
		{ID: "Tooltip", Name: "Tooltip", Props: []designkit.Prop{{Name: "content"}, {Name: "children"}}},
		{ID: "Button", Name: "Button", Props: []designkit.Prop{{Name: "variant"}, {Name: "onClick"}, {Name: "children"}}},
	}}}}
	q := url.Values{}
	q.Set("c", "Tooltip")
	q.Set("p", `{"content":"Copied","children":{"$c":"Button","props":{"variant":"primary","onClick":"alert(1)","evil":"x"},"children":["Copy",{"$c":"Script"},{"$fn":"id"},{"$fn":"x;alert(2)"}]}}`)
	page, ok := playBoard(c, "", t.TempDir(), q)
	if !ok {
		t.Fatal("no board")
	}
	for _, want := range []string{`children="{{children}}"`, `"$c":"Button"`, `"variant":"primary"`, `React.createElement(C, props, ...kids)`, `window["Kit"]`, `{"$fn":"id"}`} {
		if !strings.Contains(page, want) {
			t.Fatalf("no %q in:\n%s", want, page)
		}
	}
	for _, bad := range []string{"alert(1)", "alert(2)", "evil", `"Script"`} {
		if strings.Contains(page, bad) {
			t.Fatalf("the board carries %q:\n%s", bad, page)
		}
	}
}
