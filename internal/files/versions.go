package files

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

const (
	// MaxVersionBytes is the largest file kept in the history: a bigger one is saved with none.
	MaxVersionBytes = 100 * 1024 * 1024
	// maxVersions and maxVersionsBytes bound one file's history; the oldest go first.
	maxVersions      = 30
	maxVersionsBytes = 300 * 1024 * 1024
)

var versionName = regexp.MustCompile(`^\d+-\d+$`)

// Version is one kept state of a file; its ID is the revision the file had.
type Version struct {
	ID   string
	At   int64 // ms
	Size int64
}

func (a *Access) versionsOf(real string) string {
	sum := sha256.Sum256([]byte(real))
	return filepath.Join(a.VersionsDir, hex.EncodeToString(sum[:16]))
}

// listVersions is the file's history, newest first.
func (a *Access) listVersions(real string) []Version {
	entries, err := os.ReadDir(a.versionsOf(real))
	if err != nil {
		return nil
	}
	out := make([]Version, 0, len(entries))
	for _, entry := range entries {
		if !versionName.MatchString(entry.Name()) {
			continue
		}
		mtime, size, _ := strings.Cut(entry.Name(), "-")
		nanos, err := strconv.ParseInt(mtime, 10, 64)
		if err != nil {
			continue
		}
		bytes, err := strconv.ParseInt(size, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, Version{ID: entry.Name(), At: nanos / 1_000_000, Size: bytes})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

// keepVersion puts the file as it is now into its history, once for each revision. A file too big for it, or a disk that
// refuses, leaves no version: the history is a help, and never stands in the way of working with the file.
func (a *Access) keepVersion(real string, st os.FileInfo) {
	if a.VersionsDir == "" || st.Size() > MaxVersionBytes {
		return
	}
	dir := a.versionsOf(real)
	id := FileRevision(st)
	target := filepath.Join(dir, id)
	if _, err := os.Stat(target); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if err := copyInto(real, target, 0o600); err != nil {
		return
	}
	a.pruneVersions(real)
}

func (a *Access) pruneVersions(real string) {
	var total int64
	for at, version := range a.listVersions(real) {
		total += version.Size
		if at >= maxVersions || total > maxVersionsBytes {
			_ = os.Remove(filepath.Join(a.versionsOf(real), version.ID))
		}
	}
}

// copyInto writes a copy through a temporary file beside the target: a copy that breaks off leaves nothing under its name.
func copyInto(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(to), ".harnessly-copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, in)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), to)
}

func (a *Access) serveVersions(w http.ResponseWriter, r *http.Request, real string) {
	list := []any{}
	for _, version := range a.listVersions(real) {
		list = append(list, Object{{"id", version.ID}, {"at", version.At}, {"size", version.Size}})
	}
	WriteJSON(w, r, http.StatusOK, Object{{"versions", list}})
}

// restoreVersion puts a kept state back over the file. What the file holds now goes into the history first, so a restore
// can itself be undone.
func (a *Access) restoreVersion(w http.ResponseWriter, r *http.Request, real string, st os.FileInfo, id string) {
	source := filepath.Join(a.versionsOf(real), id)
	if !versionName.MatchString(id) {
		WriteJSON(w, r, http.StatusBadRequest, Error(locale.Pick("No such version", "Такой версии нет")))
		return
	}
	if _, err := os.Stat(source); err != nil {
		WriteJSON(w, r, http.StatusNotFound, Error(locale.Pick("No such version", "Такой версии нет")))
		return
	}
	a.keepVersion(real, st)
	if err := copyInto(source, real, st.Mode().Perm()); err != nil {
		WriteJSON(w, r, http.StatusInternalServerError, Error(locale.Pick("The version was not restored", "Версия не восстановилась")))
		return
	}
	next, err := os.Stat(real)
	if err != nil {
		WriteJSON(w, r, http.StatusInternalServerError, Error(locale.Pick("The version was not restored", "Версия не восстановилась")))
		return
	}
	a.keepVersion(real, next)
	WriteJSON(w, r, http.StatusOK, revisionAnswer(next))
}
