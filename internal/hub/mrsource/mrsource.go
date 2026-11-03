// Package mrsource polls a GitLab for merged merge requests and keeps them in the hub's database, for the KPI of
// docs/hub/analytics.md that join them with the merge requests sessions mentioned.
package mrsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	// Interval is how often the source is asked.
	Interval = 10 * time.Minute
	// Overlap is how far before the newest stored merge a poll looks, so that a request updated late is not missed.
	Overlap = 24 * time.Hour
	// FirstWindow is how far back the first poll looks.
	FirstWindow = 60 * 24 * time.Hour
	perPage     = 100
	maxPages    = 500
	maxBackoff  = time.Hour
)

// Poller reads merged requests of GitLab groups.
type Poller struct {
	URL    string
	Groups []string
	Token  string
	Store  store.Store
	Client *http.Client
	Log    *slog.Logger
	Now    func() time.Time
	// Every is the pause between polls; Interval when zero.
	Every time.Duration
}

// New returns a poller; nil when no source is configured.
func New(rawURL string, groups []string, token string, st store.Store, log *slog.Logger) *Poller {
	if rawURL == "" || len(groups) == 0 || token == "" {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &Poller{URL: strings.TrimRight(rawURL, "/"), Groups: groups, Token: token, Store: st, Log: log,
		Client: &http.Client{Timeout: 30 * time.Second}, Now: time.Now}
}

// Run polls until ctx ends. A failed poll is retried after a growing pause.
func (p *Poller) Run(ctx context.Context) {
	every := p.Every
	if every <= 0 {
		every = Interval
	}
	wait := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
			if wait == 0 {
				wait = every
			}
			wait = min(wait*2, maxBackoff)
			p.Log.Warn("merge request source poll failed", "error", err.Error(), "retry_in", wait.String())
			continue
		}
		wait = every
	}
}

// Poll reads what was merged since the newest stored merge (less the overlap) from every group. Groups that fail do not
// hold back the others; the first error is returned.
func (p *Poller) Poll(ctx context.Context) error {
	now := p.Now()
	newest, err := p.Store.NewestMergedAt(ctx)
	if err != nil {
		return err
	}
	since := now.Add(-FirstWindow)
	if newest > 0 {
		since = time.UnixMilli(newest).Add(-Overlap)
	}
	var first error
	for _, g := range p.Groups {
		if err := p.group(ctx, g, since); err != nil {
			p.Log.Warn("merge request source group failed", "group", g, "error", err.Error())
			if first == nil {
				first = err
			}
		}
	}
	return first
}

type apiMR struct {
	IID       int    `json:"iid"`
	Title     string `json:"title"`
	WebURL    string `json:"web_url"`
	CreatedAt string `json:"created_at"`
	MergedAt  string `json:"merged_at"`
	UpdatedAt string `json:"updated_at"`
	Author    struct {
		Username string `json:"username"`
	} `json:"author"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
}

func (p *Poller) group(ctx context.Context, group string, since time.Time) error {
	page := 1
	for n := 0; n < maxPages; n++ {
		q := url.Values{}
		q.Set("state", "merged")
		q.Set("updated_after", since.UTC().Format(time.RFC3339))
		q.Set("order_by", "updated_at")
		q.Set("sort", "asc")
		q.Set("scope", "all")
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))
		u := p.URL + "/api/v4/groups/" + url.PathEscape(group) + "/merge_requests?" + q.Encode()
		list, next, err := p.fetch(ctx, u)
		if err != nil {
			return err
		}
		rows := make([]store.MergedMR, 0, len(list))
		for _, m := range list {
			if r, ok := toRow(m); ok {
				rows = append(rows, r)
			}
		}
		if err := p.Store.UpsertMergedMRs(ctx, rows); err != nil {
			return err
		}
		if next == 0 {
			return nil
		}
		page = next
	}
	return fmt.Errorf("group %s: more than %d pages", group, maxPages)
}

func toRow(m apiMR) (store.MergedMR, bool) {
	key := hubproto.MRKeyOfURL(m.WebURL)
	merged, err1 := time.Parse(time.RFC3339, m.MergedAt)
	created, err2 := time.Parse(time.RFC3339, m.CreatedAt)
	if key == "" || err1 != nil || err2 != nil {
		return store.MergedMR{}, false
	}
	project := m.References.Full
	if i := strings.LastIndex(project, "!"); i > 0 {
		project = project[:i]
	}
	return store.MergedMR{Key: key, MergedAt: merged.UnixMilli(), CreatedAt: created.UnixMilli(), Title: trim(m.Title, 500),
		Author: trim(m.Author.Username, 200), Project: trim(project, 500), WebURL: trim(m.WebURL, 1000)}, true
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// fetch reads one page; next is the number of the next page, 0 at the last.
func (p *Poller) fetch(ctx context.Context, u string) ([]apiMR, int, error) {
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
		req.Header.Set("PRIVATE-TOKEN", p.Token)
		req.Header.Set("Accept", "application/json")
		resp, err := p.Client.Do(req)
		if err != nil {
			lastErr = p.scrub(err)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = p.scrub(err)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("GitLab answered %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("GitLab answered %d", resp.StatusCode)
		}
		var list []apiMR
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, 0, fmt.Errorf("GitLab answer is not a list of merge requests")
		}
		next, _ := strconv.Atoi(resp.Header.Get("X-Next-Page"))
		return list, next, nil
	}
	return nil, 0, lastErr
}

// scrub keeps the token out of an error text.
func (p *Poller) scrub(err error) error {
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), p.Token, "***"))
}
