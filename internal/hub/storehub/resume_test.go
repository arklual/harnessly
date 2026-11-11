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

// a chat's resume snapshot holds its whole history, so only the person's own machines open it: the web and
// another person's machine get a 403, a machine writes under its own id only.
func TestResumeSnapshotsAreForThePersonsOwnMachinesOnly(t *testing.T) {
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
			has = has || c == filestore.KindResume
		}
		if !has {
			t.Fatalf("the hub does not offer %s: %v", filestore.KindResume, s.Collections)
		}
		tok := s.Token
		return filesync.NewClient(srv.URL+hubproto.StoreAPIPath, filestore.KindResume,
			func(context.Context, bool) (string, error) { return tok, nil }, nil)
	}
	a, b, other, nobody := client("vm-a"), client("vm-b"), client("vm-x"), client("vm-no")
	forbidden := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "403") && !errors.Is(err, filesync.ErrRefused) {
			t.Errorf("%s: want 403, got %v", what, err)
		}
	}

	snap := []byte(`{"v":1,"opId":"r0","bundleEnc":{"nonce":"n","ciphertext":"c"}}`)
	if _, err := a.Put(ctx, "vm-a/tab-1.json", snap, "vm-a"); err != nil {
		t.Fatal(err)
	}
	// The person's other machine reads it, from another space too.
	e, err := b.Current(ctx, "vm-a/tab-1.json")
	if err != nil || e == nil || e.Sha == "" {
		t.Fatalf("the person's other machine: %+v %v", e, err)
	}
	var got strings.Builder
	if err := b.GetBlob(ctx, e.Sha, &got); err != nil || got.String() != string(snap) {
		t.Fatalf("blob: %q %v", got.String(), err)
	}
	// ...and while the source is gone from the hub too: the room is the person's own.
	delete(db, "vm-a")
	st.mu.Lock()
	delete(st.machines, "vm-a")
	st.mu.Unlock()
	if e, err := b.Current(ctx, "vm-a/tab-1.json"); err != nil || e == nil {
		t.Fatalf("after the source is gone: %+v %v", e, err)
	}
	db["vm-a"] = store.Machine{ID: "vm-a", Name: "vm-a", SpaceID: "sp1", CreatedBy: "u-owner"}

	// Another person's machine naming it is refused out loud, and its own room does not have it.
	_, err = other.Current(ctx, "vm-a/tab-1.json")
	forbidden("another person's machine, current", err)
	_, err = other.HistoryUnder(ctx, "vm-a/", 10)
	forbidden("another person's machine, its folder", err)
	if err := other.GetBlob(ctx, e.Sha, &strings.Builder{}); err == nil {
		t.Error("another person's machine got the blob")
	}
	_, err = other.Put(ctx, "vm-a/tab-1.json", []byte("x"), "vm-x")
	forbidden("another person's machine, write", err)
	if e2, err := b.Current(ctx, "vm-a/tab-1.json"); err != nil || e2 == nil || e2.Sha != e.Sha {
		t.Errorf("the snapshot changed: %+v %v", e2, err)
	}
	// A machine nobody made has no room.
	_, err = nobody.Current(ctx, "vm-a/tab-1.json")
	forbidden("a machine nobody made", err)
	// A machine writes only under its own id, not over its person's other machine.
	_, err = b.Put(ctx, "vm-a/tab-1.json", []byte("x"), "vm-b")
	forbidden("over another machine of the person", err)
	_, err = b.Put(ctx, "tab-1.json", []byte("x"), "vm-b")
	forbidden("outside any machine's folder", err)

	// The web (a person's cookie, no machine session) gets a 403, not a 401 to log in again.
	for _, auth := range []string{"", "Cookie x"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+hubproto.StoreAPIPath+"/v1/collections/"+filestore.KindResume+
			"/current?path=vm-a/tab-1.json", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.AddCookie(&http.Cookie{Name: "session", Value: "x"})
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
