package storehub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// fakeKits is the kit access with one owner per kit and a list of viewers.
type fakeKits struct {
	mu      sync.Mutex
	owner   map[string]string          // kit → machine of the owner
	viewers map[string]map[string]bool // kit → machines that may view
	legacy  []string
}

func (k *fakeKits) KitView(_ context.Context, m store.Machine) (map[string]KitGrant, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := map[string]KitGrant{}
	for kit, o := range k.owner {
		switch {
		case o == m.ID:
			out[kit] = KitGrant{See: true, Edit: true, Own: true}
		default:
			out[kit] = KitGrant{See: k.viewers[kit][m.ID]}
		}
	}
	return out, nil
}

func (k *fakeKits) ClaimKit(_ context.Context, m store.Machine, kit string) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.owner[kit]; !ok {
		k.owner[kit] = m.ID
	}
	return k.owner[kit] == m.ID, nil
}

func (k *fakeKits) OpenLegacyKit(_ context.Context, m store.Machine, kit string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.legacy = append(k.legacy, m.ID+":"+kit)
	return nil
}

// The kits collection reaches a machine only for the kits its person may see; the first machine to put a kit in the room
// owns it, privately; a viewer reads and cannot write; a machine that may not see a kit finds nothing of it.
func TestTheStoreFiltersKitsByAccess(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	meta, err := filestore.OpenMetaStore(ctx, "sqlite://"+dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { meta.Close() })
	blobs, err := filestore.NewFileBlobs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	machines := fakeMachines{
		"maker":  {ID: "maker", Name: "vm-maker", SpaceID: "sp1"},
		"viewer": {ID: "viewer", Name: "vm-viewer", SpaceID: "sp1"},
		"other":  {ID: "other", Name: "vm-other", SpaceID: "sp1"},
	}
	kits := &fakeKits{owner: map[string]string{}, viewers: map[string]map[string]bool{"acme-ds": {"viewer": true}}}
	next := "maker"
	st, err := Open(ctx, Options{Config: &config.Config{}, DB: machines, Key: []byte("0123456789abcdef0123456789abcdef"),
		Meta: meta, Blobs: blobs, Kits: kits,
		Admit: func(*http.Request) (store.Machine, error) { return machines[next], nil },
		Now:   func() time.Time { return time.Now() }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	st.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := func(machine string) *filesync.Client {
		next = machine
		resp, err := http.Post(srv.URL+hubproto.StorePath, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var s hubproto.StoreSession
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			t.Fatal(err)
		}
		tok := s.Token
		return filesync.NewClient(srv.URL+hubproto.StoreAPIPath, filestore.KindKits, func(context.Context, bool) (string, error) { return tok, nil }, nil)
	}
	maker, viewer, other := client("maker"), client("viewer"), client("other")
	put := func(c *filesync.Client, path, body string) error {
		_, err := c.Put(ctx, path, []byte(body), "t")
		return err
	}

	// The first machine to put a kit in the room owns it.
	if err := put(maker, "acme-ds/1.0.0/tokens.json", "{}"); err != nil {
		t.Fatal(err)
	}
	if kits.owner["acme-ds"] != "maker" {
		t.Fatalf("owner %v", kits.owner)
	}
	// Another machine putting a file into a kit it does not own is refused.
	if err := put(other, "acme-ds/1.0.0/evil.js", "x"); err == nil {
		t.Fatal("a stranger wrote into a kit")
	}
	// The viewer sees the kit and cannot write; the stranger sees nothing.
	vc, err := viewer.Changes(ctx, 0, 0)
	if err != nil || len(vc.Entries) != 1 {
		t.Fatalf("the viewer sees %+v %v", vc.Entries, err)
	}
	if err := put(viewer, "acme-ds/1.0.0/more.json", "{}"); err == nil {
		t.Fatal("a viewer wrote into a kit")
	}
	oc, err := other.Changes(ctx, 0, 0)
	if err != nil || len(oc.Entries) != 0 {
		t.Fatalf("a machine without access sees %+v %v", oc.Entries, err)
	}
	if e, err := other.Current(ctx, "acme-ds/1.0.0/tokens.json"); err != nil || e != nil {
		t.Fatalf("a hidden path answers %+v %v", e, err)
	}
	// A second kit is the other machine's own: the first kit stays hidden from it, and it cannot take that id.
	if err := put(other, "mine/0.1.0/tokens.json", "{}"); err != nil {
		t.Fatal(err)
	}
	if kits.owner["mine"] != "other" {
		t.Fatalf("owner %v", kits.owner)
	}
	// A seal that says legacy opens the kit to the team (once, for the owner's own kit).
	seal := filestore.NewKitSeal([]filestore.KitSealedFile{{Path: "tokens.json", Size: 2, SHA256: shaOf("{}")}})
	seal.Legacy = true
	raw, _ := json.Marshal(seal)
	if err := put(maker, "acme-ds/1.0.0/"+filestore.KitSealFile, string(raw)); err != nil {
		t.Fatal(err)
	}
	if len(kits.legacy) != 1 || kits.legacy[0] != "maker:acme-ds" {
		t.Fatalf("legacy opened: %v", kits.legacy)
	}
}

func shaOf(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}
