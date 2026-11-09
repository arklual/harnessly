package designkit

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings are a kit's own settings beside its versions, in <kits>/<id>/.settings.json: who made the kit and whether
// it is for viewing only. While ReadOnly is on, nobody edits the kit, its maker included, until the maker turns it off.
type Settings struct {
	// Owner is the email of the kit's maker; "" is the library's owner.
	Owner    string `json:"owner,omitempty"`
	ReadOnly bool   `json:"readOnly,omitempty"`
}

func settingsFile(dir, id string) string { return filepath.Join(dir, id, ".settings.json") }

// ReadSettings are the kit's settings; a kit without the file has the zero ones.
func ReadSettings(dir, id string) Settings {
	var s Settings
	if !ValidID(id) {
		return s
	}
	if raw, err := readLimited(settingsFile(dir, id), 64<<10); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// WriteSettings keeps the kit's settings (written whole, then moved into place).
func WriteSettings(dir, id string, s Settings) error {
	if !ValidID(id) {
		return os.ErrInvalid
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	file := settingsFile(dir, id)
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
