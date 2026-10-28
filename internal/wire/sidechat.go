package wire

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// sideChat serves POST /api/v2/conversations/{id}/side-chat {question, excerpts?, history?, model?}: a side question
// about the conversation, answered by a stream of server-sent frames the Legacy side chat has (data: {"type":"delta",
// "text"}, then {"type":"done","text","costUsd"?} or {"type":"error","code","message"}, and `: ping` comments). A
// question that cannot start is a JSON {error, code} with a status: 400 bad request, 404 no such conversation, 409
// no-session (the agent has no saved session yet) or busy (the previous question still runs), 422 unsupported (not a
// Claude conversation).
func (s *Server) sideChat(w http.ResponseWriter, r *http.Request, conv string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.SideChat == nil {
		refuse(w, RefuseNotFound)
		return
	}
	if !ValidConvID(conv) {
		refuse(w, RefuseBadRequest)
		return
	}
	switch err := s.SideChat(w, r, conv); {
	case err == nil:
	case s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	default:
		jsonlog.Log("warn", "v2: side chat failed", jsonlog.F{K: "conversation", V: conv}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	}
}
