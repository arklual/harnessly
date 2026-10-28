package design

import (
	"path/filepath"
	"strings"
)

// BoardWrite is a board file a tool call writes: the project it is in and the file's name.
type BoardWrite struct{ Project, File string }

// BoardWrites are the boards (*.dc.html under a project's project/ folder) a writing tool call touches. The hook of
// an agent takes (or renews) the lease on each before the write goes on.
func BoardWrites(in GuardInput) []BoardWrite {
	if !writingTools[in.ToolName] {
		return nil
	}
	cwd := in.Cwd
	var out []BoardWrite
	for _, f := range append([]string{in.ToolInput.FilePath, in.ToolInput.NotebookPath, in.ToolInput.Path}, patchTargets(in.ToolInput.Command)...) {
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) && cwd != "" {
			f = filepath.Join(cwd, f)
		}
		f = filepath.Clean(f)
		if !strings.HasSuffix(f, dcSuffix) {
			continue
		}
		root, id, ok := projectTop(f)
		if !ok {
			continue
		}
		if rel, err := filepath.Rel(filepath.Join(root, id, ProjectDirname), f); err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		out = append(out, BoardWrite{Project: id, File: filepath.Base(f)})
	}
	return out
}
