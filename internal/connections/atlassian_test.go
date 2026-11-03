package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// atlFake is a pretend Atlassian instance: routes by "METHOD /path" below an optional context path, every request
// logged. Unknown routes answer 404.
type atlFake struct {
	prefix string
	// hdr is sent with every answer (X-ANODEID, X-AREQUESTID, …).
	hdr http.Header

	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	log    []string
	bodies map[string]string
	auths  map[string]string
}

func atlNewFake(t *testing.T, prefix string) (*atlFake, *httptest.Server) {
	t.Helper()
	f := &atlFake{prefix: prefix, hdr: http.Header{}, routes: map[string]http.HandlerFunc{}, bodies: map[string]string{}, auths: map[string]string{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *atlFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	path, ok := strings.CutPrefix(r.URL.Path, f.prefix)
	key := r.Method + " " + path
	f.mu.Lock()
	f.log = append(f.log, key+"?"+r.URL.RawQuery)
	f.bodies[key] = string(raw)
	f.auths[key] = r.Header.Get("Authorization")
	h := f.routes[key]
	f.mu.Unlock()
	for k, v := range f.hdr {
		w.Header()[k] = v
	}
	if !ok || h == nil {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

// on answers a route with a fixed status and body; the type follows the body's first character.
func (f *atlFake) on(route string, status int, body string) *atlFake {
	return f.handle(route, func(w http.ResponseWriter, _ *http.Request) {
		switch {
		case strings.HasPrefix(body, "{") || strings.HasPrefix(body, "["):
			w.Header().Set("Content-Type", "application/json")
		case strings.HasPrefix(body, "<?xml"):
			w.Header().Set("Content-Type", "application/xml")
		default:
			w.Header().Set("Content-Type", "text/html")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func (f *atlFake) handle(route string, h http.HandlerFunc) *atlFake {
	f.mu.Lock()
	f.routes[route] = h
	f.mu.Unlock()
	return f
}

// requests counts the logged requests whose "METHOD path?query" contains s.
func (f *atlFake) requests(s string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, l := range f.log {
		if strings.Contains(l, s) {
			n++
		}
	}
	return n
}

func (f *atlFake) body(route string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[route]
}

func (f *atlFake) auth(route string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auths[route]
}

// atlRewrite sends every request to the test server whatever host it names: how fixed hosts (atlassian.net) are tested.
type atlRewrite struct{ target *url.URL }

func (r atlRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.URL.Scheme, c.URL.Host = r.target.Scheme, r.target.Host
	res, err := http.DefaultTransport.RoundTrip(c)
	if res != nil {
		res.Request = req // the answer belongs to the address that was asked
	}
	return res, err
}

func atlRewriteHTTP(srv *httptest.Server) *HTTP {
	u, _ := url.Parse(srv.URL)
	return NewHTTP(atlRewrite{target: u})
}

func atlParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func atlMethodIDs(d *Detection) []string {
	var ids []string
	for _, m := range d.Methods {
		ids = append(ids, m.ID)
	}
	return ids
}

func atlSame(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func TestAtlassianAuth(t *testing.T) {
	cases := []struct {
		name string
		cred Credentials
		want string
	}{
		{"cloud", Credentials{FieldEmail: "me@x.com", FieldToken: "tok"}, Basic("me@x.com", "tok")},
		{"basic", Credentials{FieldUsername: "ann", FieldPassword: "pw"}, Basic("ann", "pw")},
		{"pat", Credentials{FieldToken: "pat123"}, "Bearer pat123"},
		{"empty", Credentials{}, ""},
		{"half", Credentials{FieldUsername: "ann"}, ""},
	}
	for _, c := range cases {
		if got := atlAuth(c.cred); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAtlassianVersion(t *testing.T) {
	cases := []struct {
		v           string
		known, atLs bool
	}{
		{"9.12.4", true, true}, {"8.14.0", true, true}, {"8.13.9", true, false}, {"7.13.0", true, false},
		{"11.0.1", true, true}, {"8.14.0-m01", true, true}, {"", false, false}, {"weird", false, false},
	}
	for _, c := range cases {
		known, ge := atlAtLeast(c.v, 8, 14)
		if known != c.known || ge != c.atLs {
			t.Errorf("%q: known=%v ge=%v", c.v, known, ge)
		}
	}
	if atlEdition("9.12.4", http.Header{}, 10) != "server" || atlEdition("10.3.0", http.Header{}, 10) != "data-center" {
		t.Error("edition by version")
	}
	if atlEdition("9.12.4", http.Header{"X-Anodeid": {"node1"}}, 10) != "data-center" {
		t.Error("edition by node header")
	}
}

const atlADFDoc = `{"type":"doc","version":1,"content":[
 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Steps"}]},
 {"type":"paragraph","content":[
   {"type":"text","text":"Open "},
   {"type":"text","text":"the page ","marks":[{"type":"strong"}]},
   {"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://example.com/d"}}]},
   {"type":"text","text":" and ping "},
   {"type":"mention","attrs":{"id":"a1","text":"@Ann"}},
   {"type":"hardBreak"},
   {"type":"text","text":"call()","marks":[{"type":"code"}]}]},
 {"type":"bulletList","content":[
   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]},
     {"type":"orderedList","content":[
       {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested one"}]}]},
       {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested two"}]}]}]}]},
   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}]},
 {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"fmt.Println(1)"}]},
 {"type":"panel","attrs":{"panelType":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"careful"}]}]},
 {"type":"table","content":[
   {"type":"tableRow","content":[
     {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]},
     {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}]},
   {"type":"tableRow","content":[
     {"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]},
     {"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"x|y"}]}]}]}]},
 {"type":"taskList","content":[{"type":"taskItem","attrs":{"state":"DONE"},"content":[{"type":"text","text":"shipped"}]}]},
 {"type":"mediaSingle","content":[{"type":"media","attrs":{"type":"file","id":"m1","alt":"shot.png"}}]},
 {"type":"rule"}]}`

func TestAtlassianADF(t *testing.T) {
	got := atlADF(json.RawMessage(atlADFDoc))
	for _, want := range []string{
		"## Steps",
		"Open **the page** [docs](https://example.com/d) and ping @Ann\n`call()`",
		"- first\n  1. nested one\n  2. nested two\n- second",
		"```go\nfmt.Println(1)\n```",
		"> **Warning**\n>\n> careful",
		"| A | B |\n| --- | --- |\n| 1 | x\\|y |",
		"- [x] shipped",
		"[attachment: shot.png]",
		"---",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if atlADF(nil) != "" || atlADF(json.RawMessage(`"x"`)) != "" {
		t.Error("unreadable ADF must give empty text")
	}
}

func TestAtlassianWikiMarkup(t *testing.T) {
	in := "h2. Problem\r\nThe *login* button is _broken_ for [~jsmith] see [the docs|https://example.com/d] and [http://a.b/c].\n" +
		"* one\n** two\n# step\n## sub\n\n{code:java}\nint x = 1; // *not bold*\n{code}\n" +
		"{quote}\nsaid so\n{quote}\n" +
		"||h1||h2||\n|a|b|\n" +
		"{panel:title=Note}\nbody\n{panel}\n" +
		"Use {{foo()}} and !shot.png|thumbnail! ok, snake_case_name stays, 2 * 3 * 4 too. {color:red}red{color}\n----\n"
	got := atlWiki(in)
	for _, want := range []string{
		"## Problem",
		"The **login** button is *broken* for @jsmith see [the docs](https://example.com/d) and <http://a.b/c>.",
		"- one\n  - two\n1. step\n  1. sub",
		"```java\nint x = 1; // *not bold*\n```",
		"> said so",
		"| h1 | h2 |\n| --- | --- |\n| a | b |",
		"**Note**\nbody",
		"`foo()`", "[image: shot.png]", "snake_case_name", "2 * 3 * 4", "red\n", "---",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "{panel") || strings.Contains(got, "{color") || strings.Contains(got, "\r") {
		t.Errorf("markup left over:\n%s", got)
	}
}

// Both providers look at the same Atlassian Cloud site: exactly one claims each link.
func TestAtlassianCloudRouting(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /_edge/tenant_info", 200, `{"cloudId":"0a1b-2c3d"}`)
	p := NewProber(atlRewriteHTTP(srv))
	cases := []struct {
		link string
		want Kind
	}{
		{"https://acme.atlassian.net", Jira},
		{"https://acme.atlassian.net/browse/ABC-1", Jira},
		{"https://acme.atlassian.net/jira/software/c/projects/ABC/boards/1", Jira},
		{"https://acme.atlassian.net/wiki", Confluence},
		{"https://acme.atlassian.net/wiki/spaces/ENG/pages/123/Title", Confluence},
		{"https://acme.atlassian.net/wikis", Jira},
	}
	for _, c := range cases {
		u := atlParse(t, c.link)
		var claimed []Kind
		for _, pr := range []Provider{&jiraProvider{}, &confluenceProvider{}} {
			d, err := pr.Detect(context.Background(), p, u)
			if err != nil {
				t.Fatal(err)
			}
			if d != nil {
				claimed = append(claimed, d.Kind)
				if d.Deployment != Cloud || d.Extra["cloudId"] != "0a1b-2c3d" {
					t.Errorf("%s: %+v", c.link, d)
				}
			}
		}
		if len(claimed) != 1 || claimed[0] != c.want {
			t.Errorf("%s claimed by %v, want only %s", c.link, claimed, c.want)
		}
	}
}

func TestAtlassianManifestAndMetas(t *testing.T) {
	f, srv := atlNewFake(t, "")
	f.on("GET /rest/applinks/1.0/manifest", 200, `<?xml version="1.0" encoding="UTF-8"?><manifest><id>x</id><name>Acme</name><typeId>Jira</typeId><version>9.12.4</version><buildNumber>912004</buildNumber></manifest>`)
	f.on("GET /login.jsp", 200, `<html><head><meta name="application-name" content="JIRA" data-name="jira" data-version="9.12.4"><META NAME="ajs-context-path" CONTENT="/jira"></head></html>`)
	p := NewProber(NewHTTP(nil))
	m, ok := atlProbeManifest(context.Background(), p, srv.URL)
	if !ok || m.TypeID != "jira" || m.Version != "9.12.4" {
		t.Fatalf("manifest %+v %v", m, ok)
	}
	metas, _ := atlLoginPage(context.Background(), p, srv.URL+"/login.jsp")
	if len(metas) != 2 || metas[0]["data-version"] != "9.12.4" || metas[1]["name"] != "ajs-context-path" {
		t.Fatalf("metas %v", metas)
	}
	if _, ok := atlProbeManifest(context.Background(), p, srv.URL+"/nowhere"); ok {
		t.Error("a 404 is not a manifest")
	}
}

func TestAtlassianRelPath(t *testing.T) {
	for _, c := range []struct{ base, path, rel string }{
		{"", "/browse/A-1", "/browse/A-1"},
		{"/jira", "/jira/browse/A-1", "/browse/A-1"},
		{"/jira", "/jira", "/"},
		{"/jira/", "/jira/x", "/x"},
	} {
		if rel, ok := atlRelPath(c.base, c.path); !ok || rel != c.rel {
			t.Errorf("%+v -> %q %v", c, rel, ok)
		}
	}
	if _, ok := atlRelPath("/jira", "/jirax/browse"); ok {
		t.Error("/jirax is not under /jira")
	}
}
