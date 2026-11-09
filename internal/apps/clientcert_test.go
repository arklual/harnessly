package apps

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// bundle is a client certificate and its key as one PEM, the way the secret holds it.
func bundle(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string) string {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) +
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

func manifestWithCert(hosts ...string) Manifest {
	return Manifest{Secrets: []Secret{{Name: "bank-mtls"}},
		Network: &Network{ClientCert: &ClientCert{Secret: "bank-mtls", Hosts: hosts}}}
}

func TestClientCertForOnlyTheAppsOwnHosts(t *testing.T) {
	ca, caKey := testCA(t)
	value := bundle(t, ca, caKey, "app-one")
	read := func(string) (string, error) { return value, nil }

	// No certificate declared: none.
	if c, err := ClientCertFor(Manifest{}, MCPServer{URL: "https://crm.bank.local/mcp"}, read); c != nil || err != nil {
		t.Fatalf("none declared: %v %v", c, err)
	}
	// Declared without hosts: for its servers.
	c, err := ClientCertFor(manifestWithCert(), MCPServer{URL: "https://crm.bank.local/mcp"}, read)
	if err != nil || c == nil {
		t.Fatalf("own server: %v %v", c, err)
	}
	// Declared for one host: another host of the app gets nothing.
	m := manifestWithCert("crm.bank.local")
	if c, _ := ClientCertFor(m, MCPServer{URL: "https://other.bank.local/mcp"}, read); c != nil {
		t.Fatal("the certificate is for crm.bank.local only")
	}
	if c, _ := ClientCertFor(m, MCPServer{URL: "https://CRM.bank.local/mcp"}, read); c == nil {
		t.Fatal("host names compare without case")
	}
	// The secret has no value: the app waits, in words; a value that is no certificate is told.
	_, err = ClientCertFor(m, MCPServer{URL: "https://crm.bank.local/mcp"}, func(string) (string, error) { return "", errors.New("no") })
	if codeOf(err) != CodeHostUnavailable || !strings.Contains(err.Error()+detailOf(err), "client certificate") {
		t.Fatalf("waiting: %v", err)
	}
	_, err = ClientCertFor(m, MCPServer{URL: "https://crm.bank.local/mcp"}, func(string) (string, error) { return "junk", nil })
	if codeOf(err) != CodeHostUnavailable || !strings.Contains(detailOf(err), "bank-mtls") {
		t.Fatalf("junk: %v", err)
	}
}

func detailOf(err error) string {
	var be *BridgeError
	if errors.As(err, &be) {
		return be.Detail
	}
	return ""
}

func testCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ca"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(der)
	return ca, key
}

// A server that wants a client certificate answers only a call that carries the app's own one.
func TestAppCertificateReachesItsBackendAndNoOneElse(t *testing.T) {
	ca, caKey := testCA(t)
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	srv := httptest.NewUnstartedServer(mcpStub(t))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	srv.StartTLS()
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	withNetwork(t, DeployNetwork{Internal: []string{"127.0.0.1"}, Roots: roots})

	// An app without a certificate cannot call the backend.
	if _, err := CallTool(context.Background(), MCPServer{URL: srv.URL}, "t", nil); err == nil {
		t.Fatal("the backend wants a client certificate")
	}
	cert, err := ClientCertFor(manifestWithCert(), MCPServer{URL: srv.URL}, func(string) (string, error) { return bundle(t, ca, caKey, "app-one"), nil })
	if err != nil || cert == nil {
		t.Fatal(err)
	}
	raw, err := CallTool(context.Background(), MCPServer{URL: srv.URL, ClientCert: cert}, "t", nil)
	if err != nil || !strings.Contains(string(raw), "ok tools/call") {
		t.Fatalf("with the app's certificate: %v %s", err, raw)
	}
	// The process policy did not take it: the next app has none.
	if currentNetwork().ClientCert != nil {
		t.Fatal("an app's certificate must not become the machine's")
	}
	if _, err := CallTool(context.Background(), MCPServer{URL: srv.URL}, "t", nil); err == nil {
		t.Fatal("another app does not get it")
	}
	// The proxy path (an agent's MCP client) uses it too.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	ProxyMCP(rec, req, MCPServer{URL: srv.URL, ClientCert: cert}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy with the certificate: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	ProxyMCP(rec, req, MCPServer{URL: srv.URL}, nil)
	if rec.Code == http.StatusOK {
		t.Fatal("proxy without it must fail")
	}
	_ = net.IPv4zero
}

func TestManifestClientCertIsChecked(t *testing.T) {
	withCert := func(network string) Problems {
		raw := strings.Replace(cashback, `"network": { "connect": ["cashback-api.bank.example"] }`, `"network": `+network, 1)
		_, ps := ParseManifest([]byte(raw))
		return ps
	}
	if ps := withCert(`{ "connect": ["cashback-api.bank.example"], "clientCert": { "secret": "bank-token", "hosts": ["cashback-api.bank.example"] } }`); len(ps.Errors()) > 0 {
		t.Fatalf("a declared secret and a real host: %v", ps)
	}
	var secretMissing, hostBad bool
	for _, p := range withCert(`{ "clientCert": { "secret": "nope", "hosts": ["not a host!"] } }`) {
		secretMissing = secretMissing || strings.Contains(p.Path, "clientCert.secret")
		hostBad = hostBad || strings.Contains(p.Path, "clientCert.hosts")
	}
	if !secretMissing || !hostBad {
		t.Fatal("a certificate names a declared secret and real hosts")
	}
}
