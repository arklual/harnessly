// Package gitpush reads what `git push` told of the commits it put on a server: the remote's address ("To <url>") and
// each ref it moved ("   a1b2c3d..e4f5a6b  HEAD -> main"). Each becomes the address of the newest commit on its
// server's page (GitLab's /-/commit/<sha>, GitHub's /commit/<sha>), so a pushed commit is a thing the chat made, with
// a card of its own like a merge request. A branch deleted, a tag, or a push refused names none.
package gitpush

import (
	"regexp"
	"strings"
)

var (
	toRe  = regexp.MustCompile(`(?m)^To (https?://\S+|git@[^\s:]+:\S+|ssh://\S+)\s*$`)
	refRe = regexp.MustCompile(`(?m)^\s*[+ ]?\s*(?:[0-9a-f]{7,40}\.\.\.?([0-9a-f]{7,40})|\* \[new branch\])\s+(\S+)\s+->\s+(\S+)`)
	shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// maxCommits bounds the commits one push names.
const maxCommits = 8

// CommitLinks are the addresses of the commits a push's output names, in its order; none when it is no push's.
func CommitLinks(text string) []string {
	if !strings.Contains(text, "->") {
		return nil
	}
	tos := toRe.FindAllStringSubmatchIndex(text, -1)
	if len(tos) == 0 {
		return nil
	}
	var out []string
	for i, to := range tos {
		end := len(text)
		if i+1 < len(tos) {
			end = tos[i+1][0]
		}
		base := webBase(text[to[2]:to[3]])
		if base == "" {
			continue
		}
		for _, m := range refRe.FindAllStringSubmatch(text[to[1]:end], -1) {
			sha := m[1]
			if !shaRe.MatchString(sha) || strings.HasPrefix(m[3], "refs/tags/") {
				continue
			}
			link := base + "/-/commit/" + sha
			if strings.HasPrefix(base, "https://github.com/") {
				link = base + "/commit/" + sha
			}
			out = append(out, link)
			if len(out) == maxCommits {
				return out
			}
		}
	}
	return out
}

// webBase is the web page of a remote: https://host/group/project without .git; git@host:group/project and
// ssh://git@host/group/project become https too. "" for one it cannot tell.
func webBase(remote string) string {
	r := strings.TrimSuffix(strings.TrimSpace(remote), "/")
	r = strings.TrimSuffix(r, ".git")
	switch {
	case strings.HasPrefix(r, "git@"):
		host, path, ok := strings.Cut(strings.TrimPrefix(r, "git@"), ":")
		if !ok || host == "" || path == "" {
			return ""
		}
		return "https://" + host + "/" + path
	case strings.HasPrefix(r, "ssh://"):
		rest := strings.TrimPrefix(r, "ssh://")
		if at := strings.Index(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		host, path, ok := strings.Cut(rest, "/")
		if !ok {
			return ""
		}
		if c := strings.Index(host, ":"); c >= 0 {
			host = host[:c]
		}
		return "https://" + host + "/" + path
	case strings.HasPrefix(r, "https://") || strings.HasPrefix(r, "http://"):
		// A token in the address is never shown.
		if at := strings.Index(r, "@"); at >= 0 && at < strings.Index(r[8:], "/")+8 {
			scheme := r[:strings.Index(r, "://")+3]
			r = scheme + r[at+1:]
		}
		return r
	}
	return ""
}
