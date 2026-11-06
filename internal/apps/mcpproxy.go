package apps

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// mcpProxyTransport is the transport of the proxy's calls to host: no proxy, the address checked at the dial — a public
// one, or any but link-local for an internal host the deployment lists (netpolicy.go) — and the organization's roots.
// A test swaps it.
var mcpProxyTransport = func(host string) http.RoundTripper {
	network := currentNetwork()
	return &http.Transport{Proxy: nil, TLSClientConfig: network.tlsConfig(), ResponseHeaderTimeout: 60 * time.Second,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: dialControl(network.IsInternal(host))}).DialContext}
}

// ProxyMCP forwards one request of an agent's MCP client to the app's real remote server, with the headers of the package
// and the app's secrets in them (launch_secrets.go says why). srv is the server as the package declares it; values are
// its secrets' values. The call goes from the machine under the rules of hl.tools.call: https, a public address checked
// at the dial, no proxy, no redirects. A stream (Streamable HTTP, SSE) is passed on as it comes. The client's own headers
// (its token among them) do not go on; only the ones MCP needs do.
func ProxyMCP(w http.ResponseWriter, r *http.Request, srv MCPServer, values map[string]string) {
	filled, missing := srv.Filled(values)
	if len(missing) > 0 {
		http.Error(w, `{"error":"waiting-secret"}`, http.StatusConflict)
		return
	}
	u, err := url.Parse(filled.URL)
	if err != nil || u.Hostname() == "" ||
		(u.Scheme != "https" && (u.Scheme != "http" || !currentNetwork().IsInternal(u.Hostname()))) {
		http.Error(w, `{"error":"not-remote"}`, http.StatusNotFound)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, filled.URL, r.Body)
	if err != nil {
		http.Error(w, `{"error":"bad-request"}`, http.StatusBadRequest)
		return
	}
	for _, h := range []string{"Content-Type", "Accept", "Mcp-Session-Id", "Last-Event-Id", "Mcp-Protocol-Version"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	for k, v := range filled.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Transport: mcpProxyTransport(u.Hostname()), CheckRedirect: noRedirects}
	resp, err := client.Do(req)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.Canceled) {
			status = 499
		}
		http.Error(w, `{"error":"unreachable"}`, status)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Mcp-Session-Id", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 16<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				return
			}
			return
		}
	}
}
