package machines

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// ownerRequest is POST /api/machines/{machineId}/owner (docs/hub/machines.md, «Personal and administered machines»).
type ownerRequest struct {
	// UserID is the person the machine becomes the own one of: a member of its space.
	UserID string `json:"userId"`
	// History: "move" (the default) gives them the chats the machine sent for its previous owner too; "keep" leaves
	// those with the previous owner, and only what comes from now on is the new owner's.
	History string `json:"history"`
}

// assignOwner makes another person the machine's own one (#57): a machine an administrator added for an employee
// becomes the employee's, with its id, key, settings and chats kept. Only an owner of the machine's space may.
func (s *Service) assignOwner(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req ownerRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		return httpx.Invalid("userId", "the person the machine is given to")
	}
	if req.History == "" {
		req.History = "move"
	}
	if req.History != "move" && req.History != "keep" {
		return httpx.Invalid("history", "move or keep")
	}
	ctx := r.Context()
	m, err := s.Authorize(ctx, p.User.ID, r.PathValue("machineId"), true)
	if err != nil {
		return err
	}
	if _, err := s.spaces.Access(ctx, req.UserID, m.SpaceID); err != nil {
		return httpx.Invalid("userId", "a member of the machine's space")
	}
	prev := m.CreatedBy
	if prev != req.UserID {
		if err := s.db.S().SetMachineOwner(ctx, m.ID, req.UserID); err != nil {
			return err
		}
		if req.History == "move" && prev != "" {
			if _, err := history.MoveOwner(ctx, s.db, m.ID, prev, req.UserID); err != nil {
				return err
			}
		}
		// The link holds the machine as it was read when it dialed: it dials again and is read with its new owner.
		s.reg.Reconnect(m.ID)
		m.CreatedBy = req.UserID
		s.auth.Audit(ctx, p.User.ID, "machine_owner_changed", r, m.ID)
	}
	v, err := s.View(ctx, m)
	if err != nil {
		return err
	}
	s.Publish(ctx, m)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machine": v})
	return nil
}
