package services

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const (
	maxPaths     = 300
	maxRepos     = 8
	maxCommits   = 30
	maxReflog    = 500
	sessionHead  = 256 * 1024
	gitTimeout   = 15 * time.Second
	gitMaxOutput = 8 << 20
	maxDateMs    = 8.64e15
)

var errGit = errors.New("git failed")

// git runs git -C dir with the previous runtime's limits; output past 8 MB fails like any other error.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &limitedBuffer{b: &out, max: gitMaxOutput}
	if err := cmd.Run(); err != nil || out.Len() > gitMaxOutput {
		return "", errGit
	}
	return out.String(), nil
}

type limitedBuffer struct {
	b   *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.b.Len() > l.max {
		return 0, errGit
	}
	l.b.Write(p)
	return len(p), nil
}

// allowedJsonl: a transcript of Claude Code or Codex under the runtime user's home.
func (s *Service) allowedJsonl(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil || !strings.HasSuffix(abs, ".jsonl") {
		return false
	}
	return strings.HasPrefix(abs, filepath.Join(s.cfg.Home, ".claude", "projects")+"/") ||
		strings.HasPrefix(abs, filepath.Join(s.cfg.Home, ".codex", "sessions")+"/")
}

// sessionStart is the first timestamp in a transcript's first 256 KiB; ok is false without one.
func (s *Service) sessionStart(jsonlPath any) (float64, bool) {
	p, isString := jsonlPath.(string)
	if !isString || p == "" || strings.Contains(p, "\x00") || !s.allowedJsonl(p) {
		return 0, false
	}
	f, err := os.Open(p)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	buf := make([]byte, sessionHead)
	n, _ := readFull(f, buf)
	text := files.DecodeUTF8(buf[:n])
	if n == sessionHead {
		text = text[:strings.LastIndexByte(text, '\n')+1]
	}
	return parseSessionStart(text)
}

func readFull(f *os.File, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := f.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
		if m == 0 {
			break
		}
	}
	return n, nil
}

// parseSessionStart: Claude and Codex transcripts both carry a timestamp on each line.
func parseSessionStart(text string) (float64, bool) {
	for _, line := range strings.Split(text, "\n") {
		if jscompat.Trim(line) == "" {
			continue
		}
		v, err := jsvalue.Parse([]byte(line))
		if err != nil || v == nil {
			continue
		}
		var t float64 = math.NaN()
		switch ts := opt(v, "timestamp").(type) {
		case string:
			t = jscompat.ParseDate(ts)
		case float64:
			t = ts
		}
		if !math.IsNaN(t) && !math.IsInf(t, 0) && t > 0 {
			return t, true
		}
	}
	return 0, false
}

// jsDirname is path.dirname.
func jsDirname(p string) string {
	if p == "" {
		return "."
	}
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return "/"
	}
	i := strings.LastIndexByte(trimmed, '/')
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "/"
	}
	return strings.TrimRight(trimmed[:i], "/")
}

// repoRoots are the repositories the paths belong to, in the order of their first path, at most eight. A folder
// inside a repository already found, with no repository of its own on the way up to it, belongs to that one: git is
// asked only about the others.
func repoRoots(ctx context.Context, paths []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, p := range paths {
		if !strings.HasPrefix(p, "/") || strings.Contains(p, "\x00") || seen[p] {
			continue
		}
		seen[p] = true
		if unique = append(unique, p); len(unique) == maxPaths {
			break
		}
	}
	cache := map[string]string{}
	var roots []string
	for _, target := range unique {
		dir := target
		if st, err := os.Stat(target); err != nil || !st.IsDir() {
			dir = jsDirname(target)
		}
		root, known := cache[dir]
		if !known {
			root = insideKnown(dir, roots)
			if root == "" {
				if out, err := git(ctx, dir, "rev-parse", "--show-toplevel"); err == nil {
					root = jscompat.Trim(out)
				}
			}
			cache[dir] = root
		}
		if root != "" && !contains(roots, root) {
			roots = append(roots, root)
		}
		if len(roots) >= maxRepos {
			break
		}
	}
	return roots
}

// insideKnown is the known root a folder is in when nothing between them is a repository of its own; "" otherwise.
func insideKnown(dir string, roots []string) string {
	for _, root := range roots {
		if !strings.HasPrefix(dir, root+"/") && dir != root {
			continue
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return ""
		}
		for d := dir; d != root; d = filepath.Dir(d) {
			if st, err := os.Lstat(d); err != nil || st.Mode()&os.ModeSymlink != 0 {
				return ""
			}
			if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil || !errors.Is(err, os.ErrNotExist) {
				return ""
			}
		}
		return root
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var (
	shaRe         = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	reflogCommit  = regexp.MustCompile(`^(?:commit(?: \((?:initial|amend)\))?|cherry-pick|revert|rebase(?: -i)? \((?:pick|reword|edit|squash|fixup|continue)\)):`)
	reflogSeconds = regexp.MustCompile(`@\{(\d+)\}$`)
)

// parseReflogCommits: commits this worktree made since `since`, newest first, from `%H\x1f%gs\x1f%gd` lines.
func parseReflogCommits(stdout string, since float64) []string {
	var shas []string
	for _, line := range strings.Split(stdout, "\n") {
		parts := strings.Split(line, "\x1f")
		sha, subject, selector := parts[0], "", ""
		if len(parts) > 1 {
			subject = parts[1]
		}
		if len(parts) > 2 {
			selector = parts[2]
		}
		m := reflogSeconds.FindStringSubmatch(selector)
		if !shaRe.MatchString(sha) || m == nil {
			continue
		}
		seconds, _ := strconv.ParseFloat(m[1], 64)
		if seconds*1000 < since {
			break
		}
		if reflogCommit.MatchString(subject) && !contains(shas, sha) {
			shas = append(shas, sha)
		}
	}
	return shas
}

// isoString is new Date(ms).toISOString() for a time value in range.
func isoString(ms float64) string {
	s, _ := jscompat.ISOString(math.Trunc(ms))
	return s
}

// madeCommits says whether the repository got commits of this worktree during the session, the way the previous
// runtime listed them (only whether there are any matters here, so their stats are not read).
func madeCommits(ctx context.Context, root string, since float64) bool {
	reflog, _ := git(ctx, root, "reflog", "show", "--format=%H%x1f%gs%x1f%gd", "--date=unix", "-n", strconv.Itoa(maxReflog), "HEAD")
	var shas []string
	if jscompat.Trim(reflog) != "" {
		made := parseReflogCommits(reflog, since)
		if len(made) == 0 {
			return false
		}
		out, _ := git(ctx, root, "rev-list", "--no-merges", "--since="+isoString(since-3_600_000), "-n", "2000", "--branches", "HEAD")
		reachable := map[string]bool{}
		for _, l := range strings.Split(out, "\n") {
			reachable[l] = true
		}
		for _, sha := range made {
			if reachable[sha] {
				shas = append(shas, sha)
			}
		}
	} else {
		out, _ := git(ctx, root, "rev-list", "--no-merges", "--since="+isoString(since), "-n", strconv.Itoa(maxCommits+1), "HEAD")
		for _, l := range strings.Split(out, "\n") {
			if l != "" {
				shas = append(shas, l)
			}
		}
	}
	if len(shas) == 0 {
		return false
	}
	if len(shas) > maxCommits {
		shas = shas[:maxCommits]
	}
	out, _ := git(ctx, root, append([]string{"log", "--no-walk=sorted", "--no-merges", "--format=%x1e%H"}, shas...)...)
	for _, chunk := range strings.Split(out, "\x1e") {
		if shaRe.MatchString(strings.TrimSpace(strings.SplitN(chunk, "\n", 2)[0])) {
			return true
		}
	}
	return false
}
