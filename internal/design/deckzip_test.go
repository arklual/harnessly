package design

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestProjectArchivesKeepServiceAndSecretsOut(t *testing.T) {
	l := testDeckLibrary(t)
	dir := l.ArtifactDir("demo")
	outside := filepath.Join(t.TempDir(), "elsewhere.txt")
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "artifact.json"), `{"title":"Demo"}`)
	write(filepath.Join(dir, "meta", "notes", "1.md"), "note")
	write(filepath.Join(dir, ".media", "rec.webm"), "rec")
	write(filepath.Join(dir, ".remember", "now.md"), "agent")
	write(filepath.Join(dir, ProjectDirname, "img", "a.png"), "PNG")
	write(filepath.Join(dir, ProjectDirname, ".env"), "SECRET=1")
	write(filepath.Join(dir, ProjectDirname, "key.pem"), "KEY")
	write(outside, "outside")
	_ = os.Symlink(outside, filepath.Join(dir, ProjectDirname, "out.txt"))
	_ = os.Symlink(filepath.Join(dir, "meta", "notes", "1.md"), filepath.Join(dir, ProjectDirname, "note.md"))
	// A link to a service file of the project stays out as the file itself does.
	_ = os.Symlink(filepath.Join(dir, ".media", "rec.webm"), filepath.Join(dir, ProjectDirname, "rec.webm"))

	names := func(format DeckExportFormat) []string {
		t.Helper()
		out := filepath.Join(t.TempDir(), "a.zip")
		exp, _ := DeckExporterOf(format)
		if err := exp.Run(context.Background(), DeckExportRun{L: l, Artifact: "demo", Out: out, Progress: func(float64) {}}); err != nil {
			t.Fatal(err)
		}
		zr, err := zip.OpenReader(out)
		if err != nil {
			t.Fatal(err)
		}
		defer zr.Close()
		var got []string
		for _, f := range zr.File {
			got = append(got, f.Name)
		}
		sort.Strings(got)
		return got
	}
	if got := strings.Join(names(DeckExportZip), ","); got != "One.dc.html,img/a.png,note.md" {
		t.Errorf("project zip = %s", got)
	}
	want := "artifact.json,meta/notes/1.md,project/One.dc.html,project/img/a.png,project/note.md"
	if got := strings.Join(names(DeckExportBundle), ","); got != want {
		t.Errorf("whole project = %s", got)
	}
}

func TestBoardExportAndArchivesAsk(t *testing.T) {
	l := testDeckLibrary(t)
	a := &API{L: l, Session: func(r *http.Request) bool { return r.Header.Get("Cookie") == "ok" },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	post := func(body string) Answer {
		req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		req.Header.Set("Cookie", "ok")
		req.Header.Set("Content-Type", "application/json")
		return a.DeckExport(req)
	}
	if ans := post(`{"artifact":"demo","format":"pptx","board":"b1"}`); ans.Status != 400 {
		t.Errorf("a pptx of one board = %d", ans.Status)
	}
	if ans := post(`{"artifact":"demo","format":"html","board":"../x"}`); ans.Status != 400 {
		t.Errorf("a bad board = %d", ans.Status)
	}
	if ans := post(`{"artifact":"demo","format":"html","board":"One.dc.html"}`); ans.Status != 202 {
		t.Errorf("the html of a board by its path = %d %s", ans.Status, ans.Body)
	}
	if ans := post(`{"artifact":"demo","format":"zip"}`); ans.Status != 202 {
		t.Errorf("zip = %d %s", ans.Status, ans.Body)
	}
	if _, err := l.runSlides(DeckExportRun{L: l, Artifact: "demo", Board: "nope"}); err == nil {
		t.Error("a board that is not there is an error")
	}
}

func TestProjectArchiveDoesNotFollowASwappedFile(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	secret := filepath.Join(t.TempDir(), "id_rsa")
	_ = os.WriteFile(secret, []byte("PRIVATE"), 0o600)
	page := filepath.Join(root, "page.html")
	_ = os.WriteFile(page, []byte("<p>hi</p>"), 0o644)
	list, _, err := deckZipList(context.Background(), root, root)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	// The file becomes a link out of the project after it was listed.
	_ = os.Remove(page)
	if err := os.Symlink(secret, page); err != nil {
		t.Fatal(err)
	}
	inside, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer inside.Close()
	var buf strings.Builder
	zw := zip.NewWriter(&nopWriter{&buf})
	if n, _ := deckZipFile(zw, inside, list[0], "page.html"); n != 0 {
		t.Fatalf("read %d bytes through a swapped link", n)
	}
	_ = zw.Close()
	if strings.Contains(buf.String(), "PRIVATE") {
		t.Fatal("the archive holds the file outside")
	}
}

type nopWriter struct{ b *strings.Builder }

func (w *nopWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

func TestPictureOfOneBoardIsAPNG(t *testing.T) {
	l := testDeckLibrary(t)
	fakeExporter(t, DeckExportPNG, DeckExporter{Ext: "zip", Run: func(_ context.Context, run DeckExportRun) error {
		if run.Board != "One.dc.html" {
			t.Errorf("board = %q", run.Board)
		}
		return os.WriteFile(run.Out, []byte("\x89PNG fake"), 0o644)
	}})
	a := &API{L: l, Session: func(r *http.Request) bool { return r.Header.Get("Cookie") == "ok" },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	j, err := l.StartBoardExport("demo", DeckExportPNG, "", "One.dc.html", false)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, l, "demo", j.ID, DeckJobDone)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x?artifact=demo&job="+j.ID, nil)
	req.Header.Set("Cookie", "ok")
	a.DeckExportFile(w, req)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !strings.Contains(w.Header().Get("Content-Disposition"), ".png") {
		t.Errorf("file = %d %v", w.Code, w.Header())
	}
}
