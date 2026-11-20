package storehub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// a person's app set is read and written by the person's own machines, whatever space they are in; another
// person's machine, a machine nobody made and the web get a 403.
func TestAppSetIsForThePersonsOwnMachinesOnly(t *testing.T) {
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
	db := fakeMachines{
		"vm-a":  {ID: "vm-a", Name: "vm-a", SpaceID: "sp1", CreatedBy: "u-owner"},
		"vm-b":  {ID: "vm-b", Name: "vm-b", SpaceID: "sp2", CreatedBy: "u-owner"},
		"vm-x":  {ID: "vm-x", Name: "vm-x", SpaceID: "sp1", CreatedBy: "u-other"},
		"vm-no": {ID: "vm-no", Name: "vm-no", SpaceID: "sp1"},
	}
	next := ""
	st, err := Open(ctx, Options{Config: &config.Config{}, DB: db, Key: []byte("0123456789abcdef0123456789abcdef"),
		Meta: meta, Blobs: blobs,
		Admit: func(*http.Request) (store.Machine, error) { return db[next], nil },
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
		has := false
		for _, c := range s.Collections {
			has = has || c == filestore.KindAppSet
		}
		if !has {
			t.Fatalf("the hub does not offer %s: %v", filestore.KindAppSet, s.Collections)
		}
		tok := s.Token
		return filesync.NewClient(srv.URL+hubproto.StoreAPIPath, filestore.KindAppSet,
			func(context.Context, bool) (string, error) { return tok, nil }, nil)
	}
	a, b, other, nobody := client("vm-a"), client("vm-b"), client("vm-x"), client("vm-no")
	forbidden := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "403") && !errors.Is(err, filesync.ErrRefused) {
			t.Errorf("%s: want 403, got %v", what, err)
		}
	}
	rec := []byte(`{"v":1,"id":"acme.board","version":"1.0.0"}`)
	if _, err := a.Put(ctx, "apps/acme.board.json", rec, "vm-a"); err != nil {
		t.Fatal(err)
	}
	e, err := b.Current(ctx, "apps/acme.board.json")
	if err != nil || e == nil || e.Sha == "" {
		t.Fatalf("the person's other machine, in another space: %+v %v", e, err)
	}
	var got strings.Builder
	if err := b.GetBlob(ctx, e.Sha, &got); err != nil || got.String() != string(rec) {
		t.Fatalf("blob: %q %v", got.String(), err)
	}
	// Any machine of the person may change the record (a removal or a setting made on it).
	if _, err := b.Put(ctx, "apps/acme.board.json", []byte(`{"v":1,"removed":true}`), "vm-b"); err != nil {
		t.Fatalf("the person's other machine writes: %v", err)
	}
	// Another person's machine has a room of its own: it does not see the record, and what it writes there is not ours.
	if e, err := other.Current(ctx, "apps/acme.board.json"); err != nil || e != nil {
		t.Errorf("another person's machine sees the record: %+v %v", e, err)
	}
	if _, err := other.Put(ctx, "apps/acme.board.json", []byte("x"), "vm-x"); err != nil {
		t.Fatalf("another person's own room: %v", err)
	}
	if e, err := a.Current(ctx, "apps/acme.board.json"); err != nil || e == nil || e.Machine == "vm-x" || e.Author == "vm-x" {
		t.Errorf("the record was changed by another person: %+v %v", e, err)
	}
	_, err = nobody.Current(ctx, "apps/acme.board.json")
	forbidden("a machine nobody made", err)
	for _, auth := range []string{"", "Cookie x"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+hubproto.StoreAPIPath+"/v1/collections/"+filestore.KindAppSet+
			"/current?path=apps/acme.board.json", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("the web (%q): %d", auth, resp.StatusCode)
		}
	}
}
