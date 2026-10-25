package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// appsRig is a tracker and a GitLab on one test server; each answers what the test sets.
type appsRig struct {
	srv                       *httptest.Server
	hits                      atomic.Int32
	tracker, gitlab, userCode int
	mu                        sync.Mutex // the feed asks the tracker in parallel
	queries                   []string
	approvalsCode             int
}

const trackerOpen = `[
	{"idReadable":"ABC-2628","summary":"  An epic \u0007with\n   noise ","created":1790000000000,"updated":1792800000000,"resolved":null,
	 "reporter":{"login":"bob","fullName":"Bob Ray"},"tags":[{"name":"repo:api"}],
	 "customFields":[{"name":"Type","value":{"name":"Epic"}},{"name":"State","value":{"name":"In Progress"}},{"name":"Priority","value":{"name":"Major"}},{"name":"Assignee","value":{"name":"Ann Lee","fullName":"Ann Lee","login":"ann"}}],
	 "parent":{"issues":[]},
	 "subtasks":{"issues":[
		{"idReadable":"ABC-2630","summary":"Done one","resolved":1,"customFields":[{"name":"State","value":{"name":"Done"}},{"name":"Assignee","value":{"name":"Ann Lee","fullName":"Ann Lee"}}]},
		{"idReadable":"ABC-2631","summary":"Open one","resolved":null,"customFields":[{"name":"State","value":{"name":"New"}},{"name":"Assignee","value":null}]},
		{"idReadable":"ABC-2632","summary":"Done two","resolved":2,"customFields":[]}]}},
	{"idReadable":"OPS-214","summary":"A task of another space","created":1791000000000,"updated":1792700000000,"resolved":null,
	 "reporter":{"login":"ann","fullName":"Ann Lee"},"tags":[],
	 "customFields":[{"name":"Subsystem","value":[{"name":"a"},{"name":"b"}]},{"name":"Note","value":"free text"},{"name":"State","value":{"name":"Новая"}},{"name":"Assignee","value":{"name":"Ann Lee","fullName":"Ann Lee","login":"ann"}}],"parent":{"issues":[]}},
	{"idReadable":"ABC-3039","summary":"Task of an epic","created":1791000000000,"updated":1791900000000,"resolved":null,
	 "reporter":{"login":"bob","fullName":"Bob Ray"},"tags":[{"name":"waiting:decision"},{"name":"repo:api"}],
	 "customFields":[{"name":"Type","value":{"name":"Bug"}},{"name":"State","value":{"name":"Testing"}},{"name":"Assignee","value":{"name":"Ann Lee","fullName":"Ann Lee","login":"ann"}}],
	 "parent":{"issues":[{"idReadable":"ABC-2628","summary":"An epic"}]}},
	{"idReadable":"not a key","summary":"Rubbish","created":1,"updated":1,"customFields":[]}
]`

const trackerDone = `[
	{"idReadable":"ABC-3039","summary":"Task of an epic","created":1791000000000,"updated":1791900000000,"resolved":null,"customFields":[]},
	{"idReadable":"ABC-2559","summary":"Finished","created":1791000000000,"updated":1792000000000,"resolved":1792000000000,
	 "customFields":[{"name":"Type","value":{"name":"Engineering"}},{"name":"State","value":{"name":"Done"}},{"name":"Assignee","value":{"name":"Ann Lee","fullName":"Ann Lee","login":"ann"}}]}
]`

func newAppsRig(t *testing.T) *appsRig {
	t.Helper()
	r := &appsRig{tracker: 200, gitlab: 200, userCode: 200, approvalsCode: 200}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch req.Host {
		case "tracker.example.com":
			if req.Header.Get("Authorization") != "Bearer tracker-secret" {
				w.WriteHeader(401)
				return
			}
			if r.tracker != 200 {
				w.WriteHeader(r.tracker)
				return
			}
			if req.URL.Path == "/api/users/me" {
				_, _ = w.Write([]byte(`{"login":"ann","fullName":"Ann Lee"}`))
				return
			}
			q := req.URL.Query().Get("query")
			r.mu.Lock()
			r.queries = append(r.queries, q)
			r.mu.Unlock()
			if strings.Contains(q, "#Unresolved") {
				_, _ = w.Write([]byte(trackerOpen))
				return
			}
			_, _ = w.Write([]byte(trackerDone))
		case "gitlab.example.com":
			if req.Header.Get("Private-Token") != "gitlab-secret" {
				w.WriteHeader(401)
				return
			}
			if req.URL.Path == "/api/v4/user" {
				if r.userCode != 200 {
					w.WriteHeader(r.userCode)
					return
				}
				_, _ = w.Write([]byte(`{"username":"me","name":"Me Myself"}`))
				return
			}
			if r.gitlab != 200 {
				w.WriteHeader(r.gitlab)
				return
			}
			switch {
			case strings.HasSuffix(req.URL.Path, "/approvals"):
				if r.approvalsCode != 200 {
					w.WriteHeader(r.approvalsCode)
					return
				}
				if strings.Contains(req.URL.Path, "/merge_requests/502/") {
					_, _ = w.Write([]byte(`{"approvals_required":2,"approved_by":[{}]}`))
				} else {
					_, _ = w.Write([]byte(`{"approvals_required":0,"approved_by":[]}`))
				}
			case strings.Contains(req.URL.Path, "/projects/"):
				switch {
				case strings.HasSuffix(req.URL.Path, "/merge_requests/502"):
					_, _ = w.Write([]byte(`{"head_pipeline":{"status":"running"}}`))
				case strings.HasSuffix(req.URL.Path, "/merge_requests/471"):
					_, _ = w.Write([]byte(`{"head_pipeline":{"status":"failed"}}`))
				default:
					_, _ = w.Write([]byte(`{"head_pipeline":null}`))
				}
			case req.URL.Query().Get("state") == "merged":
				_, _ = w.Write([]byte(`[{"iid":400,"project_id":3,"title":"Merged","state":"merged","web_url":"https://gitlab.example.com/team/web/-/merge_requests/400","created_at":"2026-10-20T00:00:00Z","updated_at":"2026-10-22T00:00:00Z","merged_at":"2026-10-22T00:00:00Z","author":{"username":"me","name":"Me Myself"},"labels":[],"references":{"full":"team/web!400"}}]`))
			case req.URL.Query().Get("state") == "closed":
				_, _ = w.Write([]byte(`[{"iid":300,"project_id":3,"title":"Closed","state":"closed","web_url":"https://gitlab.example.com/team/web/-/merge_requests/300","created_at":"2026-10-10T00:00:00Z","updated_at":"2026-10-12T00:00:00Z","closed_at":null,"author":null,"labels":[],"references":{"full":"team/web!300"}}]`))
			default:
				_, _ = w.Write([]byte(`[
					{"iid":502,"project_id":1,"title":"Review me","state":"opened","web_url":"https://gitlab.example.com/team/sub/api/-/merge_requests/502","created_at":"2026-10-20T00:00:00Z","updated_at":"2026-10-24T01:00:00.000+03:00","source_branch":"feature/x","target_branch":"main","detailed_merge_status":"mergeable","blocking_discussions_resolved":false,"user_notes_count":4,"labels":["backend","urgent"],"author":{"username":"bob","name":"Bob"},"assignees":[{"username":"me","name":"Me Myself"}],"reviewers":[{"username":"me","name":"Me Myself"}],"references":{"full":"team/sub/api!502"}},
					{"iid":7,"project_id":2,"title":"My own","state":"opened","web_url":"https://gitlab.example.com/team/web/-/merge_requests/7","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-23T00:00:00Z","draft":true,"detailed_merge_status":"draft_status","labels":[],"author":{"username":"me","name":"Me Myself"},"reviewers":[{"username":"me","name":"Me Myself"}],"references":{"full":"team/web!7"}},
					{"iid":478,"project_id":4,"title":"Conflicting","state":"opened","web_url":"https://gitlab.example.com/team/webapp/-/merge_requests/478","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z","target_branch":"main","has_conflicts":true,"detailed_merge_status":"need_rebase","labels":[],"author":{"username":"ann","name":""},"reviewers":[],"references":{"full":"team/webapp!478"}},
					{"iid":471,"project_id":5,"title":"Red pipeline","state":"opened","web_url":"https://gitlab.example.com/team/payments/-/merge_requests/471","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z","labels":[],"author":{"username":"ann","name":"Ann"},"reviewers":[{"username":"zed","name":"Zed"}],"references":{"full":"team/payments!471"}},
					{"iid":466,"project_id":6,"title":"Nobody looks","state":"opened","web_url":"https://gitlab.example.com/team/storefront/-/merge_requests/466","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z","labels":[],"author":{"username":"ann","name":"Ann"},"reviewers":[],"references":{"full":"team/storefront!466"}},
					{"iid":465,"project_id":6,"title":"Waits for a verdict","state":"opened","web_url":"https://gitlab.example.com/team/storefront/-/merge_requests/465","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z","labels":[],"author":{"username":"ann","name":"Ann"},"reviewers":[{"username":"zed","name":"Zed"}],"references":{"full":"team/storefront!465"}}
				]`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *appsRig) service(t *testing.T, mutate func(*Config)) *Service {
	t.Helper()
	t.Setenv("YOUTRACK_TOKEN", "tracker-secret")
	t.Setenv("GITLAB_SELF_TOKEN", "gitlab-secret")
	cfg := Config{Transport: tlsTo(r.srv), TrackerURL: "https://tracker.example.com", GitLabHosts: []string{"gitlab.example.com"},
		TaskKey: regexp.MustCompile(`^(ABC|EXP)-[0-9]+$`)}
	if mutate != nil {
		mutate(&cfg)
	}
	return New(cfg)
}

func appOf(t *testing.T, feed wire.AppsFeed, id wire.AppID) wire.AppFeed {
	t.Helper()
	if err := feed.Validate(); err != nil {
		t.Fatalf("the feed is out of contract: %v", err)
	}
	for _, a := range feed.Apps {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no %s in the feed", id)
	return wire.AppFeed{}
}

func rowsOf(t *testing.T, a wire.AppFeed, id wire.AppSectionID) []wire.AppRow {
	t.Helper()
	for _, s := range a.Sections {
		if s.ID == id {
			return s.Rows
		}
	}
	t.Fatalf("no section %s", id)
	return nil
}

func TestAppsReadTheTrackerAndGitLab(t *testing.T) {
	r := newAppsRig(t)
	s := r.service(t, nil)
	feed := s.Apps(context.Background())
	if feed.StaleDays != appsStaleDays || len(feed.Apps) != 2 {
		t.Fatalf("%+v", feed)
	}

	tracker := appOf(t, feed, wire.AppTracker)
	if tracker.State != wire.AppReady || tracker.Host == nil || *tracker.Host != "tracker.example.com" || tracker.Me == nil || *tracker.Me != "Ann Lee" {
		t.Fatalf("%+v", tracker)
	}
	epics := rowsOf(t, tracker, wire.SectionEpics)
	if len(epics) != 1 {
		t.Fatalf("one epic: %+v", epics)
	}
	epic := epics[0]
	if epic.ID != "ABC-2628" || epic.Title != "An epic with noise" || epic.URL != "https://tracker.example.com/issue/ABC-2628" {
		t.Errorf("%+v", epic)
	}
	if epic.State == nil || *epic.State != "In Progress" || *epic.Person != "Ann Lee" || *epic.Priority != "Major" || *epic.Kind != "Epic" || *epic.Author != "Bob Ray" {
		t.Errorf("%+v", epic)
	}
	if epic.Progress == nil || epic.Progress.Done != 2 || epic.Progress.Total != 3 || len(epic.Children) != 3 || epic.Tone == nil || *epic.Tone != wire.ToneAccent {
		t.Errorf("progress and subtasks: %+v", epic)
	}
	if kid := epic.Children[1]; kid.ID != "ABC-2631" || kid.Done || kid.Person != nil || *kid.State != "New" || kid.URL != "https://tracker.example.com/issue/ABC-2631" {
		t.Errorf("a subtask: %+v", kid)
	}
	if len(epic.Labels) != 1 || epic.Labels[0] != "repo:api" || len(epic.Me) != 1 || epic.Me[0] != wire.RoleAssignee || epic.ClosedAt != nil {
		t.Errorf("%+v", epic)
	}

	tasks := rowsOf(t, tracker, wire.SectionTasks)
	byID := map[string]wire.AppRow{}
	for _, row := range tasks {
		byID[row.ID] = row
	}
	if len(tasks) != 3 || byID["OPS-214"].ID == "" {
		t.Fatalf("a task of another space is the person's too, a key out of shape is not, a task read twice is one: %+v", tasks)
	}
	if ops := byID["OPS-214"]; *ops.State != "Новая" || ops.Kind != nil || len(ops.Me) != 2 {
		t.Errorf("the person wrote and holds it: %+v", ops)
	}
	task := byID["ABC-3039"]
	if task.Parent == nil || task.Parent.ID != "ABC-2628" || task.Parent.URL != "https://tracker.example.com/issue/ABC-2628" || len(task.Labels) != 2 || *task.Kind != "Bug" || task.Progress != nil {
		t.Errorf("%+v", task)
	}
	if done := byID["ABC-2559"]; done.ClosedAt == nil || *done.ClosedAt != 1792000000000 || done.Tone != nil {
		t.Errorf("a finished task: %+v", done)
	}
	r.mu.Lock()
	queries := strings.Join(r.queries, "\n")
	r.mu.Unlock()
	if !strings.Contains(queries, "for: me #Unresolved") || !strings.Contains(queries, "resolved date: {minus 30d}") {
		t.Errorf("queries: %q", queries)
	}

	gitlab := appOf(t, feed, wire.AppGitLab)
	if gitlab.State != wire.AppReady || *gitlab.Host != "gitlab.example.com" || *gitlab.Me != "Me Myself" {
		t.Fatalf("%+v", gitlab)
	}
	mrs := map[string]wire.AppRow{}
	for _, row := range rowsOf(t, gitlab, wire.SectionMRs) {
		mrs[row.ID] = row
	}
	if len(mrs) != 8 {
		t.Fatalf("six open, one merged, one closed: %d", len(mrs))
	}
	review := mrs["!502"]
	if *review.Flag != wire.FlagReview || *review.Project != "api" || *review.Person != "Bob" || *review.Target != "main" || *review.State != "opened" {
		t.Errorf("%+v", review)
	}
	if review.UpdatedAt != time.Date(2026, 10, 23, 22, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("updatedAt: %d", review.UpdatedAt)
	}
	m := review.MR
	if m == nil || *m.Source != "feature/x" || !m.Ready || !m.Unresolved || m.Comments != 4 || m.Draft || m.Conflicts || *m.Pipeline != wire.PipelineRunning ||
		m.Approvals == nil || m.Approvals.Given != 1 || m.Approvals.Required != 2 {
		t.Errorf("%+v", m)
	}
	if len(review.Labels) != 2 || len(review.Reviewers) != 1 || review.Reviewers[0] != "Me Myself" || len(review.Me) != 2 {
		t.Errorf("people and labels: %+v", review)
	}
	if own := mrs["!7"]; !own.MR.Draft || *own.Flag == wire.FlagReview || len(own.Me) != 2 || own.Me[0] != wire.RoleAuthor {
		t.Errorf("the person's own merge request is not theirs to review: %+v", own)
	}
	if conflicting := mrs["!478"]; *conflicting.Flag != wire.FlagConflict || *conflicting.Person != "ann" || *conflicting.Tone != wire.ToneWarn || !conflicting.MR.NeedsRebase {
		t.Errorf("an author without a name is the login: %+v", conflicting)
	}
	if red := mrs["!471"]; *red.Flag != wire.FlagCIFailed || *red.MR.Pipeline != wire.PipelineFailed || red.MR.Approvals != nil {
		t.Errorf("an instance with no approvals to ask for gives none: %+v", red)
	}
	if mrs["!466"].Flag == nil || *mrs["!466"].Flag != wire.FlagNoReviewer || *mrs["!465"].Flag != wire.FlagOpen {
		t.Errorf("%+v %+v", mrs["!466"], mrs["!465"])
	}
	if done := mrs["!400"]; *done.State != "merged" || done.Flag != nil || done.ClosedAt == nil || *done.ClosedAt != time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("%+v", done)
	}
	if gone := mrs["!300"]; *gone.State != "closed" || gone.Person != nil || gone.ClosedAt == nil || *gone.ClosedAt != gone.UpdatedAt {
		t.Errorf("a closed one without a closing time is closed when it last moved: %+v", gone)
	}
}

// What the list does not carry is asked per merge request and may fail: the list stays, without the pipeline.
func TestAppsListSurvivesAFailedApprovalsRead(t *testing.T) {
	r := newAppsRig(t)
	r.approvalsCode = 404
	gitlab := appOf(t, r.service(t, nil).Apps(context.Background()), wire.AppGitLab)
	if gitlab.State != wire.AppReady {
		t.Fatalf("%+v", gitlab)
	}
	for _, row := range rowsOf(t, gitlab, wire.SectionMRs) {
		if row.MR.Approvals != nil {
			t.Errorf("%s has approvals nobody could read", row.ID)
		}
	}
}

func TestAppsSayWhyTheyAreNotThere(t *testing.T) {
	r := newAppsRig(t)
	reason := func(a wire.AppFeed, state wire.AppState, want wire.AppReason) {
		t.Helper()
		if a.State != state || a.Reason == nil || *a.Reason != want || len(a.Sections) != 0 {
			t.Errorf("%s: %+v", a.ID, a)
		}
	}

	bare := New(Config{Transport: tlsTo(r.srv)}).Apps(context.Background())
	reason(appOf(t, bare, wire.AppTracker), wire.AppOff, wire.AppNotConfigured)
	reason(appOf(t, bare, wire.AppGitLab), wire.AppOff, wire.AppNotConfigured)
	if appOf(t, bare, wire.AppTracker).Host != nil {
		t.Error("no address named, no host")
	}

	t.Setenv("YOUTRACK_TOKEN", "")
	t.Setenv("GITLAB_SELF_TOKEN", "")
	named := New(Config{Transport: tlsTo(r.srv), Home: t.TempDir(), TrackerURL: "https://tracker.example.com", GitLabHosts: []string{"gitlab.example.com"}})
	feed := named.Apps(context.Background())
	reason(appOf(t, feed, wire.AppTracker), wire.AppOff, wire.AppNoToken)
	reason(appOf(t, feed, wire.AppGitLab), wire.AppOff, wire.AppNoToken)
	if h := appOf(t, feed, wire.AppTracker).Host; h == nil || *h != "tracker.example.com" {
		t.Errorf("the host is said with the missing token: %v", h)
	}

	// A token the service refuses, and a service that is down.
	t.Setenv("YOUTRACK_TOKEN", "wrong")
	t.Setenv("GITLAB_SELF_TOKEN", "wrong")
	refused := New(Config{Transport: tlsTo(r.srv), TrackerURL: "https://tracker.example.com", GitLabHosts: []string{"gitlab.example.com"}}).Apps(context.Background())
	reason(appOf(t, refused, wire.AppTracker), wire.AppFailed, wire.AppDenied)
	reason(appOf(t, refused, wire.AppGitLab), wire.AppFailed, wire.AppDenied)

	down := newAppsRig(t)
	down.tracker, down.gitlab = 502, 500
	feed = down.service(t, nil).Apps(context.Background())
	reason(appOf(t, feed, wire.AppTracker), wire.AppFailed, wire.AppUnreachable)
	reason(appOf(t, feed, wire.AppGitLab), wire.AppFailed, wire.AppUnreachable)

	gone := newAppsRig(t)
	svc := gone.service(t, nil)
	gone.srv.Close()
	feed = svc.Apps(context.Background())
	reason(appOf(t, feed, wire.AppTracker), wire.AppFailed, wire.AppUnreachable)
}

// A ready answer is shared for a while; one that failed is asked again at once, so a token put in place is taken.
func TestAppsAnswersAreKeptOnlyWhenReady(t *testing.T) {
	r := newAppsRig(t)
	s := r.service(t, nil)
	s.Apps(context.Background())
	after := r.hits.Load()
	s.Apps(context.Background())
	if r.hits.Load() != after {
		t.Fatalf("a second look within the minute reads nothing: %d then %d", after, r.hits.Load())
	}

	r.tracker = 503
	s.apps.mu.Lock()
	for id, e := range s.apps.last {
		e.at = e.at.Add(-appsFresh - time.Second)
		s.apps.last[id] = e
	}
	s.apps.mu.Unlock()
	if a := appOf(t, s.Apps(context.Background()), wire.AppTracker); a.State != wire.AppFailed {
		t.Fatalf("%+v", a)
	}
	before := r.hits.Load()
	r.tracker = 200
	if a := appOf(t, s.Apps(context.Background()), wire.AppTracker); a.State != wire.AppReady {
		t.Fatalf("a failure is not kept: %+v", a)
	}
	if r.hits.Load() == before {
		t.Fatal("the tracker was not asked again")
	}
}
