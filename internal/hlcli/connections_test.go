package hlcli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// connRuntime answers the connection routes of the CLI: a Jira that is primary for issues and a Confluence for pages.
func connRuntime(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("x-cli-token") != "tok" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/api/cli/connections":
			_, _ = w.Write([]byte(`{"connections":[
				{"id":"env-tracker","kind":"youtrack","name":"tracker.example.com","baseUrl":"https://tracker.example.com","state":"ok","env":true,"capabilities":["issues"]},
				{"id":"c1","kind":"jira","name":"Acme Jira","baseUrl":"https://acme.atlassian.net","state":"ok","primary":true,"capabilities":["issues"],"me":{"name":"Ann Lee"}},
				{"id":"c2","kind":"confluence","name":"Acme Wiki","baseUrl":"https://acme.atlassian.net/wiki","state":"denied","capabilities":["pages"]}],"kinds":["jira"]}`))
		case "/api/cli/connections/c1/issues":
			_, _ = w.Write([]byte(`{"issues":[{"key":"ABC-1","title":"Login bug","url":"https://acme.atlassian.net/browse/ABC-1","status":{"name":"In Progress","category":"doing"},"assignee":{"name":"Ann Lee"}}]}`))
		case "/api/cli/connections/c1/issue":
			if r.URL.Query().Get("key") != "ABC-1" {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":"not-found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"issue":{"key":"ABC-1","title":"Login bug","url":"https://acme.atlassian.net/browse/ABC-1","status":{"name":"In Progress"},"description":"Steps to reproduce","comments":[{"author":{"name":"Bob"},"text":"seen it","created":"2026-11-01T10:00:00Z"}]}}`))
		case "/api/cli/connections/resolve":
			if !strings.Contains(r.URL.Query().Get("url"), "atlassian.net") {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":"not-found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"connection":{"id":"c2"},"target":{"kind":"page","key":"42"},"page":{"id":"42","title":"Runbook","url":"https://acme.atlassian.net/wiki/x","space":"ENG","body":"Restart it."}}`))
		case "/api/cli/connections/c2/pages":
			w.WriteHeader(424)
			_, _ = w.Write([]byte(`{"error":"denied"}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"not-found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func runConn(t *testing.T, url string, args ...string) result {
	t.Helper()
	return runWith(t, t.TempDir(), map[string]string{"HARNESSLY_URL": url, "HARNESSLY_CLI_TOKEN": "tok"}, "", args...)
}

func TestConnectionsCommands(t *testing.T) {
	srv, asked := connRuntime(t)
	r := runConn(t, srv.URL, "connections")
	if r.code != 0 || !strings.Contains(r.out, "c1\tjira\tAcme Jira\thttps://acme.atlassian.net\tissues\tok,primary\tas Ann Lee") ||
		!strings.Contains(r.out, "env-tracker\tyoutrack") || !strings.Contains(r.out, "ok,deployment") {
		t.Fatalf("%d %q %q", r.code, r.out, r.errs)
	}
	r = runConn(t, srv.URL, "issues", "--mine", "--open", "--project", "ABC", "login", "bug")
	if r.code != 0 || r.out != "ABC-1\t[In Progress]\tLogin bug\t@Ann Lee\thttps://acme.atlassian.net/browse/ABC-1\n" {
		t.Fatalf("%d %q %q", r.code, r.out, r.errs)
	}
	found := false
	for _, a := range *asked {
		found = found || a == "GET /api/cli/connections/c1/issues?mine=1&open=1&project=ABC&q=login+bug"
	}
	if !found {
		t.Fatalf("the primary tracker was not asked: %v", *asked)
	}
	r = runConn(t, srv.URL, "issue", "ABC-1")
	if r.code != 0 || !strings.Contains(r.out, "ABC-1  Login bug\nstatus:   In Progress\n") || !strings.Contains(r.out, "Steps to reproduce") ||
		!strings.Contains(r.out, "— Bob, ") || !strings.Contains(r.out, "seen it") {
		t.Fatalf("%d %q %q", r.code, r.out, r.errs)
	}
	if r = runConn(t, srv.URL, "issue", "ABC-9"); r.code != 1 || !strings.Contains(r.errs, "refused: not-found") {
		t.Fatalf("%d %q", r.code, r.errs)
	}
	r = runConn(t, srv.URL, "page", "https://acme.atlassian.net/wiki/spaces/ENG/pages/42")
	if r.code != 0 || !strings.Contains(r.out, "Runbook\nspace:    ENG") || !strings.Contains(r.out, "Restart it.") {
		t.Fatalf("%d %q %q", r.code, r.out, r.errs)
	}
	if r = runConn(t, srv.URL, "issue", "https://jira.other.com/browse/X-1"); r.code != 1 || !strings.Contains(r.errs, "no connection owns jira.other.com") ||
		!strings.Contains(r.errs, "Settings → Connections") {
		t.Fatalf("%d %q", r.code, r.errs)
	}
	if r = runConn(t, srv.URL, "pages", "runbook"); r.code != 1 || !strings.Contains(r.errs, "refused: denied") || !strings.Contains(r.errs, "reconnect") {
		t.Fatalf("%d %q", r.code, r.errs)
	}
	if r = runConn(t, srv.URL, "--json", "issues", "--conn", "c1"); r.code != 0 || !strings.Contains(r.out, `"key": "ABC-1"`) {
		t.Fatalf("%d %q", r.code, r.out)
	}
	if r = runConn(t, srv.URL, "issues", "--bogus"); r.code != 2 {
		t.Fatalf("%d %q", r.code, r.errs)
	}
	if r = runConn(t, srv.URL, "issues", "--help"); r.code != 0 || !strings.Contains(r.out, "Settings → Connections") {
		t.Fatalf("%d %q", r.code, r.out)
	}
	if !strings.Contains(helpText, "harnessly issues") || !strings.Contains(helpText, "harnessly connections") {
		t.Fatal("the help does not list the commands")
	}
}
