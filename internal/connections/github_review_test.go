package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const ghPullAnswer = `{"data":{"viewer":{"login":"alice"},"repository":{"pullRequest":{
"id":"PR_1","title":"HRN-1 thing","state":"OPEN","isDraft":true,"headRefOid":"ccccccc","baseRefOid":"aaaaaaa",
"author":{"login":"bob","name":"Bob"},
"viewerLatestReview":{"id":"PRR_mine","state":"APPROVED"},
"latestOpinionatedReviews":{"nodes":[{"state":"APPROVED","author":{"login":"carol","name":"Carol"}},
  {"state":"CHANGES_REQUESTED","author":{"login":"dave"}}]},
"pending":{"nodes":[{"id":"PRR_pending"}]},
"comments":{"nodes":[{"id":"IC_1","databaseId":101,"body":"overall","createdAt":"2026-11-10T09:00:00Z","viewerDidAuthor":false,"author":{"login":"carol","name":"Carol"}}]},
"reviews":{"nodes":[{"id":"PRR_2","databaseId":202,"state":"COMMENTED","body":"looks fine","createdAt":"2026-11-10T08:00:00Z","viewerDidAuthor":true,"author":{"login":"alice"}},
  {"id":"PRR_3","databaseId":203,"state":"APPROVED","body":"","createdAt":"2026-11-10T08:30:00Z","author":{"login":"carol"}}]},
"reviewThreads":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[
 {"id":"PRRT_1","isResolved":false,"isOutdated":false,"path":"a.go","line":7,"originalLine":7,"diffSide":"RIGHT","comments":{"nodes":[
   {"id":"RC_11","databaseId":11,"state":"SUBMITTED","body":"why?","createdAt":"2026-11-10T10:00:00Z","author":{"login":"bob","name":"Bob"}},
   {"id":"RC_12","databaseId":12,"state":"SUBMITTED","body":"because","createdAt":"2026-11-10T11:00:00Z","lastEditedAt":"2026-11-10T12:00:00Z","viewerDidAuthor":true,"author":{"login":"alice"}},
   {"id":"RC_13","databaseId":13,"state":"PENDING","body":"agree","viewerDidAuthor":true,"author":{"login":"alice"}}]}}]}}}}}`

const ghThreadsAnswer = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[
 {"id":"PRRT_2","isResolved":true,"isOutdated":true,"path":"b.go","line":null,"originalLine":3,"diffSide":"LEFT","comments":{"nodes":[
   {"id":"RC_21","databaseId":21,"state":"SUBMITTED","body":"old","author":{"login":"bob"}}]}},
 {"id":"PRRT_3","isResolved":false,"isOutdated":false,"path":"c.go","line":9,"diffSide":"RIGHT","comments":{"nodes":[
   {"id":"RC_31","databaseId":31,"state":"PENDING","body":"rename","viewerDidAuthor":true,"author":{"login":"alice"}}]}}]}}}}}`

var gqlOpRe = regexp.MustCompile(`mutation\([^)]*\)\{(\w+)`)

// ghReviewServer is a GitHub Enterprise Server with one pull request (acme/app#5); it records the writes it gets.
func ghReviewServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_good" {
			provJSON(w, 401, `{"message":"Bad credentials"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/api/graphql" {
			var in struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.Unmarshal(body, &in)
			if m := gqlOpRe.FindStringSubmatch(in.Query); m != nil {
				vars, _ := json.Marshal(in.Variables)
				writes = append(writes, m[1]+" "+string(vars))
				provJSON(w, 200, `{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`)
				return
			}
			if strings.Contains(in.Query, "after:$after") && in.Variables["after"] == "c1" {
				provJSON(w, 200, ghThreadsAnswer)
				return
			}
			provJSON(w, 200, ghPullAnswer)
			return
		}
		if r.Method != http.MethodGet {
			writes = append(writes, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/api/v3/repos/acme/app")+" "+string(body))
			if r.Method == http.MethodPost && r.URL.Path == "/api/v3/repos/acme/app/pulls" {
				provJSON(w, 201, `{"html_url":"https://gh.example/acme/app/pull/6"}`)
				return
			}
			provJSON(w, 200, `{}`)
			return
		}
		provJSON(w, 404, `{"message":"Not Found"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &writes
}

func ghReviewOpen(srv *httptest.Server, token string) ReviewSource {
	return gitHubProvider{}.Open(Conn{Kind: GitHub, BaseURL: srv.URL, APIURL: srv.URL + "/api/v3", Deployment: SelfHosted},
		Credentials{FieldToken: token}, NewHTTP(nil)).(ReviewSource)
}

func TestGitHubReviewReadsThreadsDraftsAndApprovals(t *testing.T) {
	srv, _ := ghReviewServer(t)
	rv, err := ghReviewOpen(srv, "ghp_good").Review(context.Background(), "acme/app", 5)
	if err != nil {
		t.Fatal(err)
	}
	if rv.Mine || !rv.Draft || !rv.CanApprove || !rv.Approved || rv.State != "opened" || rv.Refs.Head != "ccccccc" ||
		len(rv.ApprovedBy) != 1 || rv.ApprovedBy[0] != "Carol" || rv.Me == nil || rv.Me.Login != "alice" {
		t.Errorf("review %+v", rv)
	}
	// Two review threads with notes (the third holds only a draft), then the review's text and the conversation by time.
	if len(rv.Threads) != 4 {
		t.Fatalf("threads %+v", rv.Threads)
	}
	first := rv.Threads[0]
	if first.ID != "PRRT_1" || first.Path != "a.go" || first.Line != 7 || first.Side != "after" || first.Outdated ||
		!first.Resolvable || len(first.Notes) != 2 || first.Notes[0].Mine || !first.Notes[1].Mine || !first.Notes[1].Edited {
		t.Errorf("first thread %+v", first)
	}
	if old := rv.Threads[1]; old.Side != "before" || old.Line != 3 || !old.Outdated || !old.Resolved {
		t.Errorf("old thread %+v", old)
	}
	if review := rv.Threads[2]; review.ID != "review:202" || review.Path != "" || review.Resolvable || !review.Notes[0].Mine {
		t.Errorf("review thread %+v", review)
	}
	if conv := rv.Threads[3]; conv.ID != GitHubConversation || len(conv.Notes) != 1 || conv.Notes[0].ID != 101 {
		t.Errorf("conversation %+v", conv)
	}
	if len(rv.Drafts) != 2 || rv.Drafts[0].ReplyTo != "PRRT_1" || rv.Drafts[0].ID != 13 ||
		rv.Drafts[1].Path != "c.go" || rv.Drafts[1].Line != 9 || rv.Drafts[1].Side != "after" || rv.Drafts[1].ReplyTo != "" {
		t.Errorf("drafts %+v", rv.Drafts)
	}
}

func TestGitHubReviewActs(t *testing.T) {
	srv, writes := ghReviewServer(t)
	src := ghReviewOpen(srv, "ghp_good")
	ctx := context.Background()
	for _, a := range []ReviewAct{
		{Op: ActReply, Thread: "PRRT_1", Body: "ok"},
		{Op: ActReply, Thread: GitHubConversation, Body: "thanks"},
		{Op: ActEdit, Thread: "PRRT_1", Note: 12, Body: "because of x"},
		{Op: ActEdit, Thread: GitHubConversation, Note: 101, Body: "edited"},
		{Op: ActEdit, Thread: "review:202", Note: 202, Body: "fine"},
		{Op: ActDelete, Thread: "PRRT_1", Note: 12},
		{Op: ActDelete, Thread: GitHubConversation, Note: 101},
		{Op: ActResolve, Thread: "PRRT_1", Resolved: true},
		{Op: ActResolve, Thread: "PRRT_2", Resolved: false},
		{Op: ActDraft, Path: "a.go", Line: 4, Side: "before", Body: "why"},
		{Op: ActDraft, Thread: "PRRT_1", Body: "+1"},
		{Op: ActDraftEdit, Draft: 31, Body: "rename it"},
		{Op: ActDraftDelete, Draft: 13},
		{Op: ActPublish},
		{Op: ActApprove},
		{Op: ActUnapprove},
		{Op: ActReady},
	} {
		if _, err := src.ReviewAct(ctx, "acme/app", 5, a); err != nil {
			t.Fatalf("%s: %v", a.Op, err)
		}
	}
	want := []string{
		`addPullRequestReviewThreadReply {"b":"ok","r":null,"t":"PRRT_1"}`,
		`POST /issues/5/comments {"body":"thanks"}`,
		`updatePullRequestReviewComment {"b":"because of x","id":"RC_12"}`,
		`PATCH /issues/comments/101 {"body":"edited"}`,
		`PUT /pulls/5/reviews/202 {"body":"fine"}`,
		`deletePullRequestReviewComment {"id":"RC_12"}`,
		`DELETE /issues/comments/101 `,
		`resolveReviewThread {"t":"PRRT_1"}`,
		`unresolveReviewThread {"t":"PRRT_2"}`,
		// The pending review is there already: a draft goes into it.
		`addPullRequestReviewThread {"b":"why","l":4,"p":"a.go","r":"PRR_pending","s":"LEFT"}`,
		`addPullRequestReviewThreadReply {"b":"+1","r":"PRR_pending","t":"PRRT_1"}`,
		`updatePullRequestReviewComment {"b":"rename it","id":"RC_31"}`,
		`deletePullRequestReviewComment {"id":"RC_13"}`,
		`submitPullRequestReview {"e":"COMMENT","r":"PRR_pending"}`,
		`submitPullRequestReview {"e":"APPROVE","r":"PRR_pending"}`,
		`dismissPullRequestReview {"m":"Approval withdrawn","r":"PRR_mine"}`,
		`markPullRequestReadyForReview {"pr":"PR_1"}`,
	}
	if strings.Join(*writes, "\n") != strings.Join(want, "\n") {
		t.Errorf("writes:\n%s\nwant:\n%s", strings.Join(*writes, "\n"), strings.Join(want, "\n"))
	}
}

func TestGitHubReviewRefusesWhatItCannotDo(t *testing.T) {
	srv, writes := ghReviewServer(t)
	ctx := context.Background()
	src := ghReviewOpen(srv, "ghp_good")
	for _, a := range []ReviewAct{
		{Op: "merge"},
		{Op: ActReply, Thread: "PRRT_nope", Body: "x"},
		{Op: ActReply, Thread: "PRRT_1", Body: "   "},
		{Op: ActEdit, Thread: "PRRT_1", Note: 99, Body: "no such note"},
		{Op: ActDelete, Thread: "review:202", Note: 202},
		{Op: ActResolve, Thread: GitHubConversation, Resolved: true},
		{Op: ActDraft, Thread: GitHubConversation, Body: "x"},
		{Op: ActDraft, Path: "a.go", Line: 0, Side: "after", Body: "x"},
		{Op: ActDraftEdit, Draft: 12, Body: "a submitted note is no draft"},
	} {
		if _, err := src.ReviewAct(ctx, "acme/app", 5, a); !errors.Is(err, ErrBadAct) {
			t.Errorf("%+v: %v", a, err)
		}
	}
	if len(*writes) > 0 {
		t.Errorf("a refused act wrote: %v", *writes)
	}
	if _, err := ghReviewOpen(srv, "ghp_bad").Review(ctx, "acme/app", 5); !errors.Is(err, ErrDenied) {
		t.Errorf("a bad token: %v", err)
	}
	if _, err := src.Review(ctx, "not a repo", 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("a bad project: %v", err)
	}
}

func TestGitHubOpenPullRequestAndLink(t *testing.T) {
	srv, writes := ghReviewServer(t)
	link, err := ghReviewOpen(srv, "ghp_good").OpenMergeRequest(context.Background(), "acme/app",
		NewMergeRequest{Source: "feat", Target: "main", Title: " HRN-1 thing ", Draft: true})
	if err != nil || link != "https://gh.example/acme/app/pull/6" {
		t.Fatalf("%q %v", link, err)
	}
	if got := (*writes)[0]; got != `POST /pulls {"base":"main","draft":true,"head":"feat","title":"HRN-1 thing"}` {
		t.Errorf("write %s", got)
	}
	conn := Conn{Kind: GitHub, BaseURL: "https://github.com"}
	for raw, want := range map[string]string{
		"https://github.com/acme/app/pull/12":           "acme/app#12",
		"https://www.github.com/acme/app/pull/12/files": "acme/app#12",
		"https://github.com/acme/app/issues/12":         "",
		"https://gitlab.com/acme/app/pull/12":           "",
		"https://github.com/acme/app/pull/0":            "",
	} {
		project, n, ok := MergeRequestOf(conn, raw)
		got := ""
		if ok {
			got = project + "#" + strconv.Itoa(n)
		}
		if got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
	cl := gitHubProvider{}.Open(Conn{Kind: GitHub, APIURL: "https://api.github.com"}, Credentials{}, NewHTTP(nil)).(*ghClient)
	if cl.graphqlURL() != "https://api.github.com/graphql" {
		t.Errorf("cloud graphql %s", cl.graphqlURL())
	}
}
