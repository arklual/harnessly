package gitread

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CommitMeta describes one commit of the commit diff.
type CommitMeta struct {
	Hash      string   `json:"hash"`
	ShortHash string   `json:"shortHash"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	Author    string   `json:"author"`
	Email     string   `json:"email"`
	Timestamp *float64 `json:"timestamp"`
	Parents   []string `json:"parents"`
}

// CommitDiff is a commit and its diff against its first parent.
type CommitDiff struct {
	Commit CommitMeta `json:"commit"`
	Diff   string     `json:"diff"`
}

var hashRe = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

// parseCommitMeta reads `git log -1 --format=%H<US>%P<US>%an<US>%ae<US>%at<US>%s<US>%b`; false when fields are missing.
func parseCommitMeta(out string) (CommitMeta, bool) {
	parts := strings.Split(strings.TrimSuffix(out, "\n"), field)
	if len(parts) < 7 {
		return CommitMeta{}, false
	}
	parents := jsFields(parts[1])
	if parents == nil {
		parents = []string{}
	}
	return CommitMeta{
		Hash:      parts[0],
		ShortHash: jsPrefix(parts[0], 7),
		Subject:   parts[5],
		Body:      jsTrim(strings.Join(parts[6:], field)),
		Author:    parts[2],
		Email:     parts[3],
		Timestamp: jsMillis(parts[4]),
		Parents:   parents,
	}, true
}

// commitDiff is one commit of the folder's repository with its diff.
func commitDiff(cwd, hash string) (int, any) {
	r := run{timeout: diffTimeout}
	out, err := r.git("-C", cwd, "log", "-1", "--format=%H"+field+"%P"+field+"%an"+field+"%ae"+field+"%at"+field+"%s"+field+"%b", hash)
	if err != nil {
		logError("commit-diff", err)
		return 500, errBody{Error: "Failed to get commit diff"}
	}
	meta, ok := parseCommitMeta(out)
	if !ok {
		return 404, errBody{Error: "Commit not found"}
	}
	args := []string{"-C", cwd, "diff", meta.Hash + "^.." + meta.Hash}
	if len(meta.Parents) == 0 {
		args = []string{"-C", cwd, "show", "--format=", meta.Hash}
	}
	r.maxBuffer = 10 * mib
	d, err := r.git(args...)
	if err != nil {
		logError("commit-diff", err)
		return 500, errBody{Error: "Failed to get commit diff"}
	}
	return 200, CommitDiff{Commit: meta, Diff: d}
}

// LogEntry is one commit of the commit log.
type LogEntry struct {
	Hash      string   `json:"hash"`
	ShortHash string   `json:"shortHash"`
	Parents   []string `json:"parents"`
	Author    string   `json:"author"`
	Email     string   `json:"email"`
	Timestamp *float64 `json:"timestamp"`
	Subject   string   `json:"subject"`
	IsMerge   bool     `json:"isMerge"`
}

// Log is a page of the folder's history; head, branch and upstream only on the first page.
type Log struct {
	IsGitRepo    bool       `json:"isGitRepo"`
	Head         string     `json:"head"`
	Branch       string     `json:"branch"`
	UpstreamHash *string    `json:"upstreamHash"`
	Commits      []LogEntry `json:"commits"`
}

// LogNotRepo is the answer for a folder outside any work tree.
type LogNotRepo struct {
	IsGitRepo bool       `json:"isGitRepo"`
	Commits   []LogEntry `json:"commits"`
}

const record = "\x1e"

var errBadRecord = errors.New("commit record without parents")

// parseLog reads `git log --format=%H<US>%P<US>%an<US>%ae<US>%at<US>%s<RS>`. A record with a single field made the
// old parser throw.
func parseLog(out string) ([]LogEntry, error) {
	entries := []LogEntry{}
	for _, rec := range strings.Split(out, record) {
		rec = jsTrim(rec)
		if rec == "" {
			continue
		}
		f := strings.Split(rec, field)
		if len(f) < 2 {
			return nil, errBadRecord
		}
		at := func(i int) string {
			if i < len(f) {
				return f[i]
			}
			return ""
		}
		parents := jsFields(f[1])
		if parents == nil {
			parents = []string{}
		}
		e := LogEntry{
			Hash:      f[0],
			ShortHash: jsPrefix(f[0], 7),
			Parents:   parents,
			Author:    at(2),
			Email:     at(3),
			Timestamp: jsMillis(at(4)),
			IsMerge:   len(parents) > 1,
		}
		if len(f) > 5 {
			e.Subject = strings.Join(f[5:], field)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// logLimits is the page size and offset the old handler read from its parameters.
func logLimits(limitParam, skipParam string) (limit int, skip string) {
	limit = 50
	if v, ok := jsParseInt(limitParam); ok && v > 0 {
		limit = int(min(v, 500))
	}
	if v, ok := jsParseInt(skipParam); ok && v > 0 {
		skip = jsNumber(v)
	}
	return
}

// commitLog is the folder's history from skip on; skip is "" or a positive number as JavaScript wrote it.
func commitLog(cwd string, limit int, skip string) (int, any) {
	r := run{timeout: statusTimeout}
	if _, err := r.git("-C", cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		return 200, LogNotRepo{Commits: []LogEntry{}}
	}
	args := []string{"-C", cwd, "log", "-" + jsNumber(float64(limit))}
	if skip != "" {
		args = append(args, "--skip="+skip)
	}
	args = append(args, "--format=%H"+field+"%P"+field+"%an"+field+"%ae"+field+"%at"+field+"%s"+record)
	res := Log{IsGitRepo: true}
	var out string
	var err error
	big := r
	big.maxBuffer = 2 * mib
	if skip != "" {
		out, err = big.git(args...)
	} else {
		var wg sync.WaitGroup
		wg.Go(func() { out, err = big.git(args...) })
		wg.Go(func() {
			if o, e := r.git("-C", cwd, "rev-parse", "HEAD"); e == nil {
				res.Head = jsTrim(o)
			}
		})
		wg.Go(func() {
			if o, e := r.git("-C", cwd, "rev-parse", "--abbrev-ref", "HEAD"); e == nil {
				res.Branch = jsTrim(o)
			}
		})
		wg.Go(func() {
			if o, e := r.git("-C", cwd, "rev-parse", "@{upstream}"); e == nil {
				u := jsTrim(o)
				res.UpstreamHash = &u
			}
		})
		wg.Wait()
	}
	if err == nil {
		res.Commits, err = parseLog(out)
	}
	if err != nil {
		logError("git-log", err)
		return 500, errBody{Error: "Failed to get commit log"}
	}
	return 200, res
}

// Changes is one commit's diff for the changes panel, cut at a file boundary when it is too long.
type Changes struct {
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

const maxChangesDiff = 600_000

var shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// commitAt is the diff of a commit that must belong to the repository whose top folder is root; false when it does not.
func commitAt(root, sha string) (Changes, bool) {
	if !strings.HasPrefix(root, "/") || strings.Contains(root, "\x00") || !shaRe.MatchString(sha) {
		return Changes{}, false
	}
	r := run{timeout: 15 * time.Second, maxBuffer: 8 * mib}
	top, err := r.git("-C", root, "rev-parse", "--show-toplevel")
	top = jsTrim(top)
	if err != nil || top == "" || resolve(top) != filepath.Clean(root) {
		return Changes{}, false
	}
	kind, err := r.git("-C", root, "cat-file", "-t", sha)
	if err != nil || jsTrim(kind) != "commit" {
		return Changes{}, false
	}
	d, err := r.git("-C", root, "show", "--no-color", "--no-ext-diff", "--format=", sha)
	if err != nil {
		d = ""
	}
	c := Changes{Diff: d, Truncated: jsLength(d) > maxChangesDiff}
	if c.Truncated {
		cut := jsLastIndexOf(d, "\ndiff --git", maxChangesDiff) + 1
		if cut == 0 {
			cut = maxChangesDiff
		}
		c.Diff = jsPrefix(d, cut)
	}
	return c, true
}

// resolve is Node's path.resolve for one path: relative ones against the runtime's folder.
func resolve(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
