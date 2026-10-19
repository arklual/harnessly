package services

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"harnessly/internal/jscompat"
)

const (
	iconFresh     = 24 * time.Hour
	iconMissFresh = time.Hour
	iconMaxBytes  = 200_000
	iconTimeout   = 3500 * time.Millisecond
	iconPageScan  = 300_000
	// iconPageBytes bounds the page read: the scan sees 300 000 UTF-16 units, at most three bytes each.
	iconPageBytes = 3*iconPageScan + 4
	iconTries     = 4
)

var (
	// HostRe is a host name the icon route serves.
	HostRe        = regexp.MustCompile(`^[a-z0-9-]{1,63}(\.[a-z0-9-]{1,63})+$`)
	linkTagRe     = regexp.MustCompile(`<` + ci("link") + `\b[^>]*>`)
	iconRelRe     = regexp.MustCompile(`\bicon\b|apple-touch-icon`)
	svgPathRe     = regexp.MustCompile(`\.` + ci("svg") + `(\?|$)`)
	attrPatterns  = map[string]*regexp.Regexp{}
	attrPatternMu sync.Mutex
)

type iconEntry struct {
	icon *Image
	at   time.Time
}

type iconState struct {
	mu    sync.Mutex
	cache map[string]iconEntry
}

func newIconState() *iconState { return &iconState{cache: map[string]iconEntry{}} }

// ValidIconHost: a public name, not an address, not a local or internal one.
func ValidIconHost(host string) bool {
	if len(host) > 253 || !HostRe.MatchString(host) || strings.Trim(host, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".internal")
}

func attr(tag, name string) string {
	attrPatternMu.Lock()
	re := attrPatterns[name]
	if re == nil {
		re = regexp.MustCompile(ci(name) + jscompat.SpaceClass + `*=` + jscompat.SpaceClass + `*["']([^"']*)["']`)
		attrPatterns[name] = re
	}
	attrPatternMu.Unlock()
	if m := re.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

// jsLower is toLowerCase where it differs from Go's on the way to an ASCII test: İ becomes i and a combining dot.
func jsLower(s string) string { return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇")) }

type iconCandidate struct {
	href string
	rank int
}

// iconCandidates are the icon links of a page head, best first: SVG, touch icons, large PNGs, then anything marked as
// an icon.
func iconCandidates(html, base string) []iconCandidate {
	baseURL, ok := parseWebURL(base, nil)
	if !ok {
		return nil
	}
	var out []iconCandidate
	for _, tag := range linkTagRe.FindAllString(jscompat.Slice(html, iconPageScan), -1) {
		rel := jsLower(attr(tag, "rel"))
		if !iconRelRe.MatchString(rel) {
			continue
		}
		href := attr(tag, "href")
		if href == "" || strings.HasPrefix(href, "data:") {
			continue
		}
		u, ok := parseWebURL(href, baseURL)
		if !ok || u.Scheme != "https" {
			continue
		}
		typ := jsLower(attr(tag, "type"))
		size := 0.0
		for _, part := range regexp.MustCompile(jscompat.SpaceClass+`+`).Split(attr(tag, "sizes"), -1) {
			if n := jscompat.ParseInt(part, 10); n == n && n > size {
				size = n
			}
		}
		rank := 3
		switch {
		case strings.Contains(typ, "svg") || svgPathRe.MatchString(u.Pathname()):
			rank = 0
		case strings.Contains(rel, "apple-touch-icon"):
			rank = 1
		case size >= 32:
			rank = 2
		}
		out = append(out, iconCandidate{href: u.Href(), rank: rank})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rank < out[j].rank })
	return out
}

// parentHost: a subdomain without an icon of its own borrows its parent's.
func parentHost(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) > 2 {
		return strings.Join(parts[1:], ".")
	}
	return ""
}

var imageType = regexp.MustCompile(`^image/`)

func (s *Service) iconHeader() http.Header {
	h := http.Header{"Accept": {"*/*"}}
	if s.cfg.IconAgent != "" {
		h.Set("User-Agent", s.cfg.IconAgent)
	}
	return h
}

func (s *Service) fetchIcon(ctx context.Context, target string) *Image {
	resp, err := s.fetch(ctx, target, s.iconHeader(), follow, iconTimeout, iconMaxBytes)
	if err != nil || resp == nil {
		return nil
	}
	typ := resp.header.Get("Content-Type")
	if !resp.ok() || !imageType.MatchString(typ) || len(resp.body) == 0 || resp.tooBig {
		return nil
	}
	return &Image{Type: typ, Body: resp.body}
}

// iconForHost: the page's icon links in their order, then /favicon.ico. The candidates are fetched together; the
// first in order that is a picture wins, as if they were tried one by one.
func (s *Service) iconForHost(ctx context.Context, host string) *Image {
	page := "https://" + host + "/"
	html := ""
	if resp, err := s.fetch(ctx, page, s.iconHeader(), follow, iconTimeout, iconPageBytes); err == nil && resp.ok() {
		html = decodeText(resp.body)
	}
	cands := iconCandidates(html, page)
	if len(cands) > iconTries {
		cands = cands[:iconTries]
	}
	targets := make([]string, 0, len(cands)+1)
	for _, c := range cands {
		targets = append(targets, c.href)
	}
	targets = append(targets, "https://"+host+"/favicon.ico")
	results := make([]chan *Image, len(targets))
	for i, t := range targets {
		results[i] = make(chan *Image, 1)
		go func() { results[i] <- s.fetchIcon(ctx, t) }()
	}
	for _, r := range results {
		if icon := <-r; icon != nil {
			return icon
		}
	}
	return nil
}

func (s *Service) findIcon(ctx context.Context, host string) *Image {
	parent := parentHost(host)
	var fromParent chan *Image
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if parent != "" {
		fromParent = make(chan *Image, 1)
		go func() { fromParent <- s.iconForHost(pctx, parent) }()
	}
	if own := s.iconForHost(ctx, host); own != nil {
		return own
	}
	if fromParent == nil {
		return nil
	}
	return <-fromParent
}

// SiteIcon is a site's icon, kept a day (a miss an hour).
func (s *Service) SiteIcon(ctx context.Context, host string) *Image {
	st := s.icons
	st.mu.Lock()
	e, ok := st.cache[host]
	st.mu.Unlock()
	fresh := iconMissFresh
	if ok && e.icon != nil {
		fresh = iconFresh
	}
	if ok && time.Since(e.at) <= fresh {
		return e.icon
	}
	icon := s.findIcon(context.WithoutCancel(ctx), host)
	st.mu.Lock()
	st.cache[host] = iconEntry{icon: icon, at: time.Now()}
	st.mu.Unlock()
	return icon
}
