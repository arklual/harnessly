package agentnet

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseProxy(t *testing.T) {
	ok := []struct {
		in, scheme, hostport, user, pass string
	}{
		{"http://proxy.example.com:3128", "http", "proxy.example.com:3128", "", ""},
		{"HTTP://Proxy.Example.com:3128/", "http", "proxy.example.com:3128", "", ""},
		{"https://p.example.com", "https", "p.example.com:443", "", ""},
		{"socks5://10.0.0.1:1081", "socks5", "10.0.0.1:1081", "", ""},
		{"socks5h://u:p@10.0.0.1", "socks5", "10.0.0.1:1080", "u", "p"},
		{"socks5://u%40x:p%3Aq@[::1]:1080", "socks5", "[::1]:1080", "u@x", "p:q"},
		{"  http://a:1  ", "http", "a:1", "", ""},
	}
	for _, tc := range ok {
		p, err := ParseProxy(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if p.Scheme != tc.scheme || p.HostPort() != tc.hostport || p.User != tc.user || p.Pass != tc.pass || p.HasAuth != (tc.user != "") {
			t.Errorf("%q: %+v", tc.in, p)
		}
	}
	bad := []string{"", "   ", "proxy.example.com:3128", "ftp://a:1", "http://", "http://:80", "http://a:0", "http://a:99999",
		"http://a:b", "http://a:1/path", "http://a:1?x=1", "http://a:1#f", "http://a b:1", "http://a:1\nx", "socks4://a:1",
		"http://" + strings.Repeat("a", 3000)}
	for _, in := range bad {
		if _, err := ParseProxy(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	// An error never repeats the password.
	if _, err := ParseProxy("ftp://u:hunter2@a:1"); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("%v", err)
	}
}

func TestProxyEntryHidesLogin(t *testing.T) {
	p, _ := ParseProxy("socks5://u:hunter2@p.example.com:1080")
	e := p.Entry("127.0.0.1:18501")
	if e.Scheme != "socks5" || e.Host != "p.example.com:1080" || e.Bridge == nil || *e.Bridge != "127.0.0.1:18501" {
		t.Fatalf("%+v", e)
	}
	if p.Entry("").Bridge != nil {
		t.Fatal("a bridge without one")
	}
}

func newProxies(t *testing.T) (*Proxies, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent-env")
	p := NewProxies(NewEnvDir(dir))
	t.Cleanup(p.Close)
	return p, dir
}

func TestProxiesHTTP(t *testing.T) {
	p, dir := newProxies(t)
	sum, err := p.Set(ScopeAll, "http://ann:s3cret@proxy.example.com:3128")
	if err != nil {
		t.Fatal(err)
	}
	if sum.All == nil || sum.All.Host != "proxy.example.com:3128" || sum.All.Bridge != nil || sum.Claude != nil || sum.Codex != nil {
		t.Fatalf("%+v", sum)
	}
	if strings.Contains(fmt.Sprintf("%+v", sum.All), "s3cret") {
		t.Fatal("the summary has the password")
	}
	env := NewEnvDir(dir)
	for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
		if v, _ := env.Get(ScopeAll, name); v != "http://ann:s3cret@proxy.example.com:3128" {
			t.Errorf("%s = %q", name, v)
		}
	}
	if v, _ := env.Get(ScopeAll, "NO_PROXY"); !strings.Contains(v, "127.0.0.1") || !strings.Contains(v, "localhost") {
		t.Errorf("NO_PROXY = %q", v)
	}
	st, err := os.Stat(filepath.Join(dir, "proxy.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("proxy.json: %v %v", st, err)
	}
	// One agent's own proxy.
	if _, err := p.Set(ScopeCodex, "https://other.example.com:8443"); err != nil {
		t.Fatal(err)
	}
	if v, _ := env.Get(ScopeCodex, "HTTPS_PROXY"); v != "https://other.example.com:8443" {
		t.Errorf("codex: %q", v)
	}
	if _, ok := env.Get(ScopeClaude, "HTTPS_PROXY"); ok {
		t.Error("claude got codex's proxy")
	}
	if e, ok := p.Effective(ScopeClaude); !ok || e.Host != "proxy.example.com" {
		t.Errorf("claude falls back to the shared one: %+v", e)
	}
	if e, ok := p.Effective(ScopeCodex); !ok || e.Host != "other.example.com" {
		t.Errorf("codex has its own: %+v", e)
	}
	// Clearing.
	sum, err = p.Clear(ScopeCodex)
	if err != nil || sum.Codex != nil || sum.All == nil {
		t.Fatalf("%+v %v", sum, err)
	}
	if _, ok := env.Get(ScopeCodex, "HTTPS_PROXY"); ok {
		t.Error("codex file still holds the proxy")
	}
	if _, err := p.Clear(ScopeAll); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Effective(ScopeClaude); ok {
		t.Error("a proxy after clearing")
	}
	if _, err := os.Stat(filepath.Join(dir, "proxy.json")); !os.IsNotExist(err) {
		t.Errorf("proxy.json is left: %v", err)
	}
}

func TestProxiesKeepSecretsInTheSameFile(t *testing.T) {
	p, dir := newProxies(t)
	env := NewEnvDir(dir)
	if err := env.Set(ScopeAll, map[string]string{"GH_TOKEN": "ghp_x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Set(ScopeAll, "http://a.example.com:1"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Clear(ScopeAll); err != nil {
		t.Fatal(err)
	}
	if v, ok := env.Get(ScopeAll, "GH_TOKEN"); !ok || v != "ghp_x" {
		t.Fatalf("the proxy took the secret with it: %q %v", v, ok)
	}
}

func TestProxiesRefusals(t *testing.T) {
	p, _ := newProxies(t)
	if _, err := p.Set("gemini", "http://a:1"); err == nil {
		t.Error("unknown scope")
	}
	if _, err := p.Set(ScopeAll, "nonsense"); err == nil {
		t.Error("bad url")
	}
	if _, err := p.Clear("gemini"); err == nil {
		t.Error("clearing an unknown scope")
	}
	if got := p.Summary(); got.All != nil {
		t.Errorf("%+v", got)
	}
}

// A SOCKS proxy gets a bridge on loopback; the env file points at the bridge, with its token.
func TestProxiesSOCKSBridgeAndRestart(t *testing.T) {
	api := newFakeAPI(t, jsonError)
	socks := newFakeSOCKS5(t, "ann", "s3cret", func(string) string { return api.addr() })
	dir := filepath.Join(t.TempDir(), "agent-env")
	env := NewEnvDir(dir)
	p := NewProxies(env)
	sum, err := p.Set(ScopeClaude, "socks5://ann:s3cret@"+socks.addr())
	if err != nil {
		t.Fatal(err)
	}
	e := sum.Claude
	if e == nil || e.Scheme != "socks5" || e.Bridge == nil || !strings.HasPrefix(*e.Bridge, "127.0.0.1:") {
		t.Fatalf("%+v", e)
	}
	via, _ := env.Get(ScopeClaude, "HTTPS_PROXY")
	u, err := url.Parse(via)
	if err != nil || u.Scheme != "http" || u.Host != *e.Bridge || u.User.Username() != BridgeUser {
		t.Fatalf("env points at %q (%v)", via, err)
	}
	if strings.Contains(via, "s3cret") {
		t.Fatal("the SOCKS password is in the env file the agents can read")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "proxy.json"))
	if !strings.Contains(string(raw), "bridgeToken") {
		t.Fatalf("proxy.json: %s", raw)
	}

	// The same settings come back after a restart, on the same port.
	port := *e.Bridge
	p.Close()
	p2 := NewProxies(env)
	t.Cleanup(p2.Close)
	if err := p2.Start(); err != nil {
		t.Fatal(err)
	}
	again := p2.Summary().Claude
	if again == nil || again.Bridge == nil || *again.Bridge != port {
		t.Fatalf("after restart: %+v (was %s)", again, port)
	}
	if got, _ := env.Get(ScopeClaude, "HTTPS_PROXY"); got != via {
		t.Fatalf("the env file changed: %q", got)
	}

	// If the port is taken meanwhile, the bridge moves and the env file follows.
	p2.Close()
	hold, err := net.Listen("tcp", port)
	if err != nil {
		t.Skipf("port %s is taken already: %v", port, err)
	}
	defer hold.Close()
	p3 := NewProxies(env)
	t.Cleanup(p3.Close)
	if err := p3.Start(); err != nil {
		t.Fatal(err)
	}
	moved := p3.Summary().Claude
	if moved == nil || *moved.Bridge == port {
		t.Fatalf("the bridge did not move: %+v", moved)
	}
	if got, _ := env.Get(ScopeClaude, "HTTPS_PROXY"); !strings.Contains(got, *moved.Bridge) {
		t.Fatalf("env file %q does not follow the bridge %s", got, *moved.Bridge)
	}
}

func TestProxiesStartDropsUnreadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-env")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy.json"), []byte(`{"all":{"url":"nonsense"},"codex":{"url":"http://ok.example.com:1"},"bogus":{"url":"http://x:1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewProxies(NewEnvDir(dir))
	t.Cleanup(p.Close)
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	s := p.Summary()
	if s.All != nil || s.Codex == nil {
		t.Fatalf("%+v", s)
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewProxies(NewEnvDir(dir)).Start(); err == nil {
		t.Error("a broken proxy.json is silently ignored")
	}
}

func TestSOCKS5DialErrors(t *testing.T) {
	api := newFakeAPI(t, jsonError)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dial := (&net.Dialer{}).DialContext

	socks := newFakeSOCKS5(t, "", "", func(string) string { return api.addr() })
	pu, _ := ParseProxy("socks5://" + socks.addr())
	conn, err := dialSOCKS5(ctx, pu, dial, "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	// An IP target is sent as an address, not a name.
	conn, err = dialSOCKS5(ctx, pu, dial, "93.184.216.34:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if got := socks.targets(); len(got) != 2 || got[1] != "93.184.216.34:443" {
		t.Fatalf("%v", got)
	}

	socks.refuse = 5
	if _, err := dialSOCKS5(ctx, pu, dial, "example.com:443"); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("a refusal: %v", err)
	}
	socks.refuse = 2
	if _, err := dialSOCKS5(ctx, pu, dial, "example.com:443"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("a rule: %v", err)
	}
	for _, target := range []string{"example.com", "example.com:0", "example.com:x", ":443", strings.Repeat("a", 300) + ":443"} {
		if _, err := dialSOCKS5(ctx, pu, dial, target); err == nil {
			t.Errorf("%q accepted", target)
		}
	}

	// A proxy that wants a login this URL lacks.
	authed := newFakeSOCKS5(t, "ann", "pw", nil)
	noLogin, _ := ParseProxy("socks5://" + authed.addr())
	if _, err := dialSOCKS5(ctx, noLogin, dial, "example.com:443"); err == nil {
		t.Error("no login method in common")
	}
	// A server that is not SOCKS at all.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			c.Close()
		}
	}()
	notSocks, _ := ParseProxy("socks5://" + ln.Addr().String())
	if _, err := dialSOCKS5(ctx, notSocks, dial, "example.com:443"); err == nil || !strings.Contains(err.Error(), "SOCKS5") {
		t.Errorf("not SOCKS: %v", err)
	}
	// A proxy nobody listens on.
	dead, _ := ParseProxy("socks5://127.0.0.1:1")
	if _, err := dialSOCKS5(ctx, dead, dial, "example.com:443"); err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("dead: %v", err)
	}
}

func startTestBridge(t *testing.T, upstream string, token string) *Bridge {
	t.Helper()
	pu, err := ParseProxy("socks5://" + upstream)
	if err != nil {
		t.Fatal(err)
	}
	b, err := StartBridge(pu, token, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func TestBridgeConnect(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "hello via socks") })
	socks := newFakeSOCKS5(t, "", "", func(string) string { return api.addr() })
	b := startTestBridge(t, socks.addr(), "tok")

	// An HTTPS client asking the bridge for a CONNECT, as Claude Code's HTTPS_PROXY does.
	proxyURL, _ := url.Parse("http://" + BridgeUser + ":tok@" + b.Addr())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: api.pool}}, Timeout: 5 * time.Second}
	res, err := client.Get("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "hello via socks" {
		t.Fatalf("%q", body)
	}
	if got := socks.targets(); len(got) != 1 || got[0] != "example.com:443" {
		t.Fatalf("the proxy was asked for %v", got)
	}

	// Without the token: 407 and the upstream is never touched.
	for _, auth := range []string{"", basic(BridgeUser, "wrong"), basic("someone", "tok"), "Bearer tok", "Basic !!"} {
		conn, err := net.Dial("tcp", b.Addr())
		if err != nil {
			t.Fatal(err)
		}
		req := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n"
		if auth != "" {
			req += "Proxy-Authorization: " + auth + "\r\n"
		}
		_, _ = conn.Write([]byte(req + "\r\n"))
		line, _ := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if !strings.Contains(line, "407") {
			t.Errorf("auth %q: %q", auth, line)
		}
	}
	if got := socks.targets(); len(got) != 1 {
		t.Fatalf("the upstream was reached without the token: %v", got)
	}
}

func TestBridgePlainHTTPAndUpstreamFailure(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			http.Error(w, "the proxy login leaked to the origin", 500)
			return
		}
		w.Header().Set("X-Seen", r.URL.Path)
		_, _ = io.WriteString(w, "plain")
	}))
	defer origin.Close()
	socks := newFakeSOCKS5(t, "", "", func(string) string { return strings.TrimPrefix(origin.URL, "http://") })
	b := startTestBridge(t, socks.addr(), "tok")
	proxyURL, _ := url.Parse("http://" + BridgeUser + ":tok@" + b.Addr())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	res, err := client.Get("http://origin.example.com/some/path")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "plain" || res.Header.Get("X-Seen") != "/some/path" {
		t.Fatalf("%q %v", body, res.Header)
	}
	// A request that is not for a proxy.
	conn, _ := net.Dial("tcp", b.Addr())
	_, _ = conn.Write([]byte("GET /x HTTP/1.1\r\nHost: a\r\nProxy-Authorization: " + basic(BridgeUser, "tok") + "\r\n\r\n"))
	line, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(line, "400") {
		t.Errorf("%q", line)
	}
	// The SOCKS server cannot reach the target: 502 to the client, not a hang.
	socks.refuse = 4
	conn, _ = net.Dial("tcp", b.Addr())
	_, _ = conn.Write([]byte("CONNECT nowhere.example.com:443 HTTP/1.1\r\nHost: nowhere.example.com:443\r\nProxy-Authorization: " + basic(BridgeUser, "tok") + "\r\n\r\n"))
	line, _ = bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(line, "502") {
		t.Errorf("%q", line)
	}
}

func TestBridgeRefusesWhatIsNotSOCKS(t *testing.T) {
	http1, _ := ParseProxy("http://a:1")
	if _, err := StartBridge(http1, "t", 0); err == nil {
		t.Error("a bridge for an HTTP proxy")
	}
	s5, _ := ParseProxy("socks5://a:1")
	if _, err := StartBridge(s5, "", 0); err == nil {
		t.Error("a bridge without a token")
	}
}
