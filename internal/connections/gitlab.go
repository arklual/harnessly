package connections

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func init() { Register(&gitLabProvider{}) }

// gitLabProvider reads GitLab issues: gitlab.com and self-managed instances on any domain and under any subpath
// (https://example.com/gitlab).
type gitLabProvider struct{}

// Kind implements Provider.
func (gitLabProvider) Kind() Kind { return GitLab }

// glStops are GitLab's own route starts. A project path has no fixed shape, so only the "-" separator, the API and the
// few top-level pages cut a pasted link; the remaining segments are tried as context paths, longest first.
var glStops = []string{"-", "api", "users", "groups", "explore", "dashboard", "admin", "projects", "help", "search", "oauth", "profile"}

// Detect implements Provider.
func (gitLabProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	if HostIs(u.Hostname(), "gitlab.com") {
		return glDetection("https://gitlab.com", Cloud, "", ""), nil
	}
	for _, prefix := range PathPrefixes(u, glStops...) {
		base := Origin(u) + prefix
		if version, edition, ok := glProbe(ctx, p, base); ok {
			return glDetection(base, SelfHosted, version, edition), nil
		}
	}
	return nil, nil
}

// glProbe asks one candidate root for GitLab's signatures, anonymously. A 404 never counts: GitLab sends its headers
// on those too, and a wrong root must not win. version is filled only when the instance says it before sign-in.
func glProbe(ctx context.Context, p *Prober, base string) (version, edition string, ok bool) {
	// Every probe must be answered at the very address asked: GitLab redirects any path under a project to its sign-in
	// page, so a project's path would otherwise pass for the instance's root.
	// /api/v4/version: 200 JSON on instances that allow anonymous reads, else 401 from GitLab (X-Gitlab-Meta header).
	if res, err := p.Get(ctx, Join(base, "api/v4/version"), ""); err == nil && answeredAt(res, Join(base, "api/v4/version")) {
		var v struct {
			Version  string `json:"version"`
			Revision string `json:"revision"`
		}
		switch {
		case res.Status == 200 && res.JSON() && res.Decode(&v) == nil && v.Version != "" && v.Revision != "":
			ed := "ce"
			if strings.HasSuffix(v.Version, "-ee") {
				ed = "ee"
			}
			return v.Version, ed, true
		case res.Status == 401 && res.Header.Get("X-Gitlab-Meta") != "" && res.JSON():
			ok = true
		}
	}
	// /-/manifest.json: the PWA manifest names the product.
	if !ok {
		var m struct {
			Name string `json:"name"`
		}
		if res, found := p.GetJSON(ctx, Join(base, "-/manifest.json"), &m); found && res.Status == 200 && m.Name == "GitLab" && answeredAt(res, Join(base, "-/manifest.json")) {
			ok = true
		}
	}
	// /users/sign_in: the page's own markers.
	if !ok {
		if res, err := p.Get(ctx, Join(base, "users/sign_in"), "text/html"); err == nil && res.Status == 200 && answeredAt(res, Join(base, "users/sign_in")) {
			body := string(res.Body)
			ok = res.Header.Get("X-Gitlab-Meta") != "" || strings.Contains(body, "gon.gitlab_url") ||
				strings.Contains(body, `content="GitLab" property="og:site_name"`) || strings.Contains(body, `property="og:site_name" content="GitLab"`)
		}
	}
	return "", "", ok
}

func glDetection(base string, dep Deployment, version, edition string) *Detection {
	return &Detection{
		Kind:       GitLab,
		BaseURL:    base,
		Deployment: dep,
		Version:    version,
		Edition:    edition,
		Methods: []Method{{
			ID:          "pat",
			Recommended: true,
			TokenURL:    glTokenURL(base, version),
			Fields:      []Field{{ID: FieldToken, Secret: true, Placeholder: "glpat-…"}},
		}},
		Capabilities: []Capability{Issues, Code},
	}
}

// glTokenURL is the page that creates a token with the scopes reading needs; before GitLab 16 it lived under the
// profile.
func glTokenURL(base, version string) string {
	if major, _, _ := strings.Cut(version, "."); major != "" {
		if n, err := strconv.Atoi(major); err == nil && n < 16 {
			return base + "/-/profile/personal_access_tokens?name=harnessly&scopes=read_api,read_user"
		}
	}
	return base + "/-/user_settings/personal_access_tokens?name=harnessly&scopes=read_api,read_user"
}

// Open implements Provider.
func (gitLabProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &glClient{conn: conn, token: strings.TrimSpace(cred[FieldToken]), h: h, base: strings.TrimRight(conn.BaseURL, "/")}
}

// glLinkRe reads the path of an issue link below the instance root: group/sub/project[/-]/issues|work_items/N.
var glLinkRe = regexp.MustCompile(`^/(.+?)(?:/-)?/(?:issues|work_items)/(\d+)/?$`)

// Link implements Provider. Issues and work items only: merge requests are not issues, the runtime shows them itself.
func (gitLabProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	base, err := url.Parse(conn.BaseURL)
	if err != nil || !strings.EqualFold(u.Host, base.Host) {
		return LinkTarget{}, false
	}
	rest := strings.TrimPrefix(u.Path, strings.TrimRight(base.Path, "/"))
	m := glLinkRe.FindStringSubmatch(rest)
	if m == nil || strings.HasPrefix(m[1], "groups/") || strings.HasPrefix(m[1], "-/") {
		return LinkTarget{}, false
	}
	return LinkTarget{Kind: "issue", Key: m[1] + "#" + m[2]}, true
}

type glClient struct {
	conn  Conn
	token string
	h     *HTTP
	base  string
}

type glUser struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (c *glClient) person(u *glUser) *Person {
	if u == nil || (u.Username == "" && u.Name == "") {
		return nil
	}
	name := u.Name
	if name == "" {
		name = u.Username
	}
	p := &Person{Login: u.Username, Name: name, Email: u.Email, Avatar: ytAbs(c.base, u.AvatarURL)}
	if u.ID != 0 {
		p.ID = strconv.Itoa(u.ID)
	}
	return p
}

func (c *glClient) get(ctx context.Context, path, query string, out any) (*Response, error) {
	if c.token == "" {
		return nil, ErrDenied
	}
	u := c.base + "/api/v4/" + strings.TrimLeft(path, "/")
	if query != "" {
		u += "?" + query
	}
	return c.h.JSON(ctx, Request{URL: u, Header: Header("PRIVATE-TOKEN", c.token)}, out)
}

// Me implements Client.
func (c *glClient) Me(ctx context.Context) (*Person, error) {
	var u glUser
	if _, err := c.get(ctx, "user", "", &u); err != nil {
		return nil, err
	}
	return c.person(&u), nil
}

// Projects implements IssueSource: the projects the person is a member of, newest activity first.
func (c *glClient) Projects(ctx context.Context) ([]Project, error) {
	var list []struct {
		Path   string `json:"path_with_namespace"`
		Name   string `json:"name_with_namespace"`
		WebURL string `json:"web_url"`
	}
	q := Q("membership", "true", "simple", "true", "archived", "false", "order_by", "last_activity_at", "per_page", "100")
	if _, err := c.get(ctx, "projects", q, &list); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(list))
	for _, p := range list {
		out = append(out, Project{Key: p.Path, Name: p.Name, URL: p.WebURL})
	}
	return out, nil
}

// glSearchParams builds the query of an issue list. The project goes into the path, not here.
func glSearchParams(q Query) string {
	params := []string{"state", "all", "order_by", "updated_at", "sort", "desc", "per_page", strconv.Itoa(min(q.LimitOr(30), 100))}
	if q.Open {
		params[1] = "opened"
	}
	text := strings.TrimSpace(q.Text)
	extra := url.Values{}
	if q.Native != "" {
		// A native query is GitLab's own list parameters (labels=bug&milestone=1.0), else plain search text.
		if parsed, err := url.ParseQuery(q.Native); err == nil && strings.Contains(q.Native, "=") {
			extra = parsed
		} else {
			text = strings.TrimSpace(q.Native)
		}
	}
	if text != "" {
		params = append(params, "search", text)
	}
	v := url.Values{}
	for i := 0; i+1 < len(params); i += 2 {
		v.Set(params[i], params[i+1])
	}
	for k, vals := range extra {
		v[k] = vals
	}
	return v.Encode()
}

// Search implements IssueSource.
func (c *glClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	path := "issues"
	params := glSearchParams(q)
	if q.Project != "" {
		path = "projects/" + url.PathEscape(q.Project) + "/issues"
		if q.Mine {
			params += "&scope=assigned_to_me"
		}
	} else if q.Mine {
		params += "&scope=assigned_to_me"
	} else {
		params += "&scope=all"
	}
	var list []glIssue
	if _, err := c.get(ctx, path, params, &list); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(list))
	for i := range list {
		out = append(out, c.issue(&list[i]))
	}
	return out, nil
}

// Issue implements IssueSource: key is "group/sub/project#iid".
func (c *glClient) Issue(ctx context.Context, key string) (*Issue, error) {
	project, iid, ok := strings.Cut(strings.TrimSpace(key), "#")
	if n, err := strconv.Atoi(iid); !ok || project == "" || err != nil || n <= 0 {
		return nil, ErrNotFound
	}
	base := "projects/" + url.PathEscape(strings.Trim(project, "/")) + "/issues/" + iid
	var raw glIssue
	if _, err := c.get(ctx, base, "", &raw); err != nil {
		return nil, err
	}
	i := c.issue(&raw)
	if i.Project == "" {
		i.Project = project
		i.Key = project + "#" + iid
	}
	// The last 20 comments are a nicety: the issue is returned even when they cannot be read.
	var notes []struct {
		ID        int     `json:"id"`
		Body      string  `json:"body"`
		System    bool    `json:"system"`
		CreatedAt string  `json:"created_at"`
		Author    *glUser `json:"author"`
	}
	if _, err := c.get(ctx, base+"/notes", Q("sort", "desc", "order_by", "created_at", "per_page", "50"), &notes); err == nil {
		for _, n := range notes {
			if n.System || len(i.Comments) >= 20 {
				continue
			}
			i.Comments = append(i.Comments, Comment{ID: strconv.Itoa(n.ID), Author: c.person(n.Author), Text: n.Body, Created: ParseTime(n.CreatedAt)})
		}
		slices.Reverse(i.Comments)
	}
	return &i, nil
}

type glIssue struct {
	ID          int      `json:"id"`
	IID         int      `json:"iid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"`
	Labels      []string `json:"labels"`
	WebURL      string   `json:"web_url"`
	IssueType   string   `json:"issue_type"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	ClosedAt    string   `json:"closed_at"`
	Assignees   []glUser `json:"assignees"`
	Assignee    *glUser  `json:"assignee"`
	Author      *glUser  `json:"author"`
	References  struct {
		Full string `json:"full"`
	} `json:"references"`
}

// glDoing are label words (also as scoped labels, status::doing) that say an open issue is being worked on.
var glDoing = []string{"doing", "in progress", "in-progress", "in review", "wip", "started"}

func (c *glClient) issue(r *glIssue) Issue {
	project, _, _ := strings.Cut(r.References.Full, "#")
	if project == "" {
		// web_url is <base>/<path>/-/issues/<iid>.
		if rest, ok := strings.CutPrefix(r.WebURL, c.base+"/"); ok {
			project, _, _ = strings.Cut(rest, "/-/")
		}
	}
	i := Issue{
		ID:          strconv.Itoa(r.ID),
		Title:       r.Title,
		URL:         r.WebURL,
		Description: r.Description,
		Type:        r.IssueType,
		Project:     project,
		Reporter:    c.person(r.Author),
		Created:     ParseTime(r.CreatedAt),
		Updated:     ParseTime(r.UpdatedAt),
		Resolved:    ParseTime(r.ClosedAt),
	}
	if project != "" {
		i.Key = project + "#" + strconv.Itoa(r.IID)
	}
	if r.State == "closed" {
		i.Status = Status{Name: "closed", Category: Done}
	} else {
		i.Status = Status{Name: "opened", Category: Todo}
	}
	assignee := r.Assignee
	if assignee == nil && len(r.Assignees) > 0 {
		assignee = &r.Assignees[0]
	}
	i.Assignee = c.person(assignee)
	for _, l := range r.Labels {
		name, value, scoped := strings.Cut(l, "::")
		switch {
		case scoped && strings.EqualFold(name, "priority"):
			i.Priority = value
		case scoped && strings.EqualFold(name, "type"):
			i.Type = value
		case scoped && strings.EqualFold(name, "status") && r.State != "closed":
			i.Status = Status{Name: value, Category: glScopedCategory(value)}
		}
		if r.State != "closed" && i.Status.Category == Todo && slices.Contains(glDoing, strings.ToLower(l)) {
			i.Status = Status{Name: l, Category: Doing}
		}
		i.Labels = append(i.Labels, l)
	}
	return i
}

func glScopedCategory(v string) Category {
	if slices.Contains(glDoing, strings.ToLower(v)) {
		return Doing
	}
	return Todo
}
