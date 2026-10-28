package design

import (
	"compress/gzip"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
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
	return publicRequest(a, method, path, token, nil)
}

func publicRequest(a *API, method, path, token string, headers http.Header) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/agents/api/acme/design-public/"+path, nil)
	if token != "" {
		r.Header.Set(testHeader, token)
	}
	for key, values := range headers {
		for _, value := range values {
			r.Header.Add(key, value)
		}
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
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("index cache: %q", w.Header().Get("Cache-Control"))
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
	wantIndex := w.Body.String()
	if !strings.HasPrefix(wantIndex, `<script data-artifact="p1" data-guest="1" src="__guest/client.js"></script><script>`) ||
		!strings.HasSuffix(wantIndex, `<html>index</html>`) || w.Header().Get("Content-Type") != "text/html; charset=utf-8" ||
		w.Header().Get("Content-Length") != strconv.Itoa(len(wantIndex)) {
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

func TestPublicOpenHandleSurvivesPathSwap(t *testing.T) {
	a, root := publicAPI(t, "")
	outside := filepath.Join(root, "outside", "private.txt")
	writeFile(t, outside, "private")
	public := filepath.Join(root, "p1", "project", "assets", "a.css")
	want := strings.Repeat("body { color: green; }\n", 200)
	writeFile(t, public, want)

	f, st, path, ok := a.openPublic(a.L, "p1", []string{"assets", "a.css"})
	if !ok {
		t.Fatal("open public file")
	}
	defer f.Close()
	if err := os.Remove(public); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, public); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/assets/a.css", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	a.L.servePublicStatic(w, r, f, st, path, "text/css; charset=utf-8", ".css", "private, no-cache", false)
	if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("served swapped path: %d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Fatalf("served swapped bytes: got %q", body)
	}
}

func TestPublicProjectRootSymlinkCannotEscapeLibrary(t *testing.T) {
	a, root := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, filepath.Join(outside, "index.html"), "private")
	project := filepath.Join(root, "p1", "project")
	if err := os.Rename(project, project+"-safe"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, project); err != nil {
		t.Fatal(err)
	}

	w := publicGet(a, http.MethodGet, "p1/index.html", tok)
	if w.Code != http.StatusNotFound || w.Body.String() != "Not found" {
		t.Fatalf("project root symlink escaped library: %d %q", w.Code, w.Body.String())
	}
}

func TestPublicHTMLReadStaysBoundedAfterOpen(t *testing.T) {
	a, root := publicAPI(t, "")
	path := filepath.Join(root, "p1", "project", "index.html")
	f, _, _, ok := a.openPublic(a.L, "p1", []string{"index.html"})
	if !ok {
		t.Fatal("open public html")
	}
	defer f.Close()
	if err := os.Truncate(path, PublicFileLimit+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readPublicFile(f, PublicFileLimit); err == nil {
		t.Fatal("grown HTML was read past the public limit")
	}
}

func TestPublicHTMLResponseStaysWithinFileLimit(t *testing.T) {
	a, root := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	body := []byte("<html>" + strings.Repeat("x", PublicFileLimit-len("<html>")))
	if err := os.WriteFile(filepath.Join(root, "p1", "project", "large.html"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	w := publicGet(a, http.MethodGet, "p1/large.html", tok)
	if w.Code != http.StatusNotFound || w.Body.String() != "Not found" {
		t.Fatalf("expanded HTML exceeded public limit: %d bytes=%d", w.Code, w.Body.Len())
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

func TestPublicBoardsGetTheGuestScriptRelativeToThemselves(t *testing.T) {
	a, root := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	body := publicGet(a, "GET", "p1/Main.dc.html", tok).Body.String()
	guest := strings.Index(body, `<script data-artifact="p1" data-guest="1" src="__guest/client.js"></script>`)
	resources := strings.Index(body, `"__resources"`)
	engine := strings.Index(body, `src="./support.js?tkv=`)
	if guest < 0 || resources < 0 || engine < 0 || guest > engine || resources > engine {
		t.Fatalf("head of a board: %q", body)
	}
	for _, f := range vendorFiles {
		if !strings.Contains(body, `"`+f.URL+`":"__vendor/`+f.Name+`"`) {
			t.Fatalf("no public mapping for %s in %q", f.URL, body)
		}
		w := publicGet(a, "GET", "p1/__vendor/"+f.Name, tok)
		if w.Code != 200 || w.Header().Get("ETag") == "" {
			t.Fatalf("public vendor %s: %d %v", f.Name, w.Code, w.Header())
		}
	}
	writeFile(t, filepath.Join(root, "p1", "project", "lib", "deep", "page.html"), `<body>x</body>`)
	if body := publicGet(a, "GET", "p1/lib/deep/page.html", tok).Body.String(); !strings.HasPrefix(body, `<script data-artifact="p1" data-guest="1" src="../../__guest/client.js"></script>`) ||
		!strings.Contains(body, `":"../../__vendor/`) {
		t.Fatalf("nested board: %q", body)
	}
}

func TestPublicStaticUsesPrivateVersionsETagAndGzip(t *testing.T) {
	a, root := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	file := filepath.Join(root, "p1", "project", "assets", "large.js")
	body := strings.Repeat("const example = 1;\n", 200)
	writeFile(t, file, body)
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	version := FileVersion(st)

	unversioned := publicGet(a, "GET", "p1/assets/large.js", tok)
	if unversioned.Code != 200 || unversioned.Header().Get("Cache-Control") != "private, no-cache" || unversioned.Header().Get("ETag") == "" {
		t.Fatalf("unversioned: %d %v", unversioned.Code, unversioned.Header())
	}
	versioned := publicGet(a, "GET", "p1/assets/large.js?tkv="+version, tok)
	if versioned.Code != 200 || versioned.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("versioned: %d %v", versioned.Code, versioned.Header())
	}
	etag := versioned.Header().Get("ETag")
	notModified := publicRequest(a, "GET", "p1/assets/large.js?tkv="+version, tok, http.Header{"If-None-Match": {etag}})
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("not modified: %d %q", notModified.Code, notModified.Body.String())
	}
	packed := publicRequest(a, "GET", "p1/assets/large.js?tkv="+version, tok, http.Header{"Accept-Encoding": {"gzip"}})
	if packed.Code != 200 || packed.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip: %d %v", packed.Code, packed.Header())
	}
	zr, err := gzip.NewReader(packed.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != body {
		t.Fatal("gzip body differs")
	}
	html := publicGet(a, "GET", "p1/Main.dc.html", tok)
	if html.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("html cache: %q", html.Header().Get("Cache-Control"))
	}
}

func TestPublicGuestClientScript(t *testing.T) {
	a, _ := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	w := publicGet(a, "GET", "p1/__guest/client.js", tok)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(w.Body.String(), "data-guest") {
		t.Fatalf("client.js: %d %v", w.Code, w.Header())
	}
	if w := publicGet(a, "GET", "p1/__guest/other.js", tok); w.Code != 404 {
		t.Fatalf("other file of the reserved folder: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p1/__guest/client.js", ""); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
}

func guestBuild() fstest.MapFS {
	return fstest.MapFS{
		"guest.html":           {Data: []byte(`<!doctype html><script type="module" src="./assets/guest-1.js"></script>`)},
		"index.html":           {Data: []byte(`the app`)},
		"assets/guest-1.js":    {Data: []byte(`import "./chunk-2.js"`)},
		"assets/chunk-2.js":    {Data: []byte(`export {}`)},
		"assets/locale-3.js":   {Data: []byte(`export default {}`)},
		"assets/guest-4.css":   {Data: []byte(`body{}`)},
		"assets/font-5.woff2":  {Data: []byte(`font`)},
		"assets/app-only-6.js": {Data: []byte(`not for guests`)},
		".vite/manifest.json": {Data: []byte(`{
			"guest.html": {"file":"assets/guest-1.js","isEntry":true,"imports":["chunk"],"dynamicImports":["locale"],"css":["assets/guest-4.css"]},
			"chunk": {"file":"assets/chunk-2.js","assets":["assets/font-5.woff2"]},
			"locale": {"file":"assets/locale-3.js"},
			"index.html": {"file":"assets/app-only-6.js","isEntry":true}
		}`)},
	}
}

func TestPublicAppServesOnlyTheGuestBundle(t *testing.T) {
	a, _ := publicAPI(t, "")
	a.GuestApp = guestBuild()
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	for path, want := range map[string]int{
		"p1/_app/index.html":                  200,
		"p1/_app/assets/guest-1.js":           200,
		"p1/_app/assets/chunk-2.js":           200,
		"p1/_app/assets/locale-3.js":          200,
		"p1/_app/assets/guest-4.css":          200,
		"p1/_app/assets/font-5.woff2":         200,
		"p1/_app/assets/app-only-6.js":        404, // the app's own chunk
		"p1/_app/guest.html":                  404, // only as index.html
		"p1/_app/.vite/manifest.json":         404,
		"p1/_app/assets/..%2f..%2findex.html": 404,
		"p1/_app/nope.js":                     404,
		"p1/_app":                             404,
	} {
		if w := publicGet(a, "GET", path, tok); w.Code != want {
			t.Errorf("%s: got %d want %d", path, w.Code, want)
		}
	}
	w := publicGet(a, "GET", "p1/_app/index.html", tok)
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(w.Body.String(), "guest-1.js") {
		t.Fatalf("index.html: %v %q", w.Header(), w.Body.String())
	}
	w = publicGet(a, "GET", "p1/_app/assets/guest-1.js", tok)
	if w.Header().Get("Content-Type") != "text/javascript; charset=utf-8" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("js: %v", w.Header())
	}
	if w := publicGet(a, "GET", "p1/_app/assets/font-5.woff2", tok); w.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("font: %v", w.Header())
	}
	if w := publicGet(a, "GET", "p1/_app/index.html", ""); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := publicGet(a, "GET", "p2/_app/index.html", tok); w.Code != 403 {
		t.Fatalf("other project: %d", w.Code)
	}
}

func TestPublicAppWithoutABuild(t *testing.T) {
	a, _ := publicAPI(t, "")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	if w := publicGet(a, "GET", "p1/_app/index.html", tok); w.Code != 404 {
		t.Fatalf("no build: %d", w.Code)
	}
	a.GuestApp = fstest.MapFS{"guest.html": {Data: []byte("x")}}
	if w := publicGet(a, "GET", "p1/_app/index.html", tok); w.Code != 404 {
		t.Fatalf("no manifest: %d", w.Code)
	}
}
