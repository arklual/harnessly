package preflight

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AntigravityModel is a model actually reported by agy models.
type AntigravityModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Antigravity keeps authentication evidence separate from executable discovery.
type Antigravity struct {
	Installed  bool               `json:"installed"`
	Version    *string            `json:"version"`
	BinaryPath *string            `json:"binaryPath"`
	LoggedIn   bool               `json:"loggedIn"`
	Uncertain  bool               `json:"uncertain,omitempty"`
	Models     []AntigravityModel `json:"models"`
}

var antigravityModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

func parseAntigravityModels(out string) []AntigravityModel {
	models := []AntigravityModel{}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		id, label, ok := strings.Cut(strings.TrimSpace(line), "\t")
		id, label = strings.TrimSpace(id), strings.TrimSpace(label)
		if !ok || !antigravityModelID.MatchString(id) || label == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, AntigravityModel{ID: id, Label: label})
	}
	return models
}

// SetAntigravityBinary selects the launcher's binary for native probes. Empty uses
// agy on the login PATH, then ~/.local/bin; an explicit binary never falls back.
// Shared Checkers drop both cached answers only when the selection changes.
func (c *Checker) SetAntigravityBinary(binary string) {
	c.antigravityMu.Lock()
	defer c.antigravityMu.Unlock()
	if c.antigravityBinary == binary {
		return
	}
	c.antigravityBinary, c.antigravity = binary, nil
	c.mu.Lock()
	c.result, c.inflight = nil, nil
	c.mu.Unlock()
}

// AntigravityStatus shares the runtime's minute-cached, bounded native CLI probe.
func (c *Checker) AntigravityStatus() Antigravity { return c.antigravityStatus(c.shellPath()) }

func (c *Checker) antigravityStatus(path string) Antigravity {
	c.antigravityMu.Lock()
	defer c.antigravityMu.Unlock()
	clone := func(r Antigravity) Antigravity {
		r.Models = append([]AntigravityModel{}, r.Models...)
		return r
	}
	if c.antigravity != nil && time.Since(c.antigravityAt) < time.Minute {
		return clone(*c.antigravity)
	}
	r := Antigravity{Models: []AntigravityModel{}}
	name := c.antigravityBinary
	if name == "" {
		name = "agy"
	}
	bin, err := lookPath(name, path)
	if err != nil && c.antigravityBinary == "" {
		// The official installer uses this directory even when the login PATH omits it.
		dir := filepath.Join(c.Home, ".local", "bin")
		bin, err = lookPath("agy", dir)
		if err == nil {
			path = dir + string(filepath.ListSeparator) + path
		}
	}
	if err == nil {
		r.BinaryPath = &bin
		out, versionErr := c.cliVersion(bin, path)
		r.Installed, r.Version = versionErr == nil, Version(out)
		if errors.Is(versionErr, errTimedOut) {
			r.Uncertain = true
		}
		if r.Installed {
			out, probeErr := run(bin, c.withPath(path), path, "models")
			if probeErr == nil {
				r.Models = parseAntigravityModels(out)
				r.LoggedIn = len(r.Models) > 0
			} else if errors.Is(probeErr, errTimedOut) {
				r.Uncertain = true
				if c.antigravity != nil && c.antigravity.LoggedIn {
					r.LoggedIn = true
					r.Models = clone(*c.antigravity).Models
				}
			}
		}
	}
	c.antigravity, c.antigravityAt = &r, time.Now()
	return clone(r)
}
