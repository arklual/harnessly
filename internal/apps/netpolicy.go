package apps

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
)

// DeployNetwork is what the admin of a deployment lets the apps' MCP calls (a view's hl.tools.call, a Sinopia check) reach
// besides the public internet: internal hosts by name, and the organization's root certificates. An app cannot widen
// it: the list comes from the machine's environment (RUNTIME_APPS_INTERNAL_HOSTS, RUNTIME_APPS_CA_FILE).
type DeployNetwork struct {
	// Internal are host names ("gitlab.corp") or suffixes ("*.corp", any name under corp) whose addresses may be
	// private, loopback or shared (100.64.0.0/10). Link-local addresses (cloud metadata) stay closed even to them.
	Internal []string
	// Roots are the certificates a server's chain may end in besides the system's; nil: the system's only.
	Roots *x509.CertPool
}

var appNetwork atomic.Pointer[DeployNetwork]

// SetNetwork sets the deployment's network for the apps' calls.
func SetNetwork(n DeployNetwork) { appNetwork.Store(&n) }

func currentNetwork() *DeployNetwork {
	if n := appNetwork.Load(); n != nil {
		return n
	}
	return &DeployNetwork{}
}

// NetworkFromEnv reads the deployment's network: RUNTIME_APPS_INTERNAL_HOSTS (comma-separated names and *.suffixes)
// and RUNTIME_APPS_CA_FILE (PEM certificates added to the system's roots).
func NetworkFromEnv(getenv func(string) string) (DeployNetwork, error) {
	var n DeployNetwork
	for _, h := range strings.Split(getenv("RUNTIME_APPS_INTERNAL_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			n.Internal = append(n.Internal, h)
		}
	}
	if file := strings.TrimSpace(getenv("RUNTIME_APPS_CA_FILE")); file != "" {
		pem, err := os.ReadFile(file)
		if err != nil {
			return n, fmt.Errorf("RUNTIME_APPS_CA_FILE: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return n, fmt.Errorf("RUNTIME_APPS_CA_FILE: no PEM certificate in %s", file)
		}
		n.Roots = roots
	}
	return n, nil
}

// IsInternal says whether the admin listed the host as an internal one.
func (n *DeployNetwork) IsInternal(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range n.Internal {
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

func (n *DeployNetwork) tlsConfig() *tls.Config {
	if n.Roots == nil {
		return nil
	}
	return &tls.Config{RootCAs: n.Roots, MinVersion: tls.VersionTLS12}
}

// internalAddress says whether an internal host may be dialed at the address its name gave: any unicast address of
// the host's network, but never a link-local one (169.254.0.0/16, fe80::/10: the cloud's metadata service).
func internalAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

// dialControl is the dialer's Control for a call to host: it sees the address the connection really goes to (after
// DNS), so a name that answers a public address to a check and a private one to the dial cannot get through. A host
// the admin listed may be on the internal network; any other has to be public.
func dialControl(internal bool) func(_, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		ok := err == nil && publicAddress(ip)
		if !ok && internal && err == nil {
			ok = internalAddress(ip)
		}
		if !ok {
			return &BridgeError{Code: CodeNetworkDenied, Message: "refused"}
		}
		return nil
	}
}
