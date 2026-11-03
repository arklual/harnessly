package connections

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Redmine answers no version before sign-in, so it is recognized by the shape of its REST lists or by its own markers
// on the login page. An issue's key is its plain numeric id ("123"); its URL is {base}/issues/123.

func init() { Register(&redmineProvider{}) }

type redmineProvider struct{}

func (*redmineProvider) Kind() Kind { return Redmine }

var redmineStops = []string{"issues", "projects", "my", "login", "logout", "users", "admin", "search", "time_entries", "activity",
	"news", "boards", "wiki", "settings", "account", "issue_categories", "versions", "groups", "roles", "trackers", "queries"}

func (*redmineProvider) Detect(ctx context.Context, p *Prober, u *url.URL) (*Detection, error) {
	for _, prefix := range PathPrefixes(u, redmineStops...) {
		base := Origin(u) + prefix
		if redmineLooksLike(ctx, p, base) {
			return &Detection{
				Kind: Redmine, BaseURL: base, Deployment: SelfHosted, Title: "Redmine",
				Methods: []Method{
					{ID: "api-key", Recommended: true, TokenURL: Join(base, "/my/account"),
						Fields: []Field{{ID: FieldToken, Secret: true, Placeholder: "API access key"}}},
					{ID: "basic", Fields: []Field{{ID: FieldUsername}, {ID: FieldPassword, Secret: true}}},
				},
				Capabilities: []Capability{Issues},
			}, nil
		}
	}
	return nil, nil
}

func redmineLooksLike(ctx context.Context, p *Prober, base string) bool {
	var list struct {
		Projects   *[]map[string]any `json:"projects"`
		TotalCount *int              `json:"total_count"`
		Offset     *int              `json:"offset"`
		Limit      *int              `json:"limit"`
	}
	if res, ok := p.GetJSON(ctx, Join(base, "/projects.json"), &list); ok && answeredAt(res, Join(base, "/projects.json")) && list.Projects != nil && list.TotalCount != nil && list.Offset != nil && list.Limit != nil {
		return true
	}
	res, err := p.Get(ctx, Join(base, "/login"), "text/html")
	if err != nil || res.Status != 200 || res.JSON() || !answeredAt(res, Join(base, "/login")) {
		return false
	}
	body := string(res.Body)
	return strings.Contains(body, `<meta name="description" content="Redmine"`) ||
		(strings.Contains(body, "Powered by") && strings.Contains(body, "redmine.org"))
}

func (*redmineProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &redmineClient{conn: conn, cred: cred, h: h}
}

// Link: {base}/issues/123.
func (*redmineProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	segs, ok := secondarySegments(conn.BaseURL, u)
	if !ok || len(segs) != 2 || segs[0] != "issues" {
		return LinkTarget{}, false
	}
	n := strings.TrimSuffix(segs[1], ".json")
	if i, err := strconv.Atoi(n); err != nil || i <= 0 {
		return LinkTarget{}, false
	}
	return LinkTarget{Kind: "issue", Key: n}, true
}

type redmineClient struct {
	conn Conn
	cred Credentials
	h    *HTTP

	mu     sync.Mutex
	closed map[int]bool
	loaded bool
}

func (c *redmineClient) req(path string) Request {
	r := Request{URL: Join(c.conn.BaseURL, path)}
	if c.conn.Method == "basic" {
		r.Header = Header("Authorization", Basic(c.cred[FieldUsername], c.cred[FieldPassword]))
	} else {
		r.Header = Header("X-Redmine-API-Key", c.cred[FieldToken])
	}
	return r
}

type redmineName struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (n *redmineName) person() *Person {
	if n == nil || n.Name == "" {
		return nil
	}
	return &Person{ID: strconv.Itoa(n.ID), Name: n.Name}
}

func (c *redmineClient) Me(ctx context.Context) (*Person, error) {
	var out struct {
		User struct {
			ID        int    `json:"id"`
			Login     string `json:"login"`
			FirstName string `json:"firstname"`
			LastName  string `json:"lastname"`
			Mail      string `json:"mail"`
		} `json:"user"`
	}
	if _, err := c.h.JSON(ctx, c.req("/users/current.json"), &out); err != nil {
		return nil, err
	}
	u := out.User
	if u.ID == 0 {
		return nil, fmt.Errorf("%w: no user in the answer", ErrDenied)
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Login
	}
	return &Person{ID: strconv.Itoa(u.ID), Login: u.Login, Name: name, Email: u.Mail}, nil
}

func (c *redmineClient) Projects(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []struct {
			Name       string `json:"name"`
			Identifier string `json:"identifier"`
		} `json:"projects"`
	}
	if _, err := c.h.JSON(ctx, c.req("/projects.json?"+Q("limit", "100")), &out); err != nil {
		return nil, err
	}
	ps := make([]Project, 0, len(out.Projects))
	for _, p := range out.Projects {
		ps = append(ps, Project{Key: p.Identifier, Name: p.Name, URL: Join(c.conn.BaseURL, "/projects/"+p.Identifier)})
	}
	return ps, nil
}

// closedStatuses are the ids of statuses Redmine marks as closing; empty when the list is not readable.
func (c *redmineClient) closedStatuses(ctx context.Context) map[int]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.closed
	}
	c.loaded = true
	var out struct {
		Statuses []struct {
			ID       int  `json:"id"`
			IsClosed bool `json:"is_closed"`
		} `json:"issue_statuses"`
	}
	if _, err := c.h.JSON(ctx, c.req("/issue_statuses.json"), &out); err != nil {
		return nil
	}
	c.closed = map[int]bool{}
	for _, s := range out.Statuses {
		c.closed[s.ID] = s.IsClosed
	}
	return c.closed
}

type redmineIssue struct {
	ID         int          `json:"id"`
	Project    *redmineName `json:"project"`
	Tracker    *redmineName `json:"tracker"`
	Status     redmineName  `json:"status"`
	Priority   *redmineName `json:"priority"`
	Author     *redmineName `json:"author"`
	AssignedTo *redmineName `json:"assigned_to"`
	Parent     *struct {
		ID int `json:"id"`
	} `json:"parent"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	CreatedOn   string `json:"created_on"`
	UpdatedOn   string `json:"updated_on"`
	ClosedOn    string `json:"closed_on"`
	Journals    []struct {
		ID        int          `json:"id"`
		User      *redmineName `json:"user"`
		Notes     string       `json:"notes"`
		CreatedOn string       `json:"created_on"`
	} `json:"journals"`
}

func redmineCategory(i redmineIssue, closed map[int]bool) Category {
	if i.ClosedOn != "" || closed[i.Status.ID] {
		return Done
	}
	n := strings.ToLower(i.Status.Name)
	switch {
	case n == "new" || n == "новая" || n == "open":
		return Todo
	case strings.Contains(n, "progress") || strings.Contains(n, "в работе") || strings.Contains(n, "feedback") || strings.Contains(n, "обратная связь"):
		return Doing
	case strings.Contains(n, "resolved") || strings.Contains(n, "решен") || strings.Contains(n, "closed") || strings.Contains(n, "закрыт") || strings.Contains(n, "rejected") || strings.Contains(n, "отклонен"):
		return Done
	}
	if len(closed) > 0 {
		return Todo
	}
	return Unknown
}

func (c *redmineClient) issue(i redmineIssue, closed map[int]bool) Issue {
	id := strconv.Itoa(i.ID)
	it := Issue{Key: id, ID: id, Title: i.Subject, URL: Join(c.conn.BaseURL, "/issues/"+id),
		Status: Status{Name: i.Status.Name, Category: redmineCategory(i, closed)}, Assignee: i.AssignedTo.person(), Reporter: i.Author.person(),
		Created: ParseTime(i.CreatedOn), Updated: ParseTime(i.UpdatedOn), Resolved: ParseTime(i.ClosedOn), Description: i.Description}
	if i.Project != nil {
		it.Project = i.Project.Name
	}
	if i.Tracker != nil {
		it.Type = i.Tracker.Name
	}
	if i.Priority != nil {
		it.Priority = i.Priority.Name
	}
	if i.Parent != nil && i.Parent.ID != 0 {
		pid := strconv.Itoa(i.Parent.ID)
		it.Parent = &Ref{Key: pid, URL: Join(c.conn.BaseURL, "/issues/"+pid)}
	}
	return it
}

func (c *redmineClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	v := url.Values{}
	v.Set("limit", strconv.Itoa(min(q.LimitOr(30), 100)))
	v.Set("sort", "updated_on:desc")
	v.Set("status_id", "*")
	if q.Open {
		v.Set("status_id", "open")
	}
	if q.Mine {
		v.Set("assigned_to_id", "me")
	}
	if q.Project != "" {
		v.Set("project_id", q.Project)
	}
	if q.Text != "" {
		v.Set("subject", "~"+q.Text)
	}
	if q.Native != "" {
		if nv, err := url.ParseQuery(strings.TrimPrefix(q.Native, "?")); err == nil {
			for k, vals := range nv {
				v[k] = vals
			}
		}
	}
	var out struct {
		Issues []redmineIssue `json:"issues"`
	}
	if _, err := c.h.JSON(ctx, c.req("/issues.json?"+v.Encode()), &out); err != nil {
		return nil, err
	}
	closed := c.closedStatuses(ctx)
	res := make([]Issue, 0, len(out.Issues))
	for _, i := range out.Issues {
		res = append(res, c.issue(i, closed))
	}
	return res, nil
}

func (c *redmineClient) Issue(ctx context.Context, key string) (*Issue, error) {
	key = strings.TrimPrefix(strings.TrimSpace(key), "#")
	if n, err := strconv.Atoi(key); err != nil || n <= 0 {
		return nil, fmt.Errorf("%w: bad issue id %q", ErrNotFound, key)
	}
	var out struct {
		Issue redmineIssue `json:"issue"`
	}
	if _, err := c.h.JSON(ctx, c.req("/issues/"+key+".json?"+Q("include", "journals")), &out); err != nil {
		return nil, err
	}
	it := c.issue(out.Issue, c.closedStatuses(ctx))
	for _, j := range out.Issue.Journals {
		if strings.TrimSpace(j.Notes) == "" {
			continue
		}
		it.Comments = append(it.Comments, Comment{ID: strconv.Itoa(j.ID), Author: j.User.person(), Text: j.Notes, Created: ParseTime(j.CreatedOn)})
	}
	if len(it.Comments) > 10 {
		it.Comments = it.Comments[len(it.Comments)-10:]
	}
	return &it, nil
}
