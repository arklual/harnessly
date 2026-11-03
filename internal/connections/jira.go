package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Jira: Atlassian Cloud (REST v3, ADF bodies, token-paged /search/jql) and Server/Data Center 7.x through 11.x (REST
// v2, wiki-markup bodies, startAt paging). Cloud is told by its host (atlassian.net) or by serverInfo's deploymentType;
// a self-hosted instance by serverInfo, then by the application-links manifest, the login page's meta tags and
// Atlassian's response headers, for instances that turn anonymous API access off.

func init() { Register(&jiraProvider{}) }

type jiraProvider struct{}

func (*jiraProvider) Kind() Kind { return Jira }

// jiraStops are the first path segments of Jira's own pages: the context path is whatever comes before them.
var jiraStops = []string{"browse", "projects", "secure", "rest", "issues", "servicedesk", "plugins", "login.jsp", "dashboard.jspa"}

// jiraServerInfo is the anonymous /rest/api/2/serverInfo.
type jiraServerInfo struct {
	Version        string `json:"version"`
	VersionNumbers []int  `json:"versionNumbers"`
	DeploymentType string `json:"deploymentType"`
	BaseURL        string `json:"baseUrl"`
	ServerTitle    string `json:"serverTitle"`
}

func (i jiraServerInfo) isJira() bool {
	return i.Version != "" && (len(i.VersionNumbers) > 0 || i.DeploymentType != "")
}

// jiraFound is what one context-path candidate turned out to be.
type jiraFound struct {
	prefix  string
	cloud   bool
	version string
	edition string
	title   string
}

func (*jiraProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	origin := Origin(u)
	if atlCloudHost(u.Hostname()) {
		if atlWikiPath(u.Path) {
			return nil, nil
		}
		return jiraCloudDetection(ctx, p, origin), nil
	}
	hint := jiraHint(u)
	f := atlFirst(PathPrefixes(u, jiraStops...), func(prefix string) *jiraFound {
		return jiraProbe(ctx, p, origin, prefix, hint)
	})
	if f == nil {
		return nil, nil
	}
	if f.cloud {
		// A Cloud site on its own domain: Confluence is under /wiki and is not ours.
		if atlWikiPath(u.Path) {
			return nil, nil
		}
		return jiraCloudDetection(ctx, p, origin), nil
	}
	base := origin + f.prefix
	return &Detection{
		Kind: Jira, BaseURL: base, Deployment: SelfHosted, Version: f.version, Edition: f.edition, Title: f.title,
		Methods: jiraMethods(false, base, f.version), Capabilities: []Capability{Issues},
	}, nil
}

func jiraCloudDetection(ctx context.Context, p *Prober, origin string) *Detection {
	d := &Detection{
		Kind: Jira, BaseURL: origin, Deployment: Cloud,
		Methods: jiraMethods(true, origin, ""), Capabilities: []Capability{Issues},
	}
	if id := atlCloudID(ctx, p, origin); id != "" {
		d.Extra = map[string]string{"cloudId": id}
	}
	return d
}

// jiraMethods lists how to sign in, the recommended one first. Personal access tokens exist from Jira 8.14; older
// versions take a login and password only; an unknown version gets both, token first. Jira 11 turns Basic off by
// default, so the token stays first there too.
func jiraMethods(cloud bool, base, version string) []Method {
	if cloud {
		return []Method{{
			ID: "api-token", Recommended: true, TokenURL: atlCloudTokenURL,
			Fields: []Field{{ID: FieldEmail, Placeholder: "you@company.com"}, {ID: FieldToken, Secret: true}},
		}}
	}
	basic := Method{ID: "basic", Fields: []Field{{ID: FieldUsername}, {ID: FieldPassword, Secret: true}}}
	if known, ge := atlAtLeast(version, 8, 14); known && !ge {
		basic.Recommended = true
		return []Method{basic}
	}
	pat := Method{
		ID: "pat", Recommended: true, Fields: []Field{{ID: FieldToken, Secret: true}},
		TokenURL: Join(base, "/secure/ViewProfile.jspa?selectedTab=com.atlassian.pats.pats-plugin:jira-user-personal-access-tokens"),
	}
	return []Method{pat, basic}
}

// jiraHint says whether the pasted link looks like Jira's own, which lets a bare Atlassian 401 count as Jira.
func jiraHint(u *url.URL) bool {
	p := strings.ToLower(u.Path)
	return strings.Contains(strings.ToLower(u.Hostname()), "jira") || strings.Contains(p, "jira") ||
		strings.Contains(p, "/browse/") || strings.HasPrefix(p, "/secure/") || strings.HasPrefix(p, "/projects/")
}

// jiraProbe asks one candidate root whether it is Jira.
func jiraProbe(ctx context.Context, p *Prober, origin, prefix string, hint bool) *jiraFound {
	base := origin + prefix
	var info jiraServerInfo
	res, ok := p.GetJSON(ctx, base+"/rest/api/2/serverInfo", &info)
	if ok && info.isJira() && answeredAt(res, base+"/rest/api/2/serverInfo") {
		f := &jiraFound{prefix: prefix, cloud: strings.EqualFold(info.DeploymentType, "Cloud"), version: info.Version}
		if title := strings.TrimSpace(info.ServerTitle); title != "" && title != "Jira" {
			f.title = title
		}
		f.edition = atlEdition(info.Version, res.Header, 10)
		f.prefix = jiraContextPath(ctx, p, origin, prefix, info.BaseURL)
		return f
	}
	if res == nil || res.Status == http.StatusNotFound || !answeredAt(res, base+"/rest/api/2/serverInfo") {
		return nil // this root has no Jira API, or a redirect to a login page answered for it
	}
	// Anonymous access is off, or a single sign-on page answered: look for Jira's other public signs.
	if m, ok := atlProbeManifest(ctx, p, base); ok {
		if m.TypeID != "jira" {
			return nil
		}
		return &jiraFound{prefix: prefix, version: m.Version, edition: atlEdition(m.Version, res.Header, 10)}
	}
	metas, _ := atlLoginPage(ctx, p, base+"/login.jsp")
	for _, m := range metas {
		if strings.EqualFold(m["name"], "application-name") && strings.EqualFold(m["content"], "JIRA") {
			v := m["data-version"]
			return &jiraFound{prefix: prefix, version: v, edition: atlEdition(v, res.Header, 10)}
		}
	}
	// A 401 with Atlassian's request id and no Confluence marker, on a link that looks like Jira's.
	if hint && res.Status == http.StatusUnauthorized && res.Header.Get("X-AREQUESTID") != "" &&
		res.Header.Get("X-Confluence-Request-Time") == "" {
		return &jiraFound{prefix: prefix, edition: atlEdition("", res.Header, 10)}
	}
	return nil
}

// jiraContextPath is the context path serverInfo.baseUrl names, when it differs from the one the probe used and the
// API answers there too (a proxy may serve the API at the root and the site under /jira); else the probed one.
func jiraContextPath(ctx context.Context, p *Prober, origin, prefix, baseURL string) string {
	b, err := url.Parse(baseURL)
	if err != nil || b.Host == "" || !strings.EqualFold(Origin(b), origin) {
		return prefix
	}
	path := strings.TrimRight(b.Path, "/")
	if path == "" || path == prefix {
		return prefix
	}
	var info jiraServerInfo
	asked := origin + path + "/rest/api/2/serverInfo"
	if res, ok := p.GetJSON(ctx, asked, &info); ok && answeredAt(res, asked) && info.isJira() {
		return path
	}
	return prefix
}

// ---- client ----

// Open opens a connection. Cloud talks REST v3, self-hosted REST v2.
func (*jiraProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &jiraClient{conn: conn, h: h, auth: atlAuth(cred), cloud: conn.Deployment == Cloud}
}

type jiraClient struct {
	conn  Conn
	h     *HTTP
	auth  string
	cloud bool

	mu sync.Mutex
	// tokenSearch: /search/jql with page tokens (Cloud) instead of /search with startAt. Starts from the deployment
	// and flips when the instance answers 404/405/410 for the one in use.
	tokenSearch *bool
}

func (c *jiraClient) api(path string) string {
	v := "2"
	if c.cloud {
		v = "3"
	}
	return Join(c.conn.BaseURL, "/rest/api/"+v+"/"+path)
}

func (c *jiraClient) call(ctx context.Context, method, rawURL string, body, out any) (*Response, error) {
	if c.auth == "" {
		return nil, fmt.Errorf("%w: no credentials", ErrDenied)
	}
	return c.h.JSON(ctx, Request{
		Method: method, URL: rawURL, Body: body,
		Header: Header("Authorization", c.auth, "X-Atlassian-Token", "no-check"),
	}, out)
}

type jiraUser struct {
	AccountID    string            `json:"accountId"`
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	DisplayName  string            `json:"displayName"`
	EmailAddress string            `json:"emailAddress"`
	AvatarURLs   map[string]string `json:"avatarUrls"`
}

func (u *jiraUser) person() *Person {
	if u == nil {
		return nil
	}
	id := u.AccountID
	if id == "" {
		id = u.Key
	}
	name := u.DisplayName
	if name == "" {
		name = u.Name
	}
	return &Person{ID: id, Login: u.Name, Name: name, Email: u.EmailAddress, Avatar: u.AvatarURLs["48x48"]}
}

func (c *jiraClient) Me(ctx context.Context) (*Person, error) {
	var u jiraUser
	res, err := c.call(ctx, http.MethodGet, c.api("myself"), nil, &u)
	if err != nil {
		return nil, err
	}
	// Some instances answer anonymous callers 200; the person is who the product says it is.
	if strings.EqualFold(res.Header.Get("X-AUSERNAME"), "anonymous") || (u.AccountID == "" && u.Name == "" && u.Key == "") {
		return nil, fmt.Errorf("%w: signed in as nobody", ErrDenied)
	}
	return u.person(), nil
}

func (c *jiraClient) Projects(ctx context.Context) ([]Project, error) {
	var out []Project
	add := func(key, name string) {
		out = append(out, Project{Key: key, Name: name, URL: Join(c.conn.BaseURL, "/browse/"+key)})
	}
	if !c.cloud {
		var list []struct{ Key, Name string }
		if _, err := c.call(ctx, http.MethodGet, c.api("project"), nil, &list); err != nil {
			return nil, err
		}
		for _, p := range list {
			add(p.Key, p.Name)
		}
		return out, nil
	}
	for start := 0; start < 1000; {
		var page struct {
			Values []struct{ Key, Name string } `json:"values"`
			IsLast bool                         `json:"isLast"`
		}
		if _, err := c.call(ctx, http.MethodGet, c.api("project/search?"+Q("startAt", fmt.Sprint(start), "maxResults", "50")), nil, &page); err != nil {
			return nil, err
		}
		for _, p := range page.Values {
			add(p.Key, p.Name)
		}
		if page.IsLast || len(page.Values) == 0 {
			break
		}
		start += len(page.Values)
	}
	return out, nil
}

// ---- search ----

// jiraQuote is s as a JQL string literal.
func jiraQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

var jiraOrderRE = regexp.MustCompile(`(?i)\border\s+by\b.*$`)

// jiraJQL builds the query. Cloud refuses a query with no restriction ("unbounded"), so one that has none gets
// `updated >= -365d`.
func jiraJQL(q Query, cloud bool) string {
	if native := strings.TrimSpace(q.Native); native != "" {
		if cloud && strings.TrimSpace(jiraOrderRE.ReplaceAllString(native, "")) == "" {
			return "updated >= -365d " + native
		}
		return native
	}
	var where []string
	if t := strings.TrimSpace(q.Text); t != "" {
		where = append(where, "text ~ "+jiraQuote(t))
	}
	if q.Mine {
		where = append(where, "assignee = currentUser()")
	}
	if p := strings.TrimSpace(q.Project); p != "" {
		where = append(where, "project = "+jiraQuote(p))
	}
	positive := len(where) > 0
	if q.Open {
		where = append(where, "statusCategory != Done")
	}
	if cloud && !positive {
		where = append(where, "updated >= -365d")
	}
	return strings.TrimSpace(strings.Join(where, " AND ") + " ORDER BY updated DESC")
}

// jiraListFields are what a search asks for. Descriptions and comments are for Issue.
var jiraListFields = []string{"summary", "status", "issuetype", "priority", "assignee", "reporter", "project", "parent", "labels", "created", "updated", "resolutiondate"}

type jiraIssueJSON struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Fields json.RawMessage `json:"fields"`
}

type jiraFields struct {
	Summary string `json:"summary"`
	Status  struct {
		Name     string `json:"name"`
		Category struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	} `json:"status"`
	IssueType *struct{ Name string } `json:"issuetype"`
	Priority  *struct{ Name string } `json:"priority"`
	Assignee  *jiraUser              `json:"assignee"`
	Reporter  *jiraUser              `json:"reporter"`
	Project   *struct{ Key string }  `json:"project"`
	Parent    *struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
		} `json:"fields"`
	} `json:"parent"`
	Labels         []string        `json:"labels"`
	Created        string          `json:"created"`
	Updated        string          `json:"updated"`
	ResolutionDate string          `json:"resolutiondate"`
	Description    json.RawMessage `json:"description"`
}

// jiraBody is a description or comment body as Markdown: ADF on Cloud, wiki markup elsewhere.
func jiraBody(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "" || s == "null":
		return ""
	case s[0] == '{':
		return atlADF(raw)
	case s[0] == '"':
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return atlWiki(text)
		}
	}
	return ""
}

func jiraCategory(key string) Category {
	switch key {
	case "new":
		return Todo
	case "indeterminate":
		return Doing
	case "done":
		return Done
	}
	return Unknown
}

// issue converts one issue of an answer; epicField is the Epic Link custom field of a classic Server project.
func (c *jiraClient) issue(raw jiraIssueJSON, epicField string) (Issue, error) {
	var f jiraFields
	if err := json.Unmarshal(raw.Fields, &f); err != nil {
		return Issue{}, fmt.Errorf("%w: issue %s", ErrUnreachable, raw.Key)
	}
	is := Issue{
		Key: raw.Key, ID: raw.ID, Title: f.Summary, URL: Join(c.conn.BaseURL, "/browse/"+raw.Key),
		Status:   Status{Name: f.Status.Name, Category: jiraCategory(f.Status.Category.Key)},
		Assignee: f.Assignee.person(), Reporter: f.Reporter.person(), Labels: f.Labels,
		Created: ParseTime(f.Created), Updated: ParseTime(f.Updated), Resolved: ParseTime(f.ResolutionDate),
		Description: jiraBody(f.Description),
	}
	if f.IssueType != nil {
		is.Type = f.IssueType.Name
	}
	if f.Priority != nil {
		is.Priority = f.Priority.Name
	}
	if f.Project != nil {
		is.Project = f.Project.Key
	}
	switch {
	case f.Parent != nil && f.Parent.Key != "":
		is.Parent = &Ref{Key: f.Parent.Key, Title: f.Parent.Fields.Summary, URL: Join(c.conn.BaseURL, "/browse/"+f.Parent.Key)}
	case epicField != "":
		var all map[string]json.RawMessage
		var key string
		if json.Unmarshal(raw.Fields, &all) == nil && json.Unmarshal(all[epicField], &key) == nil && key != "" {
			is.Parent = &Ref{Key: key, URL: Join(c.conn.BaseURL, "/browse/"+key)}
		}
	}
	return is, nil
}

// epicFields remembers the Epic Link field per instance: it never changes, and clients are opened per request.
var epicFields sync.Map

// epicLinkField finds the custom field of the classic Epic Link ("" when the instance has none). Only self-hosted
// instances need it: Cloud links epics through `parent`.
func (c *jiraClient) epicLinkField(ctx context.Context) string {
	if c.cloud {
		return ""
	}
	if v, ok := epicFields.Load(c.conn.BaseURL); ok {
		return v.(string)
	}
	var fields []struct {
		ID     string `json:"id"`
		Schema struct {
			Custom string `json:"custom"`
		} `json:"schema"`
	}
	if _, err := c.call(ctx, http.MethodGet, c.api("field"), nil, &fields); err != nil {
		if errors.Is(err, ErrUnreachable) {
			return "" // try again next time
		}
		epicFields.Store(c.conn.BaseURL, "")
		return ""
	}
	id := ""
	for _, f := range fields {
		if f.Schema.Custom == "com.pyxis.greenhopper.jira:gh-epic-link" {
			id = f.ID
			break
		}
	}
	epicFields.Store(c.conn.BaseURL, id)
	return id
}

func (c *jiraClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	limit := q.LimitOr(25)
	fields := jiraListFields
	epic := c.epicLinkField(ctx)
	if epic != "" {
		fields = append(append([]string(nil), fields...), epic)
	}
	raws, err := c.search(ctx, jiraJQL(q, c.cloud), fields, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(raws))
	for _, r := range raws {
		is, err := c.issue(r, epic)
		if err != nil {
			return nil, err
		}
		out = append(out, is)
	}
	return out, nil
}

// useTokens says which search the client speaks now.
func (c *jiraClient) useTokens() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokenSearch == nil {
		t := c.cloud
		c.tokenSearch = &t
	}
	return *c.tokenSearch
}

func (c *jiraClient) setTokens(t bool) {
	c.mu.Lock()
	c.tokenSearch = &t
	c.mu.Unlock()
}

// search runs the query with the search the deployment has. Cloud removed /search (410 Gone) for /search/jql; a
// self-hosted instance that drops /search the same way is followed to /search/jql, and a site that does not have
// /search/jql yet falls back to /search.
func (c *jiraClient) search(ctx context.Context, jql string, fields []string, limit int) ([]jiraIssueJSON, error) {
	for attempt := 0; attempt < 2; attempt++ {
		tokens := c.useTokens()
		var out []jiraIssueJSON
		var err error
		if tokens {
			out, err = c.searchTokens(ctx, jql, fields, limit)
		} else {
			out, err = c.searchOffset(ctx, jql, fields, limit)
		}
		if err == nil || attempt == 1 || !jiraSearchGone(err) {
			return out, err
		}
		c.setTokens(!tokens)
	}
	return nil, ErrUnreachable
}

// jiraSearchGone: the endpoint is not there (404, 405) or was removed (410, which StatusErr reads as not-found).
func jiraSearchGone(err error) bool { return errors.Is(err, ErrNotFound) }

func (c *jiraClient) searchTokens(ctx context.Context, jql string, fields []string, limit int) ([]jiraIssueJSON, error) {
	var out []jiraIssueJSON
	token := ""
	for page := 0; page < 20 && len(out) < limit; page++ {
		body := map[string]any{"jql": jql, "fields": fields, "maxResults": min(limit-len(out), 100)}
		if token != "" {
			body["nextPageToken"] = token
		}
		var res struct {
			Issues        []jiraIssueJSON `json:"issues"`
			NextPageToken string          `json:"nextPageToken"`
			IsLast        bool            `json:"isLast"`
		}
		if _, err := c.call(ctx, http.MethodPost, c.api("search/jql"), body, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Issues...)
		token = res.NextPageToken
		if res.IsLast || token == "" || len(res.Issues) == 0 {
			break
		}
	}
	return out[:min(len(out), limit)], nil
}

func (c *jiraClient) searchOffset(ctx context.Context, jql string, fields []string, limit int) ([]jiraIssueJSON, error) {
	var out []jiraIssueJSON
	for page := 0; page < 20 && len(out) < limit; page++ {
		body := map[string]any{"jql": jql, "fields": fields, "startAt": len(out), "maxResults": min(limit-len(out), 100)}
		var res struct {
			Issues []jiraIssueJSON `json:"issues"`
			Total  int             `json:"total"`
		}
		if _, err := c.call(ctx, http.MethodPost, c.api("search"), body, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Issues...)
		if len(res.Issues) == 0 || len(out) >= res.Total {
			break
		}
	}
	return out[:min(len(out), limit)], nil
}

// ---- one issue ----

var jiraKeyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

// jiraCommentsMax is how many of the last comments Issue returns.
const jiraCommentsMax = 20

func (c *jiraClient) Issue(ctx context.Context, key string) (*Issue, error) {
	key = strings.TrimSpace(key)
	if !jiraKeyRE.MatchString(key) {
		return nil, fmt.Errorf("%w: %q is not an issue key", ErrNotFound, key)
	}
	epic := c.epicLinkField(ctx)
	fields := append(append([]string(nil), jiraListFields...), "description")
	if epic != "" {
		fields = append(fields, epic)
	}
	var raw jiraIssueJSON
	if _, err := c.call(ctx, http.MethodGet, c.api("issue/"+url.PathEscape(key)+"?"+Q("fields", strings.Join(fields, ","))), nil, &raw); err != nil {
		return nil, err
	}
	is, err := c.issue(raw, epic)
	if err != nil {
		return nil, err
	}
	comments, err := c.comments(ctx, key)
	if err != nil && !errors.Is(err, ErrDenied) && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	is.Comments = comments
	return &is, nil
}

// comments are the last jiraCommentsMax comments, oldest first. The first request tells how many there are; a longer
// thread needs a second for its tail (orderBy is not on every version).
func (c *jiraClient) comments(ctx context.Context, key string) ([]Comment, error) {
	type page struct {
		Total    int `json:"total"`
		Comments []struct {
			ID      string          `json:"id"`
			Author  *jiraUser       `json:"author"`
			Body    json.RawMessage `json:"body"`
			Created string          `json:"created"`
		} `json:"comments"`
	}
	base := c.api("issue/" + url.PathEscape(key) + "/comment")
	var p page
	if _, err := c.call(ctx, http.MethodGet, base+"?"+Q("startAt", "0", "maxResults", fmt.Sprint(jiraCommentsMax)), nil, &p); err != nil {
		return nil, err
	}
	if total := p.Total; total > jiraCommentsMax {
		p = page{}
		if _, err := c.call(ctx, http.MethodGet, base+"?"+Q("startAt", fmt.Sprint(total-jiraCommentsMax), "maxResults", fmt.Sprint(jiraCommentsMax)), nil, &p); err != nil {
			return nil, err
		}
	}
	out := make([]Comment, 0, len(p.Comments))
	for _, cm := range p.Comments {
		out = append(out, Comment{ID: cm.ID, Author: cm.Author.person(), Text: jiraBody(cm.Body), Created: ParseTime(cm.Created)})
	}
	return out, nil
}

// ---- links ----

var jiraLinkKeyRE = regexp.MustCompile(`(?i)^[A-Z][A-Z0-9_]*-\d+$`)

// Link reads /browse/KEY, /projects/P/issues/KEY, Cloud's /jira/software/.../issues/KEY and any board or backlog link
// with ?selectedIssue=KEY.
func (*jiraProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	base, err := url.Parse(conn.BaseURL)
	if err != nil || !strings.EqualFold(base.Host, u.Host) {
		return LinkTarget{}, false
	}
	rel, ok := atlRelPath(base.Path, u.Path)
	if !ok || atlWikiPath(rel) && conn.Deployment == Cloud {
		return LinkTarget{}, false
	}
	target := func(key string) (LinkTarget, bool) {
		return LinkTarget{Kind: "issue", Key: strings.ToUpper(key)}, true
	}
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	for i := 0; i+1 < len(segs); i++ {
		if (segs[i] == "browse" || segs[i] == "issues") && jiraLinkKeyRE.MatchString(segs[i+1]) {
			return target(segs[i+1])
		}
	}
	if k := u.Query().Get("selectedIssue"); jiraLinkKeyRE.MatchString(k) {
		return target(k)
	}
	return LinkTarget{}, false
}

// atlRelPath is path below the context path basePath; ok is false when path is not under it.
func atlRelPath(basePath, path string) (string, bool) {
	basePath = strings.TrimRight(basePath, "/")
	if basePath == "" {
		return path, true
	}
	if path == basePath {
		return "/", true
	}
	if rest, ok := strings.CutPrefix(path, basePath+"/"); ok {
		return "/" + rest, true
	}
	return "", false
}
