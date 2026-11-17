package storehub

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
)

// The collection of garbage is told of the blobs the hub's tables refer to, each under the room it lies in.
func TestPinsNameTheHubsOwnBlobsByRoom(t *testing.T) {
	ctx := context.Background()
	db := storetest.Open(t)
	q := db.S().Q()
	for _, stmt := range []string{
		`INSERT INTO chat_shares (id, space_id, machine_id, owner_id, sha256, expires_at, created_at) VALUES ('s1', 'sp1', 'm1', 'u1', 'aaa', 1, 1)`,
		`INSERT INTO chat_shares (id, space_id, machine_id, owner_id, sha256, expires_at, created_at) VALUES ('s2', 'old', 'm2', 'u1', 'ccc', 1, 1)`,
		`INSERT INTO design_derived (key, space_id, project, rev, sha256, created_at) VALUES ('k1', 'sp1', 'shop', 1, 'bbb', 1)`,
	} {
		if _, err := q.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Pins(db, "old")(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for room, shas := range map[string][]string{"files-sp1": {"aaa", "bbb"}, "files": {"ccc"}} {
		for _, sha := range shas {
			if _, ok := got[room][sha]; !ok {
				t.Fatalf("%s in %s is not pinned: %v", sha, room, got)
			}
		}
	}
}
