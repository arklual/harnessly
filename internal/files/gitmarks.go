package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Git's marks in «Файлы»: for each entry of a folder, whether git does not have it yet ("new") or it has
// changes not committed ("changed"). A folder takes the marks of what is under it: new when all that changed there is
// new, changed otherwise. Outside a repository, or when git does not answer in time, there are no marks.

const gitMarksTime = 2 * time.Second

const (
	GitMarkNew     = "new"
	GitMarkChanged = "changed"
)

// GitMarks is the marks of the entries of dir, by name.
func GitMarks(dir string) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), gitMarksTime)
	defer cancel()
	prefixOut, err := readGit(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil || !trustedGitConfig(ctx, dir) {
		return nil
	}
	prefix := strings.TrimSpace(string(prefixOut))
	out, err := readGit(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=normal", "--ignore-submodules=all", "--", ".")
	if err != nil {
		return nil
	}
	return gitMarksOf(out, prefix)
}

// gitExecKeys are the settings through which a repository's own config makes `git status` run a program: a filter
// (its clean command reads a changed file) or another config file brought in. fsmonitor is turned off on every call,
// and status does not go into submodules, whose config is not looked at.
const gitExecKeys = `^(filter|include|includeif)\.|^core\.fsmonitor$`

// trustedGitConfig is whether git may read the repository at dir: such settings come only from the person's own config
// (system or global, like git-lfs), not from the repository's, which whoever made the repository wrote.
func trustedGitConfig(ctx context.Context, dir string) bool {
	out, err := readGit(ctx, dir, "config", "--show-scope", "--name-only", "--get-regexp", gitExecKeys)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return true // nothing of the kind anywhere
	}
	if err != nil {
		return false
	}
	return trustedScopes(out)
}

func trustedScopes(out []byte) bool {
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		scope, _, _ := strings.Cut(line, "\t")
		switch scope {
		case "", "system", "global", "command":
		default:
			return false
		}
	}
	return true
}

// readGit runs git only to read, in a folder that may be anyone's repository: the repository's config may not run a
// program (core.fsmonitor), nothing takes the index's lock, and a git that is late is asked to stop, not killed.
func readGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	safe := []string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "--no-optional-locks", "-C", dir}
	cmd := exec.CommandContext(ctx, "git", append(safe, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

// gitMarksOf reads `git status --porcelain=v1 -z` (paths from the repository's root) for the folder at prefix.
func gitMarksOf(out []byte, prefix string) map[string]string {
	marks := map[string]string{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		x, y, path := f[0], f[1], string(f[3:])
		// A rename or a copy names the old path in the next field.
		if x == 'R' || x == 'C' {
			i++
		}
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rel := strings.TrimPrefix(path, prefix)
		name, deeper, _ := strings.Cut(rel, "/")
		if name == "" {
			continue
		}
		mark := GitMarkChanged
		if (x == '?' && y == '?') || x == 'A' || x == 'R' || x == 'C' {
			mark = GitMarkNew
		}
		// A file gone from the disk marks its folder only: it is not listed.
		if deeper == "" && (x == 'D' || y == 'D') {
			continue
		}
		if was, ok := marks[name]; ok && was != mark {
			mark = GitMarkChanged
		}
		marks[name] = mark
	}
	return marks
}
