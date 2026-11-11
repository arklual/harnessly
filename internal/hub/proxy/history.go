package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
//	GET <platform api>/history/conversations/<id>/work                   the work in git of the chat and its delegated chats
//	GET <platform api>/history/conversations/<id>/turns/<turn>/entries  the steps of one folded turn
//	GET <platform api>/history/conversations/<id>/entries/<entry>/output the whole output of a tool
//	GET <platform api>/history/conversations/<id>/rewinds/<n>/turns      the turns rewind n left behind
//	GET <platform api>/history/conversations/<id>/review-notes           the person's draft review remarks on the chat
//	PUT|DELETE <platform api>/history/conversations/<id>/review-notes/<note>  keep or forget one of them
//	GET <platform api>/history/search?q=&limit=                          the person's messages that hold the words
//	GET <platform api>/history/sidebar                                   the person's projects and favorites, with its rev
//	PUT <platform api>/history/sidebar {baseRev, doc}                    writes it on baseRev; 409 with the current one
//
// The person is the session's; only chats they own are served, and the list and the search hold only the person's own
// machines: a machine they reach as an administrator is named under "others" and never joins the list (#57).

var (
	historyPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/history/(conversations(/|$)|search$|sidebar$)`)
	turnID      = regexp.MustCompile(`^[A-Za-z0-9:#_.-]{1,200}$`)
)

type historyMachine struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// listChats reads the chats of the list; a variable so that a test can count the reads.
var listChats = history.List

// chatListTag is the entity tag of the list: the person, the version of their chats and the machines as the answer names
// them (a machine that goes on or off changes the answer). It is a weak tag: a proxy that compresses the answer keeps
// a weak tag as it is.
func chatListTag(userID, version string, machines, others []historyMachine) string {
	h := sha256.New()
	_, _ = io.WriteString(h, userID+"\x00"+version)
	for _, list := range [][]historyMachine{machines, others} {
		_, _ = io.WriteString(h, "\n")
		for _, m := range list {
			_, _ = io.WriteString(h, m.ID+"\x00"+m.Name+"\x00"+m.State+"\x00")
		}
	}
	return `W/"` + hex.EncodeToString(h.Sum(nil)[:12]) + `"`
}

// noneMatch is true when If-None-Match names the tag (compared weakly) or any tag.
func noneMatch(values []string, tag string) bool {
	want := strings.TrimPrefix(tag, "W/")
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "*" || strings.TrimPrefix(part, "W/") == want {
				return true
			}
		}
	}
	return false
}

// serveMachineHub answers, on a machine's own page (/m/<id>/…), the paths that belong to the hub and not to the runtime:
// the hub's copy of the chats (history) and where a chat went (chat-move). The page asks for them under the machine's
// base, as the platform's page asks its gateway; the runtime next to it has neither route and would answer 404, so a
// clean installation (hub and one machine, no gateway) read no chats there. It reports whether the request was answered.
func (s *Service) serveMachineHub(w http.ResponseWriter, r *http.Request, p *auth.Principal, id string) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, "/m/"+id)
	if !ok {
		return false
	}
	rest = strings.TrimPrefix(rest, workspaceBase)
	switch {
	case s.history && historyPath.MatchString(rest):
		s.serveHistory(w, r, p, rest)
		return true
	case rest == "/api/v2/chat-move" || strings.HasPrefix(rest, "/api/v2/chat-move/"):
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Path, u.RawPath = rest, ""
		clone.URL = &u
		s.serveMove(w, clone, p.User)
		return true
	}
	return false
}

func (s *Service) serveHistory(w http.ResponseWriter, r *http.Request, p *auth.Principal, rest string) {
	if strings.HasSuffix(rest, "/history/sidebar") {
		s.serveSidebar(w, r, p)
		return
	}
	if m := reviewNotesPath.FindStringSubmatch(rest); m != nil {
		s.serveReviewNotes(w, r, p, m[1], m[2])
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
	if errors.Is(err, errFleetOff) {
		list = nil // no fleet for this person: the copy is empty, not an error
	} else if err != nil {
		// A failure is not «off»: answered as one, the page switched the person's sidebar to a machine's own and back
		// with the next answer, and the favorites jumped between the two layouts.
		s.fail(w, r, err)
		return
	}
	// The copy is the person's own list: only their own machines are in it (machines, ids). The ones they reach as an
	// administrator are named apart (others), for their state and name; their chats are never listed or searched here.
	// A chat the person owns is still read by its address wherever it is (reachable).
	machines := make([]historyMachine, 0, len(list))
	others := make([]historyMachine, 0)
	ids := make([]string, 0, len(list))
	reachable := make([]string, 0, len(list))
	state := map[string]string{}
	for _, m := range list {
		st := "offline"
		if m.Online {
			st = "online"
		}
		state[m.ID] = st
		reachable = append(reachable, m.ID)
		if !m.Mine {
			others = append(others, historyMachine{ID: m.ID, Name: m.Name, State: st})
			continue
		}
		machines = append(machines, historyMachine{ID: m.ID, Name: m.Name, State: st})
		ids = append(ids, m.ID)
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
		// The page asks for the list again and again; one that has not changed is answered by its tag alone, before
		// any record is read. The tag is taken first: a write that lands after it only makes the next answer a full one.
		version, err := history.ListVersion(ctx, s.db, p.User.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		tag := chatListTag(p.User.ID, version, machines, others)
		w.Header().Set("ETag", tag)
		if noneMatch(r.Header.Values("If-None-Match"), tag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		chats, err := listChats(ctx, s.db, p.User.ID, ids)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		conversations := make([]json.RawMessage, 0, len(chats))
		for _, c := range chats {
			conversations = append(conversations, withMachine(c.Record, c.MachineID))
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": len(machines) > 0, "machines": machines, "others": others, "conversations": conversations, "moved": []any{},
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
		page, err := history.Read(ctx, s.db, p.User.ID, parts[0], reachable, before, limit)
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
	case len(parts) == 2 && parts[1] == "work":
		// The work of the chat and of the chats it delegated, on any of the person's machines (open or closed chats).
		entries, err := history.Work(ctx, s.db, p.User.ID, parts[0], reachable)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		named := map[string]historyMachine{}
		for _, m := range list {
			if st, ok := state[m.ID]; ok {
				named[m.ID] = historyMachine{ID: m.ID, Name: m.Name, State: st}
			}
		}
		used := []historyMachine{}
		seen := map[string]bool{}
		for _, e := range entries {
			if m, ok := named[e.MachineID]; ok && !seen[e.MachineID] {
				seen[e.MachineID] = true
				used = append(used, m)
			}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"conversationId": parts[0], "machines": used, "entries": entries})
	case len(parts) == 4 && parts[1] == "turns" && parts[3] == "entries" && turnID.MatchString(parts[2]):
		_, entries, err := history.TurnEntries(ctx, s.db, p.User.ID, parts[0], parts[2], reachable)
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
		page, err := history.EntryOutput(ctx, s.db, p.User.ID, parts[0], parts[2], reachable)
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
		page, err := history.Rewound(ctx, s.db, p.User.ID, parts[0], n, reachable)
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
	list, err := s.fleet(ctx, p.User.ID, true)
	if err != nil && !errors.Is(err, errFleetOff) {
		s.fail(w, r, err) // not «no sidebar»: see serveHistory (HRN-1167)
		return
	}
	if len(personal(list)) == 0 { // the projects and favorites are of the person's own machines' chats
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
