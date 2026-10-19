package preview

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"

	"harnessly/internal/jscompat"
	"harnessly/internal/preview/devtools"
)

// What the proxy changes so a local dev server works inside a slot origin.
type rewriteCtx struct {
	slotOrigin string
	portSlots  map[string]string
	// extra maps an external target's own origin to where it is served now: the slot.
	extra map[string]string
}

func rewriteCtxOf(c *Claim) rewriteCtx {
	if c.Target.Kind == "url" {
		return rewriteCtx{slotOrigin: c.SlotOrigin, portSlots: map[string]string{}, extra: map[string]string{c.Target.Origin: c.SlotOrigin}}
	}
	return rewriteCtx{slotOrigin: c.SlotOrigin, portSlots: c.PortSlots}
}

const localTail = 32

func isWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

var (
	schemes    = []string{"https://", "http://", "wss://", "ws://"}
	localHosts = []string{"localhost", "127.0.0.1", "0.0.0.0", "[::1]"}
)

// originMatch is one match of the origin pattern: a local origin with its scheme and port, or an extra origin.
type originMatch struct {
	start, end   int
	local        bool
	scheme, port string
}

type originPattern struct {
	hosts []string // the extra origins' hosts
	tail  int
}

func patternOf(ctx rewriteCtx) originPattern {
	p := originPattern{tail: localTail}
	for origin := range ctx.extra {
		host := origin[strings.Index(origin, "://")+3:]
		p.hosts = append(p.hosts, host)
		p.tail = max(p.tail, len(host)+len("https://")+1)
	}
	return p
}

// matchAt is the pattern at i: `\b(https?|wss?)://(localhost|127.0.0.1|0.0.0.0|[::1]):(\d{1,5})(?!\d)` first, then
// `\b(?:https?|wss?)://(?:<extra hosts>)(?![\w.:-])`.
func (p originPattern) matchAt(text string, i int) (originMatch, bool) {
	if i > 0 && isWord(text[i-1]) {
		return originMatch{}, false
	}
	for _, s := range schemes {
		if !strings.HasPrefix(text[i:], s) {
			continue
		}
		after := i + len(s)
		for _, h := range localHosts {
			if !strings.HasPrefix(text[after:], h+":") {
				continue
			}
			d := after + len(h) + 1
			n := 0
			for d+n < len(text) && text[d+n] >= '0' && text[d+n] <= '9' {
				n++
			}
			if n >= 1 && n <= 5 {
				return originMatch{start: i, end: d + n, local: true, scheme: strings.TrimSuffix(s, "://"), port: text[d : d+n]}, true
			}
		}
		for _, h := range p.hosts {
			if !strings.HasPrefix(text[after:], h) {
				continue
			}
			end := after + len(h)
			if end < len(text) && (isWord(text[end]) || strings.IndexByte(".:-", text[end]) >= 0) {
				continue
			}
			return originMatch{start: i, end: end}, true
		}
	}
	return originMatch{}, false
}

// next is the first match at or after i.
func (p originPattern) next(text string, i int) (originMatch, bool) {
	for ; i < len(text); i++ {
		c := text[i]
		if c != 'h' && c != 'w' {
			continue
		}
		if m, ok := p.matchAt(text, i); ok {
			return m, true
		}
	}
	return originMatch{}, false
}

func toWSS(origin string) string {
	if strings.HasPrefix(origin, "https:") {
		return "wss:" + origin[len("https:"):]
	}
	return origin
}

func (ctx rewriteCtx) replacement(text string, m originMatch) string {
	match := text[m.start:m.end]
	if !m.local {
		scheme, rest, _ := strings.Cut(match, "://")
		httpScheme := scheme
		if strings.HasPrefix(scheme, "ws") {
			httpScheme = strings.Replace(scheme, "ws", "http", 1)
		}
		mapped, ok := ctx.extra[httpScheme+"://"+rest]
		if !ok {
			return match
		}
		if strings.HasPrefix(scheme, "ws") {
			return toWSS(mapped)
		}
		return mapped
	}
	origin, ok := ctx.portSlots[m.port]
	if !ok || origin == "" {
		return match
	}
	if strings.HasSuffix(origin, devtools.NeighbourPrefix+m.port) && (m.end >= len(text) || text[m.end] != '/') {
		origin += "/"
	}
	if strings.HasPrefix(m.scheme, "ws") {
		return toWSS(origin)
	}
	return origin
}

func (ctx rewriteCtx) rewriteWith(text string, p originPattern) string {
	var b strings.Builder
	i := 0
	for {
		m, ok := p.next(text, i)
		if !ok {
			b.WriteString(text[i:])
			return b.String()
		}
		b.WriteString(text[i:m.start])
		b.WriteString(ctx.replacement(text, m))
		i = m.end
	}
}

// rewriteLocalOrigins maps local and extra origins in a text to the slot's addresses.
func (ctx rewriteCtx) rewriteLocalOrigins(text string) string {
	return ctx.rewriteWith(text, patternOf(ctx))
}

// originRewriter rewrites a body as it streams. The tail kept between chunks holds the longest possible match plus the
// character after it, which decides a trailing slash.
type originRewriter struct {
	w       io.Writer
	ctx     rewriteCtx
	p       originPattern
	carry   string
	decoder jscompat.UTF8Decoder
}

func newOriginRewriter(w io.Writer, ctx rewriteCtx) *originRewriter {
	return &originRewriter{w: w, ctx: ctx, p: patternOf(ctx)}
}

func (r *originRewriter) Write(b []byte) (int, error) {
	text := r.carry + r.decoder.Decode(b, true)
	// The cut is counted as the previous runtime counted it, in UTF-16 units from the end: the bytes kept between chunks,
	// and so the matches seen at a chunk's edge, are the same. A cut inside a surrogate pair goes before the pair.
	cut, units := len(text), 0
	for cut > 0 && units < r.p.tail {
		c, size := utf8.DecodeLastRuneInString(text[:cut])
		cut -= size
		units++
		if c >= 0x10000 {
			units++
		}
	}
	for i := 0; ; {
		m, ok := r.p.next(text, i)
		if !ok || m.start >= cut {
			break
		}
		if m.end >= cut {
			cut = m.start
			break
		}
		i = m.end
	}
	r.carry = text[cut:]
	if out := r.ctx.rewriteWith(text[:cut], r.p); out != "" {
		if _, err := io.WriteString(r.w, out); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// Close writes what is left.
func (r *originRewriter) Close() error {
	text := r.carry + r.decoder.Decode(nil, false)
	r.carry = ""
	if text == "" {
		return nil
	}
	_, err := io.WriteString(r.w, r.ctx.rewriteWith(text, r.p))
	return err
}

func prefixSetCookie(value, tag string) string {
	parts := strings.Split(value, ";")
	for i := range parts {
		parts[i] = jscompat.Trim(parts[i])
	}
	out := []string{"__t_" + tag + "_" + parts[0]}
	for _, attr := range parts[1:] {
		if attrDropped(attr) {
			continue
		}
		out = append(out, attr)
	}
	return strings.Join(append(out, "Secure", "SameSite=None", "Partitioned"), "; ")
}

// attrDropped: domain, secure, samesite and partitioned attributes, whatever follows the word.
func attrDropped(attr string) bool {
	lower := strings.ToLower(attr)
	for _, w := range []string{"domain", "secure", "samesite", "partitioned"} {
		if strings.HasPrefix(lower, w) && (len(lower) == len(w) || !isWord(lower[len(w)])) {
			return true
		}
	}
	return false
}

// targetCookies is the cookies of one target out of the slot's, their names without the tag.
func targetCookies(header, tag string) string {
	prefix := "__t_" + tag + "_"
	var kept []string
	for _, part := range strings.Split(header, ";") {
		part = jscompat.Trim(part)
		if strings.HasPrefix(part, prefix) {
			kept = append(kept, part[len(prefix):])
		}
	}
	return strings.Join(kept, "; ")
}

func (s Stands) isSessionSetCookie(line string) bool {
	name, _, _ := strings.Cut(strings.Split(line, ";")[0], "=")
	return isSessionCookie(jscompat.Trim(name))
}

// standCookie is the Cookie of a request to a stand: the person's sign-in from the gateway's header first, then the
// stand's own cookies. A page can plant a cookie named like the session on the slot host; the person's wins.
func standCookie(own, sessionHeader string) string {
	parts := []string{}
	if s := sessionCookies(sessionHeader); s != "" {
		parts = append(parts, s)
	}
	for _, pair := range strings.Split(own, "; ") {
		if pair == "" {
			continue
		}
		name := pair
		if i := strings.IndexByte(pair, '='); i >= 0 {
			name = pair[:i]
		} else {
			name = pair[:len(pair)-1]
		}
		if isSessionCookie(name) {
			continue
		}
		parts = append(parts, pair)
	}
	return strings.Join(parts, "; ")
}

func stripFrameAncestors(policy string) string {
	var kept []string
	for _, part := range strings.Split(policy, ";") {
		part = jscompat.Trim(part)
		lower := strings.ToLower(part)
		if part == "" || strings.HasPrefix(lower, "frame-ancestors") && (len(lower) == len("frame-ancestors") || !isWord(lower[len("frame-ancestors")])) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "; ")
}

func (ctx rewriteCtx) rewriteLocation(location, basePath string) string {
	if basePath != "" && strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "//") {
		return basePath + location
	}
	return ctx.rewriteLocalOrigins(location)
}

type headerOptions struct {
	bodyRewritten bool
	basePath      string
	cookieTag     string
	stand         bool
}

func (ctx rewriteCtx) rewriteResponseHeaders(h Headers, stands Stands, o headerOptions) Headers {
	out := h.Clone()
	out.Del("x-frame-options")
	for _, name := range []string{"content-security-policy", "content-security-policy-report-only"} {
		if !h.Has(name) {
			continue
		}
		kept := stripFrameAncestors(h.Get(name))
		switch {
		case kept == "":
			out.Del(name)
		case ctx.extra != nil:
			out.Set(name, ctx.rewriteLocalOrigins(kept))
		default:
			out.Set(name, kept)
		}
	}
	if ctx.extra != nil {
		out.Del("clear-site-data")
	}
	if h.Has("location") {
		out.Set("location", ctx.rewriteLocation(h.Get("location"), o.basePath))
	}
	if lines := h["set-cookie"]; len(lines) > 0 {
		var kept []string
		for _, line := range lines {
			if o.stand && stands.isSessionSetCookie(line) {
				continue
			}
			kept = append(kept, prefixSetCookie(line, o.cookieTag))
		}
		if len(kept) > 0 {
			out["set-cookie"] = kept
		} else {
			out.Del("set-cookie")
		}
	}
	if o.bodyRewritten {
		out.Del("content-length")
	}
	out.Set("x-robots-tag", "noindex, nofollow")
	return out
}

// bridgeInjector puts the tag before any element of the document: right after the doctype (so the page keeps standards
// mode), or after what may lead it (a byte order mark, whitespace, comments). Bytes are searched one by one, so a
// multibyte character split across chunks never matters.
type bridgeInjector struct {
	w       io.Writer
	tag     []byte
	pending []byte
	done    bool
}

const leadWindow = 64 * 1024

func isLeadSpace(c byte) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0:
		return true
	}
	return false
}

func leadOf(b []byte) int {
	i := 0
	if bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}) {
		i = 3
	}
	for i < len(b) {
		if isLeadSpace(b[i]) {
			i++
			continue
		}
		if bytes.HasPrefix(b[i:], []byte("<!--")) {
			end := bytes.Index(b[i+4:], []byte("-->"))
			if end < 0 {
				break
			}
			i += 4 + end + 3
			continue
		}
		break
	}
	return i
}

func lowerASCII(b []byte) string { return strings.ToLower(string(b)) }

// doctypeAt is the length of `<!doctype[^>]*>` at the start, 0 when there is none.
func doctypeAt(b []byte) int {
	if len(b) < 9 || lowerASCII(b[:9]) != "<!doctype" {
		return 0
	}
	end := bytes.IndexByte(b[9:], '>')
	if end < 0 {
		return 0
	}
	return 9 + end + 1
}

// undecided: the rest could still turn into a doctype or a comment once more bytes arrive.
func undecided(rest []byte) bool {
	lower := lowerASCII(rest)
	if lower == "" {
		return true
	}
	if strings.HasPrefix("<!doctype", lower) || strings.HasPrefix(lower, "<!doctype") && !strings.Contains(lower, ">") {
		return true
	}
	return strings.HasPrefix("<!--", lower) || strings.HasPrefix(lower, "<!--") && !strings.Contains(lower, "-->")
}

func (j *bridgeInjector) place(final bool) []byte {
	b := j.pending
	lead := leadOf(b)
	var at int
	if n := doctypeAt(b[lead:]); n > 0 {
		at = lead + n
	} else if !final && len(b) < leadWindow && undecided(b[lead:]) {
		return nil
	} else if len(b) >= leadWindow && !final {
		at = 0
	} else {
		at = lead
	}
	j.done = true
	j.pending = nil
	out := make([]byte, 0, len(b)+len(j.tag))
	out = append(out, b[:at]...)
	out = append(out, j.tag...)
	return append(out, b[at:]...)
}

func (j *bridgeInjector) Write(b []byte) (int, error) {
	if j.done {
		return j.w.Write(b)
	}
	j.pending = append(j.pending, b...)
	if out := j.place(false); out != nil {
		if _, err := j.w.Write(out); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

func (j *bridgeInjector) Close() error {
	if j.done || len(j.pending) == 0 {
		return nil
	}
	_, err := j.w.Write(j.place(true))
	return err
}
