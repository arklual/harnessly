package apps

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// CallTool runs one tool of an app's remote MCP server for a view (hl.tools.call): Streamable HTTP, one initialize and
// one tools/call per call, nothing kept between calls. The server is the package's own (.mcp.json, an https address);
// the call goes from the machine, so the address must be a public one, or the host one the deployment's admin listed
// as internal (DeployNetwork), which may also be plain http.
func CallTool(ctx context.Context, srv MCPServer, tool string, args map[string]any) (json.RawMessage, error) {
	u, err := url.Parse(srv.URL)
	if err != nil || u.Hostname() == "" {
		return nil, &BridgeError{Code: CodeHostNotAllowed, Message: ToolRefused, Detail: "the MCP server has no address"}
	}
	network := currentNetwork()
	internal := network.IsInternal(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || !internal) {
		return nil, &BridgeError{Code: CodeHostNotAllowed, Message: ToolRefused,
			Detail: "only a remote https MCP server, or an internal host the deployment lists, can be called from a view"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c := &mcpConn{url: srv.URL, headers: srv.Headers, http: &http.Client{
		Transport: &http.Transport{Proxy: nil, TLSClientConfig: network.tlsConfig(),
			DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: dialControl(internal)}).DialContext},
		CheckRedirect: noRedirects,
	}}
	if _, err := c.rpc(ctx, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "harnessly", "version": "1"},
	}); err != nil {
		return nil, err
	}
	if _, err := c.rpc(ctx, 0, "notifications/initialized", map[string]any{}); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return c.rpc(ctx, 2, "tools/call", map[string]any{"name": tool, "arguments": args})
}

// noRedirects: an answer that redirects is the answer (and an error): the call never goes where the server points it.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

type mcpConn struct {
	url     string
	headers map[string]string
	session string
	http    *http.Client
}

// What a view is told of a failed tool call: one of these words. The remote's own error text and the transport's are
// logged (BridgeError.Detail), never sent to the page.
const (
	ToolUnreachable = "unreachable"
	ToolTimeout     = "timeout"
	ToolRefused     = "refused"
	ToolError       = "tool-error"
)

// transportFailure maps a failed request to a short word.
func transportFailure(ctx context.Context, err error) *BridgeError {
	var be *BridgeError
	switch {
	case errors.As(err, &be):
		return &BridgeError{Code: be.Code, Message: ToolRefused, Detail: err.Error()}
	case untrustedTLS(err):
		return &BridgeError{Code: CodeTLSUntrusted, Message: ToolUnreachable, Detail: err.Error()}
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return &BridgeError{Code: CodeHostUnavailable, Message: ToolTimeout, Detail: err.Error()}
	}
	return &BridgeError{Code: CodeHostUnavailable, Message: ToolUnreachable, Detail: err.Error()}
}

// untrustedTLS says whether a request failed on the server's certificate: a chain the machine's roots do not end, a name
// it is not for, or one out of date.
func untrustedTLS(err error) bool {
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verify *tls.CertificateVerificationError
	return errors.As(err, &unknown) || errors.As(err, &host) || errors.As(err, &invalid) || errors.As(err, &verify)
}

// reserved are the ranges that are no public host though some are "global unicast" to the standard library: the
// local-network, shared, benchmarking, documentation and transition (NAT64, 6to4, Teredo) ranges. The allow rule is
// IsGlobalUnicast; these are cut out of it. An IPv4 address inside 6to4 (2002::/16), NAT64 (64:ff9b::/96) or Teredo
// (2001::/32) is blocked with its whole range, whatever it embeds.
var reserved = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16",
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

// publicAddress says whether a host may be dialed: a global unicast address, not private, and in none of the reserved
// ranges. An IPv4-mapped IPv6 address is judged as the IPv4 address it holds.
func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, p := range reserved {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

const maxMCPAnswer = 4 << 20

// rpc sends one message; id 0 is a notification, which has no answer to read.
func (c *mcpConn) rpc(ctx context.Context, id int, method string, params any) (json.RawMessage, error) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id != 0 {
		msg["id"] = id
	}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers {
		// A header with a placeholder (a secret the install keeps) is not filled here: the host does not hand secrets
		// to a view's call yet.
		if !strings.Contains(v, "${") {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportFailure(ctx, err)
	}
	defer resp.Body.Close()
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		c.session = s
	}
	if resp.StatusCode >= 300 {
		return nil, &BridgeError{Code: CodeUpstreamError, Message: ToolUnreachable, Detail: fmt.Sprintf("the MCP server answered %d", resp.StatusCode)}
	}
	if id == 0 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxMCPAnswer))
		return nil, nil
	}
	raw, err := readAnswer(resp, id)
	if err != nil {
		return nil, err
	}
	var rpc struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpc); err != nil {
		return nil, &BridgeError{Code: CodeUpstreamError, Message: ToolUnreachable, Detail: "the MCP server answered with something else than JSON-RPC"}
	}
	if rpc.Error != nil {
		return nil, &BridgeError{Code: CodePolicyBlocked, Message: ToolError, Detail: rpc.Error.Message}
	}
	return rpc.Result, nil
}

// readAnswer is the JSON-RPC message with the id: the body itself, or the matching event of an event stream.
func readAnswer(resp *http.Response, id int) ([]byte, error) {
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return io.ReadAll(io.LimitReader(resp.Body, maxMCPAnswer))
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, maxMCPAnswer))
	sc.Buffer(make([]byte, 0, 64<<10), maxMCPAnswer)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		var probe struct {
			ID json.Number `json:"id"`
		}
		data = strings.TrimSpace(data)
		if json.Unmarshal([]byte(data), &probe) == nil && probe.ID.String() == fmt.Sprint(id) {
			return []byte(data), nil
		}
	}
	return nil, &BridgeError{Code: CodeHostUnavailable, Message: ToolUnreachable, Detail: "the MCP server sent no answer"}
}
