package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestValidPick(t *testing.T) {
	for _, c := range []struct {
		p   PickRequest
		bad string
	}{
		{PickRequest{List: PickProjects}, ""},
		{PickRequest{List: PickProjects, Query: "app"}, ""},
		{PickRequest{List: "x"}, "list"},
		{PickRequest{List: PickMRs, Project: "team/app"}, ""},
		{PickRequest{List: PickMRs, Project: "team/sub/app", Filter: PickReview}, ""},
		{PickRequest{List: PickMRs, Project: "team/app", Filter: "all"}, "filter"},
		{PickRequest{List: PickMRs, Project: "app"}, "project"},
		{PickRequest{List: PickMRs, Filter: PickMine}, ""},
		{PickRequest{List: PickMRs, Filter: PickReview}, ""},
		{PickRequest{List: PickMRs, Filter: PickOpen}, "project"},
		{PickRequest{List: PickBranches, Filter: PickMine}, "project"},
		{PickRequest{List: PickMRs, Project: "../app"}, "project"},
		{PickRequest{List: PickBranches, Project: "team/a b"}, "project"},
		{PickRequest{List: PickCommits, Project: "team/app"}, "ref"},
		{PickRequest{List: PickCommits, Project: "team/app", Ref: "main"}, ""},
	} {
		if got := ValidPick(c.p); got != c.bad {
			t.Errorf("%+v: %q, want %q", c.p, got, c.bad)
		}
	}
}

func TestMrQuery(t *testing.T) {
	q := mrQuery(PickOpen, "", "me")
	if q.Get("state") != "opened" || q.Get("search") != "" || q.Get("reviewer_username") != "" {
		t.Errorf("open: %v", q)
	}
	q = mrQuery(PickReview, "!42", "eva")
	if q.Get("iids[]") != "42" || q.Get("state") != "all" || q.Get("reviewer_username") != "eva" {
		t.Errorf("number: %v", q)
	}
	q = mrQuery(PickMine, " queue fix ", "eva")
	if q.Get("search") != "queue fix" || q.Get("in") != "title" || q.Get("author_username") != "eva" || q.Get("state") != "opened" {
		t.Errorf("words: %v", q)
	}
}

func TestGitLabPickLists(t *testing.T) {
	var asked []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		if r.Header.Get("Private-Token") != "self" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/projects":
			_, _ = w.Write([]byte(`[{"id":7,"path_with_namespace":"team/app","name":"app","web_url":"https://gitlab.example.com/team/app","last_activity_at":"2026-11-10T10:00:00Z"},{"id":8,"path_with_namespace":""}]`))
		case "/api/v4/user":
			_, _ = w.Write([]byte(`{"username":"eva","name":"Eva"}`))
		case "/api/v4/projects/team%2Fapp/merge_requests":
			_, _ = w.Write([]byte(`[{"iid":5,"title":"Queue","state":"opened","draft":true,"web_url":"https://gitlab.example.com/team/app/-/merge_requests/5",
				"source_branch":"feat","target_branch":"main","author":{"username":"fil","name":"Fil"},"updated_at":"2026-11-10T11:00:00Z"}]`))
		case "/api/v4/merge_requests":
			_, _ = w.Write([]byte(`[{"iid":9,"title":"Mine","state":"opened","web_url":"https://gitlab.example.com/team/sub/app/-/merge_requests/9",
				"source_branch":"x","target_branch":"main","references":{"full":"team/sub/app!9"},"updated_at":"2026-11-10T12:00:00Z"}]`))
		case "/api/v4/projects/team%2Fapp/repository/branches":
			_, _ = w.Write([]byte(`[{"name":"a","commit":{"id":"aaa","title":"old","committed_date":"2026-01-01T00:00:00Z","web_url":"https://gitlab.example.com/team/app/-/commit/aaa"}},
				{"name":"b","commit":{"id":"bbb","title":"new","committed_date":"2026-11-01T00:00:00Z","web_url":"https://gitlab.example.com/team/app/-/commit/bbb"}}]`))
		case "/api/v4/projects/team%2Fapp/repository/commits":
			_, _ = w.Write([]byte(`[{"id":"0123456789abcdef","title":"Fix","author_name":"Fil","committed_date":"2026-11-01T00:00:00Z","web_url":"https://gitlab.example.com/team/app/-/commit/0123456789abcdef"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	t.Setenv("GITLAB_SELF_TOKEN", "self")
	s := New(Config{GitLabHosts: []string{"gitlab.example.com"}, GitLabAPI: api.URL})
	ctx := context.Background()

	got := s.GitLabPick(ctx, PickRequest{List: PickProjects, Query: "ap"})
	items := opt(got, "items").([]any)
	if opt(got, "state") != "ready" || len(items) != 1 || opt(items[0], "path") != "team/app" || opt(items[0], "activity") != "2026-11-10T10:00:00Z" {
		t.Errorf("projects %s", jsvalue.Stringify(got, ""))
	}

	got = s.GitLabPick(ctx, PickRequest{List: PickMRs, Project: "team/app", Filter: PickReview})
	items = opt(got, "items").([]any)
	if len(items) != 1 || fmt.Sprint(opt(items[0], "iid")) != "5" || opt(items[0], "author") != "Fil" ||
		opt(items[0], "draft") != true || opt(items[0], "source") != "feat" {
		t.Errorf("mrs %s", jsvalue.Stringify(got, ""))
	}
	last := asked[len(asked)-1]
	if want := "reviewer_username=eva"; !strings.Contains(last, want) {
		t.Errorf("review filter not asked: %s", last)
	}

	// Mine across every project: GitLab's own list, with the project of each.
	got = s.GitLabPick(ctx, PickRequest{List: PickMRs, Filter: PickMine})
	items = opt(got, "items").([]any)
	if len(items) != 1 || opt(items[0], "project") != "team/sub/app" || opt(items[0], "title") != "Mine" {
		t.Errorf("mine everywhere %s", jsvalue.Stringify(got, ""))
	}
	if last := asked[len(asked)-1]; !strings.HasPrefix(last, "/api/v4/merge_requests?") || !strings.Contains(last, "author_username=eva") || !strings.Contains(last, "scope=all") {
		t.Errorf("mine everywhere asked: %s", last)
	}

	got = s.GitLabPick(ctx, PickRequest{List: PickBranches, Project: "team/app"})
	items = opt(got, "items").([]any)
	if len(items) != 2 || opt(items[0], "name") != "b" || opt(items[0], "url") != "https://gitlab.example.com/team/app/-/commit/bbb" {
		t.Errorf("branches %s", jsvalue.Stringify(got, ""))
	}

	got = s.GitLabPick(ctx, PickRequest{List: PickCommits, Project: "team/app", Ref: "b"})
	items = opt(got, "items").([]any)
	if len(items) != 1 || opt(items[0], "short") != "01234567" || opt(items[0], "title") != "Fix" {
		t.Errorf("commits %s", jsvalue.Stringify(got, ""))
	}

	// A refused token is a state, not an empty list.
	t.Setenv("GITLAB_SELF_TOKEN", "other")
	s = New(Config{GitLabHosts: []string{"gitlab.example.com"}, GitLabAPI: api.URL})
	got = s.GitLabPick(ctx, PickRequest{List: PickProjects})
	if opt(got, "state") != "failed" || opt(got, "reason") != "denied" {
		t.Errorf("denied %s", jsvalue.Stringify(got, ""))
	}
	// No GitLab at all.
	got = New(Config{}).GitLabPick(ctx, PickRequest{List: PickProjects})
	if opt(got, "state") != "off" || opt(got, "reason") != "not-configured" {
		t.Errorf("off %s", jsvalue.Stringify(got, ""))
	}
}
