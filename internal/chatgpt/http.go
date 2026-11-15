package chatgpt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// Guard lets a request through or answers it itself: the runtime lets only the person's own page in (a page session,
// same origin), never an agent's cli-token.
type Guard func(w http.ResponseWriter, r *http.Request) bool

const (
	smallBodyMax = 64 << 10
	sendBodyMax  = filesTotalMax*4/3 + textMax*4 + (1 << 20)
	heartbeat    = 15 * time.Second
)

// Handler serves the integration's routes under base (e.g. /api/platform/chatgpt).
func (s *Service) Handler(base string, guard Guard) http.Handler {
	base = strings.TrimRight(base, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, base)
		if !ok || rest != "" && !strings.HasPrefix(rest, "/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": CodeNotFound})
			return
		}
		if !guard(w, r) {
			return
		}
		s.route(w, r, strings.Split(strings.Trim(rest, "/"), "/"))
	})
}

func (s *Service) route(w http.ResponseWriter, r *http.Request, p []string) {
	m := r.Method
	switch {
	case len(p) == 1 && p[0] == "" && m == http.MethodGet:
		st, err := s.State()
		answer(w, st, err)
	case len(p) == 1 && p[0] == "login" && m == http.MethodPost:
		var in struct {
			Account string `json:"account"`
		}
		if !readJSON(w, r, smallBodyMax, &in) {
			return
		}
		st, err := s.StartLogin(in.Account)
		answer(w, st, err)
	case len(p) == 1 && p[0] == "login" && m == http.MethodDelete:
		s.CancelLogin()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case len(p) == 2 && p[0] == "login" && p[1] == "callback" && m == http.MethodPost:
		var in struct {
			URL string `json:"url"`
		}
		if !readJSON(w, r, smallBodyMax, &in) {
			return
		}
		acct, err := s.CompleteLogin(r.Context(), in.URL)
		answer(w, map[string]any{"account": acct}, err)
	case len(p) == 3 && p[0] == "accounts" && p[2] == "logout" && m == http.MethodPost:
		revoked, err := s.SignOut(r.Context(), p[1])
		answer(w, map[string]bool{"revoked": revoked}, err)
	case len(p) == 3 && p[0] == "accounts" && p[2] == "active" && m == http.MethodPost:
		answer(w, map[string]bool{"ok": true}, s.Activate(p[1]))
	case len(p) == 2 && p[0] == "accounts" && m == http.MethodDelete:
		revoked, err := s.Forget(r.Context(), p[1])
		answer(w, map[string]bool{"revoked": revoked}, err)
	case len(p) == 1 && p[0] == "models" && m == http.MethodGet:
		ms, err := s.Models(r.Context())
		answer(w, map[string]any{"models": ms}, err)
	case len(p) == 1 && p[0] == "chats" && m == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"chats": s.Chats()})
	case len(p) == 1 && p[0] == "chats" && m == http.MethodPost:
		var in struct {
			Model  string `json:"model"`
			Effort string `json:"effort"`
		}
		if !readJSON(w, r, smallBodyMax, &in) {
			return
		}
		ch, err := s.CreateChat(in.Model, in.Effort)
		answer(w, map[string]any{"chat": ch}, err)
	case len(p) >= 2 && p[0] == "chats" && idRe.MatchString(p[1]):
		s.routeChat(w, r, p[1], p[2:])
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": CodeNotFound})
	}
}

func (s *Service) routeChat(w http.ResponseWriter, r *http.Request, id string, p []string) {
	m := r.Method
	switch {
	case len(p) == 0 && m == http.MethodGet:
		ch, running, err := s.Chat(id)
		answer(w, map[string]any{"chat": ch, "running": running}, err)
	case len(p) == 0 && m == http.MethodPatch:
		var in struct {
			Title string `json:"title"`
		}
		if !readJSON(w, r, smallBodyMax, &in) {
			return
		}
		answer(w, map[string]bool{"ok": true}, s.Rename(id, in.Title))
	case len(p) == 0 && m == http.MethodDelete:
		answer(w, map[string]bool{"ok": true}, s.Delete(id))
	case len(p) == 1 && p[0] == "messages" && m == http.MethodPost:
		var in struct {
			Text   string `json:"text"`
			Model  string `json:"model"`
			Effort string `json:"effort"`
			Files  []struct {
				Name string `json:"name"`
				Mime string `json:"mime"`
				Data string `json:"data"` // base64
			} `json:"files"`
		}
		if !readJSON(w, r, sendBodyMax, &in) {
			return
		}
		send := SendInput{Text: in.Text, Model: in.Model, Effort: in.Effort}
		for _, f := range in.Files {
			b, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": CodeBadRequest})
				return
			}
			send.Files = append(send.Files, Upload{Name: f.Name, Mime: f.Mime, Data: b})
		}
		msg, err := s.Send(id, send)
		answer(w, map[string]any{"message": msg}, err)
	case len(p) == 1 && p[0] == "spoken" && m == http.MethodPost:
		var in struct {
			Lines []Spoken `json:"lines"`
		}
		if !readJSON(w, r, int64(textMax*4+smallBodyMax), &in) {
			return
		}
		msgs, err := s.AppendSpoken(id, in.Lines)
		answer(w, map[string]any{"messages": msgs}, err)
	case len(p) == 1 && p[0] == "stop" && m == http.MethodPost:
		writeJSON(w, http.StatusOK, map[string]bool{"stopped": s.Stop(id)})
	case len(p) == 1 && p[0] == "stream" && m == http.MethodGet:
		s.serveStream(w, r, id)
	case len(p) == 2 && p[0] == "files" && idRe.MatchString(p[1]) && m == http.MethodGet:
		b, typ, name, err := s.FileOf(id, p[1])
		if err != nil {
			answer(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		disp := "attachment"
		if strings.HasPrefix(typ, "image/") {
			disp = "inline"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
		_, _ = w.Write(b)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": CodeNotFound})
	}
}

// serveStream follows the answer being written as server-sent events: delta {text}, done {message}; a chat with no
// answer running ends at once with idle.
func (s *Service) serveStream(w http.ResponseWriter, r *http.Request, id string) {
	if _, _, err := s.Chat(id); err != nil {
		answer(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	send := func(ev string, v any) bool {
		raw, _ := json.Marshal(v)
		if _, err := io.WriteString(w, "event: "+ev+"\ndata: "+string(raw)+"\n\n"); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	f := s.Follow(id)
	if f == nil {
		send("idle", map[string]bool{"idle": true})
		return
	}
	for {
		ctx, cancel := context.WithTimeout(r.Context(), heartbeat)
		delta, done, err := f.Next(ctx)
		cancel()
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			continue
		}
		if delta != "" && !send("delta", map[string]string{"text": delta}) {
			return
		}
		if done != nil {
			send("done", map[string]any{"message": done})
			return
		}
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, max int64, out any) bool {
	if ct := r.Header.Get("Content-Type"); r.ContentLength != 0 && !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": CodeBadRequest})
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": CodeBadRequest})
		return false
	}
	if len(raw) == 0 {
		return true
	}
	if json.Unmarshal(raw, out) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": CodeBadRequest})
		return false
	}
	return true
}

// answer writes v, or the error as {error: code, detail}. An unexpected error says only "failed": its text may name
// a path.
func answer(w http.ResponseWriter, v any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	var e *Error
	if errors.As(err, &e) {
		body := map[string]string{"error": e.Code}
		if e.Detail != "" {
			body["detail"] = e.Detail
		}
		writeJSON(w, e.Status, body)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
