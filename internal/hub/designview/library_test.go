package designview

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// a project's item of the library is read from the store with the runtime's listing, and kept until the
// project gets a newer revision.
func TestLibraryListsAProjectFromTheStore(t *testing.T) {
	room := newMemRoom()
	room.commit(map[string]string{
		"artifact.json":         `{"title":"Лендинг"}`,
		"project/home.dc.html":  "<html><body>home</body></html>",
		"project/about.dc.html": "<html><body>about</body></html>",
		"project/hero.png":      "png",
	})
	v := New(Options{Rooms: func(context.Context, string) (Room, error) { return room, nil }, Dir: t.TempDir(), Poll: time.Millisecond})
	items, err := v.Library(context.Background(), "s1", []string{"land", "../bad"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "land" || items[0].Title != "Лендинг" || items[0].Kind != "site" || len(items[0].Boards) != 2 || items[0].Rev != 1 {
		t.Fatalf("items: %+v", items)
	}
	reads := room.reads
	if _, err := v.Library(context.Background(), "s1", []string{"land"}); err != nil || room.reads != reads {
		t.Fatalf("the same revision read again: %v %d→%d", err, reads, room.reads)
	}
}

// gatedRoom holds every read of a blob until open is closed, and fails the reads of the shas in broken.
type gatedRoom struct {
	*memRoom
	open   chan struct{}
	broken map[string]bool
}

func (r *gatedRoom) OpenBlob(ctx context.Context, sha string) (io.ReadCloser, error) {
	select {
	case <-r.open:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if r.broken[sha] {
		return nil, errors.New("the bucket did not answer")
	}
	return r.memRoom.OpenBlob(ctx, sha)
}

// the caller that left does not end the build of the item, so the next caller gets it; before, every
// request of a cold hub started the reads again and none lived long enough to finish.
func TestLibraryBuildOutlivesItsRequest(t *testing.T) {
	room := &gatedRoom{memRoom: newMemRoom(), open: make(chan struct{})}
	room.commit(map[string]string{"artifact.json": `{"title":"Лендинг"}`, "project/home.dc.html": "<html><body>home</body></html>"})
	v := New(Options{Rooms: func(context.Context, string) (Room, error) { return room, nil }, Dir: t.TempDir(), Poll: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := v.Library(ctx, "s1", []string{"land"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a caller that left: %v", err)
	}
	close(room.open)
	deadline := time.Now().Add(10 * time.Second)
	for {
		v.mu.Lock()
		_, built := v.listing["s1/land"]
		v.mu.Unlock()
		if built {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the build ended with the request that started it")
		}
		time.Sleep(5 * time.Millisecond)
	}
	reads := room.reads
	items, err := v.Library(context.Background(), "s1", []string{"land"})
	if err != nil || len(items) != 1 || items[0].Title != "Лендинг" || room.reads != reads {
		t.Fatalf("the next caller: %v %+v, reads %d→%d", err, items, reads, room.reads)
	}
}

// a project the store cannot be read for is left out; the rest of the library is answered.
func TestLibraryLeavesOutAProjectItCannotRead(t *testing.T) {
	good := newMemRoom()
	good.commit(map[string]string{"artifact.json": `{"title":"Лендинг"}`, "project/home.dc.html": "<html><body>home</body></html>"})
	open := make(chan struct{})
	close(open)
	bad := &gatedRoom{memRoom: newMemRoom(), open: open, broken: map[string]bool{shaOf(`{"title":"Сломан"}`): true}}
	bad.commit(map[string]string{"artifact.json": `{"title":"Сломан"}`, "project/home.dc.html": "<html><body>x</body></html>"})
	// One room per project: the memory room holds one project's revisions.
	rooms := map[string]Room{"land": good, "broken": bad}
	v := New(Options{Dir: t.TempDir(), Poll: time.Millisecond, Rooms: func(context.Context, string) (Room, error) {
		return splitRoom(rooms), nil
	}})
	items, err := v.Library(context.Background(), "s1", []string{"land", "broken"})
	if err != nil || len(items) != 1 || items[0].ID != "land" {
		t.Fatalf("the library with one unreadable project: %v %+v", err, items)
	}
}

// splitRoom answers each project from its own room, by the project's folder in the path; a blob comes from the room
// that holds it.
type splitRoom map[string]Room

func (s splitRoom) of(prefix string) Room {
	id, _, _ := strings.Cut(prefix, "/")
	return s[id]
}

func (s splitRoom) HistoryUnder(ctx context.Context, prefix string, limit int) ([]filestore.Entry, error) {
	return s.of(prefix).HistoryUnder(ctx, prefix, limit)
}

func (s splitRoom) Snapshot(ctx context.Context, prefix string, at int64) (filestore.Snapshot, error) {
	return s.of(prefix).Snapshot(ctx, prefix, at)
}

func (s splitRoom) OpenBlob(ctx context.Context, sha string) (io.ReadCloser, error) {
	for _, r := range s {
		mem, _ := r.(*memRoom)
		if g, ok := r.(*gatedRoom); ok {
			mem = g.memRoom
		}
		mem.mu.Lock()
		_, has := mem.blobs[sha]
		mem.mu.Unlock()
		if has {
			return r.OpenBlob(ctx, sha)
		}
	}
	return nil, errors.New("no such blob")
}

// a kept blob's file carries a time made of its contents, so the version the runtime's code reads off the
// file (the guest's ETag) is the same on every replica of the hub.
func TestKeptBlobTimeIsMadeOfItsContents(t *testing.T) {
	room := newMemRoom()
	room.commit(map[string]string{"project/kit.css": "body{margin:0}"})
	if _, err := room.Snapshot(context.Background(), "p/", 1); err != nil {
		t.Fatal(err)
	}
	sha := shaOf("body{margin:0}")
	var times []time.Time
	for range 2 {
		v := New(Options{Rooms: func(context.Context, string) (Room, error) { return room, nil }, Dir: t.TempDir()})
		file, err := v.blob(context.Background(), room, "s1", sha)
		if err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		times = append(times, st.ModTime())
	}
	if !times[0].Equal(times[1]) || !times[0].Equal(blobTime(sha)) {
		t.Fatalf("two hubs keep the same blob with different times: %v", times)
	}
}

// the long poll of the library answers at once when the space has a revision past since and holds the ask
// until the wait ends otherwise.
func TestWaitLibraryAnswersANewRevision(t *testing.T) {
	room := newMemRoom()
	room.commit(map[string]string{"artifact.json": `{"title":"Лендинг"}`})
	v := New(Options{Rooms: func(context.Context, string) (Room, error) { return room, nil }, Dir: t.TempDir(), Poll: time.Millisecond})
	head, err := v.WaitLibrary(context.Background(), "s1", 0)
	if err != nil || head != 1 {
		t.Fatalf("past since: %d %v", head, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if got, err := v.WaitLibrary(ctx, "s1", head); err != nil || got != head {
		t.Fatalf("nothing new: %d %v", got, err)
	}
	room.commit(map[string]string{"artifact.json": `{"title":"Лендинг 2"}`})
	if got, err := v.WaitLibrary(context.Background(), "s1", head); err != nil || got <= head {
		t.Fatalf("new revision: %d %v", got, err)
	}
	if _, err := v.WaitLibrary(context.Background(), "../x", 0); err == nil {
		t.Fatal("a bad space was waited on")
	}
}

// a library item worked out once is kept in the hub's database; another replica, or the hub after a restart,
// gives it from there without reading the project's files again.
func TestLibraryItemIsKeptForEveryReplica(t *testing.T) {
	room := newMemRoom()
	room.commit(map[string]string{
		"artifact.json":        `{"title":"Лендинг"}`,
		"project/home.dc.html": "<html><body>home</body></html>",
	})
	kept := newMemDerived()
	replica := func() *View {
		v := New(Options{Rooms: func(context.Context, string) (Room, error) { return room, nil }, Dir: t.TempDir(), Poll: time.Millisecond})
		v.UseDerived(kept, "b1")
		return v
	}
	first, err := replica().Library(context.Background(), "s1", []string{"land"})
	if err != nil || len(first) != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	reads := room.reads
	second, err := replica().Library(context.Background(), "s1", []string{"land"})
	if err != nil || len(second) != 1 || second[0].Title != "Лендинг" || second[0].Rev != first[0].Rev || len(second[0].Boards) != 1 {
		t.Fatalf("second: %+v %v", second, err)
	}
	if room.reads != reads {
		t.Fatalf("the other replica read the project's files again: %d→%d", reads, room.reads)
	}
}
