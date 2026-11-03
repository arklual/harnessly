package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// hostRewriteRT sends every request to target whatever its scheme and host: it stands in for fixed hosts
// (acme.youtrack.cloud, linear.app, gitlab.com, api.github.com) in tests.
type hostRewriteRT struct{ target *url.URL }

func (r hostRewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.URL.Scheme, c.URL.Host = r.target.Scheme, r.target.Host
	return http.DefaultTransport.RoundTrip(c)
}

// hostRewriteHTTP is an HTTP whose requests all reach srv.
func hostRewriteHTTP(t *testing.T, srv *httptest.Server) *HTTP {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return NewHTTP(hostRewriteRT{target: u})
}

// provJSON answers with a JSON body.
func provJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

const ytIssueFixture = `{"id":"2-1234","idReadable":"ABC-12","summary":"Fix login","description":"Steps:\n1. open",
"created":1730000000000,"updated":1730100000000,"resolved":null,
"project":{"shortName":"ABC","name":"Dev"},
"reporter":{"login":"roma","fullName":"Roma","avatarUrl":"/hub/api/rest/avatar/abc?dpr=2"},
"tags":[{"name":"backend"}],
"customFields":[
 {"$type":"SingleEnumIssueCustomField","name":"Type","value":{"$type":"EnumBundleElement","name":"Bug"}},
 {"$type":"StateIssueCustomField","name":"State","value":{"$type":"StateBundleElement","name":"В работе","isResolved":false}},
 {"$type":"SingleEnumIssueCustomField","name":"Priority","value":{"name":"Major"}},
 {"$type":"SingleUserIssueCustomField","name":"Assignee","value":{"$type":"User","login":"sergey","fullName":"Sergey","avatarUrl":"/hub/api/rest/avatar/xyz"}},
 {"$type":"MultiEnumIssueCustomField","name":"Subsystem","value":[{"name":"API"}]},
 {"$type":"SimpleIssueCustomField","name":"Due Date","value":null}],
"parent":{"issues":[{"idReadable":"ABC-1","summary":"Epic"}]},
"links":[],
"comments":[{"id":"4-1","text":"first","created":1730000500000,"author":{"login":"roma","fullName":"Roma"}},
 {"id":"4-2","text":"second","created":1730000900000,"author":{"login":"sergey","fullName":"Sergey"}}]}`

// ytServer is a YouTrack at prefix ("" or "/youtrack"): config open or closed to anonymous, token perm:ok.
func ytServer(t *testing.T, prefix string, configOpen bool) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ok := strings.CutPrefix(r.URL.Path, prefix)
		if !ok {
			http.NotFound(w, r)
			return
		}
		seen = append(seen, path+"?"+r.URL.RawQuery)
		authed := r.Header.Get("Authorization") == "Bearer perm:ok"
		switch {
		case path == "/api/config" && configOpen:
			provJSON(w, 200, `{"version":"2024.3.45678","build":"45678"}`)
		case path == "/api/users/me" && !authed:
			provJSON(w, 401, `{"error":"Unauthorized","error_description":"Authorization required"}`)
		case path == "/api/users/me":
			provJSON(w, 200, `{"id":"1-1","login":"sergey","fullName":"Sergey","email":"s@x.io","avatarUrl":"/hub/api/rest/avatar/xyz"}`)
		case path == "/" || path == "/login":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><head><title>YouTrack</title></head><body></body></html>"))
		case !authed:
			provJSON(w, 401, `{"error":"Unauthorized"}`)
		case path == "/api/admin/projects":
			provJSON(w, 200, `[{"shortName":"ABC","name":"Dev","archived":false},{"shortName":"OLD","name":"Old","archived":true}]`)
		case path == "/api/issues/ABC-12":
			provJSON(w, 200, ytIssueFixture)
		case path == "/api/issues":
			provJSON(w, 200, "["+ytIssueFixture+"]")
		default:
			provJSON(w, 404, `{"error":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func ytDetect(t *testing.T, h *HTTP, raw string) *Detection {
	t.Helper()
	u, err := Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	d, err := youTrackProvider{}.Detect(context.Background(), NewProber(h), u)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestYouTrackDetectCloud(t *testing.T) {
	srv, seen := ytServer(t, "", true)
	d := ytDetect(t, hostRewriteHTTP(t, srv), "https://acme.youtrack.cloud/issue/ABC-12")
	if d == nil || d.Kind != YouTrack || d.Deployment != Cloud || d.BaseURL != "https://acme.youtrack.cloud" || d.Version != "2024.3.45678" {
		t.Fatalf("detection = %+v", d)
	}
	if len(d.Methods) != 1 || d.Methods[0].ID != "pat" || !d.Methods[0].Recommended || d.Methods[0].TokenURL != "https://acme.youtrack.cloud/users/me?tab=account-security" {
		t.Fatalf("methods = %+v", d.Methods)
	}
	if len(*seen) != 1 {
		t.Fatalf("cloud should need one probe, saw %v", *seen)
	}
}

func TestYouTrackDetectCloudAnonymousClosed(t *testing.T) {
	srv, _ := ytServer(t, "", false)
	d := ytDetect(t, hostRewriteHTTP(t, srv), "https://acme.youtrack.cloud/")
	if d == nil || d.Version != "" || d.BaseURL != "https://acme.youtrack.cloud" {
		t.Fatalf("a closed config must not stop the fast path: %+v", d)
	}
}

func TestYouTrackDetectMyJetBrains(t *testing.T) {
	srv, _ := ytServer(t, "/youtrack", true)
	d := ytDetect(t, hostRewriteHTTP(t, srv), "https://acme.myjetbrains.com/youtrack/issue/ABC-12")
	if d == nil || d.Deployment != Cloud || d.BaseURL != "https://acme.myjetbrains.com/youtrack" || d.Version != "2024.3.45678" {
		t.Fatalf("detection = %+v", d)
	}
	d = ytDetect(t, hostRewriteHTTP(t, srv), "https://acme.myjetbrains.com/")
	if d == nil || d.BaseURL != "https://acme.myjetbrains.com/youtrack" {
		t.Fatalf("a bare host still gets the context path: %+v", d)
	}
}

func TestYouTrackDetectStandalone(t *testing.T) {
	cases := []struct {
		name, prefix string
		open         bool
		version      string
	}{
		{"root with version", "", true, "2024.3.45678"},
		{"context path with version", "/yt", true, "2024.3.45678"},
		{"root, config closed", "", false, ""},
		{"context path, config closed", "/tracker", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := ytServer(t, c.prefix, c.open)
			d := ytDetect(t, NewHTTP(nil), srv.URL+c.prefix+"/issue/ABC-12/some-title")
			if d == nil || d.Deployment != SelfHosted || d.BaseURL != srv.URL+c.prefix || d.Version != c.version {
				t.Fatalf("detection = %+v", d)
			}
		})
	}
}

func TestYouTrackDetectIsExclusive(t *testing.T) {
	// A site that answers 200 HTML everywhere.
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>YouTrack fan page</title></html>"))
	}))
	defer html.Close()
	// A server with the 401 shape but no YouTrack page.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			provJSON(w, 401, `{"error":"Unauthorized","error_description":"Authorization required"}`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<title>Acme portal</title>"))
	}))
	defer other.Close()
	for _, s := range []*httptest.Server{html, other} {
		if d := ytDetect(t, NewHTTP(nil), s.URL+"/"); d != nil {
			t.Fatalf("%s claimed as YouTrack: %+v", s.URL, d)
		}
	}
}

func ytClientFor(t *testing.T, srv *httptest.Server, prefix, token string) *ytClient {
	t.Helper()
	conn := Conn{Kind: YouTrack, BaseURL: srv.URL + prefix, Deployment: SelfHosted}
	return youTrackProvider{}.Open(conn, Credentials{FieldToken: token}, NewHTTP(nil)).(*ytClient)
}

func TestYouTrackMe(t *testing.T) {
	srv, seen := ytServer(t, "/youtrack", true)
	me, err := ytClientFor(t, srv, "/youtrack", "perm:ok").Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.Login != "sergey" || me.Name != "Sergey" || me.Email != "s@x.io" || me.Avatar != srv.URL+"/youtrack/hub/api/rest/avatar/xyz" {
		t.Fatalf("me = %+v", me)
	}
	if !strings.Contains((*seen)[0], "fields=id%2Clogin%2CfullName%2Cemail%2CavatarUrl") {
		t.Fatalf("fields: %v", *seen)
	}
	if _, err := ytClientFor(t, srv, "/youtrack", "perm:bad").Me(context.Background()); Reason(err) != "denied" {
		t.Fatalf("bad token: %v", err)
	}
}

func TestYouTrackQuery(t *testing.T) {
	cases := []struct {
		q    Query
		want string
	}{
		{Query{}, "sort by: updated desc"},
		{Query{Mine: true, Open: true}, "for: me #Unresolved sort by: updated desc"},
		{Query{Text: "login bug", Project: "ABC", Open: true}, "login bug #Unresolved project: {ABC} sort by: updated desc"},
		{Query{Native: "State: {In Progress}", Mine: true}, "State: {In Progress}"},
	}
	for _, c := range cases {
		if got := ytQuery(c.q); got != c.want {
			t.Errorf("ytQuery(%+v) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestYouTrackSearchAndIssue(t *testing.T) {
	srv, seen := ytServer(t, "", true)
	c := ytClientFor(t, srv, "", "perm:ok")
	list, err := c.Search(context.Background(), Query{Mine: true, Open: true, Limit: 5})
	if err != nil || len(list) != 1 {
		t.Fatalf("search = %v, %v", list, err)
	}
	last := (*seen)[len(*seen)-1]
	if !strings.Contains(last, "query=for%3A+me+%23Unresolved+sort+by%3A+updated+desc") || !strings.Contains(last, "%24top=5") {
		t.Fatalf("search request: %s", last)
	}
	i, err := c.Issue(context.Background(), "ABC-12")
	if err != nil {
		t.Fatal(err)
	}
	if i.Key != "ABC-12" || i.Title != "Fix login" || i.URL != srv.URL+"/issue/ABC-12" || i.Project != "ABC" {
		t.Fatalf("issue = %+v", i)
	}
	if i.Status != (Status{Name: "В работе", Category: Doing}) || i.Type != "Bug" || i.Priority != "Major" {
		t.Fatalf("fields: %+v", i)
	}
	if i.Assignee == nil || i.Assignee.Login != "sergey" || i.Assignee.Avatar != srv.URL+"/hub/api/rest/avatar/xyz" {
		t.Fatalf("assignee = %+v", i.Assignee)
	}
	if i.Parent == nil || i.Parent.Key != "ABC-1" || i.Created.IsZero() || !i.Resolved.IsZero() || len(i.Labels) != 1 {
		t.Fatalf("parent/time/labels: %+v", i)
	}
	if len(i.Comments) != 2 || i.Comments[1].Text != "second" || i.Comments[1].Author.Name != "Sergey" {
		t.Fatalf("comments = %+v", i.Comments)
	}
	if _, err := c.Issue(context.Background(), "ABC-999"); Reason(err) != "not-found" {
		t.Fatalf("missing issue: %v", err)
	}
	if _, err := c.Issue(context.Background(), "../etc"); Reason(err) != "not-found" {
		t.Fatalf("bad key: %v", err)
	}
}

func TestYouTrackStatus(t *testing.T) {
	cases := []struct {
		state    ytValue
		has      bool
		resolved bool
		want     Category
	}{
		{ytValue{Name: "Open"}, true, false, Todo},
		{ytValue{Name: "In Progress"}, true, false, Doing},
		{ytValue{Name: "Fixed", IsResolved: true}, true, true, Done},
		{ytValue{Name: "Submitted"}, true, false, Todo},
		{ytValue{}, false, true, Done},
		{ytValue{}, false, false, Unknown},
	}
	for _, c := range cases {
		if got := ytStatus(c.state, c.has, c.resolved).Category; got != c.want {
			t.Errorf("%+v: %s, want %s", c.state, got, c.want)
		}
	}
}

func TestYouTrackProjects(t *testing.T) {
	srv, _ := ytServer(t, "", true)
	list, err := ytClientFor(t, srv, "", "perm:ok").Projects(context.Background())
	if err != nil || len(list) != 1 || list[0].Key != "ABC" || list[0].Name != "Dev" {
		t.Fatalf("projects = %+v, %v", list, err)
	}
}

func TestYouTrackLink(t *testing.T) {
	cases := []struct {
		base, link, key string
	}{
		{"https://acme.youtrack.cloud", "https://acme.youtrack.cloud/issue/ABC-12", "ABC-12"},
		{"https://acme.youtrack.cloud", "https://acme.youtrack.cloud/issue/ABC-12/Fix-login", "ABC-12"},
		{"https://acme.myjetbrains.com/youtrack", "https://acme.myjetbrains.com/youtrack/issue/ABC-7", "ABC-7"},
		{"https://tracker.acme.dev", "https://tracker.acme.dev/agiles/12-3/current?issue=ABC-5", "ABC-5"},
		{"https://tracker.acme.dev", "https://tracker.acme.dev/issues/ABC-5", "ABC-5"},
		{"https://tracker.acme.dev", "https://tracker.acme.dev/articles/ABC-A-1", ""},
		{"https://tracker.acme.dev", "https://other.example/issue/ABC-5", ""},
	}
	for _, c := range cases {
		u, _ := url.Parse(c.link)
		got, ok := youTrackProvider{}.Link(Conn{BaseURL: c.base}, u)
		if c.key == "" {
			if ok {
				t.Errorf("%s: unexpected %v", c.link, got)
			}
			continue
		}
		if !ok || got.Kind != "issue" || got.Key != c.key {
			t.Errorf("%s: %v %v", c.link, got, ok)
		}
	}
}
