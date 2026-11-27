// Package remote knows the folders that live on a Harnessly Connect device rather than on this machine: a chat whose
// folder is one of them runs its agent through Connect, and the places that would replace a missing folder with an
// existing one (a tmux session's start folder, a project's check) leave such a folder as it is.
package remote

import (
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Folder is a folder of a device, at the path the device itself gives it (its shell's form).
type Folder struct {
	Device string `json:"device"`
	Dir    string `json:"dir"`
}

var folders atomic.Pointer[[]Folder]

// Set replaces the known folders.
func Set(list []Folder) {
	c := append([]Folder(nil), list...)
	folders.Store(&c)
}

// Of is the device folder that path is, or lies in (the deepest one), and whether there is one.
func Of(path string) (Folder, bool) {
	p := folders.Load()
	if p == nil || path == "" {
		return Folder{}, false
	}
	path = filepath.Clean(path)
	best, found := Folder{}, false
	for _, f := range *p {
		if path == f.Dir || strings.HasPrefix(path, strings.TrimSuffix(f.Dir, "/")+"/") {
			if !found || len(f.Dir) > len(best.Dir) {
				best, found = f, true
			}
		}
	}
	return best, found
}

// Is says path is on a device.
func Is(path string) bool {
	_, ok := Of(path)
	return ok
}
