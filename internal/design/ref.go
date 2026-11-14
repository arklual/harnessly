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
	for _, r := range l.roots() {
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

// BoardOfPath is the design an absolute path is a page of, and the page as the design's viewer names it (its path in the
// project folder): a board, an .html of the project. The design's own folder (or its project/) is the design with no
// board (""). Any other file (a script, a style, the design system's notes) is ("", ""): it opens as the file it is, as
// does a path outside the library.
func (l *Library) BoardOfPath(p string) (ref, board string) {
	if ref = l.DesignOfPath(p); ref == "" {
		return "", ""
	}
	p = filepath.Clean(p)
	paths := []string{p}
	if real, err := filepath.EvalSymlinks(p); err == nil && real != p {
		paths = append(paths, real)
	}
	for _, root := range l.roots() {
		folder := filepath.Join(root, ref)
		for _, q := range paths {
			if q == folder || q == filepath.Join(folder, ProjectDirname) {
				return ref, ""
			}
			rel, err := filepath.Rel(filepath.Join(folder, ProjectDirname), q)
			if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				continue
			}
			if low := strings.ToLower(rel); strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm") {
				return ref, filepath.ToSlash(rel)
			}
		}
	}
	return "", ""
}

// roots are the library's folder as configured and, when it is a link, where it leads.
func (l *Library) roots() []string {
	root := l.Root()
	if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
		return []string{root, real}
	}
	return []string{root}
}
