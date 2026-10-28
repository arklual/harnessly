package main

import (
	"encoding/json"
	"html"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// pageConfig is what the page needs to know about this installation, written into <meta name="hl-config"> when the
// runtime serves index.html (web/app/platform/config.ts reads it). It names services and copy only; no token is in it.
type pageConfig struct {
	ProductName        string            `json:"productName"`
	DesignName         string            `json:"designName"`
	MarkURL            *string           `json:"markUrl"`
	Locale             string            `json:"locale"`
	ArtifactsURL       *string           `json:"artifactsUrl"`
	BoardsURL          *string           `json:"boardsUrl"`
	ReaderURL          *string           `json:"readerUrl"`
	StandDomain        *string           `json:"standDomain"`
	StandSignInHosts   []string          `json:"standSignInHosts"`
	PaneHosts          []string          `json:"paneHosts"`
	People             map[string]string `json:"people"`
	TrackerIssueURL    *string           `json:"trackerIssueUrl"`
	TrackerHosts       []string          `json:"trackerHosts"`
	TaskKeyPattern     string            `json:"taskKeyPattern,omitempty"`
	GitlabHosts        []string          `json:"gitlabHosts"`
	LayoutKey          string            `json:"layoutKey"`
	LaunchDefaultsKey  string            `json:"launchDefaultsKey"`
	SpeedPosthogHost   *string           `json:"speedPosthogHost"`
	SpeedPosthogKey    *string           `json:"speedPosthogKey"`
	Features           map[string]bool   `json:"features"`
	UploadHeaderPrefix string            `json:"uploadHeaderPrefix"`
	GithubHost         string            `json:"githubHost"`
	Copy               map[string]any    `json:"copy,omitempty"`
	// HubURL is the public address of the hub this machine is connected to (RUNTIME_HUB_URL), without a trailing slash;
	// null for a runtime no hub carries. The page links back to the hub and signs out there.
	HubURL *string `json:"hubUrl"`
}

// list is never null in the JSON: the page reads these as arrays.
func list(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func optional(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

// pageFeatures are the optional parts of the page (RUNTIME_WEB_FEATURES, comma separated): chats (an OpenWebUI next to
// the runtime), efficiency (the team report), vault (the password safe), design (the design library tab of the old
// panel). Other names, next-ui among them (the switch to the new interface, which is the only interface now), are ignored.
func pageFeatures(list string) map[string]bool {
	out := map[string]bool{"chats": false, "efficiency": false, "vault": false, "design": false}
	for _, name := range splitList(list) {
		if _, known := out[name]; known {
			out[name] = true
		}
	}
	return out
}

func pageConfigFromEnv() pageConfig {
	docsOrigin := ""
	if host := strings.TrimSpace(os.Getenv("RUNTIME_DOCS_HOST")); host != "" {
		docsOrigin = "https://" + host
	}
	c := pageConfig{
		ProductName:       env("RUNTIME_APP_NAME", "harnessly"),
		DesignName:        env("RUNTIME_DESIGN_NAME", "Sinopia Design"),
		MarkURL:           optional(os.Getenv("RUNTIME_APP_MARK_URL")),
		Locale:            env("RUNTIME_LOCALE", "en"),
		ArtifactsURL:      optional(env("RUNTIME_ARTIFACTS_URL", docsOrigin)),
		BoardsURL:         optional(env("RUNTIME_BOARDS_URL", docsOrigin)),
		StandDomain:       optional(os.Getenv("RUNTIME_STAND_DOMAIN")),
		StandSignInHosts:  list(splitList(os.Getenv("RUNTIME_PREVIEW_SIGN_IN_HOSTS"))),
		PaneHosts:         list(splitList(os.Getenv("RUNTIME_PANE_HOSTS"))),
		People:            people(os.Getenv("RUNTIME_PEOPLE")),
		TrackerHosts:      list(splitList(os.Getenv("RUNTIME_TRACKER_HOSTS"))),
		GitlabHosts:       list(splitList(env("RUNTIME_GITLAB_HOSTS", "gitlab.com"))),
		LayoutKey:         env("RUNTIME_LAYOUT_KEY", "layout"),
		LaunchDefaultsKey: env("RUNTIME_LAUNCH_DEFAULTS_KEY", "launchDefaults"),
		SpeedPosthogHost:  optional(os.Getenv("RUNTIME_SPEED_POSTHOG_HOST")),
		SpeedPosthogKey:   optional(os.Getenv("RUNTIME_SPEED_POSTHOG_KEY")),
		Features:          pageFeatures(os.Getenv("RUNTIME_WEB_FEATURES")),
	}
	c.GithubHost = githubFromEnv().Web
	c.HubURL = optional(strings.TrimRight(strings.TrimSpace(os.Getenv("RUNTIME_HUB_URL")), "/"))
	if prefixes := splitList(env("RUNTIME_UPLOAD_HEADER_PREFIXES", "x-harnessly-")); len(prefixes) > 0 {
		c.UploadHeaderPrefix = prefixes[0]
	}
	if host := strings.TrimSpace(os.Getenv("RUNTIME_PLATFORM_HOST")); host != "" {
		c.ReaderURL = optional("https://" + host + env("RUNTIME_DOCS_READER_PATH", "/agents/docs/") + "{slug}")
	}
	if issue := strings.TrimSpace(os.Getenv("RUNTIME_TRACKER_ISSUE_URL")); issue != "" {
		c.TrackerIssueURL = &issue
	} else if base := strings.TrimRight(strings.TrimSpace(os.Getenv("RUNTIME_TRACKER_URL")), "/"); base != "" {
		c.TrackerIssueURL = optional(base + "/issue/{key}")
	}
	if re := strings.TrimSpace(os.Getenv("RUNTIME_TASK_KEY_RE")); re != "" {
		c.TaskKeyPattern = strings.TrimSuffix(strings.TrimPrefix(re, "^"), "$")
	}
	if path := os.Getenv("RUNTIME_WEB_COPY_FILE"); path != "" {
		if raw, err := os.ReadFile(path); err != nil {
			agent.Log("warn", "web: the copy file is unreadable", agent.F{K: "err", V: err.Error()})
		} else if err := json.Unmarshal(raw, &c.Copy); err != nil {
			agent.Log("warn", "web: the copy file is not JSON", agent.F{K: "err", V: err.Error()})
		}
	}
	return c
}

// pageConfigMeta is the meta tag's escaped content.
func pageConfigMeta(c pageConfig) string {
	raw, err := json.Marshal(c)
	if err != nil {
		return "{}"
	}
	return html.EscapeString(string(raw))
}
