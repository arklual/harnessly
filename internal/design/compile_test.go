package design

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileComponentModes(t *testing.T) {
	// A script: JSX translated, top-level names left as the page's, no module wrapper.
	mode, code, err := compileComponent("lib/card.jsx", []byte("function Card(p) { return <div className=\"c\">{p.title}</div>; }\nconst Wide = () => <><Card title=\"a\" /></>;\n"))
	if err != nil || mode != compileScript {
		t.Fatalf("script: %q %v", mode, err)
	}
	out := string(code)
	for _, want := range []string{compiledMark(compileScript), "function Card(p)", `React.createElement("div"`, "React.Fragment", "const Wide"} {
		if !strings.Contains(out, want) {
			t.Errorf("script lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, compiledMark(compileScript)) || strings.Contains(out, "module.exports") || strings.Contains(out, "<div") {
		t.Errorf("script is wrapped or untranslated:\n%s", out)
	}

	// A module: types dropped, exports as CommonJS.
	mode, code, err = compileComponent("lib/price.tsx", []byte("type P = { sum: number };\nexport function Price({ sum }: P) { return <b>{sum}</b>; }\nexport default Price;\n"))
	if err != nil || mode != compileCJS {
		t.Fatalf("module: %q %v", mode, err)
	}
	out = string(code)
	if !strings.HasPrefix(out, compiledMark(compileCJS)) || !strings.Contains(out, "module.exports") || strings.Contains(out, "type P") || strings.Contains(out, ": P") {
		t.Errorf("module is not CommonJS without types:\n%s", out)
	}

	// Plain JavaScript without exports is left alone: the engine runs it as it is.
	src := []byte("window.Fmt = function (n) { return String(n); };\n")
	if mode, code, err = compileComponent("lib/fmt.js", src); err != nil || mode != compileNone || string(code) != string(src) {
		t.Fatalf("plain: %q %v %q", mode, err, code)
	}

	// A file that does not compile is an error, so the plain answer is sent and the browser reports it.
	if _, _, err = compileComponent("lib/bad.jsx", []byte("function X() { return <div>; }")); err == nil || !strings.Contains(err.Error(), "lib/bad.jsx:1:") {
		t.Fatalf("a broken file: %v", err)
	}
	if _, _, err = compileComponent("big.jsx", make([]byte, compileMaxBytes+1)); err != errCompileTooLarge {
		t.Fatalf("a huge file: %v", err)
	}
}

func TestLibraryServesComponentFilesCompiled(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "site", "project")
	writeFile(t, filepath.Join(dir, "Main.dc.html"), "<!doctype html><html><head></head><body><x-dc></x-dc></body></html>")
	writeFile(t, filepath.Join(dir, "lib", "card.jsx"), "function Card() { return <i>x</i>; }\n")
	writeFile(t, filepath.Join(dir, "lib", "bad.jsx"), "function X() { return <div>; }\n")
	writeFile(t, filepath.Join(dir, "lib", "fmt.js"), "window.Fmt = 1;\n")
	writeFile(t, filepath.Join(root, "site", "artifact.json"), `{"v":1,"title":"Site","boards":[{"path":"Main.dc.html","title":"Main"}]}`)
	l := New(Config{LibraryDir: root, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	l.srv = &server{}
	t.Cleanup(l.StopWatching)
	get := func(path string, headers ...string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18470"
		r.Header.Set("Accept-Encoding", "identity")
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		l.serveLibrary(w, r, 18470)
		return w
	}

	// Asked for compiled: translated, marked, with an ETag of its own.
	w := get("/a/site/p/lib/card.jsx?tkc=1")
	if w.Code != 200 || w.Header().Get(compiledHeader) != compileScript || !strings.HasPrefix(w.Body.String(), compiledMark(compileScript)) || !strings.Contains(w.Body.String(), "React.createElement") {
		t.Fatalf("compiled: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	etag := w.Header().Get("ETag")
	if again := get("/a/site/p/lib/card.jsx?tkc=1", "If-None-Match", etag); again.Code != 304 {
		t.Fatalf("a known ETag: %d", again.Code)
	}

	// Not asked: the source, as every engine got it before.
	plain := get("/a/site/p/lib/card.jsx")
	if plain.Code != 200 || plain.Header().Get(compiledHeader) != "" || !strings.Contains(plain.Body.String(), "<i>x</i>") || plain.Header().Get("ETag") == etag {
		t.Fatalf("plain: %d %v %q", plain.Code, plain.Header(), plain.Body.String())
	}

	// Nothing to translate, or a file that does not compile: the source, unmarked.
	for _, path := range []string{"/a/site/p/lib/fmt.js?tkc=1", "/a/site/p/lib/bad.jsx?tkc=1"} {
		if w := get(path); w.Code != 200 || w.Header().Get(compiledHeader) != "" || strings.HasPrefix(w.Body.String(), "/*tk-compiled") {
			t.Fatalf("%s: %d %v %q", path, w.Code, w.Header(), w.Body.String())
		}
	}

	// An edit is a new version: the next answer is the new code.
	file := filepath.Join(dir, "lib", "card.jsx")
	writeFile(t, file, "function Card() { return <b>changed</b>; }\n")
	st, _ := os.Stat(file)
	_ = os.Chtimes(file, st.ModTime().Add(2e9), st.ModTime().Add(2e9))
	if w := get("/a/site/p/lib/card.jsx?tkc=1"); !strings.Contains(w.Body.String(), `"changed"`) || w.Header().Get("ETag") == etag {
		t.Fatalf("after an edit: %v %q", w.Header(), w.Body.String())
	}
}

func TestPublicPathServesComponentFilesCompiled(t *testing.T) {
	a, root := publicAPI(t, "")
	writeFile(t, filepath.Join(root, "p1", "project", "lib", "card.tsx"), "export const Card = (p: { t: string }) => <i>{p.t}</i>;\n")
	tok := publicToken(t, "pair-secret", "acme", "p1", nil)
	w := publicGet(a, "GET", "p1/lib/card.tsx?tkc=1", tok)
	if w.Code != 200 || !strings.Contains(w.Body.String(), compiledMark(compileCJS)) || !strings.Contains(w.Body.String(), "React.createElement") {
		t.Fatalf("a guest's compiled file: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	if w := publicGet(a, "GET", "p1/lib/card.tsx", tok); w.Code != 200 || !strings.Contains(w.Body.String(), "<i>{p.t}</i>") {
		t.Fatalf("a guest's source: %d %q", w.Code, w.Body.String())
	}
	if w := publicGet(a, "GET", "p1/lib/card.tsx?tkc=1", ""); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
}
