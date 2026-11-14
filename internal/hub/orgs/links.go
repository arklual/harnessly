package orgs

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// hostName is a DNS name of two or more labels, with an optional port: "gitlab.example.com", "git.example.com:8443".
var hostName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(:[0-9]{1,5})?$`)

const maxGitLabHosts = 10

// cleanLinks checks the organization's integrations for the chat's links and brings them to one form: an
// empty field names nothing; the stand domain has no scheme and no "*."; an address is an origin (scheme and host,
// no path); GitLab hosts are lowercase names without repeats.
func cleanLinks(in hubproto.LinkSettings) (hubproto.LinkSettings, error) {
	var out hubproto.LinkSettings
	if d := strings.ToLower(strings.TrimSpace(in.StandDomain)); d != "" {
		d = strings.TrimPrefix(d, "*.")
		d = strings.TrimSuffix(d, ".")
		if !hostName.MatchString(d) || strings.Contains(d, ":") {
			return out, httpx.Invalid("links.standDomain", "Домен стендов — без схемы и пути, например preview.example.com")
		}
		out.StandDomain = d
	}
	var err error
	if out.TrackerURL, err = cleanOrigin(in.TrackerURL, "links.trackerUrl", "Адрес трекера"); err != nil {
		return out, err
	}
	if out.ArtifactsURL, err = cleanOrigin(in.ArtifactsURL, "links.artifactsUrl", "Адрес документов"); err != nil {
		return out, err
	}
	if out.BoardsURL, err = cleanOrigin(in.BoardsURL, "links.boardsUrl", "Адрес досок"); err != nil {
		return out, err
	}
	for _, raw := range in.GitLabHosts {
		for _, h := range strings.Split(raw, ",") {
			h = strings.ToLower(strings.TrimSpace(h))
			if h == "" {
				continue
			}
			if !hostName.MatchString(h) {
				return out, httpx.Invalid("links.gitlabHosts", "GitLab — хосты без схемы и пути, например gitlab.example.com")
			}
			dup := false
			for _, have := range out.GitLabHosts {
				dup = dup || have == h
			}
			if !dup {
				out.GitLabHosts = append(out.GitLabHosts, h)
			}
		}
	}
	if len(out.GitLabHosts) > maxGitLabHosts {
		return out, httpx.Invalid("links.gitlabHosts", "Не больше десяти хостов GitLab")
	}
	return out, nil
}

// cleanOrigin takes "https://host" (a trailing slash is cut) and returns it; "" is "not named".
func cleanOrigin(raw, field, what string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	msg := what + " — https:// или http:// и хост, без пути, например https://tracker.example.com"
	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || u.Opaque != "" || !hostName.MatchString(strings.ToLower(u.Host)) {
		return "", httpx.Invalid(field, msg)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// sameLinks says whether two settings name the same things.
func sameLinks(a, b hubproto.LinkSettings) bool {
	return a.StandDomain == b.StandDomain && a.TrackerURL == b.TrackerURL && a.ArtifactsURL == b.ArtifactsURL &&
		a.BoardsURL == b.BoardsURL && slices.Equal(a.GitLabHosts, b.GitLabHosts)
}
