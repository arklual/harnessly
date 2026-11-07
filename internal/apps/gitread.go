package apps

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// git.read: an app sees the changes and the history of the repository of the chat's folder — what
// `git status`, `git diff` and `git log` say there — without a terminal. Git runs read only: no hook, no fsmonitor,
// no external diff or textconv program of the repository's config (each would run a program the repository names),
// no lock taken, no network. A secret file (.env, keys) and the host's own folders are left out of every answer.

// GitFile is a changed file of `git status`.
type GitFile struct {
	Path string `json:"path"`
	// From is the old path of a renamed file.
	From string `json:"from,omitempty"`
	// Staged and Unstaged are the letters of git status (M, A, D, R, ?…), "" where nothing changed.
	Staged   string `json:"staged,omitempty"`
	Unstaged string `json:"unstaged,omitempty"`
}

// GitStatus is the branch and the changed files of the chat's folder.
type GitStatus struct {
	Branch string    `json:"branch"`
	Ahead  int       `json:"ahead,omitempty"`
	Behind int       `json:"behind,omitempty"`
	Files  []GitFile `json:"files"`
	// Cut: there were more files than an answer holds.
	Cut bool `json:"cut,omitempty"`
}

// GitCommit is a line of `git log`.
type GitCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	At      int64  `json:"at"`
	Subject string `json:"subject"`
}

// GitDiffOptions narrow a diff.
type GitDiffOptions struct {
	// Path is one file or folder of the chat's folder; "" is all of it.
	Path string
	// Staged: what is staged for the next commit; otherwise what is not staged yet.
	Staged bool
	// Ref is a commit to compare the folder with (HEAD~1, a hash); "" compares with the index.
	Ref string
}

const (
	gitTimeout     = 10 * time.Second
	maxGitFiles    = 1000
	maxGitDiff     = 1 << 20
	maxGitLog      = 200
	defaultGitLog  = 30
	maxDiffedFiles = 300
)

// GitScope is the folder git reads in and the host's folders it never shows.
type GitScope struct {
	Workspace string
	Forbidden []string
}

func (g GitScope) files() FileScope { return FileScope{Workspace: g.Workspace, Forbidden: g.Forbidden} }

// git runs a read-only git command in the workspace.
func (g GitScope) git(ctx context.Context, args ...string) ([]byte, error) {
	if g.Workspace == "" || !filepath.IsAbs(g.Workspace) {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "this view is in no chat folder"}
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	base := []string{"-C", g.Workspace,
		"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "diff.external=",
		"-c", "core.pager=cat", "-c", "color.ui=false", "-c", "core.quotePath=false",
		"-c", "protocol.allow=never", "-c", "log.showSignature=false", "-c", "gpg.program=" + os.DevNull,
		"--no-pager"}
	// A filter driver of the repository's config (filter.<name>.clean/process, named by .gitattributes) is a program
	// git runs on a file's content, for status and diff too: every one the config names is emptied.
	filters, err := g.filters(ctx)
	if err != nil {
		return nil, err
	}
	for _, name := range filters {
		for _, key := range []string{"clean", "smudge", "process"} {
			base = append(base, "-c", "filter."+name+"."+key+"=")
		}
		base = append(base, "-c", "filter."+name+".required=false")
	}
	return g.run(ctx, append(base, args...))
}

// filters are the names of the filter drivers the repository's config (and the person's) names.
func (g GitScope) filters(ctx context.Context) ([]string, error) {
	out, err := g.run(ctx, []string{"-C", g.Workspace, "config", "--name-only", "--get-regexp", `^filter\.`})
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil // none
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, key := range strings.Fields(string(out)) {
		rest := strings.TrimPrefix(key, "filter.")
		if i := strings.LastIndex(rest, "."); i > 0 && !seen[rest[:i]] {
			seen[rest[:i]] = true
			names = append(names, rest[:i])
		}
	}
	return names, nil
}

func (g GitScope) run(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_EXTERNAL_DIFF=", "GIT_PAGER=cat",
		"GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(args) > 3 && args[2] == "config" {
			return nil, err
		}
		msg := strings.TrimSpace(errb.String())
		if strings.Contains(msg, "not a git repository") {
			return nil, &BridgeError{Code: CodeInvalidParams, Message: "the chat's folder is not in a git repository"}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, &BridgeError{Code: CodeHostUnavailable, Message: "git took too long"}
		}
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "git refused it", Detail: msg}
	}
	return out.Bytes(), nil
}

// shown says whether a path of the workspace (as git gives it, relative to the workspace) may be in an answer.
func (g GitScope) shown(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "../") || secret(rel) {
		return false
	}
	return !g.files().hostOwn(filepath.Join(g.Workspace, filepath.FromSlash(rel)))
}

// pathspec is the path an app named, clean and inside the workspace; "" names the workspace.
func pathspec(p string) (string, error) {
	if p == "" || p == "." {
		return ".", nil
	}
	return lexical(p)
}

// refOK is a commit name of the kind an app may pass: no option, no range trick.
func refOK(ref string) bool {
	if ref == "" || len(ref) > 100 || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") {
		return false
	}
	for _, r := range ref {
		if !(r == '~' || r == '^' || r == '/' || r == '_' || r == '-' || r == '.' || r == '@' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

// Status is `git status` of the workspace.
func (g GitScope) Status(ctx context.Context) (GitStatus, error) {
	prefix, err := g.git(ctx, "rev-parse", "--show-prefix")
	if err != nil {
		return GitStatus{}, err
	}
	pre := strings.TrimSpace(string(prefix))
	raw, err := g.git(ctx, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=normal", "--ignore-submodules=all", "--", ".")
	if err != nil {
		return GitStatus{}, err
	}
	st := GitStatus{Files: []GitFile{}}
	fields := strings.Split(string(raw), "\x00")
	for i := 0; i < len(fields); i++ {
		line := fields[i]
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			var a, b int
			for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				if f[0] == '+' {
					a = n
				} else {
					b = n
				}
			}
			st.Ahead, st.Behind = a, b
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "u "):
			parts := strings.SplitN(line, " ", 9)
			if strings.HasPrefix(line, "u ") {
				parts = strings.SplitN(line, " ", 11)
			}
			g.addFile(&st, pre, parts[1], parts[len(parts)-1], "")
		case strings.HasPrefix(line, "2 "):
			parts := strings.SplitN(line, " ", 10)
			from := ""
			if i+1 < len(fields) {
				i++
				from = fields[i]
			}
			g.addFile(&st, pre, parts[1], parts[9], from)
		case strings.HasPrefix(line, "? "):
			g.addFile(&st, pre, "?.", strings.TrimPrefix(line, "? "), "")
		}
	}
	return st, nil
}

func (g GitScope) addFile(st *GitStatus, prefix, xy, p, from string) {
	rel, ok := strings.CutPrefix(p, prefix)
	if !ok || !g.shown(rel) {
		return
	}
	if len(st.Files) >= maxGitFiles {
		st.Cut = true
		return
	}
	f := GitFile{Path: rel}
	if from != "" {
		if r, ok := strings.CutPrefix(from, prefix); ok && g.shown(r) {
			f.From = r
		}
	}
	if len(xy) == 2 {
		if xy[0] != '.' {
			f.Staged = string(xy[0])
		}
		if xy[1] != '.' {
			f.Unstaged = string(xy[1])
		}
		if xy == "?." {
			f.Staged, f.Unstaged = "", "?"
		}
	}
	st.Files = append(st.Files, f)
}

// Diff is the unified diff of the workspace (or of a path in it), secrets left out; cut says it was too long.
func (g GitScope) Diff(ctx context.Context, opt GitDiffOptions) (diff string, cut bool, err error) {
	spec, err := pathspec(opt.Path)
	if err != nil {
		return "", false, err
	}
	which := []string{}
	if opt.Staged {
		which = append(which, "--cached")
	}
	if opt.Ref != "" {
		if !refOK(opt.Ref) {
			return "", false, &BridgeError{Code: CodeInvalidParams, Message: "ref is a commit: a hash, a branch, HEAD~1"}
		}
		which = append(which, opt.Ref)
	}
	names, err := g.git(ctx, append(append([]string{"diff", "--relative", "--name-only", "-z", "--no-ext-diff", "--no-textconv", "--ignore-submodules=all"}, which...), "--", spec)...)
	if err != nil {
		return "", false, err
	}
	files := []string{}
	for _, n := range strings.Split(string(names), "\x00") {
		if n != "" && g.shown(n) {
			files = append(files, n)
		}
	}
	if len(files) == 0 {
		return "", false, nil
	}
	if len(files) > maxDiffedFiles {
		files, cut = files[:maxDiffedFiles], true
	}
	args := append(append([]string{"diff", "--relative", "--no-ext-diff", "--no-textconv", "--no-color", "--ignore-submodules=all"}, which...), "--")
	for _, f := range files {
		args = append(args, ":(literal)"+f)
	}
	raw, err := g.git(ctx, args...)
	if err != nil {
		return "", false, err
	}
	if len(raw) > maxGitDiff {
		raw, cut = raw[:maxGitDiff], true
	}
	return strings.ToValidUTF8(string(raw), "�"), cut, nil
}

// Log is the last commits of the workspace (or of a path in it).
func (g GitScope) Log(ctx context.Context, limit int, p string) ([]GitCommit, error) {
	spec, err := pathspec(p)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultGitLog
	}
	limit = min(limit, maxGitLog)
	raw, err := g.git(ctx, "log", "-z", "--no-color", "--format=%H%x1f%an%x1f%at%x1f%s", "-n", strconv.Itoa(limit), "--", spec)
	if err != nil {
		return nil, err
	}
	out := []GitCommit{}
	for _, rec := range strings.Split(string(raw), "\x00") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) != 4 {
			continue
		}
		at, _ := strconv.ParseInt(f[2], 10, 64)
		out = append(out, GitCommit{Hash: f[0], Author: f[1], At: at * 1000, Subject: f[3]})
	}
	return out, nil
}
