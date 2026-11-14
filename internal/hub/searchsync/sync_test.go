package searchsync

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/designaccess"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/searchindex"
)

type lib struct {
	items map[string]designaccess.LibraryItem
}

func (l *lib) Library(_ context.Context, _ string, ids []string) ([]designaccess.LibraryItem, error) {
	var out []designaccess.LibraryItem
	for _, id := range ids {
		if it, ok := l.items[id]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

type appList []hubproto.SpaceApp

func (a appList) SpaceApps(context.Context, string) ([]hubproto.SpaceApp, error) { return a, nil }

func TestWritesReachTheIndex(t *testing.T) {
	ctx := context.Background()
	ix, _, err := searchindex.OpenSQLite(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	l := &lib{items: map[string]designaccess.LibraryItem{"site": {ID: "site", Title: "Лендинг", UpdatedAt: 1,
		Boards: []designaccess.LibraryBoard{{Title: "Главная"}}}}}
	s := &Sync{Index: ix, Library: l, Apps: appList{{ID: "crm", Name: "CRM клиентов"}}}
	find := func(q string) int {
		h, err := ix.Search(ctx, searchindex.Query{Space: "sp", Text: q})
		if err != nil {
			t.Fatal(err)
		}
		return len(h)
	}
	s.Hear("sp", "design", "site/index.html", false)
	s.Hear("sp", "design", ".folders/x.json", false) // not a project
	s.Hear("sp", "apps", apps.TeamStateFile, false)
	s.Flush(ctx)
	if find("лендинг") != 1 || find("главная") != 1 || find("crm") != 1 {
		t.Fatal("the written things are not found")
	}
	// The project's files are gone from the store: it leaves the index.
	delete(l.items, "site")
	s.Hear("sp", "design", "site/index.html", true)
	s.Flush(ctx)
	if find("лендинг") != 0 {
		t.Fatal("a removed project is still found")
	}
}

// a record of a system, a video or a deck puts the entity into the index by its title, with no files in the
// design collection; a record that is gone takes it out.
func TestRecordsReachTheIndex(t *testing.T) {
	ctx := context.Background()
	ix, _, err := searchindex.OpenSQLite(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	recs := map[string]filestore.Record{"brand": {ID: "brand", Title: "Фирменная система", Kind: "ds", At: 9}}
	s := &Sync{Index: ix, Library: &lib{items: map[string]designaccess.LibraryItem{}},
		Record: func(_ context.Context, _, id string) (filestore.Record, bool, error) {
			r, ok := recs[id]
			return r, ok, nil
		}}
	find := func(q string) int {
		h, err := ix.Search(ctx, searchindex.Query{Space: "sp", Text: q})
		if err != nil {
			t.Fatal(err)
		}
		return len(h)
	}
	s.Hear("sp", filestore.KindSystems, "brand.json", false)
	s.Hear("sp", filestore.KindSystems, "sub/x.json", false) // not a record
	s.Flush(ctx)
	if find("фирменная") != 1 {
		t.Fatal("a record without files is not found")
	}
	delete(recs, "brand")
	s.Hear("sp", filestore.KindSystems, "brand.json", true)
	s.Flush(ctx)
	if find("фирменная") != 0 {
		t.Fatal("a removed record is still found")
	}
}

func TestTeamStateFileIsTheAppsOne(t *testing.T) {
	if teamStateFile != apps.TeamStateFile {
		t.Fatalf("teamStateFile = %q, apps says %q", teamStateFile, apps.TeamStateFile)
	}
}
