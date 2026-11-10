package designaccess

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// HRN-881. The editor's saves of a deck or a video (<platform api>/design/{deck,video}[/...], EditRoutes)
// are the hub's when the project is recorded and in the store: the hub checks the person's role in its own tables and
// saves the edit as a revision, whether or not the machine the page runs over is on. A read of the deck's or the
// video's state stays the machine's while it is on (it knows the recordings and the exports that run there); with the
// machine off the hub reads it from the store. Everything else, and a project the store has nothing of, goes to the
// machine as before.

var editPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/design/((?:deck|video)(?:/[a-z]+)?)$`)

// editBodyMax caps what the hub reads of an edit (a slide's patch is the largest); a larger body goes to the machine.
const editBodyMax = 4 << 20

// editNeed is the least role an edit route takes with a method; false for a method the hub leaves to the machine.
func editNeed(route, method string) (string, bool) {
	read := method == http.MethodGet || method == http.MethodHead
	switch {
	case route == "deck" || route == "video":
		return roleView, read
	case strings.HasSuffix(route, "/comments"), route == "video/scene" && (method == http.MethodPost || method == http.MethodPatch):
		return roleComment, method == http.MethodPost || method == http.MethodPatch
	default:
		return roleEdit, method == http.MethodPut || method == http.MethodPost || method == http.MethodPatch
	}
}

func (s *Service) serveEdit(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) bool {
	if s.view == nil || m.SpaceID == "" {
		return false
	}
	match := editPath.FindStringSubmatch(strings.TrimPrefix(r.URL.EscapedPath(), "/m/"+m.ID))
	if match == nil || !EditRoutes[match[1]] {
		return false
	}
	route := match[1]
	need, ok := editNeed(route, r.Method)
	if !ok {
		return false
	}
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	if read && s.reg != nil && s.reg.Get(m.ID) != nil {
		return false
	}
	project := r.URL.Query().Get("artifact")
	var raw []byte
	if !read {
		var err error
		raw, err = io.ReadAll(io.LimitReader(r.Body, editBodyMax+1))
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
		if err != nil || len(raw) > editBodyMax {
			return false
		}
		var b struct {
			Artifact string `json:"artifact"`
		}
		_ = json.Unmarshal(raw, &b)
		project = b.Artifact
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	if !projectIDRe.MatchString(project) {
		return false
	}
	ctx := r.Context()
	t, err := s.read(ctx, m)
	if err != nil {
		return false
	}
	p, stored, err := t.effective(project)
	if err != nil || !stored {
		return false
	}
	head, err := s.view.Head(ctx, m.SpaceID, project)
	if err != nil || head == 0 {
		return false
	}
	f, err := t.fold(p, u.Email)
	if err != nil {
		return false
	}
	if f.Role == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Макета больше нет"})
		return true
	}
	if rank(f.Role) < rank(need) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Нет доступа к проекту", "code": "no_access", "role": f.Role})
		return true
	}
	in := r.Clone(ctx)
	in.Body = io.NopCloser(bytes.NewReader(raw))
	err = s.view.Edit(ctx, w, in, m.SpaceID, project, normEmail(u.Email), route)
	switch {
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Проект только что изменили: обновите его и повторите правку", "code": "conflict"})
	case err != nil:
		s.log.Warn("design: an edit not saved through the hub", "project", project, "route", route, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Правка не сохранилась: попробуйте ещё раз", "code": "unavailable"})
	}
	return true
}
