package design

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A kit moves between machines as one file (internal/designkit: Export, Import). These two routes are the UI's side of
// `design kit export|import`: the same checks, the same refusal to overwrite, answered as JSON.

// KitImport is POST (a session) with the kit file as the body, or with ?upload=<id> naming a finished chunked upload
// (the way a big file comes; the upload is deleted once the kit is installed or refused for good): ?dryRun=1 checks and installs nothing, ?asId= and
// ?asVersion= install under another name. Answers {id, version, installed, unchanged, manifest, warnings}; 409
// {error, conflict:{added,changed,removed}} when the version is installed with other content; 422 for a file that
// does not check out.
func (a *API) KitImport(w http.ResponseWriter, r *http.Request) {
	writeAnswer(w, a.kitImport(r))
}

func (a *API) kitImport(r *http.Request) Answer {
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
		if a.UploadFile == nil {
			return JSON(r, http.StatusServiceUnavailable, errorBody(locale.Pick("Uploads are not available", "Загрузка недоступна")), h)
		}
		path, ok := a.UploadFile(id)
		if !ok {
			return JSON(r, http.StatusNotFound, errorBody(locale.Pick("The uploaded file is gone: add it again", "Загруженного файла нет: добавьте его ещё раз")), h)
		}
		f, err := os.Open(path)
		if err != nil {
			return JSON(r, http.StatusNotFound, errorBody(locale.Pick("The uploaded file is gone: add it again", "Загруженного файла нет: добавьте его ещё раз")), h)
		}
		defer f.Close()
		if !dry {
			defer os.Remove(path)
		}
		src = f
	} else if r.ContentLength > designkit.MaxArchiveBytes {
		return JSON(r, http.StatusRequestEntityTooLarge, errorBody(locale.Pick("The file is too large for one request: upload it in chunks", "Файл слишком большой для одного запроса: загрузите его кусками")), h)
	}
	res, err := designkit.Import(a.L.KitsDir(), src, designkit.ImportOptions{
		AsID: QueryString(r, "asId"), AsVersion: QueryString(r, "asVersion"), DryRun: dry,
	})
	var conflict *designkit.ConflictError
	switch {
	case errors.As(err, &conflict):
		return JSON(r, http.StatusConflict, map[string]any{"error": conflict.Error(), "id": conflict.ID, "version": conflict.Version,
			"conflict": conflict.Diff, "manifest": res.Manifest}, h)
	case err != nil:
		return JSON(r, http.StatusUnprocessableEntity, errorBody(err.Error()), h)
	}
	return JSON(r, http.StatusOK, res, h)
}

// KitExport is GET ?id=&version= (a session): the kit as one file. A kit with license-restricted files is refused with
// 409 {error, restricted:[...]} until ?acceptRestricted=1 confirms; ?withSources=1 adds the sources.
func (a *API) KitExport(w http.ResponseWriter, r *http.Request) {
	if status, v, hh, refused := a.APIRefusal(r); refused {
		writeAnswer(w, Answer{status, v, hh})
		return
	}
	if !a.Session(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	id, version := QueryString(r, "id"), QueryString(r, "version")
	if version == "" {
		for _, k := range designkit.List(a.L.KitsDir()) {
			if k.ID == id && len(k.Versions) > 0 {
				version = k.Versions[0].Version
			}
		}
	}
	if !designkit.ValidID(id) || !designkit.ValidVersion(version) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	m, err := designkit.BuildManifest(a.L.KitsDir(), id, version, designkit.ExportOptions{
		WithSources: QueryString(r, "withSources") == "1", AcceptRestricted: QueryString(r, "acceptRestricted") == "1"})
	switch {
	case errors.Is(err, designkit.ErrRestricted):
		writeAnswer(w, JSON(r, http.StatusConflict, map[string]any{"error": err.Error(), "restricted": m.Restricted}, noStore()))
		return
	case err != nil:
		writeAnswer(w, JSON(r, http.StatusNotFound, errorBody(err.Error()), noStore()))
		return
	}
	tmp, err := os.CreateTemp("", "kit-export-")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := designkit.WriteArchive(a.L.KitsDir(), m, tmp); err != nil {
		a.warn("design kit: export", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	st, err := tmp.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	name := strings.NewReplacer("\"", "", "\\", "", "/", "").Replace(filepath.Base(id + "-" + version + ".kit.tar.gz"))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, st.ModTime(), tmp)
}
