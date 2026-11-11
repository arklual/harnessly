package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
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
	// Channel is the update channel the answer is of (channel.go); empty when the checker was given none.
	Channel string `json:"channel,omitempty"`
	// Note says, for the person, why the channel has no answer (no stable release was named yet, say).
	Note string `json:"note,omitempty"`
}

// Checker asks for the newest release of its channel and keeps the answer. Set Current.
type Checker struct {
	// Current is the running version, as built in ("dev" or unparsable means there is nothing to compare).
	Current string
	// URL is the latest-release address; empty means DefaultURL.
	URL string
	// Channel is ChannelStable or ChannelLatest; empty reads the latest release, as a checker without channels did.
	Channel string
	// StableURL is the stable pointer's address. Empty means DefaultStableURL while URL is the project's own; a
	// mirror named by URL has no pointer until StableURL names it.
	StableURL string
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
	s.Channel = c.Channel
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

// ErrNoStable: the source of releases names no stable one, so an installation on the stable channel stays as it is.
var ErrNoStable = errors.New("no stable release is named")

// Check makes one request and updates Status. A 304 keeps the previous answer.
func (c *Checker) Check(ctx context.Context) error {
	cur, ok := Parse(c.Current)
	if !ok {
		return errors.New("running version is not a release")
	}
	if c.Channel == ChannelStable {
		return c.checkStable(ctx, cur)
	}
	url := c.URL
	if url == "" {
		url = DefaultURL
	}
	body, etag, code, err := c.fetch(ctx, url, "application/vnd.github+json")
	if err != nil {
		return err
	}
	switch code {
	case http.StatusNotModified:
		return nil
	case http.StatusOK:
	default:
		return fmt.Errorf("status %d", code)
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return err
	}
	latest, ok := Parse(rel.TagName)
	if !ok || rel.Draft || rel.Prerelease {
		return fmt.Errorf("release %q is not a version", rel.TagName)
	}
	c.keep(Status{Latest: latest.String(), URL: safeURL(rel.HTMLURL), PublishedAt: rel.PublishedAt,
		UpdateAvailable: Compare(latest, cur) > 0}, etag)
	return nil
}

// checkStable reads the stable pointer. A source without one is not an update and not a silent switch to the latest
// release: the answer says why (Note) and the installation stays on what it runs.
func (c *Checker) checkStable(ctx context.Context, cur Version) error {
	url := c.StableURL
	if url == "" && (c.URL == "" || c.URL == DefaultURL) {
		url = DefaultStableURL
	}
	if url == "" {
		c.keep(Status{Note: "The release mirror of this installation names no stable release: set RUNTIME_UPDATE_STABLE_URL to its stable.json, or RUNTIME_UPDATE_CHANNEL=latest to follow every release."}, "")
		return ErrNoStable
	}
	body, etag, code, err := c.fetch(ctx, url, "application/json")
	if err != nil {
		return err
	}
	switch code {
	case http.StatusNotModified:
		return nil
	case http.StatusOK:
	case http.StatusNotFound:
		c.keep(Status{Note: "No stable release is named at " + shownURL(url) + " yet, so this installation stays on its version. RUNTIME_UPDATE_CHANNEL=latest follows every release."}, "")
		return ErrNoStable
	default:
		return fmt.Errorf("stable pointer: status %d", code)
	}
	p, err := ParsePointer(body)
	if err != nil {
		c.keep(Status{Note: "The stable pointer at " + shownURL(url) + " is not one this version reads, so this installation stays on its version."}, "")
		return err
	}
	stable, _ := Parse(p.Version)
	c.keep(Status{Latest: p.Version, URL: safeURL(p.URL), PublishedAt: p.ReleasedAt, UpdateAvailable: Compare(stable, cur) > 0}, etag)
	return nil
}

func (c *Checker) keep(s Status, etag string) {
	c.mu.Lock()
	c.latest, c.etag = s, etag
	c.mu.Unlock()
}

// fetch makes the one GET, with the ETag of the answer in hand.
func (c *Checker) fetch(ctx context.Context, url, accept string) (body []byte, etag string, code int, err error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: Timeout, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "harnessly-runtime")
	c.mu.Lock()
	if c.etag != "" {
		req.Header.Set("If-None-Match", c.etag)
	}
	c.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", resp.StatusCode, nil
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, bodyMax))
	if err != nil {
		return nil, "", 0, err
	}
	return body, resp.Header.Get("ETag"), resp.StatusCode, nil
}

// shownURL is an address as a note may show it: without the user, the password and the query, which may hold a
// mirror's credentials.
func shownURL(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil || u.Host == "" {
		return "the configured address"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

// safeURL keeps only an https address: the page links to it.
func safeURL(u string) string {
	if strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}
