package designkit

// Slide layouts a kit ships: every theme folder of a kit may hold _harnessly/layouts/layouts.json (the list) and one
// <id>.dc.html board per layout. A deck takes a layout by its address <kit>[@version]/<theme>/<layout>; the board is
// the new page's file and the theme's folder (themes/<theme>/) is copied into the project as ds/<theme>/.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Layout is one slide layout of a kit's theme.
type Layout struct {
	Kit        string   `json:"kit"`
	Version    string   `json:"version"`
	Theme      string   `json:"theme"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Mode       string   `json:"mode,omitempty"`
	Purpose    string   `json:"purpose,omitempty"`
	Components []string `json:"components,omitempty"`
	Source     string   `json:"source,omitempty"`
	File       string   `json:"file"`
}

// Address is how a layout is named on the command line and in a request.
func (l Layout) Address() string { return fmt.Sprintf("%s/%s/%s", l.Kit, l.Theme, l.ID) }

func themeLayouts(root, kit, version string) []Layout {
	var out []Layout
	themes, err := os.ReadDir(filepath.Join(root, "themes"))
	if err != nil {
		return nil
	}
	for _, th := range themes {
		if !th.IsDir() || !ValidID(th.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "themes", th.Name(), "_harnessly", "layouts", "layouts.json"))
		if err != nil {
			continue
		}
		var list []Layout
		if json.Unmarshal(raw, &list) != nil {
			continue
		}
		for _, l := range list {
			if !ValidID(l.ID) || l.File != l.ID+".dc.html" {
				continue
			}
			l.Kit, l.Version, l.Theme = kit, version, th.Name()
			out = append(out, l)
		}
	}
	return out
}

// Layouts lists the layouts of the newest version of every installed kit, or of one kit when kit is given.
func Layouts(dir, kit string) []Layout {
	out := []Layout{}
	for _, k := range List(dir) {
		if (kit != "" && k.ID != kit) || len(k.Versions) == 0 {
			continue
		}
		v := k.Versions[0].Version
		if root := Resolve(dir, k.ID, v, nil); root != "" {
			out = append(out, themeLayouts(root, k.ID, v)...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Address() < out[j].Address() })
	return out
}

// ParseLayoutAddress splits <kit>[@version]/<theme>/<layout>.
func ParseLayoutAddress(addr string) (kit, version, theme, layout string, err error) {
	parts := strings.Split(addr, "/")
	if len(parts) != 3 {
		return "", "", "", "", errors.New("a layout is <kit>[@version]/<theme>/<layout>")
	}
	kit, version, _ = strings.Cut(parts[0], "@")
	theme, layout = parts[1], parts[2]
	if !ValidID(kit) || !ValidID(theme) || !ValidID(layout) || (version != "" && !ValidVersion(version)) {
		return "", "", "", "", fmt.Errorf("%q is not a layout address (<kit>[@version]/<theme>/<layout>)", addr)
	}
	return kit, version, theme, layout, nil
}

// FindLayout is the layout at addr, with the folder of its kit version; the newest version when none is named.
func FindLayout(dir, addr string) (Layout, string, error) {
	kit, version, theme, id, err := ParseLayoutAddress(addr)
	if err != nil {
		return Layout{}, "", err
	}
	if version == "" {
		for _, k := range List(dir) {
			if k.ID == kit && len(k.Versions) > 0 {
				version = k.Versions[0].Version
			}
		}
	}
	root := Resolve(dir, kit, version, nil)
	if version == "" || root == "" {
		return Layout{}, "", fmt.Errorf("the kit %q is not installed", kit)
	}
	for _, l := range themeLayouts(root, kit, version) {
		if l.Theme == theme && l.ID == id {
			return l, root, nil
		}
	}
	return Layout{}, "", fmt.Errorf("%s %s has no layout %s/%s (design layouts lists them)", kit, version, theme, id)
}

// LayoutBoard is the layout's board file.
func LayoutBoard(root string, l Layout) ([]byte, error) {
	return readLimited(filepath.Join(root, "themes", l.Theme, "_harnessly", "layouts", l.File), 2<<20)
}

func readLimited(p string, max int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(b)) > max {
		err = errors.New("the file is too large")
	}
	return b, err
}

// InstallTheme copies the kit's theme folder (without _harnessly) into <project>/ds/<theme>/ unless it is already
// there, all or nothing. It reports whether it copied. The project's ds folder must be a real folder inside the
// project: a link there (which could lead the copy anywhere) is refused.
func InstallTheme(root, theme, project string) (bool, error) {
	if !ValidID(theme) {
		return false, fmt.Errorf("%q is not a theme", theme)
	}
	ds := filepath.Join(project, "ds")
	if st, err := os.Lstat(ds); err == nil && !st.IsDir() {
		return false, errors.New("the project's ds is not a folder")
	}
	if err := os.MkdirAll(ds, 0o755); err != nil {
		return false, err
	}
	realProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		return false, err
	}
	realDS, err := filepath.EvalSymlinks(ds)
	if err != nil || realDS != filepath.Join(realProject, "ds") {
		return false, errors.New("the project's ds folder leads outside the project")
	}
	dest := filepath.Join(realDS, theme)
	if _, err := os.Lstat(dest); err == nil {
		return false, nil
	}
	src := filepath.Join(root, "themes", theme)
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".ds-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)
	var total int64
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		if rel == "_harnessly" || strings.HasPrefix(rel, "_harnessly"+string(filepath.Separator)) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		out := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, in)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if total += n; total > MaxUnpackedBytes {
			return errors.New("the theme is too large")
		}
		return err
	})
	if err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return false, err
	}
	return true, nil
}
