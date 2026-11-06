package design

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// demoMarker says the library already got the demo projects once: a person who deleted them does not get them back
// on the next start, only by asking (SeedDemo with force).
const demoMarker = ".demo-seeded"

// DemoSeeded tells whether the library already got the demo projects.
func (l *Library) DemoSeeded() bool {
	_, err := os.Stat(filepath.Join(l.Root(), demoMarker))
	return err == nil
}

// SeedDemo copies the demo projects of fsys (one folder per project, named by its library id) into the library and
// gives each the engine new projects get. A project already in the library is left as it is. Without force it runs
// once per library. It returns the ids it copied.
func (l *Library) SeedDemo(fsys fs.FS, force bool) ([]string, error) {
	if !force && l.DemoSeeded() {
		return nil, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(l.Root(), 0o755); err != nil {
		return nil, err
	}
	defer l.listing.invalidateAll()
	engine := l.engineBytes()
	var copied []string
	for _, e := range entries {
		id := e.Name()
		if !e.IsDir() || !IsLibraryID(id) {
			continue
		}
		dst := l.ArtifactDir(id)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		tmp := dst + ".demo-tmp"
		_ = os.RemoveAll(tmp)
		if err := copyFS(fsys, id, tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return copied, err
		}
		if err := os.WriteFile(filepath.Join(tmp, ProjectDirname, "support.js"), engine, 0o644); err != nil {
			_ = os.RemoveAll(tmp)
			return copied, err
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.RemoveAll(tmp)
			return copied, err
		}
		copied = append(copied, id)
	}
	if err := os.WriteFile(filepath.Join(l.Root(), demoMarker), []byte("1\n"), 0o644); err != nil {
		return copied, err
	}
	return copied, nil
}

func copyFS(fsys fs.FS, root, dst string) error {
	return fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.FromSlash(p))
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !d.Type().IsRegular() {
			return errors.New("design: demo entry is not a file: " + path.Clean(p))
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}
