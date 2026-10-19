package share

import (
	"strings"
	"unicode"
	"unicode/utf8"

	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// GFM autolink literals as the previous runtime's parser found them: micromark's tokenizer (www., http(s)://, e-mail
// addresses where a construct may start) and then mdast's pass over the remaining text. Both are reproduced; goldmark's
// own linkify differs in where it looks.

// literalLink is an autolink literal found while parsing.
type literalLink struct {
	gast.BaseInline
	url, label string
}

var kindLiteralLink = gast.NewNodeKind("LiteralLink")

func (n *literalLink) Kind() gast.NodeKind { return kindLiteralLink }

func (n *literalLink) Dump(source []byte, level int) { gast.DumpHelper(n, source, level, nil, nil) }

// literalParser runs where goldmark tries inline parsers: at the start of a line (a literal starting there), and at
// a space or a punctuation mark that starts no other construct (a literal right after it). A literal after an
// emphasis or strike delimiter, a bracket or a backtick is left to the pass over text, so those keep their meaning.
type literalParser struct{ triggers string }

func (p literalParser) Trigger() []byte { return []byte(p.triggers) }

// codes is the rest of a paragraph's text as micromark sees it: code points, -4 for a line ending, end for its end.
const (
	codeEOL = -4
	codeTab = -2
	codeEnd = -100
)

type codeReader struct {
	runes []rune
}

func (c codeReader) at(i int) int {
	if i >= len(c.runes) {
		return codeEnd
	}
	switch r := c.runes[i]; r {
	case '\n', '\r':
		return codeEOL
	case '\t':
		return codeTab
	default:
		return int(r)
	}
}

func lineEndingOrSpace(c int) bool { return c != codeEnd && (c < 0 || c == ' ') }
func unicodeWS(c int) bool         { return c >= 0 && unicodeSpace(rune(c)) }
func unicodeSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// unicodePunct is micromark's punctuation test of one UTF-16 unit: a character past the BMP (a surrogate there) is not.
func unicodePunct(c int) bool {
	return c >= 0 && c <= 0xffff && !(c >= 0xd800 && c < 0xe000) && (unicode.IsPunct(rune(c)) || unicode.IsSymbol(rune(c)))
}
func asciiAlphaC(c int) bool     { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func asciiAlnumC(c int) bool     { return asciiAlphaC(c) || c >= '0' && c <= '9' }
func gfmAtext(c int) bool        { return c == '+' || c == '-' || c == '.' || c == '_' || asciiAlnumC(c) }
func asciiControl(c int) bool    { return c != codeEnd && (c < 32 || c == 127) }
func trailMarker(c int) bool     { return strings.ContainsRune(`!"')*,.:;?_~`, rune(c)) && c > 0 }
func pathTrailMarker(c int) bool { return c > 0 && strings.ContainsRune(`!"&')*,.:;<?]_~`, rune(c)) }

// trailAt: from i, trailing punctuation up to an end, so the link stops before it.
func trailAt(c codeReader, i int) bool {
	for {
		code := c.at(i)
		switch {
		case trailMarker(code):
			i++
		case code == '&':
			i++
			if !asciiAlphaC(c.at(i)) {
				return false
			}
			for asciiAlphaC(c.at(i)) {
				i++
			}
			if c.at(i) != ';' {
				return false
			}
			i++
		case code == ']':
			i++
			n := c.at(i)
			if n == codeEnd || n == '(' || n == '[' || lineEndingOrSpace(n) || unicodeWS(n) {
				return true
			}
		case code == '<' || code == codeEnd || lineEndingOrSpace(code) || unicodeWS(code):
			return true
		default:
			return false
		}
	}
}

// domainEnd is where a domain starting at i ends; false when there is none.
func domainEnd(c codeReader, i int) (int, bool) {
	var underscoreLast, underscoreLastLast, seen bool
	for {
		code := c.at(i)
		if code == '.' || code == '_' {
			if trailAt(c, i) {
				break
			}
			if code == '_' {
				underscoreLast = true
			} else {
				underscoreLastLast, underscoreLast = underscoreLast, false
			}
			i++
			continue
		}
		if code == codeEnd || lineEndingOrSpace(code) || unicodeWS(code) || (code != '-' && unicodePunct(code)) {
			break
		}
		seen = true
		i++
	}
	if underscoreLastLast || underscoreLast || !seen {
		return 0, false
	}
	return i, true
}

func pathEnd(c codeReader, i int) int {
	open, closed := 0, 0
	for {
		code := c.at(i)
		switch {
		case code == '(':
			open++
			i++
		case code == ')' && closed < open:
			closed++
			i++
		case pathTrailMarker(code):
			if trailAt(c, i) {
				return i
			}
			if code == ')' {
				closed++
			}
			i++
		case code == codeEnd || lineEndingOrSpace(code) || unicodeWS(code):
			return i
		default:
			i++
		}
	}
}

func previousWww(p int) bool {
	return p == codeEnd || p == '(' || p == '*' || p == '_' || p == '[' || p == ']' || p == '~' || lineEndingOrSpace(p)
}

// literalAt is the length (in code points) and kind of an autolink literal starting at the reader's start.
func literalAt(c codeReader, prev int) (int, string) {
	first := c.at(0)
	if gfmAtext(first) && !(prev == '/' || gfmAtext(prev)) {
		if n := emailAt(c); n > 0 {
			return n, "email"
		}
	}
	if (first == 'h' || first == 'H') && !asciiAlphaC(prev) {
		if n := protocolAt(c); n > 0 {
			return n, "http"
		}
	}
	if (first == 'w' || first == 'W') && previousWww(prev) {
		ok := true
		for i := 0; i < 3; i++ {
			if code := c.at(i); code != 'w' && code != 'W' {
				ok = false
			}
		}
		if ok && c.at(3) == '.' && c.at(4) != codeEnd {
			if end, ok := domainEnd(c, 0); ok {
				return pathEnd(c, end), "www"
			}
		}
	}
	return 0, ""
}

func emailAt(c codeReader) int {
	i := 0
	for gfmAtext(c.at(i)) {
		i++
	}
	if c.at(i) != '@' {
		return 0
	}
	i++
	var data, dot bool
	for {
		code := c.at(i)
		if code == '.' {
			if !asciiAlnumC(c.at(i + 1)) {
				break
			}
			dot = true
			i++
			continue
		}
		if code == '-' || code == '_' || asciiAlnumC(code) {
			data = true
			i++
			continue
		}
		break
	}
	if data && dot && asciiAlphaC(c.at(i-1)) {
		return i
	}
	return 0
}

func protocolAt(c codeReader) int {
	i := 0
	var buf []rune
	for asciiAlphaC(c.at(i)) && len(buf) < 5 {
		buf = append(buf, rune(c.at(i)))
		i++
	}
	if c.at(i) != ':' {
		return 0
	}
	if p := strings.ToLower(string(buf)); p != "http" && p != "https" {
		return 0
	}
	i++
	if c.at(i) != '/' || c.at(i+1) != '/' {
		return 0
	}
	i += 2
	code := c.at(i)
	if code == codeEnd || asciiControl(code) || lineEndingOrSpace(code) || unicodeWS(code) || unicodePunct(code) {
		return 0
	}
	end, ok := domainEnd(c, i)
	if !ok {
		return 0
	}
	return pathEnd(c, end)
}

func (p literalParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	line, seg := block.PeekLine()
	if len(line) == 0 {
		return nil
	}
	c := line[0]
	skip := 0
	prev := codeEOL
	if c == ' ' || c == '\t' || strings.IndexByte(p.triggers, c) >= 0 && c != ' ' {
		skip = 1
		prev = int(c)
		if c == '\t' {
			prev = codeTab
		}
	} else {
		switch r := block.PrecendingCharacter(); r {
		case '\n', '\r':
		case '\t':
			prev = codeTab
		default:
			prev = int(r)
		}
	}
	if skip >= len(line) {
		return nil
	}
	// Most places start no literal: decide that from the first character before reading further.
	first := int(line[skip])
	email := gfmAtext(first) && !(prev == '/' || gfmAtext(prev))
	protocol := (first == 'h' || first == 'H') && !asciiAlphaC(prev)
	www := (first == 'w' || first == 'W') && previousWww(prev)
	if !email && !protocol && !www {
		return nil
	}
	// A literal stops at whitespace; what follows it matters only as an end, or as a line ending when the paragraph
	// goes on (a bare "www." before one is still a link).
	end := skip
	for end < len(line) && line[end] != ' ' && line[end] != '\t' && line[end] != '\n' && line[end] != '\r' {
		end++
	}
	window := string(line[skip:end])
	if end < len(line) {
		if line[end] == '\n' || line[end] == '\r' {
			pos, s := block.Position()
			block.AdvanceLine()
			next, _ := block.PeekLine()
			block.SetPosition(pos, s)
			if len(next) > 0 {
				window += "\n"
			}
		} else {
			window += " "
		}
	}
	runes := []rune(window)
	n, kind := literalAt(codeReader{runes: runes}, prev)
	if n == 0 {
		return nil
	}
	label := string(runes[:n])
	url := label
	switch kind {
	case "www":
		url = "http://" + label
	case "email":
		url = "mailto:" + label
	}
	if skip > 0 {
		gast.MergeOrAppendTextSegment(parent, seg.WithStop(seg.Start+skip))
	}
	block.Advance(skip + len(label))
	return &literalLink{url: url, label: label}
}

var (
	literalInline     = util.Prioritized(literalParser{triggers: " \"#$%'()+,-./:;=>?@^{|}"}, 50)
	literalAfterAngle = util.Prioritized(literalParser{triggers: "<"}, 999)
)

// --- mdast's pass over text ------------------------------------------------------------------------------------------

type piece struct {
	text string
	link *literalLink // nil for text
}

// linkTexts finds the autolink literals mdast-util-gfm-autolink-literal finds in a text node: URLs first, then e-mail
// addresses in what is left.
func linkTexts(s string) []piece {
	var out []piece
	for _, t := range tokenizeLiterals(s) {
		if t.link != nil {
			out = append(out, t)
			continue
		}
		out = append(out, transformLiterals(t.text)...)
	}
	return out
}

// tokenizeLiterals finds the literals micromark's tokenizer would have taken in text goldmark gave no chance to the
// inline parser (after a letter outside ASCII, a digit, a delimiter or a bracket).
func tokenizeLiterals(s string) []piece {
	if !strings.ContainsAny(s, "@:.") {
		return []piece{{text: s}}
	}
	runes := []rune(s)
	var out []piece
	start := 0
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if !(c == 'h' || c == 'H' || c == 'w' || c == 'W' || gfmAtext(int(c))) {
			continue
		}
		prev := codeEnd
		if i > 0 {
			prev = int(runes[i-1])
			switch runes[i-1] {
			case '\n', '\r':
				prev = codeEOL
			case '\t':
				prev = codeTab
			}
		}
		n, kind := literalAt(codeReader{runes: runes[i:]}, prev)
		if n == 0 {
			continue
		}
		label := string(runes[i : i+n])
		url := label
		switch kind {
		case "www":
			url = "http://" + label
		case "email":
			url = "mailto:" + label
		}
		if start < i {
			out = append(out, piece{text: string(runes[start:i])})
		}
		out = append(out, piece{link: &literalLink{url: url, label: label}})
		start = i + n
		i = start - 1
	}
	if start < len(runes) {
		out = append(out, piece{text: string(runes[start:])})
	}
	return out
}

// transformLiterals is mdast's pass: URLs first, then e-mail addresses in what is left.
func transformLiterals(s string) []piece {
	var out []piece
	for _, p := range findURLs(s) {
		if p.link != nil {
			out = append(out, p)
			continue
		}
		out = append(out, findEmails(p.text)...)
	}
	return out
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// previousOK: the match starts the text or follows whitespace or punctuation (and for an address not a slash).
func previousOK(s string, at int, email bool) bool {
	if at == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:at])
	if r >= 0x10000 { // one surrogate of a pair, as charCodeAt sees it
		return false
	}
	return (unicodeSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)) && (!email || r != '/')
}

func findURLs(s string) []piece {
	var out []piece
	start := 0
	for i := 0; i < len(s); {
		protoLen := 0
		lower := strings.ToLower(s[i:min(len(s), i+8)])
		switch {
		case strings.HasPrefix(lower, "https://"):
			protoLen = 8
		case strings.HasPrefix(lower, "http://"):
			protoLen = 7
		case strings.HasPrefix(lower, "www") && i+3 < len(s) && s[i+3] == '.':
			protoLen = 3
		}
		if protoLen == 0 {
			i++
			continue
		}
		j := i + protoLen
		for j < len(s) && (isWordByte(s[j]) || s[j] == '-' || s[j] == '.') {
			j++
		}
		if j == i+protoLen {
			i++
			continue
		}
		domEnd := j
		for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '\r' && s[j] != '\n' {
			j++
		}
		protocol, domain, path := s[i:i+protoLen], s[i+protoLen:domEnd], s[domEnd:j]
		link, trail, ok := urlLink(s, i, protocol, domain, path)
		if !ok {
			i++
			continue
		}
		if start < i {
			out = append(out, piece{text: s[start:i]})
		}
		out = append(out, piece{link: link})
		if trail != "" {
			out = append(out, piece{text: trail})
		}
		start, i = j, j
	}
	if start < len(s) {
		out = append(out, piece{text: s[start:]})
	}
	return out
}

func urlLink(s string, at int, protocol, domain, path string) (*literalLink, string, bool) {
	if !previousOK(s, at, false) {
		return nil, "", false
	}
	prefix := ""
	if protocol[0] == 'w' || protocol[0] == 'W' {
		domain, protocol, prefix = protocol+domain, "", "http://"
	}
	if !correctDomain(domain) {
		return nil, "", false
	}
	url, trail := splitURL(domain + path)
	if url == "" {
		return nil, "", false
	}
	return &literalLink{url: prefix + protocol + url, label: protocol + url}, trail, true
}

func hasAlnum(s string) bool {
	for i := 0; i < len(s); i++ {
		if asciiAlnumC(int(s[i])) {
			return true
		}
	}
	return false
}

func correctDomain(domain string) bool {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range []string{parts[len(parts)-1], parts[len(parts)-2]} {
		if p != "" && (strings.Contains(p, "_") || !hasAlnum(p)) {
			return false
		}
	}
	return true
}

func splitURL(url string) (string, string) {
	end := len(url)
	for end > 0 && strings.IndexByte(`!"&'),.:;<>?]}`, url[end-1]) >= 0 {
		end--
	}
	if end == len(url) {
		return url, ""
	}
	trail := url[end:]
	url = url[:end]
	closing := strings.IndexByte(trail, ')')
	opening := strings.Count(url, "(")
	closed := strings.Count(url, ")")
	for closing != -1 && opening > closed {
		url += trail[:closing+1]
		trail = trail[closing+1:]
		closing = strings.IndexByte(trail, ')')
		closed++
	}
	return url, trail
}

func findEmails(s string) []piece {
	var out []piece
	start := 0
	for i := 0; i < len(s); {
		if !previousEmailBoundary(s, i) || !emailAtext(s[i]) {
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
			continue
		}
		j := i
		for j < len(s) && emailAtext(s[j]) {
			j++
		}
		if j >= len(s) || s[j] != '@' {
			i++
			continue
		}
		label, ok := emailLabel(s[j+1:])
		if !ok {
			i++
			continue
		}
		end := j + 1 + len(label)
		last := label[len(label)-1]
		if !previousOK(s, i, true) || last == '-' || last == '_' || (last >= '0' && last <= '9') {
			i++
			continue
		}
		if start < i {
			out = append(out, piece{text: s[start:i]})
		}
		out = append(out, piece{link: &literalLink{url: "mailto:" + s[i:end], label: s[i:end]}})
		start, i = end, end
	}
	if start < len(s) {
		out = append(out, piece{text: s[start:]})
	}
	return out
}

func emailAtext(c byte) bool { return isWordByte(c) || c == '-' || c == '.' || c == '+' }

// previousEmailBoundary is the address regex's lookbehind: the start, or after whitespace, punctuation or a symbol.
func previousEmailBoundary(s string, at int) bool {
	if at == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:at])
	return unicodeSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// emailLabel is [-\w]+(?:\.[-\w]+)+ at the start of s, greedy.
func emailLabel(s string) (string, bool) {
	part := func(i int) int {
		j := i
		for j < len(s) && (isWordByte(s[j]) || s[j] == '-') {
			j++
		}
		return j
	}
	end := part(0)
	if end == 0 {
		return "", false
	}
	parts := 1
	for end < len(s) && s[end] == '.' {
		next := part(end + 1)
		if next == end+1 {
			break
		}
		end = next
		parts++
	}
	if parts < 2 {
		return "", false
	}
	return s[:end], true
}
