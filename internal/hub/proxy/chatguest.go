package proxy

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

var chatGuestList = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/chat-guest-links$`)

var chatGuestManagement = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/shares/([A-Za-z0-9_-]{43})/guest$`)

func (s *Service) manageGuestChatSnapshot(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, id string) {
	if !s.cfg.ChatGuestLinks || s.uploadsStore() == nil || (r.Method != http.MethodPost && r.Method != http.MethodDelete) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	token, err := resaccess.New(s.db, s.now, "").SetSnapshotGuestLink(r.Context(), m.ID, id, u.ID, r.Method == http.MethodPost)
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		httpx.WriteJSON(w, resaccess.Status(err), map[string]string{"error": "Could not change the guest link"})
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"url": s.cfg.DesignShareURL + "/chat/" + token})
}

// ServeGuestChatSnapshot is mounted only on the configured guest origin.
func (s *Service) ServeGuestChatSnapshot(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/chat/")
	st := s.uploadsStore()
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.cfg.ChatGuestLinks || st == nil || !chatShareID.MatchString(token) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	g, err := resaccess.New(s.db, s.now, "").Resolve(r.Context(), token)
	if err != nil || g.Kind != resaccess.KindChat || g.Rights != "view" || g.Expires == nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	var sha string
	var expires int64
	var revoked sql.NullInt64
	err = s.db.QueryRow(r.Context(), `SELECT sha256,expires_at,revoked_at FROM chat_shares WHERE id=? AND space_id=?`, g.Ref.ID, g.Space).Scan(&sha, &expires, &revoked)
	if err != nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	s.chatSnapshotBlob(w, r, st, g.Space, sha, expires, revoked)
}

// listGuestChatSnapshots lets authors find and revoke links after closing the share dialog,
// including when the machine holding the original conversation is offline.
func (s *Service) listGuestChatSnapshots(w http.ResponseWriter, r *http.Request, userID string) {
	if !s.cfg.ChatGuestLinks || r.Method != http.MethodGet {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	now := store.Ms(s.now())
	rows, err := s.db.Query(r.Context(), `SELECT DISTINCT c.id,c.machine_id,c.title FROM chat_shares c JOIN resource_guest_links l ON l.kind='chat' AND l.id=c.id AND l.space_id=c.space_id WHERE c.owner_id=? AND c.revoked_at IS NULL AND c.expires_at>? AND l.revoked_at IS NULL AND l.expires_at>? ORDER BY c.title,c.id`, userID, now, now)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	type snapshot struct {
		ID      string `json:"id"`
		Machine string `json:"machine"`
		Title   string `json:"title"`
	}
	result := []snapshot{}
	for rows.Next() {
		var item snapshot
		if err := rows.Scan(&item.ID, &item.Machine, &item.Title); err != nil {
			s.fail(w, r, err)
			return
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, result)
}
