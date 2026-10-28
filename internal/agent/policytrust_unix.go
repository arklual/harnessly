//go:build linux || darwin

package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// fileWritableByAgent says whether the account this process runs as can change what the policy path names: the file,
// or a folder on the way to it (which lets it replace the file or the folder that holds it). A policy that is not
// writable by that account (root-owned in a folder only root changes, or on a read-only mount) is the owner's alone; one
// that is can be rewritten by anything that runs as the agent, and the hub reaches the runtime, which does. When it
// cannot tell, the answer is that it is writable.
//
// A file that is not there is judged by the nearest folder on its way that is: the account makes the rest if it can
// write that one.
func fileWritableByAgent(path string) bool {
	return writableAsNamed(path) || writableResolved(path)
}

// writableAsNamed judges the path as it is spelled, before any link is followed: every folder on the way (a link on the
// way is a name in some folder, and whoever can write that folder can point it elsewhere) and the file itself.
func writableAsNamed(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() && changeable(abs) {
		return true
	}
	for child := abs; ; {
		dir := filepath.Dir(child)
		if dir == child {
			return false
		}
		if _, serr := os.Lstat(dir); serr == nil && changeable(dir) && !shielded(dir, child) {
			return true
		}
		child = dir
	}
}

// writableResolved judges the place the path lands once every link is followed.
func writableResolved(path string) bool {
	real, err := filepath.EvalSymlinks(path)
	exists := err == nil
	if !exists {
		rest, dir := filepath.Base(path), filepath.Dir(path)
		for {
			resolved, derr := filepath.EvalSymlinks(dir)
			if derr == nil {
				real = filepath.Join(resolved, rest)
				break
			}
			parent := filepath.Dir(dir)
			if !errors.Is(derr, fs.ErrNotExist) || parent == dir {
				return true
			}
			rest, dir = filepath.Join(filepath.Base(dir), rest), parent
		}
	}
	if exists && changeable(real) {
		return true
	}
	for child := real; ; {
		dir := filepath.Dir(child)
		if dir == child {
			return false
		}
		if _, serr := os.Lstat(dir); serr == nil && changeable(dir) && !shielded(dir, child) {
			return true
		}
		child = dir
	}
}

// changeable: the process can write the path, or own it and so make itself able to. A read-only mount stops both.
func changeable(p string) bool {
	err := syscall.Access(p, 2) // W_OK
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EROFS) {
		return false
	}
	var st syscall.Stat_t
	if syscall.Stat(p, &st) != nil {
		return true
	}
	return int(st.Uid) == os.Geteuid()
}

// shielded: dir has the sticky bit and neither dir nor child is the process's own, so it cannot replace child (/tmp).
func shielded(dir, child string) bool {
	me := os.Geteuid()
	if me == 0 {
		return false
	}
	var d, c syscall.Stat_t
	if syscall.Stat(dir, &d) != nil || syscall.Lstat(child, &c) != nil {
		return false
	}
	return d.Mode&syscall.S_ISVTX != 0 && int(d.Uid) != me && int(c.Uid) != me
}
