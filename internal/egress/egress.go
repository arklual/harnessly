// Package egress is a browser's only way out: an HTTP proxy on the loopback that holds every connection to a rule.
// The live browser of the pane and the agent's Chrome both run behind one.
package egress

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"
)

// Proxy is the browser's only way out: an HTTP proxy on the loopback (--proxy-server, with the loopback bypass
// off, see ChromeArgs) that resolves each name itself, holds the address to the caller's rule and connects to the very
// address it checked. So every load of a page — its fetches, scripts, frames, workers, redirects, WebSockets — is
// held to the rule at the network, not at the protocol, and a name that changes its answer between the check and
// the connection (DNS rebinding) changes nothing. https and wss go through CONNECT untouched (no man in the middle);
// http and ws are carried request by request.
type Proxy struct {
	ln      net.Listener
	srv     *http.Server
	resolve func(host, port string) (addr, why string)
	rp      *httputil.ReverseProxy
}

const (
	egressDial = 15 * time.Second
	egressIdle = 90 * time.Second
)

// ErrRefused marks a connection the rule refused (the browser gets 403).
var ErrRefused = errors.New("refused")

// ChromeArgs send every connection of a Chrome through the proxy at addr: the loopback too (<-loopback), no QUIC
// and no WebRTC around it.
func ChromeArgs(addr string) []string {
	return []string{"--proxy-server=http://" + addr, "--proxy-bypass-list=<-loopback>",
		"--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp"}
}

// Listen starts a proxy on 127.0.0.1. resolve is where a host (a name or a literal address) may be reached on a port:
// the one address to connect to, or why it may not.
func Listen(resolve func(host, port string) (addr, why string)) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	e := &Proxy{ln: ln, resolve: resolve}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         e.dial,
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     egressIdle,
		TLSHandshakeTimeout: egressDial,
	}
	e.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = pr.In.URL
			pr.Out.Host = pr.In.URL.Host
			pr.Out.Header.Del("Proxy-Authorization")
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, ErrRefused) {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
	}
	e.srv = &http.Server{Handler: e, ReadHeaderTimeout: 30 * time.Second, IdleTimeout: egressIdle}
	go func() { _ = e.srv.Serve(ln) }()
	return e, nil
}

// Addr is where the browser is told to connect (host:port).
func (e *Proxy) Addr() string { return e.ln.Addr().String() }

func (e *Proxy) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = e.srv.Shutdown(ctx)
	_ = e.ln.Close()
}

// dial connects to the address the gate checked for the name, never to one the system would resolve again.
func (e *Proxy) dial(ctx context.Context, _ string, hostport string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	addr, why := e.resolve(host, port)
	if why != "" {
		return nil, errors.Join(ErrRefused, errors.New(why))
	}
	d := net.Dialer{Timeout: egressDial}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(addr, port))
}

func (e *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		e.tunnel(w, r)
		return
	}
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		// Only a proxy request (an absolute http address) is served: this is not a web server.
		http.Error(w, "proxy requests only", http.StatusBadRequest)
		return
	}
	e.rp.ServeHTTP(w, r)
}

// tunnel carries a CONNECT (https, wss) as bytes between the browser and the checked address.
func (e *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	hostport := r.Host
	if !strings.Contains(hostport, ":") {
		hostport += ":443"
	}
	up, err := e.dial(r.Context(), "tcp", hostport)
	if err != nil {
		if errors.Is(err, ErrRefused) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = up.Close()
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	down, buf, err := hj.Hijack()
	if err != nil {
		_ = up.Close()
		return
	}
	if _, err := down.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		_ = up.Close()
		_ = down.Close()
		return
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// What the browser already sent past the request line goes first.
		if buf.Reader.Buffered() > 0 {
			_, _ = io.CopyN(up, buf, int64(buf.Reader.Buffered()))
		}
		_, _ = io.Copy(up, down)
		closeWrite(up)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(down, up)
		closeWrite(down)
	}()
	wg.Wait()
	_ = up.Close()
	_ = down.Close()
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}
