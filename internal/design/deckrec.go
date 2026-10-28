package design

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// A deck's recordings: the camera and voice in chunks, the events of the show, the transcript and the montage. Files
// under DeckRecDir/<recId>:
//
//	rec.json         DeckRecMeta
//	av.webm          the chunks, appended in order (the name stays even when the browser records mp4)
//	av.state         {next, size}: the next chunk wanted and how much of av.webm is whole
//	events.jsonl     DeckEvent lines; t is ms on the media's timeline (the time a paused recording stands still is
//	                 not counted)
//	transcript.json  DeckTranscript, built behind the finish
//	edit.json        DeckEdit

const (
	recMetaFile       = "rec.json"
	recMediaFile      = "av.webm"
	recStateFile      = "av.state"
	recEventsFile     = "events.jsonl"
	recTranscriptFile = "transcript.json"
	recEditFile       = "edit.json"

	recChunkMax  = 64 << 20 // one chunk is seconds of video; this is a runaway
	recMediaMax  = 2 << 30  // a whole recording
	recEventsMax = 64 << 20 // events.jsonl
	recLinesMax  = 5000     // lines in one events batch
	recKeptMax   = 20000    // parts a person kept
	recDurMax    = int64(24 * 3600 * 1000)
	recStale     = 20 * time.Minute // a recording nobody writes to for this long was abandoned
)

// recMimeTypes are the containers a recording may be in, with what each one starts with and the demuxer ffmpeg is told
// to read it with (the name is never guessed from the bytes).
var recMimeTypes = map[string]struct {
	Ext, Demuxer string
	Magic        func(head []byte) bool
}{
	"video/webm": {"webm", "matroska", magicEBML},
	"audio/webm": {"webm", "matroska", magicEBML},
	"video/mp4":  {"mp4", "mp4", magicFtyp},
	"audio/ogg":  {"ogg", "ogg", magicOgg},
}

func magicEBML(h []byte) bool {
	return len(h) >= 4 && h[0] == 0x1A && h[1] == 0x45 && h[2] == 0xDF && h[3] == 0xA3
}
func magicFtyp(h []byte) bool { return len(h) >= 8 && string(h[4:8]) == "ftyp" }
func magicOgg(h []byte) bool  { return len(h) >= 4 && string(h[:4]) == "OggS" }

var recCodecsRe = regexp.MustCompile(`^[A-Za-z0-9.+_-]+(,[A-Za-z0-9.+_-]+)*$`)

// cleanRecMime is the mime a recording is created with: one of recMimeTypes with, at most, a codecs parameter of plain
// names. It returns the base type and the mime to keep (the base, then the codecs); false for anything else.
func cleanRecMime(mime string) (base, keep string, ok bool) {
	if len(mime) > 100 {
		return "", "", false
	}
	// Not mime.ParseMediaType: a browser writes codecs=vp9,opus unquoted, which that refuses.
	parts := strings.Split(mime, ";")
	b := strings.ToLower(strings.TrimSpace(parts[0]))
	if _, known := recMimeTypes[b]; !known {
		return "", "", false
	}
	keep = b
	for _, p := range parts[1:] {
		k, v, found := strings.Cut(strings.TrimSpace(p), "=")
		if !found || strings.ToLower(k) != "codecs" || !recCodecsRe.MatchString(v) || strings.Contains(keep, ";") {
			return "", "", false
		}
		keep += ";codecs=" + v
	}
	return b, keep, true
}

// recBase is the allowlisted base type of a stored mime; "" when it is none (a recording from before the list).
func recBase(mime string) string {
	b, _, _ := strings.Cut(mime, ";")
	b = strings.ToLower(strings.TrimSpace(b))
	if _, ok := recMimeTypes[b]; ok {
		return b
	}
	return ""
}

// recDemuxer is the ffmpeg input format of a recording.
func recDemuxer(mime string) string {
	if t, ok := recMimeTypes[recBase(mime)]; ok {
		return t.Demuxer
	}
	return "matroska"
}

// errRecNext is a chunk out of order: the one wanted is Next.
type errRecNext struct{ Next int }

func (e errRecNext) Error() string { return "chunk out of order, next is " + strconv.Itoa(e.Next) }

var (
	errRecGone     = errors.New("no such recording")
	errRecFinished = errors.New("recording is finished")
	errRecTooBig   = errors.New("recording is too big")
	errRecFormat   = errors.New("not the format the recording was started in")
)

var recLocks sync.Map

// recLock serialises the writes to one recording's files.
func (l *Library) recLock(id, rec string) *sync.Mutex {
	m, _ := recLocks.LoadOrStore(l.Root()+"|"+id+"|"+rec, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (l *Library) readRecMeta(id, rec string) (DeckRecMeta, error) {
	var m DeckRecMeta
	if !IsLibraryID(id) || !IsPageID(rec) {
		return m, errRecGone
	}
	raw, err := os.ReadFile(filepath.Join(l.recDir(id, rec), recMetaFile))
	if err != nil {
		return m, errRecGone
	}
	if json.Unmarshal(raw, &m) != nil || m.ID != rec {
		return m, errRecGone
	}
	if m.Slides == nil {
		m.Slides = []string{}
	}
	if m.Takes == nil {
		m.Takes = []DeckTake{}
	}
	return m, nil
}

func (l *Library) writeRecMeta(id string, m DeckRecMeta) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(l.recDir(id, m.ID), recMetaFile), raw)
}

// recLastWrite is when anything of the recording was last written.
func (l *Library) recLastWrite(id, rec string) time.Time {
	var last time.Time
	for _, n := range []string{recMetaFile, recMediaFile, recEventsFile, recStateFile} {
		if st, err := os.Stat(filepath.Join(l.recDir(id, rec), n)); err == nil && st.ModTime().After(last) {
			last = st.ModTime()
		}
	}
	return last
}

// recView is the recording's meta as the person is told it: one nobody has written to for recStale and that was never
// finished is failed.
func (l *Library) recView(id string, m DeckRecMeta) DeckRecMeta {
	if m.State == DeckRecRecording && time.Since(l.recLastWrite(id, m.ID)) > recStale {
		m.State = DeckRecFailed
	}
	return m
}

// CreateRec starts a recording of the deck: its pages as they are now, no chunks yet.
func (l *Library) CreateRec(id string, camera bool, mime string) (DeckRecMeta, error) {
	model := l.ReadModel(id)
	if model == nil {
		return DeckRecMeta{}, errRecGone
	}
	slides := []string{}
	for _, p := range model.Pages {
		slides = append(slides, p.ID)
	}
	m := DeckRecMeta{ID: NewPageID(), StartedAt: time.Now().UTC().Format(time.RFC3339), Camera: camera, Mime: mime,
		Slides: slides, Takes: []DeckTake{}, State: DeckRecRecording}
	if err := os.MkdirAll(l.recDir(id, m.ID), 0o755); err != nil {
		return m, err
	}
	return m, l.writeRecMeta(id, m)
}

type recChunkState struct {
	Next int   `json:"next"`
	Size int64 `json:"size"`
}

func readChunkState(file string) recChunkState {
	var s recChunkState
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// AppendRecChunk appends chunk number seq to av.webm. The chunks come in order from 0: the same seq again changes
// nothing (its bytes are read and dropped), a gap is refused with the number wanted. Whatever a failed append left
// behind the whole part is cut off before the next one.
func (l *Library) AppendRecChunk(id, rec string, seq int, body io.Reader) error {
	mu := l.recLock(id, rec)
	mu.Lock()
	defer mu.Unlock()
	meta, err := l.readRecMeta(id, rec)
	if err != nil {
		return err
	}
	dir := l.recDir(id, rec)
	st := readChunkState(filepath.Join(dir, recStateFile))
	if seq < st.Next {
		_, _ = io.Copy(io.Discard, io.LimitReader(body, recChunkMax))
		return nil
	}
	if meta.State != DeckRecRecording {
		return errRecFinished
	}
	if seq > st.Next {
		return errRecNext{st.Next}
	}
	f, err := os.OpenFile(filepath.Join(dir, recMediaFile), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(st.Size); err != nil {
		return err
	}
	if _, err := f.Seek(st.Size, io.SeekStart); err != nil {
		return err
	}
	if st.Size == 0 {
		var head [12]byte
		got, _ := io.ReadFull(body, head[:])
		if t := recMimeTypes[recBase(meta.Mime)]; t.Magic == nil || !t.Magic(head[:got]) {
			return errRecFormat
		}
		body = io.MultiReader(bytes.NewReader(head[:got]), body)
	}
	n, err := io.Copy(f, io.LimitReader(body, recChunkMax+1))
	if err != nil {
		return err
	}
	if n > recChunkMax || st.Size+n > recMediaMax {
		_ = f.Truncate(st.Size)
		return errRecTooBig
	}
	if err := f.Sync(); err != nil {
		return err
	}
	raw, _ := json.Marshal(recChunkState{Next: st.Next + 1, Size: st.Size + n})
	return writeFileAtomic(filepath.Join(dir, recStateFile), raw)
}

var recEventKinds = map[string]bool{DeckEvSlide: true, DeckEvStep: true, DeckEvCursor: true, DeckEvClick: true,
	DeckEvLaser: true, DeckEvPause: true, DeckEvResume: true, DeckEvRetake: true, DeckEvASR: true, DeckEvCam: true}

// AppendRecEvents appends lines to events.jsonl; a line of no known kind or with a time outside the recording is
// dropped. It says how many were kept.
func (l *Library) AppendRecEvents(id, rec string, lines []DeckEvent) (int, error) {
	mu := l.recLock(id, rec)
	mu.Lock()
	defer mu.Unlock()
	meta, err := l.readRecMeta(id, rec)
	if err != nil {
		return 0, err
	}
	if meta.State != DeckRecRecording {
		return 0, errRecFinished
	}
	var buf []byte
	kept := 0
	for _, ev := range lines {
		if !recEventKinds[ev.K] || ev.T < 0 || ev.T > recDurMax {
			continue
		}
		raw, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		buf = append(append(buf, raw...), '\n')
		kept++
	}
	if kept == 0 {
		return 0, nil
	}
	file := filepath.Join(l.recDir(id, rec), recEventsFile)
	if fileSize(file)+int64(len(buf)) > recEventsMax {
		return 0, errRecTooBig
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	_, err = f.Write(buf)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return kept, err
}

// FinishRec closes the recording at durationMs, fixes its takes from the events and starts the transcript behind it.
// Finishing a finished recording changes nothing.
func (l *Library) FinishRec(id, rec string, durationMs int64) (DeckRecording, error) {
	mu := l.recLock(id, rec)
	mu.Lock()
	meta, err := l.readRecMeta(id, rec)
	if err != nil {
		mu.Unlock()
		return DeckRecording{}, err
	}
	if meta.State == DeckRecRecording {
		if durationMs < 0 || durationMs > recDurMax {
			durationMs = 0
		}
		meta.DurationMs = durationMs
		meta.Takes = deckTakes(meta.Slides, readRecEvents(filepath.Join(l.recDir(id, rec), recEventsFile)), durationMs)
		meta.State = DeckRecDone
		if err := l.writeRecMeta(id, meta); err != nil {
			mu.Unlock()
			return DeckRecording{}, err
		}
		l.startTranscript(id, rec)
	}
	mu.Unlock()
	return l.ReadRecording(id, rec)
}

func validEdit(e DeckEdit) (DeckEdit, bool) {
	switch e.Cursor {
	case "smooth", "off":
	default:
		return e, false
	}
	switch e.Cam {
	case "br", "bl", "tr", "tl", "off":
	default:
		return e, false
	}
	if len(e.Kept) > recKeptMax {
		return e, false
	}
	kept := make([]string, 0, len(e.Kept))
	seen := map[string]bool{}
	for _, k := range e.Kept {
		a, b, ok := strings.Cut(k, ".")
		if _, ea := strconv.Atoi(a); !ok || ea != nil {
			return e, false
		}
		if _, eb := strconv.Atoi(b); eb != nil {
			return e, false
		}
		if !seen[k] {
			seen[k] = true
			kept = append(kept, k)
		}
	}
	e.Kept = kept
	return e, true
}

func (l *Library) readRecEdit(id, rec string) DeckEdit {
	e := DefaultDeckEdit()
	if raw, err := os.ReadFile(filepath.Join(l.recDir(id, rec), recEditFile)); err == nil {
		var read DeckEdit
		if json.Unmarshal(raw, &read) == nil {
			if v, ok := validEdit(read); ok {
				return v
			}
		}
	}
	return e
}

// WriteRecEdit keeps the montage; false when it is not one (a corner or a cursor mode that does not exist).
func (l *Library) WriteRecEdit(id, rec string, e DeckEdit) (DeckRecording, bool, error) {
	if _, err := l.readRecMeta(id, rec); err != nil {
		return DeckRecording{}, false, err
	}
	if e.Kept == nil {
		e.Kept = []string{}
	}
	e, ok := validEdit(e)
	if !ok {
		return DeckRecording{}, false, nil
	}
	raw, _ := json.Marshal(e)
	mu := l.recLock(id, rec)
	mu.Lock()
	err := writeFileAtomic(filepath.Join(l.recDir(id, rec), recEditFile), raw)
	mu.Unlock()
	if err != nil {
		return DeckRecording{}, true, err
	}
	out, err := l.ReadRecording(id, rec)
	return out, true, err
}

// ReadRecording is the recording as the page shows it. A finished recording whose transcript is not there yet (being
// built, or lost with a restart) has no segments; reading it starts the build when none is running.
func (l *Library) ReadRecording(id, rec string) (DeckRecording, error) {
	meta, err := l.readRecMeta(id, rec)
	if err != nil {
		return DeckRecording{}, err
	}
	tr, have := l.readRecTranscript(id, rec)
	if meta.State == DeckRecDone && !have {
		l.startTranscript(id, rec)
	}
	edit := l.readRecEdit(id, rec)
	_, cutMs := deckKeep(meta.DurationMs, meta.Takes, tr, edit)
	return DeckRecording{Summary: l.recView(id, meta), Segments: tr.Segments, Edit: edit, CutMs: cutMs,
		Retaken: retakenSlides(meta.Slides, meta.Takes)}, nil
}

// DeckRecordings are the deck's recordings, newest first, for the deck's state.
func (l *Library) DeckRecordings(id string) []DeckRecordingInfo {
	out := []DeckRecordingInfo{}
	names, _ := readNames(filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckRecDir)))
	for _, n := range names {
		m, err := l.readRecMeta(id, n)
		if err != nil {
			continue
		}
		m = l.recView(id, m)
		out = append(out, DeckRecordingInfo{ID: m.ID, At: m.StartedAt, DurationMs: m.DurationMs, State: m.State,
			Retaken: retakenSlides(m.Slides, m.Takes)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

// DeckPublishedState says what is under DeckPublishedDir for guests.
func (l *Library) DeckPublishedState(id string) DeckPublished {
	dir := filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckPublishedDir))
	p := DeckPublished{Video: fileSize(filepath.Join(dir, "video.mp4")) > 0, PDF: fileSize(filepath.Join(dir, "deck.pdf")) > 0}
	l.deckPdfStatus(id, &p)
	return p
}

// DeckRecsRev is the newest mtime (ms) of the files of the deck's recordings and of what it published, a part of the
// deck's rev.
func (l *Library) DeckRecsRev(id string) int64 {
	var rev int64
	note := func(file string) {
		if st, err := os.Stat(file); err == nil && st.ModTime().UnixMilli() > rev {
			rev = st.ModTime().UnixMilli()
		}
	}
	root := filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckRecDir))
	names, _ := readNames(root)
	for _, n := range names {
		for _, f := range []string{recMetaFile, recTranscriptFile, recEditFile} {
			note(filepath.Join(root, n, f))
		}
	}
	pub := filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckPublishedDir))
	for _, f := range []string{"video.mp4", "chapters.json", "deck.pdf"} {
		note(filepath.Join(pub, f))
	}
	return rev
}

// ---- routes

func recGoneAnswer(r *http.Request) Answer {
	return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such recording", "Нет такой записи")), noStore())
}

func recFailAnswer(r *http.Request, err error) Answer {
	var next errRecNext
	switch {
	case errors.Is(err, errRecGone):
		return recGoneAnswer(r)
	case errors.As(err, &next):
		return JSON(r, http.StatusConflict, jsvalue.NewObject("error", locale.Pick("A chunk is missing", "Пропущен кусок записи"),
			"code", "out-of-order", "next", float64(next.Next)), noStore())
	case errors.Is(err, errRecFinished):
		return JSON(r, http.StatusConflict, jsvalue.NewObject("error", locale.Pick("The recording is finished", "Запись уже закончена"),
			"code", "finished"), noStore())
	case errors.Is(err, errRecFormat):
		return JSON(r, http.StatusUnsupportedMediaType, errorBody(locale.Pick("The bytes are not in the format the recording was started in",
			"Данные не в том формате, в котором начата запись")), noStore())
	case errors.Is(err, errRecTooBig):
		return JSON(r, http.StatusRequestEntityTooLarge, errorBody(locale.Pick("The recording is too big", "Запись слишком большая")), noStore())
	}
	return JSON(r, http.StatusInternalServerError, errorBody(err.Error()), noStore())
}

func badRecRequest(r *http.Request) Answer {
	return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown design or recording", "Неизвестный макет или запись")), noStore())
}

// recBody is a recording route's JSON body: the artifact, the recording and the rest as the route reads it.
func recBody(body *jsvalue.Object, into any) error {
	return json.Unmarshal(nodejson.Marshal(body), into)
}

// DeckRec is GET ?artifact=&rec= (view): the DeckRecording; and POST {artifact, camera, mime} (edit): {rec} with the
// new recording's id.
func (a *API) DeckRec(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodGet, http.MethodPost)
	if refused != nil {
		return *refused
	}
	if r.Method == http.MethodGet {
		artifact, rec := QueryString(r, "artifact"), QueryString(r, "rec")
		if !IsLibraryID(artifact) || !IsPageID(rec) {
			return badRecRequest(r)
		}
		if no := a.gate(r, artifact, RoleView); no != nil {
			return *no
		}
		out, err := a.L.ReadRecording(artifact, rec)
		if err != nil {
			return recFailAnswer(r, err)
		}
		return JSON(r, http.StatusOK, out, noStore())
	}
	artifact := jscompat.String(Field(body, "artifact"))
	if !IsLibraryID(artifact) {
		return badRecRequest(r)
	}
	if no := a.gate(r, artifact, RoleEdit); no != nil {
		return *no
	}
	camera, _ := Field(body, "camera").(bool)
	mime, _ := Field(body, "mime").(string)
	mime = strings.TrimSpace(mime)
	_, mime, okMime := cleanRecMime(mime)
	if !okMime {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Not a recording format: video/webm, video/mp4, audio/webm or audio/ogg",
			"Такой формат записи не поддерживается: нужен video/webm, video/mp4, audio/webm или audio/ogg")), noStore())
	}
	m, err := a.L.CreateRec(artifact, camera, mime)
	if err != nil {
		return recFailAnswer(r, err)
	}
	return JSON(r, http.StatusOK, jsvalue.NewObject("rec", m.ID), noStore())
}

// recBodyGuard is the check of a route that carries bytes or answers them: the dispatcher's, the session, the method
// and the role. The body is not read. False after answering.
func (a *API) recBodyGuard(w http.ResponseWriter, r *http.Request, method string, min Role) (artifact, rec string, ok bool) {
	if status, v, h, refused := a.APIRefusal(r); refused {
		writeAnswer(w, Answer{status, v, h})
		return "", "", false
	}
	if !a.Session(r) {
		writeAnswer(w, JSON(r, http.StatusForbidden, errorBody("Forbidden"), noStore()))
		return "", "", false
	}
	if r.Method != method && !(method == http.MethodGet && r.Method == http.MethodHead) {
		h := noStore()
		h.Set("Allow", method)
		writeAnswer(w, Empty(http.StatusMethodNotAllowed, h))
		return "", "", false
	}
	if r.Method == http.MethodPost && !recSimpleSafe(r) {
		writeAnswer(w, JSON(r, http.StatusUnsupportedMediaType, errorBody(locale.Pick("A recording chunk is sent as application/octet-stream, video/* or audio/*",
			"Кусок записи отправляется как application/octet-stream, video/* или audio/*")), noStore()))
		return "", "", false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "same-site" && site != "none" {
		writeAnswer(w, JSON(r, http.StatusForbidden, errorBody("Forbidden"), noStore()))
		return "", "", false
	}
	artifact, rec = QueryString(r, "artifact"), QueryString(r, "rec")
	if !IsLibraryID(artifact) || !IsPageID(rec) {
		writeAnswer(w, badRecRequest(r))
		return "", "", false
	}
	if no := a.gate(r, artifact, min); no != nil {
		writeAnswer(w, *no)
		return "", "", false
	}
	return artifact, rec, true
}

// recSimpleSafe says the request has a Content-Type a page cannot send across sites without a preflight: the bytes of
// a recording, or JSON. text/plain, forms and no type at all are what a cross-site form sends, so they are refused.
func recSimpleSafe(r *http.Request) bool {
	// Only the type counts; a recorder's own type has unquoted commas in its codecs, which a parser refuses.
	ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	ct = strings.ToLower(strings.TrimSpace(ct))
	return ct == "application/octet-stream" || ct == "application/json" || strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "audio/")
}

// DeckRecChunk is POST ?artifact=&rec=&seq= with webm bytes (edit): 204, the same seq again changes nothing, a seq out
// of order answers 409 {next}.
func (a *API) DeckRecChunk(w http.ResponseWriter, r *http.Request) {
	artifact, rec, ok := a.recBodyGuard(w, r, http.MethodPost, RoleEdit)
	if !ok {
		return
	}
	seq, err := strconv.Atoi(QueryString(r, "seq"))
	if err != nil || seq < 0 {
		writeAnswer(w, JSON(r, http.StatusBadRequest, errorBody(locale.Pick("seq is a number from 0", "seq — число от 0")), noStore()))
		return
	}
	if err := a.L.AppendRecChunk(artifact, rec, seq, r.Body); err != nil {
		writeAnswer(w, recFailAnswer(r, err))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// DeckRecEvents is POST {artifact, rec, lines: [DeckEvent]} (edit): 204.
func (a *API) DeckRecEvents(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodPost)
	if refused != nil {
		return *refused
	}
	var req struct {
		Artifact string      `json:"artifact"`
		Rec      string      `json:"rec"`
		Lines    []DeckEvent `json:"lines"`
	}
	if recBody(body, &req) != nil || !IsLibraryID(req.Artifact) || !IsPageID(req.Rec) || len(req.Lines) > recLinesMax {
		return badRecRequest(r)
	}
	if no := a.gate(r, req.Artifact, RoleEdit); no != nil {
		return *no
	}
	if _, err := a.L.AppendRecEvents(req.Artifact, req.Rec, req.Lines); err != nil {
		return recFailAnswer(r, err)
	}
	return Empty(http.StatusNoContent, noStore())
}

// DeckRecFinish is POST {artifact, rec, durationMs} (edit): the DeckRecording; the transcript is built behind it.
func (a *API) DeckRecFinish(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodPost)
	if refused != nil {
		return *refused
	}
	var req struct {
		Artifact   string  `json:"artifact"`
		Rec        string  `json:"rec"`
		DurationMs float64 `json:"durationMs"`
	}
	if recBody(body, &req) != nil || !IsLibraryID(req.Artifact) || !IsPageID(req.Rec) {
		return badRecRequest(r)
	}
	if no := a.gate(r, req.Artifact, RoleEdit); no != nil {
		return *no
	}
	out, err := a.L.FinishRec(req.Artifact, req.Rec, int64(req.DurationMs))
	if err != nil {
		return recFailAnswer(r, err)
	}
	return JSON(r, http.StatusOK, out, noStore())
}

// DeckRecEdit is PUT {artifact, rec, edit: DeckEdit} (edit): the DeckRecording.
func (a *API) DeckRecEdit(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodPut)
	if refused != nil {
		return *refused
	}
	var req struct {
		Artifact string   `json:"artifact"`
		Rec      string   `json:"rec"`
		Edit     DeckEdit `json:"edit"`
	}
	if recBody(body, &req) != nil || !IsLibraryID(req.Artifact) || !IsPageID(req.Rec) {
		return badRecRequest(r)
	}
	if no := a.gate(r, req.Artifact, RoleEdit); no != nil {
		return *no
	}
	out, valid, err := a.L.WriteRecEdit(req.Artifact, req.Rec, req.Edit)
	if err != nil {
		return recFailAnswer(r, err)
	}
	if !valid {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Not a montage", "Это не монтаж")), noStore())
	}
	return JSON(r, http.StatusOK, out, noStore())
}

// DeckRecMedia is GET ?artifact=&rec= with Range (view): av.webm through http.ServeContent.
func (a *API) DeckRecMedia(w http.ResponseWriter, r *http.Request) {
	artifact, rec, ok := a.recBodyGuard(w, r, http.MethodGet, RoleView)
	if !ok {
		return
	}
	meta, err := a.L.readRecMeta(artifact, rec)
	if err != nil {
		writeAnswer(w, recGoneAnswer(r))
		return
	}
	f, err := os.Open(filepath.Join(a.L.recDir(artifact, rec), recMediaFile))
	if err != nil {
		writeAnswer(w, recGoneAnswer(r))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		writeAnswer(w, recGoneAnswer(r))
		return
	}
	ctype, ext := "application/octet-stream", "bin"
	if t, ok := recMimeTypes[recBase(meta.Mime)]; ok {
		ctype, ext = recBase(meta.Mime), t.Ext
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Disposition", `inline; filename="recording.`+ext+`"`)
	h.Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, recMediaFile, st.ModTime(), f)
}
