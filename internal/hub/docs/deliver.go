package docs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// A person's comment on a document goes to the conversation the document was published from (docs.bound) by the op
// docs.deliver on the machine's link. A machine that is off or does not answer is tried again every minute for a day;
// any other refusal is written to the comment as its delivery error, which the thread shows.
const (
	deliverTimeout = 30 * time.Second
	deliverGiveUp  = 24 * time.Hour
	// The sweep leaves a fresh comment to the attempt made when it was written, so the two never hand it over twice.
	sweepAfter = 2 * time.Minute
	sweepEvery = time.Minute
)

// Linker is the part of the machines' link registry delivery needs.
type Linker interface {
	Get(machineID string) *link.Link
	Do(ctx context.Context, machineID, op string, params any, timeout time.Duration) (json.RawMessage, error)
}

type inflight struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func (f *inflight) take(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ids == nil {
		f.ids = map[string]struct{}{}
	}
	if _, busy := f.ids[id]; busy {
		return false
	}
	f.ids[id] = struct{}{}
	return true
}

func (f *inflight) done(id string) {
	f.mu.Lock()
	delete(f.ids, id)
	f.mu.Unlock()
}

// bind records the conversation a document of this machine was published from. A board's binding is not the hub's:
// boards stay with their own service.
func (s *Service) bind(ctx context.Context, mc store.Machine, b hubproto.DocBinding) error {
	if b.Slug == "" || (b.Kind != "" && b.Kind != "artifact") {
		return nil
	}
	_, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET tab_id = ?, session_name = ?, agent_session_id = ?, provider = ?, cwd = ?,
			bound_at = ?, source_path = COALESCE(?, source_path)
		WHERE slug = ? AND machine_id = ? AND (space_id IS NULL OR space_id = ?)`,
		ptr(b.TabID), ptr(b.SessionName), ptr(b.AgentSessionID), nullIf(b.Provider), ptr(b.Cwd), s.now().UnixMilli(), ptr(b.SourcePath),
		b.Slug, mc.ID, mc.SpaceID)
	return err
}

func ptr(v *string) any {
	if v == nil {
		return nil
	}
	return nullIf(*v)
}

// agentReply writes the agent's answer in a thread of a document published from this machine, and resolves the
// thread when asked.
func (s *Service) agentReply(ctx context.Context, machineID string, r hubproto.DocReply) error {
	body := strings.TrimSpace(r.Body)
	if r.ThreadID == "" || (body == "" && !r.Resolve) {
		return errors.New("empty reply")
	}
	var slug string
	var machine sql.NullString
	err := s.db.S().Q().QueryRow(ctx, `SELECT t.slug, m.machine_id FROM doc_threads t JOIN doc_meta m ON m.slug = t.slug WHERE t.id = ?`,
		r.ThreadID).Scan(&slug, &machine)
	if err != nil {
		return err
	}
	if machine.String != machineID {
		return errors.New("the thread is not on a document of this machine")
	}
	if body != "" {
		if _, err := s.addComment(ctx, r.ThreadID, author{Kind: "agent", Name: "Агент"}, body); err != nil {
			return err
		}
	}
	if r.Resolve {
		return s.setStatus(ctx, r.ThreadID, "resolved")
	}
	return nil
}

// pending is one comment with what its message needs.
type pending struct {
	id, threadID, slug, anchor, title, source, machine string
	authorName, body                                   string
	created                                            int64
	tabID, session, agentSession, cwd                  sql.NullString
}

func (s *Service) pendingComment(ctx context.Context, id string) (*pending, error) {
	var p pending
	var machine, source sql.NullString
	err := s.db.S().Q().QueryRow(ctx, `SELECT c.id, c.thread_id, t.slug, t.anchor, m.title, m.source_path, m.machine_id, c.author_name,
			c.body, c.created_at, m.tab_id, m.session_name, m.agent_session_id, m.cwd
		FROM doc_comments c JOIN doc_threads t ON t.id = c.thread_id JOIN doc_meta m ON m.slug = t.slug
		WHERE c.id = ? AND c.author_kind = 'person' AND c.delivered_at IS NULL AND c.delivery_error IS NULL`, id).
		Scan(&p.id, &p.threadID, &p.slug, &p.anchor, &p.title, &source, &machine, &p.authorName, &p.body, &p.created,
			&p.tabID, &p.session, &p.agentSession, &p.cwd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.machine, p.source = machine.String, source.String
	if p.machine == "" || (!p.tabID.Valid && !p.session.Valid && !p.agentSession.Valid) {
		return nil, nil
	}
	return &p, nil
}

// deliver hands one comment to its conversation. From the sweep (local) it skips a machine linked to another replica:
// that replica's sweep takes it.
func (s *Service) deliver(ctx context.Context, id string, local bool) {
	if s.links == nil || !s.flight.take(id) {
		return
	}
	defer s.flight.done(id)
	p, err := s.pendingComment(ctx, id)
	if err != nil {
		s.log.Warn("doc comment not read for delivery", "comment", id, "error", err)
		return
	}
	if p == nil {
		return
	}
	if local {
		if l := s.links.Get(p.machine); l == nil || l.Remote() {
			return
		}
	}
	msg, err := s.message(ctx, p)
	if err != nil {
		s.log.Warn("doc comment message not built", "comment", id, "error", err)
		return
	}
	params := map[string]any{"tabId": nullable(p.tabID), "sessionName": nullable(p.session), "agentSessionId": nullable(p.agentSession),
		"cwd": nullable(p.cwd), "message": msg}
	_, err = s.links.Do(ctx, p.machine, hubproto.OpDocsDeliver, params, deliverTimeout)
	q := s.db.S().Q()
	switch {
	case err == nil:
		_, err = q.Exec(ctx, `UPDATE doc_comments SET delivered_at = ? WHERE id = ?`, s.now().UnixMilli(), id)
	case retryable(err) && s.now().Sub(time.UnixMilli(p.created)) < deliverGiveUp:
		return
	default:
		_, err = q.Exec(ctx, `UPDATE doc_comments SET delivery_error = ? WHERE id = ?`, deliveryText(err), id)
	}
	if err != nil {
		s.log.Warn("doc comment delivery not recorded", "comment", id, "error", err)
	}
}

func nullable(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func retryable(err error) bool {
	var he *httpx.Error
	if errors.As(err, &he) {
		return he.Code == "machine_offline" || he.Code == "machine_timeout"
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func deliveryText(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		if he.Code == "machine_offline" || he.Code == "machine_timeout" {
			return "Машина не была на связи сутки"
		}
		return he.Message
	}
	return err.Error()
}

// Deliver hands a person's new comment to the conversation of its document, in the background.
func (s *Service) Deliver(commentID string) {
	if s.links == nil {
		return
	}
	safego.Go("docs Deliver", func() {
		ctx, cancel := context.WithTimeout(context.Background(), deliverTimeout+10*time.Second)
		defer cancel()
		s.deliver(ctx, commentID, false)
	})
}

// Run probes the documents and boards services (health.go) and tries the comments not yet handed over again every minute until ctx ends, and gives up on those older than a day.
func (s *Service) Run(ctx context.Context) {
	safego.Go("docs services health", func() { s.runHealth(ctx) })
	s.reportLoose(ctx)
	if s.links == nil {
		return
	}
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweep(ctx)
		}
	}
}

func (s *Service) sweep(ctx context.Context) {
	now := s.now()
	q := s.db.S().Q()
	if _, err := q.Exec(ctx, `UPDATE doc_comments SET delivery_error = ?
		WHERE delivered_at IS NULL AND delivery_error IS NULL AND author_kind = 'person' AND created_at < ?
			AND thread_id IN (SELECT t.id FROM doc_threads t JOIN doc_meta m ON m.slug = t.slug WHERE m.bound_at IS NOT NULL)`,
		"Машина не была на связи сутки", now.Add(-deliverGiveUp).UnixMilli()); err != nil {
		s.log.Warn("doc comments not expired", "error", err)
	}
	rows, err := q.Query(ctx, `SELECT c.id FROM doc_comments c JOIN doc_threads t ON t.id = c.thread_id JOIN doc_meta m ON m.slug = t.slug
		WHERE c.delivered_at IS NULL AND c.delivery_error IS NULL AND c.author_kind = 'person' AND c.created_at < ?
			AND m.bound_at IS NOT NULL AND m.machine_id IS NOT NULL
		ORDER BY c.created_at LIMIT 50`, now.Add(-sweepAfter).UnixMilli())
	if err != nil {
		s.log.Warn("pending doc comments not read", "error", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		c, cancel := context.WithTimeout(ctx, deliverTimeout+10*time.Second)
		s.deliver(c, id, true)
		cancel()
	}
}

type docAnchor struct {
	Quote       string `json:"quote"`
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`
	HeadingText string `json:"headingText"`
}

// message is the text the agent gets, the same as the closed cluster sent: where the comment is, the thread so far,
// the comment, and what to do with it.
func (s *Service) message(ctx context.Context, p *pending) (string, error) {
	rows, err := s.db.S().Q().Query(ctx, `SELECT author_kind, author_name, body FROM doc_comments
		WHERE thread_id = ? AND id <> ? AND (created_at < ? OR (created_at = ? AND id < ?))
		ORDER BY created_at DESC, id DESC LIMIT 6`, p.threadID, p.id, p.created, p.created, p.id)
	if err != nil {
		return "", err
	}
	var earlier []string
	for rows.Next() {
		var kind, name, body string
		if err := rows.Scan(&kind, &name, &body); err != nil {
			_ = rows.Close()
			return "", err
		}
		if kind == "agent" || name == "" {
			name = map[bool]string{true: "агент", false: "человек"}[kind == "agent"]
		}
		earlier = append([]string{"— " + name + ": " + oneLine(body, 500)}, earlier...)
	}
	_ = rows.Close()
	return formatMessage(p, s.docURL(p.slug), earlier), nil
}

func (s *Service) docURL(slug string) string {
	return strings.TrimRight(s.cfg.PublicURL, "/") + "/docs/" + slug
}

func formatMessage(p *pending, url string, earlier []string) string {
	var a docAnchor
	_ = json.Unmarshal([]byte(p.anchor), &a)
	var b strings.Builder
	fmt.Fprintf(&b, "💬 Комментарий к твоему документу «%s» (%s), тред %s\n", p.title, url, p.threadID)
	if a.Quote != "" {
		place := "Место: «…" + tail(a.Prefix, 60) + "【" + cut(a.Quote, 400) + "】" + head(a.Suffix, 60) + "…»"
		if a.HeadingText != "" {
			place += " (раздел «" + cut(a.HeadingText, 120) + "»)"
		}
		b.WriteString(place + "\n")
	} else if a.HeadingText != "" {
		b.WriteString("Место: раздел «" + cut(a.HeadingText, 120) + "»\n")
	}
	if len(earlier) > 0 {
		b.WriteString("\nРаньше в треде:\n" + strings.Join(earlier, "\n") + "\n")
	}
	who := p.authorName
	if who == "" {
		who = "Человек"
	}
	fmt.Fprintf(&b, "\n%s пишет:\n%s\n\n", who, p.body)
	if p.source != "" {
		b.WriteString("Исходник: " + p.source + ". ")
	} else {
		b.WriteString("Путь к исходнику не записан — найди файл по заголовку. ")
	}
	fmt.Fprintf(&b, "Реши сам: поправь файл и перепубликуй под тем же slug (%s) или ответь в тред командой документов: "+
		"reply %s \"текст\". Если вопрос закрыт — resolve %s. Ответь в тред в любом случае, коротко: что сделал или почему нет.",
		p.slug, p.threadID, p.threadID)
	return collapseBlank(b.String())
}

func oneLine(s string, n int) string { return cut(strings.Join(strings.Fields(s), " "), n) }

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func head(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[len(r)-n:]
	}
	return string(r)
}

func collapseBlank(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

var _ Linker = (*link.Registry)(nil)
