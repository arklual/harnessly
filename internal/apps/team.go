package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// The team's apps. A team is the hub's space; the platform tells this machine which one it is in and what the person's
// role is (the snapshot, as for the design library's access), and the machine keeps it in <apps>/team.json so a restart
// or a platform that is down does not take the team's apps away.
//
//   - The catalog is what the team's members published to it: one entry per app and channel, pointing at a version
//     (and the one before it, for a rollback), the packages kept as archives under <apps>/catalog/.
//   - An owner installs an app from the catalog for the team. The members get it without an install of their own: List
//     merges the team's installs in. A member can turn it off for themselves (Mute), not remove it.
//   - An app of the trusted tier is never installed for the team: it runs code on the machine, so each person installs
//     it by hand, knowing what it is.
//   - There is no review of a catalog entry: the owner who installs it is the one who answers for it, and sees the
//     rights it asks before.

// Roles of the person in the team.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

var (
	// ErrNoTeam: this machine is not in a team.
	ErrNoTeam = errors.New("apps: this machine is not in a team")
	// ErrNotOwner: the person is a member; installing for the team is an owner's.
	ErrNotOwner = errors.New("apps: only an owner of the team does this")
	// ErrTeamApp: the app is the team's; a person turns it off for themselves, they do not remove it.
	ErrTeamApp = errors.New("apps: the app is the team's: turn it off for yourself instead")
	// ErrTrustedTeam: a trusted app is installed by hand, never for a team.
	ErrTrustedTeam = errors.New("apps: a trusted app is installed by hand, not for the team")
	// ErrNotInCatalog: the team's catalog has no such entry.
	ErrNotInCatalog = errors.New("apps: the team's catalog has no such app")
	// ErrStaleTeam: the snapshot is older than the one held.
	ErrStaleTeam = errors.New("apps: stale team snapshot")
)

// CatalogEntry is an app in the team's catalog on one channel.
type CatalogEntry struct {
	ID          string   `json:"id"`
	Channel     string   `json:"channel"`
	Version     string   `json:"version"`
	Previous    string   `json:"previous,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Trust       string   `json:"trust"`
	Targets     []string `json:"targets"`
	Permissions []string `json:"permissions"`
	By          string   `json:"by"`
	At          int64    `json:"at"`
	// Sum is the SHA-256 of the archive of Version; PreviousSum that of Previous.
	Sum         string `json:"sum"`
	PreviousSum string `json:"previousSum,omitempty"`
	// What a machine checks before it downloads the app: the harnessly it wants, the slash command and the
	// document types it takes, the secrets it needs. An entry made before these were kept has none, and is checked at
	// the install as before.
	Harnessly string   `json:"harnessly,omitempty"`
	Command   string   `json:"command,omitempty"`
	Kinds     []string `json:"kinds,omitempty"`
	Secrets   []string `json:"secrets,omitempty"`
	// Check is this machine's answer for the entry, made when the list is given; it is never stored.
	Check *Precheck `json:"check,omitempty"`
}

// TeamInstall is an app an owner installed for the team: the catalog entry's channel, and the version it is at.
type TeamInstall struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`
	Version string `json:"version"`
	// Sum is the SHA-256 of the archive of Version: the exact package the owner approved.
	Sum      string    `json:"sum"`
	By       string    `json:"by"`
	At       int64     `json:"at"`
	Dir      string    `json:"dir"`
	Manifest *Manifest `json:"manifest"`
}

type teamFile struct {
	Space    string         `json:"space"`
	Role     string         `json:"role"`
	At       int64          `json:"at"`
	Catalog  []CatalogEntry `json:"catalog"`
	Installs []TeamInstall  `json:"installs"`
	// Muted are the apps the person turned off for themselves.
	Muted []string `json:"muted"`
}

func (r *Registry) teamFile() string { return filepath.Join(r.Root, "team.json") }

func (r *Registry) readTeam() (teamFile, error) {
	var t teamFile
	raw, err := os.ReadFile(r.teamFile())
	if errors.Is(err, fs.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	return t, json.Unmarshal(raw, &t)
}

func (r *Registry) writeTeam(t teamFile) error {
	sort.Slice(t.Catalog, func(i, j int) bool {
		if t.Catalog[i].ID != t.Catalog[j].ID {
			return t.Catalog[i].ID < t.Catalog[j].ID
		}
		return t.Catalog[i].Channel < t.Catalog[j].Channel
	})
	sort.Slice(t.Installs, func(i, j int) bool { return t.Installs[i].ID < t.Installs[j].ID })
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(r.teamFile(), append(raw, '\n'))
}

// teamHas: the team installed the app. Called with the lock held.
func (r *Registry) teamHas(id string) bool {
	t, err := r.readTeam()
	return err == nil && slices.ContainsFunc(t.Installs, func(i TeamInstall) bool { return i.ID == id })
}

// mergeTeam adds the team's installs to the person's own, by id; the person's own install of an app wins, and a team
// app the person turned off comes with Muted. Called with the lock held.
func (r *Registry) mergeTeam(own []Installed) ([]Installed, error) {
	t, err := r.readTeam()
	if err != nil {
		return nil, err
	}
	out := slices.Clone(own)
	for _, ti := range t.Installs {
		if slices.ContainsFunc(own, func(a Installed) bool { return a.ID == ti.ID }) {
			continue
		}
		out = append(out, Installed{
			ID: ti.ID, Version: ti.Version, Dir: ti.Dir, Source: "team", InstalledAt: ti.At, UpdatedAt: ti.At,
			Channel: ti.Channel, Manifest: ti.Manifest, Team: true, Muted: slices.Contains(t.Muted, ti.ID),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// TeamInfo is the team as the page shows it.
type TeamInfo struct {
	Space string `json:"space"`
	Role  string `json:"role"`
	// Catalog is the catalog; Installed marks what the team has installed from it.
	Catalog []CatalogView `json:"catalog"`
	// Muted are the team's apps the person turned off for themselves.
	Muted []string `json:"muted"`
}

// CatalogView is a catalog entry with whether the team has it installed.
type CatalogView struct {
	CatalogEntry
	// Installed: the team has the app installed on this channel; At is the version it is at.
	Installed bool   `json:"installed"`
	At        string `json:"installedVersion,omitempty"`
}

// Team reads the team; Space is "" for a machine that is in none.
func (r *Registry) Team() (TeamInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return TeamInfo{}, err
	}
	out := TeamInfo{Space: t.Space, Role: t.Role, Catalog: []CatalogView{}, Muted: append([]string{}, t.Muted...)}
	for _, e := range t.Catalog {
		v := CatalogView{CatalogEntry: e}
		for _, i := range t.Installs {
			if i.ID == e.ID && i.Channel == e.Channel {
				v.Installed, v.At = true, i.Version
			}
		}
		out.Catalog = append(out.Catalog, v)
	}
	return out, nil
}

func (r *Registry) catalogArchive(id, version string) string {
	return filepath.Join(r.Root, "catalog", id, version+".tgz")
}

func (r *Registry) teamPackageDir(id, version string) string {
	return filepath.Join(r.Root, "team", id, version)
}

// requireRole reads the team and refuses a machine that is in none, or a person below the role asked.
func requireRole(t teamFile, owner bool) error {
	if t.Space == "" || t.Role == "" {
		return ErrNoTeam
	}
	if owner && t.Role != RoleOwner {
		return ErrNotOwner
	}
	return nil
}

// PublishToTeam puts a published version into the team's catalog on the channel it was published on. Any member may;
// the entry only offers the app, the owner's install is what puts it on the team's machines.
func (r *Registry) PublishToTeam(id, version string, ch Channel, by, host string, now time.Time) (CatalogEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return CatalogEntry{}, err
	}
	if err := requireRole(t, false); err != nil {
		return CatalogEntry{}, err
	}
	state, err := r.readChannels(id)
	if err != nil {
		return CatalogEntry{}, err
	}
	if _, ok := state.Versions[version]; !ok {
		return CatalogEntry{}, fmt.Errorf("%w: %s %s", ErrNoVersion, id, version)
	}
	pkg, ps := LoadPackage(r.VersionDir(id, version))
	if pkg == nil || len(ps.Errors()) > 0 {
		return CatalogEntry{}, ErrInvalid
	}
	raw, sum, err := Pack(pkg.Dir)
	if err != nil {
		return CatalogEntry{}, err
	}
	if err := writeFileAtomic(r.catalogArchive(id, version), raw); err != nil {
		return CatalogEntry{}, err
	}
	m := pkg.Manifest
	e := CatalogEntry{
		ID: id, Channel: string(ch), Version: version, Name: m.Name, Description: m.Description, Trust: m.Trust,
		Targets: m.Targets, Permissions: m.Permissions, By: by, At: now.UnixMilli(), Sum: sum,
	}
	e.describe(m)
	if e.Targets == nil {
		e.Targets = []string{}
	}
	if e.Permissions == nil {
		e.Permissions = []string{}
	}
	replaced := false
	for i := range t.Catalog {
		old := t.Catalog[i]
		if old.ID != id || old.Channel != e.Channel {
			continue
		}
		if old.Version != version {
			e.Previous, e.PreviousSum = old.Version, old.Sum
		} else {
			e.Previous, e.PreviousSum = old.Previous, old.PreviousSum
		}
		t.Catalog[i], replaced = e, true
	}
	if !replaced {
		t.Catalog = append(t.Catalog, e)
	}
	if t.Role == RoleOwner {
		t.At = now.UnixMilli()
		// The team's installs of this app follow the entry (a patch or a minor at once).
		if _, err := r.refollowTeam(&t, host, false, now); err != nil {
			return CatalogEntry{}, err
		}
	}
	// A member's publish leaves the team's time alone: it is the platform's, and its next snapshot must not be stale.
	if err := r.writeTeam(t); err != nil {
		return CatalogEntry{}, err
	}
	r.teamChanged()
	return e, nil
}

// RollbackCatalog points a catalog entry back at the version before; the installs that follow it go back too.
func (r *Registry) RollbackCatalog(id string, ch Channel, host string, now time.Time) (CatalogEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return CatalogEntry{}, err
	}
	if err := requireRole(t, true); err != nil {
		return CatalogEntry{}, err
	}
	for i := range t.Catalog {
		e := &t.Catalog[i]
		if e.ID != id || e.Channel != string(ch) {
			continue
		}
		if e.Previous == "" {
			return CatalogEntry{}, fmt.Errorf("%w: %s %s in the catalog", ErrNoPrevious, id, ch)
		}
		e.Version, e.Sum, e.Previous, e.PreviousSum = e.Previous, e.PreviousSum, "", ""
		t.At = now.UnixMilli()
		if _, err := r.refollowTeam(&t, host, true, now); err != nil {
			return CatalogEntry{}, err
		}
		if err := r.writeTeam(t); err != nil {
			return CatalogEntry{}, err
		}
		r.teamChanged()
		return *e, nil
	}
	return CatalogEntry{}, fmt.Errorf("%w: %s %s", ErrNotInCatalog, id, ch)
}

// unpackVerified unpacks the archive of a catalog entry into the team's folder for that version and returns the
// package, checked as any other: the same app and version as the entry says, never the trusted tier.
func (r *Registry) unpackVerified(id, version, sum, host string) (*Package, error) {
	raw, err := os.ReadFile(r.catalogArchive(id, version))
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s is not in this machine's catalog", ErrNotInCatalog, id, version)
	}
	return unpackArchive(raw, id, version, sum, host, r.teamPackageDir(id, version))
}

// unpackArchive is what every catalog (the team's, the public one) does with the bytes of a package before it is
// installed: the sum is the one the catalog names, the archive unpacks into dst without leaving it, the package passes
// the check and is the app and version asked for, it is not of the trusted tier, and this host runs it.
func unpackArchive(raw []byte, id, version, sum, host, dst string) (*Package, error) {
	if ArchiveSum(raw) != sum {
		return nil, fmt.Errorf("%w: the archive of %s %s is not the one the catalog names", ErrArchive, id, version)
	}
	// A folder unpacked before is reused only when it came from this very archive (its sum is kept beside it); a folder
	// whose origin is not known, or is another archive's, is unpacked afresh.
	if have, err := os.ReadFile(dst + ".sum"); err == nil && strings.TrimSpace(string(have)) == sum {
		if pkg, ps := LoadPackage(dst); pkg != nil && len(ps.Errors()) == 0 && pkg.Manifest.ID == id && pkg.Manifest.Version == version {
			return pkg, nil
		}
	}
	// Each unpack has a partial folder of its own: two at once never write into one another.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), filepath.Base(dst)+".partial-")
	if err != nil {
		return nil, err
	}
	if err := Unpack(raw, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	pkg, ps := LoadPackage(tmp)
	switch {
	case pkg == nil || len(ps.Errors()) > 0:
		_ = os.RemoveAll(tmp)
		return nil, ErrInvalid
	case pkg.Manifest.ID != id || pkg.Manifest.Version != version:
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("%w: the archive holds %s %s, not %s %s", ErrArchive, pkg.Manifest.ID, pkg.Manifest.Version, id, version)
	case pkg.Manifest.Trust == TrustTrusted:
		_ = os.RemoveAll(tmp)
		return nil, ErrTrustedTeam
	}
	// host "" reads the package without asking whether this host runs it (a catalog lists what it cannot install here).
	if rng, err := ParseRange(pkg.Manifest.Harnessly); err == nil && host != "" && !rng.Allows(host) {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("%w: %s wants harnessly %s, this is %s", ErrHostVersion, id, pkg.Manifest.Harnessly, host)
	}
	if err := writePluginManifest(tmp, pkg.Manifest); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	// The old folder goes aside and the new one takes its place by a rename, so nobody sees a half-made folder.
	_ = os.Remove(dst + ".sum")
	old := dst + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			_ = os.RemoveAll(tmp)
			return nil, err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(old)
	if err := writeFileAtomic(dst+".sum", []byte(sum+"\n")); err != nil {
		return nil, err
	}
	pkg.Dir = dst
	return pkg, nil
}

// TeamInstallOptions say how an owner installs for the team.
type TeamInstallOptions struct {
	HostVersion string
	By          string
	Now         time.Time
}

// InstallForTeam installs the catalog's app on a channel for the whole team. Only an owner; never the trusted tier.
// The install follows the catalog entry: a later version of the entry reaches the team's machines (patch and minor at
// once, a new major or new rights only when an owner installs again).
func (r *Registry) InstallForTeam(id string, ch Channel, opt TeamInstallOptions) (TeamInstall, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return TeamInstall{}, err
	}
	if err := requireRole(t, true); err != nil {
		return TeamInstall{}, err
	}
	var e *CatalogEntry
	for i := range t.Catalog {
		if t.Catalog[i].ID == id && t.Catalog[i].Channel == string(ch) {
			e = &t.Catalog[i]
		}
	}
	if e == nil {
		return TeamInstall{}, fmt.Errorf("%w: %s %s", ErrNotInCatalog, id, ch)
	}
	pkg, err := r.unpackVerified(id, e.Version, e.Sum, opt.HostVersion)
	if err != nil {
		return TeamInstall{}, err
	}
	own, err := r.read()
	if err != nil {
		return TeamInstall{}, err
	}
	for _, other := range append(slices.Clone(own), r.teamAsInstalled(t)...) {
		if other.ID != id && PluginName(other.ID) == PluginName(id) {
			return TeamInstall{}, fmt.Errorf("%w: %s and %s would be the same plugin %s", ErrPluginName, id, other.ID, PluginName(id))
		}
	}
	others := manifestsOf(own)
	for _, ti := range t.Installs {
		others = append(others, ti.Manifest)
	}
	if kind, other := kindConflict(pkg.Manifest, others); kind != "" {
		return TeamInstall{}, fmt.Errorf("%w: %s is added by %s", ErrKindTaken, kind, other)
	}
	ti := TeamInstall{ID: id, Channel: string(ch), Version: e.Version, Sum: e.Sum, By: opt.By, At: opt.Now.UnixMilli(), Dir: pkg.Dir, Manifest: pkg.Manifest}
	if i := slices.IndexFunc(t.Installs, func(x TeamInstall) bool { return x.ID == id }); i >= 0 {
		t.Installs[i] = ti
	} else {
		t.Installs = append(t.Installs, ti)
	}
	t.At = opt.Now.UnixMilli()
	if err := r.writeTeam(t); err != nil {
		return TeamInstall{}, err
	}
	r.teamChanged()
	return ti, nil
}

func (r *Registry) teamAsInstalled(t teamFile) []Installed {
	var out []Installed
	for _, i := range t.Installs {
		out = append(out, Installed{ID: i.ID})
	}
	return out
}

// UninstallForTeam takes the app off the team's machines. Only an owner.
func (r *Registry) UninstallForTeam(id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return err
	}
	if err := requireRole(t, true); err != nil {
		return err
	}
	before := len(t.Installs)
	t.Installs = slices.DeleteFunc(t.Installs, func(i TeamInstall) bool { return i.ID == id })
	if len(t.Installs) == before {
		return ErrNotInstalled
	}
	t.Muted = slices.DeleteFunc(t.Muted, func(m string) bool { return m == id })
	t.At = now.UnixMilli()
	if err := r.writeTeam(t); err != nil {
		return err
	}
	r.teamChanged()
	return nil
}

// MuteTeamApp turns a team app (or the environment's) off for the person (on false: on again). It is theirs alone: nothing is sent anywhere.
func (r *Registry) MuteTeamApp(id string, mute bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(t.Installs, func(i TeamInstall) bool { return i.ID == id }) {
		return r.muteEnv(id, mute)
	}
	t.Muted = slices.DeleteFunc(t.Muted, func(m string) bool { return m == id })
	if mute {
		t.Muted = append(t.Muted, id)
	}
	return r.writeTeam(t)
}

// refollowTeam brings the team's installs to the version their catalog entry names. A step up that changes the major
// or asks for more rights waits for an owner's install (unless force: a rollback goes back at once).
func (r *Registry) refollowTeam(t *teamFile, host string, force bool, now time.Time) (bool, error) {
	changed := false
	for i := range t.Installs {
		ti := &t.Installs[i]
		var e *CatalogEntry
		for k := range t.Catalog {
			if t.Catalog[k].ID == ti.ID && t.Catalog[k].Channel == ti.Channel {
				e = &t.Catalog[k]
			}
		}
		if e == nil || e.Version == ti.Version {
			continue
		}
		pkg, err := r.unpackVerified(e.ID, e.Version, e.Sum, host)
		if err != nil {
			return changed, err
		}
		if !force && (majorOf(e.Version) != majorOf(ti.Version) || (ti.Manifest != nil && grew(ti.Manifest, pkg.Manifest))) {
			continue
		}
		ti.Version, ti.Sum, ti.Dir, ti.Manifest, ti.At = e.Version, e.Sum, pkg.Dir, pkg.Manifest, now.UnixMilli()
		changed = true
	}
	return changed, nil
}

// TeamSnapshot is what the platform sends a machine: the team, the person's role in it and the catalog with the
// archives of its entries, and the installs by app and channel.
type TeamSnapshot struct {
	Space string `json:"space"`
	// Role is the person's: owner or member. "" with no Space takes the machine out of the team.
	Role string `json:"role"`
	At   int64  `json:"at"`
	// Catalog entries with their archives (the current version, and the previous one for a rollback).
	Catalog  []SnapshotEntry   `json:"catalog"`
	Installs []SnapshotInstall `json:"installs"`
}

// SnapshotEntry is a catalog entry on the wire.
type SnapshotEntry struct {
	CatalogEntry
	Archive         []byte `json:"archive,omitempty"`
	PreviousArchive []byte `json:"previousArchive,omitempty"`
}

// SnapshotInstall says that the team has an app on a channel installed.
type SnapshotInstall struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`
	// Version and Sum pin the package the owner installed; the machine installs exactly that, never the catalog's head.
	Version string `json:"version"`
	Sum     string `json:"sum"`
	// Archive is the pinned version's bytes (the catalog may have moved on from it).
	Archive []byte `json:"archive,omitempty"`
	By      string `json:"by"`
	At      int64  `json:"at"`
}

// Snapshot is this machine's team as a snapshot, for the platform to send the other machines of the team. The archives
// of the entries go along.
func (r *Registry) Snapshot() (TeamSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return TeamSnapshot{}, err
	}
	out := TeamSnapshot{Space: t.Space, Role: t.Role, At: t.At, Catalog: []SnapshotEntry{}, Installs: []SnapshotInstall{}}
	for _, e := range t.Catalog {
		se := SnapshotEntry{CatalogEntry: e}
		if se.Archive, err = os.ReadFile(r.catalogArchive(e.ID, e.Version)); err != nil {
			return TeamSnapshot{}, err
		}
		if e.Previous != "" {
			if raw, err := os.ReadFile(r.catalogArchive(e.ID, e.Previous)); err == nil {
				se.PreviousArchive = raw
			}
		}
		out.Catalog = append(out.Catalog, se)
	}
	for _, i := range t.Installs {
		raw, err := os.ReadFile(r.catalogArchive(i.ID, i.Version))
		if err != nil {
			return TeamSnapshot{}, err
		}
		out.Installs = append(out.Installs, SnapshotInstall{ID: i.ID, Channel: i.Channel, Version: i.Version, Sum: i.Sum, Archive: raw, By: i.By, At: i.At})
	}
	return out, nil
}

// MaxSnapshotEntries bounds a snapshot's catalog.
const MaxSnapshotEntries = 200

// Validate refuses a snapshot this machine must not act on.
func (s TeamSnapshot) Validate() error {
	if s.At <= 0 {
		return errors.New("a snapshot has a time")
	}
	if (s.Space == "") != (s.Role == "") {
		return errors.New("space and role come together")
	}
	if s.Role != "" && s.Role != RoleOwner && s.Role != RoleMember {
		return fmt.Errorf("role %q is owner or member", s.Role)
	}
	if len(s.Catalog) > MaxSnapshotEntries || len(s.Installs) > MaxSnapshotEntries {
		return errors.New("too many entries")
	}
	for _, e := range s.Catalog {
		if !idRe.MatchString(e.ID) || !semverRe.MatchString(e.Version) {
			return fmt.Errorf("catalog entry %q %q is not an app and a version", e.ID, e.Version)
		}
		if _, ok := ParseChannel(e.Channel); !ok {
			return fmt.Errorf("catalog entry %s: %q is not a channel", e.ID, e.Channel)
		}
		if e.Previous != "" && !semverRe.MatchString(e.Previous) {
			return fmt.Errorf("catalog entry %s: %q is not a version", e.ID, e.Previous)
		}
	}
	for _, i := range s.Installs {
		if !idRe.MatchString(i.ID) {
			return fmt.Errorf("install %q is not an app", i.ID)
		}
		if _, ok := ParseChannel(i.Channel); !ok {
			return fmt.Errorf("install %s: %q is not a channel", i.ID, i.Channel)
		}
		if !semverRe.MatchString(i.Version) || i.Sum == "" {
			return fmt.Errorf("install %s: it names the version and the sum the owner installed", i.ID)
		}
	}
	return nil
}

// ApplyTeam takes the platform's snapshot as the machine's team: the catalog, the role and the installs, so the
// members' registries show the team's apps without an install of their own. An entry whose archive does not match its
// sum, or holds a trusted app, is left out of the installs (the rest still applies); the problems come back.
func (r *Registry) ApplyTeam(s TeamSnapshot, host string, now time.Time) ([]string, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return nil, err
	}
	if s.At < t.At {
		return nil, ErrStaleTeam
	}
	var problems []string
	next := teamFile{Space: s.Space, Role: s.Role, At: s.At, Catalog: []CatalogEntry{}, Installs: []TeamInstall{}, Muted: t.Muted}
	for _, se := range s.Catalog {
		e := se.CatalogEntry
		keep := func(version, sum string, raw []byte) bool {
			if raw == nil {
				// No bytes came: the entry stands only if this machine already holds that version.
				return statErr(r.catalogArchive(e.ID, version)) == nil
			}
			if ArchiveSum(raw) != sum {
				problems = append(problems, fmt.Sprintf("%s %s: the archive is not the one the catalog names", e.ID, version))
				return false
			}
			if err := writeFileAtomic(r.catalogArchive(e.ID, version), raw); err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %v", e.ID, version, err))
				return false
			}
			return true
		}
		if !keep(e.Version, e.Sum, se.Archive) {
			continue
		}
		if e.Previous != "" && !keep(e.Previous, e.PreviousSum, se.PreviousArchive) {
			e.Previous, e.PreviousSum = "", ""
		}
		next.Catalog = append(next.Catalog, e)
	}
	for _, si := range s.Installs {
		// The install is the package the owner approved, not whatever the catalog names now.
		if si.Archive != nil {
			if ArchiveSum(si.Archive) != si.Sum {
				problems = append(problems, fmt.Sprintf("%s %s: the archive is not the one the owner installed", si.ID, si.Version))
				continue
			}
			if err := writeFileAtomic(r.catalogArchive(si.ID, si.Version), si.Archive); err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %v", si.ID, si.Version, err))
				continue
			}
		}
		pkg, err := r.unpackVerified(si.ID, si.Version, si.Sum, host)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", si.ID, si.Version, err))
			continue
		}
		next.Installs = append(next.Installs, TeamInstall{ID: si.ID, Channel: si.Channel, Version: si.Version, Sum: si.Sum, By: si.By, At: si.At, Dir: pkg.Dir, Manifest: pkg.Manifest})
	}
	next.Muted = slices.DeleteFunc(slices.Clone(next.Muted), func(id string) bool {
		return !slices.ContainsFunc(next.Installs, func(i TeamInstall) bool { return i.ID == id })
	})
	if err := r.writeTeam(next); err != nil {
		return problems, err
	}
	return problems, nil
}

func statErr(p string) error {
	_, err := os.Stat(p)
	return err
}

// SetTeam makes this machine a member of a space (or, with an empty space, takes it out), keeping what the catalog
// already holds: the platform's snapshot normally does this; a self-hosted machine without one is told by hand.
func (r *Registry) SetTeam(space, role string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.readTeam()
	if err != nil {
		return err
	}
	if (space == "") != (role == "") || (role != "" && role != RoleOwner && role != RoleMember) {
		return errors.New("a team has a space and a role of owner or member")
	}
	t.Space, t.Role, t.At = space, role, now.UnixMilli()
	return r.writeTeam(t)
}
