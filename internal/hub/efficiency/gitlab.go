package efficiency

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	glPerPage  = 100
	glMaxPages = 500
)

// GitLab is a CodeHost over the GitLab REST API: the merged requests of some groups.
type GitLab struct {
	URL    string
	Groups []string
	Token  string
	Client *http.Client
	// SkillsProject is the project (group/name) whose SKILL.md files are the skills; empty reads no skill commits.
	SkillsProject string
}

// NewGitLab returns the provider; nil when the source is not configured.
func NewGitLab(rawURL string, groups []string, token string) *GitLab {
	if rawURL == "" || len(groups) == 0 || token == "" {
		return nil
	}
	return &GitLab{URL: strings.TrimRight(rawURL, "/"), Groups: groups, Token: token, Client: &http.Client{Timeout: 30 * time.Second}}
}

type glMR struct {
	ID           int    `json:"id"`
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	SourceBranch string `json:"source_branch"`
	CreatedAt    string `json:"created_at"`
	MergedAt     string `json:"merged_at"`
	UserNotes    int    `json:"user_notes_count"`
	Author       struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	} `json:"author"`
	MergedBy *struct {
		Username string `json:"username"`
	} `json:"merged_by"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
}

// MergedMRs lists what was merged in the groups since a time, however the request was created. A group that fails does
// not hold back the others; the first error is returned together with what was read.
func (g *GitLab) MergedMRs(ctx context.Context, since time.Time) ([]MR, error) {
	var out []MR
	var first error
	for _, group := range g.Groups {
		list, err := g.group(ctx, group, since)
		out = append(out, list...)
		if err != nil && first == nil {
			first = err
		}
	}
	return out, first
}

func (g *GitLab) group(ctx context.Context, group string, since time.Time) ([]MR, error) {
	var out []MR
	page := 1
	for n := 0; n < glMaxPages; n++ {
		q := url.Values{}
		q.Set("state", "merged")
		q.Set("scope", "all")
		q.Set("updated_after", since.UTC().Format(time.RFC3339))
		q.Set("order_by", "updated_at")
		q.Set("sort", "asc")
		q.Set("per_page", strconv.Itoa(glPerPage))
		q.Set("page", strconv.Itoa(page))
		list, next, err := g.fetch(ctx, g.URL+"/api/v4/groups/"+url.PathEscape(group)+"/merge_requests?"+q.Encode())
		if err != nil {
			return out, err
		}
		for _, m := range list {
			if mr, ok := m.toMR(); ok {
				out = append(out, mr)
			}
		}
		if next == 0 {
			return out, nil
		}
		page = next
	}
	return out, fmt.Errorf("group %s: more than %d pages", group, glMaxPages)
}

func (m glMR) toMR() (MR, bool) {
	merged, err1 := time.Parse(time.RFC3339, m.MergedAt)
	created, err2 := time.Parse(time.RFC3339, m.CreatedAt)
	if err1 != nil || err2 != nil || m.IID == 0 {
		return MR{}, false
	}
	project := m.References.Full
	if i := strings.LastIndex(project, "!"); i > 0 {
		project = project[:i]
	}
	by := m.Author.Username
	if m.MergedBy != nil && m.MergedBy.Username != "" {
		by = m.MergedBy.Username
	}
	return MR{Project: project, IID: m.IID, Title: clip(m.Title, 500), Branch: clip(m.SourceBranch, 200), Author: m.Author.Username,
		MergedBy: by, CreatedAt: created, MergedAt: merged, ForeignNotes: m.UserNotes > 0}, true
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (g *GitLab) fetch(ctx context.Context, u string) ([]glMR, int, error) {
	body, next, err := g.fetchRaw(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	var list []glMR
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, 0, fmt.Errorf("GitLab answer is not a list of merge requests")
	}
	return list, next, nil
}

// fetchRaw reads one page of any list endpoint, with retries on a busy or failing host.
func (g *GitLab) fetchRaw(ctx context.Context, u string) ([]byte, int, error) {
	var lastErr error
	for try := 0; try < 3; try++ {
		if try > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(time.Duration(try) * 2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("PRIVATE-TOKEN", g.Token)
		req.Header.Set("Accept", "application/json")
		resp, err := g.Client.Do(req)
		if err != nil {
			lastErr = g.scrub(err)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = g.scrub(err)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("GitLab answered %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("GitLab answered %d", resp.StatusCode)
		}
		next, _ := strconv.Atoi(resp.Header.Get("X-Next-Page"))
		return body, next, nil
	}
	return nil, 0, lastErr
}

// scrub keeps the token out of an error text.
func (g *GitLab) scrub(err error) error {
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), g.Token, "***"))
}
