package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

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
//	GET <platform api>/history/conversations/<id>/entries/<entry>/output the whole output of a tool
//	GET <platform api>/history/conversations/<id>/rewinds/<n>/turns      the turns rewind n left behind
//	GET <platform api>/history/search?q=&limit=                          the person's messages that hold the words
//	GET <platform api>/history/sidebar                                   the person's projects and favorites, with its rev
//	PUT <platform api>/history/sidebar {baseRev, doc}                    writes it on baseRev; 409 with the current one
//
// The person is the session's; only chats of machines they may open and that they own are served.

var (
	historyPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/history/(conversations(/|$)|search$|sidebar$)`)
	turnID      = regexp.MustCompile(`^[A-Za-z0-9:#_.-]{1,200}$`)
)

type historyMachine struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

func (s *Service) serveHistory(w http.ResponseWriter, r *http.Request, p *auth.Principal, rest string) {
	if strings.HasSuffix(rest, "/history/sidebar") {
		s.serveSidebar(w, r, p)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	at := strings.Index(rest, "/history/conversations")
	search := strings.HasSuffix(rest, "/history/search")
	var parts []string
	if !search {
		for _, part := range strings.Split(strings.TrimPrefix(rest[at+len("/history/conversations"):], "/"), "/") {
			if part != "" {
				parts = append(parts, part)
			}
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
	case search:
		q := r.URL.Query()
		limit := 50
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 200 {
				httpx.WriteError(w, r, httpx.Invalid("limit", "1–200"))
				return
			}
			limit = n
		}
		in := ids
		if m := q.Get("machine"); m != "" && m != "local" {
			in = nil
			for _, id := range ids {
				if id == m {
					in = []string{id}
				}
			}
		}
		hits, err := history.Search(ctx, s.db, p.User.ID, q.Get("q"), in, limit)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"query": q.Get("q"), "hits": hits, "indexing": false})
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
	case len(parts) == 4 && parts[1] == "entries" && parts[3] == "output" && turnID.MatchString(parts[2]):
		page, err := history.EntryOutput(ctx, s.db, p.User.ID, parts[0], parts[2], ids)
		if errors.Is(err, history.ErrNotFound) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(page)
	case len(parts) == 4 && parts[1] == "rewinds" && parts[3] == "turns":
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 0 {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		page, err := history.Rewound(ctx, s.db, p.User.ID, parts[0], n, ids)
		if errors.Is(err, history.ErrNotFound) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"turns": page.Turns, "entries": page.Entries, "beforeTurnId": nil})
	default:
		httpx.WriteError(w, r, httpx.ErrNotFound)
	}
}

// serveSidebar is the person's sidebar document: one for every machine of theirs, kept with the copy, as the
// cluster's gateway keeps it.
func (s *Service) serveSidebar(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPut {
		httpx.WriteError(w, r, httpx.New(http.StatusMethodNotAllowed, "method_not_allowed", "Метод не поддерживается"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	list, _ := s.fleet(ctx, p.User.ID, true)
	if len(list) == 0 {
		if r.Method == http.MethodPut {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false, "rev": 0, "doc": nil})
		return
	}
	if r.Method != http.MethodPut {
		rev, doc, err := history.Sidebar(ctx, s.db, p.User.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "rev": rev, "doc": doc})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, history.SidebarMaxBytes+1024)
	var body struct {
		BaseRev *float64        `json:"baseRev"`
		Doc     json.RawMessage `json:"doc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, httpx.New(http.StatusBadRequest, "bad_request", "Некорректный запрос"))
		return
	}
	doc, ok := history.ParseSidebar(body.Doc)
	if !ok || body.BaseRev == nil || *body.BaseRev < 0 || *body.BaseRev != float64(int64(*body.BaseRev)) {
		httpx.WriteError(w, r, httpx.New(http.StatusBadRequest, "bad_request", "Некорректный запрос"))
		return
	}
	rev, err := history.WriteSidebar(ctx, s.db, p.User.ID, int64(*body.BaseRev), doc, time.Now().UnixMilli())
	if errors.Is(err, history.ErrSidebarChanged) {
		cur, curDoc, err := history.Sidebar(ctx, s.db, p.User.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{"conflict": true, "rev": cur, "doc": curDoc})
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rev": rev})
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
