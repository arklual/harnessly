// Package weburl parses URLs the way browsers and Node's URL class do (the WHATWG URL standard) for the http(s) and
// ws(s) schemes the preview deals with: lowercase scheme and host, default port dropped, dot segments resolved, the
// same characters percent-encoded. The preview compares origins and rewrites locations by these spellings.
package weburl

import (
	"net"
	"strconv"
	"strings"
)

// URL is a parsed special URL.
type URL struct {
	Scheme   string // without ":"
	Username string
	Password string
	Hostname string // IPv6 in brackets
	Port     string // "" for the scheme's default
	Path     string // starts with "/"
	Query    string // with "?", or ""
	Fragment string // with "#", or ""
}

var defaultPorts = map[string]string{"http": "80", "https": "443", "ws": "80", "wss": "443", "ftp": "21"}

// Host is hostname[:port].
func (u *URL) Host() string {
	if u.Port == "" {
		return u.Hostname
	}
	return u.Hostname + ":" + u.Port
}

// Origin is scheme://host.
func (u *URL) Origin() string { return u.Scheme + "://" + u.Host() }

// Protocol is the scheme with its colon.
func (u *URL) Protocol() string { return u.Scheme + ":" }

// Search is the query with "?", "" when empty.
func (u *URL) Search() string {
	if u.Query == "?" {
		return ""
	}
	return u.Query
}

// Hash is the fragment with "#", "" when empty.
func (u *URL) Hash() string {
	if u.Fragment == "#" {
		return ""
	}
	return u.Fragment
}

// Href is the serialised URL.
func (u *URL) Href() string {
	var b strings.Builder
	b.WriteString(u.Scheme + "://")
	if u.Username != "" || u.Password != "" {
		b.WriteString(u.Username)
		if u.Password != "" {
			b.WriteString(":" + u.Password)
		}
		b.WriteString("@")
	}
	b.WriteString(u.Host())
	b.WriteString(u.Path)
	b.WriteString(u.Query)
	b.WriteString(u.Fragment)
	return b.String()
}

func isC0Space(c byte) bool { return c <= 0x20 }

func clean(s string) string {
	i, j := 0, len(s)
	for i < j && isC0Space(s[i]) {
		i++
	}
	for j > i && isC0Space(s[j-1]) {
		j--
	}
	s = s[i:j]
	if strings.ContainsAny(s, "\t\n\r") {
		s = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(s)
	}
	return s
}

func schemeOf(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		case c == ':' && i > 0:
			return strings.ToLower(s[:i]), s[i+1:], true
		default:
			return "", s, false
		}
	}
	return "", s, false
}

func special(scheme string) bool {
	_, ok := defaultPorts[scheme]
	return ok
}

// Parse parses an absolute URL of a special scheme other than file; false otherwise or when it is invalid.
func Parse(raw string) (*URL, bool) { return parse(clean(raw), nil) }

// ParseRef resolves ref against base, as new URL(ref, base).
func ParseRef(ref string, base *URL) (*URL, bool) { return parse(clean(ref), base) }

func parse(s string, base *URL) (*URL, bool) {
	scheme, rest, ok := schemeOf(s)
	if ok {
		if !special(scheme) {
			return nil, false
		}
		rest = slashes(rest)
		if base != nil && scheme == base.Scheme && !strings.HasPrefix(rest, "/") {
			return relative(rest, base)
		}
		rest = strings.TrimLeft(rest, "/")
		return authority(scheme, rest)
	}
	if base == nil {
		return nil, false
	}
	return relative(slashes(s), base)
}

// slashes reads a backslash as a slash before the query and the fragment, as the parser does for special schemes.
func slashes(s string) string {
	end := strings.IndexAny(s, "?#")
	if end < 0 {
		end = len(s)
	}
	return strings.ReplaceAll(s[:end], "\\", "/") + s[end:]
}

func relative(s string, base *URL) (*URL, bool) {
	u := *base
	switch {
	case strings.HasPrefix(s, "//"):
		return authority(base.Scheme, strings.TrimLeft(s, "/"))
	case strings.HasPrefix(s, "/"):
		path, query, frag := splitTail(s)
		u.Path, u.Query, u.Fragment = normPath(path), encodeQuery(query, true), encodeFragment(frag)
	case strings.HasPrefix(s, "?"):
		_, query, frag := splitTail(s)
		u.Query, u.Fragment = encodeQuery(query, true), encodeFragment(frag)
	case strings.HasPrefix(s, "#"):
		u.Fragment = encodeFragment(s)
	case s == "":
		u.Fragment = ""
	default:
		path, query, frag := splitTail(s)
		dir := base.Path[:strings.LastIndex(base.Path, "/")+1]
		u.Path, u.Query, u.Fragment = normPath(dir+path), encodeQuery(query, true), encodeFragment(frag)
	}
	return &u, true
}

func splitTail(s string) (path, query, frag string) {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s, frag = s[:i], s[i:]
	}
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s, query = s[:i], s[i:]
	}
	return s, query, frag
}

func authority(scheme, s string) (*URL, bool) {
	end := strings.IndexAny(s, "/?#")
	auth, tail := s, ""
	if end >= 0 {
		auth, tail = s[:end], s[end:]
	}
	u := &URL{Scheme: scheme}
	if at := strings.LastIndexByte(auth, '@'); at >= 0 {
		cred := auth[:at]
		auth = auth[at+1:]
		user, pass, _ := strings.Cut(cred, ":")
		u.Username, u.Password = encodeSet(user, userinfoSet), encodeSet(pass, userinfoSet)
	}
	host, port := auth, ""
	if strings.HasPrefix(auth, "[") {
		close := strings.IndexByte(auth, ']')
		if close < 0 {
			return nil, false
		}
		host, port = auth[:close+1], auth[close+1:]
		if port != "" && !strings.HasPrefix(port, ":") {
			return nil, false
		}
		port = strings.TrimPrefix(port, ":")
		ip := net.ParseIP(host[1 : len(host)-1])
		if ip == nil || ip.To4() != nil && !strings.Contains(host, ":") {
			return nil, false
		}
		host = "[" + compressV6(ip) + "]"
	} else {
		if i := strings.LastIndexByte(auth, ':'); i >= 0 {
			host, port = auth[:i], auth[i+1:]
		}
		h, ok := parseHost(host)
		if !ok {
			return nil, false
		}
		host = h
	}
	if port != "" {
		for _, c := range port {
			if c < '0' || c > '9' {
				return nil, false
			}
		}
		n, err := strconv.Atoi(port)
		if err != nil || n > 65535 {
			return nil, false
		}
		port = strconv.Itoa(n)
		if port == defaultPorts[scheme] {
			port = ""
		}
	}
	u.Hostname, u.Port = host, port
	path, query, frag := splitTail(tail)
	u.Path, u.Query, u.Fragment = normPath(path), encodeQuery(query, true), encodeFragment(frag)
	return u, true
}

// parseHost lowercases a domain, reads an IPv4 address in any of its spellings, and refuses forbidden characters.
func parseHost(h string) (string, bool) {
	if h == "" {
		return "", false
	}
	if strings.Contains(h, "%") {
		d, ok := percentDecode(h)
		if !ok {
			return "", false
		}
		h = d
	}
	h = strings.ToLower(h)
	for i := 0; i < len(h); i++ {
		c := h[i]
		if c <= 0x20 || c == 0x7f || strings.IndexByte("#%/:<>?@[\\]^|", c) >= 0 {
			return "", false
		}
	}
	if v4, ok, isNum := IPv4(h); isNum {
		if !ok {
			return "", false
		}
		return v4, true
	}
	return h, true
}

func percentDecode(s string) (string, bool) {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b = append(b, byte(n))
			i += 2
			continue
		}
		b = append(b, s[i])
	}
	return string(b), true
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// IPv4 reads a host the way the URL parser does when its last label is numeric: ok with the dotted address; isNum false
// when the host is a domain.
func IPv4(h string) (string, bool, bool) {
	parts := strings.Split(h, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return "", false, false
	}
	if _, ok := ipv4Number(parts[len(parts)-1]); !ok {
		return "", false, false
	}
	if len(parts) > 4 {
		return "", false, true
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := ipv4Number(p)
		if !ok {
			return "", false, true
		}
		nums[i] = n
	}
	last := nums[len(nums)-1]
	for _, n := range nums[:len(nums)-1] {
		if n > 255 {
			return "", false, true
		}
	}
	limit := uint64(1) << (8 * (5 - uint(len(nums))))
	if last >= limit {
		return "", false, true
	}
	v := last
	for i, n := range nums[:len(nums)-1] {
		v += n << (8 * (3 - uint(i)))
	}
	return strconv.Itoa(int(v>>24)) + "." + strconv.Itoa(int(v>>16&255)) + "." + strconv.Itoa(int(v>>8&255)) + "." + strconv.Itoa(int(v&255)), true, true
}

func ipv4Number(p string) (uint64, bool) {
	if p == "" {
		return 0, false
	}
	base := 10
	switch {
	case len(p) >= 2 && (p[:2] == "0x" || p[:2] == "0X"):
		p, base = p[2:], 16
		if p == "" {
			return 0, true
		}
	case len(p) >= 2 && p[0] == '0':
		p, base = p[1:], 8
	}
	n, err := strconv.ParseUint(p, base, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func compressV6(ip net.IP) string {
	ip16 := ip.To16()
	// net.IP.String prints an IPv4-mapped address dotted; the URL parser prints every address in hex groups.
	groups := make([]uint16, 8)
	for i := range groups {
		groups[i] = uint16(ip16[2*i])<<8 | uint16(ip16[2*i+1])
	}
	bestStart, bestLen := -1, 0
	for i := 0; i < 8; {
		if groups[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && groups[j] == 0 {
			j++
		}
		if j-i > bestLen && j-i > 1 {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	var b strings.Builder
	for i := 0; i < 8; {
		if i == bestStart {
			b.WriteString("::")
			i += bestLen
			continue
		}
		if i > 0 && i != bestStart+bestLen {
			b.WriteByte(':')
		}
		b.WriteString(strconv.FormatUint(uint64(groups[i]), 16))
		i++
	}
	return b.String()
}

const (
	pathSet     = " \"#<>?`{}"
	userinfoSet = " \"#<>?`{}/:;=@[\\]^|"
	fragmentSet = " \"<>`"
	querySet    = " \"#<>'"
)

func encodeSet(s, set string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f || strings.IndexByte(set, c) >= 0 {
			b.WriteString("%" + strings.ToUpper(strconv.FormatUint(uint64(c)>>4, 16)+strconv.FormatUint(uint64(c)&15, 16)))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func encodeQuery(q string, isSpecial bool) string {
	if q == "" {
		return ""
	}
	set := querySet
	if !isSpecial {
		set = " \"#<>"
	}
	return "?" + encodeSet(q[1:], set)
}

func encodeFragment(f string) string {
	if f == "" {
		return ""
	}
	return "#" + encodeSet(f[1:], fragmentSet)
}

func isSingleDot(seg string) bool { return seg == "." || strings.EqualFold(seg, "%2e") }

func isDoubleDot(seg string) bool {
	switch strings.ToLower(seg) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

func normPath(p string) string {
	if p == "" {
		return "/"
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	var out []string
	for i, seg := range segs {
		last := i == len(segs)-1
		switch {
		case isDoubleDot(seg):
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		case isSingleDot(seg):
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, encodeSet(seg, pathSet))
		}
	}
	return "/" + strings.Join(out, "/")
}
