package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/selfupdate"
)

// Updates of a machine a hub carries (internal/selfupdate):
//
//	GET  <platform>/update        (routes_update.go) carries `machine`: the state of this machine's programs
//	POST <platform>/update/apply  a page session: check now and, when something differs, replace it and restart
//
// install.sh puts harnessly-runtime, harnessly-agent and the harnessly command side by side. Where this runtime may write there (an install of
// the person's own account, ~/.local/bin), it checks the hub's files every hour and puts the new ones in place by
// itself; a system install (/usr/local/bin) or a container cannot, and the page shows the command that updates it.
// RUNTIME_AUTO_UPDATE=0 keeps the checks but leaves the update to the button or the command.

// exitRestart is the runtime's exit code after an update: not 0, so a unit with Restart=on-failure starts it again.
const exitRestart = 75

// newSelfUpdater is the updater of a runtime a hub carries and install.sh installed; nil for any other runtime.
func newSelfUpdater() *selfupdate.Updater {
	hub := strings.TrimRight(strings.TrimSpace(os.Getenv("RUNTIME_HUB_URL")), "/")
	if hub == "" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	if filepath.Base(exe) != "harnessly-runtime" {
		return nil
	}
	dir := filepath.Dir(exe)
	files := []selfupdate.File{{Name: "runtime", Path: exe}, {Name: "harnessly", Path: filepath.Join(dir, "harnessly"), Optional: true}}
	if _, err := os.Stat(filepath.Join(dir, "harnessly-agent")); err == nil {
		files = append([]selfupdate.File{{Name: "agent", Path: filepath.Join(dir, "harnessly-agent")}}, files...)
	}
	auto := true
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_AUTO_UPDATE"))) {
	case "0", "off", "false", "no":
		auto = false
	}
	return &selfupdate.Updater{
		HubURL: hub, Files: files, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Auto: auto,
		Sudo:    strings.HasPrefix(dir, "/usr/"),
		Applied: restartAfterUpdate,
		Log: func(msg string, err error) {
			agent.Log("warn", msg, agent.F{K: "error", V: err.Error()})
		},
	}
}

// restartAfterUpdate restarts what was replaced: the agent by a signal (its service starts it again), the runtime by
// its exit. tmux and the agents in it are not the runtime's service's to stop, so they keep working.
func restartAfterUpdate(names []string) {
	agent.Log("info", "updated from the hub; restarting", agent.F{K: "programs", V: strings.Join(names, ",")})
	for _, n := range names {
		if n == "agent" {
			_ = exec.Command("pkill", "-TERM", "-U", strconv.Itoa(os.Getuid()), "-x", "harnessly-agent").Run()
		}
	}
	for _, n := range names {
		if n == "runtime" {
			// A moment for the answer of the button to reach the page.
			go func() {
				time.Sleep(time.Second)
				os.Exit(exitRestart)
			}()
		}
	}
}

func selfUpdateRoutes(d deps, u *selfupdate.Updater) []gate.Route {
	if u == nil {
		return nil
	}
	return []gate.Route{{
		Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/update/apply") },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			if !d.SessionOK(r) {
				peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
			defer cancel()
			// What went wrong is in the status (error), which the page shows as it is.
			if err := u.Apply(ctx); err != nil && !errors.Is(err, selfupdate.ErrBusy) {
				agent.Log("warn", "update from the page failed", agent.F{K: "error", V: err.Error()})
			}
			peerJSON(w, http.StatusOK, u.Status())
		}),
	}}
}
