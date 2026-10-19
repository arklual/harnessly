package preview

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"harnessly/internal/preview/devtools"
)

// upgradeLog is a WebSocket's one entry: its handshake, 101 or the reason it failed. The first outcome counts.
type upgradeLog struct {
	store *devtools.Store
	slot  string
	id    int64
	at    time.Time
	once  sync.Once
}

func (p *Proxy) logUpgrade(slot, target string, throttled bool) *upgradeLog {
	l := &upgradeLog{store: p.cfg.Store, slot: slot, at: p.cfg.Store.Now()}
	l.id = l.store.RecordRequest(slot, devtools.NetworkEntry{At: l.at.UnixMilli(), Source: "proxy", Method: "GET", URL: target, Host: hostOf(target),
		Type: "websocket", Throttled: throttled})
	return l
}

func (l *upgradeLog) done(status *int64, errText string, blocked bool) {
	l.once.Do(func() {
		l.store.Finish(l.slot, l.id, func(e *devtools.NetworkEntry) {
			e.Status, e.Error = status, nil
			if errText != "" {
				e.Error = &errText
			}
			if blocked {
				e.Blocked = true
			}
			d := l.store.Now().Sub(l.at).Milliseconds()
			e.DurationMs = &d
		})
	})
}

func statusOf(n int) *int64 { v := int64(n); return &v }

func rejectRaw(conn net.Conn, status int, reason string) {
	_, _ = io.WriteString(conn, "HTTP/1.1 "+strconv.Itoa(status)+" "+reason+"\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
	_ = conn.Close()
}

// serveUpgrade passes a WebSocket (a dev server's HMR) through: the upstream's 101 goes back with its cookies renamed
// for the target, then the bytes flow both ways with no idle limit.
func (p *Proxy) serveUpgrade(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	a := p.admit(r)
	conn, buf, err := hj.Hijack()
	if err != nil {
		return
	}
	if a.claim == nil {
		rejectRaw(conn, a.status, "Preview rejected")
		return
	}
	c, rt := a.claim, a.route
	slot := c.SlotOrigin
	store := p.cfg.Store
	store.Touch(slot, c.Target)
	settings := store.Settings(slot)
	target := targetURLOf(rt, true)
	log := p.logUpgrade(slot, target, devtools.IsOffline(settings.Throttle))
	if devtools.IsOffline(settings.Throttle) {
		log.done(nil, "offline", false)
		rejectRaw(conn, 503, "Offline in DevTools")
		return
	}
	if pattern := devtools.BlockedBy(target, settings.Blocked); pattern != "" {
		log.done(nil, "blocked: "+pattern, true)
		rejectRaw(conn, 403, "Blocked in DevTools")
		return
	}
	// The client's bytes already read past its request go to the upstream once it has switched.
	head := make([]byte, buf.Reader.Buffered())
	_, _ = io.ReadFull(buf.Reader, head)
	peer, peerReader, status, rawHeaders, err := p.dialUpgrade(r, c, rt)
	if err != nil {
		log.done(nil, nodeCode(err), false)
		_ = conn.Close()
		return
	}
	if status != 101 {
		log.done(statusOf(status), "", false)
		_ = peer.Close()
		rejectRaw(conn, status, "Upstream rejected")
		return
	}
	log.done(statusOf(status), "", false)
	tag := p.cookieTag(c)
	var b strings.Builder
	b.WriteString("HTTP/1.1 " + strconv.Itoa(status) + " Switching Protocols\r\n")
	for _, hv := range rawHeaders {
		name, value := hv[0], hv[1]
		setsCookie := strings.EqualFold(name, "set-cookie")
		if setsCookie && p.isStandRoute(rt) && p.cfg.Stands.isSessionSetCookie(value) {
			continue
		}
		if setsCookie {
			value = prefixSetCookie(value, tag)
		}
		b.WriteString(name + ": " + value + "\r\n")
	}
	b.WriteString("\r\n")
	if _, err := io.WriteString(conn, b.String()); err != nil {
		_ = conn.Close()
		_ = peer.Close()
		return
	}
	if n := peerReader.Buffered(); n > 0 {
		rest := make([]byte, n)
		_, _ = io.ReadFull(peerReader, rest)
		_, _ = conn.Write(rest)
	}
	if len(head) > 0 {
		_, _ = peer.Write(head)
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(peer, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, peer); done <- struct{}{} }()
	<-done
	_ = conn.Close()
	_ = peer.Close()
}

// dialUpgrade opens the upstream, sends the upgrade request and reads its answer's head, keeping the header lines as
// they were sent (names in their case, in their order).
func (p *Proxy) dialUpgrade(r *http.Request, c *Claim, rt *route) (net.Conn, *bufio.Reader, int, [][2]string, error) {
	h := p.upstreamHeaders(r, c, rt, true, false)
	var conn net.Conn
	var err error
	deadline := time.Time{}
	if rt.kind == "port" {
		conn, err = p.cfg.Dial(context.Background(), net.JoinHostPort(p.upstreamHostFor(rt.port), strconv.Itoa(rt.port)))
	} else {
		port := rt.url.Port
		secure := rt.url.Scheme == "https"
		if port == "" {
			port = "80"
			if secure {
				port = "443"
			}
		}
		addr := net.JoinHostPort(rt.address, port)
		conn, err = p.cfg.Dial(context.Background(), addr)
		if err == nil && secure {
			name := strings.Trim(rt.url.Hostname, "[]")
			if net.ParseIP(name) != nil {
				name = rt.address
			}
			ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeouts.Headers)
			tc := tls.Client(conn, &tls.Config{ServerName: name, RootCAs: p.cfg.RootCAs, NextProtos: []string{"http/1.1"}})
			err = tc.HandshakeContext(ctx)
			cancel()
			conn = tc
		}
		deadline = time.Now().Add(p.cfg.Timeouts.Headers)
	}
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, nil, 0, nil, err
	}
	if !deadline.IsZero() {
		_ = conn.SetDeadline(deadline)
	}
	var b strings.Builder
	b.WriteString(r.Method + " " + rt.path + " HTTP/1.1\r\n")
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if k == "content-length" {
			continue
		}
		for _, v := range h[k] {
			b.WriteString(k + ": " + v + "\r\n")
		}
	}
	b.WriteString("\r\n")
	if _, err := io.WriteString(conn, b.String()); err != nil {
		_ = conn.Close()
		return nil, nil, 0, nil, err
	}
	br := bufio.NewReader(conn)
	status, headers, err := readHead(br)
	if err != nil {
		_ = conn.Close()
		return nil, nil, 0, nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, br, status, headers, nil
}

func readHead(br *bufio.Reader) (int, [][2]string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return 0, nil, err
	}
	fields := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 3)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0, nil, io.ErrUnexpectedEOF
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, nil, err
	}
	var headers [][2]string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return 0, nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers = append(headers, [2]string{name, strings.Trim(value, " \t")})
	}
	if status >= 100 && status < 200 && status != 101 {
		return readHead(br)
	}
	return status, headers, nil
}
