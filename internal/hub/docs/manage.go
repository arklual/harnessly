package docs

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Documents and boards made, copied, renamed and put into the trash through the hub. The hub owns the
// records of an installation's documents and boards: who owns one, which space it is in and who may do what with it, so
// a person (the harnessly page) or a service on a person's behalf (the documents and boards apps) asks the hub, and the
// hub asks the documents or boards service with its own token.
//
// The rights are the boards' ones: anybody in a space makes a document or a board there and the space edits it; a copy
// needs the right to read the original and belongs to whoever made it; renaming needs the right to edit; only the
// owner or an administrator puts a thing into the trash and takes it out. A thing in the trash opens for nobody, leaves
// every list and the right the hub tells the services is none, until it is taken out. Nothing is deleted for good here.
//
// The routes (<p> is the documents prefix, /api/platform/docs):
//
//	POST   <p>                   make one: {kind, title, markdown | html | template, spaceId} → 201 {doc}
//	POST   <p>/{slug}/duplicate  a copy: {title?, spaceId?} → 201 {doc}
//	PATCH  <p>/{slug}            rename: {title} → {doc}
//	DELETE <p>/{slug}            into the trash → {slug, kind, trashedAt, trashedBy}
//	POST   <p>/{slug}/restore    out of the trash → {doc}
//	GET    <p>/_trash            what the person may take out of the trash → {docs: [...]}
//
// A service calls them as a person: Authorization: Bearer <the documents' access token or the boards' event secret>
// and X-Harnessly-Token: <the person's platform token the hub signed for that service (its origin is the audience)>.
// The secret alone names nobody: only a live token the hub signed for the person does, and a service never acts as an
// administrator, whoever the person is.

const (
	kindArtifact = "artifact"
	kindBoard    = "board"
	// tokenHeader carries the platform token of the person a service acts for.
	tokenHeader = "X-Harnessly-Token"
	titleMax    = 200
)

// docTemplates are the documents a person starts from; the app shows the same three.
var docTemplates = map[string]struct{ title, markdown string }{
	"meeting": {"Заметки встречи", "# Заметки встречи\n\n**Когда:** \n**Кто:** \n\n## Повестка\n\n- \n\n## Решения\n\n- \n\n## Что делаем дальше\n\n| Что | Кто | Когда |\n| --- | --- | --- |\n|  |  |  |\n"},
	"plan":    {"План работ", "# План работ\n\n> [!NOTE] Цель одним предложением.\n\n## Шаги\n\n1. \n2. \n3. \n\n## Риски\n\n- \n"},
	"review":  {"Разбор", "# Разбор\n\n## Что случилось\n\n## Почему\n\n## Что сделаем\n\n- \n"},
}

var (
	errNoManage = fail(http.StatusForbidden, "forbidden", "Удалить и вернуть из корзины может только владелец или администратор")
	errNoEdit   = fail(http.StatusForbidden, "forbidden", "Переименовать может тот, кто может править")
	errNoSpace  = fail(http.StatusForbidden, "forbidden", "Создавать можно только в своём пространстве")
)

// mountManage adds the routes above.
func (s *Service) mountManage(mux *http.ServeMux) {
	p := s.prefix
	mux.Handle("POST "+p, s.asPerson(s.createRoute))
	mux.Handle("GET "+p+"/_trash", s.asPerson(s.trashList))
	mux.Handle("POST "+p+"/{slug}/duplicate", s.asPerson(s.duplicateRoute))
	mux.Handle("PATCH "+p+"/{slug}", s.asPerson(s.renameRoute))
	mux.Handle("DELETE "+p+"/{slug}", s.asPerson(s.trashRoute))
	mux.Handle("POST "+p+"/{slug}/restore", s.asPerson(s.restoreRoute))
}

// asPerson is guard for a person signed in to the hub, or for a service that names the person it acts for.
func (s *Service) asPerson(fn func(http.ResponseWriter, *http.Request, *auth.Principal) error) http.Handler {
	person := s.guard(fn)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || r.Header.Get(tokenHeader) == "" || !s.serviceToken(strings.TrimSpace(got)) {
			person.ServeHTTP(w, r)
			return
		}
		if st := s.Status(); !st.Documents {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, s.notConfigured())
			return
		}
		p, err := s.principalOf(r.Context(), r.Header.Get(tokenHeader))
		if err == nil {
			err = fn(w, r, p)
		}
		if err != nil {
			s.answer(w, r, err)
		}
	})
}

// serviceToken says the bearer is one of the secrets the hub shares with the documents and boards services.
func (s *Service) serviceToken(got string) bool {
	for _, want := range []string{s.cfg.ArtifactsAccessToken, s.cfg.BoardEventsSecret} {
		if want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

// principalOf is the person whose platform token a service sends: signed by the hub for one of the services' origins,
// alive, of a user who is not switched off. Never an administrator: a service acts with the person's own rights only.
func (s *Service) principalOf(ctx context.Context, token string) (*auth.Principal, error) {
	if s.appToken == nil {
		return nil, httpx.ErrUnauthenticated
	}
	id := ""
	for _, aud := range s.serviceOrigins() {
		if sub, err := s.appToken(token, aud); err == nil && sub != "" {
			id = sub
			break
		}
	}
	if id == "" {
		return nil, httpx.ErrUnauthenticated
	}
	u, err := s.db.S().UserByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.DisabledAt != nil) {
		return nil, httpx.ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	return &auth.Principal{User: u}, nil
}

// serviceOrigins are the audiences a service's token for a person may name: the documents' and boards' origins.
func (s *Service) serviceOrigins() []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range []string{s.cfg.ArtifactsPublicURL, s.cfg.ArtifactsURL, s.cfg.BoardsPublicURL, s.cfg.BoardsURL} {
		u, err := url.Parse(raw)
		if raw == "" || err != nil || u.Scheme == "" || u.Host == "" {
			continue
		}
		o := u.Scheme + "://" + u.Host
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	return out
}

// homeSpace is the space a new thing goes to: the one asked for, when the person is in it, else the person's only one.
func (s *Service) homeSpace(ctx context.Context, p *auth.Principal, asked string) (string, error) {
	spaces, err := s.db.S().SpacesByUser(ctx, p.User.ID)
	if err != nil {
		return "", err
	}
	if asked != "" {
		for _, sp := range spaces {
			if sp.ID == asked {
				return asked, nil
			}
		}
		if p.Admin {
			if _, err := s.db.S().SpaceByID(ctx, asked); err == nil {
				return asked, nil
			}
		}
		return "", errNoSpace
	}
	switch len(spaces) {
	case 0:
		return "", errNoSpace
	case 1:
		return spaces[0].ID, nil
	}
	names := make([]string, 0, len(spaces))
	for _, sp := range spaces {
		names = append(names, sp.ID+" ("+sp.Name+")")
	}
	return "", fail(http.StatusBadRequest, "space_required", "Вы в нескольких пространствах: укажите spaceId — "+strings.Join(names, ", "))
}

type createIn struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
	Template string `json:"template"`
	SpaceID  string `json:"spaceId"`
}

func cleanTitle(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > titleMax {
		t = string(r[:titleMax])
	}
	return t
}

func (s *Service) createRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var in createIn
	if err := decode(r, &in); err != nil {
		return err
	}
	space, err := s.homeSpace(r.Context(), p, in.SpaceID)
	if err != nil {
		return err
	}
	in.Title = cleanTitle(in.Title)
	var slug string
	switch in.Kind {
	case kindBoard:
		if len([]rune(in.Title)) < 2 {
			return fail(http.StatusBadRequest, "invalid", "Название доски — хотя бы два символа")
		}
		slug, err = s.boardCall(r.Context(), "/b/api/boards", map[string]any{"title": in.Title, "template": in.Template, "author": displayName(p.User)})
	case kindArtifact, "":
		in.Kind = kindArtifact
		slug, err = s.newArtifact(r.Context(), p, in)
	default:
		return fail(http.StatusBadRequest, "invalid", "kind: artifact или board")
	}
	if err != nil {
		return err
	}
	return s.recordNew(w, r, p, slug, in.Kind, space, in.Title)
}

// newArtifact publishes a new document: the HTML or the Markdown given, a template, or an empty page with the title.
func (s *Service) newArtifact(ctx context.Context, p *auth.Principal, in createIn) (string, error) {
	pub := publishIn{Title: in.Title, Author: p.User.Email, New: true}
	switch {
	case in.Template != "":
		t, ok := docTemplates[in.Template]
		if !ok {
			return "", fail(http.StatusBadRequest, "invalid", "template: meeting, plan или review")
		}
		if pub.Title == "" {
			pub.Title = t.title
		}
		pub.Markdown = t.markdown
	case strings.TrimSpace(in.HTML) != "":
		pub.HTML = in.HTML
	case strings.TrimSpace(in.Markdown) != "":
		pub.Markdown = in.Markdown
	default:
		if pub.Title == "" {
			pub.Title = "Новый документ"
		}
		pub.Markdown = "# " + pub.Title + "\n"
	}
	out, err := s.art.publish(ctx, pub)
	if err != nil {
		return "", err
	}
	return out.Artifact.Slug, nil
}

// recordNew writes the hub's record of a new thing — owned by the person, in the space, edited by the space — and
// answers it as the list shows it.
func (s *Service) recordNew(w http.ResponseWriter, r *http.Request, p *auth.Principal, slug, kind, space, title string) error {
	ctx := r.Context()
	m := meta{Slug: slug, SpaceID: space, OwnerEmail: p.User.Email, OwnerName: displayName(p.User), Title: title, TeamLevel: levelEdit}
	if err := s.putMeta(ctx, m); err != nil {
		return err
	}
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET kind = ? WHERE slug = ?`, kind, slug); err != nil {
		return err
	}
	doc, err := s.one(ctx, p, slug)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"doc": doc})
	return nil
}

// one is a thing as the list shows it to the person.
func (s *Service) one(ctx context.Context, p *auth.Principal, slug string) (summary, error) {
	list, err := s.summaries(ctx, p, slug)
	if err != nil {
		return summary{}, err
	}
	if len(list) == 0 {
		return summary{}, errNoDoc
	}
	return list[0], nil
}

func (s *Service) duplicateRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	var in struct {
		Title   string `json:"title"`
		SpaceID string `json:"spaceId"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			return err
		}
	}
	space, err := s.homeSpace(r.Context(), p, in.SpaceID)
	if err != nil {
		return err
	}
	title := cleanTitle(in.Title)
	if title == "" {
		title = cleanTitle(doc.Title + " (копия)")
	}
	var slug string
	if doc.Kind == kindBoard {
		slug, err = s.boardCall(r.Context(), "/b/api/boards/"+url.PathEscape(doc.Slug)+"/copy", map[string]any{"title": title, "author": displayName(p.User)})
	} else {
		slug, err = s.copyArtifact(r.Context(), p, doc.Slug, title)
	}
	if err != nil {
		return err
	}
	return s.recordNew(w, r, p, slug, doc.Kind, space, title)
}

// copyArtifact publishes the current version of a document as a new one.
func (s *Service) copyArtifact(ctx context.Context, p *auth.Principal, slug, title string) (string, error) {
	var out struct {
		HTML     string `json:"html"`
		Markdown string `json:"markdown"`
	}
	if err := s.art.do(ctx, "GET", "/api/v1/artifacts/"+url.PathEscape(slug)+"/content?format=json", nil, &out); err != nil {
		return "", err
	}
	pub := publishIn{Title: title, Author: p.User.Email, New: true}
	if out.Markdown != "" {
		pub.Markdown = out.Markdown
	} else {
		pub.HTML = out.HTML
	}
	got, err := s.art.publish(ctx, pub)
	if err != nil {
		return "", err
	}
	return got.Artifact.Slug, nil
}

func (s *Service) renameRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, m, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	if s.rank(r.Context(), doc.Slug, m, doc.author, p, nil) < levelRank[levelEdit] {
		return errNoEdit
	}
	var in struct {
		Title string `json:"title"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	title := cleanTitle(in.Title)
	if len([]rune(title)) < 2 {
		return fail(http.StatusBadRequest, "invalid", "Название — хотя бы два символа")
	}
	ctx := r.Context()
	if doc.Kind == kindBoard {
		_, err = s.boardCall(ctx, "/b/"+url.PathEscape(doc.Slug)+"/meta", map[string]any{"title": title, "author": displayName(p.User)})
	} else {
		err = s.art.do(ctx, "PATCH", "/api/v1/artifacts/"+url.PathEscape(doc.Slug), map[string]any{"title": title}, nil)
	}
	if err != nil {
		return err
	}
	if m != nil {
		if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET title = ?, updated_at = ? WHERE slug = ? AND COALESCE(space_id, '') = ?`,
			title, s.now().UnixMilli(), m.Slug, m.SpaceID); err != nil {
			return err
		}
	}
	got, err := s.one(ctx, p, doc.Slug)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"doc": got})
	return nil
}

func (s *Service) trashRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, m, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	ctx := r.Context()
	if s.rank(ctx, doc.Slug, m, doc.author, p, nil) < rankManage {
		return errNoManage
	}
	if m == nil {
		// A thing the hub has no record of yet (only an administrator manages it): the trash needs one.
		if err := s.putMeta(ctx, meta{Slug: doc.Slug, Title: doc.Title, TeamLevel: levelView}); err != nil {
			return err
		}
	}
	out, err := s.toTrash(ctx, doc.Slug, doc.Kind, displayName(p.User))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// toTrash puts a document or a board into the trash, for a person or the agent of the owner's machine: the boards
// service hides a board, a document loses its public link, and the documents service learns of it (trashsync.go). The
// record must exist.
func (s *Service) toTrash(ctx context.Context, slug, kind, by string) (map[string]any, error) {
	if kind == kindBoard {
		if _, err := s.boardCall(ctx, "/b/api/boards/"+url.PathEscape(slug)+"/trash", map[string]any{"author": by}); err != nil {
			return nil, err
		}
	} else if _, public, err := s.art.access(ctx, slug); err != nil {
		return nil, err
	} else if public {
		// The public link is revoked, not hidden: taking the document out of the trash does not bring it back.
		if _, _, err := s.art.setPublic(ctx, slug, false); err != nil {
			return nil, err
		}
	}
	now := s.now().UnixMilli()
	// A document waits for the documents service to confirm its trash: until then the hub alone hides it.
	var sync any
	if kind != kindBoard {
		sync = now
	}
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET kind = ?, trashed_at = ?, trashed_by = ?, trash_sync = ?, updated_at = ? WHERE slug = ?`,
		kind, now, by, sync, now, slug); err != nil {
		return nil, err
	}
	out := map[string]any{"slug": slug, "kind": kind, "trashedAt": ts(now), "trashedBy": by}
	if kind != kindBoard {
		out["serviceConfirmed"] = s.tellTrash(ctx, slug, true, by, now)
	}
	return out, nil
}

// fromTrash takes a document or a board out of the trash; the public link of a document does not come back.
func (s *Service) fromTrash(ctx context.Context, m *meta, by string) error {
	if m.Kind == kindBoard {
		if _, err := s.boardCall(ctx, "/b/api/boards/"+url.PathEscape(m.Slug)+"/restore", map[string]any{"author": by}); err != nil {
			return err
		}
	}
	now := s.now().UnixMilli()
	var sync any
	if m.Kind != kindBoard {
		sync = now
	}
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET trashed_at = NULL, trashed_by = NULL, trash_sync = ?, updated_at = ? WHERE slug = ?`,
		sync, now, m.Slug); err != nil {
		return err
	}
	if m.Kind != kindBoard {
		s.tellTrash(ctx, m.Slug, false, by, now)
	}
	return nil
}

// trashed is the person's record of a thing in the trash they may take out: the owner's or, for an administrator, any.
func (s *Service) trashed(r *http.Request, p *auth.Principal) (*meta, error) {
	ctx := r.Context()
	m, _, err := s.meta(ctx, r.PathValue("slug"), s.reachOf(ctx, p))
	if err != nil {
		return nil, err
	}
	if m == nil || m.TrashedAt == 0 {
		return nil, errNoDoc
	}
	if !p.Admin && !strings.EqualFold(m.OwnerEmail, p.User.Email) {
		return nil, errNoManage
	}
	return m, nil
}

func (s *Service) restoreRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.trashed(r, p)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if err := s.fromTrash(ctx, m, displayName(p.User)); err != nil {
		return err
	}
	doc, err := s.one(ctx, p, m.Slug)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"doc": doc})
	return nil
}

// trashItem is a thing in the trash as the list shows it.
type trashItem struct {
	Slug      string `json:"slug"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Owner     string `json:"owner"`
	TrashedAt string `json:"trashedAt"`
	TrashedBy string `json:"trashedBy"`
}

func (s *Service) trashList(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	rc := s.reachOf(ctx, p)
	cond, args := "trashed_at IS NOT NULL AND "+rc.cond, rc.args
	if !p.Admin {
		cond += " AND LOWER(owner_email) = ?"
		args = append(append([]any{}, args...), strings.ToLower(p.User.Email))
	}
	rows, err := s.db.S().Q().Query(ctx, `SELECT `+metaCols+` FROM doc_meta WHERE `+cond+` ORDER BY trashed_at DESC LIMIT 500`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []trashItem{}
	for rows.Next() {
		m, err := scanMeta(rows)
		if err != nil {
			return err
		}
		out = append(out, trashItem{Slug: m.Slug, Kind: m.Kind, Title: m.Title, Owner: firstNonEmpty(m.OwnerName, m.OwnerEmail),
			TrashedAt: ts(m.TrashedAt), TrashedBy: m.TrashedBy})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"docs": out})
	return nil
}

// boardCall posts to the boards service with the hub's token and answers the slug the service names, if any.
func (s *Service) boardCall(ctx context.Context, path string, body any) (string, error) {
	if !s.Status().Boards {
		return "", fail(http.StatusServiceUnavailable, "unavailable", "Доски в этой установке не подключены")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.BoardsURL+path, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.BoardsToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.hc.Do(req)
	if err != nil {
		return "", &upstreamError{Status: http.StatusBadGateway}
	}
	defer func() { _ = res.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return "", &upstreamError{Status: res.StatusCode, Code: "boards", Text: boardError(data)}
	}
	var out struct {
		Slug string `json:"slug"`
	}
	_ = json.Unmarshal(data, &out)
	return out.Slug, nil
}

// boardError is the words of a refusal of the boards service.
func boardError(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return e.Error
	}
	return fmt.Sprintf("boards service: %d", len(data))
}
