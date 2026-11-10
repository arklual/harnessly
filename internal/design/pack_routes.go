package design

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The portable pack (pack.go) as routes: the same checks as `harnessly design pack export|import`, answered as JSON.

// PackImport is POST (a session) with the pack as the body, or with ?upload=<id> naming a finished chunked upload (the
// way a big file comes; the upload is deleted once the pack is installed or refused for good): ?dryRun=1 reports what
// would be added and changes nothing. Answers the PackImportResult; 409 {error, conflict:[{id,version,status,diff}]} when
// a kit version is installed with other content; 422 for a file that is not a pack, is damaged, or comes from a newer
// format.
func (a *API) PackImport(w http.ResponseWriter, r *http.Request) {
	writeAnswer(w, a.packImport(r))
}

func (a *API) packImport(r *http.Request) Answer {
	h := noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return JSON(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
	}
	if status, v, hh, refused := a.APIRefusal(r); refused {
		return Answer{status, v, hh}
	}
	if !a.Session(r) {
		return JSON(r, http.StatusForbidden, errorBody("Forbidden"), h)
	}
	var src io.Reader = r.Body
	dry := QueryString(r, "dryRun") == "1"
	if id := QueryString(r, "upload"); id != "" {
		gone := JSON(r, http.StatusNotFound, errorBody(locale.Pick("The uploaded file is gone: add it again", "Загруженного файла нет: добавьте его ещё раз")), h)
		if a.UploadFile == nil {
			return JSON(r, http.StatusServiceUnavailable, errorBody(locale.Pick("Uploads are not available", "Загрузка недоступна")), h)
		}
		path, ok := a.UploadFile(id)
		if !ok {
			return gone
		}
		f, err := os.Open(path)
		if err != nil {
			return gone
		}
		defer f.Close()
		if !dry {
			defer os.Remove(path)
		}
		src = f
	}
	res, err := a.L.PackImport(src, PackImportOptions{DryRun: dry})
	var pe *PackError
	switch {
	case errors.As(err, &pe) && len(pe.Conflict) > 0:
		return JSON(r, http.StatusConflict, map[string]any{"error": pe.Error(), "conflict": pe.Conflict, "kits": res.Kits, "manifest": res.Manifest}, h)
	case err != nil:
		return JSON(r, http.StatusUnprocessableEntity, errorBody(err.Error()), h)
	}
	return JSON(r, http.StatusOK, res, h)
}

// PackExport is GET ?project=<id>&project=<id>… or ?all=1, with &kit=<id>[@version] for kits on their own (a session):
// the pack as one file. Kits with license-restricted files are refused with 409 {error, restricted:[…]} until
// ?acceptRestricted=1 confirms.
func (a *API) PackExport(w http.ResponseWriter, r *http.Request) {
	if status, v, hh, refused := a.APIRefusal(r); refused {
		writeAnswer(w, Answer{status, v, hh})
		return
	}
	if !a.Session(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	opt := PackExportOptions{Projects: q["project"], Kits: q["kit"], All: q.Get("all") == "1", AcceptRestricted: q.Get("acceptRestricted") == "1"}
	tmp, err := os.CreateTemp("", "sinopia-pack-")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	_, err = a.L.PackExport(r.Context(), opt, tmp)
	var restricted *PackRestrictedError
	switch {
	case errors.As(err, &restricted):
		writeAnswer(w, JSON(r, http.StatusConflict, map[string]any{"error": err.Error(), "restricted": restricted.Restricted}, noStore()))
		return
	case err != nil:
		writeAnswer(w, JSON(r, http.StatusUnprocessableEntity, errorBody(err.Error()), noStore()))
		return
	}
	st, err := tmp.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	name := "sinopia-pack.tar.gz"
	if len(opt.Projects) == 1 && !opt.All && IsLibraryID(opt.Projects[0]) {
		name = strings.ReplaceAll(opt.Projects[0], "/", "") + ".sinopia-pack.tar.gz"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, st.ModTime(), tmp)
}
