package designview

import (
	"context"
	"testing"
	"time"
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
