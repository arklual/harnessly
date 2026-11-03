package hubproto

import (
	"net/url"
	"regexp"
	"strings"
)

// mrWebURL is the address of a GitLab merge request: https://host/group/sub/project/-/merge_requests/12.
var mrWebURL = regexp.MustCompile(`https?://([^/\s"'\\]+)/([^\s"'\\?#]+?)/-/merge_requests/(\d+)`)

// MRKey is the one spelling of a merge request the machine's scanner and the hub's merge-request source agree on:
// host (lower case), project path (URL-decoded) and iid, as `gitlab.example.com/group/project/!12`.
func MRKey(host, project, iid string) string {
	if p, err := url.PathUnescape(project); err == nil {
		project = p
	}
	return strings.ToLower(host) + "/" + project + "/!" + iid
}

// MRKeysIn lists the keys of the merge request addresses in text, in order, each once.
func MRKeysIn(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mrWebURL.FindAllStringSubmatchIndex(text, -1) {
		// An address followed by a path (…/merge_requests/12/diffs) names the same request but is a link into it: the
		// scanner of the other product skips these, and so do we.
		if end := m[1]; end < len(text) && text[end] == '/' {
			continue
		}
		k := MRKey(text[m[2]:m[3]], text[m[4]:m[5]], text[m[6]:m[7]])
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// MRKeyOfURL is the key of one merge request address, "" when it is not one.
func MRKeyOfURL(u string) string {
	if k := MRKeysIn(u); len(k) > 0 {
		return k[0]
	}
	return ""
}

// IsMRKey says a string is a key made by MRKey.
func IsMRKey(k string) bool {
	return !strings.Contains(k, "://") && strings.Contains(k, "/!") && len(k) <= 512
}
