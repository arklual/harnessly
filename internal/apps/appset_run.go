package apps

import (
	"context"
	"strings"
	"time"
)

// Run keeps the VM's apps in step with the person's set until ctx ends: a pass now, after every change of the registry
// or of an app's settings made here (the VM publishes it), when another VM puts something into the room (the hub's
// changes feed), and every ten minutes anyway.
func (s *AppSet) Run(ctx context.Context) {
	kick := make(chan struct{}, 1)
	poke := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	s.Reg.OnChange(poke)
	if s.Storage != nil {
		cancel := s.Storage.OnChange(func(c Change) {
			if c.Scope == ScopeInstall && c.Key == SettingsKey {
				poke()
			}
		})
		defer cancel()
	}
	go s.follow(ctx, poke)
	poke()
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-kick:
		case <-tick.C:
		}
		// A burst of writes (an install writes the list twice) is one pass.
		if !wait(ctx, 2*time.Second) {
			return
		}
		select {
		case <-kick:
		default:
		}
		rep, err := s.Reconcile(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			s.log().Warn("apps: the person's set not reconciled", "error", err)
			if !wait(ctx, 30*time.Second) {
				return
			}
			poke()
		default:
			for _, p := range rep.Problems {
				s.log().Warn("apps: the person's set", "problem", p)
			}
			if !rep.Empty() {
				s.log().Info("apps: the person's set reconciled", "published", rep.Published, "installed", rep.Installed,
					"updated", rep.Updated, "removed", rep.Removed, "changed", rep.Changed)
			}
		}
	}
}

// follow pokes when another VM writes into the room.
func (s *AppSet) follow(ctx context.Context, poke func()) {
	var since int64
	quiet := true
	for ctx.Err() == nil {
		hold := 25 * time.Second
		if quiet {
			hold = 0
		}
		ch, err := s.Room.Changes(ctx, since, hold)
		if err != nil {
			if !wait(ctx, 15*time.Second) {
				return
			}
			continue
		}
		for _, e := range ch.Entries {
			if !quiet && strings.HasPrefix(e.Path, setPrefix) && e.Author != s.Machine {
				poke()
			}
		}
		since = max(since, ch.Rev)
		if !ch.More {
			quiet = false
		}
	}
}
