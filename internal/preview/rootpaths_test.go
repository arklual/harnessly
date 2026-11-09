package preview

import (
	"bytes"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const testPrefix = "/agents/__pv/1"

func TestHTMLRootPaths(t *testing.T) {
	in := `<!doctype html><html><head><base href="/"><link rel="stylesheet" href="/src/app.css">` +
		`<link rel="icon" href='/favicon.ico'><script type="module" src="/@vite/client"></script>` +
		`<script type="module">import RefreshRuntime from "/@react-refresh"; const s = 'src="/not-an-attr"';</script>` +
		`<style>body{background:url(/bg.png)}</style>` +
		`<meta http-equiv="refresh" content="5; url=/next">` +
		`</head><body><img srcset="/a.png 1x, /b.png 2x" src=/c.png>` +
		`<a href="//cdn.example/x">cdn</a><a href="https://example.com/">abs</a><a href="rel/x">rel</a>` +
		`<a href="/agents/__pv/1/done">done</a><form action="/login"></form></body></html>`
	got := string(RewriteHTMLRootPaths([]byte(in), testPrefix))
	for _, want := range []string{
		`<base href="/agents/__pv/1/">`, `href="/agents/__pv/1/src/app.css"`, `href='/agents/__pv/1/favicon.ico'`,
		`src="/agents/__pv/1/@vite/client"`, `from "/agents/__pv/1/@react-refresh"`, `'src="/not-an-attr"'`,
		`url(/agents/__pv/1/bg.png)`, `url=/agents/__pv/1/next`, `srcset="/agents/__pv/1/a.png 1x, /agents/__pv/1/b.png 2x"`,
		`src=/agents/__pv/1/c.png`, `href="//cdn.example/x"`, `href="https://example.com/"`, `href="rel/x"`,
		`href="/agents/__pv/1/done"`, `action="/agents/__pv/1/login"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "/agents/__pv/1/agents/__pv/1") {
		t.Fatalf("prefixed twice: %s", got)
	}
}

// A document by path: the bridge takes the prefix off its URL for the routers, so its relative addresses are resolved
// against the path it opens at and prefixed, as they would be under the prefixed URL (GitHub #44).
func TestHTMLDocumentRelativePaths(t *testing.T) {
	in := `<!doctype html><html><head><link rel="stylesheet" href="./style.css"><link rel=stylesheet href=theme.css>` +
		`<script src="app.js"></script><script>const s = 'src="x.js"'; document.write('<img src="w.png">');</script>` +
		`<style>body{background:url(img/bg.png)} i{background:url("#f")} b{background:url(data:image/png;base64,AA)}</style>` +
		`</head><body style="background:url('a/b.png')"><img srcset="s1.png 1x, /s2.png 2x" src='../../../up.png'><img srcset="data:image/png;base64,AA,BB 1x,s3.png">` +
		`<a href="#top">top</a><a href="?page=2">next</a><a href="mailto:a@b.c">mail</a><a href="other.html">o</a>` +
		`<a href="{{ link }}">tpl</a><a href="https://example.com/x">ext</a><a href="//cdn.example/x">cdn</a>` +
		`<object data="clip.svg"></object><p>Write href="page.html" to link</p><img src="/agents/__pv/1/done.png"></body></html>`
	got := string(RewriteHTMLDocumentPaths([]byte(in), testPrefix, "/demo/index.html?x=1"))
	for _, want := range []string{
		`href="/agents/__pv/1/demo/style.css"`, `href=/agents/__pv/1/demo/theme.css`, `src="/agents/__pv/1/demo/app.js"`,
		`'src="x.js"'`, `'<img src="w.png">'`, `url(/agents/__pv/1/demo/img/bg.png)`, `url("#f")`,
		`url(data:image/png;base64,AA)`, `style="background:url('/agents/__pv/1/demo/a/b.png')"`,
		`srcset="/agents/__pv/1/demo/s1.png 1x, /agents/__pv/1/s2.png 2x"`,
		`srcset="data:image/png;base64,AA,BB 1x,/agents/__pv/1/demo/s3.png"`, `src='/agents/__pv/1/up.png'`,
		`href="#top"`, `href="/agents/__pv/1/demo/index.html?page=2"`, `href="mailto:a@b.c"`,
		`href="/agents/__pv/1/demo/other.html"`, `href="{{ link }}"`, `href="https://example.com/x"`,
		`href="//cdn.example/x"`, `data="clip.svg"`, `Write href="page.html" to link`, `src="/agents/__pv/1/done.png"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "/agents/__pv/1/agents/__pv/1") {
		t.Fatalf("prefixed twice: %s", got)
	}

	// A nested route: relative to its own directory.
	if got := string(RewriteHTMLDocumentPaths([]byte(`<img src="a.png">`), testPrefix, "/users/7/edit")); got != `<img src="/agents/__pv/1/users/7/a.png">` {
		t.Fatalf("nested: %s", got)
	}
	// A fragment of HTML (no document path) keeps its relative addresses: the page it lands in resolves them.
	if got := string(RewriteHTMLRootPaths([]byte(`<img src="a.png"><img src="/b.png">`), testPrefix)); got != `<img src="a.png"><img src="/agents/__pv/1/b.png">` {
		t.Fatalf("fragment: %s", got)
	}
}

// A document path that would read as a host, or an address with a backslash, resolves nothing: the page's relative
// addresses stay its own, never another host's.
func TestHTMLDocumentPathIsAPath(t *testing.T) {
	for _, docPath := range []string{"//evil.example/x/index.html", "/\\evil.example/x", "evil.example/x", ""} {
		if got := string(RewriteHTMLDocumentPaths([]byte(`<img src="a.png">`), testPrefix, docPath)); got != `<img src="a.png">` {
			t.Errorf("%q: %s", docPath, got)
		}
	}
	got := string(RewriteHTMLDocumentPaths([]byte(`<img src="a\\..\\..\\x.png"><img src="b.png">`), testPrefix, "/demo/index.html"))
	if got != `<img src="a\\..\\..\\x.png"><img src="/agents/__pv/1/demo/b.png">` {
		t.Errorf("backslash: %s", got)
	}
}

// A page with a <base href> of its own: the base gets resolved and prefixed, everything else is the browser's.
func TestHTMLDocumentExplicitBase(t *testing.T) {
	for base, want := range map[string]string{
		`/app/`:                `/agents/__pv/1/app/`,
		`sub/`:                 `/agents/__pv/1/demo/sub/`,
		`https://cdn.example/`: `https://cdn.example/`,
	} {
		in := `<head><base href="` + base + `"><link rel="stylesheet" href="style.css"></head>`
		got := string(RewriteHTMLDocumentPaths([]byte(in), testPrefix, "/demo/index.html"))
		if !strings.Contains(got, `<base href="`+want+`">`) || !strings.Contains(got, `href="style.css"`) {
			t.Errorf("base %s: %s", base, got)
		}
	}
}

func TestCSSRootPaths(t *testing.T) {
	in := `@import "/base.css"; @import url('/theme.css'); a{background:url( "/x.png" )} b{background:url(data:image/png;base64,AA)} c{src:url(//cdn/f.woff2)}`
	got := string(rewriteCSS([]byte(in), testPrefix))
	for _, want := range []string{`@import "/agents/__pv/1/base.css"`, `url('/agents/__pv/1/theme.css')`,
		`url( "/agents/__pv/1/x.png" )`, `url(data:image/png;base64,AA)`, `url(//cdn/f.woff2)`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestRootPathRewriterChunks(t *testing.T) {
	in := `<script src="/main.js"></script>`
	var out bytes.Buffer
	rw := newRootPathRewriter(&out, "html", testPrefix)
	for i := range in {
		_, _ = rw.Write([]byte{in[i]})
	}
	_ = rw.Close()
	if out.String() != `<script src="/agents/__pv/1/main.js"></script>` {
		t.Fatalf("chunks: %s", out.String())
	}
}

func TestSetCookiePathScoped(t *testing.T) {
	got := pathSetCookie("sid=abc; Path=/api; HttpOnly; SameSite=Lax; Domain=localhost", "t1", testPrefix)
	if got != "__t_t1_sid=abc; HttpOnly; Path=/agents/__pv/1/api; Secure; SameSite=None" {
		t.Fatalf("cookie: %s", got)
	}
	if got := pathSetCookie("a=1", "t1", testPrefix); got != "__t_t1_a=1; Path=/agents/__pv/1/; Secure; SameSite=None" {
		t.Fatalf("default path: %s", got)
	}
}

func TestClaimPathPrefix(t *testing.T) {
	obj := func(pairs ...any) *jsvalue.Object {
		o := jsvalue.NewObject()
		for i := 0; i < len(pairs); i += 2 {
			o.Set(pairs[i].(string), pairs[i+1])
		}
		return o
	}
	claim := func(slot, prefix string) *jsvalue.Object {
		o := obj("sub", "u", "target", obj("kind", "port", "port", 5173.0), "slotOrigin", slot,
			"portSlots", obj("5173", slot))
		if prefix != "" {
			o.Set("pathPrefix", prefix)
		}
		return o
	}
	k := "-0123456789abcdef0123456789abcdef"
	if c, ok := parseClaim(claim("https://h.example/agents/__pv/1"+k, "/agents/__pv/1"+k), true); !ok || c.PathPrefix != "/agents/__pv/1"+k {
		t.Fatalf("a slot by path: %+v %v", c, ok)
	}
	for name, o := range map[string]*jsvalue.Object{
		"prefix not at the end of the origin": claim("https://h.example/agents/__pv/1"+k, "/agents/__pv/2"+k),
		"path without a prefix":               claim("https://h.example/agents/__pv/1"+k, ""),
		"a slot past 6":                       claim("https://h.example/__pv/7"+k, "/__pv/7"+k),
		"no key":                              claim("https://h.example/agents/__pv/1", "/agents/__pv/1"),
	} {
		if _, ok := parseClaim(o, true); ok {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, ok := parseClaim(claim("https://h.example/agents/__pv/1"+k, "/agents/__pv/1"+k), false); ok {
		t.Error("only a standalone runtime's own claim goes by path")
	}
}

func TestCarriedClaimOpensOnlyTheViewServer(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	claim := func(port int, carry string) string {
		raw := `{"sub":"u","target":{"kind":"port","port":` + strconv.Itoa(port) + `},"slotOrigin":"https://s.example",` +
			`"portSlots":{"` + strconv.Itoa(port) + `":"https://s.example"}` + carry + `}`
		return signClaim(secret, []byte(raw))
	}
	p := &Proxy{cfg: Config{ClaimHeader: "x-claim", Audience: "acme-preview-proxy", Secret: secret, CarriedPort: func() int { return 4300 }}}
	admit := func(token string) admission {
		r := httptest.NewRequest("GET", "https://s.example/x.js", nil)
		r.Header.Set("x-claim", token)
		return p.admit(r)
	}
	for name, token := range map[string]string{
		"a dev server by a link": claim(5173, `,"carry":"path"`),
		"an unknown carry":       claim(4300, `,"carry":"cookie"`),
	} {
		if a := admit(token); a.claim != nil || a.status == 0 {
			t.Errorf("%s: admitted", name)
		}
	}
	if c := verifyClaim(claim(4300, `,"carry":"path"`), secret, "acme-preview-proxy", time.Now(), false); c == nil || !c.Carried {
		t.Fatalf("the view server's link: %+v", c)
	}
	neighbour := signClaim(secret, []byte(`{"sub":"u","target":{"kind":"port","port":4300},"slotOrigin":"https://s.example",`+
		`"portSlots":{"4300":"https://s.example","5173":"https://s.example/__tk/p/5173"},"carry":"path"}`))
	r := httptest.NewRequest("GET", "/__tk/p/5173/x.js", nil)
	r.Header.Set("x-claim", neighbour)
	if a := p.admit(r); a.status != 403 {
		t.Errorf("a carried session to a neighbour port: %d", a.status)
	}
	p.cfg.CarriedPort = nil
	if a := admit(claim(4300, `,"carry":"path"`)); a.status != 403 {
		t.Errorf("no view server: %d", a.status)
	}
}
