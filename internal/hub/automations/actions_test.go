package automations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeGitLab answers the three calls an approval makes and remembers the writes.
type fakeGitLab struct {
	mu     sync.Mutex
	mr     string // the body of GET merge request
	writes []string
	srv    *httptest.Server
}

func newFakeGitLab(t *testing.T, mr string) *fakeGitLab {
	t.Helper()
	f := &fakeGitLab{mr: mr}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-x" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, f.mr)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.writes = append(f.writes, r.Method+" "+r.URL.EscapedPath()+" "+string(b))
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

const greenMR = `{"state":"opened","merge_status":"can_be_merged","head_pipeline":{"status":"success"}}`

// approvable is a service with a run waiting for approval on the merge request of the fake GitLab.
func approvable(t *testing.T, mr string) (*Service, *fakeGitLab, string) {
	t.Helper()
	s := newSvc(t, noon())
	gl := newFakeGitLab(t, mr)
	s.cfg.AutomationsGitLabURL = gl.srv.URL
	s.cfg.AutomationsGitLabToken = "glpat-x"
	s.cfg.AutomationsMRProjects = []string{"harnessly/", "team/"}
	ctx := context.Background()
	r := rule(t, s, "posthog-test", Limits{})
	out, _ := s.queueRun(ctx, r, "f1", json.RawMessage(`{}`))
	run, _, token, _, err := s.claim(ctx, anyMachine)
	if err != nil || run.ID != out.RunID {
		t.Fatalf("claim: %v", err)
	}
	if err := s.recordResult(ctx, run.ID, token, "fixed", "HRN-1", gl.srv.URL+"/harnessly/harnessly/-/merge_requests/7"); err != nil {
		t.Fatal(err)
	}
	return s, gl, run.ID
}

func callback(s *Service, secret, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	s.Mount(mux)
	req := httptest.NewRequest("POST", s.prefix+"/callback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Automation-Secret", secret)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestCallbackApproveMergesAGreenMR(t *testing.T) {
	s, gl, id := approvable(t, greenMR)
	w := callback(s, "s3cret", `{"runId":"`+id+`","action":"approve"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"merged":true`) || !strings.Contains(w.Body.String(), `"status":"merged"`) {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	if len(gl.writes) != 1 || !strings.Contains(gl.writes[0], "PUT /api/v4/projects/harnessly%2Fharnessly/merge_requests/7/merge") ||
		!strings.Contains(gl.writes[0], `"squash":false`) || !strings.Contains(gl.writes[0], `"merge_when_pipeline_succeeds":false`) {
		t.Fatalf("writes: %v", gl.writes)
	}
	if got := statusOf(t, s, id).Status; got != StatusMerged {
		t.Fatalf("status %q", got)
	}
	if w := callback(s, "s3cret", `{"runId":"`+id+`","action":"approve"}`); w.Code != http.StatusConflict {
		t.Fatalf("second decision: %d", w.Code)
	}
}

func TestCallbackApproveDoesNotMergeWhatIsNotGreen(t *testing.T) {
	for name, mr := range map[string]string{
		"red":      `{"state":"opened","merge_status":"can_be_merged","head_pipeline":{"status":"failed"}}`,
		"conflict": `{"state":"opened","merge_status":"cannot_be_merged","head_pipeline":{"status":"success"}}`,
		"closed":   `{"state":"closed","merge_status":"can_be_merged","head_pipeline":{"status":"success"}}`,
		"nopipe":   `{"state":"opened","merge_status":"can_be_merged"}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, gl, id := approvable(t, mr)
			w := callback(s, "s3cret", `{"runId":"`+id+`","action":"approve"}`)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"merged":false`) || !strings.Contains(w.Body.String(), `"reason"`) {
				t.Fatalf("answer: %d %s", w.Code, w.Body)
			}
			if len(gl.writes) != 0 {
				t.Fatalf("GitLab was written to: %v", gl.writes)
			}
			if got := statusOf(t, s, id).Status; got != StatusApproved {
				t.Fatalf("status %q", got)
			}
		})
	}
}

func TestCallbackRejectClosesTheMR(t *testing.T) {
	s, gl, id := approvable(t, greenMR)
	w := callback(s, "s3cret", `{"runId":"`+id+`","action":"reject"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"rejected"`) {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	if len(gl.writes) != 1 || !strings.Contains(gl.writes[0], `"state_event":"close"`) {
		t.Fatalf("writes: %v", gl.writes)
	}
}

func TestCallbackNeedsTheSecretAndAKnownAction(t *testing.T) {
	s, gl, id := approvable(t, greenMR)
	for _, secret := range []string{"", "wrong"} {
		if w := callback(s, secret, `{"runId":"`+id+`","action":"approve"}`); w.Code != http.StatusForbidden {
			t.Fatalf("secret %q: %d", secret, w.Code)
		}
	}
	if w := callback(s, "s3cret", `{"runId":"`+id+`","action":"merge"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: %d", w.Code)
	}
	if w := callback(s, "s3cret", `{"runId":"nope","action":"approve"}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown run: %d", w.Code)
	}
	if len(gl.writes) != 0 || statusOf(t, s, id).Status != StatusAwaitingApproval {
		t.Fatalf("a refused callback changed something: %v %q", gl.writes, statusOf(t, s, id).Status)
	}
}

func TestMergeStaysInsideTheAllowedProjects(t *testing.T) {
	s, gl, _ := approvable(t, greenMR)
	for _, u := range []string{
		gl.srv.URL + "/secret/other/-/merge_requests/1",
		"https://evil.example/harnessly/harnessly/-/merge_requests/1",
		gl.srv.URL + "/harnessly/harnessly/-/issues/1",
	} {
		if _, err := s.mrOf(u); err == nil {
			t.Fatalf("%s accepted", u)
		}
	}
	merged, reason, err := s.mergeApproved(context.Background(), gl.srv.URL+"/secret/other/-/merge_requests/1")
	if merged || err != nil || reason == "" || len(gl.writes) != 0 {
		t.Fatalf("merged=%v reason=%q err=%v writes=%v", merged, reason, err, gl.writes)
	}
}

func TestApprovalWithoutGitLabOnlyRecordsTheDecision(t *testing.T) {
	s, _, id := approvable(t, greenMR)
	s.cfg.AutomationsGitLabToken = ""
	w := callback(s, "s3cret", `{"runId":"`+id+`","action":"approve"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"merged":false`) {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	if got := statusOf(t, s, id).Status; got != StatusApproved {
		t.Fatalf("status %q", got)
	}
}

func TestNoticeCarriesTheRunToTheBot(t *testing.T) {
	s, _, id := approvable(t, greenMR)
	var got map[string]any
	var auth string
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer bot.Close()
	s.cfg.AutomationsNotifyURL, s.cfg.AutomationsNotifyToken = bot.URL, "tok"
	s.notify(context.Background(), statusOf(t, s, id))
	if auth != "Bearer tok" || got["runId"] != id || got["title"] != "posthog-test" || got["summary"] != "fixed" ||
		got["bugIssueId"] != "HRN-1" || !strings.HasSuffix(got["mrUrl"].(string), "/merge_requests/7") {
		t.Fatalf("notice: auth=%q body=%v", auth, got)
	}
}
