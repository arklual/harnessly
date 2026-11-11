package apps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"
)

// Environment installs: apps the organization's environment puts on the machine (an `app` item of the hub). The person
// cannot remove one; they can turn it off for themselves unless the item is pinned. Their own install of the same app
// wins, as with the team's.

var (
	// ErrEnvApp: the app is the environment's; the person cannot remove it.
	ErrEnvApp = errors.New("apps: the app comes with the environment")
	// ErrEnvLocked: the environment's app is pinned; the person cannot turn it off.
	ErrEnvLocked = errors.New("apps: the environment's app is pinned")
	// ErrTrustedEnv: a trusted app is installed by hand, not by the environment.
	ErrTrustedEnv = errors.New("apps: a trusted app is installed by hand, not by the environment")
)

// EnvInstall is one app the environment installed.
type EnvInstall struct {
	// Link is the hub link (its folder) whose list brought it: each link prunes only its own.
	Link string `json:"link"`
	// ItemID is the hub's environment item that brought it.
	ItemID  string `json:"itemId"`
	ID      string `json:"id"`
	Version string `json:"version"`
	// Sum is the hash of the folder it was installed from: the same version with the same sum is already in place.
	Sum      string    `json:"sum"`
	Dir      string    `json:"dir"`
	Locked   bool      `json:"locked,omitempty"`
	At       int64     `json:"at"`
	Manifest *Manifest `json:"manifest"`
	// Missing are the installation's addresses the package asks for and the hub gave none of: the app is installed but
	// not configured, and the person does not see it (Installed.Unconfigured).
	Missing []string `json:"missing,omitempty"`
}

type envFile struct {
	Installs []EnvInstall `json:"installs"`
	// Muted are the environment's apps the person turned off for themselves.
	Muted []string `json:"muted"`
}

func (r *Registry) envPath() string { return filepath.Join(r.Root, "env.json") }

func (r *Registry) readEnv() (envFile, error) {
	raw, err := os.ReadFile(r.envPath())
	if errors.Is(err, fs.ErrNotExist) {
		return envFile{}, nil
	}
	if err != nil {
		return envFile{}, err
	}
	var e envFile
	if err := json.Unmarshal(raw, &e); err != nil {
		return envFile{}, err
	}
	return e, nil
}

func (r *Registry) writeEnv(e envFile) error {
	sort.Slice(e.Installs, func(i, j int) bool { return e.Installs[i].ID < e.Installs[j].ID })
	if e.Installs == nil {
		e.Installs = []EnvInstall{}
	}
	if e.Muted == nil {
		e.Muted = []string{}
	}
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.envPath(), append(raw, '\n'))
}

// envHas: the environment installed the app. Called with the lock held.
func (r *Registry) envHas(id string) bool {
	e, err := r.readEnv()
	return err == nil && slices.ContainsFunc(e.Installs, func(i EnvInstall) bool { return i.ID == id })
}

// SetOrgOff sets what says whether the organization turned off the app of a link's environment item; nil (the default,
// a machine without a hub) turns nothing off. It is called with the registry's lock held, so it must not use the
// registry.
func (r *Registry) SetOrgOff(off func(link, itemID string) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orgOff = off
}

// EnvHasItem: the app of the link's environment item is installed.
func (r *Registry) EnvHasItem(link, itemID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.readEnv()
	return err == nil && slices.ContainsFunc(e.Installs, func(i EnvInstall) bool { return i.Link == link && i.ItemID == itemID })
}

// mergeEnv adds the environment's installs the list does not have yet (the person's own and the team's win). Called
// with the lock held.
func (r *Registry) mergeEnv(list []Installed) ([]Installed, error) {
	e, err := r.readEnv()
	if err != nil {
		return nil, err
	}
	out := list
	for _, ei := range e.Installs {
		if slices.ContainsFunc(list, func(a Installed) bool { return a.ID == ei.ID }) {
			continue
		}
		if r.orgOff != nil && r.orgOff(ei.Link, ei.ItemID) {
			// The organization turned the app off: it stays installed with its data, and is not there for anybody.
			continue
		}
		out = append(out, Installed{
			ID: ei.ID, Version: ei.Version, Dir: ei.Dir, Source: "env", InstalledAt: ei.At, UpdatedAt: ei.At,
			Manifest: ei.Manifest, Env: true, Locked: ei.Locked, Muted: !ei.Locked && slices.Contains(e.Muted, ei.ID),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// EnvInstallOptions say how the environment installs an app.
type EnvInstallOptions struct {
	// Link is the hub link whose list has the item.
	Link string
	// ItemID is the hub's environment item.
	ItemID string
	// Locked: the item is pinned, the person cannot turn the app off.
	Locked      bool
	HostVersion string
	Now         time.Time
	// Install are the installation's values the hub gave with the item (${install:<name>} of the package).
	Install map[string]string
}

// InstallForEnv installs the app in dir as the environment's. It reports changed false when the same version from the
// same folder contents is already in place (only Locked may change then). Never the trusted tier.
func (r *Registry) InstallForEnv(dir string, opt EnvInstallOptions) (EnvInstall, bool, error) {
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) > 0 {
		return EnvInstall{}, false, fmt.Errorf("%w: %s", ErrInvalid, ps.Error())
	}
	m := pkg.Manifest
	if m.Trust == TrustTrusted {
		return EnvInstall{}, false, fmt.Errorf("%w: %s", ErrTrustedEnv, m.ID)
	}
	if rng, err := ParseRange(m.Harnessly); err == nil && !rng.Allows(opt.HostVersion) {
		return EnvInstall{}, false, fmt.Errorf("%w: %s wants harnessly %s, this is %s", ErrHostVersion, m.ID, m.Harnessly, opt.HostVersion)
	}
	if SignaturesRequired() {
		// An environment item is a folder of the hub's list and carries no signature.
		return EnvInstall{}, false, refuseUnsigned(m.ID)
	}
	sum, err := dirSum(pkg.Dir)
	if err != nil {
		return EnvInstall{}, false, err
	}
	if extra := installSum(packageInstallNames(pkg.Dir), opt.Install); extra != "" {
		sum += "+" + extra
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.readEnv()
	if err != nil {
		return EnvInstall{}, false, err
	}
	at := slices.IndexFunc(e.Installs, func(i EnvInstall) bool { return i.ID == m.ID })
	if at >= 0 && (e.Installs[at].Link != opt.Link || e.Installs[at].ItemID != opt.ItemID) {
		return EnvInstall{}, false, fmt.Errorf("apps: %s already comes with another environment item", m.ID)
	}
	if at >= 0 && e.Installs[at].Version == m.Version && e.Installs[at].Sum == sum {
		if e.Installs[at].Locked == opt.Locked {
			return e.Installs[at], false, nil
		}
		e.Installs[at].Locked = opt.Locked
		if err := r.writeEnv(e); err != nil {
			return EnvInstall{}, false, err
		}
		return e.Installs[at], true, nil
	}

	own, err := r.read()
	if err != nil {
		return EnvInstall{}, false, err
	}
	everyone, err := r.mergeTeam(own)
	if err != nil {
		return EnvInstall{}, false, err
	}
	for _, ei := range e.Installs {
		if ei.ID != m.ID {
			everyone = append(everyone, Installed{ID: ei.ID, Manifest: ei.Manifest})
		}
	}
	others := slices.DeleteFunc(slices.Clone(everyone), func(a Installed) bool { return a.ID == m.ID })
	for _, other := range others {
		if PluginName(other.ID) == PluginName(m.ID) {
			return EnvInstall{}, false, fmt.Errorf("%w: %s and %s would be the same plugin %s", ErrPluginName, m.ID, other.ID, PluginName(m.ID))
		}
	}
	if kind, other := kindConflict(m, manifestsOf(others)); kind != "" {
		return EnvInstall{}, false, fmt.Errorf("%w: %s is added by %s", ErrKindTaken, kind, other)
	}

	from, missing := pkg.Dir, []string(nil)
	if len(packageInstallNames(pkg.Dir)) > 0 {
		// The installation's values go into a copy of their own first: a package they make invalid leaves the version
		// in place as it was.
		stage := filepath.Join(r.Root, "env-stage", m.ID)
		defer os.RemoveAll(stage)
		if err := copyPackage(pkg.Dir, stage); err != nil {
			return EnvInstall{}, false, err
		}
		if missing, err = fillInstall(stage, &m, opt.Install); err != nil {
			return EnvInstall{}, false, err
		}
		from = stage
	}
	target := filepath.Join(r.Root, "env", m.ID, m.Version)
	if err := copyPackage(from, target); err != nil {
		return EnvInstall{}, false, err
	}
	if err := writePluginManifest(target, m); err != nil {
		return EnvInstall{}, false, err
	}
	ei := EnvInstall{Link: opt.Link, ItemID: opt.ItemID, ID: m.ID, Version: m.Version, Sum: sum, Dir: target, Locked: opt.Locked, At: opt.Now.UnixMilli(), Manifest: m, Missing: missing}
	var old *EnvInstall
	if at >= 0 {
		prev := e.Installs[at]
		old = &prev
		e.Installs[at] = ei
	} else {
		e.Installs = append(e.Installs, ei)
	}
	if err := r.writeEnv(e); err != nil {
		return EnvInstall{}, false, err
	}
	if old != nil && old.Dir != target {
		_ = os.RemoveAll(old.Dir)
	}
	return ei, true, nil
}

// fillInstall puts the installation's values into the copy in target and reads its manifest again; missing are the
// names left without a value. A package whose values make it invalid (an address that is not https) is refused.
func fillInstall(target string, m **Manifest, values map[string]string) ([]string, error) {
	missing, err := fillInstallFiles(target, values)
	if err != nil {
		return nil, err
	}
	filled, ps := LoadPackage(target)
	if filled == nil || len(ps.Errors()) > 0 {
		return nil, fmt.Errorf("%w: with the installation's addresses: %s", ErrInvalid, ps.Error())
	}
	*m = filled.Manifest
	return missing, nil
}

// PruneEnv takes off the link's environment installs whose item is not among keep, and deletes their copies; the
// installs of other links are not touched. It returns the ids it took off.
func (r *Registry) PruneEnv(link string, keep []string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.readEnv()
	if err != nil {
		return nil, err
	}
	var gone []string
	e.Installs = slices.DeleteFunc(e.Installs, func(i EnvInstall) bool {
		if i.Link != link || slices.Contains(keep, i.ItemID) {
			return false
		}
		gone = append(gone, i.ID)
		_ = os.RemoveAll(filepath.Join(r.Root, "env", i.ID))
		return true
	})
	if len(gone) == 0 {
		return nil, nil
	}
	e.Muted = slices.DeleteFunc(e.Muted, func(id string) bool { return slices.Contains(gone, id) })
	return gone, r.writeEnv(e)
}

// muteEnv turns the environment's app off for the person (on false: on again). Called with the lock held.
func (r *Registry) muteEnv(id string, mute bool) error {
	e, err := r.readEnv()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(e.Installs, func(i EnvInstall) bool { return i.ID == id })
	if i < 0 {
		return ErrNotInstalled
	}
	if mute && e.Installs[i].Locked {
		return ErrEnvLocked
	}
	e.Muted = slices.DeleteFunc(e.Muted, func(m string) bool { return m == id })
	if mute {
		e.Muted = append(e.Muted, id)
	}
	return r.writeEnv(e)
}

// dirSum hashes the package's files (names and contents, .git and node_modules left out) in a stable order.
func dirSum(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
