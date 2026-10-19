package design

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real check of a tiny project in the machine's headless browser, served by a stand-in of the library server.
func TestCheckInChrome(t *testing.T) {
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
	boards := map[string]string{
		"Good.dc.html":   `<html><body><h1>Привет, это доска</h1><button onclick="throw new Error('boom on click')">Жми</button></body></html>`,
		"Broken.dc.html": `<html><body><script>throw new Error('broken board')</script><p>текст есть</p></body></html>`,
		"Empty.dc.html":  `<html><body></body></html>`,
		"Phone.dc.html":  `<html><body><p>картинка</p><img src="missing.png"></body></html>`,
	}
	for name, src := range boards {
		if err := os.WriteFile(filepath.Join(project, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
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

	started := l.StartCheck("demo", nil)
	if started == nil || started.Total != 8 {
		t.Fatalf("start = %+v", started)
	}
	if again := l.StartCheck("demo", nil); again == nil || again.StartedAt != started.StartedAt {
		t.Fatal("a running check is not returned again")
	}
	deadline := time.Now().Add(3 * time.Minute)
	var r *CheckReport
	for {
		_, r = l.ReadCheckReport("demo")
		if r != nil && r.FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("check did not finish")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if r.Failure != nil || r.Done != 8 || len(r.Boards) != 4 {
		t.Fatalf("report = %s", reportJSON(r))
	}
	byTitle := map[string]CheckBoard{}
	for _, b := range r.Boards {
		byTitle[b.Title] = b
	}
	if s := byTitle["Broken"]; s.Status != statusError || !strings.Contains(strings.Join(s.Shots[0].Errors, "|"), "broken board") {
		t.Errorf("broken = %+v", s)
	}
	if s := byTitle["Empty"]; s.Status != statusBlank {
		t.Errorf("empty = %+v", s)
	}
	if s := byTitle["Good"]; s.Status != statusError || !strings.Contains(strings.Join(s.Shots[0].Errors, "|"), "клик «Жми»: ") {
		t.Errorf("good (a click throws) = %+v", s)
	}
	if s := byTitle["Phone"]; s.Status != statusWarning || len(s.Shots[1].FailedRequests) == 0 {
		t.Errorf("phone = %+v", s)
	}
	for _, b := range r.Boards {
		for _, sh := range b.Shots {
			if sh.Screenshot == nil {
				t.Errorf("%s %s: no screenshot", b.Title, sh.View)
				continue
			}
			if st, err := os.Stat(filepath.Join(l.CheckDir("demo"), *sh.Screenshot)); err != nil || st.Size() < 500 {
				t.Errorf("%s: screenshot %v", *sh.Screenshot, err)
			}
		}
	}
	if r.Cover == nil {
		t.Error("no cover")
	}

	ans, ok := l.BoardThumbNow("demo", "Good.dc.html", true)
	if !ok || ans.Current || ans.Drawing == nil {
		t.Fatalf("thumb answer = %+v %v", ans, ok)
	}
	file, done := ans.Drawing.Wait(time.Minute)
	if !done || file == "" {
		t.Fatal("no picture drawn")
	}
	if ans, ok := l.BoardThumbNow("demo", "Good.dc.html", true); !ok || !ans.Current || ans.File != file {
		t.Fatalf("picture not kept: %+v", ans)
	}
	l.thumbs().closeBrowser()
	profiles, _ := filepath.Glob(filepath.Join(os.TempDir(), "test-design-check-*"))
	if len(profiles) > 0 {
		t.Errorf("profiles left: %v", profiles)
	}
}
