package services

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The review picker: the projects of the deployment's GitLab this token sees, then a project's merge requests,
// branches or a branch's commits — the lists a person walks to open a change without a chat. Each item carries the
// web address the change view (ChangeView) opens. The answer is always a state, like an app's feed: a GitLab that is
// not configured, has no token or refuses says so instead of an empty list.

// Pick lists: what `list=` asks for.
const (
	PickProjects = "projects"
	PickMRs      = "mrs"
	PickBranches = "branches"
	PickCommits  = "commits"
)

// Merge request filters: open ones, the ones waiting for my review, my own.
const (
	PickOpen   = "open"
	PickReview = "review"
	PickMine   = "mine"
)

const (
	pickPerPage   = 50
	pickCommits   = 30
	pickQueryMax  = 200
	pickRefMax    = 255
	pickPathMax   = 512
	pickTitleMax  = 300
	pickProjectsQ = 2 // GitLab ignores a project search shorter than this: the newest are listed instead
)

var (
	digitsRe    = regexp.MustCompile(`^!?(\d{1,9})$`)
	projectPath = regexp.MustCompile(`^[\w.-]+(?:/[\w.-]+)+$`)
)

// PickRequest is one question of the picker.
type PickRequest struct {
	List    string
	Project string // group/sub/project
	Filter  string // mrs: open, review, mine
	Query   string
	Ref     string // commits: the branch
}

// validProject: group/sub/project, no part of it a dot or two.
func validProject(path string) bool {
	if len(path) > pickPathMax || !projectPath.MatchString(path) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if strings.Trim(part, ".") == "" {
			return false
		}
	}
	return true
}

// ValidPick says why a request cannot be asked; "" when it can.
func ValidPick(p PickRequest) string {
	switch p.List {
	case PickProjects:
	case PickMRs, PickBranches, PickCommits:
		// My merge requests and the ones waiting for my review are listed across every project: no project is named.
		everywhere := p.List == PickMRs && p.Project == "" && (p.Filter == PickReview || p.Filter == PickMine)
		if !everywhere && !validProject(p.Project) {
			return "project"
		}
		if p.List == PickMRs && p.Filter != "" && p.Filter != PickOpen && p.Filter != PickReview && p.Filter != PickMine {
			return "filter"
		}
		if p.List == PickCommits && (p.Ref == "" || len(p.Ref) > pickRefMax) {
			return "ref"
		}
	default:
		return "list"
	}
	if len(p.Query) > pickQueryMax {
		return "q"
	}
	return ""
}

type pickProject struct {
	ID           int       `json:"id"`
	Path         string    `json:"path_with_namespace"`
	Name         string    `json:"name"`
	WebURL       string    `json:"web_url"`
	LastActivity time.Time `json:"last_activity_at"`
}

type pickBranch struct {
	Name   string `json:"name"`
	Commit struct {
		ID     string    `json:"id"`
		Title  string    `json:"title"`
		At     time.Time `json:"committed_date"`
		WebURL string    `json:"web_url"`
	} `json:"commit"`
}

type pickCommit struct {
	ID     string    `json:"id"`
	Short  string    `json:"short_id"`
	Title  string    `json:"title"`
	Author string    `json:"author_name"`
	At     time.Time `json:"committed_date"`
	WebURL string    `json:"web_url"`
}

func pickOff(reason wire.AppReason, host *string) *jsvalue.Object {
	state := wire.AppOff
	if reason == wire.AppDenied || reason == wire.AppUnreachable {
		state = wire.AppFailed
	}
	return obj("state", string(state), "reason", string(reason), "host", hostOrNull(host), "items", []any{})
}

func hostOrNull(host *string) any {
	if host == nil {
		return nil
	}
	return *host
}

func pickClip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

func iso(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// GitLabPick answers one list of the picker.
func (s *Service) GitLabPick(ctx context.Context, p PickRequest) *jsvalue.Object {
	if len(s.cfg.GitLabHosts) == 0 {
		return pickOff(wire.AppNotConfigured, nil)
	}
	host := s.cfg.GitLabHosts[0]
	token := s.gitlabToken(host)
	if token == "" {
		return pickOff(wire.AppNoToken, &host)
	}
	a := &access{host: host, token: token}
	var items []any
	var err error
	switch p.List {
	case PickProjects:
		items, err = s.pickProjects(ctx, a, p.Query)
	case PickMRs:
		items, err = s.pickMRs(ctx, a, p)
	case PickBranches:
		items, err = s.pickBranches(ctx, a, p)
	case PickCommits:
		items, err = s.pickCommits(ctx, a, p)
	}
	if err != nil {
		return pickOff(failureOf(err), &host)
	}
	if items == nil {
		items = []any{}
	}
	return obj("state", string(wire.AppReady), "host", host, "items", items)
}

func (s *Service) pickProjects(ctx context.Context, a *access, q string) ([]any, error) {
	query := url.Values{
		"membership": {"true"},
		"simple":     {"true"},
		"archived":   {"false"},
		"order_by":   {"last_activity_at"},
		"sort":       {"desc"},
		"per_page":   {itoa(pickPerPage)},
	}
	if q = strings.TrimSpace(q); len([]rune(q)) >= pickProjectsQ {
		query.Set("search", q)
		query.Set("search_namespaces", "true")
	}
	var list []pickProject
	if err := s.gitlabGet(ctx, a, "/projects", query, &list); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(list))
	for _, p := range list {
		if p.Path == "" {
			continue
		}
		out = append(out, obj("id", p.ID, "path", p.Path, "name", pickClip(p.Name, pickTitleMax), "url", p.WebURL, "activity", iso(p.LastActivity)))
	}
	return out, nil
}

// mrQuery is GitLab's query for a project's merge requests: a number finds that one in any state, words search the
// titles of open ones; the filter narrows to my reviews or my own.
func mrQuery(filter, q, me string) url.Values {
	query := url.Values{
		"order_by": {"updated_at"},
		"sort":     {"desc"},
		"per_page": {itoa(pickPerPage)},
		"state":    {"opened"},
	}
	q = strings.TrimSpace(q)
	if m := digitsRe.FindStringSubmatch(q); m != nil {
		query.Set("iids[]", m[1])
		query.Set("state", "all")
	} else if q != "" {
		query.Set("search", q)
		query.Set("in", "title")
	}
	switch filter {
	case PickReview:
		query.Set("reviewer_username", me)
	case PickMine:
		query.Set("author_username", me)
	}
	return query
}

func (s *Service) pickMRs(ctx context.Context, a *access, p PickRequest) ([]any, error) {
	me := ""
	if p.Filter == PickReview || p.Filter == PickMine {
		var who gitlabPerson
		if err := s.gitlabGet(ctx, a, "/user", nil, &who); err != nil {
			return nil, err
		}
		if who.Username == "" {
			return nil, &appFailure{wire.AppUnreachable}
		}
		me = who.Username
	}
	var list []gitlabMR
	path := "/projects/" + files.EncodeURIComponent(p.Project) + "/merge_requests"
	query := mrQuery(p.Filter, p.Query, me)
	if p.Project == "" {
		// Across every project the token sees: GitLab's own list of merge requests, any author or reviewer.
		path = "/merge_requests"
		query.Set("scope", "all")
	}
	if err := s.gitlabGet(ctx, a, path, query, &list); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(list))
	for _, mr := range list {
		var author any
		if mr.Author != nil {
			author = derefOr(mr.Author.name())
		}
		out = append(out, obj(
			"iid", mr.IID, "title", pickClip(mr.Title, pickTitleMax), "url", mr.WebURL, "state", mr.State, "draft", mr.Draft,
			"author", author, "source", mr.SourceBranch, "target", mr.TargetBranch, "updated", iso(mr.UpdatedAt),
			"project", mrProject(mr.References.Full),
		))
	}
	return out, nil
}

// mrProject is the project of a merge request's full reference (group/project!12): "" when there is none.
func mrProject(full string) string {
	at := strings.LastIndexByte(full, '!')
	if at <= 0 {
		return ""
	}
	return full[:at]
}

func derefOr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func (s *Service) pickBranches(ctx context.Context, a *access, p PickRequest) ([]any, error) {
	query := url.Values{"per_page": {itoa(pickPerPage)}}
	if q := strings.TrimSpace(p.Query); q != "" {
		query.Set("search", q)
	}
	var list []pickBranch
	path := "/projects/" + files.EncodeURIComponent(p.Project) + "/repository/branches"
	if err := s.gitlabGet(ctx, a, path, query, &list); err != nil {
		return nil, err
	}
	// GitLab lists branches by name: the ones worked on last come first here.
	sort.SliceStable(list, func(i, j int) bool { return list[i].Commit.At.After(list[j].Commit.At) })
	out := make([]any, 0, len(list))
	for _, b := range list {
		out = append(out, obj("name", b.Name, "sha", b.Commit.ID, "title", pickClip(b.Commit.Title, pickTitleMax),
			"at", iso(b.Commit.At), "url", b.Commit.WebURL))
	}
	return out, nil
}

func (s *Service) pickCommits(ctx context.Context, a *access, p PickRequest) ([]any, error) {
	query := url.Values{"ref_name": {p.Ref}, "per_page": {itoa(pickCommits)}}
	var list []pickCommit
	path := "/projects/" + files.EncodeURIComponent(p.Project) + "/repository/commits"
	if err := s.gitlabGet(ctx, a, path, query, &list); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(list))
	for _, c := range list {
		short := c.Short
		if short == "" && len(c.ID) >= 8 {
			short = c.ID[:8]
		}
		out = append(out, obj("sha", c.ID, "short", short, "title", pickClip(c.Title, pickTitleMax), "author", c.Author,
			"at", iso(c.At), "url", c.WebURL))
	}
	return out, nil
}

// pickRequestOf reads the query of the picker's route.
func pickRequestOf(get func(string) (string, bool)) PickRequest {
	v := func(k string) string { s, _ := get(k); return s }
	return PickRequest{List: v("list"), Project: v("project"), Filter: v("filter"), Query: v("q"), Ref: v("ref")}
}
