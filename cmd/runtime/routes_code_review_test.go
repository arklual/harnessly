package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/connections"
)

// A review is written only through a connection the person made: the deployment's GitLab reads, never writes.
func TestReviewConnPrefersThePersonsOwnAndNeverWritesWithTheDeployments(t *testing.T) {
	env := connections.Conn{ID: "env-gitlab", Kind: connections.GitLab, BaseURL: "https://gl.example", Env: true}
	own := connections.Conn{ID: "c1", Kind: connections.GitLab, BaseURL: "https://GL.example/"}
	primary := connections.Conn{ID: "c2", Kind: connections.GitLab, BaseURL: "https://gl.example", Primary: true}
	other := connections.Conn{ID: "c3", Kind: connections.GitLab, BaseURL: "https://other.example"}
	tracker := connections.Conn{ID: "c4", Kind: connections.YouTrack, BaseURL: "https://gl.example"}

	if c, personal, ok := pickReviewConn([]connections.Conn{env, tracker, other}, "gl.example"); !ok || personal || c.ID != "env-gitlab" {
		t.Errorf("only the deployment's: %s %v %v", c.ID, personal, ok)
	}
	if c, personal, ok := pickReviewConn([]connections.Conn{env, own}, "gl.example"); !ok || !personal || c.ID != "c1" {
		t.Errorf("own over the deployment's: %s %v %v", c.ID, personal, ok)
	}
	if c, _, _ := pickReviewConn([]connections.Conn{own, env, primary}, "gl.example"); c.ID != "c2" {
		t.Errorf("the primary of the own: %s", c.ID)
	}
	if _, _, ok := pickReviewConn([]connections.Conn{tracker, other}, "gl.example"); ok {
		t.Error("a host without a GitLab connection")
	}
	gh := connections.Conn{ID: "c5", Kind: connections.GitHub, BaseURL: "https://github.com"}
	if c, personal, ok := pickReviewConn([]connections.Conn{env, gh}, "www.github.com"); !ok || !personal || c.ID != "c5" {
		t.Errorf("the person's GitHub: %s %v %v", c.ID, personal, ok)
	}
}

// A request another site makes in the person's browser, and a POST that is not JSON, are refused before anything runs.
func TestCodeReviewRoutesRefuseCrossSiteAndNonJSONPosts(t *testing.T) {
	d := deps{
		SessionOK:  func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false },
	}
	answer := func(method, target, site, contentType string) int {
		t.Helper()
		r := httptest.NewRequest(method, target, strings.NewReader("{}"))
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		for _, rt := range codeReviewRoutes(d) {
			if rt.Match(r) {
				status, _, _ := rt.Compute(r)
				return status
			}
		}
		t.Fatalf("no route for %s %s", method, target)
		return 0
	}
	if got := answer(http.MethodGet, platformAPI+"/code-review?url=x", "cross-site", ""); got != http.StatusForbidden {
		t.Errorf("a cross-site read: %d", got)
	}
	if got := answer(http.MethodPost, platformAPI+"/code-review/open", "cross-site", "application/json"); got != http.StatusForbidden {
		t.Errorf("a cross-site write: %d", got)
	}
	if got := answer(http.MethodPost, platformAPI+"/code-review/open", "same-origin", "text/plain"); got != http.StatusUnsupportedMediaType {
		t.Errorf("a form post: %d", got)
	}
	if got := answer(http.MethodGet, platformAPI+"/code-review/github", "cross-site", ""); got != http.StatusForbidden {
		t.Errorf("a cross-site read of the pull requests: %d", got)
	}
}
