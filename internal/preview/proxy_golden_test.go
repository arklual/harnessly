package preview

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"

	"harnessly/internal/preview/devtools"
)

// proxy.json is the previous runtime's proxy run end to end (testdata/gen/gen-proxy.mjs): the same fixture servers
// are started here, the same requests sent through the Go proxy, and every answer and the DevTools log at the end must
// match.

type proxyGolden struct {
	Secret   string                     `json:"secret"`
	Reserved int                        `json:"reserved"`
	Fixture  map[string]fixtureRoute    `json:"fixture"`
	Claims   map[string]json.RawMessage `json:"claims"`
	Steps    []struct {
		Kind    string            `json:"kind"`
		Name    string            `json:"name"`
		Claim   *string           `json:"claim"`
		Path    string            `json:"path"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    *string           `json:"body"`
		Op      string            `json:"op"`
		Slot    string            `json:"slot"`
		Patch   json.RawMessage   `json:"patch"`
		Out     json.RawMessage   `json:"out"`
	} `json:"steps"`
	Logs map[string][]map[string]any `json:"logs"`
}

type fixtureRoute struct {
	Status  int         `json:"status"`
	Headers [][2]string `json:"headers"`
	Body    string      `json:"body"`
	Coding  string      `json:"coding"`
}

type httpOut struct {
	Status  int            `json:"status"`
	Headers map[string]any `json:"headers"`
	Body    string         `json:"body"`
}

const standHost = "st.preview.example.org"

func standCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: standHost}, DNSNames: []string{standHost},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

type fixtureServer struct {
	routes  map[string]fixtureRoute
	fill    func(string) string
	upgrade bool
}

func (f *fixtureServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.upgrade && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		f.serveUpgrade(w, r)
		return
	}
	if r.URL.Path == "/echo" || r.URL.Path == "/remote.js" {
		body, _ := io.ReadAll(r.Body)
		headers := map[string]string{}
		for k, vs := range r.Header {
			k = strings.ToLower(k)
			if k == "connection" || k == "keep-alive" || k == "transfer-encoding" {
				continue
			}
			headers[k] = strings.Join(vs, ", ")
			if k == "cookie" {
				headers[k] = strings.Join(vs, "; ")
			}
		}
		headers["host"] = r.Host
		if r.ContentLength > 0 {
			headers["content-length"] = strconv.FormatInt(r.ContentLength, 10)
		}
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString(`{"method":` + jsonString(r.Method) + `,"url":` + jsonString(r.RequestURI) + `,"headers":{`)
		for i, k := range keys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(jsonString(k) + ":" + jsonString(headers[k]))
		}
		b.WriteString(`},"body":` + jsonString(string(body)) + `}`)
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, b.String())
		return
	}
	route, ok := f.routes[r.URL.Path]
	if !ok {
		w.Header()["Content-Type"] = nil
		w.WriteHeader(404)
		return
	}
	body := []byte(f.fill(route.Body))
	switch route.Coding {
	case "gzip":
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write(body)
		_ = zw.Close()
		body = buf.Bytes()
	case "br":
		var buf bytes.Buffer
		bw := brotli.NewWriter(&buf)
		_, _ = bw.Write(body)
		_ = bw.Close()
		body = buf.Bytes()
	}
	hasLength := false
	for _, h := range route.Headers {
		w.Header().Add(h[0], f.fill(h[1]))
		hasLength = hasLength || strings.EqualFold(h[0], "Content-Length")
	}
	if !hasLength && route.Status != 204 {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header()["Content-Type"] = nil
	}
	w.WriteHeader(route.Status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return strings.NewReplacer(`\u003c`, "<", `\u003e`, ">", `\u0026`, "&").Replace(string(b))
}

func (f *fixtureServer) serveUpgrade(w http.ResponseWriter, r *http.Request) {
	conn, buf, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if r.URL.Path == "/ws-reject" {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
		return
	}
	_, _ = io.WriteString(conn, strings.Join([]string{"HTTP/1.1 101 Switching Protocols", "Upgrade: websocket", "Connection: Upgrade", "Sec-WebSocket-Accept: abc=",
		"Set-Cookie: hmr=1; Path=/; Domain=localhost", "Set-Cookie: _oauth2_proxy=s", "X-Seen: " + strings.Join(r.Header.Values("Cookie"), "; ") + "|" + r.Header.Get("Origin") + "|" + r.Host, "", ""}, "\r\n"))
	p := make([]byte, 4096)
	for {
		n, err := buf.Read(p)
		if n > 0 {
			_, _ = conn.Write(append([]byte("echo:"), p[:n]...))
		}
		if err != nil {
			return
		}
	}
}

func signClaim(secret []byte, raw []byte) string {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	payload["aud"] = "acme-preview-proxy"
	payload["exp"] = time.Now().Unix() + 3600
	body, _ := json.Marshal(payload)
	signing := head + "." + base64.RawURLEncoding.EncodeToString(body)
	return signing + "." + agentMAC(secret, signing)
}

func agentMAC(secret []byte, s string) string { return hmacB64(secret, s) }

func TestProxyGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/proxy.json")
	if err != nil {
		t.Fatal(err)
	}
	var g proxyGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	stand := "https://" + standHost
	ports := map[string]int{}
	fill := func(s string) string {
		return strings.NewReplacer("$NPORT", strconv.Itoa(ports["n"]), "$PORT", strconv.Itoa(ports["m"]), "$STAND", stand).Replace(s)
	}
	mainSrv := httptest.NewServer(&fixtureServer{routes: g.Fixture, fill: func(s string) string { return fill(s) }, upgrade: true})
	defer mainSrv.Close()
	neighbourSrv := httptest.NewServer(&fixtureServer{routes: g.Fixture, fill: func(s string) string { return fill(s) }})
	defer neighbourSrv.Close()
	cert, pool := standCert(t)
	standSrv := httptest.NewUnstartedServer(&fixtureServer{routes: g.Fixture, fill: func(s string) string { return fill(s) }, upgrade: true})
	standSrv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	standSrv.StartTLS()
	defer standSrv.Close()
	ports["m"], ports["n"] = portOfURL(mainSrv.URL), portOfURL(neighbourSrv.URL)
	standAddr := strings.TrimPrefix(standSrv.URL, "https://")

	secret := []byte(g.Secret)
	store := devtools.NewStore()
	store.Now = func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }
	p := New(Config{
		Path: "/agents/api/platform/preview/proxy", ClaimHeader: "x-acme-preview", Audience: "acme-preview-proxy", Secret: secret,
		PrivateHeaderPrefix: "x-acme-", Forbidden: func() map[int]bool { return map[int]bool{g.Reserved: true} }, Stands: testStands, Store: store,
		Cache:  NewAssetCache(t.TempDir(), DefaultMaxTotalBytes, DefaultMaxFileBytes),
		Bridge: func() ([]byte, bool) { return []byte("bridge();"), true },
		Resolve: func(host string) Resolved {
			if host == "site.example.org" || host == standHost {
				return Resolved{Address: "127.0.0.1", Family: 4}
			}
			return ResolvePublic(host, nil, map[string]bool{})
		},
		UpstreamHost: "127.0.0.1",
		RootCAs:      pool,
		Dial: func(ctx context.Context, address string) (net.Conn, error) {
			if strings.HasSuffix(address, ":443") {
				address = standAddr
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
		},
	})
	runtime := httptest.NewServer(p)
	defer runtime.Close()

	filledClaim := func(name string) []byte {
		return quotedPort.ReplaceAll([]byte(fill(string(g.Claims[name]))), []byte(`"port":$1`))
	}
	targetOfClaim := func(name string) devtools.Target {
		var c struct {
			Target struct {
				Kind   string `json:"kind"`
				Port   int    `json:"port"`
				Origin string `json:"origin"`
			} `json:"target"`
		}
		_ = json.Unmarshal(filledClaim(name), &c)
		return devtools.Target{Kind: c.Target.Kind, Port: c.Target.Port, Origin: c.Target.Origin}
	}
	tags := map[string]string{"$TAG": CookieTag(targetOfClaim("port"), secret), "$UTAG": CookieTag(targetOfClaim("url"), secret), "$STAG": CookieTag(targetOfClaim("stand"), secret)}
	fillReq := func(s string) string {
		s = fill(s)
		for k, v := range tags {
			s = strings.ReplaceAll(s, k, v)
		}
		return s
	}
	unfill := func(s string) string {
		s = nonceRe.ReplaceAllString(s, "nonce$1NONCE")
		for k, v := range tags {
			s = strings.ReplaceAll(s, v, k)
		}
		m, n := strconv.Itoa(ports["m"]), strconv.Itoa(ports["n"])
		return strings.NewReplacer(stand, "$STAND", ":"+m, ":$PORT", "/"+m, "/$PORT", `"`+m+`"`, `"$PORT"`, "/0"+n, "/0$NPORT", ":"+n, ":$NPORT", "/"+n, "/$NPORT", "="+n, "=$NPORT").Replace(s)
	}
	client := &http.Transport{DisableCompression: true}
	compared := []string{"content-type", "cache-control", "location", "set-cookie", "content-encoding", "vary", "etag", "last-modified", "expires", "x-robots-tag",
		"x-tk-devtools", "x-tk-asset-cache", "content-security-policy", "content-security-policy-report-only", "x-frame-options", "clear-site-data",
		"x-content-type-options", "content-length"}
	encodedURLs := map[string]bool{}
	for _, st := range g.Steps {
		switch st.Kind {
		case "op":
			patch, msg := devtools.ParseSettingsPatch(mustJS(st.Patch))
			if msg != "" {
				t.Fatal(msg)
			}
			store.SetSettings(st.Slot, patch)
		case "http":
			var want httpOut
			_ = json.Unmarshal(st.Out, &want)
			var body io.Reader
			if st.Body != nil {
				body = strings.NewReader(fillReq(*st.Body))
			}
			req, _ := http.NewRequest(st.Method, runtime.URL+p.cfg.Path+fill(st.Path), body)
			req.Header["User-Agent"] = []string{""}
			for k, v := range st.Headers {
				req.Header.Set(k, fillReq(v))
			}
			if st.Claim != nil {
				if *st.Claim == "bad" {
					req.Header.Set("x-acme-preview", "not-a-token")
				} else {
					req.Header.Set("x-acme-preview", signClaim(secret, filledClaim(*st.Claim)))
				}
			}
			res, err := client.RoundTrip(req)
			if err != nil {
				t.Fatalf("%s: %v", st.Name, err)
			}
			rawBody, _ := io.ReadAll(res.Body)
			res.Body.Close()
			coding := res.Header.Get("Content-Encoding")
			decoded := rawBody
			switch coding {
			case "gzip":
				zr, err := gzip.NewReader(bytes.NewReader(rawBody))
				if err == nil {
					decoded, _ = io.ReadAll(zr)
				}
				encodedURLs[st.Path] = true
			case "br":
				decoded, _ = io.ReadAll(brotli.NewReader(bytes.NewReader(rawBody)))
				encodedURLs[st.Path] = true
			}
			if res.StatusCode != want.Status {
				t.Errorf("%s: status %d, want %d", st.Name, res.StatusCode, want.Status)
			}
			wantBody, _ := base64.StdEncoding.DecodeString(want.Body)
			wantBody = []byte(nonceRe.ReplaceAllString(string(wantBody), "nonce$1NONCE"))
			gotBody := []byte(unfill(string(decoded)))
			if st.Name == "bridge script" {
				gotBody, wantBody = nil, nil
			}
			if !bytes.Equal(gotBody, wantBody) {
				t.Errorf("%s: body\n got %q\nwant %q", st.Name, clipBytes(gotBody), clipBytes(wantBody))
			}
			for _, k := range compared {
				w, has := want.Headers[k]
				gv := res.Header.Values(k)
				if k == "content-length" && (!has || coding == "gzip" || coding == "br") {
					continue
				}
				var got any
				switch {
				case len(gv) == 0:
					got = nil
				case k == "set-cookie":
					list := make([]any, len(gv))
					for i, v := range gv {
						list[i] = unfill(v)
					}
					got = list
				default:
					got = unfill(strings.Join(gv, ", "))
				}
				if !has {
					w = nil
				} else if ws, ok := w.(string); ok {
					w = nonceRe.ReplaceAllString(ws, "nonce$1NONCE")
				}
				if !reflect.DeepEqual(got, w) {
					t.Errorf("%s: header %s = %v, want %v", st.Name, k, got, w)
				}
			}
		case "upgrade":
			var want string
			_ = json.Unmarshal(st.Out, &want)
			conn, err := net.Dial("tcp", strings.TrimPrefix(runtime.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			h := map[string]string{"host": "runtime", "connection": "Upgrade", "upgrade": "websocket"}
			for k, v := range st.Headers {
				h[k] = fillReq(v)
			}
			if st.Claim != nil {
				h["x-acme-preview"] = signClaim(secret, filledClaim(*st.Claim))
			}
			var b strings.Builder
			b.WriteString("GET " + p.cfg.Path + fill(st.Path) + " HTTP/1.1\r\n")
			for k, v := range h {
				b.WriteString(k + ": " + v + "\r\n")
			}
			b.WriteString("\r\nhello")
			_, _ = io.WriteString(conn, b.String())
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var got []byte
			rd := bufio.NewReader(conn)
			chunk := make([]byte, 4096)
			for {
				n, err := rd.Read(chunk)
				got = append(got, chunk[:n]...)
				text := string(got)
				if strings.Contains(text, "echo:hello") || strings.Contains(text, "\r\n\r\n") && !strings.HasPrefix(text, "HTTP/1.1 101") || err != nil {
					break
				}
			}
			conn.Close()
			if g := unfill(string(got)); g != want {
				t.Errorf("%s:\n got %q\nwant %q", st.Name, g, want)
			}
		}
	}
	time.Sleep(50 * time.Millisecond)
	for slot, want := range g.Logs {
		snap := store.Read(slot, 0)
		var got []map[string]any
		_ = json.Unmarshal([]byte(unfill(string(mustMarshal(snap.Network)))), &got)
		if len(got) != len(want) {
			t.Errorf("log of %s: %d entries, want %d", slot, len(got), len(want))
			continue
		}
		for i := range want {
			g, w := got[i], want[i]
			if g["durationMs"] != nil {
				g["durationMs"] = float64(0)
			}
			if u, _ := w["url"].(string); encodedForURL(encodedURLs, u) {
				g["size"], w["size"] = nil, nil
			}
			if !reflect.DeepEqual(g, w) {
				t.Errorf("log of %s #%d:\n got %v\nwant %v", slot, i, g, w)
			}
		}
	}
}

var nonceRe = regexp.MustCompile(`nonce(-|=")[A-Za-z0-9+/=]{24}`)

var quotedPort = regexp.MustCompile(`"port":\s*"(\d+)"`)

func encodedForURL(encoded map[string]bool, u string) bool {
	for p := range encoded {
		if p != "" && strings.HasSuffix(strings.SplitN(u, "?", 2)[0], strings.SplitN(p, "?", 2)[0]) {
			return true
		}
	}
	return false
}

func clipBytes(b []byte) []byte {
	if len(b) > 400 {
		return b[:400]
	}
	return b
}

func portOfURL(u string) int {
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(u, "http://"), "https://"))
	n, _ := strconv.Atoi(p)
	return n
}

func mustJS(raw json.RawMessage) any {
	v, _ := parseJSON(string(raw))
	return v
}

func mustMarshal(v any) []byte { b, _ := json.Marshal(v); return b }

func hmacB64(secret []byte, s string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
