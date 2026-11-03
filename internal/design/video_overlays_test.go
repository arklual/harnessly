package design

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func mustParse(t *testing.T, s string) *jsvalue.Object {
	t.Helper()
	v, err := jsvalue.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return Fields(v)
}

func toolFailed(res *jsvalue.Object) bool {
	v, _ := res.Get("isError")
	b, _ := v.(bool)
	return b
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestVideoOverlayValidation(t *testing.T) {
	ok := videoOverlayDefaults(VideoOverlayTitle)
	ok.ID, ok.Text, ok.Author = "title-1", "Привет", VideoAuthorHuman
	if err := ValidateVideoOverlay(ok); err != nil {
		t.Fatalf("a good overlay: %v", err)
	}
	cases := map[string]func(*VideoOverlay){
		"id":       func(o *VideoOverlay) { o.ID = "Bad Id" },
		"kind":     func(o *VideoOverlay) { o.Kind = "marquee" },
		"text":     func(o *VideoOverlay) { o.Text = strings.Repeat("я", VideoOverlayTextMax+1) },
		"empty":    func(o *VideoOverlay) { o.Text = "  " },
		"at":       func(o *VideoOverlay) { o.At = -1 },
		"out":      func(o *VideoOverlay) { v := o.At + 50; o.Out = &v },
		"x":        func(o *VideoOverlay) { o.X = 1.2 },
		"outside":  func(o *VideoOverlay) { o.X, o.W = 0.5, 0.8 },
		"size":     func(o *VideoOverlay) { o.W = 0 },
		"enter":    func(o *VideoOverlay) { o.Enter = "spin" },
		"leave":    func(o *VideoOverlay) { o.Leave = "explode" },
		"theme":    func(o *VideoOverlay) { o.Theme = "neon" },
		"anchor":   func(o *VideoOverlay) { o.Anchor = "top" },
		"author":   func(o *VideoOverlay) { o.Author = "robot" },
		"portrait": func(o *VideoOverlay) { o.Portrait = &VideoOverlayPlace{X: 0.9, Y: 0, W: 0.5, H: 0.1} },
	}
	for name, mutate := range cases {
		o := ok
		mutate(&o)
		if err := ValidateVideoOverlay(o); err == nil {
			t.Errorf("%s: a bad overlay passed", name)
		}
	}
	callout := videoOverlayDefaults(VideoOverlayCallout)
	callout.ID, callout.Text, callout.Author = "callout-1", "Тут", VideoAuthorAgent
	callout.TX = nil
	if err := ValidateVideoOverlay(callout); err == nil {
		t.Error("a callout without a target passed")
	}
	counter := videoOverlayDefaults(VideoOverlayCounter)
	counter.ID, counter.Text, counter.Author, counter.Value = "counter-1", "{n} ₽", VideoAuthorAgent, nil
	if err := ValidateVideoOverlay(counter); err == nil {
		t.Error("a counter without a value passed")
	}
	many := make([]VideoOverlay, VideoOverlaysMax+1)
	if err := ValidateVideoOverlays(many); err == nil {
		t.Error("too many overlays passed")
	}
	twice := []VideoOverlay{ok, ok}
	if err := ValidateVideoOverlays(twice); err == nil {
		t.Error("a repeated id passed")
	}
}

func TestVideoOverlayPortraitSafeZone(t *testing.T) {
	o := videoOverlayDefaults(VideoOverlayCaption)
	o.X, o.Y, o.W, o.H = 0.0, 0.9, 0.9, 0.1
	p := VideoOverlayPlaceIn(o, VideoPortrait)
	z := videoSafeZone(VideoPortrait)
	if p.X < z.l-1e-9 || p.Y+p.H > 1-z.b+1e-9 || p.X+p.W > 1-z.r+1e-9 {
		t.Errorf("not inside the safe zone: %+v", p)
	}
	own := VideoOverlayPlace{X: 0.1, Y: 0.1, W: 0.5, H: 0.1}
	o.Portrait = &own
	if got := VideoOverlayPlaceIn(o, VideoPortrait); got != own {
		t.Errorf("an own place is kept: %+v", got)
	}
	if got := VideoOverlayPlaceIn(o, VideoLandscape); got.X != 0 || got.W != 0.9 {
		t.Errorf("16:9 uses the base place: %+v", got)
	}
	hl := videoOverlayDefaults(VideoOverlayHighlight)
	hl.X, hl.Y, hl.W, hl.H = 0, 0.9, 1, 0.1
	if got := VideoOverlayPlaceIn(hl, VideoPortrait); got.Y != 0.9 {
		t.Errorf("a highlight is an area of the picture and is not moved: %+v", got)
	}
}

// A scene's meta of the first version reads as it is and the next write makes it the second.
func TestVideoSceneMetaMigration(t *testing.T) {
	v := newVideoRig(t)
	pid := v.pages[0].ID
	file := v.l.sceneMetaFile("admin", pid)
	if err := os.MkdirAll(v.l.deckFile("admin", VideoScenesDir), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"v":1,"dur":5000,"transition":null}`), 0o666); err != nil {
		t.Fatal(err)
	}
	m, err := v.l.ReadVideoSceneMeta("admin", pid)
	if err != nil || m.Dur == nil || *m.Dur != 5000 || len(m.Overlays) != 0 {
		t.Fatalf("v1 file: %+v %v", m, err)
	}
	sc, id, err := v.l.AddVideoOverlay("admin", pid, "", map[string]json.RawMessage{"kind": raw(`"title"`), "text": raw(`"Привет"`)}, VideoAuthorAgent)
	if err != nil || id != "title-1" {
		t.Fatalf("add: %q %v", id, err)
	}
	if len(sc.Overlays) != 1 || sc.Dur != 5000 {
		t.Fatalf("the scene keeps its length and gets the overlay: %+v", sc)
	}
	b, _ := os.ReadFile(file)
	var onDisk VideoSceneMeta
	if err := json.Unmarshal(b, &onDisk); err != nil || onDisk.V != 2 || len(onDisk.Overlays) != 1 {
		t.Fatalf("the file is rewritten as v2: %s", b)
	}
}

func TestVideoOverlayStoreHumanWins(t *testing.T) {
	v := newVideoRig(t)
	pid := v.pages[0].ID
	add := func(actor VideoOverlayActor) string {
		_, id, err := v.l.AddVideoOverlay("admin", pid, "", map[string]json.RawMessage{"kind": raw(`"caption"`), "text": raw(`"Подпись"`)}, actor)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second := add(VideoAuthorAgent), add(VideoAuthorAgent)
	if first != "caption-1" || second != "caption-2" {
		t.Fatalf("ids: %s %s", first, second)
	}
	// The agent changes its own overlay.
	if _, err := v.l.SetVideoOverlay("admin", pid, first, map[string]json.RawMessage{"text": raw(`"Агент"`)}, VideoAuthorAgent, false); err != nil {
		t.Fatal(err)
	}
	// A person changes it: it is theirs now.
	sc, err := v.l.SetVideoOverlay("admin", pid, first, map[string]json.RawMessage{"x": raw(`0.2`)}, VideoAuthorHuman, false)
	if err != nil {
		t.Fatal(err)
	}
	var got VideoOverlay
	for _, o := range sc.Overlays {
		if o.ID == first {
			got = o
		}
	}
	if got.Author != VideoAuthorHuman || got.X != 0.2 || got.Text != "Агент" {
		t.Fatalf("after the person's edit: %+v", got)
	}
	// The agent may not touch it without force.
	_, err = v.l.SetVideoOverlay("admin", pid, first, map[string]json.RawMessage{"text": raw(`"Назад"`)}, VideoAuthorAgent, false)
	var de *DeckError
	if !errors.As(err, &de) || de.Status != http409 {
		t.Fatalf("the agent changed a person's overlay: %v", err)
	}
	if _, err := v.l.RemoveVideoOverlay("admin", pid, first, VideoAuthorAgent, false); err == nil {
		t.Fatal("the agent removed a person's overlay")
	}
	// With force it may, and the overlay is the agent's again.
	sc, err = v.l.SetVideoOverlay("admin", pid, first, map[string]json.RawMessage{"text": raw(`"Назад"`)}, VideoAuthorAgent, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range sc.Overlays {
		if o.ID == first && (o.Text != "Назад" || o.Author != VideoAuthorAgent) {
			t.Fatalf("forced edit: %+v", o)
		}
	}
	// A person's own removal always works; so does a bad patch's refusal.
	if _, err := v.l.SetVideoOverlay("admin", pid, second, map[string]json.RawMessage{"x": raw(`5`)}, VideoAuthorHuman, false); err == nil {
		t.Fatal("a position outside the frame was accepted")
	}
	if _, err := v.l.SetVideoOverlay("admin", pid, second, map[string]json.RawMessage{"author": raw(`"human"`)}, VideoAuthorAgent, false); err == nil {
		t.Fatal("the author was set by hand")
	}
	if _, err := v.l.RemoveVideoOverlay("admin", pid, second, VideoAuthorHuman, false); err != nil {
		t.Fatal(err)
	}
	if _, err := v.l.RemoveVideoOverlay("admin", pid, "nope", VideoAuthorHuman, false); err == nil {
		t.Fatal("a missing overlay was removed")
	}
}

// Editing an overlay changes the clip key of its scene and of no other.
func TestVideoOverlayCacheKey(t *testing.T) {
	v := newVideoRig(t)
	a, b := v.pages[0].ID, v.pages[1].ID
	st, _ := v.l.ReadVideoSettings("admin")
	keys := func() (string, string) {
		scenes, err := v.l.VideoScenes("admin", st)
		if err != nil {
			t.Fatal(err)
		}
		var ka, kb string
		for _, sc := range scenes {
			switch sc.ID {
			case a:
				ka = sc.Keys[VideoLandscape]
			case b:
				kb = sc.Keys[VideoLandscape]
			}
		}
		return ka, kb
	}
	ka0, kb0 := keys()
	if ka0 == "" || kb0 == "" {
		t.Fatalf("no keys: %q %q", ka0, kb0)
	}
	_, id, err := v.l.AddVideoOverlay("admin", a, "", map[string]json.RawMessage{"kind": raw(`"title"`), "text": raw(`"Один"`)}, VideoAuthorAgent)
	if err != nil {
		t.Fatal(err)
	}
	ka1, kb1 := keys()
	if ka1 == ka0 || kb1 != kb0 {
		t.Fatalf("adding changes only its scene's key: a %v b %v", ka1 != ka0, kb1 != kb0)
	}
	if _, err := v.l.SetVideoOverlay("admin", a, id, map[string]json.RawMessage{"text": raw(`"Два"`)}, VideoAuthorHuman, false); err != nil {
		t.Fatal(err)
	}
	if ka2, kb2 := keys(); ka2 == ka1 || kb2 != kb0 {
		t.Fatal("editing changes only its scene's key")
	}
}

func TestVideoOverlayToolsAndRoutes(t *testing.T) {
	v := newVideoRig(t)
	tools := VideoTools{L: v.l, Project: "admin"}
	for _, n := range []string{"sinopia_overlay_list", "sinopia_overlay_add", "sinopia_overlay_set", "sinopia_overlay_remove"} {
		if !tools.Has(n) {
			t.Errorf("no tool %s", n)
		}
	}
	// The route's body is a person's, the tool's is the agent's.
	body := (mustParse(t, `{"page":"`+v.pages[0].ID+`","action":"add","kind":"callout","text":"Тут","x":0.2}`))
	out, err := v.l.videoOverlayOp("admin", body, VideoAuthorHuman)
	if err != nil || out.Overlay == nil || out.Overlay.Author != VideoAuthorHuman || out.Overlay.X != 0.2 {
		t.Fatalf("route add: %+v %v", out, err)
	}
	args := mustParse(t, `{"scene":"`+v.pages[0].ID+`","id":"`+out.Overlay.ID+`","text":"Агент"}`)
	res := tools.Call(context.Background(), "sinopia_overlay_set", args)
	if !toolFailed(res) {
		t.Fatal("the tool changed a person's overlay without force")
	}
	args = mustParse(t, `{"scene":"`+v.pages[0].ID+`","id":"`+out.Overlay.ID+`","text":"Агент","force":true,"at":1.5,"out":3}`)
	if res = tools.Call(context.Background(), "sinopia_overlay_set", args); toolFailed(res) {
		t.Fatalf("forced: %v", res)
	}
	list, _ := v.l.VideoOverlaysOf("admin", v.pages[0].ID)
	if len(list) != 1 || list[0].Text != "Агент" || list[0].At != 1500 || list[0].Out == nil || *list[0].Out != 3000 {
		t.Fatalf("seconds became ms: %+v", list)
	}
}

func TestVideoOverlayClashes(t *testing.T) {
	mk := func(id string, x, y float64, at int64, out *int64) VideoOverlay {
		o := videoOverlayDefaults(VideoOverlayCaption)
		o.ID, o.X, o.Y, o.W, o.H, o.At, o.Out = id, x, y, 0.3, 0.1, at, out
		return o
	}
	end := int64(1000)
	list := []VideoOverlay{mk("a", 0.1, 0.1, 0, nil), mk("b", 0.15, 0.12, 500, nil), mk("c", 0.1, 0.1, 2000, nil), mk("d", 0.6, 0.6, 0, nil), mk("e", 0.1, 0.1, 0, &end)}
	got := VideoOverlayClashes(list, "a")
	if len(got) != 3 || got[0] != "b" || got[1] != "c" || got[2] != "e" {
		t.Fatalf("a clashes with %v", got)
	}
	if got := VideoOverlayClashes(list, "d"); len(got) != 0 {
		t.Fatalf("d stands alone: %v", got)
	}
	hl := mk("h", 0.1, 0.1, 0, nil)
	hl.Kind = VideoOverlayHighlight
	if got := VideoOverlayClashes(append(list, hl), "h"); got != nil {
		t.Fatalf("a highlight is an area: %v", got)
	}
}
