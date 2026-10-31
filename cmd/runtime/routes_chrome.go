package main

import (
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chromeview"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// The machine's Chrome in the pane ("chrome" tab): /api/chrome is the socket of its live view (internal/chromeview).
// RUNTIME_CHROME_CDP is the remote debugging address of the Chrome the agents' chrome MCP drives (default
// http://127.0.0.1:9222); "off" turns it off.

var (
	chromeOnce   sync.Once
	chromeViewer *chromeview.Viewer
)

func chromeLog(level, msg string, kv ...any) {
	fs := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		fs = append(fs, agent.F{K: k, V: kv[i+1]})
	}
	agent.Log(level, msg, fs...)
}

// machineChrome is the machine's one viewer.
func machineChrome() *chromeview.Viewer {
	chromeOnce.Do(func() {
		cdp, set := os.LookupEnv("RUNTIME_CHROME_CDP")
		switch {
		case !set:
			cdp = "http://127.0.0.1:9222"
		case strings.EqualFold(strings.TrimSpace(cdp), "off"):
			cdp = ""
		}
		chromeViewer = chromeview.New(chromeview.Config{CDP: strings.TrimSpace(cdp), Log: chromeLog})
	})
	return chromeViewer
}

func chromeRoutes(d deps) []gate.Route {
	v := machineChrome()
	return []gate.Route{{
		Match: func(r *http.Request) bool { return v.On() && pathIs(r, "/api/chrome") && isUpgrade(r) },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !d.SessionOK(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			v.ServeHTTP(w, r)
		}),
	}}
}
