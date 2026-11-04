package browseract

import (
	"encoding/json"
	"net/http"
	"unicode/utf8"
)

const (
	maxBody    = 96 << 10 // a POST body
	maxOutcome = 64 << 10 // the text of an outcome
)

// ServeHTTP is the page's side, /api/v2/pane-act:
//
//	GET  ?conversation=<chat>  a long poll: 200 with one action, 204 when none came within Hold, 429 past 4 open polls
//	POST {conversation, id, state: done|failed|asking, text}  the outcome of an action: 204, 404 for an id not in flight
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.poll(w, r)
	case http.MethodPost:
		h.answer(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Hub) poll(w http.ResponseWriter, r *http.Request) {
	chatID := r.URL.Query().Get("conversation")
	if chatID == "" {
		http.Error(w, "conversation is required", http.StatusBadRequest)
		return
	}
	a, res := h.Poll(r.Context(), chatID)
	switch res {
	case PollTooMany:
		http.Error(w, "too many open polls for this chat", http.StatusTooManyRequests)
	case PollNothing:
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(a)
	}
}

func (h *Hub) answer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Conversation string `json:"conversation"`
		ID           string `json:"id"`
		State        string `json:"state"`
		Text         string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&body); err != nil ||
		body.Conversation == "" || body.ID == "" || utf8.RuneCountInString(body.Text) > maxOutcome {
		http.Error(w, "bad outcome", http.StatusBadRequest)
		return
	}
	switch body.State {
	case "done", "failed", "asking":
	default:
		http.Error(w, "bad outcome", http.StatusBadRequest)
		return
	}
	if !h.Answer(body.Conversation, body.ID, body.State, body.Text) {
		http.Error(w, "no such action", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
