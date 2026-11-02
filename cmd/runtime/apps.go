package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/apps"
)

// SandboxExec is the runtime's subcommand bwrap starts the agent's CLI through (apps.ForegroundExec).
const SandboxExec = "sandbox-exec"

// appBoundary is the machine's one wall around the agents of chats with apps (internal/apps/boundary.go): the launches,
// the machine check and the apps' page read the same probe. bwrap is looked up on the runtime's PATH; the helper is
// this binary itself, so it needs nothing else on the agent's PATH.
var appBoundary = sync.OnceValue(func() *apps.Boundary {
	b := &apps.Boundary{}
	if path, err := exec.LookPath("bwrap"); err == nil {
		b.Bwrap = path
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		b.Helper = []string{exe, SandboxExec}
	}
	return b
})

// appGuardCommand is the app-guard hook's command line: HARNESSLY_BIN when the deployment names the CLI, otherwise
// harnessly off the agents' PATH.
func appGuardCommand() string {
	if bin := os.Getenv("HARNESSLY_BIN"); bin != "" {
		return agents.ShellQuote(bin) + " hook app-guard"
	}
	return ""
}

// appsOK is convo.Deps.AppsOK over the machine's registry: an app turns on in a chat when it is installed and works in
// one.
func appsOK(r *apps.Registry) func(ids []string) string {
	return func(ids []string) string {
		list, err := r.List()
		if err != nil {
			return ids[0]
		}
		byID := make(map[string]apps.Installed, len(list))
		for _, a := range list {
			byID[a.ID] = a
		}
		for _, id := range ids {
			a, ok := byID[id]
			if !ok || a.Manifest == nil || !a.Manifest.HasTarget(apps.TargetChat) {
				return id
			}
		}
		return ""
	}
}

// agentWalled says whether the agents of chats with apps run inside the wall, for the apps' «О приложении»: the last
// launch's probe, or a probe now when no chat with apps has started yet.
var agentWalled = func(root string) bool {
	b := appBoundary()
	if probed, err := b.State(); probed {
		return err == nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return false
	}
	return b.Ready(root) == nil
}
