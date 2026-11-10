package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/storehub"
)

// an app installed for a space opens at the hub for its members with no machine at all: a browser without a
// session goes to the sign-in, a person of another space is refused, a member gets the page, the list of the space's
// apps, and an area of their own in the store.
func TestASpaceAppOpensAtTheHubForItsMembers(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Acme")
	mate := s.SignIn("mate@example.com")
	owner.Join(spaceID, mate)
	stranger := s.SignIn("stranger@example.com")
	if s.Hub.Store == nil {
		t.Skip("the hub runs without a store")
	}

	ctx := context.Background()
	room, err := s.Hub.Store.AppsRoom(ctx, spaceID)
	if err != nil {
		t.Fatal(err)
	}
	put := func(path string, raw []byte) {
		sum := sha256.Sum256(raw)
		sha := hex.EncodeToString(sum[:])
		if err := room.PutBlob(ctx, sha, bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := room.Commit(ctx, filestore.CommitRequest{Path: path, Sha: sha, Size: int64(len(raw)), Author: "test"}, "machine"); err != nil {
			t.Fatal(err)
		}
	}
	pkg := filepath.Join(t.TempDir(), "pkg")
	if err := apps.Scaffold(pkg, "acme.demo", "Demo", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	archive, sum, err := apps.Pack(pkg)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(pkg)
	put(apps.TeamArchivePath("acme.demo", "0.1.0"), archive)
	team, _ := json.Marshal(map[string]any{
		"space": spaceID, "at": 1,
		"catalog":  []map[string]any{{"id": "acme.demo", "name": "Demo", "version": "0.1.0", "targets": []string{"pane", "app"}, "sum": sum}},
		"installs": []map[string]any{{"id": "acme.demo", "version": "0.1.0", "sum": sum}},
	})
	put(apps.TeamStateFile, team)

	page := "/apps/space/" + spaceID + "/acme.demo/"
	if r := s.Browser().Do("GET", page, nil); r.Code != 302 || !strings.HasPrefix(r.Header.Get("Location"), "/signin?next=") {
		t.Fatalf("without a session: %d %q", r.Code, r.Header.Get("Location"))
	}
	if r := stranger.Do("GET", page, nil); r.Code != 403 {
		t.Fatalf("a person of another space: %s", r)
	}
	if r := stranger.Do("GET", "/api/spaces/"+spaceID+"/apps", nil); r.Code != 403 {
		t.Fatalf("another space's list: %s", r)
	}
	if r := mate.Do("GET", page, nil); r.Code != 200 || !strings.Contains(string(r.Body), `src="acme.demo/ui/index.html"`) {
		t.Fatalf("the member's page: %s", r)
	}
	var list struct {
		Apps []struct{ ID, Name, Path string }
	}
	r := mate.Do("GET", "/api/spaces/"+spaceID+"/apps", nil)
	r.JSON(&list)
	if r.Code != 200 || len(list.Apps) != 1 || list.Apps[0].Path != page || list.Apps[0].Name != "Demo" {
		t.Fatalf("the space's apps: %s", r)
	}
	r = mate.Do("POST", page+"__guest/storage", map[string]any{"op": "set", "scope": "user", "key": "k", "value": 1}, "Sec-Fetch-Site", "same-origin")
	if r.Code != 200 {
		t.Fatalf("the member's own area: %s", r)
	}
	if e, err := room.Current(ctx, "storage/acme.demo/user/"+storehub.PersonName(mate.ID)+".json"); err != nil || e == nil {
		t.Fatalf("the area is not the person's in the store: %v %v", e, err)
	}
}
