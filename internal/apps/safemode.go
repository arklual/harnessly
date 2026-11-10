package apps

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Safe mode (HRN-700): one switch turns off every app but the ones the organization's environment pins, for the chats
// that start from then on. A running chat keeps what it started with until it restarts: stopping the person's work is
// not the switch's to do. The switch is a file in the registry's folder, so every process of the machine reads the same.

func (r *Registry) safeFile() string { return filepath.Join(r.Root, "safe-mode") }

// SafeMode says whether safe mode is on.
func (r *Registry) SafeMode() bool {
	_, err := os.Stat(r.safeFile())
	return err == nil
}

// SetSafeMode turns safe mode on or off.
func (r *Registry) SetSafeMode(on bool) error {
	if !on {
		if err := os.Remove(r.safeFile()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(r.Root, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(r.safeFile(), []byte("on\n"))
}

// SafeAllows says whether an install works in safe mode: only one the environment pins.
func SafeAllows(a Installed) bool { return a.Env && a.Locked }

// Active are the apps of a launch that are installed and on now; in safe mode only the pinned ones. A chat whose app
// was removed starts without it instead of not starting.
func (l *Launcher) Active(ids []string) []string {
	list, err := l.Registry.List()
	if err != nil {
		return ids
	}
	safe := l.Registry.SafeMode()
	out := []string{}
	for _, id := range ids {
		i := slices.IndexFunc(list, func(a Installed) bool { return a.ID == id })
		if i < 0 || (safe && !SafeAllows(list[i])) {
			continue
		}
		out = append(out, id)
	}
	return out
}
