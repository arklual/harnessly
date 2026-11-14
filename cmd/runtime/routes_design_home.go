package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// HRN-1410, stage 1. GET <platform>/design/home is Sinopia's home in one answer: the library, the person's shelves,
// the design systems, the kits, the apps' document types and their documents. The page asked for them one by one,
// each a trip through the hub to the machine; here the runtime asks itself, side by side, and answers once. Each part
// is the answer its own route gives, as it gives it, so the page reads it with the readers it already has; a part that
// failed is in `failed` with its status and body, and the rest still come. A runtime with no hub serves it the same.

// homePart is one of the home's reads: its name in the answer and the request the page made for it.
type homePart struct {
	name, method, path, body string
}

func homeParts() []homePart {
	return []homePart{
		{"library", http.MethodGet, "/api/v2/designly/library", ""},
		{"layout", http.MethodPost, platformAPI + "/design/library/layout", "{}"},
		{"systems", http.MethodGet, "/api/v2/designly/systems", ""},
		{"kits", http.MethodGet, platformAPI + "/design/kit/canvases", ""},
		{"kinds", http.MethodGet, platformAPI + "/apps/kinds", ""},
		{"kindDocs", http.MethodGet, platformAPI + "/apps/kinds/docs", ""},
	}
}

// homeFailure is a part that did not answer 2xx JSON.
type homeFailure struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// designHome is the answer: the parts by name, what failed, and how long each took (ms), for the page's journal.
type designHome struct {
	Parts  map[string]json.RawMessage `json:"parts"`
	Failed map[string]homeFailure     `json:"failed"`
	TookMs map[string]int64           `json:"tookMs"`
}

func designHomeRoutes(f http.Handler) []gate.Route {
	path := platformAPI + "/design/home"
	return []gate.Route{{Slice: "design", Match: func(r *http.Request) bool {
		return r.Method == http.MethodGet && files.RouteIs(r, path)
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		home := readHome(f, r, homeParts())
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(home)
	})}}
}

// readHome asks h for every part as the page would, with the page's own cookies and headers, side by side.
func readHome(h http.Handler, r *http.Request, parts []homePart) designHome {
	home := designHome{Parts: map[string]json.RawMessage{}, Failed: map[string]homeFailure{}, TookMs: map[string]int64{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range parts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			status, body := askSelf(h, r, p)
			mu.Lock()
			defer mu.Unlock()
			home.TookMs[p.name] = time.Since(start).Milliseconds()
			if status >= 200 && status < 300 && json.Valid(body) {
				home.Parts[p.name] = body
				return
			}
			fail := homeFailure{Status: status}
			if json.Valid(body) {
				fail.Body = body
			}
			home.Failed[p.name] = fail
		}()
	}
	wg.Wait()
	return home
}

func askSelf(h http.Handler, r *http.Request, p homePart) (int, []byte) {
	sub := r.Clone(r.Context())
	u := *r.URL
	u.Path, u.RawPath = p.path, ""
	sub.URL, sub.Method, sub.RequestURI = &u, p.method, ""
	sub.Body = io.NopCloser(strings.NewReader(p.body))
	sub.ContentLength = int64(len(p.body))
	// The answer is read here, not sent on: no compression, and a body the route reads as JSON.
	sub.Header.Del("Accept-Encoding")
	sub.Header.Del("If-None-Match")
	sub.Header.Del("If-Modified-Since")
	if p.body != "" {
		sub.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sub)
	return rec.Code, bytes.TrimSpace(rec.Body.Bytes())
}
