package profilesync

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// Options say whose profile syncs and how.
type Options struct {
	Home      string
	Workspace string
	// State is the HOME-relative folder of the sync's state, design-sync's identities and the reload mark
	// ("" = storelink.DefaultProfileState).
	State string
	// LocalKeys are settings keys that stay on the machine besides filesync.DefaultLocalKeys ("env.MY_ROOT").
	LocalKeys []string
	// Claude is the CLI the plugins are installed with ("" = claude on PATH).
	Claude string
	// Safe is the hub's safe, the person's secrets under Folder.
	Safe   Safe
	Folder string
	Link   *storelink.Link
	Log    *slog.Logger
}

// Run keeps the profile in step with its room until ctx ends: it waits for the room key, then syncs HOME and keeps the
// plugins the settings enable installed.
func Run(ctx context.Context, o Options) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	log := o.Log.With("part", "profile")
	if err := o.Link.Ready(ctx, 10*time.Minute); err != nil {
		return
	}
	if o.State == "" {
		o.State = storelink.DefaultProfileState
	}
	state := filepath.Join(o.Home, o.State)
	room := o.Link.Client(filestore.KindProfile)
	var key []byte
	for told := false; key == nil; {
		k, err := Key(ctx, o.Safe, o.Folder, room, Identities(state))
		switch {
		case err == nil:
			key = k
			continue
		case ctx.Err() != nil:
			return
		case errors.Is(err, ErrNoKey):
			if !told {
				log.Warn("the profile waits for its room key: a machine of the person with design-sync's identity puts it in the hub's safe when it runs")
				told = true
			}
		default:
			log.Warn("the profile's room key", "err", err)
		}
		if !sleep(ctx, 5*time.Minute) {
			return
		}
	}
	codec := &filesync.ProfileCodec{Home: strings.TrimRight(o.Home, "/"), Workspace: strings.TrimRight(o.Workspace, "/"),
		LocalKeys: append(append([]string(nil), filesync.DefaultLocalKeys...), o.LocalKeys...)}
	codec.SetKey(key)
	rec := &Reconciler{Home: o.Home, Claude: o.Claude, Log: log.With("part", "plugins")}
	kick := make(chan struct{}, 1)
	onApplied := func(rel string) {
		switch {
		case rel == ".claude/settings.json":
			select {
			case kick <- struct{}{}:
			default:
			}
		case strings.HasPrefix(rel, ".claude/skills/"), strings.HasPrefix(rel, ".claude/agents/"), strings.HasPrefix(rel, ".claude/commands/"):
			_ = Mark(state, false, true)
		}
	}
	go func() {
		// Settings that arrived (another machine enabled a plugin) and the regular pass: install, update, reload.
		tick := time.NewTicker(15 * time.Minute)
		defer tick.Stop()
		first := time.After(30 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-kick:
			case <-tick.C:
			case <-first:
			}
			changed, err := rec.Reconcile(ctx)
			if err != nil && ctx.Err() == nil {
				log.Warn("plugins", "err", err)
			}
			if changed {
				_ = Mark(state, true, false)
			}
		}
	}()
	log.Info("the profile syncs through the hub")
	o.Link.SyncProfile(ctx, storelink.Profile{Home: o.Home, State: o.State, Codec: codec, OnApplied: onApplied})
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
