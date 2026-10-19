package share

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// normalizeURI is micromark's normalizeUri: every character outside the URL-safe ASCII set is percent-encoded as
// UTF-8, a well-formed %XX (two ASCII letters or digits) is kept, a lone surrogate becomes U+FFFD.
func normalizeURI(s string) string {
	var b strings.Builder
	units := utf16.Encode([]rune(s))
	for i := 0; i < len(units); i++ {
		c := units[i]
		switch {
		case c == '%' && i+2 < len(units) && asciiAlnum(units[i+1]) && asciiAlnum(units[i+2]):
			b.WriteByte('%')
			b.WriteByte(byte(units[i+1]))
			b.WriteByte(byte(units[i+2]))
			i += 2
		case c < 128:
			if uriSafe(byte(c)) {
				b.WriteByte(byte(c))
			} else {
				percent(&b, string(rune(c)))
			}
		case c >= 0xd800 && c < 0xe000:
			if c < 0xdc00 && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] < 0xe000 {
				percent(&b, string(utf16.DecodeRune(rune(c), rune(units[i+1]))))
				i++
			} else {
				percent(&b, "\uFFFD")
			}
		default:
			percent(&b, string(rune(c)))
		}
	}
	return b.String()
}

func asciiAlnum(c uint16) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// uriSafe is /[!#$&-;=?-Z_a-z~]/.
func uriSafe(c byte) bool {
	return c == '!' || c == '#' || c == '$' || (c >= '&' && c <= ';') || c == '=' || (c >= '?' && c <= 'Z') || c == '_' ||
		(c >= 'a' && c <= 'z') || c == '~'
}

const upperHex = "0123456789ABCDEF"

func percent(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		b.WriteByte('%')
		b.WriteByte(upperHex[s[i]>>4])
		b.WriteByte(upperHex[s[i]&15])
	}
}

// parsedURL is the part of a WHATWG URL a link check needs.
type parsedURL struct {
	protocol, hostname, pathname, href string
}

// parseURL is new URL(raw) for the links a page may keep (http, https, mailto); false for anything else, a relative
// reference included. Input is what normalizeURI left: printable ASCII.
func parseURL(raw string) (parsedURL, bool) {
	s := strings.TrimFunc(raw, func(r rune) bool { return r <= ' ' })
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	colon := strings.IndexByte(s, ':')
	if colon <= 0 || !isAlpha(s[0]) {
		return parsedURL{}, false
	}
	for i := 1; i < colon; i++ {
		c := s[i]
		if !isAlpha(c) && !(c >= '0' && c <= '9') && c != '+' && c != '-' && c != '.' {
			return parsedURL{}, false
		}
	}
	scheme := strings.ToLower(s[:colon])
	rest := s[colon+1:]
	switch scheme {
	case "http", "https":
		return parseSpecial(scheme, rest)
	case "mailto":
		return parseOpaque(scheme, rest)
	}
	return parsedURL{}, false
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// Percent-encode sets of the URL standard, for the bytes of a special URL.
func inC0(c byte) bool { return c < 0x20 || c > 0x7e }
func inFragment(c byte) bool {
	return inC0(c) || c == ' ' || c == '"' || c == '<' || c == '>' || c == '`'
}
func inQuery(c byte) bool {
	return inC0(c) || c == ' ' || c == '"' || c == '#' || c == '<' || c == '>' || c == '\''
}
func inPath(c byte) bool {
	return inQuery(c) && c != '\'' || c == '?' || c == '^' || c == '`' || c == '{' || c == '}'
}
func inUserinfo(c byte) bool {
	return inPath(c) || c == '/' || c == ':' || c == ';' || c == '=' || c == '@' || (c >= '[' && c <= ']') || c == '|'
}

func encodeSet(s string, set func(byte) bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if set(s[i]) {
			percent(&b, s[i:i+1])
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func parseSpecial(scheme, rest string) (parsedURL, bool) {
	rest = strings.TrimLeft(rest, `/\`)
	end := strings.IndexAny(rest, `/\?#`)
	if end < 0 {
		end = len(rest)
	}
	authority, tail := rest[:end], rest[end:]
	userinfo := ""
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		userinfo, authority = authority[:at], authority[at+1:]
		if authority == "" {
			return parsedURL{}, false
		}
	}
	hostPort := authority
	port := ""
	if i := strings.LastIndexByte(authority, ':'); i >= 0 && !strings.Contains(authority, "[") {
		hostPort, port = authority[:i], authority[i+1:]
	}
	if hostPort == "" {
		return parsedURL{}, false
	}
	host, ok := parseHost(hostPort)
	if !ok {
		return parsedURL{}, false
	}
	if port != "" {
		for i := 0; i < len(port); i++ {
			if port[i] < '0' || port[i] > '9' {
				return parsedURL{}, false
			}
		}
		n, err := strconv.Atoi(strings.TrimLeft(port, "0"))
		if err != nil && strings.TrimLeft(port, "0") != "" || n > 65535 {
			return parsedURL{}, false
		}
		if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
			port = ""
		} else {
			port = strconv.Itoa(n)
		}
	}
	pathPart, query, fragment := tail, "", ""
	hasQuery, hasFragment := false, false
	if i := strings.IndexByte(pathPart, '#'); i >= 0 {
		pathPart, fragment, hasFragment = pathPart[:i], pathPart[i+1:], true
	}
	if i := strings.IndexByte(pathPart, '?'); i >= 0 {
		pathPart, query, hasQuery = pathPart[:i], pathPart[i+1:], true
	}
	path := specialPath(pathPart)
	var b strings.Builder
	b.WriteString(scheme)
	b.WriteString("://")
	if userinfo != "" {
		user, pass, hasPass := strings.Cut(userinfo, ":")
		user, pass = encodeSet(user, inUserinfo), encodeSet(pass, inUserinfo)
		b.WriteString(user)
		if hasPass && pass != "" {
			b.WriteByte(':')
			b.WriteString(pass)
		}
		if user != "" || (hasPass && pass != "") {
			b.WriteByte('@')
		}
	}
	b.WriteString(host)
	if port != "" {
		b.WriteByte(':')
		b.WriteString(port)
	}
	b.WriteString(path)
	if hasQuery {
		b.WriteByte('?')
		b.WriteString(encodeSet(query, inQuery))
	}
	if hasFragment {
		b.WriteByte('#')
		b.WriteString(encodeSet(fragment, inFragment))
	}
	return parsedURL{protocol: scheme + ":", hostname: host, pathname: path, href: b.String()}, true
}

// specialPath is the path of a special URL: "\" separates like "/", dot segments are resolved.
func specialPath(p string) string {
	if p == "" {
		return "/"
	}
	parts := strings.Split(strings.ReplaceAll(p, `\`, "/"), "/")[1:]
	var out []string
	for i, seg := range parts {
		last := i == len(parts)-1
		switch strings.ToLower(seg) {
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		case ".", "%2e":
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, encodeSet(seg, inPath))
		}
	}
	return "/" + strings.Join(out, "/")
}

func parseOpaque(scheme, rest string) (parsedURL, bool) {
	path, fragment, hasFragment := rest, "", false
	if i := strings.IndexByte(path, '#'); i >= 0 {
		path, fragment, hasFragment = path[:i], path[i+1:], true
	}
	query, hasQuery := "", false
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path, query, hasQuery = path[:i], path[i+1:], true
	}
	href := scheme + ":" + encodeSet(path, inC0)
	if hasQuery {
		href += "?" + encodeSet(query, func(c byte) bool { return inC0(c) || c == ' ' || c == '"' || c == '#' || c == '<' || c == '>' })
	}
	if hasFragment {
		href += "#" + encodeSet(fragment, inFragment)
	}
	return parsedURL{protocol: scheme + ":", pathname: encodeSet(path, inC0), href: href}, true
}

// parseHost is the host parser of a special URL: percent-decoded, mapped to ASCII, checked, an IPv4 address in its
// dotted form.
func parseHost(raw string) (string, bool) {
	if strings.HasPrefix(raw, "[") {
		return "", false // an IPv6 literal never reaches here unencoded: its brackets were percent-encoded
	}
	decoded := percentDecode(raw)
	if !utf8.ValidString(decoded) {
		decoded = strings.ToValidUTF8(decoded, "\uFFFD")
	}
	ascii, ok := domainToASCII(decoded)
	if !ok {
		return "", false
	}
	for i := 0; i < len(ascii); i++ {
		if forbiddenDomain(ascii[i]) {
			return "", false
		}
	}
	if endsInNumber(ascii) {
		return parseIPv4(ascii)
	}
	return ascii, true
}

func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b = append(b, byte(v))
			i += 2
			continue
		}
		b = append(b, s[i])
	}
	return string(b)
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func forbiddenDomain(c byte) bool {
	if c <= 0x20 || c == 0x7f {
		return true
	}
	return strings.IndexByte("#%/:<>?@[\\]^|", c) >= 0
}

// domainToASCII is UTS 46 ToASCII for the hosts links carry, approximated by NFKC and lower case: a character whose
// compatibility form brings ASCII punctuation fails, as the URL parser of the previous runtime failed it; labels with
// other letters go to Punycode.
func domainToASCII(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 0x3002, 0xff0e, 0xff61:
			b.WriteByte('.')
			continue
		case 0xad, 0x200b, 0x2060, 0xfeff, 0x200c, 0x200d, 0x34f, 0x180b, 0x180c, 0x180d:
			continue
		}
		if r < 0x80 {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		m := strings.ToLower(norm.NFKC.String(string(r)))
		for i := 0; i < len(m); i++ {
			if c := m[i]; c < 0x80 && !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
		b.WriteString(m)
	}
	labels := strings.Split(norm.NFC.String(b.String()), ".")
	for i, l := range labels {
		ascii := true
		for j := 0; j < len(l); j++ {
			if l[j] >= 0x80 {
				ascii = false
				break
			}
		}
		if ascii {
			if strings.HasPrefix(l, "xn--") {
				d, ok := punyDecode(l[4:])
				if !ok || d == "" {
					return "", false
				}
				for _, r := range d {
					if r < 0xa0 {
						return "", false
					}
				}
			}
			continue
		}
		for _, r := range l {
			if unicode.IsControl(r) {
				return "", false
			}
		}
		enc, ok := punyEncode(l)
		if !ok {
			return "", false
		}
		labels[i] = "xn--" + enc
	}
	out := strings.Join(labels, ".")
	if out == "" {
		return "", false
	}
	return out, true
}

// endsInNumber: the last label (a trailing empty one aside) is digits or a 0x number, so the host is an IPv4 address.
func endsInNumber(s string) bool {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := ipv4Number(last)
	return ok && (strings.HasPrefix(last, "0x") || strings.HasPrefix(last, "0X"))
}

func ipv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s, base = s[2:], 16
	} else if len(s) >= 2 && s[0] == '0' {
		s, base = s[1:], 8
	}
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return 1 << 40, true
		}
		return 0, false
	}
	return v, true
}

func parseIPv4(s string) (string, bool) {
	parts := strings.Split(s, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := ipv4Number(p)
		if !ok {
			return "", false
		}
		nums[i] = n
	}
	for _, n := range nums[:len(nums)-1] {
		if n > 255 {
			return "", false
		}
	}
	last := nums[len(nums)-1]
	if last >= 1<<(8*(5-len(nums))) {
		return "", false
	}
	ip := last
	for i, n := range nums[:len(nums)-1] {
		ip += n << (8 * (3 - i))
	}
	return strconv.FormatUint(ip>>24, 10) + "." + strconv.FormatUint(ip>>16&255, 10) + "." + strconv.FormatUint(ip>>8&255, 10) + "." +
		strconv.FormatUint(ip&255, 10), true
}

// Punycode (RFC 3492).
const (
	pBase        = 36
	pTMin        = 1
	pTMax        = 26
	pSkew        = 38
	pDamp        = 700
	pInitialBias = 72
	pInitialN    = 128
)

func adapt(delta, numPoints int, first bool) int {
	if first {
		delta /= pDamp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > ((pBase-pTMin)*pTMax)/2 {
		delta /= pBase - pTMin
		k += pBase
	}
	return k + (pBase-pTMin+1)*delta/(delta+pSkew)
}

func punyDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}

func punyEncode(s string) (string, bool) {
	runes := []rune(s)
	var out []byte
	for _, r := range runes {
		if r < 0x80 {
			out = append(out, byte(r))
		}
	}
	b := len(out)
	h := b
	if b > 0 {
		out = append(out, '-')
	}
	n, delta, bias := pInitialN, 0, pInitialBias
	for h < len(runes) {
		m := int(^uint(0) >> 1)
		for _, r := range runes {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, r := range runes {
			if int(r) < n {
				delta++
			}
			if int(r) == n {
				q := delta
				for k := pBase; ; k += pBase {
					t := k - bias
					if t < pTMin {
						t = pTMin
					} else if t > pTMax {
						t = pTMax
					}
					if q < t {
						break
					}
					out = append(out, punyDigit(t+(q-t)%(pBase-t)))
					q = (q - t) / (pBase - t)
				}
				out = append(out, punyDigit(q))
				bias = adapt(delta, h+1, h == b)
				delta = 0
				h++
			}
		}
		delta++
		n++
	}
	return string(out), true
}

func punyDecode(s string) (string, bool) {
	var out []rune
	b := strings.LastIndexByte(s, '-')
	if b > 0 {
		for _, c := range s[:b] {
			if c >= 0x80 {
				return "", false
			}
			out = append(out, c)
		}
		s = s[b+1:]
	} else if b == 0 {
		s = s[1:]
	}
	n, i, bias := pInitialN, 0, pInitialBias
	for pos := 0; pos < len(s); {
		oldi, w := i, 1
		for k := pBase; ; k += pBase {
			if pos >= len(s) {
				return "", false
			}
			c := s[pos]
			pos++
			var d int
			switch {
			case c >= 'a' && c <= 'z':
				d = int(c - 'a')
			case c >= 'A' && c <= 'Z':
				d = int(c - 'A')
			case c >= '0' && c <= '9':
				d = int(c-'0') + 26
			default:
				return "", false
			}
			i += d * w
			t := k - bias
			if t < pTMin {
				t = pTMin
			} else if t > pTMax {
				t = pTMax
			}
			if d < t {
				break
			}
			w *= pBase - t
			if w > 1<<30 {
				return "", false
			}
		}
		bias = adapt(i-oldi, len(out)+1, oldi == 0)
		n += i / (len(out) + 1)
		i %= len(out) + 1
		if n > 0x10ffff {
			return "", false
		}
		out = append(out[:i], append([]rune{rune(n)}, out[i:]...)...)
		i++
	}
	return string(out), true
}
