package proxy

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// New chats a person started while none of their machines answered: the first message waits in the hub
// (offline_chats), the page shows it as a chat that waits for its machine on every device of the person's, and the
// hub makes the chat on the machine, with that message, once the machine is back, whether a page is open or not. The
// API is under the workspace's platform API, beside the message queue of an existing chat (outbox.go):
//
//	POST   <platform api>/offline-chats?machine=<id>   {create}  → 201 {item}
//	GET    <platform api>/offline-chats                          → {items} (the person's, every machine)
//	DELETE <platform api>/offline-chats?id=<item id>             → {item}
//
// `create` is the page's own v2 conversation.create command; the hub gives the machine exactly that body, and the
// machine answers one opId once, so a chat is never made twice.

var outboxChatsPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/offline-chats$`)

const (
	// maxOutboxChats bounds the new chats one person keeps waiting.
	maxOutboxChats = 20
	maxOutboxPlace = 4096
)

var outboxProfile = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

type outboxChat struct {
	ID        string  `json:"id"`
	MachineID string  `json:"machineId"`
	ClientID  string  `json:"clientId"`
	Text      string  `json:"text"`
	State     string  `json:"state"`
	Error     *string `json:"error"`
	ConvID    *string `json:"convId"`
	CreatedAt int64   `json:"createdAt"`
}

const outboxChatCols = `id, machine_id, client_id, text, state, error, conv_id, created_at`

func scanOutboxChat(sc interface{ Scan(...any) error }) (outboxChat, error) {
	var it outboxChat
	var errText, conv sql.NullString
	if err := sc.Scan(&it.ID, &it.MachineID, &it.ClientID, &it.Text, &it.State, &errText, &conv, &it.CreatedAt); err != nil {
		return it, err
	}
	if errText.Valid {
		it.Error = &errText.String
	}
	if conv.Valid {
		it.ConvID = &conv.String
	}
	return it, nil
}

// readCreate is the page's conversation.create as the hub may keep it: one chat in a folder or a project, its first
// message plain (nothing attached: attachments are HRN-1037's), the opId that message's clientId. Anything else is
// refused rather than half-kept.
func readCreate(raw json.RawMessage) (json.RawMessage, string, string, bool) {
	var v map[string]json.RawMessage
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil, "", "", false
	}
	str := func(m map[string]json.RawMessage, k string) (string, bool) {
		var s string
		return s, m[k] != nil && json.Unmarshal(m[k], &s) == nil
	}
	for k := range v {
		switch k {
		case "opId", "projectId", "cwd", "provider", "model", "profile", "first", "worktree":
		default:
			return nil, "", "", false
		}
	}
	opID, ok := str(v, "opId")
	if !ok || !outboxClient.MatchString(opID) {
		return nil, "", "", false
	}
	project, hasProject := str(v, "projectId")
	cwd, hasCwd := str(v, "cwd")
	place := project + cwd
	if hasProject == hasCwd || strings.TrimSpace(place) == "" || len(place) > maxOutboxPlace {
		return nil, "", "", false
	}
	if provider, ok := str(v, "provider"); !ok || !outboxWord.MatchString(provider) {
		return nil, "", "", false
	}
	if p, has := v["profile"]; has {
		var profile string
		if json.Unmarshal(p, &profile) != nil || !outboxProfile.MatchString(profile) {
			return nil, "", "", false
		}
	}
	var model map[string]json.RawMessage
	if v["model"] == nil || json.Unmarshal(v["model"], &model) != nil {
		return nil, "", "", false
	}
	if w, has := v["worktree"]; has {
		var b bool
		if json.Unmarshal(w, &b) != nil {
			return nil, "", "", false
		}
	}
	var first map[string]json.RawMessage
	if json.Unmarshal(v["first"], &first) != nil || first == nil {
		return nil, "", "", false
	}
	for k := range first {
		switch k {
		case "clientId", "text", "attachmentIds", "refs":
		default:
			return nil, "", "", false
		}
	}
	clientID, ok1 := str(first, "clientId")
	text, ok2 := str(first, "text")
	if !ok1 || !ok2 || clientID != opID || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > maxOutboxText {
		return nil, "", "", false
	}
	var att []json.RawMessage
	if json.Unmarshal(first["attachmentIds"], &att) != nil || first["attachmentIds"] == nil || len(att) > 0 {
		return nil, "", "", false
	}
	if r, has := first["refs"]; has {
		var refs []json.RawMessage
		if json.Unmarshal(r, &refs) != nil || len(refs) > maxOutboxRefs {
			return nil, "", "", false
		}
	}
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		return nil, "", "", false
	}
	return out.Bytes(), clientID, strings.TrimSpace(text), true
}

// serveOutboxChats answers <platform api>/offline-chats for a signed-in person.
func (s *Service) serveOutboxChats(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		items, err := s.outboxChatList(ctx, p.User.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		m, ok := s.outboxChatMachine(w, r, p)
		if !ok {
			return
		}
		var body struct {
			Create json.RawMessage `json:"create"`
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxOutboxBody))
		if err != nil || json.Unmarshal(raw, &body) != nil {
			outboxError(w, http.StatusBadRequest, "Нужна команда создания чата", "")
			return
		}
		create, clientID, text, ok := readCreate(body.Create)
		if !ok {
			outboxError(w, http.StatusBadRequest, "Команда создания чата не та", "bad-create")
			return
		}
		item, err := s.outboxChatAdd(ctx, p.User.ID, m.ID, clientID, text, create)
		if errors.Is(err, errOutboxFull) {
			outboxError(w, http.StatusConflict, "Слишком много чатов ждут машину", "")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"item": item})
	case http.MethodDelete:
		item, err := s.outboxChatRemove(ctx, p.User.ID, r.URL.Query().Get("id"))
		if errors.Is(err, store.ErrNotFound) {
			outboxError(w, http.StatusConflict, "Чат уже создаётся или создан", "")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"item": item})
	default:
		outboxError(w, http.StatusMethodNotAllowed, "method", "")
	}
}

// outboxChatMachine is the machine a new chat waits for: the named one, or home, if the person may open it here.
func (s *Service) outboxChatMachine(w http.ResponseWriter, r *http.Request, p *auth.Principal) (store.Machine, bool) {
	id := r.URL.Query().Get("machine")
	if id == "" || id == "local" {
		if id = s.home(r.Context(), p.User.ID, true); id == "" {
			s.fail(w, r, s.workspaceEmpty(r.Context(), p.User.ID))
			return store.Machine{}, false
		}
	}
	if !machineID.MatchString(id) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return store.Machine{}, false
	}
	m, err := s.admit(r.Context(), p.User.ID, id)
	if err == nil {
		err = s.trustedOnHub(r.Context(), m)
	}
	if err != nil {
		s.fail(w, r, err)
		return store.Machine{}, false
	}
	sp, err := s.db.S().SpaceByID(r.Context(), m.SpaceID)
	if err != nil || !s.fleetOn(r.Context(), sp) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return store.Machine{}, false
	}
	return m, true
}

func (s *Service) outboxChatList(ctx context.Context, userID string) ([]outboxChat, error) {
	rows, err := s.db.Query(ctx, `SELECT `+outboxChatCols+` FROM offline_chats
		WHERE user_id = ? AND (state <> 'delivered' OR changed_at > ?) ORDER BY created_at, id`,
		userID, store.Ms(s.now().Add(-outboxShown)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []outboxChat{}
	for rows.Next() {
		it, err := scanOutboxChat(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Service) outboxChatByClient(ctx context.Context, userID, client string) (outboxChat, error) {
	return scanOutboxChat(s.db.QueryRow(ctx, `SELECT `+outboxChatCols+` FROM offline_chats WHERE user_id = ? AND client_id = ?`, userID, client))
}

// outboxChatAdd keeps a new chat once per clientId (the page sends it again after F5), at most maxOutboxChats waiting.
func (s *Service) outboxChatAdd(ctx context.Context, userID, machine, client, text string, create json.RawMessage) (outboxChat, error) {
	if it, err := s.outboxChatByClient(ctx, userID, client); err == nil {
		return it, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return outboxChat{}, err
	}
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM offline_chats WHERE user_id = ? AND state IN ('queued', 'sending', 'failed')`,
		userID).Scan(&n); err != nil {
		return outboxChat{}, err
	}
	if n >= maxOutboxChats {
		return outboxChat{}, errOutboxFull
	}
	now := store.Ms(s.now())
	if _, err := s.db.Exec(ctx, `INSERT INTO offline_chats (id, user_id, machine_id, client_id, text, create_cmd, state, created_at, changed_at)
		VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?) ON CONFLICT (user_id, client_id) DO NOTHING`,
		store.NewID(), userID, machine, client, text, string(create), now, now); err != nil {
		return outboxChat{}, err
	}
	return s.outboxChatByClient(ctx, userID, client)
}

// outboxChatRemove takes back a chat still waiting, or one that failed: one being made may already be on the machine.
func (s *Service) outboxChatRemove(ctx context.Context, userID, id string) (outboxChat, error) {
	it, err := scanOutboxChat(s.db.QueryRow(ctx, `SELECT `+outboxChatCols+` FROM offline_chats WHERE id = ? AND user_id = ? AND state IN ('queued', 'failed')`,
		id, userID))
	if err != nil {
		return outboxChat{}, err
	}
	res, err := s.db.Exec(ctx, `DELETE FROM offline_chats WHERE id = ? AND state IN ('queued', 'failed')`, it.ID)
	if err != nil {
		return outboxChat{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return outboxChat{}, store.ErrNotFound
	}
	return it, nil
}

// ---- delivery ----

// RunOutboxChats makes the waiting chats on machines that are back, every OutboxEvery, until ctx ends.
func (s *Service) RunOutboxChats(ctx context.Context) {
	t := time.NewTicker(OutboxEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.DeliverOutboxChats(ctx)
	}
}

type outboxChatDue struct {
	id, userID, machineID string
	create                []byte
}

// DeliverOutboxChats is one pass: the waiting chats of connected machines, oldest first, one at a time, a machine that
// did not take one left for the next pass. It returns how many chats were made.
func (s *Service) DeliverOutboxChats(ctx context.Context) int {
	now := s.now()
	if _, err := s.db.Exec(ctx, `UPDATE offline_chats SET state = 'queued', changed_at = ? WHERE state = 'sending' AND changed_at < ?`,
		store.Ms(now), store.Ms(now.Add(-outboxStuck))); err != nil {
		if ctx.Err() == nil {
			s.log.Warn("offline chats: stuck chats not released", "err", err)
		}
		return 0
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM offline_chats WHERE state IN ('delivered', 'failed') AND changed_at < ?`,
		store.Ms(now.Add(-outboxKept))); err != nil && ctx.Err() == nil {
		s.log.Warn("offline chats: old chats not removed", "err", err)
	}
	ctx, cancel := context.WithTimeout(ctx, outboxPassMax)
	defer cancel()
	// Per connected machine: chats of machines that stay off never fill a pass and hold up those of a machine that is back.
	due, err := s.outboxChatsDue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("offline chats: the queue is unreadable", "err", err)
		}
		return 0
	}
	down := map[string]bool{}
	made := 0
	for _, d := range due {
		if ctx.Err() != nil {
			break
		}
		if down[d.machineID] || s.reg.Get(d.machineID) == nil {
			down[d.machineID] = true
			continue
		}
		res, err := s.db.Exec(ctx, `UPDATE offline_chats SET state = 'sending', changed_at = ? WHERE id = ? AND state = 'queued'`, store.Ms(s.now()), d.id)
		if err != nil {
			break
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // another hub of the pair took it
		}
		state, msg, conv := s.outboxChatHandOver(ctx, d)
		if state == "delivered" {
			made++
		}
		if state == "queued" {
			down[d.machineID] = true
		}
		var errText, convID any
		if msg != "" {
			errText = msg
		}
		if conv != "" {
			convID = conv
		}
		if _, err := s.db.Exec(context.WithoutCancel(ctx), `UPDATE offline_chats SET state = ?, error = ?, conv_id = ?, changed_at = ? WHERE id = ?`,
			state, errText, convID, store.Ms(s.now()), d.id); err != nil {
			s.log.Warn("offline chats: the outcome is not kept", "id", d.id, "err", err)
		}
	}
	return made
}

// outboxChatHandOver gives the machine the kept conversation.create and says what its answer makes of the chat:
// delivered with the chat's id, queued (try again) or failed with a reason for the person.
func (s *Service) outboxChatHandOver(ctx context.Context, d outboxChatDue) (string, string, string) {
	u, err := s.db.S().UserByID(ctx, d.userID)
	if errors.Is(err, store.ErrNotFound) {
		return "failed", "Этой машины больше нет у человека", ""
	}
	if err != nil {
		return "queued", "", ""
	}
	m, err := s.admit(ctx, d.userID, d.machineID)
	if err == nil {
		err = s.trustedOnHub(ctx, m)
	}
	var he *httpx.Error
	if errors.As(err, &he) && (he.Status == http.StatusNotFound || he.Status == http.StatusForbidden) {
		return "failed", "Этой машины больше нет у человека", ""
	}
	if err != nil {
		return "queued", "", ""
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outboxCall)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, "/api/v2/cmd/conversation.create", bytes.NewReader(d.create))
	if err != nil {
		return "queued", "", ""
	}
	if pu, err := url.Parse(s.cfg.PublicURL); err == nil {
		req.Host = pu.Host
	}
	req.RemoteAddr = "127.0.0.1:0"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Harnessly-Device", "hub-outbox")
	rec := &pageRecorder{header: http.Header{}}
	s.forward(rec, req, u, m)
	if !rec.wrote || cctx.Err() != nil {
		return "queued", "", ""
	}
	return outboxChatSettle(rec.status, rec.body.Bytes())
}

// outboxChatSettle reads the machine's answer to conversation.create: accepted with the chat's id is made; a refusal
// that passes (the machine busy, restarting) waits for the next pass; any other is the person's to see.
func outboxChatSettle(status int, body []byte) (string, string, string) {
	var ans struct {
		R    string  `json:"r"`
		ID   *string `json:"id"`
		Code string  `json:"code"`
	}
	_ = json.Unmarshal(body, &ans)
	if status >= 200 && status < 300 && (ans.R == "accepted" || ans.R == "queued") && ans.ID != nil && *ans.ID != "" {
		return "delivered", "", *ans.ID
	}
	if ans.R == "refused" {
		switch ans.Code {
		case "machine-offline", "timeout", "internal", "rate-limited":
			return "queued", "", ""
		case "":
			ans.Code = "refused"
		}
		return "failed", "Машина не создала чат (" + ans.Code + ")", ""
	}
	if status == 0 || status >= 500 {
		return "queued", "", ""
	}
	return "failed", "Машина не создала чат (" + http.StatusText(status) + ")", ""
}

// outboxChatsDue are the waiting chats of the machines connected to this hub or its pair, oldest first per machine.
func (s *Service) outboxChatsDue(ctx context.Context) ([]outboxChatDue, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT machine_id FROM offline_chats WHERE state = 'queued'`)
	if err != nil {
		return nil, err
	}
	var on []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if s.reg.Get(id) != nil {
			on = append(on, id)
		}
	}
	_ = rows.Close()
	var due []outboxChatDue
	for _, machine := range on {
		rows, err := s.db.Query(ctx, `SELECT id, user_id, machine_id, create_cmd FROM offline_chats WHERE state = 'queued' AND machine_id = ?
			ORDER BY created_at, id LIMIT ?`, machine, outboxPass)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var d outboxChatDue
			var create string
			if rows.Scan(&d.id, &d.userID, &d.machineID, &create) == nil {
				d.create = []byte(create)
				due = append(due, d)
			}
		}
		_ = rows.Close()
	}
	return due, nil
}
