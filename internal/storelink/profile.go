package storelink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
)

// Profile is how a person's Claude profile syncs with its room in the hub (filestore.Profile): HOME is the
// folder, the codec seals with the room key, OnApplied hears every file another machine brought.
type Profile struct {
	Home string
	// State is the HOME-relative folder the sync keeps its staging, the edits that lost and its lock in: never synced
	// ("" = DefaultProfileState).
	State     string
	Codec     *filesync.ProfileCodec
	OnApplied func(rel string)
}

// DefaultProfileState is the profile sync's state folder under HOME when the runtime names none
// (RUNTIME_PROFILE_STATE_DIR).
const DefaultProfileState = ".local/state/profile-sync"

// SyncProfile keeps the person's profile in step with its room until ctx ends. One runtime of the machine syncs a
// HOME at a time; the others wait.
func (l *Link) SyncProfile(ctx context.Context, p Profile) {
	if err := l.Ready(ctx, 10*time.Minute); err != nil {
		return
	}
	for ctx.Err() == nil {
		err := l.syncProfileOnce(ctx, p)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, errBusy):
			l.busyOnce(p.Home)
			if !sleep(ctx, time.Minute) {
				return
			}
		default:
			l.log.Warn("store: the profile's sync stopped, starting again", "err", err)
			if !sleep(ctx, 10*time.Second) {
				return
			}
		}
	}
}

func (l *Link) syncProfileOnce(ctx context.Context, p Profile) error {
	rel := p.State
	if rel == "" {
		rel = DefaultProfileState
	}
	state := filepath.Join(p.Home, rel)
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	unlock, err := lockFolder(state, "store-sync.lock")
	if err != nil {
		return err
	}
	defer unlock()
	a, err := filesync.New(filesync.Config{Root: p.Home, StateDir: filepath.Join(l.dataDir, "store-sync", l.stateKey, "profile-"+short(p.Home)),
		Store: l.Client(filestore.KindProfile), Machine: l.name, Policy: filestore.Profile{}, Codec: p.Codec,
		TmpDir: rel + "/tmp", ConflictDir: rel + "/conflicts", OnApplied: p.OnApplied, Log: l.log})
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Run(ctx)
}
