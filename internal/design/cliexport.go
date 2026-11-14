package design

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// `harnessly design export`: the agent hands the person a page of a project as one HTML file that opens
// anywhere (the board as it stands, its styles, fonts and pictures written into it) or the project as a zip, made by
// the same exporters as the project's own «Экспорт» menu. The finished file is copied under the name of the page or the
// project into the folder handed-over files are packed in, so `deliver` gives the person a file with a human name.

// CLIExportFormats are the formats an agent asks for: one page as HTML, the project/ folder, the whole project.
var CLIExportFormats = map[string]DeckExportFormat{"html": DeckExportHTML, "zip": DeckExportZip, "bundle": DeckExportBundle}

// boardOf is the board a page reference names: its id, its file (with or without .dc.html) or its title.
func (l *Library) boardOf(project, ref string) (Board, bool) {
	a := l.ReadArtifact(project)
	if a == nil {
		return Board{}, false
	}
	ref = strings.TrimSpace(ref)
	var byTitle []Board
	for _, b := range a.Boards {
		file := pageFileOf(b.Path)
		if b.ID == ref || b.Path == ref || file == ref || strings.TrimSuffix(path.Base(file), ".dc.html") == ref ||
			strings.TrimSuffix(path.Base(file), ".html") == ref {
			return b, true
		}
		if strings.EqualFold(b.Title, ref) {
			byTitle = append(byTitle, b)
		}
	}
	if len(byTitle) == 1 {
		return byTitle[0], true
	}
	return Board{}, false
}

// CLIExport is POST {project, page?, format} (start; answers the job) and GET ?project=&job= (the job; when it is
// done, the path of the file with its human name).
func (a *API) CLIExport(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	bad := func(status int, msg string) Answer { return JSON(r, status, errorBody(msg), h) }
	if r.Method == http.MethodGet {
		project := QueryString(r, "project")
		if no := a.gate(r, project, RoleView); no != nil {
			return *no
		}
		j, file, ok := a.L.DeckExportJobOf(project, QueryString(r, "job"))
		if !ok {
			return bad(http.StatusNotFound, locale.Pick("No such export: it may be older than a day", "Такого экспорта нет: возможно, он старше суток"))
		}
		out := struct {
			Job  DeckExportJob `json:"job"`
			Path string        `json:"path,omitempty"`
		}{Job: j}
		if j.State == DeckJobDone && file != "" {
			named, err := a.L.namedExport(project, j, file)
			if err != nil {
				return bad(http.StatusInternalServerError, err.Error())
			}
			out.Path = named
		}
		return JSON(r, http.StatusOK, out, h)
	}
	if r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	body := Fields(raw)
	project, _ := strField(body, "project")
	page, _ := strField(body, "page")
	name, _ := strField(body, "format")
	format, known := CLIExportFormats[name]
	if !known {
		return bad(http.StatusBadRequest, locale.Pick("format is html, zip or bundle", "Формат — html, zip или bundle"))
	}
	role := RoleView
	if format != DeckExportHTML {
		// An archive hands over the project's files, not what it shows: for those who may change it.
		role = RoleEdit
	}
	if no := a.gate(r, project, role); no != nil {
		return *no
	}
	board := ""
	if format == DeckExportHTML {
		if page == "" {
			return bad(http.StatusBadRequest, locale.Pick("Name the page to export", "Назовите страницу для экспорта"))
		}
		b, ok := a.L.boardOf(project, page)
		if !ok {
			return bad(http.StatusNotFound, locale.Pick("No such page in the project", "В проекте нет такой страницы"))
		}
		board = b.Path
		if b.ID != "" {
			board = b.ID
		}
	}
	a.L.SweepDeckExports(project)
	j, err := a.L.StartBoardExport(project, format, "", board, false)
	if err != nil {
		return bad(http.StatusInternalServerError, err.Error())
	}
	return JSON(r, http.StatusAccepted, struct {
		Job DeckExportJob `json:"job"`
	}{j}, h)
}

// namedExport copies a finished export to <PackDir>/export-<job>/<name>: the page's title or the project's, with the
// format's extension. Asked again, it answers the same copy.
func (l *Library) namedExport(project string, j DeckExportJob, file string) (string, error) {
	title := ""
	if a := l.ReadArtifact(project); a != nil {
		title = a.Title
		if j.Board != "" {
			for _, b := range a.Boards {
				if b.ID == j.Board || b.Path == j.Board {
					title = b.Title
				}
			}
		}
	}
	if title == "" {
		title = project
	}
	dir := filepath.Join(delivery.PackDir, "export-"+j.ID)
	out := filepath.Join(dir, deckFileName(title, strings.TrimPrefix(filepath.Ext(file), ".")))
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	src, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(dir, ".part-")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return out, os.Rename(tmp.Name(), out)
}
