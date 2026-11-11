package designaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// the library comes from the hub's store with every machine off, with the access of design access: a
// personal project of someone else is not listed, the person's own shelves are.

type fakeLibrary struct {
	items []LibraryItem
	files map[string][]byte
}

func (f *fakeLibrary) Head(context.Context, string, string) (int64, error) { return 0, nil }
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
}
