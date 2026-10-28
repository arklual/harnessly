// Package sites publishes static sites: a site is a name in a space, a deployment is an immutable snapshot of files
// (a manifest of path → sha256 and blobs addressed by that hash), and the site's "current" alias points at one
// deployment. The same code runs inside the hub and, with its own database, as a standalone server.
package sites

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	MaxSiteName   = 30
	MaxSpaceLabel = 20
)

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// reserved names are never a site: they are what a person would expect to find at the service's own address.
var reserved = map[string]bool{
	"www": true, "api": true, "admin": true, "login": true, "logout": true, "signin": true, "signup": true,
	"harnessly": true, "sinopia": true, "app": true, "hub": true, "mail": true, "static": true,
	"assets": true, "cdn": true, "status": true, "docs": true, "help": true, "support": true, "root": true,
	"sites": true, "site": true, "dashboard": true, "auth": true, "oauth": true, "account": true, "billing": true,
}

func validLabel(kind, s string, max int) error {
	switch {
	case s == "":
		return fmt.Errorf("%s: пусто", kind)
	case len(s) > max:
		return fmt.Errorf("%s: не длиннее %d символов", kind, max)
	case strings.Contains(s, "--"):
		return fmt.Errorf("%s: два дефиса подряд нельзя", kind)
	case !labelRe.MatchString(s):
		return fmt.Errorf("%s: только a–z, 0–9 и дефис, без дефиса в начале и в конце", kind)
	}
	return nil
}

// ValidSiteName checks a site's name: 1–30 of [a-z0-9-], no "--", not a reserved word.
func ValidSiteName(s string) error {
	if err := validLabel("имя сайта", s, MaxSiteName); err != nil {
		return err
	}
	if reserved[s] {
		return errors.New("имя сайта: это слово занято")
	}
	return nil
}

// ValidSpaceLabel checks the space part of an address: 1–20 of [a-z0-9-], no "--".
func ValidSpaceLabel(s string) error { return validLabel("пространство", s, MaxSpaceLabel) }

// Kind is which deployment of a site an address opens.
type Kind int

const (
	// KindCurrent follows the site's "current" alias.
	KindCurrent Kind = iota
	// KindVersion is one numbered deployment, whatever is current now.
	KindVersion
	// KindPreview is the preview of a branch or pull request; reserved, the edge answers 404 for it for now.
	KindPreview
)

// Addr is a parsed site address.
type Addr struct {
	Site, Space string
	Kind        Kind
	N           int
}

// Label is the single DNS label of the address: <site>--<space>, <site>--v<N>--<space>, <site>--pr-<N>--<space>.
func (a Addr) Label() string {
	switch a.Kind {
	case KindVersion:
		return a.Site + "--v" + strconv.Itoa(a.N) + "--" + a.Space
	case KindPreview:
		return a.Site + "--pr-" + strconv.Itoa(a.N) + "--" + a.Space
	}
	return a.Site + "--" + a.Space
}

// Host is the address under the sites domain.
func (a Addr) Host(domain string) string { return a.Label() + "." + domain }

var (
	versionRe = regexp.MustCompile(`^v([1-9][0-9]{0,8})$`)
	previewRe = regexp.MustCompile(`^pr-([1-9][0-9]{0,8})$`)
)

// HostName is a request host without the port, the trailing dot and the case.
func HostName(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return strings.TrimSuffix(h, ".")
}

// ParseHost reads a site address out of a request host: exactly one label in front of the domain.
func ParseHost(host, domain string) (Addr, bool) {
	domain = HostName(domain)
	if domain == "" {
		return Addr{}, false
	}
	label, ok := strings.CutSuffix(HostName(host), "."+domain)
	if !ok || label == "" || strings.Contains(label, ".") {
		return Addr{}, false
	}
	parts := strings.Split(label, "--")
	var a Addr
	switch len(parts) {
	case 2:
		a = Addr{Site: parts[0], Space: parts[1]}
	case 3:
		a = Addr{Site: parts[0], Space: parts[2]}
		if m := versionRe.FindStringSubmatch(parts[1]); m != nil {
			a.Kind = KindVersion
			a.N, _ = strconv.Atoi(m[1])
		} else if m := previewRe.FindStringSubmatch(parts[1]); m != nil {
			a.Kind = KindPreview
			a.N, _ = strconv.Atoi(m[1])
		} else {
			return Addr{}, false
		}
	default:
		return Addr{}, false
	}
	if !labelRe.MatchString(a.Site) || len(a.Site) > MaxSiteName || !labelRe.MatchString(a.Space) || len(a.Space) > MaxSpaceLabel {
		return Addr{}, false
	}
	return a, true
}
