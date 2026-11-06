package apps

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mcpStub answers initialize and tools/call as a Streamable HTTP MCP server would.
func mcpStub(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Error(err)
		}
		if msg.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok " + msg.Method}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	}
}

func withNetwork(t *testing.T, n DeployNetwork) {
	t.Helper()
	SetNetwork(n)
	t.Cleanup(func() { SetNetwork(DeployNetwork{}) })
}

func codeOf(err error) string {
	var be *BridgeError
	if errors.As(err, &be) {
		return be.Code
	}
	return ""
}

func TestInternalHostTheAdminListsIsCalled(t *testing.T) {
	srv := httptest.NewServer(mcpStub(t))
	defer srv.Close()
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	// Not listed: plain http is refused before any dial.
	if _, err := CallTool(context.Background(), MCPServer{URL: u}, "t", nil); codeOf(err) != CodeHostNotAllowed {
		t.Fatalf("unlisted: %v", err)
	}
	withNetwork(t, DeployNetwork{Internal: []string{"localhost"}})
	raw, err := CallTool(context.Background(), MCPServer{URL: u}, "t", nil)
	if err != nil || !strings.Contains(string(raw), "ok tools/call") {
		t.Fatalf("listed: %v %s", err, raw)
	}
}

func TestInternalSuffixAndLinkLocal(t *testing.T) {
	n := DeployNetwork{Internal: []string{"*.corp", "gitlab.local"}}
	for host, want := range map[string]bool{"mr.corp": true, "a.b.corp": true, "corp": false, "evilcorp": false,
		"GitLab.Local.": true, "gitlab.local.evil": false} {
		if n.IsInternal(host) != want {
			t.Errorf("%s: want %v", host, want)
		}
	}
	// A listed host is still never dialed at the cloud's metadata address.
	for addr, refused := range map[string]bool{"10.0.0.5:443": false, "127.0.0.1:80": false, "169.254.169.254:80": true,
		"[fe80::1]:443": true, "0.0.0.0:80": true} {
		if err := dialControl(true)("tcp", addr, nil); (err != nil) != refused {
			t.Errorf("%s: %v", addr, err)
		}
	}
}

func TestTLSOfAnInternalHost(t *testing.T) {
	srv := httptest.NewTLSServer(mcpStub(t))
	defer srv.Close()
	// The test server's certificate is for 127.0.0.1, signed by no root the system has.
	withNetwork(t, DeployNetwork{Internal: []string{"127.0.0.1"}})
	if _, err := CallTool(context.Background(), MCPServer{URL: srv.URL}, "t", nil); codeOf(err) != CodeTLSUntrusted {
		t.Fatalf("untrusted: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	withNetwork(t, DeployNetwork{Internal: []string{"127.0.0.1"}, Roots: roots})
	if _, err := CallTool(context.Background(), MCPServer{URL: srv.URL}, "t", nil); err != nil {
		t.Fatalf("with the organization's root: %v", err)
	}
}

func TestUpstreamErrorIsItsOwnCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	withNetwork(t, DeployNetwork{Internal: []string{"127.0.0.1"}})
	if _, err := CallTool(context.Background(), MCPServer{URL: srv.URL}, "t", nil); codeOf(err) != CodeUpstreamError {
		t.Fatalf("502: %v", err)
	}
}

func TestNetworkFromEnv(t *testing.T) {
	n, err := NetworkFromEnv(func(k string) string {
		return map[string]string{"RUNTIME_APPS_INTERNAL_HOSTS": " GitLab.corp , *.svc ,,"}[k]
	})
	if err != nil || len(n.Internal) != 2 || n.Internal[0] != "gitlab.corp" || n.Roots != nil {
		t.Fatalf("%v %+v", err, n)
	}
	if _, err := NetworkFromEnv(func(k string) string {
		return map[string]string{"RUNTIME_APPS_CA_FILE": "/nonexistent.pem"}[k]
	}); err == nil {
		t.Fatal("a missing CA file is an error")
	}
}
