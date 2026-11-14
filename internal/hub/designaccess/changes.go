package designaccess

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// GET <platform>/design/store-library/changes?since=<rev> holds the page's ask until the space's library in the
// hub's store has a revision past since (a project drawn or edited on any machine, a shelf moved), or LibraryWaitMax
// passes, and answers {"rev": <newest>}. The page reads the library again when rev grows, instead of asking on a timer.
// Behind hub-design-home: while it is off, 404 home_off and the page keeps its timer.

// LibraryWaitMax is how long the hub holds a long poll of the library.
const LibraryWaitMax = 25 * time.Second

// libraryWait is LibraryWaitMax; tests shorten it.
var libraryWait = LibraryWaitMax

// LibraryWaiter waits for the space's library in the store to move past a revision.
type LibraryWaiter interface {
	WaitLibrary(ctx context.Context, spaceID string, since int64) (int64, error)
}

func (s *Service) libraryChanges(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) {
	if s.homeOn == nil || !s.homeOn(r.Context(), normEmail(u.Email), m.ID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "home_off", "error": "Главная Синопии одним ответом выключена"})
		return
	}
	waiter, ok := s.view.(LibraryWaiter)
	if !ok || m.SpaceID == "" {
		s.writeError(w, fail("store_off", "Хранилище дизайнов не подключено"))
		return
	}
	since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if err != nil || since < 0 {
		since = 0
	}
	ctx, cancel := context.WithTimeout(r.Context(), libraryWait)
	defer cancel()
	rev, err := waiter.WaitLibrary(ctx, m.SpaceID, since)
	if err != nil {
		s.writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]int64{"rev": rev})
}
