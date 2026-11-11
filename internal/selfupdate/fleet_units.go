package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
)

// The units step of the fleet mode (docs/hub/fleet-rollout.md). A machine that runs one runtime per person
// has several systemd units for each of them, and a rollout used to write them with a script that asked systemd about
// every pair in turn: on a loaded machine one `systemctl` answers in 25-90 s, and a rollout of ten pairs did not fit
// into its quarter of an hour. The step does it as a whole: it renders the units from a spec the deployment brings,
// writes the files whose text changed, and asks systemd once to reload and once to enable what is new.

const unitsSchema = 1

// UnitsSpec says which unit files a machine has, as templates over its pairs.
type UnitsSpec struct {
	Schema int `json:"schema"`
	// Vars are available to every template as {{.name}}.
	Vars map[string]string `json:"vars,omitempty"`
	// Pairs are the people's pairs of runtimes. A pair with a Host is for the machine of that name only; the machine
	// takes its own pairs and the ones without a Host.
	Pairs []UnitsPair `json:"pairs,omitempty"`
	// Files are the unit files and drop-ins.
	Files []UnitsFile `json:"files"`
	// Remove are files (relative to the unit folder) that must not exist; they are removed and their units disabled.
	Remove []string `json:"remove,omitempty"`
}

// UnitsPair is one pair: its variables are available to the templates of a file marked PerPair, on top of Vars.
type UnitsPair struct {
	Host string            `json:"host,omitempty"`
	Vars map[string]string `json:"vars"`
}

// UnitsFile is one file of the unit folder. Path and Content are Go templates (text/template, a missing key is an
// error) over {{.version}}, {{.release}} (the folder current points at) and the variables of the spec and the pair.
// A file that renders to nothing (an empty Content, or a template whose condition is false) must not exist.
type UnitsFile struct {
	// Path is relative to the unit folder, like `runtime-gate-{{.user}}.service` or `x.service.d/memory.conf`.
	Path    string `json:"path"`
	Content string `json:"content"`
	// PerPair renders the file once for each pair of the machine.
	PerPair bool `json:"perPair,omitempty"`
	// Enable enables the unit when it is a new file (a service, socket or timer, not a drop-in).
	Enable bool `json:"enable,omitempty"`
}

// UnitsPlan is what a spec comes to on a machine: the files it wants and the ones it does not.
type UnitsPlan struct {
	Want   map[string]string
	Remove []string
	Enable []string
}

// ReadUnitsSpec reads and checks a spec file.
func ReadUnitsSpec(path string) (UnitsSpec, error) {
	var s UnitsSpec
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("units spec %s: %w", path, err)
	}
	if s.Schema != unitsSchema {
		return s, fmt.Errorf("units spec %s: schema must be %d", path, unitsSchema)
	}
	if len(s.Files) == 0 {
		return s, fmt.Errorf("units spec %s: no files", path)
	}
	return s, nil
}

// unitRel says that p is a path inside the unit folder.
func unitRel(p string) error {
	c := filepath.Clean(p)
	if p == "" || filepath.IsAbs(p) || c != p || c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return fmt.Errorf("unit path %q: inside the unit folder, no «..» and no extra slashes", p)
	}
	return nil
}

func renderUnit(name, text string, data map[string]string) (string, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Plan renders the spec for the machine named host. version and release are the new release's number and folder.
func (s UnitsSpec) Plan(host, version, release string) (UnitsPlan, error) {
	plan := UnitsPlan{Want: map[string]string{}}
	base := map[string]string{"version": version, "release": release, "host": host}
	for k, v := range s.Vars {
		base[k] = v
	}
	var pairs []map[string]string
	for _, p := range s.Pairs {
		if p.Host != "" && p.Host != host {
			continue
		}
		d := map[string]string{}
		for k, v := range base {
			d[k] = v
		}
		for k, v := range p.Vars {
			d[k] = v
		}
		pairs = append(pairs, d)
	}
	for _, f := range s.Files {
		sets := []map[string]string{base}
		if f.PerPair {
			sets = pairs
		}
		for _, d := range sets {
			rel, err := renderUnit(f.Path, f.Path, d)
			if err != nil {
				return plan, fmt.Errorf("unit path %q: %w", f.Path, err)
			}
			rel = strings.TrimSpace(rel)
			if rel == "" {
				continue
			}
			if err := unitRel(rel); err != nil {
				return plan, err
			}
			body, err := renderUnit(rel, f.Content, d)
			if err != nil {
				return plan, fmt.Errorf("unit %s: %w", rel, err)
			}
			if strings.TrimSpace(body) == "" {
				plan.Remove = append(plan.Remove, rel)
				continue
			}
			if !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			if _, dup := plan.Want[rel]; dup {
				return plan, fmt.Errorf("unit %s is rendered twice", rel)
			}
			plan.Want[rel] = body
			if f.Enable {
				plan.Enable = append(plan.Enable, rel)
			}
		}
	}
	for _, r := range s.Remove {
		if err := unitRel(r); err != nil {
			return plan, err
		}
		plan.Remove = append(plan.Remove, r)
	}
	for _, r := range plan.Remove {
		if _, wanted := plan.Want[r]; wanted {
			return plan, fmt.Errorf("unit %s is both wanted and removed", r)
		}
	}
	slices.Sort(plan.Remove)
	plan.Remove = slices.Compact(plan.Remove)
	slices.Sort(plan.Enable)
	return plan, nil
}

// UnitsChange is what a plan did to the unit folder, enough to put it back.
type UnitsChange struct {
	// Before holds the text each touched file had; nil means it did not exist.
	Before map[string]*string
	// Written and Removed are the files that changed.
	Written, Removed []string
	// Enable are the new units that are to be enabled.
	Enable []string
}

// Empty says nothing in the folder changed.
func (c UnitsChange) Empty() bool { return len(c.Written)+len(c.Removed) == 0 }

// ApplyUnits writes the plan into dir: only the files whose text differs, each through a temporary file and a rename.
// With dry nothing is written and the change says what would be. A file the plan removes is removed only if it exists.
func ApplyUnits(dir string, plan UnitsPlan, dry bool) (UnitsChange, error) {
	ch := UnitsChange{Before: map[string]*string{}}
	names := make([]string, 0, len(plan.Want))
	for n := range plan.Want {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, rel := range names {
		path := filepath.Join(dir, rel)
		old, err := os.ReadFile(path)
		switch {
		case err == nil && string(old) == plan.Want[rel]:
			continue
		case err == nil:
			s := string(old)
			ch.Before[rel] = &s
		case errors.Is(err, os.ErrNotExist):
			ch.Before[rel] = nil
			if slices.Contains(plan.Enable, rel) {
				ch.Enable = append(ch.Enable, rel)
			}
		default:
			return ch, err
		}
		ch.Written = append(ch.Written, rel)
		if dry {
			continue
		}
		if err := writeUnit(path, plan.Want[rel]); err != nil {
			return ch, err
		}
	}
	for _, rel := range plan.Remove {
		path := filepath.Join(dir, rel)
		old, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return ch, err
		}
		s := string(old)
		ch.Before[rel] = &s
		ch.Removed = append(ch.Removed, rel)
		if dry {
			continue
		}
		if err := os.Remove(path); err != nil {
			return ch, err
		}
	}
	return ch, nil
}

func writeUnit(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Restore puts the unit folder back as the change found it.
func (c UnitsChange) Restore(dir string) error {
	var first error
	for rel, before := range c.Before {
		path := filepath.Join(dir, rel)
		var err error
		if before == nil {
			err = os.Remove(path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else {
			err = writeUnit(path, *before)
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// systemctl runs systemctl with args; the Fleet's Systemctl replaces it in tests.
func systemctl(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// units applies the machine's spec for the release in dir: one daemon-reload when anything changed, one
// `enable --no-reload` for all the new units. It returns what it changed so a failed activation can put it back.
func (f *Fleet) units(ctx context.Context, version, release string) (UnitsChange, error) {
	if f.Units == nil {
		return UnitsChange{}, nil
	}
	dir := f.UnitDir
	if dir == "" {
		dir = "/etc/systemd/system"
	}
	plan, err := f.Units.Plan(f.Host, version, release)
	if err != nil {
		return UnitsChange{}, err
	}
	ch, err := ApplyUnits(dir, plan, false)
	if err != nil {
		_ = ch.Restore(dir)
		return UnitsChange{}, err
	}
	if ch.Empty() {
		f.say("the units are as the spec wants them")
		return ch, nil
	}
	f.say("units: %d written, %d removed", len(ch.Written), len(ch.Removed))
	if err := f.reloadUnits(ctx, ch); err != nil {
		_ = ch.Restore(dir)
		_ = f.systemctl(ctx, "daemon-reload")
		return UnitsChange{}, err
	}
	return ch, nil
}

func (f *Fleet) systemctl(ctx context.Context, args ...string) error {
	if f.Systemctl != nil {
		return f.Systemctl(ctx, args...)
	}
	return systemctl(ctx, args...)
}

// ReloadUnits asks systemd once to reload and once to enable the new units of a change.
func (f *Fleet) ReloadUnits(ctx context.Context, ch UnitsChange) error { return f.reloadUnits(ctx, ch) }

func (f *Fleet) reloadUnits(ctx context.Context, ch UnitsChange) error {
	if err := f.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	var enable []string
	for _, rel := range ch.Enable {
		if !strings.Contains(rel, "/") { // a drop-in lives in <unit>.d/ and is never enabled
			enable = append(enable, rel)
		}
	}
	if len(enable) == 0 {
		return nil
	}
	// A failure to enable a new unit is not a failure of the release: the unit works until the next boot.
	if err := f.systemctl(ctx, append([]string{"enable", "--no-reload"}, enable...)...); err != nil {
		f.say("units not enabled: %v", err)
	}
	return nil
}

// restoreUnits puts the units of a failed activation back and reloads systemd once.
func (f *Fleet) restoreUnits(ctx context.Context, ch UnitsChange) {
	if ch.Empty() {
		return
	}
	dir := f.UnitDir
	if dir == "" {
		dir = "/etc/systemd/system"
	}
	if err := ch.Restore(dir); err != nil {
		f.say("after rollback the units are not all back: %v", err)
	}
	if err := f.systemctl(ctx, "daemon-reload"); err != nil {
		f.say("after rollback: %v", err)
	}
}
