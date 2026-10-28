package design

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A deck's exports: a job per file, run one at a time on the machine by the exporter registered for its format
// (RegisterDeckExporter), its file kept under DeckExportsDir for a day. A job lives in memory, keyed by project and id.
// A machine that lacks what the format needs (Chrome, ffmpeg) fails the job at once with a reason in words.

const (
	deckJobTTL     = 24 * time.Hour
	deckJobTimeout = 40 * time.Minute
	deckJobKeep    = 200
)

// deckJob is a job as kept: the answer's shape plus what the runner needs.
type deckJob struct {
	DeckExportJob
	artifact string
	rec      string
	publish  bool
	path     string
	ext      string
	made     time.Time
}

type deckJobs struct {
	mu   sync.Mutex
	jobs map[string]*deckJob
}

var (
	deckJobStates sync.Map
	// The machine runs one job at a time, whatever the project, in the order they were made.
	deckTurnMu   sync.Mutex
	deckTurnBusy bool
	deckTurnWait []chan struct{}
)

// deckTurn takes a place in the line; the channel closes when it is the job's turn. deckTurnDone gives the turn on.
func deckTurn() chan struct{} {
	deckTurnMu.Lock()
	defer deckTurnMu.Unlock()
	ch := make(chan struct{})
	if !deckTurnBusy {
		deckTurnBusy = true
		close(ch)
		return ch
	}
	deckTurnWait = append(deckTurnWait, ch)
	return ch
}

func deckTurnDone() {
	deckTurnMu.Lock()
	defer deckTurnMu.Unlock()
	if len(deckTurnWait) == 0 {
		deckTurnBusy = false
		return
	}
	next := deckTurnWait[0]
	deckTurnWait = deckTurnWait[1:]
	close(next)
}

func (l *Library) deckJobs() *deckJobs {
	if s, ok := deckJobStates.Load(l); ok {
		return s.(*deckJobs)
	}
	s, _ := deckJobStates.LoadOrStore(l, &deckJobs{jobs: map[string]*deckJob{}})
	return s.(*deckJobs)
}

func jobKey(artifact, id string) string { return artifact + "/" + id }

func (j *deckJob) snapshot() DeckExportJob {
	out := j.DeckExportJob
	if j.File != nil {
		f := *j.File
		out.File = &f
	}
	if j.Error != nil {
		e := *j.Error
		out.Error = &e
	}
	return out
}

// deckBinary is whether the machine has what an exporter needs.
func (l *Library) deckBinary(need string) error {
	switch need {
	case "chrome":
		if l.findChrome() == "" {
			return errNoChrome()
		}
	case "ffmpeg":
		if DeckFFmpeg() == "" {
			return errors.New(locale.Pick("ffmpeg is not installed on this machine", "На машине нет ffmpeg"))
		}
	default:
		if _, err := exec.LookPath(need); err != nil {
			return errors.New(need + locale.Pick(" is not installed on this machine", ": на машине этого нет"))
		}
	}
	return nil
}

// StartDeckExport makes the job of a format and starts it (or joins the one already queued or running for the same
// project, format and recording). publish also keeps a finished PDF as the file guests may read.
func (l *Library) StartDeckExport(artifact string, format DeckExportFormat, rec string, publish bool) (DeckExportJob, error) {
	exp, ok := DeckExporterOf(format)
	if !ok {
		return DeckExportJob{}, fmt.Errorf("%s: %s", locale.Pick("This format is not built yet", "Этот формат ещё не готов"), format)
	}
	s := l.deckJobs()
	s.mu.Lock()
	s.sweepLocked(l)
	for _, j := range s.jobs {
		if j.artifact == artifact && j.Format == format && j.rec == rec && (j.State == DeckJobQueued || j.State == DeckJobRunning) {
			j.publish = j.publish || publish
			snap := j.snapshot()
			s.mu.Unlock()
			return snap, nil
		}
	}
	id := randomHex(8)
	j := &deckJob{
		DeckExportJob: DeckExportJob{ID: id, Format: format, State: DeckJobQueued, At: time.Now().UTC().Format(time.RFC3339)},
		artifact:      artifact, rec: rec, publish: publish, ext: exp.Ext, made: time.Now(),
	}
	for _, need := range exp.Needs {
		if err := l.deckBinary(need); err != nil {
			msg := err.Error()
			j.State, j.Error = DeckJobFailed, &msg
			s.jobs[jobKey(artifact, id)] = j
			snap := j.snapshot()
			s.mu.Unlock()
			return snap, nil
		}
	}
	dir := filepath.Join(l.ArtifactDir(artifact), DeckExportsDir)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		s.mu.Unlock()
		return DeckExportJob{}, err
	}
	j.path = filepath.Join(dir, id+"."+exp.Ext)
	s.jobs[jobKey(artifact, id)] = j
	snap := j.snapshot()
	s.mu.Unlock()
	go l.runDeckJob(j, exp, deckTurn())
	return snap, nil
}

// DeckExportJobOf is a job by id; false when the machine does not know it (it restarted, or a day passed).
func (l *Library) DeckExportJobOf(artifact, id string) (DeckExportJob, string, bool) {
	s := l.deckJobs()
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[jobKey(artifact, id)]
	if j == nil {
		return DeckExportJob{}, "", false
	}
	return j.snapshot(), j.path, true
}

func (l *Library) runDeckJob(j *deckJob, exp DeckExporter, turn chan struct{}) {
	s := l.deckJobs()
	<-turn
	defer deckTurnDone()
	set := func(fn func()) {
		s.mu.Lock()
		fn()
		s.mu.Unlock()
	}
	set(func() { j.State = DeckJobRunning })
	// What the export reads is the deck as of now: a published PDF carries this time, so a change made while it was
	// built leaves it behind the deck (deckpdfauto.go).
	began := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), deckJobTimeout)
	defer cancel()
	tmp := j.path + ".part"
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("the export broke: %v", r)
			}
		}()
		return exp.Run(ctx, DeckExportRun{
			L: l, Artifact: j.artifact, Rec: j.rec, Out: tmp,
			Progress: func(p float64) {
				if p < 0 {
					p = 0
				}
				if p > 0.99 {
					p = 0.99
				}
				set(func() {
					if p > j.Progress {
						j.Progress = p
					}
				})
			},
		})
	}()
	if err == nil {
		err = os.Rename(tmp, j.path)
	}
	if err == nil && j.publish && j.Format == DeckExportPDF {
		err = l.publishDeckFile(j.artifact, j.path, "deck.pdf")
		if err == nil {
			pub := filepath.Join(l.ArtifactDir(j.artifact), filepath.FromSlash(DeckPublishedDir), "deck.pdf")
			err = os.Chtimes(pub, began, began)
		}
	}
	if err != nil {
		_ = os.Remove(tmp)
		_ = os.Remove(j.path)
		msg := line(err.Error())
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = locale.Pick("The export took too long and was stopped", "Экспорт шёл слишком долго и был остановлен")
		}
		set(func() { j.State, j.Error = DeckJobFailed, &msg })
	} else {
		name := filepath.Base(j.path)
		set(func() { j.State, j.Progress, j.File = DeckJobDone, 1, &name })
	}
	time.AfterFunc(deckJobTTL, func() { s.drop(j) })
}

// publishDeckFile copies a finished export to the folder guests read from, whole or not at all.
func (l *Library) publishDeckFile(artifact, from, name string) error {
	dir := filepath.Join(l.ArtifactDir(artifact), DeckPublishedDir)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := filepath.Join(dir, "."+name+"."+randomHex(4)+".tmp")
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

// drop forgets a finished job and deletes its file.
func (s *deckJobs) drop(j *deckJob) {
	s.mu.Lock()
	if cur := s.jobs[jobKey(j.artifact, j.ID)]; cur == j && (j.State == DeckJobDone || j.State == DeckJobFailed) {
		delete(s.jobs, jobKey(j.artifact, j.ID))
		if j.path != "" {
			_ = os.Remove(j.path)
		}
	}
	s.mu.Unlock()
}

// sweepLocked forgets the jobs a day old (their timers may have been lost to a restart of the page) and keeps the
// list short.
func (s *deckJobs) sweepLocked(l *Library) {
	for k, j := range s.jobs {
		if time.Since(j.made) > deckJobTTL && (j.State == DeckJobDone || j.State == DeckJobFailed) {
			delete(s.jobs, k)
			if j.path != "" {
				_ = os.Remove(j.path)
			}
		}
	}
	for len(s.jobs) > deckJobKeep {
		var oldest string
		var at time.Time
		for k, j := range s.jobs {
			if (j.State == DeckJobDone || j.State == DeckJobFailed) && (oldest == "" || j.made.Before(at)) {
				oldest, at = k, j.made
			}
		}
		if oldest == "" {
			return
		}
		if j := s.jobs[oldest]; j.path != "" {
			_ = os.Remove(j.path)
		}
		delete(s.jobs, oldest)
	}
}

// SweepDeckExports deletes the files of a project's exports a day old: what a restart left behind.
func (l *Library) SweepDeckExports(artifact string) {
	dir := filepath.Join(l.ArtifactDir(artifact), DeckExportsDir)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > deckJobTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// deckJobValue is a job as the routes answer it.
func deckJobValue(j DeckExportJob) *jsvalue.Object {
	return jsvalue.NewObject("id", j.ID, "format", string(j.Format), "state", string(j.State), "progress", j.Progress,
		"file", strOrNull(j.File), "error", strOrNull(j.Error), "at", j.At)
}

func strOrNull(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// DeckExport is POST {artifact, format, rec?, publish?} (view; publish needs edit): the new DeckExportJob; and GET
// ?artifact=&job= (view): the job.
func (a *API) DeckExport(r *http.Request) Answer {
	body, refused := a.Guard(r, http.MethodGet, http.MethodPost)
	if refused != nil {
		return *refused
	}
	h := noStore()
	bad := func(status int, msg string) Answer { return JSON(r, status, errorBody(msg), h) }
	artifact := QueryString(r, "artifact")
	if r.Method != http.MethodGet {
		artifact = ""
		if v, ok := Field(body, "artifact").(string); ok {
			artifact = v
		}
	}
	if !IsLibraryID(artifact) {
		return bad(http.StatusBadRequest, locale.Pick("Unknown design", "Неизвестный макет"))
	}
	if no := a.gate(r, artifact, RoleView); no != nil {
		return *no
	}
	if r.Method == http.MethodGet {
		j, _, ok := a.L.DeckExportJobOf(artifact, QueryString(r, "job"))
		if !ok {
			return bad(http.StatusNotFound, locale.Pick("No such export: it may be older than a day", "Такого экспорта нет: возможно, он старше суток"))
		}
		return JSON(r, http.StatusOK, deckJobValue(j), h)
	}
	format := DeckExportFormat(jscompat.String(Field(body, "format")))
	known := false
	for _, f := range DeckExportFormats {
		known = known || f == format
	}
	if !known {
		return bad(http.StatusBadRequest, locale.Pick("Unknown format", "Неизвестный формат"))
	}
	rec := ""
	if v, ok := Field(body, "rec").(string); ok {
		rec = v
	}
	if rec != "" && !deckIDRe.MatchString(rec) {
		return bad(http.StatusBadRequest, locale.Pick("Unknown recording", "Неизвестная запись"))
	}
	publish := Field(body, "publish") == true
	if publish {
		if format != DeckExportPDF {
			return bad(http.StatusBadRequest, locale.Pick("Only the PDF is published here", "Здесь публикуется только PDF"))
		}
		if no := a.gate(r, artifact, RoleEdit); no != nil {
			return *no
		}
	}
	a.L.SweepDeckExports(artifact)
	j, err := a.L.StartDeckExport(artifact, format, rec, publish)
	if err != nil {
		status := http.StatusInternalServerError
		if _, ok := DeckExporterOf(format); !ok {
			status = http.StatusNotImplemented
		}
		return bad(status, err.Error())
	}
	return JSON(r, http.StatusAccepted, deckJobValue(j), h)
}

// deckTypes are the media types of the files an export makes.
var deckTypes = map[string]string{
	"pdf":  "application/pdf",
	"zip":  "application/zip",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"html": "text/html; charset=utf-8",
	"mp4":  "video/mp4",
}

// deckIDRe is the id of a recording (a ULID).
var deckIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// rfc5987 escapes a file name for the filename* parameter.
func rfc5987(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

var deckNameBad = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

// deckFileName is the name a download is given: the deck's title and the format's extension.
func deckFileName(title, ext string) string {
	name := strings.TrimSpace(deckNameBad.ReplaceAllString(title, " "))
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 80 {
		name = strings.TrimSpace(string(r[:80]))
	}
	name = strings.TrimLeft(name, ".")
	if name == "" {
		name = "deck"
	}
	return name + "." + ext
}

// asciiFileName is the same name for a client that reads only the plain filename parameter.
func asciiFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < unicode.MaxASCII && unicode.IsPrint(r) && r != '"' && r != '%' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// DeckExportFile is GET ?artifact=&job= (view): the finished file as an attachment named after the deck's title.
func (a *API) DeckExportFile(w http.ResponseWriter, r *http.Request) {
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
	j, path, ok := a.L.DeckExportJobOf(artifact, QueryString(r, "job"))
	if !ok || j.State != DeckJobDone || path == "" {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if j.Format == DeckExportPNG {
		ext = "zip"
	}
	title := ""
	if art := a.L.ReadArtifact(artifact); art != nil {
		title = art.Title
	}
	name := deckFileName(title, ext)
	ctype := deckTypes[ext]
	if ctype == "" {
		ctype = mime.TypeByExtension("." + ext)
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", `attachment; filename="`+asciiFileName(name)+`"; filename*=UTF-8''`+rfc5987(name))
	http.ServeContent(w, r, name, st.ModTime(), f)
}
