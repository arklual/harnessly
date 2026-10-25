package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/messagecounts"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/share"
	"github.com/ly-solutions/harnessly/internal/sources"
	"github.com/ly-solutions/harnessly/internal/stats"
	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/usagereport"
)

// trackerKeys are the tracker's project keys a bare issue key is recognised by: RUNTIME_TRACKER_KEYS, else the
// alternatives of RUNTIME_TASK_KEY_RE when it is the plain ^(KEY|KEY…)- form.
func trackerKeys() []string {
	if v := splitList(os.Getenv("RUNTIME_TRACKER_KEYS")); len(v) > 0 {
		return v
	}
	if m := regexp.MustCompile(`^\^\(([A-Z0-9|]+)\)-`).FindStringSubmatch(os.Getenv("RUNTIME_TASK_KEY_RE")); m != nil {
		return strings.Split(m[1], "|")
	}
	return nil
}

func sourceLinks() *sources.Links {
	return &sources.Links{
		TrackerURL:   strings.TrimRight(os.Getenv("RUNTIME_TRACKER_URL"), "/"),
		TrackerHosts: splitList(os.Getenv("RUNTIME_TRACKER_HOSTS")),
		IssueKeys:    trackerKeys(),
		GitLabHosts:  splitList(os.Getenv("RUNTIME_GITLAB_HOSTS")),
		DocsHost:     os.Getenv("RUNTIME_DOCS_HOST"),
		ReaderHost:   os.Getenv("RUNTIME_PLATFORM_HOST"),
		ReaderPath:   env("RUNTIME_DOCS_READER_PATH", "/agents/docs/"),
	}
}

// apiPathIs is the previous dispatcher's path match: either path form, trailing slashes dropped.
func apiPathIs(r *http.Request, p string) bool {
	path := r.URL.Path
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path == p || path == "/agents"+p
}

// usageRoute is this runtime's usage report (<platform API>/stats), from the agents' transcripts.
func usageRoute(d deps, authorized func(*http.Request) bool) *usagereport.Route {
	host, _ := os.Hostname()
	offset, _ := strconv.ParseFloat(os.Getenv("RUNTIME_USAGE_TZ_OFFSET_MINUTES"), 64)
	collector := usagereport.NewCollector(usagereport.Config{
		ClaudeRoot: filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(d.Home, ".claude")), "projects"),
		CodexRoot:  filepath.Join(env("CODEX_HOME", filepath.Join(d.Home, ".codex")), "sessions"),
		CacheFile:  filepath.Join(d.DataDir, "stats", env("RUNTIME_USAGE_CACHE_FILE", "usage-v2.json")),
		Projects:   &usagereport.Projects{Root: os.Getenv("RUNTIME_PROJECTS_ROOT"), TaskPrefixes: trackerKeys()},
		Clock:      usagereport.Clock{Offset: offset * 60},
		Timezone:   env("RUNTIME_USAGE_TIMEZONE", "UTC"),
		Machine:    host,
	})
	return &usagereport.Route{Collector: collector, Authorized: authorized, SessionOK: d.SessionOK, Claims: d.Claims,
		Owner: os.Getenv("RUNTIME_USAGE_OWNER"), Machine: host}
}

// shareRoute is the conversation snapshots and read-only links; nil while the deployment has not named the share
// headers, token, file prefix and app name (the routes then stay with the previous runtime).
func (c *core) shareRoute(d deps) *share.Route {
	cfg := map[string]string{}
	for _, k := range []string{"RUNTIME_SHARE_BASE_HEADER", "RUNTIME_SHARE_TOKEN_HEADER", "RUNTIME_SHARE_TOKEN_SALT",
		"RUNTIME_SHARE_TOKEN_AUDIENCE", "RUNTIME_SHARE_FILE_PREFIX", "RUNTIME_APP_NAME"} {
		if cfg[k] = os.Getenv(k); cfg[k] == "" {
			return nil
		}
	}
	return &share.Route{
		Store:  &share.Store{DataDir: d.DataDir},
		Page:   share.Page{Brand: cfg["RUNTIME_APP_NAME"], DropHostSuffix: os.Getenv("RUNTIME_SHARE_DROP_HOST"), DropPathPrefix: c.cfg.Timeline.AppBase},
		Token:  share.TokenConfig{Salt: cfg["RUNTIME_SHARE_TOKEN_SALT"], Audience: cfg["RUNTIME_SHARE_TOKEN_AUDIENCE"]},
		Secret: d.Secret,
		Home:   d.Home, EnvHome: os.Getenv("HOME"),
		WorkspaceDir: os.Getenv("RUNTIME_WORKSPACE_DIR"), ProfileHomes: os.Getenv("RUNTIME_PROFILE_HOMES"),
		Environ:         os.Environ,
		ShareBaseHeader: cfg["RUNTIME_SHARE_BASE_HEADER"], ShareTokenHeader: cfg["RUNTIME_SHARE_TOKEN_HEADER"],
		FilePrefix: cfg["RUNTIME_SHARE_FILE_PREFIX"],
		SessionOK:  d.SessionOK, APIRefusal: d.APIRefusal, Claims: d.Claims,
		FindTab:   c.layoutTabByID,
		Headlines: &launch.Headlines{},
	}
}

// shareRoutes: a snapshot is captured from a tab of the layouts, so only while the core runs; the rest reads the
// share files. Reading a published link counts a view, so it never runs in the shadow.
func (c *core) shareRoutes(f *gate.Gate, d deps) []gate.Route {
	sh := c.shareRoute(d)
	if sh == nil {
		return nil
	}
	is := func(kind share.Kind) func(*http.Request) bool {
		return func(r *http.Request) bool { k, _ := share.Match(r, platformAPI); return k == kind }
	}
	withID := func(answer func(*http.Request, string) (int, any, http.Header)) func(*http.Request) (int, any, http.Header) {
		return func(r *http.Request) (int, any, http.Header) {
			_, id := share.Match(r, platformAPI)
			return answer(r, id)
		}
	}
	routes := f.AsRoutes(
		gate.JSONRoute{Slice: "transcripts", Match: func(r *http.Request) bool { return c.manager() != nil && is(share.Snapshots)(r) }, Compute: sh.Snapshots},
		gate.JSONRoute{Slice: "transcripts", Match: is(share.Download), Compute: withID(sh.Download)},
		gate.JSONRoute{Slice: "transcripts", Match: is(share.Preview), Compute: withID(sh.Preview)},
		gate.JSONRoute{Slice: "transcripts", Match: is(share.Shares), Compute: sh.Shares},
		gate.JSONRoute{Slice: "transcripts", Match: is(share.ShareOne), Compute: withID(sh.Share)},
	)
	read := f.AsRoutes(gate.JSONRoute{Slice: "transcripts", Match: is(share.ShareRead), Compute: withID(sh.ShareRead)})
	return append(routes, read[0])
}

// transcriptRoutes are the transcripts slice: answers computed from the agents' conversation files.
func (c *core) transcriptRoutes(f *gate.Gate, d deps) []gate.Route {
	token := cliTokenFirst(d.DataDir, d.TokenHeaders)
	authorized := func(r *http.Request) bool { return token(r) || d.SessionOK(r) }
	src := &sources.Route{Links: sourceLinks(), Home: d.Home, Authorized: authorized, SessionOK: d.SessionOK,
		Codex: transcript.CodexOptions{ImageURL: transcript.NewCodexImageURL(c.cfg.Timeline.UploadsDir, c.cfg.Timeline.AppBase)}}
	counts := &messagecounts.Route{Home: d.Home, Authorized: authorized}
	usage := usageRoute(d, authorized)
	pages := &stats.Route{Home: d.Home, DataDir: d.DataDir, ConfigFile: d.ConfigFile, Authorized: authorized,
		CwdRewrites: stats.ParseCwdRewrites(os.Getenv("RUNTIME_STATS_CWD_REWRITES")), ShellPath: preflight.New(d.Home).ShellPath}
	var routes []gate.JSONRoute
	for name, answer := range pages.Routes() {
		routes = append(routes, gate.JSONRoute{
			Slice:   "transcripts",
			Match:   func(r *http.Request) bool { return apiPathIs(r, "/api/stats/"+name) },
			Compute: answer,
		})
	}
	return append(c.shareRoutes(f, d), f.AsRoutes(append(routes,
		gate.JSONRoute{
			Slice:   "transcripts",
			Match:   func(r *http.Request) bool { return apiPathIs(r, platformAPI+"/sources") },
			Compute: src.Answer,
		},
		gate.JSONRoute{
			Slice:   "transcripts",
			Match:   func(r *http.Request) bool { return apiPathIs(r, "/api/timeline/message-counts") },
			Compute: counts.Answer,
		},
		gate.JSONRoute{
			Slice:   "transcripts",
			Match:   func(r *http.Request) bool { return apiPathIs(r, platformAPI+"/stats") },
			Compute: usage.Answer,
		},
	)...)...)
}
