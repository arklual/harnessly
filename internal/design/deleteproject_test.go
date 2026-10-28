package design

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func deleteCall(a *API, id string) int {
	r := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(`{"project":"`+id+`"}`))
	r.Header.Set("Content-Type", "application/json")
	return a.DeleteProject(r).Status
}

func TestDeleteProjectMovesItToTrashForItsOwnerOnly(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"mine", "edited", "theirs"} {
		makeProject(t, root, id, id, "")
	}
	l := accessLibrary(t, root)
	var events []string
	l.emitFn = func(kind, project string) { events = append(events, kind+" "+project) }
	none := map[string]string{"mine": "none", "edited": "none", "theirs": "none"}
	grants := map[string]Grant{"mine": {Role: "owner"}, "edited": {Role: "edit"}, "theirs": {Role: "view"}}
	if err := l.SetAccess(snapshot(grants, none)); err != nil {
		t.Fatal(err)
	}
	a := pagesAPI(l)
	// a project the gateway has no row for is nobody's to delete: even the role of the legacy "everyone edits" is not enough
	if c := deleteCall(a, "nothing"); c != 403 {
		t.Errorf("missing: %d", c)
	}
	if c := deleteCall(a, "theirs"); c != 403 {
		t.Errorf("viewer: %d", c)
	}
	if c := deleteCall(a, "edited"); c != 403 {
		t.Errorf("editor: %d", c)
	}
	if len(events) != 0 {
		t.Fatalf("events on refusals: %v", events)
	}
	if c := deleteCall(a, "mine"); c != 200 {
		t.Fatalf("owner: %d", c)
	}
	if _, err := os.Stat(filepath.Join(root, "mine")); !os.IsNotExist(err) {
		t.Fatalf("folder still there: %v", err)
	}
	if kept, _ := filepath.Glob(filepath.Join(root, ".trash", "mine-*")); len(kept) != 1 {
		t.Fatalf("trash: %v", kept)
	}
	if len(events) != 1 || events[0] != "design.deleted mine" {
		t.Fatalf("events: %v", events)
	}
	snap, _ := l.Access()
	if _, has := snap.Projects["mine"]; has {
		t.Errorf("grant kept in the snapshot")
	}
	if _, has := snap.Team["mine"]; has {
		t.Errorf("team level kept in the snapshot")
	}
	if c := deleteCall(a, "mine"); c != 403 {
		t.Errorf("twice: %d", c)
	}
}

func TestDeleteProjectWithoutSnapshotNeedsTheOwnerFile(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "mine", "Mine", mineV1)
	makeProject(t, root, "theirs", "Theirs", otherV1)
	makeProject(t, root, "nobodys", "Nobodys", "")
	a := pagesAPI(accessLibrary(t, root))
	if c := deleteCall(a, "theirs"); c != 403 {
		t.Errorf("other's: %d", c)
	}
	if c := deleteCall(a, "nobodys"); c != 403 {
		t.Errorf("ownerless: %d", c)
	}
	if c := deleteCall(a, "mine"); c != 200 {
		t.Errorf("own: %d", c)
	}
}

func TestDeletedIDStaysTakenAndIsAnnouncedUntilTheGatewayForgetsIt(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "mine", "Mine", "")
	l := accessLibrary(t, root)
	var events []string
	l.emitFn = func(kind, project string) { events = append(events, kind+" "+project) }
	if err := l.SetAccess(snapshot(map[string]Grant{"mine": {Role: "owner"}}, map[string]string{"mine": "none"})); err != nil {
		t.Fatal(err)
	}
	if c := deleteCall(pagesAPI(l), "mine"); c != 200 {
		t.Fatalf("delete: %d", c)
	}
	if !l.TakenArtifactIDs()["mine"] {
		t.Fatalf("a deleted id is free for a new project")
	}
	if got := SlugifyArtifactID("Mine", l.TakenArtifactIDs()); got == "mine" {
		t.Fatalf("a new project took the deleted id: %s", got)
	}
	// the gateway still lists it (its row is not gone): the notice is sent again once it is due, never before
	if err := l.SetAccess(snapshot(map[string]Grant{"mine": {Role: "owner"}}, map[string]string{"mine": "none"})); err != nil {
		t.Fatal(err)
	}
	l.ReannounceDeleted()
	if len(events) != 1 {
		t.Fatalf("announced again too early: %v", events)
	}
	l.now = func() time.Time { return accessNow.Add(announceDeletedAfter + time.Second) }
	l.ReannounceDeleted()
	if len(events) != 2 || events[1] != "design.deleted mine" {
		t.Fatalf("not announced again: %v", events)
	}
	// the gateway forgot it: nothing more is sent
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{})); err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return accessNow.Add(3 * announceDeletedAfter) }
	l.ReannounceDeleted()
	if len(events) != 2 {
		t.Fatalf("announced after the gateway forgot it: %v", events)
	}
}
