package design

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// A one-pixel picture for the slides of a test presentation.
var tinyPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

func testPPTX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := writePPTX(&buf, pptxDeck{Title: "Квартал <1> & 2", W: 1920, H: 1080, Lang: "ru-RU", Slides: []pptxSlide{
		{Title: "Обложка", Picture: tinyPNG, Notes: "Скажи привет.\n[пауза]\nПотом цифры & выводы.", Texts: []pptxText{
			{X: 120, Y: 200, W: 900, H: 120, Align: "l", LineH: 120, Runs: []pptxRun{
				{Text: "Заголовок ", Size: 96, Bold: true, Color: "1c1c1f", Alpha: 1, Font: "Inter"},
				{Text: "слайда", Size: 96, Italic: true, Underline: true, Color: "7b61ff", Alpha: 0.5, Font: "Inter", Spacing: 1}}},
			{X: 120, Y: 400, W: 700, H: 160, Align: "ctr", LineH: 40, Wrap: true, Runs: []pptxRun{
				{Text: "Две строки", Size: 32, Color: "000000", Alpha: 1}, {Br: true}, {Text: "и ещё <одна> & «кавычки»", Size: 32, Color: "000000", Alpha: 1}}},
		}},
		{Title: "Без заметок", Picture: tinyPNG},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func unzipAll(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		out[f.Name] = b
	}
	return out
}

func TestPPTXStructure(t *testing.T) {
	parts := unzipAll(t, testPPTX(t))
	for name, b := range parts {
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			d := xml.NewDecoder(bytes.NewReader(b))
			for {
				if _, err := d.Token(); err != nil {
					if err != io.EOF {
						t.Fatalf("%s is not well-formed: %v", name, err)
					}
					break
				}
			}
		}
	}
	// Every part has a content type, and every override names a part that is there.
	var ct struct {
		Defaults []struct {
			Extension string `xml:"Extension,attr"`
		} `xml:"Default"`
		Overrides []struct {
			PartName string `xml:"PartName,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(parts["[Content_Types].xml"], &ct); err != nil {
		t.Fatal(err)
	}
	ext, over := map[string]bool{}, map[string]bool{}
	for _, d := range ct.Defaults {
		ext[d.Extension] = true
	}
	for _, o := range ct.Overrides {
		over[strings.TrimPrefix(o.PartName, "/")] = true
		if _, ok := parts[strings.TrimPrefix(o.PartName, "/")]; !ok {
			t.Errorf("override of a part that is not there: %s", o.PartName)
		}
	}
	for name := range parts {
		if name == "[Content_Types].xml" {
			continue
		}
		if !over[name] && !ext[strings.TrimPrefix(path.Ext(name), ".")] {
			t.Errorf("%s has no content type", name)
		}
		if strings.HasSuffix(name, ".xml") && !strings.HasPrefix(name, "docProps") && !over[name] {
			t.Errorf("%s has no override", name)
		}
	}
	// Every relationship points at a part that is there.
	for name, b := range parts {
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		var rels struct {
			R []struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}
		if err := xml.Unmarshal(b, &rels); err != nil {
			t.Fatal(err)
		}
		base := path.Dir(path.Dir(name))
		if base == "." {
			base = ""
		}
		for _, r := range rels.R {
			if _, ok := parts[path.Join(base, r.Target)]; !ok {
				t.Errorf("%s: %s points at %s, which is not there", name, r.ID, r.Target)
			}
		}
	}
	// Two slides, a notes page for the first only, the text in runs, the notes in paragraphs.
	for _, n := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide2.xml", "ppt/notesSlides/notesSlide1.xml", "ppt/media/image2.png"} {
		if parts[n] == nil {
			t.Errorf("missing %s", n)
		}
	}
	if parts["ppt/notesSlides/notesSlide2.xml"] != nil {
		t.Error("a slide without notes got a notes page")
	}
	s1 := string(parts["ppt/slides/slide1.xml"])
	for _, want := range []string{`<a:t>Заголовок </a:t>`, `<a:t>слайда</a:t>`, `u="sng"`, `<a:alpha val="50000"/>`, `sz="4800"`, `<a:br>`, `и ещё &lt;одна&gt; &amp; «кавычки»`, `wrap="none"`, `wrap="square"`, `r:embed="rId2"`} {
		if !strings.Contains(s1, want) {
			t.Errorf("slide1.xml lacks %s\n%s", want, s1)
		}
	}
	notes := string(parts["ppt/notesSlides/notesSlide1.xml"])
	for _, want := range []string{`<a:t>Скажи привет.</a:t>`, `<a:t>[пауза]</a:t>`, `<a:t>Потом цифры &amp; выводы.</a:t>`} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %s", want)
		}
	}
	if pres := string(parts["ppt/presentation.xml"]); !strings.Contains(pres, `<p:sldSz cx="12192000" cy="6858000"/>`) || strings.Count(pres, "<p:sldId ") != 2 {
		t.Errorf("presentation.xml = %s", pres)
	}
	// An XML character that cannot be written is dropped, not written.
	if got := pptxEsc("a\x01b\u0008c"); got != "abc" {
		t.Errorf("pptxEsc = %q", got)
	}
}

func TestPPTXRefusesInjection(t *testing.T) {
	var buf bytes.Buffer
	evil := `"/><a:x y="`
	err := writePPTX(&buf, pptxDeck{Title: evil, W: 1920, H: 1080, Lang: evil, Slides: []pptxSlide{{Title: evil, Picture: tinyPNG, Notes: "n", Texts: []pptxText{
		{X: 1, Y: 1, W: 10, H: 10, Align: evil, LineH: 20, Runs: []pptxRun{
			{Text: "ok", Size: 20, Color: "ff0000", Alpha: 1, Font: evil},
			{Text: "bad colour", Size: 20, Color: evil, Alpha: 1},
		}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := unzipAll(t, buf.Bytes())
	for name, b := range parts {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			tok, err := d.Token()
			if err != nil {
				if err != io.EOF {
					t.Fatalf("%s: %v", name, err)
				}
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "x" {
				t.Fatalf("%s: an element was injected", name)
			}
		}
	}
	s := string(parts["ppt/slides/slide1.xml"])
	if !strings.Contains(s, `algn="l"`) || strings.Contains(s, "bad colour") || !strings.Contains(s, "<a:t>ok</a:t>") {
		t.Errorf("slide1.xml = %s", s)
	}
}

func TestDeckFrameIsSandboxed(t *testing.T) {
	f := deckFrame(deckSlideSrc{W: 1920, H: 1080, Title: `a"b`}, `<p class="x">&</p>`)
	if !strings.Contains(f, ` sandbox=""`) || !strings.Contains(f, `title="a&#34;b"`) || !strings.Contains(f, `srcdoc="<p class=&quot;x&quot;>&amp;</p>"`) {
		t.Errorf("frame = %s", f)
	}
}

func TestPPTXInOffice(t *testing.T) {
	if testing.Short() {
		t.Skip("opens the file in LibreOffice")
	}
	soffice, err := exec.LookPath("soffice")
	if err != nil {
		t.Skip("no LibreOffice on this machine")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "deck.pptx")
	if err := os.WriteFile(file, testPPTX(t), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nice", "-n", "15", soffice, "--headless", "-env:UserInstallation=file://"+filepath.Join(dir, "profile"),
		"--convert-to", "pdf", "--outdir", dir, file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("soffice: %v\n%s", err, out)
	}
	pdf, err := os.ReadFile(filepath.Join(dir, "deck.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if n := deckPDFPages(pdf); n != 2 {
		t.Fatalf("LibreOffice made %d pages from 2 slides", n)
	}
}

func TestDeckFileNames(t *testing.T) {
	cases := map[string]string{
		"Квартал 3":         "Квартал 3.pdf",
		`a/b\c:d*e?"f<g>h|`: "a b c d e f g h.pdf",
		"   ":               "deck.pdf",
		"..hidden":          "hidden.pdf",
	}
	for in, want := range cases {
		if got := deckFileName(in, "pdf"); got != want {
			t.Errorf("deckFileName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := rfc5987("Квартал 3.pdf"); got != "%D0%9A%D0%B2%D0%B0%D1%80%D1%82%D0%B0%D0%BB%203.pdf" {
		t.Errorf("rfc5987 = %s", got)
	}
	if got := deckSlideFile(2, "Цифры: Q3 / итоги!", "png"); got != "03-цифры-q3-итоги.png" {
		t.Errorf("slide file = %s", got)
	}
	if got := deckSlideFile(0, "???", "png"); got != "01.png" {
		t.Errorf("slide file = %s", got)
	}
}

// fakeExporter stands in for a format for the length of a test.
func fakeExporter(t *testing.T, f DeckExportFormat, e DeckExporter) {
	t.Helper()
	deckExportersMu.Lock()
	prev, had := deckExporters[f]
	deckExporters[f] = e
	deckExportersMu.Unlock()
	t.Cleanup(func() {
		deckExportersMu.Lock()
		defer deckExportersMu.Unlock()
		if had {
			deckExporters[f] = prev
		} else {
			delete(deckExporters, f)
		}
	})
}

func waitJob(t *testing.T, l *Library, artifact, id string, want DeckJobState) DeckExportJob {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		j, _, ok := l.DeckExportJobOf(artifact, id)
		if !ok {
			t.Fatalf("job %s is gone", id)
		}
		if j.State == want {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s is %s, want %s", id, j.State, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func testDeckLibrary(t *testing.T) *Library {
	t.Helper()
	root := t.TempDir()
	l := New(Config{LibraryDir: root, PlatformAPI: "/api/x", Texts: Texts{Tag: "test-design", CLI: "acme"}})
	t.Cleanup(l.StopWatching)
	project := filepath.Join(root, "demo", ProjectDirname)
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "One.dc.html"), []byte(`<html><body><h1>One</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestDeckJobsRunOneAtATime(t *testing.T) {
	l := testDeckLibrary(t)
	gate := make(chan struct{})
	var mu sync.Mutex
	running, most := 0, 0
	fakeExporter(t, DeckExportMP4, DeckExporter{Ext: "mp4", Run: func(ctx context.Context, run DeckExportRun) error {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		run.Progress(0.5)
		<-gate
		mu.Lock()
		running--
		mu.Unlock()
		return os.WriteFile(run.Out, []byte("video"), 0o644)
	}})
	a, err := l.StartDeckExport("demo", DeckExportMP4, "rec1", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.StartDeckExport("demo", DeckExportMP4, "rec2", false)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := l.StartDeckExport("demo", DeckExportMP4, "rec1", false); again.ID != a.ID {
		t.Errorf("the same export started twice: %s and %s", again.ID, a.ID)
	}
	waitJob(t, l, "demo", a.ID, DeckJobRunning)
	time.Sleep(60 * time.Millisecond)
	if j, _, _ := l.DeckExportJobOf("demo", b.ID); j.State != DeckJobQueued {
		t.Errorf("second job is %s while the first runs", j.State)
	}
	if j, _, _ := l.DeckExportJobOf("demo", a.ID); j.Progress != 0.5 {
		t.Errorf("progress = %v", j.Progress)
	}
	gate <- struct{}{}
	done := waitJob(t, l, "demo", a.ID, DeckJobDone)
	if done.File == nil || done.Progress != 1 || done.Error != nil {
		t.Errorf("done job = %+v", done)
	}
	gate <- struct{}{}
	waitJob(t, l, "demo", b.ID, DeckJobDone)
	if most != 1 {
		t.Errorf("%d jobs ran at once", most)
	}
}

func TestDeckJobFailsInWords(t *testing.T) {
	l := testDeckLibrary(t)
	fakeExporter(t, DeckExportMP4, DeckExporter{Needs: []string{"no-such-tool-for-acme"}, Ext: "mp4", Run: func(context.Context, DeckExportRun) error {
		t.Error("ran without its tool")
		return nil
	}})
	j, err := l.StartDeckExport("demo", DeckExportMP4, "", false)
	if err != nil || j.State != DeckJobFailed || j.Error == nil || !strings.Contains(*j.Error, "no-such-tool-for-acme") {
		t.Fatalf("job = %+v, %v", j, err)
	}
	fakeExporter(t, DeckExportMP4, DeckExporter{Ext: "mp4", Run: func(_ context.Context, run DeckExportRun) error {
		_ = os.WriteFile(run.Out, []byte("half"), 0o644)
		return errors.New("the encoder stopped")
	}})
	j, _ = l.StartDeckExport("demo", DeckExportMP4, "", false)
	j = waitJob(t, l, "demo", j.ID, DeckJobFailed)
	if j.Error == nil || *j.Error != "the encoder stopped" || j.File != nil {
		t.Errorf("job = %+v", j)
	}
	if left, _ := filepath.Glob(filepath.Join(l.ArtifactDir("demo"), DeckExportsDir, "*")); len(left) != 0 {
		t.Errorf("a failed job left %v", left)
	}
	if _, err := l.StartDeckExport("demo", DeckExportFormat("gif"), "", false); err == nil {
		t.Error("an unknown format started")
	}
}

func TestDeckExportRoutes(t *testing.T) {
	l := testDeckLibrary(t)
	fakeExporter(t, DeckExportPDF, DeckExporter{Ext: "pdf", Run: func(_ context.Context, run DeckExportRun) error {
		return os.WriteFile(run.Out, []byte("%PDF-1.4 fake"), 0o644)
	}})
	a := &API{L: l, Session: func(r *http.Request) bool { return r.Header.Get("Cookie") == "ok" },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	post := func(body string) Answer {
		req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		req.Header.Set("Cookie", "ok")
		req.Header.Set("Content-Type", "application/json")
		return a.DeckExport(req)
	}
	if ans := post(`{"artifact":"Bad","format":"pdf"}`); ans.Status != 400 {
		t.Errorf("bad artifact = %d", ans.Status)
	}
	if ans := post(`{"artifact":"demo","format":"gif"}`); ans.Status != 400 {
		t.Errorf("bad format = %d", ans.Status)
	}
	if ans := post(`{"artifact":"demo","format":"pptx","publish":true}`); ans.Status != 400 {
		t.Errorf("publish of a pptx = %d", ans.Status)
	}
	ans := post(`{"artifact":"demo","format":"pdf","publish":true}`)
	if ans.Status != 202 {
		t.Fatalf("start = %d %s", ans.Status, ans.Body)
	}
	var job DeckExportJob
	if err := json.Unmarshal(ans.Body.(json.RawMessage), &job); err != nil || job.Format != DeckExportPDF || job.ID == "" {
		t.Fatalf("job = %+v, %v", job, err)
	}
	waitJob(t, l, "demo", job.ID, DeckJobDone)
	req := httptest.NewRequest("GET", "/x?artifact=demo&job="+job.ID, nil)
	req.Header.Set("Cookie", "ok")
	got := a.DeckExport(req)
	var done DeckExportJob
	if err := json.Unmarshal(got.Body.(json.RawMessage), &done); err != nil || got.Status != 200 || done.State != DeckJobDone || done.File == nil {
		t.Fatalf("poll = %d %s", got.Status, got.Body)
	}
	if got := string(got.Body.(json.RawMessage)); !strings.Contains(got, `"error":null`) || !strings.Contains(got, `"progress":1`) {
		t.Errorf("job JSON = %s", got)
	}
	req = httptest.NewRequest("GET", "/x?artifact=demo&job=nope", nil)
	req.Header.Set("Cookie", "ok")
	if got := a.DeckExport(req); got.Status != 404 {
		t.Errorf("unknown job = %d", got.Status)
	}
	// The file, as an attachment.
	w := httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/x?artifact=demo&job="+job.ID, nil)
	req.Header.Set("Cookie", "ok")
	a.DeckExportFile(w, req)
	disp := w.Header().Get("Content-Disposition")
	if w.Code != 200 || w.Body.String() != "%PDF-1.4 fake" || w.Header().Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(disp, "attachment; ") || !strings.Contains(disp, ".pdf") || !strings.Contains(disp, "filename*=UTF-8''") {
		t.Errorf("file = %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/x?artifact=demo&job="+job.ID, nil)
	a.DeckExportFile(w, req)
	if w.Code != 403 {
		t.Errorf("file without a session = %d", w.Code)
	}
	// Published for the guests.
	if b, err := os.ReadFile(filepath.Join(l.ArtifactDir("demo"), DeckPublishedDir, "deck.pdf")); err != nil || string(b) != "%PDF-1.4 fake" {
		t.Errorf("published pdf = %q, %v", b, err)
	}
}

func TestDeckExportsExpire(t *testing.T) {
	l := testDeckLibrary(t)
	dir := filepath.Join(l.ArtifactDir("demo"), DeckExportsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old, fresh := filepath.Join(dir, "old.pdf"), filepath.Join(dir, "fresh.pdf")
	for _, f := range []string{old, fresh} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	l.SweepDeckExports("demo")
	if _, err := os.Stat(old); err == nil {
		t.Error("a file a day old stayed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh file went")
	}
	// A job a day old is forgotten with its file.
	fakeExporter(t, DeckExportMP4, DeckExporter{Ext: "mp4", Run: func(_ context.Context, run DeckExportRun) error {
		return os.WriteFile(run.Out, []byte("v"), 0o644)
	}})
	j, _ := l.StartDeckExport("demo", DeckExportMP4, "", false)
	waitJob(t, l, "demo", j.ID, DeckJobDone)
	s := l.deckJobs()
	s.mu.Lock()
	s.jobs[jobKey("demo", j.ID)].made = past
	s.mu.Unlock()
	j2, _ := l.StartDeckExport("demo", DeckExportMP4, "other", false)
	waitJob(t, l, "demo", j2.ID, DeckJobDone)
	if _, p, ok := l.DeckExportJobOf("demo", j.ID); ok {
		t.Errorf("an old job is still known (%s)", p)
	}
}

// ---- the exporters in the machine's browser

const deckBoard = `<html><head><style>
html,body{margin:0;width:100%%;height:100%%;background:#fff;font-family:Georgia,serif}
.t{position:absolute;left:120px;top:140px;font-size:96px;font-weight:700;color:#1c1c1f}
.s{position:absolute;left:120px;top:300px;width:700px;font-size:36px;line-height:48px;color:#7b61ff}
.b{position:absolute;right:100px;bottom:100px;width:300px;height:200px;background:#ffdd2d;background-image:url(pic.png)}
</style></head><body>
<div class="t">%s</div><p class="s">Первая строка и <b>жирная</b> вторая строка достаточно длинная, чтобы перенестись</p>
<div class="b" onclick="evil()"></div><a href="javascript:evil()" onmouseover="evil()">x</a>
<svg width="10" height="10"><rect width="10" height="10"><animate attributeName="x" to="5"/></rect></svg><img src="pic.png" width="40" height="40" style="position:absolute;left:10px;top:10px">
</body></html>`

func TestExportersInChrome(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a browser")
	}
	root := t.TempDir()
	l := New(Config{LibraryDir: root, PlatformAPI: "/api/x", Texts: Texts{Tag: "test-design", CLI: "acme"}})
	if l.findChrome() == "" {
		t.Skip("no browser on this machine")
	}
	defer l.StopWatching()
	project := filepath.Join(root, "demo", ProjectDirname)
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	titles := []string{"Alpha", "Beta", "Gamma"}
	for i, title := range titles {
		src := strings.Replace(deckBoard, "%s", title, 1)
		src = strings.ReplaceAll(src, "%%", "%")
		if err := os.WriteFile(filepath.Join(project, string(rune('A'+i))+".dc.html"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "pic.png"), tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref, file, ok := ParsePagePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		real := l.ResolveProjectFile(ref, strings.Split(file, "/"))
		if real == "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, real)
	}))
	defer srv.Close()
	prev := libraryBase
	libraryBase = func(*Library) (string, error) { return srv.URL, nil }
	defer func() { libraryBase = prev }()

	export := func(f DeckExportFormat) string {
		j, err := l.StartDeckExport("demo", f, "", false)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Minute)
		for {
			cur, p, _ := l.DeckExportJobOf("demo", j.ID)
			if cur.State == DeckJobDone {
				return p
			}
			if cur.State == DeckJobFailed {
				t.Fatalf("%s failed: %s", f, *cur.Error)
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s did not finish", f)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	t.Run("pdf", func(t *testing.T) {
		pdf, err := os.ReadFile(export(DeckExportPDF))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) || deckPDFPages(pdf) != len(titles) {
			t.Fatalf("pdf: %d bytes, %d pages", len(pdf), deckPDFPages(pdf))
		}
	})
	t.Run("png", func(t *testing.T) {
		b, err := os.ReadFile(export(DeckExportPNG))
		if err != nil {
			t.Fatal(err)
		}
		parts := unzipAll(t, b)
		if len(parts) != len(titles) {
			t.Fatalf("zip has %d pictures", len(parts))
		}
		for name, pic := range parts {
			if !strings.HasSuffix(name, ".png") || !bytes.HasPrefix(pic, []byte("\x89PNG")) {
				t.Errorf("%s is not a picture", name)
			}
		}
	})
	t.Run("pptx", func(t *testing.T) {
		b, err := os.ReadFile(export(DeckExportPPTX))
		if err != nil {
			t.Fatal(err)
		}
		parts := unzipAll(t, b)
		for i, title := range titles {
			s := string(parts["ppt/slides/slide"+string(rune('1'+i))+".xml"])
			if !strings.Contains(s, "<a:t>"+title+"</a:t>") || !strings.Contains(s, "<a:t>жирная</a:t>") {
				t.Errorf("slide %d texts: %s", i+1, s)
			}
			if !strings.Contains(s, `b="1"`) || !strings.Contains(s, `sz="`) {
				t.Errorf("slide %d runs carry no style", i+1)
			}
		}
	})
	t.Run("html", func(t *testing.T) {
		b, err := os.ReadFile(export(DeckExportHTML))
		if err != nil {
			t.Fatal(err)
		}
		page := string(b)
		if strings.Count(page, "<iframe ") != len(titles) {
			t.Fatalf("%d frames", strings.Count(page, "<iframe "))
		}
		if bad := remoteResources(page); len(bad) > 0 {
			t.Errorf("the page still reaches for %v", bad)
		}
		if strings.Count(page, ` sandbox=""`) != len(titles) || strings.Contains(page, "javascript:") ||
			strings.Contains(page, "&lt;animate") || strings.Contains(page, "evil()") {
			t.Error("a frame is not sandboxed, or the page kept something that acts")
		}
		if !strings.Contains(page, "data:image/png;base64") || strings.Contains(page, "<script src") {
			t.Error("the picture was not written in, or a script stayed")
		}
	})
}

// remoteResources are the http(s) addresses a page loads from: src, poster, srcset and href of what is not a link,
// and url() in styles (the srcdoc of a frame is read unescaped).
func remoteResources(page string) []string {
	page = strings.NewReplacer("&quot;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(page)
	re := regexp.MustCompile(`(?i)(?:\b(?:src|poster|srcset)=["']?\s*|url\(\s*["']?)(?:https?:)?//[^\s"')]+`)
	return re.FindAllString(page, -1)
}
