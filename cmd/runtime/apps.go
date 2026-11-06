package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

// appGuardCommand is the app-guard hook's command line: HARNESSLY_BIN when the deployment names the CLI, else the CLI
// shipped beside the runtime (a machine's package holds both, and the runtime's own PATH may not reach the agents'
// one), otherwise harnessly off the agents' PATH.
func appGuardCommand() string {
	bin := os.Getenv("HARNESSLY_BIN")
	if bin == "" {
		bin = besideRuntime(os.Args[0], os.Executable)
	}
	if bin != "" {
		return agents.ShellQuote(bin) + " hook app-guard"
	}
	return ""
}

// besideRuntime is the harnessly CLI in the runtime's own folder, "" when there is none. The folder is the one the
// runtime was started from (arg0, as a deployment's `current` link names it, so a chat outlives a release swap), or
// its resolved executable when arg0 is not a path.
func besideRuntime(arg0 string, executable func() (string, error)) string {
	dir := ""
	if filepath.IsAbs(arg0) {
		dir = filepath.Dir(arg0)
	} else if exe, err := executable(); err == nil {
		dir = filepath.Dir(exe)
	}
	if dir == "" {
		return ""
	}
	cli := filepath.Join(dir, "harnessly")
	if st, err := os.Stat(cli); err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return ""
	}
	return cli
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

// alwaysApps is convo.Deps.AlwaysApps over the machine's registry: the installed apps that ask to be on in every new
// chat (`activation.on: ["always"]`) and work in one, in the registry's order. The person turns one off in a chat with
// the page's switch, as any other.
func alwaysApps(r *apps.Registry) func() []string {
	return func() []string {
		list, err := r.List()
		if err != nil {
			return nil
		}
		var out []string
		for _, a := range list {
			// Only an app the person installed themselves: a team's app is put on every member's machine by an owner, and
			// would put every chat of theirs inside the wall without their say.
			if !a.Team && a.Manifest != nil && a.Manifest.HasTarget(apps.TargetChat) && a.Manifest.ActivatesOn(apps.OnAlways) {
				out = append(out, a.ID)
			}
		}
		return out
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

// probeAppGuard runs the app-guard hook once, as Claude would, with a call it has no reason to stop: the command must
// start, finish in time and not answer that it could not read its rules.
func probeAppGuard(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	// The input is the agent's own: Codex's guard (--agent codex) reads Codex's tool names.
	probe := `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/"}}`
	if strings.Contains(command, " --agent codex") {
		probe = `{"hook_event_name":"PreToolUse","tool_name":"update_plan","tool_input":{},"cwd":"/"}`
	}
	cmd.Stdin = strings.NewReader(probe)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("no answer in 8 s")
		}
		return err
	}
	if strings.Contains(out.String(), "could not read") {
		return fmt.Errorf("it cannot read the rules of the launch: %s", strings.TrimSpace(out.String()))
	}
	return nil
}
