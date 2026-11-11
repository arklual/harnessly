package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// mcpProxyTransport is the transport of the proxy's calls to host: the proxy of the deployment if it has one, else the address checked at the dial — a public
// one, or any but link-local for an internal host the deployment lists (netpolicy.go) — and the organization's roots.
// A test swaps it.
var mcpProxyTransport = func(host string) http.RoundTripper {
	network := currentNetwork()
	return network.Transport(host, dialControl(host, network.IsInternal(host)))
}

// ProxyMCP forwards one request of an agent's MCP client to the app's real remote server, with the headers of the package
// and the app's secrets in them (launch_secrets.go says why). srv is the server as the package declares it; values are
// its secrets' values. The call goes from the machine under the rules of hl.tools.call: https, a public address checked
// at the dial, no proxy, no redirects. A stream (Streamable HTTP, SSE) is passed on as it comes. The client's own headers
// (its token among them) do not go on; only the ones MCP needs do.
func ProxyMCP(w http.ResponseWriter, r *http.Request, srv MCPServer, values map[string]string) {
	ProxyMCPAs(w, r, srv, values, "")
}

// ProxyMCPAs is ProxyMCP for the app called name. When the server cannot be reached or answers that it is unavailable
// (502, 503, 504), a JSON-RPC request is answered in JSON-RPC: a tool call gets a tool result with isError and a
// sentence the agent can pass on, any other request an error with the same sentence. The agent then reads «the service
// is unavailable» and not a transport's failure. A notification or a batch keeps the bare HTTP status.
func ProxyMCPAs(w http.ResponseWriter, r *http.Request, srv MCPServer, values map[string]string, name string) {
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
	body, call := readRPC(r)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, filled.URL, body)
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
	transport := mcpProxyTransport(u.Hostname())
	if filled.ClientCert != nil {
		// The app's own certificate, to this server only (network.clientCert).
		network := currentNetwork().WithClientCert(filled.ClientCert)
		transport = network.Transport(u.Hostname(), dialControl(u.Hostname(), network.IsInternal(u.Hostname())))
	}
	client := &http.Client{Transport: transport, CheckRedirect: noRedirects}
	resp, err := client.Do(req)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.Canceled) {
			status = 499
		} else if call.answerable() {
			call.unavailable(w, name)
			return
		}
		http.Error(w, `{"error":"unreachable"}`, status)
		return
	}
	defer resp.Body.Close()
	if call.answerable() && unavailableStatus(resp.StatusCode) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		call.unavailable(w, name)
		return
	}
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

// maxRPCPeek is the most of a request body the proxy keeps to answer it in JSON-RPC; a longer body is passed on as a
// stream and a failure is answered with the bare status.
const maxRPCPeek = 8 << 20

// rpcCall is the id and the method of the JSON-RPC request the proxy passes on; a zero id: none to answer.
type rpcCall struct {
	ID     json.RawMessage
	Method string
}

func (c rpcCall) answerable() bool { return len(c.ID) > 0 && string(c.ID) != "null" }

// readRPC reads the request's body to learn its id and method, and gives the body to pass on.
func readRPC(r *http.Request) (io.Reader, rpcCall) {
	if r.Body == nil || r.Method != http.MethodPost {
		return r.Body, rpcCall{}
	}
	head, err := io.ReadAll(io.LimitReader(r.Body, maxRPCPeek+1))
	if err != nil || len(head) > maxRPCPeek {
		return io.MultiReader(bytes.NewReader(head), r.Body), rpcCall{}
	}
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(head, &msg) != nil {
		return bytes.NewReader(head), rpcCall{}
	}
	return bytes.NewReader(head), rpcCall{ID: msg.ID, Method: msg.Method}
}

func unavailableStatus(code int) bool {
	return code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// UnavailableText is what the agent reads when an app's server is down.
func UnavailableText(name string) string {
	if name == "" {
		name = "this app"
	}
	return fmt.Sprintf("The service of %s is unavailable right now. Try again later, do not retry in a loop; "+
		"tell the person if the task needs it now.", name)
}

// unavailable answers the request with UnavailableText: a tool result marked isError for tools/call, an error otherwise.
func (c rpcCall) unavailable(w http.ResponseWriter, name string) {
	text := UnavailableText(name)
	out := map[string]any{"jsonrpc": "2.0", "id": c.ID}
	if c.Method == "tools/call" {
		out["result"] = map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": true}
	} else {
		out["error"] = map[string]any{"code": -32000, "message": text}
	}
	raw, _ := json.Marshal(out)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
