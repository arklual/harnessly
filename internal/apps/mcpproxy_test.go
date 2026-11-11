package apps

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The proxy's dial refuses a non-public address, which every httptest server is, so the header logic is judged on the
// request the proxy builds and the network rule on the refusal.
func TestProxyRefusesNonPublicTarget(t *testing.T) {
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("must not reach a loopback server") }))
	defer up.Close()
	srv := MCPServer{URL: up.URL, Headers: map[string]string{"Authorization": "Bearer ${secret:k}"}}
	rec := httptest.NewRecorder()
	ProxyMCP(rec, httptest.NewRequest("POST", "/x", strings.NewReader("{}")), srv, map[string]string{"k": secretValue})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("a loopback upstream is refused like hl.tools.call does: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secretValue) {
		t.Fatal("the answer holds the secret")
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProxyAddsSecretHeadersAndDropsTheClients(t *testing.T) {
	var seen *http.Request
	old := mcpProxyTransport
	mcpProxyTransport = func(string) http.RoundTripper {
		return rtFunc(func(r *http.Request) (*http.Response, error) {
			seen = r
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "Mcp-Session-Id": {"s1"}},
				Body: http.NoBody, Request: r}, nil
		})
	}
	defer func() { mcpProxyTransport = old }()
	srv := MCPServer{URL: "https://pay.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${secret:k}"}}
	req := httptest.NewRequest("POST", "/x", strings.NewReader("{}"))
	req.Header.Set("X-Runtime-Token", "agent.tok")
	req.Header.Set("Authorization", "evil")
	req.Header.Set("Mcp-Session-Id", "s0")
	rec := httptest.NewRecorder()
	ProxyMCP(rec, req, srv, map[string]string{"k": secretValue})
	if seen == nil || seen.URL.String() != "https://pay.example.com/mcp" {
		t.Fatalf("upstream: %v", seen)
	}
	if seen.Header.Get("Authorization") != "Bearer "+secretValue || seen.Header.Get("X-Runtime-Token") != "" || seen.Header.Get("Mcp-Session-Id") != "s0" {
		t.Fatalf("headers: %v", seen.Header)
	}
	if rec.Code != 200 || rec.Header().Get("Mcp-Session-Id") != "s1" || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("answer: %d %v", rec.Code, rec.Header())
	}
}

func TestProxyWaitsForMissingSecretAndNeedsHTTPS(t *testing.T) {
	srv := MCPServer{URL: "https://x.example.com", Headers: map[string]string{"A": "${secret:k}"}}
	rec := httptest.NewRecorder()
	ProxyMCP(rec, httptest.NewRequest("POST", "/x", nil), srv, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("no value: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ProxyMCP(rec, httptest.NewRequest("POST", "/x", nil), MCPServer{URL: "http://x.example.com"}, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("plain http: %d", rec.Code)
	}
}

func withProxyTransport(t *testing.T, rt rtFunc) {
	t.Helper()
	old := mcpProxyTransport
	mcpProxyTransport = func(string) http.RoundTripper { return rt }
	t.Cleanup(func() { mcpProxyTransport = old })
}

func TestProxyAnswersAToolCallWhenTheServiceIsDown(t *testing.T) {
	withProxyTransport(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: connection refused") })
	srv := MCPServer{URL: "https://boards.example.com/mcp"}
	rec := httptest.NewRecorder()
	ProxyMCPAs(rec, httptest.NewRequest("POST", "/x", strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"board_read"}}`)), srv, nil, "Boards")
	var out struct {
		ID     int `json:"id"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body)
	}
	if out.ID != 7 || !out.Result.IsError || len(out.Result.Content) != 1 || out.Result.Content[0].Text != UnavailableText("Boards") {
		t.Fatalf("a tool result the agent reads: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Fatal("the transport's error stays in the runtime")
	}
}

func TestProxyAnswersOtherRequestsWithAnErrorOn503(t *testing.T) {
	withProxyTransport(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})
	srv := MCPServer{URL: "https://docs.example.com/mcp"}
	rec := httptest.NewRecorder()
	ProxyMCPAs(rec, httptest.NewRequest("POST", "/x", strings.NewReader(`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)), srv, nil, "Documents")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"error":{"code":-32000,"message":"The service of Documents is unavailable`) {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body)
	}
	// A notification has nothing to answer: the bare status goes on.
	rec = httptest.NewRecorder()
	ProxyMCPAs(rec, httptest.NewRequest("POST", "/x", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)), srv, nil, "Documents")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("notification: %d", rec.Code)
	}
}

func TestProxyPassesTheBodyOn(t *testing.T) {
	var got string
	withProxyTransport(t, func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`
	ProxyMCPAs(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", strings.NewReader(body)), MCPServer{URL: "https://a.example.com/mcp"}, nil, "A")
	if got != body {
		t.Fatalf("upstream got %q", got)
	}
}
