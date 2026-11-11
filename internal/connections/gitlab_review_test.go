package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// glReviewServer is a GitLab with one merge request (acme/app!5) and records the writes it gets.
func glReviewServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-good" {
			provJSON(w, 401, `{"message":"401 Unauthorized"}`)
			return
		}
		path := r.URL.EscapedPath()
		mr := "/api/v4/projects/acme%2Fapp/merge_requests/5"
		if r.Method != http.MethodGet {
			body, _ := io.ReadAll(r.Body)
			writes = append(writes, r.Method+" "+strings.TrimPrefix(path, mr)+"?"+r.URL.RawQuery+" "+string(body))
			if r.Method == http.MethodPost && path == "/api/v4/projects/acme%2Fapp/merge_requests" {
				provJSON(w, 201, `{"web_url":"https://gl.example/acme/app/-/merge_requests/6"}`)
				return
			}
			provJSON(w, 200, `{}`)
			return
		}
		switch path {
		case "/api/v4/user":
			provJSON(w, 200, `{"id":1,"username":"alice","name":"Alice"}`)
		case mr:
			provJSON(w, 200, `{"title":"Draft: HRN-1 thing","state":"opened","draft":true,"author":{"id":2,"name":"Bob"},
"diff_refs":{"base_sha":"aaaaaaa","start_sha":"bbbbbbb","head_sha":"ccccccc"}}`)
		case mr + "/approvals":
			provJSON(w, 200, `{"user_has_approved":false,"user_can_approve":true,"approved_by":[{"user":{"id":3,"name":"Carol"}}]}`)
		case mr + "/discussions":
			provJSON(w, 200, `[
{"id":"d1","notes":[{"id":11,"body":"why?","author":{"id":2,"name":"Bob"},"created_at":"2026-11-10T10:00:00Z","updated_at":"2026-11-10T10:00:00Z",
  "resolvable":true,"resolved":false,"position":{"head_sha":"ccccccc","new_path":"a.go","old_path":"a.go","new_line":7,"old_line":null}},
  {"id":12,"body":"because","author":{"id":1,"name":"Alice"},"created_at":"2026-11-10T11:00:00Z","updated_at":"2026-11-10T12:00:00Z"}]},
{"id":"d2","notes":[{"id":21,"body":"merged main","system":true}]},
{"id":"d3","notes":[{"id":31,"body":"old","author":{"id":2,"name":"Bob"},"resolvable":true,"resolved":true,
  "position":{"head_sha":"0000000","new_path":"b.go","old_path":"b.go","new_line":null,"old_line":3}}]},
{"id":"d4","notes":[{"id":41,"body":"overall","author":{"id":3,"name":"Carol"}}]}]`)
		case mr + "/draft_notes":
			provJSON(w, 200, `[{"id":7,"note":"rename","position":{"new_path":"a.go","old_path":"a.go","new_line":9}},
{"id":8,"note":"agree","discussion_id":"d1"}]`)
		default:
			provJSON(w, 404, `{"message":"404 Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &writes
}

func glReviewOpen(srv *httptest.Server, token string) ReviewSource {
	return gitLabProvider{}.Open(Conn{Kind: GitLab, BaseURL: srv.URL, Deployment: SelfHosted},
		Credentials{FieldToken: token}, NewHTTP(nil)).(ReviewSource)
}

func TestGitLabReviewReadsThreadsDraftsAndApprovals(t *testing.T) {
	srv, _ := glReviewServer(t)
	rv, err := glReviewOpen(srv, "glpat-good").Review(context.Background(), "acme/app", 5)
	if err != nil {
		t.Fatal(err)
	}
	if rv.Mine || !rv.Draft || !rv.CanApprove || rv.Approved || len(rv.ApprovedBy) != 1 || rv.Refs.Head != "ccccccc" {
		t.Errorf("review %+v", rv)
	}
	if len(rv.Threads) != 3 {
		t.Fatalf("threads %+v", rv.Threads)
	}
	first := rv.Threads[0]
	if first.Path != "a.go" || first.Line != 7 || first.Side != "after" || first.Outdated || len(first.Notes) != 2 ||
		first.Notes[0].Mine || !first.Notes[1].Mine || !first.Notes[1].Edited {
		t.Errorf("first thread %+v", first)
	}
	if old := rv.Threads[1]; old.Side != "before" || old.Line != 3 || !old.Outdated || !old.Resolved {
		t.Errorf("old thread %+v", old)
	}
	if overall := rv.Threads[2]; overall.Path != "" || overall.Line != 0 {
		t.Errorf("overall thread %+v", overall)
	}
	if len(rv.Drafts) != 2 || rv.Drafts[0].Line != 9 || rv.Drafts[0].Side != "after" || rv.Drafts[1].ReplyTo != "d1" {
		t.Errorf("drafts %+v", rv.Drafts)
	}
}

func TestGitLabReviewActs(t *testing.T) {
	srv, writes := glReviewServer(t)
	src := glReviewOpen(srv, "glpat-good")
	ctx := context.Background()
	refs := ReviewRefs{Base: "aaaaaaa", Start: "bbbbbbb", Head: "ccccccc"}
	for _, a := range []ReviewAct{
		{Op: ActReply, Thread: "d1", Body: "ok"},
		{Op: ActEdit, Thread: "d1", Note: 12, Body: "because of x"},
		{Op: ActDelete, Thread: "d1", Note: 12},
		{Op: ActResolve, Thread: "d1", Resolved: true},
		{Op: ActDraft, Path: "a.go", Line: 4, Side: "before", Refs: refs, Body: "why"},
		{Op: ActDraft, Thread: "d1", Body: "+1"},
		{Op: ActDraft, Path: "a.go", Line: 10, Side: "after", Paired: 8, Refs: refs, Body: "ctx"},
		{Op: ActDraftEdit, Draft: 7, Body: "rename it"},
		{Op: ActDraftDelete, Draft: 8},
		{Op: ActPublish},
		{Op: ActApprove},
		{Op: ActReady},
	} {
		if _, err := src.ReviewAct(ctx, "acme/app", 5, a); err != nil {
			t.Fatalf("%s: %v", a.Op, err)
		}
	}
	got := strings.Join(*writes, "\n")
	for _, want := range []string{
		`POST /discussions/d1/notes? {"body":"ok"}`,
		`PUT /discussions/d1/notes/12? {"body":"because of x"}`,
		`DELETE /discussions/d1/notes/12? `,
		`PUT /discussions/d1?resolved=true `,
		`POST /draft_notes? {"in_reply_to_discussion_id":"d1","note":"+1"}`,
		`PUT /draft_notes/7? {"note":"rename it"}`,
		`DELETE /draft_notes/8? `,
		`"new_line":10,"new_path":"a.go","old_line":8`,
		`POST /draft_notes/bulk_publish? `,
		`POST /approve? `,
		`PUT ? {"title":"HRN-1 thing"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	// The new thread is placed on the old side's line, at the refs the person saw.
	for _, w := range *writes {
		if !strings.HasPrefix(w, "POST /draft_notes? {\"note\":\"why\"") {
			continue
		}
		var body struct{ Position map[string]any }
		_ = json.Unmarshal([]byte(strings.SplitN(w, " ", 3)[2]), &body)
		if body.Position["old_line"] != float64(4) || body.Position["new_line"] != nil || body.Position["head_sha"] != "ccccccc" {
			t.Errorf("position %v", body.Position)
		}
	}
}

func TestGitLabReviewRefusesWhatItCannotDo(t *testing.T) {
	srv, writes := glReviewServer(t)
	src := glReviewOpen(srv, "glpat-good")
	ctx := context.Background()
	for _, a := range []ReviewAct{
		{Op: "merge"},
		{Op: ActReply, Thread: "d1", Body: "  "},
		{Op: ActDraft, Path: "a.go", Line: 4, Side: "after", Body: "x"}, // no refs
		{Op: ActDraft, Path: "a.go", Line: 4, Side: "left", Refs: ReviewRefs{Base: "aaaaaaa", Start: "aaaaaaa", Head: "aaaaaaa"}, Body: "x"},
		{Op: ActEdit, Thread: "d1", Body: "x"},
	} {
		if _, err := src.ReviewAct(ctx, "acme/app", 5, a); !errors.Is(err, ErrBadAct) {
			t.Errorf("%+v: %v", a, err)
		}
	}
	if len(*writes) != 0 {
		t.Errorf("writes %v", *writes)
	}
	if _, err := glReviewOpen(srv, "glpat-bad").Review(ctx, "acme/app", 5); !errors.Is(err, ErrDenied) {
		t.Errorf("a bad token: %v", err)
	}
}

func TestGitLabOpenMergeRequest(t *testing.T) {
	srv, writes := glReviewServer(t)
	link, err := glReviewOpen(srv, "glpat-good").OpenMergeRequest(context.Background(), "acme/app",
		NewMergeRequest{Source: "agent/x", Target: "main", Title: "HRN-2 feature", Draft: true})
	if err != nil || link != "https://gl.example/acme/app/-/merge_requests/6" {
		t.Fatalf("%q %v", link, err)
	}
	if w := (*writes)[0]; !strings.Contains(w, `"title":"Draft: HRN-2 feature"`) || !strings.Contains(w, `"source_branch":"agent/x"`) {
		t.Errorf("write %s", w)
	}
}

func TestMergeRequestLinkAndDraftMark(t *testing.T) {
	c := Conn{BaseURL: "https://gl.example/gitlab"}
	if p, iid, ok := MergeRequestOf(c, "https://gl.example/gitlab/a/b/c/-/merge_requests/12/diffs?x=1"); !ok || p != "a/b/c" || iid != 12 {
		t.Errorf("%q %d %v", p, iid, ok)
	}
	for _, raw := range []string{"https://other.example/a/-/merge_requests/1", "https://gl.example/gitlab/a/-/issues/1"} {
		if _, _, ok := MergeRequestOf(c, raw); ok {
			t.Errorf("%s read as a merge request", raw)
		}
	}
	for in, want := range map[string]string{"Draft: x": "x", "[Draft] Draft: x": "x", "WIP: x": "x", "x": "x", "Drafted plan": "Drafted plan"} {
		if got := withoutDraftMark(in); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}
