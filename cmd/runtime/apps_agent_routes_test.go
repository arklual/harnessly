package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/gate"
)

func TestAgentTokenIsConfinedToItsList(t *testing.T) {
	cli := &gate.CLIToken{DataDir: t.TempDir(), Headers: []string{"x-cli-token"}}
	token, err := cli.IssueAgent("abcdef0123456789", 1e12)
	if err != nil {
		t.Fatal(err)
	}
	h := confineAgentToken(cli, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	cases := []struct {
		method, path string
		passes       bool
	}{
		{"POST", "/api/status/hook", true},
		{"POST", "/agents/api/status/hook", true},
		{"POST", "/api/cli/preview/mcp", true},
		{"POST", "/api/cli/asks", true},
		{"POST", "/api/cli/deliver", true},
		{"GET", "/api/cli/apps", true},
		{"POST", "/api/cli/secrets/use", true},
		{"GET", "/api/cli/tabs", true},
		{"GET", "/api/cli/tabs/t1", true},
		{"GET", "/api/cli/workspaces", true},
		// The person's trackers and wikis, read through the runtime: the agent never holds their tokens.
		{"GET", "/api/cli/connections", true},
		{"GET", "/api/cli/connections/env-tracker/issue", true},
		{"POST", "/api/cli/connections/env-tracker/issue", false},
		{"DELETE", "/api/cli/connections/env-tracker", false},
		// What runs or reads outside the wall.
		{"POST", "/api/cli/tabs", false},
		{"POST", "/api/cli/tabs/t1/send", false},
		{"DELETE", "/api/cli/tabs/t1", false},
		{"GET", "/api/cli/tabs/t1/result", false},
		{"POST", "/api/cli/tabs/t1/browser/eval", false},
		{"POST", "/api/tmux/send-input", false},
		{"GET", "/api/tmux/capture", false},
		{"POST", "/api/layout/pane/p/tabs", false},
		{"POST", "/api/workspace", false},
		{"POST", "/api/cli/peers/send", false},
		{"POST", "/api/cli/vault", false},
		{"GET", "/api/terminal", false},
		{"GET", "/anything/new", false},
		// A path the router may read otherwise.
		{"POST", "/api/cli/preview/../tmux/send-input", false},
		{"POST", "/api/cli/asks//x", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Method, r.URL.Path = c.method, c.path
		r.Header.Set("x-cli-token", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if got := w.Code == http.StatusTeapot; got != c.passes {
			t.Errorf("%s %s: passed %v, want %v", c.method, c.path, got, c.passes)
		}
	}
	// Anyone else is the routes' own business: the person's token, the page's session, nobody.
	r := httptest.NewRequest(http.MethodPost, "/api/tmux/send-input", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTeapot {
		t.Errorf("a request with no agent token was stopped: %d", w.Code)
	}
}
