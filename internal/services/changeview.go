package services

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The change view is what a tab shows of a merge request, a pull request, a commit or an issue, whichever service it
// lives on: GitLab and GitHub refuse frames, so the title, the state, the checks, the description, the discussion and
// the diff come through their APIs. Both services answer one shape; "provider" says which one it is. The older
// GitLab-only view (GitLabView) stays as it was.

const (
	noteMax       = 4_000
	threadsMax    = 100
	jobsPerPage   = 100
	limitedNoAuth = "no-token"
)

// IsChangeRef: the change view serves this link.
func (s *Service) IsChangeRef(raw string) bool {
	return s.IsGitLabRef(raw) || s.githubRefOf(raw) != nil
}

// ChangeView is the change a link names with its checks and discussion. nil when the service does not give it to this
// token (GitLab) — GitHub answers a card with the reason instead, so the tab never stays empty.
func (s *Service) ChangeView(ctx context.Context, raw string) (*jsvalue.Object, error) {
	if ref := s.githubRefOf(raw); ref != nil {
		return s.githubView(ctx, ref), nil
	}
	return s.gitlabView(ctx, raw, func(view *jsvalue.Object, item any, a *access, project any, ref *mrRef) {
		s.gitlabExtras(ctx, view, item, a, project, ref)
	})
}

// pipelineStatusOf is a status of a pipeline, a job or a check as the page names it.
func pipelineStatusOf(st string) string {
	if knownPipeline[st] {
		return st
	}
	switch st {
	case "preparing", "waiting_for_resource", "scheduled":
		return "pending"
	}
	return "other"
}

// worstStatus is the one status that stands for a group: a failure beats a run, a run a wait, a wait a success.
func worstStatus(list []string) string {
	rank := map[string]int{"failed": 7, "running": 6, "pending": 5, "created": 5, "manual": 4, "canceled": 3, "other": 2, "success": 1, "skipped": 0}
	best, out := -1, ""
	for _, st := range list {
		if r, ok := rank[st]; ok && r > best {
			best, out = r, st
		}
	}
	return out
}

func jobOf(name, status, url string, durationMs any) *jsvalue.Object {
	var u any
	if url != "" {
		u = url
	}
	return obj("name", name, "status", status, "url", u, "durationMs", durationMs)
}

func stageOf(name string, url any, jobs []*jsvalue.Object) *jsvalue.Object {
	statuses := make([]string, len(jobs))
	list := make([]any, len(jobs))
	for i, j := range jobs {
		statuses[i] = j.String("status")
		list[i] = j
	}
	return obj("name", name, "status", worstStatus(statuses), "url", url, "jobs", list)
}

func noteOf(author any, at any, body any) *jsvalue.Object {
	text, _ := body.(string)
	return obj("author", orNull(author), "at", orNull(at), "body", jscompat.Slice(text, noteMax))
}

// threadOf is one discussion: resolved is true/false for what can be resolved and null for the rest.
func threadOf(id string, resolved any, path any, line any, outdated bool, notes []any) *jsvalue.Object {
	return obj("id", id, "resolved", resolved, "path", orNull(path), "line", orNull(line), "outdated", outdated, "notes", notes)
}

// gitlabExtras adds the provider, the labels, the stages of the pipeline with their jobs and the discussions.
func (s *Service) gitlabExtras(ctx context.Context, view *jsvalue.Object, item any, a *access, project any, ref *mrRef) {
	id := files.EncodeURIComponent(str(project))
	pipeline := opt(item, "head_pipeline")
	discussions := "/projects/" + id + "/merge_requests/" + jscompat.Number(ref.iid) + "/discussions?per_page=" + strconv.Itoa(threadsMax)
	if ref.sha != "" {
		pipeline = opt(item, "last_pipeline")
		discussions = "/projects/" + id + "/repository/commits/" + ref.sha + "/discussions?per_page=" + strconv.Itoa(threadsMax)
	}
	var jobs, threads any
	var w sync.WaitGroup
	w.Add(2)
	go func() {
		defer w.Done()
		if pid := opt(pipeline, "id"); truthy(pid) {
			jobs, _ = s.api(ctx, a, "/projects/"+id+"/pipelines/"+str(pid)+"/jobs?per_page="+strconv.Itoa(jobsPerPage))
		}
	}()
	go func() { defer w.Done(); threads, _ = s.api(ctx, a, discussions) }()
	w.Wait()

	labels := []any{}
	if list, ok := opt(item, "labels").([]any); ok {
		for _, l := range list {
			if name, ok := l.(string); ok {
				labels = append(labels, name)
			}
		}
	}
	view.Set("provider", "gitlab")
	view.Set("labels", labels)
	view.Set("stages", gitlabStages(jobs))
	view.Set("comments", gitlabThreads(threads))
	view.Set("limited", nil)
}

// gitlabStages groups the jobs of a pipeline by stage, in the order the pipeline ran them; null for none.
func gitlabStages(list any) any {
	rows, ok := list.([]any)
	if !ok || len(rows) == 0 {
		return nil
	}
	type row struct {
		id    float64
		stage string
		job   *jsvalue.Object
	}
	var all []row
	for _, r := range rows {
		name, _ := opt(r, "name").(string)
		stage, _ := opt(r, "stage").(string)
		status, _ := opt(r, "status").(string)
		url, _ := opt(r, "web_url").(string)
		var duration any
		if d, ok := opt(r, "duration").(float64); ok {
			duration = float64(int64(d * 1000))
		}
		id, _ := opt(r, "id").(float64)
		all = append(all, row{id, stage, jobOf(name, pipelineStatusOf(status), url, duration)})
	}
	// The API lists the newest job first; the stage that has the oldest job ran first.
	sort.SliceStable(all, func(i, j int) bool { return all[i].id < all[j].id })
	var order []string
	byStage := map[string][]*jsvalue.Object{}
	for _, r := range all {
		if _, seen := byStage[r.stage]; !seen {
			order = append(order, r.stage)
		}
		byStage[r.stage] = append(byStage[r.stage], r.job)
	}
	out := make([]any, len(order))
	for i, name := range order {
		out[i] = stageOf(name, nil, byStage[name])
	}
	return out
}

// gitlabThreads are the discussions of a merge request or a commit without the system notes; null when GitLab gave none.
func gitlabThreads(list any) any {
	rows, ok := list.([]any)
	if !ok {
		return nil
	}
	out := []any{}
	for _, r := range rows {
		if len(out) == threadsMax {
			break
		}
		raw, _ := opt(r, "notes").([]any)
		var notes []any
		var path, line any
		resolvable, resolved := false, true
		for _, n := range raw {
			if truthy(opt(n, "system")) {
				continue
			}
			if notes == nil {
				if pos := opt(n, "position"); truthy(pos) {
					path, line = opt(pos, "new_path"), opt(pos, "new_line")
					if nullish(line) {
						path, line = opt(pos, "old_path"), opt(pos, "old_line")
					}
				}
			}
			if truthy(opt(n, "resolvable")) {
				resolvable = true
				resolved = resolved && truthy(opt(n, "resolved"))
			}
			notes = append(notes, noteOf(opt(opt(n, "author"), "name"), opt(n, "created_at"), opt(n, "body")))
		}
		if len(notes) == 0 {
			continue
		}
		var state any
		if resolvable {
			state = resolved
		}
		did, _ := opt(r, "id").(string)
		out = append(out, threadOf(did, state, path, line, false, notes))
	}
	return out
}

// splitMessage is the first line of a commit message and the rest of a commit's first line and the rest of its message.
func splitMessage(message string) (title, rest string) {
	title, rest, _ = strings.Cut(message, "\n")
	return strings.TrimSpace(title), strings.TrimSpace(rest)
}
