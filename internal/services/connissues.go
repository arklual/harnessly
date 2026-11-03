package services

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The tracker app, the task card and link previews of a connection (internal/connections) that is not the deployment's
// tracker: Jira, Linear, a YouTrack of the person's own, GitHub or GitLab issues. The deployment's tracker
// (RUNTIME_TRACKER_URL) keeps its own reads above, answer for answer; a connection answers in the same shapes, so the
// page does not tell them apart.

const (
	// connRecent is how many of the person's latest issues are read for the finished ones of the last appsDoneDays.
	connRecent = 100
	// connKeyMax bounds a key or a page id a route passes on to a connection.
	connKeyMax = 300
)

// isEnvTracker says whether a connection is the deployment's tracker, which the old reads serve.
func (s *Service) isEnvTracker(c connections.Conn) bool {
	return c.Env && c.Kind == connections.YouTrack && s.cfg.TrackerURL != "" &&
		strings.EqualFold(strings.TrimRight(c.BaseURL, "/"), s.trackerOrigin())
}

// personalIssues is the connection bare keys and the tracker app read: the primary one with issues, unless that is the
// deployment's tracker. ok is false when the old reads answer.
func (s *Service) personalIssues() (connections.Conn, bool) {
	if s.cfg.Connections == nil {
		return connections.Conn{}, false
	}
	c, ok := s.cfg.Connections.Primary(connections.Issues)
	if !ok || s.isEnvTracker(c) {
		return connections.Conn{}, false
	}
	return c, true
}

// issueConn is a connection by id that has issues, else the personal one for an empty id.
func (s *Service) issueConn(id string) (connections.Conn, bool) {
	if s.cfg.Connections == nil {
		return connections.Conn{}, false
	}
	if id == "" {
		return s.personalIssues()
	}
	c, err := s.cfg.Connections.Get(id)
	if err != nil || !c.Has(connections.Issues) {
		return connections.Conn{}, false
	}
	return c, true
}

// validConnKey is a key or a page id worth passing on: not empty, one line, not huge.
func validConnKey(k string) bool {
	return k != "" && len(k) <= connKeyMax && !strings.ContainsAny(k, "\r\n\x00") && strings.TrimSpace(k) == k
}

// isWebLink is an http(s) address with a host, as the feed's contract wants one.
func isWebLink(v string) bool {
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func connDone(i connections.Issue) bool {
	return i.Status.Category == connections.Done || !i.Resolved.IsZero()
}

func personName(p *connections.Person) *string {
	if p == nil {
		return nil
	}
	return or2(appText(p.Name), or2(appText(p.Login), appText(p.Email)))
}

// samePerson: two people of one product are one when an id, a login or an email matches.
func samePerson(a, b *connections.Person) bool {
	if a == nil || b == nil {
		return false
	}
	eq := func(x, y string) bool { return x != "" && strings.EqualFold(x, y) }
	return eq(a.ID, b.ID) || eq(a.Login, b.Login) || eq(a.Email, b.Email)
}

func connFailure(err error) wire.AppReason {
	if errors.Is(err, connections.ErrDenied) {
		return wire.AppDenied
	}
	return wire.AppUnreachable
}

// connRow is an issue of a connection as a row of the tracker app.
func connRow(i connections.Issue, me *connections.Person) (row wire.AppRow, epic, ok bool) {
	title := appText(i.Title)
	if title == nil || i.Key == "" || !isWebLink(i.URL) {
		return wire.AppRow{}, false, false
	}
	kind := appText(i.Type)
	epic = kind != nil && strings.EqualFold(*kind, "epic")
	done := connDone(i)
	row = wire.AppRow{Key: i.Key, ID: i.Key, Title: *title, URL: i.URL, State: appText(i.Status.Name), Project: appText(i.Project),
		Person: personName(i.Assignee), Author: personName(i.Reporter), CreatedAt: millis(i.Created), UpdatedAt: millis(i.Updated),
		Assignees: []string{}, Reviewers: []string{}, Me: []wire.AppRole{}, Labels: []string{}, Priority: appText(i.Priority), Kind: kind,
		Children: []wire.AppChild{}}
	if done {
		at := i.Resolved
		if at.IsZero() {
			at = i.Updated
		}
		row.ClosedAt = ptr(millis(at))
	}
	if row.Person != nil {
		row.Assignees = append(row.Assignees, *row.Person)
	}
	if samePerson(i.Assignee, me) {
		row.Me = append(row.Me, wire.RoleAssignee)
	}
	if samePerson(i.Reporter, me) {
		row.Me = append(row.Me, wire.RoleAuthor)
	}
	for _, l := range i.Labels {
		if name := appText(l); name != nil && len(row.Labels) < appsLabels {
			row.Labels = append(row.Labels, *name)
		}
	}
	if p := i.Parent; p != nil && p.Key != "" && isWebLink(p.URL) {
		t := appText(p.Title)
		if t == nil {
			t = &p.Key
		}
		row.Parent = &wire.AppRef{ID: p.Key, Title: *t, URL: p.URL}
	}
	if i.Status.Category == connections.Doing && !done {
		row.Tone = ptr(wire.ToneAccent)
	}
	return row, epic, true
}

// connFeed is the tracker app read from a connection: the person's open issues, and those finished in the last
// appsDoneDays. An epic's children are the listed issues under it.
func (s *Service) connFeed(ctx context.Context, c connections.Conn) wire.AppFeed {
	host := hostOf(c.BaseURL)
	src, err := s.cfg.Connections.Issues(c)
	if err != nil {
		return appOff(wire.AppTracker, connFailure(err), host)
	}
	var open, recent []connections.Issue
	err = parallel(
		func() (err error) {
			open, err = src.Search(ctx, connections.Query{Mine: true, Open: true, Limit: appsPage})
			return
		},
		func() (err error) {
			recent, err = src.Search(ctx, connections.Query{Mine: true, Limit: connRecent})
			return
		},
	)
	if err != nil {
		return appOff(wire.AppTracker, connFailure(err), host)
	}
	since := time.Now().Add(-appsDoneDays * 24 * time.Hour)
	epics, tasks := []wire.AppRow{}, []wire.AppRow{}
	seen := map[string]bool{}
	add := func(i connections.Issue) {
		if seen[i.Key] {
			return
		}
		row, epic, ok := connRow(i, c.Me)
		if !ok {
			return
		}
		seen[i.Key] = true
		if epic {
			epics = append(epics, row)
		} else {
			tasks = append(tasks, row)
		}
	}
	for _, i := range open {
		add(i)
	}
	for _, i := range recent {
		at := i.Resolved
		if at.IsZero() {
			at = i.Updated
		}
		if connDone(i) && at.After(since) {
			add(i)
		}
	}
	for e := range epics {
		for _, t := range append(append([]wire.AppRow{}, tasks...), epics...) {
			if t.Parent == nil || t.Parent.ID != epics[e].Key || len(epics[e].Children) >= appsChildren {
				continue
			}
			epics[e].Children = append(epics[e].Children, wire.AppChild{ID: t.ID, Title: t.Title, URL: t.URL, State: t.State,
				Done: t.ClosedAt != nil, Person: t.Person})
		}
		p := wire.AppProgress{Total: len(epics[e].Children)}
		for _, ch := range epics[e].Children {
			if ch.Done {
				p.Done++
			}
		}
		epics[e].Progress = &p
	}
	return wire.AppFeed{ID: wire.AppTracker, State: wire.AppReady, Host: host, Me: personName(c.Me), Sections: []wire.AppSection{
		{ID: wire.SectionEpics, Rows: epics},
		{ID: wire.SectionTasks, Rows: tasks},
	}}
}

// ── task card ──

// servesTracker says whether conn names the deployment's tracker for the card: none named, or its env connection.
func (s *Service) servesTracker(conn string) bool {
	if conn == "" {
		return true
	}
	c, ok := s.issueConn(conn)
	return ok && s.isEnvTracker(c)
}

// ConnTaskKey says whether the card serves key from a connection: conn names it ("" is the personal one), and the
// key is not one the deployment's tracker serves.
func (s *Service) ConnTaskKey(conn, key string) bool {
	if !validConnKey(key) {
		return false
	}
	c, ok := s.issueConn(conn)
	if !ok {
		return false
	}
	return !s.isEnvTracker(c)
}

func millisOrNull(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return float64(t.UnixMilli())
}

// connTask is an issue of a connection in the card's shape (web/app/lib/hl-task.ts ITask).
func connTask(i *connections.Issue) *jsvalue.Object {
	title := shortText(i.Title)
	if title == nil {
		title = i.Key
	}
	var assignee any
	if name := personName(i.Assignee); name != nil {
		assignee = obj("name", *name, "avatar", nil)
	}
	var parent any
	if p := i.Parent; p != nil && p.Key != "" {
		parent = obj("key", p.Key, "title", or(shortText(p.Title), p.Key))
	}
	all := i.Comments
	if len(all) > taskComments {
		all = all[len(all)-taskComments:]
	}
	comments := make([]any, 0, len(all))
	for _, c := range all {
		var author any
		if n := personName(c.Author); n != nil {
			author = *n
		}
		comments = append(comments, obj("author", or(author, locale.Pick("Unnamed", "Без имени")), "at", millisOrNull(c.Created),
			"text", cleanTaskText(c.Text, taskCommentMax)))
	}
	return obj("key", i.Key, "title", title, "project", shortText(i.Project), "type", shortText(i.Type), "state", shortText(i.Status.Name),
		"priority", shortText(i.Priority), "resolved", connDone(*i), "assignee", assignee, "parent", parent,
		"children", []any{}, "description", cleanTaskText(i.Description, taskDescMax), "comments", comments,
		"updated", millisOrNull(i.Updated))
}

func (s *Service) loadConnTask(ctx context.Context, c connections.Conn, key string) taskResult {
	src, err := s.cfg.Connections.Issues(c)
	if err != nil {
		if errors.Is(err, connections.ErrDenied) {
			return taskResult{err: TaskNoToken}
		}
		return taskResult{err: TaskTracker}
	}
	ctx, cancel := context.WithTimeout(ctx, appsBudget)
	defer cancel()
	i, err := src.Issue(ctx, key)
	switch {
	case errors.Is(err, connections.ErrNotFound):
		return taskResult{err: TaskNotFound}
	case err != nil || i == nil:
		return taskResult{err: TaskTracker}
	}
	return taskResult{task: connTask(i)}
}

// ConnTask is one issue of a connection for the card, kept and shared like Task. Callers check ConnTaskKey first.
func (s *Service) ConnTask(ctx context.Context, conn, key string) (*jsvalue.Object, string) {
	c, ok := s.issueConn(conn)
	if !ok {
		return nil, TaskNotFound
	}
	return s.cachedTask(ctx, "conn:"+c.ID+":"+key, func(ctx context.Context) taskResult { return s.loadConnTask(ctx, c, key) })
}

// ── link previews ──

// connLinkTarget is a link a connection claims: an issue or a page of one of the person's instances.
func (s *Service) connLinkTarget(raw string) *linkTarget {
	if s.cfg.Connections == nil {
		return nil
	}
	c, t, ok := s.cfg.Connections.Resolve(raw)
	if !ok || !validConnKey(t.Key) {
		return nil
	}
	switch t.Kind {
	case "issue":
		return &linkTarget{kind: "conn-issue", conn: c.ID, key: t.Key}
	case "page":
		return &linkTarget{kind: "conn-page", conn: c.ID, key: t.Key}
	}
	return nil
}

// connKeyTarget is a bare key of the personal connection.
func (s *Service) connKeyTarget(raw string) *linkTarget {
	key := strings.TrimSpace(raw)
	if !s.ConnTaskKey("", key) {
		return nil
	}
	c, _ := s.personalIssues()
	return &linkTarget{kind: "conn-issue", conn: c.ID, key: key}
}

// connMeta reads a connection's link for a preview: an issue in the shape of the tracker's ({kind: "issue", key, title,
// state, resolved, assignee, type, parent}), a page as {kind: "page", title, space, excerpt}. Missing or refused is
// nil, so the page shows the plain link.
func (s *Service) connMeta(ctx context.Context, t *linkTarget) (any, error) {
	cs := s.cfg.Connections
	c, err := cs.Get(t.conn)
	if err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, appsBudget)
	defer cancel()
	quiet := func(err error) error {
		if errors.Is(err, connections.ErrNotFound) || errors.Is(err, connections.ErrDenied) || errors.Is(err, connections.ErrUnsupported) {
			return nil
		}
		return err
	}
	if t.kind == "conn-page" {
		src, err := cs.Pages(c)
		if err != nil {
			return nil, quiet(err)
		}
		p, err := src.Page(ctx, t.key)
		if err != nil || p == nil {
			return nil, quiet(err)
		}
		excerpt := p.Excerpt
		if excerpt == "" {
			excerpt = p.Body
		}
		return obj("kind", "page", "id", p.ID, "title", or(shortText(p.Title), p.ID), "space", shortText(p.Space),
			"excerpt", shortText(excerpt)), nil
	}
	src, err := cs.Issues(c)
	if err != nil {
		return nil, quiet(err)
	}
	i, err := src.Issue(ctx, t.key)
	if err != nil || i == nil {
		return nil, quiet(err)
	}
	var person any
	if name := personName(i.Assignee); name != nil {
		person = obj("name", *name, "avatar", nil)
	}
	var parent any
	if p := i.Parent; p != nil && p.Key != "" {
		parent = obj("key", p.Key, "title", or(shortText(p.Title), p.Key))
	}
	return obj("kind", "issue", "key", i.Key, "title", or(shortText(i.Title), i.Key), "state", shortText(i.Status.Name),
		"resolved", connDone(*i), "assignee", person, "type", shortText(i.Type), "parent", parent), nil
}
