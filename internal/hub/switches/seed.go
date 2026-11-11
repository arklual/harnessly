package switches

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SeedBy is who a row written from the instance's configuration is signed by.
const SeedBy = "config:HUB_SWITCHES"

// Seed is one row of HUB_SWITCHES: a feature on or off for a person on a machine; empty person or machine is "*".
type Seed struct {
	Feature string `json:"feature"`
	Person  string `json:"person,omitempty"`
	Machine string `json:"machine,omitempty"`
	Enabled bool   `json:"enabled"`
	Note    string `json:"note,omitempty"`
}

// ParseSeeds reads HUB_SWITCHES, a JSON array of Seed.
func ParseSeeds(raw string) ([]Seed, error) {
	var rows []Seed
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	for i, r := range rows {
		if strings.TrimSpace(r.Feature) == "" {
			return nil, fmt.Errorf("row %d: feature is empty", i)
		}
	}
	return rows, nil
}

// Apply writes the rows the instance's configuration names, on every start: the repository that holds the
// configuration is where these rows are changed, so a row changed by hand in the admin screen goes back to it on the
// next start. Rows the configuration does not name are left as they are.
func (s *Store) Apply(ctx context.Context, rows []Seed) (int, error) {
	for _, r := range rows {
		if err := s.Set(ctx, r.Feature, r.Person, r.Machine, r.Enabled, r.Note, SeedBy); err != nil {
			return 0, fmt.Errorf("%s/%s/%s: %w", r.Feature, r.Person, r.Machine, err)
		}
	}
	return len(rows), nil
}
