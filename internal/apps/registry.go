package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// The apps installed on this machine. An install copies the package under <data>/apps/packages/<id>/<version>, so the
// app a chat runs does not change under it when the author edits their folder; a dev install points at the author's
// folder instead and every chat that starts picks up the edit. The list is <data>/apps/installed.json.

// Installed is one installed app as the registry keeps it.
type Installed struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	// Dir is the package's folder: the copy, or the author's folder for a dev install.
	Dir string `json:"dir"`
	// Source is the folder or address it was installed from.
	Source string `json:"source"`
	Dev    bool   `json:"dev,omitempty"`
	// Channel is the channel the install follows (see channels.go); "" is an install of a folder or a pinned version.
	Channel string `json:"channel,omitempty"`
	// Previous is the version the install had before the last update.
	Previous string `json:"previous,omitempty"`
	// Team: the install is the team's, made by an owner for everybody (see team.go); the person can turn it off for
	// themselves (Muted), not remove it. Never written to installed.json.
	Team        bool  `json:"team,omitempty"`
	Muted       bool  `json:"muted,omitempty"`
	InstalledAt int64 `json:"installedAt"`
	UpdatedAt   int64 `json:"updatedAt"`
	// Manifest is the manifest as installed, so the list needs no read of every package.
	Manifest *Manifest `json:"manifest"`
}

type registryBody struct {
	Apps []Installed `json:"apps"`
}

// Registry reads and writes the list; every writer of this process goes through one Registry.
type Registry struct {
	Root string

	mu sync.Mutex
	// onTeamChange is told, without blocking, that the team's catalog or installs changed here.
	onTeamChange func()
	// onPublicChange is told, without blocking, that this machine put an app in the public catalog.
	onPublicChange func()
}

// OnPublicChange sets what is called after PublishPublic. It is not called for a list applied from the platform.
func (r *Registry) OnPublicChange(f func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onPublicChange = f
}

// OnTeamChange sets what is called after the team's catalog or installs change on this machine (an owner's install
// or uninstall, a publish to the team, a rollback of the catalog). It is not called for a snapshot applied from the
// platform, nor for a person muting an app. The function must not block.
func (r *Registry) OnTeamChange(f func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onTeamChange = f
}

// teamChanged tells the hook; r.mu is held by the caller.
func (r *Registry) teamChanged() {
	if r.onTeamChange != nil {
		r.onTeamChange()
	}
}

var (
	registriesMu sync.Mutex
	registries   = map[string]*Registry{}
)

// Open is the process's one Registry of a data folder.
func Open(dataDir string) *Registry {
	root := filepath.Join(dataDir, "apps")
	registriesMu.Lock()
	defer registriesMu.Unlock()
	if r := registries[root]; r != nil {
		return r
	}
	r := &Registry{Root: root}
	registries[root] = r
	return r
}

var (
	// ErrNotInstalled: no installed app has that id.
	ErrNotInstalled = errors.New("apps: not installed")
	// ErrInvalid: the package has errors; the Problems say which.
	ErrInvalid = errors.New("apps: the package has errors")
	// ErrPluginName: another installed app has the same plugin name.
	ErrPluginName = errors.New("apps: another installed app has the same plugin name")
	// ErrHostVersion: the app does not work with this host's version.
	ErrHostVersion = errors.New("apps: needs another harnessly version")
)

func (r *Registry) file() string { return filepath.Join(r.Root, "installed.json") }

func (r *Registry) read() ([]Installed, error) {
	raw, err := os.ReadFile(r.file())
	if errors.Is(err, fs.ErrNotExist) {
		return []Installed{}, nil
	}
	if err != nil {
		return nil, err
	}
	var body registryBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if body.Apps == nil {
		body.Apps = []Installed{}
	}
	return body.Apps, nil
}

func (r *Registry) write(list []Installed) error {
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	raw, err := json.MarshalIndent(registryBody{Apps: list}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.file(), append(raw, '\n'))
}

// userInstalls are the apps the person installed on this machine.
func (r *Registry) userInstalls() ([]Installed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read()
}

// List is every installed app that is on for the person, by id: their own installs, and the team's that they did not
// turn off or install themselves (their own install of the same app wins).
func (r *Registry) List() ([]Installed, error) {
	all, err := r.ListAll()
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(a Installed) bool { return a.Muted }), nil
}

// ListAll is List with the team's apps the person turned off, which have Muted set.
func (r *Registry) ListAll() ([]Installed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list, err := r.read()
	if err != nil {
		return nil, err
	}
	return r.mergeTeam(list)
}

// Get is one installed app.
func (r *Registry) Get(id string) (Installed, error) {
	list, err := r.List()
	if err != nil {
		return Installed{}, err
	}
	for _, a := range list {
		if a.ID == id {
			return a, nil
		}
	}
	return Installed{}, ErrNotInstalled
}

// InstallOptions say how to install.
type InstallOptions struct {
	// Dev points the install at the folder instead of copying it.
	Dev bool
	// Channel is the channel the install follows ("" for none).
	Channel string
	// HostVersion is this harnessly's version, checked against the manifest's range.
	HostVersion string
	// Source is what the install records as where it came from, for a package that was unpacked in a folder of its own
	// that is removed afterwards ("" records the folder installed from).
	Source string
	Now    func() time.Time
}

// Install checks the package in dir and installs it, replacing an installed version of the same app. The problems are
// all the check found (warnings included); an install with errors is refused with ErrInvalid.
func (r *Registry) Install(dir string, opt InstallOptions) (Installed, Problems, error) {
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) > 0 {
		return Installed{}, ps, ErrInvalid
	}
	m := pkg.Manifest
	if rng, err := ParseRange(m.Harnessly); err == nil && !rng.Allows(opt.HostVersion) {
		return Installed{}, ps, fmt.Errorf("%w: %s wants harnessly %s, this is %s", ErrHostVersion, m.ID, m.Harnessly, opt.HostVersion)
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	list, err := r.read()
	if err != nil {
		return Installed{}, ps, err
	}
	everyone, err := r.mergeTeam(list)
	if err != nil {
		return Installed{}, ps, err
	}

	// The agent knows an app's tools as mcp__plugin_<PluginName>_…: two apps with one PluginName could be taken for each
	// other (a.b-c and a-b.c), so the second is refused.
	for _, other := range everyone {
		if other.ID != m.ID && PluginName(other.ID) == PluginName(m.ID) {
			return Installed{}, ps, fmt.Errorf("%w: %s and %s would be the same plugin %s", ErrPluginName, m.ID, other.ID, PluginName(m.ID))
		}
	}

	// A document type has one app behind it: a second app adding the same name is refused.
	if kind, other := kindConflict(m, manifestsOf(everyone)); kind != "" {
		return Installed{}, ps, fmt.Errorf("%w: %s is added by %s", ErrKindTaken, kind, other)
	}

	target := pkg.Dir
	if !opt.Dev {
		target = filepath.Join(r.Root, "packages", m.ID, m.Version)
		if err := copyPackage(pkg.Dir, target); err != nil {
			return Installed{}, ps, err
		}
	}
	if err := writePluginManifest(target, m); err != nil {
		return Installed{}, ps, err
	}

	at := now().UnixMilli()
	source := pkg.Dir
	if opt.Source != "" {
		source = opt.Source
	}
	entry := Installed{ID: m.ID, Version: m.Version, Dir: target, Source: source, Dev: opt.Dev, Channel: opt.Channel, InstalledAt: at, UpdatedAt: at, Manifest: m}
	var old *Installed
	for i := range list {
		if list[i].ID == m.ID {
			old = &list[i]
		}
	}
	if old != nil {
		entry.InstalledAt = old.InstalledAt
		if old.Version != m.Version {
			entry.Previous = old.Version
		} else {
			entry.Previous = old.Previous
		}
		previous := *old
		*old = entry
		if err := r.write(list); err != nil {
			return Installed{}, ps, err
		}
		r.dropCopy(previous, entry)
		return entry, ps, nil
	}
	list = append(list, entry)
	if err := r.write(list); err != nil {
		return Installed{}, ps, err
	}
	return entry, ps, nil
}

// Remove uninstalls an app and deletes its copy; the author's folder of a dev install is left as it is.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list, err := r.read()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(list, func(a Installed) bool { return a.ID == id }) && r.teamHas(id) {
		return ErrTeamApp
	}
	for i, a := range list {
		if a.ID != id {
			continue
		}
		list = append(list[:i], list[i+1:]...)
		if err := r.write(list); err != nil {
			return err
		}
		if !a.Dev {
			_ = os.RemoveAll(filepath.Join(r.Root, "packages", a.ID))
		}
		return nil
	}
	return ErrNotInstalled
}

// dropCopy deletes the copy a replaced install left, unless the new install is the same folder.
func (r *Registry) dropCopy(previous, now Installed) {
	if previous.Dev || previous.Dir == now.Dir {
		return
	}
	_ = os.RemoveAll(previous.Dir)
}

// writePluginManifest gives the package the Claude Code plugin manifest its skills and commands load by, named for the
// app and versioned with it.
func writePluginManifest(dir string, m *Manifest) error {
	raw, err := json.MarshalIndent(map[string]string{
		"name":        PluginName(m.ID),
		"version":     m.Version,
		"description": m.Description,
	}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if old, err := os.ReadFile(path); err == nil && string(old) == string(raw)+"\n" {
		return nil
	}
	return writeFileAtomic(path, append(raw, '\n'))
}

// copyPackage copies the package into a fresh folder: regular files and folders only, .git and node_modules left out,
// links followed only when they stay inside (LoadPackage already refused the others).
func copyPackage(src, dst string) error {
	tmp := dst + ".partial"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		out := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return err
		}
		return copyFile(path, out, info.Mode().Perm())
	})
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(dst)
	return os.Rename(tmp, dst)
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o600)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	return errors.Join(cerr, out.Close())
}

// writeFileAtomic replaces a file through a temporary file beside it, readable by the runtime's user only.
func writeFileAtomic(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(raw)
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp, 0o600)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Package reads an installed app's package from its folder (its .mcp.json servers included).
func (r *Registry) Package(id string) (*Package, error) {
	a, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	pkg, ps := LoadPackage(a.Dir)
	if pkg == nil {
		return nil, fmt.Errorf("apps: %s: %s", id, ps.Error())
	}
	return pkg, nil
}

// PaneRef is the canonical "<app id>/<view id>" of the pane view a text names: "<id>/<view>", or "<id>" for the app's
// default one (activation.openView when it is a pane view, else the first). "" when the app is not installed or has no
// such view for the pane.
func (r *Registry) PaneRef(text string) string {
	id, view, hasView := strings.Cut(text, "/")
	app, err := r.Get(id)
	if err != nil || app.Manifest == nil {
		return ""
	}
	m := app.Manifest
	if !m.HasTarget(TargetPane) {
		return ""
	}
	if hasView {
		for _, v := range m.Views {
			if v.ID == view && contains(v.Targets, TargetPane) {
				return id + "/" + view
			}
		}
		return ""
	}
	if a := m.Activation; a != nil && a.OpenView != "" {
		for _, v := range m.Views {
			if v.ID == a.OpenView && contains(v.Targets, TargetPane) {
				return id + "/" + v.ID
			}
		}
	}
	for _, v := range m.Views {
		if contains(v.Targets, TargetPane) {
			return id + "/" + v.ID
		}
	}
	return ""
}
