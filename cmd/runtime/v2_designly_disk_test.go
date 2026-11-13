package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The list of an unchanged library reads nothing of the projects' folders again; a change in one project's folder has
// that project read again, and only it.
func TestTheListReadsAProjectAgainOnlyWhenItsFolderChanged(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	for _, id := range []string{"talk", "landing"} {
		if err := os.MkdirAll(filepath.Join(root, id, "meta"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lib := design.New(design.Config{LibraryDir: root, DataDir: data, Texts: design.Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(lib.StopWatching)
	if _, watched := lib.ListingGen("talk"); !watched {
		// Out of inotify watches (a shared CI runner): the list then reads every project each time, as it should.
		t.Skip("the library folder cannot be watched on this machine")
	}
	reads := map[string]int{}
	read := func(l *design.Library, id string) designlyProject {
		reads[id]++
		return readDesignlyProject(l, id)
	}
	var disk designlyDisk
	list := func() map[string]designlyProject {
		out := map[string]designlyProject{}
		for _, id := range []string{"talk", "landing"} {
			out[id] = disk.project(lib, id, read)
		}
		return out
	}
	if got := list(); got["talk"].deck || got["landing"].deck {
		t.Fatalf("no deck yet: %+v", got)
	}
	for range 10 {
		list()
	}
	if reads["talk"] != 1 || reads["landing"] != 1 {
		t.Fatalf("an unchanged library was read again: %v", reads)
	}

	if err := os.WriteFile(filepath.Join(root, "talk", filepath.FromSlash(design.DeckSettingsFile)), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !list()["talk"].deck {
		if time.Now().After(deadline) {
			t.Fatal("the project that became a deck was not read again")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reads["landing"] != 1 {
		t.Fatalf("the project that did not change was read again: %v", reads)
	}
}

// A conversation's links are read again only when the conversation moved on, and a conversation that is gone is
// forgotten.
func TestTheListReadsTheLinksOfTheConversationsThatMovedOn(t *testing.T) {
	var asked [][]string
	read := func(paths []string) map[string][]string {
		asked = append(asked, append([]string(nil), paths...))
		out := map[string][]string{}
		for _, p := range paths {
			if p == "/t/a.jsonl" {
				out[p] = []string{"landing"}
			}
		}
		return out
	}
	convs := []wire.Conversation{
		{ID: "a", TranscriptPath: "/t/a.jsonl", LastActivityAt: 10},
		{ID: "b", TranscriptPath: "/t/b.jsonl", LastActivityAt: 20},
		{ID: "c"},
	}
	var disk designlyDisk
	now := time.Now()
	got := disk.linked(convs, now, read)
	if len(asked) != 1 || len(asked[0]) != 2 || len(got) != 1 || got["/t/a.jsonl"][0] != "landing" {
		t.Fatalf("the first list: asked %v, got %v", asked, got)
	}
	for range 5 {
		got = disk.linked(convs, now.Add(time.Second), read)
	}
	if len(asked) != 1 || got["/t/a.jsonl"][0] != "landing" {
		t.Fatalf("quiet conversations were read again: %v, got %v", asked, got)
	}

	convs[1].LastActivityAt = 21
	disk.linked(convs, now.Add(2*time.Second), read)
	if len(asked) != 2 || len(asked[1]) != 1 || asked[1][0] != "/t/b.jsonl" {
		t.Fatalf("after one conversation moved on: %v", asked)
	}

	// What the index finds behind comes without the conversation moving: the links are not kept for good.
	disk.linked(convs, now.Add(2*time.Second+designlyLinksFor), read)
	if len(asked) != 3 || len(asked[2]) != 2 {
		t.Fatalf("after the links grew old: %v", asked)
	}

	disk.linked(convs[:1], now.Add(3*time.Second+designlyLinksFor), read)
	if _, kept := disk.links["/t/b.jsonl"]; kept {
		t.Fatal("a conversation that is gone is still kept")
	}
}
