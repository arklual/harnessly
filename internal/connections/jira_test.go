package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	jiraServerInfo912 = `{"baseUrl":"%s","version":"9.12.4","versionNumbers":[9,12,4],"deploymentType":"Server","buildNumber":9120004,"serverTitle":"Acme Jira"}`
	jiraMyselfServer  = `{"key":"JIRAUSER10000","name":"ann","displayName":"Ann Lee","emailAddress":"ann@acme.test","active":true,"avatarUrls":{"48x48":"https://jira.acme.test/avatar/48"}}`
	jiraMyselfCloud   = `{"accountId":"5b10a2844c20165700ede21g","displayName":"Ann Lee","emailAddress":"ann@acme.test","avatarUrls":{"48x48":"https://avatar.test/a.png"}}`
)

func jiraDetect(t *testing.T, raw string) *Detection {
	t.Helper()
	d, err := (&jiraProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), atlParse(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func jiraOpen(conn Conn, cred Credentials) *jiraClient {
	return (&jiraProvider{}).Open(conn, cred, NewHTTP(nil)).(*jiraClient)
}

func TestJiraDetectCloud(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /_edge/tenant_info", 200, `{"cloudId":"0a1b-2c3d"}`)
	d, err := (&jiraProvider{}).Detect(context.Background(), NewProber(atlRewriteHTTP(srv)), atlParse(t, "acme.atlassian.net/browse/ABC-1"))
	if err != nil || d == nil {
		t.Fatalf("%v %v", d, err)
	}
	if d.Kind != Jira || d.Deployment != Cloud || d.BaseURL != "https://acme.atlassian.net" || d.Extra["cloudId"] != "0a1b-2c3d" {
		t.Errorf("%+v", d)
	}
	if !atlSame(atlMethodIDs(d), []string{"api-token"}) || !d.Methods[0].Recommended || d.Methods[0].TokenURL != atlCloudTokenURL {
		t.Errorf("methods %+v", d.Methods)
	}
	if got := []string{d.Methods[0].Fields[0].ID, d.Methods[0].Fields[1].ID}; !atlSame(got, []string{FieldEmail, FieldToken}) || !d.Methods[0].Fields[1].Secret {
		t.Errorf("fields %+v", d.Methods[0].Fields)
	}
	// The fast path does not need the API: only the tenant id is asked for.
	if f.requests("serverInfo") != 0 {
		t.Error("serverInfo was probed on atlassian.net")
	}
	// Anonymous access off: the site is still Jira.
	f2, srv2 := atlNewFake(t, "")
	f2.on("GET /_edge/tenant_info", 403, `{}`)
	d, _ = (&jiraProvider{}).Detect(context.Background(), NewProber(atlRewriteHTTP(srv2)), atlParse(t, "https://acme.atlassian.net"))
	if d == nil || d.Extra != nil {
		t.Errorf("without tenant info: %+v", d)
	}
}

func TestJiraDetectCloudCustomDomain(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /rest/api/2/serverInfo", 200, `{"baseUrl":"https://jira.acme.test","version":"1001.0.0-SNAPSHOT","versionNumbers":[1001,0,0],"deploymentType":"Cloud","buildNumber":100287,"serverTitle":"Jira"}`)
	f.on("GET /_edge/tenant_info", 200, `{"cloudId":"cid-1"}`)
	for path, claimed := range map[string]bool{"/browse/ABC-1": true, "/wiki/spaces/X/pages/1": false} {
		d, err := (&jiraProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), atlParse(t, srv.URL+path))
		if err != nil || (d != nil) != claimed {
			t.Fatalf("%s: %+v %v", path, d, err)
		}
		if d != nil && (d.Deployment != Cloud || d.BaseURL != srv.URL || d.Extra["cloudId"] != "cid-1" || d.Methods[0].ID != "api-token") {
			t.Errorf("%s: %+v", path, d)
		}
	}
}

func TestJiraDetectServerVersions(t *testing.T) {
	cases := []struct {
		name, version, edition string
		nodeHeader             bool
		methods                []string
		recommended            string
	}{
		{"7.13", "7.13.18", "server", false, []string{"basic"}, "basic"},
		{"8.0", "8.0.3", "server", false, []string{"basic"}, "basic"},
		{"8.5", "8.5.19", "server", false, []string{"basic"}, "basic"},
		{"8.13", "8.13.5", "server", false, []string{"basic"}, "basic"},
		{"8.14", "8.14.0", "server", false, []string{"pat", "basic"}, "pat"},
		{"9.12 data center", "9.12.4", "data-center", true, []string{"pat", "basic"}, "pat"},
		{"9.12 server", "9.12.4", "server", false, []string{"pat", "basic"}, "pat"},
		{"10.3", "10.3.1", "data-center", false, []string{"pat", "basic"}, "pat"},
		{"11", "11.0.1", "data-center", false, []string{"pat", "basic"}, "pat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, srv := atlNewFake(t, "")
			if c.nodeHeader {
				f.hdr.Set("X-ANODEID", "node1")
			}
			parts := strings.Split(c.version, ".")
			info := fmt.Sprintf(`{"baseUrl":"%s","version":"%s","versionNumbers":[%s,%s,0],"deploymentType":"Server","serverTitle":"Acme Jira"}`, srv.URL, c.version, parts[0], parts[1])
			f.on("GET /rest/api/2/serverInfo", 200, info)
			d := jiraDetect(t, srv.URL+"/browse/ABC-1")
			if d == nil {
				t.Fatal("not detected")
			}
			if d.Kind != Jira || d.Deployment != SelfHosted || d.Version != c.version || d.Edition != c.edition || d.BaseURL != srv.URL || d.Title != "Acme Jira" {
				t.Errorf("%+v", d)
			}
			if !atlSame(atlMethodIDs(d), c.methods) {
				t.Errorf("methods %v", atlMethodIDs(d))
			}
			for _, m := range d.Methods {
				if m.Recommended != (m.ID == c.recommended) {
					t.Errorf("%s recommended=%v", m.ID, m.Recommended)
				}
			}
			if len(d.Capabilities) != 1 || d.Capabilities[0] != Issues {
				t.Errorf("capabilities %v", d.Capabilities)
			}
			for _, m := range d.Methods {
				if m.ID == "pat" && !strings.HasPrefix(m.TokenURL, srv.URL+"/secure/ViewProfile.jspa?selectedTab=com.atlassian.pats") {
					t.Errorf("token url %s", m.TokenURL)
				}
			}
		})
	}
}

func TestJiraDetectContextPath(t *testing.T) {
	f, srv := atlNewFake(t, "/jira")
	f.on("GET /rest/api/2/serverInfo", 200, fmt.Sprintf(jiraServerInfo912, "SELF"))
	d := jiraDetect(t, srv.URL+"/jira/browse/ABC-1?focusedCommentId=5")
	if d == nil || d.BaseURL != srv.URL+"/jira" || d.Version != "9.12.4" {
		t.Fatalf("%+v", d)
	}
	if d := jiraDetect(t, srv.URL+"/jira/"); d == nil || d.BaseURL != srv.URL+"/jira" {
		t.Errorf("bare context path: %+v", d)
	}
	if d := jiraDetect(t, srv.URL+"/jira/secure/Dashboard.jspa"); d == nil || d.BaseURL != srv.URL+"/jira" {
		t.Errorf("dashboard: %+v", d)
	}
	// Only the root was pasted, serverInfo.baseUrl names the context path and the API answers there.
	f2, srv2 := atlNewFake(t, "")
	f2.handle("GET /rest/api/2/serverInfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, jiraServerInfo912, srv2.URL+"/jira")
	})
	f2.handle("GET /jira/rest/api/2/serverInfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, jiraServerInfo912, srv2.URL+"/jira")
	})
	if d := jiraDetect(t, srv2.URL); d == nil || d.BaseURL != srv2.URL+"/jira" {
		t.Errorf("baseUrl context path: %+v", d)
	}
}

func TestJiraDetectAnonymousDenied(t *testing.T) {
	denied := `{"message":"You do not have the permission to see the specified issue.","status-code":401}`
	t.Run("applinks manifest", func(t *testing.T) {
		f, srv := atlNewFake(t, "/jira")
		f.on("GET /rest/api/2/serverInfo", 401, denied)
		f.on("GET /rest/applinks/1.0/manifest", 200, `<?xml version="1.0" encoding="UTF-8"?><manifest xmlns="http://www.atlassian.com/applinks/manifest"><id>x</id><name>JIRA</name><typeId>jira</typeId><version>8.5.19</version><buildNumber>805019</buildNumber></manifest>`)
		d := jiraDetect(t, srv.URL+"/jira/browse/ABC-1")
		if d == nil || d.Version != "8.5.19" || d.BaseURL != srv.URL+"/jira" || !atlSame(atlMethodIDs(d), []string{"basic"}) {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("login page meta", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/2/serverInfo", 403, denied)
		f.on("GET /login.jsp", 200, `<!DOCTYPE html><html><head><meta name="application-name" content="JIRA" data-name="jira" data-version="9.4.12"><meta name="ajs-version-number" content="9.4.12"></head><body></body></html>`)
		d := jiraDetect(t, srv.URL)
		if d == nil || d.Version != "9.4.12" || !atlSame(atlMethodIDs(d), []string{"pat", "basic"}) {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("atlassian headers on a 401", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.hdr.Set("X-AREQUESTID", "abc123")
		f.hdr.Set("X-ASEN", "SEN-1234")
		f.on("GET /rest/api/2/serverInfo", 401, denied)
		d := jiraDetect(t, srv.URL+"/browse/ABC-1")
		if d == nil || d.Version != "" || !atlSame(atlMethodIDs(d), []string{"pat", "basic"}) || !d.Methods[0].Recommended {
			t.Fatalf("unknown version must offer the token first, then login: %+v", d)
		}
		// The same headers on a link that does not look like Jira's are not enough.
		if d := jiraDetect(t, srv.URL+"/something"); d != nil {
			t.Errorf("bare Atlassian 401 claimed: %+v", d)
		}
	})
}

func TestJiraDetectNotJira(t *testing.T) {
	t.Run("html 200 everywhere", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		for _, r := range []string{"/rest/api/2/serverInfo", "/login.jsp", "/rest/applinks/1.0/manifest"} {
			f.on("GET "+r, 200, "<html><body>app</body></html>")
		}
		if d := jiraDetect(t, srv.URL+"/browse/ABC-1"); d != nil {
			t.Errorf("%+v", d)
		}
	})
	t.Run("confluence", func(t *testing.T) {
		f, srv := atlNewFake(t, "/confluence")
		f.hdr.Set("X-AREQUESTID", "x")
		f.hdr.Set("X-Confluence-Request-Time", "1700000000")
		f.on("GET /rest/api/2/serverInfo", 401, `{"message":"no"}`)
		f.on("GET /rest/applinks/1.0/manifest", 200, `<?xml version="1.0"?><manifest><typeId>confluence</typeId><version>8.5.4</version></manifest>`)
		if d := jiraDetect(t, srv.URL+"/confluence/browse/ABC-1"); d != nil {
			t.Errorf("%+v", d)
		}
	})
	t.Run("wiki on atlassian.net", func(t *testing.T) {
		_, srv := atlNewFake(t, "")
		d, err := (&jiraProvider{}).Detect(context.Background(), NewProber(atlRewriteHTTP(srv)), atlParse(t, "https://acme.atlassian.net/wiki/spaces/ENG"))
		if d != nil || err != nil {
			t.Errorf("%+v %v", d, err)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		if d := jiraDetect(t, "http://127.0.0.1:1/"); d != nil {
			t.Errorf("%+v", d)
		}
	})
}

func TestJiraMe(t *testing.T) {
	t.Run("cloud basic", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/3/myself", 200, jiraMyselfCloud)
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: Cloud}, Credentials{FieldEmail: "ann@acme.test", FieldToken: "tok"})
		me, err := c.Me(context.Background())
		if err != nil || me.Name != "Ann Lee" || me.ID != "5b10a2844c20165700ede21g" || me.Email != "ann@acme.test" || me.Avatar == "" {
			t.Fatalf("%+v %v", me, err)
		}
		if f.auth("GET /rest/api/3/myself") != Basic("ann@acme.test", "tok") {
			t.Error("Cloud sends email:token as Basic")
		}
	})
	t.Run("server pat", func(t *testing.T) {
		f, srv := atlNewFake(t, "/jira")
		f.on("GET /rest/api/2/myself", 200, jiraMyselfServer)
		c := jiraOpen(Conn{BaseURL: srv.URL + "/jira", Deployment: SelfHosted, Version: "9.12.4"}, Credentials{FieldToken: "pat-secret"})
		me, err := c.Me(context.Background())
		if err != nil || me.Login != "ann" || me.ID != "JIRAUSER10000" {
			t.Fatalf("%+v %v", me, err)
		}
		if f.auth("GET /rest/api/2/myself") != "Bearer pat-secret" {
			t.Error("a PAT is sent as Bearer")
		}
	})
	t.Run("server basic", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/2/myself", 200, jiraMyselfServer)
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: SelfHosted, Version: "8.5.1"}, Credentials{FieldUsername: "ann", FieldPassword: "pw"})
		if _, err := c.Me(context.Background()); err != nil {
			t.Fatal(err)
		}
		if f.auth("GET /rest/api/2/myself") != Basic("ann", "pw") {
			t.Error("login and password are sent as Basic")
		}
	})
	t.Run("denied", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/2/myself", 401, `{}`)
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: SelfHosted}, Credentials{FieldToken: "bad"})
		if _, err := c.Me(context.Background()); !errors.Is(err, ErrDenied) {
			t.Errorf("%v", err)
		}
		if _, err := jiraOpen(Conn{BaseURL: srv.URL}, Credentials{}).Me(context.Background()); !errors.Is(err, ErrDenied) {
			t.Errorf("no credentials: %v", err)
		}
	})
	t.Run("anonymous 200", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.hdr.Set("X-AUSERNAME", "anonymous")
		f.on("GET /rest/api/2/myself", 200, `{"displayName":"Anonymous"}`)
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: SelfHosted}, Credentials{FieldToken: "t"})
		if _, err := c.Me(context.Background()); !errors.Is(err, ErrDenied) {
			t.Errorf("%v", err)
		}
	})
}

func TestJiraJQL(t *testing.T) {
	cases := []struct {
		name  string
		q     Query
		cloud bool
		want  string
	}{
		{"text", Query{Text: `fix "login" \ bug`}, false, `text ~ "fix \"login\" \\ bug" ORDER BY updated DESC`},
		{"mine open", Query{Mine: true, Open: true}, false, `assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC`},
		{"project", Query{Project: "ABC", Text: "x"}, false, `text ~ "x" AND project = "ABC" ORDER BY updated DESC`},
		{"open only server", Query{Open: true}, false, `statusCategory != Done ORDER BY updated DESC`},
		{"open only cloud is bounded", Query{Open: true}, true, `statusCategory != Done AND updated >= -365d ORDER BY updated DESC`},
		{"empty cloud is bounded", Query{}, true, `updated >= -365d ORDER BY updated DESC`},
		{"empty server", Query{}, false, `ORDER BY updated DESC`},
		{"mine cloud already bounded", Query{Mine: true}, true, `assignee = currentUser() ORDER BY updated DESC`},
		{"native wins", Query{Native: `project = X AND labels = a`, Text: "ignored", Mine: true}, true, `project = X AND labels = a`},
		{"native order only, cloud", Query{Native: `ORDER BY created DESC`}, true, `updated >= -365d ORDER BY created DESC`},
		{"native order only, server", Query{Native: `order by created`}, false, `order by created`},
	}
	for _, c := range cases {
		if got := jiraJQL(c.q, c.cloud); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

const jiraCloudIssue = `{"id":"10001","key":"ABC-1","fields":{
 "summary":"Fix login","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},
 "issuetype":{"name":"Bug","subtask":false},"priority":{"name":"High"},
 "assignee":{"accountId":"a1","displayName":"Ann Lee"},"reporter":{"accountId":"r1","displayName":"Bob"},
 "project":{"key":"ABC"},"parent":{"id":"10000","key":"ABC-100","fields":{"summary":"Epic one"}},
 "labels":["web","urgent"],"created":"2025-01-02T10:00:00.000+0000","updated":"2025-02-03T11:30:00.000+0100","resolutiondate":null,
 "description":` + atlADFDoc + `}}`

func jiraCommentsJSON(from, to, total int, adf bool) string {
	var parts []string
	for i := from; i < to; i++ {
		body := fmt.Sprintf(`"comment %d *bold*"`, i)
		if adf {
			body = fmt.Sprintf(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"comment %d"}]}]}`, i)
		}
		parts = append(parts, fmt.Sprintf(`{"id":"%d","author":{"accountId":"a1","name":"ann","displayName":"Ann Lee"},"body":%s,"created":"2025-03-%02dT09:00:00.000+0000"}`, 100+i, body, i%28+1))
	}
	return fmt.Sprintf(`{"startAt":%d,"maxResults":20,"total":%d,"comments":[%s]}`, from, total, strings.Join(parts, ","))
}

func TestJiraIssueCloud(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /rest/api/3/issue/ABC-1", 200, jiraCloudIssue)
	f.handle("GET /rest/api/3/issue/ABC-1/comment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("startAt") == "0" {
			_, _ = fmt.Fprint(w, jiraCommentsJSON(0, 20, 25, true))
			return
		}
		_, _ = fmt.Fprint(w, jiraCommentsJSON(5, 25, 25, true))
	})
	c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
	is, err := c.Issue(context.Background(), "ABC-1")
	if err != nil {
		t.Fatal(err)
	}
	if is.Key != "ABC-1" || is.Title != "Fix login" || is.Status != (Status{Name: "In Progress", Category: Doing}) || is.Type != "Bug" ||
		is.Priority != "High" || is.Project != "ABC" || is.URL != srv.URL+"/browse/ABC-1" || is.Assignee.Name != "Ann Lee" || is.Reporter.Name != "Bob" {
		t.Errorf("%+v", is)
	}
	if is.Parent == nil || is.Parent.Key != "ABC-100" || is.Parent.Title != "Epic one" {
		t.Errorf("parent %+v", is.Parent)
	}
	if !atlSame(is.Labels, []string{"web", "urgent"}) || is.Created.IsZero() || is.Updated.IsZero() || !is.Resolved.IsZero() {
		t.Errorf("labels/times %+v", is)
	}
	if !strings.Contains(is.Description, "## Steps") || !strings.Contains(is.Description, "[docs](https://example.com/d)") {
		t.Errorf("ADF description: %s", is.Description)
	}
	// 25 comments: the last 20 are asked for (startAt=5), oldest first.
	if len(is.Comments) != 20 || is.Comments[0].Text != "comment 5" || is.Comments[19].Text != "comment 24" {
		t.Errorf("comments %d first=%q", len(is.Comments), is.Comments[0].Text)
	}
	if f.requests("maxResults=20&startAt=5") != 1 {
		t.Errorf("log %v", f.log)
	}
	// Cloud has no Epic Link discovery: epics come through parent.
	if f.requests("/field") != 0 {
		t.Error("Cloud asked for fields")
	}
	if want := "ABC-1?fields=summary%2Cstatus"; f.requests(want) != 1 {
		t.Errorf("issue request: %v", f.log)
	}
}

func TestJiraIssueServerWikiMarkup(t *testing.T) {
	for _, version := range []string{"7.13.18", "9.12.4"} {
		t.Run(version, func(t *testing.T) {
			f, srv := atlNewFake(t, "/jira")
			f.on("GET /rest/api/2/field", 200, `[{"id":"summary","name":"Summary","custom":false,"schema":{"type":"string","system":"summary"}},{"id":"customfield_10014","name":"Epic Link","custom":true,"schema":{"type":"any","custom":"com.pyxis.greenhopper.jira:gh-epic-link","customId":10014}}]`)
			f.on("GET /rest/api/2/issue/ABC-7", 200, `{"id":"20007","key":"ABC-7","fields":{
 "summary":"Crash on save","status":{"name":"Open","statusCategory":{"key":"new","name":"To Do"}},"issuetype":{"name":"Task"},
 "priority":{"name":"Major"},"assignee":{"name":"ann","key":"ann","displayName":"Ann Lee","emailAddress":"ann@acme.test"},"reporter":null,
 "project":{"key":"ABC"},"labels":[],"created":"2019-05-06T07:08:09.000+0300","updated":"2019-05-07T07:08:09.000+0300","resolutiondate":"2019-06-01T00:00:00.000+0000",
 "customfield_10014":"ABC-50",
 "description":"h3. Repro\n# open the *file*\n# press _save_\n\n{code}\nsave();\n{code}\nSee [~ann]."}}`)
			f.on("GET /rest/api/2/issue/ABC-7/comment", 200, jiraCommentsJSON(0, 2, 2, false))
			c := jiraOpen(Conn{BaseURL: srv.URL + "/jira", Deployment: SelfHosted, Version: version}, Credentials{FieldToken: "p"})
			is, err := c.Issue(context.Background(), "ABC-7")
			if err != nil {
				t.Fatal(err)
			}
			if is.Status.Category != Todo || is.Reporter != nil || is.URL != srv.URL+"/jira/browse/ABC-7" || is.Resolved.IsZero() {
				t.Errorf("%+v", is)
			}
			if is.Parent == nil || is.Parent.Key != "ABC-50" {
				t.Errorf("epic link parent: %+v", is.Parent)
			}
			for _, want := range []string{"### Repro", "1. open the **file**", "1. press *save*", "```\nsave();\n```", "See @ann."} {
				if !strings.Contains(is.Description, want) {
					t.Errorf("missing %q in %q", want, is.Description)
				}
			}
			if len(is.Comments) != 2 || is.Comments[0].Text != "comment 0 **bold**" || is.Comments[0].Author.Login != "ann" {
				t.Errorf("comments %+v", is.Comments)
			}
			if !strings.Contains(strings.Join(f.log, "\n"), "fields=summary%2Cstatus%2Cissuetype%2Cpriority%2Cassignee%2Creporter%2Cproject%2Cparent%2Clabels%2Ccreated%2Cupdated%2Cresolutiondate%2Cdescription%2Ccustomfield_10014") {
				t.Errorf("fields requested: %v", f.log)
			}
			// The Epic Link field is looked up once per instance.
			if _, err := c.Issue(context.Background(), "ABC-7"); err != nil || f.requests("/field") != 1 {
				t.Errorf("field lookups: %d %v", f.requests("/field"), err)
			}
		})
	}
}

func TestJiraIssueErrors(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /rest/api/2/field", 200, `[]`)
	f.on("GET /rest/api/2/issue/ABC-404", 404, `{"errorMessages":["Issue Does Not Exist"]}`)
	f.on("GET /rest/api/2/issue/ABC-403", 403, `{}`)
	c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: SelfHosted}, Credentials{FieldToken: "p"})
	if _, err := c.Issue(context.Background(), "ABC-404"); !errors.Is(err, ErrNotFound) {
		t.Errorf("%v", err)
	}
	if _, err := c.Issue(context.Background(), "ABC-403"); !errors.Is(err, ErrDenied) {
		t.Errorf("%v", err)
	}
	for _, bad := range []string{"", "ABC", "../rest/api/2/myself", "ABC-1/comment", "1-2"} {
		if _, err := c.Issue(context.Background(), bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if f.requests("myself") != 0 {
		t.Error("a bad key reached the network")
	}
}

func jiraSearchIssue(key string) string {
	return fmt.Sprintf(`{"id":"1%s","key":"%s","fields":{"summary":"S %s","status":{"name":"Done","statusCategory":{"key":"done"}},"issuetype":{"name":"Story"},"project":{"key":"ABC"},"labels":[],"updated":"2025-02-03T11:30:00.000+0000"}}`, key[4:], key, key)
}

func TestJiraSearchCloud(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("POST /rest/api/3/search", 410, `{"errorMessages":["The requested API has been removed."]}`)
	f.handle("POST /rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			NextPageToken string `json:"nextPageToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body.NextPageToken == "" {
			_, _ = fmt.Fprintf(w, `{"issues":[%s,%s],"nextPageToken":"tok-2","isLast":false}`, jiraSearchIssue("ABC-3"), jiraSearchIssue("ABC-2"))
			return
		}
		_, _ = fmt.Fprintf(w, `{"issues":[%s],"isLast":true}`, jiraSearchIssue("ABC-1"))
	})
	c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
	got, err := c.Search(context.Background(), Query{Text: "login", Mine: true, Limit: 10})
	if err != nil || len(got) != 3 || got[0].Key != "ABC-3" || got[2].Key != "ABC-1" || got[0].Status.Category != Done {
		t.Fatalf("%+v %v", got, err)
	}
	if f.requests("POST /rest/api/3/search/jql") != 2 || f.requests("POST /rest/api/3/search?") != 0 {
		t.Errorf("log %v", f.log)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(f.body("POST /rest/api/3/search/jql")), &first); err != nil {
		t.Fatal(err) // the last body is the second page's
	}
	if first["nextPageToken"] != "tok-2" || first["jql"] != `text ~ "login" AND assignee = currentUser() ORDER BY updated DESC` {
		t.Errorf("last body %v", first)
	}
	fields, _ := first["fields"].([]any)
	if len(fields) < 10 || fields[0] != "summary" {
		t.Errorf("fields must be explicit: %v", first["fields"])
	}
	if f.auth("POST /rest/api/3/search/jql") != Basic("a@b.c", "t") {
		t.Error("auth")
	}
	// A limit smaller than a page cuts the answer.
	one, err := c.Search(context.Background(), Query{Limit: 1})
	if err != nil || len(one) != 1 {
		t.Errorf("%+v %v", one, err)
	}
}

func TestJiraSearchServer(t *testing.T) {
	f, srv := atlNewFake(t, "/jira")
	f.on("GET /rest/api/2/field", 200, `[{"id":"customfield_10014","name":"Epic Link","custom":true,"schema":{"custom":"com.pyxis.greenhopper.jira:gh-epic-link"}}]`)
	f.handle("POST /rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StartAt int `json:"startAt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body.StartAt == 0 {
			_, _ = fmt.Fprintf(w, `{"startAt":0,"maxResults":2,"total":3,"issues":[%s,%s]}`, jiraSearchIssue("ABC-3"), strings.Replace(jiraSearchIssue("ABC-2"), `"labels":[]`, `"labels":[],"customfield_10014":"ABC-90"`, 1))
			return
		}
		_, _ = fmt.Fprintf(w, `{"startAt":2,"maxResults":2,"total":3,"issues":[%s]}`, jiraSearchIssue("ABC-1"))
	})
	c := jiraOpen(Conn{BaseURL: srv.URL + "/jira", Deployment: SelfHosted, Version: "8.5.1"}, Credentials{FieldUsername: "ann", FieldPassword: "pw"})
	got, err := c.Search(context.Background(), Query{Open: true, Project: "ABC", Limit: 50})
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[1].Parent == nil || got[1].Parent.Key != "ABC-90" || got[0].Parent != nil {
		t.Errorf("epic link parents: %+v %+v", got[0].Parent, got[1].Parent)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(f.body("POST /rest/api/2/search")), &sent)
	if sent["jql"] != `project = "ABC" AND statusCategory != Done ORDER BY updated DESC` || sent["startAt"] != float64(2) {
		t.Errorf("last request %v", sent)
	}
	if f.requests("/search/jql") != 0 {
		t.Error("a Server instance must use /search")
	}
	if got[0].URL != srv.URL+"/jira/browse/ABC-3" {
		t.Errorf("url %s", got[0].URL)
	}
}

func TestJiraSearchEndpointFallbacks(t *testing.T) {
	t.Run("cloud-like site without /search/jql", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("POST /rest/api/3/search/jql", 404, `{}`)
		f.on("POST /rest/api/3/search", 200, fmt.Sprintf(`{"startAt":0,"total":1,"issues":[%s]}`, jiraSearchIssue("ABC-1")))
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
		got, err := c.Search(context.Background(), Query{Mine: true})
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v %v", got, err)
		}
		if _, err := c.Search(context.Background(), Query{Mine: true}); err != nil || f.requests("search/jql") != 1 {
			t.Errorf("the working endpoint must be remembered: %v", f.log)
		}
	})
	t.Run("self-hosted drops /search with 410", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/2/field", 200, `[]`)
		f.on("POST /rest/api/2/search", 410, `{}`)
		f.on("POST /rest/api/2/search/jql", 200, fmt.Sprintf(`{"issues":[%s],"isLast":true}`, jiraSearchIssue("ABC-1")))
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: SelfHosted, Version: "12.0.0"}, Credentials{FieldToken: "p"})
		got, err := c.Search(context.Background(), Query{Mine: true})
		if err != nil || len(got) != 1 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("both gone", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("POST /rest/api/3/search/jql", 410, `{}`)
		f.on("POST /rest/api/3/search", 410, `{}`)
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
		if _, err := c.Search(context.Background(), Query{Mine: true}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%v", err)
		}
	})
	t.Run("bad jql is not a missing endpoint", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("POST /rest/api/3/search/jql", 400, `{"errorMessages":["Unbounded JQL queries are not allowed here."]}`)
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
		if _, err := c.Search(context.Background(), Query{Native: "x ~ ["}); !errors.Is(err, ErrUnreachable) || f.requests("POST /rest/api/3/search?") != 0 {
			t.Errorf("%v %v", err, f.log)
		}
	})
}

func TestJiraProjects(t *testing.T) {
	t.Run("cloud paged", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.handle("GET /rest/api/3/project/search", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("startAt") == "0" {
				_, _ = fmt.Fprint(w, `{"startAt":0,"maxResults":50,"isLast":false,"values":[{"key":"ABC","name":"Alpha"},{"key":"DEF","name":"Delta"}]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"startAt":2,"maxResults":50,"isLast":true,"values":[{"key":"GHI","name":"Gamma"}]}`)
		})
		c := jiraOpen(Conn{BaseURL: srv.URL, Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
		ps, err := c.Projects(context.Background())
		if err != nil || len(ps) != 3 || ps[2].Key != "GHI" || ps[0].URL != srv.URL+"/browse/ABC" {
			t.Fatalf("%+v %v", ps, err)
		}
	})
	t.Run("server", func(t *testing.T) {
		f, srv := atlNewFake(t, "/jira")
		f.on("GET /rest/api/2/project", 200, `[{"id":"10000","key":"ABC","name":"Alpha","projectTypeKey":"software"},{"id":"10001","key":"DEF","name":"Delta"}]`)
		c := jiraOpen(Conn{BaseURL: srv.URL + "/jira", Deployment: SelfHosted}, Credentials{FieldToken: "p"})
		ps, err := c.Projects(context.Background())
		if err != nil || len(ps) != 2 || ps[1].Name != "Delta" || ps[1].URL != srv.URL+"/jira/browse/DEF" {
			t.Fatalf("%+v %v", ps, err)
		}
	})
}

func TestJiraLink(t *testing.T) {
	cloud := Conn{Kind: Jira, BaseURL: "https://acme.atlassian.net", Deployment: Cloud}
	server := Conn{Kind: Jira, BaseURL: "https://tracker.acme.test", Deployment: SelfHosted}
	ctx := Conn{Kind: Jira, BaseURL: "https://acme.test/jira", Deployment: SelfHosted}
	cases := []struct {
		conn Conn
		link string
		key  string
	}{
		{cloud, "https://acme.atlassian.net/browse/ABC-123", "ABC-123"},
		{cloud, "https://acme.atlassian.net/browse/ABC-123?focusedCommentId=10001", "ABC-123"},
		{cloud, "https://acme.atlassian.net/jira/software/projects/ABC/boards/1?selectedIssue=ABC-9", "ABC-9"},
		{cloud, "https://acme.atlassian.net/jira/software/c/projects/ABC/issues/ABC-77", "ABC-77"},
		{cloud, "https://acme.atlassian.net/jira/software/c/projects/ABC/boards/1/backlog?selectedIssue=abc-5", "ABC-5"},
		{server, "https://tracker.acme.test/browse/PRJ_X-4", "PRJ_X-4"},
		{server, "https://tracker.acme.test/projects/PRJ/issues/PRJ-12", "PRJ-12"},
		{server, "https://tracker.acme.test/projects/PRJ/issues/PRJ-12?filter=allopenissues", "PRJ-12"},
		{server, "https://tracker.acme.test/secure/RapidBoard.jspa?rapidView=3&selectedIssue=PRJ-8", "PRJ-8"},
		{ctx, "https://acme.test/jira/browse/ABC-1", "ABC-1"},
		{ctx, "https://acme.test/jira/projects/ABC/issues/ABC-2", "ABC-2"},
	}
	p := &jiraProvider{}
	for _, c := range cases {
		u, _ := url.Parse(c.link)
		got, ok := p.Link(c.conn, u)
		if !ok || got != (LinkTarget{Kind: "issue", Key: c.key}) {
			t.Errorf("%s -> %+v %v", c.link, got, ok)
		}
	}
	for _, c := range []struct {
		conn Conn
		link string
	}{
		{cloud, "https://acme.atlassian.net/wiki/spaces/ENG/pages/123/Title"},
		{cloud, "https://acme.atlassian.net/jira/software/projects/ABC/boards/1"},
		{cloud, "https://acme.atlassian.net/browse/ABC"},
		{cloud, "https://other.atlassian.net/browse/ABC-1"},
		{server, "https://tracker.acme.test/browse/PRJ/overview"},
		{ctx, "https://acme.test/browse/ABC-1"},
		{ctx, "https://acme.test/jirax/browse/ABC-1"},
	} {
		u, _ := url.Parse(c.link)
		if got, ok := p.Link(c.conn, u); ok {
			t.Errorf("%s claimed: %+v", c.link, got)
		}
	}
}

func TestJiraProviderRegistered(t *testing.T) {
	if p := ProviderOf(Jira); p == nil || p.Kind() != Jira {
		t.Fatal("jira is not registered")
	}
	cl := ProviderOf(Jira).Open(Conn{BaseURL: "https://x.test"}, Credentials{}, NewHTTP(nil))
	if _, ok := cl.(IssueSource); !ok {
		t.Error("a Jira client must be an IssueSource")
	}
}

// A deep path that the app redirects to its root login page must not win as the context path.
func TestJiraDetectIgnoresRedirectedProbes(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /rest/api/2/serverInfo", 200, fmt.Sprintf(jiraServerInfo912, "SELF"))
	redirect := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login.jsp", http.StatusFound) }
	f.on("GET /login.jsp", 200, `<html><head><meta name="application-name" content="JIRA" data-version="9.12.4"></head></html>`)
	for _, r := range []string{"/team/proj/rest/api/2/serverInfo", "/team/proj/login.jsp", "/team/rest/api/2/serverInfo", "/team/login.jsp", "/team/proj/rest/applinks/1.0/manifest", "/team/rest/applinks/1.0/manifest"} {
		f.handle("GET "+r, redirect)
	}
	d := jiraDetect(t, srv.URL+"/team/proj/some/page")
	if d == nil || d.BaseURL != srv.URL {
		t.Fatalf("%+v", d)
	}
}
