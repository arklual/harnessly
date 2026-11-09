package apps

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
)

// ClientCertFor is the certificate an app presents to the MCP server srv (network.clientCert of its manifest): the PEM
// in the app's secret, for the hosts the manifest names, or for the app's own servers when it names none. nil when the app
// has none or srv is another host. pem reads the secret; a secret with no value, or one that is no certificate with its
// key, is told in words: the call is not made without the certificate the app needs.
func ClientCertFor(m Manifest, srv MCPServer, pem func(secret string) (string, error)) (*tls.Certificate, error) {
	if m.Network == nil || m.Network.ClientCert == nil {
		return nil, nil
	}
	cc := m.Network.ClientCert
	u, err := url.Parse(srv.URL)
	if err != nil || u.Hostname() == "" {
		return nil, nil
	}
	if len(cc.Hosts) > 0 {
		host, listed := strings.ToLower(u.Hostname()), false
		for _, h := range cc.Hosts {
			listed = listed || strings.ToLower(h) == host
		}
		if !listed {
			return nil, nil
		}
	}
	value, err := pem(cc.Secret)
	if err != nil || strings.TrimSpace(value) == "" {
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: ToolRefused,
			Detail: fmt.Sprintf("the app waits for its client certificate (secret %q)", cc.Secret)}
	}
	cert, err := tls.X509KeyPair([]byte(value), []byte(value))
	if err != nil {
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: ToolRefused,
			Detail: fmt.Sprintf("secret %q is not a certificate with its key in PEM: %v", cc.Secret, err)}
	}
	return &cert, nil
}
