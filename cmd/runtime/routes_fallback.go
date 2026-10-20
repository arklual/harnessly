package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/settings"
)

// headlessMode says whether state-mode.json makes the previous runtime headless (pages are the cluster's), re-read at
// most once a second as it did.
func headlessMode(dataDir string) func() bool {
	file := filepath.Join(dataDir, "state-mode.json")
	var mu sync.Mutex
	var at time.Time
	var on bool
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) > time.Second {
			at = time.Now()
			var mode struct {
				Headless bool `json:"headless"`
			}
			raw, err := os.ReadFile(file)
			on = err == nil && json.Unmarshal(raw, &mode) == nil && mode.Headless
		}
		return on
	}
}

// slicedSockets are the socket paths a slice answers; while it is off they are the previous runtime's.
var slicedSockets = map[string]bool{"/api/terminal": true, "/api/timeline": true, "/api/sync": true, "/api/status": true, "/api/voice": true}

// fallbackRoute answers, behind the state slice, what the previous runtime answered to a request none of its routes
// took on a headless machine: "Not found" to any other path, a refused socket to any other socket path. Its routes, the
// preview proxy and the sliced sockets stay with whoever holds them. The install socket of the first-run setup (a shell
// for anyone during setup) is not served: it is refused like an unknown socket.
func fallbackRoute(d deps) gate.Route {
	headless := headlessMode(d.DataDir)
	if d.Standalone { // no pages of the old runtime to serve
		headless = func() bool { return true }
	}
	preview := previewOf(d).proxy
	socketPath := func(r *http.Request) string {
		p := r.URL.Path
		if rest, ok := strings.CutPrefix(p, "/agents/"); ok {
			p = "/" + rest
		}
		return p
	}
	return gate.Route{
		Slice: settings.Slice,
		Match: func(r *http.Request) bool {
			if preview.Matches(r) {
				return false
			}
			if isUpgrade(r) {
				p := socketPath(r)
				return p == "/api/install" || !slicedSockets[p] && headless()
			}
			return headless() && !oldRouteMatches(r)
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isUpgrade(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Not found"}`))
				return
			}
			status := "HTTP/1.1 404 Not Found\r\n\r\n"
			if socketPath(r) != "/api/install" && !d.SessionOK(r) && !d.Old.CLITokenOK(r) {
				status = "HTTP/1.1 401 Unauthorized\r\n\r\n"
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte(status))
			_ = conn.Close()
		}),
	}
}
