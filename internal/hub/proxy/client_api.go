package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The client API (docs/hub/client-api.md): what a person's own app — a launcher, a menu-bar counter, a script
// — reads and writes with a personal token (internal/hub/auth/apitoken.go). It is a narrow, stable view over what the
// workspace page does through the v2 protocol: the person's chats as the hub keeps them, a chat's prompts and answers,
// a new message to a chat, a new chat. A message to a machine that is off waits in the hub like one from the page.
//
//	GET  /api/client/v1/me                         the person and their machines
//	GET  /api/client/v1/chats?filter=&machine=     the person's chats, newest first (filter: all | attention | working)
//	GET  /api/client/v1/chats/{id}?limit=          one chat with its last turns (prompt and answer of each)
//	POST /api/client/v1/chats/{id}/messages        {text, mode?}  → {delivery}
//	POST /api/client/v1/chats/{id}/stop            stops the agent's turn
//	POST /api/client/v1/chats/{id}/read            marks the chat read
//	POST /api/client/v1/chats                      {machineId, projectId|cwd, provider, text} → {delivery, id?, url?}
//	GET  /api/client/v1/projects?machine=          the folders the person's chats ran in, newest first
//	GET  /api/client/v1/machines/{id}/agents       the machine's agents and models (only while it is online)

const (
	clientAPIBase  = "/api/client/v1/"
	clientCall     = 20 * time.Second
	clientChatsMax = 200
	clientTurnsMax = 50
	clientBody     = maxOutboxText*4 + 4096
)

func (s *Service) mountClientAPI(mux *http.ServeMux) {
	h := func(scope string, f func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error) http.Handler {
		return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			p, err := s.auth.TokenPrincipal(r)
			if err != nil {
				return err
			}
			if p == nil {
				return httpx.New(http.StatusUnauthorized, "token_invalid", "Токен не подходит: подключите приложение заново")
			}
			if !p.Can(scope) {
				return httpx.New(http.StatusForbidden, "scope", "Токену это не разрешено")
			}
			if ok, wait := s.clientLim.Allow("client-api:"+p.Token.ID, 600, time.Minute); !ok {
				return httpx.RateLimited(ratelimit.RetrySeconds(wait))
			}
			w.Header().Set("Cache-Control", "no-store")
			return f(w, r, p)
		})
	}
	mux.Handle("GET "+clientAPIBase+"me", h(auth.ScopeRead, s.clientMe))
	mux.Handle("GET "+clientAPIBase+"chats", h(auth.ScopeRead, s.clientChats))
	mux.Handle("GET "+clientAPIBase+"chats/{id}", h(auth.ScopeRead, s.clientChat))
	mux.Handle("POST "+clientAPIBase+"chats/{id}/messages", h(auth.ScopePrompt, s.clientSend))
	mux.Handle("POST "+clientAPIBase+"chats/{id}/stop", h(auth.ScopePrompt, s.clientStop))
	mux.Handle("POST "+clientAPIBase+"chats/{id}/read", h(auth.ScopeRead, s.clientRead))
	mux.Handle("POST "+clientAPIBase+"chats", h(auth.ScopePrompt, s.clientCreate))
	mux.Handle("GET "+clientAPIBase+"projects", h(auth.ScopeRead, s.clientProjects))
	mux.Handle("GET "+clientAPIBase+"machines/{id}/agents", h(auth.ScopeRead, s.clientAgents))
}

// clientMachine is a machine of the person as the client API shows it.
type clientMachine struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	Status string `json:"status"`
	Org    string `json:"org"`
}

// clientFleet is the person's own machines (not those they reach as an administrator): the client API is theirs.
func (s *Service) clientFleet(ctx context.Context, userID string) ([]clientMachine, error) {
	list, err := s.fleet(ctx, userID, true)
	if errors.Is(err, errFleetOff) {
		return []clientMachine{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []clientMachine{}
	for _, m := range list {
		if m.Mine {
			out = append(out, clientMachine{ID: m.ID, Name: m.Name, Online: m.Online, Status: m.Status, Org: m.OrgName})
		}
	}
	return out, nil
}

func machineIDs(ms []clientMachine) []string {
	ids := make([]string, 0, len(ms))
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}

func (s *Service) clientMe(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ms, err := s.clientFleet(r.Context(), p.User.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":     map[string]any{"id": p.User.ID, "email": p.User.Email, "name": p.User.Name},
		"hub":      map[string]any{"url": strings.TrimRight(s.cfg.PublicURL, "/")},
		"scopes":   strings.Fields(p.Token.Scopes),
		"machines": ms,
	})
	return nil
}

// convRecord is the part of a chat's record (the v2 Conversation) the client API reads.
type convRecord struct {
	ID        string  `json:"id"`
	ProjectID *string `json:"projectId"`
	Cwd       string  `json:"cwd"`
	Provider  string  `json:"provider"`
	Model     *struct {
		ID string `json:"id"`
	} `json:"model"`
	Title          string `json:"title"`
	Pinned         bool   `json:"pinned"`
	CreatedAt      int64  `json:"createdAt"`
	LastActivityAt int64  `json:"lastActivityAt"`
	LastPromptAt   int64  `json:"lastPromptAt"`
	State          struct {
		K     string `json:"k"`
		Since int64  `json:"since"`
	} `json:"state"`
	SeenAt   int64 `json:"seenAt"`
	LastTurn *struct {
		ID         string  `json:"id"`
		Status     string  `json:"status"`
		StartedAt  int64   `json:"startedAt"`
		EndedAt    *int64  `json:"endedAt"`
		DurationMs *int64  `json:"durationMs"`
		ToolCount  int     `json:"toolCount"`
		Snippet    *string `json:"snippet"`
	} `json:"lastTurn"`
	Queue *struct {
		Count int `json:"count"`
	} `json:"queue"`
	Gate *struct {
		Kind string `json:"kind"`
	} `json:"gate"`
	Recap *struct {
		Text string `json:"text"`
		At   int64  `json:"at"`
	} `json:"recap"`
	BgAgents int    `json:"bgAgents"`
	ParentID string `json:"parentId"`
	Test     bool   `json:"test"`
	From     *struct {
		Label string `json:"label"`
	} `json:"from"`
}

// ClientChat is a chat as the client API lists it.
type ClientChat struct {
	ID          string  `json:"id"`
	MachineID   string  `json:"machineId"`
	MachineName string  `json:"machineName"`
	Online      bool    `json:"online"`
	Title       string  `json:"title"`
	Project     string  `json:"project"`
	ProjectID   *string `json:"projectId"`
	Cwd         string  `json:"cwd"`
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	// State is the agent's state (working, waiting-question, waiting-permission, ready, idle, stopped, …).
	State string `json:"state"`
	// Attention is why the chat waits for the person: "question", "permission", "plan", "unread" or "".
	Attention    string  `json:"attention"`
	Unread       bool    `json:"unread"`
	Pinned       bool    `json:"pinned"`
	Recap        *string `json:"recap"`
	Snippet      *string `json:"snippet"`
	LastTurn     *string `json:"lastTurnStatus"`
	Queue        int     `json:"queue"`
	Background   int     `json:"background"`
	From         string  `json:"from,omitempty"`
	Delegated    bool    `json:"delegated"`
	CreatedAt    int64   `json:"createdAt"`
	LastPromptAt int64   `json:"lastPromptAt"`
	ActivityAt   int64   `json:"activityAt"`
	URL          string  `json:"url"`
}

func (s *Service) chatURL(machine, conv string) string {
	return strings.TrimRight(s.cfg.PublicURL, "/") + workspaceBase + "/?c=" + url.QueryEscape("agent:"+machine+":"+conv)
}

func projectName(cwd string) string {
	cwd = strings.TrimRight(cwd, "/")
	if cwd == "" {
		return ""
	}
	return path.Base(cwd)
}

func (s *Service) clientChatOf(rec convRecord, m clientMachine) ClientChat {
	c := ClientChat{
		ID: rec.ID, MachineID: m.ID, MachineName: m.Name, Online: m.Online, Title: rec.Title, Project: projectName(rec.Cwd),
		ProjectID: rec.ProjectID, Cwd: rec.Cwd, Provider: rec.Provider, State: rec.State.K, Pinned: rec.Pinned,
		Background: rec.BgAgents, Delegated: rec.ParentID != "", CreatedAt: rec.CreatedAt, LastPromptAt: rec.LastPromptAt,
		ActivityAt: rec.LastActivityAt, URL: s.chatURL(m.ID, rec.ID),
	}
	if rec.Model != nil {
		c.Model = rec.Model.ID
	}
	if rec.Recap != nil && rec.Recap.Text != "" {
		c.Recap = &rec.Recap.Text
	}
	if rec.LastTurn != nil {
		c.Snippet, c.LastTurn = rec.LastTurn.Snippet, &rec.LastTurn.Status
		c.Unread = rec.LastTurn.EndedAt != nil && *rec.LastTurn.EndedAt > rec.SeenAt
	}
	if rec.Queue != nil {
		c.Queue = rec.Queue.Count
	}
	if rec.From != nil {
		c.From = rec.From.Label
	}
	switch {
	case rec.Gate != nil && rec.Gate.Kind != "":
		c.Attention = rec.Gate.Kind
	case rec.State.K == "waiting-question":
		c.Attention = "question"
	case rec.State.K == "waiting-permission":
		c.Attention = "permission"
	case c.Unread:
		c.Attention = "unread"
	}
	return c
}

// working is a chat whose agent is at it now.
func (c ClientChat) working() bool {
	return c.State == "starting" || c.State == "working" || c.Background > 0
}

func (s *Service) clientChatList(ctx context.Context, userID string) ([]ClientChat, []clientMachine, error) {
	ms, err := s.clientFleet(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]clientMachine{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	chats, err := history.List(ctx, s.db, userID, machineIDs(ms))
	if err != nil {
		return nil, nil, err
	}
	out := make([]ClientChat, 0, len(chats))
	for _, ch := range chats {
		var rec convRecord
		if json.Unmarshal(ch.Record, &rec) != nil || rec.ID == "" || rec.Test {
			continue
		}
		out = append(out, s.clientChatOf(rec, byID[ch.MachineID]))
	}
	return out, ms, nil
}

func (s *Service) clientChats(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > clientChatsMax {
			return httpx.Invalid("limit", "1–200")
		}
		limit = n
	}
	filter := q.Get("filter")
	switch filter {
	case "", "all", "attention", "working":
	default:
		return httpx.Invalid("filter", "all, attention или working")
	}
	all, _, err := s.clientChatList(r.Context(), p.User.ID)
	if err != nil {
		return err
	}
	out := []ClientChat{}
	attention, working := 0, 0
	for _, c := range all {
		if c.Attention != "" {
			attention++
		}
		if c.working() {
			working++
		}
		if m := q.Get("machine"); m != "" && c.MachineID != m {
			continue
		}
		if (filter == "attention" && c.Attention == "") || (filter == "working" && !c.working()) {
			continue
		}
		if len(out) < limit {
			out = append(out, c)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"chats":  out,
		"counts": map[string]int{"all": len(all), "attention": attention, "working": working},
	})
	return nil
}

// clientTurn is one turn of a chat: the person's prompt and the agent's last answer.
type clientTurn struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   *int64 `json:"endedAt"`
	ToolCount int    `json:"toolCount"`
	Prompt    string `json:"prompt"`
	Answer    string `json:"answer"`
}

// findChat is the person's chat with the id on one of their own machines.
func (s *Service) findChat(ctx context.Context, userID, conv string, limit int) (history.Page, clientMachine, error) {
	ms, err := s.clientFleet(ctx, userID)
	if err != nil {
		return history.Page{}, clientMachine{}, err
	}
	page, err := history.Read(ctx, s.db, userID, conv, machineIDs(ms), "", limit)
	if errors.Is(err, history.ErrNotFound) {
		return history.Page{}, clientMachine{}, httpx.ErrNotFound
	}
	if err != nil {
		return history.Page{}, clientMachine{}, err
	}
	for _, m := range ms {
		if m.ID == page.MachineID {
			return page, m, nil
		}
	}
	return history.Page{}, clientMachine{}, httpx.ErrNotFound
}

func (s *Service) clientChat(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > clientTurnsMax {
			return httpx.Invalid("limit", "1–50")
		}
		limit = n
	}
	page, m, err := s.findChat(r.Context(), p.User.ID, r.PathValue("id"), limit)
	if err != nil {
		return err
	}
	var rec convRecord
	if err := json.Unmarshal(page.Conversation, &rec); err != nil {
		return err
	}
	type entry struct {
		ID   string `json:"id"`
		T    string `json:"t"`
		Text string `json:"text"`
		MD   string `json:"md"`
	}
	entries := map[string]entry{}
	for _, raw := range page.Entries {
		var e entry
		if json.Unmarshal(raw, &e) == nil && e.ID != "" {
			entries[e.ID] = e
		}
	}
	turns := []clientTurn{}
	for _, raw := range page.Turns {
		var t struct {
			ID            string  `json:"id"`
			Status        string  `json:"status"`
			StartedAt     int64   `json:"startedAt"`
			EndedAt       *int64  `json:"endedAt"`
			ToolCount     int     `json:"toolCount"`
			AnswerEntryID *string `json:"answerEntryId"`
		}
		if json.Unmarshal(raw, &t) != nil {
			continue
		}
		ct := clientTurn{ID: t.ID, Status: t.Status, StartedAt: t.StartedAt, EndedAt: t.EndedAt, ToolCount: t.ToolCount}
		if e, ok := entries[t.ID]; ok && e.T == "user" {
			ct.Prompt = e.Text
		}
		if t.AnswerEntryID != nil {
			if e, ok := entries[*t.AnswerEntryID]; ok {
				ct.Answer = e.MD
			}
		}
		turns = append(turns, ct)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"chat": s.clientChatOf(rec, m), "turns": turns, "complete": page.Complete || page.BeforeTurnID == nil,
	})
	return nil
}

// newOpID is the id of one command the hub sends for the app; the machine takes one id once.
func newOpID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "c" + hex.EncodeToString(b[:])
}

// command sends a v2 command to the person's machine as the person and returns the machine's answer; ok is false
// when the machine did not answer.
func (s *Service) command(ctx context.Context, u store.User, machine, op string, body any) (status int, answer []byte, ok bool, err error) {
	m, err := s.admit(ctx, u.ID, machine)
	if err == nil {
		err = s.trustedOnHub(ctx, m)
	}
	if err != nil {
		return 0, nil, false, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, false, err
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), clientCall)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, "/api/v2/cmd/"+op, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, false, err
	}
	if pu, err := url.Parse(s.cfg.PublicURL); err == nil {
		req.Host = pu.Host
	}
	req.RemoteAddr = "127.0.0.1:0"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Harnessly-Device", "hub-client")
	rec := &pageRecorder{header: http.Header{}}
	s.forward(rec, req, u, m)
	if !rec.wrote || cctx.Err() != nil {
		return 0, nil, false, nil
	}
	return rec.status, rec.body.Bytes(), true, nil
}

// refusal is the code of a refused command, "" when the machine took it.
func refusal(status int, body []byte) string {
	var ans struct {
		R    string `json:"r"`
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &ans)
	if status >= 200 && status < 300 && (ans.R == "accepted" || ans.R == "queued") {
		return ""
	}
	if ans.Code != "" {
		return ans.Code
	}
	return "refused"
}

func readText(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, clientBody)
	return httpx.Decode(r, v)
}

func validText(text string) bool {
	return strings.TrimSpace(text) != "" && utf8.RuneCountInString(text) <= maxOutboxText
}

func (s *Service) clientSend(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in struct {
		Text     string `json:"text"`
		Mode     string `json:"mode"`
		ClientID string `json:"clientId"`
	}
	if err := readText(w, r, &in); err != nil {
		return err
	}
	if !validText(in.Text) {
		return httpx.Invalid("text", "Пустое или слишком длинное сообщение")
	}
	switch in.Mode {
	case "":
		in.Mode = "queue"
	case "queue", "now":
	default:
		return httpx.Invalid("mode", "queue или now")
	}
	if in.ClientID == "" {
		in.ClientID = newOpID()
	} else if !outboxClient.MatchString(in.ClientID) {
		return httpx.Invalid("clientId", "[A-Za-z0-9_-]{1,64}")
	}
	ctx := r.Context()
	page, m, err := s.findChat(ctx, p.User.ID, r.PathValue("id"), 1)
	if err != nil {
		return err
	}
	var rec convRecord
	if err := json.Unmarshal(page.Conversation, &rec); err != nil {
		return err
	}
	send := map[string]any{
		"opId": in.ClientID, "clientId": in.ClientID, "conversationId": rec.ID, "text": in.Text,
		"attachmentIds": []string{}, "mode": in.Mode, "kind": "prompt",
	}
	out := map[string]any{"chatId": rec.ID, "machineId": m.ID, "clientId": in.ClientID, "url": s.chatURL(m.ID, rec.ID)}
	if m.Online {
		status, body, ok, err := s.command(ctx, p.User, m.ID, "send", send)
		if err != nil {
			return err
		}
		if ok {
			state, why := outboxSettle(status, body)
			if state != "queued" {
				out["delivery"] = state
				if why != "" {
					out["error"] = why
				}
				httpx.WriteJSON(w, http.StatusOK, out)
				return nil
			}
		}
	}
	raw, err := json.Marshal(send)
	if err != nil {
		return err
	}
	it, err := s.outboxAdd(ctx, p.User.ID, m.ID, rec.ID, in.Text, in.ClientID, raw)
	if errors.Is(err, errOutboxFull) {
		return httpx.Conflict("queue_full", "Слишком много сообщений ждут машину")
	}
	if err != nil {
		return err
	}
	out["delivery"] = "queued"
	out["queueId"] = it.ID
	httpx.WriteJSON(w, http.StatusAccepted, out)
	return nil
}

// simpleCommand sends a command about one chat that only makes sense while its machine is online.
func (s *Service) simpleCommand(w http.ResponseWriter, r *http.Request, p *auth.Principal, op string, body map[string]any) error {
	ctx := r.Context()
	page, m, err := s.findChat(ctx, p.User.ID, r.PathValue("id"), 1)
	if err != nil {
		return err
	}
	if !m.Online {
		return httpx.ErrMachineOffline
	}
	var rec convRecord
	if err := json.Unmarshal(page.Conversation, &rec); err != nil {
		return err
	}
	body["opId"], body["conversationId"] = newOpID(), rec.ID
	status, answer, ok, err := s.command(ctx, p.User, m.ID, op, body)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrMachineTimeout
	}
	if code := refusal(status, answer); code != "" {
		return httpx.New(http.StatusConflict, "refused", "Машина не выполнила команду ("+code+")")
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) clientStop(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	return s.simpleCommand(w, r, p, "interrupt", map[string]any{"pauseQueue": false})
}

func (s *Service) clientRead(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	return s.simpleCommand(w, r, p, "seen", map[string]any{"upTo": store.Ms(s.now())})
}

func (s *Service) clientCreate(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in struct {
		MachineID string  `json:"machineId"`
		ProjectID string  `json:"projectId"`
		Cwd       string  `json:"cwd"`
		Provider  string  `json:"provider"`
		Model     *string `json:"model"`
		Text      string  `json:"text"`
		Worktree  bool    `json:"worktree"`
		ClientID  string  `json:"clientId"`
	}
	if err := readText(w, r, &in); err != nil {
		return err
	}
	if !validText(in.Text) {
		return httpx.Invalid("text", "Пустое или слишком длинное сообщение")
	}
	if in.Provider == "" {
		in.Provider = "claude"
	}
	if !outboxWord.MatchString(in.Provider) {
		return httpx.Invalid("provider", "claude, codex, …")
	}
	if (in.ProjectID == "") == (in.Cwd == "") || len(in.ProjectID+in.Cwd) > maxOutboxPlace {
		return httpx.Invalid("projectId", "Нужен projectId или cwd")
	}
	if in.ClientID == "" {
		in.ClientID = newOpID()
	} else if !outboxClient.MatchString(in.ClientID) {
		return httpx.Invalid("clientId", "[A-Za-z0-9_-]{1,64}")
	}
	ctx := r.Context()
	ms, err := s.clientFleet(ctx, p.User.ID)
	if err != nil {
		return err
	}
	var m *clientMachine
	for i := range ms {
		if ms[i].ID == in.MachineID {
			m = &ms[i]
		}
	}
	if m == nil {
		return httpx.ErrNotFound
	}
	var model any
	if in.Model != nil && strings.TrimSpace(*in.Model) != "" {
		model = map[string]any{"id": strings.TrimSpace(*in.Model), "effort": nil}
	}
	create := map[string]any{
		"opId": in.ClientID, "provider": in.Provider, "model": model,
		"first": map[string]any{"clientId": in.ClientID, "text": in.Text, "attachmentIds": []string{}},
	}
	if in.ProjectID != "" {
		create["projectId"] = in.ProjectID
	} else {
		create["cwd"] = in.Cwd
	}
	if in.Worktree {
		create["worktree"] = true
	}
	out := map[string]any{"machineId": m.ID, "clientId": in.ClientID}
	if m.Online {
		status, body, ok, err := s.command(ctx, p.User, m.ID, "conversation.create", create)
		if err != nil {
			return err
		}
		if ok {
			state, why, id := outboxChatSettle(status, body)
			if state != "queued" {
				out["delivery"] = state
				if why != "" {
					out["error"] = why
				}
				if id != "" {
					out["id"], out["url"] = id, s.chatURL(m.ID, id)
				}
				httpx.WriteJSON(w, http.StatusOK, out)
				return nil
			}
		}
	}
	raw, err := json.Marshal(create)
	if err != nil {
		return err
	}
	if _, _, _, ok := readCreate(raw); !ok {
		return httpx.Invalid("body", "Чат с такими полями не создать")
	}
	it, err := s.outboxChatAdd(ctx, p.User.ID, m.ID, in.ClientID, strings.TrimSpace(in.Text), raw)
	if errors.Is(err, errOutboxFull) {
		return httpx.Conflict("queue_full", "Слишком много новых чатов ждут машину")
	}
	if err != nil {
		return err
	}
	out["delivery"] = "queued"
	out["queueId"] = it.ID
	httpx.WriteJSON(w, http.StatusAccepted, out)
	return nil
}

// clientProject is a folder the person's chats ran in.
type clientProject struct {
	MachineID   string   `json:"machineId"`
	MachineName string   `json:"machineName"`
	Online      bool     `json:"online"`
	ProjectID   *string  `json:"projectId"`
	Cwd         string   `json:"cwd"`
	Name        string   `json:"name"`
	Providers   []string `json:"providers"`
	LastUsedAt  int64    `json:"lastUsedAt"`
	Chats       int      `json:"chats"`
}

func (s *Service) clientProjects(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	all, _, err := s.clientChatList(r.Context(), p.User.ID)
	if err != nil {
		return err
	}
	only := r.URL.Query().Get("machine")
	idx := map[string]int{}
	out := []clientProject{}
	for _, c := range all {
		if c.Delegated || c.Cwd == "" || (only != "" && c.MachineID != only) {
			continue
		}
		key := c.MachineID + "\x00" + c.Cwd
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, clientProject{MachineID: c.MachineID, MachineName: c.MachineName, Online: c.Online,
				ProjectID: c.ProjectID, Cwd: c.Cwd, Name: c.Project, Providers: []string{}})
		}
		pr := &out[i]
		pr.Chats++
		pr.LastUsedAt = max(pr.LastUsedAt, c.LastPromptAt)
		if pr.ProjectID == nil {
			pr.ProjectID = c.ProjectID
		}
		has := false
		for _, v := range pr.Providers {
			has = has || v == c.Provider
		}
		if !has && c.Provider != "" {
			pr.Providers = append(pr.Providers, c.Provider)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUsedAt > out[j].LastUsedAt })
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": out})
	return nil
}

func (s *Service) clientAgents(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	ms, err := s.clientFleet(ctx, p.User.ID)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	var m *clientMachine
	for i := range ms {
		if ms[i].ID == id {
			m = &ms[i]
		}
	}
	if m == nil {
		return httpx.ErrNotFound
	}
	if !m.Online {
		return httpx.ErrMachineOffline
	}
	sm, err := s.admit(ctx, p.User.ID, id)
	if err == nil {
		err = s.trustedOnHub(ctx, sm)
	}
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), clientCall)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "/api/v2/machines", nil)
	if err != nil {
		return err
	}
	if pu, err := url.Parse(s.cfg.PublicURL); err == nil {
		req.Host = pu.Host
	}
	req.RemoteAddr = "127.0.0.1:0"
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Harnessly-Device", "hub-client")
	rec := &pageRecorder{header: http.Header{}}
	s.forward(rec, req, p.User, sm)
	if !rec.wrote || cctx.Err() != nil {
		return httpx.ErrMachineTimeout
	}
	if rec.status != http.StatusOK {
		return httpx.New(http.StatusBadGateway, "machine_error", "Машина ответила "+http.StatusText(rec.status))
	}
	var list struct {
		Machines []struct {
			ID     string          `json:"id"`
			Agents json.RawMessage `json:"agents"`
		} `json:"machines"`
	}
	var arr []struct {
		ID     string          `json:"id"`
		Agents json.RawMessage `json:"agents"`
	}
	body := rec.body.Bytes()
	if json.Unmarshal(body, &arr) != nil {
		if err := json.Unmarshal(body, &list); err != nil {
			return httpx.New(http.StatusBadGateway, "machine_error", "Машина ответила непонятно")
		}
		for _, x := range list.Machines {
			arr = append(arr, struct {
				ID     string          `json:"id"`
				Agents json.RawMessage `json:"agents"`
			}{x.ID, x.Agents})
		}
	}
	agents := json.RawMessage("[]")
	for _, x := range arr {
		if (x.ID == id || len(arr) == 1) && len(x.Agents) > 0 {
			agents = x.Agents
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"machineId": id, "agents": agents})
	return nil
}
