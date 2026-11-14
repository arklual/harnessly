package designrec

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// the embedded store with no hub: the records are written, written once, and the check finds what is missing.
func TestRecordsInTheEmbeddedStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	link, err := storelink.Open(storelink.Options{DataDir: dir, Name: "box"})
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	s := &Syncer{Author: "runtime", Clients: func(kind string) Store { return link.Client(kind) }}
	var _ Store = (*filesync.Client)(nil)
	recs := []filestore.Record{
		{ID: "pitch", Title: "Pitch", Kind: "slides", At: 1},
		{ID: "brand", Title: "Brand", Kind: "ds", At: 2},
		{ID: "teaser", Title: "Teaser", Kind: "video", At: 3},
		{ID: "site-one", Title: "Site", Kind: "site", At: 4},
	}
	if rep, err := s.Reconcile(ctx, recs); err != nil || rep.OK || len(rep.Missing) != 4 {
		t.Fatalf("before the push: %+v %v", rep, err)
	}
	if n, err := s.Push(ctx, recs); err != nil || n != 4 {
		t.Fatalf("pushed %d, %v", n, err)
	}
	if n, err := s.Push(ctx, recs); err != nil || n != 0 {
		t.Fatalf("a second push wrote %d, %v", n, err)
	}
	// A new process (no memory of what it wrote) finds the store has them already.
	again := &Syncer{Clients: s.Clients}
	if n, err := again.Push(ctx, recs); err != nil || n != 0 {
		t.Fatalf("a restart wrote %d, %v", n, err)
	}
	rep, err := s.Reconcile(ctx, recs)
	if err != nil || !rep.OK || rep.Counts[filestore.KindDecks] != (Count{1, 1}) || rep.Counts[filestore.KindProjects].Store != 1 {
		t.Fatalf("after the push: %+v %v", rep, err)
	}
	// A project the machine dropped from its library is extra in the store, not missing.
	rep, err = s.Reconcile(ctx, recs[:2])
	if err != nil || !rep.OK || len(rep.Extra) != 2 {
		t.Fatalf("with two left on the machine: %+v %v", rep, err)
	}
	// A changed title is written again.
	recs[0].Title = "Pitch v2"
	if n, err := s.Push(ctx, recs); err != nil || n != 1 {
		t.Fatalf("a changed record wrote %d, %v", n, err)
	}
}

// a cover goes into the covers collection once, and again only when the picture changed.
func TestCoverIsWrittenOnceInTheEmbeddedStore(t *testing.T) {
	l, err := storelink.Open(storelink.Options{DataDir: t.TempDir(), Name: "box"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Syncer{Author: "t", Clients: func(k string) Store { return l.Client(k) }}
	ctx := context.Background()
	if err := l.Ready(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PushCover(ctx, "shop", []byte("\xff\xd8one")); err != nil || !ok {
		t.Fatalf("first: %v %v", ok, err)
	}
	if ok, _ := s.PushCover(ctx, "shop", []byte("\xff\xd8one")); ok {
		t.Fatal("the same picture was written again")
	}
	if ok, err := s.PushCover(ctx, "shop", []byte("\xff\xd8two")); err != nil || !ok {
		t.Fatalf("changed: %v %v", ok, err)
	}
	if ok, _ := s.PushCover(ctx, "Bad Id", []byte("x")); ok {
		t.Fatal("a cover of a bad id was written")
	}
}
