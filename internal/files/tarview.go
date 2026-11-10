package files

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The viewer of a tar, plain or gzipped (HRN-679), by the zip viewer's rules: the entries as a list, one file out of it
// as an attachment. A tar has no index, so both read the archive from its start; a name is matched against the
// archive's own, never joined to a folder.

// tarKind is "tar" or "tgz" for an archive the viewer reads by its name, "" for any other file.
func tarKind(p string) string {
	low := strings.ToLower(p)
	switch {
	case strings.HasSuffix(low, ".tar"):
		return "tar"
	case strings.HasSuffix(low, ".tar.gz"), strings.HasSuffix(low, ".tgz"):
		return "tgz"
	}
	return ""
}

func openTar(real string) (*tar.Reader, func(), error) {
	f, err := os.Open(real)
	if err != nil {
		return nil, nil, err
	}
	if tarKind(real) != "tgz" {
		return tar.NewReader(f), func() { f.Close() }, nil
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return tar.NewReader(gz), func() { gz.Close(); f.Close() }, nil
}

func tarName(h *tar.Header) string {
	return strings.TrimPrefix(h.Name, "./")
}

func (a *Access) serveTar(w http.ResponseWriter, r *http.Request, q Query, real string) {
	tr, done, err := openTar(real)
	if err != nil {
		zipRefuse(w, r, http.StatusUnsupportedMediaType, locale.Pick("This is not a tar archive", "Это не tar-архив"))
		return
	}
	defer done()
	mode, _ := q.String("zip")
	want, _ := q.String("name")
	if mode == "entry" && !SafeZipName(want) {
		zipRefuse(w, r, http.StatusBadRequest, locale.Pick("A name inside the archive is required", "Нужно имя файла внутри архива"))
		return
	}
	entries := []any{}
	truncated := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if mode == "entry" || len(entries) == 0 {
				zipRefuse(w, r, http.StatusUnsupportedMediaType, locale.Pick("This is not a tar archive", "Это не tar-архив"))
				return
			}
			truncated = true
			break
		}
		name := tarName(h)
		dir := h.Typeflag == tar.TypeDir
		if dir && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		if !SafeZipName(name) || !dir && h.Typeflag != tar.TypeReg {
			continue
		}
		if mode == "entry" {
			if name != want || dir {
				continue
			}
			base := path.Base(name)
			hd := w.Header()
			hd.Set("Content-Type", "application/octet-stream")
			hd.Set("Content-Length", strconv.FormatInt(h.Size, 10))
			hd.Set("Content-Disposition", `attachment; filename="`+ASCIIName(base)+`"; filename*=UTF-8''`+EncodeURIComponent(base))
			hd.Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusOK)
			_, _ = io.Copy(w, io.LimitReader(tr, h.Size))
			return
		}
		if len(entries) == MaxZipEntries {
			truncated = true
			break
		}
		entries = append(entries, Object{{"name", name}, {"size", h.Size}, {"directory", dir}})
	}
	if mode == "entry" {
		zipRefuse(w, r, http.StatusNotFound, locale.Pick("No such file in the archive", "В архиве нет такого файла"))
		return
	}
	WriteJSON(w, r, http.StatusOK, Object{{"entries", entries}, {"truncated", truncated}})
}
