// Package release is the signed manifest of a release (schema 2): which hub, runtime, agent, command line tool and web
// page belong together, the hashes of their files, and the ranges of the extension protocols this core speaks. It is
// what `harnessly upgrade plan/apply` reads (docs/hub/upgrade.md).
//
// /dl/manifest.json (files and hashes only, read by internal/selfupdate) is unchanged and stays the default way a
// machine follows its hub; the release manifest is a separate file, /dl/release.json, with its detached signature.
package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Schema is the version of this file format.
const Schema = 2

// Component names. UI is part of the runtime binary (the webui build tag) and has no file of its own.
const (
	Hub     = "hub"
	Runtime = "runtime"
	Agent   = "agent"
	CLI     = "cli"
	UI      = "ui"
)

// Components lists the names a manifest may hold.
var Components = []string{Hub, Runtime, Agent, CLI, UI}

// File is one downloadable file.
type File struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Component is one part of the release.
type Component struct {
	Version string `json:"version"`
	// Files by download name (`runtime-linux-amd64`); empty for a part carried inside another (the UI).
	Files map[string]File `json:"files,omitempty"`
	// Requires: the least version of other components this one works with, by component name.
	Requires map[string]string `json:"requires,omitempty"`
}

// Range is the extension protocol majors this core speaks, both ends included.
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Data is what the release does to stored data. The binary of a release can be put back; the data it migrated cannot.
type Data struct {
	// SchemaVersion is the newest migration the hub applies.
	SchemaVersion int `json:"schema_version"`
	// Additive: the migrations since MinFrom only add (a table, a column); an older binary keeps working on the data.
	Additive bool `json:"additive"`
	// MinFrom is the oldest release this one upgrades from directly; empty means any.
	MinFrom string `json:"min_from,omitempty"`
}

// Manifest is the file.
type Manifest struct {
	Schema     int                  `json:"schema"`
	Version    string               `json:"version"`
	Components map[string]Component `json:"components"`
	// ExtensionAPI: for each extension protocol (`harnessly.machine-provider`) the majors this core speaks.
	ExtensionAPI map[string]Range `json:"extension_api,omitempty"`
	Data         Data             `json:"data"`
}

// Parse reads and validates a manifest.
func Parse(body []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("release manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate checks the manifest is whole and that its parts agree with each other.
func (m *Manifest) Validate() error {
	if m.Schema != Schema {
		return fmt.Errorf("release manifest: schema %d, this program reads %d", m.Schema, Schema)
	}
	if _, ok := ParseVersion(m.Version); !ok {
		return fmt.Errorf("release manifest: version %q is not X.Y.Z", m.Version)
	}
	if len(m.Components) == 0 {
		return errors.New("release manifest: no components")
	}
	for name, c := range m.Components {
		if !known(name) {
			return fmt.Errorf("release manifest: unknown component %q", name)
		}
		if _, ok := ParseVersion(c.Version); !ok {
			return fmt.Errorf("release manifest: %s version %q is not X.Y.Z", name, c.Version)
		}
		for key, f := range c.Files {
			if !sha256Hex.MatchString(f.SHA256) || f.Size <= 0 {
				return fmt.Errorf("release manifest: %s has a bad hash or size", key)
			}
			if !strings.HasPrefix(key, fileStem(name)+"-") {
				return fmt.Errorf("release manifest: file %s does not belong to %s", key, name)
			}
		}
		for dep, min := range c.Requires {
			d, ok := m.Components[dep]
			if !ok {
				return fmt.Errorf("release manifest: %s requires %s, which the release does not hold", name, dep)
			}
			need, ok := ParseVersion(min)
			if !ok {
				return fmt.Errorf("release manifest: %s requires %s %q, not X.Y.Z", name, dep, min)
			}
			have, _ := ParseVersion(d.Version)
			if have.Less(need) {
				return fmt.Errorf("release manifest: %s requires %s %s, the release has %s", name, dep, min, d.Version)
			}
		}
	}
	for p, r := range m.ExtensionAPI {
		if !protoName.MatchString(p) || r.Min < 1 || r.Max < r.Min {
			return fmt.Errorf("release manifest: extension_api %q has a bad name or range", p)
		}
	}
	return nil
}

func known(name string) bool {
	for _, c := range Components {
		if c == name {
			return true
		}
	}
	return false
}

// fileStem is the prefix of the download names of a component: the command line tool is `harnessly-<os>-<arch>`.
func fileStem(component string) string {
	if component == CLI {
		return "harnessly"
	}
	return component
}

// FileKey is the download name of a component for a platform.
func FileKey(component, goos, goarch string) string {
	return fileStem(component) + "-" + goos + "-" + goarch
}

// Names returns the component names of the manifest, in a fixed order.
func (m *Manifest) Names() []string {
	out := make([]string, 0, len(m.Components))
	for _, n := range Components {
		if _, ok := m.Components[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// Version is X.Y.Z.
type Version struct{ Major, Minor, Patch int }

var semver = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:[-+][0-9A-Za-z.-]+)?$`)

// ParseVersion reads X.Y.Z (a leading v and a suffix after - or + are allowed and ignored).
func ParseVersion(s string) (Version, bool) {
	g := semver.FindStringSubmatch(strings.TrimSpace(s))
	if g == nil {
		return Version{}, false
	}
	n := func(i int) int { v, _ := strconv.Atoi(g[i]); return v }
	return Version{n(1), n(2), n(3)}, true
}

// Less reports v < o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// SortedKeys is a helper for stable output.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
