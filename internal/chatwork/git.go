package chatwork

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/gitread"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// gitTimeout bounds one git call: a slow disk or a huge repository leaves the record as it was.
const gitTimeout = 10 * time.Second

// gitRun runs git; a variable so that a test can count the calls.
var gitRun = func(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

// place is where a path is in git: the top of its worktree and the top of the repository's main worktree.
type place struct {
	worktree, repo string
}

// locate finds the worktree a path belongs to; false for a path in no repository (or one that does not exist yet: its
// nearest existing folder is asked).
func locate(path string) (place, bool) {
	dir := existingDir(path)
	if dir == "" {
		return place{}, false
	}
	out, err := gitRun(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return place{}, false
	}
	parts := strings.Split(strings.TrimSpace(out), "\n")
	if len(parts) < 2 || parts[0] == "" {
		return place{}, false
	}
	p := place{worktree: filepath.Clean(parts[0]), repo: filepath.Clean(parts[0])}
	if common := filepath.Clean(parts[1]); filepath.Base(common) == ".git" {
		p.repo = filepath.Dir(common)
	}
	return p, true
}

func existingDir(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	p := filepath.Clean(path)
	for i := 0; i < 64; i++ {
		if st, err := os.Stat(p); err == nil {
			if st.IsDir() {
				return p
			}
			return filepath.Dir(p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
	return ""
}

// look reads where a worktree stands now into e: head, branch, uncommitted changes, the commits made since e.Since and
// the origin's address. Base is set at the first look: the parent of the oldest commit the chat made, or the head.
func look(e *Entry) {
	wt := e.Worktree
	if out, err := gitRun(wt, "rev-parse", "HEAD"); err == nil {
		e.Head = strings.TrimSpace(out)
	}
	if out, err := gitRun(wt, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if b := strings.TrimSpace(out); b != "HEAD" {
			e.Branch = b
		}
	}
	if out, err := gitRun(wt, "status", "--porcelain", "--untracked-files=normal"); err == nil {
		e.Dirty = strings.TrimSpace(out) != ""
	}
	if e.Since > 0 {
		if shas, err := gitread.MadeCommits(wt, float64(e.Since)); err == nil && len(shas) > 0 {
			e.Commits = shas[:min(len(shas), hubproto.ChatWorkCommitsMax)]
		}
	}
	if e.Base == "" {
		if len(e.Commits) > 0 {
			if out, err := gitRun(wt, "rev-parse", "--verify", "--quiet", e.Commits[len(e.Commits)-1]+"^"); err == nil {
				e.Base = strings.TrimSpace(out)
			}
		} else {
			e.Base = e.Head
		}
	}
	if out, err := gitRun(wt, "config", "--get", "remote.origin.url"); err == nil {
		e.Remote = cleanRemote(strings.TrimSpace(out))
	}
}

// cleanRemote is a remote's address without a user or a password.
func cleanRemote(raw string) string {
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		return u.String()
	}
	return raw // scp-like git@host:group/project.git: the user is no secret
}

var scpRemote = regexp.MustCompile(`^[\w.-]+@([\w.-]+):(.+)$`)

// RemoteProject is the host and project path of a remote: gitlab.example.com, group/project.
func RemoteProject(remote string) (host, project string) {
	if m := scpRemote.FindStringSubmatch(remote); m != nil {
		return strings.ToLower(m[1]), strings.TrimSuffix(strings.Trim(m[2], "/"), ".git")
	}
	u, err := url.Parse(remote)
	if err != nil || u.Host == "" {
		return "", ""
	}
	return strings.ToLower(u.Hostname()), strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
}

// mrURL finds merge request addresses (GitLab's, and GitHub's pull requests) in a tool's output.
var mrURL = regexp.MustCompile(`https?://[\w.:-]+/([\w./-]+?)/(?:-/merge_requests|pull)/(\d+)`)

// mrOfProject is the first merge request address of text on the remote's project, "" for none.
func mrOfProject(text, remote string) string {
	host, project := RemoteProject(remote)
	if project == "" {
		return ""
	}
	for _, m := range mrURL.FindAllStringSubmatch(text, -1) {
		u, err := url.Parse(m[0])
		if err != nil || !strings.EqualFold(u.Hostname(), host) || !strings.EqualFold(m[1], project) {
			continue
		}
		return m[0]
	}
	return ""
}
