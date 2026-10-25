package design

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var publicNow = time.Unix(1_800_000_000, 0)

const (
	testAudience = "acme-design-public"
	testHeader   = "x-acme-design-token"
)

func publicToken(t *testing.T, secret, salt, project string, mutate func(map[string]any)) string {
	t.Helper()
	key, err := hkdf.Key(sha256.New, []byte(secret), []byte(salt), "design-public/v1", 32)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{"aud": testAudience, "prj": project, "iat": publicNow.Unix() - 5, "exp": publicNow.Unix() + 55}
	if mutate != nil {
		mutate(claims)
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "HS256", "typ": "JWT"}) + "." + enc(claims)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func publicAPI(t *testing.T, mirror string) (*API, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "p1", "project", "Main.dc.html"), `<html><head><script src="./support.js"></script></head><body>x</body></html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "index.html"), `<html>index</html>`)
	writeFile(t, filepath.Join(root, "p1", "project", "assets", "a.css"), `body{}`)
	writeFile(t, filepath.Join(root, "p1", "project", "assets", "notes.bin"), `x`)
	writeFile(t, filepath.Join(root, "p1", "project", ".secret.txt"), `hidden`)
	writeFile(t, filepath.Join(root, "p1", "project", ".git", "config.txt"), `hidden`)
	writeFile(t, filepath.Join(root, "p1", "project", "support.js"), "/* own engine */")
	writeFile(t, filepath.Join(root, "p1", "artifact.json"), `{"title":"Shop"}`)
	writeFile(t, filepath.Join(root, "outside", "leak.txt"), `leak`)
	writeFile(t, filepath.Join(root, "outside", "leak.html"), `<html>leak</html>`)
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "p1", "project", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside", "leak.txt"), filepath.Join(root, "p1", "project", "leak.txt")); err != nil {
		t.Fatal(err)
	}
	l := New(Config{LibraryDir: root, ReleasesDir: mirror, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	a := &API{L: l, PlatformAPI: "/api/acme", PublicToken: PublicConfig{Secret: []byte("pair-secret"), Salt: "acme", Audience: testAudience, Header: testHeader, Now: func() time.Time { return publicNow }}}
	return a, root
}

func publicGet(a *API, method, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/agents/api/acme/design-public/"+path, nil)
	if token != "" {
		r.Header.Set(testHeader, token)
	}
	if !a.PublicMatch(r) {
		panic("no match")
	}
	a.Public(w, r)
	return w
}

func TestPublicTokenChecks(t *testing.T) {
	a, _ := publicAPI(t, "")
	good := publicToken(t, "pair-secret", "acme", "p1", nil)
	if w := publicGet(a, "GET", "p1", good); w.Code != 200 {
		t.Fatalf("good token: %d", w.Code)
	}
	cases := map[string]struct {
		token string
		want  int
	}{
		"none":          {"", 401},
		"foreign key":   {publicToken(t, "other-secret", "acme", "p1", nil), 401},
		"foreign salt":  {publicToken(t, "pair-secret", "other", "p1", nil), 401},
		"expired":       {publicToken(t, "pair-secret", "acme", "p1", func(c map[string]any) { c["exp"] = publicNow.Unix() - 1 }), 401},
		"too long":      {publicToken(t, "pair-secret", "acme", "p1", func(c map[string]any) { c["exp"] = publicNow.Unix() + 600 }), 401},
		"aud":           {publicToken(t, "pair-secret", "acme", "p1", func(c map[string]any) { c["aud"] = "acme-share-read" }), 401},
		"iat 20s ahead": {publicToken(t, "pair-secret", "acme", "p1", func(c map[string]any) { c["iat"] = publicNow.Unix() + 20; c["exp"] = publicNow.Unix() + 80 }), 200},
		"iat 40s ahead": {publicToken(t, "pair-secret", "acme", "p1", func(c map[string]any) { c["iat"] = publicNow.Unix() + 40; c["exp"] = publicNow.Unix() + 100 }), 401},
		"foreign prj":   {publicToken(t, "pair-secret", "acme", "p2", nil), 403},
		"garbage":       {"a.b.c", 401},
	}
	for name, c := range cases {
		if w := publicGet(a, "GET", "p1", c.token); w.Code != c.want {
			t.Errorf("%s: got %d want %d", name, w.Code, c.want)
		}
		if w := publicGet(a, "GET", "p1/index.html", c.token); w.Code != c.want {
			t.Errorf("%s (file): got %d want %d", name, w.Code, c.want)
		}
	}
}

func TestPublicIndexListsBoards(t *testing.T) {
	a, _ := publicAPI(t, "")
	w := publicGet(a, "GET", "p1", publicToken(t, "pair-secret", "acme", "p1", nil))
	var got struct {
		Title  string
		Boards []struct{ ID, Title, Path string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := a.L.ReadArtifact("p1")
	if got.Title != want.Title || len(got.Boards) != len(want.Boards) || len(got.Boards) == 0 {
		t.Fatalf("index %+v, library %+v", got, want.Boards)
	}
	if got.Boards[0].Path != want.Boards[0].Path || got.Boards[0].ID == "" {
		t.Fatalf("first board %+v", got.Boards[0])
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal(w.Header())
	}
}

func TestPublicFilesStayInTheProject(t *testing.T) {
	a, _ := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	for path, want := range map[string]int{
		"p1/index.html":              200,
		"p1/assets/a.css":            200,
		"p1/assets/notes.bin":        404, // no served type
		"p1/.secret.txt":             404,
		"p1/.git/config.txt":         404,
		"p1/assets/.hidden.css":      404,
		"p1/link/leak.txt":           404, // symlinked folder out of the project
		"p1/leak.txt":                404, // symlinked file out of the project
		"p1/nope.html":               404,
		"p1/%2e%2e/outside/leak.txt": 404,
		"p1/..%2foutside%2fleak.txt": 404,
		"p1/assets%5c..%5ca.css":     404,
	} {
		if w := publicGet(a, "GET", path, tok); w.Code != want {
			t.Errorf("%s: got %d want %d (%q)", path, w.Code, want, w.Body.String())
		}
	}
	// A project the token names but the library does not have, and a project segment that is not a project.
	if w := publicGet(a, "GET", "ghost/index.html", publicToken(t, "pair-secret", "acme", "ghost", nil)); w.Code != 404 {
		t.Errorf("unknown project: %d", w.Code)
	}
	if w := publicGet(a, "GET", "..%2fp1", publicToken(t, "pair-secret", "acme", "../p1", nil)); w.Code != 404 {
		t.Errorf("project path: %d", w.Code)
	}
	w := publicGet(a, "GET", "p1/index.html", tok)
	if w.Body.String() != "<html>index</html>" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Content-Length") != "18" {
		t.Fatalf("index.html: %q %v", w.Body.String(), w.Header())
	}
	if w := publicGet(a, "HEAD", "p1/index.html", tok); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %q", w.Code, w.Body.String())
	}
	if w := publicGet(a, "POST", "p1/index.html", tok); w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}
}

func TestPublicSizeCap(t *testing.T) {
	a, root := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	f, err := os.Create(filepath.Join(root, "p1", "project", "big.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Truncate(PublicFileLimit + 1)
	f.Close()
	if w := publicGet(a, "GET", "p1/big.txt", tok); w.Code != 404 {
		t.Fatalf("over the cap: %d", w.Code)
	}
}

func TestPublicSupportJSIsTheReleaseEngine(t *testing.T) {
	mirror := releasesMirror(t, relEntry{ID: "legacy", Status: "stable", Engine: "legacy engine"}, relEntry{ID: "r2", Status: "stable", Engine: "r2 engine"})
	a, root := publicAPI(t, mirror)
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	if w := publicGet(a, "GET", "p1/support.js", tok); w.Body.String() != "/* own engine */" {
		t.Fatalf("own engine: %q", w.Body.String())
	}
	writeFile(t, filepath.Join(root, "p1", MetaDirname, MetaProjectFile), `{"v":1,"title":"P1"}`)
	if _, err := a.L.SetProjectRelease("p1", "r2"); err != nil {
		t.Fatal(err)
	}
	w := publicGet(a, "GET", "p1/support.js", tok)
	if w.Code != 200 || w.Body.String() != "r2 engine" {
		t.Fatalf("release engine: %d %q", w.Code, w.Body.String())
	}
	_ = os.Remove(filepath.Join(root, "p1", "project", "support.js"))
	if w := publicGet(a, "GET", "p1/support.js", tok); w.Body.String() != "r2 engine" {
		t.Fatalf("no own copy: %q", w.Body.String())
	}
}

func TestPublicMatch(t *testing.T) {
	a := &API{PlatformAPI: "/api/acme"}
	for path, want := range map[string]bool{
		"/agents/api/acme/design-public/p1":          true,
		"/agents/api/acme/design-public/p1/a/b.html": true,
		"/agents/api/acme/design-public":             false,
		"/agents/api/acme/design/library":            false,
	} {
		if got := a.PublicMatch(httptest.NewRequest(http.MethodGet, path, nil)); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
}
