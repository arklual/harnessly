package preview

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/preview/weburl"
)

// Stands are the deployment's feature stands (https://<label><domain>): the one kind of external site that gets the
// person's own sign-in with the request, passed by the gateway in its own header.
type Stands struct {
	// Domain is the stands' domain with its leading dot; "" makes no host a stand.
	Domain string
	// SignInHosts are where a stand sends a person without a session.
	SignInHosts []string
	// SessionHeader carries the person's stand session from the gateway.
	SessionHeader string
}

var (
	standLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	notStands  = regexp.MustCompile(`^(?:ai-|auth$)`)
	// sessionCookie: the sign-in proxy keeps its session in one cookie, or several numbered ones when it does not fit.
	sessionCookie = regexp.MustCompile(`^_oauth2_proxy(?:_\d{1,2})?$`)
	cookieValue   = regexp.MustCompile(`^[\x21\x23-\x2b\x2d-\x3a\x3c-\x5b\x5d-\x7e]*$`)
)

const (
	maxStandSessionBytes = 16 * 1024
	signInPath           = "/oauth2/"
)

func (s Stands) IsHost(host string) bool {
	if s.Domain == "" || !strings.HasSuffix(host, s.Domain) {
		return false
	}
	label := strings.TrimSuffix(host, s.Domain)
	return standLabel.MatchString(label) && !notStands.MatchString(label)
}

// IsOrigin: https://<stand> exactly as the URL parser prints an origin.
func (s Stands) IsOrigin(origin string) bool {
	u, ok := weburl.Parse(origin)
	return ok && u.Scheme == "https" && u.Port == "" && u.Origin() == origin && s.IsHost(u.Hostname)
}

func isSessionCookie(name string) bool { return sessionCookie.MatchString(name) }

// sessionCookies is only the stand session out of a Cookie header; the first of a repeated name wins.
func sessionCookies(header string) string {
	if len(header) > maxStandSessionBytes {
		return ""
	}
	seen := map[string]bool{}
	var kept []string
	for _, part := range strings.Split(header, ";") {
		pair := strings.TrimSpace(part)
		at := strings.IndexByte(pair, '=')
		if at < 1 {
			continue
		}
		name, value := pair[:at], pair[at+1:]
		if !isSessionCookie(name) || seen[name] || !cookieValue.MatchString(value) {
			continue
		}
		seen[name] = true
		kept = append(kept, name+"="+value)
	}
	return strings.Join(kept, "; ")
}

// isSignInRedirect: next, where the stand redirected, is the way to its sign-in rather than a page of the stand.
func (s Stands) isSignInRedirect(standOrigin string, next *weburl.URL) bool {
	if next.Scheme != "https" || next.Username != "" || next.Password != "" {
		return false
	}
	if next.Origin() == standOrigin {
		return strings.HasPrefix(next.Path, signInPath)
	}
	if next.Port != "" {
		return false
	}
	for _, h := range s.SignInHosts {
		if h == next.Hostname {
			return true
		}
	}
	return false
}

// signInURL signs a person in to the stand and brings them back to pageURL; "" when pageURL is not a stand.
func (s Stands) signInURL(pageURL string) string {
	u, ok := weburl.Parse(jsTrim(pageURL))
	if !ok || u.Scheme != "https" || u.Username != "" || u.Password != "" || u.Port != "" || !s.IsHost(u.Hostname) {
		return ""
	}
	page, _ := weburl.Parse(pageURL)
	rd := page.Origin() + page.Path + page.Search()
	if strings.HasPrefix(page.Path, signInPath) {
		rd = page.Origin()
	}
	return page.Origin() + signInPath + "start?" + formEncode("rd") + "=" + formEncode(rd)
}

// formEncode is URLSearchParams' serialisation of a value.
func formEncode(s string) string {
	return strings.NewReplacer("~", "%7E", "%2A", "*").Replace(url.QueryEscape(s))
}

func jsTrim(s string) string { return jscompat.Trim(s) }
