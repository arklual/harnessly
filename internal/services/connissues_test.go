package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A Jira-like product only these tests know, at https://jira.test; its issues are what the test sets.
const svcFakeKind connections.Kind = "svc-fake"

type svcFake struct{}

var (
	svcFakeMu     sync.Mutex
	svcFakeIssues []connections.Issue
	svcFakeErr    error
	svcFakeCalls  int
)

func (svcFake) Kind() connections.Kind { return svcFakeKind }
func (svcFake) Detect(context.Context, *connections.Prober, *url.URL) (*connections.Detection, error) {
	return nil, nil
}
func (svcFake) Open(connections.Conn, connections.Credentials, *connections.HTTP) connections.Client {
	return svcFakeClient{}
}
func (svcFake) Link(_ connections.Conn, u *url.URL) (connections.LinkTarget, bool) {
	if k, ok := strings.CutPrefix(u.Path, "/browse/"); ok {
		return connections.LinkTarget{Kind: "issue", Key: k}, true
	}
	if id, ok := strings.CutPrefix(u.Path, "/wiki/pages/"); ok {
		return connections.LinkTarget{Kind: "page", Key: id}, true
	}
	return connections.LinkTarget{}, false
}

type svcFakeClient struct{}

func (svcFakeClient) Me(context.Context) (*connections.Person, error) {
	return &connections.Person{Login: "ann"}, nil
}
func (svcFakeClient) Projects(context.Context) ([]connections.Project, error) {
	return nil, nil
}
func (svcFakeClient) Search(_ context.Context, q connections.Query) ([]connections.Issue, error) {
	svcFakeMu.Lock()
	defer svcFakeMu.Unlock()
	svcFakeCalls++
	if svcFakeErr != nil {
		return nil, svcFakeErr
	}
	var out []connections.Issue
	for _, i := range svcFakeIssues {
		if q.Open && i.Status.Category == connections.Done {
			continue
		}
		out = append(out, i)
	}
	return out, nil
}
func (svcFakeClient) Issue(_ context.Context, key string) (*connections.Issue, error) {
	svcFakeMu.Lock()
	defer svcFakeMu.Unlock()
	svcFakeCalls++
	if svcFakeErr != nil {
		return nil, svcFakeErr
	}
	for _, i := range svcFakeIssues {
		if i.Key == key {
			return &i, nil
		}
	}
	return nil, connections.ErrNotFound
}
func (svcFakeClient) SearchPages(context.Context, connections.Query) ([]connections.Page, error) {
	return nil, nil
}
func (svcFakeClient) Page(_ context.Context, id string) (*connections.Page, error) {
	return &connections.Page{ID: id, Title: "Runbook", URL: "https://jira.test/wiki/pages/" + id, Space: "ENG", Excerpt: "How to"}, nil
}

func init() { connections.Register(svcFake{}) }

type memSecrets map[string]string

func (m memSecrets) Put(n, v string) error { m[n] = v; return nil }
func (m memSecrets) Reveal(n string) (string, error) {
	if v, ok := m[n]; ok {
		return v, nil
	}
	return "", connections.ErrNotFound
}
func (m memSecrets) Remove(n string) error { delete(m, n); return nil }

// connService is a connections service with one connection of the fake product, plus env when given.
func connService(t *testing.T, env ...connections.EnvConn) (*connections.Service, connections.Conn) {
	t.Helper()
	cs := connections.New(t.TempDir(), memSecrets{}, nil, env...)
	c, err := cs.Connect(context.Background(), connections.ConnectInput{
		Detection: connections.Detection{Kind: svcFakeKind, BaseURL: "https://jira.test", Methods: []connections.Method{{ID: "pat",
			Fields: []connections.Field{{ID: connections.FieldToken, Secret: true}}}}, Capabilities: []connections.Capability{connections.Issues, connections.Pages}},
		Method: "pat", Cred: connections.Credentials{connections.FieldToken: "t"}, Name: "Jira"})
	if err != nil {
		t.Fatal(err)
	}
	return cs, c
}

func setFakeIssues(t *testing.T, list []connections.Issue, err error) {
	t.Helper()
	svcFakeMu.Lock()
	svcFakeIssues, svcFakeErr, svcFakeCalls = list, err, 0
	svcFakeMu.Unlock()
}

var me = &connections.Person{Login: "ann", Name: "Ann Lee"}

func fakeIssues() []connections.Issue {
	now := time.Now()
	return []connections.Issue{
		{Key: "JR-1", Title: "Epic one", URL: "https://jira.test/browse/JR-1", Type: "Epic", Status: connections.Status{Name: "In Progress", Category: connections.Doing},
			Assignee: me, Reporter: &connections.Person{Login: "bob", Name: "Bob"}, Project: "JR", Labels: []string{"api"}, Priority: "High",
			Created: now.Add(-48 * time.Hour), Updated: now.Add(-time.Hour)},
		{Key: "JR-2", Title: "Child", URL: "https://jira.test/browse/JR-2", Type: "Task", Status: connections.Status{Name: "To Do", Category: connections.Todo},
			Assignee: me, Reporter: me, Parent: &connections.Ref{Key: "JR-1", Title: "Epic one", URL: "https://jira.test/browse/JR-1"}, Updated: now},
		{Key: "JR-3", Title: "Done lately", URL: "https://jira.test/browse/JR-3", Status: connections.Status{Name: "Done", Category: connections.Done},
			Assignee: me, Resolved: now.Add(-24 * time.Hour), Updated: now.Add(-24 * time.Hour), Parent: &connections.Ref{Key: "JR-1", URL: "https://jira.test/browse/JR-1"}},
		{Key: "JR-4", Title: "Done long ago", URL: "https://jira.test/browse/JR-4", Status: connections.Status{Name: "Done", Category: connections.Done},
			Assignee: me, Resolved: now.Add(-90 * 24 * time.Hour), Updated: now.Add(-90 * 24 * time.Hour)},
		{Key: "JR-5", Title: "No link", Status: connections.Status{Name: "To Do"}},
	}
}

func TestTrackerAppReadsThePrimaryConnection(t *testing.T) {
	setFakeIssues(t, fakeIssues(), nil)
	cs, _ := connService(t)
	s := New(Config{Connections: cs})
	a := appOf(t, s.Apps(context.Background()), wire.AppTracker)
	if a.State != wire.AppReady || a.Host == nil || *a.Host != "jira.test" {
		t.Fatalf("%+v", a)
	}
	epics, tasks := rowsOf(t, a, wire.SectionEpics), rowsOf(t, a, wire.SectionTasks)
	if len(epics) != 1 || len(tasks) != 2 {
		t.Fatalf("epics %+v tasks %+v", epics, tasks)
	}
	e := epics[0]
	if e.Key != "JR-1" || e.Tone == nil || *e.Tone != wire.ToneAccent || *e.State != "In Progress" || *e.Project != "JR" || *e.Priority != "High" ||
		*e.Author != "Bob" || len(e.Assignees) != 1 || e.Labels[0] != "api" || e.Progress == nil || e.Progress.Total != 2 || e.Progress.Done != 1 {
		t.Fatalf("epic %+v", e)
	}
	if len(e.Me) != 1 || e.Me[0] != wire.RoleAssignee {
		t.Fatalf("me %v", e.Me)
	}
	if tasks[0].Key != "JR-2" || tasks[0].Parent == nil || tasks[0].Parent.ID != "JR-1" || tasks[0].ClosedAt != nil {
		t.Fatalf("task %+v", tasks[0])
	}
	if tasks[1].Key != "JR-3" || tasks[1].ClosedAt == nil || tasks[1].Parent.Title != "JR-1" {
		t.Fatalf("done %+v", tasks[1])
	}
}

func TestTrackerAppSaysWhyAConnectionFails(t *testing.T) {
	setFakeIssues(t, nil, connections.ErrDenied)
	cs, _ := connService(t)
	a := appOf(t, New(Config{Connections: cs}).Apps(context.Background()), wire.AppTracker)
	if a.State != wire.AppFailed || *a.Reason != wire.AppDenied {
		t.Fatalf("%+v", a)
	}
	setFakeIssues(t, nil, connections.ErrUnreachable)
	a = appOf(t, New(Config{Connections: cs}).Apps(context.Background()), wire.AppTracker)
	if a.State != wire.AppFailed || *a.Reason != wire.AppUnreachable {
		t.Fatalf("%+v", a)
	}
}

// The deployment's tracker, as the runtime lists it, keeps the old reads: same queries, same rows.
func TestTrackerAppKeepsTheDeploymentsTracker(t *testing.T) {
	r := newAppsRig(t)
	env := connections.EnvConn{Conn: connections.Conn{ID: "env-tracker", Kind: connections.YouTrack, BaseURL: "https://tracker.example.com",
		Capabilities: []connections.Capability{connections.Issues}}}
	cs := connections.New(t.TempDir(), memSecrets{}, nil, env)
	plain := appOf(t, r.service(t, nil).Apps(context.Background()), wire.AppTracker)
	via := appOf(t, r.service(t, func(c *Config) { c.Connections = cs }).Apps(context.Background()), wire.AppTracker)
	a, _ := json.Marshal(plain)
	b, _ := json.Marshal(via)
	if string(a) != string(b) || plain.State != wire.AppReady {
		t.Fatalf("the env tracker changed its answer:\n%s\n%s", a, b)
	}
	// A connection of the person's made primary wins; the env tracker made primary again takes it back.
	setFakeIssues(t, fakeIssues(), nil)
	cs2, own := connService(t, env)
	if _, err := cs2.SetPrimary(own.ID); err != nil {
		t.Fatal(err)
	}
	if got := appOf(t, r.service(t, func(c *Config) { c.Connections = cs2 }).Apps(context.Background()), wire.AppTracker); *got.Host != "jira.test" {
		t.Fatalf("own primary: %v", *got.Host)
	}
	if _, err := cs2.SetPrimary("env-tracker"); err != nil {
		t.Fatal(err)
	}
	if got := appOf(t, r.service(t, func(c *Config) { c.Connections = cs2 }).Apps(context.Background()), wire.AppTracker); *got.Host != "tracker.example.com" {
		t.Fatalf("env primary: %v", *got.Host)
	}
}

func serve(h http.HandlerFunc, target string) (int, map[string]any) {
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	h(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestTaskCardAndLinkMetaOfAConnection(t *testing.T) {
	is := fakeIssues()
	is[1].Description = "Do it\r\nnow"
	is[1].Priority = "Low"
	is[1].Comments = []connections.Comment{{Author: &connections.Person{Name: "Bob"}, Text: "ok", Created: time.UnixMilli(1790000000000)}, {Text: "anon"}}
	setFakeIssues(t, is, nil)
	cs, c := connService(t)
	h := &Handlers{S: New(Config{Connections: cs}), Session: func(*http.Request) bool { return true }}

	code, task := serve(h.Task, "/task?key=JR-2")
	if code != 200 || task["title"] != "Child" || task["state"] != "To Do" || task["priority"] != "Low" || task["resolved"] != false ||
		task["description"] != "Do it\nnow" || task["parent"].(map[string]any)["key"] != "JR-1" || task["assignee"].(map[string]any)["name"] != "Ann Lee" {
		t.Fatalf("task: %d %v", code, task)
	}
	comments := task["comments"].([]any)
	if len(comments) != 2 || comments[0].(map[string]any)["author"] != "Bob" || comments[0].(map[string]any)["at"] != float64(1790000000000) ||
		comments[1].(map[string]any)["at"] != nil {
		t.Fatalf("comments %v", comments)
	}
	if _, ok := task["children"].([]any); !ok {
		t.Fatalf("children %v", task["children"])
	}
	// Kept: a second look does not ask again.
	setFakeIssues(t, is, nil)
	if code, _ := serve(h.Task, "/task?key=JR-2&conn="+c.ID); code != 200 {
		t.Fatal(code)
	}
	if code, _ := serve(h.Task, "/task?key=JR-2"); code != 200 || svcFakeCalls != 0 {
		t.Fatalf("%d calls %d", code, svcFakeCalls)
	}
	if code, out := serve(h.Task, "/task?key=JR-99"); code != 404 || out["error"] != TaskNotFound {
		t.Fatalf("missing: %d %v", code, out)
	}
	if code, _ := serve(h.Task, "/task?key=JR-2&conn=nope"); code != 400 {
		t.Fatalf("unknown connection: %d", code)
	}

	code, meta := serve(h.LinkMeta, "/link-meta?url="+url.QueryEscape("https://jira.test/browse/JR-1"))
	if code != 200 || meta["kind"] != "issue" || meta["key"] != "JR-1" || meta["state"] != "In Progress" || meta["resolved"] != false ||
		meta["type"] != "Epic" || meta["assignee"].(map[string]any)["name"] != "Ann Lee" || meta["parent"] != nil {
		t.Fatalf("meta: %d %v", code, meta)
	}
	if code, meta := serve(h.LinkMeta, "/link-meta?key=JR-3"); code != 200 || meta["resolved"] != true || meta["parent"].(map[string]any)["title"] != "JR-1" {
		t.Fatalf("key meta: %d %v", code, meta)
	}
	if code, meta := serve(h.LinkMeta, "/link-meta?url="+url.QueryEscape("https://jira.test/wiki/pages/42")); code != 200 || meta["kind"] != "page" ||
		meta["title"] != "Runbook" || meta["space"] != "ENG" || meta["excerpt"] != "How to" {
		t.Fatalf("page meta: %d %v", code, meta)
	}
	if code, _ := serve(h.LinkMeta, "/link-meta?url="+url.QueryEscape("https://other.test/browse/JR-1")); code != 400 {
		t.Fatalf("a link of no connection: %d", code)
	}
	if code, _ := serve(h.LinkMeta, "/link-meta?url="+url.QueryEscape("https://jira.test/browse/JR-404")); code != 404 {
		t.Fatalf("a missing issue: %d", code)
	}
}

func TestTaskCardWithoutConnectionsIsUnchanged(t *testing.T) {
	h := &Handlers{S: New(Config{}), Session: func(*http.Request) bool { return true }}
	if code, out := serve(h.Task, "/task?key=JR-2"); code != 400 || out["error"] != "Unsupported key" {
		t.Fatalf("%d %v", code, out)
	}
}
