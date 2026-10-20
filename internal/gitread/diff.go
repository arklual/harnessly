package gitread

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"github.com/ly-solutions/harnessly/internal/agent"
	"strings"
	"sync"
	"time"
)

const (
	diffTimeout  = 10 * time.Second
	fetchTimeout = 20 * time.Second
)

// HeadCommit is the commit HEAD points at.
type HeadCommit struct {
	Hash      string   `json:"hash"`
	ShortHash string   `json:"shortHash"`
	Subject   string   `json:"subject"`
	Author    string   `json:"author"`
	Timestamp *float64 `json:"timestamp"`
}

// DiffNotRepo is the answer for a folder outside any work tree.
type DiffNotRepo struct {
	IsGitRepo bool   `json:"isGitRepo"`
	Diff      string `json:"diff"`
	Hash      string `json:"hash"`
}

// DiffHash is the cheap answer: only whether anything changed since the caller's hash.
type DiffHash struct {
	IsGitRepo bool   `json:"isGitRepo"`
	Hash      string `json:"hash"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Fetched   bool   `json:"fetched"`
}

// Diff is the working tree against HEAD, untracked files included, with what the panel shows around it.
type Diff struct {
	IsGitRepo  bool        `json:"isGitRepo"`
	Diff       string      `json:"diff"`
	Hash       string      `json:"hash"`
	Ahead      int         `json:"ahead"`
	Behind     int         `json:"behind"`
	Branch     string      `json:"branch"`
	Upstream   *string     `json:"upstream"`
	IsDetached bool        `json:"isDetached"`
	Stash      int         `json:"stash"`
	HeadCommit *HeadCommit `json:"headCommit"`
	RepoRoot   string      `json:"repoRoot"`
	Fetched    bool        `json:"fetched"`
}

const field = "\x1f"

// all runs the functions side by side and returns the first error, as Promise.all would reject.
func all(fns ...func() error) error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() { errs[i] = fn() })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// diffHash fingerprints the working tree: HEAD, the status with every untracked file, and the size of the change.
func diffHash(r run) (string, error) {
	var head, status, shortstat string
	err := all(
		func() (err error) { head, err = r.git("rev-parse", "HEAD"); return },
		func() (err error) { status, err = r.git("status", "--porcelain", "-uall"); return },
		func() (err error) { shortstat, err = r.git("diff", "HEAD", "--shortstat"); return },
	)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(jsTrim(head) + "\n" + status + "\n" + shortstat))
	return hex.EncodeToString(sum[:])[:16], nil
}

func aheadBehind(r run) (int, int) {
	out, err := r.git("rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return 0, 0
	}
	return parseAheadBehind(out)
}

// parseHeadCommit reads `git log -1 --format=%H<US>%s<US>%an<US>%at`.
func parseHeadCommit(out string) *HeadCommit {
	parts := strings.Split(jsTrim(out), field)
	if len(parts) < 4 {
		return nil
	}
	return &HeadCommit{Hash: parts[0], ShortHash: jsPrefix(parts[0], 7), Subject: parts[1], Author: parts[2], Timestamp: jsMillis(parts[3])}
}

// untrackedPaths are the `??` entries of `git status --porcelain`.
func untrackedPaths(status string) []string {
	var files []string
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "??") {
			files = append(files, jsTrim(jsSliceFrom(line, 3)))
		}
	}
	return files
}

// jsSliceFrom is s.slice(n) for an ASCII prefix of at least n bytes, else what is left after n UTF-16 units.
func jsSliceFrom(s string, n int) string {
	p := jsPrefix(s, n)
	if len(p) >= len(s) {
		return ""
	}
	return s[len(p):]
}

// diff answers the diff panel of a folder; hashOnly leaves out everything but the fingerprint.
func diff(cwd string, hashOnly, fetch bool) (int, any) {
	r := run{dir: cwd, timeout: diffTimeout}
	if _, err := r.git("rev-parse", "--is-inside-work-tree"); err != nil {
		return 200, DiffNotRepo{}
	}
	fetched := false
	if fetch {
		f := r
		f.timeout = fetchTimeout
		if _, err := f.git("fetch", "--prune"); err != nil {
			agent.Log("warn", "silent fetch failed", agent.F{K: "error", V: err.Error()})
		} else {
			fetched = true
		}
	}
	if hashOnly {
		var hash string
		var ahead, behind int
		err := all(
			func() (err error) { hash, err = diffHash(r); return },
			func() error { ahead, behind = aheadBehind(r); return nil },
		)
		if err != nil {
			logError("diff", err)
			return 500, errBody{Error: "Failed to get diff"}
		}
		return 200, DiffHash{IsGitRepo: true, Hash: hash, Ahead: ahead, Behind: behind, Fetched: fetched}
	}

	d := Diff{IsGitRepo: true, Fetched: fetched}
	var status string
	big := r
	big.maxBuffer = 5 * mib
	err := all(
		func() (err error) { d.Diff, err = big.git("diff", "HEAD"); return },
		func() (err error) { status, err = r.git("status", "--porcelain", "-uall"); return },
		func() (err error) { d.Hash, err = diffHash(r); return },
		func() error { d.Ahead, d.Behind = aheadBehind(r); return nil },
		func() error { repoMeta(r, &d); return nil },
		func() error {
			root, err := r.git("rev-parse", "--show-toplevel")
			d.RepoRoot = jsTrim(root)
			return err
		},
	)
	if err != nil {
		logError("diff", err)
		return 500, errBody{Error: "Failed to get diff"}
	}
	var sb strings.Builder
	sb.WriteString(d.Diff)
	for _, file := range untrackedPaths(status) {
		out, err := r.git("diff", "--no-index", "/dev/null", file)
		if err != nil {
			out = stdoutOf(err) // a difference is exit code 1: the diff is in stdout all the same
		}
		sb.WriteString(out)
	}
	d.Diff = sb.String()
	return 200, d
}

// repoMeta fills branch, upstream, stash and the head commit; each part that fails is left empty.
func repoMeta(r run, d *Diff) {
	var wg sync.WaitGroup
	wg.Go(func() {
		out, err := r.git("rev-parse", "--abbrev-ref", "HEAD")
		if err == nil {
			d.Branch = jsTrim(out)
		}
		d.IsDetached = d.Branch == "HEAD"
	})
	wg.Go(func() {
		if out, err := r.git("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
			u := jsTrim(out)
			d.Upstream = &u
		}
	})
	wg.Go(func() {
		if out, err := r.git("stash", "list"); err == nil {
			d.Stash = countLines(out)
		}
	})
	wg.Go(func() {
		if out, err := r.git("log", "-1", "--format=%H"+field+"%s"+field+"%an"+field+"%at"); err == nil {
			d.HeadCommit = parseHeadCommit(out)
		}
	})
	wg.Wait()
}
