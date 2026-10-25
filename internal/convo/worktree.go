package convo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// worktreeDir is where a conversation's worktrees live inside the repository, and worktreeBranch the prefix of their
// branches. The folder is kept out of `git status` by the repository's own exclude file.
const (
	worktreeDir    = ".worktrees"
	worktreeBranch = "agent/"
	worktreeWait   = 30 * time.Second
)

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// worktreeName is a name for a worktree and its branch out of words: the launch and the model it runs.
func worktreeName(parts ...string) string {
	s := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(strings.Join(parts, "-")), "-"), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		s = "chat"
	}
	return s
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
		}
	case <-time.After(worktreeWait):
		_ = cmd.Process.Kill()
		return "", errors.New("git " + args[0] + ": timed out")
	}
	return strings.TrimSpace(out.String()), nil
}

// makeWorktree adds a git worktree named name, on a new branch off the HEAD of the repository cwd is in, and returns
// the folder cwd corresponds to inside it. A folder that is no repository (or one with no commit yet) is returned as it
// is: nothing to isolate from.
func makeWorktree(cwd, name string) (string, error) {
	top, err := git(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return cwd, nil
	}
	if _, err := git(top, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return cwd, nil
	}
	dir := filepath.Join(top, worktreeDir, name)
	if _, err := git(top, "worktree", "add", "-q", "-b", worktreeBranch+name, dir, "HEAD"); err != nil {
		return "", err
	}
	keepOut(top)
	rel, err := filepath.Rel(top, cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return dir, nil
	}
	return filepath.Join(dir, rel), nil
}

// keepOut adds the worktrees' folder to the repository's own exclude file, so they do not show as untracked. Best effort.
func keepOut(top string) {
	path, err := git(top, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(top, path)
	}
	line := "/" + worktreeDir + "/"
	if have, err := os.ReadFile(path); err == nil && bytes.Contains(have, []byte(line)) {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("\n" + line + "\n")
}
