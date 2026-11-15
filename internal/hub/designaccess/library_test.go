package designaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// the library comes from the hub's store with every machine off, with the access of design access: a
// personal project of someone else is not listed, the person's own shelves are.

type fakeLibrary struct {
	items []LibraryItem
	files map[string][]byte
	// asked is the projects the last listing was asked for.
	asked []string
	// heads is the newest revision of the projects the store has.
	heads map[string]int64
}

func (f *fakeLibrary) Head(_ context.Context, _, project string) (int64, error) {
	return f.heads[project], nil
}
func (f *fakeLibrary) Wait(context.Context, string, string, int64) (int64, error) {
	return 0, nil
}
func (f *fakeLibrary) Serve(http.ResponseWriter, *http.Request, string, string, int64, []string, []string, bool) error {
	return nil
}
func (f *fakeLibrary) Write(context.Context, string, string, string, int64, []byte, string) (Written, error) {
	return Written{}, nil
}
func (f *fakeLibrary) Edit(context.Context, http.ResponseWriter, *http.Request, string, string, string, string) error {
	return nil
}
func (f *fakeLibrary) Library(_ context.Context, _ string, also []string) ([]LibraryItem, error) {
	f.asked = also
	return f.items, nil
}
func (f *fakeLibrary) ReadFile(_ context.Context, _ string, rel string, _ int64) ([]byte, error) {
	return f.files[rel], nil
}

func askLibrary(t *testing.T, r *spaceRig, u store.User, m store.Machine) storeLibrary {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/m/"+m.ID+"/api/platform/design/store-library", nil)
	w := httptest.NewRecorder()
	if !r.svc.ServeMachine(w, req, u, m) {
		t.Fatal("the hub left the library to the machine")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out storeLibrary
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLibraryFromTheStoreKeepsAccess(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "secret", r.ownerU.Email); err != nil || !done {
		t.Fatalf("registerCreated: %v %v", done, err)
	}
	if err := r.svc.write(ctx, r.owner, func(t *txn) error { return t.setTeam("secret", r.ownerU.Email, levelNone) }); err != nil {
		t.Fatal(err)
	}
	if done, err := r.svc.registerCreated(ctx, r.owner, "shared", r.ownerU.Email); err != nil || !done {
		t.Fatalf("registerCreated: %v %v", done, err)
	}
	lib := &fakeLibrary{
		items: []LibraryItem{
			{ID: "shared", Title: "Сайт команды", UpdatedAt: 2000, Kind: "site", Boards: []LibraryBoard{{Title: "Главная", Path: "index.dc.html"}}, Rev: 7},
			{ID: "secret", Title: "Личное", UpdatedAt: 1000, Kind: "slides", Rev: 5},
			{ID: "legacy", Title: "", UpdatedAt: 500, Kind: "site", Rev: 3},
		},
		files: map[string][]byte{
			".folders/" + layoutKey(r.ownerU.Email) + ".json": []byte(`{"folders":[{"id":"f-0000aaaa","name":"Клиенты","parent":""}],"placed":{"secret":"f-0000aaaa","gone":"f-0000aaaa"}}`),
		},
	}
	r.svc.UseView(lib)

	// The owner's machine is the one asked about, and no machine is on: the owner sees both recorded projects and their shelf.
	own := askLibrary(t, r, r.ownerU, r.owner)
	ids := map[string]storeArtifact{}
	for _, a := range own.Artifacts {
		ids[a.ID] = a
	}
	if len(ids) != 2 || ids["secret"].Role != roleOwner || ids["secret"].Owner == nil || !ids["secret"].Owner.Me {
		t.Fatalf("owner's library: %+v", own.Artifacts)
	}
	// An unrecorded project is still the machines': its access is theirs, so the hub's copy leaves it out.
	if _, ok := ids["legacy"]; ok {
		t.Fatalf("an unrecorded project listed: %+v", ids["legacy"])
	}
	if len(own.Layout.Folders) != 1 || own.Layout.Placed["secret"] != "f-0000aaaa" || own.Layout.Placed["gone"] != "" {
		t.Fatalf("owner's shelves: %+v", own.Layout)
	}
	if len(own.Missing) == 0 {
		t.Fatal("the answer does not say what only the machine knows")
	}

	// The stranger, under their own machine (off too): the personal project is not there, nor the owner's shelves.
	other := askLibrary(t, r, r.strangeU, r.stranger)
	// a project closed to the person is not read from the store for them at all.
	if len(lib.asked) != 1 || lib.asked[0] != "shared" {
		t.Fatalf("the store was asked for projects closed to the stranger: %v", lib.asked)
	}
	for _, a := range other.Artifacts {
		if a.ID == "secret" {
			t.Fatalf("a stranger sees someone else's personal project: %+v", other.Artifacts)
		}
	}
	if len(other.Artifacts) != 1 || len(other.Layout.Folders) != 0 {
		t.Fatalf("stranger's library: %+v", other)
	}
	if other.Artifacts[0].Owner == nil || other.Artifacts[0].Owner.Me || other.Artifacts[0].Owner.Name == "" {
		t.Fatalf("the owner of a team project: %+v", other.Artifacts[0])
	}
}

func TestLibraryWithoutTheStoreIsNotFound(t *testing.T) {
	r := newSpaceRig(t, true)
	req := httptest.NewRequest(http.MethodGet, "/m/"+r.owner.ID+"/api/platform/design/store-library", nil)
	w := httptest.NewRecorder()
	if !r.svc.ServeMachine(w, req, r.ownerU, r.owner) || w.Code != http.StatusNotFound {
		t.Fatalf("without a store: %d %s", w.Code, w.Body.String())
	}
	// The page tells a store that is off from one with no copy by the code (GitHub #82).
	var body struct{ Code string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != "store_off" {
		t.Fatalf("without a store, the code: %q %v", body.Code, err)
	}
}

// the hub counts the recorded projects by whether the store has a revision of them, and names the missing.
func TestCoverageCountsTheProjectsTheStoreLacks(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	for _, id := range []string{"instore", "onmachine"} {
		if done, err := r.svc.registerCreated(ctx, r.owner, id, r.ownerU.Email); err != nil || !done {
			t.Fatalf("registerCreated %s: %v %v", id, done, err)
		}
	}
	r.svc.UseView(&fakeLibrary{heads: map[string]int64{"instore": 4}})
	c, missing, err := r.svc.countCoverage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Stored != 1 || c.Missing != 1 || len(missing) != 1 || !strings.HasSuffix(missing[0], "/onmachine") {
		t.Fatalf("coverage: %+v %v", c, missing)
	}
	if got := r.svc.Coverage(); !got.At.IsZero() {
		t.Fatalf("a count nobody made is said: %+v", got)
	}
}

// a project recorded on a machine the person may not open (a private one) is not the space's: its team level
// does not let them in, while the owner and a grant of their own still do.
func TestLibraryLeavesOutProjectsOfAClosedMachine(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "team-site", r.ownerU.Email); err != nil || !done {
		t.Fatalf("registerCreated: %v %v", done, err)
	}
	r.svc.UseView(&fakeLibrary{items: []LibraryItem{{ID: "team-site", Title: "Сайт", UpdatedAt: 1000, Kind: "site", Rev: 1}}})
	listed := func(u store.User, m store.Machine) string {
		for _, a := range askLibrary(t, r, u, m).Artifacts {
			if a.ID == "team-site" {
				return a.Role
			}
		}
		return ""
	}
	// An open machine: the team level counts for the space.
	if role := listed(r.mateU, r.mate); role == "" {
		t.Fatal("a team project of an open machine is not listed for a mate")
	}
	// The owner's machine turns private: only its owner opens it.
	asked := 0
	r.svc.opens = func(_ context.Context, userID string, m store.Machine) bool {
		asked++
		return m.ID != r.owner.ID || userID == r.ownerU.ID
	}
	if role := listed(r.mateU, r.mate); role != "" {
		t.Fatalf("a private machine's project is listed for a mate with the role %q", role)
	}
	if asked != 1 {
		t.Fatalf("the machine's access was asked %d times for one person's list, want 1", asked)
	}
	if role := listed(r.ownerU, r.owner); role != roleOwner {
		t.Fatalf("the owner's own project: role %q", role)
	}
	// A grant of the mate's own opens it again: privacy closes the space's door, not the ones given by name.
	if err := r.svc.write(ctx, r.owner, func(t *txn) error {
		return t.grant("team-site", r.ownerU.Email, r.mateU.Email, roleView, ttl{none: true}, false)
	}); err != nil {
		t.Fatal(err)
	}
	if role := listed(r.mateU, r.mate); role != roleView {
		t.Fatalf("a project granted by name: role %q", role)
	}
}

// Sinopia's home in one answer goes on to the machine only for whom hub-design-home is on; for the rest the
// hub answers 404 home_off and the page asks part by part, as before.
func TestHomeInOneAnswerIsBehindItsSwitch(t *testing.T) {
	r := newSpaceRig(t, true)
	ask := func() (bool, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, "/m/"+r.owner.ID+"/api/platform/design/home?machine="+r.owner.ID, nil)
		w := httptest.NewRecorder()
		return r.svc.ServeMachine(w, req, r.ownerU, r.owner), w
	}
	if served, w := ask(); !served || w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "home_off") {
		t.Fatalf("switch off: served %v, %d %s", served, w.Code, w.Body.String())
	}
	var asked string
	r.svc.homeOn = func(_ context.Context, email, machineID string) bool {
		asked = email + " " + machineID
		return true
	}
	if served, _ := ask(); served {
		t.Fatal("switch on: the hub kept the home from the machine")
	}
	if asked != normEmail(r.ownerU.Email)+" "+r.owner.ID {
		t.Fatalf("the switch was asked for %q", asked)
	}
}
