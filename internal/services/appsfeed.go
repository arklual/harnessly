package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The Apps screen's feed: every epic and task the tracker holds on the person (an epic with its subtasks), and the
// merge requests of the person's GitLab with what is known about each. Read with the person's own tokens (the same files
// the task card and the merge request pane read); an app the machine has no address or token for says so, one that does
// not answer or refuses the token says that. Nothing here is picked or invented: the page searches, filters and sorts what
// the service holds, and an empty section is an empty answer. Not limited to the keys the task card serves
// (RUNTIME_TASK_KEY_RE): the tracker's other spaces are the person's tasks too.

const (
	// appsStaleDays is how long a task or a merge request stands still before the page calls it stale, unless the
	// person chose another number.
	appsStaleDays = 5
	// appsDoneDays is how far back the finished (resolved, merged, closed) rows are listed.
	appsDoneDays = 30
	// The tracker is read in pages; a person's open work does not pass appsOpenPages of them.
	appsPage      = 200
	appsOpenPages = 5
	// GitLab's open merge requests come in pages of 100; the finished ones are the newest of them.
	appsMRPage    = 100
	appsMROpen    = 2
	appsMRMerged  = 40
	appsMRClosed  = 20
	appsEnrich    = 40
	appsEnrichPar = 8
	// appsChildren caps the subtasks listed under an epic.
	appsChildren = 60
	appsLabels   = 12
	appsTimeout  = 10 * time.Second
	// appsBudget is all a service gets to answer an app in: its pages together.
	appsBudget   = 18 * time.Second
	appsMaxBytes = 6 << 20
	// appsFresh is how long an app's answer is reused: two tabs, or a tab asking again, share one read.
	appsFresh = 30 * time.Second
	appsText  = 300
)

type appsEntry struct {
	feed wire.AppFeed
	at   time.Time
}

type appsState struct {
	mu    sync.Mutex
	locks map[wire.AppID]*sync.Mutex
	last  map[wire.AppID]appsEntry
}

func newAppsState() *appsState {
	return &appsState{
		locks: map[wire.AppID]*sync.Mutex{wire.AppTracker: {}, wire.AppGitLab: {}},
		last:  map[wire.AppID]appsEntry{},
	}
}

// appFailure is why an app could not be read.
type appFailure struct{ reason wire.AppReason }

func (e *appFailure) Error() string { return string(e.reason) }

func ptr[T any](v T) *T { return &v }

// Apps reads every app of the screen, in parallel. An answer is kept for appsFresh; one that is not ready is not kept, so
// a token put in place is taken on the next look.
func (s *Service) Apps(ctx context.Context) wire.AppsFeed {
	out := wire.AppsFeed{At: time.Now().UnixMilli(), StaleDays: appsStaleDays, Apps: make([]wire.AppFeed, len(wire.AppIDs))}
	var wg sync.WaitGroup
	for i, id := range wire.AppIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.Apps[i] = s.app(ctx, id)
		}()
	}
	wg.Wait()
	return out
}

func (s *Service) app(ctx context.Context, id wire.AppID) wire.AppFeed {
	st := s.apps
	lock := st.locks[id]
	lock.Lock()
	defer lock.Unlock()
	st.mu.Lock()
	if e, ok := st.last[id]; ok && time.Since(e.at) < appsFresh {
		st.mu.Unlock()
		return e.feed
	}
	st.mu.Unlock()
	var feed wire.AppFeed
	read, cancel := context.WithTimeout(context.WithoutCancel(ctx), appsBudget)
	defer cancel()
	switch id {
	case wire.AppTracker:
		feed = s.trackerFeed(read)
	default:
		feed = s.gitlabFeed(read)
	}
	st.mu.Lock()
	if feed.State == wire.AppReady {
		st.last[id] = appsEntry{feed: feed, at: time.Now()}
	} else {
		delete(st.last, id)
	}
	st.mu.Unlock()
	return feed
}

func appOff(id wire.AppID, reason wire.AppReason, host *string) wire.AppFeed {
	state := wire.AppOff
	if reason == wire.AppDenied || reason == wire.AppUnreachable {
		state = wire.AppFailed
	}
	return wire.AppFeed{ID: id, State: state, Reason: &reason, Host: host, Sections: []wire.AppSection{}}
}

func hostOf(origin string) *string {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return nil
	}
	return &u.Host
}

// readJSON reads one JSON answer of a service with a token; the failure says whether the token was refused.
func (s *Service) readJSON(ctx context.Context, target string, header http.Header, out any) error {
	resp, err := s.fetch(ctx, target, header, refuse, appsTimeout, appsMaxBytes)
	if resp == nil || err != nil || resp.tooBig {
		return &appFailure{wire.AppUnreachable}
	}
	switch {
	case resp.status == http.StatusUnauthorized || resp.status == http.StatusForbidden:
		return &appFailure{wire.AppDenied}
	case !resp.ok():
		return &appFailure{wire.AppUnreachable}
	}
	if json.Unmarshal([]byte(decodeText(resp.body)), out) != nil {
		return &appFailure{wire.AppUnreachable}
	}
	return nil
}

func failureOf(err error) wire.AppReason {
	if f, ok := err.(*appFailure); ok {
		return f.reason
	}
	return wire.AppUnreachable
}

// parallel runs the reads together and gives the first failure in the order they were listed.
func parallel(reads ...func() error) error {
	errs := make([]error, len(reads))
	var wg sync.WaitGroup
	for i, read := range reads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = read()
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// appText is a title or a name of a service bound for the page: controls out, one line, clipped; nil when nothing is left.
func appText(v string) *string {
	t := strings.TrimSpace(spaceRunRe.ReplaceAllString(cleanTaskText(v, appsText), " "))
	if t == "" {
		return nil
	}
	return &t
}

func millis(t time.Time) int64 { return max(0, t.UnixMilli()) }

// ── tracker ──

const trackerAppFields = "idReadable,summary,created,updated,resolved,reporter(login,fullName)," +
	"tags(name),customFields(name,value(name,fullName,login)),parent(issues(idReadable,summary))," +
	"subtasks(issues(idReadable,summary,resolved,customFields(name,value(name,fullName))))"

// trackerKeyRe is the shape of an issue key in any space of the tracker; the card's RUNTIME_TASK_KEY_RE is narrower.
var trackerKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

type trackerFieldValue struct {
	Name     string `json:"name"`
	FullName string `json:"fullName"`
	Login    string `json:"login"`
}

// UnmarshalJSON takes a value that is a user or a bundle element; a field of another shape (a text, a number, a list of
// elements) is no value of the fields read here, and must not fail the whole read.
func (v *trackerFieldValue) UnmarshalJSON(b []byte) error {
	type plain trackerFieldValue
	var p plain
	if len(b) > 0 && b[0] == '{' {
		if err := json.Unmarshal(b, &p); err != nil {
			return nil
		}
	}
	*v = trackerFieldValue(p)
	return nil
}

type trackerIssueRef struct {
	ID       string `json:"idReadable"`
	Summary  string `json:"summary"`
	Resolved *int64 `json:"resolved"`
	Fields   []struct {
		Name  string             `json:"name"`
		Value *trackerFieldValue `json:"value"`
	} `json:"customFields"`
}

type trackerIssue struct {
	ID       string `json:"idReadable"`
	Summary  string `json:"summary"`
	Created  int64  `json:"created"`
	Updated  int64  `json:"updated"`
	Resolved *int64 `json:"resolved"`
	Reporter *struct {
		Login    string `json:"login"`
		FullName string `json:"fullName"`
	} `json:"reporter"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Fields []struct {
		Name  string             `json:"name"`
		Value *trackerFieldValue `json:"value"`
	} `json:"customFields"`
	Parent *struct {
		Issues []trackerIssueRef `json:"issues"`
	} `json:"parent"`
	Subtasks *struct {
		Issues []trackerIssueRef `json:"issues"`
	} `json:"subtasks"`
}

func (i trackerIssue) value(name string) *trackerFieldValue {
	for _, f := range i.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

func (i trackerIssueRef) value(name string) *trackerFieldValue {
	for _, f := range i.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

func nameOf(v *trackerFieldValue) *string {
	if v == nil {
		return nil
	}
	return appText(v.Name)
}

// personOf is a user value of the tracker as the person reads it: the full name, else the name, else the login.
func personOf(v *trackerFieldValue) *string {
	if v == nil {
		return nil
	}
	return or2(appText(v.FullName), or2(appText(v.Name), appText(v.Login)))
}

// trackerMe is the person the token belongs to.
type trackerMe struct {
	Login    string `json:"login"`
	FullName string `json:"fullName"`
}

func (s *Service) trackerGet(ctx context.Context, token, path string, query url.Values, out any) error {
	target := s.trackerOrigin() + path + "?" + query.Encode()
	return s.readJSON(ctx, target, http.Header{"Accept": {"application/json"}, "Authorization": {"Bearer " + token}}, out)
}

// trackerIssues reads a query page after page until a page comes short, at most pages of them.
func (s *Service) trackerIssues(ctx context.Context, token, query string, pages int) ([]trackerIssue, error) {
	var all []trackerIssue
	for page := 0; page < pages; page++ {
		var batch []trackerIssue
		err := s.trackerGet(ctx, token, "/api/issues", url.Values{"query": {query}, "fields": {trackerAppFields}, "$top": {itoa(appsPage)}, "$skip": {itoa(page * appsPage)}}, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < appsPage {
			break
		}
	}
	return all, nil
}

func (s *Service) trackerChildren(i trackerIssue) []wire.AppChild {
	kids := []wire.AppChild{}
	if i.Subtasks == nil {
		return kids
	}
	for _, c := range i.Subtasks.Issues {
		title := appText(c.Summary)
		if title == nil || !trackerKeyRe.MatchString(c.ID) || len(kids) >= appsChildren {
			continue
		}
		kids = append(kids, wire.AppChild{ID: c.ID, Title: *title, URL: s.trackerOrigin() + "/issue/" + c.ID, State: nameOf(c.value("State")),
			Done: c.Resolved != nil, Person: personOf(c.value("Assignee"))})
	}
	return kids
}

func (s *Service) trackerRow(i trackerIssue, me trackerMe) (row wire.AppRow, epic, ok bool) {
	title := appText(i.Summary)
	if title == nil || !trackerKeyRe.MatchString(i.ID) {
		return wire.AppRow{}, false, false
	}
	kind := nameOf(i.value("Type"))
	epic = kind != nil && *kind == "Epic"
	state := nameOf(i.value("State"))
	assignee := i.value("Assignee")
	person := personOf(assignee)
	row = wire.AppRow{Key: i.ID, ID: i.ID, Title: *title, URL: s.trackerOrigin() + "/issue/" + i.ID, State: state, Person: person,
		CreatedAt: millis(time.UnixMilli(i.Created)), UpdatedAt: millis(time.UnixMilli(i.Updated)), Assignees: []string{}, Reviewers: []string{},
		Me: []wire.AppRole{}, Labels: []string{}, Priority: nameOf(i.value("Priority")), Kind: kind, Children: []wire.AppChild{}}
	if i.Resolved != nil {
		row.ClosedAt = ptr(millis(time.UnixMilli(*i.Resolved)))
	}
	if i.Reporter != nil {
		row.Author = or2(appText(i.Reporter.FullName), appText(i.Reporter.Login))
	}
	if person != nil {
		row.Assignees = append(row.Assignees, *person)
	}
	if assignee != nil && me.Login != "" && assignee.Login == me.Login {
		row.Me = append(row.Me, wire.RoleAssignee)
	}
	if i.Reporter != nil && me.Login != "" && i.Reporter.Login == me.Login {
		row.Me = append(row.Me, wire.RoleAuthor)
	}
	for _, tag := range i.Tags {
		if name := appText(tag.Name); name != nil && len(row.Labels) < appsLabels {
			row.Labels = append(row.Labels, *name)
		}
	}
	if i.Parent != nil && len(i.Parent.Issues) > 0 {
		if p := i.Parent.Issues[0]; trackerKeyRe.MatchString(p.ID) {
			if t := appText(p.Summary); t != nil {
				row.Parent = &wire.AppRef{ID: p.ID, Title: *t, URL: s.trackerOrigin() + "/issue/" + p.ID}
			}
		}
	}
	if state != nil && (*state == "In Progress" || *state == "Testing") && i.Resolved == nil {
		row.Tone = ptr(wire.ToneAccent)
	}
	if epic {
		row.Children = s.trackerChildren(i)
		p := wire.AppProgress{Total: len(row.Children)}
		for _, c := range row.Children {
			if c.Done {
				p.Done++
			}
		}
		row.Progress = &p
	}
	return row, epic, true
}

func (s *Service) trackerFeed(ctx context.Context) wire.AppFeed {
	if s.cfg.TrackerURL == "" {
		return appOff(wire.AppTracker, wire.AppNotConfigured, nil)
	}
	host := hostOf(s.trackerOrigin())
	token := s.trackerToken()
	if token == "" {
		return appOff(wire.AppTracker, wire.AppNoToken, host)
	}
	var me trackerMe
	var open, done []trackerIssue
	err := parallel(
		func() error {
			return s.trackerGet(ctx, token, "/api/users/me", url.Values{"fields": {"login,fullName"}}, &me)
		},
		func() (err error) {
			open, err = s.trackerIssues(ctx, token, "for: me #Unresolved sort by: updated desc", appsOpenPages)
			return
		},
		func() (err error) {
			done, err = s.trackerIssues(ctx, token, "for: me resolved date: {minus "+itoa(appsDoneDays)+"d} .. Today sort by: updated desc", 1)
			return
		},
	)
	if err != nil {
		return appOff(wire.AppTracker, failureOf(err), host)
	}
	epics, tasks := []wire.AppRow{}, []wire.AppRow{}
	seen := map[string]bool{}
	for _, i := range append(open, done...) {
		if seen[i.ID] {
			continue
		}
		seen[i.ID] = true
		row, epic, ok := s.trackerRow(i, me)
		switch {
		case !ok:
		case epic:
			epics = append(epics, row)
		default:
			tasks = append(tasks, row)
		}
	}
	return wire.AppFeed{ID: wire.AppTracker, State: wire.AppReady, Host: host, Me: or2(appText(me.FullName), appText(me.Login)), Sections: []wire.AppSection{
		{ID: wire.SectionEpics, Rows: epics},
		{ID: wire.SectionTasks, Rows: tasks},
	}}
}

// ── GitLab ──

type gitlabPerson struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

func (p gitlabPerson) name() *string { return or2(appText(p.Name), appText(p.Username)) }

type gitlabMR struct {
	IID                         int            `json:"iid"`
	ProjectID                   int            `json:"project_id"`
	Title                       string         `json:"title"`
	State                       string         `json:"state"`
	WebURL                      string         `json:"web_url"`
	CreatedAt                   time.Time      `json:"created_at"`
	UpdatedAt                   time.Time      `json:"updated_at"`
	MergedAt                    *time.Time     `json:"merged_at"`
	ClosedAt                    *time.Time     `json:"closed_at"`
	SourceBranch                string         `json:"source_branch"`
	TargetBranch                string         `json:"target_branch"`
	Draft                       bool           `json:"draft"`
	HasConflicts                bool           `json:"has_conflicts"`
	DetailedStatus              string         `json:"detailed_merge_status"`
	BlockingDiscussionsResolved *bool          `json:"blocking_discussions_resolved"`
	Notes                       int            `json:"user_notes_count"`
	Labels                      []string       `json:"labels"`
	Author                      *gitlabPerson  `json:"author"`
	Assignees                   []gitlabPerson `json:"assignees"`
	Reviewers                   []gitlabPerson `json:"reviewers"`
	References                  struct {
		Full string `json:"full"`
	} `json:"references"`
	Pipeline *struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
	approvals *wire.AppApprovals
}

func (s *Service) gitlabGet(ctx context.Context, a *access, path string, query url.Values, out any) error {
	target := s.gitlabBase(a.host) + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return s.readJSON(ctx, target, http.Header{"Private-Token": {a.token}, "Accept": {"application/json"}}, out)
}

// gitlabMRs reads the merge requests of one state, newest first, page after page until a page comes short.
func (s *Service) gitlabMRs(ctx context.Context, a *access, query url.Values, perPage, pages int) ([]gitlabMR, error) {
	query.Set("scope", "all")
	query.Set("order_by", "updated_at")
	query.Set("sort", "desc")
	query.Set("per_page", itoa(perPage))
	var all []gitlabMR
	for page := 1; page <= pages; page++ {
		query.Set("page", itoa(page))
		var batch []gitlabMR
		if err := s.gitlabGet(ctx, a, "/merge_requests", query, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			break
		}
	}
	return all, nil
}

// enrich asks for what the list does not carry — the latest pipeline and the approvals — of the newest open merge
// requests, a few at a time. A merge request whose answer does not come is shown without it: the instance may not have
// approvals at all, and a stumble of one read is not a reason to hide the list.
func (s *Service) enrich(ctx context.Context, a *access, list []gitlabMR) {
	var wg sync.WaitGroup
	gate := make(chan struct{}, appsEnrichPar)
	for i := range list {
		if i >= appsEnrich || list[i].State != "opened" {
			continue
		}
		mr := &list[i]
		wg.Add(1)
		gate <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-gate }()
			base := "/projects/" + itoa(mr.ProjectID) + "/merge_requests/" + itoa(mr.IID)
			var detail struct {
				Pipeline *struct {
					Status string `json:"status"`
				} `json:"head_pipeline"`
			}
			if s.gitlabGet(ctx, a, base, nil, &detail) == nil {
				mr.Pipeline = detail.Pipeline
			}
			var ap struct {
				Required int        `json:"approvals_required"`
				By       []struct{} `json:"approved_by"`
			}
			if s.gitlabGet(ctx, a, base+"/approvals", nil, &ap) == nil && (ap.Required > 0 || len(ap.By) > 0) {
				mr.approvals = &wire.AppApprovals{Given: len(ap.By), Required: ap.Required}
			}
		}()
	}
	wg.Wait()
}

// projectOf is the repository's own name: the last part of `group/sub/project!12`.
func projectOf(full string) *string {
	path, _, _ := strings.Cut(full, "!")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return appText(path)
}

func appPipelineOf(status string) wire.AppPipeline {
	switch status {
	case "success":
		return wire.PipelineSuccess
	case "failed":
		return wire.PipelineFailed
	case "running":
		return wire.PipelineRunning
	case "canceled", "canceling":
		return wire.PipelineCanceled
	case "skipped":
		return wire.PipelineSkipped
	case "manual":
		return wire.PipelineManual
	default:
		return wire.PipelinePending
	}
}

func people(list []gitlabPerson) []string {
	out := []string{}
	for _, p := range list {
		if n := p.name(); n != nil {
			out = append(out, *n)
		}
	}
	return out
}

func (s *Service) mrRow(mr gitlabMR, me string) (wire.AppRow, bool) {
	title := appText(mr.Title)
	if title == nil || mr.IID < 1 || mr.WebURL == "" {
		return wire.AppRow{}, false
	}
	state := mr.State
	if state != "merged" && state != "closed" {
		state = "opened"
	}
	m := wire.AppMR{Source: appText(mr.SourceBranch), Draft: mr.Draft, Conflicts: mr.HasConflicts, NeedsRebase: mr.DetailedStatus == "need_rebase",
		Ready: mr.DetailedStatus == "mergeable", Unresolved: mr.BlockingDiscussionsResolved != nil && !*mr.BlockingDiscussionsResolved,
		Comments: max(0, mr.Notes), Approvals: mr.approvals}
	if mr.Pipeline != nil && mr.Pipeline.Status != "" {
		m.Pipeline = ptr(appPipelineOf(mr.Pipeline.Status))
	}
	row := wire.AppRow{Key: mr.WebURL, ID: "!" + itoa(mr.IID), Title: *title, URL: mr.WebURL, State: ptr(state), Project: projectOf(mr.References.Full),
		Target: appText(mr.TargetBranch), CreatedAt: millis(mr.CreatedAt), UpdatedAt: millis(mr.UpdatedAt), Assignees: people(mr.Assignees),
		Reviewers: people(mr.Reviewers), Me: []wire.AppRole{}, Labels: []string{}, Children: []wire.AppChild{}, MR: &m}
	if state == "merged" && mr.MergedAt != nil {
		row.ClosedAt = ptr(millis(*mr.MergedAt))
	} else if state != "opened" && mr.ClosedAt != nil {
		row.ClosedAt = ptr(millis(*mr.ClosedAt))
	} else if state != "opened" {
		row.ClosedAt = ptr(millis(mr.UpdatedAt))
	}
	mine := false
	if mr.Author != nil {
		row.Author = mr.Author.name()
		row.Person = row.Author
		if mine = mr.Author.Username == me; mine {
			row.Me = append(row.Me, wire.RoleAuthor)
		}
	}
	for _, p := range mr.Assignees {
		if p.Username == me {
			row.Me = append(row.Me, wire.RoleAssignee)
			break
		}
	}
	asked := false
	for _, p := range mr.Reviewers {
		if p.Username == me {
			asked = true
			row.Me = append(row.Me, wire.RoleReviewer)
			break
		}
	}
	for _, l := range mr.Labels {
		if name := appText(l); name != nil && len(row.Labels) < appsLabels {
			row.Labels = append(row.Labels, *name)
		}
	}
	if state != "opened" {
		return row, true
	}
	var flag wire.AppFlag
	switch {
	case asked && !mine:
		flag = wire.FlagReview
	case mr.HasConflicts:
		flag = wire.FlagConflict
	case m.Pipeline != nil && *m.Pipeline == wire.PipelineFailed:
		flag = wire.FlagCIFailed
	case len(mr.Reviewers) == 0 && !mr.Draft:
		flag = wire.FlagNoReviewer
	default:
		flag = wire.FlagOpen
	}
	row.Flag = &flag
	switch flag {
	case wire.FlagReview:
		row.Tone = ptr(wire.ToneAccent)
	case wire.FlagConflict, wire.FlagCIFailed:
		row.Tone = ptr(wire.ToneWarn)
	}
	return row, true
}

func or2(a, b *string) *string {
	if a != nil {
		return a
	}
	return b
}

func (s *Service) gitlabFeed(ctx context.Context) wire.AppFeed {
	if len(s.cfg.GitLabHosts) == 0 {
		return appOff(wire.AppGitLab, wire.AppNotConfigured, nil)
	}
	host := s.cfg.GitLabHosts[0]
	token := s.gitlabToken(host)
	if token == "" {
		return appOff(wire.AppGitLab, wire.AppNoToken, &host)
	}
	a := &access{host: host, token: token}
	var me gitlabPerson
	if err := s.gitlabGet(ctx, a, "/user", nil, &me); err != nil {
		return appOff(wire.AppGitLab, failureOf(err), &host)
	}
	if me.Username == "" {
		return appOff(wire.AppGitLab, wire.AppUnreachable, &host)
	}
	since := time.Now().UTC().Add(-appsDoneDays * 24 * time.Hour).Format(time.RFC3339)
	var open, merged, closed []gitlabMR
	err := parallel(
		func() (err error) {
			open, err = s.gitlabMRs(ctx, a, url.Values{"state": {"opened"}}, appsMRPage, appsMROpen)
			return
		},
		func() (err error) {
			merged, err = s.gitlabMRs(ctx, a, url.Values{"state": {"merged"}, "updated_after": {since}}, appsMRMerged, 1)
			return
		},
		func() (err error) {
			closed, err = s.gitlabMRs(ctx, a, url.Values{"state": {"closed"}, "updated_after": {since}}, appsMRClosed, 1)
			return
		},
	)
	if err != nil {
		return appOff(wire.AppGitLab, failureOf(err), &host)
	}
	s.enrich(ctx, a, open)
	rows := []wire.AppRow{}
	for _, list := range [][]gitlabMR{open, merged, closed} {
		for _, mr := range list {
			if row, ok := s.mrRow(mr, me.Username); ok {
				rows = append(rows, row)
			}
		}
	}
	return wire.AppFeed{ID: wire.AppGitLab, State: wire.AppReady, Host: &host, Me: me.name(), Sections: []wire.AppSection{{ID: wire.SectionMRs, Rows: rows}}}
}
