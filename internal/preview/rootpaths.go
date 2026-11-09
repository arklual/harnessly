package preview

import (
	"bytes"
	"io"
	"net/url"
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
	// htmlTag is a start tag; htmlRelAttr an attribute that holds an address a document may give relatively; htmlBase
	// the document's <base> with an href; htmlStyle a style block, htmlStyleAttr a style attribute.
	htmlTag       = regexp.MustCompile(`<[a-zA-Z][^\s/>]*(?:\s+[^\s"'>/=]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'>]+))?|\s*/)*\s*>`)
	htmlRelAttr   = regexp.MustCompile(`(?i)(\s(?:src|href|action|formaction|poster|background|manifest)\s*=\s*)("[^"]*"|'[^']*'|[^\s"'>]+)`)
	htmlBase      = regexp.MustCompile(`(?i)<base\b[^>]*?\shref\s*=\s*("[^"]*"|'[^']*'|[^\s"'>]+)`)
	htmlStyle     = regexp.MustCompile(`(?is)(<style\b[^>]*>)(.*?)(</style\s*>)`)
	htmlStyleAttr = regexp.MustCompile(`(?i)(\sstyle\s*=\s*)("[^"]*"|'[^']*')`)
	// urlScheme starts an address with a scheme of its own (https:, data:, mailto:, javascript:…).
	urlScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
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

// pathRewriter gives an address of a document the prefix. A document's relative addresses resolve against its own
// path (docPath) once the bridge has taken the prefix off the document's URL (routers read location.pathname), so
// they get resolved here, against the path the page believes it has, and prefixed: `./style.css` of /demo/ is
// <prefix>/demo/style.css, as it would be under the prefixed URL. A fragment (`#top`), an address with a scheme, a
// template's placeholder stay; docPath "" (a style, a fragment of HTML a script fetches) leaves relative ones alone.
type pathRewriter struct {
	prefix  string
	docPath *url.URL
}

func (p pathRewriter) fix(addr string) string {
	if p.docPath == nil || addr == "" || strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "#") ||
		strings.HasPrefix(addr, "\\") || urlScheme.MatchString(addr) || strings.ContainsAny(addr, "{}<>$") {
		return prefixRoot(addr, p.prefix)
	}
	if strings.Contains(addr, "\\") {
		return addr // a browser reads `\` as `/`, net/url does not
	}
	ref, err := url.Parse(addr)
	if err != nil || ref.Scheme != "" || ref.Host != "" {
		return addr
	}
	// ResolveReference drops the dot segments at the page's root: `../../x` of /demo/ stays under the prefix.
	out := p.docPath.ResolveReference(ref)
	if out.Scheme != "" || out.Host != "" || !strings.HasPrefix(out.Path, "/") || strings.HasPrefix(out.String(), "//") {
		return addr
	}
	return prefixRoot(out.String(), p.prefix)
}

// documentURL is the path a document opens at (with its query) as a URL with neither scheme nor host: a path that
// would read as one (`//host/x`, `/\host`) gives none, and the document's relative addresses stay as they are.
func documentURL(docPath string) *url.URL {
	path, _, _ := strings.Cut(docPath, "?")
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "\\") {
		return nil
	}
	u, err := url.Parse(docPath)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return nil
	}
	u.Fragment, u.RawFragment = "", ""
	return u
}

// quoted is fix on a value that may come in its quotes.
func (p pathRewriter) quoted(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		inner := v[1 : len(v)-1]
		trimmed := strings.TrimLeft(inner, " \t\n\r")
		lead := inner[:len(inner)-len(trimmed)]
		return string(v[0]) + lead + p.fix(trimmed) + string(v[0])
	}
	return p.fix(v)
}

// srcset is fix on each candidate's address of a quoted srcset, read as the browser reads one: an address is a run
// without spaces (a data: address keeps its commas), its descriptors go up to the next comma.
func (p pathRewriter) srcset(v string) string {
	inner := v[1 : len(v)-1]
	var b strings.Builder
	b.WriteByte(v[0])
	for i := 0; i < len(inner); {
		j := i
		for j < len(inner) && (isSrcsetSpace(inner[j]) || inner[j] == ',') {
			j++
		}
		b.WriteString(inner[i:j])
		if i = j; i >= len(inner) {
			break
		}
		for j < len(inner) && !isSrcsetSpace(inner[j]) {
			j++
		}
		addr := strings.TrimRight(inner[i:j], ",")
		b.WriteString(p.fix(addr))
		b.WriteString(inner[i+len(addr) : j])
		if i = j; strings.HasSuffix(inner[:j], ",") {
			continue
		}
		for depth := 0; j < len(inner) && (inner[j] != ',' || depth > 0); j++ {
			switch inner[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		b.WriteString(inner[i:j])
		i = j
	}
	b.WriteByte(v[0])
	return b.String()
}

func isSrcsetSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// prefixQuoted is prefixRoot on a value that may come in its quotes.
func prefixQuoted(v, prefix string) string { return pathRewriter{prefix: prefix}.quoted(v) }

// RewriteHTMLRootPaths gives a document's root-absolute addresses the prefix, inline scripts' imports included.
func RewriteHTMLRootPaths(doc []byte, prefix string) []byte {
	return RewriteHTMLDocumentPaths(doc, prefix, "")
}

// RewriteHTMLDocumentPaths is RewriteHTMLRootPaths for a page the browser opens at docPath (the path without the
// prefix): its relative addresses get the prefix too. A page with a <base href> of its own resolves them against that
// base, which gets the prefix here instead; the rest is then the browser's.
func RewriteHTMLDocumentPaths(doc []byte, prefix, docPath string) []byte {
	rel := pathRewriter{prefix: prefix}
	if docPath != "" {
		rel.docPath = documentURL(docPath)
	}
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
	if rel.docPath != nil {
		doc = rel.relative(doc)
	}
	doc = htmlURLAttr.ReplaceAllFunc(doc, func(m []byte) []byte {
		g := htmlURLAttr.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), prefixQuoted(string(g[2]), prefix)...)
	})
	doc = htmlSrcset.ReplaceAllFunc(doc, func(m []byte) []byte {
		g := htmlSrcset.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), pathRewriter{prefix: prefix}.srcset(string(g[2]))...)
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

// relative resolves the relative addresses of a document's tags and style blocks (scripts already set aside), and
// only there: `href="x"` in the text of a page about HTML stays as written. A document with a <base href> of its own
// resolves them against that base, which alone gets resolved and prefixed.
func (p pathRewriter) relative(doc []byte) []byte {
	if loc := htmlBase.FindSubmatchIndex(doc); loc != nil {
		value := p.quoted(string(doc[loc[2]:loc[3]]))
		return append(append(append([]byte{}, doc[:loc[2]]...), value...), doc[loc[3]:]...)
	}
	doc = htmlTag.ReplaceAllFunc(doc, func(tag []byte) []byte {
		tag = htmlRelAttr.ReplaceAllFunc(tag, func(m []byte) []byte {
			g := htmlRelAttr.FindSubmatch(m)
			return append(append([]byte{}, g[1]...), p.quoted(string(g[2]))...)
		})
		tag = htmlSrcset.ReplaceAllFunc(tag, func(m []byte) []byte {
			g := htmlSrcset.FindSubmatch(m)
			return append(append([]byte{}, g[1]...), p.srcset(string(g[2]))...)
		})
		return htmlStyleAttr.ReplaceAllFunc(tag, func(m []byte) []byte {
			g := htmlStyleAttr.FindSubmatch(m)
			return append(append([]byte{}, g[1]...), p.css(g[2])...)
		})
	})
	return htmlStyle.ReplaceAllFunc(doc, func(m []byte) []byte {
		g := htmlStyle.FindSubmatch(m)
		return append(append(append([]byte{}, g[1]...), p.css(g[2])...), g[3]...)
	})
}

// rewriteCSS gives url() and @import of a style (or of a document's style attributes and blocks) the prefix.
func rewriteCSS(text []byte, prefix string) []byte { return pathRewriter{prefix: prefix}.css(text) }

func (p pathRewriter) css(text []byte) []byte {
	text = cssURL.ReplaceAllFunc(text, func(m []byte) []byte {
		g := cssURL.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), p.quoted(string(g[2]))...)
	})
	return cssImport.ReplaceAllFunc(text, func(m []byte) []byte {
		g := cssImport.FindSubmatch(m)
		return append(append([]byte{}, g[1]...), p.quoted(string(g[2]))...)
	})
}

// rootPathRewriter holds a document or a style whole, rewrites it on Close and writes it on.
type rootPathRewriter struct {
	w       io.Writer
	html    bool
	prefix  string
	docPath string // the path a document opens at, for its relative addresses; "" for anything else
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
		out = RewriteHTMLDocumentPaths(r.buf.Bytes(), r.prefix, r.docPath)
	} else {
		out = rewriteCSS(r.buf.Bytes(), r.prefix)
	}
	_, err := r.w.Write(out)
	return err
}
