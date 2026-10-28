package files

import (
	"archive/zip"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The viewer of a zip the agent delivered: what is inside, and one file out of it, streamed from the archive. The
// archive is opened by the same rules as any file handed over (under the roots, no secrets); a name inside it is only
// ever matched against the archive's own list, never joined to a folder, so a name that climbs out names nothing.

const (
	// MaxZipEntries is how many entries the list shows; an archive with more says so.
	MaxZipEntries = 2000
	// MaxZipEntryBytes is the biggest file taken out of an archive.
	MaxZipEntryBytes = 200 * 1024 * 1024
)

// SafeZipName: a name that stays inside the archive: relative, no "..", no backslash, no NUL.
func SafeZipName(name string) bool {
	if name == "" || strings.ContainsAny(name, "\x00\\") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return path.Clean(strings.TrimSuffix(name, "/")) != "."
}

func zipRefuse(w http.ResponseWriter, r *http.Request, status int, text string) {
	WriteJSON(w, r, status, Error(text))
}

// ServeZip answers GET file-preview?zip=list&path= (the entries) and ?zip=entry&path=&name= (one file as an attachment).
func (a *Access) ServeZip(w http.ResponseWriter, r *http.Request, q Query) {
	w.Header().Set("Cache-Control", "no-store")
	requested, _ := q.String("path")
	t := a.Download(requested)
	if !t.OK {
		zipRefuse(w, r, t.Status, t.Text)
		return
	}
	zr, err := zip.OpenReader(t.Real)
	if err != nil {
		zipRefuse(w, r, http.StatusUnsupportedMediaType, locale.Pick("This is not a zip archive", "Это не zip-архив"))
		return
	}
	defer zr.Close()
	mode, _ := q.String("zip")
	if mode == "entry" {
		name, _ := q.String("name")
		serveZipEntry(w, r, zr, name)
		return
	}
	entries := make([]any, 0, min(len(zr.File), MaxZipEntries))
	truncated := false
	for _, f := range zr.File {
		if !SafeZipName(f.Name) {
			continue
		}
		if len(entries) == MaxZipEntries {
			truncated = true
			break
		}
		entries = append(entries, Object{{"name", f.Name}, {"size", int64(f.UncompressedSize64)}, {"directory", f.FileInfo().IsDir()}})
	}
	WriteJSON(w, r, http.StatusOK, Object{{"entries", entries}, {"truncated", truncated}})
}

func serveZipEntry(w http.ResponseWriter, r *http.Request, zr *zip.ReadCloser, name string) {
	if !SafeZipName(name) {
		zipRefuse(w, r, http.StatusBadRequest, locale.Pick("A name inside the archive is required", "Нужно имя файла внутри архива"))
		return
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if f.FileInfo().IsDir() || f.Mode()&0o170000 == 0o120000 {
			zipRefuse(w, r, http.StatusBadRequest, locale.Pick("This is not a file", "Это не файл"))
			return
		}
		if f.UncompressedSize64 > MaxZipEntryBytes {
			zipRefuse(w, r, http.StatusRequestEntityTooLarge, locale.Pick("The file is too large", "Файл слишком большой"))
			return
		}
		rc, err := f.Open()
		if err != nil {
			zipRefuse(w, r, http.StatusUnprocessableEntity, locale.Pick("The file cannot be read from the archive", "Файл не читается из архива"))
			return
		}
		defer rc.Close()
		base := path.Base(name)
		h := w.Header()
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Length", strconv.FormatUint(f.UncompressedSize64, 10))
		h.Set("Content-Disposition", `attachment; filename="`+ASCIIName(base)+`"; filename*=UTF-8''`+EncodeURIComponent(base))
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		// The declared size is the archive's word: the stream is cut at it, so a lying header cannot grow a download.
		_, _ = io.Copy(w, io.LimitReader(rc, int64(f.UncompressedSize64)))
		return
	}
	zipRefuse(w, r, http.StatusNotFound, locale.Pick("No such file in the archive", "В архиве нет такого файла"))
}
