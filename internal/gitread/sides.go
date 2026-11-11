package gitread

import (
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
)

// FileSides is both versions of one file of the changes panel, for the review's diff, read when the file is opened:
// for a commit (`sha`) at its parent and at the commit, for uncommitted work at HEAD and in the working tree. `root`
// must be a repository's top folder; `path` is the file now and `oldPath` what it was (the same when absent, nothing
// when `added=1`); an empty `path` is a deleted file.
func FileSides(q url.Values) (int, any) {
	one := func(k string) (string, bool) {
		if v := q[k]; len(v) == 1 {
			return v[0], true
		}
		return "", false
	}
	root, _ := one("root")
	sha, _ := one("sha")
	newPath, _ := one("path")
	oldPath, hasOld := one("oldPath")
	if !hasOld {
		oldPath = newPath
	}
	if added, _ := one("added"); added == "1" {
		oldPath = ""
	}
	if !strings.HasPrefix(root, "/") || strings.Contains(root, "\x00") || (sha != "" && !shaRe.MatchString(sha)) {
		return 400, errBody{Error: "Invalid root or sha"}
	}
	for _, p := range []string{newPath, oldPath} {
		if _, ok := files.RepoPath([]string{p}); p != "" && !ok {
			return 400, errBody{Error: "Invalid path"}
		}
	}
	if newPath == "" && oldPath == "" {
		return 400, errBody{Error: "Invalid path"}
	}
	r := run{timeout: 15 * time.Second, maxBuffer: mib}
	top, err := r.git("-C", root, "rev-parse", "--show-toplevel")
	top = jsTrim(top)
	if err != nil || top == "" || resolve(top) != filepath.Clean(root) {
		return 404, errBody{Error: "Repository not found"}
	}
	oldRef, newRef := "HEAD", "WORKTREE"
	if sha != "" {
		oldRef, newRef = sha+"^", sha
	}
	oldSide := make(chan files.Side, 1)
	go func() { oldSide <- files.LoadSide(root, oldRef, oldPath, oldPath != "") }()
	newSide := files.LoadSide(root, newRef, newPath, newPath != "")
	return 200, files.FileContent(<-oldSide, newSide)
}
