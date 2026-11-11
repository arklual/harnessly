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
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// The message queue of a machine that is off (docs/hub/offline-prompts.md): what a person writes to it waits in
// the hub, shows on every device of theirs, and the hub hands it to the machine's runtime once the machine is back. The
// API is the cluster gateway's (src/cluster/offline-prompts.ts), under the workspace's platform API:
//
//	POST   <platform api>/offline-prompts?machine=<id>            {tabId, text, send}  → 201 {item}
//	GET    <platform api>/offline-prompts?machine=<id>&tab=<tab>                       → {items}
//	DELETE <platform api>/offline-prompts?machine=<id>&client=<clientId> (or &id=<item id>) → {item, outcome}
//
// Taking a message back has one outcome: "cancelled" (it waited here and is gone: the machine never gets it from the
// hub) or "delivered" (the machine has it; it is the machine's to cancel now). While the hub is handing it over the
// answer is 409 with code "sending" — ask again; a message the hub does not keep is 404.
//
// `send` is the page's own v2 send command; the hub gives the machine exactly that body, so a page that sends it again
// by itself gets the same answer (one opId).

var outboxPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/offline-prompts$`)

const (
	maxOutboxText = 100_000
	maxOutboxBody = maxOutboxText*4 + 1024
	maxOutboxTab  = 20
	// maxOutboxMachine bounds what one person keeps waiting for one machine across its chats.
	maxOutboxMachine = 200
	// outboxKept is how long a delivered or failed message is kept at all; past it the row goes.
	outboxKept    = 24 * time.Hour
	maxOutboxRefs = 50
	// outboxShown is how long a delivered message stays listed, so the page can show it leaving the queue.
	outboxShown = 10 * time.Minute
	// outboxStuck: a message the hub began to hand over and never finished (it restarted) goes again; the machine takes
	// one opId once.
	outboxStuck = 2 * time.Minute
	// OutboxEvery is how often the hub looks for messages whose machine is back.
	OutboxEvery = 5 * time.Second
	// outboxPass is how many waiting messages one pass looks at; outboxCall how long the machine has to answer one.
	outboxPass = 200
	outboxCall = 30 * time.Second
	// outboxPassMax bounds one pass; what it did not reach waits for the next.
	outboxPassMax = 2 * time.Minute
	// outboxParallel is how many machines are handed their messages at once.
	outboxParallel = 8
)

var (
	outboxTab    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	outboxClient = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	outboxWord   = regexp.MustCompile(`^[a-z][a-z-]{0,31}$`)
)

type outboxItem struct {
	ID        string          `json:"id"`
	TabID     string          `json:"tabId"`
	Text      string          `json:"text"`
	State     string          `json:"state"`
	Error     *string         `json:"error"`
	CreatedAt int64           `json:"createdAt"`
	ClientID  string          `json:"clientId"`
	Send      json.RawMessage `json:"send"`
}

const outboxCols = `id, tab_id, text, state, error, created_at, client_id, send`

func scanOutbox(sc interface{ Scan(...any) error }) (outboxItem, error) {
	var it outboxItem
	var errText sql.NullString
	var send string
	if err := sc.Scan(&it.ID, &it.TabID, &it.Text, &it.State, &errText, &it.CreatedAt, &it.ClientID, &send); err != nil {
		return it, err
	}
	if errText.Valid {
		it.Error = &errText.String
	}
	it.Send = json.RawMessage(send)
	return it, nil
}

// readSend is the page's send command as the hub may keep it: of this chat, nothing attached (attachments live on the
// machine), the opId its clientId. Anything else is refused rather than half-kept.
func readSend(raw json.RawMessage, tabID string) (json.RawMessage, string, bool) {
	var v map[string]json.RawMessage
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil, "", false
	}
	str := func(k string) (string, bool) {
		var s string
		return s, json.Unmarshal(v[k], &s) == nil && v[k] != nil
	}
	opID, ok1 := str("opId")
	clientID, ok2 := str("clientId")
	conv, ok3 := str("conversationId")
	text, ok4 := str("text")
	mode, ok5 := str("mode")
	kind, ok6 := str("kind")
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !outboxClient.MatchString(clientID) || opID != clientID {
		return nil, "", false
	}
	if conv != tabID || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > maxOutboxText || !outboxWord.MatchString(mode) || !outboxWord.MatchString(kind) {
		return nil, "", false
	}
	var att []json.RawMessage
	if json.Unmarshal(v["attachmentIds"], &att) != nil || v["attachmentIds"] == nil || len(att) > 0 {
		return nil, "", false
	}
	if r, has := v["refs"]; has {
		var refs []json.RawMessage
		if json.Unmarshal(r, &refs) != nil || len(refs) > maxOutboxRefs {
			return nil, "", false
		}
	}
	for k := range v {
		switch k {
		case "opId", "clientId", "conversationId", "text", "attachmentIds", "mode", "kind", "refs":
		default:
			return nil, "", false
		}
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, "", false
	}
	return out, clientID, true
}

func outboxError(w http.ResponseWriter, status int, msg, code string) {
	body := map[string]string{"error": msg}
	if code != "" {
		body["code"] = code
	}
	httpx.WriteJSON(w, status, body)
}

// serveOutbox answers <platform api>/offline-prompts for a signed-in person: the machine is the named one, or home.
func (s *Service) serveOutbox(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodDelete:
	default:
		outboxError(w, http.StatusMethodNotAllowed, "method", "")
		return
	}
	q := r.URL.Query()
	id := q.Get("machine")
	if id == "" || id == "local" {
		if id = s.home(r.Context(), p.User.ID, true); id == "" {
			s.fail(w, r, s.workspaceEmpty(r.Context(), p.User.ID))
			return
		}
	}
	if !machineID.MatchString(id) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	m, err := s.admit(r.Context(), p.User.ID, id)
	if err == nil {
		err = s.trustedOnHub(r.Context(), m)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sp, err := s.db.S().SpaceByID(r.Context(), m.SpaceID)
	if err != nil || !s.fleetOn(r.Context(), sp) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		tab := q.Get("tab")
		if !outboxTab.MatchString(tab) {
			outboxError(w, http.StatusBadRequest, "Нужна вкладка", "")
			return
		}
		items, err := s.outboxList(ctx, p.User.ID, m.ID, tab)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var body struct {
			TabID string          `json:"tabId"`
			Text  string          `json:"text"`
			Send  json.RawMessage `json:"send"`
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxOutboxBody))
		if err != nil || json.Unmarshal(raw, &body) != nil {
			outboxError(w, http.StatusBadRequest, "Нужны вкладка и текст", "")
			return
		}
		text := strings.TrimSpace(body.Text)
		if !outboxTab.MatchString(body.TabID) || text == "" {
			outboxError(w, http.StatusBadRequest, "Нужны вкладка и текст", "")
			return
		}
		if utf8.RuneCountInString(text) > maxOutboxText {
			outboxError(w, http.StatusRequestEntityTooLarge, "Сообщение слишком длинное", "")
			return
		}
		send, clientID, ok := readSend(body.Send, body.TabID)
		if !ok {
			outboxError(w, http.StatusBadRequest, "Команда отправки не та", "bad-send")
			return
		}
		item, err := s.outboxAdd(ctx, p.User.ID, m.ID, body.TabID, text, clientID, send)
		if errors.Is(err, errOutboxFull) {
			outboxError(w, http.StatusConflict, "Очередь этой машины полна", "")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"item": item})
	case http.MethodDelete:
		col, val := "id", q.Get("id")
		if c := q.Get("client"); c != "" {
			col, val = "client_id", c
		}
		item, outcome, err := s.outboxRemove(ctx, p.User.ID, m.ID, col, val)
		if errors.Is(err, store.ErrNotFound) {
			outboxError(w, http.StatusNotFound, "Такого сообщения в очереди нет", "not-found")
			return
		}
		if errors.Is(err, errOutboxSending) {
			outboxError(w, http.StatusConflict, "Сообщение уходит на машину", "sending")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"item": item, "outcome": outcome})
	}
}

var (
	errOutboxFull = errors.New("proxy: the machine's queue is full")
	// errOutboxSending: the message is being handed to the machine just now; what became of it is known after that.
	errOutboxSending = errors.New("proxy: the message is being handed over")
)

// The outcomes of taking a message back.
const (
	outboxCancelled = "cancelled"
	outboxDelivered = "delivered"
)

func (s *Service) outboxList(ctx context.Context, userID, machine, tab string) ([]outboxItem, error) {
	rows, err := s.db.Query(ctx, `SELECT `+outboxCols+` FROM offline_prompts
		WHERE user_id = ? AND machine_id = ? AND tab_id = ? AND (state <> 'delivered' OR changed_at > ?)
		ORDER BY created_at, id`, userID, machine, tab, store.Ms(s.now().Add(-outboxShown)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []outboxItem{}
	for rows.Next() {
		it, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Service) outboxByClient(ctx context.Context, userID, machine, client string) (outboxItem, error) {
	return scanOutbox(s.db.QueryRow(ctx, `SELECT `+outboxCols+` FROM offline_prompts WHERE user_id = ? AND machine_id = ? AND client_id = ?`,
		userID, machine, client))
}

// outboxAdd keeps a message once per clientId (the page sends it again by its own timer and after F5), at most
// maxOutboxTab waiting per chat.
func (s *Service) outboxAdd(ctx context.Context, userID, machine, tab, text, client string, send json.RawMessage) (outboxItem, error) {
	if it, err := s.outboxByClient(ctx, userID, machine, client); err == nil {
		return it, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return outboxItem{}, err
	}
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM offline_prompts WHERE user_id = ? AND machine_id = ? AND tab_id = ? AND state IN ('queued', 'sending')`,
		userID, machine, tab).Scan(&n); err != nil {
		return outboxItem{}, err
	}
	if n >= maxOutboxTab {
		return outboxItem{}, errOutboxFull
	}
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM offline_prompts WHERE user_id = ? AND machine_id = ? AND state IN ('queued', 'sending', 'failed')`,
		userID, machine).Scan(&n); err != nil {
		return outboxItem{}, err
	}
	if n >= maxOutboxMachine {
		return outboxItem{}, errOutboxFull
	}
	// Written order is kept even within one millisecond: a message is never older than the last one of this machine.
	now := store.Ms(s.now())
	var last int64
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(MAX(created_at), 0) FROM offline_prompts WHERE user_id = ? AND machine_id = ?`, userID, machine).Scan(&last); err != nil {
		return outboxItem{}, err
	}
	if last >= now {
		now = last + 1
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO offline_prompts (id, user_id, machine_id, tab_id, client_id, text, send, state, created_at, changed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?) ON CONFLICT (user_id, machine_id, client_id) DO NOTHING`,
		store.NewID(), userID, machine, tab, client, text, string(send), now, now); err != nil {
		return outboxItem{}, err
	}
	// Two sends of one message at once: whichever got in kept it.
	return s.outboxByClient(ctx, userID, machine, client)
}

// outboxRemove takes back a message, with one outcome. A message still waiting (or failed) is deleted: cancelled, the
// hub never hands it over — the delete and the pass that takes a message into delivery both move the row only from
// 'queued', so one of them wins. A delivered one stays as it is: delivered. One being handed over has no outcome yet
// (errOutboxSending): the pass ends it as delivered, or puts it back to wait, and the caller asks again.
func (s *Service) outboxRemove(ctx context.Context, userID, machine, col, val string) (outboxItem, string, error) {
	if col != "id" && col != "client_id" {
		return outboxItem{}, "", store.ErrNotFound
	}
	// The pass may take the message between the look and the delete: then the look is taken again.
	for range 2 {
		it, err := scanOutbox(s.db.QueryRow(ctx, `SELECT `+outboxCols+` FROM offline_prompts WHERE `+col+` = ? AND user_id = ? AND machine_id = ?`,
			val, userID, machine))
		if err != nil {
			return outboxItem{}, "", err
		}
		switch it.State {
		case "delivered":
			return it, outboxDelivered, nil
		case "sending":
			return outboxItem{}, "", errOutboxSending
		}
		res, err := s.db.Exec(ctx, `DELETE FROM offline_prompts WHERE id = ? AND state IN ('queued', 'failed')`, it.ID)
		if err != nil {
			return outboxItem{}, "", err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return it, outboxCancelled, nil
		}
	}
	return outboxItem{}, "", errOutboxSending
}

// ---- delivery ----

// RunOutbox hands the waiting messages to machines that are back, every OutboxEvery, until ctx ends.
func (s *Service) RunOutbox(ctx context.Context) {
	t := time.NewTicker(OutboxEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.DeliverOutbox(ctx)
	}
}

type outboxDue struct {
	id, userID, machineID string
	send                  []byte
}

// DeliverOutbox is one pass: the messages of connected machines, every machine's own oldest first and one at a time,
// the machines side by side. It returns how many were delivered.
func (s *Service) DeliverOutbox(ctx context.Context) int {
	now := s.now()
	if _, err := s.db.Exec(ctx, `UPDATE offline_prompts SET state = 'queued', changed_at = ? WHERE state = 'sending' AND changed_at < ?`,
		store.Ms(now), store.Ms(now.Add(-outboxStuck))); err != nil {
		if ctx.Err() == nil {
			s.log.Warn("offline prompts: stuck messages not released", "err", err)
		}
		return 0
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM offline_prompts WHERE state IN ('delivered', 'failed') AND changed_at < ?`,
		store.Ms(now.Add(-outboxKept))); err != nil && ctx.Err() == nil {
		s.log.Warn("offline prompts: old messages not removed", "err", err)
	}
	// Only the machines that are connected are read: the messages of machines that stay off never crowd out theirs.
	machines, err := s.outboxMachines(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("offline prompts: the queue is unreadable", "err", err)
		}
		return 0
	}
	// A pass is bounded: a machine that takes each message to the last second does not hold up the hub's loop for long.
	ctx, cancel := context.WithTimeout(ctx, outboxPassMax)
	defer cancel()
	// Each machine has a line of its own, handed over in order by a routine of its own: a machine that is slow to
	// take a message holds up only its own line. The routines share outboxParallel slots, so a hub with many machines
	// back at once does not open a tunnel to all of them together.
	lines := make([][]outboxDue, 0, len(machines))
	for _, machine := range machines {
		rows, err := s.db.Query(ctx, `SELECT id, user_id, machine_id, send FROM offline_prompts WHERE state = 'queued' AND machine_id = ?
			ORDER BY created_at, id LIMIT ?`, machine, outboxPass)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("offline prompts: the queue is unreadable", "err", err)
			}
			return 0
		}
		var line []outboxDue
		for rows.Next() {
			var d outboxDue
			var send string
			if rows.Scan(&d.id, &d.userID, &d.machineID, &send) == nil {
				d.send = []byte(send)
				line = append(line, d)
			}
		}
		_ = rows.Close()
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	var delivered atomic.Int64
	var wg sync.WaitGroup
	slots := make(chan struct{}, outboxParallel)
	for _, line := range lines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			safego.Run("outbox line", func() { delivered.Add(int64(s.deliverLine(ctx, line))) })
		}()
	}
	wg.Wait()
	return int(delivered.Load())
}

// deliverLine hands over the messages of one machine, oldest first, one at a time, and stops at the first the machine
// did not take, so the order of its messages (and with it the order inside each chat) stays. It returns how many were
// delivered.
func (s *Service) deliverLine(ctx context.Context, line []outboxDue) int {
	delivered := 0
	for _, d := range line {
		if ctx.Err() != nil || s.reg.Get(d.machineID) == nil {
			break
		}
		res, err := s.db.Exec(ctx, `UPDATE offline_prompts SET state = 'sending', changed_at = ? WHERE id = ? AND state = 'queued'`, store.Ms(s.now()), d.id)
		if err != nil {
			break
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // another hub of the pair took it
		}
		state, msg := s.outboxHandOver(ctx, d)
		if state == "delivered" {
			delivered++
		}
		var errText any
		if msg != "" {
			errText = msg
		}
		if _, err := s.db.Exec(context.WithoutCancel(ctx), `UPDATE offline_prompts SET state = ?, error = ?, changed_at = ? WHERE id = ?`,
			state, errText, store.Ms(s.now()), d.id); err != nil {
			s.log.Warn("offline prompts: the outcome is not kept", "id", d.id, "err", err)
		}
		if state == "queued" {
			break // the next message of this machine waits for the next pass
		}
	}
	return delivered
}

// outboxMachines are the machines with messages waiting that are connected to this hub or its pair.
func (s *Service) outboxMachines(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT machine_id FROM offline_prompts WHERE state = 'queued'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var on []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if s.reg.Get(id) != nil {
			on = append(on, id)
		}
	}
	return on, rows.Err()
}

// outboxHandOver gives the machine the kept send command and says what the answer makes of the message: delivered,
// queued (the machine is not there just now: try again) or failed with a reason for the person.
func (s *Service) outboxHandOver(ctx context.Context, d outboxDue) (string, string) {
	u, err := s.db.S().UserByID(ctx, d.userID)
	if errors.Is(err, store.ErrNotFound) {
		return "failed", "Этой машины больше нет у человека"
	}
	if err != nil {
		return "queued", ""
	}
	m, err := s.admit(ctx, d.userID, d.machineID)
	if err == nil {
		err = s.trustedOnHub(ctx, m)
	}
	var he *httpx.Error
	if errors.As(err, &he) && (he.Status == http.StatusNotFound || he.Status == http.StatusForbidden) {
		return "failed", "Этой машины больше нет у человека"
	}
	if err != nil {
		return "queued", ""
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outboxCall)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, "/api/v2/cmd/send", bytes.NewReader(d.send))
	if err != nil {
		return "queued", ""
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
		return "queued", "" // no answer in time: the next pass tries again
	}
	return outboxSettle(rec.status, rec.body.Bytes())
}

// outboxSettle is the gateway's v2SendSettles: accepted and queued are delivered; too-late means the machine has this
// message from the page itself; machine-offline, timeout, internal, rate-limited and 5xx wait for the next pass.
func outboxSettle(status int, body []byte) (string, string) {
	var ans struct {
		R    string `json:"r"`
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &ans)
	if status >= 200 && status < 300 && (ans.R == "accepted" || ans.R == "queued") {
		return "delivered", ""
	}
	if ans.R == "refused" {
		switch ans.Code {
		case "too-late":
			return "delivered", ""
		case "conflict":
			return "failed", "Этот чат продолжен на другой машине"
		case "machine-offline", "timeout", "internal", "rate-limited":
			return "queued", ""
		case "":
			ans.Code = "refused"
		}
		return "failed", "Машина не приняла сообщение (" + ans.Code + ")"
	}
	if status == 0 || status >= 500 {
		return "queued", ""
	}
	return "failed", "Машина не приняла сообщение (" + http.StatusText(status) + ")"
}
