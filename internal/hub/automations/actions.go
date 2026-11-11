package automations

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// What a run does after its conversation reported: the hub tells whoever decides (a messenger bot behind
// HUB_AUTOMATIONS_NOTIFY_URL, with the decision buttons), and the decision merges or closes the run's merge request in
// the GitLab of HUB_AUTOMATIONS_GITLAB_URL. Both are off while their address is empty; the run's status is kept either way.

// StatusMerged is an approved run whose merge request the hub merged.
const StatusMerged = "merged"

const (
	notifyTimeout = 10 * time.Second
	gitlabTimeout = 20 * time.Second
)

var mrURLPattern = regexp.MustCompile(`^(https?://[^/\s]+)/(\S+?)/-/merge_requests/(\d+)$`)

// mrRef is a merge request named by its address.
type mrRef struct {
	project string
	iid     int
}

// mrOf reads a merge request address of the GitLab the hub is set up for, and only of the projects it may touch.
func (s *Service) mrOf(mrURL string) (mrRef, error) {
	base := s.cfg.AutomationsGitLabURL
	m := mrURLPattern.FindStringSubmatch(mrURL)
	if base == "" || m == nil || !strings.EqualFold(m[1], base) {
		return mrRef{}, fmt.Errorf("%q is not a merge request of %s", mrURL, base)
	}
	for _, p := range s.cfg.AutomationsMRProjects {
		if strings.HasPrefix(m[2], p) {
			var iid int
			_, _ = fmt.Sscanf(m[3], "%d", &iid)
			return mrRef{project: m[2], iid: iid}, nil
		}
	}
	return mrRef{}, fmt.Errorf("automations may not touch merge requests of %s", m[2])
}

// gitlabOn says whether approvals reach GitLab at all.
func (s *Service) gitlabOn() bool {
	return s.cfg.AutomationsGitLabURL != "" && s.cfg.AutomationsGitLabToken != "" && len(s.cfg.AutomationsMRProjects) > 0
}

func (s *Service) gitlab(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, gitlabTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.AutomationsGitLabURL+"/api/v4"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", s.cfg.AutomationsGitLabToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("GitLab %s %s answered %d: %.200s", method, path, res.StatusCode, out)
	}
	return out, nil
}

// mergeApproved fast-forward merges the run's merge request, only when it is open, mergeable and its pipeline is green:
// the one thing an automation does without a further word, because its owner approved this very run. It never squashes,
// never merges a red pipeline and touches nothing but this merge request. merged is false with a reason when it was not
// merged; the error is for GitLab being out of reach.
func (s *Service) mergeApproved(ctx context.Context, mrURL string) (merged bool, reason string, err error) {
	ref, err := s.mrOf(mrURL)
	if err != nil {
		return false, err.Error(), nil
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", url.PathEscape(ref.project), ref.iid)
	raw, err := s.gitlab(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, "", err
	}
	var mr struct {
		State        string `json:"state"`
		MergeStatus  string `json:"merge_status"`
		HeadPipeline *struct {
			Status string `json:"status"`
		} `json:"head_pipeline"`
	}
	if err := json.Unmarshal(raw, &mr); err != nil {
		return false, "", err
	}
	switch {
	case mr.State != "opened":
		return false, "MR is " + mr.State + ", not open", nil
	case mr.HeadPipeline == nil:
		return false, "MR has no pipeline, nothing proves it is green", nil
	case mr.HeadPipeline.Status != "success":
		return false, "pipeline is " + mr.HeadPipeline.Status + ", not green", nil
	case mr.MergeStatus != "can_be_merged":
		return false, "GitLab reports merge_status " + mr.MergeStatus, nil
	}
	if _, err := s.gitlab(ctx, http.MethodPut, path+"/merge", map[string]any{"merge_when_pipeline_succeeds": false, "squash": false, "should_remove_source_branch": false}); err != nil {
		return false, "", err
	}
	return true, "", nil
}

// closeRejected closes the merge request of a rejected run.
func (s *Service) closeRejected(ctx context.Context, mrURL string) error {
	ref, err := s.mrOf(mrURL)
	if err != nil {
		return err
	}
	_, err = s.gitlab(ctx, http.MethodPut, fmt.Sprintf("/projects/%s/merge_requests/%d", url.PathEscape(ref.project), ref.iid), map[string]any{"state_event": "close"})
	return err
}

// Outcome is what a decision came to.
type Outcome struct {
	Run    Run    `json:"run"`
	Merged bool   `json:"merged"`
	Reason string `json:"reason,omitempty"`
}

func (s *Service) fail(ctx context.Context, id, reason string) {
	if _, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = ?, error = ?, finished_at = COALESCE(finished_at, ?) WHERE id = ?`,
		StatusFailed, reason, store.Ms(s.now()), id); err != nil {
		s.log.Warn("automation run not marked failed", "run", id, "error", err)
	}
}

// settle does what a recorded decision asks of the merge request. A merge that cannot be done does not leave the run
// approved for good: it is failed with the reason.
func (s *Service) settle(ctx context.Context, run Run) Outcome {
	out := Outcome{Run: run}
	if !s.gitlabOn() {
		out.Reason = "the hub is not set up to merge or close merge requests"
		return out
	}
	if run.Status == StatusRejected {
		if run.MRURL != "" {
			if err := s.closeRejected(ctx, run.MRURL); err != nil {
				s.log.Error("automation: rejected merge request not closed", "run", run.ID, "error", err)
				out.Reason = "not closed: " + err.Error()
			}
		}
		return out
	}
	if run.MRURL == "" {
		s.fail(ctx, run.ID, "approved but the run has no MR url")
		out.Run.Status, out.Reason = StatusFailed, "no MR url on this run"
		return out
	}
	merged, reason, err := s.mergeApproved(ctx, run.MRURL)
	if err != nil {
		s.log.Error("automation: approved merge request not merged", "run", run.ID, "error", err)
		s.fail(ctx, run.ID, "merge failed: "+err.Error())
		out.Run.Status, out.Reason = StatusFailed, err.Error()
		return out
	}
	if merged {
		if _, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = ? WHERE id = ? AND status = ?`, StatusMerged, run.ID, StatusApproved); err != nil {
			s.log.Warn("automation run not marked merged", "run", run.ID, "error", err)
		}
		out.Run.Status, out.Merged = StatusMerged, true
		return out
	}
	out.Reason = reason
	return out
}

// notify tells the decider about a run that reported. A failure is logged and changes nothing about the run.
func (s *Service) notify(ctx context.Context, run Run) {
	if s.cfg.AutomationsNotifyURL == "" {
		return
	}
	rule, err := s.ruleByID(ctx, run.RuleID)
	if err != nil {
		s.log.Warn("automation notice: rule not found", "run", run.ID, "error", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"runId": run.ID, "title": rule.Name, "summary": run.Summary, "bugIssueId": run.BugIssueID, "mrUrl": run.MRURL,
		"platformUrl": s.cfg.PublicURL,
	})
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AutomationsNotifyURL, bytes.NewReader(body))
	if err != nil {
		s.log.Warn("automation notice not sent", "run", run.ID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.AutomationsNotifyToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AutomationsNotifyToken)
	}
	res, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("automation notice not sent", "run", run.ID, "error", err)
		return
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode/100 != 2 {
		s.log.Warn("automation notice refused", "run", run.ID, "status", res.StatusCode)
	}
}

// callback is the decision of whoever the notice reached, with HUB_AUTOMATIONS_SECRET in X-Automation-Secret: the bot
// checked who pressed the button, the hub only checks that the bot is the caller. {runId, action: approve|reject}.
func (s *Service) callback(w http.ResponseWriter, r *http.Request) error {
	if !s.Enabled() {
		return errOff
	}
	given := r.Header.Get("X-Automation-Secret")
	if s.cfg.AutomationsSecret == "" || subtle.ConstantTimeCompare([]byte(given), []byte(s.cfg.AutomationsSecret)) != 1 {
		return httpx.ErrForbidden
	}
	var b struct {
		RunID  string `json:"runId"`
		Action string `json:"action"`
	}
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	status := map[string]string{"approve": StatusApproved, "reject": StatusRejected}[b.Action]
	if b.RunID == "" || status == "" {
		return invalid("Нужны runId и action (approve или reject)")
	}
	run, ok, err := s.decide(r.Context(), b.RunID, "", status)
	if errors.Is(err, store.ErrNotFound) {
		return errNoRun
	}
	if err != nil {
		return err
	}
	if !ok {
		return errDecided
	}
	out := s.settle(r.Context(), run)
	resp := map[string]any{"ok": true, "status": out.Run.Status, "merged": out.Merged}
	if out.Reason != "" {
		resp["reason"] = out.Reason
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}
