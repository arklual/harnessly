package design

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// videoRig is deckRig's "admin" made a video of three scenes.
type videoRig struct {
	*deckRig
	pages []Page
}

const (
	videoBoardOne = `<main class="board" data-scene data-transition="fade">
<h1 data-block="Заголовок" data-build-in="rise" data-tk-at="0.4">Оплата за 2 клика</h1>
<div data-block="График" data-build-in="pop" data-tk-at="1.8" data-tk-out="7.5">chart</div></main>`
	videoBoardTwo   = `<main class="board"><p data-block="Текст">text</p></main>`
	videoBoardThree = `<main class="board"><p data-block="Итог" data-tk-at="9">end</p></main>`
)

func newVideoRig(t *testing.T) *videoRig {
	t.Helper()
	g := newDeckRig(t)
	if err := g.l.InitDeck("admin", DeckInit{Minutes: 10, Tone: DeckToneTalk, Script: true}); err != nil {
		t.Fatal(err)
	}
	if err := g.l.InitVideo("admin", VideoInit{Formats: []VideoFormat{VideoLandscape, VideoPortrait}, Subs: true}); err != nil {
		t.Fatal(err)
	}
	v := &videoRig{deckRig: g, pages: g.pages(t)}
	for i, src := range []string{videoBoardOne, videoBoardTwo, videoBoardThree} {
		writeFile(t, filepath.Join(g.root, "admin", ProjectDirname, pageFileOf(v.pages[i].File)), src)
	}
	return v
}

func (v *videoRig) state(t *testing.T) VideoState {
	t.Helper()
	st, err := v.l.VideoStateOf("admin")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func words(n int) string { return strings.TrimSpace(strings.Repeat("слово ", n)) }

func TestParseVideoBoard(t *testing.T) {
	src := `<!-- <b data-tk-at="9"> -->
<main class="board" data-transition="push">
 <h1 data-block="A" data-tk-at="1.25" data-build-in="rise" data-dc-tpl="3">a</h1>
 <p data-block="B>c" data-tk-at="0.5" data-tk-out="2">b</p>
 <p data-block="Late" data-step="2">s2</p>
 <p data-block="Early" data-step="1" data-build-in="zoom">s1</p>
 <p data-block="Fx" data-build-in="fade">fx</p>
 <p data-block="Plain">plain</p>
 <p data-tk-at="abc" data-tk-out="-1">bad</p>
 <p data-tk-at="3" data-tk-out="2">out before at</p>
</main>
<script>var s = '<i data-tk-at="7">';</script>`
	b := ParseVideoBoard(src)
	type row struct {
		block   string
		at      int64
		out     int64
		build   string
		derived bool
	}
	var got []row
	for _, m := range b.Moments {
		r := row{m.Block, m.At, -1, m.BuildIn, m.Derived}
		if m.Out != nil {
			r.out = *m.Out
		}
		got = append(got, r)
	}
	want := []row{
		{"B>c", 500, 2000, "", false},
		{"Early", 400, -1, "", true},
		{"Late", 1400, -1, "", true},
		{"A", 1250, -1, "rise", false},
		{"Fx", 2400, -1, "fade", true},
		{"", 3000, -1, "", false},
	}
	// Time order, ties in document order.
	order := []int{1, 0, 3, 2, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("%d moments: %+v", len(got), got)
	}
	for i, w := range order {
		if got[i] != want[w] {
			t.Errorf("moment %d: %+v, want %+v", i, got[i], want[w])
		}
	}
	if b.Moments[2].Tpl == nil || *b.Moments[2].Tpl != 3 {
		t.Errorf("tpl: %+v", b.Moments[2])
	}
	if b.Transition == nil || b.Transition.Kind != VideoTransitionFade || b.Transition.Ms != videoFadeMs {
		t.Errorf("push plays as fade: %+v", b.Transition)
	}
	if none := ParseVideoBoard(`<main data-transition="none"></main>`); none.Transition == nil || none.Transition.Kind != VideoTransitionNone {
		t.Errorf("none: %+v", none.Transition)
	}
	if empty := ParseVideoBoard(`<main class="board">x</main>`); empty.Transition != nil || empty.Moments == nil || len(empty.Moments) != 0 {
		t.Errorf("empty: %+v", empty)
	}
}

func TestVideoStateOfThreeScenes(t *testing.T) {
	v := newVideoRig(t)
	if _, err := v.l.SetDeckNotes("admin", v.pages[1].ID, words(20)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.l.SetVideoScene("admin", v.pages[2].ID, map[string]json.RawMessage{"dur": json.RawMessage("5000")}); err != nil {
		t.Fatal(err)
	}
	st := v.state(t)
	if len(st.Scenes) != 3 {
		t.Fatalf("%d scenes", len(st.Scenes))
	}
	one, two, three := st.Scenes[0], st.Scenes[1], st.Scenes[2]
	// Scene one: last moment ends at 7.5 s (data-tk-out) → 7500 + 2100.
	if one.Dur != 9600 || !one.Auto || one.Start != 0 || len(one.Moments) != 2 || one.Moments[0].Block != "Заголовок" {
		t.Errorf("one: %+v", one)
	}
	if one.Transition.Kind != VideoTransitionFade || one.Transition.Ms != videoFadeMs {
		t.Errorf("one transition: %+v", one.Transition)
	}
	// Scene two: 20 words / 2.5 = 8 s.
	if two.Dur != 8000 || !two.Auto || two.Start != 9600 || two.Words != 20 || two.Notes != words(20) || two.Transition.Kind != VideoTransitionNone {
		t.Errorf("two: %+v", two)
	}
	// Scene three: explicit duration, though 9 s + 2.1 s asks for more.
	if three.Dur != 5000 || three.Auto || three.Start != 17600 {
		t.Errorf("three: %+v", three)
	}
	if st.DurMs != 22600 {
		t.Errorf("dur %d", st.DurMs)
	}
	if st.Settings.Formats[1] != VideoPortrait || st.Deck.Theme != "co" || st.Rev == 0 {
		t.Errorf("settings %+v deck %+v rev %d", st.Settings, st.Deck, st.Rev)
	}
	if st.Comments == nil || st.GuestResolved == nil || st.Renders == nil || st.Scenes[0].Keys == nil {
		t.Errorf("nulls: %+v", st)
	}
	raw, _ := json.Marshal(st)
	for _, k := range []string{`"scenes":[`, `"comments":[]`, `"guestResolved":[]`, `"job":null`, `"moments":[`} {
		if !strings.Contains(string(raw), k) {
			t.Errorf("the state lacks %s", k)
		}
	}
}

func TestVideoSceneValidation(t *testing.T) {
	v := newVideoRig(t)
	page := v.pages[1].ID
	put := func(body string) (int, map[string]any) {
		return v.call(t, v.a.VideoScene, "PUT", "/x", `{"artifact":"admin","page":"`+page+`",`+body+`}`)
	}
	for body, want := range map[string]int{
		`"dur":999`: 400, `"dur":1000`: 200, `"dur":60000`: 200, `"dur":60001`: 400, `"dur":null`: 200, `"dur":1500.5`: 400,
		`"dur":"3000"`: 400, `"transition":{"kind":"fade","ms":2000}`: 200, `"transition":{"kind":"fade","ms":2001}`: 400,
		`"transition":{"kind":"fade","ms":-1}`: 400, `"transition":{"kind":"push","ms":300}`: 400, `"transition":null`: 200,
		`"nope":1`: 200,
	} {
		if code, out := put(body); code != want {
			t.Errorf("%s: %d %v, want %d", body, code, out, want)
		}
	}
	if code, _ := v.call(t, v.a.VideoScene, "PUT", "/x", `{"artifact":"admin","dur":3000}`); code != 400 {
		t.Errorf("no page: %d", code)
	}
	if code, _ := v.call(t, v.a.VideoScene, "PUT", "/x", `{"artifact":"admin","page":"01ARZ3NDEKTSV4RRFFQ69G5FAV","dur":3000}`); code != 404 {
		t.Errorf("no such scene: %d", code)
	}
	// A value, a null and a key left out.
	code, out := put(`"dur":4000,"transition":{"kind":"fade","ms":700}`)
	sc := out["scene"].(map[string]any)
	if code != 200 || sc["dur"] != float64(4000) || sc["auto"] != false || sc["transition"].(map[string]any)["ms"] != float64(700) {
		t.Fatalf("%d %v", code, out)
	}
	code, out = put(`"dur":null`)
	sc = out["scene"].(map[string]any)
	if code != 200 || sc["auto"] != true || sc["transition"].(map[string]any)["ms"] != float64(700) {
		t.Fatalf("dur null keeps the transition: %d %v", code, out)
	}
	if code, _ = put(`"transition":{"kind":"none","ms":900}`); code != 200 {
		t.Fatal(code)
	}
	meta, err := v.l.ReadVideoSceneMeta("admin", page)
	if err != nil || meta.Transition == nil || meta.Transition.Ms != 0 || meta.Dur != nil {
		t.Fatalf("none has no length: %+v %v", meta, err)
	}
	// A scene with nothing set has no file.
	if code, _ = put(`"transition":null`); code != 200 {
		t.Fatal(code)
	}
	if _, err := os.Stat(v.l.sceneMetaFile("admin", page)); !os.IsNotExist(err) {
		t.Errorf("the file stays: %v", err)
	}
	// By file and by title, as the CLI may.
	if sc, err := v.l.SetVideoScene("admin", v.pages[0].Title, map[string]json.RawMessage{"dur": json.RawMessage("2000")}); err != nil || sc.ID != v.pages[0].ID || sc.Dur != 2000 {
		t.Errorf("by title: %+v %v", sc, err)
	}
	// A broken file is an error, not a guess.
	writeFile(t, v.l.sceneMetaFile("admin", page), `{"v":1,"dur":5}`)
	if _, err := v.l.VideoStateOf("admin"); err == nil {
		t.Error("a scene file out of bounds reads")
	}
}

func TestVideoSettingsValidation(t *testing.T) {
	v := newVideoRig(t)
	put := func(s string) (int, map[string]any) {
		return v.call(t, v.a.VideoSettings, "PUT", "/x", `{"artifact":"admin","settings":`+s+`}`)
	}
	for body, want := range map[string]int{
		`{"formats":[]}`: 400, `{"formats":["4:3"]}`: 400, `{"formats":["16:9","16:9"]}`: 400, `{"formats":["16:9","9:16","16:9"]}`: 400,
		`{"formats":"16:9"}`: 400, `{"seconds":14}`: 400, `{"seconds":301}`: 400, `{"seconds":"30"}`: 400, `{"fps":60}`: 400,
		`{"voice":"tts"}`: 400, `{"subs":"yes"}`: 400, `{"seconds":15}`: 200, `{"seconds":300}`: 200, `{"seconds":null}`: 200,
		`{"formats":["9:16"]}`: 200, `{"formats":["9:16","16:9"]}`: 200, `{"subs":false}`: 200,
	} {
		if code, out := put(body); code != want {
			t.Errorf("%s: %d %v, want %d", body, code, out, want)
		}
	}
	if code, _ := v.call(t, v.a.VideoSettings, "PUT", "/x", `{"artifact":"admin"}`); code != 400 {
		t.Errorf("no settings: %d", code)
	}
	code, out := put(`{"formats":["9:16","16:9"],"seconds":60,"subs":true}`)
	s := out["settings"].(map[string]any)
	if code != 200 || s["formats"].([]any)[0] != "16:9" || s["seconds"] != float64(60) || s["subs"] != true || s["fps"] != float64(30) || s["voice"] != "none" {
		t.Fatalf("%d %v", code, out)
	}
	if got, err := v.l.ReadVideoSettings("admin"); err != nil || len(got.Formats) != 2 || got.Seconds == nil || *got.Seconds != 60 {
		t.Errorf("on disk: %+v %v", got, err)
	}
	// A refused patch changes nothing.
	put(`{"seconds":null}`)
	put(`{"formats":[],"seconds":99}`)
	if got, _ := v.l.ReadVideoSettings("admin"); got.Seconds != nil {
		t.Errorf("a refused patch wrote: %+v", got)
	}
}

func TestVideoRoutesAskForTheRightRole(t *testing.T) {
	root := t.TempDir()
	makeProject(t, root, "p1", "Video", "")
	l := accessLibrary(t, root)
	l.Migrate(false)
	if err := l.InitDeck("p1", DeckInit{Minutes: 10, Tone: DeckToneTalk, Script: true}); err != nil {
		t.Fatal(err)
	}
	if err := l.InitVideo("p1", VideoInit{}); err != nil {
		t.Fatal(err)
	}
	page := l.ReadModel("p1").Pages[0].ID
	a := &API{L: l, Session: func(*http.Request) bool { return true }, CLIToken: func(*http.Request) bool { return true },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false }}
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
		target string
		body   string
	}
	tries := []try{
		{"state", a.Video, "GET", "/x?artifact=p1", ""},
		{"settings", a.VideoSettings, "PUT", "/x", `{"artifact":"p1","settings":{"subs":false}}`},
		{"scene", a.VideoScene, "PUT", "/x", `{"artifact":"p1","page":"` + page + `","dur":3000}`},
		{"comment", a.VideoComments, "POST", "/x", `{"artifact":"p1","page":"` + page + `","t0":500,"t1":null,"format":"16:9","text":"x"}`},
		{"cli state", a.CLIVideo, "GET", "/x?project=p1&what=state", ""},
		{"cli comments", a.CLIVideo, "GET", "/x?project=p1&what=comments&open=1", ""},
		{"cli scene", a.CLIVideoScene, "POST", "/x", `{"project":"p1","page":"` + page + `","dur":3000}`},
	}
	want := map[string][]int{ // none, view, comment, edit
		"state":        {404, 200, 200, 200},
		"settings":     {404, 403, 403, 200},
		"scene":        {404, 403, 403, 200},
		"comment":      {404, 403, 200, 200},
		"cli state":    {404, 200, 200, 200},
		"cli comments": {404, 200, 200, 200},
		"cli scene":    {404, 403, 403, 200},
	}
	for i, level := range []string{"none", "view", "comment", "edit"} {
		set(level)
		for _, tr := range tries {
			if code, out := g.call(t, tr.h, tr.method, tr.target, tr.body); code != want[tr.name][i] {
				t.Errorf("%s as %s: %d %v, want %d", tr.name, level, code, out, want[tr.name][i])
			}
		}
	}
	// A route answers by method: a wrong one is 405.
	if code, _ := g.call(t, a.VideoSettings, "POST", "/x", `{"artifact":"p1","settings":{}}`); code != 405 {
		t.Errorf("POST settings: %d", code)
	}
	if code, _ := g.call(t, a.Video, "POST", "/x?artifact=p1", `{"artifact":"p1"}`); code != 405 {
		t.Errorf("POST state: %d", code)
	}
}

func TestVideoCommentBounds(t *testing.T) {
	v := newVideoRig(t)
	one := v.pages[0].ID // lasts 9600 ms
	if err := v.l.WriteVideoSettings("admin", VideoSettings{V: 1, Formats: []VideoFormat{VideoLandscape}, FPS: VideoFPS, Voice: VideoVoiceNone}); err != nil {
		t.Fatal(err)
	}
	post := func(body string) (int, map[string]any) {
		return v.call(t, v.a.VideoComments, "POST", "/x", `{"artifact":"admin","page":"`+one+`",`+body+`}`)
	}
	for name, c := range map[string]struct {
		body string
		want int
	}{
		"moment":         {`"t0":1800,"t1":null,"format":"16:9","text":"a"`, 200},
		"start":          {`"t0":0,"t1":0,"format":"16:9","text":"a"`, 200},
		"end":            {`"t0":9600,"t1":9600,"format":"16:9","text":"a"`, 200},
		"stretch":        {`"t0":1000,"t1":4000,"format":"16:9","text":"a"`, 200},
		"t0 negative":    {`"t0":-1,"t1":null,"format":"16:9","text":"a"`, 400},
		"t0 past end":    {`"t0":9601,"t1":null,"format":"16:9","text":"a"`, 400},
		"t1 before t0":   {`"t0":2000,"t1":1999,"format":"16:9","text":"a"`, 400},
		"t1 past end":    {`"t0":2000,"t1":9601,"format":"16:9","text":"a"`, 400},
		"t0 fractional":  {`"t0":10.5,"t1":null,"format":"16:9","text":"a"`, 400},
		"t1 text":        {`"t0":10,"t1":"x","format":"16:9","text":"a"`, 400},
		"no t0":          {`"t1":null,"format":"16:9","text":"a"`, 400},
		"format off":     {`"t0":10,"t1":null,"format":"9:16","text":"a"`, 400},
		"format unknown": {`"t0":10,"t1":null,"format":"4:3","text":"a"`, 400},
		"no format":      {`"t0":10,"t1":null,"text":"a"`, 400},
		"empty text":     {`"t0":10,"t1":null,"format":"16:9","text":" "`, 400},
		"x over":         {`"t0":10,"t1":null,"format":"16:9","x":101,"y":1,"text":"a"`, 400},
		"x text":         {`"t0":10,"t1":null,"format":"16:9","x":"a","y":1,"text":"a"`, 400},
	} {
		if code, out := post(c.body); code != c.want {
			t.Errorf("%s: %d %v, want %d", name, code, out, c.want)
		}
	}
	code, c := post(`"t0":1800,"t1":3000,"format":"16:9","x":null,"y":null,"block":"График","label":"График","text":"Покрупнее"`)
	if code != 200 || c["view"] != "video" || c["t0"] != float64(1800) || c["t1"] != float64(3000) || c["format"] != "16:9" ||
		c["block"] != "График" || c["x"] != float64(0) || c["y"] != float64(0) || c["page"] != one {
		t.Fatalf("%d %v", code, c)
	}
	_, c = post(`"t0":1800,"t1":null,"format":"16:9","x":12.5,"y":40,"text":"Точка"`)
	if c["t1"] != nil || c["x"] != float64(12.5) {
		t.Errorf("a moment: %v", c)
	}
	// It is in the state with its fields, and resolves like a deck's.
	st := v.state(t)
	if len(st.Comments) != 6 {
		t.Fatalf("%d comments", len(st.Comments))
	}
	last := st.Comments[len(st.Comments)-1]
	if last.View != DeckCommentVideo || last.T0 == nil || last.T1 != nil || last.Format != VideoLandscape {
		t.Errorf("%+v", last)
	}
	if code, out := v.call(t, v.a.VideoComments, "PATCH", "/x", `{"artifact":"admin","id":"`+last.ID+`","resolved":true}`); code != 200 || out["resolved"] != true || out["t0"] != float64(1800) {
		t.Errorf("resolve: %d %v", code, out)
	}
	if code, _ := v.call(t, v.a.VideoComments, "PATCH", "/x", `{"artifact":"admin","resolved":true}`); code != 400 {
		t.Errorf("patch with no id: %d", code)
	}
	// A comment follows its scene's length: a longer scene takes a later t0.
	if code, _ := post(`"t0":12000,"t1":null,"format":"16:9","text":"late"`); code != 400 {
		t.Error("12 s is past the scene")
	}
	if _, err := v.l.SetVideoScene("admin", one, map[string]json.RawMessage{"dur": json.RawMessage("15000")}); err != nil {
		t.Fatal(err)
	}
	if code, _ := post(`"t0":12000,"t1":null,"format":"16:9","text":"late"`); code != 200 {
		t.Error("12 s is within a 15 s scene")
	}
}

func TestVideoCommentOnADeletedScene(t *testing.T) {
	v := newVideoRig(t)
	gone := v.pages[2]
	if _, err := v.l.RemovePage("admin", gone.ID); err != nil {
		t.Fatal(err)
	}
	code, out := v.call(t, v.a.VideoComments, "POST", "/x", `{"artifact":"admin","page":"`+gone.ID+`","t0":10,"t1":null,"format":"16:9","text":"a"}`)
	if code != 404 {
		t.Errorf("%d %v", code, out)
	}
	if st := v.state(t); len(st.Scenes) != 2 {
		t.Errorf("%d scenes", len(st.Scenes))
	}
	// A comment made before the scene went stays in the state: the page shows it as detached.
}

func TestAComentOfADeckStaysAsItWas(t *testing.T) {
	g := newDeckRig(t) // no meta/video.json
	page := g.pages(t)[0].ID
	code, c := g.call(t, g.a.DeckComments, "POST", "/x", `{"artifact":"admin","page":"`+page+`","x":10,"y":20,"label":"Заголовок","text":"Крупнее"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, c)
	}
	for _, k := range []string{"view", "t0", "t1", "format", "block", "frame"} {
		if _, has := c[k]; has {
			t.Errorf("a deck's comment has %s", k)
		}
	}
	raw, err := os.ReadFile(filepath.Join(g.root, "admin", DeckCommentsDir, c["id"].(string)+".json"))
	if err != nil || strings.Contains(string(raw), `"view"`) || strings.Contains(string(raw), `"t0"`) {
		t.Errorf("on disk: %s %v", raw, err)
	}
	// A deck is not a video: the video's routes say so, and time on a deck's comment is refused.
	if g.l.IsVideo("admin") {
		t.Error("a deck with no video.json is a video")
	}
	if code, _ := g.call(t, g.a.Video, "GET", "/x?artifact=admin", ""); code != 404 {
		t.Errorf("video state of a deck: %d", code)
	}
	if code, _ := g.call(t, g.a.VideoComments, "POST", "/x", `{"artifact":"admin","page":"`+page+`","t0":10,"t1":null,"format":"16:9","text":"a"}`); code != 404 {
		t.Errorf("video comment on a deck: %d", code)
	}
	if _, err := g.l.AddDeckComment("admin", DeckCommentSpec{Page: page, X: 1, Y: 1, Text: "a", T0: new(int64), Format: VideoLandscape}); err == nil {
		t.Error("time on a deck is taken")
	}
	if _, err := g.l.AddDeckComment("admin", DeckCommentSpec{Page: page, X: 1, Y: 1, Text: "a", Block: "x"}); err == nil {
		t.Error("a block with no time is taken")
	}
	if _, err := g.l.DeckState("admin"); err != nil {
		t.Error(err)
	}
}

func TestVideoRevMovesWithAScene(t *testing.T) {
	v := newVideoRig(t)
	past := time.Now().Add(-time.Hour)
	old := func(paths ...string) {
		for _, p := range paths {
			if err := os.Chtimes(p, past, past); err != nil {
				t.Fatal(err)
			}
		}
	}
	page := v.pages[0].ID
	if _, err := v.l.SetVideoScene("admin", page, map[string]json.RawMessage{"dur": json.RawMessage("4000")}); err != nil {
		t.Fatal(err)
	}
	meta := v.l.sceneMetaFile("admin", page)
	// Everything of the deck and the video is old, the rig's own fresh files too; the rev is what it is.
	ageTree(t, filepath.Join(v.root, "admin"), past)
	r0 := v.l.videoRev("admin")
	if err := os.Chtimes(meta, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	r1 := v.l.videoRev("admin")
	if r1 <= r0 {
		t.Fatalf("a scene file moved: %d → %d", r0, r1)
	}
	old(meta)
	if _, err := v.l.SetVideoScene("admin", page, map[string]json.RawMessage{"dur": json.RawMessage("null")}); err != nil {
		t.Fatal(err)
	}
	if r2 := v.l.videoRev("admin"); r2 <= r0 {
		t.Fatalf("a scene file removed: %d → %d", r0, r2)
	}
	// The video's settings and the published video.json move it too.
	old(filepath.Dir(meta))
	r3 := v.l.videoRev("admin")
	if _, err := v.l.SetVideoSettings("admin", map[string]json.RawMessage{"subs": json.RawMessage("false")}); err != nil {
		t.Fatal(err)
	}
	if r4 := v.l.videoRev("admin"); r4 <= r3 {
		t.Fatalf("settings: %d → %d", r3, r4)
	}
	old(v.l.deckFile("admin", VideoSettingsFile))
	r5 := v.l.videoRev("admin")
	writeFile(t, v.l.deckFile("admin", VideoPublishedFile), `{"formats":{}}`)
	if r6 := v.l.videoRev("admin"); r6 <= r5 {
		t.Fatalf("published: %d → %d", r5, r6)
	}
	// The state's rev is the same number.
	if st := v.state(t); st.Rev != v.l.videoRev("admin") {
		t.Errorf("state rev %d", st.Rev)
	}
}

func TestVideoSceneChangesWithTheBoard(t *testing.T) {
	v := newVideoRig(t)
	before := v.state(t).Scenes[1]
	writeFile(t, filepath.Join(v.root, "admin", ProjectDirname, pageFileOf(v.pages[1].File)),
		`<main class="board"><p data-block="Текст" data-tk-at="6">text</p></main>`)
	after := v.state(t).Scenes[1]
	if before.Dur != 3000 || after.Dur != 8100 || len(after.Moments) != 1 || after.Moments[0].At != 6000 {
		t.Errorf("%d → %d: %+v", before.Dur, after.Dur, after.Moments)
	}
	if st := v.state(t); st.Scenes[2].Start != st.Scenes[0].Dur+8100 {
		t.Errorf("later scenes follow: %d", st.Scenes[2].Start)
	}
}

func TestVideoCLIRoutes(t *testing.T) {
	v := newVideoRig(t)
	v.a.CLIToken = func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" }
	cli := func(h func(*http.Request) Answer, method, target, body string) (int, map[string]any) {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, target, nil)
		} else {
			r = httptest.NewRequest(method, target, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("x-cli-token", "ok")
		code, val, _ := h(r).Split()
		raw, _ := json.Marshal(val)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return code, out
	}
	code, out := cli(v.a.CLIVideo, "GET", "/x?project=admin&what=state", "")
	if code != 200 || out["project"] != "admin" || len(out["state"].(map[string]any)["scenes"].([]any)) != 3 {
		t.Fatalf("state %d %v", code, out)
	}
	code, out = cli(v.a.CLIVideoScene, "POST", "/x", `{"project":"admin","page":"`+v.pages[1].Title+`","dur":12000}`)
	if code != 200 || out["scene"].(map[string]any)["dur"] != float64(12000) || out["project"] != "admin" {
		t.Fatalf("scene %d %v", code, out)
	}
	if code, _ = cli(v.a.CLIVideoScene, "POST", "/x", `{"project":"admin","page":"`+v.pages[1].Title+`","dur":5}`); code != 400 {
		t.Errorf("bad dur: %d", code)
	}
	c1, err := v.l.AddDeckComment("admin", DeckCommentSpec{Page: v.pages[0].ID, Text: "open", T0: new(int64), Format: VideoLandscape})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := v.l.AddDeckComment("admin", DeckCommentSpec{Page: v.pages[0].ID, X: 5, Y: 5, Text: "board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.l.ResolveDeckComment("admin", c2.ID, true); err != nil {
		t.Fatal(err)
	}
	code, out = cli(v.a.CLIVideo, "GET", "/x?project=admin&what=comments", "")
	if all := out["comments"].([]any); code != 200 || len(all) != 2 {
		t.Fatalf("comments %d %v", code, out)
	}
	code, out = cli(v.a.CLIVideo, "GET", "/x?project=admin&what=comments&open=1", "")
	if open := out["comments"].([]any); code != 200 || len(open) != 1 || open[0].(map[string]any)["id"] != c1.ID {
		t.Fatalf("open %d %v", code, out)
	}
	if code, _ = cli(v.a.CLIVideo, "GET", "/x?project=admin&what=nope", ""); code != 400 {
		t.Errorf("what: %d", code)
	}
	if code, _ = cli(v.a.CLIVideo, "POST", "/x?project=admin&what=state", `{}`); code != 405 {
		t.Errorf("POST: %d", code)
	}
	r := httptest.NewRequest("GET", "/x?project=admin&what=state", nil)
	if code, _, _ := v.a.CLIVideo(r).Split(); code != 403 {
		t.Errorf("no token: %d", code)
	}
}
