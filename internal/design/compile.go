package design

import (
	"container/list"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// A board's component file (<x-import from="lib/card.jsx">) used to be translated in the browser: the engine fetched
// Babel standalone (3 MB) and ran it over every JSX or TSX file on every load, keeping nothing. The library server
// translates the file itself, once per version of the file, when the engine asks for it compiled
// (<file>?tkc=1). The answer's first line says how to run the code, /*tk-compiled:<mode>*/ (and X-Board-Compiled
// repeats it for a person reading the network log):
//
//	script  no exports: a classic script, run in the page's global scope
//	cjs     the file has exports: CommonJS, run with (exports, module)
//
// The mark is in the body, not only in a header, because a gateway between the browser and the library may drop a
// header it does not know and still pass the body: code run in the wrong mode would break the board.
//
// An engine that does not ask (the vendor's, an older one) gets the file as before and translates it itself, and so
// does ours when the answer carries no mark: an older server, a folder opened without one, a file with nothing to
// translate, or one that does not compile here. The browser then reports the error the way it always did.

const (
	compiledParam  = "tkc"
	compiledHeader = "X-Board-Compiled"

	compileScript = "script"
	compileCJS    = "cjs"
	compileNone   = "none"

	// compileMaxBytes bounds a source: a component file is written by hand; a bundle that large is already compiled.
	compileMaxBytes  = 4 << 20
	compileCacheMax  = 256
	compileCacheSize = 32 << 20
)

// compilable are the extensions the engine loads as component files.
var compilable = map[string]bool{".js": true, ".mjs": true, ".jsx": true, ".ts": true, ".tsx": true}

// usesExports is the engine's own test of a module (support.js, runComponentFile): the two must agree, because the
// mode decides how the code is run.
var usesExports = regexp.MustCompile(`(?m)^\s*export\s`)

var errCompileTooLarge = errors.New("design: a component file too large to translate")

// compileComponent translates a component file the way the engine's Babel did: JSX into React.createElement, types
// dropped, exports into CommonJS; nothing lowered, nothing renamed, so a script's top-level names stay the page's.
func compileComponent(name string, src []byte) (mode string, code []byte, err error) {
	if len(src) > compileMaxBytes {
		return "", nil, errCompileTooLarge
	}
	ext := strings.ToLower(extname(name))
	module := usesExports.Match(src)
	jsx := ext == ".jsx" || ext == ".tsx"
	if !jsx && ext != ".ts" && !module {
		return compileNone, src, nil
	}
	loader := api.LoaderJSX
	switch ext {
	case ".tsx":
		loader = api.LoaderTSX
	case ".ts":
		loader = api.LoaderTS
	}
	opts := api.TransformOptions{
		Loader:      loader,
		Sourcefile:  name,
		JSX:         api.JSXTransform,
		JSXFactory:  "React.createElement",
		JSXFragment: "React.Fragment",
		Target:      api.ESNext,
		Charset:     api.CharsetUTF8,
		LogLevel:    api.LogLevelSilent,
	}
	mode = compileScript
	if module {
		mode = compileCJS
		opts.Format = api.FormatCommonJS
		opts.Platform = api.PlatformNeutral
	}
	res := api.Transform(string(src), opts)
	if len(res.Errors) > 0 {
		m := res.Errors[0]
		at := ""
		if m.Location != nil {
			at = ":" + strconv.Itoa(m.Location.Line) + ":" + strconv.Itoa(m.Location.Column)
		}
		return "", nil, errors.New(name + at + ": " + m.Text)
	}
	return mode, append([]byte(compiledMark(mode)), res.Code...), nil
}

// compiledMark is the first line of a translated file.
func compiledMark(mode string) string { return "/*tk-compiled:" + mode + "*/\n" }

// compileCache keeps translated files by path and version, the least recently used dropped first.
type compileCache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
	bytes int
}

type compileEntry struct {
	key  string
	mode string
	code []byte
}

// get is the file translated; load reads its source (the caller decides from where: a path, or a handle it holds).
func (c *compileCache) get(key, name string, st os.FileInfo, load func() ([]byte, error)) (string, []byte, error) {
	key += "\n" + FileVersion(st)
	c.mu.Lock()
	if c.items == nil {
		c.items, c.order = map[string]*list.Element{}, list.New()
	}
	if el, ok := c.items[key]; ok {
		c.order.MoveToBack(el)
		e := el.Value.(*compileEntry)
		c.mu.Unlock()
		return e.mode, e.code, nil
	}
	c.mu.Unlock()
	if st.Size() > compileMaxBytes {
		return "", nil, errCompileTooLarge
	}
	src, err := load()
	if err != nil {
		return "", nil, err
	}
	mode, code, err := compileComponent(name, src)
	if err != nil {
		return "", nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; !ok {
		c.items[key] = c.order.PushBack(&compileEntry{key: key, mode: mode, code: code})
		c.bytes += len(code)
		for c.order.Len() > compileCacheMax || c.bytes > compileCacheSize && c.order.Len() > 1 {
			old := c.order.Remove(c.order.Front()).(*compileEntry)
			delete(c.items, old.key)
			c.bytes -= len(old.code)
		}
	}
	return mode, code, nil
}

// wantsCompiled: the engine asked for the file translated.
func wantsCompiled(q url.Values, ext string) bool {
	_, ok := search(q, compiledParam)
	return ok && compilable[ext]
}

// serveCompiled answers a component file translated; false when it is left to the plain answer (the file does not
// compile here, or is too large), which the engine then translates itself.
func (l *Library) serveCompiled(w http.ResponseWriter, r *http.Request, file string, st os.FileInfo, cacheControl string, head bool) bool {
	return l.serveCompiledFrom(w, r, file, file, st, cacheControl, head, func() ([]byte, error) { return os.ReadFile(file) })
}

// serveCompiledFrom is serveCompiled with the source read by load and kept under key: the public path reads only from
// the handle it opened below the project's root, in a namespace of its own.
func (l *Library) serveCompiledFrom(w http.ResponseWriter, r *http.Request, key, file string, st os.FileInfo, cacheControl string, head bool, load func() ([]byte, error)) bool {
	l.mu.Lock()
	if l.srv == nil {
		l.srv = &server{}
	}
	s := l.srv
	l.mu.Unlock()
	mode, code, err := s.compiled.get(key, filepath.Base(file), st, load)
	if err != nil || mode == compileNone {
		return false
	}
	etag := `W/"` + FileVersion(st) + `-` + mode + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Last-Modified", st.ModTime().UTC().Format(http.TimeFormat))
	h.Set("Cache-Control", cacheControl)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set(compiledHeader, mode)
	h.Add("Vary", "Accept-Encoding")
	if inm, ok := r.Header["If-None-Match"]; ok && strings.Join(inm, ", ") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	body := code
	if len(code) >= gzipMinBytes && takesGzip(r) {
		if packed, err := s.gz.getWith("compiled\n"+key, st, func() ([]byte, error) { return code, nil }); err == nil {
			body = packed
			h.Set("Content-Encoding", "gzip")
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if !head {
		_, _ = w.Write(body)
	}
	return true
}
