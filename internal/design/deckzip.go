package design

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// Archives of a project: «ZIP проекта» is its project/ folder (the boards with everything they load), «Весь
// проект» is what makes the project whole elsewhere: the description, the pages, notes and comments (meta/) and the
// boards. The service folders (names starting with a dot: recordings, exports, thumbnails, the agent's notes) stay
// out, as do files that look like credentials; a link is followed only to a file inside the project itself.

func init() {
	RegisterDeckExporter(DeckExportZip, DeckExporter{Ext: "zip", Run: func(ctx context.Context, run DeckExportRun) error {
		return zipProject(ctx, run, false)
	}})
	RegisterDeckExporter(DeckExportBundle, DeckExporter{Ext: "zip", Run: func(ctx context.Context, run DeckExportRun) error {
		return zipProject(ctx, run, true)
	}})
}

// deckZipMax bounds an archive: a project is pages and their pictures; more than this is something that should not be
// in it.
const deckZipMax = 2 << 30

var deckSecrets = files.NewAccess(nil, nil)

func zipProject(ctx context.Context, run DeckExportRun, whole bool) error {
	dir := run.L.ArtifactDir(run.Artifact)
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return errors.New(locale.Pick("The design is gone", "Макета больше нет"))
	}
	from := root
	if !whole {
		from = filepath.Join(root, ProjectDirname)
	}
	paths, total, err := deckZipList(ctx, root, from)
	if err != nil {
		return err
	}
	if total > deckZipMax {
		return errors.New(locale.Pick("The project is too big for one archive", "Проект слишком велик для одного архива"))
	}
	// The files are read through the project's root: a file changed into a link after the list was made still cannot
	// lead out of the project, and the last name is not followed at all.
	inside, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer inside.Close()
	out, err := os.Create(run.Out)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	done := int64(0)
	written := map[string]bool{}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p.name)
		n, err := deckZipFile(zw, inside, p, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		written[filepath.ToSlash(rel)] = true
		done += n
		if total > 0 {
			run.Progress(float64(done) / float64(total))
		}
	}
	// The kit the project is pinned to goes with it, where its boards look for it (ds/<mount>/).
	prefix := "ds/"
	if whole {
		prefix = ProjectDirname + "/ds/"
	}
	for mount, dir := range run.L.KitMountDirs(run.Artifact) {
		kitDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		list, _, err := deckZipList(ctx, kitDir, kitDir)
		if err != nil {
			continue
		}
		kitRoot, err := os.OpenRoot(kitDir)
		if err != nil {
			return err
		}
		for _, p := range list {
			rel, _ := filepath.Rel(kitDir, p.name)
			rel = filepath.ToSlash(rel)
			if rel == "_harnessly" || strings.HasPrefix(rel, "_harnessly/") || written[prefix+mount+"/"+rel] {
				continue
			}
			if _, err := deckZipFile(zw, kitRoot, p, prefix+mount+"/"+rel); err != nil {
				kitRoot.Close()
				return err
			}
		}
		kitRoot.Close()
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return out.Close()
}

// deckZipEntry is a file of the archive: its place in the walk (name), the file read (real, relative to the project's
// root: a link's target) and its size when listed, which is all that is read of it.
type deckZipEntry struct {
	name, real string
	size       int64
}

// deckZipList is what goes into the archive: regular files under from (links resolved inside root), in walk order.
func deckZipList(ctx context.Context, root, from string) ([]deckZipEntry, int64, error) {
	var out []deckZipEntry
	var total int64
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if d != nil && d.IsDir() && p != from {
				return fs.SkipDir
			}
			return nil
		}
		if p == from {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if strings.HasPrefix(d.Name(), ".") || deckSecrets.Secret(filepath.ToSlash(rel)) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		real := p
		switch {
		case d.IsDir():
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			r, err := filepath.EvalSymlinks(p)
			if err != nil || (r != root && !strings.HasPrefix(r, root+string(filepath.Separator))) {
				return nil
			}
			inner, _ := filepath.Rel(root, r)
			if deckSecrets.Secret(filepath.ToSlash(inner)) || deckServicePath(inner) {
				return nil
			}
			real = r
		case !d.Type().IsRegular():
			return nil
		}
		st, err := os.Stat(real)
		if err != nil || !st.Mode().IsRegular() {
			return nil
		}
		inner, err := filepath.Rel(root, real)
		if err != nil || strings.HasPrefix(inner, "..") {
			return nil
		}
		total += st.Size()
		out = append(out, deckZipEntry{name: p, real: inner, size: st.Size()})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(out) == 0 {
		return nil, 0, errors.New(locale.Pick("The project has no files yet", "В проекте пока нет файлов"))
	}
	return out, total, nil
}

// deckZipStored are the types that are compressed already.
var deckZipStored = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".avif": true, ".mp4": true, ".webm": true,
	".mov": true, ".mp3": true, ".m4a": true, ".ogg": true, ".woff": true, ".woff2": true, ".zip": true, ".gz": true,
	".pdf": true,
}

func deckZipFile(zw *zip.Writer, inside *os.Root, e deckZipEntry, name string) (int64, error) {
	f, err := inside.OpenFile(e.real, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return 0, nil
	}
	method := zip.Deflate
	if deckZipStored[strings.ToLower(filepath.Ext(name))] {
		method = zip.Store
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: st.ModTime()})
	if err != nil {
		return 0, err
	}
	return io.Copy(w, io.LimitReader(f, e.size))
}

// deckServicePath says a path inside the project lies in a service folder (or is a service file): a part of it starts
// with a dot. A link is held to the same rule as the walk, so it cannot carry such a file into the archive.
func deckServicePath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}
