package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestHubLinksOverlayTheMachinesEnvironment(t *testing.T) {
	t.Setenv("RUNTIME_STAND_DOMAIN", "own.example.com")
	t.Setenv("RUNTIME_GITLAB_HOSTS", "gitlab.own.example.com")
	t.Setenv("RUNTIME_TRACKER_URL", "")
	t.Setenv("RUNTIME_TRACKER_HOSTS", "")
	os.Unsetenv("RUNTIME_TRACKER_ISSUE_URL")
	os.Unsetenv("RUNTIME_LINK_TRACKER_URL")
	os.Unsetenv("RUNTIME_LINK_GITLAB_HOSTS")
	dir := t.TempDir()
	h := newHubLinks([]string{dir})
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"rev":1,"links":{"standDomain":"*.preview.example.org","trackerUrl":"https://tracker.example.org/","gitlabHosts":["gitlab.example.org"]}}`)
	if !h.apply() {
		t.Fatal("first links: nothing applied")
	}
	for name, want := range map[string]string{
		"RUNTIME_STAND_DOMAIN":      "preview.example.org",
		"RUNTIME_LINK_TRACKER_URL":  "https://tracker.example.org",
		"RUNTIME_TRACKER_ISSUE_URL": "https://tracker.example.org/issue/{key}",
		"RUNTIME_LINK_GITLAB_HOSTS": "gitlab.example.org",
		// Where the machine's tokens go stays the machine's word.
		"RUNTIME_TRACKER_URL":  "",
		"RUNTIME_GITLAB_HOSTS": "gitlab.own.example.com",
	} {
		if got := os.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := displayTrackerURL(); got != "https://tracker.example.org" {
		t.Errorf("tracker of the links = %q", got)
	}
	if got := displayGitLabHosts(); !slices.Equal(got, []string{"gitlab.own.example.com", "gitlab.example.org"}) {
		t.Errorf("GitLabs of the links = %v", got)
	}
	if got := displayTrackerHosts(); !slices.Equal(got, []string{"tracker.example.org"}) {
		t.Errorf("tracker hosts of the links = %v", got)
	}
	if h.apply() {
		t.Error("the same links applied twice")
	}
	// The organization clears them: the machine's own values come back.
	write(`{"rev":2}`)
	h.apply()
	if os.Getenv("RUNTIME_STAND_DOMAIN") != "own.example.com" {
		t.Errorf("own stand domain not back: %q", os.Getenv("RUNTIME_STAND_DOMAIN"))
	}
	for _, name := range []string{"RUNTIME_LINK_TRACKER_URL", "RUNTIME_TRACKER_ISSUE_URL", "RUNTIME_LINK_GITLAB_HOSTS"} {
		if _, set := os.LookupEnv(name); set {
			t.Errorf("%s the machine never had stays set", name)
		}
	}
}

func TestHubLinksKeepAMachinesOwnTracker(t *testing.T) {
	t.Setenv("RUNTIME_TRACKER_URL", "https://own-tracker.example.com")
	os.Unsetenv("RUNTIME_TRACKER_ISSUE_URL")
	os.Unsetenv("RUNTIME_LINK_TRACKER_URL")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(`{"links":{"trackerUrl":"https://tracker.example.org"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHubLinks([]string{dir})
	h.apply()
	t.Cleanup(func() { os.Unsetenv("RUNTIME_LINK_TRACKER_URL") })
	if os.Getenv("RUNTIME_TRACKER_URL") != "https://own-tracker.example.com" || os.Getenv("RUNTIME_TRACKER_ISSUE_URL") != "" {
		t.Errorf("the organization moved the machine's tracker: %q %q", os.Getenv("RUNTIME_TRACKER_URL"), os.Getenv("RUNTIME_TRACKER_ISSUE_URL"))
	}
	if displayTrackerURL() != "https://own-tracker.example.com" {
		t.Errorf("tasks lead to %q", displayTrackerURL())
	}
}
