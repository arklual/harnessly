package main

import (
	"encoding/json"
	"html"
	"testing"
)

func TestPageConfigFromEnv(t *testing.T) {
	for k, v := range map[string]string{
		"RUNTIME_APP_NAME":            "Acme",
		"RUNTIME_LOCALE":              "ru",
		"RUNTIME_DOCS_HOST":           "docs.example.com",
		"RUNTIME_BOARDS_URL":          "https://boards.example.com",
		"RUNTIME_PLATFORM_HOST":       "work.example.com",
		"RUNTIME_TRACKER_URL":         "https://tracker.example.com/",
		"RUNTIME_TASK_KEY_RE":         "^(ABC|DEF)-[0-9]{1,6}$",
		"RUNTIME_GITLAB_HOSTS":        "gitlab.example.com,gitlab.com",
		"RUNTIME_PEOPLE":              "ada=Ada,bob=Bob",
		"RUNTIME_WEB_FEATURES":        "efficiency, vault, next-ui, unknown",
		"RUNTIME_STAND_DOMAIN":        ".preview.example.com",
		"RUNTIME_ARTIFACTS_URL":       "",
		"RUNTIME_SPEED_POSTHOG_HOST":  "",
		"RUNTIME_TRACKER_ISSUE_URL":   "",
		"RUNTIME_WEB_COPY_FILE":       "",
		"RUNTIME_LAYOUT_KEY":          "",
		"RUNTIME_LAUNCH_DEFAULTS_KEY": "",
		"RUNTIME_DESIGN_NAME":         "",
		"RUNTIME_HUB_URL":             "",
	} {
		t.Setenv(k, v)
	}
	c := pageConfigFromEnv()
	if c.ProductName != "Acme" || c.DesignName != "Sinopia Design" || c.Locale != "ru" || c.LayoutKey != "layout" || c.LaunchDefaultsKey != "launchDefaults" {
		t.Fatalf("%+v", c)
	}
	if c.ArtifactsURL == nil || *c.ArtifactsURL != "https://docs.example.com" || c.BoardsURL == nil || *c.BoardsURL != "https://boards.example.com" {
		t.Fatalf("services: %v %v", c.ArtifactsURL, c.BoardsURL)
	}
	if c.ReaderURL == nil || *c.ReaderURL != "https://work.example.com/agents/docs/{slug}" {
		t.Fatalf("reader %v", c.ReaderURL)
	}
	if c.TrackerIssueURL == nil || *c.TrackerIssueURL != "https://tracker.example.com/issue/{key}" || c.TaskKeyPattern != "(ABC|DEF)-[0-9]{1,6}" {
		t.Fatalf("tracker %v %q", c.TrackerIssueURL, c.TaskKeyPattern)
	}
	if !c.Features["efficiency"] || !c.Features["vault"] || c.Features["next-ui"] || c.Features["chats"] || c.Features["design"] || len(c.Features) != 4 {
		t.Fatalf("features %v", c.Features)
	}
	if c.People["ada"] != "Ada" || len(c.GitlabHosts) != 2 || c.SpeedPosthogHost != nil {
		t.Fatalf("%+v", c)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(pageConfigMeta(c))), &back); err != nil || back["productName"] != "Acme" {
		t.Fatalf("meta: %v %v", err, back)
	}
	// No hub: the key is there and null, as the page reads it.
	if v, ok := back["hubUrl"]; !ok || v != nil || c.HubURL != nil {
		t.Fatalf("hubUrl without a hub: %v %v", v, ok)
	}
}

func TestPageConfigHubURL(t *testing.T) {
	for in, want := range map[string]string{"https://hub.example.com": "https://hub.example.com", " https://hub.example.com/ ": "https://hub.example.com",
		"http://127.0.0.1:18540//": "http://127.0.0.1:18540"} {
		t.Setenv("RUNTIME_HUB_URL", in)
		c := pageConfigFromEnv()
		if c.HubURL == nil || *c.HubURL != want {
			t.Errorf("%q: %v", in, c.HubURL)
		}
		var back map[string]any
		if err := json.Unmarshal([]byte(html.UnescapeString(pageConfigMeta(c))), &back); err != nil || back["hubUrl"] != want {
			t.Errorf("%q: meta %v %v", in, err, back["hubUrl"])
		}
	}
	t.Setenv("RUNTIME_HUB_URL", "  ")
	if c := pageConfigFromEnv(); c.HubURL != nil {
		t.Errorf("blank: %v", *c.HubURL)
	}
}
