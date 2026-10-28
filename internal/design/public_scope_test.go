package design

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scopeAPI is a library of one project with two pages and a style they share, over a history of its own.
func scopeAPI(t *testing.T) (*API, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "p1", "artifact.json"), `{"title":"Shop"}`)
	writeFile(t, filepath.Join(root, "p1", "project", "One.dc.html"), `<html><head></head><body>one v1</body></html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "Two.dc.html"), `<html><head></head><body>two v1</body></html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "assets", "a.css"), `body{}`)
	l := New(Config{LibraryDir: root, DataDir: t.TempDir(), Texts: Texts{Tag: "test-design", CLI: "acme", HistoryEmail: "h@localhost"}})
	t.Cleanup(l.StopWatching)
	a := &API{L: l, PlatformAPI: "/api/acme", PublicToken: PublicConfig{Secret: []byte("pair-secret"), Salt: "acme", Audience: testAudience, Header: testHeader, Now: func() time.Time { return publicNow }}}
	return a, root
}

func scopeToken(t *testing.T, claims map[string]any) string {
	return publicToken(t, "pair-secret", "acme", "p1", func(c map[string]any) {
		for k, v := range claims {
			c[k] = v
		}
	})
}

func boardPaths(t *testing.T, a *API, token string) []string {
	t.Helper()
	w := publicGet(a, "GET", "p1", token)
	if w.Code != 200 {
		t.Fatalf("index: %d %s", w.Code, w.Body.String())
	}
	var got struct{ Boards []struct{ Path string } }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, b := range got.Boards {
		out = append(out, b.Path)
	}
	return out
}

func TestPublicPagesHideTheOthersEverywhere(t *testing.T) {
	a, _ := scopeAPI(t)
	all := boardPaths(t, a, scopeToken(t, nil))
	if len(all) != 2 {
		t.Fatalf("boards without a limit: %v", all)
	}
	token := scopeToken(t, map[string]any{"pages": []string{"One.dc.html"}})
	if got := boardPaths(t, a, token); len(got) != 1 || got[0] != "One.dc.html" {
		t.Fatalf("limited index: %v", got)
	}
	if w := publicGet(a, "GET", "p1/One.dc.html", token); w.Code != 200 {
		t.Fatalf("chosen page: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1/Two.dc.html", token); w.Code != http.StatusNotFound {
		t.Fatalf("hidden page: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1/assets/../Two.dc.html", token); w.Code != http.StatusNotFound {
		t.Fatalf("hidden page by a detour: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1/assets/a.css", token); w.Code != 200 {
		t.Fatalf("a shared file is not a page: %d", w.Code)
	}
	// A list that came out empty shows nothing, it does not fall back to everything.
	none := scopeToken(t, map[string]any{"pages": []string{}})
	if got := boardPaths(t, a, none); len(got) != 0 {
		t.Fatalf("empty list: %v", got)
	}
	if w := publicGet(a, "GET", "p1/One.dc.html", none); w.Code != http.StatusNotFound {
		t.Fatalf("empty list, a page: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1", scopeToken(t, map[string]any{"pages": "One.dc.html"})); w.Code != 401 {
		t.Fatalf("malformed pages: %d", w.Code)
	}
}

func TestPublicPinnedVersionIsTheSnapshot(t *testing.T) {
	a, root := scopeAPI(t)
	for name, claims := range map[string]map[string]any{
		"guest":         nil,
		"pin and rev":   {"pin": true, "rev": strings.Repeat("a", 40)},
		"pin and pages": {"pin": true, "pages": []string{"One.dc.html"}},
		"pin not bool":  {"pin": "yes"},
	} {
		code := publicGet(a, "GET", "p1/"+PublicRevDir, scopeToken(t, claims)).Code
		if code != 403 && code != 401 {
			t.Fatalf("%s on __rev: %d", name, code)
		}
	}
	w := publicGet(a, "GET", "p1/"+PublicRevDir, scopeToken(t, map[string]any{"pin": true}))
	if w.Code != 200 {
		t.Fatalf("rev: %d %s", w.Code, w.Body.String())
	}
	var pin struct{ Rev string }
	if err := json.Unmarshal(w.Body.Bytes(), &pin); err != nil || !fullSHA.MatchString(pin.Rev) {
		t.Fatalf("rev answer %q: %v", w.Body.String(), err)
	}
	// The owner keeps editing: a page changes, a page is added.
	writeFile(t, filepath.Join(root, "p1", "project", "One.dc.html"), `<html><head></head><body>one v2</body></html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "Three.dc.html"), `<html><head></head><body>three</body></html>`)
	a.L.listing.invalidateAll()

	pinned := scopeToken(t, map[string]any{"rev": pin.Rev})
	body := publicGet(a, "GET", "p1/One.dc.html", pinned)
	if got := body.Header().Get(PublicRevHeader); got != pin.Rev {
		t.Fatalf("pinned answer says rev %q", got)
	}
	if body.Code != 200 || !strings.Contains(body.Body.String(), "one v1") || strings.Contains(body.Body.String(), "one v2") {
		t.Fatalf("pinned page: %d %s", body.Code, body.Body.String())
	}
	if got := boardPaths(t, a, pinned); len(got) != 2 {
		t.Fatalf("pinned index: %v", got)
	}
	if w := publicGet(a, "GET", "p1/Three.dc.html", pinned); w.Code != http.StatusNotFound {
		t.Fatalf("a page added later: %d", w.Code)
	}
	latest := scopeToken(t, nil)
	if w := publicGet(a, "GET", "p1/One.dc.html", latest); !strings.Contains(w.Body.String(), "one v2") {
		t.Fatalf("a link without a version sees the edit: %s", w.Body.String())
	}
	if got := boardPaths(t, a, latest); len(got) != 3 {
		t.Fatalf("latest index: %v", got)
	}

	// The two settings together, and a version the history does not know.
	both := scopeToken(t, map[string]any{"rev": pin.Rev, "pages": []string{"Two.dc.html"}})
	if w := publicGet(a, "GET", "p1/One.dc.html", both); w.Code != http.StatusNotFound {
		t.Fatalf("pinned and limited: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1/Two.dc.html", both); w.Code != 200 {
		t.Fatalf("pinned and limited, the chosen: %d", w.Code)
	}
	missing := scopeToken(t, map[string]any{"rev": strings.Repeat("a", 40)})
	if w := publicGet(a, "GET", "p1", missing); w.Code != http.StatusNotFound {
		t.Fatalf("unknown version: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1", scopeToken(t, map[string]any{"rev": "--all"})); w.Code != 401 {
		t.Fatalf("malformed version: %d", w.Code)
	}
}

func TestPublicRevAsksAreDeduped(t *testing.T) {
	a, _ := scopeAPI(t)
	pin := scopeToken(t, map[string]any{"pin": true})
	first := publicGet(a, "GET", "p1/"+PublicRevDir, pin).Body.String()
	if second := publicGet(a, "GET", "p1/"+PublicRevDir, pin).Body.String(); second != first {
		t.Fatalf("repeated ask: %s then %s", first, second)
	}
}

func TestPublicLimitedLinkServesOnlyChosenMarkup(t *testing.T) {
	a, root := scopeAPI(t)
	writeFile(t, filepath.Join(root, "p1", "project", "notes.html"), `<html>secret notes</html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "extra.HTM"), `<html>extra</html>`)
	token := scopeToken(t, map[string]any{"pages": []string{"One.dc.html"}})
	for _, p := range []string{
		"Two.dc.html", "TWO.DC.HTML", "two.dc.html", "./Two.dc.html", "assets/%2e%2e/Two.dc.html",
		"%54wo.dc.html", "notes.html", "NOTES.HTML", "extra.HTM",
	} {
		if w := publicGet(a, "GET", "p1/"+p, token); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := publicGet(a, "GET", "p1/One.dc.html", token); w.Code != 200 {
		t.Fatalf("chosen: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1/assets/a.css", token); w.Code != 200 {
		t.Fatalf("asset: %d", w.Code)
	}
	// Without a limit nothing changes.
	if w := publicGet(a, "GET", "p1/notes.html", scopeToken(t, nil)); w.Code != 200 {
		t.Fatalf("unlimited: %d", w.Code)
	}
}
