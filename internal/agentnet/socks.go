package agentnet

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dialFunc opens a TCP connection; the net.Dialer's DialContext is one.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

var socksReplies = map[byte]string{
	1: "general SOCKS server failure", 2: "connection not allowed by the proxy's rules", 3: "network unreachable",
	4: "host unreachable", 5: "connection refused", 6: "TTL expired", 7: "command not supported", 8: "address type not supported",
}

// dialSOCKS5 connects to target ("host:port") through a SOCKS5 proxy (RFC 1928), with username and password when the
// proxy URL has them (RFC 1929). The name is sent to the proxy to resolve, so the machine's own DNS is not needed.
func dialSOCKS5(ctx context.Context, p ProxyURL, dial dialFunc, target string) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || len(host) == 0 || len(host) > 255 {
		return nil, fmt.Errorf("bad target %q", target)
	}
	conn, err := dial(ctx, "tcp", p.HostPort())
	if err != nil {
		return nil, fmt.Errorf("the proxy %s is not reachable: %w", p.HostPort(), err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	fail := func(err error) (net.Conn, error) { _ = conn.Close(); return nil, err }

	methods := []byte{0x00}
	if p.HasAuth {
		methods = []byte{0x00, 0x02}
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return fail(err)
	}
	var choice [2]byte
	if _, err := io.ReadFull(conn, choice[:]); err != nil {
		return fail(fmt.Errorf("the proxy did not answer the greeting: %w", err))
	}
	if choice[0] != 0x05 {
		return fail(errors.New("the proxy does not speak SOCKS5"))
	}
	switch choice[1] {
	case 0x00:
	case 0x02:
		if !p.HasAuth || len(p.User) > 255 || len(p.Pass) > 255 {
			return fail(errors.New("the proxy asks for a login this URL does not carry"))
		}
		msg := append([]byte{0x01, byte(len(p.User))}, p.User...)
		msg = append(append(msg, byte(len(p.Pass))), p.Pass...)
		if _, err := conn.Write(msg); err != nil {
			return fail(err)
		}
		var res [2]byte
		if _, err := io.ReadFull(conn, res[:]); err != nil {
			return fail(err)
		}
		if res[1] != 0x00 {
			return fail(errors.New("the proxy refused the login"))
		}
	default:
		return fail(errors.New("the proxy accepts no login method this agent offers"))
	}

	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		req = append(append(req, 0x01), ip.To4()...)
	} else if ip != nil {
		req = append(append(req, 0x04), ip.To16()...)
	} else {
		req = append(append(req, 0x03, byte(len(host))), host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		return fail(err)
	}
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return fail(fmt.Errorf("the proxy did not answer the connect: %w", err))
	}
	if head[0] != 0x05 {
		return fail(errors.New("the proxy does not speak SOCKS5"))
	}
	if head[1] != 0x00 {
		reason := socksReplies[head[1]]
		if reason == "" {
			reason = "code " + strconv.Itoa(int(head[1]))
		}
		return fail(fmt.Errorf("the proxy could not connect to %s: %s", target, reason))
	}
	// The bound address is of no use here: read it off.
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4 + 2
	case 0x04:
		skip = 16 + 2
	case 0x03:
		var n [1]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return fail(err)
		}
		skip = int(n[0]) + 2
	default:
		return fail(errors.New("the proxy answered with an unknown address type"))
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skip)); err != nil {
		return fail(err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// Bridge is a small HTTP proxy on 127.0.0.1 that sends what it is given through a SOCKS5 proxy, for CLIs that take only
// HTTP(S) proxies. A caller must present the bridge's token as the proxy password, so another account on the machine
// cannot use the person's proxy.
type Bridge struct {
	upstream ProxyURL
	token    string
	dial     dialFunc
	ln       net.Listener
	srv      *http.Server
	tr       *http.Transport

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// BridgeUser is the proxy user name the bridge expects with its token.
const BridgeUser = "harnessly"

// StartBridge listens on 127.0.0.1:port (0: any free port) for the SOCKS5 proxy upstream.
func StartBridge(upstream ProxyURL, token string, port int) (*Bridge, error) {
	return startBridge(upstream, token, port, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)
}

func startBridge(upstream ProxyURL, token string, port int, dial dialFunc) (*Bridge, error) {
	if upstream.Scheme != "socks5" {
		return nil, errors.New("a bridge is for a SOCKS5 proxy")
	}
	if token == "" {
		return nil, errors.New("a bridge needs a token")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	b := &Bridge{upstream: upstream, token: token, dial: dial, ln: ln, conns: map[net.Conn]struct{}{}}
	b.tr = &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialSOCKS5(ctx, upstream, dial, addr)
		},
		ForceAttemptHTTP2: false, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 15 * time.Second,
	}
	b.srv = &http.Server{Handler: b, ReadHeaderTimeout: 15 * time.Second}
	go func() { _ = b.srv.Serve(ln) }()
	return b, nil
}

// Addr is "127.0.0.1:<port>".
func (b *Bridge) Addr() string { return b.ln.Addr().String() }

// Port is the port it listens on.
func (b *Bridge) Port() int { return b.ln.Addr().(*net.TCPAddr).Port }

// Close stops the bridge and its connections.
func (b *Bridge) Close() {
	// The listener first, here: the server closes it from its own goroutine, too late for a caller that listens again.
	_ = b.ln.Close()
	_ = b.srv.Close()
	b.tr.CloseIdleConnections()
	b.mu.Lock()
	for c := range b.conns {
		_ = c.Close()
	}
	b.mu.Unlock()
}

func (b *Bridge) authorized(r *http.Request) bool {
	scheme, enc, ok := strings.Cut(r.Header.Get("Proxy-Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
	if err != nil {
		return false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	return ok && user == BridgeUser && subtle.ConstantTimeCompare([]byte(pass), []byte(b.token)) == 1
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="harnessly"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		b.connect(w, r)
		return
	}
	if !r.URL.IsAbs() {
		http.Error(w, "this is a proxy", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Authorization")
	out.Header.Del("Proxy-Connection")
	res, err := b.tr.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	for k, vs := range res.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

func (b *Bridge) connect(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	up, err := dialSOCKS5(ctx, b.upstream, b.dial, r.Host)
	cancel()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = up.Close()
		http.Error(w, "cannot take the connection over", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		_ = up.Close()
		return
	}
	b.mu.Lock()
	b.conns[client], b.conns[up] = struct{}{}, struct{}{}
	b.mu.Unlock()
	defer func() {
		_ = client.Close()
		_ = up.Close()
		b.mu.Lock()
		delete(b.conns, client)
		delete(b.conns, up)
		b.mu.Unlock()
	}()
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		// Bytes the client sent right behind its CONNECT are in the reader's buffer.
		if n := buf.Reader.Buffered(); n > 0 {
			pending, _ := buf.Reader.Peek(n)
			_, _ = up.Write(pending)
		}
		_, _ = io.Copy(up, client)
		done <- struct{}{}
	}()
	go func() { _, _ = io.Copy(client, up); done <- struct{}{} }()
	<-done
}
