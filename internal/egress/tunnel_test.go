package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"syscall"
	"testing"
	"time"
)

// publicOnly is the direct rule of an app for a host that is not internal: public addresses only.
func publicOnly(_, address string, _ syscall.RawConn) error {
	h, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(h)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return fmt.Errorf("refused address %s", ip)
	}
	return nil
}

// resolveAs swaps the resolver for the test: names map to their addresses, others are not found.
func resolveAs(t *testing.T, names map[string][]string) *[]string {
	asked := &[]string{}
	old := lookupIP
	t.Cleanup(func() { lookupIP = old })
	lookupIP = func(_ context.Context, host string) ([]netip.Addr, error) {
		*asked = append(*asked, host)
		var out []netip.Addr
		for _, s := range names[host] {
			out = append(out, netip.MustParseAddr(s))
		}
		if len(out) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return out, nil
	}
	return asked
}

// tunnelProxy is a CONNECT proxy that records what it was asked for and tunnels to 127.0.0.1 whatever the target.
func tunnelProxy(t *testing.T) (*url.URL, func() []string) {
	var mu sync.Mutex
	var connects []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connects = append(connects, r.Host)
		mu.Unlock()
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
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
	t.Cleanup(proxy.Close)
	pu, _ := url.Parse(proxy.URL)
	return pu, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), connects...)
	}
}

// HRN-968: a name that resolves to the cloud's metadata or to a private address does not pass through the proxy:
// the machine resolves it, the direct rule judges every address, and the proxy is not asked.
func TestANameThatResolvesToAClosedAddressDoesNotPassThroughTheProxy(t *testing.T) {
	asked := resolveAs(t, map[string][]string{
		"metadata.attacker.example": {"169.254.169.254"},
		"lan.attacker.example":      {"10.0.0.5"},
		"mixed.attacker.example":    {"93.184.216.34", "10.1.2.3"},
	})
	pu, connects := tunnelProxy(t)
	p := Policy{Proxy: pu}
	for _, host := range []string{"metadata.attacker.example", "lan.attacker.example", "mixed.attacker.example"} {
		c := &http.Client{Transport: p.Transport(host, publicOnly), Timeout: 5 * time.Second}
		if _, err := c.Get("http://" + host + "/"); err == nil {
			t.Fatalf("%s went through", host)
		}
	}
	if got := connects(); len(got) != 0 {
		t.Fatalf("the proxy was asked: %v", got)
	}
	if len(*asked) != 3 {
		t.Fatalf("the names were not resolved here: %v", *asked)
	}
}

// HRN-968: the tunnel is opened to the address that was checked, not to the name the proxy would resolve again.
func TestTheTunnelGoesToTheCheckedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Host)
	}))
	defer srv.Close()
	su, _ := url.Parse(srv.URL)
	resolveAs(t, map[string][]string{"api.example.com": {"127.0.0.1"}})
	pu, connects := tunnelProxy(t)
	p := Policy{Proxy: pu}
	var judged []string
	allowLoopback := func(_, address string, _ syscall.RawConn) error {
		judged = append(judged, address)
		return nil
	}
	c := &http.Client{Transport: p.Transport("api.example.com", allowLoopback), Timeout: 5 * time.Second}
	res, err := c.Get("http://api.example.com:" + su.Port() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	want := "127.0.0.1:" + su.Port()
	if got := connects(); len(got) != 1 || got[0] != want {
		t.Fatalf("CONNECT %v, want %s", got, want)
	}
	if len(judged) != 1 || judged[0] != want {
		t.Fatalf("judged %v", judged)
	}
	if string(body) != "api.example.com:"+su.Port() {
		t.Fatalf("the request lost its host: %q", body)
	}
}

// HRN-968: a host that is a number in any but the canonical form is refused before the resolver and the proxy.
func TestANumericHostMustBeACanonicalAddress(t *testing.T) {
	asked := resolveAs(t, nil)
	p := Policy{}
	for _, host := range []string{"2130706433", "0x7f.1", "127.1", "0177.0.0.1", "0x7f000001", "0X7F.0.0.1", "169.254.169.254.", "a.0x10"} {
		target, err := p.tunnelTarget(context.Background(), host, "80", "", publicOnly)
		if host == "169.254.169.254." {
			// The canonical form with a trailing dot is an address: judged, refused by the rule.
			if err == nil || errors.Is(err, ErrNumericHost) {
				t.Fatalf("%s: %q %v", host, target, err)
			}
			continue
		}
		if !errors.Is(err, ErrNumericHost) {
			t.Fatalf("%s: %q %v", host, target, err)
		}
	}
	if len(*asked) != 0 {
		t.Fatalf("a numeric host was resolved: %v", *asked)
	}
	for _, host := range []string{"api.example.com", "x1.example.com", "0x.example", "cafe.example.com"} {
		if numericHost(host) {
			t.Fatalf("%s is a name", host)
		}
	}
}

// A host the admin listed as internal that the machine cannot resolve goes to the proxy by name; any other such name
// fails here.
func TestAnInternalNameUnknownHereGoesToTheProxyByName(t *testing.T) {
	resolveAs(t, nil)
	p := Policy{Internal: []string{"*.example.corp"}}
	target, err := p.tunnelTarget(context.Background(), "crm.example.corp", "443", "crm.example.corp", publicOnly)
	if err != nil || target != "crm.example.corp:443" {
		t.Fatalf("%q %v", target, err)
	}
	if target, err := p.tunnelTarget(context.Background(), "crm.example.com", "443", "crm.example.corp", publicOnly); err == nil {
		t.Fatalf("an unlisted name unknown here: %q", target)
	}
	// Another internal name met on a transport built for a public host is not let through by name.
	pu, connects := tunnelProxy(t)
	p.Proxy = pu
	c := &http.Client{Transport: p.Transport("api.example.com", publicOnly), Timeout: 5 * time.Second}
	if _, err := c.Get("http://crm.example.corp/"); err == nil || len(connects()) != 0 {
		t.Fatalf("an internal name on a public host's transport: %v %v", err, connects())
	}
	c = &http.Client{Transport: p.Transport("crm.example.corp", publicOnly), Timeout: 5 * time.Second}
	_, _ = c.Get("http://crm.example.corp/")
	if got := connects(); len(got) != 1 || got[0] != "crm.example.corp:80" {
		t.Fatalf("the listed host goes by name: %v", got)
	}
}
