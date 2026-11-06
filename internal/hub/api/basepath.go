package api

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strings"
)

// underBase lets a hub whose HUB_PUBLIC_URL has a path (https://example.com/harnessly) be reached either way a reverse
// proxy may forward it: with the path (proxy_pass without a URI part) or without (proxy_pass with a trailing slash,
// which cuts the location off). A request with the path has it taken off before the routes see it, so the routes stay
// the same at the root and under a path; the bare path is sent to its slash. Every redirect the hub makes to a place
// of its own (a Location that starts with a single slash) gets the path put back, so a handler writes /signin and the
// browser is sent to /harnessly/signin.
func underBase(base string, next http.Handler) http.Handler {
	if base == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == base:
			target := base + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		case strings.HasPrefix(r.URL.Path, base+"/"):
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, base)
			if r.URL.RawPath != "" {
				r2.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, base)
			}
			r2.RequestURI = r2.URL.RequestURI()
			r = r2
		}
		next.ServeHTTP(&prefixLocation{ResponseWriter: w, base: base}, r)
	})
}

// prefixLocation puts the base path on a redirect to a place of the hub.
type prefixLocation struct {
	http.ResponseWriter
	base string
}

func (p *prefixLocation) WriteHeader(code int) {
	h := p.Header()
	if loc := h.Get("Location"); strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") && !strings.HasPrefix(loc, p.base+"/") {
		h.Set("Location", p.base+loc)
	}
	p.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController (flush, hijack for WebSockets) reach the real writer.
func (p *prefixLocation) Unwrap() http.ResponseWriter { return p.ResponseWriter }

// Flush keeps event streams flowing through the wrapper.
func (p *prefixLocation) Flush() {
	if f, ok := p.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack hands the connection over for the WebSockets (the machine link, the tunnelled pages).
func (p *prefixLocation) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := p.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("the connection cannot be hijacked")
	}
	return h.Hijack()
}
