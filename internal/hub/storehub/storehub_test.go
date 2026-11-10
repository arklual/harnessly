package storehub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// the hub's store shows a machine only the design projects its person has a role in, takes its writes only to
// those its person may edit, and records a project whose owner file arrives before the hub knew it.

type fakeMachines map[string]store.Machine

func (f fakeMachines) MachineByID(_ context.Context, id string) (store.Machine, error) {
	m, ok := f[id]
	if !ok {
		return store.Machine{}, store.ErrNotFound
	}
	return m, nil
}

func (f fakeMachines) SpaceBySlug(context.Context, string) (store.Space, error) {
	return store.Space{}, store.ErrNotFound
}

type fakeDesign struct {
	mu       sync.Mutex
	view     map[string]map[string]Grant // machine → project → grant
	recorded []string
}

func (d *fakeDesign) StoreView(_ context.Context, m store.Machine) (map[string]Grant, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]Grant{}
	for k, v := range d.view[m.ID] {
		out[k] = v
	}
	return out, nil
}

func (d *fakeDesign) RecordFromStore(_ context.Context, m store.Machine, project string, file []byte) (bool, error) {
	var f struct{ Person string }
	if json.Unmarshal(file, &f) != nil {
		return false, errors.New("bad owner file")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recorded = append(d.recorded, m.ID+":"+project+":"+f.Person)
	// The project is the owner's alone from now on.
	for id := range d.view {
		if d.view[id] == nil {
			d.view[id] = map[string]Grant{}
		}
		d.view[id][project] = Grant{See: id == m.ID, Edit: id == m.ID, Own: id == m.ID}
	}
	return true, nil
}

func TestTheStoreFiltersTheDesignLibraryByRole(t *testing.T) {
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
		"owner": {ID: "owner", Name: "vm-owner", SpaceID: "sp1"},
		"mate":  {ID: "mate", Name: "vm-mate", SpaceID: "sp1"},
	}
	design := &fakeDesign{view: map[string]map[string]Grant{
		"owner": {"secret": {See: true, Edit: true, Own: true}},
		"mate":  {"secret": {See: false}},
	}}
	next := "owner"
	st, err := Open(ctx, Options{Config: &config.Config{}, DB: machines, Key: []byte("0123456789abcdef0123456789abcdef"),
		Meta: meta, Blobs: blobs, Design: design,
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
		return filesync.NewClient(srv.URL+hubproto.StoreAPIPath, filestore.KindDesign,
			func(context.Context, bool) (string, error) { return tok, nil }, nil)
	}
	owner, mate := client("owner"), client("mate")

	put := func(c *filesync.Client, path, body string) error {
		_, err := c.Put(ctx, path, []byte(body), "t")
		return err
	}
	if err := put(owner, "secret/project/Main.dc.html", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := put(owner, "open/project/Main.dc.html", "open"); err != nil {
		t.Fatal(err)
	}
	ch, err := mate.Changes(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Entries) != 1 || ch.Entries[0].Path != "open/project/Main.dc.html" || ch.Scope == "" {
		t.Fatalf("the mate's machine sees %+v (scope %q), want only the open project", ch.Entries, ch.Scope)
	}
	if e, err := mate.Current(ctx, "secret/project/Main.dc.html"); err != nil || e != nil {
		t.Fatalf("a hidden path answers %+v %v", e, err)
	}
	if err := put(mate, "secret/project/Main.dc.html", "mine now"); !errors.Is(err, filesync.ErrRefused) {
		t.Fatalf("a write to a project the person has no role in: %v", err)
	}

	// Case does not get around the filter: on a filesystem that ignores it, Secret/ is secret/.
	if err := put(mate, "Secret/project/Main.dc.html", "sneak"); !errors.Is(err, filesync.ErrRefused) {
		t.Fatalf("a write under another case of a closed project: %v", err)
	}
	// A folder that lower-cases to another project's (the Kelvin sign is a «k») is not written.
	if err := put(mate, "\u212Aopen/project/Main.dc.html", "x"); !errors.Is(err, filesync.ErrRefused) {
		t.Fatalf("a write under a look-alike of a project folder: %v", err)
	}
	// An owner file written into a project others filled takes nothing over.
	if err := put(mate, "open/.owner.json", `{"v":2,"user":"x","name":"Mate","person":"mate@example.com","private":true}`); err != nil {
		t.Fatal(err)
	}
	// Nor does one over the owner's own files from that machine: only a folder holding nothing else is new.
	if err := put(owner, "open/.owner.json", `{"v":2,"user":"x","name":"Owner","person":"owner@example.com","private":true}`); err != nil {
		t.Fatal(err)
	}
	if len(design.recorded) != 0 {
		t.Fatalf("an owner file over a filled folder recorded the project: %v", design.recorded)
	}

	// A new project: its owner file reaches the store first and the hub records it; the mate's machine stops seeing it.
	ownerFile := `{"v":2,"user":"coder","name":"Owner","person":"owner@example.com","private":true}`
	if err := put(owner, "fresh/.owner.json", ownerFile); err != nil {
		t.Fatal(err)
	}
	if len(design.recorded) != 1 || design.recorded[0] != "owner:fresh:owner@example.com" {
		t.Fatalf("recorded %v", design.recorded)
	}
	if err := put(owner, "fresh/project/Main.dc.html", "new"); err != nil {
		t.Fatal(err)
	}
	ch2, err := mate.Changes(ctx, ch.Rev, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ch2.Entries {
		if e.Path == "fresh/project/Main.dc.html" {
			t.Fatalf("a project recorded as personal reached another machine: %+v", ch2.Entries)
		}
	}
	if ch2.Scope == ch.Scope {
		t.Fatal("the scope did not change when the machine stopped seeing a project")
	}
	sum := sha256.Sum256([]byte("v1"))
	snap, err := owner.Snapshot(ctx, "secret/", ch2.Rev)
	if err != nil || len(snap.Entries) != 1 || snap.Entries[0].Sha != hex.EncodeToString(sum[:]) {
		t.Fatalf("snapshot %+v %v", snap, err)
	}
}

func TestNonASCIIFoldersAreNotWritten(t *testing.T) {
	for _, p := range []string{"\u212Aopen/x", "\u017Fecret/x", "o\u200Dpen/x", "проект/x"} {
		if !foreignFolder(p) {
			t.Fatalf("%q is written", p)
		}
	}
	for _, p := range []string{"open/x", ".folders/x", "a-1/project/b.html"} {
		if foreignFolder(p) {
			t.Fatalf("%q refused", p)
		}
	}
}

func TestProjectOfIsCaseFolded(t *testing.T) {
	if id, ok := projectOf("P/project/x.html"); !ok || id != "p" {
		t.Fatalf("projectOf: %q %v", id, ok)
	}
	if _, ok := projectOf(".folders/abc.json"); ok {
		t.Fatal("a shelf counted as a project")
	}
}
