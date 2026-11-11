package gitread

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
)

// The person's own hand in the review of a chat's work: a file of a worktree written from the diff's new side, and
// the changed files committed. Only inside the repository at root, and a file that changed on disk since the page read
// it is not overwritten (expect is the hash of what the page read).

// WriteMax is the largest file the review writes.
const WriteMax = 4 << 20

// ContentHash is the hash a page sends as expect: sha256 of the text, hex.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// repoTop is root when it is the top of a git work tree, "" otherwise.
func repoTop(root string) string {
	if !strings.HasPrefix(root, "/") || strings.Contains(root, "\x00") {
		return ""
	}
	top, err := run{timeout: 15 * time.Second, maxBuffer: mib}.git("-C", root, "rev-parse", "--show-toplevel")
	top = jsTrim(top)
	if err != nil || top == "" || resolve(top) != filepath.Clean(root) {
		return ""
	}
	return filepath.Clean(root)
}

// inside is the absolute file of a repository path, false when it would leave the repository (by .. or a link) or
// reach git's own files: no part of the path may be .git in any case (a nested repository's or a submodule's too), and
// the file may not lie in the repository's git folders wherever they are.
func inside(top, rel string) (string, bool) {
	if _, ok := files.RepoPath([]string{rel}); !ok || rel == "" || strings.Contains(rel, "\\") {
		return "", false
	}
	clean := path.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", false
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.EqualFold(part, ".git") {
			return "", false
		}
	}
	abs := filepath.Join(top, filepath.FromSlash(clean))
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", false
	}
	realTop := realPath(top)
	if dir != realTop && !strings.HasPrefix(dir, realTop+string(filepath.Separator)) {
		return "", false
	}
	for _, g := range gitDirs(top) {
		if dir == g || strings.HasPrefix(dir, g+string(filepath.Separator)) {
			return "", false
		}
	}
	if fi, err := os.Lstat(abs); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	return abs, true
}

// realPath is p with its links followed, as EvalSymlinks gives the folder of a file to compare with.
func realPath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return resolve(p)
}

// gitDirs is where the repository at top keeps git's own files: its git folder and the common one of its worktrees.
func gitDirs(top string) []string {
	out, err := run{timeout: 15 * time.Second, maxBuffer: mib}.git("-C", top, "rev-parse", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return nil
	}
	var dirs []string
	for _, line := range strings.Split(jsTrim(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !filepath.IsAbs(line) {
			line = filepath.Join(top, line)
		}
		dirs = append(dirs, realPath(line))
	}
	return dirs
}

// WriteFile writes text into the file at path of the repository at root, when the file is still what the page read.
func WriteFile(root, path, text, expect string) (int, any) {
	top := repoTop(root)
	if top == "" {
		return 404, errBody{Error: "Repository not found"}
	}
	abs, ok := inside(top, path)
	if !ok {
		return 400, errBody{Error: "Invalid path"}
	}
	if len(text) > WriteMax {
		return 413, errBody{Error: "The file is too large"}
	}
	cur, err := os.ReadFile(abs)
	switch {
	case errors.Is(err, os.ErrNotExist):
		cur = nil
	case err != nil:
		return 500, errBody{Error: "The file is not readable"}
	}
	if ContentHash(string(cur)) != expect {
		return 409, map[string]any{"error": "changed", "hash": ContentHash(string(cur))}
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(abs); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".review-*")
	if err != nil {
		return 500, errBody{Error: "The file is not writable"}
	}
	_, werr := tmp.WriteString(text)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), mode) != nil || os.Rename(tmp.Name(), abs) != nil {
		_ = os.Remove(tmp.Name())
		return 500, errBody{Error: "The file is not writable"}
	}
	return 200, map[string]any{"hash": ContentHash(text)}
}

// CommitFiles commits the given changed files of the repository at root with a message, and answers the new commit.
func CommitFiles(root, message string, paths []string) (int, any) {
	top := repoTop(root)
	if top == "" {
		return 404, errBody{Error: "Repository not found"}
	}
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 4000 || strings.Contains(message, "\x00") || len(paths) == 0 || len(paths) > 500 {
		return 400, errBody{Error: "Invalid commit"}
	}
	for _, p := range paths {
		if _, ok := inside(top, p); !ok {
			return 400, errBody{Error: "Invalid path"}
		}
	}
	r := run{timeout: 60 * time.Second, maxBuffer: mib}
	if _, err := r.git(append([]string{"-C", top, "add", "--"}, paths...)...); err != nil {
		return 500, errBody{Error: "git add failed"}
	}
	if _, err := r.git(append([]string{"-C", top, "diff", "--cached", "--quiet", "--"}, paths...)...); err == nil {
		return 409, errBody{Error: "Nothing to commit"}
	}
	if _, err := r.git(append([]string{"-C", top, "commit", "-m", message, "--"}, paths...)...); err != nil {
		return 500, errBody{Error: "git commit failed"}
	}
	sha, _ := r.git("-C", top, "rev-parse", "HEAD")
	return 200, map[string]any{"sha": jsTrim(sha)}
}
