package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// The workspace's chat history from the hub's copy (docs/hub/history.md): what a cluster's gateway answers
// under <platform api>/history, read here instead of on the person's home machine, so the list and a chat open with
// every machine off.
//
//	GET <platform api>/history/conversations                            the list, which machines are on, the chats
//	GET <platform api>/history/conversations/<id>?before=&limit=        a page of a chat, oldest turn first
//	GET <platform api>/history/conversations/<id>/turns/<turn>/entries  the steps of one folded turn
//
// The person is the session's; only chats of machines they may open and that they own are served.

var (
	historyPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/history/conversations(/|$)`)
	turnID      = regexp.MustCompile(`^[A-Za-z0-9:#_.-]{1,200}$`)
)

type historyMachine struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

func (s *Service) serveHistory(w http.ResponseWriter, r *http.Request, p *auth.Principal, rest string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	at := strings.Index(rest, "/history/conversations")
	var parts []string
	for _, part := range strings.Split(strings.TrimPrefix(rest[at+len("/history/conversations"):], "/"), "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	list, err := s.fleet(r.Context(), p.User.ID, true)
	if err != nil {
		list = nil // no fleet for this person: the copy is empty, not an error
	}
	machines := make([]historyMachine, 0, len(list))
	ids := make([]string, 0, len(list))
	state := map[string]string{}
	for _, m := range list {
		st := "offline"
		if m.Online {
			st = "online"
		}
		machines = append(machines, historyMachine{ID: m.ID, Name: m.Name, State: st})
		ids = append(ids, m.ID)
		state[m.ID] = st
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	switch {
	case len(parts) == 0:
		chats, err := history.List(ctx, s.db, p.User.ID, ids)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		conversations := make([]json.RawMessage, 0, len(chats))
		for _, c := range chats {
			conversations = append(conversations, withMachine(c.Record, c.MachineID))
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": len(machines) > 0, "machines": machines, "conversations": conversations, "moved": []any{},
		})
	case len(parts) == 1:
		q := r.URL.Query()
		before := q.Get("before")
		limit := history.SnapshotTurns
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > history.PageTurnsMax {
				httpx.WriteError(w, r, httpx.Invalid("limit", "1–100"))
				return
			}
			limit = n
		}
		if before != "" && !turnID.MatchString(before) {
			httpx.WriteError(w, r, httpx.Invalid("before", "a turn id"))
			return
		}
		page, err := history.Read(ctx, s.db, p.User.ID, parts[0], ids, before, limit)
		if errors.Is(err, history.ErrNotFound) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"machine": state[page.MachineID], "machineId": page.MachineID,
			"conversation": withMachine(page.Conversation, page.MachineID),
			"turns":        page.Turns, "entries": page.Entries, "beforeTurnId": page.BeforeTurnID,
		})
	case len(parts) == 4 && parts[1] == "turns" && parts[3] == "entries" && turnID.MatchString(parts[2]):
		_, entries, err := history.TurnEntries(ctx, s.db, p.User.ID, parts[0], parts[2], ids)
		if errors.Is(err, history.ErrNotFound) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"turnId": parts[2], "entries": entries})
	default:
		httpx.WriteError(w, r, httpx.ErrNotFound)
	}
}

// withMachine is a chat's record with the machine it is on: the record carries the machine's own name for itself.
func withMachine(record json.RawMessage, machine string) json.RawMessage {
	var c map[string]json.RawMessage
	if json.Unmarshal(record, &c) != nil || c == nil {
		return record
	}
	c["machineId"], _ = json.Marshal(machine)
	out, err := json.Marshal(c)
	if err != nil {
		return record
	}
	return out
}
