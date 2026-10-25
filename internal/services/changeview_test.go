package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestGithubRefOfReadsPullsCommitsAndIssues(t *testing.T) {
	s := New(Config{})
	cases := []struct {
		in   string
		kind string
		n    int
		sha  string
	}{
		{"https://github.com/ly-solutions/harnessly/pull/12", "pr", 12, ""},
		{"https://github.com/ly-solutions/harnessly/pull/12/files#diff-1", "pr", 12, ""},
		{"https://github.com/ly-solutions/harnessly/pull/12/commits/ABCDEF1", "pr", 12, ""},
		{"https://github.com/ly-solutions/harnessly/issues/7", "issue", 7, ""},
		{"https://github.com/ly-solutions/harnessly/issues/7?x=1", "issue", 7, ""},
		{"https://github.com/ly-solutions/harnessly/commit/0123ABC", "commit", 0, "0123abc"},
		{"https://GitHub.com/ly-solutions/harnessly/commits/0123abc/", "commit", 0, "0123abc"},
	}
	for _, c := range cases {
		r := s.githubRefOf(c.in)
		if r == nil || r.kind != c.kind || r.number != c.n || r.sha != c.sha || r.owner != "ly-solutions" || r.repo != "harnessly" {
			t.Errorf("%s → %+v", c.in, r)
		}
	}
	for _, in := range []string{
		"https://github.com/ly-solutions/harnessly", "https://github.com/ly-solutions/harnessly/pull/x",
		"https://github.com/ly-solutions/harnessly/pull/0", "https://github.com/ly-solutions/harnessly/issues/7/x",
		"https://github.com/ly-solutions/harnessly/commit/zzzzzzz", "http://github.com/a/b/pull/1",
		"https://github.com:8443/a/b/pull/1", "https://u@github.com/a/b/pull/1", "https://gitlab.com/a/b/pull/1", "",
	} {
		if r := s.githubRefOf(in); r != nil {
			t.Errorf("%q read as %+v", in, r)
		}
	}
	if r := New(Config{GitHubHost: "git.corp.example"}).githubRefOf("https://git.corp.example/a/b/pull/3"); r == nil {
		t.Error("the deployment's own GitHub is not read")
	}
}

type fakeGithub struct {
	mu    sync.Mutex
	asked []string
	auth  []string
	rate  bool
}

func (f *fakeGithub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.asked = append(f.asked, r.Method+" "+r.URL.RequestURI())
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	rate := f.rate
	f.mu.Unlock()
	if rate {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/repos/o/r/pulls/12":
		_, _ = w.Write([]byte(`{"number":12,"title":"Add the view","body":"## Why\nbecause","state":"open","merged":false,"draft":true,
			"html_url":"https://github.com/o/r/pull/12","user":{"login":"anna"},"updated_at":"2026-10-20T10:00:00Z","changed_files":3,
			"head":{"ref":"feat/view","sha":"deadbeef"},"base":{"ref":"main"},"labels":[{"name":"bug"},{"name":"ui"}]}`))
	case "/repos/o/r/pulls/13":
		_, _ = w.Write([]byte(`{"number":13,"title":"Done","state":"closed","merged":true,"merged_at":"2026-10-20T10:00:00Z","html_url":"https://github.com/o/r/pull/13","user":{"login":"bo"},"head":{"ref":"x","sha":"cafe"},"base":{"ref":"main"}}`))
	case "/repos/o/r/pulls/12/files":
		_, _ = w.Write([]byte(`[{"filename":"a.txt","status":"modified","patch":"@@ -1 +1 @@\n-a\n+b","changes":2},
			{"filename":"new.txt","status":"added","patch":"@@ -0,0 +1 @@\n+new","changes":1},
			{"filename":"img.png","previous_filename":"old.png","status":"renamed","changes":4}]`))
	case "/repos/o/r/issues/12/comments":
		_, _ = w.Write([]byte(`[{"id":1,"user":{"login":"bo"},"created_at":"2026-10-20T09:00:00Z","body":"looks fine"}]`))
	case "/repos/o/r/pulls/12/comments":
		_, _ = w.Write([]byte(`[{"id":10,"user":{"login":"cy"},"created_at":"2026-10-20T08:00:00Z","body":"rename this","path":"a.txt","line":1},
			{"id":11,"in_reply_to_id":10,"user":{"login":"anna"},"created_at":"2026-10-20T08:30:00Z","body":"done","path":"a.txt","line":1},
			{"id":12,"user":{"login":"cy"},"created_at":"2026-10-20T08:40:00Z","body":"old","path":"b.txt","line":null,"original_line":5}]`))
	case "/repos/o/r/pulls/12/reviews":
		_, _ = w.Write([]byte(`[{"id":20,"user":{"login":"cy"},"submitted_at":"2026-10-20T07:00:00Z","body":"needs work","state":"CHANGES_REQUESTED"},{"id":21,"user":{"login":"cy"},"submitted_at":"2026-10-20T07:10:00Z","body":""}]`))
	case "/graphql":
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":true,"comments":{"nodes":[{"databaseId":10}]}},{"isResolved":false,"comments":{"nodes":[{"databaseId":12}]}}]}}}}}`))
	case "/repos/o/r/commits/deadbeef/check-runs", "/repos/o/r/commits/c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00/check-runs":
		_, _ = w.Write([]byte(`{"check_runs":[
			{"name":"build","status":"completed","conclusion":"success","html_url":"https://github.com/o/r/actions/runs/1/job/1","started_at":"2026-10-20T10:00:00Z","completed_at":"2026-10-20T10:01:30Z","check_suite":{"id":77},"app":{"name":"GitHub Actions"}},
			{"name":"test","status":"completed","conclusion":"failure","html_url":"https://github.com/o/r/actions/runs/1/job/2","check_suite":{"id":77},"app":{"name":"GitHub Actions"}},
			{"name":"deploy","status":"in_progress","html_url":"https://github.com/o/r/actions/runs/2/job/3","check_suite":{"id":78},"app":{"name":"GitHub Actions"}},
			{"name":"codecov","status":"completed","conclusion":"neutral","details_url":"https://codecov.io/x","app":{"name":"Codecov"}}]}`))
	case "/repos/o/r/actions/runs":
		_, _ = w.Write([]byte(`{"workflow_runs":[{"check_suite_id":77,"name":"CI","html_url":"https://github.com/o/r/actions/runs/1"},{"check_suite_id":78,"name":"Release","html_url":"https://github.com/o/r/actions/runs/2"}]}`))
	case "/repos/o/r/commits/deadbeef/status", "/repos/o/r/commits/c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00/status":
		_, _ = w.Write([]byte(`{"state":"pending","statuses":[{"context":"ci/legacy","state":"pending","target_url":"https://ci.example/1"}]}`))
	case "/repos/o/r/commits/c0ffee0":
		_, _ = w.Write([]byte(`{"sha":"c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00","html_url":"https://github.com/o/r/commit/c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00",
			"commit":{"message":"Fix the queue\n\nIt stalled when empty.","author":{"name":"Anna A","date":"2026-10-20T06:00:00Z"}},"author":{"login":"anna"},
			"files":[{"filename":"q.go","status":"modified","patch":"@@ -1 +1 @@\n-x\n+y","changes":2}]}`))
	case "/repos/o/r/commits/c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00/comments":
		_, _ = w.Write([]byte(`[{"id":30,"user":{"login":"bo"},"created_at":"2026-10-20T06:30:00Z","body":"nice","path":"q.go","line":1}]`))
	case "/repos/o/r/issues/7":
		_, _ = w.Write([]byte(`{"number":7,"title":"It crashes","body":"steps","state":"closed","html_url":"https://github.com/o/r/issues/7","user":{"login":"dan"},"labels":[{"name":"bug"}],"updated_at":"2026-10-19T10:00:00Z"}`))
	case "/repos/o/r/issues/7/comments":
		_, _ = w.Write([]byte(`[]`))
	case "/repos/o/r/issues/12":
		_, _ = w.Write([]byte(`{"number":12,"title":"x","pull_request":{"url":"x"}}`))
	default:
		http.NotFound(w, r)
	}
}

func githubService(t *testing.T, f *fakeGithub, token string) *Service {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("RUNTIME_GITHUB_TOKEN", "")
	api := httptest.NewServer(f)
	t.Cleanup(api.Close)
	cmd := []string{"true"}
	if token != "" {
		cmd = []string{"echo", token}
	}
	return New(Config{Home: t.TempDir(), GitHubAPI: api.URL, GitHubTokenCmd: cmd})
}

func view(t *testing.T, s *Service, link string) *jsvalue.Object {
	t.Helper()
	v, err := s.ChangeView(context.Background(), link)
	if err != nil || v == nil {
		t.Fatalf("%s: %v %v", link, v, err)
	}
	return v
}

func field(t *testing.T, o any, path ...string) any {
	t.Helper()
	cur := o
	for _, k := range path {
		cur = opt(cur, k)
	}
	return cur
}

func TestGithubPullView(t *testing.T) {
	f := &fakeGithub{}
	s := githubService(t, f, "sekret-token")
	v := view(t, s, "https://github.com/o/r/pull/12/files")
	checks := map[string]any{
		"provider": "github", "kind": "mr", "host": "github.com", "project": "o/r", "ref": "#12", "title": "Add the view",
		"description": "## Why\nbecause", "author": "anna", "state": "opened", "draft": true, "sourceBranch": "feat/view",
		"targetBranch": "main", "truncated": false, "url": "https://github.com/o/r/pull/12", "limited": nil,
	}
	for k, want := range checks {
		if got := opt(v, k); got != want {
			t.Errorf("%s = %#v, want %#v", k, got, want)
		}
	}
	labels := opt(v, "labels").([]any)
	if len(labels) != 2 || labels[0] != "bug" {
		t.Errorf("labels %v", labels)
	}
	diff := opt(v, "diff").(string)
	for _, want := range []string{"diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+b", "new file mode 100644", "rename from old.png\nrename to img.png"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, diff)
		}
	}
	if st := field(t, v, "pipeline", "status"); st != "failed" {
		t.Errorf("pipeline %v", st)
	}
	if u := field(t, v, "pipeline", "url"); u != "https://github.com/o/r/pull/12/checks" {
		t.Errorf("pipeline url %v", u)
	}
	stages := opt(v, "stages").([]any)
	names := []string{}
	for _, st := range stages {
		names = append(names, opt(st, "name").(string))
	}
	if strings.Join(names, ",") != "CI,Release,Codecov,Statuses" && strings.Join(names, ",") != "CI,Release,Codecov,Статусы" {
		t.Errorf("stages %v", names)
	}
	ci := stages[0]
	if opt(ci, "status") != "failed" || len(opt(ci, "jobs").([]any)) != 2 || opt(ci, "url") != "https://github.com/o/r/actions/runs/1" {
		t.Errorf("CI %v", jsvalue.Stringify(ci, ""))
	}
	build := opt(ci, "jobs").([]any)[0]
	if opt(build, "name") != "build" || opt(build, "status") != "success" || opt(build, "durationMs") != float64(90000) ||
		opt(build, "url") != "https://github.com/o/r/actions/runs/1/job/1" {
		t.Errorf("build %v", jsvalue.Stringify(build, ""))
	}
	if opt(stages[1], "status") != "running" || opt(opt(stages[2], "jobs").([]any)[0], "status") != "skipped" {
		t.Errorf("release / codecov %v", jsvalue.Stringify(v, ""))
	}
	threads := opt(v, "comments").([]any)
	if len(threads) != 4 {
		t.Fatalf("threads %d: %s", len(threads), jsvalue.Stringify(opt(v, "comments"), ""))
	}
	// Oldest first: the review text, the line thread (resolved, with its reply), the outdated one, the comment.
	if opt(threads[0], "id") != "rv-20" || opt(threads[0], "resolved") != nil {
		t.Errorf("review %v", jsvalue.Stringify(threads[0], ""))
	}
	line := threads[1]
	if opt(line, "id") != "rc-10" || opt(line, "resolved") != true || opt(line, "path") != "a.txt" || opt(line, "line") != float64(1) ||
		len(opt(line, "notes").([]any)) != 2 || opt(opt(line, "notes").([]any)[1], "author") != "anna" {
		t.Errorf("line thread %v", jsvalue.Stringify(line, ""))
	}
	if old := threads[2]; opt(old, "resolved") != false || opt(old, "outdated") != true || opt(old, "line") != float64(5) {
		t.Errorf("outdated thread %v", jsvalue.Stringify(old, ""))
	}
	if opt(threads[3], "id") != "ic-1" {
		t.Errorf("comment %v", jsvalue.Stringify(threads[3], ""))
	}

	for _, a := range f.auth {
		if a != "Bearer sekret-token" {
			t.Errorf("authorization %q", a)
		}
	}
	if out := string(jsvalue.Stringify(v, "")); strings.Contains(out, "sekret-token") {
		t.Error("the token is in the answer")
	}

	// A second look within the cache's time asks nothing.
	f.mu.Lock()
	before := len(f.asked)
	f.mu.Unlock()
	view(t, s, "https://github.com/o/r/pull/12")
	f.mu.Lock()
	after := len(f.asked)
	f.mu.Unlock()
	if after != before {
		t.Errorf("a repeated view asked GitHub %d times again", after-before)
	}
}

func TestGithubMergedPull(t *testing.T) {
	s := githubService(t, &fakeGithub{}, "tok")
	v := view(t, s, "https://github.com/o/r/pull/13")
	if opt(v, "state") != "merged" || opt(v, "diff") != "" || opt(v, "truncated") != true {
		t.Errorf("%v", jsvalue.Stringify(v, ""))
	}
	if opt(v, "stages") != nil || opt(v, "pipeline") != nil {
		t.Errorf("checks of a pull without any: %v %v", opt(v, "stages"), opt(v, "pipeline"))
	}
}

func TestGithubCommitView(t *testing.T) {
	f := &fakeGithub{}
	s := githubService(t, f, "tok")
	v := view(t, s, "https://github.com/o/r/commit/c0ffee0")
	if opt(v, "kind") != "commit" || opt(v, "ref") != "c0ffee00" || opt(v, "title") != "Fix the queue" || opt(v, "description") != "It stalled when empty." ||
		opt(v, "author") != "anna" || opt(v, "state") != nil || !strings.Contains(opt(v, "diff").(string), "+++ b/q.go") {
		t.Errorf("%v", jsvalue.Stringify(v, ""))
	}
	if opt(opt(v, "pipeline"), "status") != "failed" {
		t.Errorf("pipeline %v", opt(v, "pipeline"))
	}
	if th := opt(v, "comments").([]any); len(th) != 1 || opt(th[0], "path") != "q.go" {
		t.Errorf("comments %v", opt(v, "comments"))
	}
}

func TestGithubIssueView(t *testing.T) {
	s := githubService(t, &fakeGithub{}, "tok")
	v := view(t, s, "https://github.com/o/r/issues/7")
	if opt(v, "kind") != "issue" || opt(v, "title") != "It crashes" || opt(v, "state") != "closed" || opt(v, "author") != "dan" || opt(v, "diff") != "" ||
		opt(v, "pipeline") != nil || len(opt(v, "comments").([]any)) != 0 || opt(v, "labels").([]any)[0] != "bug" {
		t.Errorf("%v", jsvalue.Stringify(v, ""))
	}
	// A link to /issues/12 that is a pull request shows the pull request.
	if p := view(t, s, "https://github.com/o/r/issues/12"); opt(p, "kind") != "mr" || opt(p, "title") != "Add the view" {
		t.Errorf("%v", jsvalue.Stringify(p, ""))
	}
}

func TestGithubCardsExplainWhyThereIsNoView(t *testing.T) {
	// Without a token nothing is asked.
	f := &fakeGithub{}
	s := githubService(t, f, "")
	v := view(t, s, "https://github.com/o/r/pull/12")
	if opt(v, "limited") != "no-token" || opt(v, "kind") != "mr" || opt(v, "ref") != "#12" || opt(v, "project") != "o/r" ||
		opt(v, "url") != "https://github.com/o/r/pull/12" || opt(v, "title") != "" || opt(v, "diff") != "" {
		t.Errorf("%v", jsvalue.Stringify(v, ""))
	}
	if len(f.asked) != 0 {
		t.Errorf("asked GitHub without a token: %v", f.asked)
	}
	if c := view(t, s, "https://github.com/o/r/commit/0123abcdef"); opt(c, "kind") != "commit" || opt(c, "ref") != "0123abcd" || opt(c, "limited") != "no-token" {
		t.Errorf("%v", jsvalue.Stringify(c, ""))
	}
	if i := view(t, s, "https://github.com/o/r/issues/3"); opt(i, "kind") != "issue" || opt(i, "ref") != "#3" {
		t.Errorf("%v", jsvalue.Stringify(i, ""))
	}

	// A private repository: GitHub answers 404 to a token that cannot see it.
	s = githubService(t, &fakeGithub{}, "tok")
	if v := view(t, s, "https://github.com/o/private/pull/1"); opt(v, "limited") != "not-found" || opt(v, "title") != "" {
		t.Errorf("%v", jsvalue.Stringify(v, ""))
	}

	// A spent allowance: a card, and GitHub is not asked again until it is renewed.
	f = &fakeGithub{rate: true}
	s = githubService(t, f, "tok")
	if v := view(t, s, "https://github.com/o/r/pull/12"); opt(v, "limited") != "rate-limit" && opt(v, "limited") != "not-found" {
		t.Errorf("%v", jsvalue.Stringify(v, ""))
	}
	asked := len(f.asked)
	if v := view(t, s, "https://github.com/o/r/pull/99"); opt(v, "limited") != "rate-limit" {
		t.Errorf("after the limit: %v", jsvalue.Stringify(v, ""))
	}
	if len(f.asked) != asked {
		t.Errorf("asked again while the allowance was spent: %v", f.asked[asked:])
	}
}

func TestChangeViewServesGitlabWithJobsAndDiscussions(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/team%2Fapp":
			_, _ = w.Write([]byte(`{"path_with_namespace":"team/app"}`))
		case "/api/v4/projects/team%2Fapp/merge_requests/5":
			_, _ = w.Write([]byte(`{"iid":5,"title":"Queue","state":"opened","web_url":"https://gitlab.example.com/team/app/-/merge_requests/5","labels":["backend","urgent"],
				"head_pipeline":{"id":99,"status":"running","web_url":"https://gitlab.example.com/team/app/-/pipelines/99"},"changes_count":"0"}`))
		case "/api/v4/projects/team%2Fapp/merge_requests/5/diffs":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v4/projects/team%2Fapp/pipelines/99/jobs":
			_, _ = w.Write([]byte(`[{"id":3,"name":"deploy","stage":"deploy","status":"manual","web_url":"https://gitlab.example.com/j/3","duration":null},
				{"id":2,"name":"unit","stage":"test","status":"failed","web_url":"https://gitlab.example.com/j/2","duration":12.5},
				{"id":1,"name":"compile","stage":"build","status":"success","web_url":"https://gitlab.example.com/j/1","duration":3}]`))
		case "/api/v4/projects/team%2Fapp/merge_requests/5/discussions":
			_, _ = w.Write([]byte(`[{"id":"d1","notes":[{"system":true,"body":"assigned"}]},
				{"id":"d2","individual_note":true,"notes":[{"author":{"name":"Eva"},"created_at":"2026-10-20T10:00:00Z","body":"ok","resolvable":false}]},
				{"id":"d3","notes":[{"author":{"name":"Fil"},"created_at":"2026-10-20T11:00:00Z","body":"why?","resolvable":true,"resolved":true,"position":{"new_path":"q.go","new_line":4}},
					{"author":{"name":"Eva"},"created_at":"2026-10-20T11:05:00Z","body":"because","resolvable":true,"resolved":true}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	t.Setenv("GITLAB_SELF_TOKEN", "self")
	s := New(Config{GitLabHosts: []string{"gitlab.example.com"}, GitLabAPI: api.URL})
	v := view(t, s, "https://gitlab.example.com/team/app/-/merge_requests/5")
	if opt(v, "provider") != "gitlab" || opt(v, "kind") != "mr" || opt(v, "limited") != nil || opt(v, "labels").([]any)[1] != "urgent" {
		t.Errorf("%v", jsvalue.Stringify(v, ""))
	}
	stages := opt(v, "stages").([]any)
	if len(stages) != 3 || opt(stages[0], "name") != "build" || opt(stages[1], "status") != "failed" || opt(stages[2], "name") != "deploy" ||
		opt(opt(stages[1], "jobs").([]any)[0], "durationMs") != float64(12500) || opt(opt(stages[2], "jobs").([]any)[0], "durationMs") != nil {
		t.Errorf("stages %s", jsvalue.Stringify(opt(v, "stages"), ""))
	}
	threads := opt(v, "comments").([]any)
	if len(threads) != 2 || opt(threads[0], "resolved") != nil || opt(threads[1], "resolved") != true || opt(threads[1], "path") != "q.go" ||
		opt(threads[1], "line") != float64(4) || len(opt(threads[1], "notes").([]any)) != 2 {
		t.Errorf("threads %s", jsvalue.Stringify(opt(v, "comments"), ""))
	}
	// The older route's answer is as it was.
	old, err := s.GitLabView(context.Background(), "https://gitlab.example.com/team/app/-/merge_requests/5")
	if err != nil || old.Has("provider") || old.Has("stages") {
		t.Errorf("GitLabView changed: %v %v", old, err)
	}
	// A GitLab link the token cannot read has no card: the route answers 404.
	if gone, err := s.ChangeView(context.Background(), "https://gitlab.example.com/team/app/-/merge_requests/404"); gone != nil || err != nil {
		t.Errorf("%v %v", gone, err)
	}
	if s.IsChangeRef("https://example.com/a/b/pull/1") || !s.IsChangeRef("https://github.com/a/b/pull/1") || !s.IsChangeRef("https://gitlab.example.com/team/app/-/commit/abcdef1") {
		t.Error("IsChangeRef")
	}
}
