package design

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pdfKnobs makes the keeper's waits test-sized and puts them back after the test.
func pdfKnobs(t *testing.T, quiet, maxWait, gap time.Duration) {
	t.Helper()
	q, m, g := deckPdfQuiet, deckPdfMaxWait, deckPdfGap
	deckPdfQuiet, deckPdfMaxWait, deckPdfGap = quiet, maxWait, gap
	t.Cleanup(func() { deckPdfQuiet, deckPdfMaxWait, deckPdfGap = q, m, g })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Turning the PDF on answers before the PDF exists; the first build runs in the background and is published for the
// guests, and a change to the deck builds it again once the deck is quiet.
func TestDeckPdfIsKeptForGuestsWithoutWaiting(t *testing.T) {
	g := newDeckRig(t)
	pdfKnobs(t, time.Hour, 2*time.Hour, 0)
	gate := make(chan struct{})
	var runs atomic.Int32
	fakeExporter(t, DeckExportPDF, DeckExporter{Ext: "pdf", Run: func(_ context.Context, run DeckExportRun) error {
		<-gate
		runs.Add(1)
		return os.WriteFile(run.Out, []byte("%PDF-1.4 fake"), 0o644)
	}})
	pub := filepath.Join(g.root, "admin", filepath.FromSlash(DeckPublishedDir), "deck.pdf")

	code, out := g.call(t, g.a.DeckPdf, "POST", "/x", `{"artifact":"admin","auto":true}`)
	if code != 200 {
		t.Fatalf("auto on = %d %v", code, out)
	}
	p := out["published"].(map[string]any)
	if p["pdfAuto"] != true || p["pdfBuilding"] != true || p["pdf"] != false || p["pdfAt"] != nil {
		t.Fatalf("the answer came before the PDF, building it: %v", p)
	}
	close(gate)
	waitFor(t, "the first PDF", func() bool { return fileSize(pub) > 0 })
	waitFor(t, "the build to end", func() bool { return !g.l.DeckPublishedState("admin").PDFBuilding })
	st := g.l.DeckPublishedState("admin")
	if !st.PDF || st.PDFAt == nil || st.PDFError != nil {
		t.Fatalf("published = %+v", st)
	}

	// A board changes: nothing is built while the deck keeps changing, then once it has been quiet.
	board := filepath.Join(g.root, "admin", ProjectDirname, g.pages(t)[0].File)
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(board, []byte("<main class=\"board\">changed</main>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !g.l.DeckPublishedState("admin").PDFBuilding {
		t.Error("a changed deck does not say its PDF is behind")
	}
	g.l.keepDeckPdfsOnce(time.Now())
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 1 {
		t.Fatalf("built while the deck was changing: %d runs", runs.Load())
	}
	g.l.keepDeckPdfsOnce(time.Now().Add(3 * time.Hour))
	waitFor(t, "the second build", func() bool { return runs.Load() == 2 })
	waitFor(t, "the PDF to catch up", func() bool { return !g.l.DeckPublishedState("admin").PDFBuilding })
	if from, rev := g.l.deckPdfFrom("admin"), g.l.deckContentRev("admin"); from < rev {
		t.Errorf("the published PDF (%d) is older than the deck (%d)", from, rev)
	}

	// Notes are not in the PDF: they build nothing.
	if _, err := g.l.SetDeckNotes("admin", g.pages(t)[0].ID, "Hello"); err != nil {
		t.Fatal(err)
	}
	g.l.keepDeckPdfsOnce(time.Now().Add(6 * time.Hour))
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 2 {
		t.Errorf("notes rebuilt the PDF: %d runs", runs.Load())
	}

	// Off: the marker goes, the published file stays, a change builds nothing.
	if code, out := g.call(t, g.a.DeckPdf, "POST", "/x", `{"artifact":"admin","auto":false}`); code != 200 ||
		out["published"].(map[string]any)["pdfAuto"] != false {
		t.Fatalf("auto off = %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(g.root, "admin", filepath.FromSlash(DeckPdfAutoFile))); !os.IsNotExist(err) {
		t.Errorf("the marker stayed: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	_ = os.WriteFile(board, []byte("<main class=\"board\">again</main>"), 0o644)
	g.l.keepDeckPdfsOnce(time.Now().Add(9 * time.Hour))
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 2 || fileSize(pub) == 0 {
		t.Errorf("after off: %d runs, published %d bytes", runs.Load(), fileSize(pub))
	}
}

// A build that fails says why and is not tried again until the deck changes.
func TestDeckPdfBuildThatFailsWaitsForAChange(t *testing.T) {
	g := newDeckRig(t)
	pdfKnobs(t, 0, 0, 0)
	var runs atomic.Int32
	fakeExporter(t, DeckExportPDF, DeckExporter{Ext: "pdf", Run: func(context.Context, DeckExportRun) error {
		runs.Add(1)
		return os.ErrPermission
	}})
	if _, err := g.l.SetDeckPdfAuto("admin", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failed build", func() bool {
		g.l.keepDeckPdfsOnce(time.Now())
		return g.l.DeckPublishedState("admin").PDFError != nil
	})
	for range 3 {
		g.l.keepDeckPdfsOnce(time.Now())
	}
	if runs.Load() != 1 || g.l.DeckPublishedState("admin").PDFBuilding {
		t.Fatalf("a failed build was retried on an unchanged deck: %d runs", runs.Load())
	}
	board := filepath.Join(g.root, "admin", ProjectDirname, g.pages(t)[0].File)
	time.Sleep(20 * time.Millisecond)
	_ = os.WriteFile(board, []byte("<main class=\"board\">fixed</main>"), 0o644)
	g.l.keepDeckPdfsOnce(time.Now())
	waitFor(t, "a build after the change", func() bool { return runs.Load() == 2 })
}

// Only the runtime named in the marker builds: a shared library's other runtimes leave the deck alone.
func TestDeckPdfIsBuiltOnlyByTheRuntimeThatKeepsIt(t *testing.T) {
	g := newDeckRig(t)
	pdfKnobs(t, 0, 0, 0)
	var runs atomic.Int32
	fakeExporter(t, DeckExportPDF, DeckExporter{Ext: "pdf", Run: func(_ context.Context, run DeckExportRun) error {
		runs.Add(1)
		return os.WriteFile(run.Out, []byte("%PDF-1.4 fake"), 0o644)
	}})
	marker := filepath.Join(g.root, "admin", filepath.FromSlash(DeckPdfAutoFile))
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("someone-else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.l.keepDeckPdfsOnce(time.Now())
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 0 {
		t.Errorf("another runtime's deck was built here")
	}
}

// fakePdfStore is a store with leases for the keeper: the deck's newest revision, and one lease holder at a time.
type fakePdfStore struct {
	mu     sync.Mutex
	rev    int64
	holder string
	// asks counts the history requests, by what they asked: a deck's folders, or the whole library ("").
	deckAsks, libraryAsks int
}

func (f *fakePdfStore) asked() (deck, library int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deckAsks, f.libraryAsks
}

func (f *fakePdfStore) History(context.Context, string) ([]Revision, error) { return nil, nil }
func (f *fakePdfStore) HistoryUnder(_ context.Context, prefix string, _ int) ([]Revision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if prefix == "" {
		f.libraryAsks++
		return []Revision{{Rev: f.rev, Path: "admin/" + ProjectDirname + "/Main.dc.html", At: time.Now().Add(-time.Minute).UnixMilli()}}, nil
	}
	f.deckAsks++
	if strings.HasSuffix(prefix, "/"+ProjectDirname+"/") {
		return []Revision{{Rev: f.rev, Path: prefix + "Main.dc.html", At: time.Now().Add(-time.Minute).UnixMilli()}}, nil
	}
	return nil, nil
}
func (f *fakePdfStore) Snapshot(context.Context, string, int64) ([]Revision, error) { return nil, nil }
func (f *fakePdfStore) Read(context.Context, string) ([]byte, error)                { return nil, nil }
func (f *fakePdfStore) TakeLease(_ context.Context, _, chat string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.holder != "" && f.holder != chat {
		return false, nil
	}
	f.holder = chat
	return true, nil
}

// through a store with leases, any machine that may edit a marked deck builds its PDF, but only the one
// holding the lease; whether it is behind is the store's revision against the one written beside the PDF.
func TestDeckPdfThroughTheStoreIsBuiltByTheLeaseHolderByRevision(t *testing.T) {
	g := newDeckRig(t)
	pdfKnobs(t, 0, 0, 0)
	was := deckPdfStoreFor
	deckPdfStoreFor = 0
	t.Cleanup(func() { deckPdfStoreFor = was })
	var runs atomic.Int32
	fakeExporter(t, DeckExportPDF, DeckExporter{Ext: "pdf", Run: func(_ context.Context, run DeckExportRun) error {
		runs.Add(1)
		return os.WriteFile(run.Out, []byte("%PDF-1.4 fake"), 0o644)
	}})
	marker := filepath.Join(g.root, "admin", filepath.FromSlash(DeckPdfAutoFile))
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	// Another machine's runtime turned it on: through the store that does not matter.
	if err := os.WriteFile(marker, []byte("someone-else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &fakePdfStore{rev: 7, holder: "other-machine/pdf"}
	g.l.UseRevisions(st)

	g.l.keepDeckPdfsOnce(time.Now())
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 0 {
		t.Fatal("built while another machine holds the lease")
	}

	st.mu.Lock()
	st.holder = ""
	st.mu.Unlock()
	revFile := g.l.deckFile("admin", deckPdfRevFile)
	waitFor(t, "the PDF built and its revision written", func() bool {
		g.l.keepDeckPdfsOnce(time.Now())
		raw, err := os.ReadFile(revFile)
		return err == nil && strings.TrimSpace(string(raw)) == "7"
	})
	if runs.Load() != 1 {
		t.Fatalf("builds: %d", runs.Load())
	}
	g.l.keepDeckPdfsOnce(time.Now())
	time.Sleep(50 * time.Millisecond)
	if runs.Load() != 1 {
		t.Fatalf("built again with nothing new in the store: %d", runs.Load())
	}

	st.mu.Lock()
	st.rev = 9
	st.mu.Unlock()
	// The sync brings the other machine's edit into the folder: that is what has the store asked again.
	board := filepath.Join(g.root, "admin", ProjectDirname, g.pages(t)[0].File)
	if err := os.WriteFile(board, []byte("<main class=\"board\">from another machine</main>"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the PDF built again from the new revision", func() bool {
		g.l.keepDeckPdfsOnce(time.Now())
		raw, err := os.ReadFile(revFile)
		return err == nil && strings.TrimSpace(string(raw)) == "9"
	})
}

// storeKeptDeck is a deck marked for a PDF in a library that keeps its PDFs through the store, built once from rev 7.
func storeKeptDeck(t *testing.T) (*deckRig, *fakePdfStore, string) {
	t.Helper()
	g := newDeckRig(t)
	pdfKnobs(t, 0, 0, 0)
	wasFor, wasSettle := deckPdfStoreFor, deckPdfSettle
	deckPdfStoreFor, deckPdfSettle = 0, 0
	t.Cleanup(func() { deckPdfStoreFor, deckPdfSettle = wasFor, wasSettle })
	fakeExporter(t, DeckExportPDF, DeckExporter{Ext: "pdf", Run: func(_ context.Context, run DeckExportRun) error {
		return os.WriteFile(run.Out, []byte("%PDF-1.4 fake"), 0o644)
	}})
	marker := filepath.Join(g.root, "admin", filepath.FromSlash(DeckPdfAutoFile))
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("someone-else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &fakePdfStore{rev: 7}
	g.l.UseRevisions(st)
	revFile := g.l.deckFile("admin", deckPdfRevFile)
	waitFor(t, "the first PDF", func() bool {
		g.l.keepDeckPdfsOnce(time.Now())
		raw, err := os.ReadFile(revFile)
		return err == nil && strings.TrimSpace(string(raw)) == "7"
	})
	// What the build itself wrote has reached the watcher by now.
	time.Sleep(200 * time.Millisecond)
	g.l.keepDeckPdfsOnce(time.Now())
	return g, st, revFile
}

// A marked deck nobody changes asks the store nothing, tick after tick; a change of its files has the store asked
// again and the PDF built from the new revision.
func TestDeckPdfAsksTheStoreOnlyAfterAChange(t *testing.T) {
	g, st, revFile := storeKeptDeck(t)
	deck, library := st.asked()
	if deck == 0 || library != 1 {
		t.Fatalf("the first look asked the deck's folders %d times and the library %d times", deck, library)
	}
	for range 40 {
		g.l.keepDeckPdfsOnce(time.Now())
		_ = g.l.DeckPublishedState("admin")
	}
	if d, lib := st.asked(); d != deck || lib != library {
		t.Fatalf("an unchanged deck asked the store: %d deck and %d library requests more", d-deck, lib-library)
	}

	st.mu.Lock()
	st.rev = 9
	st.mu.Unlock()
	g.l.deckPdfs().changed("admin/"+ProjectDirname+"/Main.dc.html", time.Now())
	waitFor(t, "the PDF built again from the new revision", func() bool {
		g.l.keepDeckPdfsOnce(time.Now())
		raw, err := os.ReadFile(revFile)
		return err == nil && strings.TrimSpace(string(raw)) == "9"
	})
	if d, _ := st.asked(); d == deck {
		t.Fatal("the change did not have the store asked")
	}
}

// A change the folder's watcher never told is found by the look around: one request for the whole library, and only
// the deck it names is asked about again.
func TestDeckPdfLookAroundFindsAChangeTheWatcherMissed(t *testing.T) {
	g, st, revFile := storeKeptDeck(t)
	st.mu.Lock()
	st.rev = 9
	st.mu.Unlock()
	for range 5 {
		g.l.keepDeckPdfsOnce(time.Now())
	}
	if raw, _ := os.ReadFile(revFile); strings.TrimSpace(string(raw)) != "7" {
		t.Fatalf("built before the look around was due: %q", raw)
	}
	_, library := st.asked()
	// The look around is due.
	keep := g.l.deckPdfs()
	keep.revMu.Lock()
	keep.topNext = time.Time{}
	keep.revMu.Unlock()
	waitFor(t, "the PDF built again from the revision the look around found", func() bool {
		g.l.keepDeckPdfsOnce(time.Now())
		raw, err := os.ReadFile(revFile)
		return err == nil && strings.TrimSpace(string(raw)) == "9"
	})
	if _, lib := st.asked(); lib != library+1 {
		t.Fatalf("the look around asked the library %d times", lib-library)
	}
}

func TestDeckPdfPartIsWhatThePdfIsMadeOf(t *testing.T) {
	for rel, want := range map[string]string{
		"admin/project/Main.dc.html":   "admin",
		"admin/project/img/a/b.png":    "admin",
		"admin/meta/deck.json":         "admin",
		"admin/meta/pages/p1.json":     "admin",
		"admin/meta/notes/p1.md":       "",
		"admin/meta/pages/deep/p.json": "",
		"admin/.media/published/x.pdf": "",
		"admin":                        "",
	} {
		if id, _ := deckPdfPart(rel); id != want {
			t.Errorf("deckPdfPart(%q) = %q, want %q", rel, id, want)
		}
	}
}
