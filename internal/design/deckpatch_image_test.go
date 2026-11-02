package design

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/imagegen"
)

func imagePatch(t *testing.T, tpl string, idx int, req DeckPatchRequest) (string, error) {
	t.Helper()
	src := deckBoardOf(tpl)
	req.Op = DeckPatchImage
	req.Tpl = idx
	if req.Was == nil {
		was := deckFingerprint(mustElem(t, src, idx))
		req.Was = &was
	}
	return applyDeckPatch(src, req, 1920)
}

func TestDeckPatchImage(t *testing.T) {
	ok := DeckPatchRequest{Src: "assets/gen-ab12cd34-2.png", Job: "gen-ab12cd34", Prompt: "Кот в {{ шляпе }} \"тут\""}

	got, err := imagePatch(t, `<div><img class="hero" src="old.png" srcset="a 1x"></div>`, 1, ok)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`src="assets/gen-ab12cd34-2.png"`, `data-gen-job="gen-ab12cd34"`, `data-gen-prompt="Кот в { { шляпе } } &quot;тут&quot;"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("img: missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "srcset") || strings.Contains(got, "old.png") {
		t.Fatalf("img keeps the old picture:\n%s", got)
	}

	got, err = imagePatch(t, `<main><section style="color: red">Привет</section></main>`, 1, ok)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`color: red; background-image: url(assets/gen-ab12cd34-2.png); background-position: center; background-repeat: no-repeat; background-size: cover`, `data-gen-job="gen-ab12cd34"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("block: missing %s in\n%s", want, got)
		}
	}
	// Drawn again: the same element, now with its new fingerprint, is replaced, not doubled.
	again := ok
	again.Src, again.Job = "assets/gen-ffff0000-1.png", "gen-ffff0000"
	was := deckFingerprint(mustElem(t, got, 1))
	again.Was, again.Op, again.Tpl = &was, DeckPatchImage, 1
	next, err := applyDeckPatch(got, again, 1920)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(next, "data-gen-job") != 1 || strings.Count(next, "background-image") != 1 || !strings.Contains(next, "gen-ffff0000-1.png") || strings.Contains(next, "gen-ab12cd34") {
		t.Fatalf("redraw:\n%s", next)
	}
	// A block with no style gets one.
	if got, err = imagePatch(t, `<div><p>x</p></div>`, 1, ok); err != nil || !strings.Contains(got, `<p style="background-image: url(assets/gen-ab12cd34-2.png)`) {
		t.Fatalf("no style: %v\n%s", err, got)
	}

	var pe *deckPatchErr
	stale := "div:zzzz"
	if _, err = imagePatch(t, `<div><img src="a.png"></div>`, 1, DeckPatchRequest{Src: ok.Src, Job: ok.Job, Prompt: "x", Was: &stale}); !asPatchErr(err, &pe) || pe.status != http.StatusConflict {
		t.Fatalf("stale fingerprint: %v", err)
	}
	for name, bad := range map[string]DeckPatchRequest{
		"escape":     {Src: "../secret.png", Job: ok.Job, Prompt: "x"},
		"elsewhere":  {Src: "assets/other.png", Job: ok.Job, Prompt: "x"},
		"other job":  {Src: "assets/gen-ab12cd34-1.png", Job: "gen-00000000", Prompt: "x"},
		"url":        {Src: "assets/gen-ab12cd34-1.png\") x(\"", Job: ok.Job, Prompt: "x"},
		"no prompt":  {Src: ok.Src, Job: ok.Job, Prompt: "  "},
		"nested dir": {Src: "assets/a/gen-ab12cd34-1.png", Job: ok.Job, Prompt: "x"},
	} {
		if _, err := imagePatch(t, `<div><img src="a.png"></div>`, 1, bad); !asPatchErr(err, &pe) || pe.status != http.StatusBadRequest {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err = imagePatch(t, `<div><img src="{{ pic }}"></div>`, 1, ok); !asPatchErr(err, &pe) || pe.code != DeckPatchDynamic {
		t.Fatalf("dynamic src: %v", err)
	}
	if _, err = imagePatch(t, `<div style="width: {{ w }}px"></div>`, 0, ok); !asPatchErr(err, &pe) || pe.code != DeckPatchDynamic {
		t.Fatalf("dynamic style: %v", err)
	}
}

func asPatchErr(err error, out **deckPatchErr) bool {
	pe, ok := err.(*deckPatchErr)
	if ok {
		*out = pe
	}
	return ok
}

func TestDesignImageRoute(t *testing.T) {
	l, library := historyLibrary(t)
	const project = "p1"
	dir := filepath.Join(library, project)
	if err := os.MkdirAll(filepath.Join(dir, ProjectDirname), 0o777); err != nil {
		t.Fatal(err)
	}
	a := &API{L: l, Session: func(*http.Request) bool { return true }, PlatformAPI: "/api/platform",
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	call := func(method, target string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, target, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		writeAnswer(w, a.DesignImage(r))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	start := map[string]any{"artifact": project, "prompt": "кот", "count": 2}
	if code, _ := call(http.MethodPost, "/api/platform/design/image", start); code != http.StatusServiceUnavailable {
		t.Fatalf("no service: %d", code)
	}
	a.Images = &imagegen.Service{Dir: t.TempDir(), CodexHome: t.TempDir(), Binary: "/nonexistent/codex"}
	if code, _ := call(http.MethodPost, "/api/platform/design/image", map[string]any{"artifact": project, "prompt": "кот", "count": 5}); code != http.StatusBadRequest {
		t.Fatalf("count: %d", code)
	}
	if code, _ := call(http.MethodPost, "/api/platform/design/image", map[string]any{"artifact": "../x", "prompt": "кот"}); code != http.StatusBadRequest {
		t.Fatalf("artifact: %d", code)
	}
	// assets/ is a link out of the project: nothing is drawn into it.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ProjectDirname, genAssetsDir)); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(http.MethodPost, "/api/platform/design/image", start); code != http.StatusConflict {
		t.Fatalf("link out: %d", code)
	}
	if code, _ := call(http.MethodGet, "/api/platform/design/image?artifact="+project+"&job=gen-ab12cd34", nil); code != http.StatusConflict {
		t.Fatalf("variants through a link: %d", code)
	}
	if err := os.Remove(filepath.Join(dir, ProjectDirname, genAssetsDir)); err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(dir, ProjectDirname, genAssetsDir)
	if err := os.MkdirAll(assets, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"gen-ab12cd34-2.png", "gen-ab12cd34-1.png", "gen-ab12cd34-1.txt", "gen-00000000-1.png", "logo.png"} {
		if err := os.WriteFile(filepath.Join(assets, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out := call(http.MethodGet, "/api/platform/design/image?artifact="+project+"&job=gen-ab12cd34", nil)
	images, _ := out["images"].([]any)
	if code != 200 || len(images) != 2 || images[0].(map[string]any)["src"] != "assets/gen-ab12cd34-1.png" || images[1].(map[string]any)["src"] != "assets/gen-ab12cd34-2.png" {
		t.Fatalf("variants: %d %v", code, out)
	}
	if code, _ := call(http.MethodGet, "/api/platform/design/image?artifact="+project+"&job=../x", nil); code != http.StatusBadRequest {
		t.Fatalf("bad job: %d", code)
	}
	// The picture itself: only a job's own file name, from assets/.
	file := func(name string) (int, string) {
		r := httptest.NewRequest(http.MethodGet, "/api/platform/design/image/file?artifact="+project+"&file="+name, nil)
		w := httptest.NewRecorder()
		a.DesignImageFile(w, r)
		return w.Code, w.Header().Get("Content-Type")
	}
	if code, ctype := file("gen-ab12cd34-1.png"); code != 200 || ctype != "image/png" {
		t.Fatalf("file: %d %s", code, ctype)
	}
	for _, name := range []string{"logo.png", "..%2Fgen-ab12cd34-1.png", "gen-ab12cd34-9.png"} {
		if code, _ := file(name); code != http.StatusBadRequest && code != http.StatusNotFound {
			t.Fatalf("%s: %d", name, code)
		}
	}
}

// A canvas board is named by its file: the image op lands there, and a path or a non-board is refused.
func TestDeckPatchImageByFile(t *testing.T) {
	l, library := historyLibrary(t)
	const project = "p1"
	dir := filepath.Join(library, project, ProjectDirname)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	src := deckBoardOf(`<main><img src="old.png"></main>`)
	if err := os.WriteFile(filepath.Join(dir, "Canvas.dc.html"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &API{L: l, Session: func(*http.Request) bool { return true }, PlatformAPI: "/api/platform",
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
	var answered map[string]any
	post := func(file string) int {
		body, _ := json.Marshal(map[string]any{"artifact": project, "file": file, "tpl": 1, "op": "image",
			"was": deckFingerprint(mustElem(t, src, 1)), "src": "assets/gen-ab12cd34-1.png", "job": "gen-ab12cd34", "prompt": "кот"})
		r := httptest.NewRequest(http.MethodPost, "/api/platform/design/deck/patch", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		writeAnswer(w, a.DeckPatch(r))
		answered = nil
		_ = json.Unmarshal(w.Body.Bytes(), &answered)
		return w.Code
	}
	if code := post("Canvas.dc.html"); code != 200 {
		t.Fatalf("by file: %d", code)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "Canvas.dc.html"))
	if !strings.Contains(string(got), `src="assets/gen-ab12cd34-1.png"`) {
		t.Fatalf("board:\n%s", got)
	}
	// The answer carries the element's fingerprint as it is now, for the next edit of it.
	if want := deckFingerprint(mustElem(t, string(got), 1)); answered["was"] != want {
		t.Fatalf("was: %v, want %s", answered["was"], want)
	}
	for _, file := range []string{"../Canvas.dc.html", "sub/Canvas.dc.html", "Canvas.html", ""} {
		if code := post(file); code != 400 && code != 404 {
			t.Fatalf("%q: %d", file, code)
		}
	}
}
