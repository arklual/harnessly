package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/profilesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// profileSyncOn: the person's Claude profile syncs through the hub (RUNTIME_PROFILE_SYNC=hub, set by the machine's
// profile when profileSync: hub). Off, design-sync's profile agents keep it, as before.
func profileSyncOn() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("RUNTIME_PROFILE_SYNC")), "hub")
}

// startProfileSync keeps the person's Claude profile (HOME's .claude) in step with their room in the hub's store
// (internal/profilesync): only under a hub, with the room key from the hub's safe.
func startProfileSync(ctx context.Context, l *storelink.Link, dataDir string) func() {
	if !profileSyncOn() || l == nil || l.Kind() != storelink.KindHub {
		return func() {}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		slog.Warn("profile: no HOME to sync", "err", err)
		return func() {}
	}
	var workspace string
	for _, k := range []string{"RUNTIME_PROFILE_WORKSPACE", "RUNTIME_WORKSPACE"} {
		if workspace = os.Getenv(k); workspace != "" {
			break
		}
	}
	state := strings.TrimSpace(os.Getenv("RUNTIME_PROFILE_STATE_DIR"))
	if state != "" && !filepath.IsLocal(state) {
		slog.Warn("profile: RUNTIME_PROFILE_STATE_DIR must be a folder under HOME, the default is used", "dir", state)
		state = ""
	}
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		profilesync.Run(run, profilesync.Options{Home: home, Workspace: workspace, State: state,
			LocalKeys: splitList(os.Getenv("RUNTIME_PROFILE_LOCAL_KEYS")), Claude: os.Getenv("RUNTIME_PROFILE_CLAUDE"),
			Safe:   agent.LocalSafe{DataDir: dataDir, TokenHeader: os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")},
			Folder: agent.SafeOwnFolder, Link: l, Log: slog.Default()})
	}()
	return func() {
		stop()
		<-done
	}
}
