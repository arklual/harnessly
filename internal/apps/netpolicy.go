package apps

import (
	"net"
	"net/netip"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/egress"
)

// DeployNetwork is what the admin of a deployment lets the apps' MCP calls (a view's hl.tools.call, a Sinopia check) reach
// besides the public internet: internal hosts by name, the organization's root certificates, its proxy and a client
// certificate. It is the one policy of internal/egress, which Connections share. An app cannot widen it.
type DeployNetwork = egress.Policy

// SetNetwork sets the deployment's network for the apps' calls.
func SetNetwork(n DeployNetwork) { egress.Set(n) }

func currentNetwork() *DeployNetwork { return egress.Current() }

// NetworkFromEnv reads the machine's overrides of the deployment's network (egress.FromEnv).
func NetworkFromEnv(getenv func(string) string) (DeployNetwork, error) { return egress.FromEnv(getenv) }

// internalAddress says whether an internal host may be dialed at the address its name gave: any unicast address of
// the host's network, but never a link-local one (169.254.0.0/16, fe80::/10: the cloud's metadata service).
func internalAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

// dialControl is the dialer's Control for a call to host: it sees the address the connection really goes to (after
// DNS), so a name that answers a public address to a check and a private one to the dial cannot get through. A host
// the admin listed may be on the internal network; any other has to be public.
func dialControl(host string, internal bool) func(_, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		addrHost, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(addrHost)
		ok := err == nil && publicAddress(ip)
		if !ok && internal && err == nil {
			ok = internalAddress(ip)
		}
		if !ok {
			return &BridgeError{Code: CodeNetworkDenied, Message: "refused", Hint: egress.RefusedHint(host, ip, internal)}
		}
		return nil
	}
}
