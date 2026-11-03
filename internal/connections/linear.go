package connections

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

func init() { Register(&linearProvider{}) }

// linearProvider reads Linear: one SaaS (linear.app) with a GraphQL API.
type linearProvider struct{}

const (
	linearAPI      = "https://api.linear.app/graphql"
	linearTokenURL = "https://linear.app/settings/account/security"
)

// linearKeyRe is the shape of an issue identifier.
var linearKeyRe = regexp.MustCompile(`^[A-Za-z0-9]+-\d+$`)

// Kind implements Provider.
func (linearProvider) Kind() Kind { return Linear }

// Detect implements Provider: only the host linear.app is Linear, no probing needed.
func (linearProvider) Detect(_ context.Context, _ *Prober, u *url.URL) (*Detection, error) {
	if !HostIs(u.Hostname(), "linear.app") || u.Hostname() == "api.linear.app" {
		return nil, nil
	}
	d := &Detection{
		Kind:       Linear,
		BaseURL:    "https://linear.app",
		APIURL:     linearAPI,
		Deployment: Cloud,
		Methods: []Method{{
			ID:          "api-key",
			Recommended: true,
			TokenURL:    linearTokenURL,
			Fields:      []Field{{ID: FieldToken, Secret: true, Placeholder: "lin_api_…"}},
		}},
		Capabilities: []Capability{Issues},
	}
	if ws := linearWorkspace(u.Path); ws != "" {
		d.Extra = map[string]string{"workspace": ws}
	}
	return d, nil
}

// linearReserved are first path segments that are no workspace.
var linearReserved = map[string]bool{"settings": true, "login": true, "signup": true, "join": true, "api": true, "docs": true, "app": true}

func linearWorkspace(path string) string {
	seg, _, _ := strings.Cut(strings.Trim(path, "/"), "/")
	if seg == "" || linearReserved[seg] {
		return ""
	}
	return seg
}

// Open implements Provider.
func (linearProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	api := conn.APIURL
	if api == "" {
		api = linearAPI
	}
	return &linearClient{conn: conn, token: strings.TrimSpace(cred[FieldToken]), h: h, api: api}
}

// Link implements Provider: linear.app/<workspace>/issue/ABC-12[/slug].
func (linearProvider) Link(_ Conn, u *url.URL) (LinkTarget, bool) {
	if !strings.EqualFold(u.Hostname(), "linear.app") {
		return LinkTarget{}, false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) >= 3 && segs[1] == "issue" && linearKeyRe.MatchString(segs[2]) {
		return LinkTarget{Kind: "issue", Key: strings.ToUpper(segs[2])}, true
	}
	return LinkTarget{}, false
}

type linearClient struct {
	conn  Conn
	token string
	h     *HTTP
	api   string
}

type linearUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	AvatarURL   string `json:"avatarUrl"`
}

func (u *linearUser) person() *Person {
	if u == nil || (u.Name == "" && u.DisplayName == "") {
		return nil
	}
	name := u.Name
	if name == "" {
		name = u.DisplayName
	}
	return &Person{ID: u.ID, Login: u.DisplayName, Name: name, Email: u.Email, Avatar: u.AvatarURL}
}

const linearUserFields = "id name displayName email avatarUrl"

// linearIssueFields is what an issue is read with.
const linearIssueFields = `id identifier title url description priority priorityLabel createdAt updatedAt completedAt canceledAt
	state { name type } assignee { ` + linearUserFields + ` } creator { ` + linearUserFields + ` }
	team { key name } parent { identifier title url } project { name } labels { nodes { name } }`

type linearIssue struct {
	ID            string `json:"id"`
	Identifier    string `json:"identifier"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	Description   string `json:"description"`
	PriorityLabel string `json:"priorityLabel"`
	Priority      int    `json:"priority"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	CompletedAt   string `json:"completedAt"`
	CanceledAt    string `json:"canceledAt"`
	State         *struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	Assignee *linearUser `json:"assignee"`
	Creator  *linearUser `json:"creator"`
	Team     *struct {
		Key string `json:"key"`
	} `json:"team"`
	Parent *struct {
		Identifier string `json:"identifier"`
		Title      string `json:"title"`
		URL        string `json:"url"`
	} `json:"parent"`
	Project *struct {
		Name string `json:"name"`
	} `json:"project"`
	Labels *struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Comments *struct {
		Nodes []struct {
			ID        string      `json:"id"`
			Body      string      `json:"body"`
			CreatedAt string      `json:"createdAt"`
			User      *linearUser `json:"user"`
		} `json:"nodes"`
	} `json:"comments"`
}

// linearError is an error the GraphQL API reported in its answer.
type linearError struct{ msg string }

func (e *linearError) Error() string { return "linear: " + e.msg }
func (e *linearError) Unwrap() error { return ErrUnreachable }

// gql runs one query. Linear answers errors with status 200 or 400 and an errors list, so the body is read first.
func (c *linearClient) gql(ctx context.Context, query string, vars map[string]any, out any) error {
	if c.token == "" {
		return ErrDenied
	}
	auth := c.token
	if strings.HasPrefix(auth, "Bearer ") || strings.HasPrefix(auth, "lin_oauth_") {
		auth = Bearer(strings.TrimPrefix(auth, "Bearer "))
	}
	res, err := c.h.Do(ctx, Request{
		Method: "POST",
		URL:    c.api,
		Header: Header("Authorization", auth),
		Body:   map[string]any{"query": query, "variables": vars},
	})
	if err != nil {
		return err
	}
	var body struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Type string `json:"type"`
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if res.JSON() && res.Decode(&body) == nil && len(body.Errors) > 0 {
		e := body.Errors[0]
		low := strings.ToLower(e.Message + " " + e.Extensions.Type + " " + e.Extensions.Code)
		switch {
		case res.Status == 401 || res.Status == 403 || strings.Contains(low, "authentication") || strings.Contains(low, "forbidden"):
			return fmt.Errorf("%w: linear refused the key", ErrDenied)
		case strings.Contains(low, "not found"):
			return fmt.Errorf("%w: linear: %s", ErrNotFound, e.Message)
		}
		return &linearError{msg: e.Message}
	}
	if err := StatusErr(res); err != nil {
		return err
	}
	return res.Decode(&struct {
		Data any `json:"data"`
	}{Data: out})
}

// Me implements Client.
func (c *linearClient) Me(ctx context.Context) (*Person, error) {
	var out struct {
		Viewer linearUser `json:"viewer"`
	}
	if err := c.gql(ctx, "query { viewer { "+linearUserFields+" } }", nil, &out); err != nil {
		return nil, err
	}
	return out.Viewer.person(), nil
}

// Projects implements IssueSource: Linear's teams.
func (c *linearClient) Projects(ctx context.Context) ([]Project, error) {
	var out struct {
		Teams struct {
			Nodes []struct {
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := c.gql(ctx, "query { teams(first: 100) { nodes { key name } } }", nil, &out); err != nil {
		return nil, err
	}
	ws := c.conn.Extra["workspace"]
	list := make([]Project, 0, len(out.Teams.Nodes))
	for _, t := range out.Teams.Nodes {
		p := Project{Key: t.Key, Name: t.Name}
		if ws != "" {
			p.URL = "https://linear.app/" + ws + "/team/" + t.Key
		}
		list = append(list, p)
	}
	return list, nil
}

// linearFilter is the IssueFilter for the query's flags; the text is not in it.
func linearFilter(q Query) map[string]any {
	f := map[string]any{}
	if q.Mine {
		f["assignee"] = map[string]any{"isMe": map[string]any{"eq": true}}
	}
	if q.Open {
		f["state"] = map[string]any{"type": map[string]any{"nin": []string{"completed", "canceled"}}}
	}
	if q.Project != "" {
		f["team"] = map[string]any{"key": map[string]any{"eqIgnoreCase": q.Project}}
	}
	return f
}

// Search implements IssueSource. Text goes to searchIssues (relevance); when the API refuses that query the same
// search runs as a title filter on issues. Without text issues are listed newest-updated first.
func (c *linearClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	text := strings.TrimSpace(q.Text)
	if q.Native != "" {
		text = strings.TrimSpace(q.Native)
	}
	n := q.LimitOr(30)
	filter := linearFilter(q)
	if text != "" {
		var out struct {
			SearchIssues struct {
				Nodes []linearIssue `json:"nodes"`
			} `json:"searchIssues"`
		}
		err := c.gql(ctx, "query($term: String!, $first: Int, $filter: IssueFilter) { searchIssues(term: $term, first: $first, filter: $filter) { nodes { "+linearIssueFields+" } } }",
			map[string]any{"term": text, "first": n, "filter": filter}, &out)
		if err == nil {
			return c.issues(out.SearchIssues.Nodes), nil
		}
		if errors.Is(err, ErrDenied) {
			return nil, err
		}
		filter["title"] = map[string]any{"containsIgnoreCase": text}
	}
	var out struct {
		Issues struct {
			Nodes []linearIssue `json:"nodes"`
		} `json:"issues"`
	}
	err := c.gql(ctx, "query($first: Int, $filter: IssueFilter) { issues(first: $first, orderBy: updatedAt, filter: $filter) { nodes { "+linearIssueFields+" } } }",
		map[string]any{"first": n, "filter": filter}, &out)
	if err != nil {
		return nil, err
	}
	return c.issues(out.Issues.Nodes), nil
}

func (c *linearClient) issues(nodes []linearIssue) []Issue {
	list := make([]Issue, 0, len(nodes))
	for i := range nodes {
		list = append(list, linearMap(&nodes[i]))
	}
	return list
}

// Issue implements IssueSource: by identifier (ABC-12), with its last 20 comments.
func (c *linearClient) Issue(ctx context.Context, key string) (*Issue, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	if !linearKeyRe.MatchString(key) {
		return nil, ErrNotFound
	}
	var out struct {
		Issue *linearIssue `json:"issue"`
	}
	err := c.gql(ctx, "query($id: String!) { issue(id: $id) { "+linearIssueFields+" comments(first: 20) { nodes { id body createdAt user { "+linearUserFields+" } } } } }",
		map[string]any{"id": key}, &out)
	if err != nil {
		return nil, err
	}
	if out.Issue == nil {
		return nil, ErrNotFound
	}
	i := linearMap(out.Issue)
	if out.Issue.Comments != nil {
		for _, cm := range out.Issue.Comments.Nodes {
			i.Comments = append(i.Comments, Comment{ID: cm.ID, Author: cm.User.person(), Text: cm.Body, Created: ParseTime(cm.CreatedAt)})
		}
		sort.SliceStable(i.Comments, func(a, b int) bool { return i.Comments[a].Created.Before(i.Comments[b].Created) })
	}
	return &i, nil
}

// linearCategory maps a workflow state type to a category.
func linearCategory(t string) Category {
	switch t {
	case "backlog", "unstarted", "triage":
		return Todo
	case "started":
		return Doing
	case "completed", "canceled":
		return Done
	}
	return Unknown
}

func linearMap(r *linearIssue) Issue {
	i := Issue{
		Key:         r.Identifier,
		ID:          r.ID,
		Title:       r.Title,
		URL:         r.URL,
		Description: r.Description,
		Assignee:    r.Assignee.person(),
		Reporter:    r.Creator.person(),
		Created:     ParseTime(r.CreatedAt),
		Updated:     ParseTime(r.UpdatedAt),
		Resolved:    ParseTime(r.CompletedAt),
	}
	if i.Resolved.IsZero() {
		i.Resolved = ParseTime(r.CanceledAt)
	}
	if r.State != nil {
		i.Status = Status{Name: r.State.Name, Category: linearCategory(r.State.Type)}
	} else {
		i.Status = Status{Category: Unknown}
	}
	if r.Priority != 0 {
		i.Priority = r.PriorityLabel
	}
	if r.Team != nil {
		i.Project = r.Team.Key
	}
	if r.Parent != nil {
		i.Parent = &Ref{Key: r.Parent.Identifier, Title: r.Parent.Title, URL: r.Parent.URL}
	}
	if r.Labels != nil {
		for _, l := range r.Labels.Nodes {
			i.Labels = append(i.Labels, l.Name)
		}
	}
	return i
}
