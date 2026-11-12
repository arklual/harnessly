// Package fleetdriver rolls one runtime release out to a list of machines in waves: it uploads the tarball, stages it
// everywhere first (nothing that runs changes), then activates the machines wave by wave with `harnessly fleet
// activate` on each, checks them, and puts the machines already moved back with `harnessly fleet rollback` when a
// wave fails. The machine's own side (stage, activate, rollback, delta, the units spec) is internal/selfupdate; this is
// the part that used to be a shell script in a deployment's repository. It knows no machine: the list, the way in and
// the settings of each machine come from the fleet file.
package fleetdriver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Fleet is the fleet file (schema 1).
type Fleet struct {
	Schema int `json:"schema"`
	// SSH is the command that runs another command on a machine: `<SSH...> <target> <command>`, with stdin passed
	// through. Empty: the caller's default (`ssh`).
	SSH []string `json:"ssh,omitempty"`
	// Transfers is the folder on a machine the tarball is uploaded to.
	Transfers string `json:"transfers,omitempty"`
	// Binary is the harnessly command on a machine (a path or a name on its PATH).
	Binary string `json:"binary,omitempty"`
	Hosts  []Host `json:"hosts"`
}

// Host is one machine.
type Host struct {
	// Name identifies the machine in `--only` and in the log.
	Name string `json:"name"`
	// Target is the first argument of the ssh command: `root@name`, a Teleport node and so on.
	Target string `json:"target"`
	// Wave orders the rollout: waves go in ascending order, the machines of one wave at the same time. A machine
	// without a wave is in wave 0, the first.
	Wave int `json:"wave,omitempty"`
	// EnvFile is a shell file on the machine with the fleet settings (RUNTIME_FLEET_ROOT, _RESTART, _HEALTH_URL,
	// _SERVICES, _UNITS_SPEC ...). It is sourced before every fleet command.
	EnvFile string `json:"envFile,omitempty"`
	// Root overrides RUNTIME_FLEET_ROOT for this machine (`--root`).
	Root string `json:"root,omitempty"`
	// Binary overrides Fleet.Binary.
	Binary string `json:"binary,omitempty"`
}

var (
	reName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	rePath    = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)
	reVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reSHA     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reCommit  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// reTarget lets a target carry user@host or a bare node name, never a space or a shell character.
	reTarget = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+$`)
)

// Load reads and checks the fleet file.
func Load(path string) (*Fleet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Fleet
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("fleet file %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("fleet file %s: %w", path, err)
	}
	return &f, nil
}

// Validate checks everything that ends up in a command line on a machine.
func (f *Fleet) Validate() error {
	if f.Schema != 1 {
		return errors.New(`"schema" must be 1`)
	}
	if len(f.Hosts) == 0 {
		return errors.New("no machines")
	}
	if f.Transfers != "" && !rePath.MatchString(f.Transfers) {
		return fmt.Errorf("bad transfers folder %q", f.Transfers)
	}
	if f.Binary != "" && !rePath.MatchString(f.Binary) {
		return fmt.Errorf("bad binary %q", f.Binary)
	}
	seen := map[string]bool{}
	for _, h := range f.Hosts {
		switch {
		case !reName.MatchString(h.Name):
			return fmt.Errorf("bad machine name %q", h.Name)
		case seen[h.Name]:
			return fmt.Errorf("machine %s twice", h.Name)
		case !reTarget.MatchString(h.Target):
			return fmt.Errorf("machine %s: bad target %q", h.Name, h.Target)
		case h.EnvFile != "" && !rePath.MatchString(h.EnvFile):
			return fmt.Errorf("machine %s: bad envFile %q", h.Name, h.EnvFile)
		case h.Root != "" && !rePath.MatchString(h.Root):
			return fmt.Errorf("machine %s: bad root %q", h.Name, h.Root)
		case h.Binary != "" && !rePath.MatchString(h.Binary):
			return fmt.Errorf("machine %s: bad binary %q", h.Name, h.Binary)
		case h.Wave < 0:
			return fmt.Errorf("machine %s: negative wave", h.Name)
		}
		seen[h.Name] = true
	}
	return nil
}

// Select returns the machines named in only (all of them when only is empty), in file order. A name that is not in
// the fleet is an error: a typo must not roll nothing out silently.
func (f *Fleet) Select(only []string) ([]Host, error) {
	if len(only) == 0 {
		return append([]Host(nil), f.Hosts...), nil
	}
	want := map[string]bool{}
	for _, n := range only {
		want[n] = true
	}
	var out []Host
	for _, h := range f.Hosts {
		if want[h.Name] {
			out = append(out, h)
			delete(want, h.Name)
		}
	}
	if len(want) > 0 {
		var miss []string
		for n := range want {
			miss = append(miss, n)
		}
		sort.Strings(miss)
		return nil, fmt.Errorf("not in the fleet: %s", strings.Join(miss, ", "))
	}
	return out, nil
}

// Waves groups the machines by wave, the lowest first, keeping the file order inside a wave.
func Waves(hosts []Host) [][]Host {
	by := map[int][]Host{}
	var keys []int
	for _, h := range hosts {
		if _, ok := by[h.Wave]; !ok {
			keys = append(keys, h.Wave)
		}
		by[h.Wave] = append(by[h.Wave], h)
	}
	sort.Ints(keys)
	out := make([][]Host, 0, len(keys))
	for _, k := range keys {
		out = append(out, by[k])
	}
	return out
}
