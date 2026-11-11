package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitLabBranch(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-good" {
			provJSON(w, 401, `{"message":"401 Unauthorized"}`)
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/g%2Fp":
			provJSON(w, 200, `{"default_branch":"main"}`)
		case "/api/v4/projects/g%2Fp/repository/compare":
			if r.URL.Query().Get("from") != "main" || r.URL.Query().Get("to") != "agent/x" {
				provJSON(w, 404, `{"message":"404"}`)
				return
			}
			provJSON(w, 200, `{"commits":[{"id":"aaa111","short_id":"aaa1","title":"first","author_name":"A","created_at":"2026-11-10T10:00:00Z","web_url":"u1"},
{"id":"bbb222","short_id":"bbb2","title":"second","author_name":"A","created_at":"2026-11-10T11:00:00Z","web_url":"u2"}],
"diffs":[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-x\n+y\n"},{"old_path":"n.go","new_path":"n.go","new_file":true,"diff":"@@ -0,0 +1 @@\n+z\n"}]}`)
		case "/api/v4/projects/g%2Fp/merge_requests":
			provJSON(w, 200, `[{"iid":9,"title":"X","state":"opened","web_url":"https://g/g/p/-/merge_requests/9","source_branch":"agent/x"}]`)
		default:
			provJSON(w, 404, `{"message":"404"}`)
		}
	}))
	defer srv.Close()
	c := gitLabProvider{}.Open(Conn{Kind: GitLab, BaseURL: srv.URL, Deployment: SelfHosted}, Credentials{FieldToken: "glpat-good"}, NewHTTP(nil))
	b, err := c.(BranchSource).Branch(context.Background(), "g/p", "", "agent/x")
	if err != nil {
		t.Fatalf("branch: %v (%v)", err, seen)
	}
	if b.Base != "main" || len(b.Commits) != 2 || b.Commits[0].Sha != "bbb222" || len(b.Files) != 2 || b.MR == nil || b.MR.IID != 9 {
		t.Fatalf("branch = %+v", b)
	}
	d := b.GitDiff()
	for _, want := range []string{"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n",
		"diff --git a/n.go b/n.go\nnew file mode 100644\n--- /dev/null\n+++ b/n.go\n@@ -0,0 +1 @@\n+z\n"} {
		if !strings.Contains(d, want) {
			t.Fatalf("diff lacks %q:\n%s", want, d)
		}
	}
	bad := gitLabProvider{}.Open(Conn{Kind: GitLab, BaseURL: srv.URL}, Credentials{FieldToken: "bad"}, NewHTTP(nil))
	if _, err := bad.(BranchSource).Branch(context.Background(), "g/p", "main", "agent/x"); Reason(err) != "denied" {
		t.Fatalf("bad token: %v", err)
	}
}
