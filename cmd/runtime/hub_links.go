package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The organization's links: which addresses are feature stands, the tracker, GitLab, the documents and the
// boards. An administrator sets them in the hub; the hub sends them with the environment list (env.set,
// hubproto.EnvSet.Links), the agent keeps them in environment.json of its link folder, and this runtime puts them over
// its own environment. A field the organization left empty keeps the machine's own value, and the machine's value comes
// back when the organization clears it.
//
// The machine's tokens stay with the addresses the machine itself named: the tracker and GitLab of the organization
// only say which links are tasks, merge requests and commits and where a task's key leads (RUNTIME_LINK_TRACKER_URL,
// RUNTIME_LINK_GITLAB_HOSTS, read by displayTrackerURL and displayGitLabHosts), never where the runtime sends
// YOUTRACK_TOKEN or a GitLab token (RUNTIME_TRACKER_URL, RUNTIME_GITLAB_HOSTS stay the machine's).

// hubLinksEvery is how often the links are looked at while the runtime runs.
const hubLinksEvery = 5 * time.Second

// linkEnvNames are the variables the organization's links stand for.
var linkEnvNames = []string{"RUNTIME_STAND_DOMAIN", "RUNTIME_LINK_TRACKER_URL", "RUNTIME_TRACKER_ISSUE_URL",
	"RUNTIME_LINK_GITLAB_HOSTS", "RUNTIME_ARTIFACTS_URL", "RUNTIME_BOARDS_URL", "RUNTIME_DOCS_HOST"}

type hubLinks struct {
	files []string

	mu sync.Mutex
	// own is the machine's environment before any of the organization's word, by name ("" for unset).
	own  map[string]string
	set  map[string]bool
	last string
}

func newHubLinks(linkDirs []string) *hubLinks {
	h := &hubLinks{own: map[string]string{}, set: map[string]bool{}}
	for _, d := range linkDirs {
		h.files = append(h.files, filepath.Join(d, "environment.json"))
	}
	for _, name := range linkEnvNames {
		v, ok := os.LookupEnv(name)
		h.own[name], h.set[name] = v, ok
	}
	return h
}

// read is the links of the first link folder that has any; nil for none.
func (h *hubLinks) read() *hubproto.LinkSettings {
	for _, f := range h.files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var doc struct {
			Links *hubproto.LinkSettings `json:"links"`
		}
		if json.Unmarshal(raw, &doc) == nil && !doc.Links.Empty() {
			return doc.Links
		}
	}
	return nil
}

// linkEnv is the environment the links make: the organization's value of each variable it names, the machine's own
// for the rest.
func (h *hubLinks) linkEnv(l *hubproto.LinkSettings) map[string]string {
	out := map[string]string{}
	for name, v := range h.own {
		out[name] = v
	}
	if l.Empty() {
		return out
	}
	if l.StandDomain != "" {
		out["RUNTIME_STAND_DOMAIN"] = strings.TrimPrefix(l.StandDomain, "*.")
	}
	if l.TrackerURL != "" {
		base := strings.TrimRight(l.TrackerURL, "/")
		out["RUNTIME_LINK_TRACKER_URL"] = base
		// A machine with a tracker of its own keeps its tasks' address: its previews read that tracker.
		if os.Getenv("RUNTIME_TRACKER_URL") == "" && h.own["RUNTIME_TRACKER_ISSUE_URL"] == "" {
			out["RUNTIME_TRACKER_ISSUE_URL"] = base + "/issue/{key}"
		}
	}
	if len(l.GitLabHosts) > 0 {
		out["RUNTIME_LINK_GITLAB_HOSTS"] = strings.Join(l.GitLabHosts, ",")
	}
	if l.ArtifactsURL != "" {
		out["RUNTIME_ARTIFACTS_URL"] = strings.TrimRight(l.ArtifactsURL, "/")
		out["RUNTIME_DOCS_HOST"] = hostOfURL(l.ArtifactsURL)
	}
	if l.BoardsURL != "" {
		out["RUNTIME_BOARDS_URL"] = strings.TrimRight(l.BoardsURL, "/")
	}
	return out
}

// displayTrackerURL is the tracker a task's key leads to: the machine's own, else the organization's.
func displayTrackerURL() string {
	if own := strings.TrimRight(strings.TrimSpace(os.Getenv("RUNTIME_TRACKER_URL")), "/"); own != "" {
		return own
	}
	return strings.TrimRight(strings.TrimSpace(os.Getenv("RUNTIME_LINK_TRACKER_URL")), "/")
}

// displayGitLabHosts are the GitLabs whose links are merge requests and commits: the machine's, then the
// organization's.
func displayGitLabHosts() []string {
	out := splitList(os.Getenv("RUNTIME_GITLAB_HOSTS"))
	for _, h := range splitList(os.Getenv("RUNTIME_LINK_GITLAB_HOSTS")) {
		if !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// displayTrackerHosts are the hosts whose issue links are tasks: the machine's, then the organization's tracker's.
func displayTrackerHosts() []string {
	out := splitList(os.Getenv("RUNTIME_TRACKER_HOSTS"))
	if h := hostOfURL(os.Getenv("RUNTIME_LINK_TRACKER_URL")); h != "" && !slices.Contains(out, h) {
		out = append(out, h)
	}
	return out
}

// gitlabHostsOrPublic is displayGitLabHosts, gitlab.com when neither the machine nor the organization names one.
func gitlabHostsOrPublic() []string {
	if hosts := displayGitLabHosts(); len(hosts) > 0 {
		return hosts
	}
	return []string{"gitlab.com"}
}

func hostOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// apply puts the links over the environment; it says whether anything changed.
func (h *hubLinks) apply() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	want := h.linkEnv(h.read())
	raw, _ := json.Marshal(want)
	if string(raw) == h.last {
		return false
	}
	h.last = string(raw)
	for _, name := range linkEnvNames {
		v := want[name]
		if v == "" && !(h.set[name] && h.own[name] == "") {
			_ = os.Unsetenv(name)
			continue
		}
		_ = os.Setenv(name, v)
	}
	return true
}

// watch applies the links as they change, and says so in the log.
func (h *hubLinks) watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if h.apply() {
				agent.Log("info", "links: the organization's links were applied")
				resetServiceHandlers()
			}
		}
	}
}
