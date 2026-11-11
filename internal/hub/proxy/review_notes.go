package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// reviewNotesPath is a chat's review remarks (the list) or one of them.
var reviewNotesPath = regexp.MustCompile(`/history/conversations/([^/]+)/review-notes(?:/([^/]+))?$`)

// reviewNoteMaxBytes bounds the body of one note: its text, lines and path, with room for the JSON around them.
const reviewNoteMaxBytes = 64 << 10

// serveReviewNotes keeps the draft remarks a person writes on a chat's work in the «Ревью кода» app. They are the
// person's own: read and written only under their id, so another person never sees them.
func (s *Service) serveReviewNotes(w http.ResponseWriter, r *http.Request, p *auth.Principal, conv, id string) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	bad := httpx.New(http.StatusBadRequest, "bad_request", "Некорректный запрос")
	switch {
	case id == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		notes, err := history.ReviewNotes(ctx, s.db, p.User.ID, conv)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"conversationId": conv, "notes": notes})
	case id != "" && r.Method == http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, reviewNoteMaxBytes)
		var note history.ReviewNote
		if err := json.NewDecoder(r.Body).Decode(&note); err != nil || note.ID != id {
			httpx.WriteError(w, r, bad)
			return
		}
		kept, err := history.PutReviewNote(ctx, s.db, p.User.ID, conv, note, time.Now().UnixMilli())
		switch {
		case errors.Is(err, history.ErrBadNote):
			httpx.WriteError(w, r, bad)
		case errors.Is(err, history.ErrTooManyNotes):
			httpx.WriteError(w, r, httpx.New(http.StatusConflict, "too_many_notes", "Слишком много замечаний к одному чату"))
		case err != nil:
			s.fail(w, r, err)
		default:
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"note": kept})
		}
	case id != "" && r.Method == http.MethodDelete:
		err := history.DeleteReviewNote(ctx, s.db, p.User.ID, conv, id)
		if errors.Is(err, history.ErrBadNote) {
			httpx.WriteError(w, r, bad)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		httpx.WriteError(w, r, httpx.New(http.StatusMethodNotAllowed, "method_not_allowed", "Метод не поддерживается"))
	}
}
