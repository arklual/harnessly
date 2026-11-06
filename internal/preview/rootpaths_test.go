package preview

import (
	"bytes"
	"strings"
	"testing"

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
