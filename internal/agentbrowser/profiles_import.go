package agentbrowser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Import publishes a copied Chrome directory only after the copy succeeds.
// Until publication the random ID cannot be selected or started by another page.
func (s *ProfileStore) Import(ctx context.Context, source, name, color string) (string, error) {
	pending := profileData{Profiles: map[string]Profile{}}
	profile, err := addProfile(&pending, name, color)
	if err != nil {
		return "", err
	}
	if err = s.Err(); err != nil {
		return "", err
	}
	profiles, _ := s.Snapshot()
	for _, existing := range profiles {
		if existing.Name == profile.Name {
			return "", errors.New("A browser profile already has this name")
		}
	}
	dir := filepath.Join(filepath.Dir(s.path), "profiles", profile.ID)
	warning, err := ImportChrome(ctx, source, filepath.Join(dir, "chrome"))
	if err == nil {
		err = s.change(func(d *profileData) error {
			for _, existing := range d.Profiles {
				if existing.Name == profile.Name {
					return errors.New("A browser profile already has this name")
				}
			}
			d.Profiles[profile.ID] = profile
			return nil
		})
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return warning, nil
}
