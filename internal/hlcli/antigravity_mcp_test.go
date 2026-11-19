package hlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravityMCPScopedTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli-token")
	if err := os.WriteFile(path, []byte("fixture-launch-token"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/cli/preview/mcp" || r.Header.Get("x-cli-token") != "fixture-launch-token" {
			t.Errorf("wrong MCP route/credential")
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if _, ok := req["id"]; !ok {
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{"ok": true}})
	}))
	defer srv.Close()
	env := map[string]string{"HARNESSLY_ANTIGRAVITY_CONVERSATION": "tab-probe", "HARNESSLY_ANTIGRAVITY_GENERATION": "generation-probe", "HARNESSLY_CLI_TOKEN_FILE": path, "HARNESSLY_CLI_TOKEN": "fixture-owner-token-must-not-be-used", "HARNESSLY_URL": srv.URL}
	var out, stderr bytes.Buffer
	rc := Run([]string{"antigravity-mcp"}, Env{Getenv: func(k string) string { return env[k] }, Session: func() string { return "probe" }, Client: srv.Client(), Stdin: strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"), Stdout: &out, Stderr: &stderr})
	if rc != 0 || calls != 3 || strings.Count(strings.TrimSpace(out.String()), "\n") != 1 {
		t.Fatalf("rc=%d calls=%d stdout=%s stderr=%s", rc, calls, out.String(), stderr.String())
	}
	if strings.Contains(out.String(), "fixture-") || strings.Contains(stderr.String(), "fixture-") {
		t.Fatal("credential in protocol/log")
	}
}

func TestAntigravityMCPRefusesUnbound(t *testing.T) {
	for _, env := range []map[string]string{{}, {"HARNESSLY_ANTIGRAVITY_CONVERSATION": "tab-x", "HARNESSLY_ANTIGRAVITY_GENERATION": "g"}} {
		var out, stderr bytes.Buffer
		if rc := Run([]string{"antigravity-mcp"}, Env{Getenv: func(k string) string { return env[k] }, Session: func() string { return "probe" }, Stdout: &out, Stderr: &stderr}); rc == 0 || out.Len() != 0 {
			t.Fatalf("unbound bridge accepted: %d %s", rc, out.String())
		}
	}
}
