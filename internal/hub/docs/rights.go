package docs

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The documents service asks the hub what a person may do with documents: it knows the person from the platform's
// token (sub), and the hub keeps who has access. The service calls with its own secret (HUB_ARTIFACTS_ACCESS_TOKEN),
// one the hub uses for nothing else; without it set the route is not there.

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
	Sub   string   `json:"sub"`
	Slugs []string `json:"slugs"`
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
	want := s.cfg.ArtifactsAccessToken
	if want == "" {
		http.NotFound(w, r)
		return
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(want)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		s.answer(w, r, httpx.ErrUnauthenticated)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	var req rightsRequest
	if err != nil || json.Unmarshal(raw, &req) != nil || req.Sub == "" {
		s.answer(w, r, fail(http.StatusBadRequest, "invalid", "Нужны sub и slugs"))
		return
	}
	if len(req.Slugs) > maxRightsSlugs {
		s.answer(w, r, fail(http.StatusBadRequest, "invalid", "Не больше 200 документов за раз"))
		return
	}
	out, err := s.rightsOf(r, req)
	if err != nil {
		s.answer(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rights": out})
}

func (s *Service) rightsOf(r *http.Request, req rightsRequest) (map[string]string, error) {
	ctx := r.Context()
	out := make(map[string]string, len(req.Slugs))
	var slugs []string
	for _, slug := range req.Slugs {
		if _, seen := out[slug]; !seen && slug != "" {
			out[slug] = RightNone
			slugs = append(slugs, slug)
		}
	}
	if len(slugs) == 0 {
		return out, nil
	}
	u, err := s.db.S().UserByID(ctx, req.Sub)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if u.DisabledAt != nil {
		return out, nil
	}
	admin := false
	if s.auth != nil {
		if admin, err = s.auth.IsInstanceAdmin(ctx, u.ID); err != nil {
			return nil, err
		}
	}
	p := &auth.Principal{User: u, Admin: admin}
	metas, _, err := s.metas(ctx, slugs, s.reachOf(ctx, p))
	if err != nil {
		return nil, err
	}
	c := s.circleOf(p)
	for slug, m := range metas {
		out[slug] = rightOf(s.rank(ctx, slug, &m, "", p, c))
	}
	return out, nil
}
