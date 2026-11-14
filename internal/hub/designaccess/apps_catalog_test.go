package designaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fakeKinds struct{}

func (fakeKinds) Kinds(context.Context, string) ([]hubproto.AppKind, error) {
	return []hubproto.AppKind{{ID: "notes.doc", Title: "Заметки", App: "notes", AppName: "Notes", Editor: "ed", SchemaVersion: 1}}, nil
}

// the apps' types and documents come from the hub behind hub-apps-catalog; off, the machine answers; a document
// of a machine closed to the person is not listed.
func TestAppsCatalogFromTheHubKeepsAccess(t *testing.T) {
	ctx := context.Background()
	r := newSpaceRig(t, true)
	if done, err := r.svc.registerCreated(ctx, r.owner, "note-1", r.ownerU.Email); err != nil || !done {
		t.Fatalf("registerCreated: %v %v", done, err)
	}
	if err := r.svc.write(ctx, r.owner, func(tx *txn) error {
		_, err := tx.q.Exec(tx.ctx, `UPDATE design_projects SET title = ?, kind = ? WHERE machine_id = ? AND project_id = ?`, "Мои заметки", "notes.doc", tx.key("note-1"), "note-1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	on := false
	r.svc.UseAppsCatalog(fakeKinds{}, func(context.Context, string, string) bool { return on })
	ask := func(u store.User, m store.Machine, path string) (bool, map[string]json.RawMessage) {
		req := httptest.NewRequest(http.MethodGet, "/m/"+m.ID+"/api/platform/apps/kinds"+path, nil)
		w := httptest.NewRecorder()
		served := r.svc.ServeMachine(w, req, u, m)
		var out map[string]json.RawMessage
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return served, out
	}
	if served, _ := ask(r.mateU, r.mate, ""); served {
		t.Fatal("switch off: the hub kept the types from the machine")
	}
	on = true
	if served, out := ask(r.mateU, r.mate, ""); !served || len(out["kinds"]) < 10 {
		t.Fatalf("types: %v %s", served, out["kinds"])
	}
	docs := func(u store.User, m store.Machine) string {
		_, out := ask(u, m, "/docs")
		return string(out["docs"])
	}
	if got := docs(r.mateU, r.mate); got == "[]" || got == "" {
		t.Fatalf("a mate sees no document of the open machine: %q", got)
	}
	r.svc.opens = func(_ context.Context, userID string, m store.Machine) bool {
		return m.ID != r.owner.ID || userID == r.ownerU.ID
	}
	if got := docs(r.mateU, r.mate); got != "[]" {
		t.Fatalf("a document of a private machine listed: %q", got)
	}
	if got := docs(r.ownerU, r.owner); got == "[]" {
		t.Fatalf("the owner lost their document: %q", got)
	}
}
