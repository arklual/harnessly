package docs

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
)

// A service that serves a guest link itself (the boards service, HRN-1519: a board's link opens on the board, where a
// guest with «комментарии» comments) asks the hub what the link allows. The link is the hub's: the hub made it, keeps
// its term and can switch it off, so the service asks on every opening and keeps the answer only briefly.

type guestRequest struct {
	Token string `json:"token"`
	Slug  string `json:"slug"`
	// Open counts the question as an opening of the link (the page), not a call of the page that follows it.
	Open bool `json:"open"`
}

// guestRoute is POST <p>/_guest {token, slug, open?} with the service's secret →
// {slug, rights: view|comment, forWhom, linkId, expiresAt}. A token nobody made, one for another slug, one switched off
// or run out, and a thing in the trash are all 404: the service treats them alike, as a link that opens nothing.
func (s *Service) guestRoute(w http.ResponseWriter, r *http.Request) {
	if s.access == nil || (s.cfg.ArtifactsAccessToken == "" && s.cfg.BoardEventsSecret == "") {
		http.NotFound(w, r)
		return
	}
	if !s.serviceSecret(r) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		s.answer(w, r, httpx.ErrUnauthenticated)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<10))
	var req guestRequest
	if err != nil || json.Unmarshal(raw, &req) != nil || req.Token == "" || req.Slug == "" {
		s.answer(w, r, fail(http.StatusBadRequest, "invalid", "Нужны token и slug"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	gone := fail(http.StatusNotFound, "not_found", "Ссылка не найдена")
	g, err := s.access.Resolve(r.Context(), req.Token)
	if err != nil {
		var e *resaccess.Error
		if errors.As(err, &e) {
			s.answer(w, r, gone)
			return
		}
		s.answer(w, r, err)
		return
	}
	if g.Kind != resaccess.KindDoc || g.ID != req.Slug {
		s.answer(w, r, gone)
		return
	}
	metas, _, err := s.metas(r.Context(), []string{g.ID}, reachSpace(g.Space))
	if err != nil {
		s.answer(w, r, err)
		return
	}
	if m, ok := metas[g.ID]; ok && m.TrashedAt != 0 {
		s.answer(w, r, gone)
		return
	}
	rights := RightView
	if g.Rights == RightComment {
		rights = RightComment
	}
	if req.Open {
		s.access.RecordOpen(r.Context(), g.LinkID)
	}
	var expires any
	if g.Expires != nil {
		expires = g.Expires.UTC().Format("2006-01-02T15:04:05Z")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"slug": g.ID, "rights": rights, "forWhom": g.Title, "linkId": g.LinkID, "target": g.Target, "expiresAt": expires,
	})
}

// boardLinkURL is where a guest link to a board opens: the board on the boards service, /b/<slug>?g=<token>, where the
// service asks _guest what the link allows. "" — the hub's own page — for anything else, and while the boards service
// cannot ask (no secret for it, no public address).
func (s *Service) boardLinkURL(ref resaccess.Ref, target, token string) string {
	if ref.Kind != resaccess.KindDoc || target != targetBoard || s.cfg.BoardEventsSecret == "" || s.cfg.BoardsPublicURL == "" {
		return ""
	}
	return strings.TrimRight(s.cfg.BoardsPublicURL, "/") + "/b/" + url.PathEscape(ref.ID) + "?g=" + url.QueryEscape(token)
}
