package design

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// Projects made before pins carry a copy of a kit's theme as ds/<theme>/. Adopting moves them onto the kit: the project
// is pinned to the newest installed version that has every file of the copy it shares with the kit byte for byte (an
// older copy goes onto the older version and then sees the newer one offered, nothing is updated silently). The files
// equal to the version's go to the library's trash (so they can be put back by hand); the project's own files in
// ds/<theme>/ (previews, assets the kit does not have) stay where they are and win over the kit's. A copy that no
// installed version matches is left as it is and named in the report.

// AdoptedProject is one project the adoption looked at.
type AdoptedProject struct {
	Project string   `json:"project"`
	Mounts  []string `json:"mounts"`
	// Version is the kit's version the copy matches; "" when none does.
	Version string `json:"version"`
	// Moved: the copy's kit files went to the trash and the project is pinned.
	Moved bool `json:"moved"`
	// Kit is how many files of the copy are the version's; Own how many are the project's own and stay.
	Kit int `json:"kit"`
	Own int `json:"own"`
	// Differs are files the copy has with other bytes than the newest version; the project is left as it is.
	Differs []string `json:"differs,omitempty"`
	Trash   string   `json:"trash,omitempty"`
}

// AdoptReport is what an adoption did (or, dry, would do).
type AdoptReport struct {
	Kit      string           `json:"kit"`
	Latest   string           `json:"latest"`
	Projects []AdoptedProject `json:"projects"`
}

// copyFiles are the files of a project's ds/<mount>/ copy, by mount-relative path.
func copyFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func sameBytes(a, b string) bool {
	x, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	y, err := os.ReadFile(b)
	return err == nil && bytes.Equal(x, y)
}

// adoptMatch compares a project's copies of the mounts with one version: the files equal to the version's, the files
// the version does not have and the files it has with other bytes, by mount-relative path ("mount/rel").
func adoptMatch(root, kit string, copies map[string][]string, projectDS string) (same, own, differs []string) {
	for _, m := range sortedKeys(copies) {
		base := designkit.MountOf(root, kit, m)
		for _, rel := range copies[m] {
			key := m + "/" + rel
			mine := filepath.Join(projectDS, m, filepath.FromSlash(rel))
			have := ""
			if base != "" {
				have = filepath.Join(base, filepath.FromSlash(rel))
			}
			switch {
			case have == "" || !isRealFile(have):
				own = append(own, key)
			case sameBytes(have, mine):
				same = append(same, key)
			default:
				differs = append(differs, key)
			}
		}
	}
	return same, own, differs
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AdoptKitCopies moves the library's projects off their copies of the kit's themes onto the kit's versions.
// may says which projects the caller may change (nil: all); the others are left as they are and not reported.
func (l *Library) AdoptKitCopies(kit string, dry bool, may func(project string) bool) (AdoptReport, error) {
	versions := designkit.Versions(l.KitsDir(), kit)
	if len(versions) == 0 {
		return AdoptReport{}, kitErr(404, "The kit is not installed", "Кит не установлен")
	}
	latestRoot := designkit.Root(l.KitsDir(), kit, versions[0].Version)
	mounts := designkit.Mounts(latestRoot, kit)
	rep := AdoptReport{Kit: kit, Latest: versions[0].Version, Projects: []AdoptedProject{}}
	draft, hasDraft := l.KitDraft(kit)
	stamp := strconv.FormatInt(nowMs(), 10)
	for _, a := range l.ListArtifacts() {
		if hasDraft && draft.Project == a.ID {
			continue
		}
		if may != nil && !may(a.ID) {
			continue
		}
		old, pinned := l.KitPinOf(a.ID)
		if pinned && old.ID != kit {
			continue
		}
		ds := filepath.Join(l.ProjectDir(a.ID), "ds")
		copies := map[string][]string{}
		for _, m := range mounts {
			dir := filepath.Join(ds, m)
			if !isRealDir(dir) {
				continue
			}
			files, err := copyFiles(dir)
			if err != nil {
				return rep, err
			}
			copies[m] = files
		}
		if len(copies) == 0 {
			continue
		}
		ap := AdoptedProject{Project: a.ID, Mounts: sortedKeys(copies)}
		var same []string
		for i, v := range versions {
			s, own, differs := adoptMatch(designkit.Root(l.KitsDir(), kit, v.Version), kit, copies, ds)
			if len(differs) == 0 {
				ap.Version, ap.Kit, ap.Own, same = v.Version, len(s), len(own), s
				break
			}
			if i == 0 {
				ap.Differs = prefixed("ds/", differs)
			}
		}
		if ap.Version != "" {
			ap.Differs = nil
		}
		if dry || ap.Version == "" {
			rep.Projects = append(rep.Projects, ap)
			continue
		}
		pin := KitPin{ID: kit, Version: ap.Version, Mounts: ap.Mounts}
		if pinned {
			pin.Draft, pin.Dismissed = old.Draft, old.Dismissed
			pin.Mounts = mergeMounts(old.Mounts, ap.Mounts)
		}
		if err := l.SetKitPin(a.ID, &pin); err != nil {
			ap.Differs = append(ap.Differs, err.Error())
			rep.Projects = append(rep.Projects, ap)
			continue
		}
		trash := filepath.Join(l.Root(), ".trash", a.ID+"-ds-"+stamp)
		for _, key := range same {
			from := filepath.Join(ds, filepath.FromSlash(key))
			to := filepath.Join(trash, filepath.FromSlash(key))
			if err := os.MkdirAll(filepath.Dir(to), 0o777); err != nil {
				return rep, err
			}
			if err := os.Rename(from, to); err != nil {
				return rep, err
			}
		}
		for _, m := range ap.Mounts {
			removeEmptyDirs(filepath.Join(ds, m))
		}
		_ = os.Remove(ds)
		ap.Moved, ap.Trash = true, trash
		l.listing.invalidatePath(a.ID)
		rep.Projects = append(rep.Projects, ap)
	}
	return rep, nil
}

func prefixed(prefix string, list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = prefix + s
	}
	return out
}

func mergeMounts(a, b []string) []string {
	if len(a) == 0 {
		return nil // the whole kit already
	}
	seen := map[string]bool{}
	out := []string{}
	for _, m := range append(append([]string{}, a...), b...) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// removeEmptyDirs removes the folders under dir (and dir) that hold no files.
func removeEmptyDirs(dir string) {
	var dirs []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // fails, and stays, when not empty
	}
}
