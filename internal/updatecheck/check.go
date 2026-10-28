package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the project's latest published (non-draft, non-pre-release) release.
const DefaultURL = "https://api.github.com/repos/ly-solutions/harnessly/releases/latest"

const (
	// DefaultInterval is the time between checks.
	DefaultInterval = 12 * time.Hour
	// Timeout bounds one request.
	Timeout = 10 * time.Second
	bodyMax = 1 << 20
)

// Status is what the page is told.
type Status struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	URL             string `json:"url,omitempty"`
	PublishedAt     string `json:"publishedAt,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// Checker asks for the latest release and keeps the answer. Set Current.
type Checker struct {
	// Current is the running version, as built in ("dev" or unparsable means there is nothing to compare).
	Current string
	// URL is the latest-release address; empty means DefaultURL.
	URL string
	// Client makes the request; nil means one that honours the proxy environment and times out.
	Client *http.Client
	// Interval is the time between checks; zero means DefaultInterval.
	Interval time.Duration
	// Log gets one line per failed check; may be nil.
	Log func(msg string, err error)

	mu     sync.Mutex
	etag   string
	latest Status
}

// Comparable says whether the running version can be compared with a release at all.
func Comparable(current string) bool {
	_, ok := Parse(current)
	return ok
}

// Status is the last answer; before the first one, or when nothing is newer, UpdateAvailable is false.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.latest
	s.Current = c.Current
	return s
}

// Run checks at once and then every Interval until ctx ends. Start it with go: the first request must not hold up
// anything.
func (c *Checker) Run(ctx context.Context) {
	if !Comparable(c.Current) {
		return
	}
	every := c.Interval
	if every <= 0 {
		every = DefaultInterval
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := c.Check(ctx); err != nil && ctx.Err() == nil && c.Log != nil {
			c.Log("update check failed", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type release struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

// Check makes one request and updates Status. A 304 keeps the previous answer.
func (c *Checker) Check(ctx context.Context) error {
	cur, ok := Parse(c.Current)
	if !ok {
		return errors.New("running version is not a release")
	}
	url := c.URL
	if url == "" {
		url = DefaultURL
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: Timeout, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "harnessly-runtime")
	c.mu.Lock()
	if c.etag != "" {
		req.Header.Set("If-None-Match", c.etag)
	}
	c.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil
	case http.StatusOK:
	default:
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, bodyMax)).Decode(&rel); err != nil {
		return err
	}
	latest, ok := Parse(rel.TagName)
	if !ok || rel.Draft || rel.Prerelease {
		return fmt.Errorf("release %q is not a version", rel.TagName)
	}
	s := Status{Latest: latest.String(), URL: safeURL(rel.HTMLURL), PublishedAt: rel.PublishedAt,
		UpdateAvailable: Compare(latest, cur) > 0}
	c.mu.Lock()
	c.latest, c.etag = s, resp.Header.Get("ETag")
	c.mu.Unlock()
	return nil
}

// safeURL keeps only an https address: the page links to it.
func safeURL(u string) string {
	if strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}
