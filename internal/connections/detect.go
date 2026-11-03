package connections

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// probeTimeout bounds the whole detection of one link.
const probeTimeout = 12 * time.Second

// Prober makes the anonymous requests detection needs and shares them between providers: two providers asking for the
// same address in one detection cause one request.
type Prober struct {
	h  *HTTP
	mu sync.Mutex
	// calls by method and URL.
	calls map[string]*probeCall
}

type probeCall struct {
	done chan struct{}
	res  *Response
	err  error
}

// NewProber probes through h.
func NewProber(h *HTTP) *Prober { return &Prober{h: h, calls: map[string]*probeCall{}} }

// Get fetches a URL anonymously, once per detection. Accept is sent as given ("" is JSON).
func (p *Prober) Get(ctx context.Context, rawURL string, accept string) (*Response, error) {
	key := "GET " + accept + " " + rawURL
	p.mu.Lock()
	if c, ok := p.calls[key]; ok {
		p.mu.Unlock()
		select {
		case <-c.done:
			return c.res, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &probeCall{done: make(chan struct{})}
	p.calls[key] = c
	p.mu.Unlock()
	h := http.Header{}
	if accept != "" {
		h.Set("Accept", accept)
	}
	c.res, c.err = p.h.Do(ctx, Request{URL: rawURL, Header: h})
	close(c.done)
	return c.res, c.err
}

// GetJSON fetches a URL anonymously and decodes it when the answer is 2xx JSON; ok is false otherwise.
func (p *Prober) GetJSON(ctx context.Context, rawURL string, out any) (res *Response, ok bool) {
	res, err := p.Get(ctx, rawURL, "")
	if err != nil || res.Status < 200 || res.Status >= 300 || !res.JSON() {
		return res, false
	}
	return res, res.Decode(out) == nil
}

// Normalize reads what a person pasted: an address with or without a scheme, with any path. https is assumed.
func Normalize(raw string) (*url.URL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("%w: empty link", ErrUnknown)
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: not a web address", ErrUnknown)
	}
	u.Host = strings.ToLower(u.Host)
	u.User = nil
	u.Fragment = ""
	return u, nil
}

// Origin is scheme://host[:port] of u.
func Origin(u *url.URL) string { return u.Scheme + "://" + u.Host }

// PathPrefixes are the candidate roots of an instance from a pasted link, longest first: the path cut before every
// segment, so https://h/jira/browse/X-1 gives /jira/browse/X-1, /jira/browse, /jira and "". Paths of known pages are
// cut where a product's own route starts (stop words), which providers pass.
func PathPrefixes(u *url.URL, stops ...string) []string {
	segs := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(segs) == 1 && segs[0] == "" {
		segs = nil
	}
	cut := len(segs)
scan:
	for i, s := range segs {
		for _, stop := range stops {
			if strings.EqualFold(s, stop) {
				cut = i
				break scan
			}
		}
	}
	segs = segs[:cut]
	if len(segs) > 3 {
		segs = segs[:3]
	}
	out := make([]string, 0, len(segs)+1)
	for i := len(segs); i >= 0; i-- {
		if i == 0 {
			out = append(out, "")
			continue
		}
		out = append(out, "/"+strings.Join(segs[:i], "/"))
	}
	return out
}

// Detect finds what a pasted link is. Every provider looks at it at once; the first in registry order that recognizes
// it wins, so products that share a host (Jira and Confluence on one Atlassian site) are told apart by their own
// probes.
func Detect(ctx context.Context, h *HTTP, raw string) (*Detection, error) {
	u, err := Normalize(raw)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	p := NewProber(h)
	providers := Providers()
	found := make([]*Detection, len(providers))
	var wg sync.WaitGroup
	for i, pr := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { _ = recover() }()
			d, err := pr.Detect(ctx, p, u)
			if err == nil && d != nil {
				found[i] = d
			}
		}()
	}
	wg.Wait()
	for _, d := range found {
		if d != nil {
			d.BaseURL = strings.TrimRight(d.BaseURL, "/")
			return d, nil
		}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%w: %s did not answer in time", ErrUnreachable, u.Host)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknown, u.Host)
}

// HostIs says whether host is domain or a subdomain of it.
func HostIs(host, domain string) bool {
	host = strings.ToLower(host)
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// answeredAt says whether an answer came from the path asked, not from where a redirect led. Only the path is
// compared: redirects are followed on the same host only (NewHTTP), and tests reroute hosts.
func answeredAt(res *Response, asked string) bool {
	u, err := url.Parse(asked)
	if err != nil || res.URL == nil {
		return false
	}
	return strings.TrimRight(res.URL.Path, "/") == strings.TrimRight(u.Path, "/")
}
