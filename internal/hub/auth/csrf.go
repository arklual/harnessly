package auth

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// CSRF guards the API. A request that changes something (POST, PUT, PATCH, DELETE) must not come from a page of
// another site (sameorigin.CrossSite) and, when it carries a body, must declare it as JSON, which a plain HTML form
// cannot do. Reads pass. The agent endpoints carry no cookie and are exempt; so are the OAuth callbacks, which are
// GETs and prove themselves with a state bound to a cookie.
func (s *Service) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/agent/") {
			next.ServeHTTP(w, r)
			return
		}
		if sameorigin.CrossSite(r) {
			httpx.WriteError(w, r, httpx.ErrCrossSite)
			return
		}
		// A body-less DELETE carries no Content-Type; every other change declares JSON.
		hasBody := r.ContentLength != 0
		if (r.Method != http.MethodDelete || hasBody) && !httpx.IsJSON(r) {
			httpx.WriteError(w, r, httpx.ErrCrossSite)
			return
		}
		next.ServeHTTP(w, r)
	})
}
