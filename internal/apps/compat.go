package apps

import (
	"fmt"
	"path/filepath"
	"sort"
)

// Compatibility of the installed extensions with a harnessly version, asked before the core is upgraded:
// `harnessly app compat --host 0.90.0` and, for a script that upgrades, the same exit code. An app that does not allow
// the version is an error; so is one whose package cannot be read or whose stored data could not be taken from its
// version to the one the package declares. An app that is switched off is checked too, but only warns, since nothing
// of it runs.

// CompatIssue is one thing that would stop an extension after the upgrade.
type CompatIssue struct {
	Warning bool   `json:"warning,omitempty"`
	Message string `json:"message"`
}

// CompatApp is one app's result.
type CompatApp struct {
	ID       string        `json:"id"`
	Version  string        `json:"version"`
	Kind     string        `json:"kind"`
	Disabled bool          `json:"disabled,omitempty"`
	Range    string        `json:"harnessly"`
	Issues   []CompatIssue `json:"issues,omitempty"`
}

// OK says whether the app has no error.
func (a CompatApp) OK() bool {
	for _, i := range a.Issues {
		if !i.Warning {
			return false
		}
	}
	return true
}

// CompatReport is the check of every installed app.
type CompatReport struct {
	Host string      `json:"host"`
	Apps []CompatApp `json:"apps"`
	OK   bool        `json:"ok"`
}

// Compat checks the apps of this machine against a harnessly version.
func (r *Registry) Compat(host string) (CompatReport, error) {
	all, err := r.ListAll()
	if err != nil {
		return CompatReport{}, err
	}
	return CheckCompat(all, host, OpenStorage(filepath.Dir(r.Root))), nil
}

// CheckCompat is Compat over a list; st may be nil (stored data is not looked at).
func CheckCompat(list []Installed, host string, st *Storage) CompatReport {
	rep := CompatReport{Host: host, OK: true, Apps: []CompatApp{}}
	for _, a := range list {
		if a.Manifest == nil {
			continue
		}
		m := a.Manifest
		res := CompatApp{ID: a.ID, Version: a.Version, Kind: m.ExtensionKind(), Disabled: a.Off || a.Muted, Range: m.Harnessly}
		add := func(format string, args ...any) {
			res.Issues = append(res.Issues, CompatIssue{Warning: res.Disabled, Message: fmt.Sprintf(format, args...)})
		}
		if rng, err := ParseRange(m.Harnessly); err != nil {
			add("the harnessly field is not a range: %v", err)
		} else if !rng.Allows(host) {
			add("wants harnessly %s, the upgrade is to %s", m.Harnessly, host)
		}
		pkg, ps := LoadPackage(a.Dir)
		switch {
		case pkg == nil:
			add("the package cannot be read: %s", ps.Error())
		case len(ps.Errors()) > 0:
			add("the package has errors: %s", ps.Errors().Error())
		case m.Data != nil && st != nil:
			if have := st.DataVersion(a.ID); have > m.Data.Version {
				add("the stored data is at version %d, the package at %d", have, m.Data.Version)
			}
		}
		sort.SliceStable(res.Issues, func(i, j int) bool { return !res.Issues[i].Warning && res.Issues[j].Warning })
		if !res.OK() {
			rep.OK = false
		}
		rep.Apps = append(rep.Apps, res)
	}
	sort.Slice(rep.Apps, func(i, j int) bool { return rep.Apps[i].ID < rep.Apps[j].ID })
	return rep
}
