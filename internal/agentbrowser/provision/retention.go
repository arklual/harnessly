package provision

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var versionDirectory = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$`)

type noLease struct{}

func (noLease) Close() error { return nil }

func cacheVersion(chrome string) (root, version string) {
	path, err := filepath.Abs(chrome)
	if err != nil {
		return "", ""
	}
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		parent := filepath.Dir(dir)
		if filepath.Base(parent) == "chrome" && filepath.Base(filepath.Dir(parent)) == "agent-browser" &&
			versionDirectory.MatchString(filepath.Base(dir)) {
			return parent, filepath.Base(dir)
		}
	}
	return "", ""
}

// Retain prevents an upgrade from removing resources a running Chrome may still load.
// Hosts and probes hold this lease until Chrome exits; system installations need no lease.
func Retain(chrome string) (io.Closer, error) {
	root, version := cacheVersion(chrome)
	if root == "" {
		return noLease{}, nil
	}
	lock, err := os.OpenFile(filepath.Join(root, ".use-"+version+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_SH); err != nil {
		_ = lock.Close()
		return nil, err
	}
	// Cleanup may have won the lock just before this host started.
	if !executable(chrome) {
		_ = lock.Close()
		return nil, errors.New("cached Chrome was removed before startup; retry preparation")
	}
	return lock, nil
}

func prune(root string, selected ...string) {
	keep := map[string]bool{}
	for _, path := range selected {
		_, version := cacheVersion(path)
		keep[version] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		version := entry.Name()
		if !entry.IsDir() || version == Version || keep[version] || !versionDirectory.MatchString(version) {
			continue
		}
		pruneVersion(root, version)
	}
}

func pruneVersion(root, version string) {
	lock, err := os.OpenFile(filepath.Join(root, ".use-"+version+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return
	}
	// Earlier hosts have no lease. Keep their resources, and conservatively keep everything if ps fails.
	processes, err := exec.Command("ps", "-u", strconv.Itoa(os.Getuid()), "-o", "args=").Output()
	if err != nil {
		return
	}
	dir := filepath.Join(root, version)
	// Match the version component as well, since an older host may use a symlinked data root.
	if strings.Contains(string(processes), string(filepath.Separator)+version+string(filepath.Separator)) {
		return
	}
	_ = os.RemoveAll(dir)
}
