package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const ghIssueFixture = `{"id":5001,"number":12,"title":"Fix login","body":"Steps:\n1. open","state":"open","state_reason":null,
"html_url":"https://github.com/acme/web/issues/12","repository_url":"%API%/repos/acme/web","comments":2,
"created_at":"2026-10-01T10:00:00Z","updated_at":"2026-10-02T11:30:00Z","closed_at":null,
"user":{"id":2,"login":"roma","avatar_url":"https://avatars.githubusercontent.com/u/2"},
"assignee":{"id":1,"login":"sergey","avatar_url":"https://avatars.githubusercontent.com/u/1"},
"labels":[{"name":"bug"},{"name":"priority: high"},{"name":"in progress"}],"type":{"name":"Bug"}}`

const ghPullFixture = `{"id":5002,"number":13,"title":"A PR","state":"open","html_url":"https://github.com/acme/web/pull/13",
"repository_url":"%API%/repos/acme/web","pull_request":{"url":"x"},"labels":[]}`

// ghServer is a GitHub API. apiPrefix is "" (github.com, ghe.com) or "/api/v3" (Enterprise Server); version, when
// set, is what a GHES reports in X-GitHub-Enterprise-Version.
func ghServer(t *testing.T, apiPrefix, version string, metaOpen bool) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := strings.CutPrefix(r.URL.Path, apiPrefix)
		if !ok {
			http.NotFound(w, r)
			return
		}
		seen = append(seen, path+"?"+r.URL.RawQuery+"|"+r.Header.Get("X-GitHub-Api-Version"))
		if version != "" {
			w.Header().Set("X-GitHub-Enterprise-Version", version)
		}
		api := srv.URL + apiPrefix
		fix := func(s string) string { return strings.ReplaceAll(s, "%API%", api) }
		authed := r.Header.Get("Authorization") == "Bearer ghp_good"
		switch {
		case path == "/meta" && metaOpen:
			provJSON(w, 200, `{"verifiable_password_authentication":false,"installed_version":"`+version+`","domains":{}}`)
		case path == "" || path == "/":
			if authed {
				provJSON(w, 200, `{"current_user_url":"x"}`)
			} else {
				provJSON(w, 401, `{"message":"Must authenticate to access this API.","documentation_url":"https://docs.github.com/enterprise-server@3.14/rest"}`)
			}
		case !authed:
			provJSON(w, 401, `{"message":"Bad credentials"}`)
		case path == "/user":
			provJSON(w, 200, `{"id":1,"login":"sergey","name":"Sergey","email":"s@x.io","avatar_url":"https://avatars.githubusercontent.com/u/1"}`)
		case path == "/user/repos":
			provJSON(w, 200, `[{"full_name":"acme/web","html_url":"https://github.com/acme/web","archived":false},{"full_name":"acme/old","html_url":"x","archived":true}]`)
		case path == "/search/issues":
			provJSON(w, 200, `{"total_count":2,"items":[`+fix(ghIssueFixture)+`,`+fix(ghPullFixture)+`]}`)
		case path == "/repos/acme/web/issues/12":
			provJSON(w, 200, fix(ghIssueFixture))
		case path == "/repos/acme/web/issues/13":
			provJSON(w, 200, fix(ghPullFixture))
		case path == "/repos/acme/web/issues/12/parent":
			provJSON(w, 200, `{"number":1,"title":"Epic","html_url":"https://github.com/acme/web/issues/1"}`)
		case path == "/repos/acme/web/issues/12/comments":
			provJSON(w, 200, `[{"id":1,"body":"first","created_at":"2026-10-01T12:00:00Z","user":{"login":"roma","id":2}},{"id":2,"body":"second","created_at":"2026-10-02T09:00:00Z","user":{"login":"sergey","id":1}}]`)
		default:
			provJSON(w, 404, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func ghDetect(t *testing.T, h *HTTP, raw string) *Detection {
	t.Helper()
	u, err := Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	d, err := gitHubProvider{}.Detect(context.Background(), NewProber(h), u)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGitHubDetectGitHubCom(t *testing.T) {
	d := ghDetect(t, NewHTTP(nil), "https://github.com/acme/web/issues/12")
	if d == nil || d.Deployment != Cloud || d.BaseURL != "https://github.com" || d.APIURL != "https://api.github.com" {
		t.Fatalf("detection = %+v", d)
	}
	if m := d.Methods[0]; m.ID != "pat" || m.TokenURL != "https://github.com/settings/tokens/new?description=harnessly&scopes=repo,read:user" {
		t.Fatalf("method = %+v", m)
	}
	if len(d.Capabilities) != 2 || d.Capabilities[1] != Code {
		t.Fatalf("capabilities = %v", d.Capabilities)
	}
}

func TestGitHubDetectDataResidency(t *testing.T) {
	for _, link := range []string{"https://acme.ghe.com/acme/web/issues/1", "https://api.acme.ghe.com/user"} {
		d := ghDetect(t, NewHTTP(nil), link)
		if d == nil || d.Deployment != Cloud || d.BaseURL != "https://acme.ghe.com" || d.APIURL != "https://api.acme.ghe.com" || d.Edition != "ghe.com" {
			t.Fatalf("%s: %+v", link, d)
		}
	}
}

func TestGitHubDetectEnterpriseServer(t *testing.T) {
	cases := []struct {
		name     string
		metaOpen bool
	}{{"meta open", true}, {"private mode, header only", false}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := ghServer(t, "/api/v3", "3.14.2", c.metaOpen)
			d := ghDetect(t, NewHTTP(nil), srv.URL+"/acme/web/issues/12")
			if d == nil || d.Deployment != SelfHosted || d.BaseURL != srv.URL || d.APIURL != srv.URL+"/api/v3" || d.Version != "3.14.2" || d.Edition != "server" {
				t.Fatalf("detection = %+v", d)
			}
			if d.Methods[0].TokenURL != srv.URL+"/settings/tokens/new?description=harnessly&scopes=repo,read:user" {
				t.Fatalf("token url = %s", d.Methods[0].TokenURL)
			}
		})
	}
}

func TestGitHubDetectIsExclusive(t *testing.T) {
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<title>GitHub clone</title>"))
	}))
	defer html.Close()
	// Looks like the GitHub error body, but sends no enterprise header.
	json401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provJSON(w, 401, `{"message":"Bad credentials"}`)
	}))
	defer json401.Close()
	for _, s := range []*httptest.Server{html, json401} {
		if d := ghDetect(t, NewHTTP(nil), s.URL+"/acme/web"); d != nil {
			t.Fatalf("%s claimed as GitHub: %+v", s.URL, d)
		}
	}
}

func ghOpen(t *testing.T, srv *httptest.Server, conn Conn, token string) Client {
	t.Helper()
	return gitHubProvider{}.Open(conn, Credentials{FieldToken: token}, NewHTTP(nil))
}

func TestGitHubMeAndHeaders(t *testing.T) {
	// GHES: no API version header.
	srv, seen := ghServer(t, "/api/v3", "3.14.2", true)
	conn := Conn{Kind: GitHub, BaseURL: srv.URL, APIURL: srv.URL + "/api/v3", Deployment: SelfHosted}
	me, err := ghOpen(t, srv, conn, "ghp_good").Me(context.Background())
	if err != nil || me.Login != "sergey" || me.Name != "Sergey" || me.Email != "s@x.io" || me.ID != "1" {
		t.Fatalf("me = %+v, %v", me, err)
	}
	if got := (*seen)[len(*seen)-1]; !strings.HasSuffix(got, "|") {
		t.Errorf("GHES must not send an API version: %s", got)
	}
	if _, err := ghOpen(t, srv, conn, "ghp_bad").Me(context.Background()); Reason(err) != "denied" {
		t.Fatalf("bad token: %v", err)
	}
	// GitHub.com sends it.
	srv2, seen2 := ghServer(t, "", "", false)
	conn2 := Conn{Kind: GitHub, BaseURL: "https://github.com", APIURL: srv2.URL, Deployment: Cloud}
	if _, err := ghOpen(t, srv2, conn2, "ghp_good").Me(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := (*seen2)[0]; !strings.HasSuffix(got, "|2022-11-28") {
		t.Errorf("cloud request: %s", got)
	}
}

func TestGitHubQuery(t *testing.T) {
	cases := []struct {
		q    Query
		want string
	}{
		{Query{}, "is:issue"},
		{Query{Mine: true, Open: true}, "is:issue assignee:@me state:open"},
		{Query{Project: "acme/web", Text: "login bug"}, "is:issue repo:acme/web login bug"},
		{Query{Project: "acme", Mine: true}, "is:issue assignee:@me user:acme"},
		{Query{Native: "label:bug author:roma"}, "is:issue label:bug author:roma"},
		{Query{Native: "is:pr is:open"}, "is:pr is:open"},
	}
	for _, c := range cases {
		if got := ghQuery(c.q); got != c.want {
			t.Errorf("ghQuery(%+v) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestGitHubSearchSkipsPullRequests(t *testing.T) {
	srv, seen := ghServer(t, "", "", false)
	conn := Conn{Kind: GitHub, BaseURL: "https://github.com", APIURL: srv.URL, Deployment: Cloud}
	list, err := ghOpen(t, srv, conn, "ghp_good").(IssueSource).Search(context.Background(), Query{Mine: true, Open: true, Limit: 15})
	if err != nil || len(list) != 1 {
		t.Fatalf("search = %+v, %v", list, err)
	}
	req := (*seen)[0]
	for _, want := range []string{"q=is%3Aissue+assignee%3A%40me+state%3Aopen", "sort=updated", "order=desc", "per_page=15"} {
		if !strings.Contains(req, want) {
			t.Errorf("%s lacks %s", req, want)
		}
	}
	i := list[0]
	if i.Key != "acme/web#12" || i.Project != "acme/web" || i.Title != "Fix login" || i.URL != "https://github.com/acme/web/issues/12" {
		t.Fatalf("issue = %+v", i)
	}
	if i.Status != (Status{Name: "in progress", Category: Doing}) || i.Priority != "high" || i.Type != "Bug" || len(i.Labels) != 3 {
		t.Fatalf("status/labels = %+v", i)
	}
	if i.Assignee == nil || i.Assignee.Login != "sergey" || i.Reporter.Login != "roma" || i.Updated.IsZero() {
		t.Fatalf("people/time = %+v", i)
	}
}

func TestGitHubIssue(t *testing.T) {
	srv, _ := ghServer(t, "/api/v3", "3.14.2", true)
	conn := Conn{Kind: GitHub, BaseURL: srv.URL, APIURL: srv.URL + "/api/v3", Deployment: SelfHosted}
	c := ghOpen(t, srv, conn, "ghp_good").(IssueSource)
	i, err := c.Issue(context.Background(), "acme/web#12")
	if err != nil {
		t.Fatal(err)
	}
	if i.Key != "acme/web#12" || i.Description == "" || i.Parent == nil || i.Parent.Key != "acme/web#1" || i.Parent.Title != "Epic" {
		t.Fatalf("issue = %+v", i)
	}
	if len(i.Comments) != 2 || i.Comments[0].Text != "first" || i.Comments[1].Author.Login != "sergey" {
		t.Fatalf("comments = %+v", i.Comments)
	}
	for _, key := range []string{"acme/web#13", "acme/web#99", "bad", "acme#1", "a/b/c#1", "acme/web#x"} {
		if _, err := c.Issue(context.Background(), key); Reason(err) != "not-found" {
			t.Errorf("%s: %v", key, err)
		}
	}
}

func TestGitHubClosedIssue(t *testing.T) {
	i := ghMap(&ghIssue{Number: 4, State: "closed", StateReason: "not_planned", RepositoryURL: "https://api.github.com/repos/a/b", Labels: []struct {
		Name string `json:"name"`
	}{{Name: "in progress"}}})
	if i.Status != (Status{Name: "not planned", Category: Done}) || i.Key != "a/b#4" {
		t.Fatalf("issue = %+v", i)
	}
}

func TestGitHubProjects(t *testing.T) {
	srv, seen := ghServer(t, "", "", false)
	conn := Conn{Kind: GitHub, BaseURL: "https://github.com", APIURL: srv.URL, Deployment: Cloud}
	list, err := ghOpen(t, srv, conn, "ghp_good").(IssueSource).Projects(context.Background())
	if err != nil || len(list) != 1 || list[0].Key != "acme/web" {
		t.Fatalf("projects = %+v, %v", list, err)
	}
	if !strings.Contains((*seen)[0], "sort=updated") || !strings.Contains((*seen)[0], "per_page=100") {
		t.Errorf("request: %s", (*seen)[0])
	}
}

func TestGitHubLink(t *testing.T) {
	cases := []struct {
		base, link, key string
	}{
		{"https://github.com", "https://github.com/acme/web/issues/12", "acme/web#12"},
		{"https://github.com", "https://www.github.com/acme/web/issues/12#issuecomment-1", "acme/web#12"},
		{"https://git.corp.io", "https://git.corp.io/acme/web/issues/7", "acme/web#7"},
		{"https://acme.ghe.com", "https://acme.ghe.com/o/r/issues/3", "o/r#3"},
		{"https://github.com", "https://github.com/acme/web/pull/12", ""},
		{"https://github.com", "https://github.com/acme/web/issues/new", ""},
		{"https://github.com", "https://gitlab.com/acme/web/issues/12", ""},
	}
	for _, c := range cases {
		u, _ := url.Parse(c.link)
		got, ok := gitHubProvider{}.Link(Conn{BaseURL: c.base}, u)
		if (c.key == "") == ok || (ok && (got.Key != c.key || got.Kind != "issue")) {
			t.Errorf("%s: %v %v, want %q", c.link, got, ok, c.key)
		}
	}
}
