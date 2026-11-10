package files

import (
	"archive/zip"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Downloads (HRN-679): a file of any type and size as it is on disk, in ranges so a broken download resumes; a folder
// or several files as one zip written while it is sent, so nothing is held in memory or on disk. What may be handed
// over is the caller's rule (allow) and the secrets rule, checked for the path asked and for every file in a folder;
// a link inside a folder is taken only when it leads to a file the same rules allow, a link to a folder is not followed.

// storedTypes are already compressed: a zip keeps them as they are instead of squeezing them again.
var storedTypes = map[string]bool{
	"zip": true, "gz": true, "tgz": true, "bz2": true, "xz": true, "zst": true, "7z": true, "rar": true,
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "avif": true, "heic": true,
	"mp4": true, "m4v": true, "webm": true, "mov": true, "mkv": true, "mp3": true, "m4a": true, "aac": true, "ogg": true, "opus": true, "flac": true,
	"woff": true, "woff2": true, "pdf": true, "docx": true, "xlsx": true, "pptx": true, "jar": true, "apk": true,
}

// DownloadItem is one path asked for, resolved: its real path and whether it is a folder.
type DownloadItem struct {
	Real string
	Name string
	Dir  bool
}

// ResolveDownload resolves a path asked for under allow and the secrets rule: a regular file or a folder.
func (a *Access) ResolveDownload(requested string, allow func(real string) bool) (DownloadItem, *Target) {
	if !strings.HasPrefix(requested, "/") || strings.Contains(requested, "\x00") {
		t := refuse(400, locale.Pick("An absolute path is required", "Нужен абсолютный путь"))
		return DownloadItem{}, &t
	}
	real, err := Realpath(requested)
	if err != nil {
		t := refuse(404, locale.Pick("File not found", "Файл не найден"))
		return DownloadItem{}, &t
	}
	if !allow(real) || a.Secret(real) {
		t := refuse(403, locale.Pick("This file cannot be downloaded from the workspace", "Этот файл нельзя скачать из рабочего пространства"))
		return DownloadItem{}, &t
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() && !st.IsDir() {
		t := refuse(404, locale.Pick("File not found", "Файл не найден"))
		return DownloadItem{}, &t
	}
	return DownloadItem{Real: real, Name: BaseName(real), Dir: st.IsDir()}, nil
}

// Allowed: under one of the roots.
func (a *Access) Allowed(real string) bool { return a.allowed(real) }

func attachment(h http.Header, name string) {
	h.Set("Content-Disposition", `attachment; filename="`+ASCIIName(name)+`"; filename*=UTF-8''`+EncodeURIComponent(name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
}

// ServeFileDownload sends one file to be saved, whole or in the ranges asked (a resumed download), with no limit of size.
func ServeFileDownload(w http.ResponseWriter, r *http.Request, real string) {
	f, err := os.Open(real)
	if err != nil {
		WriteJSON(w, r, http.StatusNotFound, Error(locale.Pick("File not found", "Файл не найден")))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		WriteJSON(w, r, http.StatusNotFound, Error(locale.Pick("File not found", "Файл не найден")))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	attachment(h, BaseName(real))
	// ServeContent answers Range and If-Range against the file's time, so a download resumes only over the same file.
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// ZipName is the name a zip of these items is saved under: the folder's name, or "files" for several.
func ZipName(items []DownloadItem, asked string) string {
	asked = strings.TrimSpace(strings.TrimSuffix(path.Base("/"+asked), ".zip"))
	if asked != "" && asked != "/" && asked != "." {
		return asked + ".zip"
	}
	if len(items) == 1 {
		return items[0].Name + ".zip"
	}
	return "files.zip"
}

// ServeZipDownload writes the items as one zip while it is sent: a folder with all it holds under its name, a file
// by its name. Nothing is counted ahead, so the answer has no length; a failure in the middle breaks the connection,
// so a cut archive is never saved as a whole one.
func (a *Access) ServeZipDownload(w http.ResponseWriter, r *http.Request, items []DownloadItem, name string, allow func(real string) bool) {
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	attachment(h, name)
	w.WriteHeader(http.StatusOK)
	zw := zip.NewWriter(w)
	taken := map[string]int{}
	unique := func(n string) string {
		taken[n]++
		if taken[n] == 1 {
			return n
		}
		ext := path.Ext(n)
		return strings.TrimSuffix(n, ext) + " (" + strconv.Itoa(taken[n]) + ")" + ext
	}
	for _, it := range items {
		if r.Context().Err() != nil {
			panic(http.ErrAbortHandler)
		}
		top := unique(it.Name)
		if !it.Dir {
			if err := zipFile(zw, it.Real, top); err != nil {
				panic(http.ErrAbortHandler)
			}
			continue
		}
		if err := a.zipFolder(r, zw, it.Real, top, allow); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if err := zw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (a *Access) zipFolder(r *http.Request, zw *zip.Writer, root, top string, allow func(real string) bool) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		if err != nil {
			// A folder that cannot be read is left out, as a file that cannot be is.
			if d != nil && d.IsDir() && p != root {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		name := top
		if rel != "." {
			name = top + "/" + filepath.ToSlash(rel)
		}
		if a.Secret(p) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.IsDir():
			_, err := zw.CreateHeader(&zip.FileHeader{Name: name + "/", Method: zip.Store, Modified: modTime(d)})
			return err
		case d.Type()&fs.ModeSymlink != 0:
			real, err := Realpath(p)
			if err != nil || !allow(real) || a.Secret(real) {
				return nil
			}
			if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
				return nil
			}
			return zipFile(zw, real, name)
		case d.Type().IsRegular():
			return zipFile(zw, p, name)
		}
		return nil
	})
}

func modTime(d fs.DirEntry) (t time.Time) {
	if info, err := d.Info(); err == nil {
		return info.ModTime()
	}
	return t
}

// zipFile adds one file; one that cannot be opened is left out, one that breaks while read breaks the archive.
func zipFile(zw *zip.Writer, real, name string) error {
	f, err := os.Open(real)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil
	}
	fh, err := zip.FileInfoHeader(st)
	if err != nil {
		return nil
	}
	fh.Name = name
	fh.Method = zip.Deflate
	if storedTypes[lastDotExt(name)] {
		fh.Method = zip.Store
	}
	out, err := zw.CreateHeader(fh)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	return err
}
