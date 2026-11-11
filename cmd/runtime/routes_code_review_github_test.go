package main

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestGitHubListConnPrefersThePersonsOwn(t *testing.T) {
	env := connections.Conn{ID: "env-github", Kind: connections.GitHub, Env: true}
	own := connections.Conn{ID: "c1", Kind: connections.GitHub}
	primary := connections.Conn{ID: "c2", Kind: connections.GitHub, Primary: true}
	gl := connections.Conn{ID: "c3", Kind: connections.GitLab, Primary: true}
	for _, c := range []struct {
		list []connections.Conn
		want string
	}{{[]connections.Conn{env, own, primary, gl}, "c2"}, {[]connections.Conn{env, own}, "c1"}, {[]connections.Conn{gl, env}, "env-github"}} {
		if got, ok := githubListConn(c.list); !ok || got.ID != c.want {
			t.Errorf("%v: %s", c.want, got.ID)
		}
	}
	if _, ok := githubListConn([]connections.Conn{gl}); ok {
		t.Error("no GitHub: nothing to list with")
	}
}

func TestAPullRequestIsARowOfTheLists(t *testing.T) {
	at := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	row, ok := pullRow(connections.PullSummary{URL: "https://github.com/acme/app/pull/7", Number: 7, Title: "Fix it",
		Project: "acme/app", Target: "main", Source: "fix", Author: &connections.Person{Name: "Bob"}, Created: at, Updated: at,
		Assignees: []string{}, Reviewers: []string{"Alice"}, Labels: []string{}, Checks: "failed", Asked: true})
	if !ok || row.ID != "#7" || *row.State != "opened" || *row.Flag != wire.FlagReview || *row.Tone != wire.ToneAccent ||
		*row.MR.Pipeline != wire.PipelineFailed || len(row.Me) != 1 || row.Me[0] != wire.RoleReviewer || *row.Author != "Bob" {
		t.Fatalf("%+v", row)
	}
	if err := row.Validate(); err != nil {
		t.Errorf("not a valid row: %v", err)
	}
	mine, _ := pullRow(connections.PullSummary{URL: "https://github.com/acme/app/pull/8", Number: 8, Title: "Mine",
		Mine: true, Draft: true, Assignees: []string{}, Reviewers: []string{}, Labels: []string{}})
	if *mine.Flag != wire.FlagOpen || mine.Me[0] != wire.RoleAuthor || mine.MR.Pipeline != nil {
		t.Errorf("mine %+v", mine)
	}
	if _, ok := pullRow(connections.PullSummary{Number: 9, Title: "no link"}); ok {
		t.Error("a row without a link")
	}
}
