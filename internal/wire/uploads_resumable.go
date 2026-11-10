package wire

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// Uploads of any size in chunks (HRN-826), on the same store as the one-shot PUT: the page opens a session
// (POST /uploads {name,size,type}), sends the file in chunks (PATCH /uploads/{id} with Upload-Offset), asks where the
// machine is after a break (HEAD /uploads/{id}) and closes it (POST /uploads/{id}/complete), which renames the
// temporary file into place and answers the AttachmentRef. Nothing is held in memory: a chunk goes from the socket to
// the file. The offset is the size of the temporary file, so it survives a restart of the runtime. The only limit is
// the free space of the disk (and the images' own limit: the agents take no bigger picture).
//
// These answers are not Refusals: they are {"error","message"} with the message in Russian, and the numbers the page
// shows ("нужно 4 ГБ, свободно 1 ГБ").

const (
	// UploadChunkBytes is the chunk the page is asked to send: well under every proxy's body limit (the ingress
	// takes 50 MB).
	UploadChunkBytes = 8 << 20
	// uploadChunkMax is the most a single PATCH takes.
	uploadChunkMax = 32 << 20
	// uploadReserveBytes stays free on the disk after an upload.
	uploadReserveBytes = 64 << 20
	uploadSessionTTL   = 24 * time.Hour
	uploadSweepEvery   = time.Hour
)

var uploadIDPattern = regexp.MustCompile(`^up_[A-Z2-7]{16}$`)

type uploadSession struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Size    int64  `json:"size"`
	Created int64  `json:"created"`
}

func (u *Uploads) sessionFile(id string) string { return filepath.Join(u.Dir, ".s-"+id+".json") }
func (u *Uploads) partFile(id string) string    { return filepath.Join(u.Dir, ".s-"+id+".part") }

func uploadError(w http.ResponseWriter, status int, code, message string, extra map[string]any) {
	body := map[string]any{"error": code, "message": message}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

// cleanUploadName is the name a person sees for the file: its last segment, without control characters.
func cleanUploadName(name string) (string, bool) {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || name == "/" || len(name) > 255 || !utf8.ValidString(name) {
		return "", false
	}
	return name, true
}

// lock keeps two requests from writing one upload at once; it reports whether the caller holds it.
func (u *Uploads) lock(id string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.busy == nil {
		u.busy = map[string]bool{}
	}
	if u.busy[id] {
		return false
	}
	u.busy[id] = true
	return true
}

func (u *Uploads) unlock(id string) {
	u.mu.Lock()
	delete(u.busy, id)
	u.mu.Unlock()
}

func (u *Uploads) load(id string) (uploadSession, int64, bool) {
	var s uploadSession
	if !uploadIDPattern.MatchString(id) {
		return s, 0, false
	}
	raw, err := os.ReadFile(u.sessionFile(id))
	if err != nil || json.Unmarshal(raw, &s) != nil || s.ID != id {
		return s, 0, false
	}
	st, err := os.Stat(u.partFile(id))
	if err != nil {
		return s, 0, false
	}
	return s, st.Size(), true
}

func (u *Uploads) drop(id string) {
	_ = os.Remove(u.partFile(id))
	_ = os.Remove(u.sessionFile(id))
}

// sweep removes the sessions untouched for a day with their temporary files; at most once an hour.
func (u *Uploads) sweep() {
	u.mu.Lock()
	if time.Since(u.swept) < uploadSweepEvery {
		u.mu.Unlock()
		return
	}
	u.swept = time.Now()
	u.mu.Unlock()
	entries, _ := os.ReadDir(u.Dir)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".s-") || !strings.HasSuffix(name, ".part") {
			continue
		}
		st, err := e.Info()
		if err != nil || time.Since(st.ModTime()) < uploadSessionTTL {
			continue
		}
		u.drop(strings.TrimSuffix(strings.TrimPrefix(name, ".s-"), ".part"))
	}
}

// UploadCreate serves POST /api/v2/uploads: opens a session for a file of size bytes.
func (s *Server) UploadCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Type string `json:"type"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil || body.Size < 0 {
		uploadError(w, http.StatusBadRequest, "bad-request", "Не удалось начать загрузку: запрос непонятен", nil)
		return
	}
	u := s.Uploads
	name, ok := cleanUploadName(body.Name)
	if !ok {
		uploadError(w, http.StatusBadRequest, "bad-name", "У файла недопустимое имя", nil)
		return
	}
	typ := "application/octet-stream"
	if body.Type != "" {
		mt, _, err := mime.ParseMediaType(body.Type)
		if err != nil {
			uploadError(w, http.StatusUnsupportedMediaType, "bad-type", "Не удалось определить тип файла", nil)
			return
		}
		typ = mt
	}
	limit := u.Limits.MaxFileBytes
	if imageMimes[typ] {
		limit = u.Limits.MaxImageBytes
	}
	if limit > 0 && body.Size > limit {
		uploadError(w, http.StatusRequestEntityTooLarge, "too-large", "Файл больше допустимого размера", map[string]any{"limit": limit})
		return
	}
	if err := os.MkdirAll(u.Dir, 0o700); err != nil {
		uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось подготовить место для файла", nil)
		return
	}
	if free, known := freeBytes(u.Dir); known && body.Size+uploadReserveBytes > free {
		uploadError(w, http.StatusInsufficientStorage, "no-space", "На диске машины не хватает места для этого файла",
			map[string]any{"free": free, "need": body.Size})
		return
	}
	go u.sweep()
	id := newUploadID()
	f, err := os.OpenFile(u.partFile(id), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось создать файл", nil)
		return
	}
	_ = f.Close()
	meta, _ := json.Marshal(uploadSession{ID: id, Name: name, Mime: typ, Size: body.Size, Created: time.Now().UnixMilli()})
	if err := writeFileAtomic(u.sessionFile(id), meta); err != nil {
		_ = os.Remove(u.partFile(id))
		uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось начать загрузку", nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "offset": 0, "chunkSize": UploadChunkBytes})
}

// uploadSessionCall serves HEAD, PATCH, DELETE of /uploads/{id} and POST /uploads/{id}/complete.
func (s *Server) uploadSessionCall(w http.ResponseWriter, r *http.Request, id, action string) bool {
	u := s.Uploads
	switch {
	case action == "" && r.Method == http.MethodHead, action == "" && r.Method == http.MethodPatch, action == "" && r.Method == http.MethodDelete:
	case action == "complete" && r.Method == http.MethodPost:
	default:
		return false
	}
	if r.Method == http.MethodDelete {
		if uploadIDPattern.MatchString(id) && u.lock(id) {
			u.drop(id)
			u.unlock(id)
		}
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	sess, offset, ok := u.load(id)
	if !ok && r.Method == http.MethodHead {
		return false // a HEAD of a finished upload is the file's own
	}
	if !ok {
		uploadError(w, http.StatusNotFound, "no-upload", "Загрузка не найдена: её нужно начать заново", nil)
		return true
	}
	switch {
	case r.Method == http.MethodHead:
		w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
		w.Header().Set("Upload-Length", strconv.FormatInt(sess.Size, 10))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
	case action == "complete":
		s.uploadComplete(w, sess, offset)
	default:
		s.uploadChunk(w, r, sess, offset)
	}
	return true
}

func (s *Server) uploadChunk(w http.ResponseWriter, r *http.Request, sess uploadSession, offset int64) {
	u := s.Uploads
	if !u.lock(sess.ID) {
		w.Header().Set("Retry-After", "1")
		uploadError(w, http.StatusTooManyRequests, "busy", "Эта загрузка ещё принимает предыдущий кусок", nil)
		return
	}
	defer u.unlock(sess.ID)
	// The offset on disk may have grown while the lock was awaited.
	if st, err := os.Stat(u.partFile(sess.ID)); err == nil {
		offset = st.Size()
	}
	at, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || at < 0 {
		uploadError(w, http.StatusBadRequest, "bad-offset", "Не удалось понять, с какого места продолжать", nil)
		return
	}
	if at != offset {
		uploadError(w, http.StatusConflict, "offset-mismatch", "Машина приняла другую часть файла", map[string]any{"offset": offset})
		return
	}
	room := min(sess.Size-offset, uploadChunkMax)
	if r.ContentLength > room {
		uploadError(w, http.StatusRequestEntityTooLarge, "chunk-too-large", "Кусок больше, чем остаётся файла", nil)
		return
	}
	f, err := os.OpenFile(u.partFile(sess.ID), os.O_WRONLY, 0o600)
	if err != nil {
		uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось записать файл", nil)
		return
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось записать файл", nil)
		return
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, room))
	if err == nil && n == room {
		var one [1]byte
		if m, _ := r.Body.Read(one[:]); m > 0 {
			_ = f.Truncate(offset + n)
			uploadError(w, http.StatusRequestEntityTooLarge, "chunk-too-large", "Кусок больше, чем остаётся файла", nil)
			return
		}
	}
	if err != nil {
		if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
			uploadError(w, http.StatusInsufficientStorage, "no-space", "На диске машины закончилось место", nil)
			return
		}
		// The connection broke inside the chunk: what arrived stays, the page asks for the offset and goes on from it.
		uploadError(w, http.StatusBadRequest, "interrupted", "Соединение прервалось, загрузка продолжится", map[string]any{"offset": offset + n})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"offset": offset + n})
}

func (s *Server) uploadComplete(w http.ResponseWriter, sess uploadSession, offset int64) {
	u := s.Uploads
	if !u.lock(sess.ID) {
		w.Header().Set("Retry-After", "1")
		uploadError(w, http.StatusTooManyRequests, "busy", "Эта загрузка ещё принимает предыдущий кусок", nil)
		return
	}
	defer u.unlock(sess.ID)
	if offset != sess.Size {
		uploadError(w, http.StatusConflict, "incomplete", "Файл принят не целиком", map[string]any{"offset": offset})
		return
	}
	part := u.partFile(sess.ID)
	if f, err := os.OpenFile(part, os.O_WRONLY, 0o600); err == nil {
		err = f.Sync()
		_ = f.Close()
		if err != nil {
			uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось сохранить файл", nil)
			return
		}
	}
	kind := AttachmentFile
	if imageMimes[sess.Mime] {
		kind = AttachmentImage
	}
	ref := AttachmentRef{ID: sess.ID, Name: sess.Name, Kind: kind, Mime: sess.Mime}
	meta, _ := json.Marshal(ref)
	if err := writeFileAtomic(filepath.Join(u.Dir, ref.ID+".json"), meta); err != nil {
		uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось сохранить файл", nil)
		return
	}
	if err := os.Rename(part, filepath.Join(u.Dir, ref.ID+uploadExt(ref))); err != nil {
		_ = os.Remove(filepath.Join(u.Dir, ref.ID+".json"))
		uploadError(w, http.StatusInternalServerError, "internal", "На машине не удалось сохранить файл", nil)
		return
	}
	_ = os.Remove(u.sessionFile(sess.ID))
	writeJSON(w, http.StatusOK, ref)
}
