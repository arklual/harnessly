package connections

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// BranchSource reads a branch of a code host without a clone: its commits and its diff against a base, and the merge
// request it is the source of. The work of a chat on a machine that is not on line is read this way.
type BranchSource interface {
	Branch(ctx context.Context, project, base, branch string) (*Branch, error)
}

// Branch is a branch against its base. Files hold each file's patch (a unified diff without the "diff --git" header).
type Branch struct {
	Project   string         `json:"project"`
	Branch    string         `json:"branch"`
	Base      string         `json:"base"`
	Commits   []BranchCommit `json:"commits"`
	Files     []BranchFile   `json:"files"`
	Truncated bool           `json:"truncated"`
	MR        *MergeRequest  `json:"mr"`
}

// BranchCommit is one commit of a branch, newest first.
type BranchCommit struct {
	Sha     string    `json:"sha"`
	Short   string    `json:"short"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date,omitzero"`
	URL     string    `json:"url"`
}

// BranchFile is one changed file.
type BranchFile struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	New     bool   `json:"new"`
	Deleted bool   `json:"deleted"`
	Renamed bool   `json:"renamed"`
	Patch   string `json:"patch"`
}

// GitDiff is the files as one `git diff` text, the form the page's diff list reads.
func (b *Branch) GitDiff() string {
	var s strings.Builder
	for _, f := range b.Files {
		oldP, newP := f.OldPath, f.NewPath
		fmt.Fprintf(&s, "diff --git a/%s b/%s\n", oldP, newP)
		switch {
		case f.New:
			s.WriteString("new file mode 100644\n--- /dev/null\n")
			fmt.Fprintf(&s, "+++ b/%s\n", newP)
		case f.Deleted:
			s.WriteString("deleted file mode 100644\n")
			fmt.Fprintf(&s, "--- a/%s\n+++ /dev/null\n", oldP)
		default:
			if f.Renamed {
				fmt.Fprintf(&s, "rename from %s\nrename to %s\n", oldP, newP)
			}
			fmt.Fprintf(&s, "--- a/%s\n+++ b/%s\n", oldP, newP)
		}
		s.WriteString(f.Patch)
		if f.Patch != "" && !strings.HasSuffix(f.Patch, "\n") {
			s.WriteByte('\n')
		}
	}
	return s.String()
}

const (
	glBranchCommitsMax = 100
	glBranchFilesMax   = 300
)

// Branch implements BranchSource: GitLab's compare of base...branch (base "" is the project's default branch) and the
// newest merge request from the branch.
func (c *glClient) Branch(ctx context.Context, project, base, branch string) (*Branch, error) {
	project = strings.Trim(project, "/")
	if project == "" || branch == "" {
		return nil, ErrNotFound
	}
	id := "projects/" + url.PathEscape(project)
	if base == "" {
		var p struct {
			DefaultBranch string `json:"default_branch"`
		}
		if _, err := c.get(ctx, id, "", &p); err != nil {
			return nil, err
		}
		base = p.DefaultBranch
	}
	var cmp struct {
		Commits []struct {
			ID         string `json:"id"`
			ShortID    string `json:"short_id"`
			Title      string `json:"title"`
			AuthorName string `json:"author_name"`
			CreatedAt  string `json:"created_at"`
			WebURL     string `json:"web_url"`
		} `json:"commits"`
		Diffs []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			NewFile     bool   `json:"new_file"`
			RenamedFile bool   `json:"renamed_file"`
			DeletedFile bool   `json:"deleted_file"`
			Diff        string `json:"diff"`
		} `json:"diffs"`
	}
	q := url.Values{"from": {base}, "to": {branch}, "straight": {"false"}}
	if _, err := c.get(ctx, id+"/repository/compare", q.Encode(), &cmp); err != nil {
		return nil, err
	}
	out := &Branch{Project: project, Branch: branch, Base: base, Commits: []BranchCommit{}, Files: []BranchFile{}}
	// GitLab lists the commits oldest first; the page lists them newest first.
	for i := len(cmp.Commits) - 1; i >= 0 && len(out.Commits) < glBranchCommitsMax; i-- {
		k := cmp.Commits[i]
		out.Commits = append(out.Commits, BranchCommit{Sha: k.ID, Short: k.ShortID, Subject: k.Title, Author: k.AuthorName,
			Date: ParseTime(k.CreatedAt), URL: k.WebURL})
	}
	for i, d := range cmp.Diffs {
		if i == glBranchFilesMax {
			out.Truncated = true
			break
		}
		out.Files = append(out.Files, BranchFile{OldPath: d.OldPath, NewPath: d.NewPath, New: d.NewFile, Deleted: d.DeletedFile,
			Renamed: d.RenamedFile, Patch: d.Diff})
	}
	out.Truncated = out.Truncated || len(cmp.Commits) > glBranchCommitsMax
	var mrs []glMR
	mq := url.Values{"source_branch": {branch}, "state": {"all"}, "order_by": {"updated_at"}, "sort": {"desc"}, "per_page": {"1"}}
	if _, err := c.get(ctx, id+"/merge_requests", mq.Encode(), &mrs); err == nil && len(mrs) > 0 {
		r := mrs[0]
		out.MR = &MergeRequest{IID: r.IID, Title: r.Title, URL: r.WebURL, State: r.State, Draft: r.Draft, Project: project,
			SourceBranch: r.SourceBranch, Updated: ParseTime(r.UpdatedAt), DetailedMergeStatus: r.DetailedMergeStatus}
		if r.HeadPipeline != nil {
			out.MR.Pipeline = &Pipeline{Status: r.HeadPipeline.Status, URL: r.HeadPipeline.WebURL}
		}
	}
	return out, nil
}

// Branches opens a connection's branch source.
func (s *Service) Branches(c Conn) (BranchSource, error) {
	cl, err := s.Open(c)
	if err != nil {
		return nil, err
	}
	src, ok := cl.(BranchSource)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no branches", ErrUnsupported, c.Kind)
	}
	return src, nil
}

// ForHost is the connection whose address is on host (a code host's), the primary one first; false for none.
func (s *Service) ForHost(host string) (Conn, bool) {
	list, err := s.List()
	if err != nil {
		return Conn{}, false
	}
	var found []Conn
	for _, c := range list {
		if u, err := url.Parse(c.BaseURL); err == nil && strings.EqualFold(u.Hostname(), host) {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return Conn{}, false
	}
	for _, c := range found {
		if c.Primary {
			return c, true
		}
	}
	return found[0], true
}
