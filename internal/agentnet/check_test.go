package agentnet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func jsonError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"x-api-key header is required"}}`))
}

func TestCheckVerdicts(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		verdict string
		status  int
		detail  string
	}{
		{"401 with the provider's JSON error is reachable", jsonError, "ok", 401, ""},
		{"200 is ok", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }, "ok", 200, ""},
		{"401 from a filter page is rejected", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte("<html>Corporate proxy: sign in</html>"))
		}, "rejected", 401, "Corporate proxy"},
		{"403 shows the server's own words", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
			_, _ = w.Write([]byte("Request not allowed"))
		}, "rejected", 403, "Request not allowed"},
		{"500", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, "rejected", 500, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := newFakeAPI(t, tc.handler)
			got := api.checker().Check(context.Background(), "claude", nil)
			if got.Verdict != tc.verdict {
				t.Fatalf("verdict %s: %+v", got.Verdict, got)
			}
			if got.Status == nil || *got.Status != tc.status {
				t.Fatalf("status %v", got.Status)
			}
			if tc.detail != "" && (got.Detail == nil || !strings.Contains(*got.Detail, tc.detail)) {
				t.Fatalf("detail %v", got.Detail)
			}
			if got.Via != "direct" || got.Host != "example.com" || got.Provider != "claude" || got.At == 0 {
				t.Fatalf("%+v", got)
			}
			if len(got.Hosts) != 1 || !got.Hosts[0].DNS || !got.Hosts[0].TCP || !got.Hosts[0].TLS || got.Hosts[0].Error != nil {
				t.Fatalf("hosts %+v", got.Hosts)
			}
		})
	}
}

func TestCheckRejectedBodyIsCut(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(strings.Repeat("я", 1000)))
	})
	got := api.checker().Check(context.Background(), "claude", nil)
	if got.Detail == nil || len([]rune(*got.Detail)) != 300 {
		t.Fatalf("detail %v", got.Detail)
	}
}

func TestCheckNetworkFailures(t *testing.T) {
	api := newFakeAPI(t, jsonError)
	t.Run("name not found", func(t *testing.T) {
		c := api.checker()
		c.Hosts = map[string][]string{"claude": {"example.com", "nx.example.invalid"}}
		got := c.Check(context.Background(), "claude", nil)
		if got.Verdict != "network" || got.Host != "nx.example.invalid" || got.Detail == nil || *got.Detail != "no such host" {
			t.Fatalf("%+v", got)
		}
		bad := got.Hosts[1]
		if bad.DNS || bad.TCP || bad.TLS || bad.Error == nil {
			t.Fatalf("%+v", bad)
		}
		// The host that worked is still reported as it was.
		if good := got.Hosts[0]; !good.TLS || good.Error != nil {
			t.Fatalf("%+v", good)
		}
		if got.Status != nil {
			t.Fatal("no request is made after a failed host")
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		c := api.checker()
		c.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
		}
		got := c.Check(context.Background(), "claude", nil)
		if got.Verdict != "network" || got.Detail == nil || *got.Detail != "connection refused" {
			t.Fatalf("%+v", got)
		}
		if h := got.Hosts[0]; !h.DNS || h.TCP {
			t.Fatalf("DNS passed and TCP did not: %+v", h)
		}
	})
	t.Run("certificate", func(t *testing.T) {
		c := api.checker()
		c.TLS = nil // the system's roots do not know the test server
		got := c.Check(context.Background(), "claude", nil)
		if got.Verdict != "network" || got.Detail == nil || !strings.HasPrefix(*got.Detail, "certificate: ") {
			t.Fatalf("%+v", got)
		}
		if h := got.Hosts[0]; !h.TCP || h.TLS {
			t.Fatalf("TCP passed and TLS did not: %+v", h)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		c := api.checker()
		c.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		got := c.Check(ctx, "claude", nil)
		if got.Verdict != "network" || got.Detail == nil || *got.Detail != "timeout" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("the light request fails after the hosts passed", func(t *testing.T) {
		c := api.checker()
		c.Requests = map[string]string{"claude": "https://example.com:1/v1/models"} // a port nothing listens on
		c.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasSuffix(addr, ":443") {
				return (&net.Dialer{}).DialContext(ctx, network, api.addr())
			}
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
		}
		got := c.Check(context.Background(), "claude", nil)
		if got.Verdict != "network" || got.Host != "example.com" || got.Detail == nil || *got.Detail != "connection refused" || got.Status != nil {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("unknown provider", func(t *testing.T) {
		got := (&Checker{}).Check(context.Background(), "gemini", nil)
		if got.Verdict != "network" || got.Detail == nil {
			t.Fatalf("%+v", got)
		}
	})
}

func TestCheckThroughHTTPProxy(t *testing.T) {
	api := newFakeAPI(t, jsonError)
	proxy := newHTTPConnectProxy(t, func(string) string { return api.addr() })
	c := api.checker()
	pu, err := ParseProxy("http://ann:s3cret@" + proxy.hostport())
	if err != nil {
		t.Fatal(err)
	}
	got := c.Check(context.Background(), "claude", &pu)
	if got.Verdict != "ok" || got.Via != "proxy" {
		t.Fatalf("%+v", got)
	}
	if h := got.Hosts[0]; !h.DNS || !h.TCP || !h.TLS {
		t.Fatalf("%+v", h)
	}
	proxy.mu.Lock()
	auths := append([]string(nil), proxy.auths...)
	proxy.mu.Unlock()
	if len(auths) < 2 { // one CONNECT for the host check, one for the light request
		t.Fatalf("the proxy saw %d requests", len(auths))
	}
	for _, a := range auths {
		if a != "Basic YW5uOnMzY3JldA==" {
			t.Fatalf("login not sent: %q", a)
		}
	}
	// A proxy that refuses is a network verdict that says so, without the password.
	proxy.mu.Lock()
	proxy.deny = http.StatusForbidden
	proxy.mu.Unlock()
	got = c.Check(context.Background(), "claude", &pu)
	if got.Verdict != "network" || got.Detail == nil || !strings.Contains(*got.Detail, "refused the CONNECT") || strings.Contains(*got.Detail, "s3cret") {
		t.Fatalf("%+v", got)
	}
}

func TestCheckThroughSOCKS5(t *testing.T) {
	api := newFakeAPI(t, jsonError)
	socks := newFakeSOCKS5(t, "ann", "s3cret", func(string) string { return api.addr() })
	pu, err := ParseProxy("socks5://ann:s3cret@" + socks.addr())
	if err != nil {
		t.Fatal(err)
	}
	got := api.checker().Check(context.Background(), "claude", &pu)
	if got.Verdict != "ok" || got.Via != "proxy" {
		t.Fatalf("%+v", got)
	}
	// The name goes to the proxy to resolve, not the machine's resolver.
	for _, target := range socks.targets() {
		if target != "example.com:443" {
			t.Fatalf("the proxy was asked for %q", target)
		}
	}
	if len(socks.targets()) < 2 {
		t.Fatalf("targets %v", socks.targets())
	}
	bad, _ := ParseProxy("socks5://ann:wrong@" + socks.addr())
	got = api.checker().Check(context.Background(), "claude", &bad)
	if got.Verdict != "network" || got.Detail == nil || !strings.Contains(*got.Detail, "refused the login") {
		t.Fatalf("%+v", got)
	}
}

func TestProviderTables(t *testing.T) {
	for provider, first := range map[string]string{"claude": "api.anthropic.com", "codex": "api.openai.com"} {
		if !Known(provider) || providerHosts[provider][0] != first || len(providerHosts[provider]) != 3 {
			t.Errorf("%s: %v", provider, providerHosts[provider])
		}
		if !strings.HasPrefix(lightRequests[provider], "https://"+first+"/v1/models") {
			t.Errorf("%s: %s", provider, lightRequests[provider])
		}
	}
	if Known("gemini") {
		t.Error("gemini is not implemented")
	}
}

// A key is accepted on 200, rejected on 401 and 403, and no verdict on anything else; it travels in the header the
// provider documents and nowhere else.
func TestCheckKey(t *testing.T) {
	var seen http.Header
	status := 200
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c := &Checker{TLS: &tls.Config{RootCAs: pool}, Requests: map[string]string{"claude": srv.URL + "/v1/models", "codex": srv.URL + "/v1/models"}}
	for _, tc := range []struct {
		provider string
		status   int
		verdict  string
	}{
		{"claude", 200, "accepted"}, {"claude", 401, "rejected"}, {"codex", 403, "rejected"}, {"codex", 200, "accepted"},
		{"claude", 429, "network"}, {"codex", 503, "network"},
	} {
		status = tc.status
		got := c.CheckKey(context.Background(), tc.provider, "sk-secret", nil)
		if got.Verdict != tc.verdict || !got.Present || got.Provider != tc.provider || got.Status == nil || *got.Status != tc.status {
			t.Errorf("%+v: %+v", tc, got)
		}
		if tc.provider == "claude" && (seen.Get("x-api-key") != "sk-secret" || seen.Get("anthropic-version") != "2023-06-01" || seen.Get("Authorization") != "") {
			t.Errorf("claude headers %v", seen)
		}
		if tc.provider == "codex" && (seen.Get("Authorization") != "Bearer sk-secret" || seen.Get("x-api-key") != "") {
			t.Errorf("codex headers %v", seen)
		}
	}
	if got := c.CheckKey(context.Background(), "claude", "", nil); got.Verdict != "absent" || got.Present || got.Status != nil {
		t.Errorf("%+v", got)
	}
	// Nobody home: network, with no status.
	srv.Close()
	if got := c.CheckKey(context.Background(), "claude", "sk-secret", nil); got.Verdict != "network" || got.Status != nil || !got.Present {
		t.Errorf("%+v", got)
	}
}
