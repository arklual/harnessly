package connections

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func init() { Register(&gitHubProvider{}) }

// gitHubProvider reads GitHub issues: github.com, GitHub Enterprise Server on any domain (API under /api/v3) and
// GHE.com data residency (<sub>.ghe.com, API on api.<sub>.ghe.com).
type gitHubProvider struct{}

// Kind implements Provider.
func (gitHubProvider) Kind() Kind { return GitHub }

// Detect implements Provider.
func (gitHubProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	host := u.Hostname()
	switch {
	case host == "github.com" || host == "www.github.com" || host == "api.github.com" || host == "gist.github.com":
		return ghDetection("https://github.com", "https://api.github.com", Cloud, "", ""), nil
	case HostIs(host, "ghe.com"):
		// api.<sub>.ghe.com and <sub>.ghe.com are the same tenant.
		sub := strings.TrimPrefix(host, "api.")
		return ghDetection("https://"+sub, "https://api."+sub, Cloud, "", "ghe.com"), nil
	}
	// GitHub Enterprise Server sits at the root of its host; its API is /api/v3.
	api := Origin(u) + "/api/v3"
	var meta struct {
		Version string `json:"installed_version"`
	}
	if _, ok := p.GetJSON(ctx, api+"/meta", &meta); ok && ghVersionRe.MatchString(meta.Version) {
		return ghDetection(Origin(u), api, SelfHosted, meta.Version, "server"), nil
	}
	// Private mode closes /meta, but every API answer of the server, a 401 too, names its version in a header.
	if res, err := p.Get(ctx, api, ""); err == nil {
		if v := res.Header.Get("X-GitHub-Enterprise-Version"); ghVersionRe.MatchString(v) {
			return ghDetection(Origin(u), api, SelfHosted, v, "server"), nil
		}
	}
	return nil, nil
}

var ghVersionRe = regexp.MustCompile(`^\d+\.\d+`)

func ghDetection(base, api string, dep Deployment, version, edition string) *Detection {
	return &Detection{
		Kind:       GitHub,
		BaseURL:    base,
		APIURL:     api,
		Deployment: dep,
		Version:    version,
		Edition:    edition,
		Methods: []Method{{
			ID:          "pat",
			Recommended: true,
			TokenURL:    base + "/settings/tokens/new?description=harnessly&scopes=repo,read:user",
			Fields:      []Field{{ID: FieldToken, Secret: true, Placeholder: "ghp_… / github_pat_…"}},
		}},
		Capabilities: []Capability{Issues, Code},
	}
}

// Open implements Provider.
func (gitHubProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	api := strings.TrimRight(conn.APIURL, "/")
	if api == "" {
		api = "https://api.github.com"
	}
	return &ghClient{conn: conn, token: strings.TrimSpace(cred[FieldToken]), h: h, api: api}
}

// Link implements Provider: /owner/repo/issues/12. Pull requests are not issues.
func (gitHubProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	base, err := url.Parse(conn.BaseURL)
	if err != nil {
		return LinkTarget{}, false
	}
	host := strings.ToLower(u.Hostname())
	if host != strings.ToLower(base.Hostname()) && !(base.Hostname() == "github.com" && host == "www.github.com") {
		return LinkTarget{}, false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) >= 4 && segs[2] == "issues" {
		if n, err := strconv.Atoi(segs[3]); err == nil && n > 0 {
			return LinkTarget{Kind: "issue", Key: segs[0] + "/" + segs[1] + "#" + segs[3]}, true
		}
	}
	return LinkTarget{}, false
}

type ghClient struct {
	conn  Conn
	token string
	h     *HTTP
	api   string
}

type ghUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (u *ghUser) person() *Person {
	if u == nil || u.Login == "" {
		return nil
	}
	name := u.Name
	if name == "" {
		name = u.Login
	}
	p := &Person{Login: u.Login, Name: name, Email: u.Email, Avatar: u.AvatarURL}
	if u.ID != 0 {
		p.ID = strconv.FormatInt(u.ID, 10)
	}
	return p
}

func (c *ghClient) get(ctx context.Context, path, query string, out any) (*Response, error) {
	if c.token == "" {
		return nil, ErrDenied
	}
	u := c.api + "/" + strings.TrimLeft(path, "/")
	if query != "" {
		u += "?" + query
	}
	h := Header("Authorization", Bearer(c.token), "Accept", "application/vnd.github+json")
	if c.conn.Deployment == Cloud {
		// Older Enterprise Server releases know no API versions: the header is only for the cloud.
		h.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	return c.h.JSON(ctx, Request{URL: u, Header: h}, out)
}

// Me implements Client.
func (c *ghClient) Me(ctx context.Context) (*Person, error) {
	var u ghUser
	if _, err := c.get(ctx, "user", "", &u); err != nil {
		return nil, err
	}
	return u.person(), nil
}

// Projects implements IssueSource: the repositories the person can reach, latest push first, archived left out.
func (c *ghClient) Projects(ctx context.Context) ([]Project, error) {
	var list []struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
		Archived bool   `json:"archived"`
	}
	if _, err := c.get(ctx, "user/repos", Q("sort", "updated", "per_page", "100"), &list); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(list))
	for _, r := range list {
		if !r.Archived {
			out = append(out, Project{Key: r.FullName, Name: r.FullName, URL: r.HTMLURL})
		}
	}
	return out, nil
}

// ghQuery builds the q of /search/issues: issues only, then the filters and the text.
func ghQuery(q Query) string {
	if n := strings.TrimSpace(q.Native); n != "" {
		if !strings.Contains(n, "is:issue") && !strings.Contains(n, "is:pr") {
			n = "is:issue " + n
		}
		return n
	}
	parts := []string{"is:issue"}
	if q.Mine {
		parts = append(parts, "assignee:@me")
	}
	if q.Open {
		parts = append(parts, "state:open")
	}
	if p := strings.TrimSpace(q.Project); p != "" {
		if strings.Contains(p, "/") {
			parts = append(parts, "repo:"+p)
		} else {
			parts = append(parts, "user:"+p)
		}
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

// Search implements IssueSource.
func (c *ghClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	var out struct {
		Items []ghIssue `json:"items"`
	}
	params := Q("q", ghQuery(q), "sort", "updated", "order", "desc", "per_page", strconv.Itoa(min(q.LimitOr(30), 100)))
	if _, err := c.get(ctx, "search/issues", params, &out); err != nil {
		return nil, err
	}
	list := make([]Issue, 0, len(out.Items))
	for i := range out.Items {
		if out.Items[i].PullRequest != nil {
			continue
		}
		list = append(list, ghMap(&out.Items[i]))
	}
	return list, nil
}

// Issue implements IssueSource: key is "owner/repo#12". The parent and the last 20 comments are read too; a missing
// one (older servers have no sub-issues) is not an error.
func (c *ghClient) Issue(ctx context.Context, key string) (*Issue, error) {
	repo, num, ok := strings.Cut(strings.TrimSpace(key), "#")
	n, err := strconv.Atoi(num)
	if !ok || err != nil || n <= 0 || !ValidRepo(repo) {
		return nil, ErrNotFound
	}
	path := "repos/" + repo + "/issues/" + strconv.Itoa(n)
	var raw ghIssue
	if _, err := c.get(ctx, path, "", &raw); err != nil {
		return nil, err
	}
	if raw.PullRequest != nil {
		return nil, ErrNotFound
	}
	i := ghMap(&raw)
	if i.Key == "" {
		i.Key = key
		i.Project = repo
	}
	var parent ghIssue
	if _, err := c.get(ctx, path+"/parent", "", &parent); err == nil && parent.Number > 0 {
		i.Parent = &Ref{Key: repo + "#" + strconv.Itoa(parent.Number), Title: parent.Title, URL: parent.HTMLURL}
	}
	if raw.Comments > 0 {
		page := (raw.Comments + 99) / 100
		var comments []struct {
			ID        int64   `json:"id"`
			Body      string  `json:"body"`
			CreatedAt string  `json:"created_at"`
			User      *ghUser `json:"user"`
		}
		if _, err := c.get(ctx, path+"/comments", Q("per_page", "100", "page", strconv.Itoa(page)), &comments); err == nil {
			if len(comments) > 20 {
				comments = comments[len(comments)-20:]
			}
			for _, cm := range comments {
				i.Comments = append(i.Comments, Comment{ID: strconv.FormatInt(cm.ID, 10), Author: cm.User.person(), Text: cm.Body, Created: ParseTime(cm.CreatedAt)})
			}
		}
	}
	return &i, nil
}

type ghIssue struct {
	ID            int64   `json:"id"`
	Number        int     `json:"number"`
	Title         string  `json:"title"`
	Body          string  `json:"body"`
	State         string  `json:"state"`
	StateReason   string  `json:"state_reason"`
	HTMLURL       string  `json:"html_url"`
	RepositoryURL string  `json:"repository_url"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	ClosedAt      string  `json:"closed_at"`
	Comments      int     `json:"comments"`
	User          *ghUser `json:"user"`
	Assignee      *ghUser `json:"assignee"`
	Labels        []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Type *struct {
		Name string `json:"name"`
	} `json:"type"`
	PullRequest *struct{} `json:"pull_request"`
}

// ghDoing are label names that say an open issue is being worked on.
var ghDoing = []string{"in progress", "in-progress", "doing", "wip", "in review", "started"}

// ghRepo is "owner/repo" from the issue's repository_url (…/repos/owner/repo), else from its html_url.
func ghRepo(r *ghIssue) string {
	if _, rest, ok := strings.Cut(r.RepositoryURL, "/repos/"); ok {
		return rest
	}
	if u, err := url.Parse(r.HTMLURL); err == nil {
		if segs := strings.Split(strings.Trim(u.Path, "/"), "/"); len(segs) >= 2 {
			return segs[0] + "/" + segs[1]
		}
	}
	return ""
}

func ghMap(r *ghIssue) Issue {
	repo := ghRepo(r)
	i := Issue{
		ID:          strconv.FormatInt(r.ID, 10),
		Title:       r.Title,
		URL:         r.HTMLURL,
		Description: r.Body,
		Project:     repo,
		Assignee:    r.Assignee.person(),
		Reporter:    r.User.person(),
		Created:     ParseTime(r.CreatedAt),
		Updated:     ParseTime(r.UpdatedAt),
		Resolved:    ParseTime(r.ClosedAt),
	}
	if repo != "" {
		i.Key = repo + "#" + strconv.Itoa(r.Number)
	}
	if r.Type != nil {
		i.Type = r.Type.Name
	}
	i.Status = Status{Name: "open", Category: Todo}
	if r.State == "closed" {
		name := "closed"
		if r.StateReason != "" {
			name = strings.ReplaceAll(r.StateReason, "_", " ")
		}
		i.Status = Status{Name: name, Category: Done}
	}
	for _, l := range r.Labels {
		i.Labels = append(i.Labels, l.Name)
		low := strings.ToLower(l.Name)
		if rest, ok := strings.CutPrefix(low, "priority"); ok && i.Priority == "" {
			i.Priority = strings.Trim(rest, ": /-")
		}
		if r.State != "closed" && slices.Contains(ghDoing, low) {
			i.Status = Status{Name: l.Name, Category: Doing}
		}
	}
	return i
}
