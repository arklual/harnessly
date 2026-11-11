package docs

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// Reply is what the hub answers an agent's doc.publish or doc.acl: the agent turns it into the tool result.
type Reply struct {
	Status int
	Body   any
}

// Who is the machine an event came from.
type Who struct {
	MachineID string
	// OwnerID is the account that connected the machine.
	OwnerID string
	// SpaceID is the space the machine is in: what it publishes belongs there.
	SpaceID string
}

type publishEvent struct {
	RequestID  string `json:"requestId"`
	Title      string `json:"title"`
	Format     string `json:"format"`
	Content    string `json:"content"`
	Slug       string `json:"slug"`
	Visibility string `json:"visibility"`
	Note       string `json:"note"`
	Source     struct {
		Path string `json:"path"`
	} `json:"source"`
}

type aclEvent struct {
	RequestID string          `json:"requestId"`
	Slug      string          `json:"slug"`
	Method    string          `json:"method"`
	Route     string          `json:"route"`
	Body      json.RawMessage `json:"body"`
}

func reply(status int, code, text string) Reply {
	return Reply{Status: status, Body: map[string]any{"error": text, "code": code}}
}

// Handle answers one event of an agent; ok is false for an event that is not this package's. Without documents set up
// the agent gets a 503 it can show, never silence.
func (s *Service) Handle(ctx context.Context, who Who, kind string, raw json.RawMessage) (rid string, rep Reply, ok bool) {
	switch kind {
	case "doc.publish":
		var e publishEvent
		if json.Unmarshal(raw, &e) != nil {
			return "", reply(http.StatusBadRequest, "invalid", "Не удалось разобрать запрос"), true
		}
		if st := s.Status(); !st.Documents {
			return e.RequestID, Reply{Status: http.StatusServiceUnavailable, Body: s.notConfigured()}, true
		}
		return e.RequestID, s.publish(ctx, who, e), true
	case "doc.acl":
		var e aclEvent
		if json.Unmarshal(raw, &e) != nil {
			return "", reply(http.StatusBadRequest, "invalid", "Не удалось разобрать запрос"), true
		}
		if st := s.Status(); !st.Documents {
			return e.RequestID, Reply{Status: http.StatusServiceUnavailable, Body: s.notConfigured()}, true
		}
		return e.RequestID, s.acl(ctx, who, e), true
	}
	return "", Reply{}, false
}

func (s *Service) publish(ctx context.Context, who Who, e publishEvent) Reply {
	if strings.TrimSpace(e.Content) == "" || strings.TrimSpace(e.Title) == "" {
		return reply(http.StatusBadRequest, "invalid", "У документа нужны заголовок и текст")
	}
	owner, _ := s.user(ctx, who.OwnerID)
	in := publishIn{Title: e.Title, Author: owner.Email}
	if e.Format == "html" {
		in.HTML = e.Content
	} else {
		in.Markdown = e.Content
	}
	if e.Slug != "" {
		sl := e.Slug
		in.Slug = &sl
		m, elsewhere, err := s.meta(ctx, e.Slug, reachMachine(who.SpaceID))
		if err != nil {
			return reply(http.StatusInternalServerError, "internal", "Что-то пошло не так")
		}
		if elsewhere || (m != nil && owner.Email != "" && !strings.EqualFold(m.OwnerEmail, owner.Email)) {
			return reply(http.StatusForbidden, "forbidden", "Этот документ опубликовал другой человек")
		}
	} else {
		in.New = true
	}
	if e.Note != "" {
		n := e.Note
		in.Note = &n
	}
	out, err := s.art.publish(ctx, in)
	if err != nil {
		return s.upstream(err)
	}
	slug := out.Artifact.Slug
	level := levelOf(e.Visibility)
	if err := s.putMeta(ctx, meta{Slug: slug, OwnerEmail: owner.Email, OwnerName: displayName(owner), SpaceID: who.SpaceID, MachineID: who.MachineID, SourcePath: e.Source.Path, Title: e.Title, TeamLevel: level}); err != nil {
		s.log.Error("doc_meta write failed", "slug", slug, "error", err)
		return reply(http.StatusInternalServerError, "internal", "Не удалось записать документ")
	}
	body := map[string]any{"slug": slug, "version": out.Version.N, "created": out.Created}
	if strings.EqualFold(e.Visibility, "public") {
		if u, pub, err := s.art.setPublic(ctx, slug, true); err == nil && pub {
			body["publicUrl"] = u
		}
	}
	return Reply{Status: http.StatusOK, Body: body}
}

func displayName(u store.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func (s *Service) upstream(err error) Reply {
	if isStatus(err, http.StatusConflict) {
		return reply(http.StatusConflict, "version_conflict", "Документ уже изменили или слаг занят")
	}
	if isStatus(err, http.StatusBadRequest) {
		return reply(http.StatusBadRequest, "invalid", "Сервис документов не принял документ")
	}
	s.log.Warn("documents service failed", "error", err)
	return reply(http.StatusBadGateway, "docs_unavailable", "Сервис документов сейчас недоступен, попробуйте позже")
}

// acl handles the sharing calls of the agent's gateway route. Supported: .../acl/team {level} and .../access {public}.
func (s *Service) acl(ctx context.Context, who Who, e aclEvent) Reply {
	m, _, err := s.meta(ctx, e.Slug, reachMachine(who.SpaceID))
	if err != nil {
		return reply(http.StatusInternalServerError, "internal", "Что-то пошло не так")
	}
	if m == nil {
		return reply(http.StatusNotFound, "not_found", "Документ не найден")
	}
	if owner, ok := s.user(ctx, who.OwnerID); !ok || !strings.EqualFold(owner.Email, m.OwnerEmail) {
		return reply(http.StatusForbidden, "forbidden", "Права меняет только владелец документа")
	}
	route := strings.TrimRight(e.Route, "/")
	switch {
	case strings.HasSuffix(route, "/acl/team"):
		var b struct {
			Level string `json:"level"`
		}
		if json.Unmarshal(e.Body, &b) != nil || levelRank[b.Level] == 0 && b.Level != levelNone {
			return reply(http.StatusBadRequest, "invalid", "Уровень: none, view, comment или edit")
		}
		if err := s.setLevel(ctx, m, b.Level); err != nil {
			return reply(http.StatusInternalServerError, "internal", "Что-то пошло не так")
		}
		return Reply{Status: http.StatusOK, Body: map[string]any{"slug": e.Slug, "level": b.Level}}
	case strings.HasSuffix(route, "/access"):
		var b struct {
			Public bool `json:"public"`
		}
		if json.Unmarshal(e.Body, &b) != nil {
			return reply(http.StatusBadRequest, "invalid", "Не удалось разобрать запрос")
		}
		u, pub, err := s.art.setPublic(ctx, e.Slug, b.Public)
		if err != nil {
			return s.upstream(err)
		}
		return Reply{Status: http.StatusOK, Body: s.accessBody(u, pub)}
	}
	return reply(http.StatusNotFound, "not_supported", "Эта настройка прав здесь не поддерживается")
}

// machineEvent records a document's conversation (docs.bound) or the agent's answer in a thread (docs.reply).
func (s *Service) machineEvent(m store.Machine, ev link.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var err error
	if ev.Name == hubproto.EventDocBound {
		var e struct{ Binding hubproto.DocBinding }
		if err = json.Unmarshal(ev.Raw, &e); err == nil {
			err = s.bind(ctx, m, e.Binding)
		}
	} else {
		var e hubproto.DocReply
		if err = json.Unmarshal(ev.Raw, &e); err == nil {
			err = s.agentReply(ctx, m.ID, e)
		}
	}
	if err != nil {
		s.log.Warn("document event not taken", "machine", m.ID, "event", ev.Name, "error", err)
	}
}

// Event takes the doc.publish and doc.acl events of a machine's link and answers each with the op doc.result. It
// returns at once; the answer is sent from a goroutine.
func (s *Service) Event(l *link.Link, m store.Machine, ev link.Event) {
	switch ev.Name {
	case hubproto.EventDocBound, hubproto.EventDocReply:
		safego.Go("docs machineEvent", func() { s.machineEvent(m, ev) })
		return
	case hubproto.EventDocPublish, hubproto.EventDocACL:
	default:
		return
	}
	safego.Go("docs Event", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		rid, rep, ok := s.Handle(ctx, Who{MachineID: m.ID, OwnerID: m.CreatedBy, SpaceID: m.SpaceID}, ev.Name, ev.Raw)
		if !ok || rid == "" {
			return
		}
		if _, err := l.Call(ctx, "doc.result", map[string]any{"requestId": rid, "status": rep.Status, "body": rep.Body}); err != nil {
			s.log.Warn("doc.result not delivered", "machine", m.ID, "error", err)
		}
	})
}
