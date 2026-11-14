package designaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// HRN-1418, stage 1 of «the hub is the source of truth». Sinopia's home in one answer (GET <platform>/design/home) is
// the machine's: it asks itself for the library, the shelves, the systems, the kits and the apps' documents side by side
// (cmd/runtime/routes_design_home.go). Through the hub a slow or sleeping machine kept the page empty: the page waited
// MACHINE_WAIT_MS for it before it asked the hub's copy. Now the hub asks the machine and its own store at once; the
// machine's answer goes to the page when it comes within HomeMachineBudget, otherwise the store's copy of the library
// does, at once, with `from: "store"`, and the page reads the other parts by their own routes, as before. The page puts
// the machine's library over the copy once it comes.

// HomeMachineBudget is how long the hub waits for the machine's home before it answers with its store's copy.
const HomeMachineBudget = 800 * time.Millisecond

// homeMax caps the machine's home the hub reads into memory.
const homeMax = 16 << 20

// homeBudget is HomeMachineBudget; tests shorten it.
var homeBudget = HomeMachineBudget

// homeFromStore is the hub's answer from its store: the parts it has, the ones it has not are left to their routes.
type homeFromStore struct {
	Parts  map[string]any `json:"parts"`
	Failed []any          `json:"failed"`
	From   string         `json:"from"`
	TookMs int64          `json:"tookMs"`
}

// HomeFromHeader names who answered the home: "machine" or "store".
const HomeFromHeader = "X-Harnessly-Home-From"

// ServeHome answers GET <platform>/design/home: 404 while the switch is off for the person, else the machine's answer
// or, when the machine is slow, off or answers no home, the store's copy. ask sends the request to the machine with
// the given context, as the proxy would; false when the request is not the home.
func (s *Service) ServeHome(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, ask func(ctx context.Context, w http.ResponseWriter)) bool {
	if r.Method != http.MethodGet {
		return false
	}
	p := strings.TrimPrefix(r.URL.EscapedPath(), "/m/"+m.ID)
	if !homePath.MatchString(p) {
		return false
	}
	if s.homeOn == nil || !s.homeOn(r.Context(), normEmail(u.Email), m.ID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "home_off", "error": "Главная Синопии одним ответом выключена"})
		return true
	}
	start := time.Now()
	ctx := r.Context()
	// The machine's request lives as long as the page's: past the budget its answer is still taken if it comes before
	// the store's copy.
	machine := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		ask(ctx, rec)
		machine <- rec
	}()
	type fromStore struct {
		lib storeLibrary
		err error
	}
	stored := make(chan fromStore, 1)
	go func() {
		lib, err := s.storeLibraryOf(ctx, u, m)
		stored <- fromStore{lib, err}
	}()
	budget := time.NewTimer(homeBudget)
	defer budget.Stop()
	// The store's copy is taken once the budget is spent or the machine answered no home; until then only the machine's.
	var fromStoreC chan fromStore
	var got fromStore
wait:
	for {
		select {
		case rec := <-machine:
			if s.machineHome(w, rec) {
				return true
			}
			machine, fromStoreC = nil, stored
		case <-budget.C:
			fromStoreC = stored
		case got = <-fromStoreC:
			if got.err != nil && machine != nil {
				// No copy in the store: the machine's answer, however late, is all there is.
				fromStoreC = nil
				continue
			}
			break wait
		case <-ctx.Done():
			return true
		}
	}
	if got.err != nil {
		// Neither side has a home: the page reads every part by its own route. Not 404, which would turn the home off.
		w.Header().Set(HomeFromHeader, "none")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "home_unavailable", "error": "Главная Синопии сейчас недоступна"})
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set(HomeFromHeader, "store")
	writeJSON(w, http.StatusOK, homeFromStore{
		Parts:  map[string]any{"storeLibrary": got.lib},
		Failed: []any{},
		From:   "store",
		TookMs: time.Since(start).Milliseconds(),
	})
	return true
}

// machineHome writes the machine's home when it is one: a 200 with JSON, not too big.
func (s *Service) machineHome(w http.ResponseWriter, rec *httptest.ResponseRecorder) bool {
	if rec.Code != http.StatusOK || rec.Body.Len() > homeMax || !json.Valid(rec.Body.Bytes()) {
		return false
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set(HomeFromHeader, "machine")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rec.Body.Bytes())
	return true
}
