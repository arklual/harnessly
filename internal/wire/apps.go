package wire

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// The Apps screen of «More»: the apps the machine is connected to, each as a whole section of its own. The tracker gives
// every epic and task on the person (with an epic's subtasks), GitLab the merge requests of the instance with what is
// known about each (draft, conflicts, pipeline, approvals, people, labels). Read from the services with the person's own
// tokens on this machine; an app the machine is not set up for says why instead of an empty list. The page searches,
// filters and sorts what it is given, so the rows are the app's answer whole, not a slice picked on the machine.

// AppID names an app of the screen.
type AppID string

const (
	AppTracker AppID = "tracker"
	AppGitLab  AppID = "gitlab"
)

// AppIDs is every AppID, in the schema's order (the order of the apps in the list).
var AppIDs = []AppID{AppTracker, AppGitLab}

// AppState is whether the machine could read an app: `ready` (the sections are the app's answer, any of them may be
// empty), `off` (the machine is not set up for it) or `failed` (it is set up and did not answer).
type AppState string

const (
	AppReady  AppState = "ready"
	AppOff    AppState = "off"
	AppFailed AppState = "failed"
)

// AppStates is every AppState, in the schema's order.
var AppStates = []AppState{AppReady, AppOff, AppFailed}

// AppReason says why an app is not ready. `not-configured` and `no-token` go with `off`; `denied` (the service refused
// the token) and `unreachable` with `failed`.
type AppReason string

const (
	AppNotConfigured AppReason = "not-configured"
	AppNoToken       AppReason = "no-token"
	AppDenied        AppReason = "denied"
	AppUnreachable   AppReason = "unreachable"
)

// AppReasons is every AppReason, in the schema's order.
var AppReasons = []AppReason{AppNotConfigured, AppNoToken, AppDenied, AppUnreachable}

// AppSectionID names a section of an app's feed: the epics and the tasks on the person (tracker), the merge requests
// (GitLab).
type AppSectionID string

const (
	SectionEpics AppSectionID = "epics"
	SectionTasks AppSectionID = "tasks"
	SectionMRs   AppSectionID = "mrs"
)

// AppSectionIDs is every AppSectionID, in the schema's order.
var AppSectionIDs = []AppSectionID{SectionEpics, SectionTasks, SectionMRs}

// AppFlag is what a merge request is waiting for or is stuck on, the one thing that is worth a tag.
type AppFlag string

const (
	FlagReview     AppFlag = "review"      // it waits for the person's review
	FlagConflict   AppFlag = "conflict"    // it cannot be merged: conflicts with its target
	FlagCIFailed   AppFlag = "ci-failed"   // its pipeline failed
	FlagNoReviewer AppFlag = "no-reviewer" // open, and nobody is asked to review it
	FlagOpen       AppFlag = "open"        // open and nothing else to say
)

// AppFlags is every AppFlag, in the schema's order.
var AppFlags = []AppFlag{FlagReview, FlagConflict, FlagCIFailed, FlagNoReviewer, FlagOpen}

// AppTone is how a row's status is coloured: `accent` for what is going on, `warn` for what is stuck.
type AppTone string

const (
	ToneAccent AppTone = "accent"
	ToneWarn   AppTone = "warn"
)

// AppTones is every AppTone, in the schema's order.
var AppTones = []AppTone{ToneAccent, ToneWarn}

// AppRole is how a row concerns the person: they wrote it, it is assigned to them, they are asked to review it.
type AppRole string

const (
	RoleAuthor   AppRole = "author"
	RoleAssignee AppRole = "assignee"
	RoleReviewer AppRole = "reviewer"
)

// AppRoles is every AppRole, in the schema's order.
var AppRoles = []AppRole{RoleAuthor, RoleAssignee, RoleReviewer}

// AppPipeline is the state of a merge request's latest pipeline, as GitLab names it (the ones it has beyond these are
// `pending`, which is the least wrong word for "has not started or is waiting").
type AppPipeline string

const (
	PipelineSuccess  AppPipeline = "success"
	PipelineFailed   AppPipeline = "failed"
	PipelineRunning  AppPipeline = "running"
	PipelinePending  AppPipeline = "pending"
	PipelineCanceled AppPipeline = "canceled"
	PipelineSkipped  AppPipeline = "skipped"
	PipelineManual   AppPipeline = "manual"
)

// AppPipelines is every AppPipeline, in the schema's order.
var AppPipelines = []AppPipeline{PipelineSuccess, PipelineFailed, PipelineRunning, PipelinePending, PipelineCanceled, PipelineSkipped, PipelineManual}

// AppProgress is how many of an epic's tasks are done.
type AppProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// AppRef is a task named from another: the epic a task belongs to.
type AppRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func (r AppRef) Validate() error {
	return errors.Join(need("id", r.ID), need("title", r.Title), webLink(r.URL))
}

// AppChild is a subtask of an epic: enough to list it and to tell whether it is done. `person` is its assignee.
type AppChild struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	URL    string  `json:"url"`
	State  *string `json:"state"`
	Done   bool    `json:"done"`
	Person *string `json:"person"`
}

func (c AppChild) Validate() error {
	return errors.Join(need("id", c.ID), need("title", c.Title), webLink(c.URL), needPtr("state", c.State), needPtr("person", c.Person))
}

// AppApprovals is how many of the approvals a merge request needs it has.
type AppApprovals struct {
	Given    int `json:"given"`
	Required int `json:"required"`
}

// AppMR is what GitLab says of a merge request beyond what every row has. `source` is its branch; `ready` that GitLab
// calls it mergeable now; `unresolved` that a discussion that blocks the merge is open; `comments` how many notes
// people wrote; `pipeline` the state of its latest pipeline (null: it has none, or the machine did not ask);
// `approvals` the count (null: the instance has no approvals for it, or the machine did not ask).
type AppMR struct {
	Source      *string       `json:"source"`
	Draft       bool          `json:"draft"`
	Conflicts   bool          `json:"conflicts"`
	NeedsRebase bool          `json:"needsRebase"`
	Ready       bool          `json:"ready"`
	Unresolved  bool          `json:"unresolved"`
	Comments    int           `json:"comments"`
	Pipeline    *AppPipeline  `json:"pipeline"`
	Approvals   *AppApprovals `json:"approvals"`
}

func (m AppMR) Validate() error {
	var pipeline, approvals, comments error
	if m.Pipeline != nil {
		pipeline = in("pipeline", *m.Pipeline, AppPipelines)
	}
	if m.Approvals != nil && (m.Approvals.Given < 0 || m.Approvals.Required < 0) {
		approvals = fmt.Errorf("approvals: %d of %d", m.Approvals.Given, m.Approvals.Required)
	}
	if m.Comments < 0 {
		comments = fmt.Errorf("comments: %d is below 0", m.Comments)
	}
	return errors.Join(needPtr("source", m.Source), pipeline, approvals, comments)
}

// AppRow is a task, an epic or a merge request. `key` tells it from every other row of the screen (a merge request's
// link: `!12` repeats across projects), `id` is what the person reads (`ABC-2628`, `!502`). A task has the tracker's
// own `state` name (a merge request `opened`, `merged` or `closed`), a merge request a `flag`; `project` is the
// repository, `person` the assignee or the author, `target` the branch a merge request goes to, `progress` an epic's
// tasks, `children` the epic's subtasks (empty for the others). `author` wrote it, `assignees` and `reviewers` are
// the people named on it, `me` how it concerns the person who reads. `labels` are the tracker's tags or the merge
// request's labels, `priority` and `kind` the tracker's own words, `parent` the epic a task belongs to. `createdAt`,
// `updatedAt` and `closedAt` (resolved, merged or closed; null while open) are milliseconds.
type AppRow struct {
	Key       string       `json:"key"`
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	URL       string       `json:"url"`
	State     *string      `json:"state"`
	Flag      *AppFlag     `json:"flag"`
	Tone      *AppTone     `json:"tone"`
	Project   *string      `json:"project"`
	Person    *string      `json:"person"`
	Target    *string      `json:"target"`
	CreatedAt int64        `json:"createdAt"`
	UpdatedAt int64        `json:"updatedAt"`
	ClosedAt  *int64       `json:"closedAt"`
	Author    *string      `json:"author"`
	Assignees []string     `json:"assignees"`
	Reviewers []string     `json:"reviewers"`
	Me        []AppRole    `json:"me"`
	Labels    []string     `json:"labels"`
	Priority  *string      `json:"priority"`
	Kind      *string      `json:"kind"`
	Parent    *AppRef      `json:"parent"`
	Progress  *AppProgress `json:"progress"`
	Children  []AppChild   `json:"children"`
	MR        *AppMR       `json:"mr"`
}

func webLink(v string) error {
	if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("url: %q is not a link", v)
	}
	return nil
}

func (r AppRow) Validate() error {
	var progress, flag, tone, closed, parent, mr error
	if r.Progress != nil && (r.Progress.Done < 0 || r.Progress.Total < r.Progress.Done) {
		progress = fmt.Errorf("progress: %d of %d", r.Progress.Done, r.Progress.Total)
	}
	if r.Flag != nil {
		flag = in("flag", *r.Flag, AppFlags)
	}
	if r.Tone != nil {
		tone = in("tone", *r.Tone, AppTones)
	}
	if r.ClosedAt != nil {
		closed = nonNegative("closedAt", *r.ClosedAt)
	}
	if r.Parent != nil {
		parent = under("parent", r.Parent.Validate())
	}
	if r.MR != nil {
		mr = under("mr", r.MR.Validate())
	}
	return errors.Join(need("key", r.Key), need("id", r.ID), need("title", r.Title), webLink(r.URL), needPtr("state", r.State),
		flag, tone, needPtr("project", r.Project), needPtr("person", r.Person), needPtr("target", r.Target),
		nonNegative("createdAt", r.CreatedAt), nonNegative("updatedAt", r.UpdatedAt), closed, needPtr("author", r.Author),
		allNonEmpty("assignees", r.Assignees), allNonEmpty("reviewers", r.Reviewers), allIn("me", r.Me, AppRoles),
		allNonEmpty("labels", r.Labels), needPtr("priority", r.Priority), needPtr("kind", r.Kind), parent, progress,
		each("children", r.Children), mr)
}

func allNonEmpty(name string, vs []string) error {
	for _, v := range vs {
		if v == "" {
			return fmt.Errorf("%s: empty", name)
		}
	}
	return nil
}

// AppSection is a titled run of rows; the page words the title by `id`.
type AppSection struct {
	ID   AppSectionID `json:"id"`
	Rows []AppRow     `json:"rows"`
}

func (s AppSection) Validate() error {
	return errors.Join(in("id", s.ID, AppSectionIDs), each("rows", s.Rows))
}

// AppFeed is one app's answer. `host` is the service the machine reads it from (null while none is named); `reason`
// is set exactly when the app is not ready, and then there are no sections. `me` is how the service names the person
// whose token the machine reads with (null when it did not say), so the page can tell "mine" from the others'.
type AppFeed struct {
	ID       AppID        `json:"id"`
	State    AppState     `json:"state"`
	Reason   *AppReason   `json:"reason"`
	Host     *string      `json:"host"`
	Me       *string      `json:"me"`
	Sections []AppSection `json:"sections"`
}

func (f AppFeed) Validate() error {
	var reason error
	switch {
	case f.State == AppReady && f.Reason != nil:
		reason = errors.New("reason: a ready app has none")
	case f.State != AppReady && f.Reason == nil:
		reason = errors.New("reason: none")
	case f.State != AppReady && len(f.Sections) > 0:
		reason = errors.New("sections: an app that is not ready has none")
	case f.Reason != nil:
		reason = in("reason", *f.Reason, AppReasons)
	}
	return errors.Join(in("id", f.ID, AppIDs), in("state", f.State, AppStates), reason, needPtr("host", f.Host), needPtr("me", f.Me), each("sections", f.Sections))
}

// AppsFeed is the answer of GET /api/v2/apps: every app of the screen, in the order of the list. `staleDays` is how
// long a task or merge request stands still before the page calls it stale unless the person chose otherwise.
type AppsFeed struct {
	At        int64     `json:"at"`
	StaleDays int       `json:"staleDays"`
	Apps      []AppFeed `json:"apps"`
}

func (a AppsFeed) Validate() error {
	var days error
	if a.StaleDays < 1 {
		days = fmt.Errorf("staleDays: %d is below 1", a.StaleDays)
	}
	return errors.Join(nonNegative("at", a.At), days, each("apps", a.Apps))
}

// AppsSource reads the apps. It never waits longer than its own budget: an app that does not answer in time is `failed`.
type AppsSource interface {
	Feed(ctx context.Context) (AppsFeed, error)
}

// apps serves GET /api/v2/apps. It reads and changes nothing, so it needs no guard against other origins.
func (s *Server) apps(w http.ResponseWriter, r *http.Request) {
	if s.Apps == nil {
		refuse(w, RefuseNotFound)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	feed, err := s.Apps.Feed(r.Context())
	if err != nil {
		agent.Log("warn", "v2: the apps feed failed", agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
		return
	}
	s.writeChecked(w, r, feed)
}
