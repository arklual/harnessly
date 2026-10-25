package ports

import (
	"context"
	"encoding/base64"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Page is what a web page says about itself: the name to show a person instead of a process name, a line about it and
// its small icon (inlined, since the machine's address is not one the browser can load).
type Page struct {
	Title       string
	Description string
	Icon        string
}

const (
	pageBodyLimit   = 128 << 10
	pageIconLimit   = 24 << 10
	pageTextLimit   = 120
	pageNoteLimit   = 200
	pageFetchBudget = 1500 * time.Millisecond
)

var (
	titleTag   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaTag    = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	linkTag    = regexp.MustCompile(`(?is)<link\s[^>]*>`)
	attributes = regexp.MustCompile(`(?is)([a-z][a-z0-9:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	spaces     = regexp.MustCompile(`\s+`)
)

func attrsOf(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attributes.FindAllStringSubmatch(tag, -1) {
		v := m[2] + m[3] + m[4]
		out[strings.ToLower(m[1])] = v
	}
	return out
}

func tidy(v string, limit int) string {
	v = strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(v), " "))
	if utf8.RuneCountInString(v) > limit {
		r := []rune(v)
		v = string(r[:limit-1]) + "…"
	}
	return v
}

// iconRank prefers a plain icon to a touch icon; 0 is not an icon.
func iconRank(rel string) int {
	best := 0
	for _, token := range strings.Fields(strings.ToLower(rel)) {
		switch token {
		case "icon":
			best = max(best, 3)
		case "apple-touch-icon":
			best = max(best, 2)
		}
	}
	return best
}

// ParsePage reads the title, the description and the address of the icon (relative to the page, "" when it names none)
// out of the head of an HTML document.
func ParsePage(doc string) (title, description, icon string) {
	if m := titleTag.FindStringSubmatch(doc); m != nil {
		title = tidy(m[1], pageTextLimit)
	}
	for _, tag := range metaTag.FindAllString(doc, -1) {
		a := attrsOf(tag)
		switch strings.ToLower(a["name"]) {
		case "description":
			if description == "" {
				description = tidy(a["content"], pageNoteLimit)
			}
		}
		if description == "" && strings.EqualFold(a["property"], "og:description") {
			description = tidy(a["content"], pageNoteLimit)
		}
		if title == "" && strings.EqualFold(a["property"], "og:title") {
			title = tidy(a["content"], pageTextLimit)
		}
	}
	rank := 0
	for _, tag := range linkTag.FindAllString(doc, -1) {
		a := attrsOf(tag)
		if r := iconRank(a["rel"]); r > rank && a["href"] != "" {
			rank, icon = r, html.UnescapeString(a["href"])
		}
	}
	return title, description, icon
}

func getOnce(client *http.Client, ctx context.Context, address string, limit int64) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header["User-Agent"] = []string{""}
	req.Header.Set("Accept", "text/html,image/*;q=0.8,*/*;q=0.5")
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	return res, body, nil
}

// inlineIcon is the icon at href (as the page at base names it) as a data URI; "" when it is not a small image on the
// same server (a page does not get to send the runtime to fetch from elsewhere).
func inlineIcon(ctx context.Context, base *url.URL, href string) string {
	if strings.HasPrefix(href, "data:image/") {
		if len(href) <= pageIconLimit*2 {
			return href
		}
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	at := base.ResolveReference(ref)
	if at.Scheme != "http" || at.Host != base.Host {
		return ""
	}
	res, body, err := getOnce(probeClient, ctx, at.String(), pageIconLimit)
	if err != nil || res.StatusCode != http.StatusOK || len(body) == 0 || len(body) > pageIconLimit {
		return ""
	}
	kind := http.DetectContentType(body)
	if t := strings.ToLower(res.Header.Get("Content-Type")); strings.HasPrefix(t, "image/svg+xml") {
		kind = "image/svg+xml"
	}
	if !strings.HasPrefix(kind, "image/") {
		return ""
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(body)
}

// FetchPage reads the page a port serves at its root: 127.0.0.1 first, ::1 with what is left of the budget. The zero
// Page when nothing answers.
func FetchPage(port int, budget time.Duration) Page {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	for _, host := range []string{"127.0.0.1", "::1"} {
		base := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: "/"}
		res, body, err := getOnce(probeClient, ctx, base.String(), pageBodyLimit)
		if err != nil {
			continue
		}
		if len(body) > pageBodyLimit {
			body = body[:pageBodyLimit]
		}
		title, description, icon := ParsePage(string(body))
		if icon == "" && res.StatusCode == http.StatusOK {
			icon = "/favicon.ico"
		}
		page := Page{Title: title, Description: description}
		if icon != "" {
			page.Icon = inlineIcon(ctx, base, icon)
		}
		return page
	}
	return Page{}
}
