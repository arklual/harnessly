package services

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// What the review screen adds to the change view of a GitLab merge request: who approved and who reviews, the commits,
// how far the source is behind the target, whether it conflicts, and the base and head commits the diff is taken
// between. ChangeFile then reads both versions of one file at those commits, one file at a time, when it is opened.

const (
	reviewCommitsMax = 50
	// changeFileMax is the largest version of a file the review reads; a bigger one is answered as truncated.
	changeFileMax = 1_000_000
)

func textOf(v any) string {
	s, _ := v.(string)
	return s
}

func numberOrNull(v any) any {
	if n, ok := v.(float64); ok {
		return n
	}
	return nil
}

// namesOf are the display names of a list of GitLab users, in order, without empty ones.
func namesOf(list any, user func(any) any) []any {
	out := []any{}
	rows, _ := list.([]any)
	for _, r := range rows {
		if name := textOf(opt(user(r), "name")); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func gitlabCommits(list any) []any {
	out := []any{}
	rows, _ := list.([]any)
	for _, r := range rows {
		if len(out) == reviewCommitsMax {
			break
		}
		sha := textOf(opt(r, "id"))
		if sha == "" {
			continue
		}
		out = append(out, obj("sha", sha, "title", textOf(opt(r, "title")), "author", orNull(opt(r, "author_name")),
			"at", orNull(opt(r, "created_at"))))
	}
	return out
}

// mrItemPath is the merge request as the change view reads it.
func mrItemPath(id string, iid float64) string {
	return "/projects/" + id + "/merge_requests/" + jscompat.Number(iid)
}

// gitlabReview adds the review fields of a merge request (ref.sha empty) or a commit to its change view.
func (s *Service) gitlabReview(ctx context.Context, view *jsvalue.Object, item any, a *access, project any, ref *mrRef) {
	if ref.sha != "" {
		base := ""
		if parents, ok := opt(item, "parent_ids").([]any); ok && len(parents) > 0 {
			base = textOf(parents[0])
		}
		view.Set("diffRefs", obj("base", orNull(base), "head", orNull(opt(item, "id"))))
		return
	}
	id := files.EncodeURIComponent(str(project))
	mr := "/projects/" + id + "/merge_requests/" + jscompat.Number(ref.iid)
	var approvals, commits, diverged any
	var w sync.WaitGroup
	w.Add(3)
	go func() { defer w.Done(); diverged, _ = s.api(ctx, a, mr+"?include_diverged_commits_count=true") }()
	go func() { defer w.Done(); approvals, _ = s.api(ctx, a, mr+"/approvals") }()
	go func() {
		defer w.Done()
		commits, _ = s.api(ctx, a, mr+"/commits?per_page="+strconv.Itoa(reviewCommitsMax))
	}()
	w.Wait()
	refs := opt(item, "diff_refs")
	view.Set("reviewers", namesOf(opt(item, "reviewers"), func(r any) any { return r }))
	view.Set("approvedBy", namesOf(opt(approvals, "approved_by"), func(r any) any { return opt(r, "user") }))
	view.Set("approvalsLeft", numberOrNull(opt(approvals, "approvals_left")))
	view.Set("commits", gitlabCommits(commits))
	view.Set("behind", numberOrNull(opt(diverged, "diverged_commits_count")))
	view.Set("conflicts", truthy(opt(item, "has_conflicts")))
	view.Set("diffRefs", obj("base", orNull(opt(refs, "base_sha")), "head", orNull(opt(refs, "head_sha"))))
}

// gitlabRaw is one file at one commit, or an empty side when GitLab does not give it.
func (s *Service) gitlabRaw(ctx context.Context, a *access, id, path, sha string) files.Side {
	target := s.gitlabBase(a.host) + "/projects/" + id + "/repository/files/" + files.EncodeURIComponent(path) + "/raw?ref=" + files.EncodeURIComponent(sha)
	resp, err := s.fetch(context.WithoutCancel(ctx), target, map[string][]string{"Private-Token": {a.token}}, follow, gitlabTimeout, changeFileMax)
	if err != nil || resp == nil || !resp.ok() || !strings.HasPrefix(resp.url, s.gitlabBase(a.host)+"/") {
		return files.Side{}
	}
	if resp.tooBig || len(resp.body) > changeFileMax {
		return files.Side{Truncated: true}
	}
	if strings.IndexByte(string(resp.body), 0) >= 0 {
		return files.Side{Binary: true}
	}
	return files.Side{Content: files.DecodeUTF8(resp.body)}
}

// ChangeFile is both versions of one file of a GitLab merge request or commit, read at the change's base and head
// commits: oldContent, newContent, truncated, binary. An empty path is an empty side (an added or a deleted file).
// nil when the link is not GitLab's or the token does not reach the change.
func (s *Service) ChangeFile(ctx context.Context, raw, oldPath, newPath string) (*jsvalue.Object, error) {
	ref := s.gitlabRefOf(raw)
	if ref == nil {
		return nil, nil
	}
	token := s.gitlabToken(ref.host)
	if token == "" {
		return nil, nil
	}
	a := &access{host: ref.host, token: token}
	project, err := s.canonicalProject(ctx, a, ref.project)
	if err != nil {
		return nil, err
	}
	if nullish(project) {
		project = ref.project
	}
	id := files.EncodeURIComponent(str(project))
	var base, head string
	if ref.sha != "" {
		item, err := s.api(ctx, a, "/projects/"+id+"/repository/commits/"+ref.sha)
		if err != nil || !truthy(item) {
			return nil, err
		}
		head = textOf(opt(item, "id"))
		if parents, ok := opt(item, "parent_ids").([]any); ok && len(parents) > 0 {
			base = textOf(parents[0])
		}
	} else {
		item, err := s.api(ctx, a, mrItemPath(id, ref.iid))
		if err != nil || !truthy(item) {
			return nil, err
		}
		refs := opt(item, "diff_refs")
		base, head = textOf(opt(refs, "base_sha")), textOf(opt(refs, "head_sha"))
	}
	if head == "" {
		return nil, nil
	}
	var oldSide, newSide files.Side
	var w sync.WaitGroup
	w.Add(2)
	go func() {
		defer w.Done()
		if oldPath != "" && base != "" {
			oldSide = s.gitlabRaw(ctx, a, id, oldPath, base)
		}
	}()
	go func() {
		defer w.Done()
		if newPath != "" {
			newSide = s.gitlabRaw(ctx, a, id, newPath, head)
		}
	}()
	w.Wait()
	truncated := oldSide.Truncated || newSide.Truncated
	binary := oldSide.Binary || newSide.Binary
	oldText, newText := oldSide.Content, newSide.Content
	if truncated || binary {
		oldText, newText = "", ""
	}
	return obj("oldContent", oldText, "newContent", newText, "truncated", truncated, "binary", binary), nil
}
