package docs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
)

// With HUB_RESOURCE_ACCESS a document's access is the shared model of access to a resource (internal/hub/resaccess):
// people, teams, guest links, requests and a log. A document without a row there is open as doc_meta says, so turning
// the flag on changes nothing until a manager changes who has access. Its team level reaches the people of the
// document's organizations only (see circle).

func (s *Service) actorOf(ctx context.Context, m *meta, author string, p *auth.Principal, c *circle) resaccess.Actor {
	if c == nil {
		c = s.circleOf(p)
	}
	return resaccess.Actor{UserID: p.User.ID, Email: p.User.Email, Name: p.User.Name, Verified: c.proved(ctx), Admin: p.Admin,
		Outside: !p.Admin && !c.holds(ctx, m, author)}
}

// refOf names a document for the shared model, in the space the hub has it in.
func refOf(slug string, m *meta) resaccess.Ref {
	ref := resaccess.Ref{Kind: resaccess.KindDoc, ID: slug}
	if m != nil {
		ref.Space = m.SpaceID
	}
	return ref
}

// accessRank is the person's rank on a document by the shared model.
func (s *Service) accessRank(ctx context.Context, slug string, m *meta, author string, p *auth.Principal, c *circle) int {
	if p.Admin {
		return rankManage
	}
	role, err := s.access.Role(ctx, refOf(slug, m), s.actorOf(ctx, m, author, p, c))
	if err != nil {
		s.log.Warn("document access not read", "slug", slug, "error", err)
		return 0
	}
	switch role {
	case "view", "comment", "edit":
		return levelRank[role]
	case "manage", "owner":
		return rankManage
	}
	return 0
}

func (s *Service) mountAccess(mux *http.ServeMux) {
	if s.access == nil {
		return
	}
	p := s.prefix
	mux.Handle(p+"/{slug}/acl", s.guard(s.aclRoute))
	mux.Handle(p+"/{slug}/acl/{rest...}", s.guard(s.aclRoute))
	// A guest opens a document by the link's token and signs in nowhere.
	mux.HandleFunc("GET "+p+"-guest/{token}", s.guestDoc)
	mux.HandleFunc("GET "+p+"-guest/{token}/content", s.guestContent)
	mux.HandleFunc("POST "+p+"-guest/{token}/comments", s.guestComment)
	mux.HandleFunc("GET /d/{token}", s.guestPage)
}

func (s *Service) aclRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	slug := r.PathValue("slug")
	m, elsewhere, err := s.meta(r.Context(), slug, s.reachOf(r.Context(), p))
	if err != nil {
		return err
	}
	if elsewhere {
		return errNoAccess
	}
	author := ""
	if m == nil {
		list, err := s.art.list(r.Context())
		if err != nil {
			return err
		}
		found := false
		for _, a := range list {
			if a.Slug == slug {
				found, author = true, a.Author
			}
		}
		if !found {
			return errNoDoc
		}
	}
	info := resaccess.Info{}
	if m != nil {
		info = resaccess.Info{Owner: m.OwnerEmail, OwnerName: m.OwnerName}
	}
	rest := []string{}
	for _, v := range strings.Split(r.PathValue("rest"), "/") {
		if v != "" {
			rest = append(rest, v)
		}
	}
	s.access.Serve(w, r, s.actorOf(r.Context(), m, author, p, nil), refOf(slug, m), rest, info, func(v any) (string, error) {
		if v == nil {
			return "", nil
		}
		return fmt.Sprint(v), nil
	})
	return nil
}

// guest finds the link of the request and the document it opens, or answers for it.
func (s *Service) guest(w http.ResponseWriter, r *http.Request) (*resaccess.Guest, bool) {
	g, err := s.access.Resolve(r.Context(), r.PathValue("token"))
	if err == nil && g.Kind != resaccess.KindDoc {
		err = errors.New("not a document link")
	}
	if err != nil {
		code, text := "not_found", "Ссылка не найдена"
		status := http.StatusNotFound
		var e *resaccess.Error
		if errors.As(err, &e) {
			code, text, status = e.Code, e.Message, resaccess.Status(err)
		}
		httpx.WriteJSON(w, status, map[string]any{"error": text, "code": code})
		return nil, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return g, true
}

func (s *Service) guestDoc(w http.ResponseWriter, r *http.Request) {
	g, ok := s.guest(w, r)
	if !ok {
		return
	}
	s.access.RecordOpen(r.Context(), g.LinkID)
	th, err := s.threads(r.Context(), g.ID)
	if err != nil {
		s.answer(w, r, err)
		return
	}
	title := g.Title
	if m, _, _ := s.meta(r.Context(), g.ID, reachSpace(g.Space)); m != nil && m.Title != "" {
		title = m.Title
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"doc": map[string]any{"slug": g.ID, "title": title}, "rights": g.Rights,
		"threads": th, "content": s.prefix + "-guest/" + url.PathEscape(r.PathValue("token")) + "/content"})
}

func (s *Service) guestContent(w http.ResponseWriter, r *http.Request) {
	g, ok := s.guest(w, r)
	if !ok {
		return
	}
	res, err := s.art.page(r.Context(), g.ID, g.Pinned, themeOf(r.URL.Query().Get("theme")))
	if err != nil {
		s.answer(w, r, err)
		return
	}
	defer func() { _ = res.Body.Close() }()
	page, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		s.answer(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	frameable(h)
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(withFrame(page))
}

// guestComment takes a comment from a guest whose link allows it. The text is the guest's: it is kept for the owner to
// read and is not handed to the agent of the conversation the document was published from.
func (s *Service) guestComment(w http.ResponseWriter, r *http.Request) {
	g, ok := s.guest(w, r)
	if !ok {
		return
	}
	fail400 := func(text string) {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": text, "code": "invalid"})
	}
	if g.Rights != "comment" {
		httpx.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "Эта ссылка не разрешает комментировать", "code": "forbidden"})
		return
	}
	var in struct {
		Name     string `json:"name"`
		Body     string `json:"body"`
		ThreadID string `json:"threadId"`
		Website  string `json:"website"` // a field people do not see; a bot fills it
	}
	if err := decode(r, &in); err != nil {
		s.answer(w, r, err)
		return
	}
	if in.Website != "" {
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true})
		return
	}
	body, name := strings.TrimSpace(in.Body), strings.TrimSpace(in.Name)
	if body == "" || len(body) > 5000 {
		fail400("Комментарий пустой или слишком длинный")
		return
	}
	if name == "" || len(name) > 60 {
		fail400("Назовите себя: имя до 60 знаков")
		return
	}
	if err := s.access.GuestCommentAllowed(r.Context(), g.LinkID); err != nil {
		httpx.WriteJSON(w, resaccess.Status(err), map[string]any{"error": err.Error(), "code": "rate_limited"})
		return
	}
	who := author{Kind: "guest", Name: name}
	var threadID string
	if in.ThreadID != "" {
		t, err := s.thread(r.Context(), g.ID, in.ThreadID)
		if err != nil || t == nil {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "Обсуждение не найдено", "code": "not_found"})
			return
		}
		if _, err := s.addComment(r.Context(), in.ThreadID, who, body); err != nil {
			s.answer(w, r, err)
			return
		}
		threadID = in.ThreadID
	} else {
		id, _, err := s.addThread(r.Context(), g.ID, []byte(`{"quote":"","prefix":"","suffix":""}`), who, body)
		if err != nil {
			s.answer(w, r, err)
			return
		}
		threadID = id
	}
	if err := s.access.NoteGuestComment(r.Context(), g.LinkID, threadID, name); err != nil {
		s.log.Warn("guest comment not tied to its link", "error", err)
	}
	s.tellOwner(r.Context(), g.Ref, name)
	t, _ := s.thread(r.Context(), g.ID, threadID)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"thread": t})
}

func (s *Service) tellOwner(ctx context.Context, ref resaccess.Ref, guest string) {
	slug := ref.ID
	m, _, err := s.meta(ctx, slug, reachSpace(ref.Space))
	if err != nil || m == nil || m.OwnerEmail == "" || s.notice == nil {
		return
	}
	_ = s.notice(ctx, m.OwnerEmail, "Гость оставил комментарий", guest+" оставил комментарий к «"+firstNonEmpty(m.Title, slug)+"»", s.cfg.PublicURL)
}

// A mention is @[Name](person:email) in a comment's text. Five people at most; a person the hub does not know is not one.
var mentionRe = regexp.MustCompile(`@\[[^\]\n]{1,80}\]\(person:([^)\s]{3,254})\)`)

const mentionsMax = 5

// mention records who a comment names and tells each of them once. It never fails the comment.
func (s *Service) mention(ctx context.Context, doc summary, commentID, body string, by *auth.Principal) {
	if s.access == nil {
		return
	}
	seen := map[string]bool{strings.ToLower(by.User.Email): true}
	for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
		email := strings.ToLower(strings.TrimSpace(m[1]))
		if seen[email] {
			continue
		}
		seen[email] = true
		if len(seen) > mentionsMax+1 {
			break
		}
		if _, err := s.db.S().UserByEmail(ctx, email); err != nil {
			continue
		}
		now := s.now().UnixMilli()
		res, err := s.db.S().Q().Exec(ctx, `INSERT INTO doc_comment_mentions (comment_id, email, notified_at, created_at) VALUES (?, ?, NULL, ?)
			ON CONFLICT (comment_id, email) DO NOTHING`, commentID, email, now)
		if err != nil {
			s.log.Warn("mention not kept", "error", err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 || s.notice == nil {
			continue
		}
		// The notice names the document and the author; the text stays in the document, for people who may read it.
		if err := s.notice(ctx, email, "Вас упомянули в комментарии",
			firstNonEmpty(by.User.Name, by.User.Email)+" упомянул(а) вас в комментарии к «"+firstNonEmpty(doc.Title, doc.Slug)+"»", s.cfg.PublicURL); err != nil {
			s.log.Warn("mention not mailed", "error", err)
			continue
		}
		_, _ = s.db.S().Q().Exec(ctx, `UPDATE doc_comment_mentions SET notified_at = ? WHERE comment_id = ? AND email = ?`, now, commentID, email)
	}
}
