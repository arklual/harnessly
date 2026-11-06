package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// failure is a refusal in the shape the web core reads: {error: words for the person, code}.
type failure struct {
	status int
	code   string
	text   string
}

func (f *failure) Error() string { return f.code + ": " + f.text }

func fail(status int, code, text string) *failure { return &failure{status, code, text} }

var (
	errNoAccess  = fail(http.StatusForbidden, "forbidden", "У вас нет доступа к этому документу")
	errNoComment = fail(http.StatusForbidden, "forbidden", "Комментировать этот документ нельзя")
	errNoDoc     = fail(http.StatusNotFound, "not_found", "Документ не найден")
)

func (s *Service) answer(w http.ResponseWriter, r *http.Request, err error) {
	var f *failure
	var he *httpx.Error
	var up *upstreamError
	switch {
	case errors.As(err, &f):
	case errors.As(err, &he):
		f = fail(he.Status, he.Code, he.Message)
	case errors.As(err, &up) && up.Status == http.StatusNotFound:
		f = errNoDoc
	case errors.As(err, &up) && up.Status == http.StatusConflict:
		f = fail(http.StatusConflict, "version_conflict", "Документ уже изменили: откройте его заново и повторите")
	case errors.As(err, &up) && up.Status >= 400 && up.Status < 500:
		f = fail(http.StatusBadRequest, "invalid", firstNonEmpty(up.Text, "Сервис документов не принял запрос"))
	case errors.As(err, &up) || isNetwork(err):
		s.log.Warn("documents service unavailable", "route", httpx.Route(r), "error", err)
		f = fail(http.StatusBadGateway, "docs_unavailable", "Сервис документов не отвечает")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		s.log.Error("documents request failed", "route", httpx.Route(r), "error", err)
		f = fail(http.StatusInternalServerError, "internal", "Что-то пошло не так")
	}
	httpx.WriteJSON(w, f.status, map[string]any{"error": f.text, "code": f.code})
}

func isNetwork(err error) bool {
	var ue *url.Error
	return errors.As(err, &ue)
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}

// guard serves a route for a signed-in person, and answers 503 docs_not_configured while documents are off.
func (s *Service) guard(fn func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.auth.Authenticate(r)
		if err == nil && p == nil {
			err = httpx.ErrUnauthenticated
		}
		if err != nil {
			s.answer(w, r, err)
			return
		}
		if st := s.Status(); !st.Documents {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, s.notConfigured())
			return
		}
		if err := fn(w, r, p); err != nil {
			s.answer(w, r, err)
		}
	})
}

// Mount adds the routes under the platform API prefix.
func (s *Service) Mount(mux *http.ServeMux) {
	p := s.prefix
	mux.Handle("GET "+p+"/_status", http.HandlerFunc(s.status))
	mux.Handle("GET "+p, s.guard(s.list))
	mux.Handle("GET "+p+"/people", s.guard(s.peopleRoute))
	mux.Handle("GET "+p+"/{slug}", s.guard(s.detail))
	mux.Handle("POST "+p+"/{slug}/threads", s.guard(s.newThread))
	mux.Handle("POST "+p+"/{slug}/threads/{id}/comments", s.guard(s.reply))
	mux.Handle("POST "+p+"/{slug}/threads/{id}/status", s.guard(s.threadStatus))
	mux.Handle("POST "+p+"/{slug}/read", s.guard(s.read))
	mux.Handle("GET "+p+"/{slug}/content", s.guard(s.content))
	mux.Handle("GET "+p+"/{slug}/versions", s.guard(s.versions))
	mux.Handle("GET "+p+"/{slug}/diff", s.guard(s.diffRoute))
	mux.Handle("GET "+p+"/{slug}/access", s.guard(s.getAccess))
	mux.Handle("POST "+p+"/{slug}/access", s.guard(s.postAccess))
	mux.Handle("GET "+p+"/{slug}/source", s.guard(s.source))
	mux.Handle("PUT "+p+"/{slug}/source", s.guard(s.saveSource))
	mux.Handle("PUT "+p+"/{slug}/source/block", s.guard(func(http.ResponseWriter, *http.Request, *auth.Principal) error {
		return fail(http.StatusNotImplemented, "not_supported", "Правка блока на месте в этой установке не поддерживается: правьте Markdown целиком")
	}))
}

// status tells the page whether documents work and why not; it needs a sign-in and gives nothing away to a stranger.
func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	p, err := s.auth.Authenticate(r)
	if err != nil || p == nil {
		s.answer(w, r, firstErr(err, httpx.ErrUnauthenticated))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.Status())
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func decode(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fail(http.StatusBadRequest, "invalid", "Не удалось разобрать запрос")
	}
	return nil
}

// summary of one document for the list and the header of the viewer.
type summary struct {
	Slug           string  `json:"slug"`
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	URL            string  `json:"url"`
	Bound          bool    `json:"bound"`
	Owner          *author `json:"owner"`
	MachineID      *string `json:"machineId"`
	SourcePath     *string `json:"sourcePath"`
	OpenThreads    int     `json:"openThreads"`
	Unread         int     `json:"unread"`
	LastActivityAt string  `json:"lastActivityAt"`
}

type board struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	UpdatedAt any    `json:"updatedAt"`
}

func (s *Service) boards(ctx context.Context) ([]board, error) {
	if !s.Status().Boards {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.BoardsURL+"/b/api/boards", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.BoardsToken)
	res, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, &upstreamError{Status: res.StatusCode}
	}
	var out struct {
		Boards []board `json:"boards"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out.Boards, nil
}

func (s *Service) contentPath(slug string) string {
	return s.prefix + "/" + url.PathEscape(slug) + "/content"
}

func (s *Service) summaries(ctx context.Context, p *auth.Principal, only string) ([]summary, error) {
	type item struct {
		slug, kind, title, url, updated string
	}
	var items []item
	arts, err := s.art.list(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range arts {
		if only == "" || only == a.Slug {
			items = append(items, item{a.Slug, "artifact", a.Title, s.contentPath(a.Slug), a.UpdatedAt})
		}
	}
	if bs, err := s.boards(ctx); err != nil {
		s.log.Warn("boards service unavailable", "error", err)
	} else {
		for _, b := range bs {
			if only == "" || only == b.Slug {
				items = append(items, item{b.Slug, "board", b.Title, b.URL, timeOf(b.UpdatedAt)})
			}
		}
	}
	slugs := make([]string, len(items))
	for i, it := range items {
		slugs[i] = it.slug
	}
	metas, err := s.metas(ctx, slugs)
	if err != nil {
		return nil, err
	}
	open, unread, last, err := s.counts(ctx, p.User.Email, slugs)
	if err != nil {
		return nil, err
	}
	out := []summary{}
	for _, it := range items {
		var mp *meta
		if m, ok := metas[it.slug]; ok {
			mp = &m
		}
		if s.rank(mp, p) < levelRank[levelView] {
			continue
		}
		sm := summary{Slug: it.slug, Kind: it.kind, Title: it.title, URL: it.url, OpenThreads: open[it.slug], Unread: unread[it.slug], LastActivityAt: it.updated}
		if mp != nil {
			sm.Bound = mp.MachineID != ""
			sm.Owner = &author{Kind: "person", Email: mp.OwnerEmail, Name: firstNonEmpty(mp.OwnerName, mp.OwnerEmail)}
			if mp.MachineID != "" {
				sm.MachineID = &mp.MachineID
			}
			if mp.SourcePath != "" {
				sm.SourcePath = &mp.SourcePath
			}
		}
		if ms := last[it.slug]; ms > 0 {
			if t := ts(ms); t > sm.LastActivityAt {
				sm.LastActivityAt = t
			}
		}
		if sm.LastActivityAt == "" {
			sm.LastActivityAt = ts(0)
		}
		out = append(out, sm)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastActivityAt > out[j].LastActivityAt })
	return out, nil
}

func timeOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return ts(int64(x))
	}
	return ""
}

func (s *Service) list(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	docs, err := s.summaries(r.Context(), p, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"docs": docs})
	return nil
}

// open finds a document and the person's rank on it.
func (s *Service) open(r *http.Request, p *auth.Principal, need int) (summary, *meta, error) {
	slug := r.PathValue("slug")
	m, err := s.meta(r.Context(), slug)
	if err != nil {
		return summary{}, nil, err
	}
	if s.rank(m, p) < need {
		if s.rank(m, p) < levelRank[levelView] {
			return summary{}, nil, errNoAccess
		}
		return summary{}, nil, errNoComment
	}
	list, err := s.summaries(r.Context(), p, slug)
	if err != nil {
		return summary{}, nil, err
	}
	if len(list) == 0 {
		return summary{}, nil, errNoDoc
	}
	return list[0], m, nil
}

func (s *Service) detail(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	th, err := s.threads(r.Context(), doc.Slug)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"doc": doc, "threads": th})
	return nil
}

func (s *Service) peopleRoute(w http.ResponseWriter, r *http.Request, _ *auth.Principal) error {
	ppl, err := s.people(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"people": ppl})
	return nil
}

func (s *Service) newThread(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelComment])
	if err != nil {
		return err
	}
	var in struct {
		Anchor json.RawMessage `json:"anchor"`
		Body   string          `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	body := strings.TrimSpace(in.Body)
	if body == "" || len(body) > 20000 {
		return fail(http.StatusBadRequest, "invalid", "Комментарий пустой или слишком длинный")
	}
	if len(in.Anchor) == 0 || !bytes.HasPrefix(bytes.TrimSpace(in.Anchor), []byte("{")) {
		in.Anchor = json.RawMessage(`{"quote":"","prefix":"","suffix":""}`)
	}
	id, err := s.addThread(r.Context(), doc.Slug, in.Anchor, personOf(p), body)
	if err != nil {
		return err
	}
	return s.threadAnswer(w, r, doc.Slug, id, http.StatusCreated)
}

func (s *Service) threadAnswer(w http.ResponseWriter, r *http.Request, slug, id string, status int) error {
	t, err := s.thread(r.Context(), slug, id)
	if err != nil {
		return err
	}
	if t == nil {
		return fail(http.StatusNotFound, "not_found", "Обсуждение не найдено")
	}
	httpx.WriteJSON(w, status, map[string]any{"thread": t})
	return nil
}

func (s *Service) reply(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelComment])
	if err != nil {
		return err
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	body := strings.TrimSpace(in.Body)
	if body == "" || len(body) > 20000 {
		return fail(http.StatusBadRequest, "invalid", "Комментарий пустой или слишком длинный")
	}
	id := r.PathValue("id")
	if t, err := s.thread(r.Context(), doc.Slug, id); err != nil {
		return err
	} else if t == nil {
		return fail(http.StatusNotFound, "not_found", "Обсуждение не найдено")
	}
	if err := s.addComment(r.Context(), id, personOf(p), body); err != nil {
		return err
	}
	return s.threadAnswer(w, r, doc.Slug, id, http.StatusOK)
}

func (s *Service) threadStatus(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelComment])
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.Status != "open" && in.Status != "resolved" {
		return fail(http.StatusBadRequest, "invalid", "Статус: open или resolved")
	}
	id := r.PathValue("id")
	if t, err := s.thread(r.Context(), doc.Slug, id); err != nil {
		return err
	} else if t == nil {
		return fail(http.StatusNotFound, "not_found", "Обсуждение не найдено")
	}
	if err := s.setStatus(r.Context(), id, in.Status); err != nil {
		return err
	}
	return s.threadAnswer(w, r, doc.Slug, id, http.StatusOK)
}

func (s *Service) read(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	if err := s.markRead(r.Context(), doc.Slug, p.User.Email); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// content serves the rendered page of a version with the hub's frame script added; the page runs in a sandbox without
// the origin of the hub, and the token of the documents service never reaches the browser.
func (s *Service) content(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	if doc.Kind == "board" {
		http.Redirect(w, r, doc.URL, http.StatusFound)
		return nil
	}
	res, err := s.art.page(r.Context(), doc.Slug, r.URL.Query().Get("v"), themeOf(r.URL.Query().Get("theme")))
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	page, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "sandbox allow-scripts allow-popups")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(withFrame(page))
	return nil
}

func (s *Service) versions(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	if doc.Kind == "board" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"versions": []any{}})
		return nil
	}
	_, vs, err := s.art.get(r.Context(), doc.Slug)
	if err != nil {
		return err
	}
	type v struct {
		N         int     `json:"n"`
		ID        string  `json:"id"`
		SHA       string  `json:"sha"`
		CreatedAt string  `json:"createdAt"`
		Author    *string `json:"author"`
		Note      *string `json:"note"`
		Current   bool    `json:"current"`
	}
	out := make([]v, 0, len(vs))
	for _, x := range vs {
		e := v{N: x.N, ID: x.ID, SHA: x.ID, CreatedAt: x.CreatedAt, Current: x.Current}
		if x.Author != "" {
			a := x.Author
			e.Author = &a
		}
		if x.Note != "" {
			n := x.Note
			e.Note = &n
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].N > out[j].N })
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"versions": out})
	return nil
}

func (s *Service) diffRoute(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if doc.Kind == "board" || from == "" || to == "" {
		return fail(http.StatusBadRequest, "invalid", "Укажите две версии документа")
	}
	raw, err := s.art.diff(r.Context(), doc.Slug, from, to)
	if err != nil {
		return err
	}
	var d struct {
		From  struct{ ID string } `json:"from"`
		To    struct{ ID string } `json:"to"`
		Stats struct {
			Added   int `json:"added"`
			Removed int `json:"removed"`
		} `json:"stats"`
		Blocks json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return err
	}
	if len(d.Blocks) == 0 {
		d.Blocks = json.RawMessage("[]")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"from": firstNonEmpty(d.From.ID, from), "to": firstNonEmpty(d.To.ID, to), "blocks": d.Blocks,
		"stats": map[string]int{"added": d.Stats.Added, "removed": d.Stats.Removed}})
	return nil
}

func (s *Service) accessBody(publicURL string, public bool) map[string]any {
	var u any
	if public && publicURL != "" {
		u = publicURL
	}
	return map[string]any{"public": public, "publicUrl": u}
}

func (s *Service) getAccess(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	if doc.Kind == "board" {
		httpx.WriteJSON(w, http.StatusOK, s.accessBody("", false))
		return nil
	}
	u, pub, err := s.art.access(r.Context(), doc.Slug)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s.accessBody(u, pub))
	return nil
}

// postAccess opens or closes the public link; only the owner and the administrators do.
func (s *Service) postAccess(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, rankManage)
	if err != nil {
		return err
	}
	var in struct {
		Public bool `json:"public"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if doc.Kind == "board" {
		return fail(http.StatusNotImplemented, "not_supported", "Публичная ссылка на доску не поддерживается")
	}
	u, pub, err := s.art.setPublic(r.Context(), doc.Slug, in.Public)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s.accessBody(u, pub))
	return nil
}

func (s *Service) source(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, m, err := s.open(r, p, levelRank[levelView])
	if err != nil {
		return err
	}
	can := s.rank(m, p) >= levelRank[levelEdit]
	if doc.Kind == "board" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"mode": nil, "markdown": nil, "version": 0, "canEdit": false})
		return nil
	}
	md, n, ok, err := s.art.markdown(r.Context(), doc.Slug, "")
	if err != nil {
		return err
	}
	if !ok {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"mode": nil, "markdown": nil, "version": n, "canEdit": false})
		return nil
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"mode": "markdown", "markdown": md, "version": n, "canEdit": can})
	return nil
}

func (s *Service) saveSource(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	doc, _, err := s.open(r, p, levelRank[levelEdit])
	if err != nil {
		return err
	}
	var in struct {
		Markdown    string `json:"markdown"`
		BaseVersion int    `json:"baseVersion"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if doc.Kind == "board" || strings.TrimSpace(in.Markdown) == "" {
		return fail(http.StatusBadRequest, "invalid", "Нечего сохранять")
	}
	base := in.BaseVersion
	out, err := s.art.publish(r.Context(), publishIn{Title: doc.Title, Markdown: in.Markdown, Slug: &doc.Slug, Author: p.User.Email, BaseVersion: &base})
	if err != nil {
		return err
	}
	s.touch(r.Context(), doc.Slug)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"version": out.Version.N})
	return nil
}

func (s *Service) touch(ctx context.Context, slug string) {
	if _, err := s.db.S().Q().Exec(ctx, `UPDATE doc_meta SET updated_at = ? WHERE slug = ?`, s.now().UnixMilli(), slug); err != nil {
		s.log.Warn("doc_meta touch failed", "error", err)
	}
}
