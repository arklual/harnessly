package docs

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Comments on a board (docs/hub/docs.md, «Comments on a board»). The boards service reports every change of a board's
// comments to POST <platform api>/docs/_board-events with its secret in X-Boards-Webhook-Secret. The hub mirrors the
// thread into doc_threads and doc_comments by the ids the boards service gave (the same event twice changes nothing),
// and a comment of a person goes to the conversation the board was drawn in (board_bindings, docs.bound of kind board)
// the way a comment on a document does. An event the hub does not take is passed on to HUB_BOARD_EVENTS_FORWARD_URL.

// SwitchBoardEvents is the rollout switch (docs/hub/rollout-switches.md) that makes the hub take the events of the
// boards a person drew; for the others the hub passes them on or drops them.
const SwitchBoardEvents = "hub-board-events"

const (
	maxBoardEvent   = 256 << 10
	maxBoardText    = 20000
	maxBoardQuote   = 2000
	maxBoardContext = 400
	secretHeader    = "X-Boards-Webhook-Secret"
)

var boardSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,120}$`)

var boardEventNames = map[string]bool{
	"thread.created": true, "thread.replied": true, "thread.resolved": true, "thread.reopened": true, "thread.removed": true,
}

// Gate says whether a rollout switch is on for a person on a machine.
type Gate interface {
	On(ctx context.Context, feature, person, machine string) bool
}

type boardEvent struct {
	Event  string `json:"event"`
	Slug   string `json:"slug"`
	Thread struct {
		ID       string `json:"id"`
		Resolved bool   `json:"resolved"`
		Anchor   struct {
			ElementID *string  `json:"elementId"`
			Label     *string  `json:"label"`
			X         *float64 `json:"x"`
			Y         *float64 `json:"y"`
		} `json:"anchor"`
	} `json:"thread"`
	Message *struct {
		ID          string  `json:"id"`
		Text        string  `json:"text"`
		Author      string  `json:"author"`
		AuthorEmail *string `json:"authorEmail"`
		AuthorKind  string  `json:"authorKind"`
	} `json:"message"`
}

// boardEvents is POST <p>/_board-events. The answers are the old gateway's: 403 for a wrong secret, 400 for a body
// without event, slug and thread.id, 202 {outcome} when the event was dealt with.
func (s *Service) boardEvents(w http.ResponseWriter, r *http.Request) {
	want := s.cfg.BoardEventsSecret
	if want == "" {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(secretHeader)), []byte(want)) != 1 {
		writeEventAnswer(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBoardEvent+1))
	var ev boardEvent
	if err != nil || len(raw) > maxBoardEvent || json.Unmarshal(raw, &ev) != nil {
		writeEventAnswer(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if !boardEventNames[ev.Event] || !boardSlugRe.MatchString(ev.Slug) || ev.Thread.ID == "" {
		writeEventAnswer(w, http.StatusBadRequest, map[string]string{"error": "event, slug and thread.id are required"})
		return
	}
	outcome, err := s.applyBoardEvent(r.Context(), ev, raw)
	if err != nil {
		s.log.Error("board event not applied", "slug", ev.Slug, "event", ev.Event, "error", err)
		writeEventAnswer(w, http.StatusInternalServerError, map[string]string{"error": "not applied"})
		return
	}
	writeEventAnswer(w, http.StatusAccepted, map[string]string{"outcome": outcome})
}

func writeEventAnswer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// boardBinding is the conversation a board was drawn in.
type boardBinding struct {
	Slug, Machine, Owner, Title string
}

func (s *Service) boardBinding(ctx context.Context, slug string) (*boardBinding, error) {
	var b boardBinding
	err := s.db.S().Q().QueryRow(ctx, `SELECT slug, machine_id, owner_email, title FROM board_bindings WHERE slug = ?`, slug).
		Scan(&b.Slug, &b.Machine, &b.Owner, &b.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// bindBoard records the conversation a board was drawn in; a later publish from another conversation of the same space
// moves it. The board's key is not kept.
func (s *Service) bindBoard(ctx context.Context, mc store.Machine, b hubproto.DocBinding) error {
	if !boardSlugRe.MatchString(b.Slug) {
		return nil
	}
	owner, _ := s.user(ctx, mc.CreatedBy)
	now := s.now().UnixMilli()
	_, err := s.db.S().Q().Exec(ctx, `INSERT INTO board_bindings (slug, space_id, machine_id, owner_email, title, tab_id, session_name,
			agent_session_id, provider, cwd, bound_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (slug) DO UPDATE SET machine_id = excluded.machine_id, owner_email = excluded.owner_email, title = excluded.title,
			tab_id = excluded.tab_id, session_name = excluded.session_name, agent_session_id = excluded.agent_session_id,
			provider = excluded.provider, cwd = excluded.cwd, bound_at = excluded.bound_at, updated_at = excluded.updated_at,
			space_id = COALESCE(board_bindings.space_id, excluded.space_id)
		WHERE board_bindings.space_id IS NULL OR excluded.space_id IS NULL OR board_bindings.space_id = excluded.space_id`,
		b.Slug, nullIf(mc.SpaceID), mc.ID, owner.Email, clip(b.Title, 300), ptr(b.TabID), ptr(b.SessionName), ptr(b.AgentSessionID),
		nullIf(b.Provider), ptr(b.Cwd), now, now)
	return err
}

// applyBoardEvent mirrors one event. outcome: applied, ignored (nobody to take it) or forwarded.
func (s *Service) applyBoardEvent(ctx context.Context, ev boardEvent, raw []byte) (string, error) {
	b, err := s.boardBinding(ctx, ev.Slug)
	if err != nil {
		return "", err
	}
	if b == nil || b.Owner == "" || s.gate == nil || !s.gate.On(ctx, SwitchBoardEvents, b.Owner, b.Machine) {
		return s.forwardBoardEvent(ctx, raw), nil
	}
	externalID := clip(ev.Thread.ID, 128)
	q := s.db.S().Q()
	var threadID string
	err = q.QueryRow(ctx, `SELECT id FROM doc_threads WHERE slug = ? AND external_id = ?`, ev.Slug, externalID).Scan(&threadID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if ev.Event == "thread.removed" {
		if threadID != "" {
			if _, err := q.Exec(ctx, `DELETE FROM doc_comment_mentions WHERE comment_id IN (SELECT id FROM doc_comments WHERE thread_id = ?)`, threadID); err != nil {
				return "", err
			}
			if _, err := q.Exec(ctx, `DELETE FROM doc_comments WHERE thread_id = ?`, threadID); err != nil {
				return "", err
			}
			if _, err := q.Exec(ctx, `DELETE FROM doc_threads WHERE id = ?`, threadID); err != nil {
				return "", err
			}
		}
		return "applied", nil
	}
	a := author{Kind: "person"}
	if m := ev.Message; m != nil && m.AuthorKind == "agent" {
		a = author{Kind: "agent", Email: b.Owner, Name: "Агент"}
	} else if m != nil {
		// A guest who signed with a name only has no address; the name stands in.
		a.Name = clip(strings.TrimSpace(m.Author), 200)
		if m.AuthorEmail != nil {
			a.Email = clip(strings.TrimSpace(*m.AuthorEmail), 320)
		}
		if a.Name == "" {
			a.Name = a.Email
		}
		if a.Name == "" {
			a.Name = "гость"
		}
	}
	if threadID == "" {
		threadID = store.NewID()
		anchor, _ := json.Marshal(boardAnchor(ev))
		if _, err := q.Exec(ctx, `INSERT INTO doc_threads (id, slug, anchor, status, author_kind, author_email, author_name, created_at, external_id)
			VALUES (?, ?, ?, 'open', ?, ?, ?, ?, ?) ON CONFLICT (slug, external_id) WHERE external_id IS NOT NULL DO NOTHING`,
			threadID, ev.Slug, string(anchor), a.Kind, a.Email, a.Name, s.now().UnixMilli(), externalID); err != nil {
			return "", err
		}
		// Another delivery of the same event may have made the thread first: take its id.
		if err := q.QueryRow(ctx, `SELECT id FROM doc_threads WHERE slug = ? AND external_id = ?`, ev.Slug, externalID).Scan(&threadID); err != nil {
			return "", err
		}
	}
	if m := ev.Message; m != nil && m.ID != "" && strings.TrimSpace(m.Text) != "" {
		if err := s.mirrorBoardComment(ctx, threadID, a, clip(m.ID, 128), clip(strings.TrimSpace(m.Text), maxBoardText)); err != nil {
			return "", err
		}
	}
	switch {
	case ev.Event == "thread.resolved" || ev.Thread.Resolved:
		return "applied", s.setStatus(ctx, threadID, "resolved")
	case ev.Event == "thread.reopened":
		return "applied", s.setStatus(ctx, threadID, "open")
	}
	return "applied", nil
}

// mirrorBoardComment adds the comment once and, when a person wrote it, hands it to the board's conversation.
func (s *Service) mirrorBoardComment(ctx context.Context, threadID string, a author, externalID, body string) error {
	q := s.db.S().Q()
	var have string
	err := q.QueryRow(ctx, `SELECT id FROM doc_comments WHERE thread_id = ? AND external_id = ?`, threadID, externalID).Scan(&have)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	id := store.NewID()
	res, err := q.Exec(ctx, `INSERT INTO doc_comments (id, thread_id, author_kind, author_email, author_name, body, created_at, external_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (thread_id, external_id) WHERE external_id IS NOT NULL DO NOTHING`,
		id, threadID, a.Kind, a.Email, a.Name, body, s.now().UnixMilli(), externalID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 && a.Kind == "person" {
		s.Deliver(id)
	}
	return nil
}

// boardAnchor is where on the board a thread sits: the shape (elementId) and its label, or the canvas.
func boardAnchor(ev boardEvent) map[string]any {
	a := ev.Thread.Anchor
	quote := ""
	if a.Label != nil {
		quote = clip(strings.TrimSpace(*a.Label), maxBoardQuote)
	}
	if quote == "" {
		quote = "Холст"
	}
	out := map[string]any{"quote": quote, "prefix": "", "suffix": ""}
	if a.ElementID != nil && *a.ElementID != "" {
		out["elementId"] = clip(*a.ElementID, maxBoardContext)
	}
	if a.X != nil {
		out["x"] = *a.X
	}
	if a.Y != nil {
		out["y"] = *a.Y
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// forwardBoardEvent passes an event the hub does not take on to HUB_BOARD_EVENTS_FORWARD_URL with the same secret, so a
// board nobody moved to the hub keeps working where it did. Without an address, or when it does not answer 2xx, the event
// is dropped: the boards service does not retry, and the next comment on the board is a new event.
func (s *Service) forwardBoardEvent(ctx context.Context, raw []byte) string {
	url := s.cfg.BoardEventsForwardURL
	if url == "" {
		return "ignored"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		s.log.Warn("board event not forwarded", "error", err)
		return "ignored"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(secretHeader, s.cfg.BoardEventsSecret)
	res, err := s.hc.Do(req)
	if err != nil {
		s.log.Warn("board event not forwarded", "error", err)
		return "ignored"
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	if res.StatusCode/100 != 2 {
		s.log.Warn("board event not taken by the forward address", "status", res.StatusCode)
		return "ignored"
	}
	return "forwarded"
}

var boardMentionRe = regexp.MustCompile(`@\[([^\]]*)\]\(person:[^)]*\)`)

// formatBoardMessage is the text the agent gets for a comment on its board: the gateway's text, with the thread named by
// the id the boards service knows, since the agent answers through the boards command (HUB_BOARD_COMMAND).
func formatBoardMessage(p *pending, url, command string, earlier []string) string {
	var a struct {
		Quote     string `json:"quote"`
		ElementID string `json:"elementId"`
	}
	_ = json.Unmarshal([]byte(p.anchor), &a)
	ext := p.external
	if ext == "" {
		ext = p.threadID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "💬 Комментарий на твоей доске «%s» (%s), тред %s\n", p.title, url, ext)
	switch {
	case a.ElementID != "":
		fmt.Fprintf(&b, "Место: у фигуры «%s» (id %s)\n", cut(a.Quote, 200), a.ElementID)
	default:
		b.WriteString("Место: на холсте\n")
	}
	if len(earlier) > 0 {
		b.WriteString("\nРаньше в треде:\n" + strings.Join(earlier, "\n") + "\n")
	}
	who := p.authorName
	if who == "" {
		who = "Человек"
	}
	fmt.Fprintf(&b, "\n%s пишет:\n%s\n\n", who, boardMentionRe.ReplaceAllString(p.body, "@$1"))
	fmt.Fprintf(&b, "Реши сам: поправь доску (%s edit %s …) или ответь в тред: %s comment %s --reply %s \"текст\". "+
		"Если вопрос закрыт — %s comment %s --resolve %s. Ответь в тред в любом случае, коротко: что сделал или почему нет.",
		command, p.slug, command, p.slug, ext, command, p.slug, ext)
	return collapseBlank(b.String())
}
