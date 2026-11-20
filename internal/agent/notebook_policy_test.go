package agent

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotebookRespectsTerminalPolicy(t *testing.T) {
	f := newRouteFilter(1, "/m/example", DefaultPolicy, nil)
	for _, path := range []string{
		"/api/v2/conversations/tab-a/notebook",
		"/agents/api/v2/conversations/tab-a/notebook",
		"/m/example/api/v2/conversations/tab-a/notebook",
		"/m/example/agents/api/v2/conversations/tab-a/notebook",
		"/m/example/api/v2/conversations/tab-a/%6eotebook",
	} {
		for _, terminal := range []bool{false, true} {
			p := DefaultPolicy()
			p.Terminal = terminal
			r := httptest.NewRequest("POST", "http://example.org"+path+"?path=book.ipynb", strings.NewReader(`{"action":"execute","code":"print(1)"}`))
			r.Header.Set("Content-Type", "application/json")
			reason, err := f.decide(p, r)
			if err != nil || (reason == "") != terminal {
				t.Errorf("%s terminal=%v: %q %v", path, terminal, reason, err)
			}
		}
	}
	if isTerminalRoute("/api/v2/conversations/notebook/context") {
		t.Fatal("conversation name mistaken for execution route")
	}
}
