package connections

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Gitea and Forgejo share one API (/api/v1); Forgejo reports itself as a Gitea compatibility version
// ("9.0.0+gitea-1.22.0"). Issues are read through the repository endpoints, keys are owner/repo#n.

func init() { Register(&giteaProvider{}) }

type giteaProvider struct{}

func (*giteaProvider) Kind() Kind { return Gitea }

// giteaStops are the first path segments of Gitea's own routes: the instance's context path ends before them.
var giteaStops = []string{"issues", "pulls", "src", "commit", "commits", "wiki", "releases", "actions", "projects",
	"milestones", "labels", "user", "explore", "org", "admin", "api", "login", "-", "settings", "notifications"}

// secondarySegments is the path of u below the instance's base URL, split into segments; ok is false when u is on
// another host or outside the base path. Shared by the providers of this file group for their Link methods.
func secondarySegments(baseURL string, u *url.URL) ([]string, bool) {
	b, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(b.Host, u.Host) {
		return nil, false
	}
	bp := strings.Trim(b.EscapedPath(), "/")
	up := strings.Trim(u.EscapedPath(), "/")
	if bp != "" {
		if up != bp && !strings.HasPrefix(up, bp+"/") {
			return nil, false
		}
		up = strings.Trim(strings.TrimPrefix(up, bp), "/")
	}
	if up == "" {
		return nil, true
	}
	segs := strings.Split(up, "/")
	for i, s := range segs {
		if d, err := url.PathUnescape(s); err == nil {
			segs[i] = d
		}
	}
	return segs, true
}

func giteaTokenMethod(base string) Method {
	return Method{
		ID:          "pat",
		Recommended: true,
		TokenURL:    Join(base, "/user/settings/applications"),
		Fields:      []Field{{ID: FieldToken, Secret: true, Placeholder: "access token"}},
	}
}

func (g *giteaProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	if strings.EqualFold(u.Hostname(), "codeberg.org") {
		base := "https://codeberg.org"
		return &Detection{Kind: Gitea, BaseURL: base, Deployment: Cloud, Edition: "forgejo", Title: "Codeberg",
			Methods: []Method{giteaTokenMethod(base)}, Capabilities: []Capability{Issues, Code}}, nil
	}
	for _, prefix := range PathPrefixes(u, giteaStops...) {
		base := Origin(u) + prefix
		var v struct {
			Version string `json:"version"`
		}
		if res, ok := p.GetJSON(ctx, Join(base, "/api/v1/version"), &v); !ok || !answeredAt(res, Join(base, "/api/v1/version")) || v.Version == "" {
			continue
		}
		var s struct {
			MaxResponseItems *int `json:"max_response_items"`
			DefaultPagingNum *int `json:"default_paging_num"`
		}
		res, confirmed := p.GetJSON(ctx, Join(base, "/api/v1/settings/api"), &s)
		confirmed = confirmed && answeredAt(res, Join(base, "/api/v1/settings/api")) && (s.MaxResponseItems != nil || s.DefaultPagingNum != nil)
		var sw struct {
			Swagger string `json:"swagger"`
			Info    struct {
				Title string `json:"title"`
			} `json:"info"`
		}
		title := ""
		if !confirmed {
			if res, ok := p.GetJSON(ctx, Join(base, "/swagger.v1.json"), &sw); ok && answeredAt(res, Join(base, "/swagger.v1.json")) && sw.Swagger != "" {
				t := strings.ToLower(sw.Info.Title)
				confirmed = strings.Contains(t, "gitea") || strings.Contains(t, "forgejo")
				title = sw.Info.Title
			}
		}
		if !confirmed {
			continue
		}
		edition := "gitea"
		lv := strings.ToLower(v.Version)
		if strings.Contains(lv, "gitea-") || strings.Contains(lv, "forgejo") || strings.Contains(strings.ToLower(title), "forgejo") {
			edition = "forgejo"
		}
		if title == "" {
			title = strings.ToUpper(edition[:1]) + edition[1:]
		}
		return &Detection{Kind: Gitea, BaseURL: base, Deployment: SelfHosted, Version: v.Version, Edition: edition,
			Title: title, Methods: []Method{giteaTokenMethod(base)}, Capabilities: []Capability{Issues, Code}}, nil
	}
	return nil, nil
}

func (g *giteaProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &giteaClient{conn: conn, token: cred[FieldToken], h: h}
}

// Link: /owner/repo/issues/N and /owner/repo/pulls/N.
func (g *giteaProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	segs, ok := secondarySegments(conn.BaseURL, u)
	if !ok || len(segs) < 4 || (segs[2] != "issues" && segs[2] != "pulls") {
		return LinkTarget{}, false
	}
	if _, err := strconv.Atoi(segs[3]); err != nil {
		return LinkTarget{}, false
	}
	return LinkTarget{Kind: "issue", Key: segs[0] + "/" + segs[1] + "#" + segs[3]}, true
}

type giteaClient struct {
	conn  Conn
	token string
	h     *HTTP
}

type giteaUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (u *giteaUser) person() *Person {
	if u == nil || (u.Login == "" && u.FullName == "") {
		return nil
	}
	name := u.FullName
	if name == "" {
		name = u.Login
	}
	return &Person{ID: strconv.FormatInt(u.ID, 10), Login: u.Login, Name: name, Email: u.Email, Avatar: u.AvatarURL}
}

type giteaIssue struct {
	Number  int        `json:"number"`
	Title   string     `json:"title"`
	Body    string     `json:"body"`
	State   string     `json:"state"`
	HTMLURL string     `json:"html_url"`
	User    *giteaUser `json:"user"`
	Assign  *giteaUser `json:"assignee"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Created string `json:"created_at"`
	Updated string `json:"updated_at"`
	Closed  string `json:"closed_at"`
	Repo    *struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest *struct{} `json:"pull_request"`
}

func (c *giteaClient) api(path string) string {
	b := c.conn.APIURL
	if b == "" {
		b = c.conn.BaseURL
	}
	return Join(b, "/api/v1/"+strings.TrimLeft(path, "/"))
}

func (c *giteaClient) get(ctx context.Context, path string, out any) (*Response, error) {
	return c.h.JSON(ctx, Request{URL: c.api(path), Header: Header("Authorization", "token "+c.token)}, out)
}

func (c *giteaClient) Me(ctx context.Context) (*Person, error) {
	var u giteaUser
	if _, err := c.get(ctx, "user", &u); err != nil {
		return nil, err
	}
	if p := u.person(); p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("%w: no user in the answer", ErrUnreachable)
}

func (c *giteaClient) Projects(ctx context.Context) ([]Project, error) {
	var repos []struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	}
	if _, err := c.get(ctx, "user/repos?"+Q("limit", "50"), &repos); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(repos))
	for _, r := range repos {
		out = append(out, Project{Key: r.FullName, Name: r.FullName, URL: r.HTMLURL})
	}
	return out, nil
}

func (c *giteaClient) key(i giteaIssue) string {
	repo := ""
	if i.Repo != nil {
		repo = i.Repo.FullName
	}
	if repo == "" {
		if u, err := url.Parse(i.HTMLURL); err == nil {
			if segs, ok := secondarySegments(c.conn.BaseURL, u); ok && len(segs) >= 2 {
				repo = segs[0] + "/" + segs[1]
			}
		}
	}
	return fmt.Sprintf("%s#%d", repo, i.Number)
}

func (c *giteaClient) issue(i giteaIssue) Issue {
	cat := Todo
	if i.State == "closed" {
		cat = Done
	}
	var labels []string
	for _, l := range i.Labels {
		labels = append(labels, l.Name)
		n := strings.ToLower(l.Name)
		if cat == Todo && (strings.Contains(n, "progress") || strings.Contains(n, "doing") || n == "wip") {
			cat = Doing
		}
	}
	key := c.key(i)
	repo, _, _ := strings.Cut(key, "#")
	typ := "issue"
	if i.PullRequest != nil {
		typ = "pull request"
	}
	return Issue{Key: key, ID: strconv.Itoa(i.Number), Title: i.Title, URL: i.HTMLURL, Status: Status{Name: i.State, Category: cat},
		Type: typ, Project: repo, Assignee: i.Assign.person(), Reporter: i.User.person(), Labels: labels,
		Created: ParseTime(i.Created), Updated: ParseTime(i.Updated), Resolved: ParseTime(i.Closed), Description: i.Body}
}

func (c *giteaClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	state := "all"
	if q.Open {
		state = "open"
	}
	text := q.Text
	if q.Native != "" {
		text = q.Native
	}
	args := []string{"type", "issues", "state", state, "limit", strconv.Itoa(q.LimitOr(30)), "q", text}
	path := "repos/issues/search?"
	if strings.Contains(q.Project, "/") {
		if !ValidRepo(q.Project) {
			return nil, fmt.Errorf("%w: bad repository %q", ErrNotFound, q.Project)
		}
		path = "repos/" + q.Project + "/issues?"
		if q.Mine {
			me, err := c.Me(ctx)
			if err != nil {
				return nil, err
			}
			args = append(args, "assigned_by", me.Login)
		}
	} else if q.Mine {
		args = append(args, "assigned", "true")
	}
	var raw []giteaIssue
	if _, err := c.get(ctx, path+Q(args...), &raw); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(raw))
	for _, i := range raw {
		if i.PullRequest != nil {
			continue
		}
		it := c.issue(i)
		if strings.Contains(q.Project, "/") && i.Repo == nil {
			it.Key = fmt.Sprintf("%s#%d", q.Project, i.Number)
			it.Project = q.Project
		}
		out = append(out, it)
	}
	return out, nil
}

func (c *giteaClient) Issue(ctx context.Context, key string) (*Issue, error) {
	repo, num, ok := strings.Cut(key, "#")
	if !ok || !ValidRepo(repo) {
		return nil, fmt.Errorf("%w: bad issue key %q", ErrNotFound, key)
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("%w: bad issue key %q", ErrNotFound, key)
	}
	num = strconv.Itoa(n)
	var raw giteaIssue
	if _, err := c.get(ctx, "repos/"+repo+"/issues/"+num, &raw); err != nil {
		return nil, err
	}
	it := c.issue(raw)
	if raw.Repo == nil {
		it.Key, it.Project = key, repo
	}
	var comments []struct {
		ID      int64      `json:"id"`
		Body    string     `json:"body"`
		User    *giteaUser `json:"user"`
		Created string     `json:"created_at"`
	}
	res, err := c.h.Do(ctx, Request{URL: c.api("repos/" + repo + "/issues/" + num + "/comments"), Header: Header("Authorization", "token "+c.token)})
	if err == nil && res.Status == http.StatusOK && res.Decode(&comments) == nil {
		if len(comments) > 10 {
			comments = comments[len(comments)-10:]
		}
		for _, cm := range comments {
			it.Comments = append(it.Comments, Comment{ID: strconv.FormatInt(cm.ID, 10), Author: cm.User.person(), Text: cm.Body, Created: ParseTime(cm.Created)})
		}
	}
	return &it, nil
}
