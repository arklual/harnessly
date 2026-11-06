package preview

import (
	"bytes"
	"io"
	"regexp"
	"strings"
)

// A slot by path (the runtime's /__pv/<n>/ on the page's own origin) serves its target under a prefix, but the target
// believes it is the root of localhost: its documents and styles name their files by root-absolute paths
// (`<script src="/@vite/client">`, `<base href="/">`, `url(/fonts/a.woff2)`), which would leave the slot. They get the
// prefix here; JavaScript goes through moduleRewriter, addresses built at run time through the page bridge.

// rootPathMaxBody bounds what is held to rewrite: a larger document or style passes as it came.
const rootPathMaxBody = 16 << 20

var (
	cssType = regexp.MustCompile(`(?i)^text/css\b`)
	// htmlURLAttr is an attribute that holds one address, quoted or not.
	htmlURLAttr = regexp.MustCompile(`(?i)(\s(?:src|href|action|formaction|poster|data|background|manifest)\s*=\s*)("[^"]*"|'[^']*'|[^\s"'>]+)`)
	htmlSrcset  = regexp.MustCompile(`(?i)(\s(?:srcset|imagesrcset)\s*=\s*)("[^"]*"|'[^']*')`)
	// htmlRefresh is <meta http-equiv="refresh" content="0; url=/x">.
	htmlRefresh = regexp.MustCompile(`(?i)(content\s*=\s*["']\s*\d+\s*;\s*url\s*=\s*)(/[^"'\s]*)`)
	// htmlScript is an inline script's text (no src): its imports are module specifiers.
	htmlScript = regexp.MustCompile(`(?is)(<script\b[^>]*>)(.*?)(</script\s*>)`)
	cssURL     = regexp.MustCompile(`(?i)(url\(\s*)("[^"]*"|'[^']*'|[^)\s]*)`)
	cssImport  = regexp.MustCompile(`(?i)(@import\s+)("[^"]*"|'[^']*')`)
)

// prefixRoot puts the prefix before a root-absolute address: not before `//host`, a full URL, a relative one, or one
// that already has the prefix.
func prefixRoot(addr, prefix string) string {
	if !strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "//") || strings.HasPrefix(addr, "/\\") {
		return addr
	}
	if addr == prefix || strings.HasPrefix(addr, prefix+"/") || strings.HasPrefix(addr, prefix+"?") {
		return addr
	}
	return prefix + addr
}

// prefixQuoted is prefixRoot on a value that may come in its quotes.
func prefixQuoted(v, prefix string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		inner := v[1 : len(v)-1]
		trimmed := strings.TrimLeft(inner, " \t\n\r")
		lead := inner[:len(inner)-len(trimmed)]
		return string(v[0]) + lead + prefixRoot(trimmed, prefix) + string(v[0])
	}
	return prefixRoot(v, prefix)
}

func prefixSrcset(v, prefix string) string {
	q := v[0]
	items := strings.Split(v[1:len(v)-1], ",")
	for i, item := range items {
		trimmed := strings.TrimLeft(item, " \t\n\r")
		items[i] = item[:len(item)-len(trimmed)] + prefixRoot(trimmed, prefix)
	}
	return string(q) + strings.Join(items, ",") + string(q)
}

// RewriteHTMLRootPaths gives a document's root-absolute addresses the prefix, inline scripts' imports included.
func RewriteHTMLRootPaths(doc []byte, prefix string) []byte {
	// Scripts first, and their text set aside, so an attribute-like string inside a script is never touched.
	var scripts [][]byte
	doc = htmlScript.ReplaceAllFunc(doc, func(m []byte) []byte {
		g := htmlScript.FindSubmatch(m)
		var code bytes.Buffer
		mr := newModuleRewriter(&code, prefix)
		_, _ = mr.Write(g[2])
		_ = mr.Close()
		scripts = append(scripts, code.Bytes())
		return append(append(append([]byte{}, g[1]...), "\x00hl-script\x00"...), g[3]...)
	})
	doc = htmlURLAttr.ReplaceAllFunc(doc, func(m []byte) []byte {
		g := htmlURLAttr.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), prefixQuoted(string(g[2]), prefix)...)
	})
	doc = htmlSrcset.ReplaceAllFunc(doc, func(m []byte) []byte {
		g := htmlSrcset.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), prefixSrcset(string(g[2]), prefix)...)
	})
	doc = htmlRefresh.ReplaceAllFunc(doc, func(m []byte) []byte {
		g := htmlRefresh.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), prefixRoot(string(g[2]), prefix)...)
	})
	doc = rewriteCSS(doc, prefix)
	for _, code := range scripts {
		doc = bytes.Replace(doc, []byte("\x00hl-script\x00"), code, 1)
	}
	return doc
}

// rewriteCSS gives url() and @import of a style (or of a document's style attributes and blocks) the prefix.
func rewriteCSS(text []byte, prefix string) []byte {
	text = cssURL.ReplaceAllFunc(text, func(m []byte) []byte {
		g := cssURL.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), prefixQuoted(string(g[2]), prefix)...)
	})
	return cssImport.ReplaceAllFunc(text, func(m []byte) []byte {
		g := cssImport.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), prefixQuoted(string(g[2]), prefix)...)
	})
}

// rootPathRewriter holds a document or a style whole, rewrites it on Close and writes it on.
type rootPathRewriter struct {
	w       io.Writer
	html    bool
	prefix  string
	buf     bytes.Buffer
	passing bool
}

// newRootPathRewriter: kind is "html" or "css".
func newRootPathRewriter(w io.Writer, kind, prefix string) *rootPathRewriter {
	return &rootPathRewriter{w: w, html: kind == "html", prefix: prefix}
}

func (r *rootPathRewriter) Write(b []byte) (int, error) {
	if r.passing {
		return r.w.Write(b)
	}
	r.buf.Write(b)
	if r.buf.Len() > rootPathMaxBody {
		r.passing = true
		if _, err := r.w.Write(r.buf.Bytes()); err != nil {
			return 0, err
		}
		r.buf.Reset()
	}
	return len(b), nil
}

func (r *rootPathRewriter) Close() error {
	if r.passing {
		return nil
	}
	var out []byte
	if r.html {
		out = RewriteHTMLRootPaths(r.buf.Bytes(), r.prefix)
	} else {
		out = rewriteCSS(r.buf.Bytes(), r.prefix)
	}
	_, err := r.w.Write(out)
	return err
}
