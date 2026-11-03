package connections

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const linearIssueFixture = `{"id":"u-1","identifier":"ENG-12","title":"Fix login","url":"https://linear.app/acme/issue/ENG-12/fix-login",
"description":"## Steps\n1. open","priority":2,"priorityLabel":"High","createdAt":"2026-10-01T10:00:00.000Z","updatedAt":"2026-10-02T11:30:00.000Z",
"completedAt":null,"canceledAt":null,"state":{"name":"In Progress","type":"started"},
"assignee":{"id":"a1","name":"Sergey","displayName":"sergey","email":"s@x.io","avatarUrl":"https://cdn.linear.app/a.png"},
"creator":{"id":"a2","name":"Roma","displayName":"roma","email":"r@x.io","avatarUrl":null},
"team":{"key":"ENG","name":"Engineering"},"parent":{"identifier":"ENG-1","title":"Epic","url":"https://linear.app/acme/issue/ENG-1/epic"},
"project":{"name":"Auth"},"labels":{"nodes":[{"name":"bug"}]},
"comments":{"nodes":[{"id":"c2","body":"second","createdAt":"2026-10-02T09:00:00.000Z","user":{"id":"a1","name":"Sergey","displayName":"sergey"}},
 {"id":"c1","body":"first","createdAt":"2026-10-01T12:00:00.000Z","user":{"id":"a2","name":"Roma","displayName":"roma"}}]}}`

type linearReq struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// linearServer answers GraphQL like Linear; searchBroken makes searchIssues a validation error (HTTP 400).
func linearServer(t *testing.T, searchBroken bool) (*httptest.Server, *[]linearReq) {
	t.Helper()
	var seen []linearReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "lin_api_good" {
			provJSON(w, 401, `{"errors":[{"message":"Authentication required, not authenticated","extensions":{"type":"authentication error","code":"AUTHENTICATION_ERROR"}}]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req linearReq
		_ = json.Unmarshal(raw, &req)
		seen = append(seen, req)
		q := req.Query
		switch {
		case strings.Contains(q, "viewer"):
			provJSON(w, 200, `{"data":{"viewer":{"id":"a1","name":"Sergey","displayName":"sergey","email":"s@x.io","avatarUrl":"https://cdn.linear.app/a.png"}}}`)
		case strings.Contains(q, "teams("):
			provJSON(w, 200, `{"data":{"teams":{"nodes":[{"key":"ENG","name":"Engineering"}]}}}`)
		case strings.Contains(q, "searchIssues") && searchBroken:
			provJSON(w, 400, `{"errors":[{"message":"Cannot query field \"searchIssues\" on type \"Query\".","extensions":{"code":"GRAPHQL_VALIDATION_FAILED"}}]}`)
		case strings.Contains(q, "searchIssues"):
			provJSON(w, 200, `{"data":{"searchIssues":{"nodes":[`+linearIssueFixture+`]}}}`)
		case strings.Contains(q, "issues("):
			provJSON(w, 200, `{"data":{"issues":{"nodes":[`+linearIssueFixture+`]}}}`)
		case strings.Contains(q, "issue(id"):
			if req.Variables["id"] == "ENG-12" {
				provJSON(w, 200, `{"data":{"issue":`+linearIssueFixture+`}}`)
			} else {
				provJSON(w, 200, `{"data":null,"errors":[{"message":"Entity not found: Issue","extensions":{"type":"invalid input"}}]}`)
			}
		default:
			provJSON(w, 200, `{"data":{}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func linearOpen(t *testing.T, srv *httptest.Server, key string) Client {
	t.Helper()
	conn := Conn{Kind: Linear, BaseURL: "https://linear.app", APIURL: linearAPI, Deployment: Cloud, Extra: map[string]string{"workspace": "acme"}}
	return linearProvider{}.Open(conn, Credentials{FieldToken: key}, hostRewriteHTTP(t, srv))
}

func TestLinearDetect(t *testing.T) {
	u, _ := Normalize("https://linear.app/acme/issue/ENG-12/fix-login")
	d, err := linearProvider{}.Detect(context.Background(), nil, u)
	if err != nil || d == nil {
		t.Fatalf("detect = %v, %v", d, err)
	}
	if d.Kind != Linear || d.BaseURL != "https://linear.app" || d.APIURL != linearAPI || d.Deployment != Cloud || d.Extra["workspace"] != "acme" {
		t.Fatalf("detection = %+v", d)
	}
	if m := d.Methods[0]; m.ID != "api-key" || m.TokenURL != linearTokenURL || !m.Recommended || m.Fields[0].ID != FieldToken {
		t.Fatalf("method = %+v", m)
	}
	for _, other := range []string{"https://linear.example.com/", "https://notlinear.app/", "https://api.linear.app/graphql"} {
		u, _ := Normalize(other)
		if d, _ := (linearProvider{}).Detect(context.Background(), nil, u); d != nil {
			t.Errorf("%s detected as Linear", other)
		}
	}
	u, _ = Normalize("https://linear.app/settings/account")
	if d, _ := (linearProvider{}).Detect(context.Background(), nil, u); d == nil || d.Extra != nil {
		t.Errorf("settings is no workspace: %+v", d)
	}
}

func TestLinearMe(t *testing.T) {
	srv, _ := linearServer(t, false)
	me, err := linearOpen(t, srv, "lin_api_good").Me(context.Background())
	if err != nil || me.Name != "Sergey" || me.Login != "sergey" || me.Email != "s@x.io" || me.Avatar == "" {
		t.Fatalf("me = %+v, %v", me, err)
	}
	if _, err := linearOpen(t, srv, "lin_api_bad").Me(context.Background()); Reason(err) != "denied" {
		t.Fatalf("bad key: %v", err)
	}
}

func TestLinearAuthHeaderHasNoBearer(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		provJSON(w, 200, `{"data":{"viewer":{"id":"1","name":"A"}}}`)
	}))
	defer srv.Close()
	if _, err := linearOpen(t, srv, "lin_api_x").(*linearClient).Me(context.Background()); err != nil || got != "lin_api_x" {
		t.Fatalf("header %q, %v", got, err)
	}
	if _, err := linearOpen(t, srv, "lin_oauth_x").(*linearClient).Me(context.Background()); err != nil || got != "Bearer lin_oauth_x" {
		t.Fatalf("oauth header %q, %v", got, err)
	}
}

func TestLinearSearch(t *testing.T) {
	srv, seen := linearServer(t, false)
	c := linearOpen(t, srv, "lin_api_good").(IssueSource)
	list, err := c.Search(context.Background(), Query{Mine: true, Open: true, Project: "ENG", Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("search = %v, %v", list, err)
	}
	req := (*seen)[0]
	if !strings.Contains(req.Query, "orderBy: updatedAt") || req.Variables["first"] != float64(10) {
		t.Fatalf("request = %+v", req)
	}
	filter, _ := json.Marshal(req.Variables["filter"])
	for _, want := range []string{`"isMe":{"eq":true}`, `"nin":["completed","canceled"]`, `"key":{"eqIgnoreCase":"ENG"}`} {
		if !strings.Contains(string(filter), want) {
			t.Errorf("filter %s lacks %s", filter, want)
		}
	}
	// text goes to searchIssues
	if _, err := c.Search(context.Background(), Query{Text: "login"}); err != nil {
		t.Fatal(err)
	}
	if last := (*seen)[len(*seen)-1]; !strings.Contains(last.Query, "searchIssues") || last.Variables["term"] != "login" {
		t.Fatalf("text search = %+v", last)
	}
}

func TestLinearSearchFallsBackToTitleFilter(t *testing.T) {
	srv, seen := linearServer(t, true)
	c := linearOpen(t, srv, "lin_api_good").(IssueSource)
	list, err := c.Search(context.Background(), Query{Text: "login", Mine: true})
	if err != nil || len(list) != 1 {
		t.Fatalf("search = %v, %v", list, err)
	}
	if len(*seen) != 2 || !strings.Contains((*seen)[1].Query, "issues(") {
		t.Fatalf("calls = %+v", *seen)
	}
	filter, _ := json.Marshal((*seen)[1].Variables["filter"])
	if !strings.Contains(string(filter), `"title":{"containsIgnoreCase":"login"}`) || !strings.Contains(string(filter), "isMe") {
		t.Fatalf("fallback filter = %s", filter)
	}
}

func TestLinearIssueMapping(t *testing.T) {
	srv, _ := linearServer(t, false)
	c := linearOpen(t, srv, "lin_api_good").(IssueSource)
	i, err := c.Issue(context.Background(), "eng-12")
	if err != nil {
		t.Fatal(err)
	}
	if i.Key != "ENG-12" || i.Title != "Fix login" || i.Status != (Status{Name: "In Progress", Category: Doing}) || i.Priority != "High" || i.Project != "ENG" {
		t.Fatalf("issue = %+v", i)
	}
	if i.Assignee == nil || i.Assignee.Name != "Sergey" || i.Reporter == nil || i.Parent == nil || i.Parent.Key != "ENG-1" || len(i.Labels) != 1 {
		t.Fatalf("people/parent: %+v", i)
	}
	if i.Updated.IsZero() || !i.Resolved.IsZero() {
		t.Fatalf("times: %+v", i)
	}
	if len(i.Comments) != 2 || i.Comments[0].Text != "first" || i.Comments[1].Author.Login != "sergey" {
		t.Fatalf("comments (oldest first) = %+v", i.Comments)
	}
	if _, err := c.Issue(context.Background(), "ENG-999"); Reason(err) != "not-found" {
		t.Fatalf("missing: %v", err)
	}
	if _, err := c.Issue(context.Background(), "nope"); Reason(err) != "not-found" {
		t.Fatalf("bad key: %v", err)
	}
}

func TestLinearCategories(t *testing.T) {
	want := map[string]Category{"triage": Todo, "backlog": Todo, "unstarted": Todo, "started": Doing, "completed": Done, "canceled": Done, "weird": Unknown}
	for in, w := range want {
		if got := linearCategory(in); got != w {
			t.Errorf("%s: %s, want %s", in, got, w)
		}
	}
}

func TestLinearProjectsAndLink(t *testing.T) {
	srv, _ := linearServer(t, false)
	list, err := linearOpen(t, srv, "lin_api_good").(IssueSource).Projects(context.Background())
	if err != nil || len(list) != 1 || list[0].Key != "ENG" || list[0].URL != "https://linear.app/acme/team/ENG" {
		t.Fatalf("projects = %+v, %v", list, err)
	}
	u, _ := url.Parse("https://linear.app/acme/issue/ENG-12/fix-login")
	if got, ok := (linearProvider{}).Link(Conn{}, u); !ok || got.Key != "ENG-12" || got.Kind != "issue" {
		t.Fatalf("link = %v %v", got, ok)
	}
	for _, other := range []string{"https://linear.app/acme/team/ENG/active", "https://example.com/acme/issue/ENG-12"} {
		u, _ := url.Parse(other)
		if _, ok := (linearProvider{}).Link(Conn{}, u); ok {
			t.Errorf("%s recognized", other)
		}
	}
}
