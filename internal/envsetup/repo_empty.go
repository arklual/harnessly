package envsetup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A platform may make a project's folder before its code is there, with a marker folder of its own in it. Such a
// folder holds nothing of the person's, so the repository is cloned into it rather than the step failing on «not a
// clone». A folder with any file of its own is left alone.

// kept says whether a top-level entry of a project's folder is the platform's or the person's own: a hidden folder
// other than .git. It is never removed, and the repository's copy of it is not moved over it.
func kept(e os.DirEntry) bool {
	return e.IsDir() && strings.HasPrefix(e.Name(), ".") && e.Name() != ".git"
}

// onlyMarkers says whether dir has no file at all, apart from what its hidden folders hold.
func onlyMarkers(dir string) bool {
	es, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range es {
		if kept(e) {
			continue
		}
		if !e.IsDir() || !emptyTree(filepath.Join(dir, e.Name())) {
			return false
		}
	}
	return true
}

// emptyTree: no file anywhere under dir (folders only).
func emptyTree(dir string) bool {
	es, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range es {
		if !e.IsDir() || !emptyTree(filepath.Join(dir, e.Name())) {
			return false
		}
	}
	return true
}

// cloneInto clones next to dir and moves the clone's entries in; the folder is as it was when anything fails.
func (ex *execution) cloneInto(i int, src source, dir string) {
	if msg := ex.lowDisk(ex.r.Home); msg != "" {
		ex.fail(i, msg)
		return
	}
	tmp := fmt.Sprintf("%s.harnessly-clone-%d", strings.TrimRight(dir, "/"), ex.r.now().UnixMilli())
	defer func() { _ = os.RemoveAll(ex.hostPath(tmp)) }()
	args := []string{"clone"}
	if src.Branch != "" {
		args = append(args, "--branch", src.Branch)
	}
	args = append(args, "--", src.URL, tmp)
	exit, err := ex.runLogged(i, Cmd{Name: "git", Args: args, Env: ex.baseEnv(), Timeout: gitTimeout}, "git "+strings.Join(args, " "))
	if err != nil || exit != 0 {
		ex.fail(i, "git clone не удался"+exitNote(exit, err))
		return
	}
	es, err := os.ReadDir(ex.hostPath(tmp))
	if err != nil {
		ex.fail(i, "git clone не удался: "+err.Error())
		return
	}
	var moved []string
	for _, e := range es {
		to := filepath.Join(ex.hostPath(dir), e.Name())
		if fi, err := os.Lstat(to); err == nil {
			if fi.IsDir() && strings.HasPrefix(e.Name(), ".") && e.Name() != ".git" {
				continue // the folder's own hidden folder stays; the repository's copy of it goes with tmp
			}
			// An empty folder of the project gives way to the repository's. Something written into it during the
			// clone (a chat may already work there) makes the removal fail, and the folder is then left as it is.
			if !emptyTree(to) {
				err = fmt.Errorf("в %s появились файлы, пока шёл клон", e.Name())
			} else {
				err = os.RemoveAll(to)
			}
			if err != nil {
				for _, m := range moved {
					_ = os.RemoveAll(filepath.Join(ex.hostPath(dir), m))
				}
				ex.fail(i, "клон не перенесён в "+dir+": "+err.Error())
				return
			}
		}
		if err := os.Rename(filepath.Join(ex.hostPath(tmp), e.Name()), to); err != nil {
			for _, m := range moved { // put the folder back as it was
				_ = os.RemoveAll(filepath.Join(ex.hostPath(dir), m))
			}
			ex.fail(i, "клон не перенесён в "+dir+": "+err.Error())
			return
		}
		moved = append(moved, e.Name())
	}
	ex.repoDirs[ex.items[i].ItemID] = dir
	ex.done(i, hubproto.EnvOK, "склонирован в "+dir)
}
