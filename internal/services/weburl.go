package services

import (
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"harnessly/internal/files"
)

// webURL is a URL as the WHATWG parser (JavaScript's new URL) reads one: the previous handlers decided what to fetch
// from its hostname, port and pathname, and fetched its href. Only the special schemes are parsed in full; any other
// scheme keeps just its name, which is all a caller asking for https needs. Hosts are ASCII: an internationalized
// name, which the previous runtime turned into punycode, is refused here.
type webURL struct {
	Scheme   string
	special  bool
	Username string
	Password string
	Host     string
	Port     string
	path     []string
	Query    *string
	Fragment *string
}

var defaultPorts = map[string]string{"http": "80", "https": "443", "ws": "80", "wss": "443", "ftp": "21"}

func (u *webURL) Protocol() string { return u.Scheme + ":" }

func (u *webURL) Pathname() string {
	if !u.special {
		return ""
	}
	return "/" + strings.Join(u.path, "/")
}

// Search is "?query", "" without one or with an empty one.
func (u *webURL) Search() string {
	if u.Query == nil || *u.Query == "" {
		return ""
	}
	return "?" + *u.Query
}

// Target is what a request sends for the URL: the path and the query.
func (u *webURL) Target() string {
	t := u.Pathname()
	if u.Query != nil {
		t += "?" + *u.Query
	}
	return t
}

func (u *webURL) HostPort() string {
	if u.Port == "" {
		return u.Host
	}
	return u.Host + ":" + u.Port
}

func (u *webURL) Origin() string {
	if !u.special {
		return "null"
	}
	return u.Scheme + "://" + u.HostPort()
}

func (u *webURL) Href() string {
	if !u.special {
		return u.Scheme + ":"
	}
	var b strings.Builder
	b.WriteString(u.Scheme + "://")
	if u.Username != "" || u.Password != "" {
		b.WriteString(u.Username)
		if u.Password != "" {
			b.WriteString(":" + u.Password)
		}
		b.WriteByte('@')
	}
	b.WriteString(u.HostPort())
	b.WriteString(u.Target())
	if u.Fragment != nil {
		b.WriteString("#" + *u.Fragment)
	}
	return b.String()
}

// Param is url.searchParams.get(name): nil when absent.
func (u *webURL) Param(name string) *string {
	if u.Query == nil {
		return nil
	}
	for _, pair := range strings.Split(*u.Query, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		if formDecode(k) == name {
			v = formDecode(v)
			return &v
		}
	}
	return nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// percentDecodeBytes decodes each valid %XX and keeps anything else as it is.
func percentDecodeBytes(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if h, l := hexVal(s[i+1]), hexVal(s[i+2]); h >= 0 && l >= 0 {
				out = append(out, byte(h<<4|l))
				i += 2
				continue
			}
		}
		out = append(out, s[i])
	}
	return out
}

// formDecode is application/x-www-form-urlencoded decoding of a name or a value.
func formDecode(s string) string {
	return files.DecodeUTF8(percentDecodeBytes(strings.ReplaceAll(s, "+", " ")))
}

const upperHex = "0123456789ABCDEF"

// Percent-encode sets of the URL standard, as a test on one byte of the UTF-8 form (every byte past 0x7E is encoded).
func inFragmentSet(c byte) bool {
	return c < 0x20 || c > 0x7e || c == ' ' || c == '"' || c == '<' || c == '>' || c == '`'
}

func inQuerySet(c byte) bool {
	return c < 0x20 || c > 0x7e || c == ' ' || c == '"' || c == '#' || c == '<' || c == '>'
}

func inSpecialQuerySet(c byte) bool { return inQuerySet(c) || c == '\'' }

func inPathSet(c byte) bool {
	return inQuerySet(c) || c == '?' || c == '`' || c == '{' || c == '}'
}

func inUserinfoSet(c byte) bool {
	return inPathSet(c) || c == '/' || c == ':' || c == ';' || c == '=' || c == '@' || (c >= '[' && c <= '^') || c == '|'
}

func percentEncode(s string, set func(byte) bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if set(c) {
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xF])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// parseWebURL is new URL(input, base): ok is false where it throws. base, when given, is a special URL.
func parseWebURL(input string, base *webURL) (*webURL, bool) {
	input = strings.TrimFunc(input, func(r rune) bool { return r <= 0x20 })
	input = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, input)
	input = strings.ToValidUTF8(input, "�")
	scheme, rest, hasScheme := splitScheme(input)
	if !hasScheme {
		if base == nil {
			return nil, false
		}
		return relative(input, base)
	}
	if _, special := defaultPorts[scheme]; !special {
		return &webURL{Scheme: scheme}, true
	}
	if base != nil && base.Scheme == scheme {
		return relative(rest, base)
	}
	return authority(scheme, strings.TrimLeft(rest, "/\\"))
}

func splitScheme(s string) (string, string, bool) {
	if s == "" || !isAlpha(s[0]) {
		return "", "", false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			return strings.ToLower(s[:i]), s[i+1:], true
		}
		if !isAlpha(c) && !(c >= '0' && c <= '9') && c != '+' && c != '-' && c != '.' {
			return "", "", false
		}
	}
	return "", "", false
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func relative(r string, base *webURL) (*webURL, bool) {
	u := &webURL{Scheme: base.Scheme, special: true, Username: base.Username, Password: base.Password, Host: base.Host, Port: base.Port}
	if r == "" {
		u.path = append([]string(nil), base.path...)
		u.Query = base.Query
		return u, true
	}
	switch r[0] {
	case '/', '\\':
		if len(r) > 1 && (r[1] == '/' || r[1] == '\\') {
			return authority(base.Scheme, strings.TrimLeft(r, "/\\"))
		}
		return u.rest(r[1:], []string{}, true), true
	case '?':
		return u.rest(r, append([]string{}, base.path...), false), true
	case '#':
		u.path = append([]string(nil), base.path...)
		u.Query = base.Query
		f := percentEncode(r[1:], inFragmentSet)
		u.Fragment = &f
		return u, true
	}
	p := append([]string(nil), base.path...)
	if len(p) > 0 {
		p = p[:len(p)-1]
	}
	return u.rest(r, p, true), true
}

// authority reads [userinfo@]host[:port] and what follows.
func authority(scheme, s string) (*webURL, bool) {
	end := strings.IndexAny(s, "/\\?#")
	if end < 0 {
		end = len(s)
	}
	auth, after := s[:end], s[end:]
	u := &webURL{Scheme: scheme, special: true}
	if at := strings.LastIndexByte(auth, '@'); at >= 0 {
		info := auth[:at]
		auth = auth[at+1:]
		user, pass, _ := strings.Cut(info, ":")
		u.Username = percentEncode(user, inUserinfoSet)
		u.Password = percentEncode(pass, inUserinfoSet)
		if auth == "" {
			return nil, false
		}
	}
	hostPart, port := auth, ""
	hasPort := false
	if strings.HasPrefix(auth, "[") {
		close := strings.IndexByte(auth, ']')
		if close >= 0 && close+1 < len(auth) && auth[close+1] == ':' {
			hostPart, port, hasPort = auth[:close+1], auth[close+2:], true
		}
	} else if i := strings.IndexByte(auth, ':'); i >= 0 {
		hostPart, port, hasPort = auth[:i], auth[i+1:], true
	}
	if hostPart == "" {
		return nil, false
	}
	host, ok := parseHost(hostPart)
	if !ok {
		return nil, false
	}
	u.Host = host
	if hasPort && port != "" {
		n := 0
		for i := 0; i < len(port); i++ {
			if port[i] < '0' || port[i] > '9' {
				return nil, false
			}
			if n = n*10 + int(port[i]-'0'); n > 65535 {
				return nil, false
			}
		}
		if p := strconv.Itoa(n); p != defaultPorts[scheme] {
			u.Port = p
		}
	}
	if after != "" && (after[0] == '/' || after[0] == '\\') {
		after = after[1:]
	}
	return u.rest(after, []string{}, true), true
}

// rest is the path state onwards (skipped for a reference that starts with its query): segments appended to p, then
// the query and the fragment.
func (u *webURL) rest(s string, p []string, path bool) *webURL {
	pathEnd := strings.IndexAny(s, "?#")
	if pathEnd < 0 {
		pathEnd = len(s)
	}
	if path {
		p = appendSegments(p, s[:pathEnd])
	}
	u.path = p
	s = s[pathEnd:]
	if strings.HasPrefix(s, "?") {
		q, frag, hasFrag := strings.Cut(s[1:], "#")
		q = percentEncode(q, inSpecialQuerySet)
		u.Query = &q
		if hasFrag {
			f := percentEncode(frag, inFragmentSet)
			u.Fragment = &f
		}
	} else if strings.HasPrefix(s, "#") {
		f := percentEncode(s[1:], inFragmentSet)
		u.Fragment = &f
	}
	return u
}

func isSingleDot(s string) bool { return s == "." || strings.EqualFold(s, "%2e") }

func isDoubleDot(s string) bool {
	switch strings.ToLower(s) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

// appendSegments runs the path state over a path without its query: segments split on / and \, dot segments
// resolved, each segment percent-encoded.
func appendSegments(p []string, s string) []string {
	start := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '/' && s[i] != '\\' {
			continue
		}
		seg := percentEncode(s[start:i], inPathSet)
		atEnd := i == len(s)
		switch {
		case isDoubleDot(seg):
			if len(p) > 0 {
				p = p[:len(p)-1]
			}
			if atEnd {
				p = append(p, "")
			}
		case isSingleDot(seg):
			if atEnd {
				p = append(p, "")
			}
		default:
			p = append(p, seg)
		}
		start = i + 1
	}
	return p
}

func isForbiddenDomainByte(c byte) bool {
	return c <= 0x20 || c == 0x7f || strings.IndexByte("#%/:<>?@[\\]^|", c) >= 0
}

// parseHost is the host parser for a special URL, ASCII names only.
func parseHost(s string) (string, bool) {
	if strings.HasPrefix(s, "[") {
		if !strings.HasSuffix(s, "]") {
			return "", false
		}
		ip := net.ParseIP(s[1 : len(s)-1])
		if ip == nil || !strings.Contains(s, ":") || strings.Contains(s, "%") {
			return "", false
		}
		return "[" + ipv6String(ip.To16()) + "]", true
	}
	raw := percentDecodeBytes(s)
	if !utf8.Valid(raw) {
		return "", false
	}
	for _, c := range raw {
		if c >= 0x80 || isForbiddenDomainByte(c) {
			return "", false
		}
	}
	host := strings.ToLower(string(raw))
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	if last == "" && len(labels) > 1 {
		last = labels[len(labels)-2]
	}
	if isNumberLabel(last) {
		return parseIPv4(host)
	}
	return host, true
}

func isNumberLabel(s string) bool {
	if s == "" {
		return false
	}
	if strings.Trim(s, "0123456789") == "" {
		return true
	}
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return strings.Trim(s[2:], "0123456789abcdefABCDEF") == ""
	}
	return false
}

func parseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	switch {
	case len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X"):
		s, base = s[2:], 16
	case len(s) >= 2 && s[0] == '0':
		s, base = s[1:], 8
	}
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return 1 << 40, true
		}
		return 0, false
	}
	return n, true
}

func parseIPv4(host string) (string, bool) {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := parseIPv4Number(p)
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
	lastN := nums[len(nums)-1]
	if lastN >= 1<<(8*(5-len(nums))) {
		return "", false
	}
	ip := lastN
	for i, n := range nums[:len(nums)-1] {
		ip += n << (8 * (3 - i))
	}
	return strconv.FormatUint(ip>>24, 10) + "." + strconv.FormatUint(ip>>16&255, 10) + "." +
		strconv.FormatUint(ip>>8&255, 10) + "." + strconv.FormatUint(ip&255, 10), true
}

// ipv6String serializes as the URL standard does: lowercase hex pieces, the first longest run of two or more zero
// pieces compressed.
func ipv6String(b net.IP) string {
	pieces := make([]int, 8)
	for i := range pieces {
		pieces[i] = int(b[2*i])<<8 | int(b[2*i+1])
	}
	bestStart, bestLen := -1, 1
	for i := 0; i < 8; {
		if pieces[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && pieces[j] == 0 {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	var sb strings.Builder
	for i := 0; i < 8; i++ {
		if i == bestStart {
			if i == 0 {
				sb.WriteString("::")
			} else {
				sb.WriteString(":")
			}
			i += bestLen - 1
			continue
		}
		sb.WriteString(strconv.FormatInt(int64(pieces[i]), 16))
		if i < 7 {
			sb.WriteByte(':')
		}
	}
	return sb.String()
}
