package design

import (
	"path/filepath"
	"strings"
)

// What a text names of the library: the design an agent asks the pane to open (`open design <id|path|title>`) and the
// design whose files an agent writes.

// DesignOfPath is the design whose folder holds an absolute path (the folder itself or a file in it), "" for none.
func (l *Library) DesignOfPath(p string) string {
	if !filepath.IsAbs(p) {
		return ""
	}
	p = filepath.Clean(p)
	root := l.Root()
	roots := []string{root}
	if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
		roots = append(roots, real)
	}
	for _, r := range roots {
		if ref := folderUnder(r, p); IsLibraryID(ref) && l.ReadArtifact(ref) != nil {
			return ref
		}
	}
	return ""
}

// RefOf is the design a text names: its id, its folder or a file in it, or its title (letter case aside, when only
// one design has it); "" for none.
func (l *Library) RefOf(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if IsLibraryID(text) && l.ReadArtifact(text) != nil {
		return text
	}
	if ref := l.DesignOfPath(text); ref != "" {
		return ref
	}
	found := ""
	for _, a := range l.ListArtifacts() {
		if !strings.EqualFold(strings.TrimSpace(a.Title), text) {
			continue
		}
		if found != "" {
			return ""
		}
		found = a.ID
	}
	return found
}

// BoardOfPath is the design a board file lies in and the board as the design's page names it (its path in the
// project folder); "" for a file that is not a board of the library.
func (l *Library) BoardOfPath(p string) (ref, board string) {
	ref = l.DesignOfPath(p)
	if ref == "" || !strings.HasSuffix(p, dcSuffix) {
		return "", ""
	}
	p = filepath.Clean(p)
	dir := l.ProjectDir(ref)
	dirs := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		dirs = append(dirs, real)
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	for _, d := range dirs {
		if rel, err := filepath.Rel(d, p); err == nil && !strings.HasPrefix(rel, "..") {
			return ref, filepath.ToSlash(rel)
		}
	}
	return "", ""
}
