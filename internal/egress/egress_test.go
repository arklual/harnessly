package egress

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBypassesAndProxyFor(t *testing.T) {
	p := Policy{Proxy: &url.URL{Scheme: "http", Host: "proxy.example.corp:3128"},
		NoProxy: []string{"localhost", "*.lan.example.corp", ".dev.example.corp", "10.0.0.0/8"}}
	for host, direct := range map[string]bool{
		"localhost": true, "a.lan.example.corp": true, "x.dev.example.corp": true, "dev.example.corp": true,
		"10.1.2.3": true, "11.1.2.3": false, "crm.example.corp": false, "lan.example.corp": false,
	} {
		if got := p.ProxyFor(host) == nil; got != direct {
			t.Errorf("%s: direct=%v, want %v", host, got, direct)
		}
	}
	if (&Policy{}).ProxyFor("a.example.corp") != nil {
		t.Error("no proxy configured")
	}
}

func TestFromEnvAndOverlay(t *testing.T) {
	dir := t.TempDir()
	caPEM, _, _ := selfSigned(t, "ca")
	caFile := filepath.Join(dir, "ca.pem")
	_ = os.WriteFile(caFile, caPEM, 0o600)
	env := map[string]string{
		EnvInternalHosts: "crm.example.corp, *.api.example.corp", EnvCAFile: caFile,
		EnvProxy: "proxy.example.corp:3128", EnvNoProxy: "localhost,.lan",
	}
	p, err := FromEnv(func(k string) string { return env[k] })
	if err != nil || len(p.Internal) != 2 || p.Roots == nil || p.Proxy.Host != "proxy.example.corp:3128" || p.Proxy.Scheme != "http" || len(p.NoProxy) != 2 {
		t.Fatalf("%+v %v", p, err)
	}
	if !p.IsInternal("x.api.example.corp") || p.IsInternal("api.example.corp") {
		t.Error("suffix rule")
	}
	// A bad file is named and the rest is kept.
	env[EnvCAFile] = filepath.Join(dir, "none.pem")
	p, err = FromEnv(func(k string) string { return env[k] })
	if err == nil || !strings.Contains(err.Error(), EnvCAFile) || len(p.Internal) != 2 {
		t.Fatalf("%+v %v", p, err)
	}

	hub, err := Wire{InternalHosts: []string{"a.example.corp"}, Proxy: "http://hubproxy.example.corp:80", NoProxy: []string{"x"}}.Policy()
	if err != nil {
		t.Fatal(err)
	}
	got := Overlay(hub, Policy{Internal: []string{"b.example.corp"}})
	if got.Internal[0] != "b.example.corp" || got.Proxy.Host != "hubproxy.example.corp:80" || got.NoProxy[0] != "x" {
		t.Fatalf("env replaces what it names and leaves the rest: %+v", got)
	}
	if err := (Wire{Proxy: "ftp://x"}).Validate(); err == nil {
		t.Error("a proxy is http(s)")
	}
	if err := (Wire{CAPem: "nope"}).Validate(); err == nil {
		t.Error("a bad PEM is refused")
	}
}

func TestRefusedHint(t *testing.T) {
	if h := RefusedHint("crm.example.corp", netip.MustParseAddr("10.0.0.5"), false); !strings.Contains(h, "«crm.example.corp»") || !strings.Contains(h, "политике контура") || !strings.Contains(h, "администратор") {
		t.Error(h)
	}
	if h := RefusedHint("x", netip.MustParseAddr("169.254.169.254"), true); !strings.Contains(h, "закрыт для всех") {
		t.Error(h)
	}
}

// The company's CA, a client certificate and a proxy, through one transport.
func TestTransportCAClientCertAndProxy(t *testing.T) {
	caPEM, caCert, caKey := selfSigned(t, "corp-ca")
	srvCert := issue(t, caCert, caKey, "127.0.0.1", false)
	clientCert := issue(t, caCert, caKey, "client", true)
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.TLS.PeerCertificates[0].Subject.CommonName)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{srvCert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	srv.StartTLS()
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	allow := func(_, _ string, _ syscall.RawConn) error { return nil }
	get := func(p Policy) (string, error) {
		c := &http.Client{Transport: p.Transport(u.Hostname(), allow), Timeout: 5 * time.Second}
		res, err := c.Get(srv.URL)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b), nil
	}
	roots, _ := AddRoots(caPEM)
	if _, err := get(Policy{}); err == nil {
		t.Fatal("the system's roots do not know the company's CA")
	}
	if _, err := get(Policy{Roots: roots}); err == nil {
		t.Fatal("the server wants a client certificate")
	}
	if got, err := get(Policy{Roots: roots, ClientCert: &clientCert}); err != nil || got != "client" {
		t.Fatalf("%q %v", got, err)
	}

	// Through the proxy: a CONNECT to the server; the proxy is on a loopback address, which the direct rule would refuse.
	var connects []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connects = append(connects, r.Host)
		dst, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(200)
		cl, _, _ := w.(http.Hijacker).Hijack()
		go func() { _, _ = io.Copy(dst, cl); dst.Close() }()
		_, _ = io.Copy(cl, dst)
		cl.Close()
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	deny := func(_, _ string, _ syscall.RawConn) error { return net.ErrClosed }
	p := Policy{Roots: roots, ClientCert: &clientCert, Proxy: pu}
	// An address literal is judged by the direct rule even through a proxy: refused, the proxy is not asked.
	c := &http.Client{Transport: p.Transport("crm.example.corp", deny), Timeout: 5 * time.Second}
	if _, err := c.Get(srv.URL); err == nil || len(connects) != 0 {
		t.Fatalf("a refused address literal went to the proxy: %v %v", err, connects)
	}
	c = &http.Client{Transport: p.Transport("crm.example.corp", allow), Timeout: 5 * time.Second}
	// The host name is not resolvable here: the proxy would; the request goes to the server by its address instead.
	res, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("through the proxy: %v", err)
	}
	res.Body.Close()
	if len(connects) != 1 || connects[0] != u.Host {
		t.Fatalf("the proxy was not used: %v", connects)
	}
	// An exception goes direct, under the direct rule (here: denied).
	p.NoProxy = []string{"crm.example.corp"}
	c = &http.Client{Transport: p.Transport("crm.example.corp", deny), Timeout: 5 * time.Second}
	if _, err := c.Get(srv.URL); err == nil || len(connects) != 1 {
		t.Fatalf("an exception goes direct: %v %v", err, connects)
	}
}

func TestFollowRebuildsWhenThePolicyChanges(t *testing.T) {
	t.Cleanup(func() { Set(Policy{}) })
	built := 0
	rt := Follow(func(p *Policy) *http.Transport { built++; return &http.Transport{} })
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	do := func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		res, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	Set(Policy{})
	do()
	do()
	if built != 1 {
		t.Fatalf("built %d", built)
	}
	Set(Policy{Internal: []string{"a"}})
	do()
	if built != 2 {
		t.Fatalf("not rebuilt: %d", built)
	}
}

func TestKeepTakesTheHubsPolicyAndKeepsItForARestart(t *testing.T) {
	t.Cleanup(func() { Set(Policy{}) })
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dl/network.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"internalHosts":["*.example.corp"],"proxy":"http://proxy.example.corp:3128"}`)
	}))
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	Keep(ctx, hub.URL, dir, Policy{NoProxy: []string{"localhost"}}, func(string, error) {})
	deadline := time.Now().Add(5 * time.Second)
	for !Current().IsInternal("crm.example.corp") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if p := Current(); !p.IsInternal("crm.example.corp") || p.Proxy == nil || len(p.NoProxy) != 1 {
		t.Fatalf("%+v", p)
	}
	cancel()
	hub.Close()
	// A restart with the hub gone: the kept policy is taken before anything is asked.
	Set(Policy{})
	Keep(context.Background(), "", dir, Policy{}, func(string, error) {})
	if p := Current(); !p.IsInternal("crm.example.corp") {
		t.Fatalf("%+v", p)
	}
}

func selfSigned(t *testing.T, cn string) ([]byte, *x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert, key
}

func issue(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, name string, client bool) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	if client {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tpl.IPAddresses = []net.IP{net.ParseIP(name)}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
