package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/webrelease"
	"github.com/ly-solutions/harnessly/web"
)

// RUNTIME_WEB_FROM_HUB=on (off unless asked for): the machine takes the page its hub hands out
// (internal/webrelease.Hub) into <data>/web-hub and serves it from its next start, through the RUNTIME_WEB_DIR guard
// (web_dir.go). A new page restarts the runtime as an update of its binary does, unless RUNTIME_AUTO_UPDATE is off;
// then it waits for the next start. RUNTIME_WEB_DIR set by hand wins, and turning the variable off brings back the
// compiled-in page at the next start.

const webFromHubEvery = 5 * time.Minute

func webFromHubOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_WEB_FROM_HUB"))) {
	case "on", "1", "true", "yes":
		return os.Getenv("RUNTIME_WEB_DIR") == "" && strings.TrimSpace(os.Getenv("RUNTIME_HUB_URL")) != ""
	}
	return false
}

func webFromHub(dataDir string) webrelease.Hub {
	return webrelease.Hub{
		URL: strings.TrimRight(strings.TrimSpace(os.Getenv("RUNTIME_HUB_URL")), "/"),
		Dir: filepath.Join(dataDir, "web-hub"),
	}
}

// useWebFromHub points RUNTIME_WEB_DIR at the page taken from the hub before the page is set up; it returns the
// folder ("" when it serves none) for startWebFromHub.
func useWebFromHub(dataDir string) string {
	if !webFromHubOn() {
		return ""
	}
	dir := webFromHub(dataDir).Current()
	if dir != "" {
		_ = os.Setenv("RUNTIME_WEB_DIR", dir)
		agent.Log("info", "web: the page taken from the hub", agent.F{K: "dir", V: dir})
	}
	return dir
}

// startWebFromHub looks for a new page now and then; it returns at once.
func startWebFromHub(ctx context.Context, dataDir, serving string, on bool) {
	if !on {
		return
	}
	h := webFromHub(dataDir)
	var own *webrelease.Manifest
	if m, err := webrelease.Read(web.Dist()); err == nil {
		own = &m
	}
	go func() {
		for {
			v, changed, err := h.Sync(ctx, own, serving)
			switch {
			case errors.Is(err, webrelease.ErrNoHubPage):
			case err != nil && ctx.Err() == nil:
				agent.Log("warn", "web: the hub's page was not taken", agent.F{K: "err", V: err.Error()})
			case changed:
				agent.Log("info", "web: a new page from the hub", agent.F{K: "page", V: v})
				if webAutoRestart() {
					restartAfterUpdate([]string{"runtime"})
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(webFromHubEvery):
			}
		}
	}()
}

func webAutoRestart() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_AUTO_UPDATE"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}
