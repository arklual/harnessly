package docs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A person who calls @harnessly in a comment on a board no conversation drew (a board made in the boards app) gets the
// agent on their own machine, with the board closed (docs/hub/docs.md «@harnessly on a board»): the hub binds
// the board to the caller's machine seen last, and the comment goes the way a comment on a bound board goes — to that
// machine's chat of the board, a new one the first time, typed into it or queued while it works, tried again every
// minute while the machine is away (a day at most). The caller's bell says the machine is awaited, and rings again when
// the agent answers in the thread.

// newBoardTab names no tab: the machine starts a conversation for the board's first comment and the hub keeps its id.
// As the binding's session it marks a board bound by a call: the hub takes its events without the rollout switch.
const newBoardTab = "board-call"

var agentCallRe = regexp.MustCompile(`(?i)(^|\s)@harnessly\b`)

// calledAgent says whether a person's comment calls the agent: the boards service marks it in mentions, an older one
// leaves it in the words.
func calledAgent(ev boardEvent) bool {
	m := ev.Message
	if m == nil || m.AuthorKind == "agent" || (ev.Event != "thread.created" && ev.Event != "thread.replied") {
		return false
	}
	var items []boardMention
	if len(m.Mentions) > 0 && json.Unmarshal(m.Mentions, &items) == nil {
		for _, it := range items {
			if it.Agent {
				return true
			}
		}
	}
	return agentCallRe.MatchString(m.Text)
}

// caller is the hub user who wrote the comment: by the boards service's stable id, else by the e-mail it gave.
func (s *Service) caller(ctx context.Context, ev boardEvent) (store.User, bool) {
	m := ev.Message
	var emails map[string]string
	if uid, _ := s.mentionedUser(ctx, m.AuthorID, &emails); uid != "" {
		u, err := s.db.S().UserByID(ctx, uid)
		return u, err == nil
	}
	if m.AuthorEmail == nil || strings.TrimSpace(*m.AuthorEmail) == "" {
		return store.User{}, false
	}
	u, err := s.db.S().UserByEmail(ctx, strings.ToLower(strings.TrimSpace(*m.AuthorEmail)))
	if err != nil || u.DisabledAt != nil {
		return store.User{}, false
	}
	return u, true
}

// callersMachine is the machine of the person that was on the line last: neither revoked nor switched off.
func (s *Service) callersMachine(ctx context.Context, userID string) (store.Machine, bool) {
	var id string
	err := s.db.S().Q().QueryRow(ctx, `SELECT id FROM machines WHERE created_by = ? AND revoked_at IS NULL AND disabled_at IS NULL
		ORDER BY CASE WHEN last_seen_at IS NULL THEN 1 ELSE 0 END, last_seen_at DESC, created_at DESC LIMIT 1`, userID).Scan(&id)
	if err != nil {
		return store.Machine{}, false
	}
	m, err := s.db.S().MachineByID(ctx, id)
	return m, err == nil
}

// bindCalledBoard binds a board nobody drew to the machine of the person who calls the agent on it; nil when the
// caller or a machine of theirs is not known here (the event then goes as before).
func (s *Service) bindCalledBoard(ctx context.Context, ev boardEvent) (*boardBinding, error) {
	u, ok := s.caller(ctx, ev)
	if !ok {
		return nil, nil
	}
	mc, ok := s.callersMachine(ctx, u.ID)
	if !ok {
		return nil, nil
	}
	tab := newBoardTab
	if err := s.bindBoard(ctx, mc, hubproto.DocBinding{Slug: ev.Slug, Kind: "board", Title: firstNonEmpty(strings.TrimSpace(ev.Title), ev.Slug),
		TabID: &tab, SessionName: &tab, Provider: "claude"}); err != nil {
		return nil, err
	}
	return s.boardBinding(ctx, ev.Slug)
}

// callersOwn says a comment on a board (or a service's document) bound by a call may go to the bound machine: only the
// caller's own comments do. The hub does not know who else may edit there, so another person's comment does not reach
// the caller's agent (security review 15.11): it goes as before, to the board's own conversation if any.
func (s *Service) callersOwn(ctx context.Context, b *boardBinding, ev boardEvent) bool {
	if b == nil || !b.Called {
		return true
	}
	// The agent's own answers and events with no message (a thread resolved) are the bound chat's own.
	if ev.Message == nil || ev.Message.AuthorKind == "agent" {
		return true
	}
	u, ok := s.caller(ctx, ev)
	return ok && b.Owner != "" && strings.EqualFold(u.Email, b.Owner)
}

// keepBoardTab keeps the conversation the machine gave a board's comment: the next comment goes to the same chat.
func (s *Service) keepBoardTab(ctx context.Context, slug string, answer json.RawMessage) {
	var out struct {
		TabID string `json:"tabId"`
	}
	if json.Unmarshal(answer, &out) != nil || out.TabID == "" {
		return
	}
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE board_bindings SET tab_id = ?, updated_at = ? WHERE slug = ? AND (tab_id IS NULL OR tab_id <> ?)`,
		out.TabID, s.now().UnixMilli(), slug, out.TabID); err != nil {
		s.log.Warn("board conversation not kept", "slug", slug, "error", err)
	}
}

// boardOwner is the hub user a board is bound to.
func (s *Service) boardOwner(ctx context.Context, email string) (store.User, bool) {
	if email == "" {
		return store.User{}, false
	}
	u, err := s.db.S().UserByEmail(ctx, email)
	if err != nil || u.DisabledAt != nil {
		return store.User{}, false
	}
	return u, true
}

const (
	// eventKindDoc is an event of the documents service in the boards' shape; its key is doc/<slug>.
	eventKindDoc = "doc"
	docKeyPrefix = "doc/"
)

// appOfKey is the app that opens what a key names, and its slug there.
func appOfKey(key string) (string, string) {
	if slug, ok := strings.CutPrefix(key, docKeyPrefix); ok {
		return docsAppID, slug
	}
	return boardsAppID, key
}

// placeOfKey says where a notice's thread is: «на доске» or «в документе».
func placeOfKey(key string) string {
	if strings.HasPrefix(key, docKeyPrefix) {
		return "в документе"
	}
	return "на доске"
}

// threadLink opens the thread in its app's page.
func threadLink(key, thread string) string {
	app, slug := appOfKey(key)
	return "/apps/" + app + "?" + url.Values{"o": {slug}, "th": {thread}}.Encode()
}

// ringAgentAnswer tells the person a board is bound to that the agent answered in a thread of it.
func (s *Service) ringAgentAnswer(ctx context.Context, b *boardBinding, ev boardEvent) {
	m := ev.Message
	if s.bell == nil || b == nil || m == nil || m.AuthorKind != "agent" || m.ID == "" ||
		(ev.Event != "thread.created" && ev.Event != "thread.replied") {
		return
	}
	u, ok := s.boardOwner(ctx, b.Owner)
	if !ok {
		return
	}
	_, slug := appOfKey(ev.Slug)
	title := "Агент ответил " + placeOfKey(ev.Slug) + " «" + firstNonEmpty(clip(strings.TrimSpace(ev.Title), 80), b.Title, slug) + "»"
	s.bell(ctx, u.ID, strings.ToLower(u.Email), "agent:"+ev.Slug+":"+m.ID, title, mentionLine(m.Text), threadLink(ev.Slug, ev.Thread.ID))
}

// ringAwaited tells the person once that a comment for the agent waits for their machine.
func (s *Service) ringAwaited(ctx context.Context, p *pending) {
	if s.bell == nil {
		return
	}
	var owner, name string
	q := `SELECT b.owner_email, COALESCE(m.name, '') FROM board_bindings b LEFT JOIN machines m ON m.id = b.machine_id WHERE b.slug = ?`
	args := []any{p.slug}
	if !p.board {
		// A document's: the person who wrote the comment.
		q = `SELECT c.author_email, COALESCE(m.name, '') FROM doc_comments c LEFT JOIN machines m ON m.id = ? WHERE c.id = ?`
		args = []any{p.machine, p.id}
	}
	err := s.db.S().Q().QueryRow(ctx, q, args...).Scan(&owner, &name)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return
	}
	u, ok := s.boardOwner(ctx, owner)
	if !ok {
		return
	}
	text := "Агент возьмёт комментарий, когда машина будет на связи"
	if name != "" {
		text = "Агент возьмёт комментарий, когда машина «" + name + "» будет на связи"
	}
	thread := firstNonEmpty(p.external, p.threadID)
	link := threadLink(p.slug, thread)
	if !p.board {
		link = "/apps/" + docsAppID + "?" + url.Values{"o": {p.slug}, "th": {thread}}.Encode()
	}
	_, shown := appOfKey(p.slug)
	s.bell(ctx, u.ID, strings.ToLower(u.Email), "wait:"+p.id, "Ждёт машину · «"+firstNonEmpty(p.title, shown)+"»", text, link)
}

// docsAppID is the documents' app: its page opens a document on a thread as the boards' does.
const docsAppID = "harnessly.docs"

// bindCalledDoc lets a person's @harnessly on a document no conversation published (made in the documents app) reach
// the agent the same way: the document's machine, else the caller's machine seen last, starts a chat for it.
// A document already bound keeps its chat.
func (s *Service) bindCalledDoc(ctx context.Context, slug, userID, body string, canEdit bool) {
	if !canEdit || !agentCallRe.MatchString(body) {
		return
	}
	var machine sql.NullString
	err := s.db.S().Q().QueryRow(ctx, `SELECT machine_id FROM doc_meta WHERE slug = ? AND tab_id IS NULL AND session_name IS NULL
		AND agent_session_id IS NULL`, slug).Scan(&machine)
	if err != nil {
		return
	}
	id := machine.String
	if id == "" {
		mc, ok := s.callersMachine(ctx, userID)
		if !ok {
			return
		}
		id = mc.ID
	}
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET machine_id = ?, tab_id = ?, session_name = ?, provider = 'claude', bound_at = ?
		WHERE slug = ? AND tab_id IS NULL AND session_name IS NULL AND agent_session_id IS NULL`,
		id, newBoardTab, newBoardTab, s.now().UnixMilli(), slug); err != nil {
		s.log.Warn("document not bound for the agent", "slug", slug, "error", err)
	}
}

// keepDocTab keeps the chat the machine started for a called document.
func (s *Service) keepDocTab(ctx context.Context, slug string, answer json.RawMessage) {
	var out struct {
		TabID string `json:"tabId"`
	}
	if json.Unmarshal(answer, &out) != nil || out.TabID == "" {
		return
	}
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET tab_id = ? WHERE slug = ? AND session_name = ? AND tab_id <> ?`,
		out.TabID, slug, newBoardTab, out.TabID); err != nil {
		s.log.Warn("document conversation not kept", "slug", slug, "error", err)
	}
}

// ringDocAnswer tells the person who wrote last in a document's thread that the agent answered there.
func (s *Service) ringDocAnswer(ctx context.Context, slug, threadID, body string) {
	if s.bell == nil {
		return
	}
	var email, title string
	err := s.db.S().Q().QueryRow(ctx, `SELECT c.author_email, COALESCE(m.title, '') FROM doc_comments c JOIN doc_threads t ON t.id = c.thread_id
		LEFT JOIN doc_meta m ON m.slug = t.slug WHERE c.thread_id = ? AND c.author_kind = 'person' ORDER BY c.created_at DESC LIMIT 1`,
		threadID).Scan(&email, &title)
	if err != nil {
		return
	}
	u, ok := s.boardOwner(ctx, email)
	if !ok {
		return
	}
	key := "agent:" + slug + ":" + threadID + ":" + strconv.FormatInt(s.now().UnixMilli(), 10)
	link := "/apps/" + docsAppID + "?" + url.Values{"o": {slug}, "th": {threadID}}.Encode()
	s.bell(ctx, u.ID, strings.ToLower(u.Email), key, "Агент ответил в документе «"+firstNonEmpty(clip(title, 80), slug)+"»",
		mentionLine(body), link)
}

// docCallURL is a document of the documents service, by its key.
func (s *Service) docCallURL(key string) string {
	_, slug := appOfKey(key)
	base := s.cfg.ArtifactsPublicURL
	if base == "" {
		base = s.cfg.PublicURL
	}
	return strings.TrimRight(base, "/") + "/a/" + slug
}

// formatDocCallMessage is the text the agent gets for a comment on a document of the documents service: it answers
// with the documents' tools, as the person.
func formatDocCallMessage(p *pending, url string, earlier []string) string {
	var a struct {
		Quote string `json:"quote"`
	}
	_ = json.Unmarshal([]byte(p.anchor), &a)
	_, slug := appOfKey(p.slug)
	ext := firstNonEmpty(p.external, p.threadID)
	var b strings.Builder
	fmt.Fprintf(&b, "💬 Комментарий в документе «%s» (%s), тред %s\n", firstNonEmpty(p.title, slug), url, ext)
	if q := strings.TrimSpace(a.Quote); q != "" && q != "Холст" {
		fmt.Fprintf(&b, "Место: у цитаты «%s»\n", cut(q, 200))
	}
	if len(earlier) > 0 {
		b.WriteString("\nРаньше в треде:\n" + strings.Join(earlier, "\n") + "\n")
	}
	fmt.Fprintf(&b, "\n%s пишет:\n%s\n\n", firstNonEmpty(p.authorName, "Человек"), p.body)
	fmt.Fprintf(&b, "Реши сам: поправь документ (инструменты документов: quick_apply или doc_edit, документ %s) или ответь в тред "+
		"инструментом doc_reply (документ %s, тред %s). Комментарий — это комментарий, а не просьба переделать: правь, только если "+
		"об этом просят. Ответь в тред в любом случае, коротко: что сделал или почему нет.", slug, slug, ext)
	return collapseBlank(b.String())
}
