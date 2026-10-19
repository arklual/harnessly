package gitread

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

const statusTimeout = 5 * time.Second

// ttlCache holds answers per working folder for a while; a missing answer (null) is never served from it.
type ttlCache struct {
	mu sync.Mutex
	m  map[string]cached
}

type cached struct {
	v   any
	exp time.Time
}

func (c *ttlCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.exp) {
		delete(c.m, key)
		return nil, false
	}
	return e.v, true
}

// set stores v; nil drops what was there, as storing null did before.
func (c *ttlCache) set(key string, v any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cached{}
	}
	if v == nil {
		delete(c.m, key)
		return
	}
	c.m[key] = cached{v: v, exp: time.Now().Add(ttl)}
}

const cacheTTL = 15 * time.Second

// branch is the folder's current branch ("HEAD" when detached), nil outside a repository.
func (s *Service) branch(cwd string, force bool) (*string, error) {
	key := "git-branch:" + cwd
	if !force {
		if v, ok := s.cache.get(key); ok {
			return v.(*string), nil
		}
	}
	out, err := run{timeout: statusTimeout}.git("-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		if exitCode(err) == 128 {
			s.cache.set(key, nil, cacheTTL)
			return nil, nil
		}
		return nil, err
	}
	var b *string
	if t := jsTrim(out); t != "" {
		b = &t
	}
	if b == nil {
		s.cache.set(key, nil, cacheTTL)
	} else {
		s.cache.set(key, b, cacheTTL)
	}
	return b, nil
}

// Commit is one of the recent commits of a status.
type Commit struct {
	Hash         string   `json:"hash"`
	ShortHash    string   `json:"shortHash"`
	Author       string   `json:"author"`
	Timestamp    *float64 `json:"timestamp"`
	Subject      string   `json:"subject"`
	IsMerge      bool     `json:"isMerge"`
	FilesChanged int      `json:"filesChanged"`
	Insertions   int      `json:"insertions"`
	Deletions    int      `json:"deletions"`
}

// Status is a working folder's state at a glance.
type Status struct {
	Staged        int      `json:"staged"`
	Modified      int      `json:"modified"`
	Untracked     int      `json:"untracked"`
	Ahead         int      `json:"ahead"`
	Behind        int      `json:"behind"`
	Stash         int      `json:"stash"`
	Insertions    int      `json:"insertions"`
	Deletions     int      `json:"deletions"`
	RecentCommits []Commit `json:"recentCommits"`
}

// parsePorcelain counts `git status --porcelain` lines: untracked, and staged and modified by the two status letters.
func parsePorcelain(out string) (staged, modified, untracked int) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		x := line[0]
		if x == '?' {
			untracked++
			continue
		}
		if x != ' ' {
			staged++
		}
		if len(line) > 1 && line[1] != ' ' && line[1] != '?' {
			modified++
		}
	}
	return
}

// parseAheadBehind reads `rev-list --left-right --count`.
func parseAheadBehind(out string) (ahead, behind int) {
	parts := jsFields(out)
	if len(parts) > 0 {
		ahead = jsIntOr(parts[0], 0)
	}
	if len(parts) > 1 {
		behind = jsIntOr(parts[1], 0)
	}
	return
}

// countLines is the number of lines of trimmed output, 0 for none.
func countLines(out string) int {
	t := jsTrim(out)
	if t == "" {
		return 0
	}
	return strings.Count(t, "\n") + 1
}

// parseNumstat sums `git diff --numstat`, binary files left out.
func parseNumstat(out string) (insertions, deletions int) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		add, del, _ := strings.Cut(line, "\t")
		del, _, _ = strings.Cut(del, "\t")
		if add == "-" || del == "-" {
			continue
		}
		insertions += jsIntOr(add, 0)
		deletions += jsIntOr(del, 0)
	}
	return
}

const commitMark = "__COMMIT__"

var shortstatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// parseRecentCommits reads `git log --format=__COMMIT__%H|%an|%at|%P|%s --shortstat`. A block without the four
// separators made the old parser throw, which left the list empty.
func parseRecentCommits(out string) []Commit {
	commits := []Commit{}
	for _, block := range strings.Split(out, commitMark) {
		block = jsTrim(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		header := jsTrim(lines[0])
		stat := ""
		if len(lines) > 1 {
			stat = jsTrim(lines[1])
		}
		f := strings.Split(header, "|")
		if len(f) < 4 {
			return []Commit{}
		}
		c := Commit{
			Hash:      f[0],
			ShortHash: jsPrefix(f[0], 7),
			Author:    f[1],
			Timestamp: jsMillis(f[2]),
			Subject:   strings.Join(f[4:], "|"),
			IsMerge:   len(jsFields(f[3])) > 1,
		}
		if stat != "" {
			if m := shortstatRe.FindStringSubmatch(stat); m != nil {
				c.FilesChanged = jsIntOr(m[1], 0)
				c.Insertions = jsIntOr(m[2], 0)
				c.Deletions = jsIntOr(m[3], 0)
			}
		}
		commits = append(commits, c)
	}
	return commits
}

// status is the folder's status, nil outside a repository.
func (s *Service) status(cwd string, force bool) (*Status, error) {
	key := "git-status:" + cwd
	if !force {
		if v, ok := s.cache.get(key); ok {
			return v.(*Status), nil
		}
	}
	r := run{timeout: statusTimeout}
	out, err := r.git("-C", cwd, "status", "--porcelain")
	if err != nil {
		if exitCode(err) == 128 {
			s.cache.set(key, nil, cacheTTL)
			return nil, nil
		}
		return nil, err
	}
	st := &Status{}
	st.Staged, st.Modified, st.Untracked = parsePorcelain(out)
	var wg sync.WaitGroup
	wg.Go(func() {
		if out, err := r.git("-C", cwd, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
			st.Ahead, st.Behind = parseAheadBehind(out)
		}
	})
	wg.Go(func() {
		if out, err := r.git("-C", cwd, "stash", "list"); err == nil {
			st.Stash = countLines(out)
		}
	})
	wg.Go(func() {
		if out, err := r.git("-C", cwd, "diff", "--numstat"); err == nil {
			st.Insertions, st.Deletions = parseNumstat(out)
		}
	})
	wg.Go(func() {
		st.RecentCommits = []Commit{}
		if out, err := r.git("-C", cwd, "log", "-3", "--format="+commitMark+"%H|%an|%at|%P|%s", "--shortstat"); err == nil {
			st.RecentCommits = parseRecentCommits(out)
		}
	})
	wg.Wait()
	s.cache.set(key, st, cacheTTL)
	return st, nil
}
