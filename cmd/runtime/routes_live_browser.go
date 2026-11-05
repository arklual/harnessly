package main

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/livebrowser"
	"github.com/ly-solutions/harnessly/internal/preview"
)

// The live browser: a headless Chrome of the machine for the outside sites the proxy cannot carry, shown in
// the pane's browser tab as a screencast over /api/browser/live. The pane decides which sites go here (browser-tab.tsx:
// the machine's servers and the team's own sites stay with the proxy).

func liveBrowserLog(level, msg string, kv ...any) {
	fs := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		fs = append(fs, agent.F{K: k, V: kv[i+1]})
	}
	agent.Log(level, "live browser: "+msg, fs...)
}

func envInt(name string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return n
	}
	return fallback
}

// newLiveBrowser is the machine's live browser, set up from the environment:
//
//	RUNTIME_LIVE_BROWSER_BIN       the browser binary (google-chrome, chromium ... on PATH otherwise)
//	RUNTIME_LIVE_BROWSER_ARGS      extra command-line flags
//	RUNTIME_LIVE_BROWSER_MAX_TABS  pages open at once (4)
//	RUNTIME_LIVE_BROWSER_IDLE_MIN  minutes without a page before the browser closes (3)
//	RUNTIME_LIVE_BROWSER_NICE      the browser's nice value (10)
func newLiveBrowser(d deps) *livebrowser.Service {
	return livebrowser.New(livebrowser.Config{
		DataDir:   d.DataDir,
		Binary:    os.Getenv("RUNTIME_LIVE_BROWSER_BIN"),
		ExtraArgs: livebrowser.EnvArgs(os.Getenv("RUNTIME_LIVE_BROWSER_ARGS")),
		MaxTabs:   envInt("RUNTIME_LIVE_BROWSER_MAX_TABS", 0),
		IdleAfter: time.Duration(envInt("RUNTIME_LIVE_BROWSER_IDLE_MIN", 3)) * time.Minute,
		Nice:      envInt("RUNTIME_LIVE_BROWSER_NICE", 10),
		Local:     preview.SelfAddresses(os.Getenv("RUNTIME_PREVIEW_SELF_IPS")),
		Log:       liveBrowserLog,
	})
}

func liveBrowserRoutes(d deps, s *livebrowser.Service) []gate.Route {
	socket := &livebrowser.Socket{S: s, Log: liveBrowserLog}
	return []gate.Route{
		{
			Match: func(r *http.Request) bool { return pathIs(r, "/api/browser/live") && isUpgrade(r) },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !d.SessionOK(r) {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if !s.Available() {
					http.Error(w, "No Chrome or Chromium on this machine", http.StatusServiceUnavailable)
					return
				}
				socket.ServeHTTP(w, r)
			}),
		},
		{
			// GET /api/browser/live/status: whether the machine can show a live browser, and what runs now.
			Match: func(r *http.Request) bool { return pathIs(r, "/api/browser/live/status") && r.Method == http.MethodGet },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !d.SessionOK(r) {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				connJSON(w, 200, map[string]any{"available": s.Available(), "running": s.Running(), "tabs": s.Tabs()})
			}),
		},
	}
}
