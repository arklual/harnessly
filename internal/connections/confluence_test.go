package connections

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	confSpaceList = `{"results":[{"id":1,"key":"ENG","name":"Engineering","type":"global"}],"start":0,"limit":1,"size":1,"_links":{"base":"%s","context":"/confluence"}}`
	confManifest  = `<?xml version="1.0" encoding="UTF-8"?><manifest xmlns="http://www.atlassian.com/applinks/manifest"><id>abc</id><name>Confluence</name><typeId>confluence</typeId><version>%s</version><buildNumber>1</buildNumber></manifest>`
	confStorage   = `<h2>Plan</h2><p>Ship <strong>it</strong> via <a href="https://example.com/x">the docs</a>.</p><ul><li>one</li><li>two</li></ul>` +
		`<ac:structured-macro ac:name="info" ac:schema-version="1"><ac:parameter ac:name="title">Heads up</ac:parameter><ac:rich-text-body><p>Read this</p></ac:rich-text-body></ac:structured-macro>` +
		`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[if a < b {
	run()
}]]></ac:plain-text-body></ac:structured-macro>` +
		`<ac:structured-macro ac:name="toc"><ac:parameter ac:name="maxLevel">3</ac:parameter></ac:structured-macro>` +
		`<table><tbody><tr><th>Key</th><th>Value</th></tr><tr><td>a</td><td>1</td></tr></tbody></table>` +
		`<ac:task-list><ac:task><ac:task-id>1</ac:task-id><ac:task-status>complete</ac:task-status><ac:task-body>done thing</ac:task-body></ac:task></ac:task-list>` +
		`<p>See <ac:link><ri:page ri:content-title="Other page"/></ac:link> and <ac:link><ri:user ri:account-id="x"/></ac:link>, ` +
		`<ac:image><ri:attachment ri:filename="diagram.png"/></ac:image> on <time datetime="2025-03-04"/>. Fish &amp; chips</p>`
)

func confDetect(t *testing.T, raw string) *Detection {
	t.Helper()
	d, err := (&confluenceProvider{}).Detect(context.Background(), NewProber(NewHTTP(nil)), atlParse(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func confOpen(conn Conn, cred Credentials) *confClient {
	return (&confluenceProvider{}).Open(conn, cred, NewHTTP(nil)).(*confClient)
}

func TestConfluenceDetectCloud(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /_edge/tenant_info", 200, `{"cloudId":"0a1b-2c3d"}`)
	d, err := (&confluenceProvider{}).Detect(context.Background(), NewProber(atlRewriteHTTP(srv)), atlParse(t, "https://acme.atlassian.net/wiki/spaces/ENG/pages/123/Title"))
	if err != nil || d == nil {
		t.Fatalf("%v %v", d, err)
	}
	if d.Kind != Confluence || d.Deployment != Cloud || d.BaseURL != "https://acme.atlassian.net/wiki" || d.Extra["cloudId"] != "0a1b-2c3d" {
		t.Errorf("%+v", d)
	}
	if !atlSame(atlMethodIDs(d), []string{"api-token"}) || d.Methods[0].TokenURL != atlCloudTokenURL || len(d.Capabilities) != 1 || d.Capabilities[0] != Pages {
		t.Errorf("%+v", d)
	}
	if f.requests("/rest/api") != 0 {
		t.Error("the fast path probed the API")
	}
	// A Jira link on the same site is not Confluence's.
	if d, _ := (&confluenceProvider{}).Detect(context.Background(), NewProber(atlRewriteHTTP(srv)), atlParse(t, "https://acme.atlassian.net/browse/ABC-1")); d != nil {
		t.Errorf("jira link claimed: %+v", d)
	}
}

func TestConfluenceDetectCloudCustomDomain(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /_edge/tenant_info", 200, `{"cloudId":"cid-9"}`)
	d := confDetect(t, srv.URL+"/wiki/spaces/ENG/overview")
	if d == nil || d.Deployment != Cloud || d.BaseURL != srv.URL+"/wiki" {
		t.Fatalf("%+v", d)
	}
	if d := confDetect(t, srv.URL+"/browse/ABC-1"); d != nil {
		t.Errorf("not under /wiki: %+v", d)
	}
}

func TestConfluenceDetectServerVersions(t *testing.T) {
	cases := []struct {
		name, prefix, version, edition string
		methods                        []string
		viaManifest                    bool
	}{
		{"8.5 under /confluence", "/confluence", "8.5.4", "server", []string{"pat", "basic"}, true},
		{"6.15 basic only", "", "6.15.2", "server", []string{"basic"}, true},
		{"7.8 basic only", "/wiki", "7.8.0", "server", []string{"basic"}, true},
		{"7.9 token", "", "7.9.0", "server", []string{"pat", "basic"}, true},
		{"9.2 data center", "/confluence", "9.2.1", "data-center", []string{"pat", "basic"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, srv := atlNewFake(t, c.prefix)
			f.on("GET /rest/applinks/1.0/manifest", 200, fmt.Sprintf(confManifest, c.version))
			d := confDetect(t, srv.URL+c.prefix+"/display/ENG/Home")
			if d == nil {
				t.Fatal("not detected")
			}
			if d.Kind != Confluence || d.Deployment != SelfHosted || d.Version != c.version || d.Edition != c.edition || d.BaseURL != srv.URL+c.prefix {
				t.Errorf("%+v", d)
			}
			if !atlSame(atlMethodIDs(d), c.methods) || !d.Methods[0].Recommended {
				t.Errorf("methods %v", atlMethodIDs(d))
			}
			if c.methods[0] == "pat" && d.Methods[0].TokenURL != srv.URL+c.prefix+"/plugins/personalaccesstokens/usertokens.action" {
				t.Errorf("token url %s", d.Methods[0].TokenURL)
			}
		})
	}
}

func TestConfluenceDetectManifestDenied(t *testing.T) {
	t.Run("space list shape and login page version", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/space", 200, fmt.Sprintf(confSpaceList, "SELF"))
		f.on("GET /login.action", 200, `<html><head><meta name="ajs-version-number" content="6.15.2"><meta name="ajs-context-path" content=""></head><body>Log in - Confluence</body></html>`)
		d := confDetect(t, srv.URL+"/display/ENG/Home")
		if d == nil || d.Version != "6.15.2" || !atlSame(atlMethodIDs(d), []string{"basic"}) {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("401 with Confluence header, no version", func(t *testing.T) {
		f, srv := atlNewFake(t, "/confluence")
		f.hdr.Set("X-Confluence-Request-Time", "1700000000000")
		f.hdr.Set("X-ANODEID", "node-2")
		f.on("GET /rest/api/space", 401, `{"statusCode":401,"message":"Authentication failed"}`)
		d := confDetect(t, srv.URL+"/confluence/pages/viewpage.action?pageId=5")
		if d == nil || d.Version != "" || d.Edition != "data-center" || d.BaseURL != srv.URL+"/confluence" || !atlSame(atlMethodIDs(d), []string{"pat", "basic"}) {
			t.Fatalf("%+v", d)
		}
	})
}

func TestConfluenceDetectNotConfluence(t *testing.T) {
	t.Run("jira", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.hdr.Set("X-AREQUESTID", "x")
		f.on("GET /rest/applinks/1.0/manifest", 200, `<?xml version="1.0"?><manifest><typeId>jira</typeId><version>9.12.4</version></manifest>`)
		f.on("GET /login.action", 200, `<html><head><meta name="ajs-version-number" content="9.12.4"></head><body>Jira</body></html>`)
		if d := confDetect(t, srv.URL+"/display/X/Y"); d != nil {
			t.Errorf("%+v", d)
		}
	})
	t.Run("jira login page alone", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /login.action", 200, `<html><head><meta name="application-name" content="JIRA"><meta name="ajs-version-number" content="9.12.4"></head><body>confluence mentioned</body></html>`)
		if d := confDetect(t, srv.URL); d != nil {
			t.Errorf("%+v", d)
		}
	})
	t.Run("spa answering html", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/space", 200, "<html>app</html>")
		f.on("GET /login.action", 200, "<html>app</html>")
		if d := confDetect(t, srv.URL); d != nil {
			t.Errorf("%+v", d)
		}
	})
}

func TestConfluenceMe(t *testing.T) {
	t.Run("cloud", func(t *testing.T) {
		f, srv := atlNewFake(t, "/wiki")
		f.on("GET /rest/api/user/current", 200, `{"type":"known","accountId":"5b10","accountType":"atlassian","email":"ann@acme.test","publicName":"Ann","displayName":"Ann Lee","profilePicture":{"path":"/wiki/aa-avatar/5b10","width":48}}`)
		c := confOpen(Conn{BaseURL: srv.URL + "/wiki", Deployment: Cloud}, Credentials{FieldEmail: "ann@acme.test", FieldToken: "tok"})
		me, err := c.Me(context.Background())
		if err != nil || me.Name != "Ann Lee" || me.ID != "5b10" || me.Avatar != srv.URL+"/wiki/aa-avatar/5b10" {
			t.Fatalf("%+v %v", me, err)
		}
		if f.auth("GET /rest/api/user/current") != Basic("ann@acme.test", "tok") {
			t.Error("auth")
		}
	})
	t.Run("server pat", func(t *testing.T) {
		f, srv := atlNewFake(t, "/confluence")
		f.on("GET /rest/api/user/current", 200, `{"type":"known","username":"ann","userKey":"8a7f","displayName":"Ann Lee","profilePicture":{"path":"/confluence/images/icons/profilepics/default.svg"}}`)
		c := confOpen(Conn{BaseURL: srv.URL + "/confluence", Deployment: SelfHosted, Version: "8.5.4"}, Credentials{FieldToken: "pat"})
		me, err := c.Me(context.Background())
		if err != nil || me.Login != "ann" || me.ID != "8a7f" {
			t.Fatalf("%+v %v", me, err)
		}
		if f.auth("GET /rest/api/user/current") != "Bearer pat" {
			t.Error("a PAT is sent as Bearer")
		}
	})
	t.Run("anonymous", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/user/current", 200, `{"type":"anonymous","displayName":"Anonymous"}`)
		c := confOpen(Conn{BaseURL: srv.URL, Deployment: SelfHosted}, Credentials{FieldUsername: "ann", FieldPassword: "bad"})
		if _, err := c.Me(context.Background()); !errors.Is(err, ErrDenied) {
			t.Errorf("%v", err)
		}
	})
	t.Run("401", func(t *testing.T) {
		f, srv := atlNewFake(t, "")
		f.on("GET /rest/api/user/current", 401, `{}`)
		c := confOpen(Conn{BaseURL: srv.URL}, Credentials{FieldToken: "x"})
		if _, err := c.Me(context.Background()); !errors.Is(err, ErrDenied) {
			t.Errorf("%v", err)
		}
	})
}

func TestConfluenceCQL(t *testing.T) {
	cases := []struct {
		q    Query
		want string
	}{
		{Query{}, `type in (page, blogpost) ORDER BY lastmodified DESC`},
		{Query{Text: `say "hi"`}, `type in (page, blogpost) AND text ~ "say \"hi\"" ORDER BY lastmodified DESC`},
		{Query{Text: "x", Project: "ENG", Mine: true}, `type in (page, blogpost) AND text ~ "x" AND creator = currentUser() AND space = "ENG" ORDER BY lastmodified DESC`},
		{Query{Native: `label = "adr"`, Text: "ignored"}, `label = "adr"`},
	}
	for _, c := range cases {
		if got := confCQL(c.q); got != c.want {
			t.Errorf("got  %s\nwant %s", got, c.want)
		}
	}
}

func TestConfluenceSearch(t *testing.T) {
	results := `{"results":[
 {"id":"123","type":"page","status":"current","title":"Release plan","space":{"key":"ENG","name":"Engineering"},"version":{"when":"2025-03-04T05:06:07.000Z","by":{"displayName":"Ann Lee","username":"ann"}},"_links":{"webui":"%s","self":"x"}},
 {"id":"124","type":"blogpost","title":"News","space":{"key":"ENG"},"version":{"when":"2025-03-05T05:06:07.000+0000"},"_links":{}}],
 "start":0,"limit":25,"size":2,"_links":{"base":"https://internal.invalid/confluence"}}`
	t.Run("cloud", func(t *testing.T) {
		f, srv := atlNewFake(t, "/wiki")
		f.on("GET /rest/api/content/search", 200, fmt.Sprintf(results, "/spaces/ENG/pages/123/Release+plan"))
		c := confOpen(Conn{BaseURL: srv.URL + "/wiki", Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
		got, err := c.SearchPages(context.Background(), Query{Text: "release", Limit: 10})
		if err != nil || len(got) != 2 {
			t.Fatalf("%+v %v", got, err)
		}
		if got[0].ID != "123" || got[0].Title != "Release plan" || got[0].Space != "ENG" || got[0].URL != srv.URL+"/wiki/spaces/ENG/pages/123/Release+plan" ||
			got[0].Author == nil || got[0].Author.Name != "Ann Lee" || got[0].Updated.IsZero() || got[0].Body != "" {
			t.Errorf("%+v", got[0])
		}
		if got[1].URL != srv.URL+"/wiki/pages/viewpage.action?pageId=124" {
			t.Errorf("page without webui: %s", got[1].URL)
		}
		q := f.log[len(f.log)-1]
		if !strings.Contains(q, "cql=type+in+%28page%2C+blogpost%29+AND+text+~+%22release%22+ORDER+BY+lastmodified+DESC") || !strings.Contains(q, "limit=10") || !strings.Contains(q, "expand=space%2Cversion") {
			t.Errorf("request %s", q)
		}
	})
	t.Run("server", func(t *testing.T) {
		f, srv := atlNewFake(t, "/confluence")
		f.on("GET /rest/api/content/search", 200, fmt.Sprintf(results, "/display/ENG/Release+plan"))
		c := confOpen(Conn{BaseURL: srv.URL + "/confluence", Deployment: SelfHosted, Version: "8.5.4"}, Credentials{FieldToken: "p"})
		got, err := c.SearchPages(context.Background(), Query{Native: `space = "ENG"`, Limit: 1})
		if err != nil || len(got) != 1 || got[0].URL != srv.URL+"/confluence/display/ENG/Release+plan" {
			t.Fatalf("%+v %v", got, err)
		}
		if !strings.Contains(f.log[len(f.log)-1], "cql=space+%3D+%22ENG%22") {
			t.Errorf("native CQL: %s", f.log[len(f.log)-1])
		}
	})
}

func TestConfluenceStorageText(t *testing.T) {
	got := confStorageText(confStorage)
	for _, want := range []string{
		"## Plan",
		"Ship it via [the docs](https://example.com/x).",
		"- one\n- two",
		"Read this",
		"```go\nif a < b {\n\trun()\n}\n```",
		"Key | Value |",
		"a | 1 |",
		"[x] done thing",
		"Other page", "@user", "[image: diagram.png]", "2025-03-04", "Fish & chips",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"Heads up", "maxLevel", "ac:", "ri:", "CDATA"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q left in:\n%s", bad, got)
		}
	}
}

func TestConfluencePageCloud(t *testing.T) {
	v2 := `{"id":"123","status":"current","title":"Release plan","spaceId":"98765","authorId":"5b10","version":{"number":4,"createdAt":"2025-03-04T05:06:07.000Z","authorId":"5b10"},
 "body":{"storage":{"representation":"storage","value":"<p>Hello <strong>wiki</strong></p>"}},"_links":{"webui":"/spaces/ENG/pages/123/Release+plan","base":"https://acme.atlassian.net/wiki"}}`
	f, srv := atlNewFake(t, "/wiki")
	f.on("GET /api/v2/pages/123", 200, v2)
	f.on("GET /api/v2/spaces/98765", 200, `{"id":"98765","key":"ENG","name":"Engineering"}`)
	f.on("GET /rest/api/user", 200, `{"type":"known","accountId":"5b10","displayName":"Ann Lee"}`)
	c := confOpen(Conn{BaseURL: srv.URL + "/wiki", Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
	pg, err := c.Page(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if pg.Title != "Release plan" || pg.Space != "ENG" || pg.Body != "Hello wiki" || pg.Author == nil || pg.Author.Name != "Ann Lee" ||
		pg.URL != srv.URL+"/wiki/spaces/ENG/pages/123/Release+plan" || pg.Updated.IsZero() {
		t.Errorf("%+v", pg)
	}
	if !strings.Contains(strings.Join(f.log, "\n"), "/api/v2/pages/123?body-format=storage") {
		t.Errorf("log %v", f.log)
	}
	if f.requests("/rest/api/content/") != 0 {
		t.Error("v1 was used though v2 answered")
	}
	// The space key is kept for the next page of the same space.
	f.on("GET /api/v2/pages/124", 200, strings.Replace(v2, `"id":"123"`, `"id":"124"`, 1))
	if _, err := c.Page(context.Background(), "124"); err != nil || f.requests("/api/v2/spaces/98765") != 1 {
		t.Errorf("space lookups %d %v", f.requests("/api/v2/spaces/98765"), err)
	}
}

func TestConfluencePageCloudFallsBackToV1(t *testing.T) {
	f, srv := atlNewFake(t, "/wiki")
	// v2 does not serve blog posts.
	f.on("GET /api/v2/pages/555", 404, `{"errors":[{"status":404}]}`)
	f.on("GET /rest/api/content/555", 200, `{"id":"555","type":"blogpost","title":"News","space":{"key":"ENG"},"version":{"when":"2025-03-04T05:06:07.000Z","by":{"displayName":"Bob"}},
 "history":{"createdBy":{"displayName":"Carol"}},"body":{"storage":{"value":"<p>old way</p>"}},"_links":{"webui":"/spaces/ENG/blog/2025/03/04/555/News"}}`)
	f.on("GET /api/v2/pages/666", 403, `{}`)
	c := confOpen(Conn{BaseURL: srv.URL + "/wiki", Deployment: Cloud}, Credentials{FieldEmail: "a@b.c", FieldToken: "t"})
	pg, err := c.Page(context.Background(), "555")
	if err != nil || pg.Space != "ENG" || pg.Body != "old way" || pg.Author.Name != "Carol" {
		t.Fatalf("%+v %v", pg, err)
	}
	if !strings.Contains(strings.Join(f.log, "\n"), "expand=body.storage%2Cversion%2Cspace%2Chistory") {
		t.Errorf("v1 expand: %v", f.log)
	}
	// Refused credentials are not retried on the other API.
	if _, err := c.Page(context.Background(), "666"); !errors.Is(err, ErrDenied) || f.requests("/rest/api/content/666") != 0 {
		t.Errorf("%v %v", err, f.log)
	}
	if _, err := c.Page(context.Background(), "777"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing page: %v", err)
	}
}

func TestConfluencePageServer(t *testing.T) {
	f, srv := atlNewFake(t, "/confluence")
	f.on("GET /rest/api/content/98304", 200, `{"id":"98304","type":"page","status":"current","title":"Runbook","space":{"key":"OPS","name":"Operations"},
 "history":{"latest":true,"createdBy":{"type":"known","username":"ann","displayName":"Ann Lee"},"createdDate":"2024-01-02T03:04:05.000+0000"},
 "version":{"by":{"displayName":"Bob"},"when":"2025-03-04T05:06:07.000+0100","number":9},
 "body":{"storage":{"value":"<h1>Runbook</h1><p>Restart it.</p>","representation":"storage"}},"_links":{"webui":"/display/OPS/Runbook","base":"http://internal:8090/confluence"}}`)
	c := confOpen(Conn{BaseURL: srv.URL + "/confluence", Deployment: SelfHosted, Version: "8.5.4"}, Credentials{FieldToken: "p"})
	pg, err := c.Page(context.Background(), "98304")
	if err != nil {
		t.Fatal(err)
	}
	if pg.Space != "OPS" || pg.Author.Name != "Ann Lee" || pg.URL != srv.URL+"/confluence/display/OPS/Runbook" || !strings.Contains(pg.Body, "# Runbook") || !strings.Contains(pg.Body, "Restart it.") {
		t.Errorf("%+v", pg)
	}
	if f.requests("/api/v2") != 0 {
		t.Error("Server has no v2")
	}
	if _, err := c.Page(context.Background(), "../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
}

func TestConfluencePageByTitle(t *testing.T) {
	f, srv := atlNewFake(t, "/confluence")
	f.on("GET /rest/api/content", 200, `{"results":[{"id":"42","type":"page","title":"Release Plan 2025","space":{"key":"ENG"},"version":{"when":"2025-03-04T05:06:07.000Z"},"body":{"storage":{"value":"<p>found</p>"}},"_links":{"webui":"/display/ENG/Release+Plan+2025"}}],"size":1}`)
	c := confOpen(Conn{BaseURL: srv.URL + "/confluence", Deployment: SelfHosted}, Credentials{FieldUsername: "ann", FieldPassword: "pw"})
	pg, err := c.Page(context.Background(), "title:ENG/Release Plan 2025")
	if err != nil || pg.ID != "42" || pg.Body != "found" {
		t.Fatalf("%+v %v", pg, err)
	}
	if q := f.log[len(f.log)-1]; !strings.Contains(q, "spaceKey=ENG") || !strings.Contains(q, "title=Release+Plan+2025") {
		t.Errorf("request %s", q)
	}
	f.on("GET /rest/api/content", 200, `{"results":[],"size":0}`)
	if _, err := c.Page(context.Background(), "title:ENG/Nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("%v", err)
	}
	if _, err := c.Page(context.Background(), "title:nospace"); !errors.Is(err, ErrNotFound) {
		t.Errorf("%v", err)
	}
}

func TestConfluenceLink(t *testing.T) {
	cloud := Conn{Kind: Confluence, BaseURL: "https://acme.atlassian.net/wiki", Deployment: Cloud}
	dc := Conn{Kind: Confluence, BaseURL: "https://corp.test/confluence", Deployment: SelfHosted}
	root := Conn{Kind: Confluence, BaseURL: "https://wiki.corp.test", Deployment: SelfHosted}
	cases := []struct {
		conn Conn
		link string
		kind string
		key  string
	}{
		{cloud, "https://acme.atlassian.net/wiki/spaces/ENG/pages/123456/Release+plan", "page", "123456"},
		{cloud, "https://acme.atlassian.net/wiki/spaces/ENG/pages/123456", "page", "123456"},
		{cloud, "https://acme.atlassian.net/wiki/pages/viewpage.action?pageId=777", "page", "777"},
		{dc, "https://corp.test/confluence/pages/viewpage.action?pageId=98304", "page", "98304"},
		{dc, "https://corp.test/confluence/spaces/OPS/pages/98304/Runbook", "page", "98304"},
		{dc, "https://corp.test/confluence/display/OPS/Run+book", "page", "title:OPS/Run book"},
		{dc, "https://corp.test/confluence/display/OPS/Caf%C3%A9%20menu", "page", "title:OPS/Café menu"},
		{root, "https://wiki.corp.test/display/ENG/Home", "page", "title:ENG/Home"},
		{root, "https://wiki.corp.test/pages/viewpage.action?pageId=5", "page", "5"},
	}
	p := &confluenceProvider{}
	for _, c := range cases {
		u, _ := url.Parse(c.link)
		got, ok := p.Link(c.conn, u)
		if !ok || got != (LinkTarget{Kind: c.kind, Key: c.key}) {
			t.Errorf("%s -> %+v %v", c.link, got, ok)
		}
	}
	for _, c := range []struct {
		conn Conn
		link string
	}{
		{cloud, "https://acme.atlassian.net/wiki/spaces/ENG/overview"},
		{cloud, "https://acme.atlassian.net/wiki/x/AbCd"},
		{cloud, "https://acme.atlassian.net/browse/ABC-1"},
		{dc, "https://corp.test/display/OPS/Home"},
		{dc, "https://corp.test/confluence/display/OPS"},
		{dc, "https://corp.test/confluence/display/OPS/2025/03/04/News"},
		{dc, "https://corp.test/confluence/pages/viewpage.action?pageId=abc"},
		{root, "https://other.test/display/ENG/Home"},
	} {
		u, _ := url.Parse(c.link)
		if got, ok := p.Link(c.conn, u); ok {
			t.Errorf("%s claimed: %+v", c.link, got)
		}
	}
}

func TestConfluenceProviderRegistered(t *testing.T) {
	p := ProviderOf(Confluence)
	if p == nil {
		t.Fatal("confluence is not registered")
	}
	cl := p.Open(Conn{BaseURL: "https://x.test"}, Credentials{}, NewHTTP(nil))
	if _, ok := cl.(PageSource); !ok {
		t.Error("a Confluence client must be a PageSource")
	}
}

func TestConfluenceDetectIgnoresRedirectedProbes(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /rest/applinks/1.0/manifest", 200, fmt.Sprintf(confManifest, "8.5.4"))
	f.on("GET /login.action", 200, `<html><head><meta name="ajs-version-number" content="8.5.4"></head><body>Confluence</body></html>`)
	redirect := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login.action", http.StatusFound) }
	for _, r := range []string{"/a/b/rest/applinks/1.0/manifest", "/a/b/rest/api/space", "/a/b/login.action", "/a/rest/applinks/1.0/manifest", "/a/rest/api/space", "/a/login.action"} {
		f.handle("GET "+r, redirect)
	}
	d := confDetect(t, srv.URL+"/a/b/c")
	if d == nil || d.BaseURL != srv.URL {
		t.Fatalf("%+v", d)
	}
}
