package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// an app with a remote MCP server turned on in a chat whose agent already runs goes live, and the harnessly-app
// server offers its server's tools under the app's name, calls them from the machine with the package's headers, and
// takes them away when the app is turned off.
func TestAppMCPProxyOfAnAppTurnedOnLive(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		mu.Lock()
		seen = append(seen, msg.Method+" "+r.Header.Get("X-Plain"))
		mu.Unlock()
		if msg.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch msg.Method {
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "balance", "description": "The balance.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"card": map[string]any{"type": "string"}}}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "balance of " + string(msg.Params)}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": result})
	}))
	defer remote.Close()
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}, Roots: roots})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })

	data := t.TempDir()
	reg := &apps.Registry{Root: filepath.Join(data, "apps")}
	dir := t.TempDir()
	for file, body := range map[string]string{
		"harnessly/app.json": fmt.Sprintf(liveTestManifest, "acme.cash", "Кэшбэк", `{}`),
		"instructions.md":    "Instructions of Кэшбэк.",
		"ui/index.html":      "<p>x</p>",
		".mcp.json":          `{"mcpServers":{"bank":{"type":"http","url":"` + remote.URL + `/mcp","headers":{"X-Plain":"1"}}}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := reg.Install(dir, apps.InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	pkg, err := reg.Package("acme.cash")
	if err != nil || len(pkg.MCP) != 1 {
		t.Fatalf("package: %v %+v", err, pkg)
	}
	if needs := apps.LaunchNeeds(pkg); needs != "" {
		t.Fatalf("a remote server needs no launch: %q", needs)
	}
	l := &liveApps{reg: reg, launched: func(string) []string { return nil }}
	if !l.changeIsLive(wire.Conversation{ID: "c1", Session: "s1", Provider: wire.ProviderClaude}, []string{"acme.cash"}) {
		t.Fatal("the app goes live")
	}

	d := deps{DataDir: data, Home: t.TempDir(), CLI: &gate.CLIToken{DataDir: data, Headers: []string{"x-cli-token"}}}
	on := []string{"acme.cash"}
	srv := &mcpserver.Server{Name: apps.HostServer, ListChanged: true,
		Sets: []mcpserver.Toolset{appMCPFor(d, reg, apps.NewRemoteTools(), func() []string { return on })}}
	ask := func(method string, params *jsvalue.Object) *jsvalue.Object {
		t.Helper()
		_, body := srv.Handle(context.Background(), jsvalue.NewObject("jsonrpc", "2.0", "id", 1, "method", method, "params", params))
		v, err := jsvalue.Parse(body)
		if err != nil {
			t.Fatalf("%s: %v %s", method, err, body)
		}
		return v.(*jsvalue.Object)
	}
	list := string(jsvalue.Stringify(ask("tools/list", jsvalue.NewObject()), ""))
	if !strings.Contains(list, `"name":"acme-cash_bank_balance"`) || !strings.Contains(list, "[Кэшбэк] The balance.") {
		t.Fatalf("tools/list: %s", list)
	}
	out := string(jsvalue.Stringify(ask("tools/call", jsvalue.NewObject("name", "acme-cash_bank_balance",
		"arguments", jsvalue.NewObject("card", "4242"))), ""))
	if !strings.Contains(out, `balance of {\"arguments\":{\"card\":\"4242\"},\"name\":\"balance\"}`) {
		t.Fatalf("tools/call: %s", out)
	}
	mu.Lock()
	for _, s := range seen {
		if !strings.HasSuffix(s, " 1") {
			t.Errorf("a request went without the package's header: %q", s)
		}
	}
	mu.Unlock()

	on = nil
	if list := string(jsvalue.Stringify(ask("tools/list", jsvalue.NewObject()), "")); strings.Contains(list, "acme-cash") {
		t.Fatalf("an app turned off keeps its tools: %s", list)
	}
}
