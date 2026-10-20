package design

import (
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

var sessionRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func (a *API) startBackground() { a.L.StartBackground() }

// History is GET ?artifact=&board= (the latest versions of a board) and POST { artifact, board, revision } (put the
// board back as it was at that version, 204).
func (a *API) History(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodGet, http.MethodPost)
	if refused != nil {
		return *refused
	}
	a.startBackground()
	var err error
	if r.Method == http.MethodGet {
		var snaps []BoardSnapshot
		snaps, err = a.L.HistoryForPanel(QueryString(r, "artifact"), QueryString(r, "board"), 12)
		if err == nil {
			return JSON(r, http.StatusOK, struct {
				Snapshots []BoardSnapshot `json:"snapshots"`
			}{snaps}, noStore())
		}
	} else {
		err = a.L.RestoreForPanel(Field(body, "artifact"), Field(body, "board"), Field(body, "revision"))
		if err == nil {
			return Empty(http.StatusNoContent, noStore())
		}
	}
	if IsHistoryRefusal(err) {
		return JSON(r, http.StatusConflict, errorBody(err.Error()), noStore())
	}
	a.warn("design library request failed", err)
	return JSON(r, http.StatusInternalServerError, errorBody("Не удалось открыть историю доски"), noStore())
}

// CLIHistoryNeedsKeeper: an agent's restore with its session names the chat it speaks from, which the tab keeper knows.
// The body is read and put back for the handler.
func CLIHistoryNeedsKeeper(r *http.Request) bool {
	if r.Method != http.MethodPost || r.Body == nil {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	r.Body = io.NopCloser(strings.NewReader(string(raw)))
	if err != nil {
		return false
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return false
	}
	o, ok := v.(*jsvalue.Object)
	return ok && o.Has("session")
}

// CLIHistory is the history for agents: GET ?project=&board=&n= lists versions, POST { action: 'restore', project,
// board, at, session? } puts a board back as it was.
func (a *API) CLIHistory(r *http.Request) Answer {
	raw, refused := a.CLIGuard(r)
	if refused != nil {
		return *refused
	}
	h := noStore()
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	a.startBackground()
	var err error
	if r.Method == http.MethodGet {
		limit := 20.0
		vs := jscompat.Query(r.URL.RawQuery)["n"]
		if len(vs) == 1 {
			if n, ok := jsNumber(vs[0]); ok && n != 0 && !math.IsNaN(n) {
				limit = n
			}
		}
		limit = math.Min(200, math.Max(1, limit))
		var board any
		if b, ok := Query(r, "board"); ok && b != "" {
			board = b
		}
		var project any
		if p, ok := Query(r, "project"); ok {
			project = p
		}
		var out CLIHistory
		if out, err = a.L.HistoryForCLI(project, board, limit); err == nil {
			return JSON(r, http.StatusOK, out, h)
		}
	} else {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			return JSON(r, http.StatusUnsupportedMediaType, errorBody("Нужен application/json"), h)
		}
		body := Fields(raw)
		if action, _ := Field(body, "action").(string); action != "restore" {
			return JSON(r, http.StatusBadRequest, errorBody("Неизвестное действие: restore"), h)
		}
		session := ""
		if Has(body, "session") {
			s, ok := Field(body, "session").(string)
			if !ok || !sessionRe.MatchString(s) {
				return JSON(r, http.StatusBadRequest, errorBody("Непонятная сессия вкладки агента"), h)
			}
			session = s
		}
		var res CLIRestored
		if res, err = a.L.RestoreForCLI(session, Field(body, "project"), Field(body, "board"), Field(body, "at")); err == nil {
			return JSON(r, http.StatusOK, struct {
				Board    string `json:"board"`
				Revision string `json:"revision"`
				Saved    bool   `json:"saved"`
			}{res.Board, res.Revision, res.Saved}, h)
		}
	}
	if IsHistoryRefusal(err) {
		return JSON(r, http.StatusConflict, errorBody(err.Error()), h)
	}
	a.warn("design library request failed", err)
	return JSON(r, http.StatusInternalServerError, errorBody("Не удалось выполнить команду истории"), h)
}

func decodeHeader(v string) string {
	s, ok := decodeURIComponent(v)
	if !ok {
		return ""
	}
	return s
}

var pathSep = regexp.MustCompile(`[\\/]`)

// saveBody streams the body to a file, stopping at the limit: an archive never sits in memory.
func saveBody(r *http.Request, file string) (int64, error) {
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(r.Body, LimitUploadBytes+1))
	if err != nil {
		return n, err
	}
	if n > LimitUploadBytes {
		return n, importErr("Архив больше 300 МБ")
	}
	return n, nil
}

// Import is POST with a zip export as the body (x-design-filename, URI-encoded, names it): a new library project.
// The route is open at the dispatcher and reads its own body.
func (a *API) Import(r *http.Request) Answer {
	h := noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return JSON(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
	}
	if !a.Session(r) {
		return JSON(r, http.StatusForbidden, errorBody("Forbidden"), h)
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/zip") {
		return JSON(r, http.StatusUnsupportedMediaType, errorBody("Ожидается zip-архив"), h)
	}
	if declared, ok := jsNumber(r.Header.Get("Content-Length")); ok && r.Header.Get("Content-Length") != "" && declared > LimitUploadBytes {
		return JSON(r, http.StatusRequestEntityTooLarge, errorBody("Архив больше 300 МБ"), h)
	}
	parts := pathSep.Split(decodeHeader(r.Header.Get("X-Design-Filename")), -1)
	name := jscompat.Slice(parts[len(parts)-1], 200)
	if name == "" {
		name = "design.zip"
	}
	uploads := filepath.Join(a.L.Root(), ".uploads")
	temp := filepath.Join(uploads, randomHex(6)+".zip")
	defer os.Remove(temp)
	res, err := func() (ImportResult, error) {
		if err := os.MkdirAll(uploads, 0o777); err != nil {
			return ImportResult{}, err
		}
		n, err := saveBody(r, temp)
		if err != nil {
			return ImportResult{}, err
		}
		if n == 0 {
			return ImportResult{}, errEmptyUpload
		}
		title := jscompat.Trim(zipSuffix.ReplaceAllString(name, ""))
		if title == "" {
			title = "design"
		}
		for attempt := 1; ; attempt++ {
			id := SlugifyArtifactID(name, a.L.TakenArtifactIDs())
			res, err := a.L.ImportProject(ImportOptions{Source: temp, ID: id, Title: &title, SourceName: &name})
			var ie *ImportError
			if err == nil || !errors.As(err, &ie) || !ie.Conflict || attempt >= 3 {
				return res, err
			}
		}
	}()
	if errors.Is(err, errEmptyUpload) {
		return JSON(r, http.StatusBadRequest, errorBody("Пустой файл"), h)
	}
	var ie *ImportError
	if errors.As(err, &ie) {
		return JSON(r, 422, errorBody(ie.Msg), h)
	}
	if err != nil {
		return JSON(r, http.StatusInternalServerError, errorBody("Не удалось загрузить проект"), h)
	}
	return JSON(r, http.StatusCreated, struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Boards int    `json:"boards"`
	}{res.ID, res.Title, res.Boards}, h)
}

var errEmptyUpload = errors.New("empty upload")
