package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeGitHub(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var auth []string
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("GET /repos/acme/app/issues/12", func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		reply(w, map[string]any{"number": 12, "title": "Crash on start", "body": "Steps…", "state": "closed", "state_reason": "completed",
			"assignee": map[string]string{"login": "ada", "avatar_url": "https://avatars.example/ada"}, "labels": []map[string]string{{"name": "priority: high"}},
			"updated_at": "2026-10-20T10:00:00Z", "comments": 12})
	})
	mux.HandleFunc("GET /repos/acme/app/issues/12/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "2" {
			t.Errorf("the last page of comments: %s", r.URL.RawQuery)
		}
		reply(w, []map[string]any{{"user": map[string]string{"login": "bob"}, "body": "Fixed", "created_at": "2026-10-20T09:00:00Z"}})
	})
	mux.HandleFunc("GET /repos/acme/app/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"title": "Fix crash", "state": "closed", "merged": true, "draft": false})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &auth
}

func TestRefs(t *testing.T) {
	c := FromEnv(func(string) string { return "" })
	if c.API != "https://api.github.com" || c.Web != "github.com" {
		t.Fatalf("defaults %+v", c)
	}
	ghes := FromEnv(func(k string) string { return map[string]string{"RUNTIME_GITHUB_HOST": "git.example.com"}[k] })
	if ghes.API != "https://git.example.com/api/v3" {
		t.Fatalf("enterprise API %q", ghes.API)
	}
	if r, ok := ParseKey("acme/app#12"); !ok || r.Owner != "acme" || r.Repo != "app" || r.Number != 12 {
		t.Fatalf("key %+v", r)
	}
	for _, bad := range []string{"acme#12", "acme/app#0", "acme/app#x", "../x/y#1", "acme/app/#1"} {
		if _, ok := ParseKey(bad); ok {
			t.Errorf("key %q parsed", bad)
		}
	}
	if r, ok := c.ParseURL("https://github.com/acme/app/pull/7"); !ok || !r.Pull || r.Number != 7 {
		t.Fatalf("pull url %+v", r)
	}
	for _, bad := range []string{"http://github.com/acme/app/issues/1", "https://evil.com/acme/app/issues/1", "https://github.com/acme/app/issues/1/x", "https://u:p@github.com/acme/app/issues/1"} {
		if _, ok := c.ParseURL(bad); ok {
			t.Errorf("url %q parsed", bad)
		}
	}
	c.Repos = []string{"acme/*", "other/one"}
	if !c.Allowed(Ref{Owner: "ACME", Repo: "app"}) || !c.Allowed(Ref{Owner: "other", Repo: "one"}) || c.Allowed(Ref{Owner: "other", Repo: "two"}) {
		t.Fatal("allowlist")
	}
}

func TestTaskAndLinkMeta(t *testing.T) {
	srv, auth := fakeGitHub(t)
	c := Config{API: srv.URL, Web: "github.com", Token: "tok"}
	ctx := context.Background()
	task, err := c.Task(ctx, Ref{Owner: "acme", Repo: "app", Number: 12})
	if err != nil {
		t.Fatal(err)
	}
	if task.Key != "acme/app#12" || !task.Resolved || *task.State != "Closed" || task.Assignee.Name != "ada" || *task.Priority != "priority: high" ||
		len(task.Comments) != 1 || task.Comments[0].Author != "bob" || task.URL != "https://github.com/acme/app/issues/12" || task.Provider != "github" {
		t.Fatalf("task %+v", task)
	}
	if (*auth)[0] != "Bearer tok" {
		t.Fatalf("auth %v", *auth)
	}
	meta, err := c.LinkMeta(ctx, Ref{Owner: "acme", Repo: "app", Number: 7, Pull: true})
	if pm, ok := meta.(PullMeta); err != nil || !ok || pm.State != "merged" || pm.Kind != "mr" || pm.Project != "acme/app" {
		t.Fatalf("pull meta %v %+v", err, meta)
	}
	if _, err := c.Task(ctx, Ref{Owner: "acme", Repo: "app", Number: 99}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	c.Repos = []string{"other/*"}
	if _, err := c.Task(ctx, Ref{Owner: "acme", Repo: "app", Number: 12}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a repository outside the list reads as missing: %v", err)
	}
}
