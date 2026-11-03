package connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func init() { Register(&youTrackProvider{}) }

// youTrackProvider reads JetBrains YouTrack: Cloud (*.youtrack.cloud, no context path; *.myjetbrains.com, context
// path /youtrack) and Standalone/Server on any domain and context path.
type youTrackProvider struct{}

// Kind implements Provider.
func (youTrackProvider) Kind() Kind { return YouTrack }

// ytStops are YouTrack's own routes: a pasted link is cut where one starts to find the context path.
var ytStops = []string{"issue", "issues", "agiles", "agile", "articles", "article", "dashboard", "projects", "users",
	"api", "hub", "admin", "rest", "tickets", "settings", "search", "newissue", "timesheets"}

// ytVersionRe is how YouTrack numbers its releases: 2024.3.12345.
var ytVersionRe = regexp.MustCompile(`^\d{4}\.\d+`)

// ytTitleRe finds the product name in a page title.
var ytTitleRe = regexp.MustCompile(`(?is)<title>[^<]*youtrack`)

// ytIssueRe is the shape of an issue key.
var ytIssueRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

// Detect implements Provider.
func (youTrackProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	host := u.Hostname()
	switch {
	case HostIs(host, "youtrack.cloud"):
		base := "https://" + u.Host
		return ytDetection(base, Cloud, ytVersion(ctx, p, base)), nil
	case HostIs(host, "myjetbrains.com"):
		base := "https://" + u.Host + "/youtrack"
		return ytDetection(base, Cloud, ytVersion(ctx, p, base)), nil
	}
	for _, prefix := range PathPrefixes(u, ytStops...) {
		base := Origin(u) + prefix
		if v := ytVersion(ctx, p, base); v != "" {
			return ytDetection(base, SelfHosted, v), nil
		}
		if ytAnonymousSignature(ctx, p, base) {
			return ytDetection(base, SelfHosted, ""), nil
		}
	}
	return nil, nil
}

// ytVersion reads the product version from the anonymous /api/config, "" when the instance does not say.
func ytVersion(ctx context.Context, p *Prober, base string) string {
	var cfg struct {
		Version string `json:"version"`
	}
	asked := Join(base, "api/config?"+Q("fields", "version,build"))
	if res, ok := p.GetJSON(ctx, asked, &cfg); ok && answeredAt(res, asked) && ytVersionRe.MatchString(cfg.Version) {
		return cfg.Version
	}
	return ""
}

// ytAnonymousSignature recognizes YouTrack when /api/config is closed: its own 401 JSON (or the guest user) on
// /api/users/me together with its name in the start page's title. One of them alone is not enough.
func ytAnonymousSignature(ctx context.Context, p *Prober, base string) bool {
	asked := Join(base, "api/users/me?"+Q("fields", "id,login"))
	res, err := p.Get(ctx, asked, "")
	if err != nil || !res.JSON() || !answeredAt(res, asked) {
		return false
	}
	var body struct {
		Type  string `json:"$type"`
		Error string `json:"error"`
		Desc  string `json:"error_description"`
	}
	if res.Decode(&body) != nil {
		return false
	}
	api := (res.Status == 401 && strings.EqualFold(body.Error, "unauthorized") && strings.Contains(strings.ToLower(body.Desc), "authorization required")) ||
		(res.Status == 200 && body.Type == "Me")
	if !api {
		return false
	}
	page, err := p.Get(ctx, Join(base, "/"), "text/html")
	return err == nil && page.Status == 200 && ytTitleRe.Match(page.Body)
}

func ytDetection(base string, dep Deployment, version string) *Detection {
	return &Detection{
		Kind:       YouTrack,
		BaseURL:    base,
		Deployment: dep,
		Version:    version,
		Methods: []Method{{
			ID:          "pat",
			Recommended: true,
			TokenURL:    base + "/users/me?tab=account-security",
			Fields:      []Field{{ID: FieldToken, Secret: true, Placeholder: "perm:…"}},
		}},
		Capabilities: []Capability{Issues},
	}
}

// Open implements Provider.
func (youTrackProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &ytClient{conn: conn, token: strings.TrimSpace(cred[FieldToken]), h: h, base: strings.TrimRight(conn.BaseURL, "/")}
}

// Link implements Provider: /issue/KEY, /issues/KEY and /agiles/<board>/<sprint>?issue=KEY under the context path.
func (youTrackProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	base, err := url.Parse(conn.BaseURL)
	if err != nil || !strings.EqualFold(u.Host, base.Host) {
		return LinkTarget{}, false
	}
	path := strings.TrimPrefix(u.Path, strings.TrimRight(base.Path, "/"))
	segs := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case len(segs) >= 2 && (segs[0] == "issue" || segs[0] == "issues") && ytIssueRe.MatchString(segs[1]):
		return LinkTarget{Kind: "issue", Key: segs[1]}, true
	case len(segs) >= 1 && segs[0] == "agiles":
		if key := u.Query().Get("issue"); ytIssueRe.MatchString(key) {
			return LinkTarget{Kind: "issue", Key: key}, true
		}
	}
	return LinkTarget{}, false
}

// ytIssueFields is what an issue is read with: the runtime's own tracker fields (appsfeed, task, linkmeta), the same
// custom fields (Type, State, Priority, Assignee, Subsystem) and the parent.
const ytIssueFields = "id,idReadable,summary,description,created,updated,resolved,project(shortName,name)," +
	"reporter(login,fullName,avatarUrl),tags(name)," +
	"customFields($type,name,value($type,name,fullName,login,avatarUrl,isResolved))," +
	"parent(issues(idReadable,summary)),links(direction,linkType(name),issues(idReadable,summary))"

// ytCommentFields are the comments read with one issue.
const ytCommentFields = "comments(id,text,created,author(login,fullName,avatarUrl))"

type ytClient struct {
	conn  Conn
	token string
	h     *HTTP
	base  string
}

type ytUser struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	FullName  string `json:"fullName"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
}

type ytField struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type ytRef struct {
	IDReadable string `json:"idReadable"`
	Summary    string `json:"summary"`
}

type ytIssue struct {
	ID          string `json:"id"`
	IDReadable  string `json:"idReadable"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Created     int64  `json:"created"`
	Updated     int64  `json:"updated"`
	Resolved    int64  `json:"resolved"`
	Project     *struct {
		ShortName string `json:"shortName"`
		Name      string `json:"name"`
	} `json:"project"`
	Reporter     *ytUser                 `json:"reporter"`
	Tags         []struct{ Name string } `json:"tags"`
	CustomFields []ytField               `json:"customFields"`
	Parent       *struct {
		Issues []ytRef `json:"issues"`
	} `json:"parent"`
	Links []struct {
		Direction string `json:"direction"`
		LinkType  struct {
			Name string `json:"name"`
		} `json:"linkType"`
		Issues []ytRef `json:"issues"`
	} `json:"links"`
	Comments []struct {
		ID      string  `json:"id"`
		Text    string  `json:"text"`
		Created int64   `json:"created"`
		Author  *ytUser `json:"author"`
	} `json:"comments"`
}

// ytValue is a custom field value reduced to what is read from it: an enum or state (name, isResolved) or a user.
type ytValue struct {
	Name       string `json:"name"`
	Login      string `json:"login"`
	FullName   string `json:"fullName"`
	AvatarURL  string `json:"avatarUrl"`
	IsResolved bool   `json:"isResolved"`
}

func (c *ytClient) get(ctx context.Context, path string, query string, out any) error {
	if c.token == "" {
		return ErrDenied
	}
	_, err := c.h.JSON(ctx, Request{
		URL:    Join(c.base, "api/"+path) + "?" + query,
		Header: Header("Authorization", Bearer(c.token)),
	}, out)
	return err
}

// Me implements Client.
func (c *ytClient) Me(ctx context.Context) (*Person, error) {
	var u ytUser
	if err := c.get(ctx, "users/me", Q("fields", "id,login,fullName,email,avatarUrl"), &u); err != nil {
		return nil, err
	}
	return c.person(&u), nil
}

func (c *ytClient) person(u *ytUser) *Person {
	if u == nil || (u.Login == "" && u.FullName == "") {
		return nil
	}
	name := u.FullName
	if name == "" {
		name = u.Login
	}
	return &Person{ID: u.ID, Login: u.Login, Name: name, Email: u.Email, Avatar: ytAbs(c.base, u.AvatarURL)}
}

// ytAbs makes an avatar address absolute: YouTrack answers with /hub/api/rest/avatar/… relative to its root.
func ytAbs(base, p string) string {
	if p == "" || strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	b, err := url.Parse(base)
	if err != nil {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		return Join(base, p)
	}
	if bp := strings.TrimRight(b.Path, "/"); bp != "" && strings.HasPrefix(p, bp+"/") {
		return b.Scheme + "://" + b.Host + p
	}
	return Join(base, p)
}

// Projects implements IssueSource: the non-archived projects; a person without the admin read right gets the projects
// of their latest issues instead.
func (c *ytClient) Projects(ctx context.Context) ([]Project, error) {
	var list []struct {
		ShortName string `json:"shortName"`
		Name      string `json:"name"`
		Archived  bool   `json:"archived"`
	}
	err := c.get(ctx, "admin/projects", Q("fields", "shortName,name,archived", "$top", "200"), &list)
	if err != nil {
		if !ytIsAny(err, ErrDenied, ErrNotFound) {
			return nil, err
		}
		return c.projectsFromIssues(ctx)
	}
	out := make([]Project, 0, len(list))
	for _, p := range list {
		if p.Archived || p.ShortName == "" {
			continue
		}
		out = append(out, c.project(p.ShortName, p.Name))
	}
	return out, nil
}

func (c *ytClient) project(short, name string) Project {
	return Project{Key: short, Name: name, URL: c.base + "/issues?" + Q("q", "project: {"+short+"}")}
}

func (c *ytClient) projectsFromIssues(ctx context.Context) ([]Project, error) {
	var list []ytIssue
	if err := c.get(ctx, "issues", Q("query", "sort by: updated desc", "fields", "project(shortName,name)", "$top", "100"), &list); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []Project{}
	for _, i := range list {
		if i.Project == nil || seen[i.Project.ShortName] {
			continue
		}
		seen[i.Project.ShortName] = true
		out = append(out, c.project(i.Project.ShortName, i.Project.Name))
	}
	return out, nil
}

// ytQuery turns the common query into YouTrack's: free text, then `for: me`, `#Unresolved`, `project: {X}`, newest
// first. A native query is used as written.
func ytQuery(q Query) string {
	if q.Native != "" {
		return q.Native
	}
	var parts []string
	if t := strings.TrimSpace(q.Text); t != "" {
		parts = append(parts, t)
	}
	if q.Mine {
		parts = append(parts, "for: me")
	}
	if q.Open {
		parts = append(parts, "#Unresolved")
	}
	if q.Project != "" {
		parts = append(parts, "project: {"+q.Project+"}")
	}
	parts = append(parts, "sort by: updated desc")
	return strings.Join(parts, " ")
}

// Search implements IssueSource.
func (c *ytClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	var list []ytIssue
	err := c.get(ctx, "issues", Q("query", ytQuery(q), "fields", ytIssueFields, "$top", strconv.Itoa(q.LimitOr(30))), &list)
	if err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(list))
	for i := range list {
		out = append(out, c.issue(&list[i]))
	}
	return out, nil
}

// Issue implements IssueSource: the issue with its last 20 comments.
func (c *ytClient) Issue(ctx context.Context, key string) (*Issue, error) {
	key = strings.TrimSpace(key)
	if !ytIssueRe.MatchString(key) {
		return nil, ErrNotFound
	}
	var raw ytIssue
	if err := c.get(ctx, "issues/"+url.PathEscape(key), Q("fields", ytIssueFields+","+ytCommentFields), &raw); err != nil {
		return nil, err
	}
	i := c.issue(&raw)
	comments := raw.Comments
	if len(comments) > 20 {
		comments = comments[len(comments)-20:]
	}
	for _, cm := range comments {
		i.Comments = append(i.Comments, Comment{ID: cm.ID, Author: c.person(cm.Author), Text: cm.Text, Created: Millis(cm.Created)})
	}
	return &i, nil
}

func (c *ytClient) issue(r *ytIssue) Issue {
	i := Issue{
		Key:         r.IDReadable,
		ID:          r.ID,
		Title:       r.Summary,
		URL:         c.base + "/issue/" + r.IDReadable,
		Description: r.Description,
		Reporter:    c.person(r.Reporter),
		Created:     Millis(r.Created),
		Updated:     Millis(r.Updated),
		Resolved:    Millis(r.Resolved),
	}
	if r.Project != nil {
		i.Project = r.Project.ShortName
	}
	for _, t := range r.Tags {
		i.Labels = append(i.Labels, t.Name)
	}
	fields := map[string]ytValue{}
	for _, f := range r.CustomFields {
		if v, ok := ytDecodeValue(f.Value); ok {
			fields[f.Name] = v
		}
	}
	state, hasState := fields["State"]
	i.Status = ytStatus(state, hasState, r.Resolved != 0)
	i.Type = fields["Type"].Name
	i.Priority = fields["Priority"].Name
	if a, ok := fields["Assignee"]; ok {
		i.Assignee = c.person(&ytUser{Login: a.Login, FullName: a.FullName, AvatarURL: a.AvatarURL})
	}
	i.Parent = ytParent(r)
	return i
}

// ytParent is the issue this one is a subtask of: the parent shortcut, else a Subtask link in the inward direction.
func ytParent(r *ytIssue) *Ref {
	var ref *ytRef
	if r.Parent != nil && len(r.Parent.Issues) > 0 {
		ref = &r.Parent.Issues[0]
	}
	for _, l := range r.Links {
		if ref == nil && strings.EqualFold(l.LinkType.Name, "subtask") && l.Direction == "INWARD" && len(l.Issues) > 0 {
			ref = &l.Issues[0]
		}
	}
	if ref == nil {
		return nil
	}
	return &Ref{Key: ref.IDReadable, Title: ref.Summary}
}

// ytDecodeValue reads a custom field value: an object, the first of an array, or a bare string.
func ytDecodeValue(raw json.RawMessage) (ytValue, bool) {
	s := strings.TrimSpace(string(raw))
	var v ytValue
	switch {
	case strings.HasPrefix(s, "{"):
		return v, json.Unmarshal(raw, &v) == nil
	case strings.HasPrefix(s, "["):
		var list []ytValue
		if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
			return v, false
		}
		return list[0], true
	case strings.HasPrefix(s, `"`):
		return v, json.Unmarshal(raw, &v.Name) == nil
	}
	return v, false
}

// ytDoingWords mark a state as work in progress; YouTrack has no state categories, only isResolved.
var ytDoingWords = []string{"progress", "в работе", "в процессе", "doing", "started", "develop", "в разработке", "review", "ревью", "на проверке", "testing", "тестирован"}

func ytStatus(state ytValue, has, resolved bool) Status {
	if !has {
		if resolved {
			return Status{Name: "Resolved", Category: Done}
		}
		return Status{Category: Unknown}
	}
	if state.IsResolved || (resolved && state.Name == "") {
		return Status{Name: state.Name, Category: Done}
	}
	name := strings.ToLower(state.Name)
	for _, w := range ytDoingWords {
		if strings.Contains(name, w) {
			return Status{Name: state.Name, Category: Doing}
		}
	}
	return Status{Name: state.Name, Category: Todo}
}

// ytIsAny says whether err is one of targets.
func ytIsAny(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}
