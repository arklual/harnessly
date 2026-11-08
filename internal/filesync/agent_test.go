package filesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

const tokenA, tokenB = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type rig struct {
	t     *testing.T
	store *filestore.Store
	url   string
	a, b  *Agent
	ra    string
	rb    string
}

// machines are the two test machines by their bearer.
var machines = map[string]string{tokenA: "vm-a", tokenB: "vm-b"}

func authorize(r *http.Request, coll string) (filestore.Access, error) {
	m := machines[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if m == "" {
		return filestore.Access{}, filestore.ErrUnauthorized
	}
	return filestore.Access{Member: m, Room: coll}, nil
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	srvStore, err := filestore.OpenFolder(filepath.Join(dir, "hub"), authorize)
	if err != nil {
		t.Fatal(err)
	}
	store, err := srvStore.Room(context.Background(), filestore.KindDesign)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(srvStore.Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(srvStore.Close)
	r := &rig{t: t, store: store, url: srv.URL, ra: filepath.Join(dir, "a"), rb: filepath.Join(dir, "b")}
	r.a = r.agent(r.ra, filepath.Join(dir, "sa"), tokenA, "vm-a")
	r.b = r.agent(r.rb, filepath.Join(dir, "sb"), tokenB, "vm-b")
	return r
}

func staticToken(tok string) Token {
	return func(context.Context, bool) (string, error) { return tok, nil }
}

func (r *rig) agent(root, state, token, machine string) *Agent {
	if err := os.MkdirAll(root, 0o755); err != nil {
		r.t.Fatal(err)
	}
	a, err := New(Config{Root: root, StateDir: state, Store: NewClient(r.url, filestore.KindDesign, staticToken(token), nil), Machine: machine})
	if err != nil {
		r.t.Fatal(err)
	}
	a.now = func() time.Time { return time.Date(2026, 10, 18, 21, 41, 0, 0, time.UTC) }
	r.t.Cleanup(a.Close)
	return a
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make sure a second write in the same test is seen as a change even on a coarse clock.
	later := time.Now().Add(time.Duration(len(body)) * time.Millisecond)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func headRev(t *testing.T, s *filestore.Store) int64 {
	ch, err := s.Changes(context.Background(), 0, 10000)
	must(t, err)
	return ch.Rev
}

var ctx = context.Background()

func TestEditReachesOtherMachineWithoutEcho(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "admin/project/Профиль.dc.html", "<x-dc>v1</x-dc>")
	write(t, r.ra, "admin/project/_ds/bundle.js", "bundle")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))
	if got := read(t, r.rb, "admin/project/Профиль.dc.html"); got != "<x-dc>v1</x-dc>" {
		t.Fatalf("b has %q", got)
	}
	rev := headRev(t, r.store)
	// Neither side sends anything back: the download is known, not an edit.
	must(t, r.b.ScanAll(ctx))
	must(t, r.a.SyncOnce(ctx))
	if got := headRev(t, r.store); got != rev {
		t.Fatalf("echo: hub went from rev %d to %d", rev, got)
	}
}

// A write that lost by revision leaves no "(конфликт …)" copy: the hub's version takes the board, the losing contents are
// kept with a notice, and a small edit is only kept, for its agent to re-apply.
func TestStaleSmallEditIsKeptForReapplyNotAsACopy(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "admin/project/Профиль.dc.html", "base")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))

	write(t, r.ra, "admin/project/Профиль.dc.html", "edit by roma on vm-a")
	must(t, r.a.Scan(ctx, "admin/project/Профиль.dc.html"))
	// b has not pulled yet and edits its stale copy.
	write(t, r.rb, "admin/project/Профиль.dc.html", "edit by sergey on vm-b")
	must(t, r.b.Scan(ctx, "admin/project/Профиль.dc.html"))

	if got := read(t, r.rb, "admin/project/Профиль.dc.html"); got != "edit by roma on vm-a" {
		t.Fatalf("b's board should take the accepted edit, has %q", got)
	}
	kept := ".conflicts/admin/project/Профиль.dc.html.vm-b.20261018-214100"
	if got := read(t, r.rb, kept); got != "edit by sergey on vm-b" {
		t.Fatalf("b's stale edit is not kept: %q", got)
	}
	var n Notice
	if err := json.Unmarshal([]byte(read(t, r.rb, kept+".json")), &n); err != nil {
		t.Fatal(err)
	}
	if n.Kind != KindReapply || n.Project != "admin" || n.Board != "Профиль.dc.html" || n.Kept != kept || n.Variant != "" || n.CurrentRev == 0 {
		t.Fatalf("notice: %+v", n)
	}
	must(t, r.a.SyncOnce(ctx))
	entries, _ := os.ReadDir(filepath.Join(r.ra, "admin", "project"))
	if len(entries) != 1 {
		t.Fatalf("no copy beside the board, a has %v", entries)
	}
	if _, err := os.Stat(filepath.Join(r.ra, ".conflicts")); err == nil {
		t.Fatal("what is kept is not synced")
	}
}

// A bigger edit of a board is saved as a project of its own, linked to the original.
func TestStaleBigEditBecomesAVariantProject(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "admin/artifact.json", `{"title":"Админка"}`)
	write(t, r.ra, "admin/.owner.json", `{"v":2,"user":"se"}`)
	write(t, r.ra, "admin/project/Профиль.dc.html", "base")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))

	write(t, r.ra, "admin/project/Профиль.dc.html", "edit by roma")
	must(t, r.a.Scan(ctx, "admin/project/Профиль.dc.html"))
	big := strings.Repeat("<section>sergey's redesign</section>\n", 400)
	write(t, r.rb, "admin/project/Профиль.dc.html", big)
	must(t, r.b.Scan(ctx, "admin/project/Профиль.dc.html"))

	if got := read(t, r.rb, "admin/project/Профиль.dc.html"); got != "edit by roma" {
		t.Fatalf("the board takes the accepted edit: %q", got)
	}
	id := "admin-variant-vm-b-20261018-2141"
	if got := read(t, r.rb, id+"/project/Профиль.dc.html"); got != big {
		t.Fatalf("the variant has the losing contents: %.40q", got)
	}
	var link VariantLink
	if err := json.Unmarshal([]byte(read(t, r.rb, id+"/"+VariantFile)), &link); err != nil || link.Of != "admin" || link.Board != "Профиль.dc.html" || link.Rev == 0 {
		t.Fatalf("link: %+v %v", link, err)
	}
	if got := read(t, r.rb, id+"/artifact.json"); !strings.Contains(got, "Админка (вариант)") {
		t.Fatalf("artifact.json: %s", got)
	}
	if got := read(t, r.rb, id+"/.owner.json"); !strings.Contains(got, `"user":"se"`) && !strings.Contains(got, `"user": "se"`) {
		t.Fatalf("the variant keeps the owner: %s", got)
	}
	var n Notice
	if err := json.Unmarshal([]byte(read(t, r.rb, ".conflicts/admin/project/Профиль.dc.html.vm-b.20261018-214100.json")), &n); err != nil || n.Kind != KindVariant || n.Variant != id {
		t.Fatalf("notice: %+v %v", n, err)
	}
	must(t, r.a.SyncOnce(ctx))
	if got := read(t, r.ra, id+"/project/Профиль.dc.html"); got != big {
		t.Fatalf("the variant did not reach a: %.40q", got)
	}
	if got := read(t, r.ra, id+"/"+VariantFile); !strings.Contains(got, `"of": "admin"`) {
		t.Fatalf("the link did not reach a: %s", got)
	}
}

func TestStaleEditOfAnotherFileIsKept(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "admin/assets/logo.svg", "base")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))
	write(t, r.ra, "admin/assets/logo.svg", "roma")
	must(t, r.a.Scan(ctx, "admin/assets/logo.svg"))
	write(t, r.rb, "admin/assets/logo.svg", "sergey")
	must(t, r.b.Scan(ctx, "admin/assets/logo.svg"))
	if got := read(t, r.rb, ".conflicts/admin/assets/logo.svg.vm-b.20261018-214100"); got != "sergey" {
		t.Fatalf("kept: %q", got)
	}
	var n Notice
	if err := json.Unmarshal([]byte(read(t, r.rb, ".conflicts/admin/assets/logo.svg.vm-b.20261018-214100.json")), &n); err != nil || n.Kind != KindKept {
		t.Fatalf("notice: %+v %v", n, err)
	}
}

func TestSmallEditAndVariantID(t *testing.T) {
	if !SmallEdit([]byte("<a>one</a><b>x</b>"), []byte("<a>one</a><b>y</b>")) {
		t.Fatal("one letter is small")
	}
	if SmallEdit([]byte(strings.Repeat("a", 5000)), []byte(strings.Repeat("b", 5000))) {
		t.Fatal("everything different is not small")
	}
	id := VariantID(strings.Repeat("p", 64), "Big Box!", time.Date(2026, 10, 18, 21, 41, 0, 0, time.UTC))
	if len(id) > 64 || !strings.Contains(id, "-variant-big-box-") {
		t.Fatalf("id: %q", id)
	}
}

func TestUnsentLocalEditSurvivesIncomingRevision(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "p/project/B.dc.html", "base")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))
	write(t, r.ra, "p/project/B.dc.html", "a2")
	must(t, r.a.Scan(ctx, "p/project/B.dc.html"))
	// b edited but its watcher has not sent yet when a's revision arrives.
	write(t, r.rb, "p/project/B.dc.html", "b2 unsent")
	_, err := r.b.Pull(ctx, 0)
	must(t, err)
	if got := read(t, r.rb, "p/project/B.dc.html"); got != "a2" {
		t.Fatalf("board: %q", got)
	}
	if got := read(t, r.rb, ".conflicts/p/project/B.dc.html.vm-b.20261018-214100"); got != "b2 unsent" {
		t.Fatalf("unsent edit lost: %q", got)
	}
}

func TestDeleteAndFolderRename(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "old/project/A.dc.html", "a")
	write(t, r.ra, "old/project/lib/x.js", "x")
	write(t, r.ra, "keep/project/K.dc.html", "k")
	write(t, r.ra, "keep/project/Other.dc.html", "o")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))

	must(t, os.Rename(filepath.Join(r.ra, "old"), filepath.Join(r.ra, "new")))
	must(t, os.Remove(filepath.Join(r.ra, "keep/project/K.dc.html")))
	must(t, r.a.Scan(ctx, "old"))
	must(t, r.a.Scan(ctx, "new"))
	must(t, r.a.Scan(ctx, "keep/project/K.dc.html"))
	must(t, r.b.SyncOnce(ctx))

	if _, err := os.Stat(filepath.Join(r.rb, "old")); !os.IsNotExist(err) {
		t.Fatalf("the moved-away project folder should not stay behind empty: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.rb, "keep")); err != nil {
		t.Fatal("a project that still has boards was pruned")
	}
	if got := read(t, r.rb, "new/project/lib/x.js"); got != "x" {
		t.Fatalf("renamed folder: %q", got)
	}
	for _, gone := range []string{"old/project/A.dc.html", "keep/project/K.dc.html"} {
		if got := read(t, r.rb, gone); got != "<missing>" {
			t.Fatalf("%s should be gone, has %q", gone, got)
		}
	}
}

func TestDeleteOfStaleCopyDoesNotWinOverEdit(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "p/project/B.dc.html", "base")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))
	write(t, r.ra, "p/project/B.dc.html", "newer")
	must(t, r.a.Scan(ctx, "p/project/B.dc.html"))
	must(t, os.Remove(filepath.Join(r.rb, "p/project/B.dc.html")))
	must(t, r.b.Scan(ctx, "p/project/B.dc.html"))
	if got := read(t, r.rb, "p/project/B.dc.html"); got != "newer" {
		t.Fatalf("stale delete should give way to the newer edit, b has %q", got)
	}
}

func TestMachineFilesAndUnsafePathsStayLocal(t *testing.T) {
	r := newRig(t)
	write(t, r.ra, "CLAUDE.md", "guide v5")
	write(t, r.ra, ".template/project/support.js", "t")
	write(t, r.ra, ".claims/admin/x.json", "{}")
	write(t, r.ra, "admin/.thumbs/a.png", "png")
	write(t, r.ra, "admin/project/A.dc.html.tmp.123.456", "partial")
	write(t, r.ra, "admin/project/A.dc.html", "a")
	write(t, r.ra, "admin/.claude/settings.json", `{"hooks":{}}`)
	write(t, r.ra, "admin/project/.mcp.json", "{}")
	write(t, r.ra, "admin/.check/001-phone.jpg", "jpg")
	write(t, r.ra, "admin/project/CLAUDE.md", "do this")
	write(t, r.ra, "admin/project/lib/agents.local.md", "do that")
	write(t, r.ra, "admin/project/.vscode/tasks.json", "{}")
	write(t, r.ra, "admin/project/README.md", "readme")
	must(t, os.Symlink("/etc/passwd", filepath.Join(r.ra, "admin/project/link.html")))
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))
	for _, local := range []string{"CLAUDE.md", ".template/project/support.js", ".claims/admin/x.json", "admin/.thumbs/a.png",
		"admin/project/A.dc.html.tmp.123.456", "admin/project/link.html",
		"admin/.claude/settings.json", "admin/project/.mcp.json", "admin/.check/001-phone.jpg",
		"admin/project/CLAUDE.md", "admin/project/lib/agents.local.md", "admin/project/.vscode/tasks.json"} {
		if got := read(t, r.rb, local); got != "<missing>" {
			t.Fatalf("%s should not sync, b has %q", local, got)
		}
	}
	for _, synced := range []string{"admin/project/A.dc.html", "admin/project/README.md"} {
		if got := read(t, r.rb, synced); got == "<missing>" {
			t.Fatalf("%s should sync", synced)
		}
	}
	// The hub refuses paths that escape the library whoever sends them.
	for _, bad := range []string{"../etc/x", "/abs/x", "a/../../x", "a\\b/c", ".claims/x/y", "top.txt"} {
		if _, _, err := r.store.Commit(context.Background(), filestore.CommitRequest{Path: bad, Deleted: true}, "x"); err == nil {
			t.Fatalf("hub took %q", bad)
		}
	}
}

func TestIncomingFileDoesNotFollowSymlinkedFolder(t *testing.T) {
	r := newRig(t)
	outside := t.TempDir()
	write(t, r.ra, "p/project/lib/x.js", "x")
	must(t, r.a.SyncOnce(ctx))
	if err := os.MkdirAll(filepath.Join(r.rb, "p/project"), 0o755); err != nil {
		t.Fatal(err)
	}
	must(t, os.Symlink(outside, filepath.Join(r.rb, "p/project/lib")))
	_ = r.b.SyncOnce(ctx)
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote through a symlink into %s", outside)
	}
}

func TestLiveRunDeliversWithinTwoSeconds(t *testing.T) {
	r := newRig(t)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = r.a.Run(runCtx) }()
	go func() { _ = r.b.Run(runCtx) }()
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	write(t, r.ra, "live/project/Доска.dc.html", "live edit")
	for time.Since(start) < 5*time.Second {
		if read(t, r.rb, "live/project/Доска.dc.html") == "live edit" {
			if d := time.Since(start); d > 2*time.Second {
				t.Fatalf("took %s", d)
			}
			t.Logf("delivered in %s", time.Since(start).Round(time.Millisecond))
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("not delivered in 5s")
}

func TestConflictName(t *testing.T) {
	at := time.Date(2026, 10, 18, 21, 41, 0, 0, time.UTC)
	cases := map[string]string{
		"p/project/Профиль.dc.html": "p/project/Профиль (конфликт m 2026-10-18 2141).dc.html",
		"p/project/lib/app.js":      "p/project/lib/app (конфликт m 2026-10-18 2141).js",
		"p/project/README":          "p/project/README (конфликт m 2026-10-18 2141)",
	}
	for in, want := range cases {
		if got := ConflictName(in, "m", at); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
	if !strings.Contains(ConflictName("a/b.dc.html", "x", at), "конфликт") {
		t.Fatal()
	}
}

func TestUnreadableFileIsQuarantinedAndQueueContinues(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	r := newRig(t)
	write(t, r.ra, "admin/stuck/a.dc.html", "from a")
	write(t, r.ra, "admin/other/b.dc.html", "also from a")
	// b holds a copy of the first file nobody may read (ACL mask --- on the real machine).
	write(t, r.rb, "admin/stuck/a.dc.html", "local")
	must(t, os.Chmod(filepath.Join(r.rb, "admin/stuck/a.dc.html"), 0))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(r.rb, "admin/stuck/a.dc.html"), 0o644) })
	must(t, r.a.SyncOnce(ctx))

	if _, err := r.b.Pull(ctx, 0); err != nil {
		t.Fatalf("a file without rights must not fail the pull: %v", err)
	}
	if got := read(t, r.rb, "admin/other/b.dc.html"); got != "also from a" {
		t.Fatalf("the rest of the queue did not arrive: %q", got)
	}
	q, err := r.b.Quarantined()
	must(t, err)
	if len(q) != 1 || q[0].Path != "admin/stuck/a.dc.html" {
		t.Fatalf("quarantine = %+v", q)
	}
	// Rights come back and a newer revision arrives: the file leaves quarantine.
	must(t, os.Chmod(filepath.Join(r.rb, "admin/stuck/a.dc.html"), 0o644))
	write(t, r.ra, "admin/stuck/a.dc.html", "from a, v2")
	must(t, r.a.ScanAll(ctx))
	must(t, r.b.SyncOnce(ctx))
	q, _ = r.b.Quarantined()
	if len(q) != 0 {
		t.Fatalf("still in quarantine: %+v", q)
	}
}

// Rights come back with no new revision: the rescan brings the file from the hub by itself.
func TestQuarantineRetriedWhenRightsReturn(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	r := newRig(t)
	write(t, r.ra, "admin/stuck/a.dc.html", "from a")
	write(t, r.rb, "admin/stuck/a.dc.html", "local")
	stuck := filepath.Join(r.rb, "admin/stuck")
	must(t, os.Chmod(filepath.Join(stuck, "a.dc.html"), 0))
	must(t, os.Chmod(stuck, 0o500))
	t.Cleanup(func() { _ = os.Chmod(stuck, 0o755); _ = os.Chmod(filepath.Join(stuck, "a.dc.html"), 0o644) })
	must(t, r.a.SyncOnce(ctx))
	if _, err := r.b.Pull(ctx, 0); err != nil {
		t.Fatal(err)
	}
	must(t, r.b.RetryQuarantine(ctx))
	if q, _ := r.b.Quarantined(); len(q) != 1 {
		t.Fatalf("without rights the file must stay set aside: %+v", q)
	}
	must(t, os.Chmod(stuck, 0o755))
	must(t, os.Chmod(filepath.Join(stuck, "a.dc.html"), 0o644))
	must(t, r.b.RetryQuarantine(ctx))
	if q, _ := r.b.Quarantined(); len(q) != 0 {
		t.Fatalf("still in quarantine: %+v", q)
	}
	if got := read(t, r.rb, "admin/stuck/a.dc.html"); got != "from a" {
		t.Fatalf("got %q", got)
	}
}

// Two conflicts in the same second, and a project that already has the name, never share a variant folder: nothing
// that exists is written into.
func TestVariantFolderIsNeverAnExistingOne(t *testing.T) {
	r := newRig(t)
	big := func(s string) string { return strings.Repeat("<p>"+s+"</p>\n", 600) }
	const board = "admin/project/Профиль.dc.html"
	write(t, r.ra, board, "base")
	must(t, r.a.SyncOnce(ctx))
	must(t, r.b.SyncOnce(ctx))
	id := "admin-variant-vm-b-20261018-2141"
	// A project of that very name exists on b, with a file that must stay as it is.
	write(t, r.rb, id+"/project/Профиль.dc.html", "someone else's project")
	write(t, r.rb, id+"/artifact.json", `{"title":"Other"}`)

	for i, edit := range []string{"roma 1", "roma 2"} {
		write(t, r.ra, board, edit)
		must(t, r.a.Scan(ctx, board))
		write(t, r.rb, board, big("sergey "+edit))
		must(t, r.b.Scan(ctx, board))
		if got := read(t, r.rb, id+"/project/Профиль.dc.html"); got != "someone else's project" {
			t.Fatalf("round %d: an existing project was written into: %.40q", i, got)
		}
	}
	for _, v := range []string{id + "-2", id + "-3"} {
		if got := read(t, r.rb, v+"/project/Профиль.dc.html"); !strings.Contains(got, "sergey roma") {
			t.Fatalf("variant %s: %.40q", v, got)
		}
		if got := read(t, r.rb, v+"/"+VariantFile); !strings.Contains(got, `"of": "admin"`) {
			t.Fatalf("link of %s: %s", v, got)
		}
	}
	if got := read(t, r.rb, ".conflicts/"+board+".vm-b.20261018-214100.2.json"); got == "<missing>" {
		t.Fatal("the second kept notice replaced the first")
	}
}

// A kept name that cannot be reserved because of an error other than "exists" (no right on the folder) fails at once,
// it is not retried forever.
func TestKeptNameFailsOnAnUnreadableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	r := newRig(t)
	dir := filepath.Join(r.ra, ".conflicts", "admin", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := ".conflicts/admin/project/B.dc.html.vm-a.20261018-214100"
	if got, err := r.a.claimKeptName(name); err != nil || got != name {
		t.Fatalf("free: %q %v", got, err)
	}
	if got, err := r.a.claimKeptName(name); err != nil || got != name+".2" {
		t.Fatalf("taken: %q %v", got, err)
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	done := make(chan error, 1)
	go func() { _, err := r.a.claimKeptName(name); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an unreadable folder must be an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claimKeptName hangs")
	}
}
