package docs

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The documents and the boards service ask the hub what a person may do with documents and boards: they know the
// person from the platform's token (sub) or, the boards service, from its own sign-in (a proved e-mail), and the hub
// keeps who has access. The documents service calls with its own secret (HUB_ARTIFACTS_ACCESS_TOKEN), the boards
// service with the secret of its comment events (HUB_BOARD_EVENTS_SECRET); without either the route is not
// there.

// maxRightsSlugs bounds one question.
const maxRightsSlugs = 200

// Rights a person has on a document, as the service is told.
const (
	RightNone    = "none"
	RightView    = "view"
	RightComment = "comment"
	RightEdit    = "edit"
)

type rightsRequest struct {
	Sub string `json:"sub"`
	// Email names the person when the service knows no hub id: the boards service's own sign-in. Only an
	// address a person of the hub has proved names them.
	Email string   `json:"email"`
	Slugs []string `json:"slugs"`
	// Authors is the author each document names, as the documents service knows it. A document the hub has no record
	// of is decided by it, the way the hub's own list decides it (summaries): without it such a document stays closed.
	Authors map[string]string `json:"authors"`
}

// serviceSecret checks the bearer a documents or boards service sends (serviceToken, manage.go).
func (s *Service) serviceSecret(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.serviceToken(strings.TrimSpace(got))
}

// rightOf names a rank: manage reads as edit, the most a service needs to know.
func rightOf(rank int) string {
	switch {
	case rank >= levelRank[levelEdit]:
		return RightEdit
	case rank >= levelRank[levelComment]:
		return RightComment
	case rank >= levelRank[levelView]:
		return RightView
	}
	return RightNone
}

// rights is POST <p>/_rights: {"sub", "slugs"} → {"rights": {slug: none|view|comment|edit}}, by the same model as the
// person's own page (rank). A document the hub has no record of, or one out of the person's reach, is none; so is every
// document of a person the hub does not know or has disabled.
func (s *Service) rights(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ArtifactsAccessToken == "" && s.cfg.BoardEventsSecret == "" {
		http.NotFound(w, r)
		return
	}
	if !s.serviceSecret(r) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		s.answer(w, r, httpx.ErrUnauthenticated)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	var req rightsRequest
	if err != nil || json.Unmarshal(raw, &req) != nil || (req.Sub == "" && req.Email == "") {
		s.answer(w, r, fail(http.StatusBadRequest, "invalid", "Нужны sub или email и slugs"))
		return
	}
	if len(req.Slugs) > maxRightsSlugs {
		s.answer(w, r, fail(http.StatusBadRequest, "invalid", "Не больше 200 документов за раз"))
		return
	}
	out, known, err := s.rightsOf(r, req)
	if err != nil {
		s.answer(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rights": out, "known": known})
}

// provedUser is the person who has proved the address, "" when nobody has (or two people claim it).
func (s *Service) provedUser(r *http.Request, email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", nil
	}
	rows, err := s.db.S().Q().Query(r.Context(), `SELECT DISTINCT user_id FROM identities WHERE lower(email) = ? AND email_verified = 1`, email)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) != 1 {
		return "", err
	}
	return ids[0], nil
}

// rightsOf answers each slug's right and the slugs the hub keeps a record of (known): a service applies the hub's word
// to those and keeps its own rules for what the hub has never heard of.
func (s *Service) rightsOf(r *http.Request, req rightsRequest) (map[string]string, []string, error) {
	ctx := r.Context()
	out := make(map[string]string, len(req.Slugs))
	known := []string{}
	var slugs []string
	for _, slug := range req.Slugs {
		if _, seen := out[slug]; !seen && slug != "" {
			out[slug] = RightNone
			slugs = append(slugs, slug)
		}
	}
	if len(slugs) == 0 {
		return out, known, nil
	}
	all, elsewhere, err := s.metas(ctx, slugs, reachAll())
	if err != nil {
		return nil, nil, err
	}
	// A board without a record whose access somebody has set is known too: the boards service then does what its
	// people and links say, not its own rules.
	var shared []string
	for _, slug := range slugs {
		if _, ok := all[slug]; ok || elsewhere[slug] {
			known = append(known, slug)
			continue
		}
		if s.access == nil {
			continue
		}
		stored, err := s.access.Stored(ctx, refOf(slug, nil))
		if err != nil {
			return nil, nil, err
		}
		if stored {
			known = append(known, slug)
			shared = append(shared, slug)
		}
	}
	sub := req.Sub
	if sub == "" {
		if sub, err = s.provedUser(r, req.Email); err != nil {
			return nil, nil, err
		}
		if sub == "" {
			return out, known, nil
		}
	}
	u, err := s.db.S().UserByID(ctx, sub)
	if errors.Is(err, store.ErrNotFound) {
		return out, known, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if u.DisabledAt != nil {
		return out, known, nil
	}
	admin := false
	if s.auth != nil {
		if admin, err = s.auth.IsInstanceAdmin(ctx, u.ID); err != nil {
			return nil, nil, err
		}
	}
	p := &auth.Principal{User: u, Admin: admin}
	metas, away, err := s.metas(ctx, slugs, s.reachOf(ctx, p))
	if err != nil {
		return nil, nil, err
	}
	c := s.circleOf(p)
	for slug, m := range metas {
		if m.TrashedAt != 0 {
			continue // in the trash: none
		}
		out[slug] = rightOf(s.rank(ctx, slug, &m, "", p, c))
	}
	// A document the hub has no record of anywhere: its author decides, as in the hub's own list, so the documents app
	// shows a person what the hub's list shows them.
	for _, slug := range slugs {
		if _, ok := all[slug]; ok || elsewhere[slug] || away[slug] {
			continue
		}
		if a := strings.TrimSpace(req.Authors[slug]); a != "" {
			out[slug] = rightOf(s.rank(ctx, slug, nil, a, p, c))
		}
	}
	for _, slug := range shared {
		out[slug] = rightOf(s.rank(ctx, slug, nil, strings.TrimSpace(req.Authors[slug]), p, c))
	}
	return out, known, nil
}
