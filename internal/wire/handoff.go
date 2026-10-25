package wire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// ErrDraftUnsupported: the conversation has nothing a draft can be made of yet (no transcript, no agent session), or
// its agent cannot write the note.
var ErrDraftUnsupported = errors.New("no draft can be made of this conversation")

// ErrDraftBusy: a note of the conversation is being written already; one is written at a time.
var ErrDraftBusy = errors.New("a note of this conversation is being written already")

// HandoffDraftBudget is how long a draft may take: a note is a whole answer of the agent.
const HandoffDraftBudget = 200 * time.Second

// handoffDraft serves POST /api/v2/conversations/{id}/handoff-draft {mode, provider?, model?}: the excerpt of the
// conversation's transcript at once, or the note its agent writes on a throwaway fork, which takes as long as the
// agent's answer does. Refused: not-found, unsupported (no transcript or session yet), conflict (a note of it is being
// written), timeout, internal (the agent did not write the note).
func (s *Server) handoffDraft(w http.ResponseWriter, r *http.Request, conv string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.HandoffDraft == nil {
		refuse(w, RefuseNotFound)
		return
	}
	var req HandoffDraftRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if !ValidConvID(conv) || dec.Decode(&req) != nil || req.Validate() != nil {
		refuse(w, RefuseBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), HandoffDraftBudget)
	defer cancel()
	out, err := s.HandoffDraft(ctx, conv, req)
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case errors.Is(err, ErrDraftUnsupported):
		refuse(w, RefuseUnsupported)
	case errors.Is(err, ErrDraftBusy):
		refuse(w, RefuseConflict)
	case err != nil && ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	case err != nil:
		agent.Log("warn", "v2: handoff draft failed", agent.F{K: "conversation", V: conv}, agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		s.writeChecked(w, r, out)
	}
}
