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

// a snapshot plays without its machine.

func TestLibAddressesPointAtTheSnapshot(t *testing.T) {
	l := &Library{}
	src := `<script src="/__lib/motion/gsap.min.js"></script><script src='/__lib/motion/screen.js'></script>` +
		`<style>@font-face{src:url(/__lib/motion/fonts/geologica-cyrillic-wght-normal.woff2)}</style><a href="/other">x</a>`
	got := l.libAddressesAt(src, "../")
	for _, want := range []string{`src="../__lib/motion/gsap.min.js?v=`, `src='../__lib/motion/screen.js?v=`, `url(../__lib/motion/fonts/geologica-cyrillic-wght-normal.woff2?v=`, `href="/other"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, `"/__lib/`) {
		t.Errorf("an absolute library address is left: %s", got)
	}
}

func TestVideoRuntimeComesFirstInHead(t *testing.T) {
	l := &Library{}
	got := l.withVideoRuntime(`<html><head><title>x</title></head>`, "")
	if !strings.HasPrefix(got, `<html><head><script src="__lib/video-runtime.js?v=`) {
		t.Errorf("runtime is not at the head: %s", got)
	}
}

func TestPublicLibServesVersionedFilesImmutable(t *testing.T) {
	a := &API{L: &Library{}}
	v := a.L.libFileVersion("motion/gsap.min.js")
	if v == "" {
		t.Fatal("the build has no gsap")
	}
	for _, c := range []struct{ name, query, cache string }{
		{"motion/gsap.min.js", "?v=" + v, libImmutable},
		{"motion/gsap.min.js", "?v=old", libRevalidate},
		{"video-runtime.js", "?v=" + a.L.libFileVersion("video-runtime.js"), libImmutable},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/x/"+c.name+c.query, nil)
		a.publicLib(w, r, strings.Split(c.name, "/"), false)
		if w.Code != 200 || w.Header().Get("Cache-Control") != c.cache || w.Body.Len() == 0 {
			t.Errorf("%s%s: %d %q (%d bytes)", c.name, c.query, w.Code, w.Header().Get("Cache-Control"), w.Body.Len())
		}
	}
	w := httptest.NewRecorder()
	a.publicLib(w, httptest.NewRequest(http.MethodGet, "/x", nil), []string{"events"}, false)
	if w.Code != 404 {
		t.Errorf("the library's own routes are not public: %d", w.Code)
	}
}

func TestRecordClipHasALightCopy(t *testing.T) {
	if !isRecordClip([]string{"records", "chat.mp4"}) || isRecordClip([]string{"records", "chat.view.mp4"}) || isRecordClip([]string{"other", "chat.mp4"}) {
		t.Fatal("clips are told wrong")
	}
	if got := strings.Join(viewCopySegments([]string{"records", "chat.mp4"}), "/"); got != "records/chat.view.mp4" {
		t.Fatalf("copy path %s", got)
	}
	dir := t.TempDir()
	clip, light := filepath.Join(dir, "a.mp4"), filepath.Join(dir, "a.view.mp4")
	if err := os.WriteFile(clip, []byte("clip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !viewCopyStale(clip, light) {
		t.Error("a clip with no copy is stale")
	}
	if err := os.WriteFile(light, []byte("light"), 0o644); err != nil {
		t.Fatal(err)
	}
	if viewCopyStale(clip, light) {
		t.Error("a newer copy is fresh")
	}
	old := ModTimeBefore(t, light)
	if !viewCopyStale(clip, light) || !old {
		t.Error("a copy older than its clip is stale")
	}
}

func ModTimeBefore(t *testing.T, p string) bool {
	t.Helper()
	past := mustTime(-3600)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}
	return true
}

func mustTime(offsetSeconds int) (t time.Time) {
	return time.Now().Add(time.Duration(offsetSeconds) * time.Second)
}

func TestPublicSnapshotOfAVideoPlaysWithoutTheMachine(t *testing.T) {
	a, root := publicAPI(t, "")
	writeFile(t, filepath.Join(root, "v1", "artifact.json"), `{"title":"Clip"}`)
	writeFile(t, filepath.Join(root, "v1", "meta", "video.json"), `{"v":1}`)
	writeFile(t, filepath.Join(root, "v1", "project", "Scene.dc.html"),
		`<html><head><script src="/__lib/motion/gsap.min.js"></script></head><body>tkScreen({"src":"records/chat.mp4"})</body></html>`)
	heavy, light := strings.Repeat("H", 5000), strings.Repeat("l", 700)
	writeFile(t, filepath.Join(root, "v1", "project", "records", "chat.mp4"), heavy)
	writeFile(t, filepath.Join(root, "v1", "project", "records", "chat.view.mp4"), light)
	writeFile(t, filepath.Join(root, "v1", "project", "records", "solo.mp4"), heavy)
	if !a.L.IsVideo("v1") {
		t.Skip("the fixture is not a video here")
	}
	tok := publicToken(t, "pair-secret", "acme", "v1", nil)

	page := publicGet(a, "GET", "v1/Scene.dc.html", tok)
	body := page.Body.String()
	for _, want := range []string{`src="__lib/motion/gsap.min.js?v=`, `<script src="__lib/video-runtime.js?v=`, `data-guest="1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the scene lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, `"/__lib/`) {
		t.Errorf("an absolute library address is left: %s", body)
	}

	// The kit it points at is the hub's own, kept for good.
	v := a.L.libFileVersion("motion/gsap.min.js")
	kit := publicGet(a, "GET", "v1/__lib/motion/gsap.min.js?v="+v, tok)
	if kit.Code != 200 || kit.Header().Get("Cache-Control") != libImmutable {
		t.Errorf("kit: %d %q", kit.Code, kit.Header().Get("Cache-Control"))
	}

	// A recording is answered from its light copy; one with no copy, from itself; both with ranges.
	if w := publicGet(a, "GET", "v1/records/chat.mp4", tok); w.Body.String() != light {
		t.Errorf("the view got %d bytes, not the light copy", w.Body.Len())
	}
	if w := publicGet(a, "GET", "v1/records/solo.mp4", tok); w.Body.String() != heavy {
		t.Errorf("a clip with no copy got %d bytes", w.Body.Len())
	}
	ranged := publicRequest(a, "GET", "v1/records/chat.mp4", tok, http.Header{"Range": {"bytes=100-199"}})
	if ranged.Code != 206 || ranged.Body.String() != light[100:200] {
		t.Errorf("range: %d %d bytes", ranged.Code, ranged.Body.Len())
	}
}

func TestViewCopyReadsOnlyTheProjectsOwnRecords(t *testing.T) {
	root := t.TempDir()
	own := filepath.Join(root, "p1", "project", "records", "a.mp4")
	writeFile(t, own, "clip")
	outside := filepath.Join(t.TempDir(), "x.mp4")
	writeFile(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(root, "p1", "project", "records", "link.mp4")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "p2", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "p2", "project", "records")); err != nil {
		t.Fatal(err)
	}
	if !viewCopyInside(root, own) {
		t.Error("the project's own clip is read")
	}
	if viewCopyInside(root, filepath.Join(root, "p1", "project", "records", "link.mp4")) {
		t.Error("a linked file is not read")
	}
	if viewCopyInside(root, filepath.Join(root, "p2", "project", "records", "x.mp4")) {
		t.Error("a records folder that is a link out is not read")
	}
}

// the editor plays a recording from its light copy too, and only the final render gets the clip itself.
func TestEditorPlaysTheLightCopyAndTheRenderTheClip(t *testing.T) {
	root := t.TempDir()
	l := testLibrary(t, root)
	l.srv = &server{}
	heavy, light := strings.Repeat("H", 5000), strings.Repeat("l", 700)
	writeFile(t, filepath.Join(root, "clip", ArtifactFile), `{"title":"clip"}`)
	writeFile(t, filepath.Join(root, "clip", ProjectDirname, RecordsDir, "chat.mp4"), heavy)
	writeFile(t, filepath.Join(root, "clip", ProjectDirname, RecordsDir, "chat.view.mp4"), light)
	writeFile(t, filepath.Join(root, "clip", ProjectDirname, RecordsDir, "solo.mp4"), heavy)
	get := func(path string, header http.Header) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18470"
		for k, v := range header {
			r.Header[k] = v
		}
		l.serveLibrary(w, r, 18470)
		return w
	}
	if w := get("/a/clip/p/records/chat.mp4", nil); w.Body.String() != light {
		t.Errorf("the editor got %d bytes (%d), not the light copy", w.Body.Len(), w.Code)
	}
	if w := get("/a/clip/p/records/chat.mp4", http.Header{FullClipsHeader: {"1"}}); w.Body.String() != heavy {
		t.Errorf("the render got %d bytes, not the clip", w.Body.Len())
	}
	if w := get("/a/clip/p/records/solo.mp4", nil); w.Body.String() != heavy {
		t.Errorf("a clip with no copy got %d bytes", w.Body.Len())
	}
	if w := get("/a/clip/p/records/chat.view.mp4", nil); w.Body.String() != light {
		t.Errorf("the copy itself got %d bytes", w.Body.Len())
	}
	ranged := get("/a/clip/p/records/chat.mp4", http.Header{"Range": {"bytes=100-199"}})
	if ranged.Code != 206 || ranged.Body.String() != light[100:200] {
		t.Errorf("range: %d %d bytes", ranged.Code, ranged.Body.Len())
	}
}
