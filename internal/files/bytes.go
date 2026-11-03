package files

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// MaxSaveBytes is the largest file the viewer may write back.
const MaxSaveBytes = 200 * 1024 * 1024

// EditableTypes are the files the viewer edits as bytes. Only a file that exists is written: this is no way to make files.
var EditableTypes = map[string]bool{"xlsx": true, "xlsm": true, "csv": true, "tsv": true, "docx": true, "docm": true, "pptx": true, "pptm": true}

// FileRevision names a file's state: a save over an older one is refused.
func FileRevision(st os.FileInfo) string {
	return fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
}

// editable resolves a file the viewer may write: the same places as opening it, of a type it edits.
func (a *Access) editable(requested string) (string, os.FileInfo, *Target) {
	real, refused := a.locate(requested)
	if refused != nil {
		return "", nil, refused
	}
	if !EditableTypes[lastDotExt(real)] {
		t := refuse(415, locale.Pick("This file cannot be edited here", "Этот файл здесь не правится"))
		return "", nil, &t
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		t := refuse(404, locale.Pick("File not found", "Файл не найден"))
		return "", nil, &t
	}
	return real, st, nil
}

// ServeFileBytes answers /api/v2/file-bytes?path=: GET the file's revision (of anything the pane can open, which is how
// an open file hears it changed); of a file it edits, GET &versions=1 its history, PUT the whole new file over the ?revision=
// it was read at (a file changed since is a 409 and stays as it is; no ?revision= writes over whatever is there), and POST
// &restore=<id> a kept state back.
func (a *Access) ServeFileBytes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	if r.Method == http.MethodGet && query.Get("versions") == "" {
		a.serveRevision(w, r, query.Get("path"))
		return
	}
	real, st, refused := a.editable(query.Get("path"))
	if refused != nil {
		WriteJSON(w, r, refused.Status, Error(refused.Text))
		return
	}
	switch {
	case r.Method == http.MethodGet:
		a.serveVersions(w, r, real)
	case r.Method == http.MethodPut:
		a.saveBytes(w, r, real, st)
	case r.Method == http.MethodPost && query.Get("restore") != "":
		a.restoreVersion(w, r, real, st, query.Get("restore"))
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		WriteJSON(w, r, http.StatusMethodNotAllowed, Error("Method not allowed"))
	}
}

// serveRevision also keeps the state it sees of a file with a history: an open file is looked at every few seconds, so what
// the agent wrote over it has a way back too.
func (a *Access) serveRevision(w http.ResponseWriter, r *http.Request, requested string) {
	real, refused := a.locate(requested)
	if refused != nil {
		WriteJSON(w, r, refused.Status, Error(refused.Text))
		return
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		WriteJSON(w, r, http.StatusNotFound, Error(locale.Pick("File not found", "Файл не найден")))
		return
	}
	if EditableTypes[lastDotExt(real)] {
		a.keepVersion(real, st)
	}
	WriteJSON(w, r, http.StatusOK, revisionAnswer(st))
}

func revisionAnswer(st os.FileInfo) Object {
	return Object{{"revision", FileRevision(st)}, {"size", st.Size()}}
}

func (a *Access) saveBytes(w http.ResponseWriter, r *http.Request, real string, st os.FileInfo) {
	failed := func() {
		WriteJSON(w, r, http.StatusInternalServerError, Error(locale.Pick("The file was not saved", "Файл не сохранился")))
	}
	// Into a file beside it, then renamed over: a save that breaks off leaves the old file whole.
	tmp, err := os.CreateTemp(filepath.Dir(real), ".harnessly-save-*")
	if err != nil {
		failed()
		return
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, http.MaxBytesReader(w, r.Body, MaxSaveBytes))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		WriteJSON(w, r, http.StatusBadRequest, Error(locale.Pick("The file did not arrive whole", "Файл пришёл не целиком")))
		return
	}
	// Asked as late as it can be, after a slow upload: what changed meanwhile is what must not be lost.
	if want := r.URL.Query().Get("revision"); want != "" {
		now, err := os.Stat(real)
		if err != nil || FileRevision(now) != want {
			WriteJSON(w, r, http.StatusConflict, Error(locale.Pick("The file has changed", "Файл изменился")))
			return
		}
	}
	if err := os.Chmod(tmp.Name(), st.Mode().Perm()); err != nil {
		failed()
		return
	}
	// The kernel stamps files by a coarse clock: a save of the same size in the same tick
	// would carry the old revision, and a stale save over it would pass. Step past it.
	if saved, err := os.Stat(tmp.Name()); err == nil && !saved.ModTime().After(st.ModTime()) {
		later := st.ModTime().Add(time.Microsecond)
		if err := os.Chtimes(tmp.Name(), later, later); err != nil {
			failed()
			return
		}
	}
	a.keepVersion(real, st)
	if err := os.Rename(tmp.Name(), real); err != nil {
		failed()
		return
	}
	next, err := os.Stat(real)
	if err != nil {
		failed()
		return
	}
	WriteJSON(w, r, http.StatusOK, revisionAnswer(next))
}
