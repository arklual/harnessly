package apps

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// The life of an installed extension: install and update are Install; here are the other two ends. Switching
// off keeps everything the app has (package, settings, secrets, data) and only takes it out of what is on; removing
// takes the package, and with purge also what the core kept for it.

// SetDisabled switches an installed app off or on. The team's apps are the team's: a person turns them off for
// themselves (Mute), they are not disabled here.
func (r *Registry) SetDisabled(id string, off bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list, err := r.read()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(a Installed) bool { return a.ID == id })
	if i < 0 {
		if r.teamHas(id) {
			return ErrTeamApp
		}
		return ErrNotInstalled
	}
	if list[i].Off == off {
		return nil
	}
	list[i].Off = off
	return r.write(list)
}

// Uninstall removes the app; with purge it also forgets its settings, its secrets in the safe and its stored data. The
// safe may be nil (nothing in it is touched). Without purge they stay for a later install of the same app.
func (r *Registry) Uninstall(id string, purge bool, safe *secrets.Store) error {
	a, err := r.Find(id)
	if err != nil {
		return err
	}
	if err := r.Remove(id); err != nil {
		return err
	}
	if !purge {
		return nil
	}
	data := filepath.Dir(r.Root)
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	keep(OpenStorage(data).Purge(id))
	if safe != nil && a.Manifest != nil {
		for _, s := range a.Manifest.Secrets {
			// A secret the person never set is not there to forget.
			if err := safe.RemoveApp(id, s.Name); err != nil && secrets.ReasonOf(err) != secrets.ReasonNotFound {
				keep(err)
			}
		}
	}
	keep(os.RemoveAll(filepath.Join(r.Root, "launch", id)))
	return first
}
