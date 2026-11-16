package looks

import (
	_ "embed"
	"encoding/json"
)

// presetsJSON is the same list the page shows (web/app/next/features/looks/presets.json; a test keeps them equal).
//
//go:embed presets.json
var presetsJSON []byte

// Preset is a look with a name, ready to be put on.
type Preset struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"nameEn"`
	Look   Look   `json:"look"`
}

// Presets is every preset, in the page's order.
func Presets() []Preset {
	var out []Preset
	if err := json.Unmarshal(presetsJSON, &out); err != nil {
		panic("looks: presets.json: " + err.Error())
	}
	return out
}

// PresetByID finds a preset.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
