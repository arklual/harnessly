package designrec

import (
	"context"
	"fmt"
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

// refusingStore refuses the writes of the paths in deny, as the hub does for a project none of the machine's people may
// edit, and counts what it is asked.
type refusingStore struct {
	Store
	deny  map[string]bool
	puts  map[string]int
	reads int
}

func (r *refusingStore) Current(ctx context.Context, path string) (*filestore.Entry, error) {
	r.reads++
	if r.deny[path] {
		return nil, nil // the hub answers 404 for a path the machine may not see
	}
	return r.Store.Current(ctx, path)
}

func (r *refusingStore) Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error) {
	r.puts[path]++
	if r.deny[path] {
		return filestore.Entry{}, fmt.Errorf("%w: %s", filesync.ErrRefused, path)
	}
	return r.Store.Put(ctx, path, data, author)
}

// a refused record or cover does not stop the others and is not asked again on every pass, only after it
// changed or RefusedRetry passed.
func TestRefusedWriteIsNotRepeatedOnEveryPass(t *testing.T) {
	ctx := context.Background()
	l, err := storelink.Open(storelink.Options{DataDir: t.TempDir(), Name: "box"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.Ready(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	deny := map[string]bool{filestore.RecordPath("theirs"): true, filestore.CoverPath("theirs"): true}
	stores := map[string]*refusingStore{}
	now := time.Unix(1_800_000_000, 0)
	s := &Syncer{Author: "t", Now: func() time.Time { return now }, Clients: func(k string) Store {
		if stores[k] == nil {
			stores[k] = &refusingStore{Store: l.Client(k), deny: deny, puts: map[string]int{}}
		}
		return stores[k]
	}}
	recs := []filestore.Record{{ID: "theirs", Title: "Theirs", Kind: "site", At: 1}, {ID: "mine", Title: "Mine", Kind: "site", At: 2}}
	if n, err := s.Push(ctx, recs); err != nil || n != 1 {
		t.Fatalf("first push: %d %v", n, err)
	}
	if ok, err := s.PushCover(ctx, "theirs", []byte("\xff\xd8x")); err != nil || ok {
		t.Fatalf("refused cover: %v %v", ok, err)
	}
	if s.Refused() != 2 {
		t.Fatalf("refused %d", s.Refused())
	}
	projects, covers := stores[filestore.KindProjects], stores[filestore.KindCovers]
	for range 10 {
		now = now.Add(time.Minute)
		if n, err := s.Push(ctx, recs); err != nil || n != 0 {
			t.Fatalf("a later push: %d %v", n, err)
		}
		_, _ = s.PushCover(ctx, "theirs", []byte("\xff\xd8x"))
	}
	if p, c := projects.puts[filestore.RecordPath("theirs")], covers.puts[filestore.CoverPath("theirs")]; p != 1 || c != 1 {
		t.Fatalf("refused writes asked again: record %d, cover %d", p, c)
	}
	// Changed, it is asked again at once; and so is the unchanged one an hour later.
	recs[0].Title = "Theirs v2"
	_, _ = s.Push(ctx, recs)
	now = now.Add(RefusedRetry)
	_, _ = s.PushCover(ctx, "theirs", []byte("\xff\xd8x"))
	if p, c := projects.puts[filestore.RecordPath("theirs")], covers.puts[filestore.CoverPath("theirs")]; p != 2 || c != 2 {
		t.Fatalf("after a change and an hour: record %d, cover %d", p, c)
	}
}
