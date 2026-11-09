package egress

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Through the company's proxy a call used to go by the host's name, and the proxy resolved it: a name that
// resolves to a private address or to the cloud's metadata (169.254.169.254) reached it, since only an address literal
// was judged. Now the machine resolves the name itself, every address it gets must pass the direct rule, and the tunnel
// (CONNECT) is opened to the checked address, so what the proxy reaches is what was judged. A host the admin listed as
// internal that the machine cannot resolve (the company's names may be known to the proxy alone) still goes to the
// proxy by name: the admin listed it; only the host the transport was built for, not a name met on the way. A host written as a number in any form but the canonical one (2130706433,
// 0x7f.1, 0177.0.0.1, 127.1) is refused: a resolver may read it as an address.

// lookupIP resolves a name; a test swaps it.
var lookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// ErrNumericHost: a host written as a number that is not a canonical address.
var ErrNumericHost = errors.New("egress: a host that is a number must be a canonical IP address")

// numericHost says whether a host that is not a canonical address would still be read as one: its last label is a
// number (decimal, octal or hex), as the URL standard reads such a host.
func numericHost(host string) bool {
	host = strings.TrimSuffix(host, ".")
	last := host[strings.LastIndex(host, ".")+1:]
	if last == "" {
		return false
	}
	if hex, ok := strings.CutPrefix(strings.ToLower(last), "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") == ""
	}
	return strings.Trim(last, "0123456789") == ""
}

// tunnelTarget is the address the tunnel is opened to: the canonical address of an address literal, the checked
// address of a name, or the name of an internal host the machine cannot resolve. Every address passes direct. internal
// is the host the transport (and its direct rule) was built for, when the admin listed it as internal: only that name
// may go to the proxy unresolved.
func (p *Policy) tunnelTarget(ctx context.Context, host, port, internal string, direct func(network, address string, c syscall.RawConn) error) (string, error) {
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if err := direct("tcp", net.JoinHostPort(ip.String(), port), nil); err != nil {
			return "", err
		}
		return net.JoinHostPort(ip.String(), port), nil
	}
	if host == "" || numericHost(host) {
		return "", ErrNumericHost
	}
	addrs, err := lookupIP(ctx, host)
	if err != nil || len(addrs) == 0 {
		if internal != "" && strings.EqualFold(host, internal) {
			return net.JoinHostPort(host, port), nil
		}
		if err == nil {
			err = fmt.Errorf("no address")
		}
		return "", fmt.Errorf("egress: %s not resolved: %w", host, err)
	}
	for _, a := range addrs {
		if err := direct("tcp", net.JoinHostPort(a.Unmap().String(), port), nil); err != nil {
			return "", err
		}
	}
	return net.JoinHostPort(addrs[0].Unmap().String(), port), nil
}

// connectTimeout bounds the proxy's answer to a CONNECT.
const connectTimeout = 30 * time.Second

// dialThrough opens a tunnel through proxy to the checked target of addr, for a transport built for host. The proxy
// itself is dialed under control.
func (p *Policy) dialThrough(host string, proxy *url.URL, direct, control func(network, address string, c syscall.RawConn) error) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: control}
	internal := ""
	if p.IsInternal(host) {
		internal = strings.TrimSuffix(host, ".")
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		name, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		target, err := p.tunnelTarget(ctx, name, port, internal, direct)
		if err != nil {
			return nil, err
		}
		proxyAddr := proxy.Host
		if proxy.Port() == "" {
			if proxy.Scheme == "https" {
				proxyAddr = net.JoinHostPort(proxy.Hostname(), "443")
			} else {
				proxyAddr = net.JoinHostPort(proxy.Hostname(), "80")
			}
		}
		conn, err := dialer.DialContext(ctx, network, proxyAddr)
		if err != nil {
			return nil, err
		}
		if proxy.Scheme == "https" {
			tc := tls.Client(conn, &tls.Config{ServerName: proxy.Hostname(), RootCAs: p.Roots, MinVersion: tls.VersionTLS12})
			if err := tc.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, err
			}
			conn = tc
		}
		out, err := connect(ctx, conn, proxy, target)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return out, nil
	}
}

// connect asks the proxy for a tunnel to target over conn and answers the tunnel.
func connect(ctx context.Context, conn net.Conn, proxy *url.URL, target string) (net.Conn, error) {
	deadline := time.Now().Add(connectTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: http.Header{}}
	if u := proxy.User; u != nil {
		pass, _ := u.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pass)))
	}
	if err := req.Write(conn); err != nil {
		return nil, err
	}
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, err
	}
	// The body of a 200 to a CONNECT is the tunnel: not read nor closed (closing would drain it); on a refusal the
	// caller closes the connection.
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("egress: the proxy refused the tunnel: %s", res.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// bufferedConn is a connection whose first bytes were read ahead.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.r.Read(b) }
