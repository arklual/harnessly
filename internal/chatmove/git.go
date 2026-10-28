package chatmove

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const gitTimeout = 15 * time.Second

// Check ids and the codes of their refusals, as the page reads them.
const (
	CheckProject = "project"
	CheckCommit  = "commit"
	CheckClean   = "clean"
	CheckAuth    = "auth"
	CheckModel   = "model"

	CodeNoProject    = "project_missing"
	CodeCommit       = "commit_missing"
	CodeDirty        = "worktree_dirty"
	CodeAgent        = "agent_unavailable"
	CodeModel        = "model_unavailable"
	CodeDirtySource  = "dirty_source"
	CodeBusy         = "agent_working"
	CodeNoHistory    = "no_history"
	CodeTooBig       = "too_big"
	CodeChanged      = "changed"
	CodeBadBundle    = "bad_bundle"
	CodeNotFound     = "not_found"
	CodeInternal     = "internal"
	CodeAlreadyMoved = "moved"
)

// Check is one thing a preflight looked at.
type Check struct {
	ID   string `json:"id"`
	OK   bool   `json:"ok"`
	Code string `json:"code,omitempty"`
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
}

var remoteScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://`)

// NormalizeRemote makes the same repository the same key whatever way it was cloned: without scheme, user, port and
// the .git tail, scp-style colons turned into slashes, lower-cased.
func NormalizeRemote(url string) string {
	u := remoteScheme.ReplaceAllString(strings.TrimSpace(url), "")
	slash := strings.Index(u, "/")
	if at := strings.LastIndex(u, "@"); at >= 0 && (slash < 0 || at < slash) {
		u = u[at+1:]
		slash = strings.Index(u, "/")
	}
	if colon := strings.Index(u, ":"); colon >= 0 && (slash < 0 || colon < slash) {
		rest := u[colon+1:]
		port := rest
		if i := strings.Index(rest, "/"); i >= 0 {
			port = rest[:i]
		}
		if port != "" && strings.Trim(port, "0123456789") == "" {
			u = u[:colon] + rest[len(port):] // host:port/path
		} else {
			u = u[:colon] + "/" + rest // scp-style host:path
		}
	}
	u = strings.TrimSuffix(strings.TrimRight(u, "/"), ".git")
	return strings.ToLower(strings.Trim(u, "/"))
}

// Repo is a checkout as the move sees it.
type Repo struct {
	Root    string
	Key     string
	Commit  string
	RelDir  string
	Clean   bool
	IsMain  bool // the main checkout, not a linked worktree
	Project Project
}

// Inspect reads the checkout dir is in; nil when it is in none, or in one with no remote or no commit to name it by.
func Inspect(ctx context.Context, dir string) *Repo {
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return nil
	}
	commit, err := git(ctx, root, "rev-parse", "HEAD")
	if err != nil || !validCommit(commit) {
		return nil
	}
	remote, _ := git(ctx, root, "config", "--get", "remote.origin.url")
	key := NormalizeRemote(remote)
	if key == "" {
		return nil
	}
	rel := ""
	if r, err := filepath.Rel(root, dir); err == nil && r != "." && !strings.HasPrefix(r, "..") {
		rel = filepath.ToSlash(r)
	}
	status, err := git(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal", "--", ".", ":(exclude).worktrees")
	st, derr := os.Stat(filepath.Join(root, ".git"))
	r := &Repo{Root: root, Key: key, Commit: commit, RelDir: rel, Clean: err == nil && status == "", IsMain: derr == nil && st.IsDir()}
	r.Project = Project{RepoKey: key, RelDir: rel, Commit: commit}
	return r
}

func unsafeRel(rel string) bool {
	if rel == "" {
		return false
	}
	if filepath.IsAbs(rel) || strings.Contains(rel, "\\") || strings.ContainsRune(rel, 0) {
		return true
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == "" || part == "." {
			return true
		}
	}
	return false
}

// Find lists the main checkouts of the repository under the roots (the roots themselves, and one and two levels below),
// in a stable order.
func Find(ctx context.Context, roots []string, key string) []*Repo {
	seen := map[string]bool{}
	var out []*Repo
	try := func(dir string) {
		if seen[dir] {
			return
		}
		seen[dir] = true
		if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !st.IsDir() {
			return
		}
		if r := Inspect(ctx, dir); r != nil && r.Key == key && r.IsMain {
			out = append(out, r)
		}
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		try(root)
		first, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range first {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			l1 := filepath.Join(root, e.Name())
			try(l1)
			second, err := os.ReadDir(l1)
			if err != nil {
				continue
			}
			for _, s := range second {
				if s.IsDir() && !strings.HasPrefix(s.Name(), ".") {
					try(filepath.Join(l1, s.Name()))
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out
}

func hasCommit(ctx context.Context, repo, commit string) bool {
	_, err := git(ctx, repo, "cat-file", "-e", commit+"^{commit}")
	return err == nil
}

// Match is the target's answer to where a project is: the checkout to branch a worktree off and what was checked.
type Match struct {
	Repo   *Repo
	Checks []Check
}

// FilesOK: the project is there, at the commit, clean.
func (m Match) FilesOK() bool {
	for _, c := range m.Checks {
		if !c.OK {
			return false
		}
	}
	return len(m.Checks) > 0
}

// Locate looks for the project on this machine: the repository by its key, the commit in it and a clean checkout. A
// move needs all three to bring the files; without any of them it can still go without local files.
func Locate(ctx context.Context, roots []string, p Project) Match {
	cands := Find(ctx, roots, p.RepoKey)
	if len(cands) == 0 {
		return Match{Checks: []Check{{ID: CheckProject, Code: CodeNoProject}}}
	}
	var withCommit []*Repo
	for _, c := range cands {
		if hasCommit(ctx, c.Root, p.Commit) {
			withCommit = append(withCommit, c)
		}
	}
	if len(withCommit) == 0 {
		return Match{Repo: cands[0], Checks: []Check{{ID: CheckProject, OK: true}, {ID: CheckCommit, Code: CodeCommit}}}
	}
	best := withCommit[0]
	for _, c := range withCommit {
		if c.Clean {
			best = c
			break
		}
	}
	checks := []Check{{ID: CheckProject, OK: true}, {ID: CheckCommit, OK: true}, {ID: CheckClean, OK: best.Clean}}
	if !best.Clean {
		checks[2].Code = CodeDirty
	}
	return Match{Repo: best, Checks: checks}
}

var errWorktree = errors.New("chatmove: the worktree was not made")

// AddWorktree makes the worktree of the move off the repository at the commit, on a branch of its own, and returns the
// folder the conversation starts in. Making it twice for the same name returns the same folder.
func AddWorktree(ctx context.Context, repo *Repo, name, commit, relDir string) (string, error) {
	if unsafeRel(relDir) || !validCommit(commit) || strings.ContainsAny(name, "/\\ ") || name == "" {
		return "", errWorktree
	}
	dir := filepath.Join(repo.Root, ".worktrees", name)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return withRel(dir, relDir), nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", errors.Join(errWorktree, err)
	}
	if _, err := git(ctx, repo.Root, "worktree", "add", "-b", "agent/"+name, dir, commit); err != nil {
		return "", errors.Join(errWorktree, err)
	}
	return withRel(dir, relDir), nil
}

func withRel(dir, rel string) string {
	if rel == "" {
		return dir
	}
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return p
	}
	return dir
}
