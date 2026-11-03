package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const glIssueFixture = `{"id":901,"iid":12,"project_id":7,"title":"Fix login","description":"Steps:\n1. open","state":"opened",
"labels":["bug","priority::high","status::In Progress"],"web_url":"%BASE%/acme/web/app/-/issues/12","issue_type":"issue",
"created_at":"2026-10-01T10:00:00.000Z","updated_at":"2026-10-02T11:30:00.000+03:00","closed_at":null,
"author":{"id":2,"username":"roma","name":"Roma","avatar_url":"https://secure.gravatar.com/avatar/r"},
"assignees":[{"id":1,"username":"sergey","name":"Sergey","avatar_url":"/uploads/-/system/user/avatar/1/a.png"}],
"assignee":{"id":1,"username":"sergey","name":"Sergey","avatar_url":"/uploads/-/system/user/avatar/1/a.png"},
"references":{"short":"#12","relative":"#12","full":"acme/web/app#12"}}`

// glServer is a GitLab at prefix. anonymousVersion opens /api/v4/version; manifestOnly leaves only the manifest.
func glServer(t *testing.T, prefix string, anonymousVersion bool) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := strings.CutPrefix(r.URL.EscapedPath(), prefix)
		if !ok {
			// GitLab sends its header on 404s too: a wrong root must not be taken for it.
			w.Header().Set("X-Gitlab-Meta", `{"correlation_id":"x","version":"1"}`)
			http.NotFound(w, r)
			return
		}
		seen = append(seen, path+"?"+r.URL.RawQuery)
		w.Header().Set("X-Gitlab-Meta", `{"correlation_id":"x","version":"1"}`)
		authed := r.Header.Get("PRIVATE-TOKEN") == "glpat-good"
		body := strings.ReplaceAll(glIssueFixture, "%BASE%", srv.URL+prefix)
		switch {
		case path == "/api/v4/version" && authed, path == "/api/v4/version" && anonymousVersion:
			provJSON(w, 200, `{"version":"17.5.2-ee","revision":"abc123","enterprise":true}`)
		case strings.HasPrefix(path, "/api/v4/") && !authed:
			provJSON(w, 401, `{"message":"401 Unauthorized"}`)
		case path == "/api/v4/user":
			provJSON(w, 200, `{"id":1,"username":"sergey","name":"Sergey","email":"s@x.io","avatar_url":"https://gitlab.example/a.png"}`)
		case path == "/api/v4/projects":
			provJSON(w, 200, `[{"id":7,"path_with_namespace":"acme/web/app","name_with_namespace":"Acme / Web / App","web_url":"`+srv.URL+prefix+`/acme/web/app"}]`)
		case path == "/api/v4/issues", path == "/api/v4/projects/acme%2Fweb%2Fapp/issues":
			provJSON(w, 200, "["+body+"]")
		case path == "/api/v4/projects/acme%2Fweb%2Fapp/issues/12":
			provJSON(w, 200, body)
		case path == "/api/v4/projects/acme%2Fweb%2Fapp/issues/12/notes":
			provJSON(w, 200, `[{"id":3,"body":"second","system":false,"created_at":"2026-10-02T09:00:00Z","author":{"id":1,"username":"sergey","name":"Sergey"}},
{"id":2,"body":"assigned to @sergey","system":true,"created_at":"2026-10-01T13:00:00Z","author":{"id":2,"username":"roma","name":"Roma"}},
{"id":1,"body":"first","system":false,"created_at":"2026-10-01T12:00:00Z","author":{"id":2,"username":"roma","name":"Roma"}}]`)
		case path == "/-/manifest.json":
			provJSON(w, 200, `{"name":"GitLab","short_name":"GitLab","start_url":"/"}`)
		case path == "/users/sign_in":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><meta content="GitLab" property="og:site_name"></head></html>`))
		default:
			provJSON(w, 404, `{"message":"404 Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func glDetect(t *testing.T, h *HTTP, raw string) *Detection {
	t.Helper()
	u, err := Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	d, err := gitLabProvider{}.Detect(context.Background(), NewProber(h), u)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGitLabDetectGitLabCom(t *testing.T) {
	d := glDetect(t, NewHTTP(nil), "https://gitlab.com/acme/web/app/-/issues/12")
	if d == nil || d.Deployment != Cloud || d.BaseURL != "https://gitlab.com" {
		t.Fatalf("detection = %+v", d)
	}
	if !strings.HasPrefix(d.Methods[0].TokenURL, "https://gitlab.com/-/user_settings/personal_access_tokens?name=harnessly&scopes=read_api") ||
		len(d.Capabilities) != 2 || d.Capabilities[0] != Issues || d.Capabilities[1] != Code {
		t.Fatalf("methods/capabilities = %+v %+v", d.Methods, d.Capabilities)
	}
}

func TestGitLabDetectSelfManaged(t *testing.T) {
	cases := []struct {
		name, prefix, link string
		anonVersion        bool
		version, edition   string
	}{
		{"root, 401 on version", "", "/acme/web/app/-/issues/12", false, "", ""},
		{"root, version open", "", "/", true, "17.5.2-ee", "ee"},
		{"subpath", "/gitlab", "/gitlab/acme/web/app/-/merge_requests/3", false, "", ""},
		{"subpath, bare", "/gitlab", "/gitlab", false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := glServer(t, c.prefix, c.anonVersion)
			d := glDetect(t, NewHTTP(nil), srv.URL+c.link)
			if d == nil || d.Deployment != SelfHosted || d.BaseURL != srv.URL+c.prefix || d.Version != c.version || d.Edition != c.edition {
				t.Fatalf("detection = %+v", d)
			}
		})
	}
}

func TestGitLabDetectByManifestAndSignIn(t *testing.T) {
	// A proxy hides the API: the manifest alone is enough, and without it the sign-in page's markers.
	page := map[string]string{
		"manifest": `/-/manifest.json`,
		"sign-in":  `/users/sign_in`,
	}
	for name, open := range page {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path != "/gl"+open:
					http.Error(w, "blocked", http.StatusForbidden)
				case open == "/-/manifest.json":
					provJSON(w, 200, `{"name":"GitLab"}`)
				default:
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(`<script>gon.gitlab_url = "https://git.example.com"</script>`))
				}
			}))
			defer srv.Close()
			d := glDetect(t, NewHTTP(nil), srv.URL+"/gl/x/y/-/issues/1")
			if d == nil || d.BaseURL != srv.URL+"/gl" || d.Version != "" {
				t.Fatalf("detection = %+v", d)
			}
		})
	}
}

func TestGitLabDetectIsExclusive(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>Sign in</title></html>"))
	}))
	defer plain.Close()
	// A 401 JSON with the same message but no GitLab header.
	lookalike := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provJSON(w, 401, `{"message":"401 Unauthorized"}`)
	}))
	defer lookalike.Close()
	for _, s := range []*httptest.Server{plain, lookalike} {
		if d := glDetect(t, NewHTTP(nil), s.URL+"/a/b/-/issues/1"); d != nil {
			t.Fatalf("%s claimed as GitLab: %+v", s.URL, d)
		}
	}
}

func TestGitLabTokenURLByVersion(t *testing.T) {
	if got := glTokenURL("https://g.io", "15.11.3"); !strings.Contains(got, "/-/profile/personal_access_tokens") {
		t.Errorf("15.x: %s", got)
	}
	if got := glTokenURL("https://g.io", "17.0.1-ee"); !strings.Contains(got, "/-/user_settings/personal_access_tokens") {
		t.Errorf("17.x: %s", got)
	}
}

func glOpen(t *testing.T, srv *httptest.Server, prefix, token string) Client {
	t.Helper()
	conn := Conn{Kind: GitLab, BaseURL: srv.URL + prefix, Deployment: SelfHosted}
	return gitLabProvider{}.Open(conn, Credentials{FieldToken: token}, NewHTTP(nil))
}

func TestGitLabMe(t *testing.T) {
	srv, _ := glServer(t, "/gitlab", false)
	me, err := glOpen(t, srv, "/gitlab", "glpat-good").Me(context.Background())
	if err != nil || me.Login != "sergey" || me.Email != "s@x.io" || me.ID != "1" {
		t.Fatalf("me = %+v, %v", me, err)
	}
	if _, err := glOpen(t, srv, "/gitlab", "bad").Me(context.Background()); Reason(err) != "denied" {
		t.Fatalf("bad token: %v", err)
	}
}

func TestGitLabSearchQuery(t *testing.T) {
	srv, seen := glServer(t, "", false)
	c := glOpen(t, srv, "", "glpat-good").(IssueSource)
	if _, err := c.Search(context.Background(), Query{Mine: true, Open: true, Text: "login bug", Limit: 20}); err != nil {
		t.Fatal(err)
	}
	last := (*seen)[len(*seen)-1]
	for _, want := range []string{"/api/v4/issues?", "scope=assigned_to_me", "state=opened", "search=login+bug", "order_by=updated_at", "per_page=20"} {
		if !strings.Contains(last, want) {
			t.Errorf("%s lacks %s", last, want)
		}
	}
	if _, err := c.Search(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
	if last := (*seen)[len(*seen)-1]; !strings.Contains(last, "scope=all") || !strings.Contains(last, "state=all") {
		t.Errorf("plain list: %s", last)
	}
	list, err := c.Search(context.Background(), Query{Project: "acme/web/app", Open: true})
	if err != nil || len(list) != 1 {
		t.Fatalf("project search = %v, %v", list, err)
	}
	if last := (*seen)[len(*seen)-1]; !strings.HasPrefix(last, "/api/v4/projects/acme%2Fweb%2Fapp/issues?") || strings.Contains(last, "scope=all") {
		t.Errorf("project path: %s", last)
	}
}

func TestGitLabIssueMapping(t *testing.T) {
	srv, _ := glServer(t, "/gitlab", false)
	c := glOpen(t, srv, "/gitlab", "glpat-good").(IssueSource)
	i, err := c.Issue(context.Background(), "acme/web/app#12")
	if err != nil {
		t.Fatal(err)
	}
	if i.Key != "acme/web/app#12" || i.Title != "Fix login" || i.Project != "acme/web/app" || i.URL != srv.URL+"/gitlab/acme/web/app/-/issues/12" {
		t.Fatalf("issue = %+v", i)
	}
	if i.Status != (Status{Name: "In Progress", Category: Doing}) || i.Priority != "high" || i.Type != "issue" || len(i.Labels) != 3 {
		t.Fatalf("status/labels: %+v", i)
	}
	if i.Assignee == nil || i.Assignee.Login != "sergey" || i.Assignee.Avatar != srv.URL+"/gitlab/uploads/-/system/user/avatar/1/a.png" || i.Reporter.Name != "Roma" {
		t.Fatalf("people = %+v / %+v", i.Assignee, i.Reporter)
	}
	if len(i.Comments) != 2 || i.Comments[0].Text != "first" || i.Comments[1].Text != "second" {
		t.Fatalf("comments (system notes skipped, oldest first) = %+v", i.Comments)
	}
	for _, bad := range []string{"nohash", "a/b#x", "#12", "a/b#0"} {
		if _, err := c.Issue(context.Background(), bad); Reason(err) != "not-found" {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := c.Issue(context.Background(), "acme/web/app#99"); Reason(err) != "not-found" {
		t.Errorf("missing: %v", err)
	}
}

func TestGitLabClosedIssueIsDone(t *testing.T) {
	c := &glClient{base: "https://g.io"}
	i := c.issue(&glIssue{IID: 3, State: "closed", Labels: []string{"doing"}, References: struct {
		Full string `json:"full"`
	}{Full: "a/b#3"}})
	if i.Status.Category != Done || i.Key != "a/b#3" {
		t.Fatalf("issue = %+v", i)
	}
}

func TestGitLabProjects(t *testing.T) {
	srv, seen := glServer(t, "", false)
	list, err := glOpen(t, srv, "", "glpat-good").(IssueSource).Projects(context.Background())
	if err != nil || len(list) != 1 || list[0].Key != "acme/web/app" || list[0].Name != "Acme / Web / App" {
		t.Fatalf("projects = %+v, %v", list, err)
	}
	if last := (*seen)[len(*seen)-1]; !strings.Contains(last, "membership=true") || !strings.Contains(last, "order_by=last_activity_at") || !strings.Contains(last, "simple=true") {
		t.Errorf("request: %s", last)
	}
}

func TestGitLabLink(t *testing.T) {
	cases := []struct {
		base, link, key string
	}{
		{"https://gitlab.com", "https://gitlab.com/acme/web/app/-/issues/12", "acme/web/app#12"},
		{"https://gitlab.com", "https://gitlab.com/acme/web/app/-/work_items/12?x=1", "acme/web/app#12"},
		{"https://gitlab.com", "https://gitlab.com/acme/app/issues/5", "acme/app#5"},
		{"https://example.com/gitlab", "https://example.com/gitlab/g/p/-/issues/9#note_1", "g/p#9"},
		{"https://gitlab.acme.dev", "https://gitlab.acme.dev/harnessly/harnessly/-/merge_requests/142", ""},
		{"https://gitlab.com", "https://gitlab.com/groups/acme/-/work_items/3", ""},
		{"https://gitlab.com", "https://gitlab.com/acme/app/-/issues/new", ""},
		{"https://gitlab.com", "https://elsewhere.com/acme/app/-/issues/1", ""},
	}
	for _, c := range cases {
		u, _ := url.Parse(c.link)
		got, ok := gitLabProvider{}.Link(Conn{BaseURL: c.base}, u)
		if (c.key == "") == ok || (ok && (got.Key != c.key || got.Kind != "issue")) {
			t.Errorf("%s: %v %v, want %q", c.link, got, ok, c.key)
		}
	}
}
