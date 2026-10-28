package design

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// deckRig is a library with the project "admin" (three pages) and an API whose model call is fake.
type deckRig struct {
	l    *Library
	a    *API
	root string
	// asked are the prompts the fake model got; answer is what it says.
	asked  []string
	answer string
	fail   error
}

func newDeckRig(t *testing.T) *deckRig {
	t.Helper()
	l, root := migratedLibrary(t)
	rig := &deckRig{l: l, root: root}
	rig.a = &API{L: l, Session: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false },
		OneShot: func(_ context.Context, p string) (string, error) {
			rig.asked = append(rig.asked, p)
			return rig.answer, rig.fail
		}}
	return rig
}

func (g *deckRig) call(t *testing.T, h func(*http.Request) Answer, method, target, body string) (int, map[string]any) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	code, v, _ := h(r).Split()
	raw, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return code, out
}

func (g *deckRig) pages(t *testing.T) []Page {
	t.Helper()
	return g.l.ReadModel("admin").Pages
}

func TestDeckStateIsTheDeckAsOneRead(t *testing.T) {
	g := newDeckRig(t)
	pages := g.pages(t)
	code, st := g.call(t, g.a.Deck, "GET", "/x?artifact=admin", "")
	if code != 200 {
		t.Fatalf("%d %v", code, st)
	}
	slides, _ := st["slides"].([]any)
	if len(slides) != len(pages) {
		t.Fatalf("%d slides for %d pages", len(slides), len(pages))
	}
	first := slides[0].(map[string]any)
	if first["id"] != pages[0].ID || first["notes"] != "" || first["voice"] != false || first["seconds"] != float64(10) || first["w"] == nil {
		t.Fatalf("slide: %v", first)
	}
	set := st["settings"].(map[string]any)
	if set["minutes"] != float64(10) || set["tone"] != "talk" || set["script"] != true || set["theme"] != "co" || set["slides"] != nil {
		t.Fatalf("defaults: %v", set)
	}
	for _, k := range []string{"rev", "chrome", "ffmpeg", "comments", "guestResolved", "recordings", "published"} {
		if _, ok := st[k]; !ok {
			t.Errorf("no %s", k)
		}
	}
	if c, _ := st["comments"].([]any); c == nil {
		t.Error("comments are [] before there is one, not null")
	}
	if code, _ := g.call(t, g.a.Deck, "GET", "/x?artifact=nope", ""); code != 404 && code != 400 {
		t.Errorf("no such design: %d", code)
	}
	if code, _ := g.call(t, g.a.Deck, "POST", "/x?artifact=admin", `{"artifact":"admin"}`); code != 405 {
		t.Errorf("POST: %d", code)
	}
}

func TestDeckRevMovesWithEveryDeckFile(t *testing.T) {
	g := newDeckRig(t)
	rev := func() int64 { return g.l.deckRev("admin") }
	r0 := rev()
	past := time.Now().Add(-time.Hour)
	old := func(path string) {
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}
	}
	page := g.pages(t)[0].ID
	if _, err := g.l.SetDeckNotes("admin", page, "one two three"); err != nil {
		t.Fatal(err)
	}
	old(filepath.Join(g.root, "admin", DeckNotesDir, page+".md"))
	old(filepath.Join(g.root, "admin", DeckNotesDir))
	r1 := rev()
	if _, err := g.l.AddDeckComment("admin", DeckCommentSpec{Page: page, X: 1, Y: 1, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if r2 := rev(); r2 < r1 || r2 <= r0 {
		t.Fatalf("rev %d %d %d", r0, r1, r2)
	}
}

func TestDeckSettingsAreMergedValidatedAndWrittenAsTheThemeCSS(t *testing.T) {
	g := newDeckRig(t)
	put := func(body string) (int, map[string]any) {
		return g.call(t, g.a.DeckSettings, "PUT", "/x", `{"artifact":"admin","settings":`+body+`}`)
	}
	code, out := put(`{"minutes":15,"slides":8,"tone":"biz","font":"serif","theme":"vi"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	s, err := g.l.ReadDeckSettings("admin")
	if err != nil || s.Minutes != 15 || *s.Slides != 8 || s.Tone != DeckToneBiz || s.Font != "serif" || s.Theme != "vi" || !s.Script {
		t.Fatalf("%+v %v", s, err)
	}
	css := readFileT(t, filepath.Join(g.root, "admin", ProjectDirname, "deck-theme.css"))
	for _, want := range []string{":root{", "--deck-acc:#7b61ff;", "--deck-bg:#fbfaff;", `--deck-font:Georgia, "PT Serif", serif;`} {
		if !strings.Contains(css, want) {
			t.Errorf("css has no %q: %s", want, css)
		}
	}
	// A key left out stays; slides: null is "pick yourself".
	if code, _ := put(`{"slides":null}`); code != 200 {
		t.Fatal(code)
	}
	if s, _ := g.l.ReadDeckSettings("admin"); s.Slides != nil || s.Minutes != 15 {
		t.Fatalf("%+v", s)
	}
	// The same settings are the same bytes: nothing to carry for a folder sync.
	before, _ := os.Stat(filepath.Join(g.root, "admin", DeckSettingsFile))
	put(`{"slides":null}`)
	after, _ := os.Stat(filepath.Join(g.root, "admin", DeckSettingsFile))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged settings file was rewritten")
	}
	for name, body := range map[string]string{
		"minutes -1":    `{"minutes":-1}`,
		"minutes 181":   `{"minutes":181}`,
		"slides 1":      `{"slides":1}`,
		"slides 41":     `{"slides":41}`,
		"tone":          `{"tone":"loud"}`,
		"font":          `{"font":"comic"}`,
		"theme":         `{"theme":"nope"}`,
		"unknown":       `{"colour":"red"}`,
		"wrong type":    `{"minutes":"ten"}`,
		"var injection": `{"themes":[{"id":"x","name":"X","vars":{"--deck-bg":"red;}body{display:none"}}],"theme":"x"}`,
		"var url":       `{"themes":[{"id":"x","name":"X","vars":{"--deck-bg":"url(http://example.org/a)"}}],"theme":"x"}`,
		"var name":      `{"themes":[{"id":"x","name":"X","vars":{"color":"#fff"}}],"theme":"x"}`,
		"duplicate id":  `{"themes":[{"id":"x","name":"X","vars":{}},{"id":"x","name":"Y","vars":{}}],"theme":"x"}`,
		"theme no name": `{"themes":[{"id":"x","name":"","vars":{}}],"theme":"x"}`,
		"too many vars": `{"themes":[{"id":"x","name":"X","vars":{` + manyVars(33) + `}}],"theme":"x"}`,
	} {
		if code, _ := put(body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if s, _ := g.l.ReadDeckSettings("admin"); s.Minutes != 15 || s.Theme != "vi" {
		t.Errorf("a refused settings changed the file: %+v", s)
	}
	if code, _ := g.call(t, g.a.DeckSettings, "PUT", "/x", `{"artifact":"admin"}`); code != 400 {
		t.Errorf("no settings: %d", code)
	}
}

func manyVars(n int) string {
	var parts []string
	for i := 0; i < n; i++ {
		parts = append(parts, `"--deck-v`+strings.Repeat("a", i%9)+string(rune('a'+i/9))+`":"#fff"`)
	}
	return strings.Join(parts, ",")
}

func TestDeckNotesAreOneFilePerSlideAndTheDictationGoesWithThem(t *testing.T) {
	g := newDeckRig(t)
	page := g.pages(t)[1].ID
	words := strings.TrimSpace(strings.Repeat("слово ", 220))
	code, out := g.call(t, g.a.DeckNotes, "PUT", "/x", `{"artifact":"admin","page":"`+page+`","text":"`+words+`\n"}`)
	slide, _ := out["slide"].(map[string]any)
	if code != 200 || slide["seconds"] != float64(100) || slide["voice"] != false {
		t.Fatalf("%d %v", code, out)
	}
	file := filepath.Join(g.root, "admin", DeckNotesDir, page+".md")
	if readFileT(t, file) != words+"\n" {
		t.Fatalf("file: %q", readFileT(t, file))
	}
	// A tidied dictation leaves the raw text beside the notes; a hand edit drops it.
	if _, err := g.l.SetDeckNotesFromDictation("admin", page, "Чистый текст.", "ну короче чистый текст"); err != nil {
		t.Fatal(err)
	}
	if s := g.l.DeckSlides("admin")[1]; !s.Voice || s.Notes != "Чистый текст." || s.Seconds != 10 {
		t.Fatalf("%+v", s)
	}
	if readFileT(t, filepath.Join(g.root, "admin", DeckNotesDir, page+".raw.md")) != "ну короче чистый текст\n" {
		t.Fatal("no raw file")
	}
	if code, out := g.call(t, g.a.DeckNotes, "PUT", "/x", `{"artifact":"admin","page":"`+page+`","text":"Правка"}`); code != 200 || out["slide"].(map[string]any)["voice"] != false {
		t.Fatalf("%d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(g.root, "admin", DeckNotesDir, page+".raw.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the dictation stays after a hand edit")
	}
	// Empty text takes the file away; the slide can be named by its title.
	title := g.pages(t)[1].Title
	if code, _ := g.call(t, g.a.DeckNotes, "PUT", "/x", `{"artifact":"admin","page":"`+title+`","text":"  "}`); code != 200 {
		t.Fatal(code)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty notes left a file")
	}
	for name, body := range map[string]string{"no page": `{"artifact":"admin","text":"x"}`, "no text": `{"artifact":"admin","page":"` + page + `"}`} {
		if code, _ := g.call(t, g.a.DeckNotes, "PUT", "/x", body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code, _ := g.call(t, g.a.DeckNotes, "PUT", "/x", `{"artifact":"admin","page":"01ARZ3NDEKTSV4RRFFQ69G5FAV","text":"x"}`); code != 404 {
		t.Errorf("no such slide: %d", code)
	}
	if code, _ := g.call(t, g.a.DeckNotes, "PUT", "/x", `{"artifact":"admin","page":"`+page+`","text":"`+strings.Repeat("я", maxDeckNotesRunes+1)+`"}`); code != 400 {
		t.Errorf("a huge note: %d", code)
	}
}

func TestDeckNotesSecondsFollowTheBoardsFormula(t *testing.T) {
	for words, want := range map[int]int{0: 10, 5: 10, 27: 10, 28: 15, 110: 50, 220: 100, 1000: 455} {
		if got := DeckNotesSeconds(words); got != want {
			t.Errorf("%d words: %d, want %d", words, got, want)
		}
	}
	if DeckWords("  one  two\nthree\t") != 3 || DeckWords("") != 0 {
		t.Error("words")
	}
}

func TestDeckCommentsAreOneFileEachAndGuestOnesKeepOnlyTheirFlag(t *testing.T) {
	g := newDeckRig(t)
	page := g.pages(t)[0].ID
	post := func(body string) (int, map[string]any) {
		return g.call(t, g.a.DeckComments, "POST", "/x", `{"artifact":"admin",`+body+`}`)
	}
	code, c1 := post(`"page":"` + page + `","x":74.123,"y":30,"label":"Схема","text":"Схему бы покрупнее"`)
	if code != 200 || c1["resolved"] != false || c1["x"] != 74.12 || c1["page"] != page || c1["label"] != "Схема" || !IsPageID(c1["id"].(string)) {
		t.Fatalf("%d %v", code, c1)
	}
	by := c1["by"].(map[string]any)
	if by["name"] == "" || by["name"] == nil {
		t.Fatalf("by %v", by)
	}
	_, c2 := post(`"page":"` + page + `","x":1,"y":2,"label":"","text":"Второй"`)
	dir := filepath.Join(g.root, "admin", DeckCommentsDir)
	names, _ := os.ReadDir(dir)
	if len(names) != 2 {
		t.Fatalf("%d files", len(names))
	}
	var onDisk DeckComment
	if err := json.Unmarshal([]byte(readFileT(t, filepath.Join(dir, c1["id"].(string)+".json"))), &onDisk); err != nil || onDisk.Text != "Схему бы покрупнее" || onDisk.ResolvedBy != nil {
		t.Fatalf("%+v %v", onDisk, err)
	}
	patch := func(body string) (int, map[string]any) {
		return g.call(t, g.a.DeckComments, "PATCH", "/x", `{"artifact":"admin",`+body+`}`)
	}
	code, res := patch(`"id":"` + c1["id"].(string) + `","resolved":true`)
	if code != 200 || res["resolved"] != true || res["resolvedBy"] == nil || res["resolvedAt"] == nil || res["text"] != "Схему бы покрупнее" {
		t.Fatalf("%d %v", code, res)
	}
	if _, st := g.call(t, g.a.Deck, "GET", "/x?artifact=admin", ""); len(st["comments"].([]any)) != 2 || st["comments"].([]any)[0].(map[string]any)["id"] != c1["id"] {
		t.Fatalf("comments %v", st["comments"])
	}
	code, res = patch(`"id":"` + c1["id"].(string) + `","resolved":false`)
	if code != 200 || res["resolved"] != false || res["resolvedBy"] != nil {
		t.Fatalf("reopen %d %v", code, res)
	}
	// A guest comment is the gateway's: only the flag is kept here, as g-<id>.
	code, res = patch(`"id":"g-12","resolved":true`)
	if code != 200 || res["id"] != "g-12" || res["resolved"] != true {
		t.Fatalf("%d %v", code, res)
	}
	if _, err := os.Stat(filepath.Join(dir, "g-12.json")); err != nil {
		t.Fatal(err)
	}
	if _, st := g.call(t, g.a.Deck, "GET", "/x?artifact=admin", ""); len(st["comments"].([]any)) != 2 || len(st["guestResolved"].([]any)) != 1 || st["guestResolved"].([]any)[0] != "g-12" {
		t.Fatalf("state %v %v", st["comments"], st["guestResolved"])
	}
	patch(`"id":"g-12","resolved":false`)
	if _, st := g.call(t, g.a.Deck, "GET", "/x?artifact=admin", ""); len(st["guestResolved"].([]any)) != 0 {
		t.Fatal("a reopened guest comment stays resolved")
	}
	_ = c2
	for name, body := range map[string]string{
		"empty text": `"page":"` + page + `","x":1,"y":1,"text":"  "`,
		"x over":     `"page":"` + page + `","x":101,"y":1,"text":"a"`,
		"y under":    `"page":"` + page + `","x":1,"y":-1,"text":"a"`,
		"no x":       `"page":"` + page + `","y":1,"text":"a"`,
		"no page":    `"x":1,"y":1,"text":"a"`,
		"long":       `"page":"` + page + `","x":1,"y":1,"text":"` + strings.Repeat("a", 2001) + `"`,
	} {
		if code, _ := post(body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code, _ := post(`"page":"01ARZ3NDEKTSV4RRFFQ69G5FAV","x":1,"y":1,"text":"a"`); code != 404 {
		t.Errorf("no such slide: %d", code)
	}
	for name, body := range map[string]string{"no id": `"resolved":true`, "no flag": `"id":"g-1"`, "bad id": `"id":"../x","resolved":true`} {
		if code, _ := patch(body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code, _ := patch(`"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","resolved":true`); code != 404 {
		t.Errorf("no such comment: %d", code)
	}
}

func TestDeckRoutesAskForTheRightRole(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Deck", "")
	l := accessLibrary(t, root)
	l.Migrate(false)
	page := l.ReadModel("p1").Pages[0].ID
	a := &API{L: l, Session: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false },
		OneShot:    func(context.Context, string) (string, error) { return "text", nil }}
	g := &deckRig{l: l, a: a}
	set := func(level string) {
		t.Helper()
		if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{"p1": level})); err != nil {
			t.Fatal(err)
		}
	}
	type try struct {
		name   string
		h      func(*http.Request) Answer
		method string
		body   string
	}
	tries := []try{
		{"deck", a.Deck, "GET", ""},
		{"settings", a.DeckSettings, "PUT", `{"artifact":"p1","settings":{"minutes":5}}`},
		{"notes", a.DeckNotes, "PUT", `{"artifact":"p1","page":"` + page + `","text":"x"}`},
		{"tidy", a.DeckTidy, "POST", `{"artifact":"p1","page":"` + page + `","raw":"x"}`},
		{"comment", a.DeckComments, "POST", `{"artifact":"p1","page":"` + page + `","x":1,"y":1,"text":"x"}`},
	}
	want := map[string][]int{ // none, view, comment, edit
		"deck":     {404, 200, 200, 200},
		"settings": {404, 403, 403, 200},
		"notes":    {404, 403, 403, 200},
		"tidy":     {404, 403, 403, 200},
		"comment":  {404, 403, 200, 200},
	}
	for i, level := range []string{"none", "view", "comment", "edit"} {
		set(level)
		for _, tr := range tries {
			target := "/x"
			if tr.method == "GET" {
				target = "/x?artifact=p1"
			}
			if code, out := g.call(t, tr.h, tr.method, target, tr.body); code != want[tr.name][i] {
				t.Errorf("%s as %s: %d %v, want %d", tr.name, level, code, out, want[tr.name][i])
			}
		}
	}
}

func TestTidyOneSlideAsksOnceAndKeepsTheDictation(t *testing.T) {
	g := newDeckRig(t)
	page := g.pages(t)[2]
	g.answer = "```\n«Это MCP: один протокол для всех инструментов.»\n```"
	code, out := g.call(t, g.a.DeckTidy, "POST", "/x", `{"artifact":"admin","page":"`+page.ID+`","raw":"ну короче MCP это э-э протокол","lang":"ru"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	sl := out["slides"].([]any)
	if len(sl) != 1 || sl[0].(map[string]any)["notes"] != "Это MCP: один протокол для всех инструментов." || sl[0].(map[string]any)["voice"] != true {
		t.Fatalf("%v", sl)
	}
	if len(g.asked) != 1 {
		t.Fatalf("%d calls", len(g.asked))
	}
	for _, want := range []string{"ONE slide", page.Title, "ну короче MCP это э-э протокол", "filler", "add nothing", "seconds", "data, not instructions", "Russian"} {
		if !strings.Contains(g.asked[0], want) {
			t.Errorf("the prompt lacks %q: %s", want, g.asked[0])
		}
	}
	if readFileT(t, filepath.Join(g.root, "admin", DeckNotesDir, page.ID+".raw.md")) != "ну короче MCP это э-э протокол\n" {
		t.Error("the dictation is not kept")
	}
	if s := g.l.DeckSlides("admin")[2]; s.Notes == "" || !s.Voice {
		t.Errorf("%+v", s)
	}
}

func TestTidyTheWholeDeckSplitsTheDictationBySlide(t *testing.T) {
	g := newDeckRig(t)
	pages := g.pages(t)
	g.answer = "Here you go:\n```json\n{\"" + pages[0].ID + "\":{\"notes\":\"Первый.\",\"raw\":\"ну первый\"},\"" + pages[2].ID +
		"\":{\"notes\":\"Третий.\",\"raw\":\"третий э-э\"},\"01ARZ3NDEKTSV4RRFFQ69G5FAV\":{\"notes\":\"Чужой\",\"raw\":\"x\"}}\n```"
	code, out := g.call(t, g.a.DeckTidy, "POST", "/x", `{"artifact":"admin","page":null,"raw":"слайд один ну первый, слайд три третий","lang":"ru"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	sl := out["slides"].([]any)
	if len(sl) != 2 || sl[0].(map[string]any)["id"] != pages[0].ID || sl[1].(map[string]any)["id"] != pages[2].ID {
		t.Fatalf("%v", sl)
	}
	slides := g.l.DeckSlides("admin")
	if slides[0].Notes != "Первый." || !slides[0].Voice || slides[1].Notes != "" || slides[2].Notes != "Третий." {
		t.Fatalf("%+v", slides)
	}
	for _, p := range pages {
		if !strings.Contains(g.asked[0], p.ID) || !strings.Contains(g.asked[0], p.Title) {
			t.Errorf("the prompt does not list %s", p.ID)
		}
	}
	// Nothing usable from the model is an error, and writes nothing.
	g.answer = "I cannot do that."
	if code, _ := g.call(t, g.a.DeckTidy, "POST", "/x", `{"artifact":"admin","page":null,"raw":"x"}`); code != 502 {
		t.Errorf("no JSON: %d", code)
	}
	g.answer, g.fail = "", errors.New("boom")
	if code, out := g.call(t, g.a.DeckTidy, "POST", "/x", `{"artifact":"admin","page":"`+pages[0].ID+`","raw":"x"}`); code != 502 || !strings.Contains(out["error"].(string), "boom") {
		t.Errorf("model error: %d %v", code, out)
	}
	g.fail, g.answer = nil, "  "
	if code, _ := g.call(t, g.a.DeckTidy, "POST", "/x", `{"artifact":"admin","page":"`+pages[0].ID+`","raw":"x"}`); code != 502 {
		t.Errorf("empty answer: %d", code)
	}
	for name, body := range map[string]string{"empty raw": `{"artifact":"admin","page":null,"raw":" "}`, "bad page": `{"artifact":"admin","page":5,"raw":"x"}`} {
		if code, _ := g.call(t, g.a.DeckTidy, "POST", "/x", body); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	g.a.OneShot = nil
	if code, _ := g.call(t, g.a.DeckTidy, "POST", "/x", `{"artifact":"admin","page":null,"raw":"x"}`); code != 503 {
		t.Errorf("no model wired: %d", code)
	}
}

func TestInitDeckMakesThemeFromATemplateAndRefusesWhatIsNotAColour(t *testing.T) {
	g := newDeckRig(t)
	slides := 6
	in := DeckInit{Minutes: 20, Slides: &slides, Tone: DeckToneShort, Script: false, Source: "Шаблон компании 2026.pptx", Layouts: 9,
		Vars: map[string]string{"--deck-acc": "#e4002b", "--deck-ink": "#101010"}}
	if err := ValidateDeckSettings(BuildDeckSettings(in)); err != nil {
		t.Fatal(err)
	}
	if err := g.l.InitDeck("admin", in); err != nil {
		t.Fatal(err)
	}
	s, _ := g.l.ReadDeckSettings("admin")
	if s.Minutes != 20 || *s.Slides != 6 || s.Script || s.Style.Source != "Шаблон компании 2026.pptx" || s.Style.Layouts != 9 || s.Theme != "tpl" || len(s.Themes) != 4 {
		t.Fatalf("%+v", s)
	}
	css := readFileT(t, filepath.Join(g.root, "admin", ProjectDirname, "deck-theme.css"))
	if !strings.Contains(css, "--deck-acc:#e4002b;") || !strings.Contains(css, "--deck-panel:#f4f4f6;") {
		t.Errorf("css: %s", css)
	}
	in.Vars = map[string]string{"--deck-acc": "red;}x{"}
	if err := ValidateDeckSettings(BuildDeckSettings(in)); err == nil {
		t.Error("a variable that is not a plain value")
	}
	// No template: the built-in company theme.
	if s := BuildDeckSettings(DeckInit{Minutes: 10, Tone: DeckToneTalk, Script: true}); s.Theme != "co" || s.Style != nil || len(s.Themes) != 0 {
		t.Errorf("%+v", s)
	}
}

func pptxFixture(t *testing.T, theme string, layouts int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	add("[Content_Types].xml", "<Types/>")
	if theme != "" {
		add("ppt/theme/theme1.xml", theme)
	}
	for i := 1; i <= layouts; i++ {
		add("ppt/slideLayouts/slideLayout"+string(rune('0'+i))+".xml", "<l/>")
		add("ppt/slideLayouts/_rels/slideLayout"+string(rune('0'+i))+".xml.rels", "<r/>")
	}
	add("ppt/slideMasters/slideMaster1.xml", "<m/>")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const pptxThemeXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Acme Corporate">
<a:themeElements><a:clrScheme name="Acme">
<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>
<a:dk2><a:srgbClr val="1F2A44"/></a:dk2><a:lt2><a:srgbClr val="EEECE1"/></a:lt2>
<a:accent1><a:srgbClr val="E4002B"/></a:accent1><a:accent2><a:srgbClr val="FFB81C"/></a:accent2><a:accent3><a:srgbClr val="00A3E0"/></a:accent3>
<a:accent4><a:srgbClr val="7A7A7A"/></a:accent4><a:accent5><a:srgbClr val="2E8B57"/></a:accent5><a:accent6><a:srgbClr val="8A2BE2"/></a:accent6>
</a:clrScheme><a:fontScheme name="Acme"><a:majorFont><a:latin typeface="Montserrat"/></a:majorFont><a:minorFont><a:latin typeface="Open Sans"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`

func TestPptxStyleReadsColoursFontsAndTheCountOfLayouts(t *testing.T) {
	st, err := ReadPptxStyle(pptxFixture(t, pptxThemeXML, 7), "Шаблон компании 2026.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "Шаблон компании 2026.pptx" || st.Layouts != 7 || st.Fonts.Head != "Montserrat" || st.Fonts.Body != "Open Sans" ||
		strings.Join(st.Colors, ",") != "#e4002b,#ffb81c,#00a3e0" {
		t.Fatalf("%+v", st)
	}
	if st.Vars["--deck-bg"] != "#ffffff" || st.Vars["--deck-ink"] != "#000000" || st.Vars["--deck-acc"] != "#e4002b" {
		t.Fatalf("%v", st.Vars)
	}
	if st.Vars["--deck-on-acc"] != "#ffffff" {
		t.Errorf("text on a red accent is white: %v", st.Vars)
	}
	if len(st.Vars) != 9 {
		t.Errorf("%d vars", len(st.Vars))
	}
	if err := validateDeckVars(st.Vars); err != nil {
		t.Errorf("the template's own variables are refused: %v", err)
	}
	// With no file name the theme's own name labels it.
	if st, _ := ReadPptxStyle(pptxFixture(t, pptxThemeXML, 1), ""); st.Name != "Acme Corporate" {
		t.Errorf("name %q", st.Name)
	}
	// A pale accent on a white page still reads as text.
	pale := strings.Replace(pptxThemeXML, "E4002B", "FFDD2D", 1)
	st, _ = ReadPptxStyle(pptxFixture(t, pale, 1), "y.pptx")
	if contrast(parseHex(st.Vars["--deck-acc-ink"]), parseHex(st.Vars["--deck-bg"])) < 4.5 {
		t.Errorf("accent ink %s on %s", st.Vars["--deck-acc-ink"], st.Vars["--deck-bg"])
	}
	for name, data := range map[string][]byte{"text": []byte("hello"), "no theme": pptxFixture(t, "", 2), "bad xml": pptxFixture(t, "<a:theme", 2), "empty": nil} {
		if _, err := ReadPptxStyle(data, "x.pptx"); !errors.Is(err, ErrNotPptx) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPptxStyleRoute(t *testing.T) {
	g := newDeckRig(t)
	post := func(body []byte) (int, string) {
		r := httptest.NewRequest("POST", "/x", bytes.NewReader(body))
		r.Header.Set("X-Design-Filename", "%D0%A8%D0%B0%D0%B1%D0%BB%D0%BE%D0%BD.pptx")
		w := httptest.NewRecorder()
		g.a.DeckPptxStyle(w, r)
		return w.Code, w.Body.String()
	}
	code, body := post(pptxFixture(t, pptxThemeXML, 3))
	var st DeckPptxStyle
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil || st.Layouts != 3 || st.Name != "Шаблон.pptx" || len(st.Colors) != 3 {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := post([]byte("not a zip")); code != 422 {
		t.Errorf("not a pptx: %d", code)
	}
	r := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	g.a.DeckPptxStyle(w, r)
	if w.Code != 405 {
		t.Errorf("GET: %d", w.Code)
	}
	g.a.Session = func(*http.Request) bool { return false }
	if code, _ := post(pptxFixture(t, pptxThemeXML, 1)); code != 403 {
		t.Errorf("no session: %d", code)
	}
}

func TestCLINotesReadsAndSetsASlidesNotes(t *testing.T) {
	g := newDeckRig(t)
	g.a.CLIToken = func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" }
	page := g.pages(t)[0]
	call := func(method, target, body string) (int, map[string]any) {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, target, nil)
		} else {
			r = httptest.NewRequest(method, target, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("x-cli-token", "ok")
		code, v, _ := g.a.CLINotes(r).Split()
		raw, _ := json.Marshal(v)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return code, out
	}
	if code, out := call("POST", "/api/cli/design/notes", `{"project":"admin","page":"`+page.Title+`","text":"Привет, мир"}`); code != 200 || out["slide"].(map[string]any)["notes"] != "Привет, мир" {
		t.Fatalf("%d %v", code, out)
	}
	if readFileT(t, filepath.Join(g.root, "admin", DeckNotesDir, page.ID+".md")) != "Привет, мир\n" {
		t.Fatal("not written")
	}
	code, out := call("GET", "/api/cli/design/notes?project=admin", "")
	if code != 200 || len(out["slides"].([]any)) != len(g.pages(t)) {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := call("GET", "/api/cli/design/notes?project=admin&page="+page.ID, ""); code != 200 || len(out["slides"].([]any)) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := call("POST", "/api/cli/design/notes", `{"project":"admin","page":"Nope","text":"x"}`); code != 404 {
		t.Errorf("no such slide: %d", code)
	}
	r := httptest.NewRequest("GET", "/api/cli/design/notes?project=admin", nil)
	if code, _, _ := g.a.CLINotes(r).Split(); code != 403 {
		t.Errorf("no token: %d", code)
	}
}

// An older slides project has no meta/deck.json: the deck reads with the defaults, and the first settings write makes it.
func TestAnOlderDeckReadsWithoutItsSettingsFileAndTheFirstWriteMakesIt(t *testing.T) {
	g := newDeckRig(t)
	file := filepath.Join(g.root, "admin", DeckSettingsFile)
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the fixture has a settings file")
	}
	if code, st := g.call(t, g.a.Deck, "GET", "/x?artifact=admin", ""); code != 200 || st["settings"].(map[string]any)["minutes"] != float64(10) {
		t.Fatalf("%d %v", code, st)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a read made the file")
	}
	if code, _ := g.call(t, g.a.DeckSettings, "PUT", "/x", `{"artifact":"admin","settings":{"minutes":12}}`); code != 200 {
		t.Fatal(code)
	}
	if !strings.Contains(readFileT(t, file), `"minutes": 12`) {
		t.Fatal("no file")
	}
}

// A length nobody asked for is 0: the settings stay valid and the agent decides.
func TestDeckSettingsMayLeaveTheLengthToTheAgent(t *testing.T) {
	s := BuildDeckSettings(DeckInit{Tone: DeckToneTalk, Script: true})
	if s.Minutes != 0 || s.Slides != nil {
		t.Fatalf("%+v", s)
	}
	if err := ValidateDeckSettings(s); err != nil {
		t.Fatal(err)
	}
	if seconds, _ := tidyBudget(s, 5); seconds != 120 {
		t.Fatalf("a slide of an open deck gets %d s", seconds)
	}
}
