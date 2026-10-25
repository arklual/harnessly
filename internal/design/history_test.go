package design

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type historyGolden struct {
	SnapshotMessage []struct {
		Paths, Editors []string
		Runtime        string
		Subject        *string
		Out            struct{ Subject, Body string }
	}
	ParseLog []struct {
		Input string
		Out   []struct {
			ID      string
			At      int64
			Subject string
			Editors []string
			Runtime *string
		}
	}
	ScopePath []struct {
		Project string
		File    *string
		Out     struct {
			OK    *string
			Error *string
		}
	}
	RestoreTarget []struct {
		Input string
		Out   *struct {
			Kind     string
			Ms       int64
			Revision string
		}
	}
	IsHistoryPath []struct {
		Input string
		Out   bool
	}
	Slugify []struct {
		Name  string
		Taken []string
		Out   string
	}
	UnzipListing []struct {
		Input string
		Out   *struct{ Bytes, Entries int64 }
	}
	CleanBrief []struct {
		Input string
		Out   struct {
			OK    *string
			Error *string
		}
	}
	NewProjectPrompt string
	Import           struct {
		Result struct {
			ID, Title string
			Boards    int
			Dropped   []string
		}
		Tree []string
	}
	ImportAgain, ImportBroken, CreateMissing string
	Create                                   struct {
		ID, Title string
		Tree      []string
	}
	Create2, CreateBad string
	History            struct {
		Kinds  []string
		Listed struct {
			Unsaved bool
			Entries []struct {
				Subject string
				Editors []string
				Runtime *string
			}
		}
		Refused, Missing, TooEarly string
		RestoredSame, BoardAfter   bool
		Log, Exclude, Config       string
	}
}

func loadHistoryGolden(t *testing.T) historyGolden {
	t.Helper()
	b, err := os.ReadFile("testdata/history/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g historyGolden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestHistoryPureFunctionsMatchGolden(t *testing.T) {
	g := loadHistoryGolden(t)
	for _, c := range g.SnapshotMessage {
		subject := ""
		if c.Subject != nil {
			subject = *c.Subject
		}
		s, b := SnapshotMessage(c.Paths, c.Editors, c.Runtime, subject)
		if s != c.Out.Subject || b != c.Out.Body {
			t.Errorf("snapshotMessage(%q): %q / %q, want %q / %q", c.Paths, s, b, c.Out.Subject, c.Out.Body)
		}
	}
	for _, c := range g.ParseLog {
		got := ParseLog(c.Input)
		if len(got) != len(c.Out) {
			t.Errorf("parseLog(%q): %d entries, want %d", c.Input, len(got), len(c.Out))
			continue
		}
		for i, e := range got {
			w := c.Out[i]
			if e.ID != w.ID || e.At != w.At || e.Subject != w.Subject || strings.Join(e.Editors, "|") != strings.Join(w.Editors, "|") ||
				(e.Runtime == nil) != (w.Runtime == nil) || (e.Runtime != nil && *e.Runtime != *w.Runtime) {
				t.Errorf("parseLog(%q)[%d]: %+v, want %+v", c.Input, i, e, w)
			}
		}
	}
	for _, c := range g.ScopePath {
		got, err := ScopePath(c.Project, c.File)
		if c.Out.Error != nil {
			if err == nil || err.Error() != *c.Out.Error {
				t.Errorf("scopePath(%s, %v): %q %v, want error %q", c.Project, c.File, got, err, *c.Out.Error)
			}
		} else if err != nil || got != *c.Out.OK {
			t.Errorf("scopePath(%s, %v): %q %v, want %q", c.Project, c.File, got, err, *c.Out.OK)
		}
	}
	for _, c := range g.RestoreTarget {
		got, ok := ParseRestoreTarget(c.Input)
		switch {
		case c.Out == nil:
			if ok {
				t.Errorf("parseRestoreTarget(%q): %+v, want none", c.Input, got)
			}
		case c.Out.Kind == "ago":
			if !ok || got.Revision != "" || got.Ago != time.Duration(c.Out.Ms)*time.Millisecond {
				t.Errorf("parseRestoreTarget(%q): %+v, want %dms ago", c.Input, got, c.Out.Ms)
			}
		default:
			if !ok || got.Revision != c.Out.Revision {
				t.Errorf("parseRestoreTarget(%q): %+v, want %s", c.Input, got, c.Out.Revision)
			}
		}
	}
	for _, c := range g.IsHistoryPath {
		if got := IsHistoryPath(c.Input); got != c.Out {
			t.Errorf("isHistoryPath(%q) = %v", c.Input, got)
		}
	}
	for _, c := range g.Slugify {
		taken := map[string]bool{}
		for _, k := range c.Taken {
			taken[k] = true
		}
		if got := SlugifyArtifactID(c.Name, taken); got != c.Out {
			t.Errorf("slugify(%q) = %q, want %q", c.Name, got, c.Out)
		}
	}
	for _, c := range g.UnzipListing {
		b, e, ok := ParseUnzipListing(c.Input)
		if (c.Out == nil) == ok || (ok && (b != c.Out.Bytes || e != c.Out.Entries)) {
			t.Errorf("parseUnzipListing(%q) = %d %d %v", c.Input, b, e, ok)
		}
	}
	for _, c := range g.CleanBrief {
		got, err := CleanBrief(c.Input)
		if c.Out.Error != nil {
			if err == nil || err.Error() != *c.Out.Error {
				t.Errorf("cleanBrief(%q): %q %v", c.Input, got, err)
			}
		} else if err != nil || got != *c.Out.OK {
			t.Errorf("cleanBrief(%q): %q %v, want %q", c.Input, got, err, *c.Out.OK)
		}
	}
	l := New(Config{Texts: Texts{CLI: "acme-cli", KitGlobal: "AcmeKit"}})
	if got := l.NewProjectPrompt("  Мой \n проект ", "бриф"); got != g.NewProjectPrompt {
		t.Errorf("new project prompt:\n%s\nwant\n%s", got, g.NewProjectPrompt)
	}
}

var importedAtRe = regexp.MustCompile(`"importedAt": "[^"]*"`)

func treeOf(t *testing.T, dir string) []string {
	t.Helper()
	var lines []string
	var walk func(rel string)
	walk = func(rel string) {
		entries, err := os.ReadDir(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			r := name
			if rel != "" {
				r = rel + "/" + name
			}
			st, err := os.Lstat(filepath.Join(dir, r))
			if err != nil {
				t.Fatal(err)
			}
			mode := strconv.FormatInt(int64(st.Mode().Perm()), 8)
			if st.IsDir() {
				lines = append(lines, "d "+mode+" "+r)
				walk(r)
				continue
			}
			b, _ := os.ReadFile(filepath.Join(dir, r))
			if name == ArtifactFile {
				q, _ := json.Marshal(importedAtRe.ReplaceAllString(string(b), `"importedAt": "<now>"`))
				lines = append(lines, "f "+mode+" "+r+" "+strings.NewReplacer(`\u003c`, "<", `\u003e`, ">", `\u0026`, "&").Replace(string(q)))
				continue
			}
			sum := sha1.Sum(b)
			lines = append(lines, "f "+mode+" "+r+" "+hex.EncodeToString(sum[:]))
		}
	}
	walk("")
	return lines
}

func sameLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	if out, err := exec.Command("cp", "-r", from, to).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	// cp keeps the modes of the checkout, which git does not record (only the exec bit) and so follow the umask of
	// whoever checked it out; the goldens want what a umask-022 checkout gives.
	err := filepath.WalkDir(to, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&os.ModeSymlink != 0 {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0o755)
		}
		return os.Chmod(p, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func historyLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	library := filepath.Join(tmp, "library")
	if err := os.Mkdir(library, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	l := New(Config{DataDir: filepath.Join(tmp, "data"), LibraryDir: library, HistoryDir: filepath.Join(tmp, "history"),
		Texts: Texts{CLI: "acme-cli", KitGlobal: "AcmeKit", Tag: "acme-design", HistoryEmail: "design@example.local"}})
	t.Cleanup(l.StopWatching)
	return l, library
}

func TestImportCreateAndHistoryMatchGolden(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skip("no unzip")
	}
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	g := loadHistoryGolden(t)
	l, library := historyLibrary(t)

	upload := filepath.Join(filepath.Dir(library), "upload.zip")
	b, _ := os.ReadFile("testdata/history/project.zip")
	if err := os.WriteFile(upload, b, 0o644); err != nil {
		t.Fatal(err)
	}
	title, name := "  Мой Сайт  ", "Мой Сайт.zip"
	res, err := l.ImportProject(ImportOptions{Source: upload, ID: SlugifyArtifactID(name, nil), Title: &title, SourceName: &name})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != g.Import.Result.ID || res.Title != g.Import.Result.Title || res.Boards != g.Import.Result.Boards || strings.Join(res.Dropped, ",") != strings.Join(g.Import.Result.Dropped, ",") {
		t.Errorf("import: %+v, want %+v", res, g.Import.Result)
	}
	sameLines(t, "imported tree", treeOf(t, filepath.Join(library, res.ID)), g.Import.Tree)
	if _, err := l.ImportProject(ImportOptions{Source: upload, ID: res.ID}); err == nil || err.Error() != g.ImportAgain {
		t.Errorf("import again: %v, want %s", err, g.ImportAgain)
	}
	if _, err := l.ImportProject(ImportOptions{Source: "testdata/history/template/artifact.json", ID: "broken"}); err == nil || err.Error() != g.ImportBroken {
		t.Errorf("import of a non-zip: %v, want %s", err, g.ImportBroken)
	}

	// The golden data is the old runtime's, which refused to create a project without .template/ (g.CreateMissing);
	// this runtime uses the template it ships instead, so that case is checked by TestCreateWithShippedTemplate.
	shipped, err := l.CreateProject("Новый")
	if err != nil {
		t.Fatalf("create with the shipped template: %v", err)
	}
	_ = os.RemoveAll(shipped.Dir)
	copyDir(t, "testdata/history/template", filepath.Join(library, TemplateDirname))
	_ = os.Chmod(filepath.Join(library, TemplateDirname, "project", "sub", "x.css"), 0o640)
	_ = os.Chmod(filepath.Join(library, TemplateDirname, "project", "sub"), 0o750)
	if err := os.Symlink("/etc/hostname", filepath.Join(library, TemplateDirname, "project", "link.html")); err != nil {
		t.Fatal(err)
	}
	created, err := l.CreateProject("  Новый\u0007 проект ")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != g.Create.ID || created.Title != g.Create.Title {
		t.Errorf("create: %+v", created)
	}
	// Every new project also gets meta/ and the canvas.json built from it, which the old runtime's golden tree
	// does not have: those lines are checked apart.
	var tree, model []string
	for _, line := range treeOf(t, filepath.Join(library, created.ID)) {
		if strings.Contains(line, " "+MetaDirname) || strings.Contains(line, ProjectDirname+"/"+canvasJSONName) {
			if strings.HasPrefix(line, "f ") {
				line = line[:strings.LastIndex(line, " ")]
			}
			model = append(model, line)
			continue
		}
		tree = append(tree, line)
	}
	sameLines(t, "created tree", tree, g.Create.Tree)
	if strings.Join(model, ",") != "d 755 meta,d 755 meta/pages,f 644 meta/project.json,f 644 project/canvas.json" {
		t.Errorf("created model files: %v", model)
	}
	if again, err := l.CreateProject("Новый проект"); err != nil || again.ID != g.Create2 {
		t.Errorf("second create: %+v %v", again, err)
	}
	if _, err := l.CreateProject(" \u0001 "); err == nil || err.Error() != g.CreateBad {
		t.Errorf("bad title: %v", err)
	}

	gitDir := func() string { real, _ := filepath.EvalSymlinks(library); return l.HistoryGitDir(real) }
	now := time.Now()
	first, err := l.snapshotNow("rt", nil, "", now)
	if err != nil {
		t.Fatal(err)
	}
	board := filepath.Join(library, res.ID, "project", "Board 2.dc.html")
	original, _ := os.ReadFile(board)
	owner := ClaimOwner{User: "Ann", Runtime: "rt", TabID: "tab-1", ChatTitle: "Чат"}
	if _, err := l.ClaimBoard(res.ID, "Board 2", owner, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(board, []byte("<main class=\"board\">changed</main>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var editors []string
	for _, c := range l.LiveClaims(res.ID, time.Now().UnixMilli()) {
		editors = append(editors, OwnerLine(c.Owner))
	}
	second, err := l.snapshotNow("rt", editors, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	clean, err := l.snapshotNow("rt", nil, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	file := "Board 2.dc.html"
	listed, err := l.readHistory(res.ID, &file, 12)
	if err != nil {
		t.Fatal(err)
	}
	refused := ""
	if _, err := l.restoreBoard(res.ID, file, first.sha, restoreActor{runtime: "rt", line: "Bob"}, time.Now()); err != nil {
		refused = err.Error()
	}
	restored, err := l.restoreBoard(res.ID, file, first.sha[:9], restoreActor{runtime: "rt", line: OwnerLine(owner), owner: &owner}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	errText := func(_ restoreResult, err error) string {
		if err == nil {
			return "ok"
		}
		return err.Error()
	}
	missing := errText(l.restoreBoard(res.ID, "Nope.dc.html", first.sha, restoreActor{runtime: "rt", line: "x", owner: &owner}, time.Now()))
	tooEarly := errText(l.restoreBoard(res.ID, file, "1d", restoreActor{runtime: "rt", line: "x", owner: &owner}, time.Now()))

	h := g.History
	kinds := []string{string(first.kind), string(second.kind), string(clean.kind), string(restored.snapshot.kind)}
	sameLines(t, "snapshot kinds", kinds, h.Kinds)
	if listed.unsaved != h.Listed.Unsaved || len(listed.entries) != len(h.Listed.Entries) {
		t.Fatalf("listed: %+v, want %+v", listed, h.Listed)
	}
	for i, e := range listed.entries {
		w := h.Listed.Entries[i]
		if e.Subject != w.Subject || strings.Join(e.Editors, "|") != strings.Join(w.Editors, "|") || *e.Runtime != *w.Runtime {
			t.Errorf("listed[%d]: %+v, want %+v", i, e, w)
		}
	}
	if refused != h.Refused || missing != h.Missing || tooEarly != h.TooEarly {
		t.Errorf("refusals: %q %q %q", refused, missing, tooEarly)
	}
	after, _ := os.ReadFile(board)
	if (restored.revision == first.sha) != h.RestoredSame || (string(after) == string(original)) != h.BoardAfter {
		t.Errorf("restore: revision %s (first %s), board back %v", restored.revision, first.sha, string(after) == string(original))
	}
	logOut, err := exec.Command("git", "--git-dir="+gitDir(), "log", "--format=%s%n%b%n%an <%ae>%n--").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantLog := regexp.MustCompile(`к версии [0-9a-f]{7}`).ReplaceAllString(h.Log, "к версии "+first.sha[:7])
	if string(logOut) != wantLog {
		t.Errorf("log:\n%s\nwant\n%s", logOut, wantLog)
	}
	for name, want := range map[string]string{"info/exclude": h.Exclude, "config": h.Config} {
		if got, _ := os.ReadFile(filepath.Join(gitDir(), name)); string(got) != want {
			t.Errorf("%s:\n%s\nwant\n%s", name, got, want)
		}
	}
	if entries, _ := os.ReadDir(library); func() bool {
		for _, e := range entries {
			if e.Name() == ".git" {
				return true
			}
		}
		return false
	}() {
		t.Error("the history put git data into the library")
	}
}

func TestHistoryBusyLockAndBackground(t *testing.T) {
	l, library := historyLibrary(t)
	if err := os.MkdirAll(filepath.Join(library, "p1", ProjectDirname), 0o777); err != nil {
		t.Fatal(err)
	}
	boardFile := filepath.Join(library, "p1", ProjectDirname, "A.dc.html")
	_ = os.WriteFile(boardFile, []byte("a\n"), 0o644)
	repo, err := l.ensureRepo()
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(repo.gitDir, "index.lock")
	_ = os.WriteFile(lock, nil, 0o644)
	if out, err := l.snapshotNow("rt", nil, "", time.Now()); err != nil || out.kind != SnapshotBusy {
		t.Fatalf("with a fresh lock: %+v %v", out, err)
	}
	old := time.Now().Add(-11 * time.Minute)
	_ = os.Chtimes(lock, old, old)
	if out, err := l.snapshotNow("rt", nil, "", time.Now()); err != nil || out.kind != SnapshotCommitted {
		t.Fatalf("with a stale lock: %+v %v", out, err)
	}

	l.startHistoryWith("rt", func(msg string) { t.Log("history failure:", msg) }, 200*time.Millisecond, time.Hour)
	defer l.stopHistory()
	_ = os.WriteFile(boardFile, []byte("b\n"), 0o644)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("git", "--git-dir="+repo.gitDir, "log", "--format=%s").Output()
		if strings.Count(string(out), "\n") >= 2 {
			if !strings.HasPrefix(string(out), "Правки: «A»") {
				t.Fatalf("log: %s", out)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the watcher's change was not saved")
}

func TestHistoryRestoreAgo(t *testing.T) {
	l, library := historyLibrary(t)
	dir := filepath.Join(library, "p1", ProjectDirname)
	_ = os.MkdirAll(dir, 0o777)
	board := filepath.Join(dir, "A.dc.html")
	_ = os.WriteFile(board, []byte("old\n"), 0o644)
	repo, _ := l.ensureRepo()
	if _, err := l.snapshotNow("rt", nil, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	// The first version is dated an hour back; the edit is now.
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", "--git-dir="+repo.gitDir, "--work-tree="+repo.root, "-c", "user.name=x", "-c", "user.email=x@example.local",
		"commit", "-q", "--amend", "--no-edit", "--date="+past)
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+past)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	_ = os.WriteFile(board, []byte("new\n"), 0o644)
	if _, err := l.snapshotNow("rt", nil, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := l.restoreBoard("p1", "A.dc.html", "30m", restoreActor{runtime: "rt", line: "x"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(board); string(b) != "old\n" {
		t.Fatalf("board after restore: %q", b)
	}
}
