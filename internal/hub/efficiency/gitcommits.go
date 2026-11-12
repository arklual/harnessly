package efficiency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Commit is a commit of a code host; only the head of the message is kept.
type Commit struct {
	Project string    `json:"project"`
	SHA     string    `json:"sha"`
	Title   string    `json:"title"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// SkillCommit is a commit that touched a skill of the skills repository.
type SkillCommit struct {
	SHA   string    `json:"sha"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// CommitHost is a source of commits, next to the merged requests of a CodeHost: the revert commits of every project and
// the commits that changed a skill. A host that cannot list them leaves the interface unimplemented.
type CommitHost interface {
	RevertCommits(ctx context.Context, since time.Time) ([]Commit, error)
	SkillCommits(ctx context.Context, since time.Time) ([]SkillCommit, error)
}

var revertTitle = regexp.MustCompile(`(?i)^\W*revert\b`)

// IsRevertCommit says whether a commit reverts another: a «Revert» title or the line git writes into a revert's message.
func IsRevertCommit(title, message string) bool {
	return revertTitle.MatchString(title) || strings.Contains(message, "This reverts commit")
}

type glCommit struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Message       string   `json:"message"`
	CommittedDate string   `json:"committed_date"`
	ParentIDs     []string `json:"parent_ids"`
}

type glProject struct {
	ID   int    `json:"id"`
	Path string `json:"path_with_namespace"`
}

// list reads every page of a list endpoint into out, which is a pointer to a slice.
func (g *GitLab) list(ctx context.Context, path string, q url.Values, each func(raw json.RawMessage) error) error {
	page := 1
	for n := 0; n < glMaxPages; n++ {
		q.Set("per_page", strconv.Itoa(glPerPage))
		q.Set("page", strconv.Itoa(page))
		body, next, err := g.fetchRaw(ctx, g.URL+"/api/v4"+path+"?"+q.Encode())
		if err != nil {
			return err
		}
		if err := each(body); err != nil {
			return err
		}
		if next == 0 {
			return nil
		}
		page = next
	}
	return fmt.Errorf("%s: more than %d pages", path, glMaxPages)
}

func (g *GitLab) commits(ctx context.Context, project string, since time.Time, path string) ([]glCommit, error) {
	var out []glCommit
	q := url.Values{"since": {since.UTC().Format(time.RFC3339)}}
	if path != "" {
		q.Set("path", path)
	}
	err := g.list(ctx, "/projects/"+url.PathEscape(project)+"/repository/commits", q, func(raw json.RawMessage) error {
		var page []glCommit
		if err := json.Unmarshal(raw, &page); err != nil {
			return fmt.Errorf("GitLab answer is not a list of commits")
		}
		out = append(out, page...)
		return nil
	})
	return out, err
}

// RevertCommits lists the revert commits of every project of the groups since a time. A project that fails does not hold
// back the others; the first error is returned together with what was read.
func (g *GitLab) RevertCommits(ctx context.Context, since time.Time) ([]Commit, error) {
	var out []Commit
	var first error
	for _, group := range g.Groups {
		var projects []glProject
		err := g.list(ctx, "/groups/"+url.PathEscape(group)+"/projects", url.Values{"include_subgroups": {"true"}, "archived": {"false"}, "simple": {"true"}}, func(raw json.RawMessage) error {
			var page []glProject
			if err := json.Unmarshal(raw, &page); err != nil {
				return fmt.Errorf("GitLab answer is not a list of projects")
			}
			projects = append(projects, page...)
			return nil
		})
		if err != nil && first == nil {
			first = err
		}
		for _, p := range projects {
			list, err := g.commits(ctx, strconv.Itoa(p.ID), since, "")
			if err != nil && first == nil {
				first = err
			}
			for _, c := range list {
				if !IsRevertCommit(c.Title, c.Message) {
					continue
				}
				at, err := time.Parse(time.RFC3339, c.CommittedDate)
				if err != nil {
					continue
				}
				out = append(out, Commit{Project: p.Path, SHA: c.ID, Title: clip(c.Title, 500), Message: clip(c.Message, 500), At: at})
			}
		}
	}
	return out, first
}

// SkillCommits lists the commits that touched a SKILL.md of the skills project, merges left out. Nothing is read when no
// skills project is configured.
func (g *GitLab) SkillCommits(ctx context.Context, since time.Time) ([]SkillCommit, error) {
	if g.SkillsProject == "" {
		return nil, nil
	}
	var paths []string
	err := g.list(ctx, "/projects/"+url.PathEscape(g.SkillsProject)+"/repository/tree", url.Values{"recursive": {"true"}}, func(raw json.RawMessage) error {
		var page []struct{ Name, Path string }
		if err := json.Unmarshal(raw, &page); err != nil {
			return fmt.Errorf("GitLab answer is not a list of tree entries")
		}
		for _, e := range page {
			if e.Name == "SKILL.md" {
				paths = append(paths, e.Path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]SkillCommit{}
	var first error
	for _, path := range paths {
		list, err := g.commits(ctx, g.SkillsProject, since, path)
		if err != nil && first == nil {
			first = err
		}
		for _, c := range list {
			if len(c.ParentIDs) >= 2 {
				continue
			}
			at, err := time.Parse(time.RFC3339, c.CommittedDate)
			if err != nil {
				continue
			}
			seen[c.ID] = SkillCommit{SHA: c.ID, Title: clip(c.Title, 500), At: at}
		}
	}
	out := make([]SkillCommit, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	return out, first
}
