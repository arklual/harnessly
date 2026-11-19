package design

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Finding a blender-lab project for a new 3D scene: the «Create» menu offers the projects it finds next to the
// library and the .blend files and scripts inside each, and takes a folder typed in.

// Scene3DProject is a folder that looks like a blender-lab project: scenes of code, built .blend files.
type Scene3DProject struct {
	Root    string   `json:"root"`
	Name    string   `json:"name"`
	Blends  []string `json:"blends"`  // relative to Root, the newest first
	Scripts []string `json:"scripts"` // relative to Root
}

const (
	scene3dDiscoverMax   = 30
	scene3dDiscoverFiles = 60
)

// scene3dScanRoots are the folders whose children are looked at: the library's neighbours (the workspace folder holds the
// projects of the machine), the workspace's own folder and the person's home. RUNTIME_SCENE3D_ROOTS adds more.
func (l *Library) scene3dScanRoots() []string {
	var roots []string
	add := func(p string) {
		if p != "" && filepath.IsAbs(p) {
			roots = append(roots, filepath.Clean(p))
		}
	}
	lib := l.Root()
	add(filepath.Dir(lib))
	add(filepath.Dir(filepath.Dir(lib)))
	add(l.cfg.WorkspaceDir)
	add(l.cfg.AltWorkspaceDir)
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
		add(filepath.Join(home, "projects"))
	}
	for _, p := range strings.Split(os.Getenv("RUNTIME_SCENE3D_ROOTS"), string(os.PathListSeparator)) {
		add(strings.TrimSpace(p))
	}
	return roots
}

// scene3dProjectAt reads one folder: nil when it holds neither scenes nor a built .blend.
func scene3dProjectAt(dir string) *Scene3DProject {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return nil
	}
	p := &Scene3DProject{Root: dir, Name: filepath.Base(dir), Blends: []string{}, Scripts: []string{}}
	type f struct {
		rel string
		at  int64
	}
	var blends []f
	for _, sub := range []string{"out", "."} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".blend") {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			blends = append(blends, f{filepath.ToSlash(filepath.Join(sub, e.Name())), info.ModTime().UnixNano()})
		}
	}
	sort.Slice(blends, func(i, j int) bool { return blends[i].at > blends[j].at })
	for i, b := range blends {
		if i >= scene3dDiscoverFiles {
			break
		}
		p.Blends = append(p.Blends, strings.TrimPrefix(b.rel, "./"))
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "scenes")); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".py") && len(p.Scripts) < scene3dDiscoverFiles {
				p.Scripts = append(p.Scripts, "scenes/"+e.Name())
			}
		}
		sort.Strings(p.Scripts)
	}
	if len(p.Blends) == 0 && len(p.Scripts) == 0 {
		return nil
	}
	return p
}

// Scene3DDiscover answers the projects to choose from: the one at root when a folder is given (an absolute path), else those found
// around the library. A folder that holds nothing of the kind is an error in words, not an empty list.
func (l *Library) Scene3DDiscover(root string) ([]Scene3DProject, error) {
	if root != "" {
		if !filepath.IsAbs(root) {
			return nil, deckErr(http400, "The folder is an absolute path", "Папка — абсолютный путь")
		}
		p := scene3dProjectAt(filepath.Clean(root))
		if p == nil {
			return nil, deckErr(http404, "No scenes or .blend files in that folder", "В этой папке нет сцен и файлов .blend")
		}
		return []Scene3DProject{*p}, nil
	}
	out := []Scene3DProject{}
	seen := map[string]bool{}
	skip := filepath.Clean(l.Root())
	for _, base := range l.scene3dScanRoots() {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
				continue
			}
			dir := filepath.Join(base, e.Name())
			if seen[dir] || dir == skip {
				continue
			}
			seen[dir] = true
			if p := scene3dProjectAt(dir); p != nil {
				out = append(out, *p)
				if len(out) >= scene3dDiscoverMax {
					return out, nil
				}
			}
		}
	}
	// Projects with scenes of code first: they are blender-lab's shape, a folder of stray .blend files comes after.
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Scripts) > 0 && len(out[j].Scripts) == 0 })
	return out, nil
}

// CreateScene3D makes a new 3D scene document over a project of this machine and answers its id and title.
func (l *Library) CreateScene3D(title, person string, s Scene3DSettings) (CreatedProject, error) {
	s.V = 1
	if err := ValidateScene3DSettings(s); err != nil {
		return CreatedProject{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSuffix(filepath.Base(s.Blend), filepath.Ext(s.Blend))
	}
	p, err := l.CreateProject(title)
	if err != nil {
		return CreatedProject{}, err
	}
	if err := l.InitScene3D(p.ID, s); err != nil {
		return CreatedProject{}, err
	}
	l.AnnounceNewBy(p.ID, person)
	return p, nil
}
