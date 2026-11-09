package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The slots' https origins of a standalone runtime come from one of two settings: RUNTIME_PREVIEW_PUBLIC_ORIGINS lists
// them, or RUNTIME_PREVIEW_DOMAIN names the domain they live under, and the runtime names them
// https://s<n>-<id>.<domain> itself. The id is the same on every start (RUNTIME_PREVIEW_ID, else the hub's machine id,
// else one the runtime made once and keeps in its data folder), so a restart never loses or renames them.

// PreviewIDFile is where a runtime keeps the id it made for its slots' names.
const PreviewIDFile = "preview-id"

var (
	domainRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	idRe     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,40}[a-z0-9])?$`)
)

// NormalizeDomain is RUNTIME_PREVIEW_DOMAIN as a lowercase name without a scheme, a leading "*." or a trailing dot;
// an error says why it is not a domain.
func NormalizeDomain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	d = strings.TrimSuffix(strings.TrimSuffix(d, "/"), ".")
	d = strings.TrimPrefix(d, "*.")
	if !domainRe.MatchString(d) {
		return "", fmt.Errorf("%q is not a domain name (two labels or more, letters, digits and dashes)", raw)
	}
	return d, nil
}

// ValidID says whether an id fits in the slots' names: lowercase letters, digits and inner dashes, 42 at most.
func ValidID(id string) bool { return idRe.MatchString(id) }

// DomainOrigins are the n slots' origins under domain for id: https://s1-<id>.<domain> … https://s<n>-<id>.<domain>.
func DomainOrigins(domain, id string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("https://s%d-%s.%s", i+1, id, domain)
	}
	return out
}

// PreviewID is the id of this runtime's slot names: named is RUNTIME_PREVIEW_ID, machine the hub's machine id ("" when
// there is none). Without either the id is read from dataDir/preview-id, made and written there on the first start.
func PreviewID(named, machine, dataDir string) (string, error) {
	if id := strings.ToLower(strings.TrimSpace(named)); id != "" {
		if !ValidID(id) {
			return "", fmt.Errorf("RUNTIME_PREVIEW_ID %q: only lowercase letters, digits and inner dashes, 42 at most", named)
		}
		return id, nil
	}
	if id := strings.ToLower(strings.TrimSpace(machine)); ValidID(id) {
		return id, nil
	}
	if dataDir == "" {
		return "", errors.New("no data folder to keep the preview id in; set RUNTIME_PREVIEW_ID")
	}
	path := filepath.Join(dataDir, PreviewIDFile)
	if raw, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(raw)); ValidID(id) {
			return id, nil
		}
		return "", fmt.Errorf("%s does not hold a valid id; remove it or set RUNTIME_PREVIEW_ID", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	// Written once and never again: the names behind the reverse proxy and in DNS stay what they were.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return PreviewID("", "", dataDir)
	}
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(id + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", werr
	}
	return id, nil
}

// Origin check states.
const (
	OriginOK        = "ok"        // the name resolves to an address of this machine
	OriginNotFound  = "not-found" // NXDOMAIN or no address at all
	OriginElsewhere = "elsewhere" // resolves, but to no address of this machine
	OriginError     = "error"     // the resolver failed (timeout, refused)
	OriginLiteral   = "literal"   // an IP address or localhost: there is no name to resolve
)

// OriginCheck is what DNS says of one slot origin.
type OriginCheck struct {
	Origin    string   `json:"origin"`
	Host      string   `json:"host"`
	State     string   `json:"state"`
	Addresses []string `json:"addresses,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// ContextLookup resolves a name to its addresses.
type ContextLookup func(ctx context.Context, host string) ([]string, error)

// SystemLookup is the system resolver.
func SystemLookup(ctx context.Context, host string) ([]string, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP.String())
	}
	return out, nil
}

// CheckOrigins resolves the host of every origin and says whether it leads to this machine (self: its addresses,
// SelfAddresses). A name of another machine is not wrong in itself: a reverse proxy or a balancer may stand there.
func CheckOrigins(ctx context.Context, origins []string, lookup ContextLookup, self map[string]bool) []OriginCheck {
	out := make([]OriginCheck, 0, len(origins))
	for _, o := range origins {
		c := OriginCheck{Origin: o}
		u, err := url.Parse(o)
		if err != nil || u.Hostname() == "" {
			c.State, c.Error = OriginError, "not an origin"
			out = append(out, c)
			continue
		}
		c.Host = strings.ToLower(u.Hostname())
		if _, err := netip.ParseAddr(c.Host); err == nil || localhostName.MatchString(c.Host) {
			c.State = OriginLiteral
			out = append(out, c)
			continue
		}
		addrs, err := lookup(ctx, c.Host)
		var dnsErr *net.DNSError
		switch {
		case err != nil && errors.As(err, &dnsErr) && dnsErr.IsNotFound, err == nil && len(addrs) == 0:
			c.State, c.Error = OriginNotFound, "the name does not resolve (NXDOMAIN)"
		case err != nil:
			c.State, c.Error = OriginError, err.Error()
		default:
			c.Addresses = addrs
			c.State = OriginElsewhere
			for _, a := range addrs {
				if self[strings.ToLower(a)] {
					c.State = OriginOK
					break
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// OriginsHint is what to do about the worst of the checks, "" when every name leads here.
func OriginsHint(checks []OriginCheck) string {
	worst := ""
	for _, c := range checks {
		switch c.State {
		case OriginNotFound:
			worst = OriginNotFound
		case OriginError:
			if worst != OriginNotFound {
				worst = OriginError
			}
		case OriginElsewhere:
			if worst == "" {
				worst = OriginElsewhere
			}
		}
	}
	switch worst {
	case OriginNotFound:
		return "the preview names are not in DNS: add a wildcard record (*.<preview domain>, or one record per slot) " +
			"pointing to this machine or to the reverse proxy in front of it; without it the panel cannot open a view over https"
	case OriginError:
		return "the resolver did not answer for the preview names: check the machine's DNS (/etc/resolv.conf) and try again"
	case OriginElsewhere:
		return "the preview names point to another address: right when a reverse proxy or balancer stands there and " +
			"forwards the slots here; behind NAT name the public address in RUNTIME_PREVIEW_SELF_IPS"
	}
	return ""
}
