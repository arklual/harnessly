package design

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The routes of a video's frames, check and render. Two packages fill this file, each only inside its own section.

// ==== WP2a: frames and check ====

// VideoFrame is GET ?artifact&page&at&format[&draft] (view, body route): the scene's frame at `at` ms as image/jpeg.
func (a *API) VideoFrame(w http.ResponseWriter, r *http.Request) {
	if !a.imageGuard(w, r) {
		return
	}
	artifact := QueryString(r, "artifact")
	if !IsLibraryID(artifact) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if a.denyImage(w, artifact) {
		return
	}
	at, okAt := videoMsParam(QueryString(r, "at"))
	f := VideoFormat(QueryString(r, "format"))
	page := QueryString(r, "page")
	if !okAt || !f.Valid() || page == "" {
		writeAnswer(w, JSON(r, http.StatusBadRequest, errorBody(locale.Pick("page, at and format are expected", "Нужны page, at и format")), noStore()))
		return
	}
	file, err := a.L.VideoFrameFile(r.Context(), artifact, page, f, QueryString(r, "draft") == "1", at)
	if err != nil {
		writeAnswer(w, a.deckFail(r, err, "draw the frame"))
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	image(w, r, data, "private, no-cache")
}

// CLIVideoFrame is GET ?project&page&at[&format&strip=a:b:step&draft&sheet=1] (view): {frames: [{t, path}]}, absolute paths of
// the cached JPEGs for the agent to open. format is the video's first format when not given; strip is a stretch in ms
// (from a to b, every step), instead of at.
func (a *API) CLIVideoFrame(r *http.Request) Answer {
	if _, no := a.CLIGuard(r); no != nil {
		return *no
	}
	h := noStore()
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	project := QueryString(r, "project")
	if no := a.gate(r, project, RoleView); no != nil {
		return *no
	}
	if err := a.L.requireVideo(project); err != nil {
		return a.deckFail(r, err, "read the video")
	}
	page := QueryString(r, "page")
	sheet := QueryString(r, "sheet") == "1"
	times, err := videoStripTimes(QueryString(r, "at"), QueryString(r, "strip"))
	if sheet {
		times, err = []int64{0}, nil
	}
	if err != nil || page == "" {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("page and at (or strip=a:b:step, ms) are expected",
			"Нужны page и at (или strip=a:b:step, мс)")), h)
	}
	f := VideoFormat(QueryString(r, "format"))
	if f == "" {
		s, err := a.L.ReadVideoSettings(project)
		if err != nil {
			return a.deckFail(r, err, "read the video")
		}
		f = s.Formats[0]
	}
	if !f.Valid() {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("format is 16:9 or 9:16", "format — 16:9 или 9:16")), h)
	}
	draft := QueryString(r, "draft") == "1"
	type frame struct {
		T    int64  `json:"t"`
		Path string `json:"path"`
	}
	var files []string
	if sheet {
		var file string
		file, err = a.L.VideoSheetFile(r.Context(), project, page, f)
		files = []string{file}
	} else {
		files, err = a.L.VideoFrameFiles(r.Context(), project, page, f, draft, times)
	}
	if err != nil {
		return a.deckFail(r, err, "draw the frame")
	}
	frames := make([]frame, len(times))
	for i, t := range times {
		frames[i] = frame{t, files[i]}
	}
	return JSON(r, http.StatusOK, struct {
		Project string  `json:"project"`
		Frames  []frame `json:"frames"`
	}{project, frames}, h)
}

// CLIVideoCheck is POST {project, page?, format?} (view): {issues: [VideoCheckIssue]}.
func (a *API) CLIVideoCheck(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	body := Fields(raw)
	project, _ := strField(body, "project")
	if no := a.gate(r, project, RoleView); no != nil {
		return *no
	}
	if err := a.L.requireVideo(project); err != nil {
		return a.deckFail(r, err, "check the video")
	}
	var page *string
	if p, ok := strField(body, "page"); ok && p != "" {
		page = &p
	}
	var f *VideoFormat
	if v, ok := strField(body, "format"); ok && v != "" {
		fm := VideoFormat(v)
		if !fm.Valid() {
			return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("format is 16:9 or 9:16", "format — 16:9 или 9:16")), h)
		}
		f = &fm
	}
	ctx, cancel := context.WithTimeout(r.Context(), videoCheckTimeout)
	defer cancel()
	issues, err := a.L.VideoCheck(ctx, project, page, f)
	if err != nil {
		return a.deckFail(r, err, "check the video")
	}
	return JSON(r, http.StatusOK, struct {
		Project string            `json:"project"`
		Issues  []VideoCheckIssue `json:"issues"`
	}{project, issues}, h)
}

const (
	videoCheckTimeout = 10 * time.Minute
	// videoStripMax is the most frames one CLI call draws.
	videoStripMax = 60
)

// videoMsParam reads a time in ms: a whole number from 0 to an hour.
func videoMsParam(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 0 && n <= 3_600_000
}

// videoStripTimes is at, or the times of strip=a:b:step (a to b inclusive, at most videoStripMax; at may repeat a).
func videoStripTimes(at, strip string) ([]int64, error) {
	bad := errors.New("bad times")
	if strip == "" {
		t, ok := videoMsParam(at)
		if !ok {
			return nil, bad
		}
		return []int64{t}, nil
	}
	parts := strings.Split(strip, ":")
	if len(parts) != 3 {
		return nil, bad
	}
	from, ok1 := videoMsParam(parts[0])
	// The CLI also sends at = a; any other at is a mistake.
	if a, ok := videoMsParam(at); at != "" && (!ok || a != from) {
		return nil, bad
	}
	to, ok2 := videoMsParam(parts[1])
	step, ok3 := videoMsParam(parts[2])
	if !ok1 || !ok2 || !ok3 || step == 0 || to < from || (to-from)/step+1 > videoStripMax {
		return nil, bad
	}
	var out []int64
	for t := from; t <= to; t += step {
		out = append(out, t)
	}
	return out, nil
}

// ==== end WP2a ====

// ==== WP2b: render and file ====

// videoRenderFail words an error of a render as an answer.
func (a *API) videoRenderFail(r *http.Request, err error) Answer {
	return a.deckFail(r, err, "start the render")
}

// videoRenderRequest reads {format, page, draft} of a body; page is the scene's id as given.
func videoRenderRequest(body *jsvalue.Object) (VideoRenderRequest, string) {
	req := VideoRenderRequest{Format: VideoFormat(jscompat.String(Field(body, "format"))), Draft: Field(body, "draft") == true}
	page := ""
	if v, ok := Field(body, "page").(string); ok {
		page = v
	}
	return req, page
}

// VideoRender is POST {artifact, format, page?, draft} (edit): the VideoRenderJob; and GET ?artifact&id (view): the job.
func (a *API) VideoRender(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleView, http.MethodGet, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method == http.MethodGet {
		j, ok := a.L.VideoRenderJobOf(id, QueryString(r, "id"))
		if !ok {
			return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such render: it may be older than a day", "Такого рендера нет: возможно, он старше суток")), h)
		}
		return JSON(r, http.StatusOK, j, h)
	}
	if gated := a.gate(r, id, RoleEdit); gated != nil {
		return *gated
	}
	req, page := videoRenderRequest(body)
	if page != "" {
		req.Page = &page
	}
	j, err := a.L.StartVideoRender(id, req)
	if err != nil {
		return a.videoRenderFail(r, err)
	}
	return JSON(r, http.StatusAccepted, j, h)
}

// VideoFile is GET ?artifact&format (view, body route): the published MP4 of the format, as an attachment. With
// &job=<id> it is the finished job's file instead (a draft, or one scene).
func (a *API) VideoFile(w http.ResponseWriter, r *http.Request) {
	if !a.imageGuard(w, r) {
		return
	}
	id := QueryString(r, "artifact")
	if !IsLibraryID(id) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if a.denyImage(w, id) {
		return
	}
	notFound := func() {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
	}
	f := VideoFormat(QueryString(r, "format"))
	if !f.Valid() {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	path := filepath.Join(a.L.publishedDir(id), VideoPublishedName(f))
	if job := QueryString(r, "job"); job != "" {
		p, ok := a.L.videoJobFile(id, job)
		if !ok {
			notFound()
			return
		}
		path = p
	}
	file, err := os.Open(path)
	if err != nil {
		notFound()
		return
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		notFound()
		return
	}
	title := ""
	if art := a.L.ReadArtifact(id); art != nil {
		title = art.Title
	}
	if f == VideoPortrait {
		title += " 9x16"
	}
	name := deckFileName(title, "mp4")
	h := w.Header()
	h.Set("Content-Type", "video/mp4")
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", `attachment; filename="`+asciiFileName(name)+`"; filename*=UTF-8''`+rfc5987(name))
	http.ServeContent(w, r, name, st.ModTime(), file)
}

// CLIVideoRender is POST {project, format, page?, draft} (edit) and GET ?project&id (view): as VideoRender, the plain VideoRenderJob. page is a scene's id, file or title.
func (a *API) CLIVideoRender(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method == http.MethodGet {
		project := QueryString(r, "project")
		if gated := a.gate(r, project, RoleView); gated != nil {
			return *gated
		}
		if err := a.L.requireVideo(project); err != nil {
			return a.deckFail(r, err, "read the render")
		}
		j, ok := a.L.VideoRenderJobOf(project, QueryString(r, "id"))
		if !ok {
			return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such render: it may be older than a day", "Такого рендера нет: возможно, он старше суток")), h)
		}
		return JSON(r, http.StatusOK, j, h)
	}
	if r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	body := Fields(raw)
	project, _ := strField(body, "project")
	if gated := a.gate(r, project, RoleEdit); gated != nil {
		return *gated
	}
	if err := a.L.requireVideo(project); err != nil {
		return a.deckFail(r, err, "render the video")
	}
	req, page := videoRenderRequest(body)
	if page != "" {
		p, err := a.L.deckPage(project, page)
		if err != nil {
			return a.videoRenderFail(r, err)
		}
		req.Page = &p.ID
	}
	j, err := a.L.StartVideoRender(project, req)
	if err != nil {
		return a.videoRenderFail(r, err)
	}
	return JSON(r, http.StatusAccepted, j, h)
}

// ==== end WP2b ====
