package proxy

import (
	"net/http"
	"net/url"
	"regexp"
)

// The hub's documents (docs/hub/docs.md, HUB_DOCS): the workspace's page asks them under the platform API,
//
//	<platform api>/docs[/<slug>[/content|threads|versions|…]]
//
// and the hub answers itself with the signed-in person's account, never forwarding to a machine: a document opens
// whether or not any machine is up, and its page is framed by the workspace (a path under /agents/ carries no
// X-Frame-Options of the hub's own pages).

var docsPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+(/docs(?:/.*)?)$`)

// UseDocs turns the hub's documents on for the workspace: h answers them under prefix (the documents' own platform
// API prefix). nil leaves /docs to the machines, as before.
func (s *Service) UseDocs(h http.Handler, prefix string) {
	s.mu.Lock()
	s.docs, s.docsPrefix = h, prefix
	s.mu.Unlock()
}

// serveDocs answers a documents request of the workspace from the hub; false when the path is not one or the hub
// keeps no documents.
func (s *Service) serveDocs(w http.ResponseWriter, r *http.Request, rest string) bool {
	m := docsPath.FindStringSubmatch(rest)
	if m == nil {
		return false
	}
	s.mu.Lock()
	h, prefix := s.docs, s.docsPrefix
	s.mu.Unlock()
	if h == nil {
		return false
	}
	asked := r.Clone(r.Context())
	u := url.URL{Path: prefix + m[1], RawQuery: r.URL.RawQuery}
	asked.URL = &u
	asked.RequestURI = ""
	h.ServeHTTP(w, asked)
	return true
}
