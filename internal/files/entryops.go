package files

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// What «Файлы» changes in a folder: a new empty file, a new name for an entry in the same folder, and an
// entry moved to the trash (the system's, as a file manager does it: it can be put back). The caller has checked the
// folder; the names are checked here. A link is renamed or trashed itself, never what it leads to.

const entryNameMax = 255

// EntryError is a refusal of a change said for the person: it names no path.
type EntryError struct{ text string }

func (e *EntryError) Error() string { return e.text }

func entryError(en, ru string) error { return &EntryError{text: locale.Pick(en, ru)} }

// ErrEntryExists: the name is taken in the folder.
var ErrEntryExists = entryError("Something of this name is there already", "Здесь уже есть что-то с таким именем")

// SecretName is whether a file's name looks like credentials (.env, keys, certificates).
func SecretName(name string) bool { return secretFile.MatchString(name) }

// CheckEntryName refuses a name that is not one entry of a folder, or that looks like credentials.
func CheckEntryName(name string) error {
	switch {
	case name == "", name == ".", name == "..", len(name) > entryNameMax:
		return entryError("This name cannot be used", "Такое имя нельзя")
	case strings.ContainsAny(name, "/\x00"):
		return entryError("A name cannot have / in it", "В имени не может быть /")
	case secretFile.MatchString(name):
		return entryError("Files like this are not made from the workspace", "Такие файлы из рабочего пространства не создаются")
	}
	return nil
}

// NewEmptyFile makes an empty file in dir; it answers its path.
func NewEmptyFile(dir, name string) (string, error) {
	if err := CheckEntryName(name); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return "", ErrEntryExists
	}
	if err != nil {
		return "", err
	}
	return p, f.Close()
}

// RenameEntry gives the entry at abs a new name in its folder; it answers the new path.
func RenameEntry(abs, name string) (string, error) {
	if err := CheckEntryName(name); err != nil {
		return "", err
	}
	to := filepath.Join(filepath.Dir(abs), name)
	if to == abs {
		return abs, nil
	}
	// A different name that is the same file on a disk that ignores the case is a rename of the case only.
	if st, err := os.Lstat(to); err == nil {
		if was, werr := os.Lstat(abs); werr == nil && os.SameFile(st, was) {
			return to, os.Rename(abs, to)
		}
		return "", ErrEntryExists
	}
	if err := renameNoReplace(abs, to); err != nil {
		return "", err
	}
	return to, nil
}

// renameChecked is a rename where the system cannot refuse to replace: the place is checked just before.
func renameChecked(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return ErrEntryExists
	}
	return os.Rename(from, to)
}

// TrashDir is the system's trash of the person (the freedesktop.org one): $XDG_DATA_HOME/Trash, or
// ~/.local/share/Trash.
func TrashDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "Trash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash"), nil
}

// TrashEntry moves the entry at abs into trash (a folder as TrashDir names it), with the note a file manager reads
// to put it back. An entry on another disk than the trash is not moved: it would have to be copied.
func TrashEntry(abs, trash string, now time.Time) error {
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	filesDir, infoDir := filepath.Join(trash, "files"), filepath.Join(trash, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return err
	}
	base := filepath.Base(abs)
	for n := 0; n < 1000; n++ {
		name := base
		if n > 0 {
			ext := filepath.Ext(base)
			name = fmt.Sprintf("%s.%d%s", strings.TrimSuffix(base, ext), n+1, ext)
		}
		// A name is free when neither the note nor the entry is there (a file manager may have left one without the other).
		if _, err := os.Lstat(filepath.Join(filesDir, name)); err == nil {
			continue
		}
		info, err := os.OpenFile(filepath.Join(infoDir, name+".trashinfo"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		note := "[Trash Info]\nPath=" + trashPath(abs) + "\nDeletionDate=" + now.Format("2006-01-02T15:04:05") + "\n"
		_, werr := info.WriteString(note)
		cerr := info.Close()
		if werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = renameNoReplace(abs, filepath.Join(filesDir, name))
		}
		if werr != nil {
			_ = os.Remove(filepath.Join(infoDir, name+".trashinfo"))
			if errors.Is(werr, ErrEntryExists) {
				continue
			}
			if errors.Is(werr, syscall.EXDEV) {
				return entryError("This is on another disk than the trash: it was not moved", "Это на другом диске, чем корзина: не перенесено")
			}
			return werr
		}
		return nil
	}
	return entryError("The trash has too many of this name", "В корзине слишком много с таким именем")
}

// trashPath is the path as the trash note writes it: each part escaped as in a URL.
func trashPath(abs string) string {
	parts := strings.Split(abs, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
