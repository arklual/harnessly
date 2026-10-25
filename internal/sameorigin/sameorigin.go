// Package sameorigin tells a request of a page of another origin from one of the machine's own pages.
package sameorigin

import (
	"net/http"
	"net/url"
	"strings"
)

// CrossSite says whether a request comes from a page of another origin. It guards what a person's session must not
// do for any page that asks: the sockets, which open a terminal on the machine, and the writes beside them. A browser
// sends the session cookie to a request of any site, so the cookie alone proves nothing about who asks.
//
// Sec-Fetch-Site is the browser's own word and decides alone when it is there: same-origin (or none: the person
// typed the address) passes, same-site (a sibling subdomain, a preview stand) and cross-site do not. Without it, an
// Origin header must name the host asked (or the host a proxy in front was asked as). A request with neither is not a
// browser's, and it has its own credentials to show.
func CrossSite(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site != "same-origin" && site != "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}
	if strings.EqualFold(u.Host, r.Host) {
		return false
	}
	fwd, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	return fwd == "" || !strings.EqualFold(u.Host, strings.TrimSpace(fwd))
}

// Refuse answers a request CrossSite names with 403 and says it did; the sockets call it before they upgrade.
func Refuse(w http.ResponseWriter, r *http.Request) bool {
	if !CrossSite(r) {
		return false
	}
	http.Error(w, "Forbidden", http.StatusForbidden)
	return true
}
