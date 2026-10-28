package design

import (
	"context"
	"os"
	"path/filepath"
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
