package connections

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// maxBody caps what is read of one answer.
const maxBody = 8 << 20

// HTTP is how providers call an instance: a client that refuses cloud metadata addresses, follows redirects only on
// the same host, and reads at most maxBody of an answer.
type HTTP struct {
	Client *http.Client
	// UserAgent of every request.
	UserAgent string
}

// metadataNet is the link-local range cloud metadata services answer on: never a tracker.
var metadataNet = mustCIDR("169.254.0.0/16")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// NewHTTP is the default client: 20 s per request, through transport (nil is a fresh one that refuses link-local
// addresses). Self-hosted instances on private addresses are allowed: the runtime runs next to them.
func NewHTTP(transport http.RoundTripper) *HTTP {
	if transport == nil {
		dialer := &net.Dialer{
			Timeout: 8 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); ip != nil && metadataNet.Contains(ip) {
					return fmt.Errorf("refused address %s", host)
				}
				return nil
			},
		}
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       60 * time.Second,
		}
	}
	return &HTTP{
		Client: &http.Client{
			Transport: transport,
			Timeout:   20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if req.URL.Host != via[0].URL.Host {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		UserAgent: "harnessly-connections/1",
	}
}

// Request is one call.
type Request struct {
	Method string
	URL    string
	Header http.Header
	// Body is sent as JSON unless it is a []byte or an io.Reader.
	Body any
}

// Response is an answer, its body read.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
	// URL is where the answer finally came from (after same-host redirects).
	URL *url.URL
}

// JSON says whether the answer is JSON by its type.
func (r *Response) JSON() bool {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	return strings.Contains(ct, "json")
}

// Decode reads the body into out; an answer that is not JSON is ErrUnreachable.
func (r *Response) Decode(out any) error {
	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("%w: not JSON from %s", ErrUnreachable, r.URL.Redacted())
	}
	return nil
}

// Do sends a request and reads the answer whatever its status.
func (h *HTTP) Do(ctx context.Context, req Request) (*Response, error) {
	var body io.Reader
	header := http.Header{}
	for k, v := range req.Header {
		header[k] = v
	}
	switch b := req.Body.(type) {
	case nil:
	case []byte:
		body = bytes.NewReader(b)
	case io.Reader:
		body = b
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
		if header.Get("Content-Type") == "" {
			header.Set("Content-Type", "application/json")
		}
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	r, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	r.Header = header
	if r.Header.Get("Accept") == "" {
		r.Header.Set("Accept", "application/json")
	}
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", h.UserAgent)
	}
	res, err := h.Client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return &Response{Status: res.StatusCode, Header: res.Header, Body: raw, URL: res.Request.URL}, nil
}

// JSON sends a request and decodes a 2xx JSON answer into out (nil skips decoding). 401 and 403 are ErrDenied, 404
// ErrNotFound, anything else that is not 2xx ErrUnreachable.
func (h *HTTP) JSON(ctx context.Context, req Request, out any) (*Response, error) {
	res, err := h.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := StatusErr(res); err != nil {
		return res, err
	}
	if out != nil {
		return res, res.Decode(out)
	}
	return res, nil
}

// StatusErr is the error a status stands for, nil for 2xx.
func StatusErr(res *Response) error {
	switch {
	case res.Status >= 200 && res.Status < 300:
		return nil
	case res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden:
		return fmt.Errorf("%w: %d from %s", ErrDenied, res.Status, res.URL.Redacted())
	case res.Status == http.StatusNotFound || res.Status == http.StatusGone:
		return fmt.Errorf("%w: %d from %s", ErrNotFound, res.Status, res.URL.Redacted())
	default:
		return fmt.Errorf("%w: %d from %s", ErrUnreachable, res.Status, res.URL.Redacted())
	}
}

// Basic is an Authorization header value for user and secret.
func Basic(user, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+secret))
}

// Bearer is an Authorization header value for a token.
func Bearer(token string) string { return "Bearer " + token }

// Header is a one-entry header set.
func Header(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			h.Set(kv[i], kv[i+1])
		}
	}
	return h
}

// Join is base plus path, with exactly one slash between them; path may carry a query.
func Join(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

// Q encodes query parameters as key, value pairs.
func Q(kv ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v.Encode()
}

// ParseTime reads the timestamp formats products use; zero when it is none of them.
func ParseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02T15:04:05.000-0700", // Jira, Confluence
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05.000Z0700",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Millis is a timestamp in milliseconds since the epoch (YouTrack); zero for 0.
func Millis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// repoNameRe is one owner or repository name as code hosts allow them.
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidRepo says whether s is exactly "owner/repo" with safe names, so it can go into an API path as it is: no
// "..", no extra segments, no characters a path or query would read differently.
func ValidRepo(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	if !ok {
		return false
	}
	for _, part := range []string{owner, name} {
		if !repoNameRe.MatchString(part) || part == "." || part == ".." {
			return false
		}
	}
	return true
}
