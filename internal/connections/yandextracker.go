package connections

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Yandex Tracker (Яндекс Трекер) is a SaaS only: tracker.yandex.ru / tracker.yandex.com in the browser,
// https://api.tracker.yandex.net/v2 for the API. The organization id goes in X-Org-ID (Yandex 360 for Business) or
// X-Cloud-Org-ID (Yandex Cloud Organization); the two are the methods "oauth-360" and "oauth-cloud".

func init() { Register(&yandexTrackerProvider{}) }

type yandexTrackerProvider struct{}

func (*yandexTrackerProvider) Kind() Kind { return YandexTracker }

const yandexTrackerAPI = "https://api.tracker.yandex.net"

var yandexKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

func yandexTrackerHost(host string) bool {
	return HostIs(host, "tracker.yandex.ru") || HostIs(host, "tracker.yandex.com")
}

func (*yandexTrackerProvider) Detect(_ context.Context, _ *Prober, u *url.URL) (*Detection, error) {
	if !yandexTrackerHost(u.Hostname()) {
		return nil, nil
	}
	fields := []Field{
		{ID: FieldToken, Secret: true, Placeholder: "y0_…"},
		{ID: FieldOrg, Placeholder: "organization id"},
	}
	return &Detection{
		Kind:       YandexTracker,
		BaseURL:    "https://" + strings.ToLower(u.Hostname()),
		APIURL:     yandexTrackerAPI,
		Deployment: Cloud,
		Title:      "Яндекс Трекер",
		Methods: []Method{
			{ID: "oauth-360", Recommended: true, TokenURL: "https://oauth.yandex.ru/", Fields: fields},
			{ID: "oauth-cloud", TokenURL: "https://oauth.yandex.ru/", Fields: fields},
		},
		Capabilities: []Capability{Issues},
	}, nil
}

func (*yandexTrackerProvider) Open(conn Conn, cred Credentials, h *HTTP) Client {
	return &yandexTrackerClient{conn: conn, token: cred[FieldToken], org: strings.TrimSpace(cred[FieldOrg]), h: h}
}

// Link: https://tracker.yandex.ru/ABC-12 (also /issue/ABC-12).
func (*yandexTrackerProvider) Link(conn Conn, u *url.URL) (LinkTarget, bool) {
	if !yandexTrackerHost(u.Hostname()) {
		if b, err := url.Parse(conn.BaseURL); err != nil || !strings.EqualFold(b.Host, u.Host) {
			return LinkTarget{}, false
		}
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) == 2 && segs[0] == "issue" {
		segs = segs[1:]
	}
	if len(segs) == 1 && yandexKeyRe.MatchString(segs[0]) {
		return LinkTarget{Kind: "issue", Key: strings.ToUpper(segs[0])}, true
	}
	return LinkTarget{}, false
}

type yandexTrackerClient struct {
	conn  Conn
	token string
	org   string
	h     *HTTP
}

func (c *yandexTrackerClient) header() map[string]string {
	h := map[string]string{"Authorization": "OAuth " + c.token}
	if c.conn.Method == "oauth-cloud" {
		h["X-Cloud-Org-ID"] = c.org
	} else {
		h["X-Org-ID"] = c.org
	}
	return h
}

func (c *yandexTrackerClient) req(method, path string, body any) Request {
	hd := c.header()
	return Request{Method: method, URL: Join(orDefault(c.conn.APIURL, yandexTrackerAPI), "/v2/"+strings.TrimLeft(path, "/")),
		Header: Header("Authorization", hd["Authorization"], "X-Org-ID", hd["X-Org-ID"], "X-Cloud-Org-ID", hd["X-Cloud-Org-ID"]), Body: body}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

type yandexRef struct {
	Key     string `json:"key"`
	Display string `json:"display"`
}

type yandexUser struct {
	ID        string `json:"id"`
	UID       any    `json:"uid"`
	Login     string `json:"login"`
	Display   string `json:"display"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

func (u *yandexUser) person() *Person {
	if u == nil {
		return nil
	}
	name := u.Display
	if name == "" {
		name = strings.TrimSpace(u.FirstName + " " + u.LastName)
	}
	if name == "" {
		name = u.Login
	}
	if name == "" {
		return nil
	}
	id := u.ID
	if id == "" && u.UID != nil {
		id = fmt.Sprint(u.UID)
	}
	return &Person{ID: id, Login: u.Login, Name: name, Email: u.Email}
}

func (c *yandexTrackerClient) Me(ctx context.Context) (*Person, error) {
	var u yandexUser
	if _, err := c.h.JSON(ctx, c.req("GET", "myself", nil), &u); err != nil {
		return nil, err
	}
	p := u.person()
	if p == nil {
		return nil, fmt.Errorf("%w: no user in the answer", ErrUnreachable)
	}
	return p, nil
}

func (c *yandexTrackerClient) Projects(ctx context.Context) ([]Project, error) {
	var qs []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if _, err := c.h.JSON(ctx, c.req("GET", "queues?"+Q("perPage", "100"), nil), &qs); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(qs))
	for _, q := range qs {
		out = append(out, Project{Key: q.Key, Name: q.Name, URL: Join(c.conn.BaseURL, q.Key)})
	}
	return out, nil
}

type yandexIssue struct {
	Key         string      `json:"key"`
	ID          string      `json:"id"`
	Summary     string      `json:"summary"`
	Description string      `json:"description"`
	Status      yandexRef   `json:"status"`
	StatusType  *yandexRef  `json:"statusType"`
	Type        *yandexRef  `json:"type"`
	Priority    *yandexRef  `json:"priority"`
	Queue       *yandexRef  `json:"queue"`
	Assignee    *yandexUser `json:"assignee"`
	CreatedBy   *yandexUser `json:"createdBy"`
	Parent      *yandexRef  `json:"parent"`
	Tags        []string    `json:"tags"`
	CreatedAt   string      `json:"createdAt"`
	UpdatedAt   string      `json:"updatedAt"`
	ResolvedAt  string      `json:"resolvedAt"`
}

func yandexCategory(i yandexIssue) Category {
	if i.StatusType != nil {
		switch strings.ToLower(i.StatusType.Key) {
		case "new":
			return Todo
		case "inprogress", "paused":
			return Doing
		case "done", "cancelled":
			return Done
		}
	}
	k := strings.ToLower(i.Status.Key)
	switch {
	case i.ResolvedAt != "" || k == "closed" || k == "resolved" || k == "done" || k == "cancelled":
		return Done
	case k == "open" || k == "new" || k == "backlog" || k == "needinfo":
		return Todo
	case strings.Contains(k, "progress") || strings.Contains(k, "review") || strings.Contains(k, "test"):
		return Doing
	}
	return Unknown
}

func (c *yandexTrackerClient) issue(i yandexIssue) Issue {
	it := Issue{Key: i.Key, ID: i.ID, Title: i.Summary, URL: Join(c.conn.BaseURL, i.Key),
		Status: Status{Name: i.Status.Display, Category: yandexCategory(i)}, Assignee: i.Assignee.person(), Reporter: i.CreatedBy.person(),
		Labels: i.Tags, Created: ParseTime(i.CreatedAt), Updated: ParseTime(i.UpdatedAt), Resolved: ParseTime(i.ResolvedAt), Description: i.Description}
	if i.Status.Display == "" {
		it.Status.Name = i.Status.Key
	}
	if i.Type != nil {
		it.Type = i.Type.Display
	}
	if i.Priority != nil {
		it.Priority = i.Priority.Display
	}
	if i.Queue != nil {
		it.Project = i.Queue.Key
	}
	if i.Parent != nil {
		it.Parent = &Ref{Key: i.Parent.Key, Title: i.Parent.Display, URL: Join(c.conn.BaseURL, i.Parent.Key)}
	}
	return it
}

func yandexQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// yandexQuery builds Tracker's query language from the common filters.
func yandexQuery(q Query) string {
	if q.Native != "" {
		return q.Native
	}
	var parts []string
	if q.Project != "" {
		parts = append(parts, "Queue: "+yandexQuote(q.Project))
	}
	if q.Mine {
		parts = append(parts, "Assignee: me()")
	}
	if q.Open {
		parts = append(parts, "Resolution: empty()")
	}
	if q.Text != "" {
		parts = append(parts, yandexQuote(q.Text))
	}
	s := strings.Join(parts, " AND ")
	return strings.TrimSpace(s + ` "Sort by": Updated DESC`)
}

func (c *yandexTrackerClient) Search(ctx context.Context, q Query) ([]Issue, error) {
	var raw []yandexIssue
	body := map[string]string{"query": yandexQuery(q)}
	if _, err := c.h.JSON(ctx, c.req("POST", "issues/_search?"+Q("perPage", fmt.Sprint(q.LimitOr(30))), body), &raw); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(raw))
	for _, i := range raw {
		out = append(out, c.issue(i))
	}
	return out, nil
}

func (c *yandexTrackerClient) Issue(ctx context.Context, key string) (*Issue, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	if !yandexKeyRe.MatchString(key) {
		return nil, fmt.Errorf("%w: bad issue key %q", ErrNotFound, key)
	}
	var raw yandexIssue
	if _, err := c.h.JSON(ctx, c.req("GET", "issues/"+key, nil), &raw); err != nil {
		return nil, err
	}
	it := c.issue(raw)
	var comments []struct {
		ID        any         `json:"id"`
		Text      string      `json:"text"`
		CreatedBy *yandexUser `json:"createdBy"`
		CreatedAt string      `json:"createdAt"`
	}
	if res, err := c.h.Do(ctx, c.req("GET", "issues/"+key+"/comments?"+Q("perPage", "50"), nil)); err == nil && res.Status == 200 && res.Decode(&comments) == nil {
		if len(comments) > 10 {
			comments = comments[len(comments)-10:]
		}
		for _, cm := range comments {
			it.Comments = append(it.Comments, Comment{ID: fmt.Sprint(cm.ID), Author: cm.CreatedBy.person(), Text: cm.Text, Created: ParseTime(cm.CreatedAt)})
		}
	}
	return &it, nil
}
