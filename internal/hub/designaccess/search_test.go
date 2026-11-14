package designaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/searchindex"
)

func askSearch(t *testing.T, r *spaceRig, u store.User, m store.Machine, q string) (int, []searchindex.Hit) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/m/"+m.ID+"/api/platform/search?q="+q, nil)
	w := httptest.NewRecorder()
	if !r.svc.ServeMachine(w, req, u, m) {
		t.Fatal("the hub left the search to the machine")
	}
	var out struct {
		Hits []searchindex.Hit `json:"hits"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out.Hits
}

// the search is behind hub-search, finds a project of a machine that is off, and leaves out the project of a
// machine closed to the person.
func TestSearchFromTheIndexKeepsAccess(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "team-site", r.ownerU.Email); err != nil || !done {
		t.Fatalf("registerCreated: %v %v", done, err)
	}
	ix, _, err := searchindex.OpenSQLite(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []searchindex.Doc{
		{Space: r.owner.SpaceID, Kind: searchindex.KindDesign, ID: "team-site", Parent: "team-site", Title: "Сайт команды", UpdatedAt: 5},
		{Space: r.owner.SpaceID, Kind: searchindex.KindDesign, ID: "stranger", Parent: "stranger", Title: "Сайт чужой", UpdatedAt: 4},
		{Space: r.owner.SpaceID, Kind: searchindex.KindApp, ID: "crm", Title: "Сайт-конструктор", UpdatedAt: 3},
	} {
		if err := ix.Put(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	on := false
	r.svc.UseSearch(ix, func(context.Context, string, string) bool { return on })
	if code, _ := askSearch(t, r, r.mateU, r.mate, "сайт"); code != http.StatusNotFound {
		t.Fatalf("switch off: %d", code)
	}
	on = true
	ids := func(u store.User, m store.Machine) string {
		code, hits := askSearch(t, r, u, m, "сайт")
		if code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
		var s []string
		for _, h := range hits {
			s = append(s, h.Kind+":"+h.ID)
		}
		return strings.Join(s, ",")
	}
	// The machine holding the project is off (no agent serves it): the index still answers; a project with no row of
	// the hub is not the person's to see.
	if got := ids(r.mateU, r.mate); got != "design:team-site,app:crm" {
		t.Fatalf("open machine: %q", got)
	}
	r.svc.opens = func(_ context.Context, userID string, m store.Machine) bool {
		return m.ID != r.owner.ID || userID == r.ownerU.ID
	}
	if got := ids(r.mateU, r.mate); got != "app:crm" {
		t.Fatalf("a project of a private machine is found by a mate: %q", got)
	}
	if got := ids(r.ownerU, r.owner); got != "design:team-site,app:crm" {
		t.Fatalf("the owner: %q", got)
	}
}
