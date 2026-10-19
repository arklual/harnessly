package gitread

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

// The changes of a conversation (POST /api/<changes> {paths, jsonlPath}): every git repository the agent's paths
// belong to, with its uncommitted diff and the commits made since the session started (the transcript's first
// timestamp). Same git calls, limits and order as the previous runtime; the independent calls run side by side.

const (
	maxPaths         = 300
	maxRepos         = 8
	maxUntracked     = 40
	maxSessionCommit = 30
	maxReflog        = 500
	sessionHeadBytes = 256 * 1024
	// changesWorkers bound the git processes one request runs at once.
	changesWorkers = 4
)

// CommitSummary is one commit of the session.
type CommitSummary struct {
	Sha       string  `json:"sha"`
	Short     string  `json:"short"`
	Subject   string  `json:"subject"`
	Author    string  `json:"author"`
	Date      float64 `json:"date"`
	Additions float64 `json:"additions"`
	Deletions float64 `json:"deletions"`
	Files     int     `json:"files"`
}

// RepoChanges is one repository's changes.
type RepoChanges struct {
	Root             string          `json:"root"`
	Name             string          `json:"name"`
	Branch           *string         `json:"branch"`
	Diff             string          `json:"diff"`
	Truncated        bool            `json:"truncated"`
	Commits          []CommitSummary `json:"commits"`
	CommitsTruncated bool            `json:"commitsTruncated"`
}

// SessionChangesBody is the answer.
type SessionChangesBody struct {
	Since *float64       `json:"since"`
	Repos []*RepoChanges `json:"repos"`
}

// ErrBadDate is a session start no Date can hold (the previous runtime threw, and its dispatcher answered 500).
var ErrBadDate = errors.New("session start out of the range of a date")

var changesRun = run{timeout: 15 * time.Second, maxBuffer: 8 * mib}

// TranscriptRoots are the folders a transcript may be read from (the agents' conversation folders under the home).
func TranscriptRoots(home string) []string {
	return []string{filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".codex", "sessions")}
}

func allowedTranscript(p string, roots []string) bool {
	abs, err := filepath.Abs(p)
	if err != nil || !strings.HasSuffix(abs, ".jsonl") {
		return false
	}
	for _, r := range roots {
		if strings.HasPrefix(abs, r+"/") {
			return true
		}
	}
	return false
}

// ParseSessionStart is the first timestamp of a transcript (a string date or a number of milliseconds), nil for none.
func ParseSessionStart(text string) *float64 {
	for _, line := range strings.Split(text, "\n") {
		if jscompat.Trim(line) == "" {
			continue
		}
		v, err := jsvalue.Parse([]byte(line))
		if err != nil {
			continue
		}
		o, ok := v.(*jsvalue.Object)
		if !ok {
			continue
		}
		ts, _ := o.Get("timestamp")
		t := math.NaN()
		switch x := ts.(type) {
		case string:
			t = jscompat.ParseDate(x)
		case float64:
			t = x
		}
		if !math.IsNaN(t) && !math.IsInf(t, 0) && t > 0 {
			return &t
		}
	}
	return nil
}

// ReadSessionStart reads the start of a session from the head of its transcript; nil when it is not one of roots'. A
// transcript that opens but cannot be read is an error (the previous runtime threw).
func ReadSessionStart(transcript string, roots []string) (*float64, error) {
	if transcript == "" || strings.Contains(transcript, "\x00") || !allowedTranscript(transcript, roots) {
		return nil, nil
	}
	f, err := os.Open(transcript)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	buf := make([]byte, sessionHeadBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	text := jscompat.DecodeUTF8(buf[:n])
	if n == sessionHeadBytes {
		text = text[:strings.LastIndex(text, "\n")+1]
	}
	return ParseSessionStart(text), nil
}

var reflogCommitRe = regexp.MustCompile(`^(?:commit(?: \((?:initial|amend)\))?|cherry-pick|revert|rebase(?: -i)? \((?:pick|reword|edit|squash|fixup|continue)\)):`)
var reflogSelectorRe = regexp.MustCompile(`@\{(\d+)\}$`)
var numstatLineRe = regexp.MustCompile(`^(\d+|-)\t(\d+|-)\t`)

// parseReflogCommits: the commits this worktree created since `since` (ms), newest first, from the HEAD reflog.
func parseReflogCommits(stdout string, since float64) []string {
	var shas []string
	seen := map[string]bool{}
	for _, line := range strings.Split(stdout, "\n") {
		parts := strings.Split(line, field)
		sha, subject, selector := parts[0], "", ""
		if len(parts) > 1 {
			subject = parts[1]
		}
		if len(parts) > 2 {
			selector = parts[2]
		}
		m := reflogSelectorRe.FindStringSubmatch(selector)
		if !shaRe.MatchString(sha) || m == nil {
			continue
		}
		seconds, _ := strconv.ParseFloat(m[1], 64)
		if math.IsInf(seconds, 0) {
			continue
		}
		if seconds*1000 < since {
			break
		}
		if reflogCommitRe.MatchString(subject) && !seen[sha] {
			seen[sha] = true
			shas = append(shas, sha)
		}
	}
	return shas
}

func jsNum(s string) float64 {
	if s == "-" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// parseCommitLog reads `git log --numstat --format=%x1e%H%x1f%h%x1f%aI%x1f%an%x1f%s`.
func parseCommitLog(stdout string) []CommitSummary {
	out := []CommitSummary{}
	for _, chunk := range strings.Split(stdout, "\x1e") {
		if jscompat.Trim(chunk) == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		h := strings.Split(lines[0], field)
		get := func(i int) string {
			if i < len(h) {
				return h[i]
			}
			return ""
		}
		if get(0) == "" || !shaRe.MatchString(get(0)) {
			continue
		}
		c := CommitSummary{Sha: get(0), Short: get(1), Subject: get(4), Author: get(3)}
		if d := jscompat.ParseDate(get(2)); !math.IsNaN(d) {
			c.Date = d
		}
		for _, line := range lines[1:] {
			m := numstatLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			c.Files++
			c.Additions += jsNum(m[1])
			c.Deletions += jsNum(m[2])
		}
		out = append(out, c)
	}
	return out
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func gitOr(dir, fallback string, args ...string) string {
	out, err := changesRun.git(append([]string{"-C", dir}, args...)...)
	if err != nil {
		return fallback
	}
	return out
}

func sessionCommits(root string, since float64) ([]CommitSummary, bool, error) {
	iso, ok := jscompat.ISOString(since)
	if !ok {
		return nil, false, ErrBadDate
	}
	reflog := gitOr(root, "", "reflog", "show", "--format=%H%x1f%gs%x1f%gd", "--date=unix", "-n", strconv.Itoa(maxReflog), "HEAD")
	var shas []string
	if jscompat.Trim(reflog) != "" {
		made := parseReflogCommits(reflog, since)
		if len(made) == 0 {
			return []CommitSummary{}, false, nil
		}
		hourBefore, ok := jscompat.ISOString(since - 3_600_000)
		if !ok {
			return nil, false, ErrBadDate
		}
		reachable := map[string]bool{}
		for _, l := range strings.Split(gitOr(root, "", "rev-list", "--no-merges", "--since="+hourBefore, "-n", "2000", "--branches", "HEAD"), "\n") {
			reachable[l] = true
		}
		for _, sha := range made {
			if reachable[sha] {
				shas = append(shas, sha)
			}
		}
	} else {
		shas = lines(gitOr(root, "", "rev-list", "--no-merges", "--since="+iso, "-n", strconv.Itoa(maxSessionCommit+1), "HEAD"))
	}
	if len(shas) == 0 {
		return []CommitSummary{}, false, nil
	}
	args := append([]string{"log", "--no-walk=sorted", "--no-merges", "--numstat", "--format=%x1e%H%x1f%h%x1f%aI%x1f%an%x1f%s"}, shas[:min(len(shas), maxSessionCommit)]...)
	return parseCommitLog(gitOr(root, "", args...)), len(shas) > maxSessionCommit, nil
}

// nodeDirname is Node's path.dirname.
func nodeDirname(p string) string {
	if p == "" {
		return "."
	}
	end, matchedSlash := -1, true
	for i := len(p) - 1; i >= 1; i-- {
		if p[i] == '/' {
			if !matchedSlash {
				end = i
				break
			}
		} else {
			matchedSlash = false
		}
	}
	switch {
	case end == -1 && p[0] == '/':
		return "/"
	case end == -1:
		return "."
	case end == 1 && p[0] == '/':
		return "//"
	}
	return p[:end]
}

// parallel runs f for 0..n-1 on at most changesWorkers goroutines.
func parallel(n int, f func(i int)) {
	sem := make(chan struct{}, changesWorkers)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			f(i)
		}()
	}
	wg.Wait()
}

func cutDiff(diff string) string {
	cut := jscompat.LastIndexOf(diff, "\ndiff --git", maxChangesDiff) + 1
	if cut == 0 {
		cut = maxChangesDiff
	}
	return jscompat.Slice(diff, cut)
}

func repoChanges(root string, since *float64) (*RepoChanges, error) {
	repo := &RepoChanges{Root: root, Name: jscompat.Basename(root, ""), Commits: []CommitSummary{}}
	var tracked string
	var untracked []string
	var err error
	parallel(4, func(i int) {
		switch i {
		case 0:
			if out, e := changesRun.git("-C", root, "rev-parse", "--abbrev-ref", "HEAD"); e == nil {
				b := jscompat.Trim(out)
				repo.Branch = &b
			}
		case 1:
			tracked = gitOr(root, "", "diff", "--no-color", "--no-ext-diff", "HEAD")
		case 2:
			untracked = lines(gitOr(root, "", "ls-files", "--others", "--exclude-standard"))
		case 3:
			if since != nil && *since != 0 {
				repo.Commits, repo.CommitsTruncated, err = sessionCommits(root, *since)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	files := untracked[:min(len(untracked), maxUntracked)]
	diffs := make([]string, len(files))
	parallel(len(files), func(i int) {
		out, e := changesRun.git("-C", root, "diff", "--no-color", "--no-index", "--", "/dev/null", files[i])
		if e != nil {
			out = stdoutOf(e)
		}
		diffs[i] = out
	})
	diff := tracked + strings.Join(diffs, "")
	n := jscompat.Len(diff)
	repo.Truncated = n > maxChangesDiff || len(untracked) > maxUntracked
	if n > maxChangesDiff {
		diff = cutDiff(diff)
	}
	repo.Diff = diff
	if repo.Commits == nil {
		repo.Commits = []CommitSummary{}
	}
	return repo, nil
}

// CollectChanges is the uncommitted diff and the session's commits of every repository the paths belong to (at most
// eight, in the order the paths name them), the repositories with changes first, then by name.
func CollectChanges(paths []string, since *float64) ([]*RepoChanges, error) {
	var unique []string
	seen := map[string]bool{}
	for _, p := range paths {
		if strings.HasPrefix(p, "/") && !strings.Contains(p, "\x00") && !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}
	unique = unique[:min(len(unique), maxPaths)]
	dirs := make([]string, len(unique))
	for i, target := range unique {
		if st, err := os.Stat(target); err == nil && st.IsDir() {
			dirs[i] = target
		} else {
			dirs[i] = nodeDirname(target)
		}
	}
	// Each folder's top is asked once; they are asked side by side, then walked in order as the previous runtime did.
	var distinct []string
	tops := map[string]*string{}
	for _, d := range dirs {
		if _, ok := tops[d]; !ok {
			tops[d] = nil
			distinct = append(distinct, d)
		}
	}
	found := make([]*string, len(distinct))
	parallel(len(distinct), func(i int) {
		if out, err := changesRun.git("-C", distinct[i], "rev-parse", "--show-toplevel"); err == nil {
			t := jscompat.Trim(out)
			found[i] = &t
		}
	})
	for i, d := range distinct {
		tops[d] = found[i]
	}
	var roots []string
	for _, d := range dirs {
		if t := tops[d]; t != nil && *t != "" && !contains(roots, *t) {
			roots = append(roots, *t)
		}
		if len(roots) >= maxRepos {
			break
		}
	}
	repos := make([]*RepoChanges, len(roots))
	errs := make([]error, len(roots))
	var wg sync.WaitGroup
	for i, root := range roots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			repos[i], errs[i] = repoChanges(root, since)
		}()
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	idle := func(r *RepoChanges) int {
		if r.Diff == "" && len(r.Commits) == 0 {
			return 1
		}
		return 0
	}
	col := collate.New(language.AmericanEnglish)
	sort.SliceStable(repos, func(i, j int) bool {
		a, b := repos[i], repos[j]
		if idle(a) != idle(b) {
			return idle(a) < idle(b)
		}
		return col.CompareString(a.Name, b.Name) < 0
	})
	return repos, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
