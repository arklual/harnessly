package coreapi

import (
	"net/netip"
	"strconv"
	"strings"
)

// Access is the runtime's network access rule: which remote addresses may call it. Spec words: "all" (also "*",
// "0.0.0.0"), "localhost", "tailscale", "lan", or a CIDR / address, comma-separated; the deployment derives the spec
// from its setting (an explicit host, else the configured network access, "all" by default).
type Access struct {
	all      bool
	prefixes []netip.Prefix
}

var accessWords = map[string][]string{
	"localhost": {"127.0.0.0/8", "::1/128"},
	"tailscale": {"100.64.0.0/10", "fd7a:115c:a1e0::/48"},
	"lan":       {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "fc00::/7", "fe80::/10"},
}

// AccessFor reads the setting of the network access option: localhost, tailscale (with localhost) or all.
func AccessFor(networkAccess string) Access {
	switch networkAccess {
	case "localhost":
		return ParseAccess("localhost")
	case "tailscale":
		return ParseAccess("localhost,tailscale")
	}
	return ParseAccess("all")
}

func parsePrefix(s string) (netip.Prefix, bool) {
	addr, bits, hasBits := strings.Cut(s, "/")
	a, ok := parseAddr(addr)
	if !ok {
		return netip.Prefix{}, false
	}
	n := a.BitLen()
	if hasBits {
		v, err := strconv.Atoi(bits)
		if err != nil || v < 0 || v > a.BitLen() {
			return netip.Prefix{}, false
		}
		n = v
	}
	p, err := a.Prefix(n)
	return p, err == nil
}

// parseAddr reads an address as the previous runtime did: an IPv4-mapped IPv6 address is its IPv4 address, a zone is
// dropped.
func parseAddr(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.WithZone("").Unmap(), true
}

// ParseAccess reads a spec; parts it cannot read are left out.
func ParseAccess(spec string) Access {
	var a Access
	for _, part := range strings.Split(spec, ",") {
		t := strings.ToLower(strings.TrimSpace(part))
		if t == "" {
			continue
		}
		if t == "all" || t == "*" || t == "0.0.0.0" {
			a.all = true
			continue
		}
		ranges, word := accessWords[t]
		if !word {
			ranges = []string{strings.TrimSpace(part)}
		}
		for _, r := range ranges {
			if p, ok := parsePrefix(r); ok {
				a.prefixes = append(a.prefixes, p)
			}
		}
	}
	return a
}

// Allowed tells whether a remote address (host, or host:port) may call.
func (a Access) Allowed(remote string) bool {
	if a.all {
		return true
	}
	host := remote
	if h, _, err := splitHostPort(remote); err == nil {
		host = h
	}
	addr, ok := parseAddr(host)
	if !ok {
		return false
	}
	for _, p := range a.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func splitHostPort(s string) (string, string, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return "", "", err
	}
	return ap.Addr().String(), strconv.Itoa(int(ap.Port())), nil
}
