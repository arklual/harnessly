package agentnet

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// perHost is the time each host gets for DNS, TCP and TLS, and the light request gets after them.
const perHost = 8 * time.Second

// providerHosts are the servers a provider's agent needs, the first being the API host (docs/hub/api.md).
var providerHosts = map[string][]string{
	hubproto.ProviderClaude: {"api.anthropic.com", "claude.ai", "platform.claude.com"},
	hubproto.ProviderCodex:  {"api.openai.com", "chatgpt.com", "auth.openai.com"},
}

// lightRequests are the one request per provider that reaches the API without a key and costs nothing.
var lightRequests = map[string]string{
	hubproto.ProviderClaude: "https://api.anthropic.com/v1/models",
	hubproto.ProviderCodex:  "https://api.openai.com/v1/models",
}

// Checker makes provider checks. The zero value checks for real; the fields are for tests.
type Checker struct {
	// Dial opens TCP connections (nil: the system's, 8 s).
	Dial dialFunc
	// LookupHost resolves a name (nil: the system's resolver).
	LookupHost func(ctx context.Context, host string) ([]string, error)
	// TLS is the client configuration (nil: the system's roots).
	TLS *tls.Config
	// Hosts and Requests replace the providers' hosts and light requests.
	Hosts    map[string][]string
	Requests map[string]string
	Now      func() time.Time
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) hosts(provider string) []string {
	if h, ok := c.Hosts[provider]; ok {
		return h
	}
	return providerHosts[provider]
}

func (c *Checker) request(provider string) string {
	if r, ok := c.Requests[provider]; ok {
		return r
	}
	return lightRequests[provider]
}

func (c *Checker) dialer() dialFunc {
	if c.Dial != nil {
		return c.Dial
	}
	return (&net.Dialer{Timeout: perHost}).DialContext
}

// Known: a provider with servers to check.
func Known(provider string) bool { return len(providerHosts[provider]) > 0 }

// Check makes the verdict on the network from this machine to a provider's servers: DNS, TCP and TLS to each host, then
// one light request to the API. proxy is the proxy to go through, nil for direct. No country logic and no judging of
// the answer beyond ok, network and rejected.
func (c *Checker) Check(ctx context.Context, provider string, proxy *ProxyURL) hubproto.ProviderCheck {
	res := hubproto.ProviderCheck{Provider: provider, Via: "direct", At: c.now().UnixMilli(), Hosts: []hubproto.HostCheck{}}
	if proxy != nil {
		res.Via = "proxy"
	}
	hosts := c.hosts(provider)
	if len(hosts) == 0 {
		res.Verdict, res.Host = hubproto.VerdictNetwork, ""
		detail := "no servers are known for " + provider
		res.Detail = &detail
		return res
	}
	out := make([]hubproto.HostCheck, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hctx, cancel := context.WithTimeout(ctx, perHost)
			defer cancel()
			out[i] = c.checkHost(hctx, h, proxy)
		}()
	}
	wg.Wait()
	res.Hosts = out
	for _, h := range out {
		if h.Error != nil {
			res.Verdict, res.Host, res.Detail = hubproto.VerdictNetwork, h.Host, h.Error
			return res
		}
	}
	res.Host = hosts[0]
	rctx, cancel := context.WithTimeout(ctx, perHost)
	defer cancel()
	status, body, err := c.lightRequest(rctx, c.request(provider), proxy, nil)
	if err != nil {
		msg := describe(err)
		res.Verdict, res.Detail = hubproto.VerdictNetwork, &msg
		return res
	}
	res.Status = &status
	if lightOK(status, body) {
		res.Verdict = hubproto.VerdictOK
		return res
	}
	res.Verdict = hubproto.VerdictRejected
	text := cutText(strings.TrimSpace(body), 300)
	res.Detail = &text
	return res
}

// lightOK: the API answered. Any 2xx is fine; so is the 401 of a request without a key when it carries the provider's
// JSON error (a corporate filter's 401 page is not that).
func lightOK(status int, body string) bool {
	if status >= 200 && status < 300 {
		return true
	}
	if status != http.StatusUnauthorized {
		return false
	}
	var v struct {
		Error any `json:"error"`
	}
	return json.Unmarshal([]byte(body), &v) == nil && v.Error != nil
}

func cutText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// describe is a network error in the words the person sees: timeout, refused, certificate, name not found.
func describe(err error) string {
	var dns *net.DNSError
	var ne net.Error
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &dns):
		switch {
		case dns.IsNotFound:
			return "no such host"
		case dns.IsTimeout:
			return "timeout resolving the name"
		}
		return "name resolution failed: " + dns.Err
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate"):
		return "certificate: " + cutText(msg, 200)
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	}
	return cutText(msg, 200)
}

func (c *Checker) tlsConfig(host string) *tls.Config {
	var cfg *tls.Config
	if c.TLS != nil {
		cfg = c.TLS.Clone()
	} else {
		cfg = &tls.Config{}
	}
	cfg.ServerName = host
	if cfg.MinVersion == 0 {
		cfg.MinVersion = tls.VersionTLS12
	}
	return cfg
}

// checkHost: DNS, TCP and TLS to host:443. Through a proxy the name is the proxy's to resolve, so DNS is true when the
// tunnel opened, TCP is the tunnel and TLS the handshake inside it.
func (c *Checker) checkHost(ctx context.Context, host string, proxy *ProxyURL) hubproto.HostCheck {
	start := time.Now()
	res := hubproto.HostCheck{Host: host}
	done := func(err error) hubproto.HostCheck {
		res.MS = time.Since(start).Milliseconds()
		if err != nil {
			msg := describe(err)
			res.Error = &msg
		}
		return res
	}
	target := net.JoinHostPort(host, "443")
	var conn net.Conn
	var err error
	if proxy != nil {
		conn, err = c.dialThrough(ctx, *proxy, target)
		if err != nil {
			return done(err)
		}
		res.DNS, res.TCP = true, true
	} else {
		dial := c.dialer()
		address := target
		if net.ParseIP(host) == nil {
			lookup := c.LookupHost
			if lookup == nil {
				lookup = net.DefaultResolver.LookupHost
			}
			addrs, lerr := lookup(ctx, host)
			if lerr != nil || len(addrs) == 0 {
				if lerr == nil {
					lerr = &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
				}
				return done(lerr)
			}
			// Dial the address found, so the TCP step is only TCP.
			address = net.JoinHostPort(addrs[0], "443")
		}
		res.DNS = true
		conn, err = dial(ctx, "tcp", address)
		if err != nil {
			return done(err)
		}
		res.TCP = true
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tc := tls.Client(conn, c.tlsConfig(host))
	if err := tc.HandshakeContext(ctx); err != nil {
		return done(err)
	}
	res.TLS = true
	return done(nil)
}

// dialThrough opens a connection to target through the proxy.
func (c *Checker) dialThrough(ctx context.Context, p ProxyURL, target string) (net.Conn, error) {
	dial := c.dialer()
	if p.Scheme == "socks5" {
		return dialSOCKS5(ctx, p, dial, target)
	}
	conn, err := dial(ctx, "tcp", p.HostPort())
	if err != nil {
		return nil, fmt.Errorf("the proxy %s is not reachable: %w", p.HostPort(), err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if p.Scheme == "https" {
		cfg := c.tlsConfig(p.Host)
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("the proxy's certificate or TLS failed: %w", err)
		}
		conn = tc
	}
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if p.HasAuth {
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(p.User+":"+p.Pass)) + "\r\n"
	}
	if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("the proxy did not answer the CONNECT: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("the proxy refused the CONNECT to %s: %s", target, res.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// lightRequest is GET rawURL without a key, through the proxy when there is one.
func (c *Checker) lightRequest(ctx context.Context, rawURL string, proxy *ProxyURL, header http.Header) (int, string, error) {
	tr := &http.Transport{
		TLSClientConfig:     c.tlsConfig(""),
		TLSHandshakeTimeout: perHost,
		DisableKeepAlives:   true,
	}
	dial := c.dialer()
	tr.DialContext = dial
	if c.LookupHost != nil {
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil || net.ParseIP(host) != nil {
				return dial(ctx, network, addr)
			}
			addrs, err := c.LookupHost(ctx, host)
			if err != nil || len(addrs) == 0 {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			return dial(ctx, network, net.JoinHostPort(addrs[0], port))
		}
	}
	if proxy != nil {
		switch proxy.Scheme {
		case "socks5":
			tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
				return dialSOCKS5(ctx, *proxy, dial, addr)
			}
		default:
			tr.Proxy = http.ProxyURL(proxy.URL())
		}
	}
	// The transport sets the server name from the URL; an empty one in the config is filled in per connection.
	tr.TLSClientConfig.ServerName = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "harnessly-agent provider-check")
	for k, v := range header {
		req.Header[k] = v
	}
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return 0, "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	return res.StatusCode, string(body), nil
}

// keyHeaders are how each provider takes an API key on its models list.
func keyHeaders(provider, key string) http.Header {
	h := http.Header{}
	if provider == hubproto.ProviderClaude {
		h.Set("x-api-key", key)
		h.Set("anthropic-version", "2023-06-01")
	} else {
		h.Set("Authorization", "Bearer "+key)
	}
	return h
}

// CheckKey asks the provider's models list with the key, through proxy (nil: direct): 200 and the key is accepted,
// 401 or 403 and it is rejected; no answer is the network's fault, and any other answer is not a verdict on the key
// either. The key is only ever in the request's header. An empty key is "absent" without asking.
func (c *Checker) CheckKey(ctx context.Context, provider, key string, proxy *ProxyURL) hubproto.KeyCheck {
	res := hubproto.KeyCheck{Provider: provider, Present: key != "", Verdict: hubproto.KeyAbsent}
	if key == "" || !Known(provider) {
		return res
	}
	rctx, cancel := context.WithTimeout(ctx, perHost)
	defer cancel()
	status, _, err := c.lightRequest(rctx, c.request(provider), proxy, keyHeaders(provider, key))
	if err != nil {
		res.Verdict = hubproto.KeyNetwork
		return res
	}
	res.Status = &status
	switch {
	case status >= 200 && status < 300:
		res.Verdict = hubproto.KeyAccepted
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		res.Verdict = hubproto.KeyRejected
	default:
		// 429, 5xx, a filter's page: the key was not judged.
		res.Verdict = hubproto.KeyNetwork
	}
	return res
}
