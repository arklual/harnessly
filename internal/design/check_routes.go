package design

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

const (
	thumbWait     = 20 * time.Second
	cliWaitLimit  = 10 * time.Minute
	cliWaitPoll   = time.Second
	immutableShot = "private, max-age=31536000, immutable"
)

// stringsOf are the strings of a JSON array, at most max (0: all); nil when v is not an array.
func stringsOf(v any, max int) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, true
}

// Check is GET ?artifact= (the last check, filling while it runs) and POST {artifact, boards?} (start one).
func (a *API) Check(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodGet, http.MethodPost)
	if refused != nil {
		return *refused
	}
	h := noStore()
	artifact := QueryString(r, "artifact")
	if r.Method != http.MethodGet {
		artifact = ""
		if v := Field(body, "artifact"); v != nil {
			artifact = jscompat.String(v)
		}
	}
	if !IsLibraryID(artifact) {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), h)
	}
	need := RoleEdit
	if r.Method == http.MethodGet {
		need = RoleView
	}
	if no := a.gate(r, artifact, need); no != nil {
		return *no
	}
	if r.Method == http.MethodGet {
		v, _ := a.L.ReadCheckReport(artifact)
		return JSON(r, http.StatusOK, jsvalue.NewObject("report", reportValue(v)), h)
	}
	only, _ := stringsOf(Field(body, "boards"), 400)
	report := a.L.StartCheck(artifact, only)
	if report == nil {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such design or board", "Нет такого макета или такой доски")), h)
	}
	return JSON(r, http.StatusAccepted, jsvalue.NewObject("report", reportValue(report)), h)
}

// reportValue is a report as a JSON value inside an answer object.
func reportValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case *CheckReport:
		raw, _ := jsvalue.Parse(nodejson.Marshal(x))
		return raw
	}
	return v
}

// CLICheck is the check part of POST /api/cli/design {artifact, boards?, wait?}: 202 with the report as an agent reads
// it, or with wait the finished report (polled every second, at most ten minutes).
func (a *API) CLICheck(r *http.Request, body *jsvalue.Object, artifact string) Answer {
	h := noStore()
	only, _ := stringsOf(Field(body, "boards"), 0)
	started := a.L.StartCheck(artifact, only)
	if started == nil {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such design or board: see "+a.L.cfg.Texts.CLI+" design list", "Нет такого макета или такой доски: смотри "+a.L.cfg.Texts.CLI+" design list")), h)
	}
	if w, _ := Field(body, "wait").(bool); !w {
		return JSON(r, http.StatusAccepted, a.L.ForAgent(started), h)
	}
	deadline := time.Now().Add(cliWaitLimit)
	for {
		_, report := a.L.ReadCheckReport(artifact)
		if report == nil || report.FinishedAt != nil || time.Now().After(deadline) {
			if report == nil {
				report = started
			}
			return JSON(r, http.StatusOK, a.L.ForAgent(report), h)
		}
		select {
		case <-r.Context().Done():
			return Empty(http.StatusServiceUnavailable, h)
		case <-time.After(cliWaitPoll):
		}
	}
}

// image is res.send(buffer) of a JPEG: the dispatcher's ETag of the bytes, 304 when the caller holds them.
func image(w http.ResponseWriter, r *http.Request, data []byte, cache string) {
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", cache)
	h.Set("X-Content-Type-Options", "nosniff")
	tag := nodejson.ETag(data)
	h.Set("ETag", tag)
	if nodejson.Fresh(r.Header, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// imageGuard is the dispatcher's check and body, then the handler's GET and session; false after answering.
func (a *API) imageGuard(w http.ResponseWriter, r *http.Request) bool {
	if status, v, h, refused := a.APIRefusal(r); refused {
		writeAnswer(w, Answer{status, v, h})
		return false
	}
	if _, bad := ReadBody(r); bad != nil {
		writeAnswer(w, *bad)
		return false
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	if !a.Session(r) {
		w.WriteHeader(http.StatusForbidden)
		return false
	}
	return true
}

func writeAnswer(w http.ResponseWriter, a Answer) {
	for k, vs := range a.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	var raw []byte
	switch b := a.Body.(type) {
	case nil:
	case gate.Raw:
		w.Header().Set("Content-Type", b.ContentType)
		raw = b.Body
	case json.RawMessage:
		raw = b
	default:
		raw = nodejson.Marshal(b)
	}
	if raw != nil && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(a.Status)
	if raw != nil {
		_, _ = w.Write(raw)
	}
}

// Shot is GET ?artifact=&file= — a screenshot of a design's check: the cover of a card and the report's thumbnails.
func (a *API) Shot(w http.ResponseWriter, r *http.Request) {
	if !a.imageGuard(w, r) {
		return
	}
	if a.denyImage(w, QueryString(r, "artifact")) {
		return
	}
	file := a.L.checkShotPath(QueryString(r, "artifact"), QueryString(r, "file"))
	var data []byte
	if file != "" {
		data, _ = os.ReadFile(file)
	}
	if data == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	image(w, r, data, immutableShot)
}

// Thumb is GET ?artifact=&board=&v=[&all=1] — the gallery picture of a board. An edited board answers at once with its
// earlier picture, not to be kept, while the new one is drawn; all draws the project's other pictures ahead; 404 when
// there is none yet or at all. In the shadow (a writer that cannot flush) nothing is drawn: only a picture already
// there is served.
func (a *API) Thumb(w http.ResponseWriter, r *http.Request) {
	if !a.imageGuard(w, r) {
		return
	}
	_, live := w.(http.Flusher)
	artifact := QueryString(r, "artifact")
	board := ParseBoardPath(QueryString(r, "board"))
	if !IsLibraryID(artifact) || board == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if a.denyImage(w, artifact) {
		return
	}
	var ans ThumbAnswer
	var ok bool
	if live {
		ans, ok = a.L.BoardThumbNow(artifact, board, true)
	} else {
		ans, ok = a.L.currentThumb(artifact, board)
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if live && QueryString(r, "all") == "1" {
		go a.L.WarmThumbs(artifact)
	}
	file, current := ans.File, ans.Current
	if file == "" && ans.Drawing != nil {
		file, _ = ans.Drawing.Wait(thumbWait)
		current = true
	}
	var data []byte
	if file != "" {
		data, _ = os.ReadFile(file)
	}
	if data == nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	cache := "private, no-cache"
	if current {
		cache = immutableShot
	}
	image(w, r, data, cache)
}

// currentThumb is what is there for a board without drawing anything.
func (l *Library) currentThumb(id, boardPath string) (ThumbAnswer, bool) {
	a := l.ReadArtifact(id)
	if a == nil {
		return ThumbAnswer{}, false
	}
	for _, b := range a.Boards {
		if b.Path != boardPath {
			continue
		}
		version := l.thumbVersion(id, b.Path)
		if version == "" {
			return ThumbAnswer{}, false
		}
		dir := l.ArtifactDir(id) + "/" + ThumbDirname
		file := dir + "/" + thumbFileName(b.Path, version)
		if _, err := os.Stat(file); err == nil {
			return ThumbAnswer{File: file, Current: true}, true
		}
		return ThumbAnswer{File: earlierThumb(dir, thumbHash(b.Path))}, true
	}
	return ThumbAnswer{}, false
}
