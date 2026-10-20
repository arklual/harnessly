package gate

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
)

// canonical is r with its API path as the old runtime's router read it: trailing and doubled slashes dropped
// ("/api/x/", "/api//x" are "/api/x"), so a route here matches what the router matched. A socket's path was read as
// it is, and a segment holding an encoded "/" cannot be written back as a path: both stay as they came.
func canonical(r *http.Request) *http.Request {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return r
	}
	parts, _, ok := files.APIParts(r)
	if !ok {
		return r
	}
	for _, p := range parts {
		if strings.Contains(p, "/") {
			return r
		}
	}
	p := "/" + strings.Join(parts, "/")
	if strings.HasPrefix(r.URL.Path, "/agents/") {
		p = "/agents" + p
	}
	if p == r.URL.Path {
		return r
	}
	out := new(http.Request)
	*out = *r
	u := *r.URL
	u.Path, u.RawPath = p, ""
	out.URL = &u
	return out
}
