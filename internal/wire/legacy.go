package wire

import (
	"context"
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// LegacyTab is the answer of GET /api/v2/conversations/{id}/legacy: the Legacy tab a conversation runs in, which
// the right pane's tools of the current interface read (its timeline, diff, merge requests, subagents, spend).
type LegacyTab struct {
	TabID          string  `json:"tabId"`
	WorkspaceID    string  `json:"workspaceId"`
	SessionName    string  `json:"sessionName"`
	JsonlPath      *string `json:"jsonlPath"`
	AgentSessionID *string `json:"agentSessionId"`
	Cwd            string  `json:"cwd"`
}

func (l LegacyTab) Validate() error {
	return errors.Join(need("tabId", l.TabID), need("sessionName", l.SessionName), needPtr("jsonlPath", l.JsonlPath),
		needPtr("agentSessionId", l.AgentSessionID))
}

// legacy serves GET /api/v2/conversations/{id}/legacy; a conversation without a tmux session yet is not found.
func (s *Server) legacy(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	if s.Legacy == nil {
		refuse(w, RefuseNotFound)
		return
	}
	tab, err := s.Legacy(ctx, conv)
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil:
		jsonlog.Log("warn", "v2: legacy tab failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		s.writeChecked(w, r, tab)
	}
}
