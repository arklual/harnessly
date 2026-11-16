package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const historyReadPerMinute = 120

var errHistoryNoOwner = httpx.New(http.StatusForbidden, "history_no_owner", "У машины нет владельца, чью историю можно читать")

func (h *Hub) historyOwner(r *http.Request) (store.Machine, string, error) {
	m, err := h.Dialer.AdmitHistory(r)
	if err != nil {
		return store.Machine{}, "", err
	}
	if ok, _ := h.limiter.Allow("history:"+m.ID, historyReadPerMinute, time.Minute); !ok {
		return store.Machine{}, "", httpx.New(http.StatusTooManyRequests, "rate_limited", "Слишком много запросов")
	}
	if m.CreatedBy == "" || m.SpaceID == "" {
		return store.Machine{}, "", errHistoryNoOwner
	}
	if role, err := h.db.S().Role(r.Context(), m.SpaceID, m.CreatedBy); err != nil || role == "" {
		return store.Machine{}, "", errHistoryNoOwner
	}
	return m, m.CreatedBy, nil
}

func (h *Hub) historyMachines(r *http.Request, person string) ([]string, error) {
	spaces, err := h.db.S().SpacesByUser(r.Context(), person)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for _, space := range spaces {
		machines, err := h.db.S().MachinesByUser(r.Context(), space.ID, person)
		if err != nil {
			return nil, err
		}
		for _, m := range machines {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// agentHistoryRead is a browserless, machine-signed page read for the registered person's own central history.
func (h *Hub) agentHistoryRead(w http.ResponseWriter, r *http.Request) error {
	if h.History == nil {
		return httpx.ErrNotFound
	}
	_, person, err := h.historyOwner(r)
	if err != nil {
		return err
	}
	var in hubproto.HistoryRead
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in); err != nil || in.Conversation == "" {
		return httpx.Invalid("conversation", "Идентификатор чата")
	}
	if in.Limit < 1 || in.Limit > history.PageTurnsMax {
		return httpx.Invalid("limit", "1–100")
	}
	machines, err := h.historyMachines(r, person)
	if err != nil {
		return err
	}
	page, err := history.Read(r.Context(), h.db, person, in.Conversation, machines, in.Before, in.Limit)
	if err == history.ErrNotFound {
		var present int
		errPresent := h.db.QueryRow(r.Context(), `SELECT 1 FROM history_conversations WHERE conv_id = ? AND record IS NOT NULL AND deleted_at IS NULL LIMIT 1`, in.Conversation).Scan(&present)
		if errPresent == nil {
			return httpx.New(http.StatusForbidden, "history_forbidden", "Нет доступа к чату")
		}
		if errPresent == sql.ErrNoRows {
			return httpx.ErrNotFound
		}
		return errPresent
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, hubproto.HistoryPage{MachineID: page.MachineID, Conversation: page.Conversation, Turns: page.Turns, Entries: page.Entries, BeforeTurnID: page.BeforeTurnID, Complete: page.Complete})
	return nil
}
