package design

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"syscall"
)

// The new-project template the runtime ships, used when the library has no .template/ of its own: a board template
// that pulls in ./support.js, and support.js, the board engine of the design tool's format (Anthropic's dc-runtime;
// the deployment may point RUNTIME_DESIGN_ENGINE_FILE at another build of it). Every new project gets meta/ besides,
// whichever template it came from.

//go:embed all:newproject
var newProjectFiles embed.FS

const newProjectRoot = "newproject"

func defaultBoardTemplate() []byte {
	b, err := newProjectFiles.ReadFile(newProjectRoot + "/" + ProjectDirname + "/_board-template.html")
	if err != nil {
		panic(err)
	}
	return b
}

// engine is the support.js a new project gets from the shipped template.
func (l *Library) engine() []byte {
	if l.cfg.EngineFile != "" {
		if b, err := readFileLimited(l.cfg.EngineFile, 16<<20); err == nil {
			return b
		}
		l.warn("design: the configured board engine cannot be read, the shipped one is used", nil)
	}
	b, err := newProjectFiles.ReadFile(newProjectRoot + "/" + ProjectDirname + "/" + SupportJSName)
	if err != nil {
		panic(err)
	}
	return b
}

// hasOwnTemplate: the library keeps its own .template/ folder (a link is judged by checkTemplate).
func (l *Library) hasOwnTemplate() bool {
	_, err := os.Lstat(l.TemplateDir())
	return err == nil
}

// writeShippedTemplate puts the shipped template into a fresh folder opened as into.
func (l *Library) writeShippedTemplate(into *os.File) error {
	return fs.WalkDir(newProjectFiles, newProjectRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == newProjectRoot {
			return err
		}
		rel := p[len(newProjectRoot)+1:]
		target := viaFd(into, rel)
		if d.IsDir() {
			return os.Mkdir(target, 0o777)
		}
		data, err := newProjectFiles.ReadFile(p)
		if err != nil {
			return err
		}
		if path.Base(rel) == SupportJSName && path.Dir(rel) == ProjectDirname {
			data = l.engine()
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o666)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	})
}

// writeNewMeta gives the staged project its meta/: the template's own project.json settings (release, kits,
// tailwind) are kept, its pages never are, so two projects never share a page id. A release named here (the one new
// projects start on) wins over the template's.
func writeNewMeta(stage, title, createdAt, canvasKey, release string) error {
	metaDir := filepath.Join(stage, MetaDirname)
	m := ProjectMeta{Title: title, CreatedAt: createdAt, Release: LegacyRelease, Kits: []any{}, CanvasKey: canvasKey}
	if o, ok := readSmallJSON(filepath.Join(metaDir, MetaProjectFile)); ok {
		t := parseProjectMeta(o, title)
		m.Release, m.Kits, m.Tailwind = t.Release, t.Kits, t.Tailwind
		for _, k := range []string{"title", "source", "createdAt", "release", "kits", "tailwind", "v", "canvasKey"} {
			o.Delete(k)
		}
		m.raw = o
	}
	if release != "" {
		m.Release = release
	}
	if err := os.RemoveAll(metaDir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(metaDir, MetaPagesDirname), 0o777); err != nil {
		return err
	}
	_, err := writeIfChanged(filepath.Join(metaDir, MetaProjectFile), m.document())
	return err
}
