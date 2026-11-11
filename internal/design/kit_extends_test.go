package design

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

func extendsLibrary(t *testing.T) *Library {
	t.Helper()
	l := New(Config{LibraryDir: t.TempDir(), KitsDir: t.TempDir(), Texts: Texts{Tag: "test-design", CLI: "acme"}})
	installKitFixture(t, l.KitsDir(), "acme-ui", "1.0.0")
	copyKitFixture(t, "../designkit/testdata/kit/acme-brand", filepath.Join(l.KitsDir(), "acme-brand", "1.0.0"))
	return l
}

// A page that draws a component of a kit on a kit loads the base first, then the kit; the base's files are served under
// the base's name; an inherited component is called by the base's namespace.
func TestPlayBoardOfAChildLoadsItsBaseFirst(t *testing.T) {
	l := extendsLibrary(t)
	c, err := designkit.ReadCanvas(l.KitsDir(), "acme-brand", "1.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{}
	q.Set("c", "Card")
	page, ok := playBoard(c, "", l.KitsDir(), q)
	if !ok {
		t.Fatal("no board")
	}
	order := []string{`href="ds/acme-ui/components/bundle.css"`, `href="ds/acme-brand/components/bundle.css"`,
		`src="ds/acme-ui/components/bundle.js"`, `src="ds/acme-brand/components/bundle.js"`}
	last := -1
	for _, want := range order {
		i := strings.Index(page, want)
		if i < 0 || i < last {
			t.Fatalf("%s is missing or out of order in:\n%s", want, page)
		}
		last = i
	}
	for _, want := range []string{`component-from-global-scope="Acme.Root"`, `component-from-global-scope="Acme.Card"`, `window["AcmeBrand"], window["Acme"]`} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s in:\n%s", want, page)
		}
	}
	for path, code := range map[string]int{
		"ds/acme-ui/components/bundle.js":    200,
		"ds/acme-brand/components/bundle.js": 200,
		"ds/acme-other/components/bundle.js": 404,
		"ds/acme-ui/../../kit.json":          404,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/kitv/acme-brand/1.0.0/default/"+path, nil)
		l.serveKitView(rec, req, url.Values{}, append([]string{"acme-brand", "1.0.0", "default"}, strings.Split(path, "/")...), false)
		if rec.Code != code {
			t.Errorf("%s: %d, want %d", path, rec.Code, code)
		}
	}
}

func TestKitToolsAnswerAChildAsAssembled(t *testing.T) {
	l := extendsLibrary(t)
	comp, _, bad := callKit(t, l, "kit_component", "kit", "acme-brand", "name", "Card")
	if bad || comp["from"] != "acme-ui@1.0.0" || !strings.Contains(comp["call"].(string), `"Acme.Card"`) {
		t.Fatalf("inherited: %v", comp)
	}
	btn, _, _ := callKit(t, l, "kit_component", "kit", "acme-brand", "name", "Button")
	if btn["overrides"] != "acme-ui@1.0.0" {
		t.Errorf("replacing: %v", btn)
	}
	idx, text, _ := callKit(t, l, "kit_index", "kit", "acme-brand")
	if !strings.Contains(text, "Card +2 ←acme-ui") || idx["namespace"] != "AcmeBrand" {
		t.Errorf("index: %s", text)
	}
	chk, _, _ := callKit(t, l, "kit_check", "kit", "acme-brand")
	if chk["ok"] != true {
		t.Errorf("check: %v", chk)
	}
}
