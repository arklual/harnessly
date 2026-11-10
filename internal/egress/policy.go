// The policy side of egress is the one rule for what leaves a machine over HTTP on behalf of apps and Connections inside a
// company network (HRN-920, item 10): the company's root certificates, the proxy with its exceptions, a client
// certificate for mutual TLS, and the internal host names that may be private. One policy, one transport builder, and
// one way to say why a call was refused.

package egress

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Policy is what the admin of a deployment lets calls reach. An app cannot widen it: it comes from the hub's policy
// of the network ("контур") and from the machine's environment, which overrides it (FromEnv).
type Policy struct {
	// Internal are host names ("gitlab.corp") or suffixes ("*.corp") whose addresses may be private, loopback or shared
	// (100.64.0.0/10). Link-local addresses (cloud metadata) stay closed even to them.
	Internal []string
	// Roots are the certificates a server's chain may end in besides the system's; nil: the system's only.
	Roots *x509.CertPool
	// Proxy is the company's proxy for these calls; nil: none (a direct dial, address checked).
	Proxy *url.URL
	// NoProxy are the hosts that go direct: names, "*.suffix" or ".suffix", IP addresses and CIDRs.
	NoProxy []string
	// ClientCert is the certificate sent when a server asks for one (mutual TLS); nil: none.
	ClientCert *tls.Certificate
	// Publishers are the keys app packages may be signed with; RequireSigned: an unsigned package, or one signed by none
	// of them, is not installed (internal/apps/sign.go).
	Publishers    []Publisher
	RequireSigned bool
}

// WithClientCert is the policy with another client certificate: an app's own, for the calls to its own hosts only. The
// policy of the process is not changed.
func (p *Policy) WithClientCert(c *tls.Certificate) *Policy {
	cp := *p
	cp.ClientCert = c
	return &cp
}

var current atomic.Pointer[Policy]

// Set sets the policy of the process.
func Set(p Policy) { current.Store(&p) }

// Current is the policy of the process; the zero one before Set.
func Current() *Policy {
	if p := current.Load(); p != nil {
		return p
	}
	return &Policy{}
}

// Env names of the machine's overrides.
const (
	EnvInternalHosts = "RUNTIME_APPS_INTERNAL_HOSTS"
	EnvCAFile        = "RUNTIME_APPS_CA_FILE"
	EnvProxy         = "RUNTIME_APPS_PROXY"
	EnvNoProxy       = "RUNTIME_APPS_NO_PROXY"
	EnvClientCert    = "RUNTIME_APPS_CLIENT_CERT_FILE"
	EnvClientKey     = "RUNTIME_APPS_CLIENT_KEY_FILE"
	EnvRequireSigned = "RUNTIME_APPS_REQUIRE_SIGNED"
)

// FromEnv reads the machine's overrides: comma-separated internal hosts, a PEM file of roots added to the system's, the
// proxy URL and its exceptions, and a client certificate with its key. What it cannot read it names; the rest is kept.
func FromEnv(getenv func(string) string) (Policy, error) {
	var p Policy
	p.Internal = splitList(getenv(EnvInternalHosts))
	p.NoProxy = splitList(getenv(EnvNoProxy))
	p.RequireSigned = slices.Contains([]string{"1", "true", "on", "yes"}, strings.ToLower(strings.TrimSpace(getenv(EnvRequireSigned))))
	var errs []string
	if file := strings.TrimSpace(getenv(EnvCAFile)); file != "" {
		pem, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, EnvCAFile+": "+err.Error())
		} else if roots, err := AddRoots(pem); err != nil {
			errs = append(errs, EnvCAFile+": "+err.Error()+" in "+file)
		} else {
			p.Roots = roots
		}
	}
	if raw := strings.TrimSpace(getenv(EnvProxy)); raw != "" {
		u, err := ParseProxy(raw)
		if err != nil {
			errs = append(errs, EnvProxy+": "+err.Error())
		} else {
			p.Proxy = u
		}
	}
	if cert, key := strings.TrimSpace(getenv(EnvClientCert)), strings.TrimSpace(getenv(EnvClientKey)); cert != "" || key != "" {
		if c, err := tls.LoadX509KeyPair(cert, key); err != nil {
			errs = append(errs, EnvClientCert+"/"+EnvClientKey+": "+err.Error())
		} else {
			p.ClientCert = &c
		}
	}
	if len(errs) > 0 {
		return p, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return p, nil
}

// AddRoots is the system's roots with the PEM certificates added.
func AddRoots(pem []byte) (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no PEM certificate")
	}
	return roots, nil
}

// ParseProxy reads a proxy address: http(s)://[user:pass@]host:port; a bare host:port is http.
func ParseProxy(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("a proxy is http(s)://host:port")
	}
	return u, nil
}

func splitList(s string) []string {
	var out []string
	for _, h := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' }) {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// IsInternal says whether the admin listed the host as an internal one.
func (p *Policy) IsInternal(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.Internal {
		if suffix, ok := strings.CutPrefix(h, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

// TLSConfig is the client side of TLS for these calls; nil when the system's roots serve and there is no certificate.
func (p *Policy) TLSConfig() *tls.Config {
	if p.Roots == nil && p.ClientCert == nil {
		return nil
	}
	c := &tls.Config{RootCAs: p.Roots, MinVersion: tls.VersionTLS12}
	if p.ClientCert != nil {
		cert := *p.ClientCert
		c.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &cert, nil }
	}
	return c
}

// ProxyFor is the proxy a call to host goes through; nil: direct (no proxy, or the host is an exception).
func (p *Policy) ProxyFor(host string) *url.URL {
	if p.Proxy == nil || p.Bypasses(host) {
		return nil
	}
	return p.Proxy
}

// Bypasses says whether the host is one of the proxy's exceptions: a name, a ".suffix" or "*.suffix" it ends in, an
// address or a CIDR it is in. The internal hosts of the policy are not exceptions by themselves: a bank's proxy often
// reaches them.
func (p *Policy) Bypasses(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	ip, ipErr := netip.ParseAddr(host)
	for _, e := range p.NoProxy {
		switch {
		case e == "*":
			return true
		case strings.Contains(e, "/"):
			if pre, err := netip.ParsePrefix(e); err == nil && ipErr == nil && pre.Contains(ip.Unmap()) {
				return true
			}
		case strings.HasPrefix(e, "*."):
			if strings.HasSuffix(host, e[1:]) {
				return true
			}
		case strings.HasPrefix(e, "."):
			if strings.HasSuffix(host, e) || host == e[1:] {
				return true
			}
		case host == e:
			return true
		}
	}
	return false
}

// RefusedHint is the sentence for a host whose address was refused at the dial: a private or loopback address of a
// host the policy does not list, or an address nobody may reach (cloud metadata).
func RefusedHint(host string, addr netip.Addr, internalListed bool) string {
	addr = addr.Unmap()
	switch {
	case addr.IsValid() && addr.IsLinkLocalUnicast():
		return "адрес " + addr.String() + " (служебный, облачные метаданные) закрыт для всех"
	case internalListed:
		return "адрес " + addr.String() + " хоста «" + host + "» недопустим для вызова"
	}
	return "домен «" + host + "» не в политике контура — попросите администратора добавить его во внутренние домены"
}

// Transport builds the transport for a call to host. Direct, it dials under direct (the control that checks the address
// the name resolved to, at the dial). Through the proxy it opens the tunnel itself (connect.go, HRN-968): the name is
// resolved here, every address passes direct, and the CONNECT goes to the checked address; the proxy, which the admin
// named and which may be on a private address, is dialed with only link-local addresses refused. TLS has the policy's
// roots and certificate; Header is not touched.
func (p *Policy) Transport(host string, direct func(network, address string, c syscall.RawConn) error) *http.Transport {
	t := &http.Transport{
		TLSClientConfig:       p.TLSConfig(),
		ResponseHeaderTimeout: 60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
	}
	if proxy := p.ProxyFor(host); proxy != nil {
		control := func(_, address string, _ syscall.RawConn) error {
			h, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip, err := netip.ParseAddr(h); err == nil && (ip.Unmap().IsLinkLocalUnicast() || ip.IsUnspecified()) {
				return fmt.Errorf("refused address %s", h)
			}
			return nil
		}
		// No Proxy: the transport sees a plain connection (the tunnel) and does TLS to the host by its name over it.
		t.DialContext = p.dialThrough(host, proxy, direct, control)
		return t
	}
	t.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: direct}).DialContext
	return t
}

// Follow is a RoundTripper that builds its transport from the policy of the moment and rebuilds it when the policy is
// set again: a client made before the runtime read the hub's policy still goes by it. build gets the policy; the proxy
// of a request is Policy.ProxyFor when the policy has one, else what build says.
func Follow(build func(p *Policy) *http.Transport) http.RoundTripper { return &follow{build: build} }

type follow struct {
	build func(p *Policy) *http.Transport
	mu    sync.Mutex
	seen  *Policy
	t     *http.Transport
}

func (f *follow) RoundTrip(r *http.Request) (*http.Response, error) {
	p := Current()
	f.mu.Lock()
	if f.t == nil || f.seen != p {
		if f.t != nil {
			f.t.CloseIdleConnections()
		}
		f.t, f.seen = f.build(p), p
	}
	t := f.t
	f.mu.Unlock()
	return t.RoundTrip(r)
}

// ProxyFunc is a transport's Proxy: the policy's proxy for a host that is not an exception when the policy has one,
// else the environment's (HTTPS_PROXY, NO_PROXY).
func (p *Policy) ProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(r *http.Request) (*url.URL, error) {
		if p.Proxy != nil {
			return p.ProxyFor(r.URL.Hostname()), nil
		}
		return http.ProxyFromEnvironment(r)
	}
}
