package main

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/webstatic"
)

// serveStatic answers for one file of the page, gzipped for browsers that take it (internal/webstatic). It reports
// false when the file cannot be read, so the caller can fall through to the page's own routes.
func (c *webConfig) serveStatic(w http.ResponseWriter, r *http.Request, name string) bool {
	return c.static.Serve(w, r, c.files, name)
}

// serveHTML sends the rendered page, gzipped when the browser takes it. It stays uncached (no-cache, no tag).
func serveHTML(w http.ResponseWriter, r *http.Request, raw []byte) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	webstatic.WriteBody(w, r, raw, nil)
}
