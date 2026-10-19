package preview

import (
	"context"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// An external preview target is fetched from this machine, so its name must not lead into the machine itself, the
// office network or the overlay mesh. The name is resolved here once and the proxy connects to the address that was
// checked: a second resolution (DNS rebinding) never happens.

// Resolved is where a name points, or why it is refused: "forbidden" or "not-found".
type Resolved struct {
	Address string
	Family  int
	Error   string
}

func prefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, p := range list {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

var (
	forbiddenV4 = prefixes("0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4")
	// IPv4-mapped, -compatible, SIIT-translated, NAT64, 6to4 and Teredo carry an IPv4 address inside: refused whole.
	forbiddenV6 = prefixes("::/96", "::ffff:0:0/96", "::ffff:0:0:0/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32",
		"2001:db8::/32", "2002::/16", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8")
	ipv4Part = regexp.MustCompile(`(?i)^(?:0x[0-9a-f]*|0[0-7]*|[1-9]\d*)$`)
)

// ipFamily is Node's net.isIP: 4, 6 or 0.
func ipFamily(s string) int {
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return 0
	}
	if a.Is4() {
		return 4
	}
	return 6
}

func parsePart(p string) float64 {
	lower := strings.ToLower(p)
	if strings.HasPrefix(lower, "0x") {
		if len(p) == 2 {
			return 0
		}
		n, _ := strconv.ParseUint(p[2:], 16, 64)
		return float64(n)
	}
	if len(p) > 1 && p[0] == '0' {
		n, _ := strconv.ParseUint(p[1:], 8, 64)
		return float64(n)
	}
	n, _ := strconv.ParseFloat(p, 64)
	return n
}

// NormalizeIPv4 is inet_aton: 2130706433, 0177.0.0.1, 0x7f.1 are all 127.0.0.1, as the URL parser and getaddrinfo read
// them; "" for anything else.
func NormalizeIPv4(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) < 1 || len(parts) > 4 {
		return ""
	}
	nums := make([]float64, len(parts))
	for i, p := range parts {
		if !ipv4Part.MatchString(p) {
			return ""
		}
		nums[i] = parsePart(p)
	}
	last := nums[len(nums)-1]
	nums = nums[:len(nums)-1]
	for _, n := range nums {
		if n > 255 {
			return ""
		}
	}
	limit := 1.0
	for i := 0; i < 4-len(nums); i++ {
		limit *= 256
	}
	if last >= limit {
		return ""
	}
	v := last
	for i, n := range nums {
		m := 1.0
		for j := 0; j < 3-i; j++ {
			m *= 256
		}
		v += n * m
	}
	u := uint32(v)
	return strconv.Itoa(int(u>>24)) + "." + strconv.Itoa(int(u>>16&255)) + "." + strconv.Itoa(int(u>>8&255)) + "." + strconv.Itoa(int(u&255))
}

// LiteralAddress is the address a host names by itself (an IPv6 literal, an IPv4 in any spelling); Family 0 for a name.
func LiteralAddress(host string) Resolved {
	bare := host
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") && len(host) >= 2 {
		bare = host[1 : len(host)-1]
	}
	if ipFamily(bare) == 6 {
		return Resolved{Address: strings.ToLower(bare), Family: 6}
	}
	if v4 := NormalizeIPv4(bare); v4 != "" {
		return Resolved{Address: v4, Family: 4}
	}
	return Resolved{}
}

// IsForbiddenAddress: internal, special or this machine's own; not an address at all is refused too.
func IsForbiddenAddress(address string, local map[string]bool) bool {
	family := ipFamily(address)
	if family == 0 || strings.Contains(address, "%") {
		return true
	}
	if local[strings.ToLower(address)] {
		return true
	}
	a := netip.MustParseAddr(address)
	list := forbiddenV6
	if family == 4 {
		list = forbiddenV4
	}
	for _, p := range list {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// SelfAddresses are the machine's own: its interfaces and the public ones named for it (behind NAT the public address
// is on no interface).
func SelfAddresses(named string) map[string]bool {
	out := map[string]bool{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				out[strings.ToLower(ipn.IP.String())] = true
			}
		}
	}
	for _, s := range strings.Split(named, ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out[s] = true
		}
	}
	return out
}

var localhostName = regexp.MustCompile(`(?i)(^|\.)localhost\.?$`)

// Lookup is the system resolver's answers for a name.
type Lookup func(host string) ([]string, error)

func systemLookup(host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Zone != "" {
			out = append(out, a.IP.String()+"%"+a.Zone)
			continue
		}
		out = append(out, a.IP.String())
	}
	return out, nil
}

// ResolvePublic resolves a host once and refuses it when any of its addresses is not public.
func ResolvePublic(host string, lookup Lookup, local map[string]bool) Resolved {
	if lit := LiteralAddress(host); lit.Family != 0 {
		if IsForbiddenAddress(lit.Address, local) {
			return Resolved{Error: "forbidden"}
		}
		return lit
	}
	if localhostName.MatchString(host) {
		return Resolved{Error: "forbidden"}
	}
	if lookup == nil {
		lookup = systemLookup
	}
	answers, err := lookup(host)
	if err != nil || len(answers) == 0 {
		return Resolved{Error: "not-found"}
	}
	for _, a := range answers {
		if IsForbiddenAddress(a, local) {
			return Resolved{Error: "forbidden"}
		}
	}
	chosen := answers[0]
	for _, a := range answers {
		if ipFamily(a) == 4 {
			chosen = a
			break
		}
	}
	family := 6
	if ipFamily(chosen) == 4 {
		family = 4
	}
	return Resolved{Address: chosen, Family: family}
}

// resolveCache keeps a public answer briefly: pages load dozens of resources at once. Refusals are asked again.
type resolveCache struct {
	mu      sync.Mutex
	entries map[string]resolveEntry
	ttl     time.Duration
	resolve func(string) Resolved
}

type resolveEntry struct {
	r     Resolved
	until time.Time
}

func (c *resolveCache) get(host string) Resolved {
	c.mu.Lock()
	if e, ok := c.entries[host]; ok && e.until.After(time.Now()) {
		c.mu.Unlock()
		return e.r
	}
	c.mu.Unlock()
	r := c.resolve(host)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.entries {
		if !e.until.After(now) {
			delete(c.entries, k)
		}
	}
	if r.Error == "" {
		c.entries[host] = resolveEntry{r, now.Add(c.ttl)}
	}
	return r
}
