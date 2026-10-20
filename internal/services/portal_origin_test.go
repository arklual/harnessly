package services

import "testing"

func TestOriginOf(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:8082":         "http://localhost:8082",
		"http://localhost:8082/a/b?x=1": "http://localhost:8082",
		"https://docs.example.com":      "https://docs.example.com",
		"https://docs.example.com:443":  "https://docs.example.com",
		"https://user:pw@example.com":   "",
		"ftp://example.com":             "",
		"not a url":                     "",
		"":                              "",
	} {
		if got := OriginOf(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// The compose workspace: the documents and the boards on other ports of localhost, over http. Their links get the same
// metadata as links on a documents host; anything else on that machine does not.
func TestLinksOnThePortalOriginsAreRecognised(t *testing.T) {
	s := New(Config{
		GitLabHosts: []string{"gitlab.example.com"}, TrackerURL: "https://tracker.example.com", TrackerHosts: []string{"tracker.example.com"},
		PortalOrigins: []string{"http://localhost:8082", "http://localhost:8081"},
		ReaderHost:    "platform.example.com", ReaderPath: "/agents/docs/",
	})
	for raw, want := range map[string]string{
		"http://localhost:8082/a/plan-1a2b3c":                  "http://localhost:8082/a/plan-1a2b3c",
		"http://localhost:8081/b/flow-4d5e6f?k=abc123#frag":    "http://localhost:8081/b/flow-4d5e6f?k=abc123",
		"https://platform.example.com/agents/docs/plan-1a2b3c": "http://localhost:8082/a/plan-1a2b3c",
	} {
		lt := s.parseLinkTarget(raw)
		if lt == nil || lt.kind != "artifact" || lt.url != want {
			t.Errorf("%s: %+v, want %s", raw, lt, want)
		}
	}
	for _, raw := range []string{
		"https://localhost:8082/a/plan-1a2b3c",
		"http://localhost:9999/a/plan-1a2b3c",
		"http://localhost/a/plan-1a2b3c",
		"http://127.0.0.1:8082/a/plan-1a2b3c",
		"http://user:pw@localhost:8082/a/plan-1a2b3c",
		"http://localhost:8082/a/api",
		"http://localhost:8082/a/plan-1a2b3c/extra",
		"http://localhost:8082/a/plan-1a2b3c?k=bad%20key",
		"http://tracker.example.com/issue/ABC-12",
		"https://tracker.example.com:8443/issue/ABC-12",
		"http://gitlab.example.com/a/b/-/merge_requests/3",
	} {
		if lt := s.parseLinkTarget(raw); lt != nil {
			t.Errorf("%s was accepted: %+v", raw, lt)
		}
	}
	if lt := s.parseLinkTarget("https://tracker.example.com/issue/ABC-12"); lt == nil || lt.kind != "issue" || lt.key != "ABC-12" {
		t.Errorf("the tracker stays plain https: %+v", lt)
	}
	if slug, ok := s.docSlugOf("http://localhost:8081/b/flow-4d5e6f"); !ok || slug != "flow-4d5e6f" {
		t.Errorf("slug: %q %v", slug, ok)
	}
}
