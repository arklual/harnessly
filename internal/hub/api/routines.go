package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A person's routines from their machine (docs/hub/routines.md): the machine proves itself with its key,
// and the routines are those of the person who registered it, while they are still a member of its space.

const routinesPerMinute = 120

var errRoutinesNoOwner = httpx.New(http.StatusForbidden, "routines_no_owner", "У машины нет владельца, чьи регулярные задачи можно менять")

// agentRoutines is POST /agent/routines.
func (h *Hub) agentRoutines(w http.ResponseWriter, r *http.Request) error {
	m, err := h.Dialer.AdmitRoutines(r)
	if err != nil {
		return err
	}
	if ok, _ := h.limiter.Allow("routines:"+m.ID, routinesPerMinute, time.Minute); !ok {
		return httpx.New(http.StatusTooManyRequests, "rate_limited", "Слишком много запросов")
	}
	if m.CreatedBy == "" || m.SpaceID == "" {
		return errRoutinesNoOwner
	}
	if role, err := h.db.S().Role(r.Context(), m.SpaceID, m.CreatedBy); err != nil || role == "" {
		return errRoutinesNoOwner
	}
	u, err := h.db.S().UserByID(r.Context(), m.CreatedBy)
	if err != nil {
		return err
	}
	var call hubproto.RoutinesCall
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&call); err != nil {
		return httpx.Invalid("body", "Некорректный запрос")
	}
	out, err := h.Automations.MachineCall(r.Context(), u.ID, u.Email, m.ID, call)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
