// Package github reads issues and pull requests from GitHub (or GitHub Enterprise) for the page: an issue as a task
// card and tab, an issue or a pull request link as a chip. The token stays on the machine.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config names the GitHub the runtime reads.
type Config struct {
	// API is the REST API root: https://api.github.com, or https://<host>/api/v3 for GitHub Enterprise.
	API string
	// Web is the host of the issue and pull request pages: github.com, or the Enterprise host.
	Web string
	// Token is a token with read access to the repositories; public repositories work without one (rate limited).
	Token string
	// Repos are the repositories whose issues the page may show: "owner/repo" or "owner/*"; empty allows any.
	Repos []string
	// Client calls the API; nil is a client with a 10 s timeout.
	Client *http.Client
}

// FromEnv reads RUNTIME_GITHUB_API, RUNTIME_GITHUB_HOST, RUNTIME_GITHUB_TOKEN and RUNTIME_GITHUB_REPOS.
func FromEnv(getenv func(string) string) Config {
	c := Config{
		API:   strings.TrimRight(strings.TrimSpace(getenv("RUNTIME_GITHUB_API")), "/"),
		Web:   strings.TrimSpace(getenv("RUNTIME_GITHUB_HOST")),
		Token: strings.TrimSpace(getenv("RUNTIME_GITHUB_TOKEN")),
	}
	for _, r := range strings.Split(getenv("RUNTIME_GITHUB_REPOS"), ",") {
		if r = strings.TrimSpace(r); r != "" {
			c.Repos = append(c.Repos, r)
		}
	}
	if c.Web == "" {
		c.Web = "github.com"
	}
	if c.API == "" {
		if c.Web == "github.com" {
			c.API = "https://api.github.com"
		} else {
			c.API = "https://" + c.Web + "/api/v3"
		}
	}
	return c
}

// Ref is one issue or pull request.
type Ref struct {
	Owner, Repo string
	Number      int
	// Pull is set for a pull request link (an issue key may name either).
	Pull bool
}

// Key is the ref as the page names it: owner/repo#12.
func (r Ref) Key() string { return fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number) }

var (
	nameRe = `[A-Za-z0-9](?:[A-Za-z0-9._-]{0,99})`
	keyRe  = regexp.MustCompile(`^(` + nameRe + `)/(` + nameRe + `)#([1-9][0-9]{0,8})$`)
	pathRe = regexp.MustCompile(`^/(` + nameRe + `)/(` + nameRe + `)/(issues|pull)/([1-9][0-9]{0,8})/?$`)
)

// ParseKey reads owner/repo#12.
func ParseKey(key string) (Ref, bool) {
	m := keyRe.FindStringSubmatch(key)
	if m == nil {
		return Ref{}, false
	}
	n, _ := strconv.Atoi(m[3])
	return Ref{Owner: m[1], Repo: m[2], Number: n}, true
}

// ParseURL reads https://<web>/owner/repo/issues/12 or /pull/12.
func (c Config) ParseURL(raw string) (Ref, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != c.Web || u.User != nil {
		return Ref{}, false
	}
	m := pathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return Ref{}, false
	}
	n, _ := strconv.Atoi(m[4])
	return Ref{Owner: m[1], Repo: m[2], Number: n, Pull: m[3] == "pull"}, true
}

// Allowed says whether the page may read this repository.
func (c Config) Allowed(r Ref) bool {
	if len(c.Repos) == 0 {
		return true
	}
	full := strings.ToLower(r.Owner + "/" + r.Repo)
	for _, pattern := range c.Repos {
		if ok, _ := path.Match(strings.ToLower(pattern), full); ok {
			return true
		}
	}
	return false
}

// ErrNotFound: no such issue, or the token cannot see it.
var ErrNotFound = errors.New("not found")

// ErrUnavailable: GitHub did not answer, or refused (rate limit, bad token).
var ErrUnavailable = errors.New("unavailable")

func (c Config) get(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.API+target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return ErrUnavailable
	}
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return ErrNotFound
	case res.StatusCode < 200 || res.StatusCode > 299:
		return ErrUnavailable
	}
	return json.Unmarshal(raw, out)
}

type user struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

type issue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	StateReason string    `json:"state_reason"`
	Assignee    *user     `json:"assignee"`
	PullRequest *struct{} `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	UpdatedAt string `json:"updated_at"`
	Comments  int    `json:"comments"`
}

// Person is who an issue is assigned to.
type Person struct {
	Name   string  `json:"name"`
	Avatar *string `json:"avatar"`
}

// Comment is one comment of an issue.
type Comment struct {
	Author string `json:"author"`
	At     *int64 `json:"at"`
	Text   string `json:"text"`
}

// Task is an issue in the page's task shape (the same one the YouTrack tracker answers with).
type Task struct {
	Key         string     `json:"key"`
	Title       string     `json:"title"`
	Project     *string    `json:"project"`
	Type        *string    `json:"type"`
	State       *string    `json:"state"`
	Priority    *string    `json:"priority"`
	Resolved    bool       `json:"resolved"`
	Assignee    *Person    `json:"assignee"`
	Parent      *struct{}  `json:"parent"`
	Children    []struct{} `json:"children"`
	Description string     `json:"description"`
	Comments    []Comment  `json:"comments"`
	Updated     *int64     `json:"updated"`
	URL         string     `json:"url"`
	Provider    string     `json:"provider"`
}

func millis(iso string) *int64 {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

func strp(s string) *string { return &s }

func (c Config) person(u *user) *Person {
	if u == nil {
		return nil
	}
	p := &Person{Name: u.Login}
	if u.AvatarURL != "" {
		p.Avatar = strp(u.AvatarURL)
	}
	return p
}

func stateOf(i issue) string {
	if i.State == "closed" {
		if i.StateReason == "not_planned" {
			return "Closed (not planned)"
		}
		return "Closed"
	}
	return "Open"
}

// Task is one issue (or pull request) with its last comments.
func (c Config) Task(ctx context.Context, r Ref) (Task, error) {
	if !c.Allowed(r) {
		return Task{}, ErrNotFound
	}
	var i issue
	base := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(r.Owner), url.PathEscape(r.Repo), r.Number)
	if err := c.get(ctx, base, &i); err != nil {
		return Task{}, err
	}
	kind := "Issue"
	if i.PullRequest != nil {
		kind = "Pull request"
	}
	t := Task{
		Key: r.Key(), Title: i.Title, Project: strp(r.Owner + "/" + r.Repo), Type: strp(kind), State: strp(stateOf(i)),
		Resolved: i.State == "closed", Assignee: c.person(i.Assignee), Children: []struct{}{}, Description: i.Body,
		Comments: []Comment{}, Updated: millis(i.UpdatedAt), Provider: "github",
		URL: fmt.Sprintf("https://%s/%s/%s/issues/%d", c.Web, r.Owner, r.Repo, r.Number),
	}
	for _, l := range i.Labels {
		if p := strings.ToLower(l.Name); strings.HasPrefix(p, "priority") || strings.HasPrefix(p, "p0") || strings.HasPrefix(p, "p1") || strings.HasPrefix(p, "p2") {
			t.Priority = strp(l.Name)
			break
		}
	}
	if i.Comments > 0 {
		var comments []struct {
			User      *user  `json:"user"`
			Body      string `json:"body"`
			CreatedAt string `json:"created_at"`
		}
		page := (i.Comments + 9) / 10
		if err := c.get(ctx, fmt.Sprintf("%s/comments?per_page=10&page=%d", base, page), &comments); err == nil {
			for _, cm := range comments {
				author := ""
				if cm.User != nil {
					author = cm.User.Login
				}
				t.Comments = append(t.Comments, Comment{Author: author, At: millis(cm.CreatedAt), Text: cm.Body})
			}
		}
	}
	return t, nil
}

// IssueMeta is an issue link's chip.
type IssueMeta struct {
	Kind     string  `json:"kind"`
	Key      string  `json:"key"`
	Title    string  `json:"title"`
	State    *string `json:"state"`
	Resolved bool    `json:"resolved"`
	Assignee *Person `json:"assignee"`
	Type     *string `json:"type"`
}

// PullMeta is a pull request link's chip, in the page's merge request shape.
type PullMeta struct {
	Kind    string `json:"kind"`
	IID     int    `json:"iid"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Project string `json:"project"`
	Draft   bool   `json:"draft"`
}

// LinkMeta is the chip of an issue or pull request link.
func (c Config) LinkMeta(ctx context.Context, r Ref) (any, error) {
	if !c.Allowed(r) {
		return nil, ErrNotFound
	}
	if r.Pull {
		var p struct {
			Title  string `json:"title"`
			State  string `json:"state"`
			Merged bool   `json:"merged"`
			Draft  bool   `json:"draft"`
		}
		if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(r.Owner), url.PathEscape(r.Repo), r.Number), &p); err != nil {
			return nil, err
		}
		state := "opened"
		switch {
		case p.Merged:
			state = "merged"
		case p.State == "closed":
			state = "closed"
		}
		return PullMeta{Kind: "mr", IID: r.Number, Title: p.Title, State: state, Project: r.Owner + "/" + r.Repo, Draft: p.Draft}, nil
	}
	t, err := c.Task(ctx, r)
	if err != nil {
		return nil, err
	}
	return IssueMeta{Kind: "issue", Key: t.Key, Title: t.Title, State: t.State, Resolved: t.Resolved, Assignee: t.Assignee, Type: t.Type}, nil
}
