package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
)

func TestMCPProxyRefusesWhatIsNotThisLaunchs(t *testing.T) {
	data := t.TempDir()
	cli := &gate.CLIToken{DataDir: data, Headers: []string{"x-cli-token"}}
	d := deps{DataDir: data, Home: t.TempDir(), CLI: cli}
	reg := apps.Open(data)
	// The launch "aaaaaaaaaaaaaaaa" has acme.pay on; "bbbbbbbbbbbbbbbb" has acme.other.
	grant := func(session, app string) {
		dir := filepath.Join(data, "app-launch", session)
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(filepath.Join(dir, "grant.json"), []byte(`{"apps":["`+app+`"]}`), 0o600)
	}
	grant("aaaaaaaaaaaaaaaa", "acme.pay")
	grant("bbbbbbbbbbbbbbbb", "acme.other")
	route := appMCPProxyRoute(d, reg)
	tokA, _ := cli.IssueAgent("aaaaaaaaaaaaaaaa", 1e12)
	tokB, _ := cli.IssueAgent("bbbbbbbbbbbbbbbb", 1e12)
	person := "person-cli-token"
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte(person), 0o600)
	cases := []struct {
		name, token, path string
		want              int
	}{
		{"no token", "", "/api/cli/apps/mcp/acme.pay/pay", http.StatusForbidden},
		{"the person's token is not an agent's", person, "/api/cli/apps/mcp/acme.pay/pay", http.StatusForbidden},
		{"another chat's token", tokB, "/api/cli/apps/mcp/acme.pay/pay", http.StatusForbidden},
		{"an app that is not on in the chat", tokA, "/api/cli/apps/mcp/acme.other/pay", http.StatusForbidden},
		{"on, but not installed", tokA, "/api/cli/apps/mcp/acme.pay/pay", http.StatusNotFound},
		{"a bad path", tokA, "/api/cli/apps/mcp/acme.pay", http.StatusNotFound},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, c.path, nil)
		if c.token != "" {
			r.Header.Set("x-cli-token", c.token)
		}
		if !route.Match(r) {
			t.Fatalf("%s: route does not match", c.name)
		}
		w := httptest.NewRecorder()
		route.Handler.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, w.Code, c.want)
		}
	}
}
