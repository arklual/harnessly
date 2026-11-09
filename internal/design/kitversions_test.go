package design

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// kitLibrary is the legacy library migrated, with a history and a made-up kit "acme-ds" 1.0.0 (one theme, "plain").
func kitLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "")
	root := legacyLibrary(t)
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	l := New(Config{DataDir: filepath.Join(tmp, "data"), LibraryDir: root, HistoryDir: filepath.Join(tmp, "history"),
		KitsDir: filepath.Join(tmp, "kits"), Texts: Texts{Tag: "test-design", CLI: "acme", HistoryEmail: "design@example.local"}})
	l.osUser = func() string { return "agent" }
	t.Cleanup(func() {
		l.StopWatching()
		l.StopBackground()
	})
	l.Migrate(false)
	l.listing.invalidateAll()
	theme := filepath.Join(tmp, "kits", "acme-ds", "1.0.0", "themes", "plain")
	writeFile(t, filepath.Join(tmp, "kits", "acme-ds", "1.0.0", "kit.json"), `{"id":"acme-ds","version":"1.0.0"}`)
	writeFile(t, filepath.Join(theme, "button.css"), ".button{color:gray}")
	writeFile(t, filepath.Join(theme, "README.md"), "# Plain")
	// An installed version is older than any edit of its draft: a draft compares an untouched file by its time.
	then := time.Now().Add(-time.Hour)
	_ = filepath.WalkDir(filepath.Join(tmp, "kits"), func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			_ = os.Chtimes(p, then, then)
		}
		return nil
	})
	return l, root
}

func kitCSS(t *testing.T, l *Library, project string) string {
	t.Helper()
	file := l.ResolveProjectFile(project, []string{"ds", "plain", "button.css"})
	if file == "" {
		return ""
	}
	return readFileT(t, file)
}

func TestAProjectOnAKitSeesTheDraftAndMovesWithThePublishedVersion(t *testing.T) {
	l, _ := kitLibrary(t)
	for _, p := range []string{"admin", "post"} {
		if err := l.SetKitPin(p, &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}}); err != nil {
			t.Fatal(p, err)
		}
		if got := kitCSS(t, l, p); got != ".button{color:gray}" {
			t.Fatalf("%s: the kit's file is not served from its version: %q", p, got)
		}
	}
	if l.ResolveProjectFile("admin", []string{"ds", "plain", "..", "..", "kit.json"}) != "" || l.ResolveProjectFile("admin", []string{"ds", "other", "button.css"}) != "" {
		t.Fatal("only the mounted theme is served, and only inside it")
	}
	// The other keys of the model stay when the pin is written.
	if m := l.ReadModel("admin"); m == nil || m.Meta.Title != "Admin" {
		t.Fatalf("model: %+v", m)
	}

	// «Править в ките»: the draft is made and the project goes on it; the other project does not.
	d, err := l.EditKitFrom("admin")
	if err != nil {
		t.Fatal(err)
	}
	if pin, _ := l.KitPinOf("admin"); !pin.Draft || pin.Version != "1.0.0" {
		t.Fatalf("admin is not on the draft: %+v", pin)
	}
	if l.ReadArtifact(d.Project) == nil || d.Base != "1.0.0" {
		t.Fatalf("draft: %+v", d)
	}
	if st := l.ProjectKit(d.Project); st.KitDraftOf != "acme-ds" {
		t.Fatalf("the draft's project does not say it is the kit's draft: %+v", st)
	}
	writeFile(t, filepath.Join(l.ProjectDir(d.Project), "themes", "plain", "button.css"), ".button{color:gold}")
	if got := kitCSS(t, l, "admin"); got != ".button{color:gold}" {
		t.Fatalf("the project on the draft does not see the edit: %q", got)
	}
	if got := kitCSS(t, l, "post"); got != ".button{color:gray}" {
		t.Fatalf("a project on its version sees the draft: %q", got)
	}
	changed := d.Project + "/" + ProjectDirname + "/themes/plain/button.css"
	if got := l.draftPathFor("admin", changed); got != "ds/plain/button.css" {
		t.Fatalf("the draft's change is not streamed to the project as its own path: %q", got)
	}
	if got := l.draftPathFor("post", changed); got != "" {
		t.Fatalf("a project on its version gets the draft's changes: %q", got)
	}
	// The project's change stream carries the draft's edit as the project's own path.
	got := make(chan []string, 4)
	stopWatch := l.watchDesign("admin", func(paths []string) { got <- paths })
	if stopWatch == nil {
		t.Fatal("the library is not watched")
	}
	time.Sleep(300 * time.Millisecond)
	writeFile(t, filepath.Join(l.ProjectDir(d.Project), "themes", "plain", "button.css"), ".button{color:gold}")
	select {
	case paths := <-got:
		if len(paths) != 1 || paths[0] != "ds/plain/button.css" {
			t.Fatalf("streamed: %v", paths)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the draft's edit is not streamed to the project")
	}
	stopWatch()
	board := `<link rel="stylesheet" href="ds/plain/button.css">`
	root, _ := filepath.EvalSymlinks(l.ProjectDir("admin"))
	if out := withAssetVersionsOf(board, root, root, nil, l.kitMissing("admin", root), nil); !strings.Contains(out, "button.css?tkv=") {
		t.Fatalf("a kit's file gets no version in the board: %s", out)
	}
	st, err := l.KitState("acme-ds", true)
	if err != nil || st.Draft == nil || st.Draft.ChangedCount != 1 || st.Draft.Changes[0].Path != "themes/plain/button.css" || len(st.Projects) != 2 {
		t.Fatalf("kit state: %+v %v", st, err)
	}

	// «Опубликовать версию»: minor by default; the project that edited goes on it, the other is told quietly.
	pub, err := l.PublishKitDraft("acme-ds", "", "  Кнопка золотая ")
	if err != nil {
		t.Fatal(err)
	}
	if pub.Version.Version != "1.1.0" || pub.Notes != "Кнопка золотая" || pub.ChangedCount != 1 {
		t.Fatalf("published: %+v", pub)
	}
	if pin, _ := l.KitPinOf("admin"); pin.Draft || pin.Version != "1.1.0" {
		t.Fatalf("the project that edited is not on the new version: %+v", pin)
	}
	if got := kitCSS(t, l, "admin"); got != ".button{color:gold}" {
		t.Fatalf("admin on 1.1.0: %q", got)
	}
	post := l.ProjectKit("post")
	if post.Pin == nil || post.Pin.Version != "1.0.0" || post.Update == nil || post.Update.Version.Version != "1.1.0" || post.Update.Notes != "Кнопка золотая" {
		t.Fatalf("post is not offered the new version: %+v", post)
	}
	if got := kitCSS(t, l, "post"); got != ".button{color:gray}" {
		t.Fatalf("post was updated silently: %q", got)
	}
	if _, err := l.UseKitVersion("post", "1.1.0", true); err != nil {
		t.Fatal(err)
	}
	if l.ProjectKit("post").Update != nil {
		t.Fatal("«Остаться» still offers the version")
	}
	if _, err := l.UseKitVersion("post", "1.1.0", false); err != nil || kitCSS(t, l, "post") != ".button{color:gold}" {
		t.Fatalf("«Обновить»: %v", err)
	}
	var ke *KitError
	if _, err := l.PublishKitDraft("acme-ds", "", ""); !errors.As(err, &ke) || ke.Status != 409 {
		t.Fatalf("a draft with no edits must not publish: %v", err)
	}

	// «Отменить черновик»: the projects on it go back to their version, the draft's project goes to the trash.
	if _, err := l.EditKitFrom("post"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(l.ProjectDir(d.Project), "themes", "plain", "button.css"), ".button{color:red}")
	if kitCSS(t, l, "post") != ".button{color:red}" {
		t.Fatal("post is not on the draft")
	}
	if err := l.DiscardKitDraft("acme-ds"); err != nil {
		t.Fatal(err)
	}
	if pin, _ := l.KitPinOf("post"); pin.Draft || pin.Version != "1.1.0" || kitCSS(t, l, "post") != ".button{color:gold}" {
		t.Fatalf("after discard: %+v", pin)
	}
	if l.ReadArtifact(d.Project) != nil {
		t.Fatal("the draft's project is still listed")
	}
	if st, _ := l.KitState("acme-ds", false); st.Draft != nil || st.Latest != "1.1.0" || len(st.Versions) != 2 {
		t.Fatalf("after discard: %+v", st)
	}
}

func TestACopyOfTheKitInTheProjectWinsOverThePin(t *testing.T) {
	l, _ := kitLibrary(t)
	if err := l.SetKitPin("admin", &KitPin{ID: "acme-ds", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(l.ProjectDir("admin"), "ds", "plain", "button.css"), ".own{}")
	if got := kitCSS(t, l, "admin"); got != ".own{}" {
		t.Fatalf("the project's own copy must win: %q", got)
	}
	writeFile(t, filepath.Join(l.KitsDir(), "acme-ds", "1.0.0", "themes", "plain", "card.css"), ".card{}")
	if f := l.ResolveProjectFile("admin", []string{"ds", "plain", "card.css"}); f == "" || readFileT(t, f) != ".card{}" {
		t.Fatalf("a file the project does not have comes from the kit: %q", f)
	}
	if dirs := l.KitMountDirs("admin"); dirs["plain"] == "" {
		t.Fatalf("exports add the kit's files the project does not have: %v", dirs)
	}
	if err := os.RemoveAll(filepath.Join(l.ProjectDir("admin"), "ds")); err != nil {
		t.Fatal(err)
	}
	if dirs := l.KitMountDirs("admin"); dirs["plain"] == "" {
		t.Fatalf("exports take the kit's mount: %v", dirs)
	}
}

func TestProjectVersionsNameCompareAndRestore(t *testing.T) {
	l, _ := kitLibrary(t)
	if err := l.SetKitPin("admin", &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}}); err != nil {
		t.Fatal(err)
	}
	board := filepath.Join(l.ProjectDir("admin"), "All.dc.html")
	v, err := l.SaveProjectVersion("admin", "  Для показа клиенту ", "Анна")
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "Для показа клиенту" || v.Kit == nil || v.Kit.Version != "1.0.0" || !fullSHA.MatchString(v.ID) {
		t.Fatalf("named: %+v", v)
	}
	if _, err := l.SaveProjectVersion("admin", " ", "x"); err == nil {
		t.Fatal("a version needs a name")
	}
	writeFile(t, board, "<main>all, edited</main>")
	writeFile(t, filepath.Join(l.ProjectDir("admin"), "New.dc.html"), "<main>new</main>")
	if err := l.SetKitPin("admin", &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}, Draft: true}); err != nil {
		t.Fatal(err)
	}
	diff, err := l.DiffProject("admin", v.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, b := range diff.Boards {
		kinds[b.Path] = b.Kind
	}
	if kinds["All.dc.html"] != "changed" || kinds["New.dc.html"] != "added" || diff.KitThen == nil || diff.KitThen.Draft || diff.KitNow == nil || !diff.KitNow.Draft {
		t.Fatalf("diff: %+v", diff)
	}
	h, err := l.ProjectHistory("admin", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Points) < 2 || len(h.Named) != 1 || h.Named[0].ID != v.ID {
		t.Fatalf("history: %+v", h)
	}
	named := false
	for _, p := range h.Points {
		named = named || (p.ID == v.ID && p.Name == "Для показа клиенту")
	}
	if !named {
		t.Fatalf("the named point is not marked: %+v", h.Points)
	}
	before := h.Points[0].ID
	if _, err := l.RestoreProjectVersion("admin", v.ID, "Анна"); err != nil {
		t.Fatal(err)
	}
	if got := readFileT(t, board); got != "<main>all</main>" {
		t.Fatalf("board after restore: %q", got)
	}
	if _, err := os.Stat(filepath.Join(l.ProjectDir("admin"), "New.dc.html")); err == nil {
		t.Fatal("a board the version did not have is still there")
	}
	if pin, _ := l.KitPinOf("admin"); pin.Draft {
		t.Fatal("the version's kit is not back")
	}
	// The restore is undone the same way: the state before it is a point of the history.
	if _, err := l.RestoreProjectVersion("admin", before, "Анна"); err != nil {
		t.Fatal(err)
	}
	if got := readFileT(t, board); got != "<main>all, edited</main>" {
		t.Fatalf("board after undoing the restore: %q", got)
	}
	if err := l.RenameProjectVersion("admin", v.ID, ""); err != nil {
		t.Fatal(err)
	}
	if h, _ := l.ProjectHistory("admin", 50); len(h.Named) != 0 {
		t.Fatalf("the name is not taken away: %+v", h.Named)
	}
}

func TestAdoptingPinsACopyToTheVersionItMatchesAndKeepsTheProjectsOwnFiles(t *testing.T) {
	l, root := kitLibrary(t)
	newer := filepath.Join(l.KitsDir(), "acme-ds", "1.1.0")
	writeFile(t, filepath.Join(newer, "kit.json"), `{"id":"acme-ds","version":"1.1.0"}`)
	writeFile(t, filepath.Join(newer, "themes", "plain", "button.css"), ".button{color:blue}")
	writeFile(t, filepath.Join(newer, "themes", "plain", "README.md"), "# Plain")
	// admin: the older version's file and a file of its own; post: a file the kit has with other bytes.
	writeFile(t, filepath.Join(l.ProjectDir("admin"), "ds", "plain", "button.css"), ".button{color:gray}")
	writeFile(t, filepath.Join(l.ProjectDir("admin"), "ds", "plain", "previews", "button.html"), "<p>own</p>")
	writeFile(t, filepath.Join(l.ProjectDir("post"), "ds", "plain", "button.css"), ".button{color:red}")
	dry, err := l.AdoptKitCopies("acme-ds", true, nil)
	if err != nil || dry.Latest != "1.1.0" || len(dry.Projects) != 2 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if _, err := os.Stat(filepath.Join(l.ProjectDir("admin"), "ds", "plain", "button.css")); err != nil {
		t.Fatal("a dry run moved something")
	}
	if none, _ := l.AdoptKitCopies("acme-ds", true, func(string) bool { return false }); len(none.Projects) != 0 {
		t.Fatalf("projects the caller may not change are not looked at: %+v", none)
	}
	rep, err := l.AdoptKitCopies("acme-ds", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]AdoptedProject{}
	for _, p := range rep.Projects {
		byID[p.Project] = p
	}
	if a := byID["admin"]; !a.Moved || a.Version != "1.0.0" || a.Kit != 1 || a.Own != 1 {
		t.Fatalf("admin: %+v", a)
	}
	if p := byID["post"]; p.Moved || p.Version != "" || len(p.Differs) != 1 {
		t.Fatalf("post: %+v", p)
	}
	if pin, ok := l.KitPinOf("admin"); !ok || pin.Version != "1.0.0" || pin.Mounts[0] != "plain" {
		t.Fatalf("admin pin: %+v", pin)
	}
	if _, ok := l.KitPinOf("post"); ok {
		t.Fatal("a copy no version matches is not pinned")
	}
	if got := kitCSS(t, l, "admin"); got != ".button{color:gray}" {
		t.Fatalf("admin shows what it showed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(l.ProjectDir("admin"), "ds", "plain", "button.css")); err == nil {
		t.Fatal("the kit's file is still copied in the project")
	}
	if f := l.ResolveProjectFile("admin", []string{"ds", "plain", "previews", "button.html"}); f == "" || readFileT(t, f) != "<p>own</p>" {
		t.Fatal("the project's own file must stay")
	}
	if k := l.ProjectKit("admin"); k.Update == nil || k.Update.Version.Version != "1.1.0" {
		t.Fatalf("the newer version is offered, not taken: %+v", k.Update)
	}
	if f := filepath.Join(root, ".trash", filepath.Base(byID["admin"].Trash), "plain", "button.css"); readFileT(t, f) != ".button{color:gray}" {
		t.Fatal("the kit's files are kept in the trash")
	}
}

func TestADraftMadeWhileTheLibraryIsWatchedStreamsItsEdits(t *testing.T) {
	l, _ := kitLibrary(t)
	if err := l.SetKitPin("admin", &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}}); err != nil {
		t.Fatal(err)
	}
	got := make(chan []string, 8)
	stopWatch := l.watchDesign("admin", func(paths []string) { got <- paths })
	if stopWatch == nil {
		t.Fatal("the library is not watched")
	}
	defer stopWatch()
	d, err := l.EditKitFrom("admin")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	for len(got) > 0 {
		<-got
	}
	writeFile(t, filepath.Join(l.ProjectDir(d.Project), "themes", "plain", "button.css"), ".button{color:gold}")
	deadline := time.After(5 * time.Second)
	for {
		select {
		case paths := <-got:
			for _, p := range paths {
				if p == "ds/plain/button.css" {
					return
				}
			}
		case <-deadline:
			t.Fatal("the edit of a draft made while watching is not streamed")
		}
	}
}
