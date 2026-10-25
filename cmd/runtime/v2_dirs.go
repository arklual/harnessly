package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The folders offered for a new project: those one level below the machine's default folder, and the git folders one
// level below those that are not git themselves (a folder of projects). The list is cut at maxDirs.
const maxDirs = 200

// skipDir are folders nobody makes a project of.
var skipDir = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true, "venv": true,
	"__pycache__": true, "lost+found": true}

// v2Dirs lists the folders of root: git folders first, then by name.
func v2Dirs(ctx context.Context, root string) ([]wire.Dir, error) {
	out := []wire.Dir{}
	if root == "" {
		return out, nil
	}
	top, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	add := func(path, name string) (wire.Dir, bool) {
		d := wire.Dir{Path: path, Name: name}
		d.Git, d.Branch = gitBranch(path)
		entries, err := os.ReadDir(path)
		if err != nil {
			return d, false
		}
		d.Empty = len(entries) == 0
		return d, true
	}
	for _, e := range top {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !isCandidate(root, e) {
			continue
		}
		path := filepath.Join(root, e.Name())
		d, ok := add(path, e.Name())
		if !ok {
			continue
		}
		out = append(out, d)
		if d.Git || d.Empty {
			continue
		}
		subs, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			if !isCandidate(path, sub) {
				continue
			}
			subPath := filepath.Join(path, sub.Name())
			if git, _ := gitBranch(subPath); !git {
				continue
			}
			if sd, ok := add(subPath, sub.Name()); ok {
				out = append(out, sd)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Git != out[j].Git {
			return out[i].Git
		}
		if li, lj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); li != lj {
			return li < lj
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > maxDirs {
		out = out[:maxDirs]
	}
	return out, nil
}

// isCandidate is a folder (or a link to one) that is not hidden and not a build output.
func isCandidate(parent string, e os.DirEntry) bool {
	name := e.Name()
	if strings.HasPrefix(name, ".") || skipDir[name] {
		return false
	}
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink != 0 {
		st, err := os.Stat(filepath.Join(parent, name))
		return err == nil && st.IsDir()
	}
	return false
}

// gitBranch tells a git folder (.git is a folder, or a file that points to the folder of a worktree) and the branch its
// HEAD names, nil on a detached head or when HEAD is unreadable.
func gitBranch(dir string) (bool, *string) {
	dot := filepath.Join(dir, ".git")
	st, err := os.Stat(dot)
	if err != nil {
		return false, nil
	}
	gitDir := dot
	if !st.IsDir() {
		raw, err := os.ReadFile(dot)
		if err != nil {
			return true, nil
		}
		target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
		if !ok {
			return true, nil
		}
		gitDir = strings.TrimSpace(target)
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(dir, gitDir)
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return true, nil
	}
	if ref, ok := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/"); ok && ref != "" {
		return true, &ref
	}
	return true, nil
}
