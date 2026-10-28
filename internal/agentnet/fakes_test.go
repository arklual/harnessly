package agentnet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeSOCKS5 is a SOCKS5 server for tests (RFC 1928, 1929): it connects to whatever target maps gives for the asked one.
type fakeSOCKS5 struct {
	ln     net.Listener
	user   string
	pass   string
	maps   func(target string) string
	mu     sync.Mutex
	asked  []string
	refuse byte // a reply code to answer every connect with
}

func newFakeSOCKS5(t *testing.T, user, pass string, maps func(string) string) *fakeSOCKS5 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSOCKS5{ln: ln, user: user, pass: pass, maps: maps}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *fakeSOCKS5) addr() string { return s.ln.Addr().String() }

func (s *fakeSOCKS5) targets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func (s *fakeSOCKS5) serve(c net.Conn) {
	defer c.Close()
	var head [2]byte
	if _, err := io.ReadFull(c, head[:]); err != nil || head[0] != 5 {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	wantAuth := s.user != ""
	offered := func(m byte) bool { return strings.IndexByte(string(methods), m) >= 0 }
	switch {
	case wantAuth && offered(2):
		_, _ = c.Write([]byte{5, 2})
		var v [2]byte
		if _, err := io.ReadFull(c, v[:]); err != nil {
			return
		}
		u := make([]byte, v[1])
		_, _ = io.ReadFull(c, u)
		var pl [1]byte
		_, _ = io.ReadFull(c, pl[:])
		p := make([]byte, pl[0])
		_, _ = io.ReadFull(c, p)
		if string(u) != s.user || string(p) != s.pass {
			_, _ = c.Write([]byte{1, 1})
			return
		}
		_, _ = c.Write([]byte{1, 0})
	case !wantAuth && offered(0):
		_, _ = c.Write([]byte{5, 0})
	default:
		_, _ = c.Write([]byte{5, 0xff})
		return
	}
	var req [4]byte
	if _, err := io.ReadFull(c, req[:]); err != nil || req[1] != 1 {
		return
	}
	var host string
	switch req[3] {
	case 1:
		var ip [4]byte
		_, _ = io.ReadFull(c, ip[:])
		host = net.IP(ip[:]).String()
	case 3:
		var n [1]byte
		_, _ = io.ReadFull(c, n[:])
		name := make([]byte, n[0])
		_, _ = io.ReadFull(c, name)
		host = string(name)
	case 4:
		var ip [16]byte
		_, _ = io.ReadFull(c, ip[:])
		host = net.IP(ip[:]).String()
	}
	var pb [2]byte
	_, _ = io.ReadFull(c, pb[:])
	target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb[:]))))
	s.mu.Lock()
	s.asked = append(s.asked, target)
	s.mu.Unlock()
	if s.refuse != 0 {
		_, _ = c.Write([]byte{5, s.refuse, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	real := target
	if s.maps != nil {
		real = s.maps(target)
	}
	up, err := net.Dial("tcp", real)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	_, _ = c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
	go func() { _, _ = io.Copy(up, c); _ = up.Close() }()
	_, _ = io.Copy(c, up)
}

// fakeAPI is an HTTPS server standing for a provider: the handler decides what /v1/models answers.
type fakeAPI struct {
	srv  *httptest.Server
	pool *x509.CertPool
}

func newFakeAPI(t *testing.T, h http.HandlerFunc) *fakeAPI {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &fakeAPI{srv: srv, pool: pool}
}

func (f *fakeAPI) addr() string { return strings.TrimPrefix(f.srv.URL, "https://") }

// checker is a Checker whose "example.com" is this server: DNS answers 127.0.0.1, any dial to port 443 reaches it
// (other addresses dial for real, which is what a proxy address needs), and its certificate is trusted.
func (f *fakeAPI) checker() *Checker {
	real := &net.Dialer{}
	return &Checker{
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasSuffix(addr, ":443") {
				return real.DialContext(ctx, network, f.addr())
			}
			return real.DialContext(ctx, network, addr)
		},
		LookupHost: func(_ context.Context, host string) ([]string, error) {
			if host == "example.com" {
				return []string{"127.0.0.1"}, nil
			}
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
		TLS:      &tls.Config{RootCAs: f.pool},
		Hosts:    map[string][]string{"claude": {"example.com"}},
		Requests: map[string]string{"claude": "https://example.com/v1/models"},
	}
}

// httpConnectProxy is a forward proxy for tests: it answers CONNECT by dialing maps(target).
type httpConnectProxy struct {
	*httptest.Server
	mu    sync.Mutex
	auths []string
	deny  int
}

func newHTTPConnectProxy(t *testing.T, maps func(string) string) *httpConnectProxy {
	t.Helper()
	p := &httpConnectProxy{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.auths = append(p.auths, r.Header.Get("Proxy-Authorization"))
		deny := p.deny
		p.mu.Unlock()
		if r.Method != http.MethodConnect {
			http.Error(w, "connect only", http.StatusMethodNotAllowed)
			return
		}
		if deny != 0 {
			http.Error(w, "no", deny)
			return
		}
		up, err := net.Dial("tcp", maps(r.Host))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { _, _ = io.Copy(up, client); _ = up.Close() }()
		_, _ = io.Copy(client, up)
		_ = client.Close()
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *httpConnectProxy) hostport() string { return strings.TrimPrefix(p.URL, "http://") }
