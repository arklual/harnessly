package peers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxBody is the largest request body the peers routes read.
const MaxBody = 256 << 10

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// ReadJSON decodes a request body of at most MaxBody into out.
func ReadJSON(r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
	if err != nil || len(raw) > MaxBody {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// sender is the verified sender of a request from another machine; a refusal is answered here.
func (s *Service) sender(w http.ResponseWriter, r *http.Request, secret []byte) (Sender, bool) {
	sender, err := VerifyInbox(secret, bearer(r), time.Now())
	if err != nil {
		writeJSON(w, http.StatusForbidden, refused("", RefuseUnverified, ""))
		return Sender{}, false
	}
	// A message cannot come from this very machine through the gateway: that would be a token replayed back.
	if strings.EqualFold(sender.Machine, s.Self.Machine) && s.Self.Machine != "" {
		writeJSON(w, http.StatusForbidden, refused("", RefuseUnverified, "own machine"))
		return Sender{}, false
	}
	return sender, true
}

// InboxHandler is POST /api/peers/inbox: a message from another machine, forwarded by the gateway with a token signed
// with this runtime's secret. The sender sees the answer: delivered, or refused and why.
func (s *Service) InboxHandler(secret []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		sender, ok := s.sender(w, r, secret)
		if !ok {
			return
		}
		var in Inbound
		if !ReadJSON(r, &in) {
			writeJSON(w, http.StatusBadRequest, refused("", RefuseBadRef, "body"))
			return
		}
		writeJSON(w, http.StatusOK, s.Receive(r.Context(), sender, in))
	})
}

// DirectoryHandler is GET /api/peers/directory: this machine's chats, for another machine's agent the allow-list lets in.
func (s *Service) DirectoryHandler(secret []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		sender, ok := s.sender(w, r, secret)
		if !ok {
			return
		}
		if allowed, why := s.Settings.Get().Decide(sender); !allowed {
			writeJSON(w, http.StatusForbidden, refused("", why, ""))
			return
		}
		chats, _ := s.Directory(r.Context(), sender, false)
		writeJSON(w, http.StatusOK, map[string]any{"chats": chats})
	})
}
