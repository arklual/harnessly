package designaccess

import (
	"context"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// coverLibrary is a fakeLibrary that also keeps covers.
type coverLibrary struct {
	fakeLibrary
	covers map[string][]byte
}

func (c *coverLibrary) CoverVersions(context.Context, string) (map[string]string, error) {
	out := map[string]string{}
	for id := range c.covers {
		out[id] = strings.Repeat("a", 64)
	}
	return out, nil
}
func (c *coverLibrary) Cover(_ context.Context, _, project string, _ int64) ([]byte, error) {
	return c.covers[project], nil
}

// the cover of a project is given from the store under the library's access: the owner gets the address and
// the picture, a person the project is closed to gets neither.
func TestCoverFromTheStoreKeepsAccess(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "secret", r.ownerU.Email); err != nil || !done {
		t.Fatal(done, err)
	}
	if err := r.svc.write(ctx, r.owner, func(t *txn) error { return t.setTeam("secret", r.ownerU.Email, levelNone) }); err != nil {
		t.Fatal(err)
	}
	lib := &coverLibrary{fakeLibrary: fakeLibrary{items: []LibraryItem{{ID: "secret", Title: "Личное", UpdatedAt: 1, Kind: "site", Rev: 1}}},
		covers: map[string][]byte{"secret": []byte("\xff\xd8jpeg")}}
	r.svc.UseView(lib)

	got := askLibrary(t, r, r.ownerU, r.owner).Artifacts
	if len(got) != 1 || got[0].Cover == nil || !strings.Contains(*got[0].Cover, "store-cover?project=secret") {
		t.Fatalf("the owner's cover: %+v", got)
	}
	ask := func(u store.User, m store.Machine) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/m/"+m.ID+"/api/platform/design/store-cover?project=secret", nil)
		w := httptest.NewRecorder()
		if !r.svc.ServeMachine(w, req, u, m) {
			t.Fatal("left to the machine")
		}
		return w
	}
	if w := ask(r.ownerU, r.owner); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || w.Body.Len() == 0 {
		t.Fatalf("the owner: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w := ask(r.strangeU, r.stranger); w.Code != http.StatusNotFound || w.Body.Len() > 200 {
		t.Fatalf("a stranger got %d %s", w.Code, w.Body.String())
	}
}

// the hub's own routes of the design owner are reached through the machine's path.
func TestOwnerRoutesAreServed(t *testing.T) {
	r := newSpaceRig(t, true)
	req := httptest.NewRequest(http.MethodGet, "/m/"+r.owner.ID+"/api/platform/design/store-records-check", nil)
	w := httptest.NewRecorder()
	if !r.svc.ServeMachine(w, req, r.ownerU, r.owner) || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"projects"`) {
		t.Fatalf("records-check: %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/m/"+r.owner.ID+"/api/platform/design/store-work-machine", strings.NewReader(`{"project":"x"}`))
	w = httptest.NewRecorder()
	if !r.svc.ServeMachine(w, req, r.ownerU, r.owner) || w.Code == http.StatusOK {
		t.Fatalf("work-machine with the switch off: %d", w.Code)
	}
}
