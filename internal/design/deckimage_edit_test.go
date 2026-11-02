package design

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/imagegen"
)

// A picture of the project is changed from where the person points at it: only a picture the project holds, found
// through its links without leaving it.
func TestDesignImageEditSource(t *testing.T) {
	l, library := historyLibrary(t)
	const project = "p1"
	dir := filepath.Join(library, project, ProjectDirname)
	assets := filepath.Join(dir, genAssetsDir)
	if err := os.MkdirAll(assets, 0o777); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(assets, "logo.png"))
	write(filepath.Join(assets, "notes.txt"))
	write(filepath.Join(outside, "secret.png"))
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(assets, "linked.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}

	for name, rel := range map[string]string{
		"a picture":      "assets/logo.png",
		"in the project": "assets/../assets/logo.png",
	} {
		got := l.projectPicture(project, rel)
		if name == "a picture" && got == "" {
			t.Fatalf("%s: refused", name)
		}
		if name == "in the project" && got != "" {
			t.Fatalf("%s: a .. segment is kept out, got %s", name, got)
		}
	}
	for name, rel := range map[string]string{
		"empty":         "",
		"not a picture": "assets/notes.txt",
		"absolute":      "/etc/passwd.png",
		"remote":        "https://example.com/a.png",
		"link out":      "assets/linked.png",
		"folder link":   "out/secret.png",
		"missing":       "assets/none.png",
		"up":            "../secret.png",
	} {
		if got := l.projectPicture(project, rel); got != "" {
			t.Fatalf("%s: resolved to %s", name, got)
		}
	}

	a := &API{L: l, Session: func(*http.Request) bool { return true }, PlatformAPI: "/api/platform",
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false },
		Images:     &imagegen.Service{Dir: t.TempDir(), CodexHome: t.TempDir(), Binary: "/nonexistent/codex"}}
	post := func(body map[string]any) int {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/api/platform/design/image", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		writeAnswer(w, a.DesignImage(r))
		return w.Code
	}
	base := func(extra map[string]any) map[string]any {
		out := map[string]any{"artifact": project, "prompt": "светлее", "count": 1}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	for name, body := range map[string]map[string]any{
		"link out":          base(map[string]any{"source": "assets/linked.png"}),
		"folder link":       base(map[string]any{"source": "out/secret.png"}),
		"remote":            base(map[string]any{"source": "https://example.com/a.png"}),
		"not a picture":     base(map[string]any{"source": "assets/notes.txt"}),
		"mode with nothing": base(map[string]any{"mode": "erase"}),
		"marks, no picture": base(map[string]any{"overlay": "AAAA"}),
		"bad marks":         base(map[string]any{"source": "assets/logo.png", "mode": "mark", "overlay": "!!!"}),
		"unknown mode":      base(map[string]any{"source": "assets/logo.png", "mode": "melt"}),
		"size, no format":   base(map[string]any{"source": "assets/logo.png", "mode": "size"}),
	} {
		if code := post(body); code != http.StatusBadRequest {
			t.Fatalf("%s: %d", name, code)
		}
	}

	// The picture itself, for the editor's original: any picture of the project by its path, nothing that leaves it.
	file := func(query string) int {
		r := httptest.NewRequest(http.MethodGet, "/api/platform/design/image/file?artifact="+project+"&"+query, nil)
		w := httptest.NewRecorder()
		a.DesignImageFile(w, r)
		return w.Code
	}
	if code := file("path=assets/logo.png"); code != http.StatusOK {
		t.Fatalf("path: %d", code)
	}
	for _, q := range []string{"path=assets/linked.png", "path=out/secret.png", "path=assets/notes.txt", "path=%2Fetc%2Fa.png", "path=assets/none.png"} {
		if code := file(q); code == http.StatusOK {
			t.Fatalf("%s served", q)
		}
	}
}

// A changed picture remembers what it was made from; the original put back makes the element forget the job; a slot
// mark is spent once a picture is in.
func TestDeckPatchImageFromAndRestore(t *testing.T) {
	job := DeckPatchRequest{Src: "assets/gen-ab12cd34-1.png", Job: "gen-ab12cd34", Prompt: "светлее", From: "assets/photo.jpg assets/gen-00ff00ff-1.png"}
	board := `<div><div data-image-slot data-image-prompt="Обложка" style="width: 10px">x</div></div>`
	got, err := imagePatch(t, board, 1, job)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-gen-from="assets/photo.jpg assets/gen-00ff00ff-1.png"`, `data-gen-job="gen-ab12cd34"`, `data-image-prompt="Обложка"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "data-image-slot") {
		t.Fatalf("the slot mark stays:\n%s", got)
	}

	back := DeckPatchRequest{Src: "assets/photo.jpg"}
	was := deckFingerprint(mustElem(t, got, 1))
	back.Was, back.Op, back.Tpl = &was, DeckPatchImage, 1
	restored, err := applyDeckPatch(got, back, 1920)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(restored, "url(assets/photo.jpg)") || strings.Contains(restored, "data-gen-") {
		t.Fatalf("restore:\n%s", restored)
	}

	var pe *deckPatchErr
	for name, bad := range map[string]DeckPatchRequest{
		"from escapes":   {Src: job.Src, Job: job.Job, Prompt: "x", From: "../a.png"},
		"from remote":    {Src: job.Src, Job: job.Job, Prompt: "x", From: "https://a/b.png"},
		"from one bad":   {Src: job.Src, Job: job.Job, Prompt: "x", From: "assets/a.png ../b.png"},
		"from quote":     {Src: job.Src, Job: job.Job, Prompt: "x", From: "a\") x(\".png"},
		"restore escape": {Src: "../a.png"},
		"restore folder": {Src: "assets//a.png"},
		"restore url":    {Src: "assets/a b).png"},
		"restore text":   {Src: "assets/notes.txt"},
	} {
		if _, err := imagePatch(t, `<div><img src="a.png"></div>`, 1, bad); !asPatchErr(err, &pe) || pe.status != http.StatusBadRequest {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// The picture of an element comes from the board's source (the page only sees heavy files by their content address):
// the right element, the project's own file, nothing remote or interpolated.
func TestDesignImageSource(t *testing.T) {
	l, library := historyLibrary(t)
	const project = "p1"
	dir := filepath.Join(library, project, ProjectDirname)
	if err := os.MkdirAll(filepath.Join(dir, genAssetsDir), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, genAssetsDir, "logo.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	board := `<div><img src="./assets/logo.png?v=2"><section style="color: red; background: #fff url('assets/logo.png') center"></section><img src="https://x.test/a.png"><img src="{{ pic }}"><img src="assets/none.png"></div>`
	src := deckBoardOf(board)
	if err := os.WriteFile(filepath.Join(dir, "A.dc.html"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &API{L: l, Session: func(*http.Request) bool { return true }, PlatformAPI: "/api/platform",
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	get := func(idx int, extra string) (int, map[string]any) {
		was := deckFingerprint(mustElem(t, src, idx))
		r := httptest.NewRequest(http.MethodGet, "/api/platform/design/image?artifact="+project+"&file=A.dc.html&tpl="+strconv.Itoa(idx)+"&was="+was+extra, nil)
		w := httptest.NewRecorder()
		writeAnswer(w, a.DesignImage(r))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	for idx, want := range map[int]string{1: "assets/logo.png", 2: "assets/logo.png"} {
		if code, out := get(idx, ""); code != http.StatusOK || out["src"] != want {
			t.Fatalf("element %d: %d %v", idx, code, out)
		}
	}
	for _, idx := range []int{3, 4, 5} {
		if code, _ := get(idx, ""); code != http.StatusUnprocessableEntity {
			t.Fatalf("element %d is not the project's own picture: %d", idx, code)
		}
	}
	// A fingerprint of another element, a path that is not a board, a link out.
	r := httptest.NewRequest(http.MethodGet, "/api/platform/design/image?artifact="+project+"&file=A.dc.html&tpl=1&was=img:zzzz", nil)
	w := httptest.NewRecorder()
	writeAnswer(w, a.DesignImage(r))
	if w.Code != http.StatusConflict {
		t.Fatalf("stale: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/platform/design/image?artifact="+project+"&file=..%2FA.dc.html&tpl=1&was=img:zzzz", nil)
	w = httptest.NewRecorder()
	writeAnswer(w, a.DesignImage(r))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("not a board: %d", w.Code)
	}
}

// The marks of an edit travel as a base64 PNG: more than the dispatcher's 1mb is taken by this route, and a body over its
// own limit is refused with words the page shows.
func TestDesignImageBodyLimit(t *testing.T) {
	post := func(n int) *http.Request {
		body := `{"overlay":"` + strings.Repeat("A", n) + `"}`
		r := httptest.NewRequest(http.MethodPost, "/design/image", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	if _, refused := readBody(post(3<<20), imageBodyMax); refused != nil {
		t.Fatalf("3 MB of marks refused: %d", refused.Status)
	}
	if _, refused := readBody(post(3<<20), bodyLimit); refused == nil || refused.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("the dispatcher's limit stays 1 MB: %+v", refused)
	}
	if _, refused := readBody(post(imageBodyMax+10), imageBodyMax); refused == nil || refused.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body over the route's limit is refused: %+v", refused)
	}
}
