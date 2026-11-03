package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Confluence: Atlassian Cloud (under /wiki; pages read through REST v2, searched with v1 CQL) and Server/Data Center
// 6.x through 9.x (REST v1 only, at the root or under a context path such as /confluence). On one Atlassian Cloud site
// a link under /wiki is Confluence and every other link is Jira; both providers decide by atlWikiPath.

func init() { Register(&confluenceProvider{}) }

type confluenceProvider struct{}

func (*confluenceProvider) Kind() Kind { return Confluence }

// confStops are the first path segments of Confluence's own pages.
var confStops = []string{"display", "spaces", "pages", "rest", "plugins", "x", "login.action", "dashboard.action", "dosearchsite.action"}

type confFound struct {
	prefix  string
	version string
	edition string
}

func (*confluenceProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	origin := Origin(u)
	if atlCloudHost(u.Hostname()) {
		if !atlWikiPath(u.Path) {
			return nil, nil
		}
		return confCloudDetection(ctx, p, origin), nil
	}
	if atlWikiPath(u.Path) && atlCloudID(ctx, p, origin) != "" {
		// Atlassian Cloud on its own domain.
		return confCloudDetection(ctx, p, origin), nil
	}
	f := atlFirst(PathPrefixes(u, confStops...), func(prefix string) *confFound {
		return confProbe(ctx, p, origin, prefix)
	})
	if f == nil {
		return nil, nil
	}
	base := origin + f.prefix
	return &Detection{
		Kind: Confluence, BaseURL: base, Deployment: SelfHosted, Version: f.version, Edition: f.edition,
		Methods: confMethods(false, base, f.version), Capabilities: []Capability{Pages},
	}, nil
}

func confCloudDetection(ctx context.Context, p *Prober, origin string) *Detection {
	base := origin + "/wiki"
	d := &Detection{
		Kind: Confluence, BaseURL: base, Deployment: Cloud,
		Methods: confMethods(true, base, ""), Capabilities: []Capability{Pages},
	}
	if id := atlCloudID(ctx, p, origin); id != "" {
		d.Extra = map[string]string{"cloudId": id}
	}
	return d
}

// confMethods lists how to sign in. Personal access tokens exist from Confluence 7.9; older versions take a login and
// password only; an unknown version gets both, token first.
func confMethods(cloud bool, base, version string) []Method {
	if cloud {
		return []Method{{
			ID: "api-token", Recommended: true, TokenURL: atlCloudTokenURL,
			Fields: []Field{{ID: FieldEmail, Placeholder: "you@company.com"}, {ID: FieldToken, Secret: true}},
		}}
	}
	basic := Method{ID: "basic", Fields: []Field{{ID: FieldUsername}, {ID: FieldPassword, Secret: true}}}
	if known, ge := atlAtLeast(version, 7, 9); known && !ge {
		basic.Recommended = true
		return []Method{basic}
	}
	pat := Method{
		ID: "pat", Recommended: true, Fields: []Field{{ID: FieldToken, Secret: true}},
		TokenURL: Join(base, "/plugins/personalaccesstokens/usertokens.action"),
	}
	return []Method{pat, basic}
}

// confProbe asks one candidate root whether it is Confluence: by the application-links manifest, by the shape of
// /rest/api/space (or Confluence's own request-time header on whatever it answers), then by the login page.
func confProbe(ctx context.Context, p *Prober, origin, prefix string) *confFound {
	base := origin + prefix
	f := &confFound{prefix: prefix}
	var header http.Header
	switch m, ok := atlProbeManifest(ctx, p, base); {
	case ok && m.TypeID != "confluence":
		return nil
	case ok:
		f.version, header = m.Version, http.Header{}
	default:
		var space map[string]json.RawMessage
		res, ok := p.GetJSON(ctx, base+"/rest/api/space?limit=1", &space)
		if res == nil || !answeredAt(res, base+"/rest/api/space?limit=1") {
			return nil
		}
		_, hasResults := space["results"]
		_, hasLimit := space["limit"]
		_, hasSize := space["size"]
		switch {
		case ok && hasResults && (hasLimit || hasSize):
		case res.Header.Get("X-Confluence-Request-Time") != "":
		default:
			metas, body := atlLoginPage(ctx, p, base+"/login.action")
			version := confLoginVersion(metas, body)
			if version == "" {
				return nil
			}
			f.version = version
		}
		header = res.Header
	}
	if f.version == "" {
		metas, body := atlLoginPage(ctx, p, base+"/login.action")
		f.version = confLoginVersion(metas, body)
	}
	f.edition = atlEdition(f.version, header, 9)
	return f
}

// confLoginVersion is the version of a Confluence login page; "" when the page is not Confluence's (Jira's carries
// the same ajs-version-number meta).
func confLoginVersion(metas []map[string]string, body string) string {
	v := ""
	for _, m := range metas {
		if m["name"] == "ajs-version-number" {
			v = m["content"]
		}
	}
	if v == "" || !strings.Contains(strings.ToLower(body), "confluence") {
		return ""
	}
	for _, m := range metas {
		if strings.EqualFold(m["name"], "application-name") && strings.EqualFold(m["content"], "JIRA") {
			return ""
		}
	}
	return v
}

// ---- client ----

func (*confluenceProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &confClient{conn: conn, h: h, auth: atlAuth(cred), cloud: conn.Deployment == Cloud}
}

type confClient struct {
	conn  Conn
	h     *HTTP
	auth  string
	cloud bool

	mu     sync.Mutex
	spaces map[string]string // v2 space id to key
}

func (c *confClient) call(ctx context.Context, rawURL string, out any) (*Response, error) {
	if c.auth == "" {
		return nil, fmt.Errorf("%w: no credentials", ErrDenied)
	}
	return c.h.JSON(ctx, Request{URL: rawURL, Header: Header("Authorization", c.auth)}, out)
}

// v1 is a REST v1 address; BaseURL already ends in /wiki on Cloud.
func (c *confClient) v1(path string) string { return Join(c.conn.BaseURL, "/rest/api/"+path) }

func (c *confClient) origin() string {
	if u, err := url.Parse(c.conn.BaseURL); err == nil {
		return Origin(u)
	}
	return ""
}

type confUser struct {
	Type           string `json:"type"`
	AccountID      string `json:"accountId"`
	Username       string `json:"username"`
	UserKey        string `json:"userKey"`
	DisplayName    string `json:"displayName"`
	PublicName     string `json:"publicName"`
	Email          string `json:"email"`
	ProfilePicture struct {
		Path string `json:"path"`
	} `json:"profilePicture"`
}

func (u *confUser) person(origin string) *Person {
	if u == nil || (u.DisplayName == "" && u.PublicName == "" && u.Username == "") {
		return nil
	}
	id := u.AccountID
	if id == "" {
		id = u.UserKey
	}
	name := u.DisplayName
	if name == "" {
		name = u.PublicName
	}
	p := &Person{ID: id, Login: u.Username, Name: name, Email: u.Email}
	if path := u.ProfilePicture.Path; strings.HasPrefix(path, "/") {
		p.Avatar = origin + path
	}
	return p
}

func (c *confClient) Me(ctx context.Context) (*Person, error) {
	var u confUser
	if _, err := c.call(ctx, c.v1("user/current"), &u); err != nil {
		return nil, err
	}
	// Anonymous callers get 200 and {"type":"anonymous"}.
	if u.Type == "anonymous" || (u.AccountID == "" && u.Username == "" && u.UserKey == "") {
		return nil, fmt.Errorf("%w: signed in as nobody", ErrDenied)
	}
	if p := u.person(c.origin()); p != nil {
		return p, nil
	}
	return &Person{ID: u.AccountID, Login: u.Username, Name: u.Username}, nil
}

// ---- search ----

func confQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// confCQL builds the query: pages and blog posts, newest change first.
func confCQL(q Query) string {
	if native := strings.TrimSpace(q.Native); native != "" {
		return native
	}
	where := []string{"type in (page, blogpost)"}
	if t := strings.TrimSpace(q.Text); t != "" {
		where = append(where, "text ~ "+confQuote(t))
	}
	if q.Mine {
		where = append(where, "creator = currentUser()")
	}
	if s := strings.TrimSpace(q.Project); s != "" {
		where = append(where, "space = "+confQuote(s))
	}
	return strings.Join(where, " AND ") + " ORDER BY lastmodified DESC"
}

type confContent struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	SpaceID any    `json:"spaceId"`
	Space   *struct {
		Key string `json:"key"`
	} `json:"space"`
	Version *struct {
		When      string    `json:"when"`
		CreatedAt string    `json:"createdAt"`
		AuthorID  string    `json:"authorId"`
		By        *confUser `json:"by"`
	} `json:"version"`
	History *struct {
		CreatedBy *confUser `json:"createdBy"`
	} `json:"history"`
	Body *struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Links struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

func (c *confClient) page(ct confContent, withBody bool) *Page {
	pg := &Page{ID: ct.ID, Title: ct.Title}
	if ct.Space != nil {
		pg.Space = ct.Space.Key
	}
	if ct.Links.WebUI != "" {
		pg.URL = Join(c.conn.BaseURL, ct.Links.WebUI)
	} else {
		pg.URL = Join(c.conn.BaseURL, "/pages/viewpage.action?pageId="+ct.ID)
	}
	if v := ct.Version; v != nil {
		pg.Updated = ParseTime(v.When)
		if pg.Updated.IsZero() {
			pg.Updated = ParseTime(v.CreatedAt)
		}
		pg.Author = v.By.person(c.origin())
	}
	if ct.History != nil && ct.History.CreatedBy != nil {
		pg.Author = ct.History.CreatedBy.person(c.origin())
	}
	if withBody && ct.Body != nil {
		pg.Body = confStorageText(ct.Body.Storage.Value)
	}
	return pg
}

func (c *confClient) SearchPages(ctx context.Context, q Query) ([]Page, error) {
	limit := q.LimitOr(25)
	var res struct {
		Results []confContent `json:"results"`
	}
	u := c.v1("content/search?" + Q("cql", confCQL(q), "limit", fmt.Sprint(min(limit, 100)), "expand", "space,version"))
	if _, err := c.call(ctx, u, &res); err != nil {
		return nil, err
	}
	out := make([]Page, 0, len(res.Results))
	for _, r := range res.Results {
		out = append(out, *c.page(r, false))
	}
	return out[:min(len(out), limit)], nil
}

// ---- one page ----

const confTitlePrefix = "title:"

var confIDRE = regexp.MustCompile(`^\d+$`)

// Page reads a page by id, or by "title:SPACE/Title" (what a /display/SPACE/Title link stands for).
func (c *confClient) Page(ctx context.Context, id string) (*Page, error) {
	id = strings.TrimSpace(id)
	if t, ok := strings.CutPrefix(id, confTitlePrefix); ok {
		return c.pageByTitle(ctx, t)
	}
	if !confIDRE.MatchString(id) {
		return nil, fmt.Errorf("%w: %q is not a page id", ErrNotFound, id)
	}
	if c.cloud {
		pg, err := c.pageV2(ctx, id)
		if err == nil || errors.Is(err, ErrDenied) {
			return pg, err
		}
		// v2 has no blog posts and may be turned off for a site: v1 answers for everything.
	}
	var ct confContent
	if _, err := c.call(ctx, c.v1("content/"+id+"?"+Q("expand", "body.storage,version,space,history")), &ct); err != nil {
		return nil, err
	}
	return c.page(ct, true), nil
}

func (c *confClient) pageByTitle(ctx context.Context, t string) (*Page, error) {
	space, title, ok := strings.Cut(t, "/")
	if !ok || space == "" || title == "" {
		return nil, fmt.Errorf("%w: %q is not SPACE/Title", ErrNotFound, t)
	}
	var res struct {
		Results []confContent `json:"results"`
	}
	u := c.v1("content?" + Q("spaceKey", space, "title", title, "type", "page", "limit", "1", "expand", "body.storage,version,space,history"))
	if _, err := c.call(ctx, u, &res); err != nil {
		return nil, err
	}
	if len(res.Results) == 0 {
		return nil, fmt.Errorf("%w: page %q in space %s", ErrNotFound, title, space)
	}
	return c.page(res.Results[0], true), nil
}

// pageV2 reads a Cloud page through REST v2, which carries ids only for space and author: the space key is looked up
// (and kept), the author's name is best effort.
func (c *confClient) pageV2(ctx context.Context, id string) (*Page, error) {
	var ct confContent
	if _, err := c.call(ctx, Join(c.conn.BaseURL, "/api/v2/pages/"+id+"?body-format=storage"), &ct); err != nil {
		return nil, err
	}
	if ct.ID == "" {
		return nil, fmt.Errorf("%w: page %s", ErrUnreachable, id)
	}
	pg := c.page(ct, true)
	if sid := fmt.Sprint(ct.SpaceID); ct.SpaceID != nil && sid != "" {
		pg.Space = c.spaceKey(ctx, sid)
	}
	if ct.Version != nil && ct.Version.AuthorID != "" {
		var u confUser
		if _, err := c.call(ctx, c.v1("user?"+Q("accountId", ct.Version.AuthorID)), &u); err == nil {
			pg.Author = u.person(c.origin())
		}
	}
	return pg, nil
}

func (c *confClient) spaceKey(ctx context.Context, id string) string {
	c.mu.Lock()
	key, ok := c.spaces[id]
	c.mu.Unlock()
	if ok {
		return key
	}
	var s struct {
		Key string `json:"key"`
	}
	if _, err := c.call(ctx, Join(c.conn.BaseURL, "/api/v2/spaces/"+url.PathEscape(id)), &s); err != nil {
		return ""
	}
	c.mu.Lock()
	if c.spaces == nil {
		c.spaces = map[string]string{}
	}
	c.spaces[id] = s.Key
	c.mu.Unlock()
	return s.Key
}

// ---- links ----

// Link reads /spaces/KEY/pages/ID/Title (Cloud, Data Center 8+), /pages/viewpage.action?pageId=ID and
// /display/SPACE/Title, which is answered as the key "title:SPACE/Title" that Page resolves. Short /x/ links are not
// read.
func (*confluenceProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	base, err := url.Parse(conn.BaseURL)
	if err != nil || !strings.EqualFold(base.Host, u.Host) {
		return LinkTarget{}, false
	}
	rel, ok := atlRelPath(base.Path, u.EscapedPath())
	if !ok {
		return LinkTarget{}, false
	}
	page := func(id string) (LinkTarget, bool) { return LinkTarget{Kind: "page", Key: id}, true }
	if id := u.Query().Get("pageId"); strings.HasSuffix(rel, "/viewpage.action") && confIDRE.MatchString(id) {
		return page(id)
	}
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "pages" && confIDRE.MatchString(segs[i+1]) && i >= 2 && segs[i-2] == "spaces" {
			return page(segs[i+1])
		}
	}
	if len(segs) == 3 && segs[0] == "display" {
		space, err1 := url.PathUnescape(segs[1])
		title, err2 := url.PathUnescape(segs[2])
		if err1 != nil || err2 != nil || space == "" || title == "" {
			return LinkTarget{}, false
		}
		return LinkTarget{Kind: "page", Key: confTitlePrefix + space + "/" + strings.ReplaceAll(title, "+", " ")}, true
	}
	return LinkTarget{}, false
}

// ---- storage format ----

var (
	confCodeRE   = regexp.MustCompile(`(?is)<ac:structured-macro\b[^>]*ac:name="(?:code|noformat)"[^>]*>(.*?)</ac:structured-macro>`)
	confLangRE   = regexp.MustCompile(`(?is)<ac:parameter\b[^>]*ac:name="language"[^>]*>(.*?)</ac:parameter>`)
	confCDATARE  = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)
	confParamRE  = regexp.MustCompile(`(?is)<ac:parameter\b[^>]*>.*?</ac:parameter>`)
	confTaskRE   = regexp.MustCompile(`(?is)<ac:task>.*?<ac:task-status>(.*?)</ac:task-status>.*?<ac:task-body>(.*?)</ac:task-body>.*?</ac:task>`)
	confLinkRE   = regexp.MustCompile(`(?is)<ac:link\b[^>]*>(.*?)</ac:link>`)
	confImageRE  = regexp.MustCompile(`(?is)<ac:image\b[^>]*>(.*?)</ac:image>`)
	confTitleRE  = regexp.MustCompile(`(?i)ri:content-title="([^"]*)"`)
	confFileRE   = regexp.MustCompile(`(?i)ri:filename="([^"]*)"`)
	confAnchorRE = regexp.MustCompile(`(?is)<a\b[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	confHeadRE   = regexp.MustCompile(`(?i)<h([1-6])\b[^>]*>`)
	confCellRE   = regexp.MustCompile(`(?i)</t[dh]>`)
	confTimeRE   = regexp.MustCompile(`(?i)<time\b[^>]*datetime="([^"]*)"[^>]*/?>(?:</time>)?`)
)

// confStorageText is the readable text of a page in storage format (XHTML with ac: and ri: elements): code macros
// become fences, tasks check boxes, links and headings Markdown; other macros keep their body text and lose their
// parameters.
func confStorageText(s string) string {
	var codes []string
	s = confCodeRE.ReplaceAllStringFunc(s, func(m string) string {
		lang := ""
		if l := confLangRE.FindStringSubmatch(m); l != nil {
			lang = strings.TrimSpace(l[1])
		}
		code := ""
		if c := confCDATARE.FindStringSubmatch(m); c != nil {
			code = strings.Trim(c[1], "\n")
		}
		codes = append(codes, "```"+lang+"\n"+code+"\n```")
		return fmt.Sprintf("<p>\x00%d\x00</p>", len(codes)-1)
	})
	s = confTaskRE.ReplaceAllStringFunc(s, func(m string) string {
		t := confTaskRE.FindStringSubmatch(m)
		box := "[ ] "
		if strings.EqualFold(strings.TrimSpace(t[1]), "complete") {
			box = "[x] "
		}
		return "<li>" + box + t[2] + "</li>"
	})
	s = confLinkRE.ReplaceAllStringFunc(s, func(m string) string {
		inner := confLinkRE.FindStringSubmatch(m)[1]
		if c := confCDATARE.FindStringSubmatch(inner); c != nil {
			return html.EscapeString(c[1])
		}
		if strings.Contains(inner, "<ri:user") {
			return "@user"
		}
		if t := confTitleRE.FindStringSubmatch(inner); t != nil {
			return t[1]
		}
		if f := confFileRE.FindStringSubmatch(inner); f != nil {
			return f[1]
		}
		return ""
	})
	s = confImageRE.ReplaceAllStringFunc(s, func(m string) string {
		if f := confFileRE.FindStringSubmatch(m); f != nil {
			return "[image: " + f[1] + "]"
		}
		return "[image]"
	})
	s = confParamRE.ReplaceAllString(s, "")
	s = confCDATARE.ReplaceAllStringFunc(s, func(m string) string { return html.EscapeString(confCDATARE.FindStringSubmatch(m)[1]) })
	s = confAnchorRE.ReplaceAllString(s, "[$2]($1)")
	s = confHeadRE.ReplaceAllStringFunc(s, func(m string) string {
		return "\n\n" + strings.Repeat("#", int(confHeadRE.FindStringSubmatch(m)[1][0]-'0')) + " "
	})
	s = confCellRE.ReplaceAllString(s, " | ")
	s = confTimeRE.ReplaceAllString(s, "$1")
	s = confTightLists(HTMLText(s))
	for i, c := range codes {
		s = strings.Replace(s, fmt.Sprintf("\x00%d\x00", i), c, 1)
	}
	return s
}

// confTightLists drops the blank line HTMLText leaves between two list items.
func confTightLists(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0:0]
	for i, l := range lines {
		if l == "" && i > 0 && i+1 < len(lines) && strings.HasPrefix(lines[i-1], "- ") && strings.HasPrefix(lines[i+1], "- ") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
