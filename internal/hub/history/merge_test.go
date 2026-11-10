package history

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// HRN-883: the machine's projects and favorites join the person's sidebar the way the page joins them.
func TestAMachineJoinsTheSidebarAsThePageJoinsIt(t *testing.T) {
	doc := ProfileDoc{
		Projects: []ProfileProject{{ID: "pp-1", Name: "Работа", CreatedAt: 1,
			Members: []ProfileMember{{MachineID: "m-1", ProjectID: "a"}}}},
		Favorites: ProfileFavorites{Groups: []ProfileGroup{{ID: "fg-aaaa", Name: "Важное", Chats: []string{"c1"}}},
			Loose: []string{"c2"}},
		Merged: []string{"m-1"},
	}
	favs := &ProfileFavorites{
		Groups: []ProfileGroup{{ID: "fg-aaaa", Name: " важное ", Chats: []string{"c1", "c3"}},
			{ID: "fg-aaaa", Name: "Новое", Chats: []string{"c2", "c4"}}, {ID: "fg-bbbb", Name: " ", Chats: []string{"c5"}}},
		Loose: []string{"c6", "c3", "bad id"},
	}
	got, changed := MergeMachine(doc, "m-2", []MachineProject{
		{ID: "z", Name: "Своё", CreatedAt: 9}, {ID: "b", Name: "работа", CreatedAt: 2}, {ID: "x", Name: "", CreatedAt: 3},
	}, favs)
	if !changed {
		t.Fatal("a new machine changed nothing")
	}
	if len(got.Projects) != 2 || len(got.Projects[0].Members) != 2 || got.Projects[0].Members[1] != (ProfileMember{"m-2", "b"}) {
		t.Fatalf("projects: %+v", got.Projects)
	}
	if p := got.Projects[1]; p.Name != "Своё" || !strings.HasPrefix(p.ID, "pp-") || p.Members[0] != (ProfileMember{"m-2", "z"}) {
		t.Fatalf("a project of its own: %+v", p)
	}
	g := got.Favorites.Groups
	if len(g) != 2 || strings.Join(g[0].Chats, ",") != "c1,c3" || g[1].Name != "Новое" || g[1].ID == "fg-aaaa" ||
		strings.Join(g[1].Chats, ",") != "c4" {
		t.Fatalf("groups: %+v", g)
	}
	if strings.Join(got.Favorites.Loose, ",") != "c2,c6" || strings.Join(got.Merged, ",") != "m-1,m-2" {
		t.Fatalf("loose %v merged %v", got.Favorites.Loose, got.Merged)
	}
	if out, _ := json.Marshal(got); func() bool { _, ok := ParseSidebar(out); return !ok }() {
		t.Fatalf("the hub would not keep it: %s", out)
	}
	// Merged once: the favorites stay, a project made since still joins.
	again, changed := MergeMachine(got, "m-2", []MachineProject{{ID: "z", Name: "Своё", CreatedAt: 9}}, favs)
	if changed || len(again.Favorites.Loose) != 2 {
		t.Fatalf("merged twice: %v %+v", changed, again.Favorites)
	}
	if more, changed := MergeMachine(got, "m-2", []MachineProject{{ID: "n", Name: "Новый", CreatedAt: 20}}, favs); !changed ||
		len(more.Projects) != 3 {
		t.Fatalf("a new project did not join: %+v", more.Projects)
	}
}

func TestTheMergeIsWrittenOnTheRevisionItRead(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	if ok, _ := MachineMerged(ctx, db, "u-sergey", "m-1"); ok {
		t.Fatal("merged before anything")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i, m := range []string{"m-1", "m-2", "m-3"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := MergeMachineInto(ctx, db, "u-sergey", m, []MachineProject{{ID: "p", Name: "Работа", CreatedAt: int64(i)}},
				&ProfileFavorites{Loose: []string{"c-" + m}}, 1)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, raw, _ := Sidebar(ctx, db, "u-sergey")
	var doc ProfileDoc
	_ = json.Unmarshal(raw, &doc)
	if len(doc.Projects) != 1 || len(doc.Projects[0].Members) != 3 || len(doc.Merged) != 3 || len(doc.Favorites.Loose) != 3 {
		t.Fatalf("a write was lost: %s", raw)
	}
	for _, m := range []string{"m-1", "m-2", "m-3"} {
		if ok, err := MachineMerged(ctx, db, "u-sergey", m); !ok || err != nil {
			t.Fatalf("%s not merged: %v", m, err)
		}
	}
	if ok, _ := MachineMerged(ctx, db, "u-roma", "m-1"); ok {
		t.Fatal("another person's sidebar")
	}
}
