package sources

import (
	"math"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/preview/weburl"
)

// MRMention is a merge request link somebody wrote in the conversation: the agent in its answer or the person in a
// message. What a tool printed (a grep through test fixtures, a log) is not a mention, and neither is a link to a
// server that is not one of the deployment's GitLabs.
type MRMention struct {
	// URL is https://<host>/<project>/-/merge_requests/<iid>, without what follows the number.
	URL       string
	Host      string
	Project   string
	IID       string
	Timestamp float64
}

// Mentions gathers the merge request links of the texts of a conversation, in the order entries are fed.
type Mentions struct {
	Links *Links
	order []string
	byURL map[string]*MRMention
}

// Add takes in the links of the agent's answers and of the person's messages among entries.
func (m *Mentions) Add(entries []*jsvalue.Object) {
	if m.byURL == nil {
		m.byURL = map[string]*MRMention{}
	}
	for _, e := range entries {
		var text string
		switch e.String("type") {
		case "assistant-message":
			md, _ := e.Get("markdown")
			text, _ = md.(string)
		case "user-message":
			v, _ := e.Get("text")
			text, _ = v.(string)
		default:
			continue
		}
		tsRaw, present := e.Get("timestamp")
		ts := toNumber(tsRaw)
		if !present || math.IsNaN(ts) {
			ts = 0
		}
		for _, f := range m.Links.linksOf(text) {
			if f.kind != "mr" {
				continue
			}
			mention, ok := m.Links.mentionOf(f.url)
			if !ok {
				continue
			}
			have := m.byURL[mention.URL]
			if have == nil {
				mention.Timestamp = ts
				m.byURL[mention.URL] = &mention
				m.order = append(m.order, mention.URL)
				continue
			}
			have.Timestamp = math.Max(have.Timestamp, ts)
		}
	}
}

// List is the mentions, the oldest first (the latest mention of a merge request dates it).
func (m *Mentions) List() []MRMention {
	out := make([]MRMention, 0, len(m.order))
	for _, u := range m.order {
		out = append(out, *m.byURL[u])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out
}

// mentionOf is a merge request link of a GitLab of the deployment, normalized; false for anything else.
func (c *Links) mentionOf(raw string) (MRMention, bool) {
	u, ok := weburl.Parse(raw)
	if !ok || u.Scheme != "https" || u.Port != "" || u.Username != "" || u.Password != "" {
		return MRMention{}, false
	}
	host := strings.TrimPrefix(u.Hostname, "www.")
	if !has(c.GitLabHosts, host) {
		return MRMention{}, false
	}
	match := mrPath.FindStringSubmatch(u.Path)
	if match == nil || strings.Contains(match[1], "..") {
		return MRMention{}, false
	}
	return MRMention{URL: "https://" + host + "/" + match[1] + "/-/merge_requests/" + match[2], Host: host, Project: match[1], IID: match[2]}, true
}
