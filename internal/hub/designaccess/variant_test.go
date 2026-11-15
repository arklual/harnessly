package designaccess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fileLibrary is a fakeLibrary whose Serve answers files of its projects, and 404 for the rest.
type fileLibrary struct {
	fakeLibrary
	served map[string]string
}

func (f *fileLibrary) Serve(w http.ResponseWriter, _ *http.Request, _, project string, _ int64, segs, _ []string, _ bool) error {
	body, ok := f.served[project+"/"+strings.Join(segs, "/")]
	if !ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Not found"))
		return nil
	}
	w.Header().Set("Content-Type", "text/javascript")
	_, _ = w.Write([]byte(body))
	return nil
}

// a conflict variant holds only its board; the board's other files come from the project it was split from,
// for a person who may open that project, and stay 404 for anyone else.
func TestVariantFilesComeFromItsProject(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	for _, id := range []string{"site", "site-variant-m1"} {
		if done, err := r.svc.registerCreated(ctx, r.owner, id, r.ownerU.Email); err != nil || !done {
			t.Fatal(id, done, err)
		}
	}
	if err := r.svc.write(ctx, r.owner, func(t *txn) error { return t.setTeam("site", r.ownerU.Email, levelNone) }); err != nil {
		t.Fatal(err)
	}
	lib := &fileLibrary{
		fakeLibrary: fakeLibrary{
			heads: map[string]int64{"site": 9, "site-variant-m1": 4},
			files: map[string][]byte{"site-variant-m1/variant.json": []byte(`{"v":1,"of":"site","board":"a.dc.html","reason":"conflict"}`)},
		},
		served: map[string]string{
			"site/support.js":           "orig()",
			"site-variant-m1/a.dc.html": "<html>",
			"site-variant-m1/own.js":    "mine()",
			"site/a.dc.html":            "<old>",
		},
	}
	r.svc.UseView(lib)
	get := func(owner bool, file string) *httptest.ResponseRecorder {
		u, m := r.ownerU, r.owner
		if !owner {
			u, m = r.strangeU, r.stranger
		}
		req := httptest.NewRequest(http.MethodGet, "/m/"+m.ID+"/api/platform/design/store/site-variant-m1/f/"+file, nil)
		w := httptest.NewRecorder()
		if !r.svc.ServeMachine(w, req, u, m) {
			t.Fatal("left to the machine")
		}
		return w
	}
	for file, want := range map[string]string{"a.dc.html": "<html>", "own.js": "mine()", "support.js": "orig()"} {
		if w := get(true, file); w.Code != http.StatusOK || w.Body.String() != want {
			t.Errorf("owner %s: %d %q", file, w.Code, w.Body.String())
		}
	}
	if w := get(true, "nope.js"); w.Code != http.StatusNotFound || w.Body.String() != "Not found" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("a file of neither: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	// The stranger may open the variant but not the personal project it came from: its files stay closed.
	if w := get(false, "own.js"); w.Code != http.StatusOK {
		t.Fatalf("the stranger cannot open the variant: %d %s", w.Code, w.Body.String())
	}
	if w := get(false, "support.js"); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "orig") {
		t.Errorf("the stranger read the closed project through the variant: %d %q", w.Code, w.Body.String())
	}
}
