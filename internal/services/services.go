// Package services answers the runtime's routes that only call external services with the person's tokens: a
// session's pushes and merge requests on GitLab, a merge request or commit with its diff, link previews (tracker
// issues, merge requests, published documents), tracker avatars, project keys and task cards, and site icons. The
// organizer (a small file of the person's own) and the page's start data are here too. Answers, caches and their
// lifetimes are the previous runtime's; the GitLab walk asks git only what it needs, runs its requests in parallel and
// keeps their answers a few seconds.
package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"harnessly/internal/files"
	"harnessly/internal/jscompat"
)

// Config is what the deployment names.
type Config struct {
	// Home is the runtime user's home: Claude Code and Codex transcripts are under it.
	Home string
	// SecretDirs are home-relative folders that hold token files (".config/<app>"), searched in order.
	SecretDirs []string
	// GitLabHosts are the GitLabs links and remotes may point at; the first is the self-hosted one, which reads its
	// own token.
	GitLabHosts []string
	// TrackerURL is the issue tracker's origin (https://tracker.example.com); TrackerHosts the hosts its issue links use.
	TrackerURL   string
	TrackerHosts []string
	// DocsHost serves published documents (/a/<slug>) and boards (/b/<slug>); ReaderHost and ReaderPath are where the
	// platform shows the same document.
	DocsHost   string
	ReaderHost string
	ReaderPath string
	// TaskKey is a task key the task card serves.
	TaskKey *regexp.Regexp
	// AvatarPath is the route that serves tracker avatars, as the page loads it.
	AvatarPath string
	// IconAgent is the User-Agent of site icon requests.
	IconAgent string
	// OrganizerFile is the person's organizer.
	OrganizerFile string
	// Transport carries every outgoing request; nil is the default one.
	Transport http.RoundTripper
}

// Service holds the caches the answers share.
type Service struct {
	cfg      Config
	follow   *http.Client
	noFollow *http.Client
	homes    []string

	gitlab   *gitlabState
	meta     *linkMetaState
	tasks    *taskState
	icons    *iconState
	organize sync.Mutex
}

func New(cfg Config) *Service {
	t := cfg.Transport
	if t == nil {
		t = http.DefaultTransport
	}
	homes := []string{cfg.Home}
	if u, err := user.Current(); err == nil && u.HomeDir != "" && u.HomeDir != cfg.Home {
		homes = append(homes, u.HomeDir)
	}
	if cfg.ReaderPath == "" {
		cfg.ReaderPath = "/"
	}
	return &Service{
		cfg:      cfg,
		homes:    homes,
		follow:   &http.Client{Transport: t, CheckRedirect: followRedirect},
		noFollow: &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		gitlab:   newGitlabState(),
		meta:     newLinkMetaState(),
		tasks:    newTaskState(),
		icons:    newIconState(),
	}
}

// secret is an environment variable, else the first non-empty file of the names in a secret folder of a home; read
// on every use, so a rotated token is taken at once.
func (s *Service) secret(envName string, names ...string) string {
	if v := jscompat.Trim(os.Getenv(envName)); v != "" {
		return v
	}
	for _, home := range s.homes {
		for _, dir := range s.cfg.SecretDirs {
			for _, name := range names {
				raw, err := os.ReadFile(filepath.Join(home, dir, name))
				if err != nil {
					continue
				}
				if v := jscompat.Trim(string(raw)); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func (s *Service) trackerToken() string {
	if s.cfg.TrackerURL == "" {
		return ""
	}
	return s.secret("YOUTRACK_TOKEN", "youtrack-token")
}

// gitlabToken: each GitLab gets only its own token, the public one never sees the self-hosted one's.
func (s *Service) gitlabToken(host string) string {
	if len(s.cfg.GitLabHosts) > 0 && host == s.cfg.GitLabHosts[0] {
		return s.secret("GITLAB_SELF_TOKEN", "gitlab-self-token")
	}
	return s.secret("GITLAB_TOKEN", "gitlab-token", "glab-token")
}

func (s *Service) isGitlabHost(host string) bool {
	for _, h := range s.cfg.GitLabHosts {
		if h == host {
			return true
		}
	}
	return false
}

// redirect modes of fetch.
type redirectMode int

const (
	follow redirectMode = iota
	refuse              // a redirect is a network error
)

// errNetwork is a rejected fetch.
var errNetwork = errors.New("network error")

// response is a fetched answer: its status, headers, final URL and body, read in full under the same deadline.
type response struct {
	status int
	header http.Header
	url    string
	body   []byte
	tooBig bool
}

func (r *response) ok() bool { return r.status >= 200 && r.status <= 299 }

// fetch requests a URL as the previous runtime's fetch did, the whole exchange within timeout. max bounds the body
// read (tooBig past it).
func (s *Service) fetch(ctx context.Context, target string, header http.Header, mode redirectMode, timeout time.Duration, max int64) (*response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := newRequest(ctx, target)
	if err != nil {
		return nil, errNetwork
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	client := s.follow
	if mode == refuse {
		client = s.noFollow
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errNetwork
	}
	defer resp.Body.Close()
	if mode == refuse {
		switch resp.StatusCode {
		case 301, 302, 303, 307, 308:
			return nil, errNetwork
		}
	}
	out := &response{status: resp.StatusCode, header: resp.Header, url: hrefOf(resp.Request.URL)}
	if max <= 0 {
		max = 64 << 20
	}
	out.body, err = io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return out, errBody
	}
	if int64(len(out.body)) > max {
		out.body, out.tooBig = out.body[:max], true
	}
	return out, nil
}

// errBody: the answer came, its body did not (the deadline passed while it was read).
var errBody = errors.New("body read failed")

// newRequest builds a GET for a URL written as the WHATWG serializer writes it, sending its path and query as they are.
func newRequest(ctx context.Context, target string) (*http.Request, error) {
	u, ok := parseWebURL(target, nil)
	if !ok || u.Scheme != "http" && u.Scheme != "https" {
		return nil, errNetwork
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Scheme+"://"+u.HostPort()+"/", nil)
	if err != nil {
		return nil, err
	}
	setTarget(req, u)
	return req, nil
}

// setTarget points a request at a parsed URL; the path goes out exactly as serialized.
func setTarget(req *http.Request, u *webURL) {
	path, query, hasQuery := strings.Cut(u.Target(), "?")
	if strings.HasPrefix(path, "//") {
		path = "//" + u.HostPort() + path
	}
	req.URL = &url.URL{Scheme: u.Scheme, Host: u.HostPort(), Opaque: path, RawQuery: query, ForceQuery: hasQuery && query == ""}
	req.Host = ""
}

// hrefOf is a request's URL as fetch reports it.
func hrefOf(u *url.URL) string {
	if u.Opaque == "" {
		return u.String()
	}
	q := ""
	if u.RawQuery != "" || u.ForceQuery {
		q = "?" + u.RawQuery
	}
	if strings.HasPrefix(u.Opaque, "//") {
		return u.Scheme + ":" + u.Opaque + q
	}
	return u.Scheme + "://" + u.Host + u.Opaque + q
}

// followRedirect resolves a Location the way fetch does (the URL standard, against the URL it answered), up to 20.
func followRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 20 {
		return errNetwork
	}
	prev, ok := parseWebURL(hrefOf(via[len(via)-1].URL), nil)
	if !ok || req.Response == nil {
		return errNetwork
	}
	next, ok := parseWebURL(req.Response.Header.Get("Location"), prev)
	if !ok || next.Scheme != "http" && next.Scheme != "https" {
		return errNetwork
	}
	setTarget(req, next)
	req.Header.Del("Referer")
	return nil
}

// decodeText is a body as fetch's text() reads it: UTF-8, a byte order mark dropped, bad bytes replaced.
func decodeText(b []byte) string {
	return files.DecodeUTF8(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")))
}
