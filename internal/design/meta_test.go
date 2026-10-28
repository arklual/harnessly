package design

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The page model: meta/ as the truth of a project, the files built from it, the page commands, claims by
// page id, the migration of a library, the canvas.json guard and the shipped new-project template.

func writeFile(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFileT(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func testLibrary(t *testing.T, root string) *Library {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "")
	l := New(Config{LibraryDir: root, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	l.osUser = func() string { return "agent" }
	t.Cleanup(l.StopWatching)
	return l
}

const wrapperBoard = `<iframe src="lib/page.html?id=payouts&v='+v+'"></iframe><script>const v = props.variant ?? 'Desktop'</script>`

// legacyLibrary is a library the way projects lie today: one listed in artifact.json with a wrapper and an unlisted
// file, one known only by canvas.json, one only by its files.
func legacyLibrary(t *testing.T) string {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "admin", ArtifactFile), `{
  "v": 1, "title": "Admin", "source": "zip", "importedAt": "2026-10-08T22:05:18.382Z", "extra": {"keep": true},
  "boards": [
    {"title": "All pages", "path": "All.dc.html", "w": null, "h": null},
    {"title": "Payouts", "path": "lib/page.html?id=payouts&v=Desktop&theme=dark", "w": 1440, "h": 1000},
    {"title": "Gone", "path": "Gone.dc.html"}
  ]
}`)
	writeFile(t, filepath.Join(root, "admin", "project", "All.dc.html"), "<main>all</main>")
	writeFile(t, filepath.Join(root, "admin", "project", "Payouts.dc.html"), wrapperBoard)
	writeFile(t, filepath.Join(root, "admin", "project", "lib", "page.html"), "<p>page</p>")
	writeFile(t, filepath.Join(root, "admin", "project", "Later.dc.html"), "<main>later</main>")
	writeFile(t, filepath.Join(root, "admin", "project", "_board-template.html"), "<main class=\"board\">TEMPLATE</main>")
	writeFile(t, filepath.Join(root, "admin", "project", "support.js"), "/* engine */")
	writeFile(t, filepath.Join(root, "post", ArtifactFile), `{"v":1,"title":"Post","boards":[{"title":"Post 4:5","path":"Main.dc.html","w":null,"h":null}]}`)
	writeFile(t, filepath.Join(root, "post", "project", canvasJSONName), `{
  "v": 3,
  "createdOnFiles": { "v": 1, "at": "2026-09-26T20:40:00Z" },
  "title": "Post",
  "launch": { "view": "canvas" },
  "pages": [],
  "boards": { "Main.dc.html": { "x": 10, "y": 20, "w": 1080, "h": 1350, "is_interactive": true, "expand": "fill" } },
  "order": ["Main.dc.html"],
  "notes": {"n1": "keep"},
  "designSystems": [{"title": "Kit", "version": "1"}],
  "futureKey": 42
}`)
	writeFile(t, filepath.Join(root, "post", "project", "Main.dc.html"), "<div>post</div>")
	writeFile(t, filepath.Join(root, "files", "project", "B.dc.html"), "<main>b</main>")
	writeFile(t, filepath.Join(root, "files", "project", "A.dc.html"), "<main>a</main>")
	writeFile(t, filepath.Join(root, "files", "project", "assets", "x.png"), "PNG")
	if err := os.MkdirAll(filepath.Join(root, "not-a-project"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

type boardLine struct {
	Title, Path string
	W, H        any
}

func listing(l *Library) map[string][]boardLine {
	out := map[string][]boardLine{}
	for _, a := range l.ListArtifacts() {
		var lines []boardLine
		for _, b := range a.Boards {
			lines = append(lines, boardLine{b.Title, b.Path, nullable(b.W), nullable(b.H)})
		}
		out[a.ID+" "+a.Title] = lines
	}
	return out
}

// inventory is the sha256 of every file of the library but the ones a migration is allowed to write.
func inventory(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if len(parts) >= 2 && (parts[1] == MetaDirname || parts[1] == ArtifactFile || (len(parts) == 3 && parts[1] == ProjectDirname && parts[2] == canvasJSONName)) {
			return nil
		}
		b, _ := os.ReadFile(p)
		sum := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	return out
}

func TestULID(t *testing.T) {
	id := ULID(0, [10]byte{})
	if id != "00000000000000000000000000" || !IsPageID(id) {
		t.Fatalf("zero: %s", id)
	}
	var e [10]byte
	for i := range e {
		e[i] = 0xff
	}
	if got := ULID(1<<48-1, e); got != "7ZZZZZZZZZZZZZZZZZZZZZZZZZ" {
		t.Fatalf("max: %s", got)
	}
	// The ULID spec's own example: 1469922850259 ms is 01ARZ3NDEK.
	if got := ULID(1469922850259, [10]byte{}); !strings.HasPrefix(got, "01ARZ3NDEK") {
		t.Fatalf("time part: %s", got)
	}
	a, b := ULID(5, e), ULID(6, [10]byte{})
	if !(a < b) {
		t.Fatalf("ids sort by time: %s %s", a, b)
	}
	if IsPageID("01arYZ6S41TSV4RRFFQ69G5FAV") || IsPageID("01ARYZ6S41TSV4RRFFQ69G5FA") || IsPageID("01ARYZ6S41TSV4RRFFQ69G5FAU") {
		t.Fatal("lower case, short and U are no ids")
	}
	if !IsPageID(NewPageID()) {
		t.Fatal("a new id")
	}
}

func TestMigrationKeepsTheListingAndTheFiles(t *testing.T) {
	root := legacyLibrary(t)
	before := inventory(t, root)
	l := testLibrary(t, root)
	shown := listing(l)

	dry := l.Migrate(true)
	if len(dry) != 4 {
		t.Fatalf("dry run: %+v", dry)
	}
	for _, p := range dry {
		if p.ID == "not-a-project" && p.Status != "skipped" || p.ID != "not-a-project" && p.Status != "migrate" {
			t.Fatalf("dry run %s: %+v", p.ID, p)
		}
	}
	for _, id := range []string{"admin", "post", "files"} {
		if exists(filepath.Join(root, id, MetaDirname)) {
			t.Fatalf("a dry run wrote %s/meta", id)
		}
	}
	if exists(filepath.Join(root, "admin", ProjectDirname, canvasJSONName)) {
		t.Fatal("a dry run wrote canvas.json")
	}

	report := l.Migrate(false)
	pages := map[string][]MigratedPage{}
	for _, p := range report {
		if p.ID != "not-a-project" && p.Status != "migrated" {
			t.Fatalf("migrate %s: %+v", p.ID, p)
		}
		pages[p.ID] = p.Pages
	}
	for i, p := range dry {
		if !jsonEqual(p.Pages, report[i].Pages) {
			t.Fatalf("the dry run promised other pages for %s: %+v vs %+v", p.ID, p.Pages, report[i].Pages)
		}
	}
	if after := inventory(t, root); !jsonEqual(before, after) {
		t.Fatalf("board or asset files changed:\n%v\n%v", before, after)
	}
	l.listing.invalidateAll()
	if got := listing(l); !jsonEqual(got, shown) {
		t.Fatalf("listing changed:\n%v\nwant\n%v", got, shown)
	}
	// What the listing showed is now the pages, in order, with ids; the gone board is not one.
	admin := pages["admin"]
	if len(admin) != 3 || admin[0].File != "All.dc.html" || admin[1].File != "lib/page.html?id=payouts&v=Desktop&theme=dark" || admin[2].File != "Later.dc.html" {
		t.Fatalf("admin pages: %+v", admin)
	}
	if !(admin[0].ID < admin[1].ID && admin[1].ID < admin[2].ID) {
		t.Fatalf("ids sort in the list's order: %+v", admin)
	}
	a := l.ReadArtifact("admin")
	for i, b := range a.Boards {
		if b.ID != admin[i].ID {
			t.Fatalf("the listing has no page ids: %+v", a.Boards)
		}
	}
	if files := pages["files"]; len(files) != 2 || files[0].File != "A.dc.html" {
		t.Fatalf("files pages: %+v", files)
	}
	m := l.ReadModel("admin")
	if m.Meta.Title != "Admin" || m.Meta.Source == nil || *m.Meta.Source != "zip" || m.Meta.Release != LegacyRelease || m.Meta.CreatedAt != "2026-10-08T22:05:18.382Z" {
		t.Fatalf("admin meta: %+v", m.Meta)
	}
	if p, _ := m.Page(admin[1].ID); p.W == nil || *p.W != 1440 {
		t.Fatalf("a size is kept: %+v", p)
	}
	post := l.ReadModel("post")
	if p := post.Pages[0]; p.Expand == nil || *p.Expand != "fill" || *p.W != 1080 || *p.H != 1350 || p.Title != "Post 4:5" {
		t.Fatalf("post page: %+v", p)
	}

	// The built files: canvas.json keeps what the design tool wrote, artifact.json what was there.
	canvas, _ := readJSON(filepath.Join(root, "post", ProjectDirname, canvasJSONName)).(*jsvalue.Object)
	if n, _ := canvas.Number("futureKey"); n != 42 || canvas.Object("notes").String("n1") != "keep" || len(canvas.Array("designSystems")) != 1 {
		t.Fatalf("canvas keys lost: %s", jsvalue.Stringify(canvas, ""))
	}
	main := canvas.Object("boards").Object("Main.dc.html")
	if x, _ := main.Number("x"); x != 10 || main.String("expand") != "fill" || main.String("title") != "Post 4:5" {
		t.Fatalf("canvas board: %s", jsvalue.Stringify(main, ""))
	}
	if v, _ := main.Get("is_interactive"); v != true {
		t.Fatalf("a board's own key is lost: %s", jsvalue.Stringify(main, ""))
	}
	own := canvas.Object(DefaultCanvasKey)
	if own == nil || len(own.Array("pages")) != 1 || own.Array("pages")[0].(*jsvalue.Object).String("id") != post.Pages[0].ID {
		t.Fatalf("canvas own data: %s", jsvalue.Stringify(canvas, ""))
	}
	adminCanvas, _ := readJSON(filepath.Join(root, "admin", ProjectDirname, canvasJSONName)).(*jsvalue.Object)
	if order := jsvalue.Strings(adminCanvas.Array("order")); strings.Join(order, ",") != "All.dc.html,Later.dc.html" {
		t.Fatalf("canvas order holds the board files only: %v", order)
	}
	later := adminCanvas.Object("boards").Object("Later.dc.html")
	if x, _ := later.Number("x"); x != canvasDefaultW+canvasGap {
		t.Fatalf("grid place: %s", jsvalue.Stringify(later, ""))
	}
	if len(adminCanvas.Object(DefaultCanvasKey).Array("pages")) != 3 {
		t.Fatal("canvas own data lists every page")
	}
	art, _ := readJSON(filepath.Join(root, "admin", ArtifactFile)).(*jsvalue.Object)
	if art.String("importedAt") != "2026-10-08T22:05:18.382Z" || art.Object("extra") == nil || len(art.Array("boards")) != 3 ||
		art.Array("boards")[0].(*jsvalue.Object).String("id") != admin[0].ID {
		t.Fatalf("artifact.json: %s", jsvalue.Stringify(art, ""))
	}
	// An old reader of artifact.json still sees the same boards.
	if old := ParseArtifactFile(art); len(old.Boards) != 3 || old.Boards[2].Path != "Later.dc.html" {
		t.Fatalf("old reader: %+v", old)
	}

	// Again: nothing to do, nothing written.
	stamp := map[string]time.Time{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			st, _ := os.Stat(p)
			stamp[p] = st.ModTime()
		}
		return nil
	})
	time.Sleep(20 * time.Millisecond)
	for _, p := range l.Migrate(false) {
		if p.ID != "not-a-project" && (p.Status != "current" || len(p.Writes) != 0) {
			t.Fatalf("second run %s: %+v", p.ID, p)
		}
	}
	for p, at := range stamp {
		if st, _ := os.Stat(p); !st.ModTime().Equal(at) {
			t.Fatalf("second run wrote %s", p)
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// Two machines migrating the same library on their own get the same bytes, so the folder sync has nothing to merge.
func TestMigrationIsTheSameOnTwoMachines(t *testing.T) {
	one, two := legacyLibrary(t), legacyLibrary(t)
	testLibrary(t, one).Migrate(false)
	testLibrary(t, two).Migrate(false)
	for _, rel := range []string{"admin/meta", "post/meta", "files/meta"} {
		a, b := map[string]string{}, map[string]string{}
		for dir, out := range map[string]map[string]string{one: a, two: b} {
			_ = filepath.WalkDir(filepath.Join(dir, rel), func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					r, _ := filepath.Rel(dir, p)
					out[r] = readFileT(t, p)
				}
				return nil
			})
		}
		if len(a) == 0 || !jsonEqual(a, b) {
			t.Fatalf("%s differs:\n%v\n%v", rel, a, b)
		}
	}
	for _, rel := range []string{"admin/project/canvas.json", "admin/artifact.json", "post/project/canvas.json"} {
		if readFileT(t, filepath.Join(one, rel)) != readFileT(t, filepath.Join(two, rel)) {
			t.Fatalf("%s differs", rel)
		}
	}
}

func TestMigrationRefusesAHalfMetaFolder(t *testing.T) {
	root := legacyLibrary(t)
	if err := os.MkdirAll(filepath.Join(root, "files", MetaDirname, MetaPagesDirname), 0o755); err != nil {
		t.Fatal(err)
	}
	l := testLibrary(t, root)
	if p := l.MigrateProject("files", false); p.Status != "error" || !strings.Contains(p.Error, "without project.json") {
		t.Fatalf("half meta: %+v", p)
	}
}

func migratedLibrary(t *testing.T) (*Library, string) {
	root := legacyLibrary(t)
	l := testLibrary(t, root)
	l.Migrate(false)
	l.listing.invalidateAll()
	return l, root
}

func TestPageCommands(t *testing.T) {
	l, root := migratedLibrary(t)
	project := filepath.Join(root, "admin", ProjectDirname)

	p, err := l.CreatePage("admin", PageSpec{Title: "Refunds / new", Route: strPtr("/refunds")})
	if err != nil {
		t.Fatal(err)
	}
	if !IsPageID(p.ID) || p.File != "Refunds - new.dc.html" || *p.Route != "/refunds" {
		t.Fatalf("created: %+v", p)
	}
	if readFileT(t, filepath.Join(project, p.File)) != "<main class=\"board\">TEMPLATE</main>" {
		t.Fatal("a new page starts as the project's board template")
	}
	a := l.ReadArtifact("admin")
	if last := a.Boards[len(a.Boards)-1]; last.ID != p.ID || last.Title != "Refunds / new" {
		t.Fatalf("the new page is listed last: %+v", a.Boards)
	}
	canvas, _ := readJSON(filepath.Join(project, canvasJSONName)).(*jsvalue.Object)
	if canvas.Object("boards").Object(p.File) == nil {
		t.Fatal("canvas.json is rebuilt after a change")
	}
	if again, err := l.CreatePage("admin", PageSpec{Title: "Refunds / new"}); err != nil || again.File != "Refunds - new 2.dc.html" {
		t.Fatalf("a free name: %+v %v", again, err)
	}
	if _, err := l.CreatePage("admin", PageSpec{Title: "x", File: "All.dc.html"}); err == nil || err.(*PageError).Status != 409 {
		t.Fatalf("a file that is a page already: %v", err)
	}
	if _, err := l.CreatePage("admin", PageSpec{Title: "x", File: "../x.dc.html"}); err == nil || err.(*PageError).Status != 400 {
		t.Fatalf("a path: %v", err)
	}
	if _, err := l.CreatePage("admin", PageSpec{Title: " "}); err == nil {
		t.Fatal("an empty title")
	}
	// A file an agent made by hand shows without an id until it is registered.
	writeFile(t, filepath.Join(project, "Hand.dc.html"), "<main>hand</main>")
	l.listing.invalidateAll()
	list, _ := l.ListPages("admin")
	if strings.Join(list.Unregistered, ",") != "Hand.dc.html" {
		t.Fatalf("unregistered: %+v", list.Unregistered)
	}
	hand, err := l.CreatePage("admin", PageSpec{Title: "Hand made", File: "Hand"})
	if err != nil || hand.File != "Hand.dc.html" || readFileT(t, filepath.Join(project, "Hand.dc.html")) != "<main>hand</main>" {
		t.Fatalf("adopt: %+v %v", hand, err)
	}
	variant, err := l.CreatePage("admin", PageSpec{Title: "Hand, dark", VariantOf: "Hand made"})
	if err != nil || *variant.VariantOf != hand.ID || readFileT(t, filepath.Join(project, variant.File)) != "<main>hand</main>" {
		t.Fatalf("variant: %+v %v", variant, err)
	}

	renamed, err := l.RenamePage("admin", hand.ID, strPtr("Handmade"), strPtr("Handmade.dc.html"))
	if err != nil || renamed.ID != hand.ID || renamed.File != "Handmade.dc.html" || exists(filepath.Join(project, "Hand.dc.html")) || !exists(filepath.Join(project, "Handmade.dc.html")) {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	if _, err := l.RenamePage("admin", "Handmade", nil, strPtr("All.dc.html")); err == nil || err.(*PageError).Status != 409 {
		t.Fatalf("rename onto a file: %v", err)
	}
	if _, err := l.RenamePage("admin", "Payouts", nil, strPtr("P.dc.html")); err == nil || err.(*PageError).Status != 422 {
		t.Fatalf("rename a wrapper's file: %v", err)
	}

	removed, err := l.RemovePage("admin", "Handmade.dc.html")
	if err != nil || removed.ID != hand.ID || exists(filepath.Join(project, "Handmade.dc.html")) || exists(filepath.Join(root, "admin", MetaDirname, MetaPagesDirname, hand.ID+".json")) {
		t.Fatalf("remove: %+v %v", removed, err)
	}
	trash, _ := os.ReadDir(filepath.Join(root, ".trash"))
	if len(trash) != 2 {
		t.Fatalf("trash: %v", trash)
	}
	if _, err := l.RemovePage("admin", hand.ID); err == nil || err.(*PageError).Status != 404 {
		t.Fatalf("remove again: %v", err)
	}
	if _, err := l.CreatePage("nope", PageSpec{Title: "x"}); err == nil || err.(*PageError).Status != 404 {
		t.Fatalf("no project: %v", err)
	}
	writeFile(t, filepath.Join(root, "legacy", "project", "A.dc.html"), "a")
	if _, err := l.CreatePage("legacy", PageSpec{Title: "x"}); err == nil || err.(*PageError).Status != 409 || !strings.Contains(err.Error(), "acme design migrate") {
		t.Fatalf("no meta: %v", err)
	}
}

func strPtr(s string) *string { return &s }

func TestPanelEditsOfAProjectWithMeta(t *testing.T) {
	l, root := migratedLibrary(t)
	project := filepath.Join(root, "files", ProjectDirname)
	writeFile(t, filepath.Join(project, "C.dc.html"), "<main>c</main>")
	l.listing.invalidateAll()
	boards, err := l.EditBoards("files", BoardEdit{Action: "rename", Path: strPtr("A.dc.html"), Title: strPtr("Alpha")})
	if err != nil || boards[0].Title != "Alpha" || boards[2].Path != "C.dc.html" || boards[2].ID == "" {
		t.Fatalf("rename (and the hand-made board gets a page): %+v %v", boards, err)
	}
	ids := []string{boards[0].ID, boards[1].ID, boards[2].ID}
	one := 1.0
	boards, err = l.EditBoards("files", BoardEdit{Action: "move", Path: strPtr("A.dc.html"), Delta: &one})
	if err != nil || boards[0].ID != ids[1] || boards[1].ID != ids[0] {
		t.Fatalf("move: %+v %v", boards, err)
	}
	boards, err = l.EditBoards("files", BoardEdit{Action: "duplicate", Path: strPtr("A.dc.html")})
	if err != nil || len(boards) != 4 || boards[2].Title != "Alpha copy" || boards[2].ID == "" || !exists(filepath.Join(project, "A copy.dc.html")) {
		t.Fatalf("duplicate: %+v %v", boards, err)
	}
	boards, err = l.EditBoards("files", BoardEdit{Action: "remove", Path: strPtr("C.dc.html")})
	if err != nil || len(boards) != 3 || exists(filepath.Join(project, "C.dc.html")) {
		t.Fatalf("remove: %+v %v", boards, err)
	}
	if _, err := l.EditBoards("files", BoardEdit{Action: "title", Title: strPtr("Files")}); err != nil || l.ReadModel("files").Meta.Title != "Files" {
		t.Fatalf("title: %v", err)
	}
	art, _ := readJSON(filepath.Join(root, "files", ArtifactFile)).(*jsvalue.Object)
	if art.String("title") != "Files" || len(art.Array("boards")) != 3 {
		t.Fatalf("artifact.json follows: %s", jsvalue.Stringify(art, ""))
	}
	if got := l.ReadArtifact("files"); got.Title != "Files" || got.Boards[0].ID != ids[1] {
		t.Fatalf("listing: %+v", got)
	}
}

func TestClaimsFollowThePageID(t *testing.T) {
	l, root := migratedLibrary(t)
	a := l.ReadArtifact("files")
	pageA := a.Boards[0].ID
	key, err := ResolveBoardKey(a, "A")
	if err != nil || key != pageA {
		t.Fatalf("key: %s %v", key, err)
	}
	for _, name := range []string{"A.dc.html", pageA} {
		if k, _ := ResolveBoardKey(a, name); k != pageA {
			t.Fatalf("%s: %s", name, k)
		}
	}
	if k, _ := ResolveBoardKey(a, "New board"); k != "New board" {
		t.Fatalf("a board not made yet keeps its name: %s", k)
	}
	owner := ClaimOwner{User: "Ann", Runtime: "agent", TabID: "tab-1", ChatTitle: "Chat"}
	now := nowMs()
	if res, err := l.ClaimBoard("files", key, owner, now); err != nil || res.Held != nil {
		t.Fatalf("claim: %+v %v", res, err)
	}
	if _, err := l.RenamePage("files", pageA, strPtr("Alpha"), strPtr("Alpha.dc.html")); err != nil {
		t.Fatal(err)
	}
	l.listing.invalidateAll()
	a = l.ReadArtifact("files")
	if k, _ := ResolveBoardKey(a, "Alpha"); k != pageA {
		t.Fatalf("after a rename: %s", k)
	}
	other := ClaimOwner{User: "Bob", Runtime: "agent", TabID: "tab-2", ChatTitle: "Other"}
	if res, _ := l.ClaimBoard("files", pageA, other, now+1); res.Held == nil || res.Held.Owner.TabID != "tab-1" {
		t.Fatalf("the rename kept the claim: %+v", res)
	}
	if !l.claimNamesFile("files", pageA, "Alpha.dc.html") || l.claimNamesFile("files", pageA, "B.dc.html") || !l.claimNamesFile("files", "B", "B.dc.html") {
		t.Fatal("claimNamesFile")
	}
	l.SetKeeper(func() ([]Chat, bool) {
		return []Chat{{TabID: "tab-1", PanelType: "claude-code", State: "busy"}}, true
	})
	arts := []*Artifact{a}
	if err := l.AttachClaims(arts); err != nil || len(arts[0].Claims) != 1 || arts[0].Claims[0].Board != "Alpha" || arts[0].Claims[0].Page != pageA {
		t.Fatalf("the panel sees the board's key and the page: %+v %v", arts[0].Claims, err)
	}
	lines, err := l.WhoHolds("files")
	if err != nil || len(lines) != 1 || lines[0].Board != "Alpha" || lines[0].Page != pageA {
		t.Fatalf("who: %+v %v", lines, err)
	}
	_ = root
}

// A runtime that knows no meta/ claims the board under its old key; this runtime does not let another chat take the
// page meanwhile.
func TestAnOldKeyClaimHoldsThePage(t *testing.T) {
	l, _ := migratedLibrary(t)
	old := ClaimOwner{User: "Bob", Runtime: "agent", TabID: "tab-old", ChatTitle: "Old runtime"}
	if res, err := l.ClaimBoard("files", "B", old, nowMs()); err != nil || res.Held != nil {
		t.Fatalf("old claim: %+v %v", res, err)
	}
	l.SetKeeper(func() ([]Chat, bool) {
		return []Chat{{TabID: "tab-old", PanelType: "claude-code", State: "busy"}, {TabID: "tab-new", PanelType: "claude-code", State: "busy", Session: "s-new"}}, true
	})
	out, err := l.ClaimForChat("s-new", "files", "B")
	if err != nil || out.Holder == nil || out.Holder.Owner.TabID != "tab-old" || !strings.Contains(out.Message, "\"B\"") {
		t.Fatalf("held under the old key: %+v %v", out, err)
	}
	if _, err := l.ReleaseForChat("s-new", "files", "B"); err != nil {
		t.Fatal(err)
	}
	if len(l.LiveClaims("files", nowMs())) != 1 {
		t.Fatal("another chat's claim is not released")
	}
	// The holder's own claim of the page then goes through.
	l.SetKeeper(func() ([]Chat, bool) {
		return []Chat{{TabID: "tab-old", PanelType: "claude-code", State: "busy", Session: "s-old"}}, true
	})
	out, err = l.ClaimForChat("s-old", "files", "B")
	if err != nil || out.Holder != nil || out.Claim.Board != "B" || !IsPageID(out.Page) {
		t.Fatalf("own: %+v %v", out, err)
	}
	released, err := l.ReleaseForChat("s-old", "files", "B")
	if err != nil || len(released) != 2 {
		t.Fatalf("both keys released: %+v %v", released, err)
	}
}

func TestCanvasGuard(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "p", ArtifactFile), "{}")
	writeFile(t, filepath.Join(root, "p", ProjectDirname, canvasJSONName), "{}")
	writeFile(t, filepath.Join(root, "q", "project", canvasJSONName), "{}")
	canvas := filepath.Join(root, "p", ProjectDirname, canvasJSONName)
	call := func(tool, key, value, cwd string) bool {
		raw, _ := json.Marshal(map[string]any{"tool_name": tool, "tool_input": map[string]any{key: value}, "cwd": cwd})
		out := GuardHookAnswer(raw, "acme")
		if out != nil && !strings.Contains(string(out), `"permissionDecision":"deny"`) {
			t.Fatalf("answer: %s", out)
		}
		return out != nil
	}
	cases := []struct {
		tool, key, value, cwd string
		deny                  bool
	}{
		{"Write", "file_path", canvas, "", true},
		{"Edit", "file_path", "project/canvas.json", filepath.Join(root, "p"), true},
		{"MultiEdit", "file_path", canvas, "", true},
		{"Read", "file_path", canvas, "", false},
		{"Write", "file_path", filepath.Join(root, "p", ProjectDirname, "Main.dc.html"), "", false},
		{"Write", "file_path", filepath.Join(root, "q", "project", canvasJSONName), "", false},
		{"Bash", "command", "cat " + canvas, "", false},
		{"Bash", "command", "jq . p/project/canvas.json 2>/dev/null | head", "", false},
		{"Bash", "command", "echo '{}' > p/project/canvas.json", "", true},
		{"Bash", "command", "jq '.x=1' p/project/canvas.json | tee p/project/canvas.json", "", true},
		{"Bash", "command", "sed -i 's/a/b/' p/project/canvas.json", "", true},
		{"Bash", "command", "python3 -c \"import json;json.dump({}, open('p/project/canvas.json','w'))\"", "", true},
		{"Bash", "command", "ls p/project", "", false},
	}
	for _, c := range cases {
		if got := call(c.tool, c.key, c.value, c.cwd); got != c.deny {
			t.Errorf("%s %s: deny %v, want %v", c.tool, c.value, got, c.deny)
		}
	}
	if GuardHookAnswer([]byte("not json"), "acme") != nil {
		t.Fatal("broken input is let through")
	}
	_, reason := GuardCanvas(GuardInput{ToolName: "Write"}, "acme")
	if reason != "" {
		t.Fatal("no reason without a refusal")
	}
	in := GuardInput{ToolName: "Write"}
	in.ToolInput.FilePath = canvas
	if deny, reason := GuardCanvas(in, "acme"); !deny || !strings.Contains(reason, "acme design page") {
		t.Fatalf("reason: %s", reason)
	}
}

func TestCreateWithShippedTemplate(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	p, err := l.CreateProject("Landing")
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(p.Dir, ProjectDirname)
	if readFileT(t, filepath.Join(project, SupportJSName)) != string(testEngine) {
		t.Fatal("support.js is the machine's engine")
	}
	if !strings.Contains(readFileT(t, filepath.Join(project, "_board-template.html")), `<script src="./support.js"></script>`) {
		t.Fatal("the board template pulls in support.js")
	}
	m := l.ReadModel(p.ID)
	if m == nil || m.Meta.Title != "Landing" || m.Meta.Release != LegacyRelease || len(m.Pages) != 0 || m.Meta.CreatedAt == "" {
		t.Fatalf("meta: %+v", m)
	}
	if !exists(filepath.Join(project, canvasJSONName)) {
		t.Fatal("canvas.json is built")
	}
	page, err := l.CreatePage(p.ID, PageSpec{Title: "Home"})
	if err != nil || readFileT(t, filepath.Join(project, page.File)) != readFileT(t, filepath.Join(project, "_board-template.html")) {
		t.Fatalf("first page: %+v %v", page, err)
	}
	if a := l.ReadArtifact(p.ID); len(a.Boards) != 1 || a.Boards[0].ID != page.ID {
		t.Fatalf("listed: %+v", a.Boards)
	}

	// A configured engine replaces the shipped one; a template's meta keeps its settings but never its pages.
	writeFile(t, filepath.Join(root, "engine.js"), "/* newer engine */")
	l2 := New(Config{LibraryDir: root, EngineFile: filepath.Join(root, "engine.js"), Texts: Texts{CLI: "acme"}})
	t.Cleanup(l2.StopWatching)
	p2, err := l2.CreateProject("Second")
	if err != nil || readFileT(t, filepath.Join(p2.Dir, ProjectDirname, SupportJSName)) != "/* newer engine */" {
		t.Fatalf("engine file: %v", err)
	}
	tpl := filepath.Join(root, TemplateDirname)
	writeFile(t, filepath.Join(tpl, ArtifactFile), `{"title":"x"}`)
	writeFile(t, filepath.Join(tpl, ProjectDirname, "_board-template.html"), "own")
	writeFile(t, filepath.Join(tpl, MetaDirname, MetaProjectFile), `{"v":1,"title":"T","release":"1792547779-0491","kits":["kit@1"],"tailwind":true,"x":1}`)
	writeFile(t, filepath.Join(tpl, MetaDirname, MetaPagesDirname, "01ARYZ6S41TSV4RRFFQ69G5FAV.json"), `{"id":"01ARYZ6S41TSV4RRFFQ69G5FAV","title":"T","file":"T.dc.html"}`)
	p3, err := l.CreateProject("Third")
	if err != nil {
		t.Fatal(err)
	}
	m3 := l.ReadModel(p3.ID)
	if m3.Meta.Title != "Third" || m3.Meta.Release != "1792547779-0491" || !m3.Meta.Tailwind || len(m3.Meta.Kits) != 1 || len(m3.Pages) != 0 {
		t.Fatalf("template meta: %+v", m3)
	}
	if v, _ := m3.Meta.raw.Get("x"); v != 1.0 {
		t.Fatalf("a template's own key is kept: %v", v)
	}
}

func TestWriteIfChangedTouchesNothingWhenSame(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.json")
	if changed, err := writeIfChanged(file, []byte("x")); !changed || err != nil {
		t.Fatal(changed, err)
	}
	st, _ := os.Stat(file)
	time.Sleep(10 * time.Millisecond)
	if changed, err := writeIfChanged(file, []byte("x")); changed || err != nil {
		t.Fatal(changed, err)
	}
	if st2, _ := os.Stat(file); !st2.ModTime().Equal(st.ModTime()) {
		t.Fatal("rewritten")
	}
	if err := os.Symlink(file, filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeIfChanged(filepath.Join(dir, "link.json"), []byte("y")); err == nil || readFileT(t, file) != "x" {
		t.Fatal("written through a link")
	}
	names, _ := os.ReadDir(dir)
	var left []string
	for _, n := range names {
		left = append(left, n.Name())
	}
	sort.Strings(left)
	if strings.Join(left, ",") != "a.json,link.json" {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestPagesRoute(t *testing.T) {
	l, _ := migratedLibrary(t)
	a := &API{L: l, CLIToken: func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" }}
	call := func(method, target, body string) (int, string) {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, target, nil)
		} else {
			r = httptest.NewRequest(method, target, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("x-cli-token", "ok")
		code, v, _ := a.CLIPages(r).Split()
		b, _ := json.Marshal(v)
		return code, string(b)
	}
	if code, body := call("GET", "/api/cli/design/pages?project=files", ""); code != 200 || !strings.Contains(body, `"file":"A.dc.html"`) || !strings.Contains(body, `"present":true`) {
		t.Fatalf("list: %d %s", code, body)
	}
	code, body := call("POST", "/api/cli/design/pages", `{"action":"create","project":"files","title":"New","w":390,"h":844,"expand":true}`)
	if code != 200 || !strings.Contains(body, `"file":"New.dc.html"`) || !strings.Contains(body, `"w":390`) || !strings.Contains(body, `"expand":"fill"`) {
		t.Fatalf("create: %d %s", code, body)
	}
	if code, body := call("POST", "/api/cli/design/pages", `{"action":"rename","project":"files","page":"New","title":"Newer"}`); code != 200 || !strings.Contains(body, `"title":"Newer"`) {
		t.Fatalf("rename: %d %s", code, body)
	}
	if code, body := call("POST", "/api/cli/design/pages", `{"action":"remove","project":"files","page":"Newer"}`); code != 200 {
		t.Fatalf("remove: %d %s", code, body)
	}
	if code, _ := call("POST", "/api/cli/design/pages", `{"action":"remove","project":"files","page":"Newer"}`); code != 404 {
		t.Fatalf("remove again: %d", code)
	}
	if code, _ := call("POST", "/api/cli/design/pages", `{"action":"fly","project":"files"}`); code != 400 {
		t.Fatalf("unknown action: %d", code)
	}
	r := httptest.NewRequest("GET", "/api/cli/design/pages?project=files", nil)
	if code, _, _ := a.CLIPages(r).Split(); code != 403 {
		t.Fatalf("no token: %d", code)
	}
}

// The canvas key is fixed in meta/project.json when meta/ is made: a command or runtime configured with another key
// builds the same canvas.json, so the two never rewrite each other's file.
func TestTheCanvasKeyIsTheProjects(t *testing.T) {
	root := legacyLibrary(t)
	t.Setenv("RUNTIME_LOCALE", "")
	l := New(Config{LibraryDir: root, CanvasKey: "acme"})
	t.Cleanup(l.StopWatching)
	l.Migrate(false)
	if l.ReadModel("post").Meta.CanvasKey != "acme" {
		t.Fatal("the key is not kept")
	}
	canvas, _ := readJSON(filepath.Join(root, "post", ProjectDirname, canvasJSONName)).(*jsvalue.Object)
	if canvas.Object("acme") == nil || canvas.Has(DefaultCanvasKey) {
		t.Fatalf("canvas: %s", jsvalue.Stringify(canvas, ""))
	}
	other := New(Config{LibraryDir: root})
	t.Cleanup(other.StopWatching)
	for _, p := range other.Migrate(false) {
		if len(p.Writes) != 0 {
			t.Fatalf("%s rewritten under another key: %v", p.ID, p.Writes)
		}
	}
	if New(Config{LibraryDir: root, CanvasKey: "bad key!"}).canvasKey() != DefaultCanvasKey {
		t.Fatal("a bad key is not used")
	}
}
